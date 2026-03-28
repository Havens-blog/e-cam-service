package cam

import (
	"context"

	// 使用新的独立 IAM 模块
	"github.com/Havens-blog/e-cam-service/internal/alert"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/allocation"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/analysis"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/anomaly"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/budget"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/collector"
	costhandler "github.com/Havens-blog/e-cam-service/internal/cam/cost/handler"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/normalizer"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/optimizer"
	costdao "github.com/Havens-blog/e-cam-service/internal/cam/cost/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/dictionary"
	"github.com/Havens-blog/e-cam-service/internal/cam/iam"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/cam/servicetree"
	"github.com/Havens-blog/e-cam-service/internal/cam/template"
	cmdbrepository "github.com/Havens-blog/e-cam-service/internal/cmdb/repository"
	cmdbdao "github.com/Havens-blog/e-cam-service/internal/cmdb/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/mongox"
	"github.com/gotomicro/ego/core/elog"
	"github.com/redis/go-redis/v9"

	// 注册各云厂商 billing adapter（触发 init() 注册到全局注册表）
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/aliyun"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/aws"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/huawei"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/tencent"
	_ "github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing/volcano"
)

// InitModuleWithIAM 初始化CAM模块（包含IAM和成本管理）
func InitModuleWithIAM(db *mongox.Mongo, redisClient redis.Cmdable, alertModule *alert.Module) (*Module, error) {
	logger := elog.DefaultLogger
	logger.Info("开始初始化CAM模块（包含IAM）")

	// 先初始化基础模块
	module, err := InitModule(db)
	if err != nil {
		logger.Error("初始化CAM基础模块失败", elog.FieldErr(err))
		return nil, err
	}
	logger.Info("CAM基础模块初始化成功")

	// 初始化IAM模块（使用新的独立模块）
	logger.Info("开始初始化IAM模块")
	iamModule, err := iam.InitModule(db)
	if err != nil {
		logger.Error("初始化IAM模块失败", elog.FieldErr(err))
		return nil, err
	}
	logger.Info("IAM模块初始化成功")

	module.IAMModule = iamModule

	// 创建 InstanceRepository 用于服务树模块
	instanceDAO := dao.NewInstanceDAO(db)
	instanceRepo := repository.NewInstanceRepository(instanceDAO)

	// 创建 CMDB InstanceRepository 用于节点资产查询
	cmdbInstanceDAO := cmdbdao.NewInstanceDAO(db)
	cmdbInstanceRepo := cmdbrepository.NewInstanceRepository(cmdbInstanceDAO)

	// 初始化服务树模块
	logger.Info("开始初始化服务树模块")
	stModule, err := servicetree.InitModule(db, instanceRepo, cmdbInstanceRepo, logger)
	if err != nil {
		logger.Error("初始化服务树模块失败", elog.FieldErr(err))
		return nil, err
	}
	// 初始化服务树索引
	if err := servicetree.InitIndexes(db); err != nil {
		logger.Warn("初始化服务树索引失败", elog.FieldErr(err))
	}
	logger.Info("服务树模块初始化成功")

	module.ServiceTreeModule = stModule

	// 初始化数据字典模块
	logger.Info("开始初始化数据字典模块")
	if err := dictionary.InitIndexes(db); err != nil {
		logger.Warn("初始化数据字典索引失败", elog.FieldErr(err))
	}
	dictDAO := dictionary.NewDictDAO(db)
	dictSvc := dictionary.NewDictService(dictDAO)
	module.DictHdl = dictionary.NewDictHandler(dictSvc)
	logger.Info("数据字典模块初始化成功")

	// 初始化字典种子数据（为所有已有租户）
	seedCreated, seedSkipped, seedErr := dictionary.SeedDictDataForAllTenants(context.Background(), dictSvc, db)
	if seedErr != nil {
		logger.Warn("字典种子数据初始化出现错误", elog.FieldErr(seedErr))
	}
	logger.Info("字典种子数据初始化完成",
		elog.Int("created", seedCreated),
		elog.Int("skipped", seedSkipped))

	// 初始化主机模板模块
	logger.Info("开始初始化主机模板模块")
	if err := initTemplateModule(module, db, logger); err != nil {
		logger.Warn("初始化主机模板模块失败", elog.FieldErr(err))
	} else {
		logger.Info("主机模板模块初始化成功")
	}

	// 初始化成本管理模块
	logger.Info("开始初始化成本管理模块")
	if err := initCostModule(module, db, redisClient, alertModule, logger); err != nil {
		logger.Error("初始化成本管理模块失败", elog.FieldErr(err))
		// 成本模块初始化失败不阻塞启动，仅记录警告
		logger.Warn("成本管理模块未启用，相关功能不可用")
	} else {
		logger.Info("成本管理模块初始化成功")
	}

	// 启动自动同步调度器
	if module.AutoScheduler != nil {
		logger.Info("启动自动同步调度器")
		module.AutoScheduler.Start()
	}

	logger.Info("CAM模块（包含IAM）初始化完成")
	return module, nil
}

// initCostModule 初始化成本管理子模块
func initCostModule(module *Module, db *mongox.Mongo, redisClient redis.Cmdable, alertModule *alert.Module, logger *elog.Component) error {
	// 初始化成本模块 MongoDB 索引
	if err := costdao.InitCostIndexes(db); err != nil {
		logger.Warn("初始化成本模块索引失败", elog.FieldErr(err))
	}

	// 初始化 DAO 层
	billDAO := costdao.NewBillDAO(db)
	collectLogDAO := costdao.NewCollectLogDAO(db)
	budgetDAO := costdao.NewBudgetDAO(db)
	allocationDAO := costdao.NewAllocationDAO(db)
	anomalyDAO := costdao.NewAnomalyDAO(db)
	optimizerDAO := costdao.NewOptimizerDAO(db)

	// 初始化标准化服务
	normalizerSvc := normalizer.NewNormalizerService(billDAO, logger)

	// 初始化采集服务
	// 使用 cam 模块的 AccountSvc，它满足 accountservice.CloudAccountService 接口
	collectorSvc := collector.NewCollectorService(normalizerSvc, billDAO, collectLogDAO, module.AccountSvc, redisClient, logger)

	// 初始化成本分析服务
	costSvc := analysis.NewCostService(billDAO, redisClient, logger)

	// 初始化预算管理服务
	var alertSvc = alertModule.AlertService
	budgetSvc := budget.NewBudgetService(budgetDAO, billDAO, alertSvc, logger)

	// 初始化成本分摊服务
	allocationSvc := allocation.NewAllocationService(allocationDAO, billDAO, logger)

	// 初始化异常检测服务
	anomalySvc := anomaly.NewAnomalyService(anomalyDAO, billDAO, alertSvc, logger)

	// 初始化优化建议服务
	optimizerSvc := optimizer.NewOptimizerService(optimizerDAO, billDAO, logger)

	// 初始化 HTTP 处理器
	module.CostHdl = costhandler.NewCostHandler(costSvc, anomalySvc, optimizerSvc)
	module.BudgetHdl = costhandler.NewBudgetHandler(budgetSvc)
	module.AllocationHdl = costhandler.NewAllocationHandler(allocationSvc)
	module.CollectorHdl = costhandler.NewCollectorHandler(collectorSvc, module.TaskSvc)

	// 注册账单采集执行器到任务队列
	module.TaskModule.RegisterBillingExecutor(normalizerSvc, billDAO, collectLogDAO, module.AccountSvc, redisClient, logger)

	// 设置服务引用（供定时任务使用）
	module.CostCollectorSvc = collectorSvc
	module.CostBudgetSvc = budgetSvc
	module.CostAnomalySvc = anomalySvc
	module.CostOptimizerSvc = optimizerSvc

	return nil
}

// initTemplateModule 初始化主机模板子模块
func initTemplateModule(module *Module, db *mongox.Mongo, logger *elog.Component) error {
	// 初始化索引
	if err := template.InitIndexes(db); err != nil {
		logger.Warn("初始化主机模板索引失败", elog.FieldErr(err))
	}

	// 初始化 DAO
	tmplDAO := template.NewTemplateDAO(db)
	taskDAO := template.NewProvisionTaskDAO(db)

	// 创建账号提供者适配器（复用现有 AccountSvc）
	accountProvider := &accountProviderAdapter{accountSvc: module.AccountSvc}

	// 创建适配器工厂（复用现有 cloudx 注册机制）
	adapterFactory := &cloudxAdapterFactory{}

	// 初始化校验器
	validator := template.NewTemplateValidator(accountProvider, adapterFactory)

	// 初始化服务
	svc := template.NewTemplateService(tmplDAO, taskDAO, nil)

	// 初始化 HTTP 处理器
	module.TemplateHdl = template.NewTemplateHandler(svc, accountProvider, adapterFactory)

	// 初始化执行器（注册到任务队列）
	_ = template.NewCreateECSExecutor(tmplDAO, taskDAO, validator, accountProvider, adapterFactory, nil, logger)

	logger.Info("主机模板模块初始化完成")
	return nil
}

// accountProviderAdapter 将 CloudAccountService 适配为 template.AccountProvider
type accountProviderAdapter struct {
	accountSvc CloudAccountService
}

func (a *accountProviderAdapter) GetByID(ctx context.Context, id int64) (*shareddomain.CloudAccount, error) {
	return a.accountSvc.GetAccountWithCredentials(ctx, id)
}

// cloudxAdapterFactory 使用 cloudx 全局注册机制创建适配器
type cloudxAdapterFactory struct{}

func (f *cloudxAdapterFactory) GetAdapter(account *shareddomain.CloudAccount) (cloudx.CloudAdapter, error) {
	creator, err := cloudx.GetAdapterCreator(account.Provider)
	if err != nil {
		return nil, err
	}
	return creator(account)
}
