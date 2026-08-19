package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------------------------------------------------------------------
// 清单生成（任务 5.2：指纹聚合 + 四项前置校验 + SAN 预检 + 不可执行项分区 +
// 盲区声明 + 快照绑定；tech-design Interface 3 GenerateChangeList）
// ---------------------------------------------------------------------

// ChangeList GenerateChangeList 生成的变更清单（非持久化载荷，tech-design
// Service-Level Types）。
type ChangeList struct {
	OrderID          string           // 预生成的变更单 ID（待确认态 pending_confirm）
	OldFingerprint   string           // 旧证书指纹，SHA256 hex
	NewCertID        string           // 新证书 ID
	SnapshotID       string           // 清单绑定的扫描快照（新鲜度校验依据）
	ScanFreshnessHrs int              // 生成时扫描新鲜度（小时）；超阈值的快照直接阻断生成
	Items            []ChangeListItem // 按指纹聚合的引用项
	SANCheck         SanCheckResult   // SAN 预检结果
	Warnings         []string         // 盲区声明/不可自动变更项/覆盖率分母不可用提示
}

// ChangeListItem 清单单项（tech-design Service-Level Types）。
type ChangeListItem struct {
	ItemID         string                // 变更项 ID（持久化 ChangeItem._id，报告/回滚关联）
	Target         deployer.DeployTarget // 目标资源定位；持久化完整写入 ChangeItem.resourceRef（按 action 分支必填）
	Action         domain.ChangeAction   // "upload_and_bind" | "patch_crd"
	AutoChangeable bool                  // false=不可自动变更（discovery-only 云首期无部署器 / K8s 管理权受限）
	Reason         string                // AutoChangeable=false 时的判定依据（信号类型+具体键 / ERR_DISCOVERY_ONLY）
}

// SanCheckResult SAN 预检结果（基准 = 变更清单目标域名集合，PRD 评估遗留项 #4）。
type SanCheckResult struct {
	Passed  bool     // 新证书 SAN ⊇ 全部目标域名
	Missing []string // 缺失域名列表；Passed=false 时非空
	NewSANs []string // 新增域名（提示性，不做拦截）
}

// ManagementProbe K8s 管理权三信号探测端口（GitOps 管理 label / ownerReferences
// 非空 / 管理类 annotation，判定规则集见 tech-design"K8s 管理权判定与变更后复检"）。
// 实际探测逻辑属 5.6 K8sAPIChannel，经接口注入解耦——清单生成期读取判定结果
// （5.6 亦可在扫描期预标记后经本端口返回缓存结论）。
type ManagementProbe interface {
	// Probe 探测单个 K8s 资源是否可自动变更（三信号任一命中即不可）。
	// manageable=false 时 reason 记录命中信号类型+具体键；err 非 nil（如集群
	// 不可达）时调用方按不可执行项分区处理（单点不可用不阻塞清单生成）。
	Probe(ctx context.Context, ref domain.ResourceRef) (manageable bool, reason string, err error)
}

// K8sAPIChannel（任务 5.6）结构化实现本端口——清单生成期 K8s 项的
// AutoChangeable 判定由该通道三信号探测回填（编译期绑定，防签名漂移）。
var _ ManagementProbe = (*deployer.K8sAPIChannel)(nil)

// discoveryOnlyClouds 首期无部署器的 discovery-only 云（PRD Out of Scope：华为云/
// AWS/Azure 部署器二期；引用纳入台账与覆盖率分母，进入清单时为不可执行项，
// cloudx.ErrDiscoveryOnly 同语义，清单生成期按云名单静态判定）。
var discoveryOnlyClouds = map[domain.Cloud]struct{}{
	domain.CloudHuawei: {},
	domain.CloudAWS:    {},
	domain.CloudAzure:  {},
}

// 清单 Warnings 固定文案（盲区声明；仅静态描述与安全参数，不含凭证/私钥片段）。
const (
	// warningNginxBoundary 覆盖边界声明（PRD In Scope：视图与变更清单显式声明
	// 覆盖边界——首期不含 VM Nginx 配置级引用）。
	warningNginxBoundary = "覆盖边界：本清单不含 VM Nginx 配置级引用（首期盲区，仅 TLS 探测监控；更换依赖二期堡垒机/Agent 通道）"
	// warningK8sUnprobed 管理权探测通道未接入声明（5.6 落地前 K8s 项按不可执行分区）。
	warningK8sUnprobed = "K8s 项管理权未探测：探测通道未接入（5.6），K8s 引用按不可执行项分区，接入后自动判定"
	// warningPartitionFmt 不可执行项分区汇总（Hard Rule：不静默放行，显式声明原因与出路）。
	warningPartitionFmt = "不可执行项 %d 项（首期无部署器/K8s 管理权受限），不计入执行成功率分母：请走二期部署器、GitOps/控制器管理链路或手工更换"
	// warningBlindSpotFmt 扫描通道失败盲区（该范围引用可能缺失，"未发现引用"≠"无引用"）。
	warningBlindSpotFmt = "盲区：%s/%s（%s）扫描通道失败（%s），该范围引用可能缺失"
	// warningDenominatorFmt coverageMeta total=-1 分母不可用（asset 盘点缺失；K8s crd 恒 -1）。
	warningDenominatorFmt = "分母不可用：%s/%s 资产盘点分母缺失（total=-1），覆盖率不显示 0%%"
)

// 不可执行原因文案（Reason 首词为可机读标记，对齐 tech-design Interface 2 与
// K8s 管理权判定规则集的信号口径）。
const (
	reasonDiscoveryOnly       = "ERR_DISCOVERY_ONLY: %s 首期无部署器（discovery-only 云），待二期部署器开放，请手工更换"
	reasonK8sUnprobed         = "K8S_MANAGEMENT_UNPROBED: 管理权探测通道未接入（5.6），暂不可自动变更"
	reasonK8sProbeFailedFmt   = "K8S_MANAGEMENT_PROBE_FAILED: %s"
	reasonK8sNotManageableFmt = "K8S_MANAGEMENT_SIGNAL: %s"
)

// GenerateChangeList 清单生成（tech-design Interface 3）：
// 按旧证书指纹聚合最新成功快照（status=done）内的 CertReference，逐项生成
// ChangeListItem（resourceRef 按 action 分支持久化完整 DeployTarget），预生成
// pending_confirm 订单 + 变更项并绑定 snapshotId。
//
// 四项前置校验全部前置于清单生成期（Hard Rule：任何一项不满足不生成可执行清单，
// 不得延后到 Execute）：
//  1. SCAN_STALE —— 无成功快照，或 now-startedAt > thresholds.scanFreshnessHours
//     （清单强制绑定最近扫描，超期阻断并提示先扫描）；
//  2. CHANGE_IN_FLIGHT —— 同 oldFingerprint 已有活跃单（应用层预检查仅快速失败，
//     正确性由 uk_active_mutex 部分唯一索引在订单插入路径强制，竞态窗口被关闭）；
//  3. NEW_CERT_FINGERPRINT_ONLY —— 新证书 hostingStatus≠complete（fingerprint_only
//     无私钥，无法上传云证书库执行两段式第一段）；
//  4. SAN_INSUFFICIENT —— 新证书 SAN ⊉ 目标域名集合（基准 = 旧证书 SAN，即清单项
//     所服务域名的并集；防通配符/多 SAN 证书更换"静默丢域名"漏换）。
//
// 不可执行项分区（Hard Rule：不静默放行）：discovery-only 云与 K8s 管理权受限项
// AutoChangeable=false + Reason，持久化即标 skipped（不计入执行成功率分母，
// 5.7 仅执行 pending 项）；盲区声明（覆盖边界/通道失败盲区/分母不可用）写��� Warnings。
func (s *changeService) GenerateChangeList(ctx context.Context, oldCertFingerprint, newCertID string) (ChangeList, error) {
	// ---- 前置校验 1：扫描新鲜度（含"无成功快照"阻断） ----
	snap, err := s.snapshots.LatestDone(ctx)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ChangeList{}, fmt.Errorf("%w: no successful scan snapshot available", domain.ErrScanStale)
		}
		return ChangeList{}, fmt.Errorf("change: load latest scan snapshot: %w", err)
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return ChangeList{}, fmt.Errorf("change: get alert config: %w", err)
	}
	freshness := time.Since(snap.StartedAt)
	if freshness > time.Duration(cfg.Thresholds.ScanFreshnessHours)*time.Hour {
		return ChangeList{}, fmt.Errorf("%w: scan snapshot age %dh exceeds scanFreshnessHours=%d",
			domain.ErrScanStale, int(freshness.Hours()), cfg.Thresholds.ScanFreshnessHours)
	}

	// ---- 前置校验 2：在途互斥（快速失败；正确性由 uk_active_mutex 保证） ----
	if _, err := s.orders.GetByMutexToken(ctx, oldCertFingerprint); err == nil {
		return ChangeList{}, fmt.Errorf("%w: active change order already exists for fingerprint", domain.ErrChangeInFlight)
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return ChangeList{}, fmt.Errorf("change: check in-flight order: %w", err)
	}

	// ---- 前置校验 3：新证书托管形态 ----
	newCert, err := s.certs.GetByID(ctx, newCertID)
	if err != nil {
		return ChangeList{}, fmt.Errorf("change: load new certificate: %w", err)
	}
	if newCert.HostingStatus != domain.HostingStatusComplete {
		return ChangeList{}, fmt.Errorf("%w: new certificate hostingStatus=%s (no private key to upload)",
			domain.ErrNewCertFingerprintOnly, newCert.HostingStatus)
	}

	// ---- 前置校验 4：SAN 预检（基准 = 旧证书 SAN = 清单项所服务域名并集） ----
	oldCert, err := s.certs.GetByFingerprint(ctx, oldCertFingerprint)
	if err != nil {
		return ChangeList{}, fmt.Errorf("change: load old certificate: %w", err)
	}
	sanCheck := checkSansCover(oldCert.Sans, newCert.Sans)
	if !sanCheck.Passed {
		return ChangeList{}, fmt.Errorf("%w: new certificate SAN missing %d target domain(s): %s",
			domain.ErrSanInsufficient, len(sanCheck.Missing), strings.Join(sanCheck.Missing, ", "))
	}

	// ---- 指纹聚合：最新成功快照内该指纹引用（资源去重，与覆盖率 covered 同键） ----
	snapRefs, err := s.refs.ListBySnapshotID(ctx, snap.ID.Hex())
	if err != nil {
		return ChangeList{}, fmt.Errorf("change: load snapshot references: %w", err)
	}
	refs := make([]domain.CertReference, 0, len(snapRefs))
	seen := make(map[string]struct{}, len(snapRefs))
	for _, r := range snapRefs {
		if r.CertFingerprint != oldCertFingerprint {
			continue
		}
		key := resourceDedupKey(r)
		if _, dup := seen[key]; dup {
			continue // 同资源多处引用聚合为一项（单次部署动作覆盖）
		}
		seen[key] = struct{}{}
		refs = append(refs, r)
	}

	// ---- 预生成订单落库（pending_confirm；条件写入 activeMutex，索引层兜底互斥） ----
	order := &domain.ChangeOrder{
		OldCertFingerprint: oldCertFingerprint,
		NewCertID:          newCertID,
		Status:             domain.ChangeStatusPendingConfirm,
		SnapshotID:         snap.ID.Hex(),
		ActiveMutex:        oldCertFingerprint,
	}
	orderID, err := s.orders.Create(ctx, order)
	if err != nil {
		if errors.Is(err, domain.ErrChangeInFlight) {
			// check-then-insert 竞态窗口由索引关闭：冲突同样映射 CHANGE_IN_FLIGHT
			return ChangeList{}, fmt.Errorf("%w: active change order already exists for fingerprint", domain.ErrChangeInFlight)
		}
		return ChangeList{}, fmt.Errorf("change: create order: %w", err)
	}

	// ---- 逐项生成（不可执行项分区 + 持久化） ----
	listItems, changeItems, unchangeable := s.buildChangeItems(ctx, orderID, refs)
	if len(changeItems) > 0 {
		if _, err := s.items.CreateMulti(ctx, changeItems); err != nil {
			return ChangeList{}, fmt.Errorf("change: create items: %w", err)
		}
	}

	return ChangeList{
		OrderID:          orderID,
		OldFingerprint:   oldCertFingerprint,
		NewCertID:        newCertID,
		SnapshotID:       snap.ID.Hex(),
		ScanFreshnessHrs: int(freshness.Hours()),
		Items:            listItems,
		SANCheck:         sanCheck,
		Warnings:         s.listWarnings(snap, refs, unchangeable),
	}, nil
}

// buildChangeItems 逐项构建清单项与持久化变更项：
//   - 云引用 → action=upload_and_bind，resourceRef 持久化
//     {channel,cloud,product,accountKey,resourceId}（5.7 子任务凭持久化数据
//     重构 DeployTarget，不回查台账/快照）；
//   - K8s 引用（product=crd）→ action=patch_crd，resourceRef 持久化
//     {channel,clusterId,namespace,kind,resourceId}；
//   - 不可执行项持久化即标 skipped + Error=Reason（不计入执行成功率分母，
//     5.7 仅执行 pending 项）；ID 预生成以保证清单项与持久化项一一对应。
func (s *changeService) buildChangeItems(ctx context.Context, orderID string, refs []domain.CertReference) (listItems []ChangeListItem, changeItems []domain.ChangeItem, unchangeable int) {
	listItems = make([]ChangeListItem, 0, len(refs))
	changeItems = make([]domain.ChangeItem, 0, len(refs))
	for _, r := range refs {
		var (
			target deployer.DeployTarget
			action domain.ChangeAction
		)
		if r.Product == domain.ProductCRD {
			target = deployer.DeployTarget{
				Channel:    string(deployer.ChannelTypeK8sAPI),
				ClusterID:  r.ClusterID,
				Namespace:  r.Namespace,
				Kind:       r.Kind,
				ResourceID: r.ResourceID,
			}
			action = domain.ActionPatchCRD
		} else {
			target = deployer.DeployTarget{
				Channel:    string(deployer.ChannelTypeCloudAPI),
				Cloud:      string(r.Cloud),
				Product:    string(r.Product),
				AccountKey: r.AccountKey,
				ResourceID: r.ResourceID,
			}
			action = domain.ActionUploadAndBind
		}
		changeable, reason := s.assessChangeable(ctx, target)
		if !changeable {
			unchangeable++
		}
		item := domain.ChangeItem{
			ID:             primitive.NewObjectID(),
			OrderID:        orderID,
			Action:         action,
			ResourceRef:    target.ToResourceRef(),
			OldCloudCertID: r.ReferencedCloudCertID,
			Status:         domain.ItemStatusPending,
		}
		if !changeable {
			item.Status = domain.ItemStatusSkipped
			item.Error = reason
		}
		changeItems = append(changeItems, item)
		listItems = append(listItems, ChangeListItem{
			ItemID:         item.ID.Hex(),
			Target:         target,
			Action:         action,
			AutoChangeable: changeable,
			Reason:         reason,
		})
	}
	return listItems, changeItems, unchangeable
}

// assessChangeable 单项可自动变更判定：
//   - 云通道：discovery-only 云（huawei/aws/azure）false + ERR_DISCOVERY_ONLY
//     （首期无部署器，PRD Out of Scope 三云部署器二期）；
//   - K8s 通道：ManagementProbe 三信号判定（5.6 实现）；探测通道未注入或探测
//     失败（如集群不可达）按不可执行项分区——单点不可用不阻塞清单生成
//     （PRD"单点失败不阻塞其他目标"），且不静默放行（Reason 显式声明）。
func (s *changeService) assessChangeable(ctx context.Context, target deployer.DeployTarget) (bool, string) {
	if target.Channel == string(deployer.ChannelTypeCloudAPI) {
		if _, discoveryOnly := discoveryOnlyClouds[domain.Cloud(target.Cloud)]; discoveryOnly {
			return false, fmt.Sprintf(reasonDiscoveryOnly, target.Cloud)
		}
		return true, ""
	}
	if s.probe == nil {
		return false, reasonK8sUnprobed
	}
	manageable, reason, err := s.probe.Probe(ctx, target.ToResourceRef())
	if err != nil {
		return false, fmt.Sprintf(reasonK8sProbeFailedFmt, err)
	}
	if !manageable {
		return false, fmt.Sprintf(reasonK8sNotManageableFmt, reason)
	}
	return true, ""
}

// listWarnings 盲区声明汇总（Hard Rule：不可执行项不静默放行；盲区显式声明）：
//   - 覆盖边界：首期不含 VM Nginx 配置级引用（恒定声明，PRD In Scope）；
//   - 扫描通道失败（snapshot.partialFailures）→ 该范围引用可能缺失的盲区提示
//     （"未发现引用"≠"无引用"）；
//   - coverageMeta total=-1 → "分母不可用"（K8s crd 恒 -1：asset 不盘点 K8s）；
//   - K8s 引用存在但探测通道未接入 → 未探测声明；
//   - 不可执行项汇总 → 原因与出路（不计入执行成功率分母）。
func (s *changeService) listWarnings(snap domain.ScanSnapshot, refs []domain.CertReference, unchangeable int) []string {
	warnings := []string{warningNginxBoundary}
	for _, pf := range snap.PartialFailures {
		warnings = append(warnings, fmt.Sprintf(warningBlindSpotFmt, pf.Cloud, pf.Product, pf.Account, pf.Reason))
	}
	for _, cm := range snap.CoverageMeta {
		if cm.Total == -1 {
			warnings = append(warnings, fmt.Sprintf(warningDenominatorFmt, cm.Cloud, cm.Product))
		}
	}
	hasK8s := false
	for _, r := range refs {
		if r.Product == domain.ProductCRD {
			hasK8s = true
			break
		}
	}
	if hasK8s && s.probe == nil {
		warnings = append(warnings, warningK8sUnprobed)
	}
	if unchangeable > 0 {
		warnings = append(warnings, fmt.Sprintf(warningPartitionFmt, unchangeable))
	}
	return warnings
}

// checkSansCover SAN 覆盖预检（纯函数）：新证书 SAN（大小写不敏感、去重）是否
// ⊇ 目标域名集合。Missing 保持目标域名次序、NewSANs 保持新证书 SAN 次序
// （提示性，不做拦截）；目标域名为空（旧证书无 SAN）时恒通过。
func checkSansCover(targetDomains, newSANs []string) SanCheckResult {
	hasSAN := make(map[string]struct{}, len(newSANs))
	for _, san := range newSANs {
		if key := normalizeDomain(san); key != "" {
			hasSAN[key] = struct{}{}
		}
	}
	targetSet := make(map[string]struct{}, len(targetDomains))
	result := SanCheckResult{Passed: true}
	for _, d := range targetDomains {
		key := normalizeDomain(d)
		if key == "" {
			continue
		}
		if _, dup := targetSet[key]; dup {
			continue
		}
		targetSet[key] = struct{}{}
		if _, ok := hasSAN[key]; !ok {
			result.Passed = false
			result.Missing = append(result.Missing, d)
		}
	}
	seenNew := make(map[string]struct{}, len(newSANs))
	for _, san := range newSANs {
		key := normalizeDomain(san)
		if key == "" {
			continue
		}
		if _, inTarget := targetSet[key]; inTarget {
			continue
		}
		if _, dup := seenNew[key]; dup {
			continue
		}
		seenNew[key] = struct{}{}
		result.NewSANs = append(result.NewSANs, san)
	}
	return result
}

// normalizeDomain 域名归一（小写+去空白；空值返回空串）。
func normalizeDomain(d string) string {
	return strings.ToLower(strings.TrimSpace(d))
}
