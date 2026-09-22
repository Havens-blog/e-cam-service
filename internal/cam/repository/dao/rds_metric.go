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

const RDSMetricCollection = "ecam_rds_metric"

// RDSMetricDAO 云数据库单日指标数据访问接口
type RDSMetricDAO interface {
	// UpsertMetric 按 (account_id, rds_id, date) 唯一键幂等写入单日指标。
	// 写入路径先过数据质量门禁:三使用率(cpu/memory/disk_percent)越出 [0,100]
	// 或 connections 为负拒绝并报错(错误携带 rds_id/date);四指标全 0 例外放行
	// 并强制打 qc_status=zero_exception(不拦截不跳过)。「今日行首写生效 /
	// 昨日行覆盖更新」的采集语义由采集执行器落实,DAO 层只保证唯一键幂等与
	// 数据质量门禁。
	UpsertMetric(ctx context.Context, m types.RDSMetric) error
	// BulkUpsertMetrics 批量幂等写入单日指标(mongo BulkWrite + upsert),唯一键
	// 与 UpsertMetric 一致;空批直接返回 nil;任一行未过门禁则整批拒绝(不良行
	// 不得落库,错误携带 rds_id 供执行器失败归因);BulkWrite 错误原样回传。
	BulkUpsertMetrics(ctx context.Context, metrics []types.RDSMetric) error
	// BulkInsertIfAbsent 批量「首写生效」写入(采集执行器专用):更新文档仅含
	// $setOnInsert——唯一键 (account_id, rds_id, date) 命中已存在行时不做任何
	// 修改(今日行当日已有则不覆盖),仅补缺失行;未命中则整行插入。空批直接
	// 返回 nil;门禁与 BulkUpsertMetrics 同口径,任一行未过则整批拒绝。
	BulkInsertIfAbsent(ctx context.Context, metrics []types.RDSMetric) error
	// CountMetricsByProviders 统计各厂商自 sinceDate(含当日,YYYY-MM-DD)以来
	// 已落库的指标行数(自我健康监控用:窗口内行存在即「成功采集」证据),
	// 与 NAS/OSS/Disk 同名同签名先例一致。
	CountMetricsByProviders(ctx context.Context, providers []string, sinceDate string) (map[string]int64, error)
	// ListByRDS 取指定账号+rds_id 近 N 天单日指标,按 date 升序(趋势接口读取)。
	ListByRDS(ctx context.Context, accountID int64, rdsID string, days int) ([]types.RDSMetric, error)
	// ListByAccounts 取一组账号近 N 天全部指标行(Top 聚合读取,服务层按 rds_id
	// 去重与分页)。accountIDs 为空返回空切片;days 缺省/上限同 ListByRDS。
	ListByAccounts(ctx context.Context, accountIDs []int64, days int) ([]types.RDSMetric, error)
}

type rdsMetricDAO struct {
	db *mongox.Mongo
}

// NewRDSMetricDAO 创建 RDS 指标 DAO,并确保 (account_id, rds_id, date) 唯一索引。
//
// 注意:唯一键必须含 account_id(Hard Rule)——同一 RDS 实例可被多家云账号纳管
// (跨账号共享数据库),(rds_id, date) 作为唯一键会让后写的账号覆盖先写账号的行,
// 共享实例的指标视图丢失一方数据(复用 CDN/NAS/OSS/Disk 指标表多账号修复经验,
// 3cea6d7)。rds_id 是地域内唯一标识(与 NAS 的 fs_id/Disk 的 disk_id 同型,RDS
// 是地域性资源而非 OSS 全局服务);集合为本 feature 新建,无历史索引需清理。
func NewRDSMetricDAO(db *mongox.Mongo) RDSMetricDAO {
	d := &rdsMetricDAO{db: db}
	col := db.Collection(RDSMetricCollection)
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "account_id", Value: 1},
			{Key: "rds_id", Value: 1},
			{Key: "date", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	return d
}

// rdsMetricQC 写入路径数据质量门禁(proposal「单位归一化」+ probe-report §2
// 归一定案「三使用率百分比 0~100、connections 绝对值」+ T5/T8 zero_exception 契约):
//   - cpu/memory/disk_percent 越出 [0,100](含负值):拒绝写入并报错——该形态即
//     厂商口径异常/未归一,门禁保证落库每行可反向验证口径正确(归一在 T3 适配器
//     采集边界完成:aliyun/huawei % 直给,aws FreeableMemory/FreeStorageSpace
//     字节换算,probe-report §2 实盘 5/5 PASS);
//   - connections 为负:拒绝写入并报错(连接数无负值语义);
//   - 四指标全 0(CPU/内存/磁盘/连接):例外放行,强制打 qc_status=zero_exception
//     后正常落库——T5 执行器「不继承 CDN 全零过滤(四指标全 0 落库打
//     zero_exception)」,T8 读取侧原样暴露并甄别(停用/重启中实例打 data_status);
//   - 其余:原样放行(保留适配器已打的 qc_status 标注;单指标 0 低负载是正常
//     业务事实,probe-report 零值 13 行,不触发打标)。
func rdsMetricQC(m types.RDSMetric) (types.RDSMetric, error) {
	for _, u := range []struct {
		name string
		val  float64
	}{
		{"cpu_percent", m.CPUPercent},
		{"memory_percent", m.MemoryPercent},
		{"disk_percent", m.DiskPercent},
	} {
		if u.val < 0 || u.val > usagePercentMax {
			return types.RDSMetric{}, fmt.Errorf(
				"rds metric %s out of range [0,100]: rds_id=%s date=%s %s=%g (自查口径归一:厂商原始口径须在采集边界归一为 0~100)",
				u.name, m.RdsID, m.Date, u.name, u.val)
		}
	}
	if m.Connections < 0 {
		return types.RDSMetric{}, fmt.Errorf(
			"rds metric connections must be non-negative: rds_id=%s date=%s connections=%d",
			m.RdsID, m.Date, m.Connections)
	}
	if m.CPUPercent == 0 && m.MemoryPercent == 0 && m.DiskPercent == 0 && m.Connections == 0 {
		m.QcStatus = types.RDSMetricQcZeroException
	}
	return m, nil
}

func (d *rdsMetricDAO) UpsertMetric(ctx context.Context, m types.RDSMetric) error {
	qc, err := rdsMetricQC(m)
	if err != nil {
		return err
	}
	_, err = d.db.Collection(RDSMetricCollection).UpdateOne(ctx, rdsMetricFilter(qc), rdsMetricUpsertUpdate(qc), options.Update().SetUpsert(true))
	return err
}

// BulkUpsertMetrics 批量幂等写入单日指标,替代逐条 UpsertMetric 降低写放大。
// 逐行先过门禁,任一行越界则整批拒绝(不良行不得落库);
// filter 与 UpsertMetric 同唯一键 (account_id, rds_id, date),upsert 覆盖语义,
// 整批可安全重放(同键二次写入只覆盖不新增);空批直接返回 nil 不触碰数据库。
func (d *rdsMetricDAO) BulkUpsertMetrics(ctx context.Context, metrics []types.RDSMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(metrics))
	for _, m := range metrics {
		qc, err := rdsMetricQC(m)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(rdsMetricFilter(qc)).
			SetUpdate(rdsMetricUpsertUpdate(qc)).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.db.Collection(RDSMetricCollection).BulkWrite(ctx, models, opts)
	return err
}

// BulkInsertIfAbsent 批量「首写生效」写入:filter 定位唯一键行,更新文档仅含
// $setOnInsert(upsert)——命中已存在行时不修改任何字段(首写保护,今日行当日
// 已有则不覆盖;昨日行由次日补采覆盖更新,语义由执行器选择写入方法落实),未命中
// 才整行插入;同键二次写入天然幂等且不产生脏行。整批 ordered=false 提升吞吐。
func (d *rdsMetricDAO) BulkInsertIfAbsent(ctx context.Context, metrics []types.RDSMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(metrics))
	for _, m := range metrics {
		qc, err := rdsMetricQC(m)
		if err != nil {
			return err
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(rdsMetricFilter(qc)).
			SetUpdate(bson.M{"$setOnInsert": rdsMetricInsertDoc(qc)}).
			SetUpsert(true))
	}
	opts := options.BulkWrite().SetOrdered(false)
	_, err := d.db.Collection(RDSMetricCollection).BulkWrite(ctx, models, opts)
	return err
}

// rdsMetricInsertDoc 首写生效的插入文档($setOnInsert 全字段:插入时补齐整行)
func rdsMetricInsertDoc(m types.RDSMetric) bson.M {
	return bson.M{
		"account_id":     m.AccountID,
		"rds_id":         m.RdsID,
		"date":           m.Date,
		"instance_name":  m.InstanceName,
		"cpu_percent":    m.CPUPercent,
		"memory_percent": m.MemoryPercent,
		"disk_percent":   m.DiskPercent,
		"connections":    m.Connections,
		"engine":         m.Engine,
		"qc_status":      m.QcStatus,
		"provider":       m.Provider,
	}
}

// rdsMetricFilter 以 (account_id, rds_id, date) 唯一键定位单日指标行:
// 同一 RDS 实例可被多家云账号纳管(跨账号共享数据库),各账号同日数据独立
// 保留,互不覆盖。
func rdsMetricFilter(m types.RDSMetric) bson.M {
	return bson.M{
		"account_id": m.AccountID,
		"rds_id":     m.RdsID,
		"date":       m.Date,
	}
}

// rdsMetricUpsertUpdate 单日指标覆盖字段($set 重采值,$setOnInsert 补齐键字段)
func rdsMetricUpsertUpdate(m types.RDSMetric) bson.M {
	return bson.M{
		"$set": bson.M{
			"cpu_percent":    m.CPUPercent,
			"memory_percent": m.MemoryPercent,
			"disk_percent":   m.DiskPercent,
			"connections":    m.Connections,
			"engine":         m.Engine,
			"qc_status":      m.QcStatus,
			"instance_name":  m.InstanceName,
			"account_id":     m.AccountID,
			"provider":       m.Provider,
		},
		"$setOnInsert": bson.M{
			"rds_id": m.RdsID,
			"date":   m.Date,
		},
	}
}

// CountMetricsByProviders 统计各厂商自 sinceDate(含当日)以来已落库的指标行数。
// 逐厂商 CountDocuments(必达厂商仅 3 家,无需聚合管道);date 字符串按
// YYYY-MM-DD 字典序比较即时间序,与写入路径格式一致(蓝本 disk_metric.go)。
func (d *rdsMetricDAO) CountMetricsByProviders(ctx context.Context, providers []string, sinceDate string) (map[string]int64, error) {
	out := make(map[string]int64, len(providers))
	for _, p := range providers {
		n, err := d.db.Collection(RDSMetricCollection).CountDocuments(ctx, bson.M{
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
