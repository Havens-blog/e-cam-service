package dao

import (
	"context"
	"errors"
	"time"

	"github.com/Havens-blog/e-common-go/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AccountPermissionCheckCollection 云账号权限体检快照集合
const AccountPermissionCheckCollection = "ecam_account_permission_check"

// AccountPermissionItem 权限体检单项
type AccountPermissionItem struct {
	AssetType string `json:"asset_type" bson:"asset_type"`
	AssetName string `json:"asset_name" bson:"asset_name"`
	Status    string `json:"status" bson:"status"`
	Code      string `json:"code" bson:"code"`
	Message   string `json:"message" bson:"message"`
}

// AccountPermissionSummary 权限体检汇总
type AccountPermissionSummary struct {
	OK          int `json:"ok" bson:"ok"`
	Denied      int `json:"denied" bson:"denied"`
	Error       int `json:"error" bson:"error"`
	Unsupported int `json:"unsupported" bson:"unsupported"`
}

// AccountPermissionCheck 权限体检快照（按账号唯一）
type AccountPermissionCheck struct {
	AccountID        int64                     `json:"account_id" bson:"account_id"`
	Provider         string                    `json:"provider" bson:"provider"`
	IdentityType     string                    `json:"identity_type" bson:"identity_type"`
	IsPrimaryAccount bool                      `json:"is_primary_account" bson:"is_primary_account"`
	Items            []AccountPermissionItem   `json:"items" bson:"items"`
	Summary          AccountPermissionSummary  `json:"summary" bson:"summary"`
	CheckedAt        time.Time                 `json:"checked_at" bson:"checked_at"`
	UTime            int64                     `json:"utime" bson:"utime"`
}

// AccountPermissionCheckDAO 权限体检快照 DAO
type AccountPermissionCheckDAO interface {
	// Save 按 account_id upsert 快照
	Save(ctx context.Context, check AccountPermissionCheck) error
	// GetByAccountID 读取快照，未体检过返回 (zero, false, nil)
	GetByAccountID(ctx context.Context, accountID int64) (AccountPermissionCheck, bool, error)
}

type accountPermissionCheckDAO struct {
	db *mongox.Mongo
}

// NewAccountPermissionCheckDAO 创建 DAO
func NewAccountPermissionCheckDAO(db *mongox.Mongo) AccountPermissionCheckDAO {
	return &accountPermissionCheckDAO{db: db}
}

// Save 按 account_id upsert
func (d *accountPermissionCheckDAO) Save(ctx context.Context, check AccountPermissionCheck) error {
	check.UTime = time.Now().Unix()
	filter := bson.M{"account_id": check.AccountID}
	update := bson.M{"$set": check}
	_, err := d.db.Collection(AccountPermissionCheckCollection).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// GetByAccountID 读取快照
func (d *accountPermissionCheckDAO) GetByAccountID(ctx context.Context, accountID int64) (AccountPermissionCheck, bool, error) {
	var doc AccountPermissionCheck
	err := d.db.Collection(AccountPermissionCheckCollection).FindOne(ctx, bson.M{"account_id": accountID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return AccountPermissionCheck{}, false, nil
		}
		return AccountPermissionCheck{}, false, err
	}
	return doc, true, nil
}
