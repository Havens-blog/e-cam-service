package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CloudPolicyTemplatesCollection = "cloud_policy_templates"

// TemplateCategory 模板分类
type TemplateCategory string

const (
	TemplateCategoryReadOnly  TemplateCategory = "read_only"
	TemplateCategoryAdmin     TemplateCategory = "admin"
	TemplateCategoryDeveloper TemplateCategory = "developer"
	TemplateCategoryCustom    TemplateCategory = "custom"
)

// PolicyTemplate DAO层策略模板模型
type PolicyTemplate struct {
	ID             int64              `bson:"id"`
	Name           string             `bson:"name"`
	Description    string             `bson:"description"`
	Category       TemplateCategory   `bson:"category"`
	Policies       []PermissionPolicy `bson:"policies"`
	CloudPlatforms []CloudProvider    `bson:"cloud_platforms"`
	IsBuiltIn      bool               `bson:"is_built_in"`
	TenantID       string             `bson:"tenant_id"`
	CreateTime     time.Time          `bson:"create_time"`
	UpdateTime     time.Time          `bson:"update_time"`
	CTime          int64              `bson:"ctime"`
	UTime          int64              `bson:"utime"`
}

// TemplateFilter DAO层过滤条件
type TemplateFilter struct {
	Category  TemplateCategory
	IsBuiltIn *bool
	TenantID  string
	Keyword   string
	Offset    int64
	Limit     int64
}

// PolicyTemplateDAO 策略模板数据访问接口
type PolicyTemplateDAO interface {
	Create(ctx context.Context, template PolicyTemplate) (int64, error)
	Update(ctx context.Context, template PolicyTemplate) error
	GetByID(ctx context.Context, id int64) (PolicyTemplate, error)
	GetByName(ctx context.Context, name, tenantID string) (PolicyTemplate, error)
	List(ctx context.Context, filter TemplateFilter) ([]PolicyTemplate, error)
	Count(ctx context.Context, filter TemplateFilter) (int64, error)
	Delete(ctx context.Context, id int64) error
	ListBuiltInTemplates(ctx context.Context) ([]PolicyTemplate, error)
}

type policyTemplateDAO struct {
	db *mongox.Mongo
}

// NewPolicyTemplateDAO 创建策略模板DAO
func NewPolicyTemplateDAO(db *mongox.Mongo) PolicyTemplateDAO {
	return &policyTemplateDAO{
		db: db,
	}
}

// Create 创建策略模板
func (dao *policyTemplateDAO) Create(ctx context.Context, template PolicyTemplate) (int64, error) {
	now := time.Now()
	nowUnix := now.Unix()

	template.CreateTime = now
	template.UpdateTime = now
	template.CTime = nowUnix
	template.UTime = nowUnix

	if template.ID == 0 {
		template.ID = dao.db.GetIdGenerator(CloudPolicyTemplatesCollection)
	}

	// 初始化策略列表
	if template.Policies == nil {
		template.Policies = []PermissionPolicy{}
	}

	// 初始化云平台列表
	if template.CloudPlatforms == nil {
		template.CloudPlatforms = []CloudProvider{}
	}

	_, err := dao.db.Collection(CloudPolicyTemplatesCollection).InsertOne(ctx, template)
	if err != nil {
		return 0, err
	}

	return template.ID, nil
}

// Update 更新策略模板
func (dao *policyTemplateDAO) Update(ctx context.Context, template PolicyTemplate) error {
	template.UpdateTime = time.Now()
	template.UTime = template.UpdateTime.Unix()

	filter := bson.M{"id": template.ID}
	update := bson.M{"$set": template}

	_, err := dao.db.Collection(CloudPolicyTemplatesCollection).UpdateOne(ctx, filter, update)
	return err
}

// GetByID 根据ID获取策略模板
func (dao *policyTemplateDAO) GetByID(ctx context.Context, id int64) (PolicyTemplate, error) {
	var template PolicyTemplate
	filter := bson.M{"id": id}

	err := dao.db.Collection(CloudPolicyTemplatesCollection).FindOne(ctx, filter).Decode(&template)
	return template, err
}

// GetByName 根据名称和租户ID获取策略模板
func (dao *policyTemplateDAO) GetByName(ctx context.Context, name, tenantID string) (PolicyTemplate, error) {
	var template PolicyTemplate
	filter := bson.M{
		"name":      name,
		"tenant_id": tenantID,
	}

	err := dao.db.Collection(CloudPolicyTemplatesCollection).FindOne(ctx, filter).Decode(&template)
	return template, err
}

// List 获取策略模板列表
func (dao *policyTemplateDAO) List(ctx context.Context, filter TemplateFilter) ([]PolicyTemplate, error) {
	var templates []PolicyTemplate

	// 构建查询条件
	query := bson.M{}
	if filter.Category != "" {
		query["category"] = filter.Category
	}
	if filter.IsBuiltIn != nil {
		query["is_built_in"] = *filter.IsBuiltIn
	}
	if filter.TenantID != "" {
		query["$or"] = []bson.M{
			{"tenant_id": filter.TenantID},
			{"is_built_in": true}, // 内置模板对所有租户可见
		}
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
	opts.SetSort(bson.M{"is_built_in": -1, "ctime": -1})

	cursor, err := dao.db.Collection(CloudPolicyTemplatesCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &templates)
	return templates, err
}

// Count 统计策略模板数量
func (dao *policyTemplateDAO) Count(ctx context.Context, filter TemplateFilter) (int64, error) {
	// 构建查询条件
	query := bson.M{}
	if filter.Category != "" {
		query["category"] = filter.Category
	}
	if filter.IsBuiltIn != nil {
		query["is_built_in"] = *filter.IsBuiltIn
	}
	if filter.TenantID != "" {
		query["$or"] = []bson.M{
			{"tenant_id": filter.TenantID},
			{"is_built_in": true},
		}
	}
	if filter.Keyword != "" {
		query["$or"] = []bson.M{
			{"name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"description": bson.M{"$regex": filter.Keyword, "$options": "i"}},
		}
	}

	return dao.db.Collection(CloudPolicyTemplatesCollection).CountDocuments(ctx, query)
}

// Delete 删除策略模板
func (dao *policyTemplateDAO) Delete(ctx context.Context, id int64) error {
	filter := bson.M{"id": id}
	_, err := dao.db.Collection(CloudPolicyTemplatesCollection).DeleteOne(ctx, filter)
	return err
}

// ListBuiltInTemplates 获取所有内置模板
func (dao *policyTemplateDAO) ListBuiltInTemplates(ctx context.Context) ([]PolicyTemplate, error) {
	var templates []PolicyTemplate

	query := bson.M{"is_built_in": true}
	opts := options.Find().SetSort(bson.M{"category": 1, "ctime": 1})

	cursor, err := dao.db.Collection(CloudPolicyTemplatesCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &templates)
	return templates, err
}
