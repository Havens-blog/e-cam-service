package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TTL 时长常量（与 schema.sql expireAfterSeconds 一致）。
const (
	ttlProbe90DaysSeconds     = 7776000 // 90 天
	ttlBatchSession30dSeconds = 2592000 // 30 天
)

// index 简化索引模型构造（键方向恒为 1/-1 的单键/复合键）。
func index(name string, unique bool, keys ...bson.E) mongo.IndexModel {
	keysDoc := bson.D{}
	for _, k := range keys {
		keysDoc = append(keysDoc, k)
	}
	opt := options.Index().SetName(name)
	if unique {
		opt = opt.SetUnique(true)
	}
	return mongo.IndexModel{Keys: keysDoc, Options: opt}
}

// partialIndex 部分索引构造。
func partialIndex(name string, unique bool, filter bson.M, keys ...bson.E) mongo.IndexModel {
	m := index(name, unique, keys...)
	m.Options = m.Options.SetPartialFilterExpression(filter)
	return m
}

// key 索引键简写。
func key(name string, value int32) bson.E { return bson.E{Key: name, Value: value} }

// collectionIndexes 集合名 → 索引清单（与 schema.sql createIndex 逐条对应，索引名 1:1）。
var collectionIndexes = map[string][]mongo.IndexModel{
	CertificatesCollection: {
		index("uk_fingerprint", true, key("fingerprint", 1)),        // 去重/聚合键
		index("idx_hosting_status", false, key("hostingStatus", 1)), // 台账统计
		index("idx_not_after", false, key("notAfter", 1)),           // 到期分级扫描
	},
	CertReferencesCollection: {
		index("idx_fp_cloud_product", false,
			key("certFingerprint", 1), key("cloud", 1), key("product", 1)), // 引用查询
		index("idx_snapshot", false, key("snapshotId", 1)), // 按快照清理
	},
	ScanSnapshotsCollection: {
		index("idx_started_at_desc", false, key("startedAt", -1)), // 最新成功快照查询
	},
	ChangeOrdersCollection: {
		// 在途互斥强制：同一 oldCertFingerprint 同时仅一张活跃单；
		// partialFilterExpression activeMutex 存在（$type string），终态 $unset 后不参与唯一约束
		partialIndex("uk_active_mutex", true,
			bson.M{"activeMutex": bson.M{"$type": "string"}},
			key("activeMutex", 1)),
		index("idx_old_fp_status", false,
			key("oldCertFingerprint", 1), key("status", 1)), // 历史单查询（终态）
	},
	ChangeItemsCollection: {
		index("idx_order", false, key("orderId", 1)),                          // 变更单明细
		index("idx_order_status", false, key("orderId", 1), key("status", 1)), // 批次进度统计
		index("idx_order_batch", false, key("orderId", 1), key("batchNo", 1)), // 当前批执行取项
		partialIndex("idx_status_heartbeat", false,
			bson.M{"status": "running"},
			key("status", 1), key("heartbeatAt", 1)), // executing-timeout 扫描
	},
	CloudCertMappingsCollection: {
		index("uk_fp_cloud_account", true,
			key("certFingerprint", 1), key("cloud", 1), key("accountKey", 1)), // 两段式去重
	},
	ProbeResultsCollection: {
		index("idx_domain_probe_desc", false, key("domain", 1), key("probeAt", -1)), // 最近探测查询
		{ // TTL：90 天自动清理
			Keys:    bson.D{key("probeAt", 1)},
			Options: options.Index().SetName("ttl_probe_90d").SetExpireAfterSeconds(ttlProbe90DaysSeconds),
		},
	},
	ExemptionsCollection: {
		index("uk_domain", true, key("domain", 1)),
	},
	AlertConfigCollection: {}, // 单文档集合，无二级索引
	K8sCredentialsCollection: {
		index("uk_cluster_name", true, key("clusterName", 1)),
	},
	BatchSessionsCollection: {
		{ // TTL：30 天自动清理
			Keys:    bson.D{key("createdAt", 1)},
			Options: options.Index().SetName("ttl_batch_session_30d").SetExpireAfterSeconds(ttlBatchSession30dSeconds),
		},
	},
	CrdRegistrationsCollection: {
		index("uk_cluster_group_kind", true,
			key("clusterId", 1), key("apiGroup", 1), key("kind", 1)), // 登记去重
	},
}

// collectionOrder EnsureIndexes 的应用顺序（确定性执行，便于排障）。
var collectionOrder = []string{
	CertificatesCollection,
	CertReferencesCollection,
	ScanSnapshotsCollection,
	ChangeOrdersCollection,
	ChangeItemsCollection,
	CloudCertMappingsCollection,
	ProbeResultsCollection,
	ExemptionsCollection,
	AlertConfigCollection,
	K8sCredentialsCollection,
	BatchSessionsCollection,
	CrdRegistrationsCollection,
}

// EnsureIndexes 注册全部 cert_* 集合校验器（$jsonSchema）与索引
// （唯一/部分唯一/TTL），幂等可重入——module init 调用：
// 集合不存在则带校验器创建；已存在则经 collMod 同步校验器后重建缺失索引
// （CreateMany 对已存在同名索引为 no-op）。
func EnsureIndexes(ctx context.Context, db *mongox.Mongo) error {
	for _, name := range collectionOrder {
		if err := ensureCollection(ctx, db, name); err != nil {
			return fmt.Errorf("cert repository: ensure collection %s: %w", name, err)
		}
	}
	return nil
}

// ensureCollection 单集合：校验器注册 + 索引创建。
func ensureCollection(ctx context.Context, db *mongox.Mongo, name string) error {
	if err := ensureValidator(ctx, db, name); err != nil {
		return err
	}
	indexes := collectionIndexes[name]
	if len(indexes) == 0 {
		return nil
	}
	_, err := db.Collection(name).Indexes().CreateMany(ctx, indexes)
	return err
}

// ensureValidator 注册集合校验器：不存在则创建，已存在则 collMod 同步。
func ensureValidator(ctx context.Context, db *mongox.Mongo, name string) error {
	validator := collectionValidators[name]
	err := db.Database().CreateCollection(ctx, name,
		options.CreateCollection().SetValidator(validator))
	if err == nil {
		return nil
	}
	if !isNamespaceExists(err) {
		return err
	}
	// 集合已存在：同步校验器定义（幂等；schema 演进时可更新）
	return db.Database().RunCommand(ctx, bson.D{
		{Key: "collMod", Value: name},
		{Key: "validator", Value: validator},
	}).Err()
}

// isNamespaceExists 判断 createCollection 的"集合已存在"错误（code 48）。
func isNamespaceExists(err error) bool {
	if err == nil {
		return false
	}
	if cmdErr, ok := err.(mongo.CommandError); ok && cmdErr.Code == 48 {
		return true
	}
	return strings.Contains(err.Error(), "already exists")
}
