package cloudx

import (
	"context"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAliyunValidator_ValidateCredentialFormat_Simple(t *testing.T) {
	validator := NewAliyunValidator().(*AliyunValidator)

	t.Run("有效的阿里云凭证格式", func(t *testing.T) {
		account := &domain.CloudAccount{
			AccessKeyID:     "LTAI5tFakeKeyForTesting1",       // 24位
			AccessKeySecret: "FakeSecretKeyForTestingPurpose", // 30位
		}

		err := validator.validateCredentialFormat(account)
		assert.NoError(t, err)
	})

	t.Run("AccessKeyID 长度不正确", func(t *testing.T) {
		account := &domain.CloudAccount{
			AccessKeyID:     "LTAI5tShort",
			AccessKeySecret: "FakeSecretKeyForTestingPurpose",
		}

		err := validator.validateCredentialFormat(account)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "长度应为 24 位")
	})

	t.Run("AccessKeyID 前缀不正确", func(t *testing.T) {
		account := &domain.CloudAccount{
			AccessKeyID:     "AKID5tFakeKeyForTesting1",       // 24位但前缀错误
			AccessKeySecret: "FakeSecretKeyForTestingPurpose", // 30位
		}

		err := validator.validateCredentialFormat(account)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "应以 LTAI 开头")
	})
}

func TestAliyunValidator_ValidateCredentials_FormatError_Simple(t *testing.T) {
	validator := NewAliyunValidator()
	ctx := context.Background()

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
	assert.True(t, result.ResponseTime > 0)
}

func TestAliyunValidator_MaskAccessKey(t *testing.T) {
	tests := []struct {
		name      string
		accessKey string
		expected  string
	}{
		{
			name:      "正常长度的 AccessKey",
			accessKey: "LTAI5tFakeKeyForTesting1",
			expected:  "LTAI***ing1",
		},
		{
			name:      "短 AccessKey",
			accessKey: "short",
			expected:  "***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskAccessKey(tt.accessKey)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAliyunValidator_TestConnection_Timeout(t *testing.T) {
	validator := NewAliyunValidator()

	// 创建一个已经取消的 context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // 确保 context 已经超时

	account := &domain.CloudAccount{
		Provider:        domain.CloudProviderAliyun,
		AccessKeyID:     "LTAI5tFakeKeyForTesting1",       // 24位
		AccessKeySecret: "FakeSecretKeyForTestingPurpose", // 30位
		Region:          "cn-hangzhou",
	}

	err := validator.TestConnection(ctx, account)
	assert.Equal(t, ErrConnectionTimeout, err)
}
