package dao

import (
	"context"
	"strings"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ensureIndexes 创建索引，遇到 IndexOptionsConflict 时先删除冲突索引再重建
func ensureIndexes(ctx context.Context, collection *mongo.Collection, indexes []mongo.IndexModel) error {
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err == nil {
		return nil
	}

	// 如果不是索引冲突错误，直接返回
	if !strings.Contains(err.Error(), "IndexOptionsConflict") &&
		!strings.Contains(err.Error(), "already exists with a different name") {
		return err
	}

	// 逐个创建，遇到冲突就删除旧索引再重建
	for _, idx := range indexes {
		_, createErr := collection.Indexes().CreateOne(ctx, idx)
		if createErr == nil {
			continue
		}
		if !strings.Contains(createErr.Error(), "IndexOptionsConflict") &&
			!strings.Contains(createErr.Error(), "already exists with a different name") {
			return createErr
		}

		// 删除冲突的旧索引（按字段匹配）
		cursor, listErr := collection.Indexes().List(ctx)
		if listErr != nil {
			return listErr
		}
		var existingIndexes []bson.M
		if listErr = cursor.All(ctx, &existingIndexes); listErr != nil {
			return listErr
		}

		// 找到字段相同但名字不同的索引并删除
		for _, existing := range existingIndexes {
			existingName, _ := existing["name"].(string)
			if existingName == "_id_" {
				continue
			}
			existingKey, _ := existing["key"].(bson.M)
			if existingKey == nil {
				continue
			}
			wantKey := idx.Keys.(bson.D)
			if len(existingKey) != len(wantKey) {
				continue
			}
			match := true
			for _, kv := range wantKey {
				if _, ok := existingKey[kv.Key]; !ok {
					match = false
					break
				}
			}
			if match {
				_, _ = collection.Indexes().DropOne(ctx, existingName)
				break
			}
		}

		// 重新创建
		if _, createErr = collection.Indexes().CreateOne(ctx, idx); createErr != nil {
			return createErr
		}
	}
	return nil
}

// InitCollections 初始化所有IAM相关的MongoDB集合和索引
func InitCollections(ctx context.Context, db *mongox.Mongo) error {
	if err := initCloudUsersCollection(ctx, db); err != nil {
		return err
	}
	if err := initPermissionGroupsCollection(ctx, db); err != nil {
		return err
	}
	if err := initSyncTasksCollection(ctx, db); err != nil {
		return err
	}
	if err := initAuditLogsCollection(ctx, db); err != nil {
		return err
	}
	if err := initPolicyTemplatesCollection(ctx, db); err != nil {
		return err
	}
	if err := initTenantsCollection(ctx, db); err != nil {
		return err
	}
	return nil
}

// initCloudUsersCollection 初始化云用户集合
func initCloudUsersCollection(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(CloudIAMUsersCollection)
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "cloud_user_id", Value: 1}, {Key: "provider", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_cloud_user_id_provider"),
		},
		{
			Keys:    bson.D{{Key: "cloud_account_id", Value: 1}},
			Options: options.Index().SetName("idx_cloud_account_id"),
		},
		{
			Keys:    bson.D{{Key: "provider", Value: 1}, {Key: "status", Value: 1}},
			Options: options.Index().SetName("idx_provider_status"),
		},
		{
			Keys:    bson.D{{Key: "tenant_id", Value: 1}},
			Options: options.Index().SetName("idx_tenant_id"),
		},
		{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetName("idx_username"),
		},
		{
			Keys:    bson.D{{Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_ctime"),
		},
	}
	return ensureIndexes(ctx, collection, indexes)
}

// initPermissionGroupsCollection 初始化权限组集合
func initPermissionGroupsCollection(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(CloudPermissionGroupsCollection)
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "name", Value: 1}, {Key: "tenant_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_name_tenant_id"),
		},
		{
			Keys:    bson.D{{Key: "tenant_id", Value: 1}},
			Options: options.Index().SetName("idx_tenant_id"),
		},
		{
			Keys:    bson.D{{Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_ctime"),
		},
	}
	return ensureIndexes(ctx, collection, indexes)
}

// initSyncTasksCollection 初始化同步任务集合
func initSyncTasksCollection(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(CloudSyncTasksCollection)
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_status_ctime"),
		},
		{
			Keys:    bson.D{{Key: "target_type", Value: 1}, {Key: "target_id", Value: 1}},
			Options: options.Index().SetName("idx_target_type_target_id"),
		},
		{
			Keys:    bson.D{{Key: "cloud_account_id", Value: 1}},
			Options: options.Index().SetName("idx_cloud_account_id"),
		},
		{
			Keys:    bson.D{{Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_ctime"),
		},
	}
	return ensureIndexes(ctx, collection, indexes)
}

// initAuditLogsCollection 初始化审计日志集合
func initAuditLogsCollection(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(CloudAuditLogsCollection)
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "operation_type", Value: 1}, {Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_operation_type_ctime"),
		},
		{
			Keys:    bson.D{{Key: "operator_id", Value: 1}, {Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_operator_id_ctime"),
		},
		{
			Keys:    bson.D{{Key: "target_type", Value: 1}, {Key: "target_id", Value: 1}},
			Options: options.Index().SetName("idx_target_type_target_id"),
		},
		{
			Keys:    bson.D{{Key: "cloud_platform", Value: 1}, {Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_cloud_platform_ctime"),
		},
		{
			Keys:    bson.D{{Key: "tenant_id", Value: 1}, {Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_tenant_id_ctime"),
		},
		{
			Keys:    bson.D{{Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_ctime"),
		},
		{
			Keys:    bson.D{{Key: "create_time", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(7776000).SetName("idx_ttl_create_time"),
		},
	}
	return ensureIndexes(ctx, collection, indexes)
}

// initPolicyTemplatesCollection 初始化策略模板集合
func initPolicyTemplatesCollection(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(CloudPolicyTemplatesCollection)
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "name", Value: 1}, {Key: "tenant_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_name_tenant_id"),
		},
		{
			Keys:    bson.D{{Key: "category", Value: 1}},
			Options: options.Index().SetName("idx_category"),
		},
		{
			Keys:    bson.D{{Key: "is_built_in", Value: 1}},
			Options: options.Index().SetName("idx_is_built_in"),
		},
		{
			Keys:    bson.D{{Key: "tenant_id", Value: 1}},
			Options: options.Index().SetName("idx_tenant_id"),
		},
	}
	return ensureIndexes(ctx, collection, indexes)
}

// initTenantsCollection 初始化租户集合
func initTenantsCollection(ctx context.Context, db *mongox.Mongo) error {
	collection := db.Collection(TenantsCollection)
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "name", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_name"),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "create_time", Value: -1}},
			Options: options.Index().SetName("idx_status_create_time"),
		},
		{
			Keys:    bson.D{{Key: "metadata.industry", Value: 1}},
			Options: options.Index().SetName("idx_industry"),
		},
		{
			Keys:    bson.D{{Key: "metadata.region", Value: 1}},
			Options: options.Index().SetName("idx_region"),
		},
		{
			Keys:    bson.D{{Key: "ctime", Value: -1}},
			Options: options.Index().SetName("idx_ctime"),
		},
	}
	return ensureIndexes(ctx, collection, indexes)
}

// DropCollections 删除所有IAM相关的MongoDB集合
func DropCollections(ctx context.Context, db *mongox.Mongo) error {
	collections := []string{
		CloudIAMUsersCollection,
		CloudPermissionGroupsCollection,
		CloudSyncTasksCollection,
		CloudAuditLogsCollection,
		CloudPolicyTemplatesCollection,
		TenantsCollection,
	}
	for _, collName := range collections {
		if err := db.Collection(collName).Drop(ctx); err != nil {
			return err
		}
	}
	return nil
}

// InitIndexes 初始化所有索引
func InitIndexes(db *mongox.Mongo) error {
	ctx := context.Background()
	return InitCollections(ctx, db)
}

// EnsureIndexes 确保所有索引都已创建（幂等操作）
func EnsureIndexes(ctx context.Context, db *mongox.Mongo) error {
	return InitCollections(ctx, db)
}

// GetCollectionStats 获取集合统计信息
func GetCollectionStats(ctx context.Context, db *mongox.Mongo) (map[string]any, error) {
	stats := make(map[string]any)
	collections := []string{
		CloudIAMUsersCollection,
		CloudPermissionGroupsCollection,
		CloudSyncTasksCollection,
		CloudAuditLogsCollection,
		CloudPolicyTemplatesCollection,
		TenantsCollection,
	}
	for _, collName := range collections {
		count, err := db.Collection(collName).CountDocuments(ctx, bson.M{})
		if err != nil {
			return nil, err
		}
		stats[collName] = count
	}
	return stats, nil
}
