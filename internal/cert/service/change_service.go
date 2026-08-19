package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ---------------------------------------------------------------------
// 变更单生命周期服务（任务 5.1 骨架：状态机迁移 + 取消语义 +
// 暂停超时自动取消；GenerateChangeList 在 5.2 落地（changelist_generator.go）；
// Execute/Rollback 在 5.7/5.8 落地，ConfirmBatch/GetReport 随执行链路任务补充）
// ---------------------------------------------------------------------

// ChangeService 变更单生命周期（tech-design Interface 3）。
// 互斥正确性由 uk_active_mutex 部分唯一索引保证（Hard Rule）：本服务不做
// 应用层预检查（预检查仅作快速失败，属 5.2 清单生成路径）。
type ChangeService interface {
	// GenerateChangeList 清单生成（任务 5.2，实现见 changelist_generator.go）：
	// 按旧证书指纹聚合最新成功快照的 CertReference 生成变更清单，预生成
	// pending_confirm 订单（条件写入 activeMutex）+ 变更项并绑定 snapshotId；
	// 四项前置校验阻断（409）：SCAN_STALE / CHANGE_IN_FLIGHT /
	// NEW_CERT_FINGERPRINT_ONLY / SAN_INSUFFICIENT。
	GenerateChangeList(ctx context.Context, oldCertFingerprint, newCertID string) (ChangeList, error)
	// Transition 状态机迁移入口：先按 domain.Transition 白名单校验，再路由至
	// 仓储原子原语——
	//   - 活跃态（pending_confirm/executing/verifying）：TransitionActive，
	//     activeMutex=oldCertFingerprint 与状态同一原子 update；token 与在途
	//     活跃单冲突（索引强制）→ ErrChangeInFlight（409 CHANGE_IN_FLIGHT）；
	//   - completed/partial_completed：TransitionTerminalWithProtect，按
	//     thresholds.rollbackProtectDays 同原子固化订单 protectUntil 并写
	//     旧证书保护期（2.3 删除拦截依据）；
	//   - 其余终态：TransitionTerminal（$unset token 与状态同一原子 update）。
	Transition(ctx context.Context, orderID string, target domain.ChangeStatus) error
	// Cancel 取消语义（AC-3，分支判定见 domain.CancelModeOf）：
	//   - draft / pending_confirm / 批间暂停（executing 且 batchInfo.paused）：
	//     整单取消——未执行项标 skipped，转 cancelled 终态并同原子清除 activeMutex；
	//   - executing（未暂停）：中止路径——仅 pending 项标 skipped，running 项
	//     不中断、订单保持 executing，待运行项完成后按剩余项重算收敛 cancelled
	//     （收敛逻辑在 5.5/5.7 执行链路）；
	//   - verifying 与全部终态：ErrChangeNotCancellable（409 CHANGE_NOT_CANCELLABLE）。
	Cancel(ctx context.Context, orderID string) error
	// CancelByTimeout 批间暂停超时自动取消（互斥活性保障路径②，调度入口在 7.1）：
	// 扫描 batchInfo.paused=true 且 pausedAt + thresholds.pauseTimeoutHours < now
	// 的订单，逐单整单取消（同 Cancel 整单路径，未执行项标 skipped）。返回本次
	// 取消的订单 ID 清单，供调度方发送"变更单超时取消"处置通知（运维处置类
	// 通知，不计入 PRD 四类业务告警口径）。单笔失败不中断其余扫描，首批错误
	// 随已取消清单一并返回。
	CancelByTimeout(ctx context.Context) ([]string, error)
}

type changeService struct {
	orders   domain.ChangeOrderRepository
	items    domain.ChangeItemRepository
	certs    domain.CertificateRepository
	alertCfg domain.AlertConfigRepository
	// 清单生成依赖（任务 5.2）
	snapshots domain.ScanSnapshotRepository
	refs      domain.CertReferenceRepository
	// probe K8s 管理权三信号探测端口（实际探测属 5.6 K8sAPIChannel，接口注入
	// 解耦）：nil=探测通道未接入，K8s 项按不可执行项分区（不静默放行）。
	probe ManagementProbe
}

// NewChangeService 创建变更单生命周期服务。
func NewChangeService(
	orders domain.ChangeOrderRepository,
	items domain.ChangeItemRepository,
	certs domain.CertificateRepository,
	alertCfg domain.AlertConfigRepository,
	snapshots domain.ScanSnapshotRepository,
	refs domain.CertReferenceRepository,
	probe ManagementProbe,
) ChangeService {
	return &changeService{
		orders:    orders,
		items:     items,
		certs:     certs,
		alertCfg:  alertCfg,
		snapshots: snapshots,
		refs:      refs,
		probe:     probe,
	}
}

// Transition 状态机迁移：白名单校验 → 原子原语路由。
func (s *changeService) Transition(ctx context.Context, orderID string, target domain.ChangeStatus) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("change: get order: %w", err)
	}
	if err := domain.Transition(order.Status, target); err != nil {
		return err // *InvalidTransitionError：白名单外迁移（调用方误用/状态漂移）
	}
	if domain.IsTerminalChangeStatus(target) {
		return s.transitionTerminal(ctx, order, target)
	}
	// 活跃态：token 写入与状态同一原子 update；uk_active_mutex 对 update 同样
	// 强制（check-then-update 竞态窗口被关闭）。
	if err := s.orders.TransitionActive(ctx, orderID, target, order.OldCertFingerprint); err != nil {
		return fmt.Errorf("change: transition to %s: %w", target, err)
	}
	return nil
}

// transitionTerminal 终态落地：完成类终态附带回滚保护期固化，其余仅状态
// 迁移 + token 清除（同一原子 update）。
func (s *changeService) transitionTerminal(ctx context.Context, order domain.ChangeOrder, target domain.ChangeStatus) error {
	switch target {
	case domain.ChangeStatusCompleted, domain.ChangeStatusPartialCompleted:
		cfg, err := s.alertCfg.Get(ctx)
		if err != nil {
			return fmt.Errorf("change: get alert config: %w", err)
		}
		protectUntil := time.Now().AddDate(0, 0, cfg.Thresholds.RollbackProtectDays)
		// 订单终态 + protectUntil + token ���除同一原子 update（Hard Rule）
		if err := s.orders.TransitionTerminalWithProtect(ctx, order.ID.Hex(), target, protectUntil); err != nil {
			return fmt.Errorf("change: transition to %s: %w", target, err)
		}
		// 旧证书保护期（2.3 删除拦截依据）：终态先落地，证书保护随后固化
		//（幂等、只延长不缩短）；失败上抛供调用方感知，不回滚订单终态。
		if err := s.certs.SetProtectUntil(ctx, order.OldCertFingerprint, protectUntil); err != nil {
			return fmt.Errorf("change: set old cert protect until: %w", err)
		}
		return nil
	default:
		if err := s.orders.TransitionTerminal(ctx, order.ID.Hex(), target); err != nil {
			return fmt.Errorf("change: transition to %s: %w", target, err)
		}
		return nil
	}
}

// Cancel 取消：按 CancelModeOf 分支（draft/pending_confirm/批间暂停=整单，
// executing=中止未开始项，verifying/终态=不可取消）。
func (s *changeService) Cancel(ctx context.Context, orderID string) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("change: get order: %w", err)
	}
	switch domain.CancelModeOf(order.Status, order.BatchInfo) {
	case domain.CancelModeNone:
		// verifying 与终态不可取消（409 CHANGE_NOT_CANCELLABLE）
		return fmt.Errorf("%w: status=%s", domain.ErrChangeNotCancellable, order.Status)
	case domain.CancelModeAbort:
		// 执行中止：仅未开始项标 skipped；running 不中断、状态保持 executing
		if _, err := s.items.MarkPendingSkipped(ctx, orderID); err != nil {
			return fmt.Errorf("change: mark pending items skipped: %w", err)
		}
		return nil
	case domain.CancelModeWhole:
		return s.cancelWhole(ctx, order)
	default:
		return fmt.Errorf("change: unknown cancel mode for status=%s", order.Status)
	}
}

// cancelWhole 整单取消：未执行项标 skipped → cancelled 终态 + token 清除
// （同一原子 update）。标记先于终态迁移：两步之间崩溃可重试 Cancel 收敛
// （标记幂等），反序则会在项残留 pending 的同时丢失活跃单上下文。
// 注：终态迁移不做状态前置条件 CAS——人工取消与状态机推进并发的窗口下
// 以终态覆盖终态（token 已清、$unset 幂等），活单残留 token 的风险不存在。
func (s *changeService) cancelWhole(ctx context.Context, order domain.ChangeOrder) error {
	if _, err := s.items.MarkPendingSkipped(ctx, order.ID.Hex()); err != nil {
		return fmt.Errorf("change: mark pending items skipped: %w", err)
	}
	if err := s.orders.TransitionTerminal(ctx, order.ID.Hex(), domain.ChangeStatusCancelled); err != nil {
		return fmt.Errorf("change: cancel order: %w", err)
	}
	return nil
}

// CancelByTimeout 暂停超时自动取消（liveness：暂停单不无限期持锁）。
func (s *changeService) CancelByTimeout(ctx context.Context) ([]string, error) {
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("change: get alert config: %w", err)
	}
	cutoff := time.Now().Add(-time.Duration(cfg.Thresholds.PauseTimeoutHours) * time.Hour)
	paused, err := s.orders.ListPausedBefore(ctx, cutoff)
	if err != nil {
		return nil, fmt.Errorf("change: list paused orders: %w", err)
	}
	cancelled := make([]string, 0, len(paused))
	var firstErr error
	for _, o := range paused {
		if err := s.cancelWhole(ctx, o); err != nil {
			if firstErr == nil {
				firstErr = err // 单笔失败不���断扫描，保留首批错误上抛
			}
			continue
		}
		cancelled = append(cancelled, o.ID.Hex())
	}
	return cancelled, firstErr
}
