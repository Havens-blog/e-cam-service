package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ---------------------------------------------------------------------
// crd-recheck 消费者（任务 5.9，tech-design Scheduler Tasks crd-recheck 行 +
// "K8s 管理权判定与变更后复检"）：
//
//	patch_crd 项 patch 完成（status=success、executedAt 固化）即视为入队，
//	延迟 thresholds.recheckDelayMinutes（默认 5 分钟）到期后消费：调 5.6
//	K8sAPIChannel.RecheckCRDField 单轮复检 CRD 证书引用字段——
//	  - 仍为新证书 ID → 项保持 success（MarkRechecked 固化 recheckedAt）；
//	  - 被 reconcile 回写为旧值/其他值（或字段缺失/读取失败）→ 项转 failed +
//	    error + 告警（TLS 差异通道语义，附 orderId/itemId）。
//	Hard Rule：复检次数固定 1，失败不自动二次复检（recheckedAt 幂等标记 +
//	MarkRechecked CAS，转人工决策：登记接管/调整 GitOps 同步后再发起）。
//
// 期望终态（CRDRecheckItem.NewCertID）由 K8s 通道同一解析口径重解析
//（ResolveCloudCertID：K8s DeployResult.NewCloudCertID 恒空，5.7 未在项上
// 持久化 patch 写入值）。调度注册在 7.1（周期建议分钟级）。
// ---------------------------------------------------------------------

// crdRecheckErrFailed 复检未通过（回写/字段缺失/期望值不可解析）的项级
// error 码前缀（ChangeItem.error = "码: 详情"；读取失败经 itemErrCode 映射
// K8S_UNREACHABLE 等）。
const crdRecheckErrFailed = "CRD_RECHECK_FAILED"

// CrdRechecker 5.6 K8s 通道单轮复检端口（生产实现 *deployer.K8sAPIChannel）。
type CrdRechecker interface {
	// RecheckCRDField 单轮复检（读取失败返回 err，调用方按 failed+告警承接）。
	RecheckCRDField(ctx context.Context, item deployer.CRDRecheckItem) (deployer.RecheckResult, error)
	// ResolveCloudCertID 新证书指纹 → 唯一 active 云证书 ID（patch 写入值与
	// 复检期望终态同一解析口径）。
	ResolveCloudCertID(ctx context.Context, fingerprint string) (string, error)
}

// 编译期断言：K8s 通道满足复检端口。
var _ CrdRechecker = (*deployer.K8sAPIChannel)(nil)

// CrdRecheckService crd-recheck 消费者（AC-3）。
type CrdRecheckService interface {
	// RunDueRechecks 消费全部到期复检项（success 且 executedAt +
	// recheckDelayMinutes <= now 且 recheckedAt 缺失的 patch_crd 项）：逐项
	// 单轮复检。返回消费条数；单项基础设施故障（仓储/告警失败）不中断其他项
	//（逐项隔离），首批错误随计数返回——复检业务失败（回写）为项级状态，
	// 不以 error 呈现。
	RunDueRechecks(ctx context.Context) (int, error)
}

// ---------------------------------------------------------------------
// 服务实现
// ---------------------------------------------------------------------

type crdRecheckService struct {
	orders   domain.ChangeOrderRepository
	items    domain.ChangeItemRepository
	certs    domain.CertificateRepository
	alertCfg domain.AlertConfigRepository // recheckDelayMinutes 来源

	rechecker CrdRechecker       // 5.6 通道复检；nil=fail closed
	publisher CertAlertPublisher // 回写失败告警（TLS 差异通道语义）
	now       func() time.Time   // 测试可注入时间源
}

// NewCrdRecheckService 创建 crd-recheck 消费者。publisher nil 回退日志发布
// （同 5.8 口径）。
func NewCrdRecheckService(
	orders domain.ChangeOrderRepository,
	items domain.ChangeItemRepository,
	certs domain.CertificateRepository,
	alertCfg domain.AlertConfigRepository,
	rechecker CrdRechecker,
	publisher CertAlertPublisher,
) CrdRecheckService {
	if publisher == nil {
		publisher = NewLoggingAlertPublisher()
	}
	return &crdRecheckService{
		orders:    orders,
		items:     items,
		certs:     certs,
		alertCfg:  alertCfg,
		rechecker: rechecker,
		publisher: publisher,
		now:       time.Now,
	}
}

// RunDueRechecks 到期消费（AC-3）。
func (s *crdRecheckService) RunDueRechecks(ctx context.Context) (int, error) {
	delayMinutes := domain.DefaultThresholds().RecheckDelayMinutes
	if s.alertCfg != nil {
		if cfg, err := s.alertCfg.Get(ctx); err == nil && cfg.Thresholds.RecheckDelayMinutes > 0 {
			delayMinutes = cfg.Thresholds.RecheckDelayMinutes
		} // 配置读取失败回退默认（复检不中断）
	}
	before := s.now().Add(-time.Duration(delayMinutes) * time.Minute)
	due, err := s.items.ListPatchCRDDueRecheck(ctx, before)
	if err != nil {
		return 0, fmt.Errorf("crd recheck: list due items: %w", err)
	}
	consumed := 0
	var firstErr error
	for _, it := range due {
		if ctx.Err() != nil {
			break // 取消中断：未消费项保持 due（扫描幂等重入）
		}
		handled, err := s.recheckItem(ctx, it)
		if handled {
			consumed++ // 复检动作已发生（含告警发布失败的重试后续）
		}
		if err != nil && firstErr == nil {
			firstErr = err // 逐项隔离
		}
	}
	return consumed, firstErr
}

// recheckItem 单项复检（AC-3）：解析期望终态 → RecheckCRDField 单轮复检 →
// 结果回填（MarkRechecked CAS status=success）。回写/读取/期望值解析失败 →
// failed + error + 告警（CAS 命中才告警——并发消费落败方幂等跳过，不重复告警，
// AC-4）。返回 handled=false 的情形：CAS 未命中（项已被并发回滚/复检）。
func (s *crdRecheckService) recheckItem(ctx context.Context, it domain.ChangeItem) (bool, error) {
	if s.rechecker == nil {
		return false, fmt.Errorf("crd recheck: rechecker not assembled (fail closed)")
	}
	order, err := s.orders.GetByID(ctx, it.OrderID)
	if err != nil {
		return false, fmt.Errorf("crd recheck: load order %s: %w", it.OrderID, err)
	}
	newCert, err := s.certs.GetByID(ctx, order.NewCertID)
	if err != nil {
		return false, fmt.Errorf("crd recheck: load new certificate %s: %w", order.NewCertID, err)
	}

	// 期望终态解析（patch 写入值同口径）：失败按复检失败承接（fail closed：
	// 无法验证即转人工，不猜测期望值——同 5.6 ErrCloudCertIDUnresolved 口径）。
	newCertID, resolveErr := s.rechecker.ResolveCloudCertID(ctx, newCert.Fingerprint)
	var (
		res     deployer.RecheckResult
		readErr error
	)
	if resolveErr == nil {
		res, readErr = s.rechecker.RecheckCRDField(ctx, deployer.CRDRecheckItem{
			Ref:       it.ResourceRef,
			OrderID:   order.ID.Hex(),
			ItemID:    it.ID.Hex(),
			NewCertID: newCertID,
			OldCertID: it.OldCloudCertID,
		})
	}
	at := s.now()
	if resolveErr == nil && readErr == nil && res.RecheckPassed {
		// 通过 → 项保持 success（仅固化 recheckedAt；5.11 progress 枚举无复检态）。
		ok, err := s.items.MarkRechecked(ctx, it.ID.Hex(), true, "", at)
		if err != nil {
			return false, fmt.Errorf("crd recheck: mark item %s rechecked: %w", it.ID.Hex(), err)
		}
		return ok, nil
	}

	// 未通过：reconcile 回写（res.Reason）/ 读取失败（K8S_UNREACHABLE 等码值
	// 映射）/ 期望值解析失败。Hard Rule：复检次数固定 1，失败转人工——项转
	// failed + 告警，无二次自动复检。
	var reason, code string
	switch {
	case resolveErr != nil:
		reason, code = resolveErr.Error(), crdRecheckErrFailed
	case readErr != nil:
		reason = readErr.Error()
		code = itemErrCode(readErr) // K8S_UNREACHABLE 哨兵 → 码值；其余通用前缀
	default:
		reason, code = res.Reason, crdRecheckErrFailed
	}
	msg := fmt.Sprintf("%s: %s", code, reason)
	ok, err := s.items.MarkRechecked(ctx, it.ID.Hex(), false, msg, at)
	if err != nil {
		return false, fmt.Errorf("crd recheck: mark item %s recheck failed: %w", it.ID.Hex(), err)
	}
	if !ok {
		return false, nil // CAS 未命中（并发回滚/已复检）：幂等跳过，不重复告警
	}
	if aerr := s.publisher.PublishAlert(ctx, CertAlertEvent{
		Category:    AlertCategoryTLSDiff, // TLS 差异通道语义（tech-design crd-recheck 行）
		Title:       "CRD 证书引用复检失败（reconcile 回写），转人工处置",
		Fingerprint: order.OldCertFingerprint,
		OrderID:     order.ID.Hex(),
		Detail:      msg, // reason 已附 orderId/itemId（5.6 recheckFailReason）
		At:          at,
	}); aerr != nil {
		return true, fmt.Errorf("crd recheck: publish recheck-failed alert for item %s: %w", it.ID.Hex(), aerr)
	}
	return true, nil
}
