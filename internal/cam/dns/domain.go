package dns

// DNSDomainVO 域名视图对象（聚合多云账号数据后的展示模型）
type DNSDomainVO struct {
	DomainName  string `json:"domain_name"`
	Provider    string `json:"provider"`
	AccountID   int64  `json:"account_id"`
	AccountName string `json:"account_name"`
	RecordCount int64  `json:"record_count"`
	Status      string `json:"status"`
	DomainID    string `json:"domain_id"`
}

// DNSRecordVO 解析记录视图对象
type DNSRecordVO struct {
	RecordID       string          `json:"record_id"`
	Domain         string          `json:"domain"`
	RR             string          `json:"rr"`
	Type           string          `json:"type"`
	Value          string          `json:"value"`
	TTL            int             `json:"ttl"`
	Priority       int             `json:"priority"`
	Line           string          `json:"line"`
	Status         string          `json:"status"`
	Provider       string          `json:"provider"`
	AccountID      int64           `json:"account_id"`
	LinkedResource *LinkedResource `json:"linked_resource,omitempty"`
}

// LinkedResource 关联资源信息（用于拓扑联动标签展示）
type LinkedResource struct {
	Type string `json:"type"`
	Name string `json:"name"`
	ID   string `json:"id"`
}

// DomainFilter 域名列表查询过滤
type DomainFilter struct {
	Keyword   string `json:"keyword"`
	Provider  string `json:"provider"`
	AccountID int64  `json:"account_id"`
	Offset    int64  `json:"offset"`
	Limit     int64  `json:"limit"`
}

// RecordFilter 解析记录列表查询过滤
type RecordFilter struct {
	Keyword    string `json:"keyword"`
	RecordType string `json:"record_type"`
	Offset     int64  `json:"offset"`
	Limit      int64  `json:"limit"`
}

// CreateRecordReq 创建解析记录请求
type CreateRecordReq struct {
	AccountID int64  `json:"account_id" binding:"required"`
	RR        string `json:"rr" binding:"required"`
	Type      string `json:"type" binding:"required"`
	Value     string `json:"value" binding:"required"`
	TTL       int    `json:"ttl"`
	Priority  int    `json:"priority"`
	Line      string `json:"line"`
}

// UpdateRecordReq 修改解析记录请求
type UpdateRecordReq struct {
	AccountID int64  `json:"account_id" binding:"required"`
	RR        string `json:"rr"`
	Type      string `json:"type"`
	Value     string `json:"value"`
	TTL       int    `json:"ttl"`
	Priority  int    `json:"priority"`
	Line      string `json:"line"`
}

// BatchDeleteResult 批量删除结果
type BatchDeleteResult struct {
	Total        int             `json:"total"`
	SuccessCount int             `json:"success_count"`
	FailedCount  int             `json:"failed_count"`
	Failures     []FailureDetail `json:"failures"`
}

// FailureDetail 失败明细
type FailureDetail struct {
	RecordID string `json:"record_id"`
	Error    string `json:"error"`
}

// DNSStats DNS 统计数据
type DNSStats struct {
	TotalDomains      int64            `json:"total_domains"`
	TotalRecords      int64            `json:"total_records"`
	ProviderDistrib   map[string]int64 `json:"provider_distribution"`
	RecordTypeDistrib map[string]int64 `json:"record_type_distribution"`
}

// ValidRecordTypes 合法的 DNS 记录类型
var ValidRecordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "MX": true,
	"TXT": true, "NS": true, "SRV": true, "CAA": true,
}
