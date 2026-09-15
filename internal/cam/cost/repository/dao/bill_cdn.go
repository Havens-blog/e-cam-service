package dao

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CDNServiceTypeNameRegex CDN 账单的 service_type_name 过滤正则。
// 关键背景:实测 CDN 账单中约 64% 金额 service_type=other,无法用 service_type
// 等值过滤圈定,必须按 service_type_name 正则匹配;裸前缀 ^cdn 会误伤
// "cdnbilling",故附加 \b 词边界("cdn"/"p_cdn"/"dcdn" 精确命中,后跟
// 非单词字符亦可,如 "cdn-pro")。
const CDNServiceTypeNameRegex = `^(cdn|p_cdn|dcdn)\b`

// NewCDNBillDAO 创建 CDN 账单聚合 DAO(复用 billDAO 底层集合 ecam_cost_unified_bill)
func NewCDNBillDAO(db *mongox.Mongo) repository.CDNBillQuerier {
	return &billDAO{db: db}
}

// AggregateByServiceTypeName 按 service_type_name 正则圈定 CDN 账单后,按指定字段
// 聚合金额(amount / amount_cny 求和,按 amount_cny 降序)。
// 常用 field: "account_id"(账号占比)、"provider"(厂商分布)。
func (d *billDAO) AggregateByServiceTypeName(ctx context.Context, tenantID int64, field, startDate, endDate string) ([]repository.AggregateResult, error) {
	match := bson.M{
		"tenant_id":         tenantID,
		"billing_date":      bson.M{"$gte": startDate, "$lte": endDate},
		"service_type_name": bson.M{"$regex": CDNServiceTypeNameRegex},
	}
	pipeline := bson.A{
		bson.M{"$match": match},
		bson.M{"$group": bson.M{
			"_id":        "$" + field,
			"amount":     bson.M{"$sum": "$amount"},
			"amount_cny": bson.M{"$sum": "$amount_cny"},
		}},
		bson.M{"$sort": bson.M{"amount_cny": -1}},
	}
	return aggregateCDNResults(ctx, d.db.Collection(UnifiedBillCollection), pipeline)
}

// AggregateCDNMonthly 按「月份 × service_type_name」聚合 CDN 账单金额(amount_cny)。
// 月份取 billing_date 前 7 位(YYYY-MM)。
func (d *billDAO) AggregateCDNMonthly(ctx context.Context, tenantID int64, startDate, endDate string) ([]repository.CDNMonthlyRow, error) {
	match := bson.M{
		"tenant_id":         tenantID,
		"billing_date":      bson.M{"$gte": startDate, "$lte": endDate},
		"service_type_name": bson.M{"$regex": CDNServiceTypeNameRegex},
	}
	pipeline := bson.A{
		bson.M{"$match": match},
		bson.M{"$group": bson.M{
			"_id": bson.M{
				"month":             bson.M{"$substrCP": bson.A{"$billing_date", 0, 7}},
				"service_type_name": "$service_type_name",
			},
			"amount_cny": bson.M{"$sum": "$amount_cny"},
		}},
		bson.M{"$sort": bson.M{"_id.month": 1, "_id.service_type_name": 1}},
	}

	cursor, err := d.db.Collection(UnifiedBillCollection).Aggregate(ctx, pipeline,
		options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, fmt.Errorf("cdn monthly aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Key struct {
			Month           string `bson:"month"`
			ServiceTypeName string `bson:"service_type_name"`
		} `bson:"_id"`
		AmountCNY float64 `bson:"amount_cny"`
	}
	if err = cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("cdn monthly aggregate decode: %w", err)
	}

	results := make([]repository.CDNMonthlyRow, 0, len(rows))
	for _, r := range rows {
		results = append(results, repository.CDNMonthlyRow{
			Month:           r.Key.Month,
			ServiceTypeName: r.Key.ServiceTypeName,
			AmountCNY:       r.AmountCNY,
		})
	}
	return results, nil
}

// aggregateCDNResults 执行聚合管道并解码为 AggregateResult。
// _id 可能是数值型(如 account_id),不能直接解码进 string 字段,先收 interface{} 再转。
func aggregateCDNResults(ctx context.Context, coll *mongo.Collection, pipeline bson.A) ([]repository.AggregateResult, error) {
	cursor, err := coll.Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, fmt.Errorf("cdn aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Key       interface{} `bson:"_id"`
		Amount    float64     `bson:"amount"`
		AmountCNY float64     `bson:"amount_cny"`
	}
	if err = cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("cdn aggregate decode: %w", err)
	}

	results := make([]repository.AggregateResult, 0, len(rows))
	for _, r := range rows {
		results = append(results, repository.AggregateResult{
			Key:       aggregateKeyToString(r.Key),
			Amount:    r.Amount,
			AmountCNY: r.AmountCNY,
		})
	}
	return results, nil
}

// aggregateKeyToString 将聚合 _id(可能是 string / int32 / int64 / null)转为 string
func aggregateKeyToString(v interface{}) string {
	switch k := v.(type) {
	case nil:
		return ""
	case string:
		return k
	case int32:
		return fmt.Sprintf("%d", k)
	case int64:
		return fmt.Sprintf("%d", k)
	default:
		return fmt.Sprintf("%v", k)
	}
}
