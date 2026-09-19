package dao

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const NASMetricCollection = "ecam_nas_metric"

// NASMetricDAO NAS 文件系统单日容量指标数据访问接口
type NASMetricDAO interface {
	// UpsertMetric 按 (account_id, fs_id, date) 唯一键幂等写入单日指标。
	// 写入路径先过数量级自检:capacity=0 例外放行并强制打 qc_status=zero_exception
	// (不拦截不跳过);非零 capacity 越出 [1MB, 1PB] 拒绝并报错(错误携带 fs_id/date)。
	// 「今日行首写生效 / 昨日行覆盖更新」的采集语义由采集执行器落实,DAO 层只保证
	// 唯一键幂等与数据质量门禁。
	UpsertMetric(ctx context.Context, m types.NASMetric) error
	// BulkUpsertMetrics 批量幂等写入单日指标(mongo BulkWrite + upsert),唯一键
	// 与 UpsertMetric 一致;空批直接返回 nil;任一行未过数量级自检则整批拒绝
	// (不良行不得落库,错误携带 fs_id 供执行器失败归因);BulkWrite 错误原样回传。
	BulkUpsertMetrics(ctx context.Context, metrics []types.NASMetric) error
}

type nasMetricDAO struct {
	db *mongox.Mongo
}

// NewNASMetricDAO 创建 NAS 指标 DAO,并确保 (account_id, fs_id, date) 唯一索引。
//
// 注意:唯一键必须含 account_id——同一文件系统可被多家云账号纳管(多活/共享
// NAS 实例),(fs_id, date) 作为唯一键会让后写的账号覆盖先写账号的行,共享
// 文件系统的容量视图丢失一方数据(复用 CDN 指标表 (account_id, domain, date)
// 的多账号修复经验)。集合为本 feature 新建,无历史索引需清理。
func NewNASMetricDAO(db *mongox.Mongo) NASMetricDAO {
	d := &nasMetricDAO{db: db}
	col := db.Collection(NASMetricCollection)
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "account_id", Value: 1},
			{Key: "fs_id", Value: 1},
			{Key: "date", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	return d
}

// nasBytesPerGB 字节 → GB(二进制 GiB)换算分母(与采集边界换算同口径)。
const nasBytesPerGB = 1024 * 1024 * 1024

// 数量级自检区间(规格:非零 capacity 落在 [1MB, 1PB] 区间),以 GB(二进制 GiB) 计
var (
	nasCapacityMinGB = float64(1024*1024) / float64(nasBytesPerGB)                // 1MiB
	nasCapacityMaxGB = float64(1024*1024*1024*1024*1024) / float64(nasBytesPerGB) // 1PiB
)

// nasMetricQC 写入路径数量级自检与数据质量打标(spec「单位归一化与字段语义」):
//   - capacity=0:例外放行(不拦截不跳过),强制打 qc_status=zero_exception 后
//     正常落库——华为/AWS 实盘现状即 capacity=0,拦截/跳过会让首日整表静默为空;
//   - 非零 capacity 越出 [1MB, 1PB]:拒绝写入并报错——该形态即「字节直写 GB 字段」
//     的单位 bug(sync_nas.go 数据质量 bug 根源,新链路不得复刻),门禁保证落库
//     每行可反向验证数量级正确;
//   - 其余:原样放行(保留适配器已打的 qc_status 标注)。
//
// 自检只针对 capacity(规格口径);used>capacity 的收敛与 utilization 派生是
// 读取侧语义,不在写入门禁范围。
func nasMetricQC(m types.NASMetric) (types.NASMetric, error) {
	switch {
	case m.Capacity == 0:
		m.QcStatus = types.NASMetricQcZeroException
		return m, nil
	case m.Capacity < nasCapacityMinGB || m.Capacity > nasCapacityMaxGB:
		return types.NASMetric{}, fmt.Errorf(
			"nas metric capacity out of range [1MB,1PB]: fs_id=%s date=%s capacity=%g (自查单位换算:适配器须在采集边界完成字节→GB)",
			m.FsID, m.Date, m.Capacity)
	default:
		return m, nil
	}
}

func (d *nasMetricDAO) UpsertMetric(ctx context.Context, m types.NASMetric) error {
	qc, err := nasMetricQC(m)
	if err != nil {
		return err
	}
	_, err = d.db.Collection(NASMetricCollection).UpdateOne(ctx, nasMetricFilter(qc), nasMetricUpsertUpdate(qc), options.Update().SetUpsert(true))
	return err
}

// BulkUpsertMetrics 批量幂等写入单日指标,替代逐条 UpsertMetric 降低写放大。
// 逐行先过数量级自检,任一行越界则整批拒绝(不良行不得落库);
// filter 与 UpsertMetric 同唯一键 (account_id, fs_id, date),upsert 覆盖语义,
// 整批可安全重放(同键二次写入只覆盖不新增);空批直接返回 nil 不触碰数据库。
func (d *nasMetricDAO) BulkUpsertMetrics(ctx context.Context, metrics []types.NASMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(metrics))
	for _, m := range metrics {
		qc, err := nasMetricQC(m)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(nasMetricFilter(qc)).
			SetUpdate(nasMetricUpsertUpdate(qc)).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.db.Collection(NASMetricCollection).BulkWrite(ctx, models, opts)
	return err
}

// nasMetricFilter 以 (account_id, fs_id, date) 唯一键定位单日指标行:
// 同一文件系统可被多家云账号纳管(多活/共享实例),各账号同日数据独立保留,互不覆盖。
func nasMetricFilter(m types.NASMetric) bson.M {
	return bson.M{
		"account_id": m.AccountID,
		"fs_id":      m.FsID,
		"date":       m.Date,
	}
}

// nasMetricUpsertUpdate 单日指标覆盖字段($set 重采值,$setOnInsert 补齐键字段)
func nasMetricUpsertUpdate(m types.NASMetric) bson.M {
	return bson.M{
		"$set": bson.M{
			"capacity":      m.Capacity,
			"used_capacity": m.UsedCapacity,
			"qc_status":     m.QcStatus,
			"fs_name":       m.FsName,
			"account_id":    m.AccountID,
			"provider":      m.Provider,
		},
		"$setOnInsert": bson.M{
			"fs_id": m.FsID,
			"date":  m.Date,
		},
	}
}
