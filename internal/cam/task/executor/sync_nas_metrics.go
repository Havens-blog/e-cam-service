// Package executor 多云 NAS 指标采集任务执行器(异步任务队列)
//
// 文件：internal/cam/task/executor/sync_nas_metrics.go
//
// 作用：实现 SyncNASMetricsExecutor，由任务队列(定时任务/手动触发)调度，
// 按活跃云账号遍历 NAS 实例(以 ecam_instance 枚举为准)，调 NASMetricQuerier
// 按日采集容量/用量指标写入 ecam_nas_metric(唯一键 {account_id, fs_id, date})。
//
// upsert 语义(spec「日快照取值口径与采集窗口」，与 CDN「同日重采覆盖」不同):
//   - 今日行首写生效:当日已有行不覆盖，仅补当日缺失行(DAO BulkInsertIfAbsent，
//     $setOnInsert 保护)，保证「每天一个值」而非「每天最后一个碰巧写到的值」;
//   - 昨日(及更早)行覆盖更新:次日补采的昨日完整行显式覆盖昨日 00:10 初态行
//     (DAO BulkUpsertMetrics)，首写生效仅保护今日行，昨日行补采后冻结不可变。
//
// 不继承 CDN「当日全零即跳过」过滤(Hard Rule):capacity=0 异常行必须落库可见
// (DAO 数量级自检例外放行并打 qc_status=zero_exception，华为/AWS 实盘现状)。
//
// 厂商适配：NASAdapter 通过可选接口 NASMetricQuerier 暴露指标能力(签名带
// region——NAS 是地域性资源，按实例所在 region 调用对应厂商监控 API)，
// 未实现的厂商跳过不视为失败。
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
)

// TaskTypeNASCollectMetrics NAS 指标采集任务类型
const TaskTypeNASCollectMetrics taskx.TaskType = "nas:collect_metrics"

const (
	// nasCollectDefaultDays 默认采集区间 [昨日, 今日](days=2):补昨日完整行 +
	// 今日初态，避开各厂商监控指标聚合延迟窗口(对齐 CDN days=2 思路)
	nasCollectDefaultDays = 2
	// nasCollectMaxDays 采集区间上限(日粒度回看窗口，对齐 CDN 31 天上限)
	nasCollectMaxDays = 31
	// nasMetricInstanceConcurrency 单账号实例采集有界并发上限(对齐厂商限流 QPS
	// 量级，沿 CDN 指标执行器 semaphore 先例;调大前先评估厂商限流)
	nasMetricInstanceConcurrency = 5
	// nasInstanceSearchPageSize ecam_instance 分页枚举页大小
	nasInstanceSearchPageSize = 500
)

// syncNASMetricsParams 指标采集参数(executor 内部解析用)
type syncNASMetricsParams struct {
	Provider  string `json:"provider"`   // 可选,限定云厂商
	AccountID int64  `json:"account_id"` // 可选,限定单账号
	Days      int    `json:"days"`       // 可选,采集近 N 天(含今日),默认 2
}

// nasAccountCollectResult 单账号采集结果汇总
type nasAccountCollectResult struct {
	written         int                // 成功写入的日指标条数
	failedInstances int                // 采集失败实例数(查询/写库失败，单实例失败不中断其余实例)
	noMetricSupport bool               // 厂商未实现 NASMetricQuerier(整体跳过)
	noNASInstances  bool               // ecam_instance 中无 NAS 实例(非活跃账号，不采集)
	failure         *nasAccountFailure // 厂商/账号维度失败累计(无失败时 ErrorCount=0)
}

// nasProviderFailure 厂商/账号维度失败汇总(任务 Result 携带，运营可查)。
// spec「失败可观测性」：扩展 CDN skipped_providers 雏形为含错误明细的结构，
// 让「适配器失效」与「真实无指标」在结果上可分辨(Hard Rule)。
type nasProviderFailure struct {
	Provider   string `json:"provider"`    // 云厂商
	AccountID  int64  `json:"account_id"`  // 云账号 ID
	ErrorCount int    `json:"error_count"` // 本次任务内该账号累计失败次数
	LastError  string `json:"last_error"`  // 末次错误信息
}

// nasAccountFailure 单账号失败累计器(并发安全)：实例采集在有界并发 goroutine
// 中进行，失败次数与末次错误需互斥累计；采集结束经 snapshot 转为 Result 明细。
type nasAccountFailure struct {
	mu         sync.Mutex
	provider   string
	accountID  int64
	errorCount int
	lastError  string
}

func newNASAccountFailure(provider string, accountID int64) *nasAccountFailure {
	return &nasAccountFailure{provider: provider, accountID: accountID}
}

// record 记一次失败(仅对实际发生的错误计数，不把「真实无数据」算作失败)
func (f *nasAccountFailure) record(err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errorCount++
	f.lastError = err.Error()
}

// snapshot 无失败返回 nil，有失败返回 Result 携带的汇总结构
func (f *nasAccountFailure) snapshot() *nasProviderFailure {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errorCount == 0 {
		return nil
	}
	return &nasProviderFailure{
		Provider:   f.provider,
		AccountID:  f.accountID,
		ErrorCount: f.errorCount,
		LastError:  f.lastError,
	}
}

// SyncNASMetricsExecutor NAS 指标采集任务执行器
type SyncNASMetricsExecutor struct {
	accountRepo   camrepository.CloudAccountRepository
	instanceRepo  camrepository.InstanceRepository
	cloudxFactory *cloudx.AdapterFactory
	metricDAO     dao.NASMetricDAO
	taskRepo      taskx.TaskRepository
	logger        *elog.Component
	// syncingNow 账号级采集互斥(account_id -> task_id)，与 CDN 指标执行器
	// 同模式：同一账号同时只放行一个采集任务，避免重复消耗厂商 API 配额。
	syncMu     sync.Mutex
	syncingNow map[int64]string
	// healthAlerter 自我健康监控告警桥(与日闸告警共用同一实现，cam/wire.go
	// 装配；nil 时仅跳过健康监控，不影响采集主链路)
	healthAlerter NASHealthAlerter
}

// NewSyncNASMetricsExecutor 创建 NAS 指标采集执行器
func NewSyncNASMetricsExecutor(
	accountRepo camrepository.CloudAccountRepository,
	instanceRepo camrepository.InstanceRepository,
	metricDAO dao.NASMetricDAO,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncNASMetricsExecutor {
	return &SyncNASMetricsExecutor{
		accountRepo:   accountRepo,
		instanceRepo:  instanceRepo,
		cloudxFactory: cloudx.NewAdapterFactory(logger),
		metricDAO:     metricDAO,
		taskRepo:      taskRepo,
		logger:        logger,
		syncingNow:    make(map[int64]string),
	}
}

// tryAcquireAccount 占用账号采集权;已被其他任务持有返回 false(持有者自身幂等)。
func (e *SyncNASMetricsExecutor) tryAcquireAccount(accountID int64, taskID string) bool {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()
	if owner, busy := e.syncingNow[accountID]; busy && owner != taskID {
		return false
	}
	e.syncingNow[accountID] = taskID
	return true
}

// releaseAccount 释放账号采集权(仅持有者可释放)。
func (e *SyncNASMetricsExecutor) releaseAccount(accountID int64, taskID string) {
	e.syncMu.Lock()
	defer e.syncMu.Unlock()
	if owner, busy := e.syncingNow[accountID]; busy && owner == taskID {
		delete(e.syncingNow, accountID)
	}
}

// GetType 获取任务类型
func (e *SyncNASMetricsExecutor) GetType() taskx.TaskType {
	return TaskTypeNASCollectMetrics
}

// Execute 执行 NAS 指标采集任务
func (e *SyncNASMetricsExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	e.logger.Info("开始执行NAS指标采集任务", elog.String("task_id", t.ID))

	var params syncNASMetricsParams
	paramsBytes, err := json.Marshal(t.Params)
	if err != nil {
		return fmt.Errorf("序列化任务参数失败: %w", err)
	}
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}

	// 采集区间:近 N 天(含今日，运营时区 Asia/Shanghai)，默认 [昨日, 今日]
	days := params.Days
	if days <= 0 {
		days = nasCollectDefaultDays
	}
	if days > nasCollectMaxDays {
		days = nasCollectMaxDays
	}
	startDate, endDate := nasMetricsDateRange(days)

	e.taskRepo.UpdateProgress(ctx, t.ID, 10,
		fmt.Sprintf("准备采集 %s ~ %s 的 NAS 指标", startDate, endDate))

	// 获取需要采集的账号列表(活跃账号，不检查 EnableAutoSync 开关)
	accounts, err := e.resolveAccounts(ctx, params)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		t.Progress = 100
		t.Message = "无活跃云账号,未采集"
		return nil
	}

	var (
		totalMetrics       int
		collectedAccounts  int
		failedInstances    int
		noMetricSupport    []string
		skippedAccounts    []string
		accountsWithoutNAS []string
		failures           = make([]nasProviderFailure, 0)
	)

	for ai, account := range accounts {
		// 账号级互斥:该账号已有采集任务在执行时跳过，不与其踩踏
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

		result, collectErr := e.collectAccount(ctx, &account, startDate, endDate)
		if collectErr != nil {
			e.logger.Error("采集账号NAS指标失败",
				elog.String("account", account.Name),
				elog.Int64("account_id", account.ID),
				elog.FieldErr(collectErr))
		}
		if f := result.failure.snapshot(); f != nil {
			// 失败可观测(spec「失败可观测性」):厂商/账号维度失败明细入 Result
			failures = append(failures, *f)
		}
		if result.noMetricSupport {
			noMetricSupport = append(noMetricSupport, string(account.Provider))
		}
		if result.noNASInstances {
			// 活跃账号口径 = 存在 ≥1 个 NAS 实例;无实例账号不算采集账号
			accountsWithoutNAS = append(accountsWithoutNAS, account.Name)
		} else {
			collectedAccounts++
		}
		totalMetrics += result.written
		failedInstances += result.failedInstances
		e.releaseAccount(account.ID, t.ID)
	}

	e.taskRepo.UpdateProgress(ctx, t.ID, 95, "正在汇总采集结果")

	// 自我健康监控(每日采集完成钩子):仅全量运行判定，手动单账号/单厂商
	// 运行不判定(避免以偏概全误报)。必达厂商连续 3 天零成功且实盘存在
	// ≥1 个 NAS 实例 → 经共用告警通道升级告警。
	healthAlerts := e.checkMandatoryProviderHealth(ctx, params)

	t.Result = map[string]any{
		"metrics_total":        totalMetrics,
		"accounts":             collectedAccounts,
		"date_range":           fmt.Sprintf("%s ~ %s", startDate, endDate),
		"skipped_accounts":     skippedAccounts,
		"no_metric_support":    noMetricSupport,
		"accounts_without_nas": accountsWithoutNAS,
		"failed_instances":     failedInstances,
		"failures":             failures,
		"health_alerts":        healthAlerts,
	}
	t.Progress = 100
	t.Message = fmt.Sprintf("NAS 指标采集完成,共写入 %d 条日指标(%d 个账号)", totalMetrics, collectedAccounts)

	e.logger.Info("NAS指标采集任务执行完成",
		elog.String("task_id", t.ID),
		elog.Int("total_metrics", totalMetrics),
		elog.Int("failed_instances", failedInstances))
	return nil
}

// resolveAccounts 解析待采集账号:指定 account_id 取单个，否则取全部活跃账号
// (provider 可选过滤)。指标采集对已纳管账号统一执行，不依赖账号的
// EnableAutoSync 开关——与 CDN 一致，避免未开自动同步的账号静默漏采。
func (e *SyncNASMetricsExecutor) resolveAccounts(ctx context.Context, params syncNASMetricsParams) ([]domain.CloudAccount, error) {
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

// listAccountNASInstances 从 ecam_instance 枚举该账号的全部 NAS 实例
// (活跃账号口径以本地资产枚举为准，不调云端 ListInstances;region 取实例
// attributes["region"]，多 region 账号按「实例 → region」逐实例查询)。
func (e *SyncNASMetricsExecutor) listAccountNASInstances(ctx context.Context, account *domain.CloudAccount) ([]camdomain.Instance, error) {
	var out []camdomain.Instance
	offset := int64(0)
	for {
		page, total, err := e.instanceRepo.Search(ctx, camdomain.SearchFilter{
			TenantID:   account.TenantID,
			AccountID:  account.ID,
			AssetTypes: []string{"nas"},
			Offset:     offset,
			Limit:      nasInstanceSearchPageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("枚举账号 NAS 实例失败: %w", err)
		}
		out = append(out, page...)
		if int64(len(page)) < nasInstanceSearchPageSize || int64(len(out)) >= total {
			break
		}
		offset += nasInstanceSearchPageSize
	}
	return out, nil
}

// collectAccount 采集单个账号全部 NAS 实例在 [startDate, endDate] 的指标
// (endDate 即「今日」，今日行首写生效、更早行覆盖更新的分流以此为准)。
// 实例循环有界并发(semaphore 上限 nasMetricInstanceConcurrency)，单实例失败
// 记日志并计入失败明细、不中断其余实例。厂商未实现 NASMetricQuerier 时整体
// 跳过(noMetricSupport，探测不支持属 INFO 语义，不计失败)。
func (e *SyncNASMetricsExecutor) collectAccount(
	ctx context.Context,
	account *domain.CloudAccount,
	startDate, endDate string,
) (nasAccountCollectResult, error) {
	failure := newNASAccountFailure(string(account.Provider), account.ID)

	adapter, err := e.cloudxFactory.CreateAdapter(account)
	if err != nil {
		err = fmt.Errorf("创建适配器失败: %w", err)
		failure.record(err)
		return nasAccountCollectResult{failure: failure}, err
	}

	nasAdapter := adapter.NAS()
	if nasAdapter == nil {
		err := fmt.Errorf("NAS适配器不可用")
		failure.record(err)
		return nasAccountCollectResult{failure: failure}, err
	}

	querier, ok := nasAdapter.(cloudx.NASMetricQuerier)
	if !ok {
		// 探测不支持(指标能力缺失):INFO 语义，不计失败、不触发告警
		e.logger.Info("该厂商NAS适配器不支持指标查询,跳过",
			elog.String("provider", string(account.Provider)))
		return nasAccountCollectResult{noMetricSupport: true, failure: failure}, nil
	}

	// 活跃账号口径:ecam_instance 中存在 ≥1 个 NAS 实例
	instances, err := e.listAccountNASInstances(ctx, account)
	if err != nil {
		failure.record(err)
		return nasAccountCollectResult{failure: failure}, err
	}
	if len(instances) == 0 {
		return nasAccountCollectResult{noNASInstances: true, failure: failure}, nil
	}

	// 实例有界并发采集(沿 CDN 指标执行器 semaphore 先例):单实例失败记日志
	// + 计入失败明细，不中断其余实例;任务取消不再派发。
	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, nasMetricInstanceConcurrency)
		written atomic.Int64
	)
	for _, inst := range instances {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		go func(inst camdomain.Instance) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			written.Add(int64(e.collectInstanceMetrics(ctx, querier, account, inst, startDate, endDate, failure)))
		}(inst)
	}
	wg.Wait()
	return nasAccountCollectResult{written: int(written.Load()), failure: failure}, nil
}

// collectInstanceMetrics 单实例采集 [startDate, endDate] 指标并按日期分流写库:
//   - Date == endDate(今日)行 → BulkInsertIfAbsent 首写生效(当日已有行不覆盖);
//   - 更早(昨日及以前)行 → BulkUpsertMetrics 覆盖更新(次日补采显式覆盖昨日行)。
//
// 不做 CDN「全零跳过」过滤:capacity=0 行照常入批，由 DAO 数量级自检例外放行
// 并打 qc_status=zero_exception。查询/写库失败记日志 + 计入失败明细(调用失败
// 属 ERROR 语义，与「真实无数据」的空结果可分辨)，返回已写条数，不中断其余
// 实例(由调用方并发调度)。
func (e *SyncNASMetricsExecutor) collectInstanceMetrics(
	ctx context.Context,
	querier cloudx.NASMetricQuerier,
	account *domain.CloudAccount,
	inst camdomain.Instance,
	startDate, endDate string,
	failure *nasAccountFailure,
) int {
	fsID := inst.AssetID
	if fsID == "" {
		e.logger.Warn("NAS实例缺少 asset_id,跳过采集",
			elog.Int64("account_id", account.ID),
			elog.Int64("instance_id", inst.ID))
		return 0
	}
	fsName := inst.AssetName
	region, _ := inst.Attributes["region"].(string)

	metrics, err := querier.GetNASMetrics(ctx, fsID, fsName, region, startDate, endDate)
	if err != nil {
		e.logger.Error("查询NAS实例指标失败",
			elog.String("fs_id", fsID),
			elog.String("region", region),
			elog.Int64("account_id", account.ID),
			elog.FieldErr(err))
		failure.record(err)
		return 0
	}

	var todayRows, pastRows []types.NASMetric
	for _, m := range metrics {
		if m.Date == "" {
			continue // 无日期的行无法定位唯一键,跳过
		}
		m.AccountID = account.ID
		m.Provider = string(account.Provider)
		if m.Date == endDate {
			todayRows = append(todayRows, m)
		} else {
			pastRows = append(pastRows, m)
		}
	}

	written := 0
	if len(todayRows) > 0 {
		if err := e.metricDAO.BulkInsertIfAbsent(ctx, todayRows); err != nil {
			e.logger.Error("首写生效批量写入NAS指标失败",
				elog.String("fs_id", fsID),
				elog.Int("batch_size", len(todayRows)),
				elog.FieldErr(err))
			failure.record(err)
		} else {
			written += len(todayRows)
		}
	}
	if len(pastRows) > 0 {
		if err := e.metricDAO.BulkUpsertMetrics(ctx, pastRows); err != nil {
			e.logger.Error("覆盖更新批量写入NAS指标失败",
				elog.String("fs_id", fsID),
				elog.Int("batch_size", len(pastRows)),
				elog.FieldErr(err))
			failure.record(err)
		} else {
			written += len(pastRows)
		}
	}
	return written
}

// nasMetricsDateRange 近 N 天(含今日，运营时区)的采集区间 [startDate, endDate]
func nasMetricsDateRange(days int) (string, string) {
	now := time.Now().In(nasMetricsCSTZone)
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	return startDate, endDate
}

// nasMetricsCSTZone NAS 指标按运营时区(Asia/Shanghai)取日，勿改用服务器本地时区
var nasMetricsCSTZone = time.FixedZone("CST", 8*3600)
