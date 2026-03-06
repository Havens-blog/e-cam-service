package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	accountservice "github.com/Havens-blog/e-cam-service/internal/account/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/domain"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/normalizer"
	"github.com/Havens-blog/e-cam-service/internal/cam/cost/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/billing"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
	"github.com/redis/go-redis/v9"
)

const (
	TaskTypeSyncBilling taskx.TaskType = "cam:sync_billing"
	billingBatchSize                   = 1000
	billingLockPrefix                  = "finops:collect:lock:"
	billingLockTTL                     = 30 * time.Minute
)

// syncBillingParams 账单采集参数（executor 内部解析用）
type syncBillingParams struct {
	AccountID int64  `json:"account_id"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	TenantID  string `json:"tenant_id"`
}

// SyncBillingExecutor 账单采集任务执行器
type SyncBillingExecutor struct {
	normalizer    *normalizer.NormalizerService
	billDAO       repository.BillDAO
	collectLogDAO repository.CollectLogDAO
	accountSvc    accountservice.CloudAccountService
	redisClient   redis.Cmdable
	taskRepo      taskx.TaskRepository
	logger        *elog.Component
}

// NewSyncBillingExecutor 创建账单采集执行器
func NewSyncBillingExecutor(
	normalizerSvc *normalizer.NormalizerService,
	billDAO repository.BillDAO,
	collectLogDAO repository.CollectLogDAO,
	accountSvc accountservice.CloudAccountService,
	redisClient redis.Cmdable,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncBillingExecutor {
	return &SyncBillingExecutor{
		normalizer:    normalizerSvc,
		billDAO:       billDAO,
		collectLogDAO: collectLogDAO,
		accountSvc:    accountSvc,
		redisClient:   redisClient,
		taskRepo:      taskRepo,
		logger:        logger,
	}
}

// GetType 获取任务类型
func (e *SyncBillingExecutor) GetType() taskx.TaskType {
	return TaskTypeSyncBilling
}

// Execute 执行账单采集任务
func (e *SyncBillingExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	var params syncBillingParams
	paramsBytes, _ := json.Marshal(t.Params)
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}

	startTime, err := time.Parse(time.RFC3339, params.StartTime)
	if err != nil {
		return fmt.Errorf("解析 start_time 失败: %w", err)
	}
	endTime, err := time.Parse(time.RFC3339, params.EndTime)
	if err != nil {
		return fmt.Errorf("解析 end_time 失败: %w", err)
	}

	accountID := params.AccountID
	collectStart := time.Now()

	// 1. 获取分布式锁
	e.taskRepo.UpdateProgress(ctx, t.ID, 5, "正在获取采集锁")
	lockKey := fmt.Sprintf("%s%d", billingLockPrefix, accountID)
	acquired, err := e.redisClient.SetNX(ctx, lockKey, time.Now().Unix(), billingLockTTL).Result()
	if err != nil {
		return fmt.Errorf("获取锁失败: %w", err)
	}
	if !acquired {
		return domain.ErrCollectAlreadyRunning
	}
	defer e.redisClient.Del(ctx, lockKey)

	// 2. 获取账号信息
	e.taskRepo.UpdateProgress(ctx, t.ID, 10, "正在获取云账号信息")
	account, err := e.accountSvc.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return fmt.Errorf("获取云账号失败: %w", err)
	}

	// 3. 创建采集日志
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
	logID, err := e.collectLogDAO.Create(ctx, collectLog)
	if err != nil {
		return fmt.Errorf("创建采集日志失败: %w", err)
	}
	collectLog.ID = logID

	// 4. 获取计费适配器
	e.taskRepo.UpdateProgress(ctx, t.ID, 15, "正在创建计费适配器")
	creator, err := billing.GetBillingAdapter(account.Provider)
	if err != nil {
		e.finishLog(ctx, &collectLog, collectStart, 0, "failed", err.Error())
		return fmt.Errorf("获取计费适配器失败: %w", err)
	}
	adapter, err := creator(account)
	if err != nil {
		e.finishLog(ctx, &collectLog, collectStart, 0, "failed", err.Error())
		return fmt.Errorf("创建计费适配器失败: %w", err)
	}

	// 5. 拉取账单数据
	e.taskRepo.UpdateProgress(ctx, t.ID, 20, "正在拉取账单数据")
	fetchParams := billing.FetchBillParams{
		AccountID:   strconv.FormatInt(account.ID, 10),
		StartTime:   startTime,
		EndTime:     endTime,
		PageSize:    300,
		Granularity: "monthly",
	}
	items, err := adapter.FetchBillDetails(ctx, fetchParams)
	if err != nil {
		e.finishLog(ctx, &collectLog, collectStart, 0, "failed", err.Error())
		return fmt.Errorf("拉取账单失败: %w", err)
	}

	e.taskRepo.UpdateProgress(ctx, t.ID, 50, fmt.Sprintf("拉取完成，共 %d 条记录，正在处理", len(items)))

	if len(items) == 0 {
		e.finishLog(ctx, &collectLog, collectStart, 0, "success", "")
		t.Result = map[string]interface{}{"record_count": 0, "provider": string(account.Provider)}
		t.Progress = 100
		t.Message = "采集完成，无账单数据"
		return nil
	}

	// 6. 标准化
	e.taskRepo.UpdateProgress(ctx, t.ID, 55, "正在标准化账单数据")
	unifiedBills, err := e.normalizer.Normalize(ctx, items)
	if err != nil {
		e.finishLog(ctx, &collectLog, collectStart, 0, "failed", err.Error())
		return fmt.Errorf("标准化失败: %w", err)
	}
	for i := range unifiedBills {
		unifiedBills[i].AccountID = accountID
		unifiedBills[i].AccountName = account.Name
		unifiedBills[i].TenantID = account.TenantID
	}

	// 7. 并发写入
	e.taskRepo.UpdateProgress(ctx, t.ID, 60, "正在写入数据库")
	collectID := strconv.FormatInt(logID, 10)
	var wg sync.WaitGroup
	var rawErr, unifiedErr error
	var inserted int64

	wg.Add(2)
	go func() {
		defer wg.Done()
		rawRecords := e.convertToRawRecords(items, accountID, collectID)
		rawErr = e.batchInsertRaw(ctx, rawRecords)
	}()
	go func() {
		defer wg.Done()
		inserted, unifiedErr = e.batchInsertUnified(ctx, unifiedBills)
	}()
	wg.Wait()

	if rawErr != nil {
		e.logger.Warn("写入原始账单失败", elog.FieldErr(rawErr))
	}
	if unifiedErr != nil {
		e.finishLog(ctx, &collectLog, collectStart, 0, "failed", unifiedErr.Error())
		return fmt.Errorf("写入统一账单失败: %w", unifiedErr)
	}

	// 8. 完成
	e.finishLog(ctx, &collectLog, collectStart, inserted, "success", "")

	t.Result = map[string]interface{}{
		"record_count": inserted,
		"provider":     string(account.Provider),
		"bill_range":   fmt.Sprintf("%s ~ %s", startTime.Format("2006-01-02"), endTime.Format("2006-01-02")),
	}
	t.Progress = 100
	t.Message = fmt.Sprintf("采集完成，共 %d 条账单", inserted)
	return nil
}

func (e *SyncBillingExecutor) finishLog(ctx context.Context, log *domain.CollectLog, start time.Time, count int64, status, errMsg string) {
	log.EndTime = time.Now()
	log.Duration = time.Since(start).Milliseconds()
	log.RecordCount = count
	log.Status = status
	log.ErrorMsg = errMsg
	if err := e.collectLogDAO.Update(ctx, *log); err != nil {
		e.logger.Error("更新采集日志失败", elog.Int64("log_id", log.ID), elog.FieldErr(err))
	}
}

func (e *SyncBillingExecutor) convertToRawRecords(items []billing.RawBillItem, accountID int64, collectID string) []domain.RawBillRecord {
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

func (e *SyncBillingExecutor) batchInsertRaw(ctx context.Context, records []domain.RawBillRecord) error {
	for i := 0; i < len(records); i += billingBatchSize {
		end := i + billingBatchSize
		if end > len(records) {
			end = len(records)
		}
		if _, err := e.billDAO.InsertRawBills(ctx, records[i:end]); err != nil {
			return fmt.Errorf("batch raw [%d:%d]: %w", i, end, err)
		}
	}
	return nil
}

func (e *SyncBillingExecutor) batchInsertUnified(ctx context.Context, bills []domain.UnifiedBill) (int64, error) {
	var total int64
	for i := 0; i < len(bills); i += billingBatchSize {
		end := i + billingBatchSize
		if end > len(bills) {
			end = len(bills)
		}
		n, err := e.billDAO.InsertUnifiedBills(ctx, bills[i:end])
		if err != nil {
			return total, fmt.Errorf("batch unified [%d:%d]: %w", i, end, err)
		}
		total += n
	}
	return total, nil
}
