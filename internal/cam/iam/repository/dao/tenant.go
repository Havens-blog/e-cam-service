package dao

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const TenantsCollection = "tenants"

// Tenant DAO layer tenant model
type Tenant struct {
	ID          string         `bson:"_id"`
	Name        string         `bson:"name"`
	DisplayName string         `bson:"display_name"`
	Description string         `bson:"description"`
	Status      string         `bson:"status"`
	Settings    TenantSettings `bson:"settings"`
	Metadata    TenantMetadata `bson:"metadata"`
	CreateTime  time.Time      `bson:"create_time"`
	UpdateTime  time.Time      `bson:"update_time"`
	CTime       int64          `bson:"ctime"`
	UTime       int64          `bson:"utime"`
}

// TenantSettings DAO layer tenant settings
type TenantSettings struct {
	MaxCloudAccounts int               `bson:"max_cloud_accounts"`
	MaxUsers         int               `bson:"max_users"`
	MaxUserGroups    int               `bson:"max_user_groups"`
	AllowedProviders []string          `bson:"allowed_providers"`
	Features         map[string]bool   `bson:"features"`
	CustomFields     map[string]string `bson:"custom_fields"`
}

// TenantMetadata DAO layer tenant metadata
type TenantMetadata struct {
	Owner        string            `bson:"owner"`
	ContactEmail string            `bson:"contact_email"`
	ContactPhone string            `bson:"contact_phone"`
	CompanyName  string            `bson:"company_name"`
	Industry     string            `bson:"industry"`
	Region       string            `bson:"region"`
	Tags         map[string]string `bson:"tags"`
}

// TenantFilter DAO layer filter conditions
type TenantFilter struct {
	Keyword  string
	Status   string
	Industry string
	Region   string
	Offset   int64
	Limit    int64
}

// TenantDAO tenant data access interface
type TenantDAO interface {
	Create(ctx context.Context, tenant Tenant) error
	Update(ctx context.Context, tenant Tenant) error
	GetByID(ctx context.Context, tenantID string) (Tenant, error)
	GetByName(ctx context.Context, name string) (Tenant, error)
	List(ctx context.Context, filter TenantFilter) ([]Tenant, error)
	Count(ctx context.Context, filter TenantFilter) (int64, error)
	Delete(ctx context.Context, tenantID string) error
}

type tenantDAO struct {
	db *mongox.Mongo
}

// NewTenantDAO creates tenant DAO
func NewTenantDAO(db *mongox.Mongo) TenantDAO {
	return &tenantDAO{
		db: db,
	}
}

// Create creates tenant
func (dao *tenantDAO) Create(ctx context.Context, tenant Tenant) error {
	now := time.Now()
	tenant.CreateTime = now
	tenant.UpdateTime = now
	tenant.CTime = now.Unix()
	tenant.UTime = now.Unix()

	// Initialize maps if nil
	if tenant.Settings.Features == nil {
		tenant.Settings.Features = make(map[string]bool)
	}
	if tenant.Settings.CustomFields == nil {
		tenant.Settings.CustomFields = make(map[string]string)
	}
	if tenant.Metadata.Tags == nil {
		tenant.Metadata.Tags = make(map[string]string)
	}

	_, err := dao.db.Collection(TenantsCollection).InsertOne(ctx, tenant)
	return err
}

// Update updates tenant
func (dao *tenantDAO) Update(ctx context.Context, tenant Tenant) error {
	tenant.UpdateTime = time.Now()
	tenant.UTime = tenant.UpdateTime.Unix()

	filter := bson.M{"_id": tenant.ID}
	update := bson.M{"$set": tenant}

	_, err := dao.db.Collection(TenantsCollection).UpdateOne(ctx, filter, update)
	return err
}

// GetByID gets tenant by ID
func (dao *tenantDAO) GetByID(ctx context.Context, tenantID string) (Tenant, error) {
	var tenant Tenant
	filter := bson.M{"_id": tenantID}

	err := dao.db.Collection(TenantsCollection).FindOne(ctx, filter).Decode(&tenant)
	return tenant, err
}

// GetByName gets tenant by name
func (dao *tenantDAO) GetByName(ctx context.Context, name string) (Tenant, error) {
	var tenant Tenant
	filter := bson.M{"name": name}

	err := dao.db.Collection(TenantsCollection).FindOne(ctx, filter).Decode(&tenant)
	return tenant, err
}

// List gets tenant list
func (dao *tenantDAO) List(ctx context.Context, filter TenantFilter) ([]Tenant, error) {
	var tenants []Tenant

	// Build query conditions
	query := dao.buildFilter(filter)

	// Build options
	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})

	cursor, err := dao.db.Collection(TenantsCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	if err = cursor.All(ctx, &tenants); err != nil {
		return nil, err
	}

	return tenants, nil
}

// Count counts tenants
func (dao *tenantDAO) Count(ctx context.Context, filter TenantFilter) (int64, error) {
	query := dao.buildFilter(filter)
	return dao.db.Collection(TenantsCollection).CountDocuments(ctx, query)
}

// Delete deletes tenant
func (dao *tenantDAO) Delete(ctx context.Context, tenantID string) error {
	filter := bson.M{"_id": tenantID}
	_, err := dao.db.Collection(TenantsCollection).DeleteOne(ctx, filter)
	return err
}

// buildFilter builds query filter
func (dao *tenantDAO) buildFilter(filter TenantFilter) bson.M {
	query := bson.M{}

	// Keyword search
	if filter.Keyword != "" {
		query["$or"] = []bson.M{
			{"name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"display_name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"description": bson.M{"$regex": filter.Keyword, "$options": "i"}},
			{"metadata.company_name": bson.M{"$regex": filter.Keyword, "$options": "i"}},
		}
	}

	// Status filter
	if filter.Status != "" {
		query["status"] = filter.Status
	}

	// Industry filter
	if filter.Industry != "" {
		query["metadata.industry"] = filter.Industry
	}

	// Region filter
	if filter.Region != "" {
		query["metadata.region"] = filter.Region
	}

	return query
}
