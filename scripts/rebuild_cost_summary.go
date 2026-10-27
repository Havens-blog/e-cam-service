//go:build ignore
// +build ignore

// Rebuild cost daily summary: unified_bill → ecam_cost_daily_summary。
//
// 成本查询（summary/trend/distribution）走预聚合汇总表 ecam_cost_daily_summary
// （internal/cam/init.go SetSummaryDAO）。补归一化只写明细表 unified_bill，不重建
// 汇总表时页面查询不到新数据。本脚本从 unified_bill 聚合重建指定租户 + 日期范围的
// 汇总表行（先删后插，幂等，可安全重跑）。
//
// Usage
//   # 重建 tenant 3 的 2026-02 月汇总（默认）
//   go run scripts/rebuild_cost_summary.go -apply
//
//   # 重建全部租户 + 指定日期范围
//   go run scripts/rebuild_cost_summary.go -tenant 0 -start 2026-02-01 -end 2026-02-28 -apply
//
// Flags
//   -mongo/-db/-user/-password  MongoDB 连接（默认 prod ecam）
//   -tenant  租户 ID（0 = 全部租户，默认 3）
//   -start   起始日期 YYYY-MM-DD（默认 2026-02-01）
//   -end     结束日期 YYYY-MM-DD（默认 2026-02-28）
//   -apply   是否写库（默认 false = dry-run 预览）
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	unifiedCollection = "ecam_cost_unified_bill"
	summaryCollection = "ecam_cost_daily_summary"
)

var (
	mongoURI  = flag.String("mongo", "mongodb://106.52.187.69:27017/ecam?authSource=admin", "MongoDB URI")
	database  = flag.String("db", "ecam", "Database name")
	username  = flag.String("user", "ecmdb", "MongoDB username")
	password  = flag.String("password", "123456", "MongoDB password")
	tenantID  = flag.Int64("tenant", 3, "Tenant ID (0 = all tenants)")
	startDate = flag.String("start", "2026-02-01", "Start date YYYY-MM-DD")
	endDate   = flag.String("end", "2026-02-28", "End date YYYY-MM-DD")
	apply     = flag.Bool("apply", false, "Apply (default: dry-run preview only)")
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	uri := *mongoURI
	if *username != "" {
		parts := strings.SplitN(uri, "//", 2)
		if len(parts) == 2 {
			uri = parts[0] + "//" + *username + ":" + *password + "@" + parts[1]
		}
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		log.Fatalf("connect mongodb: %v", err)
	}
	defer client.Disconnect(ctx)
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ping mongodb: %v", err)
	}
	db := client.Database(*database)

	scope := "指定租户 " + fmt.Sprintf("%d", *tenantID)
	if *tenantID == 0 {
		scope = "全部租户"
	}
	fmt.Printf("重建汇总表: %s, 日期范围 %s ~ %s\n", scope, *startDate, *endDate)

	// 聚合 unified_bill（dry-run 与 apply 共用）
	rows, err := aggregateSummary(ctx, db, *tenantID, *startDate, *endDate)
	if err != nil {
		log.Fatalf("aggregate: %v", err)
	}
	fmt.Printf("聚合得到汇总行: %d 行\n", len(rows))

	var grandAmount float64
	byProvider := map[string]float64{}
	for _, r := range rows {
		grandAmount += r.AmountCNY
		byProvider[r.Provider] += r.AmountCNY
	}
	fmt.Printf("金额合计: ¥%.2f\n", grandAmount)
	fmt.Println("按 provider 金额分布:")
	for p, a := range byProvider {
		fmt.Printf("  %-10s : ¥%.2f\n", p, a)
	}

	if !*apply {
		fmt.Println("\nDRY-RUN 完成（未写库，加 -apply 执行）")
		return
	}

	// 先删后插
	del := bson.M{
		"billing_date": bson.M{"$gte": *startDate, "$lte": *endDate},
	}
	if *tenantID != 0 {
		del["tenant_id"] = *tenantID
	}
	delRes, err := db.Collection(summaryCollection).DeleteMany(ctx, del)
	if err != nil {
		log.Fatalf("delete old summary: %v", err)
	}
	fmt.Printf("\n已删除旧汇总: %d 行\n", delRes.DeletedCount)

	written, err := insertSummary(ctx, db, rows)
	if err != nil {
		log.Fatalf("insert summary: %v", err)
	}
	fmt.Printf("已写入新汇总: %d 行\n", written)
	fmt.Println("✅ 汇总表重建完成")
}

type summaryRow struct {
	BillingDate string  `bson:"billing_date"`
	TenantID    int64   `bson:"tenant_id"`
	Provider    string  `bson:"provider"`
	AccountID   int64   `bson:"account_id"`
	ServiceType string  `bson:"service_type"`
	Region      string  `bson:"region"`
	Amount      float64 `bson:"amount"`
	AmountCNY   float64 `bson:"amount_cny"`
	RecordCount int64   `bson:"record_count"`
	UpdateTime  int64   `bson:"utime"`
}

func aggregateSummary(ctx context.Context, db *mongo.Database, tenantID int64, startDate, endDate string) ([]summaryRow, error) {
	match := bson.M{}
	if tenantID != 0 {
		match["tenant_id"] = tenantID
	}
	if startDate != "" && endDate != "" {
		match["billing_date"] = bson.M{"$gte": startDate, "$lte": endDate}
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "billing_date", Value: "$billing_date"},
				{Key: "tenant_id", Value: "$tenant_id"},
				{Key: "provider", Value: "$provider"},
				{Key: "account_id", Value: "$account_id"},
				{Key: "service_type", Value: "$service_type"},
				{Key: "region", Value: "$region"},
			}},
			{Key: "amount", Value: bson.D{{Key: "$sum", Value: "$amount"}}},
			{Key: "amount_cny", Value: bson.D{{Key: "$sum", Value: "$amount_cny"}}},
			{Key: "record_count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := db.Collection(unifiedCollection).Aggregate(ctx, pipeline,
		options.Aggregate().SetAllowDiskUse(true).SetBatchSize(5000))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	now := time.Now().Unix()
	rows := make([]summaryRow, 0, 1000)
	for cursor.Next(ctx) {
		var r struct {
			ID struct {
				BillingDate string `bson:"billing_date"`
				TenantID    int64  `bson:"tenant_id"`
				Provider    string `bson:"provider"`
				AccountID   int64  `bson:"account_id"`
				ServiceType string `bson:"service_type"`
				Region      string `bson:"region"`
			} `bson:"_id"`
			Amount      float64 `bson:"amount"`
			AmountCNY   float64 `bson:"amount_cny"`
			RecordCount int64   `bson:"record_count"`
		}
		if err := cursor.Decode(&r); err != nil {
			continue
		}
		rows = append(rows, summaryRow{
			BillingDate: r.ID.BillingDate,
			TenantID:    r.ID.TenantID,
			Provider:    r.ID.Provider,
			AccountID:   r.ID.AccountID,
			ServiceType: r.ID.ServiceType,
			Region:      r.ID.Region,
			Amount:      r.Amount,
			AmountCNY:   r.AmountCNY,
			RecordCount: r.RecordCount,
			UpdateTime:  now,
		})
	}
	return rows, cursor.Err()
}

func insertSummary(ctx context.Context, db *mongo.Database, rows []summaryRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	docs := make([]interface{}, len(rows))
	for i := range rows {
		docs[i] = rows[i]
	}
	res, err := db.Collection(summaryCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(res.InsertedIDs)), nil
}
