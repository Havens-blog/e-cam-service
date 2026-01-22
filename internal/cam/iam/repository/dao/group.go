package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CloudPermissionGroupsCollection = "c_cloud_user_groups"

// PolicyType 策略类型
type PolicyType string

const (
	PolicyTypeSystem PolicyType = "system"
	PolicyTypeCustom PolicyType = "custom"
)

// PermissionPolicy 权限策略
type PermissionPolicy struct {
	PolicyID       string        `bson:"policy_id"`
	PolicyName     string        `bson:"policy_name"`
	PolicyDocument string        `bson:"policy_document"`
	Provider       CloudProvider `bson:"provider"`
	PolicyType     PolicyType    `bson:"policy_type"`
}

// UserGroup DAO层用户组模型
type UserGroup struct {
	ID             int64              `bson:"id"`
	Name           string             `bson:"name"`
	Description    string             `bson:"description"`
	Policies       []PermissionPolicy `bson:"policies"`
	CloudPlatforms []CloudProvider    `bson:"cloud_platforms"`
	UserCount      int                `bson:"user_count"`
	TenantID       string             `bson:"tenant_id"`
	CreateTime     time.Time          `bson:"create_time"`
	UpdateTime     time.Time          `bson:"update_time"`
	CTime          int64              `bson:"ctime"`
	UTime          int64              `bson:"utime"`
}

// UserGroupFilter DAO层过滤条件
type UserGroupFilter struct {
	TenantID string
	Keyword  string
	Offset   int64
	Limit    int64
}

// UserGroupDAO 用户组数据访问接口
type UserGroupDAO interface {
	Create(ctx context.Context, group UserGroup) (int64, error)
	Update(ctx context.Context, group UserGroup) error
	GetByID(ctx context.Context, id int64) (UserGroup, error)
	GetByName(ctx context.Context, name, tenantID string) (UserGroup, error)
	List(ctx context.Context, filter UserGroupFilter) ([]UserGroup, error)
	Count(ctx context.Context, filter UserGroupFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	UpdatePolicies(ctx context.Context, id int64, policies []PermissionPolicy) error
	IncrementUserCount(ctx context.Context, id int64, delta int) error
}

type userGroupDAO struct {
	db *mongox.Mongo
}

// NewUserGroupDAO 创建用户组DAO
func NewUserGroupDAO(db *mongox.Mongo) UserGroupDAO {
	return &userGroupDAO{
		db: db,
	}
}

// Create 创建用户组
func (dao *userGroupDAO) Create(ctx context.Context, group UserGroup) (int64, error) {
	now := time.Now()
	nowUnix := now.Unix()

	group.CreateTime = now
	group.UpdateTime = now
	group.CTime = nowUnix
	group.UTime = nowUnix

	if group.ID == 0 {
		group.ID = dao.db.GetIdGenerator(CloudPermissionGroupsCollection)
	}

	// 初始化策略列表
	if group.Policies == nil {
		group.Policies = []PermissionPolicy{}
	}

	// 初始化云平台列表
	if group.CloudPlatforms == nil {
		group.CloudPlatforms = []CloudProvider{}
	}

	// 初始化用户数量
	if group.UserCount == 0 {
		group.UserCount = 0
	}

	_, err := dao.db.Collection(CloudPermissionGroupsCollection).InsertOne(ctx, group)
	if err != nil {
		return 0, err
	}

	return group.ID, nil
}

// Update 更新用户组
func (dao *userGroupDAO) Update(ctx context.Context, group UserGroup) error {
	group.UpdateTime = time.Now()
	group.UTime = group.UpdateTime.Unix()

	filter := bson.M{"id": group.ID}
	update := bson.M{"$set": group}

	_, err := dao.db.Collection(CloudPermissionGroupsCollection).UpdateOne(ctx, filter, update)
	return err
}

// GetByID 根据ID获取用户组
func (dao *userGroupDAO) GetByID(ctx context.Context, id int64) (UserGroup, error) {
	var group UserGroup
	filter := bson.M{"id": id}

	err := dao.db.Collection(CloudPermissionGroupsCollection).FindOne(ctx, filter).Decode(&group)
	return group, err
}

// GetByName 根据名称和租户ID获取用户组
func (dao *userGroupDAO) GetByName(ctx context.Context, name, tenantID string) (UserGroup, error) {
	var group UserGroup
	filter := bson.M{
		"name":      name,
		"tenant_id": tenantID,
	}

	err := dao.db.Collection(CloudPermissionGroupsCollection).FindOne(ctx, filter).Decode(&group)
	return group, err
}

// List 获取用户组列表
func (dao *userGroupDAO) List(ctx context.Context, filter UserGroupFilter) ([]UserGroup, error) {
	var groups []UserGroup

	// 构建查询条件
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Keyword != "" {
		query["$or"] = []bson.M{
			{"name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"description": bson.M{"$regex": filter.Keyword, "$options": "i"}},
		}
	}

	// 设置分页选项
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.M{"ctime": -1})

	cursor, err := dao.db.Collection(CloudPermissionGroupsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &groups)
	return groups, err
}

// Count 统计用户组数量
func (dao *userGroupDAO) Count(ctx context.Context, filter UserGroupFilter) (int64, error) {
	// 构建查询条件
	query := bson.M{}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Keyword != "" {
		query["$or"] = []bson.M{
			{"name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"description": bson.M{"$regex": filter.Keyword, "$options": "i"}},
		}
	}

	return dao.db.Collection(CloudPermissionGroupsCollection).CountDocuments(ctx, query)
}

// Delete 删除用户组
func (dao *userGroupDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := dao.db.Collection(CloudPermissionGroupsCollection).DeleteOne(ctx, filter)
	return err
}

// UpdatePolicies 更新权限策略
func (dao *userGroupDAO) UpdatePolicies(ctx context.Context, id int64, policies []PermissionPolicy) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"policies":    policies,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudPermissionGroupsCollection).UpdateOne(ctx, filter, update)
	return err
}

// IncrementUserCount 增加或减少用户数量
func (dao *userGroupDAO) IncrementUserCount(ctx context.Context, id int64, delta int) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$inc": bson.M{
			"user_count": delta,
		},
		"$set": bson.M{
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudPermissionGroupsCollection).UpdateOne(ctx, filter, update)
	return err
}
