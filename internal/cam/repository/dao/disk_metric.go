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

const DiskMetricCollection = "ecam_disk_metric"

// DiskMetricDAO 云硬盘单日指标数据访问接口
type DiskMetricDAO interface {
	// UpsertMetric 按 (account_id, disk_id, date) 唯一键幂等写入单日指标。
	// 写入路径先过数据质量门禁:usage_percent=0 例外放行并强制打
	// qc_status=zero_exception(不拦截不跳过);usage_percent 越出 [0,100] 或
	// iops/throughput 为负拒绝并报错(错误携带 disk_id/date)。「今日行首写生效 /
	// 昨日行覆盖更新」的采集语义由采集执行器落实,DAO 层只保证唯一键幂等与
	// 数据质量门禁。
	UpsertMetric(ctx context.Context, m types.DiskMetric) error
	// BulkUpsertMetrics 批量幂等写入单日指标(mongo BulkWrite + upsert),唯一键
	// 与 UpsertMetric 一致;空批直接返回 nil;任一行未过门禁则整批拒绝(不良行
	// 不得落库,错误携带 disk_id 供执行器失败归因);BulkWrite 错误原样回传。
	BulkUpsertMetrics(ctx context.Context, metrics []types.DiskMetric) error
	// BulkInsertIfAbsent 批量「首写生效」写入(采集执行器专用):更新文档仅含
	// $setOnInsert——唯一键 (account_id, disk_id, date) 命中已存在行时不做任何
	// 修改(今日行当日已有则不覆盖),仅补缺失行;未命中则整行插入。空批直接
	// 返回 nil;门禁与 BulkUpsertMetrics 同口径,任一行未过则整批拒绝。
	BulkInsertIfAbsent(ctx context.Context, metrics []types.DiskMetric) error
	// CountMetricsByProviders 统计各厂商自 sinceDate(含当日,YYYY-MM-DD)以来
	// 已落库的指标行数(自我健康监控用:窗口内行存在即「成功采集」证据),
	// 与 NAS/OSS 同名同签名先例一致。
	CountMetricsByProviders(ctx context.Context, providers []string, sinceDate string) (map[string]int64, error)
	// ListByDisk 取指定账号+disk_id 近 N 天单日指标,按 date 升序(趋势接口读取)。
	ListByDisk(ctx context.Context, accountID int64, diskID string, days int) ([]types.DiskMetric, error)
	// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合读取,服务层按 disk_id
	// 去重与分页)。accountIDs 为空返回空切片;days 缺省/上限同 ListByDisk。
	ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.DiskMetric, error)
}

type diskMetricDAO struct {
	db *mongox.Mongo
}

// NewDiskMetricDAO 创建 Disk 指标 DAO,并确保 (account_id, disk_id, date) 唯一索引。
//
// 注意:唯一键必须含 account_id(Hard Rule)——同一云盘可被多家云账号纳管(共享盘,
// 多重挂载),(disk_id, date) 作为唯一键会让后写的账号覆盖先写账号的行,共享盘的
// 指标视图丢失一方数据(复用 CDN/NAS/OSS 指标表多账号修复经验,3cea6d7)。
// disk_id 是地域内唯一标识(与 NAS 的 fs_id 同型,Disk 是地域性资源而非 OSS 全局
// 服务);集合为本 feature 新建,无历史索引需清理。
func NewDiskMetricDAO(db *mongox.Mongo) DiskMetricDAO {
	d := &diskMetricDAO{db: db}
	col := db.Collection(DiskMetricCollection)
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "account_id", Value: 1},
			{Key: "disk_id", Value: 1},
			{Key: "date", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	return d
}

// usagePercentMax usage_percent 归一口径上界(与探测归一门禁
// nasprobe.CheckUsagePercentRange 同值 0~100,proposal「使用率口径归一」)。
const usagePercentMax = 100.0

// diskMetricQC 写入路径数据质量门禁(proposal「单位归一化」+ AC 写入门禁数量级):
//   - usage_percent 越出 [0,100](含负值):拒绝写入并报错——该形态即厂商口径
//     异常/未归一(如阿里 Burst 系列实盘 -1 哨兵值,probe-report §1.1:须在采集
//     边界过滤,不得入 0~100 门禁误报),门禁保证落库每行可反向验证口径正确;
//   - usage_percent=0:例外放行(不拦截不跳过),强制打 qc_status=zero_exception
//     后正常落库——可能是口径缺失(厂商无该盘指标,T3 不填假值留 0)也可能是
//     合法闲盘(AWS busy_share 派生 0.00),统一打标、读取侧甄别(T8);
//   - iops/throughput 为负:拒绝写入并报错(性能指标无负值语义);
//   - 其余:原样放行(保留适配器已打的 qc_status/usage_scope 标注)。
//
// Disk 无容量字段,「字节 → GB 走共享 types.BytesToGB」不适用(AC「如适用」);
// throughput 的 byte/s → MB/s 归一在适配器采集边界完成(probe-report §1.1/§1.2
// 实盘 byte/s 口径),DAO 门禁不重复换算。
func diskMetricQC(m types.DiskMetric) (types.DiskMetric, error) {
	switch {
	case m.UsagePercent < 0 || m.UsagePercent > usagePercentMax:
		return types.DiskMetric{}, fmt.Errorf(
			"disk metric usage_percent out of range [0,100]: disk_id=%s date=%s usage_percent=%g (自查口径归一:厂商原始口径须在采集边界归一为 0~100)",
			m.DiskID, m.Date, m.UsagePercent)
	case m.IOPS < 0 || m.Throughput < 0:
		return types.DiskMetric{}, fmt.Errorf(
			"disk metric iops/throughput must be non-negative: disk_id=%s date=%s iops=%g throughput=%g",
			m.DiskID, m.Date, m.IOPS, m.Throughput)
	case m.UsagePercent == 0:
		m.QcStatus = types.DiskMetricQcZeroException
		return m, nil
	default:
		return m, nil
	}
}

func (d *diskMetricDAO) UpsertMetric(ctx context.Context, m types.DiskMetric) error {
	qc, err := diskMetricQC(m)
	if err != nil {
		return err
	}
	_, err = d.db.Collection(DiskMetricCollection).UpdateOne(ctx, diskMetricFilter(qc), diskMetricUpsertUpdate(qc), options.Update().SetUpsert(true))
	return err
}

// BulkUpsertMetrics 批量幂等写入单日指标,替代逐条 UpsertMetric 降低写放大。
// 逐行先过门禁,任一行越界则整批拒绝(不良行不得落库);
// filter 与 UpsertMetric 同唯一键 (account_id, disk_id, date),upsert 覆盖语义,
// 整批可安全重放(同键二次写入只覆盖不新增);空批直接返回 nil 不触碰数据库。
func (d *diskMetricDAO) BulkUpsertMetrics(ctx context.Context, metrics []types.DiskMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(metrics))
	for _, m := range metrics {
		qc, err := diskMetricQC(m)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(diskMetricFilter(qc)).
			SetUpdate(diskMetricUpsertUpdate(qc)).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.db.Collection(DiskMetricCollection).BulkWrite(ctx, models, opts)
	return err
}

// BulkInsertIfAbsent 批量「首写生效」写入:filter 定位唯一键行,更新文档仅含
// $setOnInsert(upsert)——命中已存在行时不修改任何字段(首写保护,今日行当日
// 已有则不覆盖;昨日行由次日补采覆盖更新,语义由执行器选择写入方法落实),未命中
// 才整行插入;同键二次写入天然幂等且不产生脏行。整批 ordered=false 提升吞吐。
func (d *diskMetricDAO) BulkInsertIfAbsent(ctx context.Context, metrics []types.DiskMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(metrics))
	for _, m := range metrics {
		qc, err := diskMetricQC(m)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(diskMetricFilter(qc)).
			SetUpdate(bson.M{"$setOnInsert": diskMetricInsertDoc(qc)}).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.db.Collection(DiskMetricCollection).BulkWrite(ctx, models, opts)
	return err
}

// diskMetricInsertDoc 首写生效的插入文档($setOnInsert 全字段:插入时补齐整行)
func diskMetricInsertDoc(m types.DiskMetric) bson.M {
	return bson.M{
		"account_id":    m.AccountID,
		"disk_id":       m.DiskID,
		"date":          m.Date,
		"disk_name":     m.DiskName,
		"usage_percent": m.UsagePercent,
		"usage_scope":   m.UsageScope,
		"iops":          m.IOPS,
		"throughput":    m.Throughput,
		"qc_status":     m.QcStatus,
		"provider":      m.Provider,
	}
}

// diskMetricFilter 以 (account_id, disk_id, date) 唯一键定位单日指标行:
// 同一云盘可被多家云账号纳管(共享盘),各账号同日数据独立保留,互不覆盖。
func diskMetricFilter(m types.DiskMetric) bson.M {
	return bson.M{
		"account_id": m.AccountID,
		"disk_id":    m.DiskID,
		"date":       m.Date,
	}
}

// diskMetricUpsertUpdate 单日指标覆盖字段($set 重采值,$setOnInsert 补齐键字段)
func diskMetricUpsertUpdate(m types.DiskMetric) bson.M {
	return bson.M{
		"$set": bson.M{
			"usage_percent": m.UsagePercent,
			"usage_scope":   m.UsageScope,
			"iops":          m.IOPS,
			"throughput":    m.Throughput,
			"qc_status":     m.QcStatus,
			"disk_name":     m.DiskName,
			"account_id":    m.AccountID,
			"provider":      m.Provider,
		},
		"$setOnInsert": bson.M{
			"disk_id": m.DiskID,
			"date":    m.Date,
		},
	}
}

// CountMetricsByProviders 统计各厂商自 sinceDate(含当日)以来已落库的指标行数。
// 逐厂商 CountDocuments(必达厂商仅 3 家,无需聚合管道);date 字符串按
// YYYY-MM-DD 字典序比较即时间序,与写入路径格式一致(蓝本 nas_metric.go)。
func (d *diskMetricDAO) CountMetricsByProviders(ctx context.Context, providers []string, sinceDate string) (map[string]int64, error) {
	out := make(map[string]int64, len(providers))
	for _, p := range providers {
		n, err := d.db.Collection(DiskMetricCollection).CountDocuments(ctx, bson.M{
			"provider": p,
			"date":     bson.M{"$gte": sinceDate},
		})
		if err != nil {
			return nil, fmt.Errorf("统计厂商 %s 指标行数失败: %w", p, err)
		}
		out[p] = n
	}
	return out, nil
}
