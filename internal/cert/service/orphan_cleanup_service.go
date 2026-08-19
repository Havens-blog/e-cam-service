package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 孤儿清理消费者（任务 5.9，tech-design Scheduler Tasks orphan-cleanup 行）：
//
//	CloudCertMapping.status=orphan 即清理队列成员（5.3 第二段失败补偿标记 /
//	5.8 回滚成功 markNewCertOrphan / 本服务在归属单终态时对成功项旧证书入队）。
//	消费门槛（Hard Rule：不得清理仍在 active（未替换完成）或保护期内的云证书）：
//	  - 清理前复核映射最新状态仍为 orphan（被重新 upsert 回 active 即未替换完成）；
//	  - 归属变更单验证达标/终态——映射证书仍是任一活跃单的旧证书（activeMutex
//	    token）或新证书（ListByNewCertID）时不消费（替换/复检在途）；
//	  - 证书保护期（Certificate.protectUntil > now）→ skip_keep 暂留。
//	逐项调 CloudDeployer.CleanupOrphan（经 CloudAPIChannel 路由，幂等——对
//	已删除证书成功），结果 OrphanCleanupResult 写入变更报告；失败项保留 orphan
//	转运维处置告警（AlertCategoryOps，不计入 PRD 四类业务告警口径）。
//
// 两个消费入口（7.1 调度注册）：
//	  - ConsumeOrderQueue：事件触发（验证窗口达标关闭后即时消费该单清理队列）；
//	  - SweepOrphans：天级批扫（ListByStatus 全量孤儿，事件遗漏的兜底）。
//
// 幂等（AC-4，以项 ID + 动作去重）：清理成功即删除映射（不再命中扫描）；
// 失败/暂留结果经 recorder 以 (orderID, cloudCertId, action, success) 去重，
// 重复消费不产生重复结果/重复告警。
// ---------------------------------------------------------------------

// OrphanCleanupResult 孤儿云证书清理单项结果（tech-design Service-Level Types，
// ChangeReport.OrphanCleanup 元素；逐项成功/失败，PRD Story 3/5 AC）。
type OrphanCleanupResult struct {
	Cloud       string    // 清理动作所属云（aliyun|tencent）
	CloudCertID string    // 被清理的云侧证书 ID
	Action      string    // cleanup=执行清理 | skip_keep=暂留（保护期内）
	Success     bool      // 清理成败；false 触发运维处置告警
	At          time.Time // 清理动作时间
}

// OrphanCleanupResult.Action 取值。
const (
	// OrphanActionCleanup 执行清理。
	OrphanActionCleanup = "cleanup"
	// OrphanActionSkipKeep 暂留（保护期内）。
	OrphanActionSkipKeep = "skip_keep"
)

// OrphanCleanupRecorder 变更报告 OrphanCleanup 结果写入端口（任务 5.9）。
//
// tech-design 定义 ChangeReport 为非持久化载荷（GetReport 查询时聚合），故结果
// 落库经本端口承载（同 5.8 RollbackAuditRecorder 口径）：生产实现由 7.x 接线
// （5.11 报告聚合消费，audit action=orphan_cleanup）；nil=no-op（消费仍执行，
// 结果仅余映射状态与日志）。幂等契约（AC-4）：实现方以
// (orderID, cloudCertId, action, success) 为去重键，重复记录返回
// recorded=false（调用方据此抑制重复告警）。
type OrphanCleanupRecorder interface {
	// RecordOrphanCleanup 记录单项清理结果；返回是否新写入（false=同键已存在）。
	RecordOrphanCleanup(ctx context.Context, orderID string, result OrphanCleanupResult) (bool, error)
}

// OrphanCleaner 孤儿清理执行端口：per 云 CloudDeployer.CleanupOrphan 路由
// （生产实现 *deployer.CloudAPIChannel.CleanupOrphanCert，5.4/5.5 组装的
// 部署器经注册表路由；discovery-only 三云无孤儿映射，天然不触达）。
type OrphanCleaner interface {
	// CleanupOrphanCert 清理 cloud 证书库中的 cloudCertID（对已删除证书幂等成功）。
	CleanupOrphanCert(ctx context.Context, creds deployer.Credential, cloud, cloudCertID string) error
}

// 编译期断言：云 API 通道满足清理执行端口。
var _ OrphanCleaner = (*deployer.CloudAPIChannel)(nil)

// OrphanCleanupService 孤儿清理消费者（orphan-cleanup 队列消费，AC-1/AC-2）。
type OrphanCleanupService interface {
	// ConsumeOrderQueue 事件触发消费：验证窗口达标关闭/终态后即时消费该单清理
	// 队列——成功项旧证书映射入队（active→orphan，OrphanCandidate=true 的
	// 归属单达标入队语义），随后逐项清理该单 old/new 两指纹的全部 orphan 映射。
	// 入口门控：仅终态单可消费（completed/partial_completed/rolled_back/
	// rollback_failed/cancelled），非终态返回错误（Hard Rule：验证未达标不清理）。
	// 返回消费条数；单项基础设施故障不中断其他项（逐项隔离），首批错误随计数返回。
	ConsumeOrderQueue(ctx context.Context, orderID string) (int, error)
	// SweepOrphans 天级批扫：全量 status=orphan 映射逐项消费（保护期内
	// skip_keep；归属单仍活跃的静默跳过，留待该单终态消费）。返回消费条数；
	// 单项失败不中断扫描，首批错误随计数返回。
	SweepOrphans(ctx context.Context) (int, error)
}

// ---------------------------------------------------------------------
// 服务实现
// ---------------------------------------------------------------------

type orphanCleanupService struct {
	orders   domain.ChangeOrderRepository
	items    domain.ChangeItemRepository
	certs    domain.CertificateRepository
	mappings domain.CloudCertMappingRepository

	cleaner   OrphanCleaner           // per 云 CleanupOrphan 路由；nil=fail closed
	creds     ChannelCredentialSource // 云账号凭证（5.7 端口复用；Secret 用后 Zeroize）
	recorder  OrphanCleanupRecorder   // 报告结果写入；nil=no-op
	publisher CertAlertPublisher      // 清理失败运维处置通知（AlertCategoryOps）
	now       func() time.Time        // 测试可注入时间源
}

// NewOrphanCleanupService 创建孤儿清理消费者。cleaner/creds 为清理执行必需
// （nil 时清理动作 fail closed 报错，队列状态不受影响）；recorder nil=no-op；
// publisher nil 回退日志发布（同 5.8 口径）。
func NewOrphanCleanupService(
	orders domain.ChangeOrderRepository,
	items domain.ChangeItemRepository,
	certs domain.CertificateRepository,
	mappings domain.CloudCertMappingRepository,
	cleaner OrphanCleaner,
	creds ChannelCredentialSource,
	recorder OrphanCleanupRecorder,
	publisher CertAlertPublisher,
) OrphanCleanupService {
	if publisher == nil {
		publisher = NewLoggingAlertPublisher()
	}
	return &orphanCleanupService{
		orders:    orders,
		items:     items,
		certs:     certs,
		mappings:  mappings,
		cleaner:   cleaner,
		creds:     creds,
		recorder:  recorder,
		publisher: publisher,
		now:       time.Now,
	}
}

// ConsumeOrderQueue 事件触发消费（AC-1）。
func (s *orphanCleanupService) ConsumeOrderQueue(ctx context.Context, orderID string) (int, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return 0, fmt.Errorf("orphan cleanup: get order: %w", err)
	}
	if !domain.IsTerminalChangeStatus(order.Status) {
		return 0, fmt.Errorf("orphan cleanup: order %s is %s, not terminal (验证达标/终态后才可消费清理队列)",
			orderID, order.Status)
	}
	items, err := s.items.ListByOrder(ctx, orderID)
	if err != nil {
		return 0, fmt.Errorf("orphan cleanup: list items of order %s: %w", orderID, err)
	}

	// 入队（OrphanCandidate=true 且归属单达标后入清理队列）：成功项旧云证书
	// 映射 active→orphan；失败/skipped 项引用未被改动、rolled_back 项旧证书
	// 已恢复绑定，均不入队。
	if err := s.enqueueOldCertOrphans(ctx, items); err != nil {
		return 0, err
	}

	// 该单清理队列：old/new 两指纹的全部 orphan 映射（5.8 回滚孤儿/5.3 第二段
	// 失败孤儿按 newFp 命中，本单入队旧证书按 oldFp 命中），按映射 ID 去重。
	fingerprints := []string{order.OldCertFingerprint}
	if newCert, err := s.certs.GetByID(ctx, order.NewCertID); err == nil && newCert.Fingerprint != "" {
		fingerprints = append(fingerprints, newCert.Fingerprint)
	} else if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return 0, fmt.Errorf("orphan cleanup: load new certificate %s: %w", order.NewCertID, err)
	}
	candidates, err := s.orphanMappingsFor(ctx, fingerprints)
	if err != nil {
		return 0, err
	}

	consumed := 0
	var firstErr error
	for _, m := range candidates {
		handled, err := s.cleanupOne(ctx, order.ID.Hex(), m)
		if handled {
			consumed++ // 清理动作已发生（含结果记录失败的重试后续）
		}
		if err != nil && firstErr == nil {
			firstErr = err // 逐项隔离：单项基础设施故障不阻塞其他项
		}
	}
	return consumed, firstErr
}

// SweepOrphans 天级批扫（AC-1）。
func (s *orphanCleanupService) SweepOrphans(ctx context.Context) (int, error) {
	orphans, err := s.mappings.ListByStatus(ctx, domain.MappingStatusOrphan)
	if err != nil {
		return 0, fmt.Errorf("orphan cleanup: sweep list orphans: %w", err)
	}
	consumed := 0
	var firstErr error
	for _, m := range orphans {
		// 归属单可解析则以其承载报告（best-effort；不可解析的记录 orderID 空，
		// 天级兜底口径），活跃单门槛在 cleanupOne 内统一判定。
		_, ownerID, err := s.owningOrders(ctx, m)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		handled, err := s.cleanupOne(ctx, ownerID, m)
		if handled {
			consumed++
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return consumed, firstErr
}

// enqueueOldCertOrphans 成功项旧云证书映射入队（active→orphan；幂等——已
// orphan 无操作）。无映射记录（外部上传/历史数据）静默跳过：无队列载荷。
func (s *orphanCleanupService) enqueueOldCertOrphans(ctx context.Context, items []domain.ChangeItem) error {
	for _, it := range items {
		if it.Status != domain.ItemStatusSuccess {
			continue // 仅成功项的旧证书被替换（OrphanCandidate）
		}
		if it.ResourceRef.Channel != domain.ChannelCloudAPI || it.OldCloudCertID == "" {
			continue // K8s 项无云证书库映射；无旧值不构成候选
		}
		m, err := s.mappings.FindByCloudCertID(ctx, it.ResourceRef.Cloud, it.ResourceRef.AccountKey, it.OldCloudCertID)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue
			}
			return fmt.Errorf("orphan cleanup: find old cert mapping for item %s: %w", it.ID.Hex(), err)
		}
		if m.Status == domain.MappingStatusOrphan {
			continue
		}
		if err := s.mappings.UpdateStatus(ctx, m.ID.Hex(), domain.MappingStatusOrphan); err != nil {
			return fmt.Errorf("orphan cleanup: enqueue old cloud cert %s orphan: %w", it.OldCloudCertID, err)
		}
	}
	return nil
}

// orphanMappingsFor 收集各指纹的 orphan 映射（按映射 ID 去重，uploadedAt 序）。
func (s *orphanCleanupService) orphanMappingsFor(ctx context.Context, fingerprints []string) ([]domain.CloudCertMapping, error) {
	seen := make(map[string]struct{})
	var out []domain.CloudCertMapping
	for _, fp := range fingerprints {
		if fp == "" {
			continue
		}
		ms, err := s.mappings.ListByFingerprint(ctx, fp)
		if err != nil {
			return nil, fmt.Errorf("orphan cleanup: list mappings for %s: %w", fp, err)
		}
		for _, m := range ms {
			if m.Status != domain.MappingStatusOrphan {
				continue
			}
			if _, dup := seen[m.ID.Hex()]; dup {
				continue
			}
			seen[m.ID.Hex()] = struct{}{}
			out = append(out, m)
		}
	}
	return out, nil
}

// cleanupOne 消费单个孤儿映射（AC-2 状态流转 + Hard Rule 门槛）。返回
// handled=true 表示产生了清理动作或 skip_keep 记录；返回 error 仅为基础设施
// 故障（仓储/凭证/报告写入失败——清理失败本身是项级结果，保留 orphan 供
// 重试，不以 error 呈现）。
func (s *orphanCleanupService) cleanupOne(ctx context.Context, orderID string, m domain.CloudCertMapping) (bool, error) {
	// Hard Rule：仍在 active（未替换完成）的不清理——清理前复核最新映射状态
	//（扫描列表后可能已被重新 upsert 回 active，或已被并发消费清理）。
	cur, err := s.mappings.FindByCloudCertID(ctx, m.Cloud, m.AccountKey, m.CloudCertID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil // 已清理/覆盖：静默跳过（幂等）
		}
		return false, fmt.Errorf("orphan cleanup: re-read mapping for cloud cert %s: %w", m.CloudCertID, err)
	}
	if cur.Status != domain.MappingStatusOrphan {
		return false, nil // 重新激活（未替换完成）：不清理
	}

	// Hard Rule：归属变更单验证达标/终态——映射证书仍是任一活跃单的替换对象
	// 或新证书（第二段失败孤儿在单在途期间不清理，等该单终态消费）。
	active, _, err := s.owningOrders(ctx, cur)
	if err != nil {
		return false, err
	}
	if active {
		return false, nil
	}

	// Hard Rule：保护期内不清理（skip_keep 暂留，结果入报告）。
	protected, err := s.certProtected(ctx, cur.CertFingerprint)
	if err != nil {
		return false, err
	}
	at := s.now()
	if protected {
		if _, err := s.record(ctx, orderID, OrphanCleanupResult{
			Cloud:       cur.Cloud,
			CloudCertID: cur.CloudCertID,
			Action:      OrphanActionSkipKeep,
			Success:     true,
			At:          at,
		}); err != nil {
			return true, err
		}
		return true, nil
	}

	if s.cleaner == nil || s.creds == nil {
		return false, fmt.Errorf("orphan cleanup: cleaner or credential source not assembled (fail closed)")
	}
	creds, err := s.creds.CloudCredential(ctx, cur.Cloud, cur.AccountKey)
	if err != nil {
		return false, fmt.Errorf("orphan cleanup: resolve credential for cloud %s account %s: %w",
			cur.Cloud, cur.AccountKey, err)
	}
	cerr := s.cleaner.CleanupOrphanCert(ctx, creds, cur.Cloud, cur.CloudCertID)
	creds.Zeroize()

	if cerr != nil {
		// 清理失败（AC-2）：映射保留 orphan 供重试；结果 + 运维处置告警
		//（AlertCategoryOps，不计入四类业务告警）。幂等：重复消费经 recorder
		// 去重命中（recorded=false）不再重复告警（AC-4）。
		recorded, rerr := s.record(ctx, orderID, OrphanCleanupResult{
			Cloud:       cur.Cloud,
			CloudCertID: cur.CloudCertID,
			Action:      OrphanActionCleanup,
			Success:     false,
			At:          at,
		})
		if rerr != nil {
			return true, rerr
		}
		if recorded {
			if aerr := s.publisher.PublishAlert(ctx, CertAlertEvent{
				Category:    AlertCategoryOps,
				Title:       "孤儿云证书清理失败，转运维处置",
				Fingerprint: cur.CertFingerprint,
				OrderID:     orderID,
				Detail: fmt.Sprintf("cloud %s account %s cert %s: %v",
					cur.Cloud, cur.AccountKey, cur.CloudCertID, cerr),
				At: at,
			}); aerr != nil {
				return true, fmt.Errorf("orphan cleanup: publish cleanup-failed alert for cloud cert %s: %w",
					cur.CloudCertID, aerr)
			}
		}
		return true, nil
	}

	// 清理成功（AC-2）：orphan→删除映射（status enum 仅 active/orphan，
	// "标 cleaned"以删除承载）+ 结果入报告。
	if err := s.mappings.DeleteByID(ctx, cur.ID.Hex()); err != nil {
		return true, fmt.Errorf("orphan cleanup: delete mapping for cloud cert %s: %w", cur.CloudCertID, err)
	}
	if _, err := s.record(ctx, orderID, OrphanCleanupResult{
		Cloud:       cur.Cloud,
		CloudCertID: cur.CloudCertID,
		Action:      OrphanActionCleanup,
		Success:     true,
		At:          at,
	}); err != nil {
		return true, err
	}
	return true, nil
}

// owningOrders 解析映射证书关联的变更单（归属门槛 + 报告归属）：
//   - active=true：存在活跃单以该证书为旧证书（activeMutex token，替换在途）
//     或新证书（第二段失败/回滚孤儿上传的单在途）——Hard Rule 不清理；
//   - ownerID：NewCertID 命中的最近单（createdAt 降序首条，报告承载）；
//     无命中返回空串（天级兜底口径）。
func (s *orphanCleanupService) owningOrders(ctx context.Context, m domain.CloudCertMapping) (bool, string, error) {
	if _, err := s.orders.GetByMutexToken(ctx, m.CertFingerprint); err == nil {
		return true, "", nil // 活跃单以该证书为旧证书：替换在途
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return false, "", fmt.Errorf("orphan cleanup: query active order by mutex token: %w", err)
	}
	cert, err := s.certs.GetByFingerprint(ctx, m.CertFingerprint)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, "", nil // 台账无此证（已删/外部）：无新证书语境归属单
		}
		return false, "", fmt.Errorf("orphan cleanup: get certificate %s: %w", m.CertFingerprint, err)
	}
	orders, err := s.orders.ListByNewCertID(ctx, cert.ID.Hex())
	if err != nil {
		return false, "", fmt.Errorf("orphan cleanup: list orders by new cert: %w", err)
	}
	ownerID := ""
	for _, o := range orders { // createdAt 降序：首条即最近归属单
		if ownerID == "" {
			ownerID = o.ID.Hex()
		}
		if domain.IsActiveChangeStatus(o.Status) {
			return true, ownerID, nil // 以该证书为新证书的单在途
		}
	}
	return false, ownerID, nil
}

// certProtected 证书是否处于保护期（Certificate.protectUntil > now；Hard
// Rule：保护期内不清理）。台账无此证时视为无保护（删除拦截已保证未保护才可删）。
func (s *orphanCleanupService) certProtected(ctx context.Context, fingerprint string) (bool, error) {
	cert, err := s.certs.GetByFingerprint(ctx, fingerprint)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil
		}
		return false, fmt.Errorf("orphan cleanup: get certificate %s for protection check: %w", fingerprint, err)
	}
	return cert.ProtectUntil != nil && cert.ProtectUntil.After(s.now()), nil
}

// record 报告结果写入（nil=no-op，返回 recorded=true 交由调用方告警判定）。
func (s *orphanCleanupService) record(ctx context.Context, orderID string, result OrphanCleanupResult) (bool, error) {
	if s.recorder == nil {
		return true, nil
	}
	recorded, err := s.recorder.RecordOrphanCleanup(ctx, orderID, result)
	if err != nil {
		return false, fmt.Errorf("orphan cleanup: record result for cloud cert %s: %w", result.CloudCertID, err)
	}
	return recorded, nil
}
