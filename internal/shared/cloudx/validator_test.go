package cloudx

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudValidatorFactory(t *testing.T) {
	factory := NewCloudValidatorFactory()

	tests := []struct {
		name     string
		provider domain.CloudProvider
		wantErr  bool
	}{
		{
			name:     "阿里云验证器",
			provider: domain.CloudProviderAliyun,
			wantErr:  false,
		},
		{
			name:     "AWS验证器",
			provider: domain.CloudProviderAWS,
			wantErr:  false,
		},
		{
			name:     "Azure验证器",
			provider: domain.CloudProviderAzure,
			wantErr:  false,
		},
		{
			name:     "腾讯云验证器",
			provider: domain.CloudProviderTencent,
			wantErr:  false,
		},
		{
			name:     "华为云验证器",
			provider: domain.CloudProviderHuawei,
			wantErr:  false,
		},
		{
			name:     "不支持的云厂商",
			provider: domain.CloudProvider("unsupported"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator, err := factory.CreateValidator(tt.provider)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, validator)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, validator)
			}
		})
	}
}

func TestAliyunValidator(t *testing.T) {
	validator := NewAliyunValidator()
	ctx := context.Background()

	t.Run("有效的阿里云凭证格式", func(t *testing.T) {
		account := &domain.CloudAccount{
			Provider:        domain.CloudProviderAliyun,
			AccessKeyID:     "LTAI5tFakeKeyForTesting",
			AccessKeySecret: "FakeSecretKeyForTestingPurpose",
			Region:          "cn-hangzhou",
		}

		result, err := validator.ValidateCredentials(ctx, account)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.NotEmpty(t, result.Message)
		assert.NotEmpty(t, result.Regions)
		assert.True(t, result.ResponseTime > 0)
	})

	t.Run("无效的阿里云凭证格式", func(t *testing.T) {
		account := &domain.CloudAccount{
			Provider:        domain.CloudProviderAliyun,
			AccessKeyID:     "invalid_key",
			AccessKeySecret: "invalid_secret",
			Region:          "cn-hangzhou",
		}

		result, err := validator.ValidateCredentials(ctx, account)
		require.NoError(t, err)
		assert.False(t, result.Valid)
		assert.Contains(t, result.Message, "长度应为")
	})

	t.Run("获取支持的地域", func(t *testing.T) {
		account := &domain.CloudAccount{
			Provider: domain.CloudProviderAliyun,
			Region:   "cn-hangzhou",
		}

		regions, err := validator.GetSupportedRegions(ctx, account)
		require.NoError(t, err)
		assert.NotEmpty(t, regions)
		assert.Contains(t, regions, "cn-hangzhou")
	})
}

func TestAWSValidator(t *testing.T) {
	validator := NewAWSValidator()
	ctx := context.Background()

	t.Run("有效的AWS凭证格式", func(t *testing.T) {
		account := &domain.CloudAccount{
			Provider:        domain.CloudProviderAWS,
			AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
			AccessKeySecret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			Region:          "us-east-1",
		}

		result, err := validator.ValidateCredentials(ctx, account)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.NotEmpty(t, result.Regions)
	})

	t.Run("无效的AWS凭证格式", func(t *testing.T) {
		account := &domain.CloudAccount{
			Provider:        domain.CloudProviderAWS,
			AccessKeyID:     "invalid",
			AccessKeySecret: "invalid",
			Region:          "us-east-1",
		}

		result, err := validator.ValidateCredentials(ctx, account)
		require.NoError(t, err)
		assert.False(t, result.Valid)
		assert.Contains(t, result.Message, "长度应为")
	})
}

func TestValidationTimeout(t *testing.T) {
	validator := NewAliyunValidator()

	// 创建一个已经取消的 context 来测试超时
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // 确保 context 已经超时

	account := &domain.CloudAccount{
		Provider:        domain.CloudProviderAliyun,
		AccessKeyID:     "LTAI5tFakeKeyForTesting",
		AccessKeySecret: "FakeSecretKeyForTestingPurpose",
		Region:          "cn-hangzhou",
	}

	err := validator.TestConnection(ctx, account)
	assert.Error(t, err)
	assert.Equal(t, ErrConnectionTimeout, err)
}
