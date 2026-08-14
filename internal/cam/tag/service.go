package tag

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

// CloudAccountService 云账号服务接口（用于获取账号信息以路由到正确的适配器）
type CloudAccountService interface {
	GetAccountWithCredentials(ctx context.Context, id int64) (*domain.CloudAccount, error)
}

// InstanceCollection 实例集合名称
const InstanceCollection = "ecam_instance"

// TagService 标签管理业务逻辑层接口
type TagService interface {
	// 标签聚合查询（基于本地 MongoDB 资源数据）
	ListTags(ctx context.Context, tenantID int64, filter TagFilter) ([]TagSummary, int64, error)
	GetTagStats(ctx context.Context, tenantID int64) (*TagStats, error)
	ListTagResources(ctx context.Context, tenantID int64, filter TagResourceFilter) ([]TagResource, int64, error)

	// 标签操作（调用云厂商 API）
	BindTags(ctx context.Context, tenantID int64, req BindTagsReq) (*BatchResult, error)
	UnbindTags(ctx context.Context, tenantID int64, req UnbindTagsReq) (*BatchResult, error)

	// 标签策略
	CreatePolicy(ctx context.Context, tenantID int64, req CreatePolicyReq) (TagPolicy, error)
	ListPolicies(ctx context.Context, tenantID int64, filter PolicyFilter) ([]TagPolicy, int64, error)
	UpdatePolicy(ctx context.Context, tenantID int64, id int64, req UpdatePolicyReq) error
	DeletePolicy(ctx context.Context, tenantID int64, id int64) error
	CheckCompliance(ctx context.Context, tenantID int64, filter ComplianceFilter) ([]ComplianceResult, int64, error)

	// 自动打标规则
	CreateRule(ctx context.Context, tenantID int64, req CreateRuleReq) (TagRule, error)
	ListRules(ctx context.Context, tenantID int64, filter RuleFilter) ([]TagRule, int64, error)
	UpdateRule(ctx context.Context, tenantID int64, id int64, req UpdateRuleReq) error
	DeleteRule(ctx context.Context, tenantID int64, id int64) error
	PreviewRules(ctx context.Context, tenantID int64, ruleIDs []int64) ([]RulePreviewResult, error)
	ExecuteRules(ctx context.Context, tenantID int64, ruleIDs []int64) ([]RuleExecuteResult, error)
}

// tagService TagService 实现
type tagService struct {
	dao            TagDAO
	instanceColl   *mongo.Collection
	accountSvc     CloudAccountService
	adapterFactory *cloudx.AdapterFactory
}

// NewTagService 创建 TagService 实例
func NewTagService(
	dao TagDAO,
	instanceColl *mongo.Collection,
	accountSvc CloudAccountService,
	adapterFactory *cloudx.AdapterFactory,
) TagService {
	return &tagService{
		dao:            dao,
		instanceColl:   instanceColl,
		accountSvc:     accountSvc,
		adapterFactory: adapterFactory,
	}
}

// Ensure compile-time interface compliance
var _ TagService = (*tagService)(nil)

