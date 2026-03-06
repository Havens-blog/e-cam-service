package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
)

// BillDAO 账单数据访问接口
type BillDAO interface {
	// InsertRawBill 插入原始账单记录
	InsertRawBill(ctx context.Context, record domain.RawBillRecord) (int64, error)
	// InsertRawBills 批量插入原始账单记录
	InsertRawBills(ctx context.Context, records []domain.RawBillRecord) (int64, error)
	// GetRawBillByID 根据 ID 获取原始账单
	GetRawBillByID(ctx context.Context, id int64) (domain.RawBillRecord, error)
	// ListRawBills 按账号和日期范围查询原始账单
	ListRawBills(ctx context.Context, accountID int64, startDate, endDate string) ([]domain.RawBillRecord, error)
	// ListRawBillsByCollectID 按采集任务 ID 查询原始账单
	ListRawBillsByCollectID(ctx context.Context, collectID string) ([]domain.RawBillRecord, error)

	// InsertUnifiedBill 插入统一账单
	InsertUnifiedBill(ctx context.Context, bill domain.UnifiedBill) (int64, error)
	// InsertUnifiedBills 批量插入统一账单
	InsertUnifiedBills(ctx context.Context, bills []domain.UnifiedBill) (int64, error)
	// GetUnifiedBillByID 根据 ID 获取统一账单
	GetUnifiedBillByID(ctx context.Context, id int64) (domain.UnifiedBill, error)
	// ListUnifiedBills 按筛选条件查询统一账单
	ListUnifiedBills(ctx context.Context, filter UnifiedBillFilter) ([]domain.UnifiedBill, error)
	// CountUnifiedBills 统计统一账单数量
	CountUnifiedBills(ctx context.Context, filter UnifiedBillFilter) (int64, error)
	// AggregateByField 按指定字段聚合统一账单金额
	AggregateByField(ctx context.Context, tenantID string, field string, startDate, endDate string) ([]AggregateResult, error)
	// AggregateDailyAmount 按日聚合统一账单金额
	AggregateDailyAmount(ctx context.Context, tenantID string, startDate, endDate string, filter UnifiedBillFilter) ([]DailyAmount, error)
	// SumAmount 汇总指定筛选条件下的总金额
	SumAmount(ctx context.Context, filter UnifiedBillFilter) (float64, error)
	// DeleteUnifiedBillsByPeriod 按租户和账期删除统一账单（用于重新分摊）
	DeleteUnifiedBillsByPeriod(ctx context.Context, tenantID string, period string) error
	// DeleteRawBillsByAccountAndRange 按账号和日期范围删除原始账单（采集去重）
	DeleteRawBillsByAccountAndRange(ctx context.Context, accountID int64, startDate, endDate string) (int64, error)
	// DeleteUnifiedBillsByAccountAndRange 按账号和日期范围删除统一账单（采集去重）
	DeleteUnifiedBillsByAccountAndRange(ctx context.Context, accountID int64, startDate, endDate string) (int64, error)
	// AggregateByTag 按标签 key 聚合统一账单金额（展开 tags map）
	AggregateByTag(ctx context.Context, tenantID string, startDate, endDate string) ([]AggregateResult, error)
}

// UnifiedBillFilter 统一账单筛选条件
type UnifiedBillFilter struct {
	TenantID    string
	Provider    string
	AccountID   int64
	ServiceType string
	Region      string
	StartDate   string // YYYY-MM-DD
	EndDate     string // YYYY-MM-DD
	ResourceID  string
	Offset      int64
	Limit       int64
}

// AggregateResult 聚合结果
type AggregateResult struct {
	Key       string  `bson:"_id" json:"key"`
	Amount    float64 `bson:"amount" json:"amount"`
	AmountCNY float64 `bson:"amount_cny" json:"amount_cny"`
}

// DailyAmount 每日金额
type DailyAmount struct {
	Date      string  `bson:"_id" json:"date"`
	Amount    float64 `bson:"amount" json:"amount"`
	AmountCNY float64 `bson:"amount_cny" json:"amount_cny"`
}

// CollectLogDAO 采集日志数据访问接口
type CollectLogDAO interface {
	// Create 创建采集日志
	Create(ctx context.Context, log domain.CollectLog) (int64, error)
	// Update 更新采集日志
	Update(ctx context.Context, log domain.CollectLog) error
	// GetByID 根据 ID 获取采集日志
	GetByID(ctx context.Context, id int64) (domain.CollectLog, error)
	// GetLastSuccess 获取指定账号最近一次成功的采集日志
	GetLastSuccess(ctx context.Context, accountID int64) (domain.CollectLog, error)
	// GetLastFailed 获取指定账号最近一次失败的采集日志
	GetLastFailed(ctx context.Context, accountID int64) (domain.CollectLog, error)
	// List 按筛选条件查询采集日志
	List(ctx context.Context, filter CollectLogFilter) ([]domain.CollectLog, error)
	// Count 统计采集日志数量
	Count(ctx context.Context, filter CollectLogFilter) (int64, error)
}

// CollectLogFilter 采集日志筛选条件
type CollectLogFilter struct {
	TenantID  string
	AccountID int64
	Status    string
	Offset    int64
	Limit     int64
}

// BudgetDAO 预算规则数据访问接口
type BudgetDAO interface {
	// Create 创建预算规则
	Create(ctx context.Context, budget domain.BudgetRule) (int64, error)
	// Update 更新预算规则
	Update(ctx context.Context, budget domain.BudgetRule) error
	// GetByID 根据 ID 获取预算规则
	GetByID(ctx context.Context, id int64) (domain.BudgetRule, error)
	// List 按筛选条件查询预算规则
	List(ctx context.Context, filter BudgetFilter) ([]domain.BudgetRule, error)
	// Count 统计预算规则数量
	Count(ctx context.Context, filter BudgetFilter) (int64, error)
	// ListActive 获取所有启用的预算规则
	ListActive(ctx context.Context, tenantID string) ([]domain.BudgetRule, error)
	// UpdateStatus 更新预算状态
	UpdateStatus(ctx context.Context, id int64, status string) error
	// UpdateNotifiedAt 更新阈值通知时间
	UpdateNotifiedAt(ctx context.Context, id int64, notifiedAt map[string]time.Time) error
	// Delete 删除预算规则
	Delete(ctx context.Context, id int64) error
}

// BudgetFilter 预算规则筛选条件
type BudgetFilter struct {
	TenantID  string
	ScopeType string
	Status    string
	Offset    int64
	Limit     int64
}

// AllocationDAO 成本分摊数据访问接口
type AllocationDAO interface {
	// CreateRule 创建分摊规则
	CreateRule(ctx context.Context, rule domain.AllocationRule) (int64, error)
	// UpdateRule 更新分摊规则
	UpdateRule(ctx context.Context, rule domain.AllocationRule) error
	// GetRuleByID 根据 ID 获取分摊规则
	GetRuleByID(ctx context.Context, id int64) (domain.AllocationRule, error)
	// ListRules 按筛选条件查询分摊规则
	ListRules(ctx context.Context, filter AllocationRuleFilter) ([]domain.AllocationRule, error)
	// ListActiveRules 获取所有启用的分摊规则（按优先级排序）
	ListActiveRules(ctx context.Context, tenantID string) ([]domain.AllocationRule, error)
	// DeleteRule 删除分摊规则
	DeleteRule(ctx context.Context, id int64) error

	// SaveDefaultPolicy 保存默认分摊策略（upsert）
	SaveDefaultPolicy(ctx context.Context, policy domain.DefaultAllocationPolicy) error
	// GetDefaultPolicy 获取默认分摊策略
	GetDefaultPolicy(ctx context.Context, tenantID string) (domain.DefaultAllocationPolicy, error)

	// InsertAllocation 插入分摊结果
	InsertAllocation(ctx context.Context, alloc domain.CostAllocation) (int64, error)
	// InsertAllocations 批量插入分摊结果
	InsertAllocations(ctx context.Context, allocs []domain.CostAllocation) (int64, error)
	// DeleteAllocationsByPeriod 按租户和账期删除分摊结果（用于重新分摊）
	DeleteAllocationsByPeriod(ctx context.Context, tenantID string, period string) error
	// ListAllocations 按筛选条件查询分摊结果
	ListAllocations(ctx context.Context, filter AllocationFilter) ([]domain.CostAllocation, error)
	// GetAllocationByDimension 按维度查询分摊结果
	GetAllocationByDimension(ctx context.Context, tenantID, dimType, dimValue, period string) ([]domain.CostAllocation, error)
	// GetAllocationByNode 按服务树节点查询分摊结果
	GetAllocationByNode(ctx context.Context, tenantID string, nodeID int64, period string) ([]domain.CostAllocation, error)
}

// AllocationRuleFilter 分摊规则筛选条件
type AllocationRuleFilter struct {
	TenantID string
	RuleType string
	Status   string
	Offset   int64
	Limit    int64
}

// AllocationFilter 分摊结果筛选条件
type AllocationFilter struct {
	TenantID string
	DimType  string
	Period   string
	Offset   int64
	Limit    int64
}

// AnomalyDAO 异常事件数据访问接口
type AnomalyDAO interface {
	// Create 创建异常事件
	Create(ctx context.Context, anomaly domain.CostAnomaly) (int64, error)
	// CreateBatch 批量创建异常事件
	CreateBatch(ctx context.Context, anomalies []domain.CostAnomaly) (int64, error)
	// GetByID 根据 ID 获取异常事件
	GetByID(ctx context.Context, id int64) (domain.CostAnomaly, error)
	// List 按筛选条件查询异常事件
	List(ctx context.Context, filter AnomalyFilter) ([]domain.CostAnomaly, error)
	// Count 统计异常事件数量
	Count(ctx context.Context, filter AnomalyFilter) (int64, error)
}

// AnomalyFilter 异常事件筛选条件
type AnomalyFilter struct {
	TenantID  string
	Dimension string
	Severity  string
	StartDate string
	EndDate   string
	SortBy    string // severity / date
	Offset    int64
	Limit     int64
}

// OptimizerDAO 优化建议数据访问接口
type OptimizerDAO interface {
	// Create 创建优化建议
	Create(ctx context.Context, rec domain.Recommendation) (int64, error)
	// CreateBatch 批量创建优化建议
	CreateBatch(ctx context.Context, recs []domain.Recommendation) (int64, error)
	// GetByID 根据 ID 获取优化建议
	GetByID(ctx context.Context, id int64) (domain.Recommendation, error)
	// Update 更新优化建议
	Update(ctx context.Context, rec domain.Recommendation) error
	// List 按筛选条件查询优化建议
	List(ctx context.Context, filter RecommendationFilter) ([]domain.Recommendation, error)
	// Count 统计优化建议数量
	Count(ctx context.Context, filter RecommendationFilter) (int64, error)
	// FindByResourceAndType 查找指定资源和类型的建议（用于去重和忽略检查）
	FindByResourceAndType(ctx context.Context, tenantID, resourceID, recType string) (domain.Recommendation, error)
}

// RecommendationFilter 优化建议筛选条件
type RecommendationFilter struct {
	TenantID       string
	Type           string
	Provider       string
	Status         string
	ExcludeDismiss bool // 排除已忽略且未过期的建议
	Offset         int64
	Limit          int64
}
