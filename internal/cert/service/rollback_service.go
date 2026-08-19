package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 回滚服务（任务 5.8，tech-design Interface 3 Rollback）：
//
//   - 回滚对象仅限本次变更执行成功项（status=success）——失败/skipped 项引用
//     从未被改动，不参与回滚，仅在报告标记（Hard Rule：失败项绝不自动回滚）；
//   - 逐项前置 GetCert(oldCloudCertId) 三判定（云侧已删除/已过期/指纹被替换）
//     → 任一无效即整体阻断：不自动回滚、转人工决策、记录审计（Hard Rule：
//     无效目标绝不自动回滚），错误码 ROLLBACK_TARGET_INVALID（409）；
//   - 回滚执行经通道 Rollback 恢复引用为旧证书 ID（K8s 项按 patch 恢复旧值）；
//     失败项标 rollback_failed + 立即告警（四类之"回滚失败"）转人工
//     （Hard Rule：不得静默）；
//   - 订单收敛：全部成功项回滚成功 → rolled_back；任一 rollback_failed →
//     rollback_failed 终态（转人工）；
//   - 回滚产生的孤儿云证书（被替换下的新云证书）映射标 orphan 入 5.9 清理
//     队列，RollbackResult.OrphanCleaned 记录入审计（报告呈现归 5.10/5.11）。
//
// 入口状态：执行中（出现失败项后）/验证中/部分完成（PRD 回滚语义）；状态机
// 白名单规定 rolled_back/rollback_failed 终态迁移仅自 completed/partial_completed
// 发生（domain changeTransitions），executing/verifying 入口先沿白名单路径
//（executing→verifying→partial_completed）收敛后再进入回滚终链。
// ---------------------------------------------------------------------

// ChangeRollbackService 变更回滚服务（Interface 3 Rollback 落地，与 5.7
// ChangeExecuteService 同为 Interface 3 的分任务实现）。
type ChangeRollbackService interface {
	// Rollback 对订单内 itemIds 指定的成功项执行回滚：
	//   - itemIds 中的非 success 项不参与回滚（引用未被改动，报告按其既有
	//     状态标记），全部非 success 或清单为空返回错误；
	//   - 入口状态校验（executing 须已出现失败项且无在途项/verifying/
	//     partial_completed），其余状态返回 *domain.InvalidTransitionError；
	//   - 任一回滚目标无效 → 整体阻断返回 ErrRollbackTargetInvalid（409），
	//     无任何状态变更，转人工决策并记录审计；
	//   - 回滚失败为项级状态（rollback_failed + 立即告警），不以同步错误
	//     返回（异步子任务状态口径，tech-design 同步错误 vs 异步子任务状态）；
	//     仅基础设施故障（仓储/告警发布）以聚合 error 上抛。
	Rollback(ctx context.Context, orderID string, itemIDs []string) error
}

// ---------------------------------------------------------------------
// 注入端口
// ---------------------------------------------------------------------

// RollbackTargetSource 回滚目标有效性校验端口（GetCert 只读三判定数据源）：
// 生产实现 *deployer.CloudAPIChannel（InspectCloudCert 经 5.4/5.5 注册的
// per 云部署器路由，GetCert 走 3.1/3.2 适配）。discovery-only 三云无成功项
// 场景天然不触达（不可执行项不会 success）。
type RollbackTargetSource interface {
	// InspectCloudCert 查询 cloudCertID 在 cloud 证书库的在库状态。
	InspectCloudCert(ctx context.Context, creds deployer.Credential, cloud, cloudCertID string) (deployer.CloudCertInfo, error)
}

// 编译期断言：云 API 通道满足回滚目标校验端口。
var _ RollbackTargetSource = (*deployer.CloudAPIChannel)(nil)

// RollbackAuditEvent 回滚审计事件（action=rollback，7.2 统一接线 internal/audit；
// 5.11 GET /changes/:id/audit 按 OrderID 过滤呈现）。Detail 为静态文案+安全
// 参数（资源定位/云证书 ID/错误码），不含私钥/凭证片段。
type RollbackAuditEvent struct {
	OrderID string    // 关联变更单
	ItemID  string    // 项级事件必填；订单级事件为空
	Outcome string    // RollbackAudit* 结果常量
	Detail  string    // 机器可读详情
	At      time.Time // 事件时间
}

// RollbackAuditOutcome 审计结果常量。
const (
	// RollbackAuditTargetInvalid 前置三判定命中：目标无效，转人工决策。
	RollbackAuditTargetInvalid = "rollback_target_invalid"
	// RollbackAuditItemRolledBack 单项回滚成功（Detail 附 OrphanCleaned 载荷）。
	RollbackAuditItemRolledBack = "rollback_item_rolled_back"
	// RollbackAuditItemFailed 单项回滚失败（已立即告警转人工）。
	RollbackAuditItemFailed = "rollback_item_failed"
	// RollbackAuditOrderRolledBack 订单收敛 rolled_back 终态。
	RollbackAuditOrderRolledBack = "rollback_order_rolled_back"
	// RollbackAuditOrderFailed 订单收敛 rollback_failed 终态（转人工）。
	RollbackAuditOrderFailed = "rollback_order_rollback_failed"
	// RollbackAuditOrderHeld 部分成功项已回滚、清单仍有 success 项：订单保持
	// 完成类终态，可再次 Rollback 处理剩余项（人工决策续作）。
	RollbackAuditOrderHeld = "rollback_order_held"
)

// RollbackAuditRecorder 回滚审计端口（7.2 统一接线 internal/audit；nil=no-op，
// 同 5.7 ExecuteAlertNotifier 口径）。审计失败不阻塞回滚主流程（错误经调用方
// 聚合上抛），项级状态与告警优先落地。
type RollbackAuditRecorder interface {
	// RecordRollback 记录单条回滚审计事件。
	RecordRollback(ctx context.Context, event RollbackAuditEvent) error
}

// rollbackErrGeneric 通道未映射的回滚失败通用错误码前缀
// （ChangeItem.error = "码: 详情"；CLOUD_API_RATELIMITED/K8S_UNREACHABLE/
// ROLLBACK_TARGET_INVALID 由通道/前置校验产生）。
const rollbackErrGeneric = "ROLLBACK_FAILED"

// ErrRollbackScopeInvalid 回滚范围解析失败（请求侧同步错误：itemIDs 为空 /
// 含不属本单的项 / 全部非成功项——5.11 端点映射 400，区别于项级回滚失败的
// 异步子任务状态口径 rollback_failed）。
var ErrRollbackScopeInvalid = errors.New("rollback: request scope invalid (empty, foreign item or no rollbackable success items)")

// ---------------------------------------------------------------------
// 服务实现
// ---------------------------------------------------------------------

type changeRollbackService struct {
	orders   domain.ChangeOrderRepository
	items    domain.ChangeItemRepository
	certs    domain.CertificateRepository
	alertCfg domain.AlertConfigRepository
	mappings domain.CloudCertMappingRepository // nil=跳过孤儿映射标记（装配弹性，同 5.3 oldRefs）

	channels  map[string]deployer.ExecutionChannel // 按 resourceRef.channel 路由
	targets   RollbackTargetSource                 // GetCert 三判定数据源；nil=fail closed
	creds     ChannelCredentialSource              // 凭证来源（5.7 端口复用）
	publisher CertAlertPublisher                   // 四类告警发布（nil=日志实现）
	auditor   RollbackAuditRecorder                // nil=no-op（7.2 接线）
	now       func() time.Time                     // 测试可注入时间源
}

// NewChangeRollbackService 创建回滚服务。channels 为已装配执行通道（5.3/5.6
// 产物，按 Type() 路由）；targets 通常为 *deployer.CloudAPIChannel；publisher
// nil 回退日志发布（4.3 通道前的默认路径，同 inspection 口径）。
func NewChangeRollbackService(
	orders domain.ChangeOrderRepository,
	items domain.ChangeItemRepository,
	certs domain.CertificateRepository,
	alertCfg domain.AlertConfigRepository,
	mappings domain.CloudCertMappingRepository,
	channels []deployer.ExecutionChannel,
	targets RollbackTargetSource,
	creds ChannelCredentialSource,
	publisher CertAlertPublisher,
	auditor RollbackAuditRecorder,
) ChangeRollbackService {
	chs := make(map[string]deployer.ExecutionChannel, len(channels))
	for _, c := range channels {
		if c != nil {
			chs[string(c.Type())] = c
		}
	}
	if publisher == nil {
		publisher = NewLoggingAlertPublisher()
	}
	return &changeRollbackService{
		orders:    orders,
		items:     items,
		certs:     certs,
		alertCfg:  alertCfg,
		mappings:  mappings,
		channels:  chs,
		targets:   targets,
		creds:     creds,
		publisher: publisher,
		auditor:   auditor,
		now:       time.Now,
	}
}

// invalidRollbackTarget 前置三判定命中的无效目标（AC-2）。
type invalidRollbackTarget struct {
	itemID string
	reason string // 静态文案+安全参数（云证书 ID/RFC3339 时间）
}

// Rollback 回滚编排（校验次序：入口状态 → 范围解析 → 前置三判定 →
// 入口收敛 → 逐项回滚 → 订单收敛）。
func (s *changeRollbackService) Rollback(ctx context.Context, orderID string, itemIDs []string) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("rollback: get order: %w", err)
	}
	all, err := s.items.ListByOrder(ctx, orderID)
	if err != nil {
		return fmt.Errorf("rollback: list items: %w", err)
	}
	if err := s.validateEntry(order, all); err != nil {
		return err
	}
	participating, err := resolveRollbackScope(all, itemIDs)
	if err != nil {
		return err
	}

	// ---- 前置三判定（无状态变更；任一无效 → 整体阻断转人工） ----
	invalid, err := s.precheckTargets(ctx, order, participating)
	if err != nil {
		return err // GetCert/凭证等基础设施故障：fail closed，可重试
	}
	if len(invalid) > 0 {
		var errs []error
		ids := make([]string, 0, len(invalid))
		for _, v := range invalid {
			ids = append(ids, v.itemID)
			if aerr := s.recordAudit(ctx, RollbackAuditEvent{
				OrderID: orderID, ItemID: v.itemID,
				Outcome: RollbackAuditTargetInvalid, Detail: v.reason, At: s.now(),
			}); aerr != nil {
				errs = append(errs, aerr)
			}
		}
		// 409 ROLLBACK_TARGET_INVALID（附转人工提示语义，5.11 端点映射）：
		// 项保持 success、引用未被改动、订单保持入口状态——人工决策后可
		// 剔除无效项重发或人工处置。
		errs = append(errs, fmt.Errorf("%w: %d item(s) invalid [%s]: manual decision required",
			domain.ErrRollbackTargetInvalid, len(invalid), strings.Join(ids, ",")))
		return errors.Join(errs...)
	}

	// ---- 入口收敛：executing/verifying → partial_completed（白名单前置终态） ----
	if err := s.convergeForRollback(ctx, order, all); err != nil {
		return err
	}

	// ---- 逐项回滚（项级隔离：单项失败/告警失败不阻塞其他项） ----
	var soft []error
	for _, it := range participating {
		if err := s.rollbackItem(ctx, order, it); err != nil {
			soft = append(soft, err)
		}
	}
	// ---- 订单收敛（AC-4） ----
	if err := s.convergeAfterRollback(ctx, order); err != nil {
		soft = append(soft, err)
	}
	return errors.Join(soft...)
}

// validateEntry 入口状态校验（AC-1）：executing（出现失败项后，且无在途项——
// running/rate_limited 项未收敛前回滚会与部署竞态）/ verifying /
// partial_completed；其余状态拒绝。
func (s *changeRollbackService) validateEntry(order domain.ChangeOrder, items []domain.ChangeItem) error {
	switch order.Status {
	case domain.ChangeStatusPartialCompleted, domain.ChangeStatusVerifying:
		return nil
	case domain.ChangeStatusExecuting:
		failed, inFlight := 0, 0
		for _, it := range items {
			switch it.Status {
			case domain.ItemStatusFailed:
				failed++
			case domain.ItemStatusRunning, domain.ItemStatusRateLimited:
				inFlight++
			}
		}
		if failed == 0 {
			return fmt.Errorf("rollback: order %s is executing without failed items (entry requires failed items present)", order.ID.Hex())
		}
		if inFlight > 0 {
			return fmt.Errorf("rollback: order %s has %d in-flight item(s); wait for settlement or abort first", order.ID.Hex(), inFlight)
		}
		return nil
	default:
		return &domain.InvalidTransitionError{From: order.Status, To: domain.ChangeStatusRolledBack}
	}
}

// resolveRollbackScope 范围解析（AC-1，纯函数）：itemIDs 须非空且全部归属本单；
// 仅 status=success 的项参与回滚（失败/skipped 项引用未被改动，不参与回滚，
// 报告按其既有状态标记）；无任何成功项返回错误。返回按项 ID 稳定排序
// （确定性回滚次序，审计/告警可复现）。
func resolveRollbackScope(items []domain.ChangeItem, itemIDs []string) ([]domain.ChangeItem, error) {
	if len(itemIDs) == 0 {
		return nil, fmt.Errorf("%w: itemIDs is empty", ErrRollbackScopeInvalid)
	}
	byID := make(map[string]domain.ChangeItem, len(items))
	for _, it := range items {
		byID[it.ID.Hex()] = it
	}
	participating := make([]domain.ChangeItem, 0, len(itemIDs))
	for _, id := range itemIDs {
		it, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: item %s not found in order", ErrRollbackScopeInvalid, id)
		}
		if it.Status == domain.ItemStatusSuccess {
			participating = append(participating, it)
		}
	}
	if len(participating) == 0 {
		return nil, fmt.Errorf("%w: no rollbackable success items in request (failed/skipped items keep untouched references and never roll back)", ErrRollbackScopeInvalid)
	}
	sort.Slice(participating, func(i, j int) bool {
		return participating[i].ID.Hex() < participating[j].ID.Hex()
	})
	return participating, nil
}

// precheckTargets 前置三判定（AC-2）：对每个参与项校验回滚目标有效性——
//   - oldCloudCertId 缺失（无可恢复目标）→ 无效；
//   - cloud_api 项：GetCert(oldCloudCertId) 三判定——Exists=false（云侧已删除）、
//     NotAfter < now（已过期）、Fingerprint ≠ 订单 oldCertFingerprint（被替换）；
//   - k8s_api 项：无云证书库语境（引用经 patch 恢复旧值，AC-3），目标存在即有效。
//
// GetCert/凭证解析等基础设施错误整体返回（fail closed：无法判定有效性时
// 不自动回滚，可重试），不混入"无效目标"语义。
func (s *changeRollbackService) precheckTargets(ctx context.Context, order domain.ChangeOrder, items []domain.ChangeItem) ([]invalidRollbackTarget, error) {
	var invalid []invalidRollbackTarget
	for _, it := range items {
		if it.OldCloudCertID == "" {
			invalid = append(invalid, invalidRollbackTarget{
				itemID: it.ID.Hex(),
				reason: "oldCloudCertId missing: no restorable target",
			})
			continue
		}
		if it.ResourceRef.Channel != domain.ChannelCloudAPI {
			continue
		}
		if s.targets == nil {
			return nil, fmt.Errorf("rollback: target inspector not assembled (fail closed)")
		}
		if s.creds == nil {
			return nil, fmt.Errorf("rollback: credential source not assembled")
		}
		creds, err := s.creds.CloudCredential(ctx, it.ResourceRef.Cloud, it.ResourceRef.AccountKey)
		if err != nil {
			return nil, fmt.Errorf("rollback: resolve credential for item %s: %w", it.ID.Hex(), err)
		}
		info, err := s.targets.InspectCloudCert(ctx, creds, it.ResourceRef.Cloud, it.OldCloudCertID)
		creds.Zeroize()
		if err != nil {
			return nil, fmt.Errorf("rollback: inspect cloud cert %s: %w", it.OldCloudCertID, err)
		}
		switch {
		case !info.Exists:
			invalid = append(invalid, invalidRollbackTarget{
				itemID: it.ID.Hex(),
				reason: fmt.Sprintf("cloud cert %s deleted on cloud side (Exists=false)", it.OldCloudCertID),
			})
		case info.NotAfter.Before(s.now()):
			invalid = append(invalid, invalidRollbackTarget{
				itemID: it.ID.Hex(),
				reason: fmt.Sprintf("cloud cert %s expired at %s", it.OldCloudCertID, info.NotAfter.Format(time.RFC3339)),
			})
		case info.Fingerprint != order.OldCertFingerprint:
			invalid = append(invalid, invalidRollbackTarget{
				itemID: it.ID.Hex(),
				reason: fmt.Sprintf("cloud cert %s fingerprint mismatch: target replaced", it.OldCloudCertID),
			})
		}
	}
	return invalid, nil
}

// convergeForRollback 入口收敛：rolled_back/rollback_failed 终态迁移仅自
// completed/partial_completed 发生（domain 白名单），executing/verifying 入口
// 先沿白名单路径收敛到 partial_completed——回滚发起即表示变更未被接受为
// 完整完成（存在失败项或验证期发现异常），completed 会误表述实际状态；
// partial_completed 同时保持回滚重入口（剩余成功项可续作）。
//   - executing：未执行项（引用未被改动）标 skipped，经 verifying 桥接
//     （executing→verifying→partial_completed 均为白名单边）；
//   - 同原子固化 protectUntil（旧证书回滚后重新在用，保护期随终态生效）
//     并清除 activeMutex；旧证书台账保护期随后固化（幂等、只延长不缩短）。
func (s *changeRollbackService) convergeForRollback(ctx context.Context, order domain.ChangeOrder, items []domain.ChangeItem) error {
	if order.Status == domain.ChangeStatusPartialCompleted {
		return nil // 已在合法前置终态（重入/续作）
	}
	if order.Status == domain.ChangeStatusExecuting {
		if _, err := s.items.MarkPendingSkipped(ctx, order.ID.Hex()); err != nil {
			return fmt.Errorf("rollback: mark pending items skipped: %w", err)
		}
		if err := s.orders.TransitionActive(ctx, order.ID.Hex(), domain.ChangeStatusVerifying, order.OldCertFingerprint); err != nil {
			return fmt.Errorf("rollback: bridge order to verifying: %w", err)
		}
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return fmt.Errorf("rollback: get alert config: %w", err)
	}
	protectUntil := s.now().AddDate(0, 0, cfg.Thresholds.RollbackProtectDays)
	if err := s.orders.TransitionTerminalWithProtect(ctx, order.ID.Hex(), domain.ChangeStatusPartialCompleted, protectUntil); err != nil {
		return fmt.Errorf("rollback: converge order to partial_completed: %w", err)
	}
	if err := s.certs.SetProtectUntil(ctx, order.OldCertFingerprint, protectUntil); err != nil {
		return fmt.Errorf("rollback: set old cert protect until: %w", err)
	}
	return nil
}

// rollbackItem 单项回滚（AC-3，项级隔离）：通道 Rollback 恢复旧引用 →
// 新云证书映射标 orphan（5.9 队列 + OrphanCleaned 记录，AC-5）→ 项标
// rolled_back；通道失败 → rollback_failed + 立即告警（四类之"回滚失败"，
// Hard Rule：不得静默）+ 审计。返回 error 仅为基础设施故障（项级失败已落
// 状态与告警，不以同步错误中断其他项）。
func (s *changeRollbackService) rollbackItem(ctx context.Context, order domain.ChangeOrder, it domain.ChangeItem) error {
	itemID := it.ID.Hex()
	channel, ok := s.channels[string(it.ResourceRef.Channel)]
	if !ok {
		return s.failRollbackItem(ctx, order, it, rollbackErrGeneric,
			fmt.Sprintf("no execution channel assembled for %q", it.ResourceRef.Channel))
	}
	if s.creds == nil {
		return s.failRollbackItem(ctx, order, it, rollbackErrGeneric, "credential source not assembled")
	}
	creds, err := resolveCredential(ctx, s.creds, it.ResourceRef)
	if err != nil {
		return s.failRollbackItem(ctx, order, it, rollbackErrGeneric, fmt.Sprintf("resolve credential: %v", err))
	}
	result, rerr := channel.Rollback(ctx, creds, deployer.DeployTargetFromResourceRef(it.ResourceRef), rollbackOldRef(it))
	creds.Zeroize()
	if rerr != nil || !result.Success {
		code, reason := result.ErrCode, result.Reason
		if code == "" {
			code = rollbackErrGeneric
		}
		if reason == "" && rerr != nil {
			reason = rerr.Error()
		}
		return s.failRollbackItem(ctx, order, it, code, reason)
	}

	// 孤儿标记先行（5.9 队列入口）：崩溃于标记后、项终态前时，重试回滚
	// 幂等（重绑旧证书 + 重复标记无副作用），反序则可能丢失孤儿清理载荷。
	orphans, oerr := s.markNewCertOrphan(ctx, it)
	ok2, ferr := s.items.FinishRollback(ctx, itemID, domain.ItemStatusRolledBack, "")
	if ferr != nil {
		return errors.Join(oerr, fmt.Errorf("rollback: mark item %s rolled back: %w", itemID, ferr))
	}
	if !ok2 {
		return oerr // CAS 未命中（并发回滚落败方）：幂等跳过项级动作
	}
	detail := fmt.Sprintf("restored reference to cloud cert %s", it.OldCloudCertID)
	if len(orphans) > 0 {
		detail += fmt.Sprintf("; orphanCleaned=%s", strings.Join(orphans, ","))
	}
	if aerr := s.recordAudit(ctx, RollbackAuditEvent{
		OrderID: order.ID.Hex(), ItemID: itemID,
		Outcome: RollbackAuditItemRolledBack, Detail: detail, At: s.now(),
	}); aerr != nil {
		return errors.Join(oerr, aerr)
	}
	return oerr
}

// failRollbackItem 回滚失败落库 + 立即告警（AC-3/Hard Rule）+ 审计：
// rollback_failed 项级状态优先落地；CAS 未命中（并发回滚已完成）幂等跳过
// 告警与审计。告警/审计失败不吞项级状态，错误聚合上抛（不得静默）。
func (s *changeRollbackService) failRollbackItem(ctx context.Context, order domain.ChangeOrder, it domain.ChangeItem, code, reason string) error {
	msg := fmt.Sprintf("%s: %s", code, reason)
	ok, ferr := s.items.FinishRollback(ctx, it.ID.Hex(), domain.ItemStatusRollbackFailed, msg)
	if ferr != nil {
		return fmt.Errorf("rollback: mark item %s rollback failed: %w", it.ID.Hex(), ferr)
	}
	if !ok {
		return nil
	}
	var errs []error
	if aerr := s.publisher.PublishAlert(ctx, CertAlertEvent{
		Category:    AlertCategoryRollbackFailed,
		Title:       "证书变更回滚失败，转人工处置",
		Fingerprint: order.OldCertFingerprint,
		OrderID:     order.ID.Hex(),
		Detail: fmt.Sprintf("item %s (%s/%s/%s): %s", it.ID.Hex(),
			it.ResourceRef.Cloud, it.ResourceRef.Product, it.ResourceRef.ResourceID, msg),
		At: s.now(),
	}); aerr != nil {
		errs = append(errs, fmt.Errorf("rollback: publish rollback-failed alert for item %s: %w", it.ID.Hex(), aerr))
	}
	if aerr := s.recordAudit(ctx, RollbackAuditEvent{
		OrderID: order.ID.Hex(), ItemID: it.ID.Hex(),
		Outcome: RollbackAuditItemFailed, Detail: msg, At: s.now(),
	}); aerr != nil {
		errs = append(errs, aerr)
	}
	return errors.Join(errs...)
}

// markNewCertOrphan 回滚成功后被替换下的新云证书映射标记 orphan（5.9
// orphan-cleanup 队列入口；清理动作与结果呈现归 5.9，本任务仅入队 +
// OrphanCleaned 审计载荷）。K8s 项/未装配映射仓储/无映射记录时跳过
// （返回空）；仓储故障返回软错误（不改变项级 rolled_back 语义）。
func (s *changeRollbackService) markNewCertOrphan(ctx context.Context, it domain.ChangeItem) ([]string, error) {
	if it.NewCloudCertID == "" || s.mappings == nil {
		return nil, nil
	}
	m, err := s.mappings.FindByCloudCertID(ctx, it.ResourceRef.Cloud, it.ResourceRef.AccountKey, it.NewCloudCertID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("rollback: find mapping for new cloud cert %s: %w", it.NewCloudCertID, err)
	}
	if err := s.mappings.UpdateStatus(ctx, m.ID.Hex(), domain.MappingStatusOrphan); err != nil {
		return nil, fmt.Errorf("rollback: mark new cloud cert %s orphan: %w", it.NewCloudCertID, err)
	}
	return []string{it.NewCloudCertID}, nil
}

// convergeAfterRollback 订单收敛（AC-4，按回滚后全清单实际状态判定）：
//   - 任一 rollback_failed → rollback_failed 终态（转人工）；
//   - 订单内无 success 项残留（全部成功项回滚成功）→ rolled_back 终态；
//   - 部分成功项已回滚、仍有 success 项 → 保持 partial_completed（可再次
//     Rollback 续作剩余项，人工决策驱动）。
func (s *changeRollbackService) convergeAfterRollback(ctx context.Context, order domain.ChangeOrder) error {
	items, err := s.items.ListByOrder(ctx, order.ID.Hex())
	if err != nil {
		return fmt.Errorf("rollback: relist items for convergence: %w", err)
	}
	anyFailed, anySuccess := false, false
	for _, it := range items {
		switch it.Status {
		case domain.ItemStatusRollbackFailed:
			anyFailed = true
		case domain.ItemStatusSuccess:
			anySuccess = true
		}
	}
	var (
		target  domain.ChangeStatus
		outcome string
		detail  string
	)
	switch {
	case anyFailed:
		target, outcome = domain.ChangeStatusRollbackFailed, RollbackAuditOrderFailed
		detail = "rollback failed item(s) present: manual intervention required"
	case !anySuccess:
		target, outcome = domain.ChangeStatusRolledBack, RollbackAuditOrderRolledBack
		detail = "all success items rolled back"
	default:
		return s.recordAudit(ctx, RollbackAuditEvent{
			OrderID: order.ID.Hex(), Outcome: RollbackAuditOrderHeld,
			Detail: "subset rolled back; success items remain, order kept for further rollback",
			At:     s.now(),
		})
	}
	if err := s.orders.TransitionTerminal(ctx, order.ID.Hex(), target); err != nil {
		return fmt.Errorf("rollback: converge order to %s: %w", target, err)
	}
	return s.recordAudit(ctx, RollbackAuditEvent{
		OrderID: order.ID.Hex(), Outcome: outcome, Detail: detail, At: s.now(),
	})
}

// recordAudit 审计记录（nil=no-op，7.2 统一接线 internal/audit）。
func (s *changeRollbackService) recordAudit(ctx context.Context, event RollbackAuditEvent) error {
	if s.auditor == nil {
		return nil
	}
	if err := s.auditor.RecordRollback(ctx, event); err != nil {
		return fmt.Errorf("rollback: record audit (order %s item %s %s): %w",
			event.OrderID, event.ItemID, event.Outcome, err)
	}
	return nil
}

// resolveCredential 按 resourceRef.channel 分支解析凭证（cloud_ak / kubeconfig；
// execute 与 rollback 共用）。
func resolveCredential(ctx context.Context, src ChannelCredentialSource, ref domain.ResourceRef) (deployer.Credential, error) {
	switch ref.Channel {
	case domain.ChannelCloudAPI:
		return src.CloudCredential(ctx, ref.Cloud, ref.AccountKey)
	case domain.ChannelK8sAPI:
		return src.K8sCredential(ctx, ref.ClusterID)
	default:
		return deployer.Credential{}, fmt.Errorf("unknown channel %q", ref.Channel)
	}
}

// rollbackOldRef 由变更项持久化数据重构回滚旧引用（通道 Rollback oldRef
// 入参；ReferencedCloudCertID=oldCloudCertId，K8s 项 product 归一 crd 形态，
// 与 execute 引用一致性同键空间）。
func rollbackOldRef(it domain.ChangeItem) domain.CertReference {
	ref := it.ResourceRef
	r := domain.CertReference{
		Cloud:                 domain.Cloud(ref.Cloud),
		Product:               domain.Product(ref.Product),
		ClusterID:             ref.ClusterID,
		Namespace:             ref.Namespace,
		Kind:                  ref.Kind,
		ResourceID:            ref.ResourceID,
		AccountKey:            ref.AccountKey,
		ReferencedCloudCertID: it.OldCloudCertID,
	}
	if ref.Channel == domain.ChannelK8sAPI {
		r.Product = domain.ProductCRD
	}
	return r
}
