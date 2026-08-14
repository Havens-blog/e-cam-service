package tag

import (
	"context"
	"math"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ==================== 标签聚合查询 ====================

// ListTags 通过 MongoDB 聚合管道从 instances 集合的 attributes.tags 字段按 key/value 分组统计
func (s *tagService) ListTags(ctx context.Context, tenantID int64, filter TagFilter) ([]TagSummary, int64, error) {
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
func (s *tagService) GetTagStats(ctx context.Context, tenantID int64) (*TagStats, error) {
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
func (s *tagService) ListTagResources(ctx context.Context, tenantID int64, filter TagResourceFilter) ([]TagResource, int64, error) {
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
