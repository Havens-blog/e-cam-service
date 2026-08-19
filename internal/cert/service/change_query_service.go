package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ---------------------------------------------------------------------
// 变更管理查询端（任务 5.11）：列表（状态 Tab 筛选）/ 详情（含 ChangeList
// 结构）/ 报告（ChangeReport 全字段，tech-design Interface 3 GetReport）/
// 进度（逐项状态轮询）/ 审计按单查询。写路径（生成/确认/执行/续批/取消/
// 回滚）分别在 5.2 GenerateChangeList、5.7 Confirm/Execute/ConfirmBatch、
// 5.1 Cancel、5.8 Rollback；本服务只读聚合，不产生任何状态变更。
// ---------------------------------------------------------------------

// ChangeAuditLog 变更审计流水单条（api-handbook 变更审计契约：
// {at, actor, action, detail, itemId?}）。Action 覆盖
// create/confirm/execute/item_result/rollback/verify/orphan_cleanup
// （PRD Story 5：审计可与 ChangeReport 逐条比对）。
type ChangeAuditLog struct {
	At     time.Time // 事件时间（RFC3339 呈现）
	Actor  string    // 操作者（人工操作=EIAM 账号；系统事件=scheduler/executor 标识）
	Action string    // create|confirm|execute|item_result|rollback|verify|orphan_cleanup
	Detail string    // 机器可读详情（静态文案+安全参数，不含私钥/凭证片段）
	ItemID string    // 项级事件（item_result/rollback）必填；订单级事件为空
}

// ChangeAuditSource 变更审计流水读取端口（任务 5.11 查询端点契约；写入侧由
// 7.2 统一接线 internal/audit——5.7/5.8/5.10 的审计/结果记录端口在生产装配
// 时同步落地本端口的读实现）。只读查询，绝不补写/修改流水（Hard Rule）；
// nil=审计存储未接线（返回空流水，端点契约仍可用）。
type ChangeAuditSource interface {
	// ListByOrder 按变更单号查询审计流水（at 升序稳定返回）。
	ListByOrder(ctx context.Context, orderID string) ([]ChangeAuditLog, error)
}

// ChangeUnmetSource 窗口关闭未达标清单读取端口（5.10 VerifyWindowRecorder
// 写入侧的对称读端口，7.2 接线；nil=未接线返回空）。
type ChangeUnmetSource interface {
	// ListUnmetDomains 窗口关闭未达标域名清单（partial_completed 终局判定
	// 固化结果——查询期不重算，防窗口关闭后新探测漂移）。
	ListUnmetDomains(ctx context.Context, orderID string) ([]string, error)
}

// ChangeOrphanCleanupSource 孤儿清理结果读取端口（5.9 OrphanCleanupRecorder
// 写入侧的对称读端口，7.2 接线；nil=未接线返回空）。
type ChangeOrphanCleanupSource interface {
	// ListOrphanCleanup 按单查询孤儿清理结果（at 升序）。
	ListOrphanCleanup(ctx context.Context, orderID string) ([]OrphanCleanupResult, error)
}

// ChangeReport GetReport 返回的变更报告（tech-design Service-Level Types，
// 非持久化载荷：查询时按订单+变更项+verifyExpected+探测记录聚合）。
type ChangeReport struct {
	OrderID       string                // 变更单 ID
	Status        string                // 9 态状态机当前态
	Summary       ReportSummary         // 汇总计数
	Items         []ReportItem          // 与 ChangeItem 一一对应
	Verify        VerifySummary         // 验证窗口结果
	OrphanCleanup []OrphanCleanupResult // 孤儿证书补偿清理结果
	UnmetDomains  []string              // 窗口关闭未达标域名清单（partial_completed 时非空）
	FinishedAt    time.Time             // 全部批次完成时间；未完成为零值
}

// ReportSummary 报告汇总（口径：rollback_failed 计入 Failed——该项变更结果
// 未被接受且回滚未成，属失败遗留；pending/running/rate_limited 在途项不计入
// 任何终态计数）。
type ReportSummary struct {
	Total      int // 总项数
	Success    int // 成功
	Failed     int // 失败（含 rollback_failed）
	Skipped    int // 跳过（不可自动变更/人工取消）
	RolledBack int // 已回滚
}

// ReportItem 报告单项。LatencyMs/完成时点为查询期从持久化时间戳推导
// （ExecutedAt=领取时点 → HeartbeatAt=完成时点终值），非独立持久化字段。
type ReportItem struct {
	ItemID    string             // 变更项 ID
	Target    domain.ResourceRef // 目标（持久化完整 DeployTarget）
	Status    string             // pending|running|success|failed|rate_limited|rolled_back|skipped|rollback_failed
	ErrCode   string             // 失败时的错误码（ChangeItem.error 原文，"码: 详情"格式）
	LatencyMs int64              // 单项执行耗时（HeartbeatAt-ExecutedAt；未完成为 0）
}

// VerifySummary 验证窗口汇总。ProbePass/ProbeDiff/ProbeSkipped 为查询期按
// verifyExpected + 最近探测记录聚合（与 5.10 judgeWindow 同口径：通配符按
// wildcardProbeOverrides 替换、连续 verifyConfirmProbes 次一致=达标）。
type VerifySummary struct {
	WindowUntil  time.Time // 窗口截止（=verifyExpected.windowUntil）
	ExpectedNew  string    // 预期终态指纹（verifyExpected.newCertFingerprint 快照）
	ProbePass    int       // 窗口内探测达标域名数
	ProbeDiff    int       // 窗口内差异域名数（含 change_linked_diff）
	ProbeSkipped int       // 计 skipped 的验证项数（豁免 excludedDomains + 无 override 的通配符）
	Unmet        int       // 窗口关闭未达标域名数（与 ChangeReport.UnmetDomains 对应）
}

// ChangeDetail 变更单详情（GET /changes/:id）：订单要素 + ChangeList 结构
// （items 与生成期清单一一对应，AutoChangeable 自持久化状态推导）+ 终态时
// 附 ChangeReport 全字段。
type ChangeDetail struct {
	OrderID           string
	OldFingerprint    string
	NewCertID         string
	SnapshotID        string
	ScanFreshnessHrs  int // 绑定快照的新鲜度（快照缺失为 -1）
	Status            domain.ChangeStatus
	BatchInfo         *domain.BatchInfo
	VerifyWindowUntil *time.Time
	ProtectUntil      *time.Time
	Creator           string
	CreatedAt         time.Time
	Items             []ChangeDetailItem
	Report            *ChangeReport // 终态单非 nil
}

// ChangeDetailItem 详情单项：清单项（target/action/autoChangeable/reason）+
// 执行态（batchNo/status/error）。AutoChangeable 推导：生成期不可执行项
// 持久化即标 skipped 且 Error=Reason（5.2 口径），据此反推。
type ChangeDetailItem struct {
	ItemID         string
	Target         domain.ResourceRef
	Action         domain.ChangeAction
	AutoChangeable bool
	Reason         string // AutoChangeable=false 时的判定依据（生成期 Error 原文）
	BatchNo        int
	Status         domain.ChangeItemStatus
	Error          string
}

// ChangeProgress 逐项进度轮询载荷（GET /changes/:id/progress）。
type ChangeProgress struct {
	OrderID      string
	Status       domain.ChangeStatus
	CurrentBatch int // 分批单=当前批号；未分批为 0
	ItemStates   []ProgressItem
}

// ProgressItem 单项执行状态（异步子任务状态载体，非 HTTP 错误口径）。
type ProgressItem struct {
	ItemID  string
	Status  domain.ChangeItemStatus // pending/running/success/failed/rate_limited/rolled_back/skipped
	Error   string                  // 失败/跳过原因（安全文案，"码: 详情"格式）
	BatchNo int
}

// ChangeOrderListResult 变更单列表分页结果。
type ChangeOrderListResult struct {
	Items    []domain.ChangeOrder
	Total    int64
	Page     int
	PageSize int
}

// ListChangesQuery 列表查询入参（状态 Tab 筛选；分页界值由服务归一）。
type ListChangesQuery struct {
	Status   domain.ChangeStatus // 空=全部
	Page     int                 // <1 归一为 1
	PageSize int                 // <1 归一为 20，>100 截断为 100
}

// 分页界值常量（与台账列表同口径）。
const (
	changeListDefaultPageSize = 20
	changeListMaxPageSize     = 100
)

// ChangeQueryService 变更管理只读查询服务（任务 5.11）。
type ChangeQueryService interface {
	// ListOrders 变更单列表（状态 Tab 筛选 + 分页，createdAt 降序）。
	ListOrders(ctx context.Context, q ListChangesQuery) (ChangeOrderListResult, error)
	// GetDetail 变更单详情（含 ChangeList 结构；终态单附 ChangeReport 全字段）。
	GetDetail(ctx context.Context, orderID string) (ChangeDetail, error)
	// GetReport 变更报告（tech-design Interface 3 GetReport，全字段）。
	GetReport(ctx context.Context, orderID string) (ChangeReport, error)
	// GetProgress 逐项进度（progress 逐项轮询端点数据源）。
	GetProgress(ctx context.Context, orderID string) (ChangeProgress, error)
	// ListAudit 按变更单号查询审计流水（只读，Hard Rule：不得修改/补写）。
	ListAudit(ctx context.Context, orderID string) ([]ChangeAuditLog, error)
}

type changeQueryService struct {
	orders    domain.ChangeOrderRepository
	items     domain.ChangeItemRepository
	snapshots domain.ScanSnapshotRepository
	probes    domain.ProbeResultRepository
	alertCfg  domain.AlertConfigRepository

	unmet  ChangeUnmetSource         // 5.10 写入侧对称读端口；nil=空
	orphan ChangeOrphanCleanupSource // 5.9 写入侧对称读端口；nil=空
	audit  ChangeAuditSource         // 7.2 接线；nil=空流水
}

// NewChangeQueryService 创建变更管理查询服务。unmet/orphan/audit 为
// nil 时对应报告字段/审计流水返回空（生产装配 7.2 统一接线）。
func NewChangeQueryService(
	orders domain.ChangeOrderRepository,
	items domain.ChangeItemRepository,
	snapshots domain.ScanSnapshotRepository,
	probes domain.ProbeResultRepository,
	alertCfg domain.AlertConfigRepository,
	unmet ChangeUnmetSource,
	orphan ChangeOrphanCleanupSource,
	audit ChangeAuditSource,
) ChangeQueryService {
	return &changeQueryService{
		orders:    orders,
		items:     items,
		snapshots: snapshots,
		probes:    probes,
		alertCfg:  alertCfg,
		unmet:     unmet,
		orphan:    orphan,
		audit:     audit,
	}
}

// ListOrders 变更单列表（分页归一：page>=1、pageSize∈[1,100]）。
func (s *changeQueryService) ListOrders(ctx context.Context, q ListChangesQuery) (ChangeOrderListResult, error) {
	page, pageSize := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = changeListDefaultPageSize
	}
	if pageSize > changeListMaxPageSize {
		pageSize = changeListMaxPageSize
	}
	orders, total, err := s.orders.ListPage(ctx, q.Status, (page-1)*pageSize, pageSize)
	if err != nil {
		return ChangeOrderListResult{}, fmt.Errorf("change query: list orders: %w", err)
	}
	return ChangeOrderListResult{Items: orders, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetDetail 变更单详情：订单要素 + 清单项（ChangeList 结构）+ 终态报告。
func (s *changeQueryService) GetDetail(ctx context.Context, orderID string) (ChangeDetail, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return ChangeDetail{}, fmt.Errorf("change query: get order: %w", err)
	}
	items, err := s.items.ListByOrder(ctx, orderID)
	if err != nil {
		return ChangeDetail{}, fmt.Errorf("change query: list items: %w", err)
	}
	freshness := -1
	if snap, err := s.snapshots.GetByID(ctx, order.SnapshotID); err == nil {
		freshness = int(time.Since(snap.StartedAt).Hours())
	} // 快照缺失/非法 ID 不阻塞详情（新鲜度退化为 -1 未知）

	detail := ChangeDetail{
		OrderID:           order.ID.Hex(),
		OldFingerprint:    order.OldCertFingerprint,
		NewCertID:         order.NewCertID,
		SnapshotID:        order.SnapshotID,
		ScanFreshnessHrs:  freshness,
		Status:            order.Status,
		BatchInfo:         order.BatchInfo,
		VerifyWindowUntil: order.VerifyWindowUntil,
		ProtectUntil:      order.ProtectUntil,
		Creator:           order.Creator,
		CreatedAt:         order.CreatedAt,
		Items:             make([]ChangeDetailItem, 0, len(items)),
	}
	for _, it := range items {
		// 生成期不可执行项：持久化即 skipped + Error=Reason（5.2）；人工取消
		// 的 skipped 项无 Error——autoChangeable 保持 true（可执行但被取消）。
		reason := ""
		auto := !(it.Status == domain.ItemStatusSkipped && it.Error != "")
		if !auto {
			reason = it.Error
		}
		detail.Items = append(detail.Items, ChangeDetailItem{
			ItemID:         it.ID.Hex(),
			Target:         it.ResourceRef,
			Action:         it.Action,
			AutoChangeable: auto,
			Reason:         reason,
			BatchNo:        it.BatchNo,
			Status:         it.Status,
			Error:          it.Error,
		})
	}
	if domain.IsTerminalChangeStatus(order.Status) {
		// 终态单附带 ChangeReport 全字段（Interface 3 GetReport 单一聚合入口；
		// 只读路径，重复取单无一致性风险）。
		report, err := s.GetReport(ctx, orderID)
		if err != nil {
			return ChangeDetail{}, err
		}
		detail.Report = &report
	}
	return detail, nil
}

// GetReport 变更报告（全字段聚合）。
func (s *changeQueryService) GetReport(ctx context.Context, orderID string) (ChangeReport, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return ChangeReport{}, fmt.Errorf("change query: get order: %w", err)
	}
	items, err := s.items.ListByOrder(ctx, orderID)
	if err != nil {
		return ChangeReport{}, fmt.Errorf("change query: list items: %w", err)
	}
	return s.buildReport(ctx, order, items)
}

// buildReport 报告聚合（订单 + 变更项 + verifyExpected + 探测 + 报告存档）。
func (s *changeQueryService) buildReport(ctx context.Context, order domain.ChangeOrder, items []domain.ChangeItem) (ChangeReport, error) {
	report := ChangeReport{
		OrderID:       order.ID.Hex(),
		Status:        string(order.Status),
		Summary:       summarizeItems(items),
		Items:         make([]ReportItem, 0, len(items)),
		OrphanCleanup: s.listOrphanCleanup(ctx, order.ID.Hex()),
		UnmetDomains:  []string{},
	}
	for _, it := range items {
		report.Items = append(report.Items, ReportItem{
			ItemID:    it.ID.Hex(),
			Target:    it.ResourceRef,
			Status:    string(it.Status),
			ErrCode:   it.Error,
			LatencyMs: itemLatencyMs(it),
		})
	}
	if order.VerifyExpected != nil {
		tally, err := s.summarizeVerify(ctx, order)
		if err != nil {
			return ChangeReport{}, err
		}
		report.Verify = tally.summary
		report.UnmetDomains = tally.unmet
		// 未达标清单权威来源：partial_completed 终局判定固化的存档（5.10
		// recorder 落库，防窗口关闭后探测漂移）；未接线时退化为查询期推导。
		if order.Status == domain.ChangeStatusPartialCompleted && s.unmet != nil {
			if persisted, err := s.unmet.ListUnmetDomains(ctx, order.ID.Hex()); err == nil && len(persisted) > 0 {
				report.UnmetDomains = persisted
			}
		}
		report.Verify.Unmet = len(report.UnmetDomains)
	}
	report.FinishedAt = finishedAt(items)
	return report, nil
}

// summarizeItems 汇总计数（rollback_failed 计入 Failed；在途项不计入）。
func summarizeItems(items []domain.ChangeItem) ReportSummary {
	sum := ReportSummary{Total: len(items)}
	for _, it := range items {
		switch it.Status {
		case domain.ItemStatusSuccess:
			sum.Success++
		case domain.ItemStatusFailed, domain.ItemStatusRollbackFailed:
			sum.Failed++
		case domain.ItemStatusSkipped:
			sum.Skipped++
		case domain.ItemStatusRolledBack:
			sum.RolledBack++
		}
	}
	return sum
}

// itemLatencyMs 单项执行耗时：HeartbeatAt（完成时点终值）- ExecutedAt
// （领取时点）；任一缺失或非正差值为 0。
func itemLatencyMs(it domain.ChangeItem) int64 {
	if it.ExecutedAt == nil || it.HeartbeatAt == nil {
		return 0
	}
	if d := it.HeartbeatAt.Sub(*it.ExecutedAt); d > 0 {
		return d.Milliseconds()
	}
	return 0
}

// finishedAt 完成时点推导：全部变更项完成时点（HeartbeatAt 终值 /
// RecheckedAt 复检时点）的最大值；无完成项为零值（语义同
// "未完成为零值"，含取消单未写完成时点的项）。
func finishedAt(items []domain.ChangeItem) time.Time {
	var latest time.Time
	for _, it := range items {
		switch {
		case it.HeartbeatAt != nil && it.HeartbeatAt.After(latest):
			latest = *it.HeartbeatAt
		case it.RecheckedAt != nil && it.RecheckedAt.After(latest):
			latest = *it.RecheckedAt
		}
	}
	return latest
}

// verifyTally 验证窗口计数与未达标清单（查询期推导的中间载体）。
type verifyTally struct {
	summary VerifySummary
	unmet   []string
}

// summarizeVerify 验证窗口汇总（与 5.10 judgeWindow 同口径的查询期推导）：
// 通配符按 wildcardProbeOverrides 替换、无 override 计 skipped；达标 = 最近
// 连续 verifyConfirmProbes 次线上指纹=预期指纹；ProbeDiff = 未达标且有探测
// 记录的域名（含 change_linked_diff 形态差异）。unmet 保持
// verifyExpected.Domains 次序（无探测记录与差异域名均计未达标，同终局判定
// 的安全侧口径）。
func (s *changeQueryService) summarizeVerify(ctx context.Context, order domain.ChangeOrder) (verifyTally, error) {
	expected := order.VerifyExpected
	tally := verifyTally{
		summary: VerifySummary{
			ExpectedNew: expected.NewCertFingerprint,
			WindowUntil: expected.WindowUntil,
		},
		unmet: []string{},
	}
	cfg, err := s.alertCfg.Get(ctx)
	if err != nil {
		return tally, fmt.Errorf("change query: get alert config: %w", err)
	}
	tally.summary.ProbeSkipped += len(expected.ExcludedDomains) // 豁免域名构建期剔除，计 skipped

	// 判定目标解析与 streak 口径同 judgeWindow（resolveVerifyTargets）。
	targets, skipped, confirmProbes, err := resolveVerifyTargets(ctx, s.probes, expected, cfg)
	if err != nil {
		return tally, fmt.Errorf("change query: %w", err)
	}
	tally.summary.ProbeSkipped += len(skipped) // 无 override 通配符计 skipped
	if len(targets) == 0 {
		return tally, nil // 全部 skipped：窗口不构成差异
	}
	for _, t := range targets {
		switch {
		case t.streak >= confirmProbes:
			tally.summary.ProbePass++
		case len(t.results) > 0:
			tally.summary.ProbeDiff++ // 有记录但不一致（含 change_linked_diff）
			tally.unmet = append(tally.unmet, t.domain)
		default:
			tally.unmet = append(tally.unmet, t.domain) // 记录不足阈值次数：不可判定=未达标
		}
	}
	return tally, nil
}

// listOrphanCleanup 孤儿清理结果（未接线/无记录返回空切片）。
func (s *changeQueryService) listOrphanCleanup(ctx context.Context, orderID string) []OrphanCleanupResult {
	if s.orphan == nil {
		return []OrphanCleanupResult{}
	}
	results, err := s.orphan.ListOrphanCleanup(ctx, orderID)
	if err != nil || len(results) == 0 {
		return []OrphanCleanupResult{}
	}
	return results
}

// GetProgress 逐项进度（订单状态 + 当前批 + 全部项状态）。
func (s *changeQueryService) GetProgress(ctx context.Context, orderID string) (ChangeProgress, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return ChangeProgress{}, fmt.Errorf("change query: get order: %w", err)
	}
	items, err := s.items.ListByOrder(ctx, orderID)
	if err != nil {
		return ChangeProgress{}, fmt.Errorf("change query: list items: %w", err)
	}
	progress := ChangeProgress{
		OrderID:    order.ID.Hex(),
		Status:     order.Status,
		ItemStates: make([]ProgressItem, 0, len(items)),
	}
	if order.BatchInfo != nil {
		progress.CurrentBatch = order.BatchInfo.CurrentBatch
	}
	for _, it := range items {
		progress.ItemStates = append(progress.ItemStates, ProgressItem{
			ItemID:  it.ID.Hex(),
			Status:  it.Status,
			Error:   it.Error,
			BatchNo: it.BatchNo,
		})
	}
	return progress, nil
}

// ListAudit 按变更单号查询审计流水（只读；未接线返回空流水）。
func (s *changeQueryService) ListAudit(ctx context.Context, orderID string) ([]ChangeAuditLog, error) {
	if _, err := s.orders.GetByID(ctx, orderID); err != nil {
		return nil, fmt.Errorf("change query: get order: %w", err)
	}
	if s.audit == nil {
		return []ChangeAuditLog{}, nil
	}
	logs, err := s.audit.ListByOrder(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("change query: list audit logs: %w", err)
	}
	return logs, nil
}
