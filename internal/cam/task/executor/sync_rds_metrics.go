// Package executor 多云 RDS 指标采集任务执行器(异步任务队列)
//
// 文件：internal/cam/task/executor/sync_rds_metrics.go
//
// 作用：实现 SyncRDSMetricsExecutor，由任务队列(定时任务/手动触发)调度，
// 按活跃云账号遍历 RDS 实例(以 ecam_instance 枚举为准)，调 RDSMetricQuerier
// 按日采集 CPU/内存/磁盘使用率 + 连接数指标写入 ecam_rds_metric
// (唯一键 {account_id, rds_id, date})。
//
// upsert 语义(与 NAS/OSS/Disk 同口径——RDS 是状态型日快照):
//   - 今日行首写生效:当日已有行不覆盖，仅补当日缺失行(DAO BulkInsertIfAbsent，
//     $setOnInsert 保护)，保证「每天一个值」;
//   - 昨日(及更早)行覆盖更新:次日补采的昨日完整行显式覆盖昨日初态行
//     (DAO BulkUpsertMetrics)，首写生效仅保护今日行。
//
// 不继承 CDN「当日全零即跳过」过滤(Hard Rule):四指标全 0 异常行必须落库可见
// (DAO rdsMetricQC 例外放行并打 qc_status=zero_exception，实例停用/重启态的
// 甄别是 T8 读取侧语义，执行器不拦截)。
//
// 厂商适配：RDSAdapter 通过可选接口 RDSMetricQuerier 暴露指标能力(签名带
// region+engine——RDS 是地域性资源且多引擎分派，按实例 attributes 的
// region/engine 逐实例查询，不做全局推断)，未实现的厂商跳过不视为失败。
//
// 平移蓝本(第四次平移)：sync_nas_metrics.go / sync_disk_metrics.go。
// 账号清单解析(resolveNASAccounts)、失败累计器(nasAccountFailure 别名)、
// 日期区间(nasMetricsDateRange/nasMetricsCSTZone)复用同包共享实现;
// 账号互斥闸按任务类型独立:复制 nasAccountGate 为 rdsAccountGate
// (RDS 采集与 NAS/OSS/Disk 采集互不阻塞，各自持互斥表)。
// spec「复用共享账号互斥闸」要求全部采集执行器共用同一把闸——父任务明示
// RDS 复制独立闸(避免与并行任务踩踏)，以父定案为准。
// 自我健康监控(T6)不在本执行器范围:Result 保留 health_alerts 键位恒为 null，
// 与 NAS/Disk Result 同构，装配后置(module.go 无对应 Setter)。
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/Havens-blog/e-common-go/taskx"
	"github.com/gotomicro/ego/core/elog"
)

// TaskTypeRDSCollectMetrics RDS 指标采集任务类型
const TaskTypeRDSCollectMetrics taskx.TaskType = "rds:collect_metrics"

const (
	// rdsCollectDefaultDays 默认采集区间 [昨日, 今日](days=2):补昨日完整行 +
	// 今日初态，避开各厂商监控指标聚合延迟窗口(与 NAS/CDN days=2 同语义)
	rdsCollectDefaultDays = nasCollectDefaultDays
	// rdsCollectMaxDays 采集区间上限(对齐 NAS 31 天上限)
	rdsCollectMaxDays = nasCollectMaxDays
	// rdsMetricInstanceConcurrency 单账号实例采集有界并发上限(对齐 NAS 限流先例)
	rdsMetricInstanceConcurrency = nasMetricInstanceConcurrency
	// rdsInstanceSearchPageSize ecam_instance 分页枚举页大小
	rdsInstanceSearchPageSize = nasInstanceSearchPageSize
)

// syncRDSMetricsParams 指标采集参数(executor 内部解析用)
type syncRDSMetricsParams struct {
	Provider  string `json:"provider"`   // 可选,限定云厂商
	AccountID int64  `json:"account_id"` // 可选,限定单账号
	Days      int    `json:"days"`       // 可选,采集近 N 天(含今日),默认 2
}

// rdsAccountCollectResult 单账号采集结果汇总
type rdsAccountCollectResult struct {
	written         int                // 成功写入的日指标条数
	failedInstances int                // 采集失败实例数(查询/写库失败，单实例失败不中断其余实例)
	noMetricSupport bool               // 厂商未实现 RDSMetricQuerier(整体跳过)
	noRDSInstances  bool               // ecam_instance 中无 RDS 实例(非活跃账号，不采集)
	failure         *nasAccountFailure // 厂商/账号维度失败累计(无失败时 ErrorCount=0)
}

// rdsProviderFailure = nasProviderFailure(别名复用):厂商/账号维度失败明细
// (任务 Result 携带，运营可查；与 NAS/OSS/Disk 同结构，避免跨资源语义漂移)。
type rdsProviderFailure = nasProviderFailure

// rdsAccountGate RDS 执行器账号级互斥闸(account_id -> task_id)。按任务类型
// 独立复制 nasAccountGate、不共享实例:同一账号同时只放行一个 RDS 采集任务，
// 避免重复消耗厂商 API 配额;与 NAS 采集/回填执行器的闸互不相干。
type rdsAccountGate struct {
	gateMu     sync.Mutex
	syncingNow map[int64]string
}

// newRDSAccountGate 创建 RDS 执行器账号级互斥闸
func newRDSAccountGate() rdsAccountGate {
	return rdsAccountGate{syncingNow: make(map[int64]string)}
}

// tryAcquireAccount 尝试获取账号采集权:已有任务在执行时拒绝(返回 false)，
// 避免同名任务踩踏;调用方须成对调用 releaseAccount 释放。
func (g *rdsAccountGate) tryAcquireAccount(accountID int64, taskID string) bool {
	g.gateMu.Lock()
	defer g.gateMu.Unlock()
	if current, exists := g.syncingNow[accountID]; exists && current != taskID {
		return false
	}
	g.syncingNow[accountID] = taskID
	return true
}

// releaseAccount 释放账号采集权
func (g *rdsAccountGate) releaseAccount(accountID int64, taskID string) {
	g.gateMu.Lock()
	defer g.gateMu.Unlock()
	if current, exists := g.syncingNow[accountID]; exists && current == taskID {
		delete(g.syncingNow, accountID)
	}
}

// SyncRDSMetricsExecutor RDS 指标采集任务执行器
type SyncRDSMetricsExecutor struct {
	accountRepo   camrepository.CloudAccountRepository
	instanceRepo  camrepository.InstanceRepository
	cloudxFactory *cloudx.AdapterFactory
	metricDAO     dao.RDSMetricDAO
	taskRepo      taskx.TaskRepository
	logger        *elog.Component
	// rdsAccountGate 账号级采集互斥(account_id -> task_id)，与 NAS 执行器
	// 同模式、独立复刻(见类型注释)。
	rdsAccountGate
}

// NewSyncRDSMetricsExecutor 创建 RDS 指标采集执行器
func NewSyncRDSMetricsExecutor(
	accountRepo camrepository.CloudAccountRepository,
	instanceRepo camrepository.InstanceRepository,
	metricDAO dao.RDSMetricDAO,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncRDSMetricsExecutor {
	return &SyncRDSMetricsExecutor{
		accountRepo:    accountRepo,
		instanceRepo:   instanceRepo,
		cloudxFactory:  cloudx.NewAdapterFactory(logger),
		metricDAO:      metricDAO,
		taskRepo:       taskRepo,
		logger:         logger,
		rdsAccountGate: newRDSAccountGate(),
	}
}

var _ taskx.TaskExecutor = (*SyncRDSMetricsExecutor)(nil)

// GetType 获取任务类型
func (e *SyncRDSMetricsExecutor) GetType() taskx.TaskType {
	return TaskTypeRDSCollectMetrics
}

// Execute 执行 RDS 指标采集任务
func (e *SyncRDSMetricsExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	e.logger.Info("开始执行RDS指标采集任务", elog.String("task_id", t.ID))

	var params syncRDSMetricsParams
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
		days = rdsCollectDefaultDays
	}
	if days > rdsCollectMaxDays {
		days = rdsCollectMaxDays
	}
	startDate, endDate := nasMetricsDateRange(days)

	e.taskRepo.UpdateProgress(ctx, t.ID, 10,
		fmt.Sprintf("准备采集 %s ~ %s 的 RDS 指标", startDate, endDate))

	// 获取需要采集的账号列表(活跃账号，不检查 EnableAutoSync 开关)
	accounts, err := resolveNASAccounts(ctx, e.accountRepo, params.AccountID, params.Provider)
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
		accountsWithoutRDS []string
		failures           = make([]rdsProviderFailure, 0)
	)

	for ai, account := range accounts {
		// 账号级互斥:该账号已有 RDS 采集任务在执行时跳过，不与其踩踏
		if !e.tryAcquireAccount(account.ID, t.ID) {
			e.logger.Warn("该账号已有RDS指标采集任务在执行,跳过该账号",
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
			e.logger.Error("采集账号RDS指标失败",
				elog.String("account", account.Name),
				elog.Int64("account_id", account.ID),
				elog.FieldErr(collectErr))
		}
		if f := result.failure.snapshot(); f != nil {
			// 失败可观测:账号维度失败明细入 Result["failures"]，让「适配器失效」
			// 与「真实无指标」在结果上可分辨(探测不支持与真实无数据不计失败)
			failures = append(failures, *f)
		}
		if result.noMetricSupport {
			noMetricSupport = append(noMetricSupport, string(account.Provider))
		}
		if result.noRDSInstances {
			// 活跃账号口径 = 存在 ≥1 个 RDS 实例;无实例账号不算采集账号
			accountsWithoutRDS = append(accountsWithoutRDS, account.Name)
		} else {
			collectedAccounts++
		}
		totalMetrics += result.written
		failedInstances += result.failedInstances
		e.releaseAccount(account.ID, t.ID)
	}

	e.taskRepo.UpdateProgress(ctx, t.ID, 95, "正在汇总采集结果")

	// RDS 执行器暂不装配自我健康监控(T6 独立任务范围):health_alerts 键位
	// 恒为 null,与 NAS/OSS/Disk Result 同构,装配后替换为钩子调用。
	t.Result = map[string]any{
		"metrics_total":        totalMetrics,
		"accounts":             collectedAccounts,
		"date_range":           fmt.Sprintf("%s ~ %s", startDate, endDate),
		"skipped_accounts":     skippedAccounts,
		"no_metric_support":    noMetricSupport,
		"accounts_without_rds": accountsWithoutRDS,
		"failed_instances":     failedInstances,
		"failures":             failures,
		"health_alerts":        ([]string)(nil),
	}
	t.Progress = 100
	t.Message = fmt.Sprintf("RDS 指标采集完成,共写入 %d 条日指标(%d 个账号)", totalMetrics, collectedAccounts)

	e.logger.Info("RDS指标采集任务执行完成",
		elog.String("task_id", t.ID),
		elog.Int("total_metrics", totalMetrics),
		elog.Int("failed_instances", failedInstances))
	return nil
}

// listAccountRDSInstances 从 ecam_instance 枚举该账号的全部 RDS 实例
// (活跃账号口径以本地资产枚举为准，不调云端 ListInstances;rds_id 取
// instance.AssetID——资产同步侧 sync_rds.go 落库 AssetID=inst.InstanceID;
// region/engine 逐实例从 attributes 取，多 region/多引擎账号按
// 「实例 → region/engine」查询)。
func (e *SyncRDSMetricsExecutor) listAccountRDSInstances(ctx context.Context, account *domain.CloudAccount) ([]camdomain.Instance, error) {
	var out []camdomain.Instance
	offset := int64(0)
	for {
		page, total, err := e.instanceRepo.Search(ctx, camdomain.SearchFilter{
			TenantID:   account.TenantID,
			AccountID:  account.ID,
			AssetTypes: []string{"rds"},
			Offset:     offset,
			Limit:      rdsInstanceSearchPageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("枚举账号 RDS 实例失败: %w", err)
		}
		out = append(out, page...)
		if int64(len(page)) < rdsInstanceSearchPageSize || int64(len(out)) >= total {
			break
		}
		offset += rdsInstanceSearchPageSize
	}
	return out, nil
}

// collectAccount 采集单个账号全部 RDS 实例在 [startDate, endDate] 的指标
// (endDate 即「今日」，今日行首写生效、更早行覆盖更新的分流以此为准)。
// rds 循环有界并发(semaphore 上限 rdsMetricInstanceConcurrency)，单实例失败
// 记日志并计入失败明细、不中断其余实例。厂商未实现 RDSMetricQuerier 时整体
// 跳过(noMetricSupport，探测不支持属 INFO 语义，不计失败)。
func (e *SyncRDSMetricsExecutor) collectAccount(
	ctx context.Context,
	account *domain.CloudAccount,
	startDate, endDate string,
) (rdsAccountCollectResult, error) {
	failure := newNASAccountFailure(string(account.Provider), account.ID)

	adapter, err := e.cloudxFactory.CreateAdapter(account)
	if err != nil {
		err = fmt.Errorf("创建适配器失败: %w", err)
		failure.record(err)
		return rdsAccountCollectResult{failure: failure}, err
	}

	rdsAdapter := adapter.RDS()
	if rdsAdapter == nil {
		err := fmt.Errorf("RDS适配器不可用")
		failure.record(err)
		return rdsAccountCollectResult{failure: failure}, err
	}

	querier, ok := rdsAdapter.(cloudx.RDSMetricQuerier)
	if !ok {
		// 探测不支持(指标能力缺失):INFO 语义，不计失败、不触发告警
		e.logger.Info("该厂商RDS适配器不支持指标查询,跳过",
			elog.String("provider", string(account.Provider)))
		return rdsAccountCollectResult{noMetricSupport: true, failure: failure}, nil
	}

	// 活跃账号口径:ecam_instance 中存在 ≥1 个 RDS 实例(不依赖 EnableAutoSync)
	instances, err := e.listAccountRDSInstances(ctx, account)
	if err != nil {
		failure.record(err)
		return rdsAccountCollectResult{failure: failure}, err
	}
	if len(instances) == 0 {
		return rdsAccountCollectResult{noRDSInstances: true, failure: failure}, nil
	}

	// rds 有界并发采集(与 NAS 实例并发同模式):单实例失败记日志
	// + 计入失败明细，不中断其余实例;任务取消不再派发。
	var (
		wg          sync.WaitGroup
		sem         = make(chan struct{}, rdsMetricInstanceConcurrency)
		written     atomic.Int64
		failedCount atomic.Int64
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
			n := e.collectRDSMetrics(ctx, querier, account, inst, startDate, endDate, failure)
			if n < 0 {
				failedCount.Add(1)
				return
			}
			written.Add(int64(n))
		}(inst)
	}
	wg.Wait()
	return rdsAccountCollectResult{
		written:         int(written.Load()),
		failedInstances: int(failedCount.Load()),
		failure:         failure,
	}, nil
}

// collectRDSMetrics 单实例采集 [startDate, endDate] 指标并按日期分流写库:
//   - Date == endDate(今日)行 → BulkInsertIfAbsent 首写生效(当日已有行不覆盖);
//   - 更早(昨日及以前)行 → BulkUpsertMetrics 覆盖更新(次日补采显式覆盖昨日行)。
//
// rdsID/instanceName 取资产实例 AssetID/AssetName(sync_rds.go 落库形态);
// region/engine 从实例 attributes 取(地域性资源 + 多引擎分派，按实例真实值
// 查询，不做全局推断);AccountID/Provider 由执行器回填(querier 签名不感知账号，
// 唯一键必须含 account_id 支持跨账号共享盘同型);QcStatus 由 DAO 打标。
// 单实例失败返回 -1(计入 failedInstances)并记入账号失败累计器，不中断其余实例。
func (e *SyncRDSMetricsExecutor) collectRDSMetrics(
	ctx context.Context,
	querier cloudx.RDSMetricQuerier,
	account *domain.CloudAccount,
	inst camdomain.Instance,
	startDate, endDate string,
	failure *nasAccountFailure,
) int {
	rdsID := inst.AssetID
	instanceName := inst.AssetName
	region := inst.GetStringAttribute("region")
	engine := inst.GetStringAttribute("engine")
	if rdsID == "" {
		e.logger.Warn("RDS实例AssetID为空,跳过采集",
			elog.Int64("account_id", account.ID))
		return 0
	}

	metrics, err := querier.GetRDSMetrics(ctx, rdsID, instanceName, region, engine, startDate, endDate)
	if err != nil {
		e.logger.Error("查询RDS实例指标失败",
			elog.String("rds_id", rdsID),
			elog.String("region", region),
			elog.String("engine", engine),
			elog.Int64("account_id", account.ID),
			elog.FieldErr(err))
		failure.record(err)
		return -1
	}

	var todayRows, pastRows []types.RDSMetric
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
			e.logger.Error("首写生效批量写入RDS指标失败",
				elog.String("rds_id", rdsID),
				elog.FieldErr(err))
			failure.record(err)
			return -1
		}
		written += len(todayRows)
	}
	if len(pastRows) > 0 {
		if err := e.metricDAO.BulkUpsertMetrics(ctx, pastRows); err != nil {
			e.logger.Error("覆盖更新批量写入RDS指标失败",
				elog.String("rds_id", rdsID),
				elog.FieldErr(err))
			failure.record(err)
			return -1
		}
		written += len(pastRows)
	}
	return written
}
