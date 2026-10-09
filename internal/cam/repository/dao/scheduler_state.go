// Package dao scheduler_state 持久化日闸数据访问。
//
// 文件：internal/cam/repository/dao/scheduler_state.go
//
// 作用：为调度器提供「每日一次」触发的持久化闸门存储(spec「持久化日闸 +
// 原子认领」,修 CDN 内存闸 lastMetricsCollectDate 的重启重复提交缺陷)。
//
// 文档结构:{resource_type, last_date, updated_at},按 resource_type 分键
// (nas/cdn 独立,互不覆盖——Hard Rule)。resource_type 建唯一索引:upsert
// 并发竞争时靠唯一索引挡重复插入。
//
// 日期一律 Asia/Shanghai YYYY-MM-DD 字符串(与 metricCollectDate 口径一致):
// 同格式日期的字典序与时间序一致,mongo $lt 可直接做「last_date < today」比较。
package dao

import (
	"context"
	"errors"
	"time"

	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const SchedulerStateCollection = "ecam_scheduler_state"

// SchedulerStateDAO 持久化日闸数据访问接口
type SchedulerStateDAO interface {
	// TryClaimDaily 原子认领 resource_type 当日(date)触发权:
	// 一次 findOneAndUpdate(条件 resource_type=? AND last_date<date,更新为 date,
	// upsert)。认领成功(本实例把 last_date 推进为 date)返回 true;已被其他实例
	// 认领或当日已认领返回 false,nil。多副本/手动+自动重叠时同一资源只被一个
	// 实例认领(Hard Rule:原子认领是唯一提交入口)。
	TryClaimDaily(ctx context.Context, resourceType, date string) (bool, error)
	// GetLastDate 读取 resource_type 当前 last_date;无记录返回 ""(首次认领过渡:
	// 视为首次认领,认领后触发一次当日提交,不回溯补采历史)。
	GetLastDate(ctx context.Context, resourceType string) (string, error)
}

type schedulerStateDAO struct {
	db *mongox.Mongo
}

// NewSchedulerStateDAO 创建持久化日闸 DAO,并确保 resource_type 唯一索引
// (upsert 并发竞争靠唯一索引挡重复插入,见 TryClaimDaily 的重复键重试)。
func NewSchedulerStateDAO(db *mongox.Mongo) SchedulerStateDAO {
	d := &schedulerStateDAO{db: db}
	col := db.Collection(SchedulerStateCollection)
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "resource_type", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	return d
}

func (d *schedulerStateDAO) TryClaimDaily(ctx context.Context, resourceType, date string) (bool, error) {
	col := d.db.Collection(SchedulerStateCollection)
	filter := bson.M{
		"resource_type": resourceType,
		"last_date":     bson.M{"$lt": date},
	}
	update := bson.M{
		"$set": bson.M{
			"last_date":  date,
			"updated_at": time.Now(),
		},
		// 首次认领(无记录):filter 中 $lt 无法提取等值条件,$setOnInsert 保证
		// 新文档 resource_type 完整(unique 索引依赖)。
		"$setOnInsert": bson.M{"resource_type": resourceType},
	}
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)

	// 并发首次认领:两个实例同时 upsert 时,后到的插入撞唯一索引(重复键)。
	// 重试一次即命中对方已插入的文档走正常更新路径。
	for attempt := 0; attempt < 2; attempt++ {
		err := col.FindOneAndUpdate(ctx, filter, update, opts).Err()
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, mongo.ErrNoDocuments):
			// 已有更新的 last_date(他人已认领或当日已认领),认领失败
			return false, nil
		case mongo.IsDuplicateKeyError(err):
			continue
		default:
			return false, err
		}
	}
	return false, errors.New("scheduler_state upsert 重复键重试耗尽")
}

func (d *schedulerStateDAO) GetLastDate(ctx context.Context, resourceType string) (string, error) {
	var doc struct {
		LastDate string `bson:"last_date"`
	}
	err := d.db.Collection(SchedulerStateCollection).
		FindOne(ctx, bson.M{"resource_type": resourceType}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return doc.LastDate, nil
}
