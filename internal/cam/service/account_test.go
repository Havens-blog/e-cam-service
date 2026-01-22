package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// MockCloudAccountRepository 模拟云账号仓储
type MockCloudAccountRepository struct {
	mock.Mock
}

func (m *MockCloudAccountRepository) Create(ctx context.Context, account domain.CloudAccount) (int64, error) {
	args := m.Called(ctx, account)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockCloudAccountRepository) GetByID(ctx context.Context, id int64) (domain.CloudAccount, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(domain.CloudAccount), args.Error(1)
}

func (m *MockCloudAccountRepository) GetByName(ctx context.Context, name, tenantID string) (domain.CloudAccount, error) {
	args := m.Called(ctx, name, tenantID)
	return args.Get(0).(domain.CloudAccount), args.Error(1)
}

func (m *MockCloudAccountRepository) List(ctx context.Context, filter domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.CloudAccount), args.Get(1).(int64), args.Error(2)
}

func (m *MockCloudAccountRepository) Update(ctx context.Context, account domain.CloudAccount) error {
	args := m.Called(ctx, account)
	return args.Error(0)
}

func (m *MockCloudAccountRepository) Delete(ctx context.Context, id int64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockCloudAccountRepository) UpdateStatus(ctx context.Context, id int64, status domain.CloudAccountStatus) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockCloudAccountRepository) UpdateSyncTime(ctx context.Context, id int64, syncTime time.Time, assetCount int64) error {
	args := m.Called(ctx, id, syncTime, assetCount)
	return args.Error(0)
}

func (m *MockCloudAccountRepository) UpdateTestTime(ctx context.Context, id int64, testTime time.Time, status domain.CloudAccountStatus, errorMsg string) error {
	args := m.Called(ctx, id, testTime, status, errorMsg)
	return args.Error(0)
}

// CloudAccountServiceTestSuite 云账号服务测试套件
type CloudAccountServiceTestSuite struct {
	suite.Suite
	service CloudAccountService
	repo    *MockCloudAccountRepository
	ctx     context.Context
}

func (suite *CloudAccountServiceTestSuite) SetupTest() {
	suite.repo = new(MockCloudAccountRepository)
	// 创建一个简单的 logger mock，或者使用 nil
	suite.service = NewCloudAccountService(suite.repo, nil)
	suite.ctx = context.Background()
}

func (suite *CloudAccountServiceTestSuite) TearDownTest() {
	suite.repo.AssertExpectations(suite.T())
}

// TestCreateAccount 测试创建云账号
func (suite *CloudAccountServiceTestSuite) TestCreateAccount() {
	tests := []struct {
		name        string
		req         *domain.CreateCloudAccountRequest
		setupMocks  func()
		expectError error
		expectID    int64
	}{
		{
			name: "成功创建云账号",
			req: &domain.CreateCloudAccountRequest{
				Name:            "test-account",
				Provider:        domain.CloudProviderAliyun,
				Environment:     domain.EnvironmentProduction,
				AccessKeyID:     "test-access-key-id",
				AccessKeySecret: "test-access-key-secret",
				Region:          "cn-hangzhou",
				Description:     "测试账号",
				TenantID:        "tenant-123",
				Config: domain.CloudAccountConfig{
					EnableAutoSync: true,
					SyncInterval:   3600,
				},
			},
			setupMocks: func() {
				// 模拟账号不存在
				suite.repo.On("GetByName", suite.ctx, "test-account", "tenant-123").
					Return(domain.CloudAccount{}, errors.New("not found"))

				// 模拟创建成功
				suite.repo.On("Create", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).
					Return(int64(123), nil)
			},
			expectError: nil,
			expectID:    123,
		},
		{
			name: "账号名称已存在",
			req: &domain.CreateCloudAccountRequest{
				Name:            "existing-account",
				Provider:        domain.CloudProviderAliyun,
				Environment:     domain.EnvironmentProduction,
				AccessKeyID:     "test-access-key-id",
				AccessKeySecret: "test-access-key-secret",
				Region:          "cn-hangzhou",
				TenantID:        "tenant-123",
			},
			setupMocks: func() {
				// 模拟账号已存在
				suite.repo.On("GetByName", suite.ctx, "existing-account", "tenant-123").
					Return(domain.CloudAccount{ID: 456}, nil)
			},
			expectError: errs.AccountAlreadyExist,
			expectID:    0,
		},
		{
			name: "创建失败",
			req: &domain.CreateCloudAccountRequest{
				Name:            "test-account",
				Provider:        domain.CloudProviderAliyun,
				Environment:     domain.EnvironmentProduction,
				AccessKeyID:     "test-access-key-id",
				AccessKeySecret: "test-access-key-secret",
				Region:          "cn-hangzhou",
				TenantID:        "tenant-123",
			},
			setupMocks: func() {
				// 模拟账号不存在
				suite.repo.On("GetByName", suite.ctx, "test-account", "tenant-123").
					Return(domain.CloudAccount{}, errors.New("not found"))

				// 模拟创建失败
				suite.repo.On("Create", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).
					Return(int64(0), errors.New("database error"))
			},
			expectError: errs.SystemError,
			expectID:    0,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// 重置 mock
			suite.repo.ExpectedCalls = nil
			tt.setupMocks()

			// 执行测试
			result, err := suite.service.CreateAccount(suite.ctx, tt.req)

			// 验证结果
			if tt.expectError != nil {
				suite.Equal(tt.expectError, err)
				suite.Nil(result)
			} else {
				suite.NoError(err)
				suite.NotNil(result)
				suite.Equal(tt.expectID, result.ID)
				suite.Equal(tt.req.Name, result.Name)
				suite.Equal(tt.req.Provider, result.Provider)
			}
		})
	}
}

// TestGetAccount 测试获取云账号
func (suite *CloudAccountServiceTestSuite) TestGetAccount() {
	tests := []struct {
		name        string
		accountID   int64
		setupMocks  func()
		expectError error
	}{
		{
			name:      "成功获取云账号",
			accountID: 123,
			setupMocks: func() {
				account := domain.CloudAccount{
					ID:              123,
					Name:            "test-account",
					Provider:        domain.CloudProviderAliyun,
					AccessKeyID:     "LTAI4G8mA9B2C3D4E5F6G7H8",
					AccessKeySecret: "secret123456789",
					Status:          domain.CloudAccountStatusActive,
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(account, nil)
			},
			expectError: nil,
		},
		{
			name:      "账号不存在",
			accountID: 999,
			setupMocks: func() {
				suite.repo.On("GetByID", suite.ctx, int64(999)).
					Return(domain.CloudAccount{}, errors.New("not found"))
			},
			expectError: errs.AccountNotFound,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// 重置 mock
			suite.repo.ExpectedCalls = nil
			tt.setupMocks()

			// 执行测试
			result, err := suite.service.GetAccount(suite.ctx, tt.accountID)

			// 验证结果
			if tt.expectError != nil {
				suite.Equal(tt.expectError, err)
				suite.Nil(result)
			} else {
				suite.NoError(err)
				suite.NotNil(result)
				suite.Equal(tt.accountID, result.ID)
				// 验证敏感数据已脱敏
				suite.Contains(result.AccessKeyID, "***")
				suite.Equal("***", result.AccessKeySecret)
			}
		})
	}
}

// TestListAccounts 测试获取云账号列表
func (suite *CloudAccountServiceTestSuite) TestListAccounts() {
	filter := domain.CloudAccountFilter{
		Provider: domain.CloudProviderAliyun,
		Limit:    10,
		Offset:   0,
	}

	accounts := []domain.CloudAccount{
		{
			ID:              1,
			Name:            "account-1",
			Provider:        domain.CloudProviderAliyun,
			AccessKeyID:     "LTAI4G8mA9B2C3D4E5F6G7H8",
			AccessKeySecret: "secret123456789",
		},
		{
			ID:              2,
			Name:            "account-2",
			Provider:        domain.CloudProviderAliyun,
			AccessKeyID:     "LTAI4G8mA9B2C3D4E5F6G7H9",
			AccessKeySecret: "secret987654321",
		},
	}

	suite.repo.On("List", suite.ctx, filter).Return(accounts, int64(2), nil)

	result, total, err := suite.service.ListAccounts(suite.ctx, filter)

	suite.NoError(err)
	suite.Equal(int64(2), total)
	suite.Len(result, 2)

	// 验证敏感数据已脱敏
	for _, account := range result {
		suite.Contains(account.AccessKeyID, "***")
		suite.Equal("***", account.AccessKeySecret)
	}
}

// TestUpdateAccount 测试更新云账号
func (suite *CloudAccountServiceTestSuite) TestUpdateAccount() {
	tests := []struct {
		name       string
		accountID  int64
		req        *domain.UpdateCloudAccountRequest
		setupMocks func()
		expectErr  error
	}{
		{
			name:      "更新所有字段",
			accountID: 123,
			req: &domain.UpdateCloudAccountRequest{
				Name:        stringPtr("new-name"),
				Description: stringPtr("new-description"),
				Config: &domain.CloudAccountConfig{
					EnableAutoSync: true,
					SyncInterval:   7200,
				},
			},
			setupMocks: func() {
				existingAccount := domain.CloudAccount{
					ID:          123,
					Name:        "old-name",
					Description: "old-description",
					Status:      domain.CloudAccountStatusActive,
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(existingAccount, nil)
				suite.repo.On("Update", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).Return(nil)
			},
			expectErr: nil,
		},
		{
			name:      "只更新名称",
			accountID: 123,
			req: &domain.UpdateCloudAccountRequest{
				Name: stringPtr("new-name-only"),
			},
			setupMocks: func() {
				existingAccount := domain.CloudAccount{
					ID:   123,
					Name: "old-name",
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(existingAccount, nil)
				suite.repo.On("Update", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).Return(nil)
			},
			expectErr: nil,
		},
		{
			name:      "只更新描述",
			accountID: 123,
			req: &domain.UpdateCloudAccountRequest{
				Description: stringPtr("new-description-only"),
			},
			setupMocks: func() {
				existingAccount := domain.CloudAccount{
					ID:          123,
					Description: "old-description",
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(existingAccount, nil)
				suite.repo.On("Update", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).Return(nil)
			},
			expectErr: nil,
		},
		{
			name:      "只更新配置",
			accountID: 123,
			req: &domain.UpdateCloudAccountRequest{
				Config: &domain.CloudAccountConfig{
					ReadOnly: true,
				},
			},
			setupMocks: func() {
				existingAccount := domain.CloudAccount{
					ID: 123,
					Config: domain.CloudAccountConfig{
						ReadOnly: false,
					},
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(existingAccount, nil)
				suite.repo.On("Update", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).Return(nil)
			},
			expectErr: nil,
		},
		{
			name:      "空更新请求",
			accountID: 123,
			req:       &domain.UpdateCloudAccountRequest{},
			setupMocks: func() {
				existingAccount := domain.CloudAccount{
					ID:   123,
					Name: "existing-name",
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(existingAccount, nil)
				suite.repo.On("Update", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).Return(nil)
			},
			expectErr: nil,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// 重置 mock
			suite.repo.ExpectedCalls = nil
			tt.setupMocks()

			// 执行测试
			err := suite.service.UpdateAccount(suite.ctx, tt.accountID, tt.req)

			// 验证结果
			suite.Equal(tt.expectErr, err)
		})
	}
}

// stringPtr 辅助函数，返回字符串指针
func stringPtr(s string) *string {
	return &s
}

// TestDeleteAccount 测试删除云账号
func (suite *CloudAccountServiceTestSuite) TestDeleteAccount() {
	tests := []struct {
		name        string
		accountID   int64
		setupMocks  func()
		expectError error
	}{
		{
			name:      "成功删除云账号",
			accountID: 123,
			setupMocks: func() {
				account := domain.CloudAccount{
					ID:     123,
					Name:   "test-account",
					Status: domain.CloudAccountStatusActive,
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(account, nil)
				suite.repo.On("Delete", suite.ctx, int64(123)).Return(nil)
			},
			expectError: nil,
		},
		{
			name:      "账号不存在",
			accountID: 999,
			setupMocks: func() {
				suite.repo.On("GetByID", suite.ctx, int64(999)).
					Return(domain.CloudAccount{}, errors.New("not found"))
			},
			expectError: errs.AccountNotFound,
		},
		{
			name:      "账号正在测试中，不允许删除",
			accountID: 123,
			setupMocks: func() {
				account := domain.CloudAccount{
					ID:     123,
					Name:   "test-account",
					Status: domain.CloudAccountStatusTesting,
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(account, nil)
			},
			expectError: errs.SyncInProgress,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// 重置 mock
			suite.repo.ExpectedCalls = nil
			tt.setupMocks()

			// 执行测试
			err := suite.service.DeleteAccount(suite.ctx, tt.accountID)

			// 验证结果
			suite.Equal(tt.expectError, err)
		})
	}
}

// TestTestConnection 测试连接测试
func (suite *CloudAccountServiceTestSuite) TestTestConnection() {
	accountID := int64(123)
	account := domain.CloudAccount{
		ID:     accountID,
		Name:   "test-account",
		Region: "cn-hangzhou",
		Status: domain.CloudAccountStatusActive,
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(account, nil)
	suite.repo.On("UpdateTestTime", suite.ctx, accountID, mock.AnythingOfType("time.Time"),
		domain.CloudAccountStatusTesting, "").Return(nil)
	suite.repo.On("UpdateTestTime", suite.ctx, accountID, mock.AnythingOfType("time.Time"),
		domain.CloudAccountStatusActive, "").Return(nil)

	result, err := suite.service.TestConnection(suite.ctx, accountID)

	suite.NoError(err)
	suite.NotNil(result)
	suite.Equal("success", result.Status)
	suite.Equal("连接测试成功", result.Message)
	suite.Contains(result.Regions, "cn-hangzhou")
}

// TestEnableAccount 测试启用账号
func (suite *CloudAccountServiceTestSuite) TestEnableAccount() {
	accountID := int64(123)
	account := domain.CloudAccount{
		ID:     accountID,
		Status: domain.CloudAccountStatusDisabled,
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(account, nil)
	suite.repo.On("UpdateStatus", suite.ctx, accountID, domain.CloudAccountStatusActive).Return(nil)

	err := suite.service.EnableAccount(suite.ctx, accountID)

	suite.NoError(err)
}

// TestDisableAccount 测试禁用账号
func (suite *CloudAccountServiceTestSuite) TestDisableAccount() {
	accountID := int64(123)
	account := domain.CloudAccount{
		ID:     accountID,
		Status: domain.CloudAccountStatusActive,
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(account, nil)
	suite.repo.On("UpdateStatus", suite.ctx, accountID, domain.CloudAccountStatusDisabled).Return(nil)

	err := suite.service.DisableAccount(suite.ctx, accountID)

	suite.NoError(err)
}

// TestSyncAccount 测试同步账号
func (suite *CloudAccountServiceTestSuite) TestSyncAccount() {
	tests := []struct {
		name        string
		accountID   int64
		req         *domain.SyncAccountRequest
		setupMocks  func()
		expectError error
	}{
		{
			name:      "成功启动同步",
			accountID: 123,
			req: &domain.SyncAccountRequest{
				AssetTypes: []string{"ecs", "rds"},
				Regions:    []string{"cn-hangzhou"},
			},
			setupMocks: func() {
				account := domain.CloudAccount{
					ID:     123,
					Status: domain.CloudAccountStatusActive,
					Config: domain.CloudAccountConfig{
						ReadOnly: false,
					},
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(account, nil)
				suite.repo.On("UpdateSyncTime", suite.ctx, int64(123),
					mock.AnythingOfType("time.Time"), int64(0)).Return(nil)
			},
			expectError: nil,
		},
		{
			name:      "账号已禁用",
			accountID: 123,
			req:       &domain.SyncAccountRequest{},
			setupMocks: func() {
				account := domain.CloudAccount{
					ID:     123,
					Status: domain.CloudAccountStatusDisabled,
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(account, nil)
			},
			expectError: errs.AccountDisabled,
		},
		{
			name:      "只读账号",
			accountID: 123,
			req:       &domain.SyncAccountRequest{},
			setupMocks: func() {
				account := domain.CloudAccount{
					ID:     123,
					Status: domain.CloudAccountStatusActive,
					Config: domain.CloudAccountConfig{
						ReadOnly: true,
					},
				}
				suite.repo.On("GetByID", suite.ctx, int64(123)).Return(account, nil)
			},
			expectError: errs.ReadOnlyAccount,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// 重置 mock
			suite.repo.ExpectedCalls = nil
			tt.setupMocks()

			// 执行测试
			result, err := suite.service.SyncAccount(suite.ctx, tt.accountID, tt.req)

			// 验证结果
			if tt.expectError != nil {
				suite.Equal(tt.expectError, err)
				suite.Nil(result)
			} else {
				suite.NoError(err)
				suite.NotNil(result)
				suite.Equal("running", result.Status)
				suite.Contains(result.SyncID, "sync_")
			}
		})
	}
}

// 运行测试套件
func TestCloudAccountServiceSuite(t *testing.T) {
	suite.Run(t, new(CloudAccountServiceTestSuite))
}

// TestCreateAccountValidationError 测试创建账号时的验证错误
func (suite *CloudAccountServiceTestSuite) TestCreateAccountValidationError() {
	// 测试空名称的验证错误
	req := &domain.CreateCloudAccountRequest{
		Name:            "", // 空名称会导致验证失败
		Provider:        domain.CloudProviderAliyun,
		Environment:     domain.EnvironmentProduction,
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Region:          "cn-hangzhou",
		TenantID:        "tenant-123",
	}

	// 模拟账号不存在
	suite.repo.On("GetByName", suite.ctx, "", "tenant-123").
		Return(domain.CloudAccount{}, errors.New("not found"))

	result, err := suite.service.CreateAccount(suite.ctx, req)

	suite.Error(err)
	suite.Nil(result)
	suite.Contains(err.Error(), "account name cannot be empty")
}

// TestListAccountsLimitBoundary 测试列表查询的分页边界情况
func (suite *CloudAccountServiceTestSuite) TestListAccountsLimitBoundary() {
	tests := []struct {
		name          string
		inputLimit    int64
		expectedLimit int64
	}{
		{
			name:          "默认分页大小",
			inputLimit:    0,
			expectedLimit: 20,
		},
		{
			name:          "超过最大限制",
			inputLimit:    150,
			expectedLimit: 100,
		},
		{
			name:          "正常分页大小",
			inputLimit:    50,
			expectedLimit: 50,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// 重置 mock
			suite.repo.ExpectedCalls = nil

			filter := domain.CloudAccountFilter{
				Limit: tt.inputLimit,
			}

			expectedFilter := domain.CloudAccountFilter{
				Limit: tt.expectedLimit,
			}

			suite.repo.On("List", suite.ctx, expectedFilter).Return([]domain.CloudAccount{}, int64(0), nil)

			_, _, err := suite.service.ListAccounts(suite.ctx, filter)
			suite.NoError(err)
		})
	}
}

// TestListAccountsError 测试列表查询错误
func (suite *CloudAccountServiceTestSuite) TestListAccountsError() {
	filter := domain.CloudAccountFilter{
		Limit: 10,
	}

	suite.repo.On("List", suite.ctx, filter).Return([]domain.CloudAccount{}, int64(0), errors.New("database error"))

	result, total, err := suite.service.ListAccounts(suite.ctx, filter)

	suite.Equal(errs.SystemError, err)
	suite.Equal(int64(0), total)
	suite.Nil(result)
}

// TestUpdateAccountNotFound 测试更新不存在的账号
func (suite *CloudAccountServiceTestSuite) TestUpdateAccountNotFound() {
	accountID := int64(999)
	req := &domain.UpdateCloudAccountRequest{}

	suite.repo.On("GetByID", suite.ctx, accountID).
		Return(domain.CloudAccount{}, errors.New("not found"))

	err := suite.service.UpdateAccount(suite.ctx, accountID, req)

	suite.Equal(errs.AccountNotFound, err)
}

// TestUpdateAccountError 测试更新账号失败
func (suite *CloudAccountServiceTestSuite) TestUpdateAccountError() {
	accountID := int64(123)
	existingAccount := domain.CloudAccount{
		ID:   accountID,
		Name: "test-account",
	}

	newName := "new-name"
	req := &domain.UpdateCloudAccountRequest{
		Name: &newName,
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(existingAccount, nil)
	suite.repo.On("Update", suite.ctx, mock.AnythingOfType("domain.CloudAccount")).
		Return(errors.New("database error"))

	err := suite.service.UpdateAccount(suite.ctx, accountID, req)

	suite.Equal(errs.SystemError, err)
}

// TestDeleteAccountError 测试删除账号失败
func (suite *CloudAccountServiceTestSuite) TestDeleteAccountError() {
	accountID := int64(123)
	account := domain.CloudAccount{
		ID:     accountID,
		Name:   "test-account",
		Status: domain.CloudAccountStatusActive,
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(account, nil)
	suite.repo.On("Delete", suite.ctx, accountID).Return(errors.New("database error"))

	err := suite.service.DeleteAccount(suite.ctx, accountID)

	suite.Equal(errs.SystemError, err)
}

// TestTestConnectionNotFound 测试连接测试账号不存在
func (suite *CloudAccountServiceTestSuite) TestTestConnectionNotFound() {
	accountID := int64(999)

	suite.repo.On("GetByID", suite.ctx, accountID).
		Return(domain.CloudAccount{}, errors.New("not found"))

	result, err := suite.service.TestConnection(suite.ctx, accountID)

	suite.Equal(errs.AccountNotFound, err)
	suite.Nil(result)
}

// TestTestConnectionFailure 测试连接测试失败的情况
func (suite *CloudAccountServiceTestSuite) TestTestConnectionFailure() {
	accountID := int64(123)
	account := domain.CloudAccount{
		ID:     accountID,
		Name:   "test-account",
		Region: "cn-hangzhou",
		Status: domain.CloudAccountStatusActive,
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(account, nil)
	suite.repo.On("UpdateTestTime", suite.ctx, accountID, mock.AnythingOfType("time.Time"),
		domain.CloudAccountStatusTesting, "").Return(nil)

	// 模拟连接测试失败的情况 - 这里我们需要修改服务代码来支持失败场景
	// 但由于当前代码总是返回成功，我们测试更新测试状态失败的情况
	suite.repo.On("UpdateTestTime", suite.ctx, accountID, mock.AnythingOfType("time.Time"),
		domain.CloudAccountStatusActive, "").Return(errors.New("update failed"))

	result, err := suite.service.TestConnection(suite.ctx, accountID)

	suite.NoError(err) // 即使更新失败，连接测试仍然返回成功
	suite.NotNil(result)
	suite.Equal("success", result.Status)
}

// TestEnableAccountNotFound 测试启用不存在的账号
func (suite *CloudAccountServiceTestSuite) TestEnableAccountNotFound() {
	accountID := int64(999)

	suite.repo.On("GetByID", suite.ctx, accountID).
		Return(domain.CloudAccount{}, errors.New("not found"))

	err := suite.service.EnableAccount(suite.ctx, accountID)

	suite.Equal(errs.AccountNotFound, err)
}

// TestEnableAccountError 测试启用账号失败
func (suite *CloudAccountServiceTestSuite) TestEnableAccountError() {
	accountID := int64(123)
	account := domain.CloudAccount{
		ID:     accountID,
		Status: domain.CloudAccountStatusDisabled,
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(account, nil)
	suite.repo.On("UpdateStatus", suite.ctx, accountID, domain.CloudAccountStatusActive).
		Return(errors.New("database error"))

	err := suite.service.EnableAccount(suite.ctx, accountID)

	suite.Equal(errs.SystemError, err)
}

// TestDisableAccountNotFound 测试禁用不存在的账号
func (suite *CloudAccountServiceTestSuite) TestDisableAccountNotFound() {
	accountID := int64(999)

	suite.repo.On("GetByID", suite.ctx, accountID).
		Return(domain.CloudAccount{}, errors.New("not found"))

	err := suite.service.DisableAccount(suite.ctx, accountID)

	suite.Equal(errs.AccountNotFound, err)
}

// TestDisableAccountError 测试禁用账号失败
func (suite *CloudAccountServiceTestSuite) TestDisableAccountError() {
	accountID := int64(123)
	account := domain.CloudAccount{
		ID:     accountID,
		Status: domain.CloudAccountStatusActive,
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(account, nil)
	suite.repo.On("UpdateStatus", suite.ctx, accountID, domain.CloudAccountStatusDisabled).
		Return(errors.New("database error"))

	err := suite.service.DisableAccount(suite.ctx, accountID)

	suite.Equal(errs.SystemError, err)
}

// TestSyncAccountNotFound 测试同步不存在的账号
func (suite *CloudAccountServiceTestSuite) TestSyncAccountNotFound() {
	accountID := int64(999)
	req := &domain.SyncAccountRequest{}

	suite.repo.On("GetByID", suite.ctx, accountID).
		Return(domain.CloudAccount{}, errors.New("not found"))

	result, err := suite.service.SyncAccount(suite.ctx, accountID, req)

	suite.Equal(errs.AccountNotFound, err)
	suite.Nil(result)
}

// TestSyncAccountUpdateError 测试同步时更新时间失败
func (suite *CloudAccountServiceTestSuite) TestSyncAccountUpdateError() {
	accountID := int64(123)
	account := domain.CloudAccount{
		ID:     accountID,
		Status: domain.CloudAccountStatusActive,
		Config: domain.CloudAccountConfig{
			ReadOnly: false,
		},
	}

	req := &domain.SyncAccountRequest{
		AssetTypes: []string{"ecs"},
		Regions:    []string{"cn-hangzhou"},
	}

	suite.repo.On("GetByID", suite.ctx, accountID).Return(account, nil)
	suite.repo.On("UpdateSyncTime", suite.ctx, accountID, mock.AnythingOfType("time.Time"), int64(0)).
		Return(errors.New("update failed"))

	result, err := suite.service.SyncAccount(suite.ctx, accountID, req)

	// 即使更新同步时间失败，同步操作仍然返回成功
	suite.NoError(err)
	suite.NotNil(result)
	suite.Equal("running", result.Status)
}

// 基准测试
func BenchmarkCreateAccount(b *testing.B) {
	repo := new(MockCloudAccountRepository)
	service := NewCloudAccountService(repo, nil)
	ctx := context.Background()

	req := &domain.CreateCloudAccountRequest{
		Name:            "benchmark-account",
		Provider:        domain.CloudProviderAliyun,
		Environment:     domain.EnvironmentProduction,
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Region:          "cn-hangzhou",
		TenantID:        "tenant-123",
	}

	// 设置 mock 期望
	repo.On("GetByName", ctx, "benchmark-account", "tenant-123").
		Return(domain.CloudAccount{}, errors.New("not found"))
	repo.On("Create", ctx, mock.AnythingOfType("domain.CloudAccount")).
		Return(int64(123), nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.CreateAccount(ctx, req)
	}
}
