package template

import (
	"context"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// mockAccountProvider for validator tests
type mockAccountProvider struct {
	accounts map[int64]*domain.CloudAccount
}

func (m *mockAccountProvider) GetByID(_ context.Context, id int64) (*domain.CloudAccount, error) {
	acc, ok := m.accounts[id]
	if !ok {
		return nil, ErrTemplateNotFound
	}
	return acc, nil
}

// mockAdapterFactory returns nil adapters (no cloud API calls in unit tests)
type mockAdapterFactory struct{}

func (m *mockAdapterFactory) GetAdapter(_ *domain.CloudAccount) (cloudx.CloudAdapter, error) {
	return nil, nil
}

// noopAdapterFactory returns a mock adapter with nil ResourceQuery
type noopAdapterFactory struct{}

type noopCloudAdapter struct{}

func (a *noopCloudAdapter) GetProvider() domain.CloudProvider           { return "mock" }
func (a *noopCloudAdapter) Asset() cloudx.AssetAdapter                  { return nil }
func (a *noopCloudAdapter) ECS() cloudx.ECSAdapter                      { return nil }
func (a *noopCloudAdapter) SecurityGroup() cloudx.SecurityGroupAdapter  { return nil }
func (a *noopCloudAdapter) Image() cloudx.ImageAdapter                  { return nil }
func (a *noopCloudAdapter) Disk() cloudx.DiskAdapter                    { return nil }
func (a *noopCloudAdapter) Snapshot() cloudx.SnapshotAdapter            { return nil }
func (a *noopCloudAdapter) RDS() cloudx.RDSAdapter                      { return nil }
func (a *noopCloudAdapter) Redis() cloudx.RedisAdapter                  { return nil }
func (a *noopCloudAdapter) MongoDB() cloudx.MongoDBAdapter              { return nil }
func (a *noopCloudAdapter) VPC() cloudx.VPCAdapter                      { return nil }
func (a *noopCloudAdapter) EIP() cloudx.EIPAdapter                      { return nil }
func (a *noopCloudAdapter) VSwitch() cloudx.VSwitchAdapter              { return nil }
func (a *noopCloudAdapter) LB() cloudx.LBAdapter                        { return nil }
func (a *noopCloudAdapter) CDN() cloudx.CDNAdapter                      { return nil }
func (a *noopCloudAdapter) WAF() cloudx.WAFAdapter                      { return nil }
func (a *noopCloudAdapter) NAS() cloudx.NASAdapter                      { return nil }
func (a *noopCloudAdapter) OSS() cloudx.OSSAdapter                      { return nil }
func (a *noopCloudAdapter) Kafka() cloudx.KafkaAdapter                  { return nil }
func (a *noopCloudAdapter) Elasticsearch() cloudx.ElasticsearchAdapter  { return nil }
func (a *noopCloudAdapter) IAM() cloudx.IAMAdapter                      { return nil }
func (a *noopCloudAdapter) ECSCreate() cloudx.ECSCreateAdapter          { return nil }
func (a *noopCloudAdapter) ResourceQuery() cloudx.ResourceQueryAdapter  { return nil }
func (a *noopCloudAdapter) ValidateCredentials(_ context.Context) error { return nil }

func (m *noopAdapterFactory) GetAdapter(_ *domain.CloudAccount) (cloudx.CloudAdapter, error) {
	return &noopCloudAdapter{}, nil
}

func validParams() types.CreateInstanceParams {
	return types.CreateInstanceParams{
		Region:           "cn-hangzhou",
		Zone:             "cn-hangzhou-a",
		InstanceType:     "ecs.g6.large",
		ImageID:          "img-abc123",
		VPCID:            "vpc-abc123",
		SubnetID:         "vsw-abc123",
		SecurityGroupIDs: []string{"sg-abc123"},
		Count:            1,
	}
}

// Feature: vm-template-provisioning, Property 11: 参数校验器统一性与完整性
// For any creation params with invalid cloud resources, the validator returns errors
// listing each invalid field. Both creation sources use the same validation logic.
//
// **Validates: Requirements 4.4, 4.5, 9.5, 9.6, 10.6**
func TestProperty_ValidatorRequiredFields(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		accounts := map[int64]*domain.CloudAccount{
			1: {ID: 1, Status: "enabled", Provider: "aliyun"},
		}
		validator := NewTemplateValidator(
			&mockAccountProvider{accounts: accounts},
			&noopAdapterFactory{},
		)
		ctx := context.Background()

		// Pick a random subset of required fields to blank out
		fields := []string{"region", "zone", "instance_type", "image_id", "vpc_id", "subnet_id", "security_group_ids"}
		blankCount := rapid.IntRange(1, len(fields)).Draw(rt, "blankCount")
		blanked := make(map[string]bool)

		// Randomly select which fields to blank
		indices := rapid.SliceOfNDistinct(rapid.IntRange(0, len(fields)-1), blankCount, blankCount, func(i int) int { return i }).Draw(rt, "indices")
		for _, idx := range indices {
			blanked[fields[idx]] = true
		}

		params := validParams()
		if blanked["region"] {
			params.Region = ""
		}
		if blanked["zone"] {
			params.Zone = ""
		}
		if blanked["instance_type"] {
			params.InstanceType = ""
		}
		if blanked["image_id"] {
			params.ImageID = ""
		}
		if blanked["vpc_id"] {
			params.VPCID = ""
		}
		if blanked["subnet_id"] {
			params.SubnetID = ""
		}
		if blanked["security_group_ids"] {
			params.SecurityGroupIDs = nil
		}

		errs := validator.ValidateParams(ctx, 1, &params)

		// Should have at least one error
		assert.NotEmpty(rt, errs, "should have validation errors when required fields are blank")

		// Each blanked field should appear in errors
		errFields := make(map[string]bool)
		for _, e := range errs {
			errFields[e.Field] = true
		}
		for field := range blanked {
			assert.True(rt, errFields[field], "blanked field %s should appear in validation errors", field)
		}
	})
}

// Feature: vm-template-provisioning, Property 11 (cont): Valid params pass validation
func TestProperty_ValidatorAcceptsValidParams(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		accounts := map[int64]*domain.CloudAccount{
			1: {ID: 1, Status: "enabled", Provider: "aliyun"},
		}
		validator := NewTemplateValidator(
			&mockAccountProvider{accounts: accounts},
			&noopAdapterFactory{},
		)
		ctx := context.Background()

		params := validParams()
		params.Count = rapid.IntRange(1, 20).Draw(rt, "count")

		errs := validator.ValidateParams(ctx, 1, &params)
		assert.Empty(rt, errs, "valid params should pass validation")
	})
}

// Feature: vm-template-provisioning, Property 11 (cont): Disabled account fails validation
func TestProperty_ValidatorRejectsDisabledAccount(t *testing.T) {
	accounts := map[int64]*domain.CloudAccount{
		1: {ID: 1, Status: "disabled", Provider: "aliyun"},
	}
	validator := NewTemplateValidator(
		&mockAccountProvider{accounts: accounts},
		&noopAdapterFactory{},
	)
	ctx := context.Background()

	params := validParams()
	errs := validator.ValidateParams(ctx, 1, &params)
	assert.NotEmpty(t, errs)
	assert.Equal(t, "cloud_account_id", errs[0].Field)
}

// Feature: vm-template-provisioning, Property 8 (partial): Count range validation
func TestProperty_ValidatorCountRange(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		accounts := map[int64]*domain.CloudAccount{
			1: {ID: 1, Status: "enabled", Provider: "aliyun"},
		}
		validator := NewTemplateValidator(
			&mockAccountProvider{accounts: accounts},
			&noopAdapterFactory{},
		)
		ctx := context.Background()

		// Invalid count
		invalidCount := rapid.OneOf(
			rapid.IntRange(-100, 0),
			rapid.IntRange(21, 1000),
		).Draw(rt, "invalidCount")

		params := validParams()
		params.Count = invalidCount

		errs := validator.ValidateParams(ctx, 1, &params)
		hasCountErr := false
		for _, e := range errs {
			if e.Field == "count" {
				hasCountErr = true
				break
			}
		}
		assert.True(rt, hasCountErr, "invalid count %d should produce count validation error", invalidCount)
	})
}
