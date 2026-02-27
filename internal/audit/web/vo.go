package web

// AuditReportReq 生成审计报告请求
type AuditReportReq struct {
	StartTime int64 `json:"start_time" binding:"required"`
	EndTime   int64 `json:"end_time" binding:"required"`
}
