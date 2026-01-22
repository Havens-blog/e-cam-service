package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const AccountsCollection = "cloud_accounts"

// CloudAccountStatus 云账号状态枚举
type CloudAccountStatus string

const (
	CloudAccountStatusActive   CloudAccountStatus = "active"   // 活跃状态
	CloudAccountStatusDisabled CloudAccountStatus = "disabled" // 已禁用
	CloudAccountStatusError    CloudAccountStatus = "error"    // 错误状态
	CloudAccountStatusTesting  CloudAccountStatus = "testing"  // 测试中
)

// CloudProvider 云厂商枚举
type CloudProvider string

const (
	CloudProviderAliyun  CloudProvider = "aliyun"  // 阿里云
	CloudProviderAWS     CloudProvider = "aws"     // Amazon Web Services
	CloudProviderAzure   CloudProvider = "azure"   // Microsoft Azure
	CloudProviderTencent CloudProvider = "tencent" // 腾讯云
	CloudProviderHuawei  CloudProvider = "huawei"  // 华为云
)

// Environment 环境枚举
type Environment string

const (
	EnvironmentProduction  Environment = "production"  // 生产环境
	EnvironmentStaging     Environment = "staging"     // 预发环境
	EnvironmentDevelopment Environment = "development" // 开发环境
)

// CloudAccountConfig 云账号配置信息
type CloudAccountConfig struct {
	EnableAutoSync       bool     `json:"enable_auto_sync" bson:"enable_auto_sync"`             // 是否启用自动同步
	SyncInterval         int64    `json:"sync_interval" bson:"sync_interval"`                   // 同步间隔(秒)
	ReadOnly             bool     `json:"read_only" bson:"read_only"`                           // 只读权限
	ShowSubAccounts      bool     `json:"show_sub_accounts" bson:"show_sub_accounts"`           // 显示子账号
	EnableCostMonitoring bool     `json:"enable_cost_monitoring" bson:"enable_cost_monitoring"` // 启用成本监控
	SupportedRegions     []string `json:"supported_regions" bson:"supported_regions"`           // 支持的地域列表
	SupportedAssetTypes  []string `json:"supported_asset_types" bson:"supported_asset_types"`   // 支持的资产类型
}

// CloudAccount DAO层云账号模型
type CloudAccount struct {
	ID              int64              `json:"id" bson:"id"`                               // 账号ID
	Name            string             `json:"name" bson:"name"`                           // 账号名称
	Provider        CloudProvider      `json:"provider" bson:"provider"`                   // 云厂商
	Environment     Environment        `json:"environment" bson:"environment"`             // 环境
	AccessKeyID     string             `json:"access_key_id" bson:"access_key_id"`         // 访问密钥ID
	AccessKeySecret string             `json:"access_key_secret" bson:"access_key_secret"` // 访问密钥Secret (加密存储)
	Regions         []string           `json:"regions" bson:"regions"`                     // 支持的地域列表
	Description     string             `json:"description" bson:"description"`             // 描述信息
	Status          CloudAccountStatus `json:"status" bson:"status"`                       // 账号状态
	Config          CloudAccountConfig `json:"config" bson:"config"`                       // 配置信息
	TenantID        string             `json:"tenant_id" bson:"tenant_id"`                 // 租户ID
	LastSyncTime    *time.Time         `json:"last_sync_time" bson:"last_sync_time"`       // 最后同步时间
	LastTestTime    *time.Time         `json:"last_test_time" bson:"last_test_time"`       // 最后测试时间
	AssetCount      int64              `json:"asset_count" bson:"asset_count"`             // 资产数量
	ErrorMessage    string             `json:"error_message" bson:"error_message"`         // 错误信息
	CreateTime      time.Time          `json:"create_time" bson:"create_time"`             // 创建时间
	UpdateTime      time.Time          `json:"update_time" bson:"update_time"`             // 更新时间
	CTime           int64              `json:"ctime" bson:"ctime"`                         // 创建时间戳
	UTime           int64              `json:"utime" bson:"utime"`                         // 更新时间戳
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
	UpdateAccountStatus(ctx context.Context, id int64, status CloudAccountStatus) error
	UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error
	UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status CloudAccountStatus, errorMsg string) error
}

// CloudAccountFilter DAO层过滤条件
type CloudAccountFilter struct {
	Provider    CloudProvider
	Environment Environment
	Status      CloudAccountStatus
	TenantID    string
	Name        string
	Offset      int64
	Limit       int64
}

type cloudAccountDAO struct {
	db *mongox.Mongo
}

// NewCloudAccountDAO 创建云账号DAO
func NewCloudAccountDAO(db *mongox.Mongo) CloudAccountDAO {
	return &cloudAccountDAO{
		db: db,
	}
}

// CreateAccount 创建云账号
func (dao *cloudAccountDAO) CreateAccount(ctx context.Context, account CloudAccount) (int64, error) {
	now := time.Now()
	nowUnix := now.Unix()

	account.CreateTime = now
	account.UpdateTime = now
	account.CTime = nowUnix
	account.UTime = nowUnix

	if account.ID == 0 {
		account.ID = dao.db.GetIdGenerator(AccountsCollection)
	}

	// 设置默认状态
	if account.Status == "" {
		account.Status = CloudAccountStatusActive
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
	filter := bson.M{
		"name":      name,
		"tenant_id": tenantID,
	}

	err := dao.db.Collection(AccountsCollection).FindOne(ctx, filter).Decode(&account)
	return account, err
}

// ListAccounts 获取云账号列表
func (dao *cloudAccountDAO) ListAccounts(ctx context.Context, filter CloudAccountFilter) ([]CloudAccount, error) {
	var accounts []CloudAccount

	// 构建查询条件
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

	// 设置分页选项
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
	// 构建查询条件
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
func (dao *cloudAccountDAO) UpdateAccountStatus(ctx context.Context, id int64, status CloudAccountStatus) error {
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
func (dao *cloudAccountDAO) UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status CloudAccountStatus, errorMsg string) error {
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
