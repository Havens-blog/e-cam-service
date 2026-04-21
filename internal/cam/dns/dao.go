package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Collection names
const (
	DomainCollection = "c_dns_domain"
	RecordCollection = "c_dns_record"
)

// DnsDomainDoc DNS 域名 MongoDB 文档
type DnsDomainDoc struct {
	DomainID    string `bson:"domain_id"`
	DomainName  string `bson:"domain_name"`
	Provider    string `bson:"provider"`
	AccountID   int64  `bson:"account_id"`
	AccountName string `bson:"account_name"`
	TenantID    string `bson:"tenant_id"`
	RecordCount int64  `bson:"record_count"`
	Status      string `bson:"status"`
	Ctime       int64  `bson:"ctime"`
	Utime       int64  `bson:"utime"`
}

// DnsRecordDoc DNS 解析记录 MongoDB 文档
type DnsRecordDoc struct {
	RecordID  string `bson:"record_id"`
	Domain    string `bson:"domain"`
	RR        string `bson:"rr"`
	Type      string `bson:"type"`
	Value     string `bson:"value"`
	TTL       int    `bson:"ttl"`
	Priority  int    `bson:"priority"`
	Line      string `bson:"line"`
	Status    string `bson:"status"`
	Provider  string `bson:"provider"`
	AccountID int64  `bson:"account_id"`
	TenantID  string `bson:"tenant_id"`
	Ctime     int64  `bson:"ctime"`
	Utime     int64  `bson:"utime"`
}

// DnsDomainDAO DNS 域名数据访问
type DnsDomainDAO struct {
	coll *mongo.Collection
}

// NewDnsDomainDAO 创建域名 DAO
func NewDnsDomainDAO(coll *mongo.Collection) *DnsDomainDAO {
	return &DnsDomainDAO{coll: coll}
}

// UpsertDomain upsert 域名（按 tenant_id + domain_name + account_id）
func (d *DnsDomainDAO) UpsertDomain(ctx context.Context, doc DnsDomainDoc) error {
	now := time.Now().Unix()
	filter := bson.M{
		"tenant_id":   doc.TenantID,
		"domain_name": doc.DomainName,
		"account_id":  doc.AccountID,
	}
	update := bson.M{
		"$set": bson.M{
			"domain_id":    doc.DomainID,
			"domain_name":  doc.DomainName,
			"provider":     doc.Provider,
			"account_id":   doc.AccountID,
			"account_name": doc.AccountName,
			"tenant_id":    doc.TenantID,
			"record_count": doc.RecordCount,
			"status":       doc.Status,
			"utime":        now,
		},
		"$setOnInsert": bson.M{
			"ctime": now,
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := d.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

// ListDomains 查询域名列表（支持过滤+分页）
func (d *DnsDomainDAO) ListDomains(ctx context.Context, tenantID string, filter DomainFilter) ([]DnsDomainDoc, int64, error) {
	query := bson.M{"tenant_id": tenantID}
	if filter.Keyword != "" {
		query["domain_name"] = bson.M{"$regex": filter.Keyword, "$options": "i"}
	}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.AccountID > 0 {
		query["account_id"] = filter.AccountID
	}

	total, err := d.coll.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("count dns domains: %w", err)
	}

	offset := filter.Offset
	limit := filter.Limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}

	opts := options.Find().
		SetSkip(offset).
		SetLimit(limit).
		SetSort(bson.D{{Key: "domain_name", Value: 1}})

	cursor, err := d.coll.Find(ctx, query, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find dns domains: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []DnsDomainDoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, 0, fmt.Errorf("decode dns domains: %w", err)
	}
	return docs, total, nil
}

// CountDomains 统计域名总数
func (d *DnsDomainDAO) CountDomains(ctx context.Context, tenantID string) (int64, error) {
	return d.coll.CountDocuments(ctx, bson.M{"tenant_id": tenantID})
}

// CountDomainsByProvider 按云厂商统计域名数
func (d *DnsDomainDAO) CountDomainsByProvider(ctx context.Context, tenantID string) (map[string]int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"tenant_id": tenantID}}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$provider",
			"count": bson.M{"$sum": 1},
		}}},
	}
	cursor, err := d.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate dns domains by provider: %w", err)
	}
	defer cursor.Close(ctx)

	result := make(map[string]int64)
	for cursor.Next(ctx) {
		var item struct {
			ID    string `bson:"_id"`
			Count int64  `bson:"count"`
		}
		if err := cursor.Decode(&item); err != nil {
			continue
		}
		if item.ID != "" {
			result[item.ID] = item.Count
		}
	}
	return result, nil
}

// DeleteStaleDomains 删除不再存在的域名
func (d *DnsDomainDAO) DeleteStaleDomains(ctx context.Context, tenantID string, accountID int64, currentNames []string) error {
	filter := bson.M{
		"tenant_id":  tenantID,
		"account_id": accountID,
	}
	if len(currentNames) > 0 {
		filter["domain_name"] = bson.M{"$nin": currentNames}
	}
	_, err := d.coll.DeleteMany(ctx, filter)
	return err
}

// DnsRecordDAO DNS 解析记录数据访问
type DnsRecordDAO struct {
	coll *mongo.Collection
}

// NewDnsRecordDAO 创建记录 DAO
func NewDnsRecordDAO(coll *mongo.Collection) *DnsRecordDAO {
	return &DnsRecordDAO{coll: coll}
}

// UpsertRecord upsert 解析记录（按 tenant_id + record_id + account_id）
func (d *DnsRecordDAO) UpsertRecord(ctx context.Context, doc DnsRecordDoc) error {
	now := time.Now().Unix()
	filter := bson.M{
		"tenant_id":  doc.TenantID,
		"record_id":  doc.RecordID,
		"account_id": doc.AccountID,
	}
	update := bson.M{
		"$set": bson.M{
			"record_id":  doc.RecordID,
			"domain":     doc.Domain,
			"rr":         doc.RR,
			"type":       doc.Type,
			"value":      doc.Value,
			"ttl":        doc.TTL,
			"priority":   doc.Priority,
			"line":       doc.Line,
			"status":     doc.Status,
			"provider":   doc.Provider,
			"account_id": doc.AccountID,
			"tenant_id":  doc.TenantID,
			"utime":      now,
		},
		"$setOnInsert": bson.M{
			"ctime": now,
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := d.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

// ListRecords 查询解析记录列表（支持过滤+分页）
func (d *DnsRecordDAO) ListRecords(ctx context.Context, tenantID string, domain string, filter RecordFilter) ([]DnsRecordDoc, int64, error) {
	query := bson.M{
		"tenant_id": tenantID,
		"domain":    domain,
	}
	if filter.RecordType != "" {
		query["type"] = filter.RecordType
	}
	if filter.Keyword != "" {
		query["$or"] = bson.A{
			bson.M{"rr": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			bson.M{"value": bson.M{"$regex": filter.Keyword, "$options": "i"}},
		}
	}

	total, err := d.coll.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("count dns records: %w", err)
	}

	offset := filter.Offset
	limit := filter.Limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}

	opts := options.Find().
		SetSkip(offset).
		SetLimit(limit).
		SetSort(bson.D{{Key: "rr", Value: 1}})

	cursor, err := d.coll.Find(ctx, query, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find dns records: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []DnsRecordDoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, 0, fmt.Errorf("decode dns records: %w", err)
	}
	return docs, total, nil
}

// SearchRecords 跨域名搜索解析记录（按 RR 或完整子域名模糊匹配）
func (d *DnsRecordDAO) SearchRecords(ctx context.Context, tenantID string, keyword string, limit int64) ([]DnsRecordDoc, int64, error) {
	query := bson.M{
		"tenant_id": tenantID,
		"$or": bson.A{
			bson.M{"rr": bson.M{"$regex": keyword, "$options": "i"}},
			bson.M{"value": bson.M{"$regex": keyword, "$options": "i"}},
			bson.M{"domain": bson.M{"$regex": keyword, "$options": "i"}},
		},
	}

	total, err := d.coll.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("count search records: %w", err)
	}

	if limit <= 0 {
		limit = 50
	}

	opts := options.Find().
		SetLimit(limit).
		SetSort(bson.D{{Key: "rr", Value: 1}})

	cursor, err := d.coll.Find(ctx, query, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("search dns records: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []DnsRecordDoc
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, 0, fmt.Errorf("decode search records: %w", err)
	}
	return docs, total, nil
}

// CountRecordsByType 按记录类型统计
func (d *DnsRecordDAO) CountRecordsByType(ctx context.Context, tenantID string) (map[string]int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"tenant_id": tenantID}}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$type",
			"count": bson.M{"$sum": 1},
		}}},
	}
	cursor, err := d.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate dns records by type: %w", err)
	}
	defer cursor.Close(ctx)

	result := make(map[string]int64)
	for cursor.Next(ctx) {
		var item struct {
			ID    string `bson:"_id"`
			Count int64  `bson:"count"`
		}
		if err := cursor.Decode(&item); err != nil {
			continue
		}
		if item.ID != "" {
			result[item.ID] = item.Count
		}
	}
	return result, nil
}

// DeleteStaleRecords 删除不再存在的记录
func (d *DnsRecordDAO) DeleteStaleRecords(ctx context.Context, tenantID string, accountID int64, domain string, currentIDs []string) error {
	filter := bson.M{
		"tenant_id":  tenantID,
		"account_id": accountID,
		"domain":     domain,
	}
	if len(currentIDs) > 0 {
		filter["record_id"] = bson.M{"$nin": currentIDs}
	}
	_, err := d.coll.DeleteMany(ctx, filter)
	return err
}

// InitIndexes 创建 DNS 集合的 MongoDB 索引
func InitIndexes(db *mongox.Mongo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 域名集合索引
	domainColl := db.Collection(DomainCollection)
	domainIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "domain_name", Value: 1},
				{Key: "account_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "provider", Value: 1},
			},
		},
	}
	if _, err := domainColl.Indexes().CreateMany(ctx, domainIndexes); err != nil {
		return fmt.Errorf("create dns domain indexes: %w", err)
	}

	// 记录集合索引
	recordColl := db.Collection(RecordCollection)
	recordIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "record_id", Value: 1},
				{Key: "account_id", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "tenant_id", Value: 1},
				{Key: "domain", Value: 1},
				{Key: "type", Value: 1},
			},
		},
	}
	if _, err := recordColl.Indexes().CreateMany(ctx, recordIndexes); err != nil {
		return fmt.Errorf("create dns record indexes: %w", err)
	}

	return nil
}
