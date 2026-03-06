// Package collector 账单采集服务
package collector

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	accountservice "github.com/Havens-blog/e-cam-service/internal/account/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/normalizer"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing"
	shareddomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
	"github.com/redis/go-redis/v9"
)

const (
	// lockKeyPrefix Redis 分布式锁 key 前缀
	lockKeyPrefix = "finops:collect:lock:"
	// lockTTL 分布式锁 TTL
	lockTTL = 30 * time.Minute
	// defaultPageSize 默认分页大小（阿里云 BSS API 最大支持 300）
	defaultPageSize = 300
)

// ManualCollectRequest 手动采集请求
type ManualCollectRequest struct {
	AccountID int64
	StartTime time.Time
	EndTime   time.Time
	TenantID  string
}

// CollectorService 账单采集服务
type CollectorService struct {
	normalizer    *normalizer.NormalizerService
	billDAO       repository.BillDAO
	collectLogDAO repository.CollectLogDAO
	accountSvc    accountservice.CloudAccountService
	redisClient   redis.Cmdable
	logger        *elog.Component
}

// NewCollectorService 创建采集服务（Wire DI 兼容）
func NewCollectorService(
	normalizerSvc *normalizer.NormalizerService,
	billDAO repository.BillDAO,
	collectLogDAO repository.CollectLogDAO,
	accountSvc accountservice.CloudAccountService,
	redisClient redis.Cmdable,
	logger *elog.Component,
) *CollectorService {
	return &CollectorService{
		normalizer:    normalizerSvc,
		billDAO:       billDAO,
		collectLogDAO: collectLogDAO,
		accountSvc:    accountSvc,
		redisClient:   redisClient,
		logger:        logger,
	}
}

// acquireLock 获取账号级分布式锁
func (s *CollectorService) acquireLock(ctx context.Context, accountID int64) (bool, error) {
	key := fmt.Sprintf("%s%d", lockKeyPrefix, accountID)
	ok, err := s.redisClient.SetNX(ctx, key, time.Now().Unix(), lockTTL).Result()
	if err != nil {
		return false, fmt.Errorf("redis SetNX failed: %w", err)
	}
	return ok, nil
}

// releaseLock 释放账号级分布式锁
func (s *CollectorService) releaseLock(ctx context.Context, accountID int64) {
	key := fmt.Sprintf("%s%d", lockKeyPrefix, accountID)
	if err := s.redisClient.Del(ctx, key).Err(); err != nil {
		s.logger.Warn("failed to release collect lock",
			elog.Int64("account_id", accountID),
			elog.FieldErr(err),
		)
	}
}

// StartScheduledCollection 启动定时采集（由 cron 调度）
// 列出所有活跃云账号，逐个执行增量采集
func (s *CollectorService) StartScheduledCollection(ctx context.Context) error {
	filter := shareddomain.CloudAccountFilter{
		Status: shareddomain.CloudAccountStatusActive,
		Limit:  1000,
	}
	accounts, _, err := s.accountSvc.ListAccounts(ctx, filter)
	if err != nil {
		s.logger.Error("failed to list active accounts for scheduled collection", elog.FieldErr(err))
		return fmt.Errorf("list active accounts: %w", err)
	}

	s.logger.Info("starting scheduled collection", elog.Int("account_count", len(accounts)))

	for _, acct := range accounts {
		// 计算增量范围
		start, end := s.calculateIncrementalRange(ctx, acct.ID)
		if err := s.CollectAccount(ctx, acct.ID, start, end); err != nil {
			s.logger.Error("scheduled collection failed for account",
				elog.Int64("account_id", acct.ID),
				elog.FieldErr(err),
			)
			// 继续处理其他账号
			continue
		}
	}
	return nil
}

// TriggerManualCollection 手动触发指定云账号和时间范围的采集
func (s *CollectorService) TriggerManualCollection(ctx context.Context, req ManualCollectRequest) error {
	if req.AccountID <= 0 {
		return fmt.Errorf("invalid account ID: %d", req.AccountID)
	}
	if req.EndTime.Before(req.StartTime) || req.EndTime.Equal(req.StartTime) {
		return fmt.Errorf("end time must be after start time")
	}
	return s.CollectAccount(ctx, req.AccountID, req.StartTime, req.EndTime)
}

// CollectAccount 采集单个云账号的账单
// 流程：获取锁 → 获取账号信息 → 创建日志 → 分页拉取 → 标准化 → 存储 → 更新日志 → 释放锁
func (s *CollectorService) CollectAccount(ctx context.Context, accountID int64, startTime, endTime time.Time) error {
	// 1. 获取分布式锁
	acquired, err := s.acquireLock(ctx, accountID)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrCollectLockFailed, err)
	}
	if !acquired {
		return domain.ErrCollectAlreadyRunning
	}
	defer s.releaseLock(ctx, accountID)

	collectStart := time.Now()

	// 2. 获取账号信息（需要完整凭证用于调用云 API）
	account, err := s.accountSvc.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get account %d: %w", accountID, err)
	}

	// 3. 创建采集日志（状态: running）
	collectLog := domain.CollectLog{
		AccountID:  accountID,
		Provider:   string(account.Provider),
		Status:     "running",
		StartTime:  collectStart,
		BillStart:  startTime,
		BillEnd:    endTime,
		TenantID:   account.TenantID,
		CreateTime: time.Now().Unix(),
	}
	logID, err := s.collectLogDAO.Create(ctx, collectLog)
	if err != nil {
		s.logger.Error("failed to create collect log", elog.FieldErr(err))
		return fmt.Errorf("create collect log: %w", err)
	}
	collectLog.ID = logID

	// 4. 获取计费适配器
	creator, err := billing.GetBillingAdapter(account.Provider)
	if err != nil {
		s.finishCollectLog(ctx, &collectLog, collectStart, 0, "failed", fmt.Sprintf("get billing adapter: %v", err))
		return fmt.Errorf("get billing adapter for %s: %w", account.Provider, err)
	}
	adapter, err := creator(account)
	if err != nil {
		s.finishCollectLog(ctx, &collectLog, collectStart, 0, "failed", fmt.Sprintf("create billing adapter: %v", err))
		return fmt.Errorf("create billing adapter: %w", err)
	}

	// 5. 拉取原始账单数据（adapter 内部已处理分页和跨月）
	var totalRecords int64
	params := billing.FetchBillParams{
		AccountID:   strconv.FormatInt(account.ID, 10),
		StartTime:   startTime,
		EndTime:     endTime,
		PageSize:    defaultPageSize,
		Granularity: "monthly",
	}

	items, fetchErr := adapter.FetchBillDetails(ctx, params)
	if fetchErr != nil {
		s.finishCollectLog(ctx, &collectLog, collectStart, 0, "failed", fmt.Sprintf("fetch bill details: %v", fetchErr))
		return fmt.Errorf("fetch bill details: %w", fetchErr)
	}

	s.logger.Info("fetched raw bill items",
		elog.Int64("account_id", accountID),
		elog.Int("item_count", len(items)),
	)

	if len(items) > 0 {
		collectID := strconv.FormatInt(logID, 10)

		// 6. 标准化（CPU 密集，先做）
		unifiedBills, normErr := s.normalizer.Normalize(ctx, items)
		if normErr != nil {
			s.finishCollectLog(ctx, &collectLog, collectStart, 0, "failed", fmt.Sprintf("normalize: %v", normErr))
			return fmt.Errorf("normalize bills: %w", normErr)
		}

		// 填充 AccountID、AccountName、TenantID
		for i := range unifiedBills {
			unifiedBills[i].AccountID = accountID
			unifiedBills[i].AccountName = account.Name
			unifiedBills[i].TenantID = account.TenantID
		}

		// 6.5 去重：删除该账号在该时间范围内的旧账单数据（先删后插）
		startDateStr := startTime.Format("2006-01-02")
		endDateStr := endTime.Format("2006-01-02")
		delRaw, _ := s.billDAO.DeleteRawBillsByAccountAndRange(ctx, accountID, startDateStr, endDateStr)
		delUnified, _ := s.billDAO.DeleteUnifiedBillsByAccountAndRange(ctx, accountID, startDateStr, endDateStr)
		if delRaw > 0 || delUnified > 0 {
			s.logger.Info("dedup: deleted old bills before re-insert",
				elog.Int64("account_id", accountID),
				elog.Int64("deleted_raw", delRaw),
				elog.Int64("deleted_unified", delUnified),
				elog.String("range", fmt.Sprintf("%s ~ %s", startDateStr, endDateStr)),
			)
		}

		// 7. 并发写入 raw bills 和 unified bills
		var wg sync.WaitGroup
		var rawErr, unifiedErr error
		var inserted int64

		wg.Add(2)

		// 写入原始账单（审计用）
		go func() {
			defer wg.Done()
			rawRecords := s.convertToRawRecords(items, accountID, collectID)
			if err := s.batchInsertRawBills(ctx, rawRecords); err != nil {
				rawErr = err
			}
		}()

		// 写入统一账单
		go func() {
			defer wg.Done()
			if len(unifiedBills) > 0 {
				n, err := s.batchInsertUnifiedBills(ctx, unifiedBills)
				if err != nil {
					unifiedErr = err
				}
				inserted = n
			}
		}()

		wg.Wait()

		if rawErr != nil {
			s.logger.Warn("failed to insert raw bills", elog.FieldErr(rawErr))
		}
		if unifiedErr != nil {
			s.finishCollectLog(ctx, &collectLog, collectStart, 0, "failed", fmt.Sprintf("insert unified bills: %v", unifiedErr))
			return fmt.Errorf("insert unified bills: %w", unifiedErr)
		}
		totalRecords = inserted
	}

	// 9. 更新采集日志为成功
	s.finishCollectLog(ctx, &collectLog, collectStart, totalRecords, "success", "")

	// 10. 清除成本分析缓存，确保页面展示最新数据
	s.invalidateCostCache(ctx)

	s.logger.Info("collection completed",
		elog.Int64("account_id", accountID),
		elog.Int64("record_count", totalRecords),
		elog.String("bill_range", fmt.Sprintf("%s ~ %s", startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))),
	)
	return nil
}

// calculateIncrementalRange 计算增量采集时间范围
// 优先检查失败日志进行重试，否则从上次成功采集的 BillEnd 开始
// 如果没有历史记录，默认从当月第一天开始
func (s *CollectorService) calculateIncrementalRange(ctx context.Context, accountID int64) (time.Time, time.Time) {
	now := time.Now().UTC()
	endTime := now

	// 检查是否有失败的采集任务需要重试
	failedLog, err := s.collectLogDAO.GetLastFailed(ctx, accountID)
	if err == nil && !failedLog.BillStart.IsZero() {
		// 重试失败的时间范围：从失败的 BillStart 开始
		return failedLog.BillStart, endTime
	}

	// 查询上次成功采集时间
	successLog, err := s.collectLogDAO.GetLastSuccess(ctx, accountID)
	if err == nil && !successLog.BillEnd.IsZero() {
		// 增量采集：从上次成功的 BillEnd 开始
		return successLog.BillEnd, endTime
	}

	// 无历史记录：默认从当月第一天开始
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return monthStart, endTime
}

// finishCollectLog 更新采集日志的最终状态
func (s *CollectorService) finishCollectLog(ctx context.Context, log *domain.CollectLog, startTime time.Time, recordCount int64, status string, errMsg string) {
	log.EndTime = time.Now()
	log.Duration = time.Since(startTime).Milliseconds()
	log.RecordCount = recordCount
	log.Status = status
	log.ErrorMsg = errMsg

	if err := s.collectLogDAO.Update(ctx, *log); err != nil {
		s.logger.Error("failed to update collect log",
			elog.Int64("log_id", log.ID),
			elog.String("status", status),
			elog.FieldErr(err),
		)
	}
}

// ListCollectLogs 查询采集日志列表
func (s *CollectorService) ListCollectLogs(ctx context.Context, filter repository.CollectLogFilter) ([]domain.CollectLog, int64, error) {
	logs, err := s.collectLogDAO.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list collect logs: %w", err)
	}
	count, err := s.collectLogDAO.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count collect logs: %w", err)
	}
	return logs, count, nil
}

// convertToRawRecords 将 RawBillItem 转换为 RawBillRecord（用于审计存储）
func (s *CollectorService) convertToRawRecords(items []billing.RawBillItem, accountID int64, collectID string) []domain.RawBillRecord {
	records := make([]domain.RawBillRecord, 0, len(items))
	now := time.Now().Unix()
	for _, item := range items {
		records = append(records, domain.RawBillRecord{
			AccountID:   accountID,
			Provider:    string(item.Provider),
			RawData:     item.RawData,
			CollectID:   collectID,
			BillingDate: item.BillingCycle,
			CreateTime:  now,
		})
	}
	return records
}

const batchSize = 1000

// batchInsertRawBills 分批插入原始账单记录
func (s *CollectorService) batchInsertRawBills(ctx context.Context, records []domain.RawBillRecord) error {
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}
		if _, err := s.billDAO.InsertRawBills(ctx, records[i:end]); err != nil {
			return fmt.Errorf("batch insert raw bills [%d:%d]: %w", i, end, err)
		}
	}
	return nil
}

// batchInsertUnifiedBills 分批插入统一账单记录
func (s *CollectorService) batchInsertUnifiedBills(ctx context.Context, bills []domain.UnifiedBill) (int64, error) {
	var total int64
	for i := 0; i < len(bills); i += batchSize {
		end := i + batchSize
		if end > len(bills) {
			end = len(bills)
		}
		inserted, err := s.billDAO.InsertUnifiedBills(ctx, bills[i:end])
		if err != nil {
			return total, fmt.Errorf("batch insert unified bills [%d:%d]: %w", i, end, err)
		}
		total += inserted
	}
	return total, nil
}

// invalidateCostCache 清除成本分析相关的 Redis 缓存
// 采集完成后调用，确保页面展示最新数据
func (s *CollectorService) invalidateCostCache(ctx context.Context) {
	if s.redisClient == nil {
		return
	}
	prefixes := []string{"finops:cost:summary:*", "finops:cost:trend:*"}
	for _, pattern := range prefixes {
		keys, err := s.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			s.logger.Warn("failed to scan cache keys", elog.String("pattern", pattern), elog.FieldErr(err))
			continue
		}
		if len(keys) > 0 {
			deleted, err := s.redisClient.Del(ctx, keys...).Result()
			if err != nil {
				s.logger.Warn("failed to delete cache keys", elog.String("pattern", pattern), elog.FieldErr(err))
			} else {
				s.logger.Info("invalidated cost cache", elog.String("pattern", pattern), elog.Int64("deleted", deleted))
			}
		}
	}
}
