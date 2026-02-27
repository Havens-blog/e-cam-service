// Package domain 审计模块领域模型
package domain

// AuditOperationType 审计操作类型
type AuditOperationType string

const (
	// API 级别操作类型
	AuditOpAPIAccountCreate AuditOperationType = "api_account_create"
	AuditOpAPIAccountUpdate AuditOperationType = "api_account_update"
	AuditOpAPIAccountDelete AuditOperationType = "api_account_delete"
	AuditOpAPIAssetSync     AuditOperationType = "api_asset_sync"
	AuditOpAPITaskCreate    AuditOperationType = "api_task_create"
	AuditOpAPIAlertCreate   AuditOperationType = "api_alert_create"
	AuditOpAPIAlertUpdate   AuditOperationType = "api_alert_update"
	AuditOpAPIAlertDelete   AuditOperationType = "api_alert_delete"
	AuditOpAPIGeneric       AuditOperationType = "api_generic"
)

// AuditResult 审计结果
type AuditResult string

const (
	AuditResultSuccess AuditResult = "success"
	AuditResultFailed  AuditResult = "failed"
)

// AuditLog 审计日志领域模型
type AuditLog struct {
	ID            int64              `json:"id" bson:"id"`
	OperationType AuditOperationType `json:"operation_type" bson:"operation_type"`
	OperatorID    string             `json:"operator_id" bson:"operator_id"`
	OperatorName  string             `json:"operator_name" bson:"operator_name"`
	TenantID      string             `json:"tenant_id" bson:"tenant_id"`
	HTTPMethod    string             `json:"http_method" bson:"http_method"`
	APIPath       string             `json:"api_path" bson:"api_path"`
	RequestBody   string             `json:"request_body" bson:"request_body"`
	StatusCode    int                `json:"status_code" bson:"status_code"`
	Result        AuditResult        `json:"result" bson:"result"`
	RequestID     string             `json:"request_id" bson:"request_id"`
	DurationMs    int64              `json:"duration_ms" bson:"duration_ms"`
	ClientIP      string             `json:"client_ip" bson:"client_ip"`
	UserAgent     string             `json:"user_agent" bson:"user_agent"`
	Ctime         int64              `json:"ctime" bson:"ctime"`
}

// AuditLogFilter 审计日志查询过滤器
type AuditLogFilter struct {
	OperationType AuditOperationType `json:"operation_type"`
	OperatorID    string             `json:"operator_id"`
	TenantID      string             `json:"tenant_id"`
	HTTPMethod    string             `json:"http_method"`
	APIPath       string             `json:"api_path"` // 前缀匹配
	RequestID     string             `json:"request_id"`
	StatusCode    int                `json:"status_code"`
	StartTime     *int64             `json:"start_time"` // Unix 毫秒
	EndTime       *int64             `json:"end_time"`
	Offset        int64              `json:"offset"`
	Limit         int64              `json:"limit"`
}

// AuditReport 审计报告
type AuditReport struct {
	StartTime       int64                        `json:"start_time"`
	EndTime         int64                        `json:"end_time"`
	TotalOps        int64                        `json:"total_ops"`
	SuccessOps      int64                        `json:"success_ops"`
	FailedOps       int64                        `json:"failed_ops"`
	OpsByType       map[AuditOperationType]int64 `json:"ops_by_type"`
	OpsByMethod     map[string]int64             `json:"ops_by_method"`
	TopEndpoints    []EndpointStats              `json:"top_endpoints"`
	ErrorByEndpoint []EndpointStats              `json:"error_by_endpoint"`
	TopOperators    []OperatorStats              `json:"top_operators"`
	GeneratedAt     int64                        `json:"generated_at"`
}

// EndpointStats 端点统计
type EndpointStats struct {
	APIPath   string  `json:"api_path" bson:"_id"`
	Count     int64   `json:"count" bson:"count"`
	ErrorRate float64 `json:"error_rate" bson:"error_rate"`
}

// OperatorStats 操作人统计
type OperatorStats struct {
	OperatorID   string `json:"operator_id" bson:"_id"`
	OperatorName string `json:"operator_name" bson:"operator_name"`
	OpCount      int64  `json:"op_count" bson:"count"`
}
