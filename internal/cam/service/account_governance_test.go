package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/governance"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// CloudAccountGovernanceTestSuite 创建云账号租户治理校验（R7 白名单 / R2.4 配额）
// 专项测试：mock 治理端口（跨服务不连真实 eiam），全部为 mock 验证。
type CloudAccountGovernanceTestSuite struct {
	suite.Suite
	repo *MockCloudAccountRepository
	gov  *governance.MockTenantGovernancePort
	ctx  context.Context
}

func (suite *CloudAccountGovernanceTestSuite) SetupTest() {
	suite.repo = new(MockCloudAccountRepository)
	suite.gov = new(governance.MockTenantGovernancePort)
	suite.ctx = context.Background()
}

// newGovernanceService 构造注入了 mock 治理端口的服务
// （adapterFactory/taskQueue/logger 同既有套件的取值口径）
func (suite *CloudAccountGovernanceTestSuite) newGovernanceService() CloudAccountService {
	return NewCloudAccountService(suite.repo, nil, nil, nil, elog.DefaultLogger, suite.gov)
}

func governanceTestReq() *domain.CreateCloudAccountRequest {
	return &domain.CreateCloudAccountRequest{
		Name:            "governed-account",
		Provider:        domain.CloudProviderAliyun,
		Environment:     domain.EnvironmentProduction,
		AccessKeyID:     "test-access-key-id",
		AccessKeySecret: "test-access-key-secret",
		Regions:         []string{"cn-hangzhou"},
		TenantID:        8,
	}
}

// TestCheckTenantGovernance 表驱动覆盖白名单/配额/fail-closed 三类边界
func (suite *CloudAccountGovernanceTestSuite) TestCheckTenantGovernance() {
	tests := []struct {
		name string
		// 治理端口预置行为；nil 时按零值配置（无约束）放行
		governanceFunc func(ctx context.Context, tenantID int64) (int64, []string, error)
		// 配额计数查询预置（仅 MaxAccounts>0 时会被触发）；nil 表示不设 List 期望
		setupList func()
		req       func() *domain.CreateCloudAccountRequest
		// expectErr 用 errors.Is 断言（拒绝错误带 %w 上下文包装）
		expectErr         error
		expectGoverning   bool // 是否应发生治理配置查询
		expectCountCalled bool // 是否应发生配额计数查询
		expectDupCheck    bool // 是否应进入原有查重流程（放行证明）
	}{
		{
			name:            "缺少租户上下文 tid=0 直接拒绝且不触发任何查询",
			governanceFunc:  nil,
			req:             func() *domain.CreateCloudAccountRequest { r := governanceTestReq(); r.TenantID = 0; return r },
			expectErr:       errs.ErrInvalidTenantID,
			expectGoverning: false,
		},
		{
			name: "治理配置查询失败 fail-closed 且不进入查重",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) {
				return 0, nil, errors.New("eiam unreachable")
			},
			req:             governanceTestReq,
			expectErr:       errs.ErrTenantGovernanceUnavailable,
			expectGoverning: true,
		},
		{
			name: "租户不存在按查询失败 fail-closed",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) {
				return 0, nil, errors.New("tenant not found")
			},
			req:             governanceTestReq,
			expectErr:       errs.ErrTenantGovernanceUnavailable,
			expectGoverning: true,
		},
		{
			name: "白名单未命中拒绝且先于查重（重名也不改判）",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) {
				return 0, []string{"aws", "huawei"}, nil
			},
			setupList: func() {
				// 故意让重名命中：若治理校验未先行，这里会返回 AccountAlreadyExist
				suite.repo.On("GetByName", suite.ctx, "governed-account", int64(8)).
					Return(domain.CloudAccount{ID: 456}, nil)
			},
			req:             governanceTestReq,
			expectErr:       errs.ErrVendorNotAllowed,
			expectGoverning: true,
		},
		{
			name: "白名单命中放行进入原有查重流程",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) {
				return 0, []string{"aliyun", "aws"}, nil
			},
			setupList: func() {
				suite.repo.On("GetByName", suite.ctx, "governed-account", int64(8)).
					Return(domain.CloudAccount{ID: 456}, nil)
			},
			req:             governanceTestReq,
			expectErr:       errs.AccountAlreadyExist,
			expectGoverning: true,
			expectDupCheck:  true,
		},
		{
			name:           "白名单为空全部允许（R7.4 零值语义）",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) { return 0, nil, nil },
			setupList: func() {
				suite.repo.On("GetByName", suite.ctx, "governed-account", int64(8)).
					Return(domain.CloudAccount{ID: 456}, nil)
			},
			req:             governanceTestReq,
			expectErr:       errs.AccountAlreadyExist,
			expectGoverning: true,
			expectDupCheck:  true,
		},
		{
			name:           "配额未设(0)不产生计数查询（R2.7 零值语义）",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) { return 0, nil, nil },
			setupList: func() {
				suite.repo.On("GetByName", suite.ctx, "governed-account", int64(8)).
					Return(domain.CloudAccount{ID: 456}, nil)
			},
			req:             governanceTestReq,
			expectErr:       errs.AccountAlreadyExist,
			expectGoverning: true,
			expectDupCheck:  true,
		},
		{
			name: "已达配额上限拒绝创建",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) {
				return 3, nil, nil
			},
			setupList: func() {
				suite.repo.On("List", suite.ctx, domain.CloudAccountFilter{TenantID: 8, Limit: 1}).
					Return([]domain.CloudAccount{}, int64(3), nil)
			},
			req:               governanceTestReq,
			expectErr:         errs.ErrTenantQuotaExceeded,
			expectGoverning:   true,
			expectCountCalled: true,
		},
		{
			name: "未达配额放行进入原有查重流程",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) {
				return 5, nil, nil
			},
			setupList: func() {
				suite.repo.On("List", suite.ctx, domain.CloudAccountFilter{TenantID: 8, Limit: 1}).
					Return([]domain.CloudAccount{}, int64(4), nil)
				suite.repo.On("GetByName", suite.ctx, "governed-account", int64(8)).
					Return(domain.CloudAccount{ID: 456}, nil)
			},
			req:               governanceTestReq,
			expectErr:         errs.AccountAlreadyExist,
			expectGoverning:   true,
			expectCountCalled: true,
			expectDupCheck:    true,
		},
		{
			name: "配额计数查询失败拒绝创建（fail-closed）",
			governanceFunc: func(ctx context.Context, tenantID int64) (int64, []string, error) {
				return 3, nil, nil
			},
			setupList: func() {
				suite.repo.On("List", suite.ctx, domain.CloudAccountFilter{TenantID: 8, Limit: 1}).
					Return([]domain.CloudAccount{}, int64(0), errors.New("database error"))
			},
			req:               governanceTestReq,
			expectErr:         errs.SystemError,
			expectGoverning:   true,
			expectCountCalled: true,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// 每个子用例全新 mock：testify 会跨用例累积调用记录与期望，
			// 复用同一 mock 会令 AssertNotCalled 读到前序用例的调用痕迹
			suite.repo = new(MockCloudAccountRepository)
			suite.gov = new(governance.MockTenantGovernancePort)
			svc := suite.newGovernanceService()
			suite.gov.GetGovernanceFunc = tt.governanceFunc
			if tt.setupList != nil {
				tt.setupList()
			}

			result, err := svc.CreateAccount(suite.ctx, tt.req())

			if tt.expectErr != nil {
				suite.True(errors.Is(err, tt.expectErr), "期望 %v，实际 %v", tt.expectErr, err)
				suite.Nil(result)
			}
			if tt.expectGoverning {
				suite.Equal([]int64{8}, suite.gov.Calls, "治理端口应被查询一次（租户 8）")
			} else {
				suite.Empty(suite.gov.Calls, "治理端口不应被查询")
			}
			if !tt.expectCountCalled {
				suite.repo.AssertNotCalled(suite.T(), "List", mock.Anything, mock.Anything)
			}
			if !tt.expectDupCheck {
				suite.repo.AssertNotCalled(suite.T(), "GetByName", mock.Anything, mock.Anything, mock.Anything)
			}
		})
	}
}

// TestGovernancePortNotAssembled 治理端口未装配时 fail-closed（构造缺省防线）
func (suite *CloudAccountGovernanceTestSuite) TestGovernancePortNotAssembled() {
	svc := NewCloudAccountService(suite.repo, nil, nil, nil, elog.DefaultLogger, nil)

	result, err := svc.CreateAccount(suite.ctx, governanceTestReq())

	suite.True(errors.Is(err, errs.ErrTenantGovernanceUnavailable), "期望治理配置不可用，实际 %v", err)
	suite.Nil(result)
	suite.Empty(suite.gov.Calls)
	suite.repo.AssertNotCalled(suite.T(), "GetByName", mock.Anything, mock.Anything, mock.Anything)
}

// TestGovernanceQueryTenantIDPassthrough 治理查询使用的租户 ID 与请求一致
func (suite *CloudAccountGovernanceTestSuite) TestGovernanceQueryTenantIDPassthrough() {
	svc := suite.newGovernanceService()
	suite.gov.GetGovernanceFunc = func(ctx context.Context, tenantID int64) (int64, []string, error) {
		return 0, nil, errors.New("stop")
	}
	req := governanceTestReq()
	req.TenantID = 42

	_, err := svc.CreateAccount(suite.ctx, req)

	suite.True(errors.Is(err, errs.ErrTenantGovernanceUnavailable))
	suite.Equal([]int64{42}, suite.gov.Calls)
}

// 运行测试套件
func TestCloudAccountGovernanceSuite(t *testing.T) {
	suite.Run(t, new(CloudAccountGovernanceTestSuite))
}
