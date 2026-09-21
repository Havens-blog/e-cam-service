---
created: "2026-09-21"
author: "Haven"
status: Approved
intent: "new-feature"
---

# Proposal: 多云云数据库 RDS 经营洞察(CPU/内存/磁盘/连接数指标)

## Problem

多云平台已能枚举/同步 5 家厂商(aliyun/tencent/huawei/volcengine/aws)的云数据库 RDS,但**没有数据库负载与资源水位的经营视角**:运维无法看到 CPU/内存/磁盘使用率趋势、无法判断扩容/规格升级时机、无法发现连接数异常(连接泄露/耗尽)。`RDSInstance` 虽有 `cpu`/`memory`/`storage`/`max_iops` 字段,但只是**枚举时的一次性快照**(写入 `ecam_instance.attributes`),无时序落库——看不到历史趋势、负载增长、资源水位变化,不足以支撑运营决策。

### Evidence

- RDS 抽屉(`RdsDetailDrawer.vue`)声明了「监控」tab(`name="monitor"`),但**无任何内容分支**——监控 tab 点击无响应(空壳),与 NAS/Disk 优化前完全同型。
- `RDSInstance` 结构体(`internal/shared/cloudx/types/rds.go`)已有 `CPU`/`Memory`/`Storage`/`MaxIOPS`/`Engine` 字段,这些是 `ListRDS` 枚举时写入 `attributes` 的单点快照;`ecam_instance` 无任何 RDS 指标 collection(对比 CDN 的 `ecam_cdn_metric`、NAS 的 `ecam_nas_metric`、OSS 的 `ecam_oss_metric`、Disk 的 `ecam_disk_metric`)。
- 后端无任何 RDS 指标读取接口(grep rdsMetric / rds_metric 为空);无 `sync_rds_metrics` 执行器。
- 对标:CDN、NAS、OSS、Disk 已实现完整经营洞察(天粒度指标 + 前端趋势 + 运营卡),RDS 是缺经营视角的核心数据资产(数据库是业务的中枢,负载/容量故障直接导致业务中断)。
- **实盘 RDS 分布(证据缺口,探测任务补充)**:当前采样不足以支撑「必达 vs 尽力而为」分组——探测任务须统计各厂商 RDS 数量/规格分布,作为分组与覆盖承诺的证据。

### Urgency

- 云数据库是负载与容量敏感资产:CPU/内存 100% 直接导致业务卡顿/超时;磁盘使用率 100% 导致数据库只读/写入失败;连接数耗尽导致新连接被拒(业务雪崩)。
- **量化口径**:「无趋势视图 → 无法发现高负载/高水位数据库 → 数据库故障或性能事故」的成本 ≈ 高危实例数 × 故障影响时长 × 业务损失;上线前由探测任务顺带统计实盘 RDS 中 CPU/内存/磁盘使用率 > 80% 的高危实例数作为近失证据。
- 缺口与 CDN/NAS/OSS/Disk 不齐:用户对基础设施类资产已有趋势视图,RDS 缺,产品能力不完整。
- 数据库是核心数据资产,越晚建指标表,历史负载/水位基线缺失越多(逐日指标从启用日起才有)。

## Proposed Solution

对标 NAS/Disk 指标模式(地域性资源,已上线验证),为 RDS 新增 CPU/内存/磁盘使用率 + 连接数天粒度指标采集与展示。**关键差异:RDS 是地域性资源**(实例绑定 region),与 NAS/Disk 同型——Querier 签名**带 region**,唯一键用 rds 实例 ID:

1. **RDSMetricQuerier 可选接口**(仿 `cloudx.NASMetricQuerier`/`DiskMetricQuerier`,地域性资源故签名带 region):`GetRDSMetrics(ctx, rdsID, instanceName, region, engine, startDate, endDate) ([]types.RDSMetric, error)`。RDSMetric 落库字段:`rds_id / date / cpu_percent / memory_percent / disk_percent / connections / qc_status`;四个指标为状态型(使用率百分比 + 连接数绝对值)。`RDSMetric` 唯一键 `(account_id, rds_id, date)`——rds_id 是地域内唯一标识,跨账号共享实例的隔离需求与 NAS/Disk 修复后的多账号经验同构。
2. **5 厂商实现**:aliyun(CMS `acs_rds_dashboard` 或 RDS 专属指标)、huawei(CES `SYS.RDS`)、aws(CloudWatch `AWS/RDS` `CPUUtilization`/`FreeableMemory`/`FreeStorageSpace`/`DatabaseConnections`)、tencent(monitor `QCE/CDB`)、volcengine(cloudmonitor,指标名经探测任务确认)。**必达项:aliyun/huawei/aws 三家**(标准指标,AWS/RDS `CPUUtilization`/`DatabaseConnections` 是公认标准);**尽力而为项:tencent/volcengine**(指标可用性待探测)。任一适配器失败只返回自身空,不阻塞全流程,但须走「失败可观测性」路径。**必达厂商选择依据(非仅实现便利)**:分组由「指标标准化程度 × 实盘分布」双因素决定——探测任务统计实盘 RDS 按厂商占比;若 tencent/volcengine 任一占比 >15%:探测可用则升格为必达项纳入本期,探测不可用则显式降级为「二期补」并在发布说明承诺二期补采窗口;占比 ≤15% 维持尽力而为并记录理由。
3. **RDS 指标采集执行器** `rds:collect_metrics`(仿 `sync_disk_metrics.go`):按**活跃账号**遍历 RDS 实例 → 调 querier → 写入 `ecam_rds_metric`。**「活跃账号」口径 = 租户下已纳管且存在 ≥1 个 RDS 实例的云账号**(以 `ecam_instance` 枚举为准),采集不依赖账号 EnableAutoSync 开关(与 NAS/CDN/OSS/Disk 一致)。同日行**首写生效**(补缺式 upsert,当日已有行不覆盖;仅保护今日行,昨日行由次日补采覆盖更新),与 NAS/Disk 同口径(状态型日快照)。账号级互斥 + rds 有界并发(复用 `nasAccountGate` 模式)。多引擎(mysql/pg/mariadb/sqlserver)实例统一采集,engine 作为元数据透传不参与指标口径分支(若探测显示引擎间指标口径差异大,则在适配器内按 engine 分派,探测确认)。
4. **持久化日闸复用**:RDS 每日采集直接复用 NAS/OSS/Disk 已实现的 `scheduler_state` 持久化日闸(**rds 键**)+ 告警桥(SchedulerGateAlerter),原子认领/写失败退避重试/读失败 5 分钟退避/特性开关回滚(`SCHEDULER_PERSISTENT_GATE_ENABLED`)全部沿用——RDS 只需给日闸加 `resource_type=rds` 分支并注册采集任务,验证「一个日闸多资源复用」的扩展性(CDN/NAS/OSS/Disk/RDS 五资源并行)。
5. **RDS 指标读取接口**(契约,租户校验见 Non-Functional Requirements):`GET /assets/rds/metrics?rds_id=&account_id=&days=`(单实例趋势)与 `GET /assets/rds/top?account_id=&days=&sort=&top=&page=&page_size=`(账号视角 Top)。接口从鉴权上下文取 tenantID,服务端校验客户端传入的 `account_id` ∈ 该租户账号集合,越权返回 404(不泄露账号存在性);`days` 限 1~90;`sort` ∈ `cpu_percent|memory_percent|disk_percent|connections`(用近 N 天均值口径);趋势与 Top 同时返回「最新一天」与「近 N 天均值」两类值。**qc_status 读取侧闭环**:写路径的 `qc_status=zero_exception`(指标全 0 异常行)在读取响应中原样暴露并映射进 `data_status`——前端可分辨「全 0 是异常」而非当正常空负载。
6. **前端**:RDS 抽屉「监控」tab 填趋势图(CPU/内存/磁盘使用率 + 连接数多指标,仿 NasDetailDrawer 多轴);RDS 列表页顶部加运营卡(平均 CPU/内存/磁盘水位 + 高负载实例数)。空态区分「无数据」与「采集失败/未启用」,采集异常时运营卡显示警示而非纯空(见「失败可观测性」)。**数据来源声明(统一展示口径)**:RDS 相关界面(列表页行内容量/负载、运营卡、趋势图、Top)一律以 `ecam_rds_metric` 指标表为唯一数据来源;资产表 `ecam_instance` 的 cpu/memory/storage 仅作枚举与元数据,不再在 RDS 界面展示其数值——避免「资产表快照 vs 指标表趋势」同屏矛盾;资产表字段本身修复不在本期范围(见 Out of Scope)。

### Innovation Highlights

- 直接平移已验证的 NAS/Disk 经营洞察模式(地域性资源带 region),是 CDN/NAS/OSS/Disk 后第五次平移,架构成熟。
- 复用 NAS/OSS/Disk 已落地的持久化日闸 + 原子认领,RDS 零新增调度机制——验证「一个日闸多资源复用」的扩展性(CDN/NAS/OSS/Disk/RDS 五资源)。
- 指标语义为状态型快照(CPU/内存/磁盘使用率 + 连接数天粒度),比 Disk 多出「连接数」维度——覆盖「数据库过载」与「连接耗尽」两类故障。

## Requirements Analysis

### Key Scenarios

- **Happy path**:每日调度器认领 rds 日闸 → 遍历租户活跃账号的 RDS → 调各厂商监控 API 取当日 CPU/内存/磁盘/连接数 → 落库 `ecam_rds_metric`;用户打开 RDS 列表页看到运营卡,点开抽屉「监控」tab 看趋势。
- **多账号共享实例**:同一 rds_id 出现在多个账号(共享数据库)——唯一键含 account_id 隔离,各账号各留一行,Top 按 rds_id 去重取代表行(日期 desc 再 CPU desc),不跨账号求和/双计。
- **多引擎并存**:mysql/pg/mariadb/sqlserver 实例统一采集,engine 作为元数据;若探测显示引擎指标口径差异大,适配器按 engine 分派。
- **首次认领过渡**:`scheduler_state` 无 rds 记录 → 视为首次认领,认领后触发一次当日提交(不回溯补采历史)。
- **探测不支持 vs 调用失败**:厂商无该指标(探测不支持)→ INFO + 空返回,不算失败;适配器调用失败 → ERROR + error 字段 + Result.failures 计数,不阻塞其他厂商。
- **qc_status 异常**:四指标全 0(zero_exception)落库可见,读取时原样暴露;停用/重启中的实例指标可能为 0 → 探测确认处理方案。
- **错误场景**:日闸写失败指数退避重试 + 升级告警;读失败 ≥5 分钟退避;特性开关可切回内存闸兜底。

### Non-Functional Requirements

- **租户隔离**:所有读取接口从鉴权上下文取 tenantID,校验 account_id ∈ 租户账号集合,越权返回 404(不泄露账号存在性)。
- **单位归一化**:cpu_percent/memory_percent/disk_percent 统一百分比 0~100(厂商百分比口径归一);connections 保留绝对值(个)。内存厂商可能给 FreeableMemory(可用内存)→ 探测确认换算为使用率。
- **可观测性**:失败计数入 `Result["failures"]`(provider/account/error_count/last_error);必达厂商近 3 天零成功写库 → 健康告警。
- **并发安全**:账号级互斥 + rds 有界并发(复用 `nasAccountGate` 模式)。
- **幂等**:唯一键 `(account_id, rds_id, date)` upsert;同日首写生效不覆盖。

### Constraints & Dependencies

- 5 厂商监控 SDK 已在 go.mod(cloudwatch/monitor/cms/ces/cloudmonitor),无新增重依赖。
- 厂商监控 namespace/指标名需探测任务确认(aliyun `acs_rds_dashboard`、华为 `SYS.RDS`、AWS `AWS/RDS`、腾讯 `QCE/CDB`、火山 cloudmonitor)——M1 探测是发布 gate。
- 云数据库是地域性资源:Querier 带 region,按实例真实 region 查询;不做全局 region 推断。
- 多引擎(mysql/pg/mariadb/sqlserver):探测确认引擎间指标口径是否一致,若差异大按 engine 分派。
- 复用 NAS/OSS/Disk 持久化日闸:需给日闸加 `resource_type=rds` 分支,不改既有 nas/cdn/oss/disk 键行为。

## Alternatives & Industry Benchmarking

### Industry Solutions

- 阿里云控制台 RDS「监控」:按实例展示 CPU/内存/磁盘/连接数趋势,基于云监控 RDS 专属指标。
- AWS Console RDS「Monitoring」:`CPUUtilization`/`FreeableMemory`/`FreeStorageSpace`/`DatabaseConnections`,基于 CloudWatch。
- 华为云/腾讯云控制台 RDS 监控:各基于自家监控服务的数据库实例级指标。
- Prometheus + 数据库 exporter(mysqld_exporter 等):从实例内部采集指标,但只能覆盖有访问凭证的实例,非云托管全量。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing | — | 零成本 | 无趋势视图,数据库负载/水位不可见,故障滞后 | Rejected: 与 CDN/NAS/OSS/Disk 能力不齐,运营缺口真实 |
| 各厂商控制台看 | 阿里/AWS/华为/腾讯控制台 | 免费,厂商维护 | 跨厂商割裂,无统一视图,无租户聚合 | Rejected: 无法统一呈现,不解决多云聚合 |
| 数据库 exporter(mysqld_exporter) | 社区 | 覆盖实例内部 | 只覆盖有访问凭证的实例,需维护凭证/agent,与既有架构不一致 | Rejected: 覆盖不全,偏离已验证模式 |
| **平移 NAS/Disk 指标模式** | 本仓库已上线实现 | 复用已验证架构,厂商 SDK 已在,日闸可复用 | 探测成本(命名空间需确认) | **Selected: 一致性最高,复用最大化** |

## Feasibility Assessment

### Technical Feasibility

- 5 厂商监控 SDK 已依赖;CDN/NAS/OSS/Disk 的完整管线(querier 接口 → 执行器 → DAO → 读取服务 → handler → 前端趋势)本仓库已实现四遍,RDS 是第五次平移,模式成熟。
- 唯一新增:RDS 专属 namespace 探测 + RDSMetric/RDSMetricDAO + rds 日闸分支 + RDS 前端监控 tab/运营卡。
- 风险点:内存使用率需从 FreeableMemory 派生(各厂商口径不同,阿里直接给/AWS 给 FreeableMemory)、引擎间指标口径——探测任务须归一口径(详见 Key Risks)。

### Resource & Timeline

- 团队已连续完成 CDN/NAS/OSS/Disk 四套指标管线,技能完备;RDS 与前三者同型,预计与 Disk 一期工作量相当(~9 任务)。
- 风险点:厂商 namespace 实盘与文档差异(如 NAS 的 SFS_Turbo 实盘在 SYS.EFS)——探测任务是发布 gate,不可省。

### Dependency Readiness

- 厂商监控 API 对 RDS 的资源使用率/连接数指标普遍成熟(数据库负载是核心监控维度),无 SaaS 不稳定的硬风险。
- 探测任务须验证:aliyun `acs_rds_dashboard`/华为 `SYS.RDS`/AWS `AWS/RDS`/腾讯 `QCE/CDB`/火山 namespace 的实盘可用性。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| 「RDS 需要经营洞察」 | 5 Whys | Confirmed: CPU/内存/磁盘/连接数是数据库负载与容量敏感运营指标;用户已确认核心诉求是核心性能经营洞察(非深度诊断) |
| 「RDS 与 NAS/Disk 同构,直接平移」 | Assumption Flip | Refined: RDS 是地域性资源(带 region)与 NAS/Disk 同型;但指标是 CPU/内存/磁盘/连接数(多维度),比 Disk 的 3 指标更丰富;唯一键用 rds_id;多引擎需探测口径 |
| 「5 厂商都要适配」 | Stress Test | Refined: 先探测再分组(必达/尽力而为),避免像 volcengine NAS 指标订阅未开通导致白做;占比 ≤15% 且探测不可用则降级二期 |
| 「内存使用率可直接采」 | Occam's Razor | Refined: 内存厂商口径差异大(阿里直接给/AWS 给 FreeableMemory 需换算),探测任务须归一口径,不可默认同构 |
| 「连接数值得采」 | XY Detection | Confirmed: 连接数是数据库业务负载的关键信号(连接耗尽直接故障),厂商普遍提供(如 AWS DatabaseConnections) |

## Scope

### In Scope
- M1 探测任务:5 厂商 RDS 监控 namespace/指标名实盘验证 + CPU/内存/磁盘/连接数口径归一(内存从 FreeableMemory 换算方案)+ 多引擎口径确认 + 实盘 RDS 数/规格分布统计 + 高危实例数 → 定必达/尽力而为分组(发布 gate)
- `RDSMetric` 模型(`rds_id/date/cpu_percent/memory_percent/disk_percent/connections/qc_status`)+ `RDSMetricQuerier` 可选接口(带 region)+ `ecam_rds_metric` DAO(唯一键 `(account_id, rds_id, date)` + 0~100 门禁 + zero_exception)
- 必达厂商 RDS 适配器(aliyun/huawei/aws)
- 尽力而为厂商 RDS 适配器(tencent/volcengine,探测不可用则二期补)
- `rds:collect_metrics` 采集执行器(活跃账号遍历 + 首写生效 + 账号互斥 + 失败计数入 Result)
- `scheduler_state` 日闸 `rds` 键接入 + 采集任务注册(复用持久化日闸)
- RDS 读取接口:`GET /assets/rds/metrics`(单实例趋势)+ `GET /assets/rds/top`(账号 Top,租户校验 + 分页 + qc_status 闭环)
- 前端:RDS 抽屉「监控」tab 趋势图(CPU/内存/磁盘/连接数多指标)+ 列表页运营卡(空态/警示/数据来源统一)

### Out of Scope
- QPS/IOPS 吞吐型指标——留二期,本期聚焦状态型资源指标
- 慢查询/锁等待/事务延迟等深度数据库诊断——二期项(厂商口径差异最大)
- 性能基线/异常检测(自动识别数据库负载退化)——二期项
- 容量告警/阈值规则(资源使用率超阈值触发告警)——二期项,本期仅健康告警(零成功)
- 只读副本/主备切换拓扑监控——不属于指标经营范畴
- 资产表 `ecam_instance` 的 cpu/memory/storage 字段修复——仅作枚举快照,不在本期修

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| 厂商 namespace 实盘与文档不符(如 NAS 的 SFS_Turbo 实盘在 SYS.EFS) | H | H | M1 探测任务为发布 gate,实盘验证非零后才定 namespace;探测不可用厂商降级二期补 |
| 内存使用率口径差异大(阿里直接给/AWS FreeableMemory 需换算) | H | M | 探测任务确认各厂商内存口径并归一(使用率百分比);换算公式固化到适配器并单测覆盖 |
| 多引擎(mysql/pg/mariadb/sqlserver)指标口径差异 | M | M | 探测确认;差异大则适配器按 engine 分派,统一写 RDSMetric |
| volcengine/tencent 指标订阅未开通 | M | M | 探测任务归因(订阅未开通 vs 指标不存在);订阅未开通记「二期补+开通路径」,不伪装数据 |
| 停用/重启中实例指标全 0 被误判异常 | M | M | 探测确认处理方案;结合实例 Status 甄别(停用中非 zero_exception,打标 data_status) |
| 跨账号共享实例被误聚合 | L | H | 唯一键含 account_id;Top 按 rds_id 去重取代表行(日期 desc 再 CPU desc),不跨账号求和 |
| 日闸 rds 键接入破坏既有 nas/cdn/oss/disk 键 | L | H | 复用日闸时按 resource_type 分键,回归测试验证既有键行为不变;特性开关回滚兜底 |
| 前端数据来源不统一(资产表快照 vs 指标表趋势) | M | M | SC 强制 RDS 界面一律以指标表为唯一来源,资产表仅作枚举 |

## Success Criteria

- [ ] M1 探测完成:5 厂商 RDS namespace/指标名实盘验证非零通过(必达三家 100% 有真实非零数据点;尽力而为两家记录原因与二期路径),CPU/内存/磁盘/连接数口径归一(内存换算方案定案),多引擎口径确认,实盘分布统计(必达占比确认或升格/降级决策记录)
- [ ] `ecam_rds_metric` 唯一键 `(account_id, rds_id, date)` 生效:多账号同 rds_id 并存各留一行;同日幂等(首写生效不覆盖),次日补昨日覆盖更新
- [ ] `rds:collect_metrics` 执行器按活跃账号遍历采集,写库后实盘 RDS 每日 ≥1 行/实例;cpu/memory/disk_percent 归一 0~100、connections 绝对值,zero_exception 落库可见
- [ ] RDS 每日采集接入 `scheduler_state` rds 键:原子认领并发(多 goroutine 只一胜)、写失败退避重试+告警、读失败 ≥5 分钟退避;重启×3 各仅 1 条;特性开关切回内存闸后 NAS/CDN/OSS/Disk/RDS 调度仍可用
- [ ] `GET /assets/rds/metrics`(趋势)与 `GET /assets/rds/top`(Top):租户校验(tenant A 查不到 B 的实例,越权 404)、rds_id 去重、分页/limit(days 1~90, top 默认 10 最大 50)、qc_status 传递、缺失日 data_status 标注不填假值
- [ ] 前端 RDS 抽屉「监控」tab 趋势(CPU/内存/磁盘/连接数多指标)+ 列表页运营卡(平均水位/高负载实例数);空态区分「无数据」与「采集失败」;RDS 界面数据一律来自指标表(资产表快照不显示)
- [ ] 单测:DAO(唯一键隔离/幂等/门禁)、适配器(单位换算/失败路径三分/内存派生)、执行器(账号遍历/首写生效/失败计数)、日闸 rds 键(并发认领/退避)、读取服务(租户隔离/去重/分页/qc_status)、前端(纯逻辑层);`go build ./...` + `go vet` 通过

## Next Steps

- Proceed to `/quick-tasks`(quick 模式,直接从 proposal 生成任务并执行,与 NAS/OSS/Disk 同管线)
