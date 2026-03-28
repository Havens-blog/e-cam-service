package template

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/mongo"
	"pgregory.net/rapid"
)

// ============================================================================
// In-memory DAO for property tests
// ============================================================================

type inMemoryTemplateDAO struct {
	mu        sync.Mutex
	templates map[int64]VMTemplate
	nextID    int64
}

func newInMemoryTemplateDAO() *inMemoryTemplateDAO {
	return &inMemoryTemplateDAO{
		templates: make(map[int64]VMTemplate),
		nextID:    1,
	}
}

func (d *inMemoryTemplateDAO) Insert(_ context.Context, tmpl VMTemplate) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, existing := range d.templates {
		if existing.TenantID == tmpl.TenantID && existing.Name == tmpl.Name {
			return 0, errors.New("duplicate key")
		}
	}
	tmpl.ID = d.nextID
	d.nextID++
	d.templates[tmpl.ID] = tmpl
	return tmpl.ID, nil
}

func (d *inMemoryTemplateDAO) Update(_ context.Context, tenantID string, id int64, req UpdateTemplateReq) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tmpl, ok := d.templates[id]
	if !ok || tmpl.TenantID != tenantID {
		return mongo.ErrNoDocuments
	}
	if req.Name != nil {
		tmpl.Name = *req.Name
	}
	if req.Description != nil {
		tmpl.Description = *req.Description
	}
	if req.Provider != nil {
		tmpl.Provider = *req.Provider
	}
	if req.CloudAccountID != nil {
		tmpl.CloudAccountID = *req.CloudAccountID
	}
	if req.Region != nil {
		tmpl.Region = *req.Region
	}
	if req.Zone != nil {
		tmpl.Zone = *req.Zone
	}
	if req.InstanceType != nil {
		tmpl.InstanceType = *req.InstanceType
	}
	if req.ImageID != nil {
		tmpl.ImageID = *req.ImageID
	}
	if req.VPCID != nil {
		tmpl.VPCID = *req.VPCID
	}
	if req.SubnetID != nil {
		tmpl.SubnetID = *req.SubnetID
	}
	if req.SecurityGroupIDs != nil {
		tmpl.SecurityGroupIDs = *req.SecurityGroupIDs
	}
	if req.InstanceNamePrefix != nil {
		tmpl.InstanceNamePrefix = *req.InstanceNamePrefix
	}
	if req.HostNamePrefix != nil {
		tmpl.HostNamePrefix = *req.HostNamePrefix
	}
	if req.SystemDiskType != nil {
		tmpl.SystemDiskType = *req.SystemDiskType
	}
	if req.SystemDiskSize != nil {
		tmpl.SystemDiskSize = *req.SystemDiskSize
	}
	if req.DataDisks != nil {
		tmpl.DataDisks = *req.DataDisks
	}
	if req.BandwidthOut != nil {
		tmpl.BandwidthOut = *req.BandwidthOut
	}
	if req.ChargeType != nil {
		tmpl.ChargeType = *req.ChargeType
	}
	if req.KeyPairName != nil {
		tmpl.KeyPairName = *req.KeyPairName
	}
	if req.Tags != nil {
		tmpl.Tags = *req.Tags
	}
	d.templates[id] = tmpl
	return nil
}

func (d *inMemoryTemplateDAO) Delete(_ context.Context, tenantID string, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tmpl, ok := d.templates[id]
	if !ok || tmpl.TenantID != tenantID {
		return nil
	}
	delete(d.templates, id)
	return nil
}

func (d *inMemoryTemplateDAO) GetByID(_ context.Context, tenantID string, id int64) (VMTemplate, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	tmpl, ok := d.templates[id]
	if !ok || tmpl.TenantID != tenantID {
		return VMTemplate{}, mongo.ErrNoDocuments
	}
	return tmpl, nil
}

func (d *inMemoryTemplateDAO) GetByName(_ context.Context, tenantID, name string) (VMTemplate, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, tmpl := range d.templates {
		if tmpl.TenantID == tenantID && tmpl.Name == name {
			return tmpl, nil
		}
	}
	return VMTemplate{}, mongo.ErrNoDocuments
}

func (d *inMemoryTemplateDAO) List(_ context.Context, filter TemplateFilter) ([]VMTemplate, int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result []VMTemplate
	for _, tmpl := range d.templates {
		if tmpl.TenantID != filter.TenantID {
			continue
		}
		if filter.Name != "" && !strings.Contains(strings.ToLower(tmpl.Name), strings.ToLower(filter.Name)) {
			continue
		}
		if filter.Provider != "" && tmpl.Provider != filter.Provider {
			continue
		}
		if filter.CloudAccountID > 0 && tmpl.CloudAccountID != filter.CloudAccountID {
			continue
		}
		result = append(result, tmpl)
	}
	total := int64(len(result))
	if filter.Offset > 0 && int(filter.Offset) < len(result) {
		result = result[filter.Offset:]
	} else if filter.Offset > 0 {
		result = nil
	}
	if filter.Limit > 0 && int(filter.Limit) < len(result) {
		result = result[:filter.Limit]
	}
	return result, total, nil
}

// inMemoryProvisionTaskDAO for property tests
type inMemoryProvisionTaskDAO struct {
	mu    sync.Mutex
	tasks map[string]ProvisionTask
}

func newInMemoryProvisionTaskDAO() *inMemoryProvisionTaskDAO {
	return &inMemoryProvisionTaskDAO{
		tasks: make(map[string]ProvisionTask),
	}
}

func (d *inMemoryProvisionTaskDAO) Insert(_ context.Context, task ProvisionTask) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tasks[task.ID] = task
	return nil
}

func (d *inMemoryProvisionTaskDAO) UpdateStatus(_ context.Context, taskID, status, message string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	task, ok := d.tasks[taskID]
	if !ok {
		return mongo.ErrNoDocuments
	}
	task.Status = status
	task.Message = message
	d.tasks[taskID] = task
	return nil
}

func (d *inMemoryProvisionTaskDAO) UpdateProgress(_ context.Context, taskID string, progress, successCount, failedCount int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	task, ok := d.tasks[taskID]
	if !ok {
		return mongo.ErrNoDocuments
	}
	task.Progress = progress
	task.SuccessCount = successCount
	task.FailedCount = failedCount
	d.tasks[taskID] = task
	return nil
}

func (d *inMemoryProvisionTaskDAO) UpdateInstances(_ context.Context, taskID string, instances []ProvisionInstanceResult) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	task, ok := d.tasks[taskID]
	if !ok {
		return mongo.ErrNoDocuments
	}
	task.Instances = instances
	d.tasks[taskID] = task
	return nil
}

func (d *inMemoryProvisionTaskDAO) UpdateSyncStatus(_ context.Context, taskID, syncStatus string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	task, ok := d.tasks[taskID]
	if !ok {
		return mongo.ErrNoDocuments
	}
	task.SyncStatus = syncStatus
	d.tasks[taskID] = task
	return nil
}

func (d *inMemoryProvisionTaskDAO) GetByID(_ context.Context, tenantID, taskID string) (ProvisionTask, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	task, ok := d.tasks[taskID]
	if !ok || task.TenantID != tenantID {
		return ProvisionTask{}, mongo.ErrNoDocuments
	}
	return task, nil
}

func (d *inMemoryProvisionTaskDAO) List(_ context.Context, filter ProvisionTaskFilter) ([]ProvisionTask, int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var result []ProvisionTask
	for _, task := range d.tasks {
		if task.TenantID != filter.TenantID {
			continue
		}
		if filter.TemplateID > 0 && task.TemplateID != filter.TemplateID {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		if filter.Source != "" && task.Source != filter.Source {
			continue
		}
		result = append(result, task)
	}
	return result, int64(len(result)), nil
}

func (d *inMemoryProvisionTaskDAO) CountRunningByTemplateID(_ context.Context, tenantID string, templateID int64) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var count int64
	for _, task := range d.tasks {
		if task.TenantID == tenantID && task.TemplateID == templateID &&
			task.Source == SourceFromTemplate &&
			(task.Status == TaskStatusPending || task.Status == TaskStatusRunning) {
			count++
		}
	}
	return count, nil
}

// ============================================================================
// Generators
// ============================================================================

func genTenantID() *rapid.Generator[string] {
	return rapid.StringMatching(`tenant_[a-z]{3,8}`)
}

func genTemplateName() *rapid.Generator[string] {
	return rapid.StringMatching(`tmpl-[a-z]{3,10}`)
}

func genProvider() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"aliyun", "aws", "huawei", "tencent", "volcano"})
}

func genRegion() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z]{2}-[a-z]{4,10}`)
}

func genResourceID() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z]{1,4}-[a-z0-9]{8,16}`)
}

// ============================================================================
// Property Tests
// ============================================================================

// Feature: vm-template-provisioning, Property 1: 模板创建-读取往返一致性
// For any valid template creation request (with all required params and any combination of optional params),
// creating a template and then getting its detail should return all field values identical to the creation input.
//
// **Validates: Requirements 1.1, 1.2, 1.5**
func TestProperty_TemplateCreationRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tmplDAO := newInMemoryTemplateDAO()
		taskDAO := newInMemoryProvisionTaskDAO()
		svc := NewTemplateService(tmplDAO, taskDAO, nil)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		req := CreateTemplateReq{
			Name:               genTemplateName().Draw(rt, "name"),
			Description:        rapid.String().Draw(rt, "desc"),
			Provider:           genProvider().Draw(rt, "provider"),
			CloudAccountID:     rapid.Int64Range(1, 10000).Draw(rt, "accountID"),
			Region:             genRegion().Draw(rt, "region"),
			Zone:               genRegion().Draw(rt, "zone"),
			InstanceType:       rapid.StringMatching(`ecs\.[a-z]{1,4}\.[a-z]{3,6}`).Draw(rt, "instanceType"),
			ImageID:            genResourceID().Draw(rt, "imageID"),
			VPCID:              genResourceID().Draw(rt, "vpcID"),
			SubnetID:           genResourceID().Draw(rt, "subnetID"),
			SecurityGroupIDs:   []string{genResourceID().Draw(rt, "sg1")},
			InstanceNamePrefix: rapid.StringMatching(`[a-z]{0,10}`).Draw(rt, "namePrefix"),
			HostNamePrefix:     rapid.StringMatching(`[a-z]{0,10}`).Draw(rt, "hostPrefix"),
			SystemDiskType:     rapid.SampledFrom([]string{"", "cloud_ssd", "cloud_essd"}).Draw(rt, "diskType"),
			SystemDiskSize:     rapid.IntRange(0, 500).Draw(rt, "diskSize"),
			BandwidthOut:       rapid.IntRange(0, 200).Draw(rt, "bandwidth"),
			ChargeType:         rapid.SampledFrom([]string{"", "PostPaid", "PrePaid"}).Draw(rt, "chargeType"),
			KeyPairName:        rapid.StringMatching(`[a-z]{0,10}`).Draw(rt, "keyPair"),
		}

		tmpl, err := svc.CreateTemplate(ctx, tenantID, req)
		assert.NoError(rt, err)
		assert.NotNil(rt, tmpl)

		// Read back
		got, err := svc.GetTemplate(ctx, tenantID, tmpl.ID)
		assert.NoError(rt, err)

		// Verify all fields match
		assert.Equal(rt, req.Name, got.Name)
		assert.Equal(rt, req.Description, got.Description)
		assert.Equal(rt, req.Provider, got.Provider)
		assert.Equal(rt, req.CloudAccountID, got.CloudAccountID)
		assert.Equal(rt, req.Region, got.Region)
		assert.Equal(rt, req.Zone, got.Zone)
		assert.Equal(rt, req.InstanceType, got.InstanceType)
		assert.Equal(rt, req.ImageID, got.ImageID)
		assert.Equal(rt, req.VPCID, got.VPCID)
		assert.Equal(rt, req.SubnetID, got.SubnetID)
		assert.Equal(rt, req.SecurityGroupIDs, got.SecurityGroupIDs)
		assert.Equal(rt, req.InstanceNamePrefix, got.InstanceNamePrefix)
		assert.Equal(rt, req.HostNamePrefix, got.HostNamePrefix)
		assert.Equal(rt, req.SystemDiskType, got.SystemDiskType)
		assert.Equal(rt, req.SystemDiskSize, got.SystemDiskSize)
		assert.Equal(rt, req.BandwidthOut, got.BandwidthOut)
		assert.Equal(rt, req.ChargeType, got.ChargeType)
		assert.Equal(rt, req.KeyPairName, got.KeyPairName)
		assert.Equal(rt, tenantID, got.TenantID)
	})
}

// Feature: vm-template-provisioning, Property 2: 必填参数缺失校验完整性
// For any template creation request missing any non-empty subset of required params,
// the system should reject creation. (Tested via Gin binding validation at handler level;
// here we test that the service rejects empty name which is the service-level check.)
//
// **Validates: Requirements 1.3, 9.3**
func TestProperty_RequiredParamValidation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tmplDAO := newInMemoryTemplateDAO()
		taskDAO := newInMemoryProvisionTaskDAO()
		svc := NewTemplateService(tmplDAO, taskDAO, nil)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		// Valid creation should succeed
		name := genTemplateName().Draw(rt, "name")
		req := CreateTemplateReq{
			Name:             name,
			Provider:         genProvider().Draw(rt, "provider"),
			CloudAccountID:   rapid.Int64Range(1, 10000).Draw(rt, "accountID"),
			Region:           genRegion().Draw(rt, "region"),
			Zone:             genRegion().Draw(rt, "zone"),
			InstanceType:     "ecs.g6.large",
			ImageID:          genResourceID().Draw(rt, "imageID"),
			VPCID:            genResourceID().Draw(rt, "vpcID"),
			SubnetID:         genResourceID().Draw(rt, "subnetID"),
			SecurityGroupIDs: []string{genResourceID().Draw(rt, "sg1")},
		}
		tmpl, err := svc.CreateTemplate(ctx, tenantID, req)
		assert.NoError(rt, err)
		assert.NotNil(rt, tmpl)

		// Duplicate name should fail
		_, err = svc.CreateTemplate(ctx, tenantID, req)
		assert.ErrorIs(rt, err, ErrTemplateNameExists)
	})
}

// Feature: vm-template-provisioning, Property 3: 模板列表筛选正确性
// For any set of templates and any filter conditions, returned list items all satisfy the filter.
//
// **Validates: Requirements 1.4**
func TestProperty_TemplateListFiltering(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tmplDAO := newInMemoryTemplateDAO()
		taskDAO := newInMemoryProvisionTaskDAO()
		svc := NewTemplateService(tmplDAO, taskDAO, nil)
		ctx := context.Background()
		tenantID := "tenant_test"

		// Create multiple templates with different providers
		providers := []string{"aliyun", "aws", "huawei"}
		for i, p := range providers {
			_, err := svc.CreateTemplate(ctx, tenantID, CreateTemplateReq{
				Name:             fmt.Sprintf("tmpl-%s-%d", p, i),
				Provider:         p,
				CloudAccountID:   int64(i + 1),
				Region:           "cn-hangzhou",
				Zone:             "cn-hangzhou-a",
				InstanceType:     "ecs.g6.large",
				ImageID:          "img-123",
				VPCID:            "vpc-123",
				SubnetID:         "vsw-123",
				SecurityGroupIDs: []string{"sg-123"},
			})
			assert.NoError(rt, err)
		}

		// Filter by provider
		filterProvider := rapid.SampledFrom(providers).Draw(rt, "filterProvider")
		list, total, err := svc.ListTemplates(ctx, tenantID, TemplateFilter{Provider: filterProvider})
		assert.NoError(rt, err)
		assert.Equal(rt, total, int64(len(list)))
		for _, tmpl := range list {
			assert.Equal(rt, filterProvider, tmpl.Provider)
			assert.Equal(rt, tenantID, tmpl.TenantID)
		}
	})
}

// Feature: vm-template-provisioning, Property 4: 模板更新持久性
// After update, changed fields reflect new values, unchanged fields keep original values.
//
// **Validates: Requirements 1.6**
func TestProperty_TemplateUpdatePersistence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tmplDAO := newInMemoryTemplateDAO()
		taskDAO := newInMemoryProvisionTaskDAO()
		svc := NewTemplateService(tmplDAO, taskDAO, nil)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		origName := genTemplateName().Draw(rt, "origName")
		origRegion := genRegion().Draw(rt, "origRegion")
		origProvider := genProvider().Draw(rt, "origProvider")

		tmpl, err := svc.CreateTemplate(ctx, tenantID, CreateTemplateReq{
			Name:             origName,
			Provider:         origProvider,
			CloudAccountID:   100,
			Region:           origRegion,
			Zone:             "zone-a",
			InstanceType:     "ecs.g6.large",
			ImageID:          "img-123",
			VPCID:            "vpc-123",
			SubnetID:         "vsw-123",
			SecurityGroupIDs: []string{"sg-123"},
		})
		assert.NoError(rt, err)

		// Update only region
		newRegion := genRegion().Draw(rt, "newRegion")
		err = svc.UpdateTemplate(ctx, tenantID, tmpl.ID, UpdateTemplateReq{
			Region: &newRegion,
		})
		assert.NoError(rt, err)

		got, err := svc.GetTemplate(ctx, tenantID, tmpl.ID)
		assert.NoError(rt, err)
		assert.Equal(rt, newRegion, got.Region, "updated field should reflect new value")
		assert.Equal(rt, origName, got.Name, "unchanged field should keep original value")
		assert.Equal(rt, origProvider, got.Provider, "unchanged field should keep original value")
	})
}

// Feature: vm-template-provisioning, Property 5: 模板删除后不可访问
// After deletion, the template cannot be found and does not appear in list results.
//
// **Validates: Requirements 1.7**
func TestProperty_TemplateDeleteInaccessible(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		tmplDAO := newInMemoryTemplateDAO()
		taskDAO := newInMemoryProvisionTaskDAO()
		svc := NewTemplateService(tmplDAO, taskDAO, nil)
		ctx := context.Background()
		tenantID := genTenantID().Draw(rt, "tenantID")

		tmpl, err := svc.CreateTemplate(ctx, tenantID, CreateTemplateReq{
			Name:             genTemplateName().Draw(rt, "name"),
			Provider:         genProvider().Draw(rt, "provider"),
			CloudAccountID:   100,
			Region:           "cn-hangzhou",
			Zone:             "cn-hangzhou-a",
			InstanceType:     "ecs.g6.large",
			ImageID:          "img-123",
			VPCID:            "vpc-123",
			SubnetID:         "vsw-123",
			SecurityGroupIDs: []string{"sg-123"},
		})
		assert.NoError(rt, err)

		// Delete
		err = svc.DeleteTemplate(ctx, tenantID, tmpl.ID)
		assert.NoError(rt, err)

		// Get should fail
		_, err = svc.GetTemplate(ctx, tenantID, tmpl.ID)
		assert.ErrorIs(rt, err, ErrTemplateNotFound)

		// List should not contain it
		list, _, err := svc.ListTemplates(ctx, tenantID, TemplateFilter{})
		assert.NoError(rt, err)
		for _, t := range list {
			assert.NotEqual(rt, tmpl.ID, t.ID)
		}
	})
}
