package dao

import (
	"context"
	"fmt"

	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const OSSMetricCollection = "ecam_oss_metric"

// OSSMetricDAO OSS 存储桶单日容量指标数据访问接口
type OSSMetricDAO interface {
	// UpsertMetric 按 (account_id, bucket_name, date) 唯一键幂等写入单日指标。
	// 写入路径先过数量级自检:storage_size=0 例外放行并强制打 qc_status=zero_exception
	// (不拦截不跳过);非零 storage_size 越出 [1MB, 1PB] 拒绝并报错(错误携带
	// bucket_name/date)。「今日行首写生效 / 昨日行覆盖更新」的采集语义由采集执行器
	// 落实,DAO 层只保证唯一键幂等与数据质量门禁。
	UpsertMetric(ctx context.Context, m types.OSSMetric) error
	// BulkUpsertMetrics 批量幂等写入单日指标(mongo BulkWrite + upsert),唯一键
	// 与 UpsertMetric 一致;空批直接返回 nil;任一行未过数量级自检则整批拒绝
	// (不良行不得落库,错误携带 bucket_name 供执行器失败归因);BulkWrite 错误原样回传。
	BulkUpsertMetrics(ctx context.Context, metrics []types.OSSMetric) error
	// BulkInsertIfAbsent 批量「首写生效」写入(采集执行器专用):更新文档仅含
	// $setOnInsert——唯一键 (account_id, bucket_name, date) 命中已存在行时不做任何
	// 修改(今日行当日已有则不覆盖),仅补缺失行;未命中则整行插入。空批直接返回 nil;
	// 数量级自检与 BulkUpsertMetrics 同口径,任一行未过则整批拒绝。
	BulkInsertIfAbsent(ctx context.Context, metrics []types.OSSMetric) error
	// CountMetricsByProviders 统计各厂商自 sinceDate(含当日,YYYY-MM-DD)以来
	// 已落库的指标行数(OSS 自我健康监控用:窗口内行存在即「成功采集」证据,
	// 与 NAS 同口径;date 字符串字典序即时间序)。
	CountMetricsByProviders(ctx context.Context, providers []string, sinceDate string) (map[string]int64, error)
	// ListByBucket 取指定账号+bucket_name 近 N 天单日指标,按 date 升序(趋势接口读取)。
	// days 缺省 30、上限收敛 90(读取窗口 1~90,规格「Proposed Solution」第 5 点)。
	ListByBucket(ctx context.Context, accountID int64, bucketName string, days int) ([]types.OSSMetric, error)
	// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合读取,服务层按
	// bucket_name 去重与分页)。accountIDs 为空返回空切片;days 缺省/上限同 ListByBucket。
	ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.OSSMetric, error)
}

type ossMetricDAO struct {
	db *mongox.Mongo
}

// NewOSSMetricDAO 创建 OSS 指标 DAO,并确保 (account_id, bucket_name, date) 唯一索引。
//
// 注意:唯一键必须含 account_id(Hard Rule)——同一 bucket 可被多家云账号纳管
// (跨账号共享存储),(bucket_name, date) 作为唯一键会让后写的账号覆盖先写账号
// 的行,共享存储的容量视图丢失一方数据(复用 CDN 指标表 (account_id, domain, date)
// 的 3cea6d7 多账号修复经验)。集合为本 feature 新建,无历史索引需清理。
func NewOSSMetricDAO(db *mongox.Mongo) OSSMetricDAO {
	d := &ossMetricDAO{db: db}
	col := db.Collection(OSSMetricCollection)
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "account_id", Value: 1},
			{Key: "bucket_name", Value: 1},
			{Key: "date", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	return d
}

// ossBytesPerGB 字节 → GB(二进制 GiB)换算分母(与采集边界换算同口径;
// 适配器换算统一走共享 types.BytesToGB,此处分母仅供门禁区间定义)。
const ossBytesPerGB = 1024 * 1024 * 1024

// 数量级自检区间(规格:非零 storage_size 落在 [1MB, 1PB] 区间),以 GB(二进制 GiB) 计
var (
	ossStorageMinGB = float64(1024*1024) / float64(ossBytesPerGB)                // 1MiB
	ossStorageMaxGB = float64(1024*1024*1024*1024*1024) / float64(ossBytesPerGB) // 1PiB
)

// ossMetricQC 写入路径数量级自检与数据质量打标(proposal「单位归一化」):
//   - storage_size=0:例外放行(不拦截不跳过),强制打 qc_status=zero_exception 后
//     正常落库——实盘空桶/未开通统计的桶容量即 0,拦截/跳过会让首日整表静默为空;
//   - 非零 storage_size 越出 [1MB, 1PB]:拒绝写入并报错——该形态即「字节直写 GB
//     字段」的单位 bug,门禁保证落库每行可反向验证数量级正确;
//   - 其余:原样放行(保留适配器已打的 qc_status 标注)。
//
// 自检只针对 storage_size(规格口径);utilization 是读取侧派生语义,不在写入门禁
// 范围(Hard Rule:utilization 不落库)。
func ossMetricQC(m types.OSSMetric) (types.OSSMetric, error) {
	switch {
	case m.StorageSize == 0:
		m.QcStatus = types.OSSMetricQcZeroException
		return m, nil
	case m.StorageSize < ossStorageMinGB || m.StorageSize > ossStorageMaxGB:
		return types.OSSMetric{}, fmt.Errorf(
			"oss metric storage_size out of range [1MB,1PB]: bucket_name=%s date=%s storage_size=%g (自查单位换算:适配器须在采集边界完成字节→GB)",
			m.BucketName, m.Date, m.StorageSize)
	default:
		return m, nil
	}
}

func (d *ossMetricDAO) UpsertMetric(ctx context.Context, m types.OSSMetric) error {
	qc, err := ossMetricQC(m)
	if err != nil {
		return err
	}
	_, err = d.db.Collection(OSSMetricCollection).UpdateOne(ctx, ossMetricFilter(qc), ossMetricUpsertUpdate(qc), options.Update().SetUpsert(true))
	return err
}

// BulkUpsertMetrics 批量幂等写入单日指标,替代逐条 UpsertMetric 降低写放大。
// 逐行先过数量级自检,任一行越界则整批拒绝(不良行不得落库);
// filter 与 UpsertMetric 同唯一键 (account_id, bucket_name, date),upsert 覆盖语义,
// 整批可安全重放(同键二次写入只覆盖不新增);空批直接返回 nil 不触碰数据库。
func (d *ossMetricDAO) BulkUpsertMetrics(ctx context.Context, metrics []types.OSSMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(metrics))
	for _, m := range metrics {
		qc, err := ossMetricQC(m)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(ossMetricFilter(qc)).
			SetUpdate(ossMetricUpsertUpdate(qc)).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.db.Collection(OSSMetricCollection).BulkWrite(ctx, models, opts)
	return err
}

// BulkInsertIfAbsent 批量「首写生效」写入:filter 定位唯一键行,更新文档仅含
// $setOnInsert(upsert)——命中已存在行时不修改任何字段(首写保护,今日行当日
// 已有则不覆盖;昨日行由次日补采覆盖更新,语义由执行器选择写入方法落实),未命中
// 才整行插入;同键二次写入天然幂等且不产生脏行。整批 ordered=false 提升吞吐。
func (d *ossMetricDAO) BulkInsertIfAbsent(ctx context.Context, metrics []types.OSSMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(metrics))
	for _, m := range metrics {
		qc, err := ossMetricQC(m)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(ossMetricFilter(qc)).
			SetUpdate(bson.M{"$setOnInsert": ossMetricInsertDoc(qc)}).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.db.Collection(OSSMetricCollection).BulkWrite(ctx, models, opts)
	return err
}

// ossMetricInsertDoc 首写生效的插入文档($setOnInsert 全字段:插入时补齐整行)
func ossMetricInsertDoc(m types.OSSMetric) bson.M {
	return bson.M{
		"account_id":   m.AccountID,
		"bucket_name":  m.BucketName,
		"date":         m.Date,
		"storage_size": m.StorageSize,
		"object_count": m.ObjectCount,
		"qc_status":    m.QcStatus,
		"provider":     m.Provider,
	}
}

// ossMetricFilter 以 (account_id, bucket_name, date) 唯一键定位单日指标行:
// 同一 bucket 可被多家云账号纳管(跨账号共享存储),各账号同日数据独立保留,互不覆盖。
func ossMetricFilter(m types.OSSMetric) bson.M {
	return bson.M{
		"account_id":  m.AccountID,
		"bucket_name": m.BucketName,
		"date":        m.Date,
	}
}

// ossMetricUpsertUpdate 单日指标覆盖字段($set 重采值,$setOnInsert 补齐键字段)
func ossMetricUpsertUpdate(m types.OSSMetric) bson.M {
	return bson.M{
		"$set": bson.M{
			"storage_size": m.StorageSize,
			"object_count": m.ObjectCount,
			"qc_status":    m.QcStatus,
			"bucket_name":  m.BucketName,
			"account_id":   m.AccountID,
			"provider":     m.Provider,
		},
		"$setOnInsert": bson.M{
			"date": m.Date,
		},
	}
}

// CountMetricsByProviders 统计各厂商自 sinceDate(含当日)以来已落库的指标行数。
// 逐厂商 CountDocuments(必达厂商仅 3 家,无需聚合管道);date 字符串按
// YYYY-MM-DD 字典序比较即时间序,与写入路径格式一致(与 NAS 同口径)。
func (d *ossMetricDAO) CountMetricsByProviders(ctx context.Context, providers []string, sinceDate string) (map[string]int64, error) {
	out := make(map[string]int64, len(providers))
	for _, p := range providers {
		n, err := d.db.Collection(OSSMetricCollection).CountDocuments(ctx, bson.M{
			"provider": p,
			"date":     bson.M{"$gte": sinceDate},
		})
		if err != nil {
			return nil, fmt.Errorf("统计厂商 %s OSS 指标行数失败: %w", p, err)
		}
		out[p] = n
	}
	return out, nil
}
