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
}

// NewAutoSyncScheduler 创建自动同步调度器
func NewAutoSyncScheduler(
	accountRepo repository.CloudAccountRepository,
	taskQueue *taskx.Queue,
	logger *elog.Component,
) *AutoSyncScheduler {
	if logger == nil {
		logger = elog.DefaultLogger
	}
	return &AutoSyncScheduler{
		accountRepo:   accountRepo,
		taskQueue:     taskQueue,
		logger:        logger,
		checkInterval: 1 * time.Minute, // 每分钟检查一次
		stopCh:        make(chan struct{}),
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
		if s.shouldSync(&accounts[i], now) {
			if err := s.triggerSync(ctx, &accounts[i]); err != nil {
				s.logger.Error("触发自动同步失败",
					elog.Int64("account_id", accounts[i].ID),
					elog.String("account_name", accounts[i].Name),
					elog.FieldErr(err))
				continue
			}
			triggered++
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

	// 获取要同步的资产类型
	assetTypes := account.Config.SupportedAssetTypes
	if len(assetTypes) == 0 {
		assetTypes = []string{"ecs"} // 默认同步 ECS
	}

	// 构建任务参数
	params := map[string]any{
		"provider":    string(account.Provider),
		"asset_types": assetTypes,
		"regions":     account.Config.SupportedRegions,
		"account_id":  account.ID,
		"tenant_id":   account.TenantID,
		"auto_sync":   true, // 标记为自动同步
	}

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
