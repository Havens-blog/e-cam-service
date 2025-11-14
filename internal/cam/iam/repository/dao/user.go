package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CloudIAMUsersCollection = "cloud_iam_users"

// CloudUserType 用户类型
type CloudUserType string

const (
	CloudUserTypeAPIKey    CloudUserType = "api_key"
	CloudUserTypeAccessKey CloudUserType = "access_key"
	CloudUserTypeRAMUser   CloudUserType = "ram_user"
	CloudUserTypeIAMUser   CloudUserType = "iam_user"
)

// CloudUserStatus 用户状态
type CloudUserStatus string

const (
	CloudUserStatusActive   CloudUserStatus = "active"
	CloudUserStatusInactive CloudUserStatus = "inactive"
	CloudUserStatusDeleted  CloudUserStatus = "deleted"
)

// CloudUserMetadata 用户元数据
type CloudUserMetadata struct {
	LastLoginTime   *time.Time        `bson:"last_login_time"`
	LastSyncTime    *time.Time        `bson:"last_sync_time"`
	AccessKeyCount  int               `bson:"access_key_count"`
	MFAEnabled      bool              `bson:"mfa_enabled"`
	PasswordLastSet *time.Time        `bson:"password_last_set"`
	Tags            map[string]string `bson:"tags"`
}

// CloudUser DAO层云用户模型
type CloudUser struct {
	ID               int64             `bson:"id"`
	Username         string            `bson:"username"`
	UserType         CloudUserType     `bson:"user_type"`
	CloudAccountID   int64             `bson:"cloud_account_id"`
	Provider         CloudProvider     `bson:"provider"`
	CloudUserID      string            `bson:"cloud_user_id"`
	DisplayName      string            `bson:"display_name"`
	Email            string            `bson:"email"`
	PermissionGroups []int64           `bson:"permission_groups"`
	Metadata         CloudUserMetadata `bson:"metadata"`
	Status           CloudUserStatus   `bson:"status"`
	TenantID         string            `bson:"tenant_id"`
	CreateTime       time.Time         `bson:"create_time"`
	UpdateTime       time.Time         `bson:"update_time"`
	CTime            int64             `bson:"ctime"`
	UTime            int64             `bson:"utime"`
}

// CloudUserFilter DAO层过滤条件
type CloudUserFilter struct {
	Provider       CloudProvider
	UserType       CloudUserType
	Status         CloudUserStatus
	CloudAccountID int64
	TenantID       string
	Keyword        string
	Offset         int64
	Limit          int64
}

// CloudUserDAO 云用户数据访问接口
type CloudUserDAO interface {
	Create(ctx context.Context, user CloudUser) (int64, error)
	Update(ctx context.Context, user CloudUser) error
	GetByID(ctx context.Context, id int64) (CloudUser, error)
	GetByCloudUserID(ctx context.Context, cloudUserID string, provider CloudProvider) (CloudUser, error)
	List(ctx context.Context, filter CloudUserFilter) ([]CloudUser, error)
	Count(ctx context.Context, filter CloudUserFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	UpdateStatus(ctx context.Context, id int64, status CloudUserStatus) error
	UpdatePermissionGroups(ctx context.Context, id int64, groupIDs []int64) error
	BatchUpdatePermissionGroups(ctx context.Context, userIDs []int64, groupIDs []int64) error
	UpdateMetadata(ctx context.Context, id int64, metadata CloudUserMetadata) error
}

type cloudUserDAO struct {
	db *mongox.Mongo
}

// NewCloudUserDAO 创建云用户DAO
func NewCloudUserDAO(db *mongox.Mongo) CloudUserDAO {
	return &cloudUserDAO{
		db: db,
	}
}

// Create 创建云用户
func (dao *cloudUserDAO) Create(ctx context.Context, user CloudUser) (int64, error) {
	now := time.Now()
	nowUnix := now.Unix()

	user.CreateTime = now
	user.UpdateTime = now
	user.CTime = nowUnix
	user.UTime = nowUnix

	if user.ID == 0 {
		user.ID = dao.db.GetIdGenerator(CloudIAMUsersCollection)
	}

	// 设置默认状态
	if user.Status == "" {
		user.Status = CloudUserStatusActive
	}

	// 初始化权限组列表
	if user.PermissionGroups == nil {
		user.PermissionGroups = []int64{}
	}

	// 初始化元数据
	if user.Metadata.Tags == nil {
		user.Metadata.Tags = make(map[string]string)
	}

	_, err := dao.db.Collection(CloudIAMUsersCollection).InsertOne(ctx, user)
	if err != nil {
		return 0, err
	}

	return user.ID, nil
}

// Update 更新云用户
func (dao *cloudUserDAO) Update(ctx context.Context, user CloudUser) error {
	user.UpdateTime = time.Now()
	user.UTime = user.UpdateTime.Unix()

	filter := bson.M{"id": user.ID}
	update := bson.M{"$set": user}

	_, err := dao.db.Collection(CloudIAMUsersCollection).UpdateOne(ctx, filter, update)
	return err
}

// GetByID 根据ID获取云用户
func (dao *cloudUserDAO) GetByID(ctx context.Context, id int64) (CloudUser, error) {
	var user CloudUser
	filter := bson.M{"id": id}

	err := dao.db.Collection(CloudIAMUsersCollection).FindOne(ctx, filter).Decode(&user)
	return user, err
}

// GetByCloudUserID 根据云平台用户ID和云厂商获取用户
func (dao *cloudUserDAO) GetByCloudUserID(ctx context.Context, cloudUserID string, provider CloudProvider) (CloudUser, error) {
	var user CloudUser
	filter := bson.M{
		"cloud_user_id": cloudUserID,
		"provider":      provider,
	}

	err := dao.db.Collection(CloudIAMUsersCollection).FindOne(ctx, filter).Decode(&user)
	return user, err
}

// List 获取云用户列表
func (dao *cloudUserDAO) List(ctx context.Context, filter CloudUserFilter) ([]CloudUser, error) {
	var users []CloudUser

	// 构建查询条件
	query := bson.M{}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.UserType != "" {
		query["user_type"] = filter.UserType
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.CloudAccountID > 0 {
		query["cloud_account_id"] = filter.CloudAccountID
	}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Keyword != "" {
		query["$or"] = []bson.M{
			{"username": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"display_name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"email": bson.M{"$regex": filter.Keyword, "$options": "i"}},
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

	cursor, err := dao.db.Collection(CloudIAMUsersCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &users)
	return users, err
}

// Count 统计云用户数量
func (dao *cloudUserDAO) Count(ctx context.Context, filter CloudUserFilter) (int64, error) {
	// 构建查询条件
	query := bson.M{}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.UserType != "" {
		query["user_type"] = filter.UserType
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.CloudAccountID > 0 {
		query["cloud_account_id"] = filter.CloudAccountID
	}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Keyword != "" {
		query["$or"] = []bson.M{
			{"username": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"display_name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"email": bson.M{"$regex": filter.Keyword, "$options": "i"}},
		}
	}

	return dao.db.Collection(CloudIAMUsersCollection).CountDocuments(ctx, query)
}

// Delete 删除云用户
func (dao *cloudUserDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := dao.db.Collection(CloudIAMUsersCollection).DeleteOne(ctx, filter)
	return err
}

// UpdateStatus 更新用户状态
func (dao *cloudUserDAO) UpdateStatus(ctx context.Context, id int64, status CloudUserStatus) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"status":      status,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudIAMUsersCollection).UpdateOne(ctx, filter, update)
	return err
}

// UpdatePermissionGroups 更新用户权限组
func (dao *cloudUserDAO) UpdatePermissionGroups(ctx context.Context, id int64, groupIDs []int64) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"permission_groups": groupIDs,
			"update_time":       time.Now(),
			"utime":             time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudIAMUsersCollection).UpdateOne(ctx, filter, update)
	return err
}

// BatchUpdatePermissionGroups 批量更新用户权限组
func (dao *cloudUserDAO) BatchUpdatePermissionGroups(ctx context.Context, userIDs []int64, groupIDs []int64) error {
	filter := bson.M{"id": bson.M{"$in": userIDs}}
	update := bson.M{
		"$set": bson.M{
			"permission_groups": groupIDs,
			"update_time":       time.Now(),
			"utime":             time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudIAMUsersCollection).UpdateMany(ctx, filter, update)
	return err
}

// UpdateMetadata 更新用户元数据
func (dao *cloudUserDAO) UpdateMetadata(ctx context.Context, id int64, metadata CloudUserMetadata) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"metadata":    metadata,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}

	_, err := dao.db.Collection(CloudIAMUsersCollection).UpdateOne(ctx, filter, update)
	return err
}
