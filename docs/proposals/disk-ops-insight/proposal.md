---
created: "2026-09-21"
author: "Haven"
status: Completed
intent: "new-feature"
---

# Proposal: 多云云硬盘 Disk 经营洞察(使用率/IOPS/吞吐指标)

## Problem

多云平台已能枚举/同步 5 家厂商(aliyun/tencent/huawei/volcengine/aws)的云硬盘,但**没有磁盘水位与性能的经营视角**:运维无法看到磁盘使用率增长趋势、无法判断磁盘扩容时机、无法发现 IOPS/吞吐性能瓶颈。`DiskInstance` 虽有 `size`/`iops`/`throughput` 字段,但只是**枚举时的一次性快照**(写入 `ecam_instance.attributes`),无时序落库——看不到历史趋势、容量增长、性能退化,不足以支撑运营决策。

### Evidence

- Disk 抽屉(`DiskDetailDrawer.vue`)声明了「监控」tab(`name="monitor"`),但**无任何内容分支**——监控 tab 点击无响应(空壳),与 NAS 优化前完全同型。
- `DiskInstance` 结构体(`internal/shared/cloudx/types/disk.go`)已有 `Size`/`IOPS`/`Throughput`/`Status` 字段,这些是 `ListDisk` 枚举时写入 `attributes` 的单点快照;`ecam_instance` 无任何 Disk 指标 collection(对比 CDN 的 `ecam_cdn_metric`、NAS 的 `ecam_nas_metric`、OSS 的 `ecam_oss_metric`)。
- 后端无任何 Disk 指标读取接口(grep diskMetric / disk_metric 为空);无 `sync_disk_metrics` 执行器。
- 对标:CDN、NAS、OSS 已实现完整经营洞察(天粒度指标 + 前端趋势 + 运营卡),Disk 是缺经营视角的核心基础设施资产(云硬盘是计算域的基础,容量/性能故障直接导致业务中断)。
- **实盘 Disk 分布(证据缺口,探测任务补充)**:当前采样不足以支撑「必达 vs 尽力而为」分组——探测任务须统计各厂商 Disk 数量/容量占比,作为分组与覆盖承诺的证据。

### Urgency

- 云硬盘是容量与性能敏感资产:磁盘使用率 100% 直接导致业务写入失败(数据库/应用挂起);IOPS 打满导致性能急剧退化;磁盘扩容/治理滞后直接带来故障事故或成本浪费。
- **量化口径**:「无趋势视图 → 无法发现高水位/高 IO 磁盘 → 磁盘满故障或性能事故」的成本 ≈ 高水位磁盘数 × 故障影响时长 × 业务损失;上线前由探测任务顺带统计实盘 Disk 中 `used/size > 80%` 或 IOPS 超阈值的高危磁盘数作为近失证据。
- 缺口与 CDN/NAS/OSS 不齐:用户对存储类资产已有趋势视图,Disk 缺,产品能力不完整。
- 磁盘是计算实例的底层依赖,越晚建指标表,历史水位/性能基线缺失越多(逐日指标从启用日起才有)。

## Proposed Solution

对标 NAS 指标模式(地域性资源,已上线验证),为 Disk 新增使用率/IOPS/吞吐天粒度指标采集与展示。**关键差异:云硬盘是地域性资源**(disk_id 绑定 region),与 NAS 同型而非 OSS(全局服务)——因此 Querier 签名**带 region**,唯一键用 disk_id:

1. **DiskMetricQuerier 可选接口**(仿 `cloudx.NASMetricQuerier`,地域性资源故签名带 region):`GetDiskMetrics(ctx, diskID, diskName, region, startDate, endDate) ([]types.DiskMetric, error)`。DiskMetric 落库字段:`disk_id / date / usage_percent / iops / throughput / qc_status`;usage_percent 为磁盘使用率(0~100 或厂商百分比口径归一),iops/throughput 为当日均值。`DiskMetric` 唯一键 `(account_id, disk_id, date)`——disk_id 是地域内唯一标识,跨账号共享磁盘(共享盘)的隔离需求与 NAS 修复后的 `(account_id, fs_id, date)` 同构,直接复用多账号经验。
2. **5 厂商实现**:aliyun(CMS `acs_ecs_dashboard` 的 DiskUtilization/DiskReadBPS/DiskWriteBPS 等)、huawei(CES `SYS.ECS` 或云硬盘专属 namespace,经探测确认)、aws(CloudWatch `AWS/EBS` `VolumeReadBytes`/`VolumeWriteBytes`/`VolumeIdleTime`)、tencent(monitor `QCE/CVM` 或 CBS 专属)、volcengine(cloudmonitor,指标名经探测任务确认)。**必达项:aliyun/huawei/aws 三家**(标准指标,EBS `VolumeReadBytes` 是公认标准);**尽力而为项:tencent/volcengine**(指标可用性待探测)。任一适配器失败只返回自身空,不阻塞全流程,但须走「失败可观测性」路径。**必达厂商选择依据(非仅实现便利)**:分组由「指标标准化程度 × 实盘容量分布」双因素决定——探测任务统计实盘容量按厂商占比;若 tencent/volcengine 任一占比 >15%:探测可用则升格为必达项纳入本期,探测不可用则显式降级为「二期补」并在发布说明承诺二期补采窗口;占比 ≤15% 维持尽力而为并记录理由。
3. **Disk 指标采集执行器** `disk:collect_metrics`(仿 `sync_nas_metrics.go`):按**活跃账号**遍历 Disk 实例 → 调 querier → 写入 `ecam_disk_metric`。**「活跃账号」口径 = 租户下已纳管且存在 ≥1 个 Disk 实例的云账号**(以 `ecam_instance` 枚举为准),采集不依赖账号 EnableAutoSync 开关(与 NAS/CDN/OSS 一致,防非活跃账号下磁盘静默漏采)。同日行**首写生效**(补缺式 upsert,当日已有行不覆盖;仅保护今日行,昨日行由次日补采覆盖更新),与 NAS 同口径(状态型日快照)。账号级互斥 + disk 有界并发(复用 `nasAccountGate` 模式)。
4. **持久化日闸复用**:Disk 每日采集直接复用 NAS/OSS 已实现的 `scheduler_state` 持久化日闸(**disk 键**)+ 告警桥(SchedulerGateAlerter),原子认领/写失败退避重试/读失败 5 分钟退避/特性开关回滚(`SCHEDULER_PERSISTENT_GATE_ENABLED`)全部沿用——Disk 只需给日闸加 `resource_type=disk` 分支并注册采集任务,验证「一个日闸多资源复用」的扩展性(CDN/NAS/OSS/Disk 四资源并行)。
5. **Disk 指标读取接口**(契约,租户校验见 Non-Functional Requirements):`GET /assets/disk/metrics?disk_id=&account_id=&days=`(单盘趋势)与 `GET /assets/disk/top?account_id=&days=&sort=&top=&page=&page_size=`(账号视角 Top)。接口从鉴权上下文取 tenantID,服务端校验客户端传入的 `account_id` ∈ 该租户账号集合,越权返回 404(不泄露账号存在性);`days` 限 1~90;`sort` ∈ `usage_percent|iops|throughput`(用近 N 天均值口径);趋势与 Top 同时返回「最新一天」与「近 N 天均值」两类值。**qc_status 读取侧闭环**:写路径的 `qc_status=zero_exception`(usage_percent=0 异常行)在读取响应中原样暴露并映射进 `data_status`——前端可分辨「使用率 0 是异常」而非当正常空盘。
6. **前端**:Disk 抽屉「监控」tab 填趋势图(使用率折线 + IOPS/吞吐双轴,仿 NasDetailDrawer 双轴:容量柱 + 性能线);Disk 列表页顶部加运营卡(总容量/平均使用率/IO 繁忙盘数)。空态区分「无数据」与「采集失败/未启用」,采集异常时运营卡显示警示而非纯空(见「失败可观测性」)。**数据来源声明(统一展示口径)**:Disk 相关界面(列表页行内容量/使用率、运营卡、趋势图、Top)一律以 `ecam_disk_metric` 指标表为唯一数据来源;资产表 `ecam_instance` 的 size/iops/throughput 仅作枚举与元数据,不再在 Disk 界面展示其数值——避免「资产表快照 vs 指标表趋势」同屏矛盾;资产表字段本身修复不在本期范围(见 Out of Scope)。

### Innovation Highlights

- 直接平移已验证的 NAS 经营洞察模式(地域性资源带 region),是 CDN/NAS/OSS 后第四次平移,架构成熟。
- 复用 NAS/OSS 已落地的持久化日闸 + 原子认领,Disk 零新增调度机制——验证「一个日闸多资源复用」的扩展性(CDN/NAS/OSS/Disk 四资源)。
- 指标语义为状态型快照(使用率/IOPS/吞吐天粒度),比 NAS/OSS 的纯容量多出性能维度——覆盖「磁盘满」与「性能瓶颈」两类故障。

## Requirements Analysis

### Key Scenarios

- **Happy path**:每日调度器认领 disk 日闸 → 遍历租户活跃账号的 Disk → 调各厂商监控 API 取当日使用率/IOPS/吞吐 → 落库 `ecam_disk_metric`;用户打开 Disk 列表页看到运营卡,点开抽屉「监控」tab 看趋势。
- **多账号共享磁盘**:同一 disk_id 出现在多个账号(共享盘)——唯一键含 account_id 隔离,各账号各留一行,Top 按 disk_id 去重取代表行(日期 desc 再使用率 desc),不跨账号求和/双计。
- **首次认领过渡**:`scheduler_state` 无 disk 记录 → 视为首次认领,认领后触发一次当日提交(不回溯补采历史)。
- **探测不支持 vs 调用失败**:厂商无该指标(探测不支持)→ INFO + 空返回,不算失败;适配器调用失败 → ERROR + error 字段 + Result.failures 计数,不阻塞其他厂商。
- **qc_status 异常**:usage_percent=0(zero_exception)落库可见,读取时原样暴露;无挂载磁盘(available 状态)使用率无意义 → 按探测结论处理(可能为 null 或 0 打标)。
- **错误场景**:日闸写失败指数退避重试 + 升级告警;读失败 ≥5 分钟退避;特性开关可切回内存闸兜底。

### Non-Functional Requirements

- **租户隔离**:所有读取接口从鉴权上下文取 tenantID,校验 account_id ∈ 租户账号集合,越权返回 404(不泄露账号存在性)。
- **单位归一化**:usage_percent 统一百分比(0~100,厂商百分比口径归一);iops/throughput 保留原始单位(次/秒、MB/s 归一);容量字节 → GB 走共享 `types.BytesToGB`(如适用)。
- **可观测性**:失败计数入 `Result["failures"]`(provider/account/error_count/last_error);必达厂商近 3 天零成功写库 → 健康告警。
- **并发安全**:账号级互斥 + disk 有界并发(复用 `nasAccountGate` 模式)。
- **幂等**:唯一键 `(account_id, disk_id, date)` upsert;同日首写生效不覆盖。

### Constraints & Dependencies

- 5 厂商监控 SDK 已在 go.mod(cloudwatch/monitor/cms/ces/cloudmonitor),无新增重依赖。
- 厂商监控 namespace/指标名需探测任务确认(aliyun `acs_ecs_dashboard` 磁盘指标、华为云硬盘 namespace、AWS `AWS/EBS`、腾讯 CBS、火山 cloudmonitor)——M1 探测是发布 gate。
- 云硬盘是地域性资源:Querier 带 region,按实例真实 region 查询;不做全局 region 推断。
- 复用 NAS/OSS 持久化日闸:需给日闸加 `resource_type=disk` 分支,不改既有 nas/cdn/oss 键行为。

## Alternatives & Industry Benchmarking

### Industry Solutions

- 阿里云控制台云盘「监控」:按磁盘展示使用率/IOPS/吞吐趋势(分钟/天粒度),基于云监控 `acs_ecs_dashboard` 磁盘指标。
- AWS Console EBS「Monitoring」:`VolumeReadBytes`/`VolumeWriteBytes`/`VolumeIdleTime` 等,基于 CloudWatch。
- 华为云/腾讯云控制台磁盘监控:各基于自家监控服务的云硬盘级指标。
- Prometheus + 云厂商 exporter(如 node_exporter 挂载实例):从 OS 侧采集磁盘使用率/IO,但只能覆盖有 agent 的实例,非云盘级全量。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing | — | 零成本 | 无趋势视图,磁盘水位/性能不可见,故障滞后 | Rejected: 与 CDN/NAS/OSS 能力不齐,运营缺口真实 |
| 各厂商控制台看 | 阿里/AWS/华为/腾讯控制台 | 免费,厂商维护 | 跨厂商割裂,无统一视图,无租户聚合 | Rejected: 无法统一呈现,不解决多云聚合 |
| OS agent(node_exporter) | 社区 | 覆盖挂载实例 | 只覆盖有 agent 的实例,非云盘级全量,与既有架构不一致 | Rejected: 覆盖不全,偏离已验证模式 |
| **平移 NAS 指标模式** | 本仓库已上线实现 | 复用已验证架构,厂商 SDK 已在,日闸可复用 | 探测成本(命名空间需确认) | **Selected: 一致性最高,复用最大化** |

## Feasibility Assessment

### Technical Feasibility

- 5 厂商监控 SDK 已依赖;CDN/NAS/OSS 的完整管线(querier 接口 → 执行器 → DAO → 读取服务 → handler → 前端趋势)本仓库已实现三遍,Disk 是第四次平移,模式成熟。
- 唯一新增:Disk 专属 namespace 探测 + DiskMetric/DiskMetricDAO + disk 日闸分支 + Disk 前端监控 tab/运营卡。
- 风险点:磁盘使用率的厂商口径差异(阿里按云盘、AWS 无直接使用率需派生、华为按挂载实例)——探测任务须归一口径(详见 Key Risks)。

### Resource & Timeline

- 团队已连续完成 CDN/NAS/OSS 三套指标管线,技能完备;Disk 与前三者同型,预计与 NAS/OSS 一期工作量相当(~9 任务)。
- 风险点:厂商 namespace 实盘与文档差异(如 NAS 的 SFS_Turbo 实盘在 SYS.EFS)——探测任务是发布 gate,不可省。

### Dependency Readiness

- 厂商监控 API 对云硬盘的容量/性能指标普遍成熟(磁盘使用率/IO 是核心监控维度),无 SaaS 不稳定的硬风险。
- 探测任务须验证:aliyun `acs_ecs_dashboard` 磁盘指标/华为云硬盘 namespace/AWS `AWS/EBS`/腾讯 CBS/火山 namespace 的实盘可用性。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| 「Disk 需要经营洞察」 | 5 Whys | Confirmed: 磁盘使用率/IOPS 是容量与性能敏感运营指标;用户已确认核心诉求是容量+性能经营洞察(非仅前端) |
| 「Disk 与 NAS 同构,直接平移」 | Assumption Flip | Refined: Disk 是地域性资源(带 region)与 NAS 同型;但指标是使用率+IOPS+吞吐(性能维度),比 NAS 纯容量丰富;唯一键用 disk_id |
| 「5 厂商都要适配」 | Stress Test | Refined: 先探测再分组(必达/尽力而为),避免像 volcengine NAS 指标订阅未开通导致白做;占比 ≤15% 且探测不可用则降级二期 |
| 「磁盘使用率可直接采」 | Occam's Razor | Refined: 使用率口径厂商差异大(阿里云盘直接有/AWS 需派生/华为按实例),探测任务须归一口径,不可默认同构 |
| 「IOPS/吞吐值得采」 | XY Detection | Confirmed: 用户核心是容量+性能双视角,IOPS/吞吐是磁盘性能瓶颈的关键信号;时延留二期(口径差异最大) |

## Scope

### In Scope
- M1 探测任务:5 厂商 Disk 监控 namespace/指标名实盘验证 + 使用率口径确认(云盘 vs 挂载实例)+ 实盘 Disk 数/容量分布统计 + 高危磁盘数 → 定必达/尽力而为分组(发布 gate)
- `DiskMetric` 模型(`disk_id/date/usage_percent/iops/throughput/qc_status`)+ `DiskMetricQuerier` 可选接口(带 region)+ `ecam_disk_metric` DAO(唯一键 `(account_id, disk_id, date)` + 数量级门禁 + zero_exception)
- 必达厂商 Disk 适配器(aliyun/huawei/aws)
- 尽力而为厂商 Disk 适配器(tencent/volcengine,探测不可用则二期补)
- `disk:collect_metrics` 采集执行器(活跃账号遍历 + 首写生效 + 账号互斥 + 失败计数入 Result)
- `scheduler_state` 日闸 `disk` 键接入 + 采集任务注册(复用 T7 持久化日闸)
- Disk 读取接口:`GET /assets/disk/metrics`(单盘趋势)+ `GET /assets/disk/top`(账号 Top,租户校验 + 分页 + qc_status 闭环)
- 前端:Disk 抽屉「监控」tab 趋势图(使用率 + IOPS/吞吐双轴)+ 列表页运营卡(空态/警示/数据来源统一)

### Out of Scope
- 磁盘时延指标(读/写时延、P99)——留二期,本期聚焦使用率+IOPS+吞吐
- 性能基线/异常检测(自动识别磁盘性能退化)——二期项
- 容量告警/阈值规则(磁盘使用率超阈值触发告警)——二期项,本期仅健康告警(零成功)
- 快照/计费模型(磁盘快照成本)——不属于指标经营范畴
- 资产表 `ecam_instance` 的 size/iops/throughput 字段修复——仅作枚举快照,不在本期修
- 磁盘挂载关系优化(实例-磁盘拓扑)——不属于指标经营范畴

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| 厂商 namespace 实盘与文档不符(如 NAS 的 SFS_Turbo 实盘在 SYS.EFS) | H | H | M1 探测任务为发布 gate,实盘验证非零后才定 namespace;探测不可用厂商降级二期补 |
| 磁盘使用率口径差异大(阿里云盘/AWS 派生/华为实例级) | H | M | 探测任务确认各厂商使用率口径并归一口径(百分比);无直接使用率的厂商用 IO 时间派生或打标 |
| volcengine/tencent 指标订阅未开通 | M | M | 探测任务归因(订阅未开通 vs 指标不存在);订阅未开通记「二期补+开通路径」,不伪装数据 |
| AWS 无直接磁盘使用率(需从 VolumeIdleTime 派生) | M | M | 探测确认派生公式;派生不可靠则本期只采 IOPS/吞吐,使用率打标缺失 |
| 跨账号共享磁盘被误聚合 | L | H | 唯一键含 account_id;Top 按 disk_id 去重取代表行(日期 desc 再使用率 desc),不跨账号求和 |
| 日闸 disk 键接入破坏既有 nas/cdn/oss 键 | L | H | 复用 T7 日闸时按 resource_type 分键,回归测试验证既有键行为不变;特性开关回滚兜底 |
| 前端数据来源不统一(资产表快照 vs 指标表趋势) | M | M | SC 强制 Disk 界面一律以指标表为唯一来源,资产表仅作枚举 |

## Success Criteria

- [ ] M1 探测完成:5 厂商 Disk namespace/指标名实盘验证非零通过(必达三家 100% 有真实非零数据点;尽力而为两家记录原因与二期路径),磁盘使用率口径确认与归一,实盘分布统计(必达占比确认或升格/降级决策记录)
- [ ] `ecam_disk_metric` 唯一键 `(account_id, disk_id, date)` 生效:多账号同 disk_id 并存各留一行;同日幂等(首写生效不覆盖),次日补昨日覆盖更新
- [ ] `disk:collect_metrics` 执行器按活跃账号遍历采集,写库后实盘 Disk 每日 ≥1 行/盘;usage_percent/iops/throughput 单位归一,zero_exception 落库可见
- [ ] Disk 每日采集接入 `scheduler_state` disk 键:原子认领并发(多 goroutine 只一胜)、写失败退避重试+告警、读失败 ≥5 分钟退避;重启×3 各仅 1 条;特性开关切回内存闸后 NAS/CDN/OSS/Disk 调度仍可用
- [ ] `GET /assets/disk/metrics`(趋势)与 `GET /assets/disk/top`(Top):租户校验(tenant A 查不到 B 的磁盘,越权 404)、disk_id 去重、分页/limit(days 1~90, top 默认 10 最大 50)、qc_status 传递、缺失日 data_status 标注不填假值
- [ ] 前端 Disk 抽屉「监控」tab 趋势(使用率折线 + IOPS/吞吐双轴)+ 列表页运营卡(总容量/平均使用率/IO 繁忙盘数);空态区分「无数据」与「采集失败」;Disk 界面数据一律来自指标表(资产表快照不显示)
- [ ] 单测:DAO(唯一键隔离/幂等/门禁)、适配器(单位换算/失败路径三分)、执行器(账号遍历/首写生效/失败计数)、日闸 disk 键(并发认领/退避)、读取服务(租户隔离/去重/分页/qc_status)、前端(纯逻辑层);`go build ./...` + `go vet` 通过

## Next Steps

- Proceed to `/quick-tasks`(quick 模式,直接从 proposal 生成任务并执行,与 NAS/OSS 同管线)
