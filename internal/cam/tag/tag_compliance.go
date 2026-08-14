package tag

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

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
func (s *tagService) CheckCompliance(ctx context.Context, tenantID int64, filter ComplianceFilter) ([]ComplianceResult, int64, error) {
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
