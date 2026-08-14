package tag

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ==================== 自动打标规则 ====================

func (s *tagService) CreateRule(ctx context.Context, tenantID int64, req CreateRuleReq) (TagRule, error) {
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

func (s *tagService) ListRules(ctx context.Context, tenantID int64, filter RuleFilter) ([]TagRule, int64, error) {
	filter.TenantID = tenantID
	return s.dao.ListRules(ctx, filter)
}

func (s *tagService) UpdateRule(ctx context.Context, tenantID int64, id int64, req UpdateRuleReq) error {
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

func (s *tagService) DeleteRule(ctx context.Context, tenantID int64, id int64) error {
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
func (s *tagService) PreviewRules(ctx context.Context, tenantID int64, ruleIDs []int64) ([]RulePreviewResult, error) {
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
func (s *tagService) ExecuteRules(ctx context.Context, tenantID int64, ruleIDs []int64) ([]RuleExecuteResult, error) {
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
func buildRuleQuery(tenantID int64, rule TagRule) bson.M {
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
func (s *tagService) getRulesByIDs(ctx context.Context, tenantID int64, ruleIDs []int64) ([]TagRule, error) {
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
