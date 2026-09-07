package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const StatsSnapshotCollection = "ecam_stats_snapshot"

// StatsSnapshot 通用统计日快照。
// 各统计接口(GetImageStats/GetTagStats/…)在返回统计值时惰性写入当日快照,
// 趋势 = 当前值 - 基线快照(7 天前当天或之前最近一条)。无定时任务依赖,
// 快照随使用逐步补齐;接口侧的取值失败一律容忍(趋势缺失 ≠ 接口失败)。
type StatsSnapshot struct {
	ID       int64                  `bson:"id"`
	Domain   string                 `bson:"domain"` // 统计域(含过滤维度),如 image:p=aliyun;a=3
	TenantID int64                  `bson:"tenant_id"`
	Date     string                 `bson:"date"`    // YYYY-MM-DD,按 Asia/Shanghai
	Metrics  map[string]interface{} `bson:"metrics"` // 数值型指标
	Ctime    int64                  `bson:"ctime"`
}

// StatsSnapshotDAO 统计快照数据访问接口
type StatsSnapshotDAO interface {
	// Upsert 按 (domain, tenant, date) 幂等写入当日快照,同日复查覆盖为最新值
	Upsert(ctx context.Context, snap StatsSnapshot) error
	// GetNearestOnOrBefore 返回 date(含)之前最近的一条快照;无基线返回 (nil, nil)
	GetNearestOnOrBefore(ctx context.Context, domain string, tenantID int64, date string) (*StatsSnapshot, error)
}

type statsSnapshotDAO struct {
	db *mongox.Mongo
}

// cstZone 统计快照按运营时区(Asia/Shanghai)取日,勿改用服务器本地时区
var cstZone = time.FixedZone("CST", 8*3600)

// SnapshotDate 按运营时区格式化快照日期(YYYY-MM-DD)
func SnapshotDate(t time.Time) string {
	return t.In(cstZone).Format("2006-01-02")
}

// Delta 计算当前指标相对本快照基线的净变化;基线缺失的指标按 0 处理
func (s *StatsSnapshot) Delta(current map[string]float64) map[string]float64 {
	res := make(map[string]float64, len(current))
	for k, v := range current {
		var base float64
		if s != nil {
			switch b := s.Metrics[k].(type) {
			case float64:
				base = b
			case int64:
				base = float64(b)
			case int32:
				base = float64(b)
			}
		}
		res[k] = v - base
	}
	return res
}

// NewStatsSnapshotDAO 创建统计快照 DAO,并确保唯一索引
func NewStatsSnapshotDAO(db *mongox.Mongo) StatsSnapshotDAO {
	d := &statsSnapshotDAO{db: db}
	_, _ = db.Collection(StatsSnapshotCollection).Indexes().CreateOne(
		context.Background(), mongo.IndexModel{
			Keys: bson.D{
				{Key: "domain", Value: 1},
				{Key: "tenant_id", Value: 1},
				{Key: "date", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		})
	return d
}

func (d *statsSnapshotDAO) Upsert(ctx context.Context, snap StatsSnapshot) error {
	if snap.ID == 0 {
		snap.ID = d.db.GetIdGenerator(StatsSnapshotCollection)
	}
	filter := bson.M{
		"domain":    snap.Domain,
		"tenant_id": snap.TenantID,
		"date":      snap.Date,
	}
	update := bson.M{
		"$set": bson.M{
			"metrics": snap.Metrics,
			"ctime":   snap.Ctime,
		},
		"$setOnInsert": bson.M{
			"id":        snap.ID,
			"domain":    snap.Domain,
			"tenant_id": snap.TenantID,
			"date":      snap.Date,
		},
	}
	_, err := d.db.Collection(StatsSnapshotCollection).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (d *statsSnapshotDAO) GetNearestOnOrBefore(ctx context.Context, domain string, tenantID int64, date string) (*StatsSnapshot, error) {
	filter := bson.M{
		"domain":    domain,
		"tenant_id": tenantID,
		"date":      bson.M{"$lte": date},
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "date", Value: -1}})
	var snap StatsSnapshot
	if err := d.db.Collection(StatsSnapshotCollection).FindOne(ctx, filter, opts).Decode(&snap); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}
