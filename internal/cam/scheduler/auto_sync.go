package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/task/executor"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/elog"
)

// AutoSyncScheduler 自动同步调度器
type AutoSyncScheduler struct {
	accountRepo repository.CloudAccountRepository
	taskQueue   *taskx.Queue
	logger      *elog.Component

	checkInterval time.Duration // 检查间隔
	stopCh        chan struct{}
	wg            sync.WaitGroup
	running       bool
	mu            sync.Mutex
	syncing       map[int64]bool // 正在同步的账号ID，防止重复提交
	syncingMu     sync.Mutex
	// lastMetricsCollectDate CDN 内存闸日期(Asia/Shanghai YYYY-MM-DD)。
	// 仅特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 显式关闭(回滚到内存闸)时
	// 使用;默认走持久化日闸 dailyGate 的 cdn 键(见 auto_sync_metrics.go)。
	lastMetricsCollectDate string
	// lastNASMetricsCollectDate NAS 内存闸日期,仅回滚模式下使用
	// (NAS 生产走持久化日闸,内存闸是 Hard Rule 要求的回滚退路)。
	lastNASMetricsCollectDate string
	// lastOSSMetricsCollectDate OSS 内存闸日期,仅回滚模式下使用
	// (OSS 生产走持久化日闸 oss 键,内存闸是 Hard Rule 要求的回滚退路)。
	lastOSSMetricsCollectDate string
	// dailyGate 持久化日闸(scheduler_state,findOneAndUpdate 原子认领):
	// NAS/CDN/OSS 每日采集的提交入口,详见 daily_gate.go / auto_sync_nas_metrics.go。
	dailyGate *PersistentDailyGate
	// persistentGateEnabled 持久化日闸特性开关(SCHEDULER_PERSISTENT_GATE_ENABLED,
	// 默认开启):开启时 NAS/CDN 每日采集经持久化日闸原子认领;显式关闭时整体
	// 回滚到内存闸(spec「特性开关与回滚」,Hard Rule:必须有退路)。
	persistentGateEnabled bool
	// nowFn 时钟注入点(NAS 回填错峰窗口判定用,单测固定窗口时刻)
	nowFn func() time.Time
}

// NewAutoSyncScheduler 创建自动同步调度器。
// dailyGate 为持久化日闸(scheduler_state DAO 装配);persistentGateEnabled 为
// 特性开关 SCHEDULER_PERSISTENT_GATE_ENABLED 的解析结果(默认开启):开启时
// NAS/CDN 每日采集经持久化日闸原子认领,dailyGate 传 nil 时安全跳过;关闭时
// 回滚到内存闸(调度可用性优先,接受重启重复提交旧缺陷)。
func NewAutoSyncScheduler(
	accountRepo repository.CloudAccountRepository,
	taskQueue *taskx.Queue,
	logger *elog.Component,
	dailyGate *PersistentDailyGate,
	persistentGateEnabled bool,
) *AutoSyncScheduler {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	return &AutoSyncScheduler{
		accountRepo:           accountRepo,
		taskQueue:             taskQueue,
		logger:                logger,
		dailyGate:             dailyGate,
		persistentGateEnabled: persistentGateEnabled,
		checkInterval:         1 * time.Minute, // 每分钟检查一次
		stopCh:                make(chan struct{}),
		syncing:               make(map[int64]bool),
		nowFn:                 time.Now,
	}
}

// Start 启动调度器
func (s *AutoSyncScheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.wg.Add(1)
	go s.run()

	s.logger.Info("自动同步调度器已启动", elog.Duration("check_interval", s.checkInterval))
}

// Stop 停止调度器
func (s *AutoSyncScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopCh)
	s.wg.Wait()

	s.logger.Info("自动同步调度器已停止")
}

// run 运行调度循环
func (s *AutoSyncScheduler) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	// 启动时立即检查一次
	s.checkAndSync()

	for {
		select {
		case <-ticker.C:
			s.checkAndSync()
		case <-s.stopCh:
			return
		}
	}
}

// checkAndSync 检查并触发需要同步的账号
func (s *AutoSyncScheduler) checkAndSync() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 每日 CDN 指标采集(与账号自动同步解耦,详见 auto_sync_metrics.go)
	s.checkMetricsCollection()

	// 每日 NAS 指标采集(持久化日闸原子认领,与账号自动同步解耦,
	// 详见 auto_sync_nas_metrics.go / daily_gate.go)
	s.checkNASMetricsCollection()

	// NAS 历史指标回填(错峰窗口 01:30~06:00 内日闸提交,
	// 详见 auto_sync_nas_backfill.go)
	s.checkNASMetricsBackfill()

	// 每日 OSS 指标采集(持久化日闸 oss 键原子认领,与账号自动同步解耦,
	// 详见 auto_sync_oss_metrics.go / daily_gate.go)
	s.checkOSSMetricsCollection()

	// 获取所有启用自动同步的活跃账号
	accounts, err := s.getAutoSyncAccounts(ctx)
	if err != nil {
		s.logger.Error("获取自动同步账号失败", elog.FieldErr(err))
		return
	}

	if len(accounts) == 0 {
		return
	}

	now := time.Now()
	triggered := 0

	for i := range accounts {
		accountID := accounts[i].ID

		if s.shouldSync(&accounts[i], now) {
			// 检查是否已有正在执行的同步任务
			s.syncingMu.Lock()
			if s.syncing[accountID] {
				s.syncingMu.Unlock()
				s.logger.Debug("账号已有同步任务在执行，跳过",
					elog.Int64("account_id", accountID),
					elog.String("account_name", accounts[i].Name))
				continue
			}
			s.syncing[accountID] = true
			s.syncingMu.Unlock()

			if err := s.triggerSync(ctx, &accounts[i]); err != nil {
				s.syncingMu.Lock()
				delete(s.syncing, accountID)
				s.syncingMu.Unlock()
				s.logger.Error("触发自动同步失败",
					elog.Int64("account_id", accountID),
					elog.String("account_name", accounts[i].Name),
					elog.FieldErr(err))
				continue
			}
			triggered++
		} else {
			// 任务已完成（LastSyncTime 已更新），清除同步标记
			s.syncingMu.Lock()
			delete(s.syncing, accountID)
			s.syncingMu.Unlock()
		}
	}

	if triggered > 0 {
		s.logger.Info("自动同步检查完成",
			elog.Int("total_accounts", len(accounts)),
			elog.Int("triggered", triggered))
	}
}

// getAutoSyncAccounts 获取启用自动同步的账号
func (s *AutoSyncScheduler) getAutoSyncAccounts(ctx context.Context) ([]domain.CloudAccount, error) {
	// 获取所有活跃账号
	filter := domain.CloudAccountFilter{
		Status: domain.CloudAccountStatusActive,
		Limit:  1000, // 最多处理1000个账号
	}

	accounts, _, err := s.accountRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// 过滤出启用自动同步的账号
	var autoSyncAccounts []domain.CloudAccount
	for _, account := range accounts {
		if account.Config.EnableAutoSync && account.Config.SyncInterval > 0 {
			autoSyncAccounts = append(autoSyncAccounts, account)
		}
	}

	return autoSyncAccounts, nil
}

// shouldSync 判断账号是否需要同步
func (s *AutoSyncScheduler) shouldSync(account *domain.CloudAccount, now time.Time) bool {
	// 如果从未同步过，需要同步
	if account.LastSyncTime == nil {
		s.logger.Debug("账号从未同步过，需要同步",
			elog.Int64("account_id", account.ID),
			elog.String("account_name", account.Name))
		return true
	}

	// 计算距离上次同步的时间
	elapsed := now.Sub(*account.LastSyncTime)

	// SyncInterval 存储的是分钟数（前端传入的是分钟）
	// 转换为 Duration
	interval := time.Duration(account.Config.SyncInterval) * time.Minute

	shouldSync := elapsed >= interval

	s.logger.Debug("检查账号是否需要同步",
		elog.Int64("account_id", account.ID),
		elog.String("account_name", account.Name),
		elog.Duration("elapsed", elapsed),
		elog.Duration("interval", interval),
		elog.Any("should_sync", shouldSync))

	return shouldSync
}

// triggerSync 触发同步任务
func (s *AutoSyncScheduler) triggerSync(ctx context.Context, account *domain.CloudAccount) error {
	taskID := uuid.New().String()

	params := buildSyncParams(account)

	task := &taskx.Task{
		ID:        taskID,
		Type:      executor.TaskTypeSyncAssets,
		Status:    taskx.TaskStatusPending,
		Params:    params,
		Progress:  0,
		Message:   "自动同步任务已创建",
		CreatedBy: "auto_sync_scheduler",
	}

	if err := s.taskQueue.Submit(task); err != nil {
		return err
	}

	s.logger.Info("自动同步任务已提交",
		elog.Int64("account_id", account.ID),
		elog.String("account_name", account.Name),
		elog.String("task_id", taskID))

	return nil
}

// buildSyncParams 构建自动同步任务参数（纯函数，便于单元测试）。
// 资产类型：显式配置 SupportedAssetTypes 优先；为空时使用
// domain.DefaultSyncAssetTypes（含 dns，见其注释中的历史缺陷说明）。
func buildSyncParams(account *domain.CloudAccount) map[string]any {
	assetTypes := account.Config.SupportedAssetTypes
	if len(assetTypes) == 0 {
		assetTypes = domain.DefaultSyncAssetTypes
	}

	return map[string]any{
		"provider":    string(account.Provider),
		"asset_types": assetTypes,
		"regions":     account.Config.SupportedRegions,
		"account_id":  account.ID,
		"tenant_id":   account.TenantID,
		"auto_sync":   true, // 标记为自动同步
	}
}
