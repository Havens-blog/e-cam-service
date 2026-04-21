package tag

import "github.com/Havens-blog/e-cam-service/internal/cam/errs"

// 标签管理相关错误码
var (
	ErrTagKeyEmpty         = errs.ErrorCode{Code: 400030, Msg: "tag key cannot be empty"}
	ErrTagValueEmpty       = errs.ErrorCode{Code: 400031, Msg: "tag value cannot be empty"}
	ErrPolicyNameEmpty     = errs.ErrorCode{Code: 400032, Msg: "policy name cannot be empty"}
	ErrPolicyKeysEmpty     = errs.ErrorCode{Code: 400033, Msg: "required keys list cannot be empty"}
	ErrPolicyNotFound      = errs.ErrorCode{Code: 404030, Msg: "tag policy not found"}
	ErrCloudTagAPIFailed   = errs.ErrorCode{Code: 500030, Msg: "cloud tag API call failed"}
	ErrPartialTagOperation = errs.ErrorCode{Code: 500031, Msg: "partial tag operation failure"}
)

// TagPolicy 标签策略
type TagPolicy struct {
	ID                  int64               `bson:"id" json:"id"`
	Name                string              `bson:"name" json:"name"`
	Description         string              `bson:"description" json:"description"`
	RequiredKeys        []string            `bson:"required_keys" json:"required_keys"`
	KeyValueConstraints map[string][]string `bson:"key_value_constraints" json:"key_value_constraints"`
	ResourceTypes       []string            `bson:"resource_types" json:"resource_types"`
	Status              string              `bson:"status" json:"status"` // enabled / disabled
	TenantID            string              `bson:"tenant_id" json:"tenant_id"`
	Ctime               int64               `bson:"ctime" json:"created_at"`
	Utime               int64               `bson:"utime" json:"updated_at"`
}

// TagSummary 标签聚合结果（非持久化）
type TagSummary struct {
	Key           string   `json:"key"`
	Value         string   `json:"value"`
	ResourceCount int64    `json:"resource_count"`
	Providers     []string `json:"providers"`
}

// TagStats 标签统计（非持久化）
type TagStats struct {
	TotalKeys       int64   `json:"total_keys"`
	TotalValues     int64   `json:"total_values"`
	TaggedResources int64   `json:"tagged_resources"`
	TotalResources  int64   `json:"total_resources"`
	CoveragePercent float64 `json:"coverage_percent"`
}

// TagFilter 标签列表查询过滤
type TagFilter struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	Provider     string `json:"provider"`
	AccountID    int64  `json:"account_id"`
	ResourceType string `json:"resource_type"`
	Offset       int64  `json:"offset"`
	Limit        int64  `json:"limit"`
}

// TagResourceFilter 标签关联资源查询过滤
type TagResourceFilter struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	Provider     string `json:"provider"`
	ResourceType string `json:"resource_type"`
	Offset       int64  `json:"offset"`
	Limit        int64  `json:"limit"`
}

// ResourceRef 资源引用
type ResourceRef struct {
	AccountID    int64  `json:"account_id"`
	Region       string `json:"region"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

// BindTagsReq 绑定标签请求
type BindTagsReq struct {
	Resources []ResourceRef     `json:"resources"`
	Tags      map[string]string `json:"tags"`
}

// UnbindTagsReq 解绑标签请求
type UnbindTagsReq struct {
	Resources []ResourceRef `json:"resources"`
	TagKeys   []string      `json:"tag_keys"`
}

// BatchResult 批量操作结果
type BatchResult struct {
	Total        int             `json:"total"`
	SuccessCount int             `json:"success_count"`
	FailedCount  int             `json:"failed_count"`
	Failures     []FailureDetail `json:"failures"`
}

// FailureDetail 失败明细
type FailureDetail struct {
	ResourceID string `json:"resource_id"`
	Error      string `json:"error"`
}

// ComplianceResult 合规检查结果
type ComplianceResult struct {
	AssetID      string      `json:"asset_id"`
	AssetName    string      `json:"asset_name"`
	ResourceType string      `json:"resource_type"`
	Provider     string      `json:"provider"`
	Region       string      `json:"region"`
	AccountID    int64       `json:"account_id"`
	Violations   []Violation `json:"violations"`
}

// Violation 违规项
type Violation struct {
	Type    string   `json:"type"` // missing_key / invalid_value
	Key     string   `json:"key"`
	Value   string   `json:"value,omitempty"`
	Allowed []string `json:"allowed,omitempty"`
}

// CreatePolicyReq 创建策略请求
type CreatePolicyReq struct {
	Name                string              `json:"name"`
	Description         string              `json:"description"`
	RequiredKeys        []string            `json:"required_keys"`
	KeyValueConstraints map[string][]string `json:"key_value_constraints"`
	ResourceTypes       []string            `json:"resource_types"`
}

// UpdatePolicyReq 更新策略请求
type UpdatePolicyReq struct {
	Name                *string              `json:"name"`
	Description         *string              `json:"description"`
	RequiredKeys        *[]string            `json:"required_keys"`
	KeyValueConstraints *map[string][]string `json:"key_value_constraints"`
	ResourceTypes       *[]string            `json:"resource_types"`
	Status              *string              `json:"status"`
}

// PolicyFilter 策略列表查询过滤
type PolicyFilter struct {
	TenantID string `json:"tenant_id"`
	Offset   int64  `json:"offset"`
	Limit    int64  `json:"limit"`
}

// ComplianceFilter 合规检查过滤
type ComplianceFilter struct {
	PolicyID     int64  `json:"policy_id"`
	ResourceType string `json:"resource_type"`
	Provider     string `json:"provider"`
	Offset       int64  `json:"offset"`
	Limit        int64  `json:"limit"`
}

// TagResource 标签关联资源
type TagResource struct {
	AssetID      string `json:"asset_id"`
	AssetName    string `json:"asset_name"`
	ResourceType string `json:"resource_type"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
	AccountID    int64  `json:"account_id"`
	AccountName  string `json:"account_name"`
}

// ==================== 自动打标规则 ====================

// TagRule 自动打标规则
type TagRule struct {
	ID          int64             `bson:"id" json:"id"`
	Name        string            `bson:"name" json:"name"`
	Description string            `bson:"description" json:"description"`
	Logic       string            `bson:"logic" json:"logic"` // "and" | "or"
	Conditions  []RuleCondition   `bson:"conditions" json:"conditions"`
	Tags        map[string]string `bson:"tags" json:"tags"` // 命中后打的标签
	Priority    int               `bson:"priority" json:"priority"`
	Status      string            `bson:"status" json:"status"` // enabled / disabled
	TenantID    string            `bson:"tenant_id" json:"tenant_id"`
	Ctime       int64             `bson:"ctime" json:"created_at"`
	Utime       int64             `bson:"utime" json:"updated_at"`
}

// RuleCondition 规则匹配条件
type RuleCondition struct {
	Field    string `bson:"field" json:"field"`       // asset_name, asset_id, model_uid, provider, region, account_name
	Operator string `bson:"operator" json:"operator"` // contains, equals, prefix, suffix, regex
	Value    string `bson:"value" json:"value"`
}

// CreateRuleReq 创建规则请求
type CreateRuleReq struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Logic       string            `json:"logic"`
	Conditions  []RuleCondition   `json:"conditions"`
	Tags        map[string]string `json:"tags"`
	Priority    int               `json:"priority"`
}

// UpdateRuleReq 更新规则请求
type UpdateRuleReq struct {
	Name        *string            `json:"name"`
	Description *string            `json:"description"`
	Logic       *string            `json:"logic"`
	Conditions  *[]RuleCondition   `json:"conditions"`
	Tags        *map[string]string `json:"tags"`
	Priority    *int               `json:"priority"`
	Status      *string            `json:"status"`
}

// RuleFilter 规则列表查询过滤
type RuleFilter struct {
	TenantID string `json:"tenant_id"`
	Offset   int64  `json:"offset"`
	Limit    int64  `json:"limit"`
}

// RulePreviewResult 规则预览结果
type RulePreviewResult struct {
	RuleID     int64             `json:"rule_id"`
	RuleName   string            `json:"rule_name"`
	MatchCount int64             `json:"match_count"`
	Resources  []PreviewResource `json:"resources"`
}

// PreviewResource 预览匹配的资源摘要
type PreviewResource struct {
	AssetID      string `json:"asset_id"`
	AssetName    string `json:"asset_name"`
	ResourceType string `json:"resource_type"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
}

// RuleExecuteResult 规则执行结果
type RuleExecuteResult struct {
	RuleID       int64  `json:"rule_id"`
	RuleName     string `json:"rule_name"`
	MatchCount   int64  `json:"match_count"`
	SuccessCount int    `json:"success_count"`
	FailedCount  int    `json:"failed_count"`
}

// 自动打标规则错误码
var (
	ErrRuleNameEmpty   = errs.ErrorCode{Code: 400040, Msg: "rule name cannot be empty"}
	ErrRuleNoCondition = errs.ErrorCode{Code: 400041, Msg: "rule must have at least one condition"}
	ErrRuleNoTags      = errs.ErrorCode{Code: 400042, Msg: "rule must have at least one tag"}
	ErrRuleNotFound    = errs.ErrorCode{Code: 404040, Msg: "tag rule not found"}
)
