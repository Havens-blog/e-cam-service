package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

// ---------------------------------------------------------------------
// 变更管理端点（任务 5.11，api-handbook 变更管理表）：
// 列表（状态 Tab 筛选）/ 生成清单 / 详情（含 ChangeList 结构，终态附
// ChangeReport 全字段）/ confirm / execute / confirm-batch / cancel /
// progress 逐项轮询 / rollback / audit 按单查询。
// handler 仅做参数绑定与错误映射；写路径逻辑在 5.1/5.2/5.7/5.8 service，
// 读路径聚合在 5.11 ChangeQueryService。异步执行失败不以 HTTP 错误返回
//（Hard Rule：走 progress itemStates 的 status 字段），仅同步路径错误映射
// 4xx/5xx（映射表见 response.go mapError）。
// ---------------------------------------------------------------------

// ChangeHandler 变更管理 handler。
type ChangeHandler struct {
	query    service.ChangeQueryService
	changes  service.ChangeService         // GenerateChangeList（5.2）/ Cancel（5.1）
	execute  service.ChangeExecuteService  // Confirm / Execute / ConfirmBatch（5.7）
	rollback service.ChangeRollbackService // Rollback（5.8）
	audit    service.ChangeAuditWriter     // 订单生命周期事件审计（7.2；nil=no-op）
}

// NewChangeHandler 创建变更管理 handler。audit 为 7.2 按单审计写入端口
// （生产装配 internal/audit 桥；nil=no-op）。
func NewChangeHandler(
	query service.ChangeQueryService,
	changes service.ChangeService,
	execute service.ChangeExecuteService,
	rollback service.ChangeRollbackService,
	audit service.ChangeAuditWriter,
) *ChangeHandler {
	return &ChangeHandler{query: query, changes: changes, execute: execute, rollback: rollback, audit: audit}
}

// RegisterRoutes 注册变更管理端点（角色门卫按 api-handbook Auth 列 +
// 任务 7.2 AC"运维主管/审计=变更查看"）：
//
//	GET    /changes                 列表（状态 Tab 筛选；工程师/主管/审计）
//	POST   /changes                 生成变更清单（四阻断 409；运维工程师）
//	GET    /changes/:id             详情/报告（ChangeList 结构；工程师/主管/审计）
//	POST   /changes/:id/confirm     确认执行（batchConf 越界 400；运维工程师）
//	POST   /changes/:id/execute     触发批量执行（运维工程师）
//	POST   /changes/:id/confirm-batch 人工续批（409 BATCH_NOT_CONFIRMABLE）
//	POST   /changes/:id/cancel      取消（409 CHANGE_NOT_CANCELLABLE）
//	GET    /changes/:id/progress    逐项进度轮询（运维工程师）
//	POST   /changes/:id/rollback    回滚成功项（409 ROLLBACK_TARGET_INVALID）
//	GET    /changes/:id/audit       按单查询审计流水（工程师/主管/审计，只读）
//
// 注意 Gin 通配顺序：/changes 静态段先于 ledger /:id 进入路由树（router.go
// 注册顺序保证）。
func (h *ChangeHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/changes", RequireRoles(RoleOpsEngineer, RoleOpsSupervisor, RoleAuditor), h.ListChanges)
	g.POST("/changes", RequireRoles(RoleOpsEngineer), h.GenerateList)
	g.GET("/changes/:id", RequireRoles(RoleOpsEngineer, RoleOpsSupervisor, RoleAuditor), h.GetChange)
	g.POST("/changes/:id/confirm", RequireRoles(RoleOpsEngineer), h.Confirm)
	g.POST("/changes/:id/execute", RequireRoles(RoleOpsEngineer), h.Execute)
	g.POST("/changes/:id/confirm-batch", RequireRoles(RoleOpsEngineer), h.ConfirmBatch)
	g.POST("/changes/:id/cancel", RequireRoles(RoleOpsEngineer), h.Cancel)
	g.GET("/changes/:id/progress", RequireRoles(RoleOpsEngineer), h.Progress)
	g.POST("/changes/:id/rollback", RequireRoles(RoleOpsEngineer), h.Rollback)
	g.GET("/changes/:id/audit", RequireRoles(RoleOpsEngineer, RoleOpsSupervisor, RoleAuditor), h.Audit)
}

// ---------------------------------------------------------------------
// 请求载荷
// ---------------------------------------------------------------------

// GenerateChangeRequest POST /changes 请求体（oldFingerprint+newCertId 必填）。
type GenerateChangeRequest struct {
	OldFingerprint string `json:"oldFingerprint"`
	NewCertID      string `json:"newCertId"`
}

// BatchConfVO 分批灰度配置（confirm 请求体；界值注释见 deployer.BatchConf）。
type BatchConfVO struct {
	Enabled       bool    `json:"enabled"`
	BatchSize     int     `json:"batchSize"`
	MaxBatchRatio float64 `json:"maxBatchRatio"`
}

// ConfirmRequest POST /changes/:id/confirm 请求体（batchConf 可省——省略时
// 按单批语义校验，仅 total<=1 的清单合法）。
type ConfirmRequest struct {
	BatchConf *BatchConfVO `json:"batchConf"`
}

// RollbackRequest POST /changes/:id/rollback 请求体（itemIds 必填非空）。
type RollbackRequest struct {
	ItemIDs []string `json:"itemIds"`
}

// ---------------------------------------------------------------------
// 响应载荷（Hard Rule：白名单字段，不含任何私钥/密文/凭证字段）
// ---------------------------------------------------------------------

// targetVO 目标资源定位（持久化 ResourceRef 的白名单投影）。
type targetVO struct {
	Channel    string `json:"channel"`
	Cloud      string `json:"cloud,omitempty"`
	Product    string `json:"product,omitempty"`
	AccountKey string `json:"accountKey,omitempty"`
	ClusterID  string `json:"clusterId,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Kind       string `json:"kind,omitempty"`
	ResourceID string `json:"resourceId"`
}

// changeListItemVO 清单项（生成清单响应；同构复用于详情 items 基本面）。
type changeListItemVO struct {
	ItemID         string   `json:"itemId"`
	Target         targetVO `json:"target"`
	Action         string   `json:"action"`
	AutoChangeable bool     `json:"autoChangeable"`
	Reason         string   `json:"reason,omitempty"`
}

// sanCheckVO SAN 预检结果。
type sanCheckVO struct {
	Passed  bool     `json:"passed"`
	Missing []string `json:"missing"`
	NewSANs []string `json:"newSans"`
}

// changeListVO 生成清单响应（tech-design ChangeList 全字段）。
type changeListVO struct {
	OrderID          string             `json:"orderId"`
	OldFingerprint   string             `json:"oldFingerprint"`
	NewCertID        string             `json:"newCertId"`
	SnapshotID       string             `json:"snapshotId"`
	ScanFreshnessHrs int                `json:"scanFreshnessHrs"`
	Items            []changeListItemVO `json:"items"`
	SANCheck         sanCheckVO         `json:"sanCheck"`
	Warnings         []string           `json:"warnings"`
}

// changeOrderListItemVO 列表项（订单基本面，报告经 GET /:id 获取）。
type changeOrderListItemVO struct {
	ID                string  `json:"id"`
	OldFingerprint    string  `json:"oldFingerprint"`
	NewCertID         string  `json:"newCertId"`
	Status            string  `json:"status"`
	SnapshotID        string  `json:"snapshotId"`
	CurrentBatch      int     `json:"currentBatch"`
	TotalBatches      int     `json:"totalBatches"`
	Paused            bool    `json:"paused"`
	VerifyWindowUntil *string `json:"verifyWindowUntil,omitempty"`
	Creator           string  `json:"creator"`
	CreatedAt         string  `json:"createdAt"`
}

// batchInfoVO 分批灰度信息。
type batchInfoVO struct {
	TotalBatches int     `json:"totalBatches"`
	CurrentBatch int     `json:"currentBatch"`
	BatchSize    int     `json:"batchSize"`
	Paused       bool    `json:"paused"`
	PausedAt     *string `json:"pausedAt,omitempty"`
}

// changeDetailItemVO 详情单项（ChangeList 项 + 执行态）。
type changeDetailItemVO struct {
	ItemID         string   `json:"itemId"`
	Target         targetVO `json:"target"`
	Action         string   `json:"action"`
	AutoChangeable bool     `json:"autoChangeable"`
	Reason         string   `json:"reason,omitempty"`
	BatchNo        int      `json:"batchNo"`
	Status         string   `json:"status"`
	Error          string   `json:"error,omitempty"`
}

// reportSummaryVO 报告汇总计数。
type reportSummaryVO struct {
	Total      int `json:"total"`
	Success    int `json:"success"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
	RolledBack int `json:"rolledBack"`
}

// reportItemVO 报告单项。
type reportItemVO struct {
	ItemID    string   `json:"itemId"`
	Target    targetVO `json:"target"`
	Status    string   `json:"status"`
	ErrCode   string   `json:"errCode,omitempty"`
	LatencyMs int64    `json:"latencyMs"`
}

// verifySummaryVO 验证窗口汇总（ChangeReport.Verify 全字段）。
type verifySummaryVO struct {
	WindowUntil  string `json:"windowUntil"`
	ExpectedNew  string `json:"expectedNew"`
	ProbePass    int    `json:"probePass"`
	ProbeDiff    int    `json:"probeDiff"`
	ProbeSkipped int    `json:"probeSkipped"`
	Unmet        int    `json:"unmet"`
}

// orphanCleanupVO 孤儿清理单项结果。
type orphanCleanupVO struct {
	Cloud       string `json:"cloud"`
	CloudCertID string `json:"cloudCertId"`
	Action      string `json:"action"`
	Success     bool   `json:"success"`
	At          string `json:"at"`
}

// changeReportVO ChangeReport 全字段（终态单 GET /:id 附带；GET /:id 报告面）。
type changeReportVO struct {
	OrderID       string            `json:"orderId"`
	Status        string            `json:"status"`
	Summary       reportSummaryVO   `json:"summary"`
	Items         []reportItemVO    `json:"items"`
	Verify        verifySummaryVO   `json:"verify"`
	OrphanCleanup []orphanCleanupVO `json:"orphanCleanup"`
	UnmetDomains  []string          `json:"unmetDomains"`
	FinishedAt    string            `json:"finishedAt"` // 零值输出空串（未完成）
}

// changeDetailVO 详情响应（订单要素 + ChangeList 结构 + 终态报告）。
type changeDetailVO struct {
	OrderID           string               `json:"orderId"`
	OldFingerprint    string               `json:"oldFingerprint"`
	NewCertID         string               `json:"newCertId"`
	SnapshotID        string               `json:"snapshotId"`
	ScanFreshnessHrs  int                  `json:"scanFreshnessHrs"`
	Status            string               `json:"status"`
	BatchInfo         *batchInfoVO         `json:"batchInfo,omitempty"`
	VerifyWindowUntil *string              `json:"verifyWindowUntil,omitempty"`
	ProtectUntil      *string              `json:"protectUntil,omitempty"`
	Creator           string               `json:"creator"`
	CreatedAt         string               `json:"createdAt"`
	Items             []changeDetailItemVO `json:"items"`
	Report            *changeReportVO      `json:"report,omitempty"` // 终态单非 nil
}

// progressItemVO 逐项进度状态（异步子任务状态载体）。
type progressItemVO struct {
	ItemID  string `json:"itemId"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	BatchNo int    `json:"batchNo"`
}

// changeProgressVO 逐项进度轮询响应。
type changeProgressVO struct {
	OrderID      string           `json:"orderId"`
	Status       string           `json:"status"`
	CurrentBatch int              `json:"currentBatch"`
	ItemStates   []progressItemVO `json:"itemStates"`
}

// auditLogVO 审计流水单条（api-handbook 变更审计契约）。
type auditLogVO struct {
	At     string `json:"at"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Detail string `json:"detail"`
	ItemID string `json:"itemId,omitempty"`
}

// changeAuditVO 按单审计查询响应。
type changeAuditVO struct {
	OrderID string       `json:"orderId"`
	Logs    []auditLogVO `json:"logs"`
}

// changeAckVO 写操作受理确认（进度/报告经专用端点轮询）。
type changeAckVO struct {
	OrderID string `json:"orderId"`
}

// ---------------------------------------------------------------------
// 端点实现
// ---------------------------------------------------------------------

// recordAudit 记录订单生命周期审计事件（7.2；操作成功受理后追加，
// create/confirm/execute/cancel 订单级事件，item_result/rollback/verify/
// orphan_cleanup 分别在执行引擎/5.8/5.10/5.9 服务内写入）。审计失败不改
// 变响应（操作结果优先，仅日志告警——同 5.8 端口契约，Hard Rule：仅追加）。
func (h *ChangeHandler) recordAudit(c *gin.Context, orderID, action, detail string) {
	if h.audit == nil {
		return
	}
	if err := h.audit.WriteChangeAudit(c.Request.Context(), service.ChangeAuditEvent{
		OrderID: orderID,
		Actor:   operator(c),
		Action:  action,
		Detail:  detail,
		At:      time.Now(),
	}); err != nil {
		elog.DefaultLogger.Warn("变更订单审计写入失败",
			elog.FieldErr(err),
			elog.String("order_id", orderID),
			elog.String("action", action),
		)
	}
}

// ListChanges GET /api/v1/certs/changes —— 变更单列表（状态 Tab 筛选）。
//
// Query 参数：
//
//	status  9 态之一（缺省=全部）
//	page    页码（1 起，缺省 1）
//	pageSize 每页条数（缺省 20，上限 100）
func (h *ChangeHandler) ListChanges(c *gin.Context) {
	status := domain.ChangeStatus(strings.TrimSpace(c.Query("status")))
	if status != "" && !isValidChangeStatus(status) {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"status must be one of draft, pending_confirm, executing, verifying, completed, partial_completed, rolled_back, rollback_failed, cancelled")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))
	res, err := h.query.ListOrders(c.Request.Context(), service.ListChangesQuery{
		Status:   status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		WriteError(c, err)
		return
	}
	items := make([]changeOrderListItemVO, 0, len(res.Items))
	for _, o := range res.Items {
		vo := changeOrderListItemVO{
			ID:                o.ID.Hex(),
			OldFingerprint:    o.OldCertFingerprint,
			NewCertID:         o.NewCertID,
			Status:            string(o.Status),
			SnapshotID:        o.SnapshotID,
			VerifyWindowUntil: formatTimePtr(o.VerifyWindowUntil),
			Creator:           o.Creator,
			CreatedAt:         formatTime(o.CreatedAt),
		}
		if o.BatchInfo != nil {
			vo.CurrentBatch = o.BatchInfo.CurrentBatch
			vo.TotalBatches = o.BatchInfo.TotalBatches
			vo.Paused = o.BatchInfo.Paused
		}
		items = append(items, vo)
	}
	WriteOK(c, http.StatusOK, items, pageMeta{Total: res.Total, Page: res.Page, PageSize: res.PageSize})
}

// GenerateList POST /api/v1/certs/changes —— 生成变更清单（oldFingerprint+
// newCertId；四项前置校验阻断映射 409：SCAN_STALE/SAN_INSUFFICIENT/
// CHANGE_IN_FLIGHT/NEW_CERT_FINGERPRINT_ONLY）。
func (h *ChangeHandler) GenerateList(c *gin.Context) {
	var req GenerateChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
		return
	}
	req.OldFingerprint = strings.TrimSpace(req.OldFingerprint)
	req.NewCertID = strings.TrimSpace(req.NewCertID)
	if req.OldFingerprint == "" || req.NewCertID == "" {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest,
			"oldFingerprint and newCertId are required")
		return
	}
	list, err := h.changes.GenerateChangeList(c.Request.Context(), req.OldFingerprint, req.NewCertID)
	if err != nil {
		WriteError(c, err)
		return
	}
	h.recordAudit(c, list.OrderID, service.AuditActionCreate,
		fmt.Sprintf("create change order old=%s new=%s items=%d", req.OldFingerprint, req.NewCertID, len(list.Items)))
	WriteOK(c, http.StatusCreated, toChangeListVO(list), nil)
}

// GetChange GET /api/v1/certs/changes/:id —— 详情（ChangeList 结构）；
// 终态单附带 ChangeReport 全字段。
func (h *ChangeHandler) GetChange(c *gin.Context) {
	d, err := h.query.GetDetail(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, toChangeDetailVO(d), nil)
}

// Confirm POST /api/v1/certs/changes/:id/confirm —— 确认执行（分批配置在此
// 固化批次分配；batchConf 越界 400 / MaxBatchRatio>0.5 拒绝）。
func (h *ChangeHandler) Confirm(c *gin.Context) {
	var req ConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
		return
	}
	var conf deployer.BatchConf
	if req.BatchConf != nil {
		conf = deployer.BatchConf{
			Enabled:       req.BatchConf.Enabled,
			BatchSize:     req.BatchConf.BatchSize,
			MaxBatchRatio: req.BatchConf.MaxBatchRatio,
		}
	}
	if err := h.execute.Confirm(c.Request.Context(), c.Param("id"), conf); err != nil {
		WriteError(c, err)
		return
	}
	h.recordAudit(c, c.Param("id"), service.AuditActionConfirm,
		fmt.Sprintf("confirm order batchConf enabled=%t batchSize=%d maxBatchRatio=%.2f",
			conf.Enabled, conf.BatchSize, conf.MaxBatchRatio))
	WriteOK(c, http.StatusOK, changeAckVO{OrderID: c.Param("id")}, nil)
}

// Execute POST /api/v1/certs/changes/:id/execute —— 触发批量执行（执行当前批
// batchNo=currentBatch 的项；逐项失败走 itemStates，不以 HTTP 错误返回）。
func (h *ChangeHandler) Execute(c *gin.Context) {
	if err := h.execute.Execute(c.Request.Context(), c.Param("id")); err != nil {
		WriteError(c, err)
		return
	}
	h.recordAudit(c, c.Param("id"), service.AuditActionExecute, "execute current batch")
	WriteOK(c, http.StatusOK, changeAckVO{OrderID: c.Param("id")}, nil)
}

// ConfirmBatch POST /api/v1/certs/changes/:id/confirm-batch —— 人工续批
// （门控不满足 409 BATCH_NOT_CONFIRMABLE）。
func (h *ChangeHandler) ConfirmBatch(c *gin.Context) {
	if err := h.execute.ConfirmBatch(c.Request.Context(), c.Param("id")); err != nil {
		WriteError(c, err)
		return
	}
	h.recordAudit(c, c.Param("id"), service.AuditActionConfirm, "confirm-batch resume next batch")
	WriteOK(c, http.StatusOK, changeAckVO{OrderID: c.Param("id")}, nil)
}

// Cancel POST /api/v1/certs/changes/:id/cancel —— 取消（verifying/终态
// 409 CHANGE_NOT_CANCELLABLE；executing 中止语义见 5.1 Cancel）。
func (h *ChangeHandler) Cancel(c *gin.Context) {
	if err := h.changes.Cancel(c.Request.Context(), c.Param("id")); err != nil {
		WriteError(c, err)
		return
	}
	h.recordAudit(c, c.Param("id"), service.AuditActionCancel, "cancel order")
	WriteOK(c, http.StatusOK, changeAckVO{OrderID: c.Param("id")}, nil)
}

// Progress GET /api/v1/certs/changes/:id/progress —— 逐项状态轮询
// （itemStates：pending/running/success/failed/rate_limited/rolled_back/
// skipped + error + 批次）。
func (h *ChangeHandler) Progress(c *gin.Context) {
	p, err := h.query.GetProgress(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, err)
		return
	}
	states := make([]progressItemVO, 0, len(p.ItemStates))
	for _, it := range p.ItemStates {
		states = append(states, progressItemVO{
			ItemID:  it.ItemID,
			Status:  string(it.Status),
			Error:   it.Error,
			BatchNo: it.BatchNo,
		})
	}
	WriteOK(c, http.StatusOK, changeProgressVO{
		OrderID:      p.OrderID,
		Status:       string(p.Status),
		CurrentBatch: p.CurrentBatch,
		ItemStates:   states,
	}, nil)
}

// Rollback POST /api/v1/certs/changes/:id/rollback —— 回滚成功项（仅
// status=success 的项参与；无效目标 409 ROLLBACK_TARGET_INVALID 转人工）。
func (h *ChangeHandler) Rollback(c *gin.Context) {
	var req RollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
		return
	}
	if len(req.ItemIDs) == 0 {
		WriteAPIError(c, http.StatusBadRequest, CodeInvalidRequest, "itemIds is required")
		return
	}
	if err := h.rollback.Rollback(c.Request.Context(), c.Param("id"), req.ItemIDs); err != nil {
		WriteError(c, err)
		return
	}
	WriteOK(c, http.StatusOK, changeAckVO{OrderID: c.Param("id")}, nil)
}

// Audit GET /api/v1/certs/changes/:id/audit —— 按变更单号查询审计流水
// （只读；Hard Rule：流水不得被修改/补写）。action 覆盖 create/confirm/
// execute/item_result/rollback/verify/orphan_cleanup，可与 ChangeReport
// 逐条比对。
func (h *ChangeHandler) Audit(c *gin.Context) {
	logs, err := h.query.ListAudit(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, err)
		return
	}
	out := make([]auditLogVO, 0, len(logs))
	for _, l := range logs {
		out = append(out, auditLogVO{
			At:     formatTime(l.At),
			Actor:  l.Actor,
			Action: l.Action,
			Detail: l.Detail,
			ItemID: l.ItemID,
		})
	}
	WriteOK(c, http.StatusOK, changeAuditVO{OrderID: c.Param("id"), Logs: out}, nil)
}

// ---------------------------------------------------------------------
// VO 映射
// ---------------------------------------------------------------------

// toTargetVO ResourceRef → 白名单目标投影。
func toTargetVO(ref domain.ResourceRef) targetVO {
	return targetVO{
		Channel:    string(ref.Channel),
		Cloud:      ref.Cloud,
		Product:    ref.Product,
		AccountKey: ref.AccountKey,
		ClusterID:  ref.ClusterID,
		Namespace:  ref.Namespace,
		Kind:       ref.Kind,
		ResourceID: ref.ResourceID,
	}
}

// toChangeListVO 生成清单 → 响应载荷（ChangeList 全字段）。
func toChangeListVO(list service.ChangeList) changeListVO {
	items := make([]changeListItemVO, 0, len(list.Items))
	for _, it := range list.Items {
		items = append(items, changeListItemVO{
			ItemID:         it.ItemID,
			Target:         toTargetVO(it.Target.ToResourceRef()),
			Action:         string(it.Action),
			AutoChangeable: it.AutoChangeable,
			Reason:         it.Reason,
		})
	}
	missing := list.SANCheck.Missing
	if missing == nil {
		missing = []string{}
	}
	newSANs := list.SANCheck.NewSANs
	if newSANs == nil {
		newSANs = []string{}
	}
	warnings := list.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return changeListVO{
		OrderID:          list.OrderID,
		OldFingerprint:   list.OldFingerprint,
		NewCertID:        list.NewCertID,
		SnapshotID:       list.SnapshotID,
		ScanFreshnessHrs: list.ScanFreshnessHrs,
		Items:            items,
		SANCheck: sanCheckVO{
			Passed:  list.SANCheck.Passed,
			Missing: missing,
			NewSANs: newSANs,
		},
		Warnings: warnings,
	}
}

// toChangeDetailVO 详情 → 响应载荷。
func toChangeDetailVO(d service.ChangeDetail) changeDetailVO {
	vo := changeDetailVO{
		OrderID:           d.OrderID,
		OldFingerprint:    d.OldFingerprint,
		NewCertID:         d.NewCertID,
		SnapshotID:        d.SnapshotID,
		ScanFreshnessHrs:  d.ScanFreshnessHrs,
		Status:            string(d.Status),
		VerifyWindowUntil: formatTimePtr(d.VerifyWindowUntil),
		ProtectUntil:      formatTimePtr(d.ProtectUntil),
		Creator:           d.Creator,
		CreatedAt:         formatTime(d.CreatedAt),
		Items:             make([]changeDetailItemVO, 0, len(d.Items)),
	}
	if d.BatchInfo != nil {
		vo.BatchInfo = &batchInfoVO{
			TotalBatches: d.BatchInfo.TotalBatches,
			CurrentBatch: d.BatchInfo.CurrentBatch,
			BatchSize:    d.BatchInfo.BatchSize,
			Paused:       d.BatchInfo.Paused,
			PausedAt:     formatTimePtr(d.BatchInfo.PausedAt),
		}
	}
	for _, it := range d.Items {
		vo.Items = append(vo.Items, changeDetailItemVO{
			ItemID:         it.ItemID,
			Target:         toTargetVO(it.Target),
			Action:         string(it.Action),
			AutoChangeable: it.AutoChangeable,
			Reason:         it.Reason,
			BatchNo:        it.BatchNo,
			Status:         string(it.Status),
			Error:          it.Error,
		})
	}
	if d.Report != nil {
		r := toChangeReportVO(*d.Report)
		vo.Report = &r
	}
	return vo
}

// toChangeReportVO 报告 → 响应载荷（ChangeReport 全字段投影）。
func toChangeReportVO(r service.ChangeReport) changeReportVO {
	items := make([]reportItemVO, 0, len(r.Items))
	for _, it := range r.Items {
		items = append(items, reportItemVO{
			ItemID:    it.ItemID,
			Target:    toTargetVO(it.Target),
			Status:    it.Status,
			ErrCode:   it.ErrCode,
			LatencyMs: it.LatencyMs,
		})
	}
	orphans := make([]orphanCleanupVO, 0, len(r.OrphanCleanup))
	for _, o := range r.OrphanCleanup {
		orphans = append(orphans, orphanCleanupVO{
			Cloud:       o.Cloud,
			CloudCertID: o.CloudCertID,
			Action:      o.Action,
			Success:     o.Success,
			At:          formatTime(o.At),
		})
	}
	unmet := r.UnmetDomains
	if unmet == nil {
		unmet = []string{}
	}
	return changeReportVO{
		OrderID: r.OrderID,
		Status:  r.Status,
		Summary: reportSummaryVO{
			Total:      r.Summary.Total,
			Success:    r.Summary.Success,
			Failed:     r.Summary.Failed,
			Skipped:    r.Summary.Skipped,
			RolledBack: r.Summary.RolledBack,
		},
		Items: items,
		Verify: verifySummaryVO{
			WindowUntil:  formatTime(r.Verify.WindowUntil),
			ExpectedNew:  r.Verify.ExpectedNew,
			ProbePass:    r.Verify.ProbePass,
			ProbeDiff:    r.Verify.ProbeDiff,
			ProbeSkipped: r.Verify.ProbeSkipped,
			Unmet:        r.Verify.Unmet,
		},
		OrphanCleanup: orphans,
		UnmetDomains:  unmet,
		FinishedAt:    formatTime(r.FinishedAt),
	}
}

// isValidChangeStatus 状态 Tab 合法值（9 态）。
func isValidChangeStatus(s domain.ChangeStatus) bool {
	switch s {
	case domain.ChangeStatusDraft, domain.ChangeStatusPendingConfirm,
		domain.ChangeStatusExecuting, domain.ChangeStatusVerifying,
		domain.ChangeStatusCompleted, domain.ChangeStatusPartialCompleted,
		domain.ChangeStatusRolledBack, domain.ChangeStatusRollbackFailed,
		domain.ChangeStatusCancelled:
		return true
	}
	return false
}
