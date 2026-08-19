package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 批量执行引擎（任务 5.7）：Confirm 批次分配固化 + Execute 子任务派发 +
// 逐项隔离 + 30s 心跳 + executing-timeout 恢复 + rate_limited 退避 +
// ConfirmBatch 人工续批门控 + 订单状态按剩余项重算。
//
// 子任务执行器挂 internal/task 框架（pkg/taskx.Queue + TaskExecutor，
// 参考 internal/task executor 既有用法）；执行编排与业务级状态机判断
//（成功/失败/重算收敛）归本层，通道只做单次动作（Hard Rule，见 deployer 包）。
// ---------------------------------------------------------------------

// ChangeExecuteService 变更执行编排（tech-design Interface 3 的
// Confirm/Execute/ConfirmBatch + executing-timeout 恢复函数）。
//
// 互斥正确性由 uk_active_mutex 部分唯一索引保证（5.1 体系）；本服务全部
// 状态迁移经仓储原子原语（CAS 条件 update），与 Cancel/pause-timeout 并发安全。
type ChangeExecuteService interface {
	// Confirm 人工确认：快照确认时点重校验（新鲜度/引用一致性）+ 批次分配
	// 一次性固化。批次分配规则（tech-design"分批执行门控"）：
	//   - items 按 (cloud, product, resourceId) 字典序稳定排序；
	//   - 首批 = min(BatchSize, floor(total/2))——单批 ≤ floor(total/2) 为硬约束
	//     （PRD 分批灰度 ≤50%），越界 Confirm 拒绝；
	//   - 余下项按 BatchSize 均分为后续批（末批可不足额），逐项写 batchNo；
	//   - batchInfo{totalBatches, currentBatch=1, batchSize=有效批大小, paused=false}。
	// 状态门控：仅 pending_confirm 可确认；批次分配一次性固化（重复 Confirm 拒绝）。
	// Enable=false 单批全量仅 total≤1 允许（Hard Rule 推论：单批全量必然越界
	// floor(total/2)，total≥2 须分批）。
	Confirm(ctx context.Context, orderID string, conf deployer.BatchConf) error
	// Execute 派发子任务执行当前批（ChangeItem.batchNo = batchInfo.currentBatch）：
	// pending_confirm→executing（首批）或 executing 续批（ConfirmBatch 放行后）；
	// 批间暂停（executing+paused）返回 409 BATCH_NOT_CONFIRMABLE（分批一律人工续批，
	// 不提供自动续批）；仅派发当前批 pending 项，逐项隔离（派发失败不阻塞其他项，
	// 失败项保持 pending，重入 Execute 幂等补派）。
	Execute(ctx context.Context, orderID string) error
	// ConfirmBatch 人工续批门控：仅当上一批全部 success（skipped=不可执行项不
	// 计分母，放行）且批级验证达标（提频探测连续 verifyConfirmProbes 次一致，
	// 判定由 5.10 经 BatchVerifyChecker 提供）→ currentBatch+1 放行；否则 409
	// BATCH_NOT_CONFIRMABLE。批间暂停标记（batchInfo.paused=true/pausedAt）在
	// 批执行完成进入验证窗口时固化（EnterVerify 原子写入）。
	ConfirmBatch(ctx context.Context, orderID string) error
	// ExecuteItem 子任务执行单体（ItemRunner 入口，语义见下方接口定义）：
	// 7.1 装配时 taskx ChangeItemExecutor 经本方法驱动队列子任务——列入本
	// 接口使模块装配可凭接口值直接构造执行器（additive，实现已存在）。
	ExecuteItem(ctx context.Context, orderID, itemID string) error
	// RecoverTimedOutItems executing-timeout 恢复函数（Scheduler Tasks 表，
	// 7.1 以 5 分钟周期调度）：扫 running 且 heartbeatAt +
	// thresholds.itemHeartbeatTimeoutMinutes < now 的项 → failed(EXEC_TIMEOUT)
	// + 告警 + 单据状态按剩余项重算。返回恢复条数；单笔失败不中断扫描，
	// 首批错误随恢复计数一并返回。
	RecoverTimedOutItems(ctx context.Context) (int, error)
}

// 编译期断言：执行服务接口满足子任务执行入口（7.1 模块装配消费）。
var _ ItemRunner = (ChangeExecuteService)(nil)

// ---------------------------------------------------------------------
// 注入端口
// ---------------------------------------------------------------------

// BatchVerifyChecker 批级验证达标判定端口（任务 5.10 实现，本任务以接口消费，
// 5.10 完成后联调）：verifyExpected.domains 提频探测连续 verifyConfirmProbes
// 次一致 = 达标。reason 为安全文案（409 上下文，不含凭证/私钥片段）。
type BatchVerifyChecker interface {
	// BatchVerified 返回（达标?，未达标原因，判定通道自身错误）。
	// err 非 nil 时调用方按门控未满足处理（安全侧：不放行）。
	BatchVerified(ctx context.Context, order domain.ChangeOrder) (bool, string, error)
}

// VerifyWindowSealer 验证窗口预期终态固化端口（任务 5.10 实现，5.7 EnterVerify
// 缝消费）：批执行完成进入 verifying 后立即固化 verifyExpected（该批窗口判定
// 依据，固化后不随台账变化）。nil=固化缝未接线（5.10 调度入口惰性补固化兜底）。
type VerifyWindowSealer interface {
	// SealVerifyExpected 固化 verifyExpected 快照；返回（是否写入，错误）。
	// CAS status=verifying 未命中返回 false（并发迁移，幂等）。
	SealVerifyExpected(ctx context.Context, orderID string) (bool, error)
}

// ExecuteAlertNotifier 执行期运维处置通知端口（executing-timeout 恢复事件；
// nil=no-op）。运维处置类通知，不计入 PRD 四类业务告警口径（同 scan-timeout/
// pause-timeout 通知口径，见 alert_events.go）；7.1 调度装配时接入 internal/alert
// 通道，通知失败不阻塞恢复流程。
type ExecuteAlertNotifier interface {
	// NotifyItemTimedOut 心跳超时项恢复通知（附订单/项定位与心跳时点）。
	NotifyItemTimedOut(ctx context.Context, orderID, itemID string, heartbeatAt, recoveredAt time.Time) error
}

// ItemRunner 子任务执行入口（taskx 执行器与同步派发共用；
// *changeExecuteService 实现）。
type ItemRunner interface {
	// ExecuteItem 执行单个变更项：领取（CAS pending→running）→ 心跳 30s →
	// 凭 resourceRef 重构 DeployTarget 经通道部署 → 限流退避 → 项级终态 +
	// 订单状态重算。幂等：项非 pending（已领取/已终态）时仅触发重算后返回。
	ExecuteItem(ctx context.Context, orderID, itemID string) error
}

// SubtaskDispatcher 子任务派发端口：Execute 编排 → internal/task 框架。
// 生产实现 TaskxItemDispatcher（pkg/taskx.Queue.Submit）；测试/降级可注入
// 同步执行器。
type SubtaskDispatcher interface {
	DispatchItem(ctx context.Context, orderID, itemID string) error
}

// ChannelCredentialSource 执行通道凭证来源端口：按变更项 resourceRef 解析
// deployer.Credential（解密后内存形态；Secret 用后 Zeroize、禁入日志/响应，
// 完整生命周期由本引擎管理：解密→逐项传递→用毕清零）。
type ChannelCredentialSource interface {
	// CloudCredential 云账号 AK/SK 凭证（cloud_api 通道）。
	CloudCredential(ctx context.Context, cloud, accountKey string) (deployer.Credential, error)
	// K8sCredential 集群 kubeconfig 凭证（k8s_api 通道形态校验用；连接材料由
	// 通道的 CRDClientProvider 按 clusterName 解析，凭证载荷不承载连接）。
	K8sCredential(ctx context.Context, clusterID string) (deployer.Credential, error)
}

// ---------------------------------------------------------------------
// 限流退避策略（AC-4：退避上限耗尽 → failed）
// ---------------------------------------------------------------------

// defaultItemHeartbeatInterval 执行期心跳间隔（tech-design：固定 30 秒）。
const defaultItemHeartbeatInterval = 30 * time.Second

// ItemRateLimitPolicy 项级限流退避策略：部署器（5.4/5.5）内部已有有界退避
// （哨兵语义经 %w 透传），本策略为引擎级外层闸门——项遇 CLOUD_API_RATELIMITED
// 标记 rate_limited（进度轮询可见"限流重试中"）后按序列退避重试，次数或总
// 时长任一耗尽即收敛 failed。双闸门防无限重试（Hard Rule 同 5.4/5.5 口径）。
type ItemRateLimitPolicy struct {
	MaxAttempts  int             // 总尝试次数上限（含首次），>=1
	Backoffs     []time.Duration // 固定退避序列：第 n 次失败后等待 Backoffs[n-1]，序列耗尽沿用末值
	MaxTotalWait time.Duration   // 退避总时长上限；下一档将超限即停止
}

// DefaultItemRateLimitPolicy 缺省保守策略：3 次尝试（2 次引擎级退避），
// 30s/2m 序列，总退避上限 3m。
func DefaultItemRateLimitPolicy() ItemRateLimitPolicy {
	return ItemRateLimitPolicy{
		MaxAttempts:  3,
		Backoffs:     []time.Duration{30 * time.Second, 2 * time.Minute},
		MaxTotalWait: 3 * time.Minute,
	}
}

// normalized 零值/非法配置回退缺省（重试安全侧）。
func (p ItemRateLimitPolicy) normalized() ItemRateLimitPolicy {
	def := DefaultItemRateLimitPolicy()
	if p.MaxAttempts < 1 || len(p.Backoffs) == 0 || p.MaxTotalWait <= 0 {
		return def
	}
	for _, b := range p.Backoffs {
		if b <= 0 {
			return def
		}
	}
	return p
}

// waitAfter 第 failedAttempts 次失败后的退避时长；ok=false 表示退避上限已耗尽
// （次数或总时长闸门任一命中，收敛 failed）。
func (p ItemRateLimitPolicy) waitAfter(failedAttempts int, waited time.Duration) (time.Duration, bool) {
	if failedAttempts >= p.MaxAttempts {
		return 0, false
	}
	idx := failedAttempts - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(p.Backoffs) {
		idx = len(p.Backoffs) - 1
	}
	backoff := p.Backoffs[idx]
	if waited+backoff > p.MaxTotalWait {
		return 0, false
	}
	return backoff, true
}

// 项级 error 字段可机读前缀（ChangeItem.error = "码: 详情"；EXEC_TIMEOUT/
// CLOUD_API_RATELIMITED/K8S_UNREACHABLE 为 domain 已声明码值）。
const (
	itemErrExecFailed = "EXEC_FAILED"
	itemErrExecPanic  = "EXEC_PANIC"
)

// ---------------------------------------------------------------------
// 服务实现
// ---------------------------------------------------------------------

type changeExecuteService struct {
	orders    domain.ChangeOrderRepository
	items     domain.ChangeItemRepository
	certs     domain.CertificateRepository
	alertCfg  domain.AlertConfigRepository
	snapshots domain.ScanSnapshotRepository // Confirm 快照确认时点重校验
	refs      domain.CertReferenceRepository

	channels map[string]deployer.ExecutionChannel // 按 resourceRef.channel 路由（cloud_api/k8s_api）
	creds    ChannelCredentialSource
	dispatch SubtaskDispatcher
	verify   BatchVerifyChecker   // 5.10 注入；nil=门控未满足（安全侧不放行）
	sealer   VerifyWindowSealer   // 5.10 注入；nil=固化缝未接线（调度惰性补固化）
	notifier ExecuteAlertNotifier // nil=no-op
	audit    ChangeAuditWriter    // 7.2 item_result 审计；nil=no-op

	heartbeatInterval time.Duration                                    // 执行期心跳间隔（默认 30s）
	rateLimit         ItemRateLimitPolicy                              // 项级限流退避（引擎级外层闸门）
	now               func() time.Time                                 // 测试可注入时间源
	sleep             func(ctx context.Context, d time.Duration) error // 测试可注入退避睡眠
}

// NewChangeExecuteService 创建批量执行引擎。channels 为已装配执行通道实例
// （5.3/5.6 产物，按 Type() 路由）；dispatch 为子任务派发器（生产装配：
// TaskxItemDispatcher + 同队列注册 NewChangeItemExecutor）；sealer 为 5.10
// 验证窗口固化缝（批执行完成进入 verifying 时固化 verifyExpected；nil=未接线）。
// audit 为 7.2 item_result 审计写入端口（nil=no-op，生产装配接线 internal/audit）。
func NewChangeExecuteService(
	orders domain.ChangeOrderRepository,
	items domain.ChangeItemRepository,
	certs domain.CertificateRepository,
	alertCfg domain.AlertConfigRepository,
	snapshots domain.ScanSnapshotRepository,
	refs domain.CertReferenceRepository,
	channels []deployer.ExecutionChannel,
	creds ChannelCredentialSource,
	dispatch SubtaskDispatcher,
	verify BatchVerifyChecker,
	sealer VerifyWindowSealer,
	notifier ExecuteAlertNotifier,
	audit ChangeAuditWriter,
) ChangeExecuteService {
	chs := make(map[string]deployer.ExecutionChannel, len(channels))
	for _, c := range channels {
		if c != nil {
			chs[string(c.Type())] = c
		}
	}
	return &changeExecuteService{
		orders:            orders,
		items:             items,
		certs:             certs,
		alertCfg:          alertCfg,
		snapshots:         snapshots,
		refs:              refs,
		channels:          chs,
		creds:             creds,
		dispatch:          dispatch,
		verify:            verify,
		sealer:            sealer,
		notifier:          notifier,
		audit:             audit,
		heartbeatInterval: defaultItemHeartbeatInterval,
		rateLimit:         DefaultItemRateLimitPolicy(),
		now:               time.Now,
		sleep:             sleepWithContext,
	}
}

// 编译期断言。
var _ ItemRunner = (*changeExecuteService)(nil)

// sleepWithContext 退避睡眠（可被 ctx 取消打断）。
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---------------------------------------------------------------------
// Confirm：快照重校验 + 批次分配一次性固化
// ---------------------------------------------------------------------

// Confirm 人工确认（批次分配固化）。校验次序：状态门控 → 一次性固化门控 →
// 快照新鲜度 → 引用一致性 → BatchConf 硬校验 → 分配算法 → 落库
// （AssignBatches + SetBatchInfo CAS pending_confirm）。
func (s *changeExecuteService) Confirm(ctx context.Context, orderID string, conf deployer.BatchConf) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("change: get order: %w", err)
	}
	if order.Status != domain.ChangeStatusPendingConfirm {
		return fmt.Errorf("change: confirm requires pending_confirm order, got %s", order.Status)
	}
	if order.BatchInfo != nil {
		return fmt.Errorf("change: batch allocation already fixed for order %s (一次性固化，不可重复确认)", orderID)
	}

	// ---- 快照确认时点重校验：新鲜度 ----
	snap, err := s.snapshots.GetByID(ctx, order.SnapshotID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fmt.Errorf("%w: bound snapshot %s no longer exists", domain.ErrScanStale, order.SnapshotID)
		}
		return fmt.Errorf("change: load snapshot: %w", err)
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return fmt.Errorf("change: get alert config: %w", err)
	}
	if age := s.now().Sub(snap.StartedAt); age > time.Duration(cfg.Thresholds.ScanFreshnessHours)*time.Hour {
		return fmt.Errorf("%w: snapshot age %dh exceeds scanFreshnessHours=%d at confirm time",
			domain.ErrScanStale, int(age.Hours()), cfg.Thresholds.ScanFreshnessHours)
	}

	// ---- 快照确认时点重校验：引用一致性（清单项仍与绑定快照逐资源对应） ----
	items, err := s.items.ListByOrder(ctx, orderID)
	if err != nil {
		return fmt.Errorf("change: list items: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("change: order %s has no items to batch", orderID)
	}
	if err := s.verifyReferenceConsistency(ctx, order, items); err != nil {
		return err
	}

	// ---- 批次分配（含硬校验） ----
	if err := deployer.ValidateBatchConf(conf); err != nil {
		return fmt.Errorf("change: %w", err)
	}
	assignments, batchInfo, err := allocateBatches(items, conf)
	if err != nil {
		return err
	}
	if err := s.items.AssignBatches(ctx, orderID, assignments); err != nil {
		return fmt.Errorf("change: assign batches: %w", err)
	}
	ok, err := s.orders.SetBatchInfo(ctx, orderID, batchInfo)
	if err != nil {
		return fmt.Errorf("change: set batch info: %w", err)
	}
	if !ok {
		return fmt.Errorf("change: order %s left pending_confirm concurrently, batch allocation aborted", orderID)
	}
	return nil
}

// verifyReferenceConsistency 引用一致性重校验（AC-1）：绑定快照内该旧证书指纹
// 的去重资源集合与持久化清单项一一对应（执行期间清单快照固定不变的前置保障；
// 不一致映射 409 SCAN_STALE——快照与清单已不可信，需重新生成清单）。
func (s *changeExecuteService) verifyReferenceConsistency(ctx context.Context, order domain.ChangeOrder, items []domain.ChangeItem) error {
	snapRefs, err := s.refs.ListBySnapshotID(ctx, order.SnapshotID)
	if err != nil {
		return fmt.Errorf("change: load snapshot references: %w", err)
	}
	want := make(map[string]struct{}, len(snapRefs))
	for _, r := range snapRefs {
		if r.CertFingerprint != order.OldCertFingerprint {
			continue
		}
		want[resourceDedupKey(r)] = struct{}{}
	}
	got := make(map[string]struct{}, len(items))
	for _, it := range items {
		got[itemRefKey(it.ResourceRef)] = struct{}{}
	}
	if len(want) != len(got) {
		return fmt.Errorf("%w: snapshot has %d referenced resources but order holds %d items",
			domain.ErrScanStale, len(want), len(got))
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			return fmt.Errorf("%w: order item %q not present in bound snapshot references", domain.ErrScanStale, key)
		}
	}
	return nil
}

// itemRefKey 变更项 resourceRef → 资源去重键（与 resourceDedupKey 同键空间：
// 云引用 (cloud,product,accountKey,resourceId)、K8s 引用
// (clusterId,namespace,kind,resourceId)——K8s 项按 Channel 分支归一到
// product=crd 形态）。
func itemRefKey(ref domain.ResourceRef) string {
	r := domain.CertReference{
		Cloud:      domain.Cloud(ref.Cloud),
		Product:    domain.Product(ref.Product),
		AccountKey: ref.AccountKey,
		ClusterID:  ref.ClusterID,
		Namespace:  ref.Namespace,
		Kind:       ref.Kind,
		ResourceID: ref.ResourceID,
	}
	if ref.Channel == domain.ChannelK8sAPI {
		r.Product = domain.ProductCRD
	}
	return resourceDedupKey(r)
}

// allocateBatches 批次分配纯函数（AC-1）：
//   - items 按 (cloud, product, resourceId) 字典序稳定排序（clusterId/
//     namespace/kind 作确定性 tie-breaker，末位 itemID 兜底）；
//   - Enabled=true：有效批大小 = EffectiveBatchSize（min(BatchSize,
//     floor(total/2))，硬约束单批 ≤ floor(total/2)，对全部批生效——BatchSize
//     超过 floor 时后续批同样按 floor 截断）；首批 = 有效批大小，余下按同一
//     批大小均分（末批可不足额）；有效批大小 ≥ total（单引用等微小清单）
//     退化为单批；
//   - Enabled=false：单批全量仅 total≤1 允许（Hard Rule：单批全量必然越界
//     floor(total/2)，total≥2 须分批）；
//   - MaxBatchRatio 交叉校验：有效批大小/total ≤ MaxBatchRatio（对实际生效
//     的批大小校验——仅发生拆分时；单引用清单无灰度拆分语义）。
//
// 返回逐项 batchNo 指派与 batchInfo（currentBatch=1、paused=false）。
func allocateBatches(items []domain.ChangeItem, conf deployer.BatchConf) ([]domain.ItemBatchAssignment, *domain.BatchInfo, error) {
	sorted := make([]domain.ChangeItem, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].ResourceRef, sorted[j].ResourceRef
		if a.Cloud != b.Cloud {
			return a.Cloud < b.Cloud
		}
		if a.Product != b.Product {
			return a.Product < b.Product
		}
		if a.ResourceID != b.ResourceID {
			return a.ResourceID < b.ResourceID
		}
		// 确定性 tie-breaker（同资源多形态理论上不存在，防御性稳定排序）
		if a.ClusterID != b.ClusterID {
			return a.ClusterID < b.ClusterID
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return sorted[i].ID.Hex() < sorted[j].ID.Hex()
	})
	total := len(sorted)

	if !conf.Enabled {
		if total > 1 {
			return nil, nil, fmt.Errorf("%w: 单批全量仅 total<=1 允许（Hard Rule 单批≤floor(total/2)），%d 项须分批",
				deployer.ErrInvalidBatchConf, total)
		}
		return batchAssignmentsFor(sorted, 0, total), &domain.BatchInfo{
			TotalBatches: 1, CurrentBatch: 1, BatchSize: total,
		}, nil
	}

	effective := deployer.EffectiveBatchSize(conf.BatchSize, total)
	if effective >= total {
		// 微小清单：无灰度拆分意义（EffectiveBatchSize total<2 语义）
		return batchAssignmentsFor(sorted, 0, total), &domain.BatchInfo{
			TotalBatches: 1, CurrentBatch: 1, BatchSize: effective,
		}, nil
	}
	if ratio := float64(effective) / float64(total); ratio > conf.MaxBatchRatio {
		return nil, nil, fmt.Errorf("%w: effective batch size %d is %g of total %d, exceeds maxBatchRatio %g",
			deployer.ErrInvalidBatchConf, effective, ratio, total, conf.MaxBatchRatio)
	}

	// Hard Rule（单批 ≤ floor(total/2)）对全部批生效：后续批按有效批大小
	//（= min(BatchSize, floor(total/2))）均分，BatchSize ≤ floor 时即为
	// BatchSize 本身（tech-design"余下按 BatchSize 均分，末批可不足额"）。
	rest := total - effective
	totalBatches := 1 + (rest+effective-1)/effective // 余下均分（末批可不足额）
	return batchAssignmentsFor(sorted, effective, effective), &domain.BatchInfo{
		TotalBatches: totalBatches, CurrentBatch: 1, BatchSize: effective,
	}, nil
}

// batchAssignmentsFor 按排序序列切批：首个切片 first 项为批 1，其后每
// batchSize 项一批（末批可不足额）。
func batchAssignmentsFor(sorted []domain.ChangeItem, first, batchSize int) []domain.ItemBatchAssignment {
	out := make([]domain.ItemBatchAssignment, 0, len(sorted))
	batch, count := 1, 0
	capacity := first
	if capacity == 0 { // 单批全量（total<=1）路径
		capacity = batchSize
	}
	for _, it := range sorted {
		if count == capacity {
			batch++
			count = 0
			capacity = batchSize
		}
		out = append(out, domain.ItemBatchAssignment{ItemID: it.ID.Hex(), BatchNo: batch})
		count++
	}
	return out
}

// ---------------------------------------------------------------------
// Execute：派发当前批
// ---------------------------------------------------------------------

// Execute 派发子任务执行当前批（AC-2）。
func (s *changeExecuteService) Execute(ctx context.Context, orderID string) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("change: get order: %w", err)
	}
	switch order.Status {
	case domain.ChangeStatusPendingConfirm:
		if order.BatchInfo == nil {
			return fmt.Errorf("change: order %s not confirmed yet (批次分配未固化，先 Confirm)", orderID)
		}
		// 首批执行：pending_confirm→executing（token 写入同一原子 update）
		if err := s.orders.TransitionActive(ctx, orderID, domain.ChangeStatusExecuting, order.OldCertFingerprint); err != nil {
			return fmt.Errorf("change: transition to executing: %w", err)
		}
	case domain.ChangeStatusExecuting:
		// 批间暂停：分批一律人工续批（Hard Rule），未放行前不可执行下一批
		if order.BatchInfo != nil && order.BatchInfo.Paused {
			return fmt.Errorf("%w: order %s paused between batches (current=%d), ConfirmBatch first",
				domain.ErrBatchNotConfirmable, orderID, order.BatchInfo.CurrentBatch)
		}
	default:
		return &domain.InvalidTransitionError{From: order.Status, To: domain.ChangeStatusExecuting}
	}

	currentBatch := 1
	if order.BatchInfo != nil {
		currentBatch = order.BatchInfo.CurrentBatch
	}
	batchItems, err := s.items.ListByOrderAndBatch(ctx, orderID, currentBatch)
	if err != nil {
		return fmt.Errorf("change: list batch items: %w", err)
	}
	dispatched := 0
	var firstErr error
	for _, it := range batchItems {
		if it.Status != domain.ItemStatusPending {
			continue // 不可执行项（生成期已 skipped）/已领取/已终态
		}
		if err := s.dispatch.DispatchItem(ctx, orderID, it.ID.Hex()); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("change: dispatch item %s: %w", it.ID.Hex(), err)
			}
			continue // 逐项隔离：派发失败不阻塞其他项（失败项保持 pending，重入幂等补派）
		}
		dispatched++
	}
	if firstErr != nil {
		return firstErr
	}
	if dispatched == 0 {
		// 当前批无待派发项：幂等收敛（全 skipped/已完成的订单按剩余项重算）
		return s.recomputeOrderStatus(ctx, orderID)
	}
	return nil
}

// ---------------------------------------------------------------------
// ExecuteItem：子任务执行单体（逐项隔离 + 心跳 + 限流退避）
// ---------------------------------------------------------------------

// ExecuteItem 执行单个变更项（AC-2/AC-3/AC-4）：
//   - 仅凭持久化 resourceRef 重构 DeployTarget（不回查台账/快照——执行期
//     清单快照固定不变，Hard Rule）；
//   - 逐项隔离：单项 panic/失败 recover 并落项级状态，不阻塞其他项；
//   - 心跳：执行期按 heartbeatInterval（默认 30s）更新 heartbeatAt；
//   - rate_limited：CLOUD_API_RATELIMITED → status=rate_limited 退避重试，
//     引擎级退避上限耗尽 → failed；
//   - 每项终态变更后按剩余项重算订单状态。
func (s *changeExecuteService) ExecuteItem(ctx context.Context, orderID, itemID string) (err error) {
	item, err := s.items.GetByID(ctx, itemID)
	if err != nil {
		return fmt.Errorf("change: load item %s: %w", itemID, err)
	}
	if item.OrderID != orderID {
		return fmt.Errorf("change: item %s does not belong to order %s", itemID, orderID)
	}
	claimed, err := s.items.ClaimForExecution(ctx, itemID, s.now())
	if err != nil {
		return fmt.Errorf("change: claim item %s: %w", itemID, err)
	}
	if !claimed {
		// 幂等：任务框架重投递/并发双派发落败方——触发重算（修复卡滞）后跳过
		return s.recomputeOrderStatus(ctx, orderID)
	}

	// 逐项隔离：单项 panic 落项级 failed（AC-2），不向任务框架抛出
	//（项级状态已收敛，框架重试无意义）。
	defer func() {
		if r := recover(); r != nil {
			s.finishItem(ctx, orderID, itemID, domain.ItemStatusFailed,
				fmt.Sprintf("%s: %v", itemErrExecPanic, r), "")
			err = nil
		}
	}()

	stopHeartbeat := s.startHeartbeat(ctx, itemID)
	defer stopHeartbeat()

	channel, ok := s.channels[string(item.ResourceRef.Channel)]
	if !ok {
		s.finishItem(ctx, orderID, itemID, domain.ItemStatusFailed,
			fmt.Sprintf("%s: no execution channel assembled for %q", itemErrExecFailed, item.ResourceRef.Channel), "")
		return nil
	}
	creds, err := s.credentialFor(ctx, item.ResourceRef)
	if err != nil {
		s.finishItem(ctx, orderID, itemID, domain.ItemStatusFailed,
			fmt.Sprintf("%s: resolve credential: %v", itemErrExecFailed, err), "")
		return nil
	}
	// 新证书指纹：订单 NewCertID → 台账指纹（两段式上传/CRD patch 目标证书）
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		s.finishItem(ctx, orderID, itemID, domain.ItemStatusFailed,
			fmt.Sprintf("%s: load order: %v", itemErrExecFailed, err), "")
		return nil
	}
	newCert, err := s.certs.GetByID(ctx, order.NewCertID)
	if err != nil {
		s.finishItem(ctx, orderID, itemID, domain.ItemStatusFailed,
			fmt.Sprintf("%s: load new certificate %s: %v", itemErrExecFailed, order.NewCertID, err), "")
		return nil
	}

	target := deployer.DeployTargetFromResourceRef(item.ResourceRef) // 仅凭持久化 resourceRef 重构
	policy := s.rateLimit.normalized()
	waited := time.Duration(0)
	attempt := 0
	for {
		attempt++
		result, derr := channel.Deploy(ctx, creds, target, newCert.Fingerprint)
		if derr == nil {
			s.finishItem(ctx, orderID, itemID, domain.ItemStatusSuccess, "", result.NewCloudCertID)
			return nil
		}
		if errors.Is(derr, cloudx.ErrCloudRateLimited) {
			backoff, retryable := policy.waitAfter(attempt, waited)
			if !retryable {
				s.finishItem(ctx, orderID, itemID, domain.ItemStatusFailed,
					fmt.Sprintf("%s: 限流退避上限耗尽 after %d attempts (total backoff %s): %v",
						domain.CodeCloudApiRateLimited, attempt, waited, derr), "")
				return nil
			}
			// 进度轮询可见"限流重试中"（AC-4），心跳随标记刷新保活
			if err := s.items.MarkRateLimited(ctx, itemID, s.now()); err != nil {
				return fmt.Errorf("change: mark item %s rate limited: %w", itemID, err)
			}
			if serr := s.sleep(ctx, backoff); serr != nil {
				return fmt.Errorf("change: item %s rate limit backoff interrupted: %w", itemID, serr)
			}
			waited += backoff
			continue
		}
		s.finishItem(ctx, orderID, itemID, domain.ItemStatusFailed,
			fmt.Sprintf("%s: %v", itemErrCode(derr), derr), "")
		return nil
	}
}

// credentialFor 按 resourceRef.channel 分支解析凭证（cloud_ak / kubeconfig）。
func (s *changeExecuteService) credentialFor(ctx context.Context, ref domain.ResourceRef) (deployer.Credential, error) {
	if s.creds == nil {
		return deployer.Credential{}, fmt.Errorf("credential source not assembled")
	}
	return resolveCredential(ctx, s.creds, ref)
}

// itemErrCode 部署失败错误码映射（K8S_UNREACHABLE 哨兵 → 码值；其余
// EXEC_FAILED 通用前缀——错误详情随 err 文本保留）。
func itemErrCode(err error) string {
	if errors.Is(err, domain.ErrK8sUnreachable) {
		return domain.CodeK8sUnreachable
	}
	return itemErrExecFailed
}

// finishItem 项级终态落库 + item_result 审计（7.2）+ 订单状态按剩余项重算
// （AC-6）。审计失败不阻塞执行主流程（项级状态优先落地，端口契约同 5.8）。
func (s *changeExecuteService) finishItem(ctx context.Context, orderID, itemID string, status domain.ChangeItemStatus, errMsg, newCloudCertID string) {
	_, _ = s.items.FinishItem(ctx, itemID, status, errMsg, newCloudCertID, s.now())
	s.auditItemResult(ctx, orderID, itemID, status, errMsg, newCloudCertID)
	// 重算失败不回滚项级终态：订单侧由 executing-timeout 扫描/重投递幂等重算兜底
	_ = s.recomputeOrderStatus(ctx, orderID)
}

// auditItemResult 追加 item_result 审计事件（actor：ctx 操作者优先，系统
// 回退 executor 标识）。detail 为安全文案：状态/错误码/新云证书 ID，
// 不含凭证与私钥片段。
func (s *changeExecuteService) auditItemResult(ctx context.Context, orderID, itemID string, status domain.ChangeItemStatus, errMsg, newCloudCertID string) {
	if s.audit == nil {
		return
	}
	actor := OperatorFromContext(ctx)
	if actor == "" {
		actor = ActorExecutor
	}
	detail := fmt.Sprintf("item finished status=%s", status)
	if errMsg != "" {
		detail += " error=" + errMsg
	}
	if newCloudCertID != "" {
		detail += " cloudCertId=" + newCloudCertID
	}
	_ = s.audit.WriteChangeAudit(ctx, ChangeAuditEvent{
		OrderID: orderID,
		ItemID:  itemID,
		Actor:   actor,
		Action:  AuditActionItemResult,
		Detail:  detail,
		At:      s.now(),
	})
}

// startHeartbeat 执行期心跳（AC-3）：固定间隔刷新 heartbeatAt（默认 30s）；
// 返回停止函数（项终态后停跳）。刷新失败静默（executing-timeout 兜底）。
func (s *changeExecuteService) startHeartbeat(ctx context.Context, itemID string) (stop func()) {
	if s.heartbeatInterval <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(s.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = s.items.UpdateHeartbeat(ctx, itemID, s.now())
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

// ---------------------------------------------------------------------
// 订单状态重算（AC-6：executing↔verifying 循环、终态收敛）
// ---------------------------------------------------------------------

// recomputeOrderStatus 每项终态变更后按剩余项重算订单状态（仅 executing 态；
// verifying 终局判定归 5.10 window-expiry / 7.1）：
//   - 在途（running/rate_limited）或当前批仍有待派发 pending → 保持 executing；
//   - 当前批全部终态 + 后续批仍有 pending → 进入该批验证窗口（verifyWindowUntil
//     刷新）+ 批间暂停标记（batchInfo.paused=true/pausedAt=now）——批级验证
//     达标后由 ConfirmBatch 人工续批放行（activeMutex 全程持有）；
//   - 全部项终态：executing 中止路径（Cancel Abort：pending 已标 skipped，
//     running 完成后收敛）或后续批被整单跳过 → cancelled（token 同原子清除）；
//     终批正常完成 → 最终验证窗口（终局判定归 5.10/7.1）。
//
// 中止收敛判据：cancel 路径 MarkPendingSkipped 仅写 status 不写 error，而生成期
// 不可执行项 skipped 携带 Error=Reason（5.2 buildChangeItems）——status=skipped
// 且 Error 为空即人工取消痕迹；后续批 pending 全部消失（被 skipped）同为中止信号
// （正常流转下未到期批次保持 pending）。
func (s *changeExecuteService) recomputeOrderStatus(ctx context.Context, orderID string) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("change: get order for recompute: %w", err)
	}
	if order.Status != domain.ChangeStatusExecuting {
		return nil
	}
	items, err := s.items.ListByOrder(ctx, orderID)
	if err != nil {
		return fmt.Errorf("change: list items for recompute: %w", err)
	}
	currentBatch := 1
	if order.BatchInfo != nil {
		currentBatch = order.BatchInfo.CurrentBatch
	}

	var futurePending, cancelSkipped bool
	for _, it := range items {
		switch {
		case it.Status == domain.ItemStatusRunning || it.Status == domain.ItemStatusRateLimited:
			return nil // 在途项未收敛，保持 executing
		case it.Status == domain.ItemStatusPending && it.BatchNo <= currentBatch:
			return nil // 当前批待派发/待补派（Execute 幂等重入）
		case it.Status == domain.ItemStatusPending:
			futurePending = true // 后续批未到期（正常批间流转）
		case it.Status == domain.ItemStatusSkipped && it.Error == "":
			cancelSkipped = true // 人工取消痕迹（见函数注释）
		}
	}

	if futurePending {
		return s.enterVerifyWindow(ctx, order)
	}
	lastBatch := order.BatchInfo == nil || order.BatchInfo.CurrentBatch >= order.BatchInfo.TotalBatches
	if cancelSkipped || !lastBatch {
		// executing 中止收敛（Cancel Abort 语义）或后续批整体跳过 → cancelled
		if err := s.orders.TransitionTerminal(ctx, orderID, domain.ChangeStatusCancelled); err != nil {
			return fmt.Errorf("change: converge order to cancelled: %w", err)
		}
		return nil
	}
	// 终批正常完成 → 最终验证窗口
	return s.enterVerifyWindow(ctx, order)
}

// enterVerifyWindow 进入验证窗口：verifyWindowUntil = now + verifyWindowHours
// （分批单每批刷新——该批验证截止，5.10 窗口判定依据）；分批单同原子固化批间
// 暂停标记。CAS 未命中（状态被 Cancel 等并发迁移）幂等忽略。进入 verifying 后
// 经 VerifyWindowSealer 固化 verifyExpected（5.10 缝：该批窗口判定依据）。
func (s *changeExecuteService) enterVerifyWindow(ctx context.Context, order domain.ChangeOrder) error {
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return fmt.Errorf("change: get alert config for verify window: %w", err)
	}
	now := s.now()
	ok, err := s.orders.EnterVerify(ctx, order.ID.Hex(),
		now.Add(time.Duration(cfg.Thresholds.VerifyWindowHours)*time.Hour), now)
	if err != nil {
		return fmt.Errorf("change: enter verify window: %w", err)
	}
	if !ok {
		return nil // 状态已被并发迁移（Cancel/超时取消），幂等
	}
	if s.sealer != nil {
		if _, serr := s.sealer.SealVerifyExpected(ctx, order.ID.Hex()); serr != nil {
			return fmt.Errorf("change: seal verify expected for order %s: %w", order.ID.Hex(), serr)
		}
		// CAS 未命中（续批并发推进）：固化交由 5.10 调度入口惰性补固化，幂等
	}
	return nil
}

// ---------------------------------------------------------------------
// ConfirmBatch：人工续批门控
// ---------------------------------------------------------------------

// ConfirmBatch 人工续批（AC-5）：双门控（上一批全部 success + 批级验证达标）
// 通过 → currentBatch+1 放行（CAS 原子推进，并发双确认仅一个生效）；
// 任一不满足 → 409 BATCH_NOT_CONFIRMABLE（domain.ErrBatchNotConfirmable）。
func (s *changeExecuteService) ConfirmBatch(ctx context.Context, orderID string) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("change: get order: %w", err)
	}
	if order.BatchInfo == nil {
		return fmt.Errorf("%w: order %s is not batched", domain.ErrBatchNotConfirmable, orderID)
	}
	paused := order.Status == domain.ChangeStatusExecuting && order.BatchInfo.Paused
	inVerify := order.Status == domain.ChangeStatusVerifying
	if !paused && !inVerify {
		return fmt.Errorf("%w: order status %s is not awaiting batch continuation", domain.ErrBatchNotConfirmable, order.Status)
	}
	if order.BatchInfo.CurrentBatch >= order.BatchInfo.TotalBatches {
		return fmt.Errorf("%w: order %s already at last batch %d/%d",
			domain.ErrBatchNotConfirmable, orderID, order.BatchInfo.CurrentBatch, order.BatchInfo.TotalBatches)
	}

	// 门控 1：上一批全部 success（skipped=生成期不可执行项，不计执行成功率
	// 分母——PRD 口径，不阻塞续批；failed/在途/待派发均拒绝）。
	batchItems, err := s.items.ListByOrderAndBatch(ctx, orderID, order.BatchInfo.CurrentBatch)
	if err != nil {
		return fmt.Errorf("change: list previous batch items: %w", err)
	}
	for _, it := range batchItems {
		if it.Status == domain.ItemStatusSuccess || it.Status == domain.ItemStatusSkipped {
			continue
		}
		return fmt.Errorf("%w: previous batch has item %s in status %s",
			domain.ErrBatchNotConfirmable, it.ID.Hex(), it.Status)
	}

	// 门控 2：批级验证达标（提频探测连续 verifyConfirmProbes 一致；5.10 提供）
	if s.verify == nil {
		return fmt.Errorf("%w: batch verify checker not wired (5.10)", domain.ErrBatchNotConfirmable)
	}
	verified, reason, verr := s.verify.BatchVerified(ctx, order)
	if verr != nil {
		return fmt.Errorf("%w: batch verify check failed: %v", domain.ErrBatchNotConfirmable, verr)
	}
	if !verified {
		return fmt.Errorf("%w: %s", domain.ErrBatchNotConfirmable, reason)
	}

	advanced, err := s.orders.AdvanceBatch(ctx, orderID, order.Status, order.BatchInfo.CurrentBatch+1)
	if err != nil {
		return fmt.Errorf("change: advance batch: %w", err)
	}
	if !advanced {
		return fmt.Errorf("%w: order state changed concurrently", domain.ErrBatchNotConfirmable)
	}
	return nil
}

// ---------------------------------------------------------------------
// RecoverTimedOutItems：executing-timeout 恢复（AC-3）
// ---------------------------------------------------------------------

// RecoverTimedOutItems 心跳超时恢复：running 且 heartbeatAt +
// itemHeartbeatTimeoutMinutes < now → failed(EXEC_TIMEOUT) + 告警 + 单据状态
// 按剩余项重算（worker 崩溃/云 API 挂起不会使订单永久停留 executing——
// tech-design executing 态活性保障）。
func (s *changeExecuteService) RecoverTimedOutItems(ctx context.Context) (int, error) {
	timeoutMinutes := domain.DefaultThresholds().ItemHeartbeatTimeoutMinutes
	if s.alertCfg != nil {
		if cfg, err := s.alertCfg.Get(ctx); err == nil && cfg.Thresholds.ItemHeartbeatTimeoutMinutes > 0 {
			timeoutMinutes = cfg.Thresholds.ItemHeartbeatTimeoutMinutes
		} // 阈值读取失败回退默认（恢复流程不中断）
	}
	cutoff := s.now().Add(-time.Duration(timeoutMinutes) * time.Minute)
	timedOut, err := s.items.ListRunningBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("change: list timed-out items: %w", err)
	}
	recovered := 0
	var firstErr error
	ordersSeen := make(map[string]struct{})
	for _, it := range timedOut {
		var heartbeat time.Time
		if it.HeartbeatAt != nil {
			heartbeat = *it.HeartbeatAt
		}
		msg := fmt.Sprintf("%s: heartbeat %s older than %d minutes (item recovered by executing-timeout)",
			domain.CodeExecTimeout, heartbeat.Format(time.RFC3339), timeoutMinutes)
		ok, ferr := s.items.FinishItem(ctx, it.ID.Hex(), domain.ItemStatusFailed, msg, "", s.now())
		if ferr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("change: fail timed-out item %s: %w", it.ID.Hex(), ferr)
			}
			continue // 单笔失败不中断扫描
		}
		if !ok {
			continue // 竞态：子任务恰在扫描间隙完成，幂等跳过
		}
		recovered++
		if s.notifier != nil {
			// 告警失败不阻塞恢复（项已转 failed，下轮不再命中）
			_ = s.notifier.NotifyItemTimedOut(ctx, it.OrderID, it.ID.Hex(), heartbeat, s.now())
		}
		ordersSeen[it.OrderID] = struct{}{}
	}
	// 受影响订单逐单重算（确定性次序）；重算失败保留首批错误
	for _, oid := range sortedKeys(ordersSeen) {
		if err := s.recomputeOrderStatus(ctx, oid); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return recovered, firstErr
}

// sortedKeys map 键排序（确定性遍历）。
func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------
// internal/task 框架挂载（pkg/taskx：执行器 + 派发器）
// ---------------------------------------------------------------------

// TaskTypeExecuteChangeItem 子任务类型（taskx.Queue.RegisterExecutor 注册键）。
const TaskTypeExecuteChangeItem taskx.TaskType = "cert:execute_change_item"

// ChangeItemExecutor 变更项子任务执行器（internal/task 框架既有用法对齐：
// 实现 taskx.TaskExecutor，7.x 模块装配时注册至 cert 任务队列）。
// Execute 返回 error 仅表示基础设施故障（框架按默认重试策略重投递——
// 项级领取 CAS 保证重投递幂等）；项级业务成败已落 ChangeItem.status，
// 不以框架错误呈现（tech-design 同步错误 vs 异步子任务状态）。
type ChangeItemExecutor struct {
	runner ItemRunner
}

// NewChangeItemExecutor 创建子任务执行器（runner 通常为 ChangeExecuteService）。
func NewChangeItemExecutor(runner ItemRunner) *ChangeItemExecutor {
	return &ChangeItemExecutor{runner: runner}
}

// GetType 任务类型标识。
func (e *ChangeItemExecutor) GetType() taskx.TaskType { return TaskTypeExecuteChangeItem }

// Execute 按 Params{orderId, itemId} 执行单项。
func (e *ChangeItemExecutor) Execute(ctx context.Context, task *taskx.Task) error {
	orderID, _ := task.Params["orderId"].(string)
	itemID, _ := task.Params["itemId"].(string)
	if orderID == "" || itemID == "" {
		return fmt.Errorf("cert executor: task %s params missing orderId/itemId", task.ID)
	}
	return e.runner.ExecuteItem(ctx, orderID, itemID)
}

// 编译期断言：满足 taskx 执行器契约。
var _ taskx.TaskExecutor = (*ChangeItemExecutor)(nil)

// TaskxItemDispatcher 生产派发器：Execute 编排 → taskx.Queue.Submit
// （执行器经 RegisterExecutor 注册于同一队列）。
type TaskxItemDispatcher struct {
	Queue *taskx.Queue
}

// DispatchItem 提交单项子任务（Params 携带 orderId/itemId；队列满/关闭时
// 返回错误，Execute 侧逐项隔离不阻塞其他项）。
func (d TaskxItemDispatcher) DispatchItem(_ context.Context, orderID, itemID string) error {
	return d.Queue.Submit(&taskx.Task{
		ID:     primitive.NewObjectID().Hex(),
		Type:   TaskTypeExecuteChangeItem,
		Status: taskx.TaskStatusPending,
		Params: map[string]interface{}{"orderId": orderID, "itemId": itemID},
	})
}

// 编译期断言：满足派发端口。
var _ SubtaskDispatcher = TaskxItemDispatcher{}

// ---------------------------------------------------------------------
// 凭证来源生产实现
// ---------------------------------------------------------------------

// AccountCredentialSource 生产凭证来源：云 AK（account 仓储读取路径解密，
// 参考 3.5 accountScanSource 口径）+ K8s kubeconfig（信封解密，同 1.1 体系）。
// Secret 明文仅内存传递（deployer.Credential 契约：用后 Zeroize、禁入
// 日志/响应/审计），错误文案仅含账号名/集群名等安全参数。
type AccountCredentialSource struct {
	accounts accountrepo.CloudAccountRepository
	k8sCreds domain.K8sCredentialRepository
	crypto   *domain.EnvelopeCrypto
}

// NewAccountCredentialSource 创建生产凭证来源。
func NewAccountCredentialSource(
	accounts accountrepo.CloudAccountRepository,
	k8sCreds domain.K8sCredentialRepository,
	crypto *domain.EnvelopeCrypto,
) *AccountCredentialSource {
	return &AccountCredentialSource{accounts: accounts, k8sCreds: k8sCreds, crypto: crypto}
}

// CloudCredential 解析云账号 AK/SK 凭证（active 账号；AccountKey=账号名，
// 3.5 扫描与 5.2 清单同口径）。
func (s *AccountCredentialSource) CloudCredential(ctx context.Context, cloud, accountKey string) (deployer.Credential, error) {
	accounts, _, err := s.accounts.List(ctx, sharedomain.CloudAccountFilter{
		Provider: sharedomain.CloudProvider(cloud),
		Status:   sharedomain.CloudAccountStatusActive,
	})
	if err != nil {
		return deployer.Credential{}, fmt.Errorf("cert: list active accounts for %s: %w", cloud, err)
	}
	for i := range accounts {
		if accounts[i].Name != accountKey {
			continue
		}
		a := accounts[i]
		if a.AccessKeyID == "" || a.AccessKeySecret == "" {
			break
		}
		return deployer.Credential{
			Kind:       deployer.CredentialKindCloudAK,
			Cloud:      cloud,
			AccountKey: accountKey,
			AccessKey:  a.AccessKeyID,
			Secret:     []byte(a.AccessKeySecret),
			KeyVersion: 1, // 账号密钥体系无版本概念（区别于信封加密），审计下限
		}, nil
	}
	return deployer.Credential{}, fmt.Errorf("cert: no usable active account %q for cloud %s", accountKey, cloud)
}

// K8sCredential 解析集群 kubeconfig 凭证（形态校验与通道契约对齐；连接材料
// 由 K8sAPIChannel 的 CRDClientProvider 按集群名另行解析）。
func (s *AccountCredentialSource) K8sCredential(ctx context.Context, clusterID string) (deployer.Credential, error) {
	cred, err := s.k8sCreds.GetByClusterName(ctx, clusterID)
	if err != nil {
		return deployer.Credential{}, fmt.Errorf("cert: load k8s credential for cluster %q: %w", clusterID, err)
	}
	if cred.Kubeconfig == nil {
		return deployer.Credential{}, fmt.Errorf("cert: cluster %q has no encrypted kubeconfig stored", clusterID)
	}
	plain, err := s.crypto.Decrypt(cred.Kubeconfig.Ciphertext, cred.Kubeconfig.KeyVersion)
	if err != nil {
		return deployer.Credential{}, fmt.Errorf("cert: decrypt kubeconfig for cluster %q: %w", clusterID, err)
	}
	return deployer.Credential{
		Kind:       deployer.CredentialKindKubeconfig,
		Secret:     plain,
		KeyVersion: cred.Kubeconfig.KeyVersion,
	}, nil
}

// 编译期断言：满足凭证来源端口。
var _ ChannelCredentialSource = (*AccountCredentialSource)(nil)
