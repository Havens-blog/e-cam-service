// Package executor 多云 NAS 历史指标回填任务执行器(一次性上线回填)
//
// 文件：internal/cam/task/executor/sync_nas_metrics_backfill.go
//
// 作用：实现 SyncNASBackfillExecutor(任务类型 nas:backfill_metrics),上线时从
// 厂商监控 API 拉取启用日前 N 天(14~90 天,默认 30 天)历史指标写入
// ecam_nas_metric,让 30 天趋势上线即可见。复用每日采集执行器(sync_nas_metrics.go)
// 的适配器调用(NASMetricQuerier)与 DAO 写入路径;差异在三点(spec「日快照
// 取值口径与采集窗口 / 回填配额节流」):
//   - 多天批量:区间 [昨日-(N-1), 昨日](不含今日——今日行归每日采集首写生效,
//     回填与日采错峰不碰撞),全部为「昨日及更早」覆盖更新路径(BulkUpsertMetrics);
//   - 节流分片:按「厂商 × 账号」分片,批 = ≤nasBackfillBatchInstances 实例 ×
//     ≤nasBackfillBatchDays 连续天;批间退避 nasBackfillBackoffInitial 起,
//     命中限流指数退避至上限,重试耗尽仍限流则该厂商挂起(其余厂商续跑);
//   - 幂等去重:每批预检 ListExistingMetricDates,已成功批次(区间全已落库)
//     不重试厂商 API,只对缺失日期回源补采,重跑不产生脏行(唯一键幂等)。
//
// 错峰窗口:回填仅在 01:30~06:00(Asia/Shanghai)窗口执行,与每日自动采集
// (00:10 后持久化日闸提交)不碰撞。窗口外触发直接跳过(window_skipped);
// 窗口中途到期则挂起剩余批次(window_suspended),命中限流的厂商挂起同样以
// 去重为续跑基础——次日窗口重新提交任务后,已成功批次凭唯一键预检跳过,
// 从缺失处继续(调度侧见 scheduler/auto_sync_nas_backfill.go 的窗口内日闸提交)。
//
// CloudWatch GetMetricData 配额:每 60 秒 5 万指标点,批大小按配额换算并留
// nasBackfillQuotaMarginPercent(30%)余量(1 实例·天 ≈ 1 指标点)。
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// TaskTypeNASBackfillMetrics NAS 历史指标回填任务类型
const TaskTypeNASBackfillMetrics taskx.TaskType = "nas:backfill_metrics"

// 回填节流与窗口参数(Hard Rule:落地为配置常量,非硬编码散落)
const (
	// nasBackfillDefaultDays 默认回填天数(30 天趋势上线即可见)
	nasBackfillDefaultDays = 30
	// nasBackfillMinDays / nasBackfillMaxDays 回填天数上下限(14~90 天)
	nasBackfillMinDays = 14
	nasBackfillMaxDays = 90
	// nasBackfillBatchInstances 单批实例数上限(厂商×账号分片内)
	nasBackfillBatchInstances = 5
	// nasBackfillBatchDays 单批连续天数上限
	nasBackfillBatchDays = 10
	// nasBackfillBackoffInitial 批间退避起点(限流时自此指数翻倍)
	nasBackfillBackoffInitial = 5 * time.Second
	// nasBackfillBackoffMax 退避上限(指数退避封顶值)
	nasBackfillBackoffMax = 5 * time.Minute
	// nasBackfillRateLimitMaxRetries 单区间限流重试上限(耗尽仍限流 → 厂商挂起)
	nasBackfillRateLimitMaxRetries = 3
	// 窗口 [01:30, 06:00) Asia/Shanghai(与每日采集 00:10 后错峰)
	nasBackfillWindowStartMinute = 1*60 + 30
	nasBackfillWindowEndMinute   = 6 * 60
	// nasBackfillCloudWatchQuotaPointsPerMin CloudWatch GetMetricData 配额
	// (每 60 秒 5 万指标点)
	nasBackfillCloudWatchQuotaPointsPerMin = 50000
	// nasBackfillQuotaMarginPercent 配额换算余量(30%)
	nasBackfillQuotaMarginPercent = 30
)

// syncNASBackfillParams 回填参数(executor 内部解析用)
type syncNASBackfillParams struct {
	Provider  string `json:"provider"`   // 可选,限定云厂商
	AccountID int64  `json:"account_id"` // 可选,限定单账号
	Days      int    `json:"days"`       // 可选,回填近 N 天历史(14~90,默认 30;止于昨日)
}

// nasBackfillDateChunk 日期区间(闭区间,YYYY-MM-DD)
type nasBackfillDateChunk struct {
	start string
	end   string
}

// SyncNASBackfillExecutor NAS 历史指标回填任务执行器
type SyncNASBackfillExecutor struct {
	accountRepo   camrepository.CloudAccountRepository
	instanceRepo  camrepository.InstanceRepository
	cloudxFactory *cloudx.AdapterFactory
	metricDAO     dao.NASMetricDAO
	taskRepo      taskx.TaskRepository
	logger        *elog.Component
	// nasAccountGate 账号级回填互斥：多个回填任务并发时同账号只放行一个,
	// 避免重复消耗厂商 API 配额(与每日采集执行器共用同一实现,
	// nas_account_gate.go;嵌入后方法/字段同名提升)。
	nasAccountGate
	// nowFn / sleepFn 时钟与退避等待注入点(单测固定窗口时刻、消除真实等待)
	nowFn   func() time.Time
	sleepFn func(time.Duration)
}

// NewSyncNASBackfillExecutor 创建 NAS 历史指标回填执行器
func NewSyncNASBackfillExecutor(
	accountRepo camrepository.CloudAccountRepository,
	instanceRepo camrepository.InstanceRepository,
	metricDAO dao.NASMetricDAO,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncNASBackfillExecutor {
	return &SyncNASBackfillExecutor{
		accountRepo:    accountRepo,
		instanceRepo:   instanceRepo,
		cloudxFactory:  cloudx.NewAdapterFactory(logger),
		metricDAO:      metricDAO,
		taskRepo:       taskRepo,
		logger:         logger,
		nasAccountGate: newNASAccountGate(),
		nowFn:          time.Now,
		sleepFn:        time.Sleep,
	}
}

// GetType 获取任务类型
func (e *SyncNASBackfillExecutor) GetType() taskx.TaskType {
	return TaskTypeNASBackfillMetrics
}

// NASBackfillWindowActive 回填错峰窗口判定:[01:30, 06:00) Asia/Shanghai。
// 与每日自动采集(00:10 后)不碰撞;调度侧窗口内才提交,执行侧窗口外直接跳过。
func NASBackfillWindowActive(now time.Time) bool {
	t := now.In(nasMetricsCSTZone)
	minute := t.Hour()*60 + t.Minute()
	return minute >= nasBackfillWindowStartMinute && minute < nasBackfillWindowEndMinute
}

// nasBackfillDateRange 回填区间:止于昨日(今日行归每日采集首写生效,不回填),
// 往前共 N 天(钳位 [14, 90],默认 30)。返回钳位后天数与 [startDate, endDate]。
func nasBackfillDateRange(days int, now time.Time) (int, string, string) {
	if days <= 0 {
		days = nasBackfillDefaultDays
	}
	if days < nasBackfillMinDays {
		days = nasBackfillMinDays
	}
	if days > nasBackfillMaxDays {
		days = nasBackfillMaxDays
	}
	end := now.In(nasMetricsCSTZone).AddDate(0, 0, -1)
	start := end.AddDate(0, 0, -(days - 1))
	return days, start.Format("2006-01-02"), end.Format("2006-01-02")
}

// nasBackfillChunkIndexes 将 n 个元素按 size 分片,返回各片元素下标
// (除末片外每片 size 个;size≤0 或 n≤0 返回 nil)。
func nasBackfillChunkIndexes(n, size int) [][]int {
	if n <= 0 || size <= 0 {
		return nil
	}
	var out [][]int
	for i := 0; i < n; i += size {
		end := i + size
		if end > n {
			end = n
		}
		idx := make([]int, 0, end-i)
		for j := i; j < end; j++ {
			idx = append(idx, j)
		}
		out = append(out, idx)
	}
	return out
}

// nasBackfillChunkDates 将 [start, end](闭区间)按 chunkDays 切成首尾相接的
// 连续日期片(末片可不足 chunkDays)。
func nasBackfillChunkDates(start, end string, chunkDays int) []nasBackfillDateChunk {
	startT, err := time.ParseInLocation("2006-01-02", start, nasMetricsCSTZone)
	if err != nil {
		return nil
	}
	endT, err := time.ParseInLocation("2006-01-02", end, nasMetricsCSTZone)
	if err != nil || endT.Before(startT) {
		return nil
	}
	var out []nasBackfillDateChunk
	cursor := startT
	for !cursor.After(endT) {
		chunkEnd := cursor.AddDate(0, 0, chunkDays-1)
		if chunkEnd.After(endT) {
			chunkEnd = endT
		}
		out = append(out, nasBackfillDateChunk{
			start: cursor.Format("2006-01-02"),
			end:   chunkEnd.Format("2006-01-02"),
		})
		cursor = chunkEnd.AddDate(0, 0, 1)
	}
	return out
}

// nasBackfillMissingRuns 从日期片内扣除已落库日期,返回缺失日期的连续段
// (只对缺失区间回源,已成功批次/日期零调用)。
func nasBackfillMissingRuns(chunk nasBackfillDateChunk, present map[string]struct{}) []nasBackfillDateChunk {
	all := nasBackfillChunkDates(chunk.start, chunk.end, 1)
	var runs []nasBackfillDateChunk
	var runStart string
	prev := ""
	for _, d := range all {
		day := d.start
		if _, ok := present[day]; ok {
			if runStart != "" {
				runs = append(runs, nasBackfillDateChunk{start: runStart, end: prev})
				runStart = ""
			}
			prev = day
			continue
		}
		if runStart == "" {
			runStart = day
		}
		prev = day
	}
	if runStart != "" {
		runs = append(runs, nasBackfillDateChunk{start: runStart, end: prev})
	}
	return runs
}

// nasBackfillQuotaBudgetPointsPerMin CloudWatch GetMetricData 每分钟配额按
// nasBackfillQuotaMarginPercent 余量换算后的可用指标点预算。
func nasBackfillQuotaBudgetPointsPerMin() int {
	return nasBackfillCloudWatchQuotaPointsPerMin * (100 - nasBackfillQuotaMarginPercent) / 100
}

// nasBackfillBatchWithinQuota 判断单批指标点(1 实例·天 ≈ 1 点)是否在配额
// 预算内(防御常量被调大后越过厂商限流)。
func nasBackfillBatchWithinQuota(batchPoints int) bool {
	return batchPoints > 0 && batchPoints <= nasBackfillQuotaBudgetPointsPerMin()
}

// nextNASBackfillBackoff 退避曲线:首退 nasBackfillBackoffInitial,此后指数
// 翻倍,封顶 nasBackfillBackoffMax。
func nextNASBackfillBackoff(prev time.Duration) time.Duration {
	if prev <= 0 {
		return nasBackfillBackoffInitial
	}
	next := prev * 2
	if next > nasBackfillBackoffMax {
		return nasBackfillBackoffMax
	}
	return next
}

// nasIsRateLimitError 识别厂商限流错误(各厂商措辞不同,按关键词宽松匹配;
// 仅用于回填挂起决策,不改变失败计数口径)。
func nasIsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, kw := range []string{"throttl", "toomanyrequests", "rate limit", "ratelimit", "429", "限流"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// Execute 执行 NAS 历史指标回填任务
func (e *SyncNASBackfillExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	e.logger.Info("开始执行NAS历史指标回填任务", elog.String("task_id", t.ID))

	var params syncNASBackfillParams
	paramsBytes, err := json.Marshal(t.Params)
	if err != nil {
		return fmt.Errorf("序列化任务参数失败: %w", err)
	}
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}

	// 配额防御:默认批指标点须在留 30% 余量的配额预算内(常量被误调大时快速失败)
	if !nasBackfillBatchWithinQuota(nasBackfillBatchInstances * nasBackfillBatchDays) {
		return fmt.Errorf("回填批参数(%d 实例 × %d 天)超出 CloudWatch 配额预算 %d 点,拒绝执行",
			nasBackfillBatchInstances, nasBackfillBatchDays, nasBackfillQuotaBudgetPointsPerMin())
	}

	days, startDate, endDate := nasBackfillDateRange(params.Days, e.nowFn())
	if !NASBackfillWindowActive(e.nowFn()) {
		// 窗口外触发:直接跳过(调度侧仅在窗口内提交;手动误触发不消耗厂商配额)
		t.Result = map[string]any{
			"window_skipped": true,
			"date_range":     fmt.Sprintf("%s ~ %s", startDate, endDate),
		}
		t.Progress = 100
		t.Message = "不在回填窗口(01:30~06:00 Asia/Shanghai),本次跳过;将在次日窗口由调度自动续跑"
		return nil
	}

	e.taskRepo.UpdateProgress(ctx, t.ID, 10,
		fmt.Sprintf("准备回填 %s ~ %s 的 NAS 历史指标(%d 天)", startDate, endDate, days))

	accounts, err := resolveNASAccounts(ctx, e.accountRepo, params.AccountID, params.Provider)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		t.Progress = 100
		t.Message = "无活跃云账号,未回填"
		return nil
	}

	// 按「厂商 × 账号」分片:厂商保序聚合,命中限流的厂商整体挂起,
	// 其余厂商续跑(spec:命中限流的厂商回填挂起、次日窗口续跑)
	providerOrder := make([]string, 0, len(accounts))
	byProvider := make(map[string][]domain.CloudAccount)
	for _, account := range accounts {
		p := string(account.Provider)
		if _, seen := byProvider[p]; !seen {
			providerOrder = append(providerOrder, p)
		}
		byProvider[p] = append(byProvider[p], account)
	}

	var (
		metricsTotal        int
		batchesDone         int
		batchesSkipped      int
		windowSuspended     bool
		suspendedProviders  []string
		suspended           = make(map[string]bool)
		failures            = make([]nasProviderFailure, 0)
		collectedAccounts   int
		noMetricSupportList []string
	)

progressLoop:
	for pi, provider := range providerOrder {
		if suspended[provider] {
			continue
		}
		for _, account := range byProvider[provider] {
			if ctx.Err() != nil {
				break progressLoop
			}
			progress := 20 + (pi*70)/len(providerOrder)
			e.taskRepo.UpdateProgress(ctx, t.ID, progress,
				fmt.Sprintf("正在回填账号 %s(厂商 %s,%d/%d)", account.Name, provider, pi+1, len(providerOrder)))

			result := e.backfillAccount(ctx, &account, startDate, endDate, &windowSuspended)
			metricsTotal += result.written
			batchesDone += result.batchesDone
			batchesSkipped += result.batchesSkipped
			collectedAccounts += result.accounts
			if f := result.failure.snapshot(); f != nil {
				failures = append(failures, *f)
			}
			if result.noMetricSupport {
				noMetricSupportList = append(noMetricSupportList, provider)
			}
			if result.rateLimited {
				// 该厂商挂起:跳过其剩余账号,次日窗口续跑(其余厂商不受累)
				suspended[provider] = true
				suspendedProviders = append(suspendedProviders, provider)
				e.logger.Warn("NAS回填命中限流,厂商挂起待次日窗口续跑",
					elog.String("provider", provider))
				continue progressLoop
			}
			if windowSuspended {
				break progressLoop
			}
		}
	}

	e.taskRepo.UpdateProgress(ctx, t.ID, 95, "正在汇总回填结果")

	message := fmt.Sprintf("NAS 历史回填完成,写入 %d 条(%d 批,跳过已成功 %d 批)", metricsTotal, batchesDone, batchesSkipped)
	switch {
	case windowSuspended:
		message = fmt.Sprintf("回填窗口(01:30~06:00)已到期,已写入 %d 条,次日窗口从缺失处继续", metricsTotal)
	case len(suspendedProviders) > 0:
		message = fmt.Sprintf("回填因限流挂起厂商 %v,已写入 %d 条,次日窗口续跑;其余厂商完成", suspendedProviders, metricsTotal)
	}

	t.Result = map[string]any{
		"metrics_written":     metricsTotal,
		"batches_done":        batchesDone,
		"batches_skipped":     batchesSkipped,
		"suspended_providers": suspendedProviders,
		"window_suspended":    windowSuspended,
		"no_metric_support":   noMetricSupportList,
		"accounts":            collectedAccounts,
		"failures":            failures,
		"date_range":          fmt.Sprintf("%s ~ %s", startDate, endDate),
		"days":                days,
	}
	t.Progress = 100
	t.Message = message

	e.logger.Info("NAS历史指标回填任务执行完成",
		elog.String("task_id", t.ID),
		elog.Int("metrics_written", metricsTotal),
		elog.Int("batches_done", batchesDone),
		elog.Int("batches_skipped", batchesSkipped),
		elog.String("suspended_providers", strings.Join(suspendedProviders, ",")))
	return nil
}

// nasBackfillAccountResult 单账号回填结果汇总
type nasBackfillAccountResult struct {
	written         int  // 成功写入的历史指标条数
	batchesDone     int  // 实际发生回源采集的批数
	batchesSkipped  int  // 幂等跳过(整批已成功)的批数
	accounts        int  // 实际回填的账号数(无 NAS 实例不计)
	noMetricSupport bool // 厂商未实现 NASMetricQuerier(INFO 语义跳过)
	rateLimited     bool // 命中限流且重试耗尽(调用方挂起整个厂商)
	failure         *nasAccountFailure
}

// backfillAccount 回填单账号:实例分片(≤5)× 日期分片(≤10)逐批处理。
// 每批先预检已落库日期(幂等去重:整批已成功不重试),只对缺失日期回源补采;
// 批间退避 nasBackfillBackoffInitial;命中限流指数退避、重试耗尽返回 rateLimited
// 由调用方挂起厂商;窗口中途到期置 windowSuspended(次日窗口续跑)。
func (e *SyncNASBackfillExecutor) backfillAccount(
	ctx context.Context,
	account *domain.CloudAccount,
	startDate, endDate string,
	windowSuspended *bool,
) nasBackfillAccountResult {
	result := nasBackfillAccountResult{failure: newNASAccountFailure(string(account.Provider), account.ID)}

	taskID := fmt.Sprintf("backfill:%d", account.ID)
	if !e.tryAcquireAccount(account.ID, taskID) {
		e.logger.Warn("该账号已有回填任务在执行,跳过该账号",
			elog.Int64("account_id", account.ID))
		return result
	}
	defer e.releaseAccount(account.ID, taskID)
	result.accounts = 1

	adapter, err := e.cloudxFactory.CreateAdapter(account)
	if err != nil {
		result.failure.record(fmt.Errorf("创建适配器失败: %w", err))
		return result
	}
	nasAdapter := adapter.NAS()
	if nasAdapter == nil {
		result.failure.record(fmt.Errorf("NAS适配器不可用"))
		return result
	}
	querier, ok := nasAdapter.(cloudx.NASMetricQuerier)
	if !ok {
		// 探测不支持(指标能力缺失):INFO 语义,不计失败、不挂起
		e.logger.Info("该厂商NAS适配器不支持指标查询,回填跳过",
			elog.String("provider", string(account.Provider)))
		result.noMetricSupport = true
		return result
	}

	instances, err := listAccountNASInstancesFromRepo(ctx, e.instanceRepo, account)
	if err != nil {
		result.failure.record(err)
		return result
	}
	if len(instances) == 0 {
		result.accounts = 0
		return result
	}

	instChunks := nasBackfillChunkIndexes(len(instances), nasBackfillBatchInstances)
	dateChunks := nasBackfillChunkDates(startDate, endDate, nasBackfillBatchDays)
	totalBatches := len(instChunks) * len(dateChunks)
	batchIndex := 0

	for _, instChunk := range instChunks {
		for _, dateChunk := range dateChunks {
			// 错峰:窗口中途到期 → 挂起剩余批次,次日窗口续跑
			if !NASBackfillWindowActive(e.nowFn()) {
				*windowSuspended = true
				return result
			}
			if ctx.Err() != nil {
				return result
			}

			// 幂等预检:本批 (fs × 日期) 已落库集合,整批已成功则跳过
			fsIDs := make([]string, 0, len(instChunk))
			for _, idx := range instChunk {
				if fsID := instances[idx].AssetID; fsID != "" {
					fsIDs = append(fsIDs, fsID)
				}
			}
			existing, err := e.metricDAO.ListExistingMetricDates(ctx, account.ID, fsIDs, dateChunk.start, dateChunk.end)
			if err != nil {
				result.failure.record(fmt.Errorf("回填幂等预检失败: %w", err))
				result.batchesSkipped++
				continue
			}

			chunkHadWork := false
			for _, idx := range instChunk {
				inst := instances[idx]
				written, rateLimited := e.backfillInstanceChunkDates(ctx, querier, account, inst, dateChunk, existing, result.failure)
				if rateLimited {
					result.rateLimited = true
					return result
				}
				if written > 0 {
					chunkHadWork = true
					result.written += written
				}
			}
			if chunkHadWork {
				result.batchesDone++
			} else {
				result.batchesSkipped++
			}

			// 批间退避(末批不等待):与厂商 API 保持安全调用间隔
			batchIndex++
			if batchIndex < totalBatches {
				e.sleepFn(nasBackfillBackoffInitial)
			}
		}
	}
	return result
}

// backfillInstanceChunkDates 回填单实例在日期片内缺失的连续区间:
// 预检 existing 推导缺失段(已成功批次零调用)→ 逐段回源 → 历史行(昨日及
// 更早)经 BulkUpsertMetrics 覆盖更新写入。命中限流重试耗尽返回 rateLimited=true;
// 单段失败记失败明细、不中断其余段。
func (e *SyncNASBackfillExecutor) backfillInstanceChunkDates(
	ctx context.Context,
	querier cloudx.NASMetricQuerier,
	account *domain.CloudAccount,
	inst camdomain.Instance,
	dateChunk nasBackfillDateChunk,
	existing map[string]map[string]struct{},
	failure *nasAccountFailure,
) (written int, rateLimited bool) {
	fsID := inst.AssetID
	if fsID == "" {
		e.logger.Warn("NAS实例缺少 asset_id,跳过回填",
			elog.Int64("account_id", account.ID),
			elog.Int64("instance_id", inst.ID))
		return 0, false
	}
	region, _ := inst.Attributes["region"].(string)

	missingRuns := nasBackfillMissingRuns(dateChunk, existing[fsID])
	if len(missingRuns) == 0 {
		return 0, false
	}

	for _, run := range missingRuns {
		metrics, rl := e.fetchNASMetricsWithBackoff(ctx, querier, account, fsID, inst.AssetName, region, run, failure)
		if rl {
			return written, true
		}
		if metrics == nil {
			continue // 非限流错误已记失败明细,该段由下次窗口续跑
		}

		rows := make([]types.NASMetric, 0, len(metrics))
		for _, m := range metrics {
			if m.Date == "" || m.Date < run.start || m.Date > run.end {
				continue // 只写本次请求区间内的行,不越界
			}
			m.AccountID = account.ID
			m.Provider = string(account.Provider)
			rows = append(rows, m)
		}
		if len(rows) == 0 {
			continue
		}
		// 回填全为「昨日及更早」历史行:覆盖更新路径(与每日采集昨日行同语义)
		if err := e.metricDAO.BulkUpsertMetrics(ctx, rows); err != nil {
			e.logger.Error("回填批量写入NAS指标失败",
				elog.String("fs_id", fsID),
				elog.Int("batch_size", len(rows)),
				elog.FieldErr(err))
			failure.record(err)
			continue
		}
		written += len(rows)
	}
	return written, false
}

// fetchNASMetricsWithBackoff 单缺失段回源:命中限流按 nasBackfillBackoffInitial
// 起指数退避重试至 nasBackfillRateLimitMaxRetries 次,耗尽返回 rateLimited=true
// (调用方挂起厂商,次日窗口续跑);非限流错误记失败明细后返回 metrics=nil
// (不重试,该段由下次窗口续跑)。
func (e *SyncNASBackfillExecutor) fetchNASMetricsWithBackoff(
	ctx context.Context,
	querier cloudx.NASMetricQuerier,
	account *domain.CloudAccount,
	fsID, fsName, region string,
	run nasBackfillDateChunk,
	failure *nasAccountFailure,
) (metrics []types.NASMetric, rateLimited bool) {
	wait := time.Duration(0)
	for attempt := 0; ; attempt++ {
		m, err := querier.GetNASMetrics(ctx, fsID, fsName, region, run.start, run.end)
		if err == nil {
			return m, false
		}
		if !nasIsRateLimitError(err) {
			e.logger.Error("查询NAS实例历史指标失败",
				elog.String("fs_id", fsID),
				elog.String("region", region),
				elog.Int64("account_id", account.ID),
				elog.FieldErr(err))
			failure.record(err)
			return nil, false
		}
		if attempt >= nasBackfillRateLimitMaxRetries {
			// 重试耗尽仍限流:挂起该厂商(次日窗口以幂等去重续跑)
			failure.record(fmt.Errorf("限流重试 %d 次耗尽: %w", nasBackfillRateLimitMaxRetries, err))
			return nil, true
		}
		wait = nextNASBackfillBackoff(wait)
		e.logger.Warn("NAS回填命中限流,指数退避后重试",
			elog.String("fs_id", fsID),
			elog.Int("attempt", attempt+1),
			elog.String("backoff", wait.String()))
		e.sleepFn(wait)
	}
}
