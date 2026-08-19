package repository

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ProbeResultsCollection TLS 探测结果集合名。
const ProbeResultsCollection = "cert_probe_results"

type probeResultRepository struct {
	db *mongox.Mongo
}

// NewProbeResultRepository 创建探测结果仓储。
func NewProbeResultRepository(db *mongox.Mongo) domain.ProbeResultRepository {
	return &probeResultRepository{db: db}
}

// Create 写入探测结果；DEFAULT 填充：probeAt=now（TTL 90 天自动清理基准）。
func (r *probeResultRepository) Create(ctx context.Context, result *domain.ProbeResult) error {
	if result.ProbeAt.IsZero() {
		result.ProbeAt = time.Now()
	}
	_, err := r.db.Collection(ProbeResultsCollection).InsertOne(ctx, result)
	return err
}

// LatestByDomain 最近一次探测（idx_domain_probe_desc）。
func (r *probeResultRepository) LatestByDomain(ctx context.Context, domainName string) (domain.ProbeResult, error) {
	var result domain.ProbeResult
	err := r.db.Collection(ProbeResultsCollection).
		FindOne(ctx, bson.M{"domain": domainName},
			options.FindOne().SetSort(bson.D{{Key: "probeAt", Value: -1}})).
		Decode(&result)
	return result, err
}

// LatestPerDomain 每个域名的最近一次探测（domain 去重、probeAt 最新）：
// $sort(domain, probeAt desc) + $group($first) 取各域最新记录。
func (r *probeResultRepository) LatestPerDomain(ctx context.Context) ([]domain.ProbeResult, error) {
	cursor, err := r.db.Collection(ProbeResultsCollection).Aggregate(ctx, mongo.Pipeline{
		{{Key: "$sort", Value: bson.D{
			{Key: "domain", Value: 1},
			{Key: "probeAt", Value: -1},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$domain"},
			{Key: "latest", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
		}}},
		{{Key: "$replaceRoot", Value: bson.D{{Key: "newRoot", Value: "$latest"}}}},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var results []domain.ProbeResult
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	if results == nil {
		results = []domain.ProbeResult{}
	}
	return results, nil
}

// ListRecentByDomains 批量查询各域名最近探测记录（任务 5.10 验证窗口达标判定）：
// domain ∈ domains 过滤、$sort(domain, probeAt desc) 后每域名截取至多 limit 条
// （"连续 verifyConfirmProbes 次一致"判据的数据源）；domain 字典序、同域 probeAt
// 降序稳定返回。domains 为空或 limit<=0 返回空切片。
func (r *probeResultRepository) ListRecentByDomains(ctx context.Context, domains []string, limit int) ([]domain.ProbeResult, error) {
	if len(domains) == 0 || limit <= 0 {
		return []domain.ProbeResult{}, nil
	}
	cursor, err := r.db.Collection(ProbeResultsCollection).Find(ctx,
		bson.M{"domain": bson.M{"$in": domains}},
		options.Find().SetSort(bson.D{
			{Key: "domain", Value: 1},
			{Key: "probeAt", Value: -1},
		}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := []domain.ProbeResult{}
	perDomain := make(map[string]int, len(domains))
	for cursor.Next(ctx) {
		var result domain.ProbeResult
		if err := cursor.Decode(&result); err != nil {
			return nil, err
		}
		if perDomain[result.Domain] >= limit {
			continue // 该域名已取满 limit 条（排序保证取的是最近记录）
		}
		perDomain[result.Domain]++
		out = append(out, result)
	}
	return out, cursor.Err()
}
