// Package dao 云账号数据访问层
package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const AccountsCollection = "cloud_accounts"

// CloudAccountConfig DAO层云账号配置
type CloudAccountConfig struct {
	EnableAutoSync       bool     `json:"enable_auto_sync" bson:"enable_auto_sync"`
	SyncInterval         int64    `json:"sync_interval" bson:"sync_interval"`
	ReadOnly             bool     `json:"read_only" bson:"read_only"`
	ShowSubAccounts      bool     `json:"show_sub_accounts" bson:"show_sub_accounts"`
	EnableCostMonitoring bool     `json:"enable_cost_monitoring" bson:"enable_cost_monitoring"`
	SupportedRegions     []string `json:"supported_regions" bson:"supported_regions"`
	SupportedAssetTypes  []string `json:"supported_asset_types" bson:"supported_asset_types"`
}

// CloudAccount DAO层云账号模型
type CloudAccount struct {
	ID              int64                     `json:"id" bson:"id"`
	Name            string                    `json:"name" bson:"name"`
	Provider        domain.CloudProvider      `json:"provider" bson:"provider"`
	Environment     domain.Environment        `json:"environment" bson:"environment"`
	AccessKeyID     string                    `json:"access_key_id" bson:"access_key_id"`
	AccessKeySecret string                    `json:"access_key_secret" bson:"access_key_secret"`
	Regions         []string                  `json:"regions" bson:"regions"`
	Description     string                    `json:"description" bson:"description"`
	Status          domain.CloudAccountStatus `json:"status" bson:"status"`
	Config          CloudAccountConfig        `json:"config" bson:"config"`
	TenantID        string                    `json:"tenant_id" bson:"tenant_id"`
	LastSyncTime    *time.Time                `json:"last_sync_time" bson:"last_sync_time"`
	LastTestTime    *time.Time                `json:"last_test_time" bson:"last_test_time"`
	AssetCount      int64                     `json:"asset_count" bson:"asset_count"`
	ErrorMessage    string                    `json:"error_message" bson:"error_message"`
	CreateTime      time.Time                 `json:"create_time" bson:"create_time"`
	UpdateTime      time.Time                 `json:"update_time" bson:"update_time"`
	CTime           int64                     `json:"ctime" bson:"ctime"`
	UTime           int64                     `json:"utime" bson:"utime"`
}

// CloudAccountFilter DAO层过滤条件
type CloudAccountFilter struct {
	Provider    domain.CloudProvider
	Environment domain.Environment
	Status      domain.CloudAccountStatus
	TenantID    string
	Name        string
	Offset      int64
	Limit       int64
}

// CloudAccountDAO 云账号数据访问接口
type CloudAccountDAO interface {
	CreateAccount(ctx context.Context, account CloudAccount) (int64, error)
	UpdateAccount(ctx context.Context, account CloudAccount) error
	GetAccountById(ctx context.Context, id int64) (CloudAccount, error)
	GetAccountByName(ctx context.Context, name, tenantID string) (CloudAccount, error)
	ListAccounts(ctx context.Context, filter CloudAccountFilter) ([]CloudAccount, error)
	CountAccounts(ctx context.Context, filter CloudAccountFilter) (int64, error)
	DeleteAccount(ctx context.Context, id int64) error
	UpdateAccountStatus(ctx context.Context, id int64, status domain.CloudAccountStatus) error
	UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error
	UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status domain.CloudAccountStatus, errorMsg string) error
}

type cloudAccountDAO struct {
	db *mongox.Mongo
}

// NewCloudAccountDAO 创建云账号DAO
func NewCloudAccountDAO(db *mongox.Mongo) CloudAccountDAO {
	return &cloudAccountDAO{db: db}
}

// CreateAccount 创建云账号
func (dao *cloudAccountDAO) CreateAccount(ctx context.Context, account CloudAccount) (int64, error) {
	now := time.Now()
	account.CreateTime = now
	account.UpdateTime = now
	account.CTime = now.Unix()
	account.UTime = now.Unix()

	if account.ID == 0 {
		account.ID = dao.db.GetIdGenerator(AccountsCollection)
	}
	if account.Status == "" {
		account.Status = domain.CloudAccountStatusActive
	}

	_, err := dao.db.Collection(AccountsCollection).InsertOne(ctx, account)
	if err != nil {
		return 0, err
	}
	return account.ID, nil
}

// UpdateAccount 更新云账号
func (dao *cloudAccountDAO) UpdateAccount(ctx context.Context, account CloudAccount) error {
	account.UpdateTime = time.Now()
	account.UTime = account.UpdateTime.Unix()

	filter := bson.M{"id": account.ID}
	update := bson.M{"$set": account}
	_, err := dao.db.Collection(AccountsCollection).UpdateOne(ctx, filter, update)
	return err
}

// GetAccountById 根据ID获取云账号
func (dao *cloudAccountDAO) GetAccountById(ctx context.Context, id int64) (CloudAccount, error) {
	var account CloudAccount
	filter := bson.M{"id": id}
	err := dao.db.Collection(AccountsCollection).FindOne(ctx, filter).Decode(&account)
	return account, err
}

// GetAccountByName 根据名称和租户ID获取云账号
func (dao *cloudAccountDAO) GetAccountByName(ctx context.Context, name, tenantID string) (CloudAccount, error) {
	var account CloudAccount
	filter := bson.M{"name": name, "tenant_id": tenantID}
	err := dao.db.Collection(AccountsCollection).FindOne(ctx, filter).Decode(&account)
	return account, err
}

// ListAccounts 获取云账号列表
func (dao *cloudAccountDAO) ListAccounts(ctx context.Context, filter CloudAccountFilter) ([]CloudAccount, error) {
	var accounts []CloudAccount

	query := bson.M{}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.Environment != "" {
		query["environment"] = filter.Environment
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Name != "" {
		query["name"] = bson.M{"$regex": filter.Name, "$options": "i"}
	}

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.M{"ctime": -1})

	cursor, err := dao.db.Collection(AccountsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &accounts)
	return accounts, err
}

// CountAccounts 统计云账号数量
func (dao *cloudAccountDAO) CountAccounts(ctx context.Context, filter CloudAccountFilter) (int64, error) {
	query := bson.M{}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.Environment != "" {
		query["environment"] = filter.Environment
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.TenantID != "" {
		query["tenant_id"] = filter.TenantID
	}
	if filter.Name != "" {
		query["name"] = bson.M{"$regex": filter.Name, "$options": "i"}
	}

	return dao.db.Collection(AccountsCollection).CountDocuments(ctx, query)
}

// DeleteAccount 删除云账号
func (dao *cloudAccountDAO) DeleteAccount(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := dao.db.Collection(AccountsCollection).DeleteOne(ctx, filter)
	return err
}

// UpdateAccountStatus 更新账号状态
func (dao *cloudAccountDAO) UpdateAccountStatus(ctx context.Context, id int64, status domain.CloudAccountStatus) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"status":      status,
			"update_time": time.Now(),
			"utime":       time.Now().Unix(),
		},
	}
	_, err := dao.db.Collection(AccountsCollection).UpdateOne(ctx, filter, update)
	return err
}

// UpdateSyncTime 更新同步时间和资产数量
func (dao *cloudAccountDAO) UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"last_sync_time": syncTime,
			"asset_count":    assetCount,
			"update_time":    time.Now(),
			"utime":          time.Now().Unix(),
		},
	}
	_, err := dao.db.Collection(AccountsCollection).UpdateOne(ctx, filter, update)
	return err
}

// UpdateTestTime 更新测试时间和状态
func (dao *cloudAccountDAO) UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status domain.CloudAccountStatus, errorMsg string) error {
	filter := bson.M{"id": id}
	update := bson.M{
		"$set": bson.M{
			"last_test_time": testTime,
			"status":         status,
			"error_message":  errorMsg,
			"update_time":    time.Now(),
			"utime":          time.Now().Unix(),
		},
	}
	_, err := dao.db.Collection(AccountsCollection).UpdateOne(ctx, filter, update)
	return err
}
