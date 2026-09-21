// Package executor 多云 Disk(云硬盘)指标采集任务执行器(异步任务队列)
//
// 文件：internal/cam/task/executor/sync_disk_metrics.go
//
// 作用：实现 SyncDiskMetricsExecutor，由任务队列(定时任务/手动触发)调度，
// 按活跃云账号遍历 Disk 实例(以 ecam_instance 枚举为准)，调 DiskMetricQuerier
// 按日采集使用率/IOPS/吞吐指标写入 ecam_disk_metric(唯一键 {account_id, disk_id, date})。
//
// upsert 语义(与 NAS/OSS 同口径——Disk 是状态型日快照，spec「Proposed Solution」第 3 条):
//   - 今日行首写生效:当日已有行不覆盖，仅补当日缺失行(DAO BulkInsertIfAbsent，
//     $setOnInsert 保护)，保证「每天一个值」而非「每天最后一个碰巧写到的值」;
//   - 昨日(及更早)行覆盖更新:次日补采的昨日完整行显式覆盖昨日初态行
//     (DAO BulkUpsertMetrics)，首写生效仅保护今日行。
//
// 不继承 CDN「当日全零即跳过」过滤(Hard Rule):usage_percent=0 异常行必须落库可见
// (口径缺失 0 与合法闲盘 0 双语义)，qc_status=zero_exception 打标由 DAO 写路径
// (diskMetricQC)统一落实，执行器透传适配器标注(含 usage_scope 口径标注)不篡改。
//
// 厂商适配：DiskAdapter 通过可选接口 DiskMetricQuerier 暴露指标能力(签名带
// region——云硬盘是地域性资源，disk_id 绑定 region，与 NAS 同型而非 OSS 全局
// 服务；按实例 attributes["region"] 逐盘查询，不做全局 region 推断)，
// 未实现的厂商跳过不视为失败。
//
// 复用说明(第三次平移，蓝本 sync_oss_metrics.go / sync_nas_metrics.go):账号
// 互斥闸(nasAccountGate)、账号清单解析(resolveNASAccounts)、账号失败累计器
// (nasAccountFailure/nasProviderFailure)、日期区间(nasMetricsDateRange/
// nasMetricsCSTZone)均直接复用同包共享实现，不重造(Hard Rule:复用共享账号互斥闸)。
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
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
	"github.com/gotomicro/ego/core/elog"
)

// TaskTypeDiskCollectMetrics Disk 指标采集任务类型
const TaskTypeDiskCollectMetrics taskx.TaskType = "disk:collect_metrics"

const (
	// diskCollectDefaultDays 默认采集区间 [昨日, 今日](days=2):补昨日完整行 +
	// 今日初态(对齐 NAS/OSS days=2，避开各厂商监控指标聚合延迟窗口)
	diskCollectDefaultDays = nasCollectDefaultDays
	// diskCollectMaxDays 采集区间上限(日粒度回看/补采窗口，对齐 NAS/OSS/CDN 31 天上限)
	diskCollectMaxDays = nasCollectMaxDays
	// diskMetricInstanceConcurrency 单账号 disk 采集有界并发上限(proposal「并发安全」:
	// disk 有界并发 5，对齐 NAS 实例并发量级;调大前先评估厂商限流)
	diskMetricInstanceConcurrency = nasMetricInstanceConcurrency
	// diskInstanceSearchPageSize ecam_instance 分页枚举页大小
	diskInstanceSearchPageSize = nasInstanceSearchPageSize
)

// syncDiskMetricsParams 指标采集参数(executor 内部解析用)
type syncDiskMetricsParams struct {
	Provider  string `json:"provider"`   // 可选,限定云厂商
	AccountID int64  `json:"account_id"` // 可选,限定单账号
	Days      int    `json:"days"`       // 可选,采集近 N 天(含今日),默认 2,上限 31
}

// diskAccountCollectResult 单账号采集结果汇总
type diskAccountCollectResult struct {
	written         int                // 成功写入的日指标条数
	failedDisks     int                // 采集失败 disk 数(查询/写库失败，单 disk 失败不中断其余 disk)
	noMetricSupport bool               // 厂商未实现 DiskMetricQuerier(整体跳过)
	noDiskInstances bool               // ecam_instance 中无 Disk 实例(非活跃账号，不采集)
	failure         *nasAccountFailure // 账号维度失败累计(无失败时 ErrorCount=0)
}

// diskProviderFailure Disk 失败明细汇总(task 6 命名口径，与 NAS/OSS 共享同一底层
// 结构——字段同型 {provider, account_id, error_count, last_error}，直接复用
// nasProviderFailure 不重造，仅按 Disk 语义起别名)。
type diskProviderFailure = nasProviderFailure

// SyncDiskMetricsExecutor Disk 指标采集任务执行器
type SyncDiskMetricsExecutor struct {
	accountRepo   camrepository.CloudAccountRepository
	instanceRepo  camrepository.InstanceRepository
	cloudxFactory *cloudx.AdapterFactory
	metricDAO     dao.DiskMetricDAO
	taskRepo      taskx.TaskRepository
	logger        *elog.Component
	// nasAccountGate 账号级采集互斥(account_id -> task_id)，复用 NAS 共享闸:
	// 同一账号同时只放行一个采集任务，避免重复消耗厂商 API 配额(Hard Rule)。
	nasAccountGate
	// healthAlerter 自我健康监控告警桥(与日闸/NAS/OSS 健康监控共用同一实现，
	// cam/wire.go 装配；nil 时仅跳过健康监控，不影响采集主链路)
	healthAlerter DiskHealthAlerter
}

// NewSyncDiskMetricsExecutor 创建 Disk 指标采集执行器
func NewSyncDiskMetricsExecutor(
	accountRepo camrepository.CloudAccountRepository,
	instanceRepo camrepository.InstanceRepository,
	metricDAO dao.DiskMetricDAO,
	taskRepo taskx.TaskRepository,
	logger *elog.Component,
) *SyncDiskMetricsExecutor {
	return &SyncDiskMetricsExecutor{
		accountRepo:    accountRepo,
		instanceRepo:   instanceRepo,
		cloudxFactory:  cloudx.NewAdapterFactory(logger),
		metricDAO:      metricDAO,
		taskRepo:       taskRepo,
		logger:         logger,
		nasAccountGate: newNASAccountGate(),
	}
}

// GetType 获取任务类型
func (e *SyncDiskMetricsExecutor) GetType() taskx.TaskType {
	return TaskTypeDiskCollectMetrics
}

// Execute 执行 Disk 指标采集任务
func (e *SyncDiskMetricsExecutor) Execute(ctx context.Context, t *taskx.Task) error {
	e.logger.Info("开始执行Disk指标采集任务", elog.String("task_id", t.ID))

	var params syncDiskMetricsParams
	paramsBytes, err := json.Marshal(t.Params)
	if err != nil {
		return fmt.Errorf("序列化任务参数失败: %w", err)
	}
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}

	// 采集区间:近 N 天(含今日，运营时区 Asia/Shanghai)，默认 [昨日, 今日]，
	// 上限 31 天补采窗口(超限收敛)
	days := params.Days
	if days <= 0 {
		days = diskCollectDefaultDays
	}
	if days > diskCollectMaxDays {
		days = diskCollectMaxDays
	}
	startDate, endDate := nasMetricsDateRange(days)

	e.taskRepo.UpdateProgress(ctx, t.ID, 10,
		fmt.Sprintf("准备采集 %s ~ %s 的 Disk 指标", startDate, endDate))

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
		totalMetrics        int
		collectedAccounts   int
		failedDisks         int
		noMetricSupport     []string
		skippedAccounts     []string
		accountsWithoutDisk []string
		failures            = make([]diskProviderFailure, 0)
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
			e.logger.Error("采集账号Disk指标失败",
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
		if result.noDiskInstances {
			// 活跃账号口径 = 存在 ≥1 个 Disk 实例;无实例账号不算采集账号
			accountsWithoutDisk = append(accountsWithoutDisk, account.Name)
		} else {
			collectedAccounts++
		}
		totalMetrics += result.written
		failedDisks += result.failedDisks
		e.releaseAccount(account.ID, t.ID)
	}

	e.taskRepo.UpdateProgress(ctx, t.ID, 95, "正在汇总采集结果")

	// 自我健康监控(每日采集完成钩子):仅全量运行判定，手动单账号/单厂商
	// 运行不判定(避免以偏概全误报)。必达厂商连续 3 天零成功且实盘存在
	// ≥1 个 Disk 实例 → 经共用告警通道升级告警。
	healthAlerts := e.checkMandatoryProviderHealth(ctx, params)

	t.Result = map[string]any{
		"metrics_total":         totalMetrics,
		"accounts":              collectedAccounts,
		"date_range":            fmt.Sprintf("%s ~ %s", startDate, endDate),
		"skipped_accounts":      skippedAccounts,
		"no_metric_support":     noMetricSupport,
		"accounts_without_disk": accountsWithoutDisk,
		"failed_disks":          failedDisks,
		"failures":              failures,
		"health_alerts":         healthAlerts,
	}
	t.Progress = 100
	t.Message = fmt.Sprintf("Disk 指标采集完成,共写入 %d 条日指标(%d 个账号)", totalMetrics, collectedAccounts)

	e.logger.Info("Disk指标采集任务执行完成",
		elog.String("task_id", t.ID),
		elog.Int("total_metrics", totalMetrics),
		elog.Int("failed_disks", failedDisks))
	return nil
}

// listAccountDiskInstances 从 ecam_instance 枚举该账号的全部 Disk 实例
// (活跃账号口径以本地资产枚举为准，不调云端 ListInstances;region 取
// attributes["region"]，多 region 账号按「实例 → region」逐盘查询;
// disk_id 取 instance.AssetID——资产同步侧 convertDiskToInstance 落库即 disk_id)。
func (e *SyncDiskMetricsExecutor) listAccountDiskInstances(ctx context.Context, account *domain.CloudAccount) ([]camdomain.Instance, error) {
	var out []camdomain.Instance
	offset := int64(0)
	for {
		page, total, err := e.instanceRepo.Search(ctx, camdomain.SearchFilter{
			TenantID:   account.TenantID,
			AccountID:  account.ID,
			AssetTypes: []string{"disk"},
			Offset:     offset,
			Limit:      diskInstanceSearchPageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("枚举账号 Disk 实例失败: %w", err)
		}
		out = append(out, page...)
		if int64(len(page)) < diskInstanceSearchPageSize || int64(len(out)) >= total {
			break
		}
		offset += diskInstanceSearchPageSize
	}
	return out, nil
}

// collectAccount 采集单个账号全部 Disk 实例在 [startDate, endDate] 的指标
// (endDate 即「今日」，今日行首写生效、更早行覆盖更新的分流以此为准)。
// disk 循环有界并发(semaphore 上限 diskMetricInstanceConcurrency)，单 disk 失败
// 记日志并计入失败明细、不中断其余 disk。厂商未实现 DiskMetricQuerier 时整体
// 跳过(noMetricSupport，探测不支持属 INFO 语义，不计失败)。
func (e *SyncDiskMetricsExecutor) collectAccount(
	ctx context.Context,
	account *domain.CloudAccount,
	startDate, endDate string,
) (diskAccountCollectResult, error) {
	failure := newNASAccountFailure(string(account.Provider), account.ID)

	adapter, err := e.cloudxFactory.CreateAdapter(account)
	if err != nil {
		err = fmt.Errorf("创建适配器失败: %w", err)
		failure.record(err)
		return diskAccountCollectResult{failure: failure}, err
	}

	diskAdapter := adapter.Disk()
	if diskAdapter == nil {
		err := fmt.Errorf("Disk适配器不可用")
		failure.record(err)
		return diskAccountCollectResult{failure: failure}, err
	}

	querier, ok := diskAdapter.(cloudx.DiskMetricQuerier)
	if !ok {
		// 探测不支持(指标能力缺失):INFO 语义，不计失败、不触发告警
		e.logger.Info("该厂商Disk适配器不支持指标查询,跳过",
			elog.String("provider", string(account.Provider)))
		return diskAccountCollectResult{noMetricSupport: true, failure: failure}, nil
	}

	// 活跃账号口径:ecam_instance 中存在 ≥1 个 Disk 实例(不依赖 EnableAutoSync)
	instances, err := e.listAccountDiskInstances(ctx, account)
	if err != nil {
		failure.record(err)
		return diskAccountCollectResult{failure: failure}, err
	}
	if len(instances) == 0 {
		return diskAccountCollectResult{noDiskInstances: true, failure: failure}, nil
	}

	// disk 有界并发采集(与 NAS 实例并发同模式):单 disk 失败记日志
	// + 计入失败明细，不中断其余 disk;任务取消不再派发。
	var (
		wg          sync.WaitGroup
		sem         = make(chan struct{}, diskMetricInstanceConcurrency)
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
			n := e.collectDiskMetrics(ctx, querier, account, inst, startDate, endDate, failure)
			if n < 0 {
				failedCount.Add(1)
				return
			}
			written.Add(int64(n))
		}(inst)
	}
	wg.Wait()
	return diskAccountCollectResult{
		written:     int(written.Load()),
		failedDisks: int(failedCount.Load()),
		failure:     failure,
	}, nil
}

// collectDiskMetrics 单 disk 采集 [startDate, endDate] 指标并按日期分流写库:
//   - Date == endDate(今日)行 → BulkInsertIfAbsent 首写生效(当日已有行不覆盖);
//   - 更早(昨日及以前)行 → BulkUpsertMetrics 覆盖更新(次日补采显式覆盖昨日行)。
//
// region 从实例 attributes["region"] 取(云硬盘是地域性资源，按实例真实 region
// 查询，不做全局 region 推断);AccountID/Provider 由执行器回填(querier 签名不
// 感知账号，唯一键必须含 account_id 支持跨账号共享盘);UsageScope/QcStatus 透传
// 适配器标注，zero_exception 打标由 DAO 写路径(diskMetricQC)统一落实。不做 CDN
// 「全零跳过」过滤:usage_percent=0 行照常入批。
//
// 返回已写条数；查询/写库失败记日志 + 计入失败明细，返回 -1 表示该 disk 失败
// (由调用方并发调度计数，不中断其余 disk)。
func (e *SyncDiskMetricsExecutor) collectDiskMetrics(
	ctx context.Context,
	querier cloudx.DiskMetricQuerier,
	account *domain.CloudAccount,
	inst camdomain.Instance,
	startDate, endDate string,
	failure *nasAccountFailure,
) int {
	diskID := inst.AssetID
	if diskID == "" {
		diskID = inst.AssetName
	}
	if diskID == "" {
		e.logger.Warn("Disk实例缺少 asset_id/asset_name,跳过采集",
			elog.Int64("account_id", account.ID),
			elog.Int64("instance_id", inst.ID))
		return 0
	}
	diskName := inst.AssetName
	region, _ := inst.Attributes["region"].(string)

	metrics, err := querier.GetDiskMetrics(ctx, diskID, diskName, region, startDate, endDate)
	if err != nil {
		e.logger.Error("查询Disk实例指标失败",
			elog.String("disk_id", diskID),
			elog.String("region", region),
			elog.Int64("account_id", account.ID),
			elog.FieldErr(err))
		failure.record(err)
		return -1
	}

	var todayRows, pastRows []types.DiskMetric
	for _, m := range metrics {
		if m.Date == "" {
			continue // 无日期的行无法定位唯一键,跳过
		}
		// AccountID/Provider 回填(querier 不感知账号;唯一键含 account_id);
		// UsageScope/QcStatus 透传适配器标注不篡改
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
			e.logger.Error("首写生效批量写入Disk指标失败",
				elog.String("disk_id", diskID),
				elog.Int("batch_size", len(todayRows)),
				elog.FieldErr(err))
			failure.record(err)
			return -1
		}
		written += len(todayRows)
	}
	if len(pastRows) > 0 {
		if err := e.metricDAO.BulkUpsertMetrics(ctx, pastRows); err != nil {
			e.logger.Error("覆盖更新批量写入Disk指标失败",
				elog.String("disk_id", diskID),
				elog.Int("batch_size", len(pastRows)),
				elog.FieldErr(err))
			failure.record(err)
			return -1
		}
		written += len(pastRows)
	}
	return written
}

// 编译期断言:执行器实现任务执行器接口
var _ taskx.TaskExecutor = (*SyncDiskMetricsExecutor)(nil)
