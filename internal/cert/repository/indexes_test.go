package repository

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// expectedIndexes schema.sql 全部索引名（1:1 对照 er-diagram.md 索引策略表）。
var expectedIndexes = map[string][]string{
	CertificatesCollection:            {"uk_fingerprint", "idx_hosting_status", "idx_not_after"},
	CertReferencesCollection:          {"idx_fp_cloud_product", "idx_snapshot"},
	ScanSnapshotsCollection:           {"idx_started_at_desc"},
	ChangeOrdersCollection:            {"uk_active_mutex", "idx_old_fp_status"},
	ChangeItemsCollection:             {"idx_order", "idx_order_status", "idx_order_batch", "idx_status_heartbeat"},
	CloudCertMappingsCollection:       {"uk_fp_cloud_account"},
	ProbeResultsCollection:            {"idx_domain_probe_desc", "ttl_probe_90d"},
	ExemptionsCollection:              {"uk_domain"},
	AlertConfigCollection:             {},
	K8sCredentialsCollection:          {"uk_cluster_name"},
	BatchSessionsCollection:           {"ttl_batch_session_30d"},
	DiscoveryImportSessionsCollection: {"ttl_discovery_import_session_30d"},
	CrdRegistrationsCollection:        {"uk_cluster_group_kind"},
}

// TestEnsureIndexes_CreatesAllCollectionsAndIndexes（集成，需 mongox test 实例）
// EnsureIndexes 后 13 集合存在且索引名与 schema.sql 完全一致；重复执行幂等。
func TestEnsureIndexes_CreatesAllCollectionsAndIndexes(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()

	require.NoError(t, EnsureIndexes(ctx, db))
	// 幂等：重复执行不报错（collMod 同步 + CreateMany no-op）
	require.NoError(t, EnsureIndexes(ctx, db))

	cols, err := db.Database().ListCollectionNames(ctx, bson.M{})
	require.NoError(t, err)
	present := map[string]bool{}
	for _, c := range cols {
		present[c] = true
	}
	for name, indexes := range expectedIndexes {
		require.True(t, present[name], "集合 %s 未创建", name)
		got, err := indexNames(ctx, db, name)
		require.NoError(t, err)
		assert.ElementsMatch(t, indexes, got, "集合 %s 索引不一致", name)
	}
}

// defaultIDIndex MongoDB 每个集合自动创建的默认索引名。
const defaultIDIndex = "_id_"

// TestEnsureIndexes_IndexOptions 关键索引选项：唯一/部分唯一/TTL（集成）。
func TestEnsureIndexes_IndexOptions(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))

	idx := findIndexByName(t, ctx, db, CertificatesCollection, "uk_fingerprint")
	assert.Equal(t, true, idx["unique"], "uk_fingerprint 应为唯一索引")
	assert.Nil(t, idx["partialFilterExpression"], "uk_fingerprint 非部分索引")

	mutex := findIndexByName(t, ctx, db, ChangeOrdersCollection, "uk_active_mutex")
	assert.Equal(t, true, mutex["unique"], "uk_active_mutex 应为唯一索引")
	assert.Equal(t, bson.M{"activeMutex": bson.M{"$type": "string"}}, mutex["partialFilterExpression"],
		"uk_active_mutex 部分过滤条件应为 activeMutex 存在")

	hb := findIndexByName(t, ctx, db, ChangeItemsCollection, "idx_status_heartbeat")
	assert.Equal(t, bson.M{"status": "running"}, hb["partialFilterExpression"],
		"idx_status_heartbeat 部分过滤条件应为 status=running")

	ttlProbe := findIndexByName(t, ctx, db, ProbeResultsCollection, "ttl_probe_90d")
	assert.Equal(t, int32(ttlProbe90DaysSeconds), ttlProbe["expireAfterSeconds"],
		"ttl_probe_90d 应为 90 天 TTL")

	ttlBatch := findIndexByName(t, ctx, db, BatchSessionsCollection, "ttl_batch_session_30d")
	assert.Equal(t, int32(ttlBatchSession30dSeconds), ttlBatch["expireAfterSeconds"],
		"ttl_batch_session_30d 应为 30 天 TTL")

	ttlDiscovery := findIndexByName(t, ctx, db, DiscoveryImportSessionsCollection, "ttl_discovery_import_session_30d")
	assert.Equal(t, int32(ttlBatchSession30dSeconds), ttlDiscovery["expireAfterSeconds"],
		"ttl_discovery_import_session_30d 应为 30 天 TTL（与批量会话同口径）")

	for _, u := range []struct{ coll, name string }{
		{CloudCertMappingsCollection, "uk_fp_cloud_account"},
		{CrdRegistrationsCollection, "uk_cluster_group_kind"},
		{ExemptionsCollection, "uk_domain"},
		{K8sCredentialsCollection, "uk_cluster_name"},
	} {
		assert.Equal(t, true, findIndexByName(t, ctx, db, u.coll, u.name)["unique"],
			"%s 应为唯一索引", u.name)
	}
}

// TestValidators_RejectInvalidDocuments（集成）集合校验器拒绝越界文档：
// hostingStatus 非法值、fingerprint 非 64 hex、变更单 status 非枚举、change item 缺少 action 分支必填字段。
func TestValidators_RejectInvalidDocuments(t *testing.T) {
	db := newTestMongo(t)
	ctx := context.Background()
	require.NoError(t, EnsureIndexes(ctx, db))

	validFp := "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"

	t.Run("hostingStatus 非法值被拒绝", func(t *testing.T) {
		_, err := db.Collection(CertificatesCollection).InsertOne(ctx, bson.M{
			"fingerprint":   validFp,
			"hostingStatus": "bogus",
			"createdAt":     now(),
		})
		require.Error(t, err)
		assert.True(t, isValidationErr(t, err), "应为文档校验失败: %v", err)
	})

	t.Run("fingerprint 非 64 hex 被拒绝", func(t *testing.T) {
		_, err := db.Collection(CertificatesCollection).InsertOne(ctx, bson.M{
			"fingerprint":   "not-hex",
			"hostingStatus": "complete",
			"createdAt":     now(),
		})
		require.Error(t, err)
		assert.True(t, isValidationErr(t, err), "应为文档校验失败: %v", err)
	})

	t.Run("change order status 非枚举被拒绝", func(t *testing.T) {
		_, err := db.Collection(ChangeOrdersCollection).InsertOne(ctx, bson.M{
			"oldCertFingerprint": validFp,
			"newCertId":          "cert-1",
			"status":             "waiting",
			"snapshotId":         "snap-1",
			"creator":            "op",
			"createdAt":          now(),
		})
		require.Error(t, err)
		assert.True(t, isValidationErr(t, err), "应为文档校验失败: %v", err)
	})

	t.Run("change item 缺少 action 分支必填字段被拒绝", func(t *testing.T) {
		// action=upload_and_bind 但 resourceRef 缺 accountKey（cloud_api 分支必填）
		_, err := db.Collection(ChangeItemsCollection).InsertOne(ctx, bson.M{
			"orderId": "order-1",
			"action":  "upload_and_bind",
			"resourceRef": bson.M{
				"channel":    "cloud_api",
				"cloud":      "aliyun",
				"product":    "cdn",
				"resourceId": "res-1",
			},
			"status": "pending",
		})
		require.Error(t, err)
		assert.True(t, isValidationErr(t, err), "应为文档校验失败: %v", err)
	})

	t.Run("alert config thresholds 越界被拒绝", func(t *testing.T) {
		_, err := db.Collection(AlertConfigCollection).InsertOne(ctx, bson.M{
			"thresholds": bson.M{
				"scanFreshnessHours":          24,
				"verifyWindowHours":           24,
				"rollbackProtectDays":         7,
				"verifyConfirmProbes":         2,
				"verifyProbeIntervalMinutes":  10,
				"pauseTimeoutHours":           72,
				"recheckDelayMinutes":         5,
				"itemHeartbeatTimeoutMinutes": 30,
				"scanTimeoutHours":            200, // 越界：最大 12
				"expiryLevels":                []int{30, 14, 7},
			},
		})
		require.Error(t, err)
		assert.True(t, isValidationErr(t, err), "应为文档校验失败: %v", err)
	})

	t.Run("合法文档被接受", func(t *testing.T) {
		_, err := db.Collection(CertificatesCollection).InsertOne(ctx, bson.M{
			"fingerprint":   validFp,
			"hostingStatus": "complete",
			"createdAt":     now(),
		})
		assert.NoError(t, err)
	})
}

// indexNames 读取集合全部索引名。
func indexNames(ctx context.Context, db *mongox.Mongo, coll string) ([]string, error) {
	cursor, err := db.Collection(coll).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var specs []bson.M
	if err := cursor.All(ctx, &specs); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		if n, ok := s["name"].(string); ok && n != defaultIDIndex {
			names = append(names, n)
		}
	}
	return names, nil
}

// findIndexByName 读取指定索引的原始规格（断言存在；含 partialFilterExpression 等全部选项）。
func findIndexByName(t *testing.T, ctx context.Context, db *mongox.Mongo, coll, name string) bson.M {
	t.Helper()
	cursor, err := db.Collection(coll).Indexes().List(ctx)
	require.NoError(t, err)
	defer cursor.Close(ctx)
	var specs []bson.M
	require.NoError(t, cursor.All(ctx, &specs))
	for _, s := range specs {
		if n, _ := s["name"].(string); n == name {
			return s
		}
	}
	t.Fatalf("索引 %s.%s 不存在", coll, name)
	return nil
}

// isValidationErr 判断是否为 $jsonSchema 校验拒绝
// （InsertOne 以 WriteException/CommandError 形态返回 code 121 DocumentValidationFailure）。
func isValidationErr(t *testing.T, err error) bool {
	t.Helper()
	switch e := err.(type) {
	case mongo.CommandError:
		return e.Code == 121
	case mongo.WriteException:
		for _, we := range e.WriteErrors {
			if we.Code == 121 {
				return true
			}
		}
	case mongo.BulkWriteException:
		for _, we := range e.WriteErrors {
			if we.Code == 121 {
				return true
			}
		}
	}
	return false
}
