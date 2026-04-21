package tag

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CloudAccountService 云账号服务接口（用于获取账号信息以路由到正确的适配器）
type CloudAccountService interface {
	GetAccountWithCredentials(ctx context.Context, id int64) (*domain.CloudAccount, error)
}

// InstanceCollection 实例集合名称
const InstanceCollection = "c_instance"

// TagService 标签管理业务逻辑层接口
type TagService interface {
	// 标签聚合查询（基于本地 MongoDB 资源数据）
	ListTags(ctx context.Context, tenantID string, filter TagFilter) ([]TagSummary, int64, error)
	GetTagStats(ctx context.Context, tenantID string) (*TagStats, error)
	ListTagResources(ctx context.Context, tenantID string, filter TagResourceFilter) ([]TagResource, int64, error)

	// 标签操作（调用云厂商 API）
	BindTags(ctx context.Context, tenantID string, req BindTagsReq) (*BatchResult, error)
	UnbindTags(ctx context.Context, tenantID string, req UnbindTagsReq) (*BatchResult, error)

	// 标签策略
	CreatePolicy(ctx context.Context, tenantID string, req CreatePolicyReq) (TagPolicy, error)
	ListPolicies(ctx context.Context, tenantID string, filter PolicyFilter) ([]TagPolicy, int64, error)
	UpdatePolicy(ctx context.Context, tenantID string, id int64, req UpdatePolicyReq) error
	DeletePolicy(ctx context.Context, tenantID string, id int64) error
	CheckCompliance(ctx context.Context, tenantID string, filter ComplianceFilter) ([]ComplianceResult, int64, error)

	// 自动打标规则
	CreateRule(ctx context.Context, tenantID string, req CreateRuleReq) (TagRule, error)
	ListRules(ctx context.Context, tenantID string, filter RuleFilter) ([]TagRule, int64, error)
	UpdateRule(ctx context.Context, tenantID string, id int64, req UpdateRuleReq) error
	DeleteRule(ctx context.Context, tenantID string, id int64) error
	PreviewRules(ctx context.Context, tenantID string, ruleIDs []int64) ([]RulePreviewResult, error)
	ExecuteRules(ctx context.Context, tenantID string, ruleIDs []int64) ([]RuleExecuteResult, error)
}

// tagService TagService 实现
type tagService struct {
	dao            TagDAO
	instanceColl   *mongo.Collection
	accountSvc     CloudAccountService
	adapterFactory *cloudx.AdapterFactory
}

// NewTagService 创建 TagService 实例
func NewTagService(
	dao TagDAO,
	instanceColl *mongo.Collection,
	accountSvc CloudAccountService,
	adapterFactory *cloudx.AdapterFactory,
) TagService {
	return &tagService{
		dao:            dao,
		instanceColl:   instanceColl,
		accountSvc:     accountSvc,
		adapterFactory: adapterFactory,
	}
}

// ==================== 标签聚合查询 ====================

// ListTags 通过 MongoDB 聚合管道从 instances 集合的 attributes.tags 字段按 key/value 分组统计
func (s *tagService) ListTags(ctx context.Context, tenantID string, filter TagFilter) ([]TagSummary, int64, error) {
	// Build match stage
	matchStage := bson.M{"tenant_id": tenantID, "attributes.tags": bson.M{"$exists": true, "$nin": []interface{}{nil, bson.M{}}}}
	if filter.Provider != "" {
		matchStage["attributes.provider"] = filter.Provider
	}
	if filter.AccountID > 0 {
		matchStage["account_id"] = filter.AccountID
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchStage}},
		{{Key: "$project", Value: bson.M{
			"tags":     bson.M{"$objectToArray": "$attributes.tags"},
			"provider": "$attributes.provider",
		}}},
		{{Key: "$unwind", Value: "$tags"}},
	}

	// Optional key/value filter after unwind
	postFilter := bson.M{}
	if filter.Key != "" {
		postFilter["tags.k"] = bson.M{"$regex": filter.Key, "$options": "i"}
	}
	if filter.Value != "" {
		postFilter["tags.v"] = bson.M{"$regex": filter.Value, "$options": "i"}
	}
	if len(postFilter) > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: postFilter}})
	}

	// Group by key+value
	pipeline = append(pipeline,
		bson.D{{Key: "$group", Value: bson.M{
			"_id":            bson.M{"key": "$tags.k", "value": "$tags.v"},
			"resource_count": bson.M{"$sum": 1},
			"providers":      bson.M{"$addToSet": "$provider"},
		}}},
		bson.D{{Key: "$sort", Value: bson.M{"resource_count": -1}}},
	)

	// Count total via facet
	countPipeline := append(mongo.Pipeline{}, pipeline...)
	countPipeline = append(countPipeline, bson.D{{Key: "$count", Value: "total"}})

	// Pagination
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	pipeline = append(pipeline,
		bson.D{{Key: "$skip", Value: offset}},
		bson.D{{Key: "$limit", Value: limit}},
	)

	// Execute main pipeline
	cursor, err := s.instanceColl.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	type aggResult struct {
		ID struct {
			Key   string `bson:"key"`
			Value string `bson:"value"`
		} `bson:"_id"`
		ResourceCount int64    `bson:"resource_count"`
		Providers     []string `bson:"providers"`
	}

	var results []aggResult
	if err = cursor.All(ctx, &results); err != nil {
		return nil, 0, err
	}

	items := make([]TagSummary, 0, len(results))
	for _, r := range results {
		items = append(items, TagSummary{
			Key:           r.ID.Key,
			Value:         r.ID.Value,
			ResourceCount: r.ResourceCount,
			Providers:     r.Providers,
		})
	}

	// Get total count
	countCursor, err := s.instanceColl.Aggregate(ctx, countPipeline)
	if err != nil {
		return items, int64(len(items)), nil
	}
	defer countCursor.Close(ctx)

	var countResult []struct {
		Total int64 `bson:"total"`
	}
	if err = countCursor.All(ctx, &countResult); err != nil || len(countResult) == 0 {
		return items, int64(len(items)), nil
	}

	return items, countResult[0].Total, nil
}

// GetTagStats 统计标签键总数、标签值总数、已打标资源数、总资源数、覆盖率
func (s *tagService) GetTagStats(ctx context.Context, tenantID string) (*TagStats, error) {
	// Total resources
	totalResources, err := s.instanceColl.CountDocuments(ctx, bson.M{"tenant_id": tenantID})
	if err != nil {
		return nil, err
	}

	// Tagged resources (has non-empty tags)
	taggedResources, err := s.instanceColl.CountDocuments(ctx, bson.M{
		"tenant_id":       tenantID,
		"attributes.tags": bson.M{"$exists": true, "$nin": []interface{}{nil, bson.M{}}},
	})
	if err != nil {
		return nil, err
	}

	// Distinct keys and key-value pairs via aggregation
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"tenant_id":       tenantID,
			"attributes.tags": bson.M{"$exists": true, "$nin": []interface{}{nil, bson.M{}}},
		}}},
		{{Key: "$project", Value: bson.M{"tags": bson.M{"$objectToArray": "$attributes.tags"}}}},
		{{Key: "$unwind", Value: "$tags"}},
		{{Key: "$group", Value: bson.M{
			"_id":          nil,
			"unique_keys":  bson.M{"$addToSet": "$tags.k"},
			"total_values": bson.M{"$sum": 1},
		}}},
	}

	cursor, err := s.instanceColl.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var aggResults []struct {
		UniqueKeys  []string `bson:"unique_keys"`
		TotalValues int64    `bson:"total_values"`
	}
	if err = cursor.All(ctx, &aggResults); err != nil {
		return nil, err
	}

	stats := &TagStats{
		TaggedResources: taggedResources,
		TotalResources:  totalResources,
	}

	if len(aggResults) > 0 {
		stats.TotalKeys = int64(len(aggResults[0].UniqueKeys))
		stats.TotalValues = aggResults[0].TotalValues
	}

	stats.CoveragePercent = CalculateCoverage(taggedResources, totalResources)

	return stats, nil
}

// CalculateCoverage 计算标签覆盖率
func CalculateCoverage(tagged, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round(float64(tagged)/float64(total)*10000) / 100
}

// ListTagResources 查询指定标签键值关联的资源列表
func (s *tagService) ListTagResources(ctx context.Context, tenantID string, filter TagResourceFilter) ([]TagResource, int64, error) {
	if filter.Key == "" {
		return nil, 0, ErrTagKeyEmpty
	}

	query := bson.M{"tenant_id": tenantID}
	if filter.Value != "" {
		query["attributes.tags."+filter.Key] = filter.Value
	} else {
		query["attributes.tags."+filter.Key] = bson.M{"$exists": true}
	}
	if filter.Provider != "" {
		query["attributes.provider"] = filter.Provider
	}

	total, err := s.instanceColl.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: query}},
		{{Key: "$sort", Value: bson.M{"utime": -1}}},
		{{Key: "$skip", Value: offset}},
		{{Key: "$limit", Value: limit}},
	}

	cursor, err := s.instanceColl.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	type instanceDoc struct {
		AssetID    string                 `bson:"asset_id"`
		AssetName  string                 `bson:"asset_name"`
		ModelUID   string                 `bson:"model_uid"`
		AccountID  int64                  `bson:"account_id"`
		Attributes map[string]interface{} `bson:"attributes"`
	}

	var docs []instanceDoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	items := make([]TagResource, 0, len(docs))
	for _, doc := range docs {
		provider, _ := doc.Attributes["provider"].(string)
		region, _ := doc.Attributes["region"].(string)
		accountName, _ := doc.Attributes["cloud_account_name"].(string)
		items = append(items, TagResource{
			AssetID:      doc.AssetID,
			AssetName:    doc.AssetName,
			ResourceType: doc.ModelUID,
			Provider:     provider,
			Region:       region,
			AccountID:    doc.AccountID,
			AccountName:  accountName,
		})
	}

	return items, total, nil
}

// ==================== 标签绑定/解绑 ====================

// ValidateTagKeys 校验标签键值，拒绝空字符串和纯空白字符串
func ValidateTagKeys(tags map[string]string) error {
	for k, v := range tags {
		if strings.TrimSpace(k) == "" {
			return ErrTagKeyEmpty
		}
		if strings.TrimSpace(v) == "" {
			return ErrTagValueEmpty
		}
	}
	return nil
}

// ValidateTagKeysList 校验标签键列表
func ValidateTagKeysList(keys []string) error {
	for _, k := range keys {
		if strings.TrimSpace(k) == "" {
			return ErrTagKeyEmpty
		}
	}
	return nil
}

// BindTags 为资源绑定标签
func (s *tagService) BindTags(ctx context.Context, tenantID string, req BindTagsReq) (*BatchResult, error) {
	if err := ValidateTagKeys(req.Tags); err != nil {
		return nil, err
	}

	total := len(req.Resources)
	result := &BatchResult{
		Total:    total,
		Failures: make([]FailureDetail, 0),
	}

	for _, res := range req.Resources {
		err := s.bindSingleResource(ctx, res, req.Tags)
		if err != nil {
			result.FailedCount++
			result.Failures = append(result.Failures, FailureDetail{
				ResourceID: res.ResourceID,
				Error:      err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	return result, nil
}

func (s *tagService) bindSingleResource(ctx context.Context, res ResourceRef, tags map[string]string) error {
	account, err := s.accountSvc.GetAccountWithCredentials(ctx, res.AccountID)
	if err != nil {
		return err
	}

	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return err
	}

	tagAdapter := adapter.Tag()
	if tagAdapter == nil {
		return errors.New("tag adapter not supported for provider: " + string(account.Provider))
	}

	return tagAdapter.TagResource(ctx, res.Region, res.ResourceType, res.ResourceID, tags)
}

// UnbindTags 解绑资源标签
func (s *tagService) UnbindTags(ctx context.Context, tenantID string, req UnbindTagsReq) (*BatchResult, error) {
	if err := ValidateTagKeysList(req.TagKeys); err != nil {
		return nil, err
	}

	total := len(req.Resources)
	result := &BatchResult{
		Total:    total,
		Failures: make([]FailureDetail, 0),
	}

	for _, res := range req.Resources {
		err := s.unbindSingleResource(ctx, res, req.TagKeys)
		if err != nil {
			result.FailedCount++
			result.Failures = append(result.Failures, FailureDetail{
				ResourceID: res.ResourceID,
				Error:      err.Error(),
			})
		} else {
			result.SuccessCount++
		}
	}

	return result, nil
}

func (s *tagService) unbindSingleResource(ctx context.Context, res ResourceRef, tagKeys []string) error {
	account, err := s.accountSvc.GetAccountWithCredentials(ctx, res.AccountID)
	if err != nil {
		return err
	}

	adapter, err := s.adapterFactory.CreateAdapter(account)
	if err != nil {
		return err
	}

	tagAdapter := adapter.Tag()
	if tagAdapter == nil {
		return errors.New("tag adapter not supported for provider: " + string(account.Provider))
	}

	return tagAdapter.UntagResource(ctx, res.Region, res.ResourceType, res.ResourceID, tagKeys)
}

// ==================== 标签策略 CRUD ====================

// CreatePolicy 创建标签策略
func (s *tagService) CreatePolicy(ctx context.Context, tenantID string, req CreatePolicyReq) (TagPolicy, error) {
	if strings.TrimSpace(req.Name) == "" {
		return TagPolicy{}, ErrPolicyNameEmpty
	}
	if len(req.RequiredKeys) == 0 {
		return TagPolicy{}, ErrPolicyKeysEmpty
	}

	policy := TagPolicy{
		Name:                req.Name,
		Description:         req.Description,
		RequiredKeys:        req.RequiredKeys,
		KeyValueConstraints: req.KeyValueConstraints,
		ResourceTypes:       req.ResourceTypes,
		Status:              "enabled",
		TenantID:            tenantID,
	}

	id, err := s.dao.InsertPolicy(ctx, policy)
	if err != nil {
		return TagPolicy{}, err
	}
	policy.ID = id
	return policy, nil
}

// ListPolicies 查询标签策略列表
func (s *tagService) ListPolicies(ctx context.Context, tenantID string, filter PolicyFilter) ([]TagPolicy, int64, error) {
	filter.TenantID = tenantID
	return s.dao.ListPolicies(ctx, filter)
}

// UpdatePolicy 更新标签策略
func (s *tagService) UpdatePolicy(ctx context.Context, tenantID string, id int64, req UpdatePolicyReq) error {
	existing, err := s.dao.GetPolicyByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrPolicyNotFound
		}
		return err
	}

	if existing.TenantID != tenantID {
		return ErrPolicyNotFound
	}

	// Apply partial updates
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.RequiredKeys != nil {
		existing.RequiredKeys = *req.RequiredKeys
	}
	if req.KeyValueConstraints != nil {
		existing.KeyValueConstraints = *req.KeyValueConstraints
	}
	if req.ResourceTypes != nil {
		existing.ResourceTypes = *req.ResourceTypes
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}
	existing.Utime = time.Now().UnixMilli()

	return s.dao.UpdatePolicy(ctx, existing)
}

// DeletePolicy 删除标签策略
func (s *tagService) DeletePolicy(ctx context.Context, tenantID string, id int64) error {
	existing, err := s.dao.GetPolicyByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrPolicyNotFound
		}
		return err
	}
	if existing.TenantID != tenantID {
		return ErrPolicyNotFound
	}
	return s.dao.DeletePolicy(ctx, id)
}

// ==================== 合规检查 ====================

// resourceTypeToModelUID 将前端传入的资源类型转为 MongoDB model_uid 的正则匹配
// 数据库中 model_uid 格式为 "{provider}_{type}" 如 aliyun_cdn, volcano_ecs, cloud_vm 等
func resourceTypeToModelUID(resourceType string) bson.M {
	// Map user-facing type to possible model_uid suffixes
	suffixes := map[string][]string{
		"ecs":            {"_ecs", "_vm", "cloud_vm"},
		"rds":            {"_rds", "cloud_rds"},
		"redis":          {"_redis", "cloud_redis"},
		"mongodb":        {"_mongodb", "cloud_mongodb"},
		"vpc":            {"_vpc", "cloud_vpc"},
		"eip":            {"_eip", "cloud_eip"},
		"vswitch":        {"_vswitch", "cloud_vswitch"},
		"lb":             {"_lb", "cloud_lb"},
		"nas":            {"_nas", "_sfs", "cloud_nas"},
		"oss":            {"_oss", "cloud_oss"},
		"cdn":            {"_cdn", "cloud_cdn"},
		"waf":            {"_waf", "cloud_waf"},
		"disk":           {"_disk", "cloud_disk"},
		"snapshot":       {"_snapshot", "cloud_snapshot"},
		"security_group": {"_security_group", "cloud_security_group"},
	}

	if patterns, ok := suffixes[resourceType]; ok {
		// Build regex: match any of the suffixes
		regex := ""
		for i, p := range patterns {
			if i > 0 {
				regex += "|"
			}
			regex += p
		}
		return bson.M{"$regex": regex, "$options": "i"}
	}
	// Fallback: exact match
	return bson.M{"$eq": resourceType}
}

// mapResourceTypes is no longer needed since resourceTypeToModelUID returns bson.M regex

// CheckCompliance 使用 MongoDB 查询预过滤不合规资源，支持分页
// 返回当前页不合规资源列表和不合规资源总数
func (s *tagService) CheckCompliance(ctx context.Context, tenantID string, filter ComplianceFilter) ([]ComplianceResult, int64, error) {
	// Get the policy
	policy, err := s.dao.GetPolicyByID(ctx, filter.PolicyID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, 0, ErrPolicyNotFound
		}
		return nil, 0, err
	}

	// Build base query
	baseQuery := bson.M{"tenant_id": tenantID}
	if filter.Provider != "" {
		baseQuery["attributes.provider"] = filter.Provider
	}
	if filter.ResourceType != "" {
		baseQuery["model_uid"] = resourceTypeToModelUID(filter.ResourceType)
	}

	// Scan up to 5000 docs, collect all non-compliant, then paginate in memory
	opts := options.Find().
		SetSort(bson.D{{Key: "asset_id", Value: 1}}).
		SetLimit(5000)

	cursor, err := s.instanceColl.Find(ctx, baseQuery, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	type instanceDoc struct {
		AssetID    string                 `bson:"asset_id"`
		AssetName  string                 `bson:"asset_name"`
		ModelUID   string                 `bson:"model_uid"`
		AccountID  int64                  `bson:"account_id"`
		Attributes map[string]interface{} `bson:"attributes"`
	}

	// Collect all non-compliant resources
	var allNonCompliant []ComplianceResult
	for cursor.Next(ctx) {
		var doc instanceDoc
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		violations := CheckResourceCompliance(doc.Attributes, policy)
		if len(violations) > 0 {
			provider, _ := doc.Attributes["provider"].(string)
			region, _ := doc.Attributes["region"].(string)
			allNonCompliant = append(allNonCompliant, ComplianceResult{
				AssetID:      doc.AssetID,
				AssetName:    doc.AssetName,
				ResourceType: doc.ModelUID,
				Provider:     provider,
				Region:       region,
				AccountID:    doc.AccountID,
				Violations:   violations,
			})
		}
	}

	total := int64(len(allNonCompliant))

	// Apply pagination
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	start := offset
	if start > total {
		return []ComplianceResult{}, total, nil
	}
	end := start + limit
	if end > total {
		end = total
	}

	return allNonCompliant[start:end], total, nil
}

// CheckResourceCompliance 检查单个资源的合规性（导出以便测试）
func CheckResourceCompliance(attributes map[string]interface{}, policy TagPolicy) []Violation {
	var violations []Violation

	// Extract tags from attributes
	tags := extractTags(attributes)

	// Check missing required keys
	for _, requiredKey := range policy.RequiredKeys {
		if _, exists := tags[requiredKey]; !exists {
			violations = append(violations, Violation{
				Type: "missing_key",
				Key:  requiredKey,
			})
		}
	}

	// Check invalid values
	for key, allowedValues := range policy.KeyValueConstraints {
		if val, exists := tags[key]; exists && len(allowedValues) > 0 {
			found := false
			for _, allowed := range allowedValues {
				if val == allowed {
					found = true
					break
				}
			}
			if !found {
				violations = append(violations, Violation{
					Type:    "invalid_value",
					Key:     key,
					Value:   val,
					Allowed: allowedValues,
				})
			}
		}
	}

	return violations
}

// extractTags 从 attributes 中提取 tags map
func extractTags(attributes map[string]interface{}) map[string]string {
	result := make(map[string]string)
	if attributes == nil {
		return result
	}

	tagsRaw, ok := attributes["tags"]
	if !ok || tagsRaw == nil {
		return result
	}

	switch t := tagsRaw.(type) {
	case map[string]string:
		return t
	case map[string]interface{}:
		for k, v := range t {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
	case bson.M:
		for k, v := range t {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
	default:
		// Try to handle primitive.M or other map-like types via reflection
		if m, ok := tagsRaw.(interface{ Map() map[string]interface{} }); ok {
			for k, v := range m.Map() {
				if s, ok := v.(string); ok {
					result[k] = s
				}
			}
		}
	}
	return result
}

// Ensure compile-time interface compliance
var _ TagService = (*tagService)(nil)

// ==================== 自动打标规则 ====================

func (s *tagService) CreateRule(ctx context.Context, tenantID string, req CreateRuleReq) (TagRule, error) {
	if strings.TrimSpace(req.Name) == "" {
		return TagRule{}, ErrRuleNameEmpty
	}
	if len(req.Conditions) == 0 {
		return TagRule{}, ErrRuleNoCondition
	}
	if len(req.Tags) == 0 {
		return TagRule{}, ErrRuleNoTags
	}
	logic := req.Logic
	if logic == "" {
		logic = "and"
	}
	rule := TagRule{
		Name:        req.Name,
		Description: req.Description,
		Logic:       logic,
		Conditions:  req.Conditions,
		Tags:        req.Tags,
		Priority:    req.Priority,
		Status:      "enabled",
		TenantID:    tenantID,
	}
	id, err := s.dao.InsertRule(ctx, rule)
	if err != nil {
		return TagRule{}, err
	}
	rule.ID = id
	return rule, nil
}

func (s *tagService) ListRules(ctx context.Context, tenantID string, filter RuleFilter) ([]TagRule, int64, error) {
	filter.TenantID = tenantID
	return s.dao.ListRules(ctx, filter)
}

func (s *tagService) UpdateRule(ctx context.Context, tenantID string, id int64, req UpdateRuleReq) error {
	existing, err := s.dao.GetRuleByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrRuleNotFound
		}
		return err
	}
	if existing.TenantID != tenantID {
		return ErrRuleNotFound
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Logic != nil {
		existing.Logic = *req.Logic
	}
	if req.Conditions != nil {
		existing.Conditions = *req.Conditions
	}
	if req.Tags != nil {
		existing.Tags = *req.Tags
	}
	if req.Priority != nil {
		existing.Priority = *req.Priority
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}
	return s.dao.UpdateRule(ctx, existing)
}

func (s *tagService) DeleteRule(ctx context.Context, tenantID string, id int64) error {
	existing, err := s.dao.GetRuleByID(ctx, id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrRuleNotFound
		}
		return err
	}
	if existing.TenantID != tenantID {
		return ErrRuleNotFound
	}
	return s.dao.DeleteRule(ctx, id)
}

// PreviewRules 预览规则匹配结果（不实际打标）— 使用 MongoDB 查询
func (s *tagService) PreviewRules(ctx context.Context, tenantID string, ruleIDs []int64) ([]RulePreviewResult, error) {
	rules, err := s.getRulesByIDs(ctx, tenantID, ruleIDs)
	if err != nil {
		return nil, err
	}

	type instanceDoc struct {
		AssetID    string                 `bson:"asset_id"`
		AssetName  string                 `bson:"asset_name"`
		ModelUID   string                 `bson:"model_uid"`
		Attributes map[string]interface{} `bson:"attributes"`
	}

	var results []RulePreviewResult
	for _, rule := range rules {
		query := buildRuleQuery(tenantID, rule)
		count, _ := s.instanceColl.CountDocuments(ctx, query)

		// Fetch first 100 matching resources for preview
		var resources []PreviewResource
		cursor, err := s.instanceColl.Find(ctx, query, options.Find().SetLimit(100).SetSort(bson.D{{Key: "asset_name", Value: 1}}))
		if err == nil {
			for cursor.Next(ctx) {
				var doc instanceDoc
				if err := cursor.Decode(&doc); err != nil {
					continue
				}
				provider, _ := doc.Attributes["provider"].(string)
				region, _ := doc.Attributes["region"].(string)
				resources = append(resources, PreviewResource{
					AssetID:      doc.AssetID,
					AssetName:    doc.AssetName,
					ResourceType: doc.ModelUID,
					Provider:     provider,
					Region:       region,
				})
			}
			cursor.Close(ctx)
		}

		results = append(results, RulePreviewResult{
			RuleID:     rule.ID,
			RuleName:   rule.Name,
			MatchCount: count,
			Resources:  resources,
		})
	}
	return results, nil
}

// ExecuteRules 执行规则：用 MongoDB 查询匹配资源，然后调用云 API 打标
func (s *tagService) ExecuteRules(ctx context.Context, tenantID string, ruleIDs []int64) ([]RuleExecuteResult, error) {
	rules, err := s.getRulesByIDs(ctx, tenantID, ruleIDs)
	if err != nil {
		return nil, err
	}

	type instanceDoc struct {
		AssetID    string                 `bson:"asset_id"`
		ModelUID   string                 `bson:"model_uid"`
		AccountID  int64                  `bson:"account_id"`
		Attributes map[string]interface{} `bson:"attributes"`
	}

	var results []RuleExecuteResult
	for _, rule := range rules {
		res := RuleExecuteResult{RuleID: rule.ID, RuleName: rule.Name}
		query := buildRuleQuery(tenantID, rule)
		cursor, err := s.instanceColl.Find(ctx, query, options.Find().SetLimit(5000))
		if err != nil {
			results = append(results, res)
			continue
		}

		for cursor.Next(ctx) {
			var doc instanceDoc
			if err := cursor.Decode(&doc); err != nil {
				continue
			}
			res.MatchCount++
			attrs, _ := doc.Attributes["region"].(string)
			err := s.bindSingleResource(ctx, ResourceRef{
				AccountID:    doc.AccountID,
				Region:       attrs,
				ResourceType: doc.ModelUID,
				ResourceID:   doc.AssetID,
			}, rule.Tags)
			if err != nil {
				res.FailedCount++
			} else {
				res.SuccessCount++
			}
		}
		cursor.Close(ctx)
		results = append(results, res)
	}
	return results, nil
}

// buildRuleQuery 将规则条件转为 MongoDB 查询
func buildRuleQuery(tenantID string, rule TagRule) bson.M {
	base := bson.M{"tenant_id": tenantID}
	if len(rule.Conditions) == 0 {
		return base
	}

	var condFilters []bson.M
	for _, cond := range rule.Conditions {
		f := conditionToFilter(cond)
		if f != nil {
			condFilters = append(condFilters, f)
		}
	}

	if len(condFilters) == 0 {
		return base
	}

	if rule.Logic == "or" {
		base["$or"] = condFilters
	} else {
		// AND: merge all conditions into base
		for _, f := range condFilters {
			for k, v := range f {
				base[k] = v
			}
		}
	}
	return base
}

// conditionToFilter 将单个条件转为 MongoDB 过滤器
func conditionToFilter(cond RuleCondition) bson.M {
	field := condFieldToMongoField(cond.Field)
	if field == "" {
		return nil
	}

	switch cond.Operator {
	case "equals":
		return bson.M{field: cond.Value}
	case "contains":
		return bson.M{field: bson.M{"$regex": regexp.QuoteMeta(cond.Value), "$options": "i"}}
	case "prefix":
		return bson.M{field: bson.M{"$regex": "^" + regexp.QuoteMeta(cond.Value), "$options": "i"}}
	case "suffix":
		return bson.M{field: bson.M{"$regex": regexp.QuoteMeta(cond.Value) + "$", "$options": "i"}}
	case "regex":
		return bson.M{field: bson.M{"$regex": cond.Value, "$options": "i"}}
	default:
		return nil
	}
}

// condFieldToMongoField 将前端字段名映射到 MongoDB 字段路径
func condFieldToMongoField(field string) string {
	switch field {
	case "asset_name":
		return "asset_name"
	case "asset_id":
		return "asset_id"
	case "model_uid":
		return "model_uid"
	case "provider":
		return "attributes.provider"
	case "region":
		return "attributes.region"
	case "account_name":
		return "attributes.cloud_account_name"
	default:
		return ""
	}
}

// getRulesByIDs 获取指定 ID 的规则，如果 ruleIDs 为空则获取所有启用的规则
func (s *tagService) getRulesByIDs(ctx context.Context, tenantID string, ruleIDs []int64) ([]TagRule, error) {
	if len(ruleIDs) == 0 {
		return s.dao.ListEnabledRules(ctx, tenantID)
	}
	var rules []TagRule
	for _, id := range ruleIDs {
		rule, err := s.dao.GetRuleByID(ctx, id)
		if err != nil {
			continue
		}
		if rule.TenantID == tenantID {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

// MatchRule 检查一个资源文档是否匹配规则（导出以便测试）
func MatchRule(doc bson.M, rule TagRule) bool {
	if len(rule.Conditions) == 0 {
		return false
	}
	attrs, _ := doc["attributes"].(bson.M)

	if rule.Logic == "or" {
		for _, cond := range rule.Conditions {
			if matchCondition(doc, attrs, cond) {
				return true
			}
		}
		return false
	}
	// default: AND
	for _, cond := range rule.Conditions {
		if !matchCondition(doc, attrs, cond) {
			return false
		}
	}
	return true
}

// matchCondition 检查单个条件是否匹配
func matchCondition(doc bson.M, attrs bson.M, cond RuleCondition) bool {
	val := getFieldValue(doc, attrs, cond.Field)
	return matchOperator(val, cond.Operator, cond.Value)
}

// getFieldValue 从文档中提取字段值
func getFieldValue(doc bson.M, attrs bson.M, field string) string {
	switch field {
	case "asset_name":
		v, _ := doc["asset_name"].(string)
		return v
	case "asset_id":
		v, _ := doc["asset_id"].(string)
		return v
	case "model_uid":
		v, _ := doc["model_uid"].(string)
		return v
	case "provider":
		if attrs != nil {
			v, _ := attrs["provider"].(string)
			return v
		}
	case "region":
		if attrs != nil {
			v, _ := attrs["region"].(string)
			return v
		}
	case "account_name":
		if attrs != nil {
			v, _ := attrs["cloud_account_name"].(string)
			return v
		}
	}
	return ""
}

// matchOperator 执行匹配操作
func matchOperator(fieldVal, operator, pattern string) bool {
	switch operator {
	case "equals":
		return fieldVal == pattern
	case "contains":
		return strings.Contains(strings.ToLower(fieldVal), strings.ToLower(pattern))
	case "prefix":
		return strings.HasPrefix(strings.ToLower(fieldVal), strings.ToLower(pattern))
	case "suffix":
		return strings.HasSuffix(strings.ToLower(fieldVal), strings.ToLower(pattern))
	case "regex":
		matched, err := regexp.MatchString(pattern, fieldVal)
		return err == nil && matched
	default:
		return false
	}
}
