// Package executor 多云 CDN 指标采集任务执行器(异步任务队列)
//
// 文件：internal/cam/task/executor/sync_cdn_metrics.go
//
// 作用：实现 SyncCDNMetricsExecutor，由任务队列(定时任务/手动触发)调度，
// 遍历活跃云账号的活跃 CDN 域名，按日采集带宽/流量/命中率指标并逐条 upsert
// 到 ecam_cdn_metric(按 {domain,date} 幂等)。
//
// 厂商适配：CDNAdapter 通过可选接口 CDNMetricQuerier 暴露指标能力，
// 未实现(探测不到可用监控 API，如 AWS CloudFront)的厂商跳过不视为失败。
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
)

// TaskTypeCollectMetrics CDN 指标采集任务类型
const TaskTypeCollectMetrics taskx.TaskType = "cdn:collect_metrics"

const (
	// defaultCollectDays 默认采集近 1 天(今日;通常凌晨补采前一日可传 days=2)
	defaultCollectDays = 1
	// maxCollectDays 采集区间上限(厂商 5min/日粒度明细最长 31 天)
	maxCollectDays = 31
)

// syncCDNMetricsParams 指标采集参数(executor 内部解析用)
type syncCDNMetricsParams struct {
	Provider  string `json:"provider"`   // 可选,限定云厂商
	AccountID int64  `json:"account_id"` // 可选,限定单账号
	Days      int    `json:"days"`       // 可选,采集近 N 天(含今日),默认 1
}

// SyncCDNMetricsExecutor CDN 指标采集任务执行器
type SyncCDNMetricsExecutor struct {
	accountRepo   camrepository.CloudAccountRepository
	cloudxFactory *cloudx.AdapterFactory
	metricDAO     dao.CDNMetricDAO
	taskRepo      taskx.TaskRepository
	logger        *elog.Component
	// syncingNow 账号级采集互斥(account_id -> task_id),与 SyncAssetsExecutor
	// 同模式:同一账号同时只放行一个采集任务,避免重复消耗厂商 API 配额。
	syncMu     sync.Mutex
	syncingNow map[int64]string
}

// NewSyncCDNMetricsExecutor 创建 CDN 指标采集执行器
func NewSyncCDNMetricsExecutor(
	accountRepo camrepository.CloudAccountRepository,
	metricDAO dao.CDNMetricDAO,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncCDNMetricsExecutor {
	return &SyncCDNMetricsExecutor{
		accountRepo:   accountRepo,
		cloudxFactory: cloudx.NewAdapterFactory(logger),
		metricDAO:     metricDAO,
		taskRepo:      taskRepo,
		logger:        logger,
		syncingNow:    make(map[int64]string),
	}
}

// tryAcquireAccount 占用账号采集权;已被其他任务持有返回 false(持有者自身幂等)。
func (e *SyncCDNMetricsExecutor) tryAcquireAccount(accountID int64, taskID string) bool {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()
	if owner, busy := e.syncingNow[accountID]; busy && owner != taskID {
		return false
	}
	e.syncingNow[accountID] = taskID
	return true
}

// releaseAccount 释放账号采集权(仅持有者可释放)。
func (e *SyncCDNMetricsExecutor) releaseAccount(accountID int64, taskID string) {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()
	if owner, busy := e.syncingNow[accountID]; busy && owner == taskID {
		delete(e.syncingNow, accountID)
	}
}

// GetType 获取任务类型
func (e *SyncCDNMetricsExecutor) GetType() taskx.TaskType {
	return TaskTypeCollectMetrics
}

// Execute 执行 CDN 指标采集任务
func (e *SyncCDNMetricsExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	e.logger.Info("开始执行CDN指标采集任务", elog.String("task_id", t.ID))

	var params syncCDNMetricsParams
	paramsBytes, err := json.Marshal(t.Params)
	if err != nil {
		return fmt.Errorf("序列化任务参数失败: %w", err)
	}
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}

	// 采集区间:近 N 天(含今日,运营时区 Asia/Shanghai)
	days := params.Days
	if days <= 0 {
		days = defaultCollectDays
	}
	if days > maxCollectDays {
		days = maxCollectDays
	}
	startDate, endDate := cdnMetricsDateRange(days)

	e.taskRepo.UpdateProgress(ctx, t.ID, 10,
		fmt.Sprintf("准备采集 %s ~ %s 的 CDN 指标", startDate, endDate))

	// 获取需要采集的账号列表
	accounts, err := e.resolveAccounts(ctx, params)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		t.Progress = 100
		t.Message = "无活跃云账号,未采集"
		return nil
	}

	totalMetrics := 0
	skippedProviders := make([]string, 0)
	skippedAccounts := make([]string, 0)

	for ai, account := range accounts {
		// 账号级互斥:该账号已有采集任务在执行时跳过,不与其踩踏
		if !e.tryAcquireAccount(account.ID, t.ID) {
			e.logger.Warn("该账号已有指标采集任务在执行,跳过该账号",
				elog.Int64("account_id", account.ID),
				elog.String("task_id", t.ID))
			skippedAccounts = append(skippedAccounts, account.Name)
			continue
		}

		progress := 20 + (ai*70)/len(accounts)
		e.taskRepo.UpdateProgress(ctx, t.ID, progress,
			fmt.Sprintf("正在采集账号 %s (%d/%d)", account.Name, ai+1, len(accounts)))

		count, providerSkipped, collectErr := e.collectAccount(ctx, &account, startDate, endDate)
		if collectErr != nil {
			e.logger.Error("采集账号CDN指标失败",
				elog.String("account", account.Name),
				elog.Int64("account_id", account.ID),
				elog.FieldErr(collectErr))
		}
		if providerSkipped {
			skippedProviders = append(skippedProviders, string(account.Provider))
		}
		totalMetrics += count
		e.releaseAccount(account.ID, t.ID)
	}

	e.taskRepo.UpdateProgress(ctx, t.ID, 95, "正在汇总采集结果")

	t.Result = map[string]any{
		"metrics_total":     totalMetrics,
		"accounts":          len(accounts),
		"date_range":        fmt.Sprintf("%s ~ %s", startDate, endDate),
		"skipped_accounts":  skippedAccounts,
		"no_metric_support": skippedProviders,
	}
	t.Progress = 100
	t.Message = fmt.Sprintf("CDN 指标采集完成,共写入 %d 条日指标(%d 个账号)", totalMetrics, len(accounts))

	e.logger.Info("CDN指标采集任务执行完成",
		elog.String("task_id", t.ID),
		elog.Int("total_metrics", totalMetrics))
	return nil
}

// resolveAccounts 解析待采集账号:指定 account_id 取单个,否则取全部活跃账号
// (provider 可选过滤)。
func (e *SyncCDNMetricsExecutor) resolveAccounts(ctx context.Context, params syncCDNMetricsParams) ([]domain.CloudAccount, error) {
	if params.AccountID > 0 {
		account, err := e.accountRepo.GetByID(ctx, params.AccountID)
		if err != nil {
			return nil, fmt.Errorf("获取云账号失败: %w", err)
		}
		return []domain.CloudAccount{account}, nil
	}
	filter := domain.CloudAccountFilter{
		Provider: domain.CloudProvider(params.Provider),
		Status:   domain.CloudAccountStatusActive,
		Limit:    100,
	}
	accts, _, err := e.accountRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("获取云账号列表失败: %w", err)
	}
	return accts, nil
}

// collectAccount 采集单个账号所有活跃 CDN 域名在 [startDate, endDate] 的指标。
// 返回写入条数;providerSkipped 表示厂商未实现 CDNMetricQuerier(整体跳过)。
func (e *SyncCDNMetricsExecutor) collectAccount(
	ctx context.Context,
	account *domain.CloudAccount,
	startDate, endDate string,
) (int, bool, error) {
	adapter, err := e.cloudxFactory.CreateAdapter(account)
	if err != nil {
		return 0, false, fmt.Errorf("创建适配器失败: %w", err)
	}

	cdnAdapter := adapter.CDN()
	if cdnAdapter == nil {
		return 0, true, fmt.Errorf("CDN适配器不可用")
	}

	metricQuerier, ok := cdnAdapter.(cloudx.CDNMetricQuerier)
	if !ok {
		e.logger.Info("该厂商CDN适配器不支持指标查询,跳过",
			elog.String("provider", string(account.Provider)))
		return 0, true, nil
	}

	// 活跃域名列表(CDN 为全局服务,region 不参与过滤)
	instances, err := cdnAdapter.ListInstances(ctx, "")
	if err != nil {
		return 0, false, fmt.Errorf("获取CDN域名列表失败: %w", err)
	}

	written := 0
	for _, inst := range instances {
		// 仅采集在线域名(空状态按在线处理,避免厂商未回状态时漏采)
		if inst.Status != "" && inst.Status != "online" {
			continue
		}
		domainName := inst.DomainName
		if domainName == "" {
			domainName = inst.DomainID
		}

		metrics, err := metricQuerier.GetDomainMetrics(ctx, domainName, inst.DomainID, startDate, endDate)
		if err != nil {
			e.logger.Error("查询CDN域名指标失败",
				elog.String("domain", domainName),
				elog.Int64("account_id", account.ID),
				elog.FieldErr(err))
			continue
		}
		for _, m := range metrics {
			if m.Date == "" || (m.Bytes == 0 && m.Bandwidth == 0 && m.HitRate < 0) {
				continue // 当日无数据不写库
			}
			m.AccountID = account.ID
			m.Provider = string(account.Provider)
			if err := e.metricDAO.UpsertMetric(ctx, m); err != nil {
				e.logger.Error("写入CDN指标失败",
					elog.String("domain", m.Domain),
					elog.String("date", m.Date),
					elog.FieldErr(err))
				continue
			}
			written++
		}
	}
	return written, false, nil
}

// cdnMetricsDateRange 近 N 天(含今日,运营时区)的采集区间 [startDate, endDate]
func cdnMetricsDateRange(days int) (string, string) {
	now := time.Now().In(cdnMetricsCSTZone)
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	return startDate, endDate
}

// cdnMetricsCSTZone CDN 指标按运营时区(Asia/Shanghai)取日,勿改用服务器本地时区
var cdnMetricsCSTZone = time.FixedZone("CST", 8*3600)
