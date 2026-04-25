package template

import (
	"context"
	"time"

	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	VMTemplateCollection    = "ecam_vm_template"
	ProvisionTaskCollection = "ecam_provision_task"
)

// ============================================================================
// TemplateDAO - 模板数据访问接口
// ============================================================================

// TemplateDAO 模板数据访问接口
type TemplateDAO interface {
	Insert(ctx context.Context, tmpl VMTemplate) (int64, error)
	Update(ctx context.Context, tenantID string, id int64, req UpdateTemplateReq) error
	Delete(ctx context.Context, tenantID string, id int64) error
	GetByID(ctx context.Context, tenantID string, id int64) (VMTemplate, error)
	GetByName(ctx context.Context, tenantID, name string) (VMTemplate, error)
	List(ctx context.Context, filter TemplateFilter) ([]VMTemplate, int64, error)
}

type templateDAO struct {
	db *mongox.Mongo
}

// NewTemplateDAO 创建模板 DAO
func NewTemplateDAO(db *mongox.Mongo) TemplateDAO {
	return &templateDAO{db: db}
}

func (d *templateDAO) Insert(ctx context.Context, tmpl VMTemplate) (int64, error) {
	now := time.Now().UnixMilli()
	tmpl.Ctime = now
	tmpl.Utime = now
	if tmpl.ID == 0 {
		tmpl.ID = d.db.GetIdGenerator(VMTemplateCollection)
	}
	_, err := d.db.Collection(VMTemplateCollection).InsertOne(ctx, tmpl)
	if err != nil {
		return 0, err
	}
	return tmpl.ID, nil
}

func (d *templateDAO) Update(ctx context.Context, tenantID string, id int64, req UpdateTemplateReq) error {
	filter := bson.M{"id": id, "tenant_id": tenantID}
	setFields := bson.M{"utime": time.Now().UnixMilli()}

	if req.Name != nil {
		setFields["name"] = *req.Name
	}
	if req.Description != nil {
		setFields["description"] = *req.Description
	}
	if req.Provider != nil {
		setFields["provider"] = *req.Provider
	}
	if req.CloudAccountID != nil {
		setFields["cloud_account_id"] = *req.CloudAccountID
	}
	if req.Region != nil {
		setFields["region"] = *req.Region
	}
	if req.Zone != nil {
		setFields["zone"] = *req.Zone
	}
	if req.InstanceType != nil {
		setFields["instance_type"] = *req.InstanceType
	}
	if req.ImageID != nil {
		setFields["image_id"] = *req.ImageID
	}
	if req.VPCID != nil {
		setFields["vpc_id"] = *req.VPCID
	}
	if req.SubnetID != nil {
		setFields["subnet_id"] = *req.SubnetID
	}
	if req.SecurityGroupIDs != nil {
		setFields["security_group_ids"] = *req.SecurityGroupIDs
	}
	if req.InstanceNamePrefix != nil {
		setFields["instance_name_prefix"] = *req.InstanceNamePrefix
	}
	if req.HostNamePrefix != nil {
		setFields["host_name_prefix"] = *req.HostNamePrefix
	}
	if req.SystemDiskType != nil {
		setFields["system_disk_type"] = *req.SystemDiskType
	}
	if req.SystemDiskSize != nil {
		setFields["system_disk_size"] = *req.SystemDiskSize
	}
	if req.DataDisks != nil {
		setFields["data_disks"] = *req.DataDisks
	}
	if req.BandwidthOut != nil {
		setFields["bandwidth_out"] = *req.BandwidthOut
	}
	if req.ChargeType != nil {
		setFields["charge_type"] = *req.ChargeType
	}
	if req.KeyPairName != nil {
		setFields["key_pair_name"] = *req.KeyPairName
	}
	if req.Tags != nil {
		setFields["tags"] = *req.Tags
	}

	update := bson.M{"$set": setFields}
	_, err := d.db.Collection(VMTemplateCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *templateDAO) Delete(ctx context.Context, tenantID string, id int64) error {
	filter := bson.M{"id": id, "tenant_id": tenantID}
	_, err := d.db.Collection(VMTemplateCollection).DeleteOne(ctx, filter)
	return err
}

func (d *templateDAO) GetByID(ctx context.Context, tenantID string, id int64) (VMTemplate, error) {
	var tmpl VMTemplate
	filter := bson.M{"id": id, "tenant_id": tenantID}
	err := d.db.Collection(VMTemplateCollection).FindOne(ctx, filter).Decode(&tmpl)
	return tmpl, err
}

func (d *templateDAO) GetByName(ctx context.Context, tenantID, name string) (VMTemplate, error) {
	var tmpl VMTemplate
	filter := bson.M{"tenant_id": tenantID, "name": name}
	err := d.db.Collection(VMTemplateCollection).FindOne(ctx, filter).Decode(&tmpl)
	return tmpl, err
}

func (d *templateDAO) List(ctx context.Context, filter TemplateFilter) ([]VMTemplate, int64, error) {
	query := bson.M{"tenant_id": filter.TenantID}
	if filter.Name != "" {
		query["name"] = primitive.Regex{Pattern: filter.Name, Options: "i"}
	}
	if filter.Provider != "" {
		query["provider"] = filter.Provider
	}
	if filter.CloudAccountID > 0 {
		query["cloud_account_id"] = filter.CloudAccountID
	}

	total, err := d.db.Collection(VMTemplateCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(VMTemplateCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var templates []VMTemplate
	if err = cursor.All(ctx, &templates); err != nil {
		return nil, 0, err
	}
	return templates, total, nil
}

// ============================================================================
// ProvisionTaskDAO - 创建任务数据访问接口
// ============================================================================

// ProvisionTaskDAO 创建任务数据访问接口
type ProvisionTaskDAO interface {
	Insert(ctx context.Context, task ProvisionTask) error
	UpdateStatus(ctx context.Context, taskID, status, message string) error
	UpdateProgress(ctx context.Context, taskID string, progress, successCount, failedCount int) error
	UpdateInstances(ctx context.Context, taskID string, instances []ProvisionInstanceResult) error
	UpdateSyncStatus(ctx context.Context, taskID, syncStatus string) error
	GetByID(ctx context.Context, tenantID, taskID string) (ProvisionTask, error)
	List(ctx context.Context, filter ProvisionTaskFilter) ([]ProvisionTask, int64, error)
	CountRunningByTemplateID(ctx context.Context, tenantID string, templateID int64) (int64, error)
}

type provisionTaskDAO struct {
	db *mongox.Mongo
}

// NewProvisionTaskDAO 创建任务 DAO
func NewProvisionTaskDAO(db *mongox.Mongo) ProvisionTaskDAO {
	return &provisionTaskDAO{db: db}
}

func (d *provisionTaskDAO) Insert(ctx context.Context, task ProvisionTask) error {
	now := time.Now().UnixMilli()
	task.Ctime = now
	task.Utime = now
	_, err := d.db.Collection(ProvisionTaskCollection).InsertOne(ctx, task)
	return err
}

func (d *provisionTaskDAO) UpdateStatus(ctx context.Context, taskID, status, message string) error {
	filter := bson.M{"_id": taskID}
	update := bson.M{
		"$set": bson.M{
			"status":  status,
			"message": message,
			"utime":   time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(ProvisionTaskCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *provisionTaskDAO) UpdateProgress(ctx context.Context, taskID string, progress, successCount, failedCount int) error {
	filter := bson.M{"_id": taskID}
	update := bson.M{
		"$set": bson.M{
			"progress":      progress,
			"success_count": successCount,
			"failed_count":  failedCount,
			"utime":         time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(ProvisionTaskCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *provisionTaskDAO) UpdateInstances(ctx context.Context, taskID string, instances []ProvisionInstanceResult) error {
	filter := bson.M{"_id": taskID}
	update := bson.M{
		"$set": bson.M{
			"instances": instances,
			"utime":     time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(ProvisionTaskCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *provisionTaskDAO) UpdateSyncStatus(ctx context.Context, taskID, syncStatus string) error {
	filter := bson.M{"_id": taskID}
	update := bson.M{
		"$set": bson.M{
			"sync_status": syncStatus,
			"utime":       time.Now().UnixMilli(),
		},
	}
	_, err := d.db.Collection(ProvisionTaskCollection).UpdateOne(ctx, filter, update)
	return err
}

func (d *provisionTaskDAO) GetByID(ctx context.Context, tenantID, taskID string) (ProvisionTask, error) {
	var task ProvisionTask
	filter := bson.M{"_id": taskID}
	if tenantID != "" {
		filter["tenant_id"] = tenantID
	}
	err := d.db.Collection(ProvisionTaskCollection).FindOne(ctx, filter).Decode(&task)
	return task, err
}

func (d *provisionTaskDAO) List(ctx context.Context, filter ProvisionTaskFilter) ([]ProvisionTask, int64, error) {
	query := bson.M{"tenant_id": filter.TenantID}
	if filter.TemplateID > 0 {
		query["template_id"] = filter.TemplateID
	}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.Source != "" {
		query["source"] = filter.Source
	}
	if filter.StartTime > 0 {
		if _, ok := query["ctime"]; !ok {
			query["ctime"] = bson.M{}
		}
		query["ctime"].(bson.M)["$gte"] = filter.StartTime
	}
	if filter.EndTime > 0 {
		if _, ok := query["ctime"]; !ok {
			query["ctime"] = bson.M{}
		}
		query["ctime"].(bson.M)["$lte"] = filter.EndTime
	}

	total, err := d.db.Collection(ProvisionTaskCollection).CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find()
	if filter.Offset > 0 {
		opts.SetSkip(filter.Offset)
	}
	if filter.Limit > 0 {
		opts.SetLimit(filter.Limit)
	}
	opts.SetSort(bson.D{{Key: "ctime", Value: -1}})

	cursor, err := d.db.Collection(ProvisionTaskCollection).Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var tasks []ProvisionTask
	if err = cursor.All(ctx, &tasks); err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

func (d *provisionTaskDAO) CountRunningByTemplateID(ctx context.Context, tenantID string, templateID int64) (int64, error) {
	filter := bson.M{
		"tenant_id":   tenantID,
		"template_id": templateID,
		"source":      SourceFromTemplate,
		"status":      bson.M{"$in": []string{TaskStatusPending, TaskStatusRunning}},
	}
	return d.db.Collection(ProvisionTaskCollection).CountDocuments(ctx, filter)
}
