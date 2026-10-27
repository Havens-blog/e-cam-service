//go:build ignore
// +build ignore

// Backfill script: 华为云历史账单补归一化（raw_bill → unified_bill）。
//
// Background
// ----------
// 成本统计「只显示阿里云」的根因之一是：华为云 ecam_cost_raw_bill 有 29.7 万条
// 原始账单（2026-02 月），但 ecam_cost_unified_bill 为 0 条——那批原始账单从未被
// 归一化写入统一账单，之后又因华为云账号缺 bss:billDetail:view 权限无法重新采集
// （223 次采集失败），导致华为云成本在页面彻底缺失。
//
// 本脚本从 raw_bill 读取华为云原始账单，复用线上 normalizer 的 service_type 映射
// 与币种转换（保证与正常采集产出一致），重建统一账单并写入 unified_bill。
// 幂等：按「账号 + 月份」先删后插，可安全重跑。
//
// 安全性
//   * 默认 dry-run，只读 + 预览，不写库、不消耗 ID。加 -apply 才真正执行。
//   * ID 复用线上 ecam_id_generator 集合原子自增（与 DAO InsertUnifiedBills 一致），
//     避免与正常采集插入的账单 ID 冲突。
//   * 只处理华为云（provider=huawei, account_id=2），不碰其他云数据。
//   * 先删后插：-apply 时先删除该账号该月份的旧 unified_bill，再流式写入新账单，
//     避免重复（重跑幂等）。
//
// Usage
//   # Dry-run（默认）：预览归一化结果与统计，不写库。
//   go run scripts/backfill_huawei_unified_bill.go
//
//   # Apply：真正执行（先删后插 + 写入）。
//   go run scripts/backfill_huawei_unified_bill.go -apply
//
// Flags
//   -mongo     MongoDB URI    (default: prod ecam)
//   -db        Database name  (default: ecam)
//   -user      Username       (default: ecmdb)
//   -password  Password       (default: 123456)
//   -account   Huawei account id (default: 2)
//   -apply     Enable mutation   (default: false = dry-run)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/normalizer"
	shareddomain "github.com/Havens-blog/e-cloudx-sdk/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	rawCollection     = "ecam_cost_raw_bill"
	unifiedCollection = "ecam_cost_unified_bill"
	idGeneratorColl   = "ecam_id_generator"
	batchSize         = 1000
)

var (
	mongoURI  = flag.String("mongo", "mongodb://106.52.187.69:27017/ecam?authSource=admin", "MongoDB URI")
	database  = flag.String("db", "ecam", "Database name")
	username  = flag.String("user", "ecmdb", "MongoDB username")
	password  = flag.String("password", "123456", "MongoDB password")
	accountID = flag.Int64("account", 2, "Huawei cloud account id")
	apply     = flag.Bool("apply", false, "Apply the backfill (default: dry-run, preview only)")
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// 构建带凭据的 URI
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

	// 1. 读华为云账号（拿 name + tenant_id）
	acc, err := loadAccount(ctx, db, *accountID)
	if err != nil {
		log.Fatalf("load account %d: %v", *accountID, err)
	}
	fmt.Printf("账号: id=%d provider=%s name=%s tenant_id=%d\n", acc.ID, acc.Provider, acc.Name, acc.TenantID)
	if acc.Provider != string(shareddomain.CloudProviderHuawei) {
		log.Fatalf("账号 %d 的 provider 是 %q，不是 huawei，拒绝处理（防止误写其他云）", acc.ID, acc.Provider)
	}

	// 2. 复用线上 normalizer 的映射器与币种配置
	mapper := normalizer.NewServiceTypeMapper()
	currency := normalizer.NewCurrencyConfig(normalizer.DefaultUSDToCNYRate)

	// 3. 先查 raw_bill 覆盖的月份（用于先删后插的去重键）
	months, err := distinctMonths(ctx, db, *accountID)
	if err != nil {
		log.Fatalf("distinct months: %v", err)
	}
	fmt.Printf("raw_bill 覆盖月份: %v\n\n", months)
	if len(months) == 0 {
		fmt.Println("没有华为云 raw_bill 数据，无需处理。")
		return
	}

	// 4. 先删后插：-apply 时，先删除这些月份的旧 unified_bill（幂等，重跑不重复）
	if *apply {
		fmt.Println("================ 删除旧 unified_bill（先删后插）================")
		for _, m := range months {
			res, err := db.Collection(unifiedCollection).DeleteMany(ctx, bson.M{
				"account_id":   *accountID,
				"billing_date": bson.M{"$regex": "^" + m},
			})
			if err != nil {
				log.Fatalf("delete old unified bills for month %s: %v", m, err)
			}
			fmt.Printf("  已删除 %s 旧 unified_bill: %d 条\n", m, res.DeletedCount)
		}
		fmt.Println()
	}

	// 5. 流式读华为云 raw_bill，逐条归一化并（-apply 时）写入
	fmt.Printf("开始读取 provider=huawei, account_id=%d 的 raw_bill...\n", *accountID)

	type monthStat struct {
		count     int
		amountCNY float64
	}
	monthStats := map[string]*monthStat{}
	svcStats := map[string]int{}
	var totalRead, totalNormalized, totalSkipped int64
	var totalWritten int64

	rawTotal, _ := db.Collection(rawCollection).CountDocuments(ctx, bson.M{
		"provider":   string(shareddomain.CloudProviderHuawei),
		"account_id": *accountID,
	})
	fmt.Printf("raw_bill 总量: %d 条\n\n", rawTotal)

	cursor, err := db.Collection(rawCollection).Find(ctx, bson.M{
		"provider":   string(shareddomain.CloudProviderHuawei),
		"account_id": *accountID,
	}, options.Find().SetBatchSize(int32(batchSize)).SetSort(bson.D{{Key: "id", Value: 1}}))
	if err != nil {
		log.Fatalf("find raw bills: %v", err)
	}
	defer cursor.Close(ctx)

	var unifiedBills []domain.UnifiedBill

	for cursor.Next(ctx) {
		var raw domain.RawBillRecord
		if err := cursor.Decode(&raw); err != nil {
			log.Printf("decode raw bill failed (skip): %v", err)
			totalSkipped++
			continue
		}
		totalRead++

		bill, month, ok := normalizeHuawei(raw, acc, mapper, currency)
		if !ok {
			totalSkipped++
			continue
		}
		totalNormalized++

		ms, exists := monthStats[month]
		if !exists {
			ms = &monthStat{}
			monthStats[month] = ms
		}
		ms.count++
		ms.amountCNY += bill.AmountCNY
		svcStats[bill.ServiceType]++

		unifiedBills = append(unifiedBills, bill)

		if len(unifiedBills) >= batchSize {
			if *apply {
				n, err := flushBatch(ctx, db, unifiedBills)
				if err != nil {
					log.Fatalf("flush batch: %v", err)
				}
				totalWritten += n
			}
			unifiedBills = unifiedBills[:0]
			fmt.Printf("\r  进度: %d / %d 条已处理...", totalRead, rawTotal)
		}
	}
	if err := cursor.Err(); err != nil {
		log.Fatalf("cursor: %v", err)
	}
	if len(unifiedBills) > 0 {
		if *apply {
			n, err := flushBatch(ctx, db, unifiedBills)
			if err != nil {
				log.Fatalf("flush final batch: %v", err)
			}
			totalWritten += n
		}
		unifiedBills = unifiedBills[:0]
	}
	fmt.Printf("\r  进度: %d / %d 条已处理。\n\n", totalRead, rawTotal)

	// 6. 重建汇总表：成本查询走 ecam_cost_daily_summary 预聚合，只写明细不重建
	// 汇总时页面查询不到新数据。汇总重建由独立脚本完成（职责单一、可复用）：
	//   go run scripts/rebuild_cost_summary.go -apply   （按租户/日期范围重建）

	// 7. 报告
	fmt.Printf("================ 归一化报告 ================\n")
	fmt.Printf("读取 raw_bill: %d 条\n", totalRead)
	fmt.Printf("成功归一化:   %d 条\n", totalNormalized)
	fmt.Printf("跳过(异常):   %d 条\n", totalSkipped)
	if *apply {
		fmt.Printf("已写入 unified_bill: %d 条\n", totalWritten)
	}
	fmt.Printf("\n按月分布:\n")
	var grandAmount float64
	for m, ms := range monthStats {
		grandAmount += ms.amountCNY
		fmt.Printf("  %s : %d 条, 合计 ¥%.2f\n", m, ms.count, ms.amountCNY)
	}
	fmt.Printf("  合计: ¥%.2f\n\n", grandAmount)

	fmt.Printf("按统一服务类型分布:\n")
	for st, cnt := range svcStats {
		fmt.Printf("  %-12s : %d 条\n", st, cnt)
	}

	fmt.Printf("\n%s\n", map[bool]string{true: "✅ 已完成写入", false: "DRY-RUN 完成（未写库，加 -apply 执行）"}[*apply])
}

// accountInfo 华为云账号关键字段
type accountInfo struct {
	ID       int64
	Provider string
	Name     string
	TenantID int64
}

func loadAccount(ctx context.Context, db *mongo.Database, id int64) (accountInfo, error) {
	var doc bson.M
	err := db.Collection("ecam_cloud_account").FindOne(ctx, bson.M{"id": id}).Decode(&doc)
	if err != nil {
		return accountInfo{}, err
	}
	return accountInfo{
		ID:       id,
		Provider: strVal(doc["provider"]),
		Name:     strVal(doc["name"]),
		TenantID: intVal(doc["tenant_id"]),
	}, nil
}

// distinctMonths 返回 raw_bill 中该账号覆盖的去重月份列表（按 billing_date 前 7 位）。
func distinctMonths(ctx context.Context, db *mongo.Database, accountID int64) ([]string, error) {
	vals, err := db.Collection(rawCollection).Distinct(ctx, "billing_date", bson.M{
		"provider":   string(shareddomain.CloudProviderHuawei),
		"account_id": accountID,
	})
	if err != nil {
		return nil, err
	}
	months := make([]string, 0, len(vals))
	seen := map[string]bool{}
	for _, v := range vals {
		s := strVal(v)
		if len(s) >= 7 {
			m := s[:7] // "2026-02"
			if !seen[m] {
				seen[m] = true
				months = append(months, m)
			}
		}
	}
	return months, nil
}

// normalizeHuawei 将华为云 raw_bill 记录归一化为 UnifiedBill。
// 复用线上 normalizer 的 mapper 与 currency，与正常采集产出保持一致。
// 返回 (bill, month("2026-02"), ok)。month 用于先删后插的去重键。
func normalizeHuawei(
	raw domain.RawBillRecord,
	acc accountInfo,
	mapper *normalizer.ServiceTypeMapper,
	currency *normalizer.CurrencyConfig,
) (domain.UnifiedBill, string, bool) {
	rd := raw.RawData
	if rd == nil {
		return domain.UnifiedBill{}, "", false
	}

	// 华为云字段（与 e-cloudx-sdk/billing/huawei/adapter.go parseBillRecords 一致）
	rawServiceType := strVal(rd["CloudServiceType"])
	resourceID := strVal(rd["ResInstanceId"])
	resourceName := strVal(rd["ResourceName"])
	region := strVal(rd["Region"])
	amount := floatVal(rd["ConsumeAmount"])
	cycle := strings.TrimSpace(raw.BillingDate)
	if cycle == "" {
		cycle = strings.TrimSpace(strVal(rd["Cycle"]))
	}
	if cycle == "" {
		// 兜底：无法确定账期则跳过（避免 billing_date 错乱）
		return domain.UnifiedBill{}, "", false
	}

	// 归一化账期：cycle("2026-02") → billingStart=2026-02-01, billingEnd=2026-02 月末
	billingStart, billingEnd := parseCycle(cycle)
	billingDate := billingStart.Format("2006-01-02")

	// 统一服务类型 + 币种
	serviceType := mapper.Map(shareddomain.CloudProviderHuawei, rawServiceType)
	curr := currency.Convert(shareddomain.CloudProviderHuawei, amount, "CNY")

	// 标签（与 adapter 的 tags 构建一致）
	tags := make(map[string]string)
	if tag := strVal(rd["ResourceTag"]); tag != "" {
		tags["raw_tag"] = tag
	}
	if epID := strVal(rd["EnterpriseProjectId"]); epID != "" {
		tags["enterprise_project_id"] = epID
	}
	if epName := strVal(rd["EnterpriseProjectName"]); epName != "" {
		tags["enterprise_project_name"] = epName
	}
	if len(tags) == 0 {
		tags = nil
	}

	now := time.Now().UnixMilli()
	bill := domain.UnifiedBill{
		Provider:        string(shareddomain.CloudProviderHuawei),
		AccountID:       acc.ID,
		AccountName:     acc.Name,
		BillingStart:    billingStart,
		BillingEnd:      billingEnd,
		ServiceType:     serviceType,
		ServiceTypeName: rawServiceType, // 与 normalizer 一致：存原始服务类型
		ResourceID:      resourceID,
		ResourceName:    resourceName,
		Region:          region,
		Amount:          amount,
		Currency:        curr.Currency,
		AmountCNY:       curr.AmountCNY,
		Tags:            tags,
		TenantID:        acc.TenantID,
		BillingDate:     billingDate,
		CreateTime:      now,
		UpdateTime:      now,
	}

	return bill, cycle, true
}

// flushBatch 批量写入 unified_bill（复用 ecam_id_generator 生成 ID），返回写入条数。
func flushBatch(ctx context.Context, db *mongo.Database, bills []domain.UnifiedBill) (int64, error) {
	if len(bills) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	docs := make([]interface{}, len(bills))
	for i := range bills {
		bills[i].CreateTime = now
		bills[i].UpdateTime = now
		bills[i].ID = nextID(ctx, db, unifiedCollection)
		if bills[i].ID == 0 {
			return 0, fmt.Errorf("generate id failed for bill index %d", i)
		}
		docs[i] = bills[i]
	}
	res, err := db.Collection(unifiedCollection).InsertMany(ctx, docs)
	if err != nil {
		return 0, err
	}
	return int64(len(res.InsertedIDs)), nil
}

// nextID 复用线上 ecam_id_generator 集合原子自增，与 DAO 一致。
func nextID(ctx context.Context, db *mongo.Database, name string) int64 {
	var result struct {
		NextID int64 `bson:"next_id"`
	}
	filter := bson.M{"name": name}
	update := bson.M{"$inc": bson.M{"next_id": int64(1)}}
	opt := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)
	if err := db.Collection(idGeneratorColl).FindOneAndUpdate(ctx, filter, update, opt).Decode(&result); err != nil {
		return 0
	}
	return result.NextID
}

// parseCycle 解析账期 "YYYY-MM" → 起止时间（复刻 normalizer.parseBillingCycle）。
func parseCycle(cycle string) (time.Time, time.Time) {
	cycle = strings.TrimSpace(cycle)
	if cycle == "" {
		now := time.Now().UTC()
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0).Add(-time.Second)
	}
	t, err := time.Parse("2006-01", cycle)
	if err != nil {
		now := time.Now().UTC()
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0).Add(-time.Second)
	}
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0).Add(-time.Second)
}

// strVal 安全地把 interface{} 转字符串。
func strVal(v interface{}) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", s)
	}
}

// floatVal 安全地把 interface{} 转 float64。
func floatVal(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

// intVal 安全地把 interface{} 转 int64。
func intVal(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}
