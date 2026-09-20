---
created: "2026-09-20"
author: "Haven"
status: Completed
intent: "new-feature"
---

# Proposal: 多云对象存储 OSS 经营洞察(容量/对象数指标)

## Problem

多云平台已能枚举/同步 5 家厂商(aliyun/tencent/huawei/volcengine/aws)的 OSS 存储桶,但**没有容量与对象数的经营视角**:运维无法看到存储量增长趋势、无法判断存储治理/限流时机、无法按账号/厂商对比存储规模。`OSSBucket` 虽有 `storage_size`/`object_count`/分层大小字段,但只是**枚举时的一次性快照**(写入 `ecam_instance.attributes`),无时序落库——看不到历史趋势、跨月增长、异常突增,不足以支撑运营决策。

### Evidence

- `OSSBucket` 结构体(`internal/shared/cloudx/types/oss.go`)已有 `StorageSize`/`ObjectCount`/`StandardSize`/`IASize`/`ArchiveSize`/`ColdArchiveSize`,但这些是 `ListBuckets` 枚举时写入 `attributes` 的单点快照,`ecam_instance` 无任何 OSS 指标 collection(对比 CDN 的 `ecam_cdn_metric`、NAS 的 `ecam_nas_metric`)。
- OSS 抽屉(`OssDetailDrawer.vue`)有 6 个 tab(详情/文件列表/访问控制/生命周期/标签/操作日志),**无监控趋势 tab**——与 NAS 优化前的空壳状态同型。
- 后端无任何 OSS 指标读取接口(grep ossMetric / oss_metric 为空);无 `sync_oss_metrics` 执行器。
- 对标:CDN、NAS 已实现完整经营洞察(天粒度指标 + 前端趋势 + 运营卡),OSS 是缺经营视角的重点存储资产。
- **实盘 OSS 分布(证据缺口,探测任务补充)**:当前采样不足以支撑「必达 vs 尽力而为」分组——探测任务须统计各厂商 OSS bucket 数/容量占比,作为分组与覆盖承诺的证据(见「必达厂商选择依据」)。

### Urgency

- 对象存储是成本与容量敏感资产:存储量无限增长、分层(标准/低频/归档)配比失当直接带来成本浪费;容量突增可能是异常写入(日志/备份失控)。
- **量化口径**:「无趋势视图 → 无法发现存储量突增/高水位 bucket → 成本失控或容量告警滞后」的成本 ≈ 突增 bucket 数 × 超预期增量 × 存储单价 × 失控天数;上线前由探测任务顺带统计实盘 OSS 中近 30 天存储量增速 > X% 的高增长 bucket 数作为近失证据。
- 缺口与 CDN/NAS 不齐:用户对 CDN/NAS 已有趋势视图,OSS 缺,产品能力不完整。
- 存储量是计费基础,越晚建指标表,历史水位缺失越多(逐日指标从启用日起才有)。

## Proposed Solution

对标 CDN/NAS 指标模式(已上线验证),为 OSS 新增容量/对象数天粒度指标采集与展示。**关键差异:OSS 是全局服务**(`ListBuckets` 的 region 参数可选,各厂商实现均有 `defaultRegion` 回退),与 CDN 同型而非 NAS(地域性资源)——因此 Querier 签名**不带 region**,唯一键用 bucket_name(全局唯一)类比 CDN 的 domain:

1. **OSSMetricQuerier 可选接口**(仿 `cloudx.CDNMetricQuerier`,全局服务故签名无 region):`GetOSSMetrics(ctx, bucketName, startDate, endDate) ([]types.OSSMetric, error)`。OSS 落库字段:`bucket_name / date / storage_size(GB) / object_count / qc_status`;分层大小(Standard/IA/Archive/ColdArchive)**本期不采集**(见 Out of Scope)。`OSSMetric` 唯一键 `(account_id, bucket_name, date)`——bucket_name 全局唯一,跨账号共享同名 bucket 的隔离需求与 CDN 修复后的 `(account_id, domain, date)` 同构,直接复用多账号经验。
2. **5 厂商实现**:aliyun(CMS `acs_oss`<!-- 探测定案修正:acs_oss 实盘基本失效,现行可用为 acs_oss_dashboard,见 probe-report §1.1 与文末 Drift Verification -->)、huawei(CES `SYS.OBS`)、aws(CloudWatch `AWS/S3` `BucketSizeBytes`/`NumberOfObjects`)、tencent(monitor `QCE/COS`)、volcengine(cloudmonitor,指标名经探测任务确认)。**必达项:aliyun/huawei/aws 三家**(标准指标,S3 `BucketSizeBytes` 是公认标准指标);**尽力而为项:tencent/volcengine**(指标可用性待探测)。任一适配器失败只返回自身空,不阻塞全流程,但须走「失败可观测性」路径。**必达厂商选择依据(非仅实现便利)**:分组由「指标标准化程度 × 实盘容量分布」双因素决定——探测任务统计实盘容量按厂商占比;若 tencent/volcengine 任一占比 >15%:探测可用则升格为必达项纳入本期,探测不可用则显式降级为「二期补」并在发布说明承诺二期补采窗口;占比 ≤15% 维持尽力而为并记录理由。
3. **OSS 指标采集执行器** `oss:collect_metrics`(仿 `sync_nas_metrics.go`):按**活跃账号**遍历 OSS bucket → 调 querier → 写入 `ecam_oss_metric`。**「活跃账号」口径 = 租户下已纳管且存在 ≥1 个 OSS bucket 的云账号**(以 `ecam_instance` 枚举为准),采集不依赖账号 EnableAutoSync 开关(与 NAS/CDN 一致,防非活跃账号下 bucket 静默漏采)。同日行**首写生效**(补缺式 upsert,当日已有行不覆盖;仅保护今日行,昨日行由次日补采覆盖更新),与 NAS 同口径(状态型日快照)。**OBS 特殊项**:华为 OBS 的 bucket 级容量指标可能不区分 region(全局聚合),探测任务确认;若为全局聚合则同一 bucket 直接按账号查询即可。
4. **持久化日闸复用**:OSS 每日采集直接复用 NAS 已实现的 `scheduler_state` 持久化日闸(**oss 键**)+ 告警桥(SchedulerGateAlerter),原子认领/写失败退避重试/读失败 5 分钟退避/特性开关回滚(`SCHEDULER_PERSISTENT_GATE_ENABLED`)全部沿用——OSS 不需要另建机制,只需给日闸加一个 `resource_type=oss` 分支并注册采集任务。
5. **OSS 指标读取接口**(契约,租户校验见 Non-Functional Requirements):`GET /assets/oss/metrics?bucket_name=&account_id=&days=`(单 bucket 趋势)与 `GET /assets/oss/top?account_id=&days=&sort=&top=&page=&page_size=`(账号视角 Top)。接口从鉴权上下文取 tenantID,服务端校验客户端传入的 `account_id` ∈ 该租户账号集合,越权返回 404(不泄露账号存在性);`days` 限 1~90;`sort` ∈ `storage_size|object_count`(用近 N 天均值口径);趋势与 Top 同时返回「最新一天」与「近 N 天均值」两类值。**qc_status 读取侧闭环**:写路径的 `qc_status=zero_exception`(storage_size=0 异常行)在读取响应中原样暴露并映射进 `data_status`——前端可分辨「容量为 0 是异常」而非当正常空桶。
6. **前端**:OSS 抽屉「监控」tab 填容量/对象数趋势图(echarts,仿 NasDetailDrawer 双轴:容量柱 + 对象数线);OSS 列表页顶部加运营卡(总容量/对象数/近 7 天增速)。空态区分「无数据」与「采集失败/未启用」,采集异常时运营卡显示警示而非纯空(见「失败可观测性」)。**数据来源声明(统一展示口径)**:OSS 相关界面(列表页行内容量/对象数、运营卡、趋势图、Top)一律以 `ecam_oss_metric` 指标表为唯一数据来源;资产表 `ecam_instance` 的 storage_size/object_count 仅作 bucket 枚举与元数据,不再在 OSS 界面展示其容量数值——避免「资产表快照 vs 指标表趋势」同屏矛盾;资产表字段本身修复不在本期范围(见 Out of Scope)。

### Innovation Highlights

- 直接平移已验证的 CDN/NAS 经营洞察模式(带 region 的 NAS → 无 region 的 OSS,签名简化),不发明新架构。
- 复用 NAS 已落地的持久化日闸 + 原子认领,OSS 零新增调度机制——验证「一个日闸多资源复用」的扩展性。
- 指标语义为状态型快照(容量/对象数天粒度),与 NAS 同型;对象数提供「数据规模」第二维度。

## Requirements Analysis

### Key Scenarios

- **Happy path**:每日调度器认领 oss 日闸 → 遍历租户活跃账号的 bucket → 调各厂商监控 API 取当日容量/对象数 → 落库 `ecam_oss_metric`;用户打开 OSS 列表页看到运营卡,点开抽屉「监控」tab 看趋势。
- **多账号同 bucket 并存**:同一 bucket_name 出现在多个账号(跨账号共享存储)——唯一键含 account_id 隔离,各账号各留一行,Top 按 bucket_name 去重取代表行(日期 desc 再容量 desc),不跨账号求和/双计。
- **首次认领过渡**:`scheduler_state` 无 oss 记录 → 视为首次认领,认领后触发一次当日提交(不回溯补采历史)。
- **探测不支持 vs 调用失败**:厂商无该指标(探测不支持)→ INFO + 空返回,不算失败;适配器调用失败 → ERROR + error 字段 + Result.failures 计数,不阻塞其他厂商。
- **qc_status 异常**:storage_size=0(zero_exception)落库可见,读取时原样暴露;used/capacity 边界(容量为 0 时使用率派生为 null 不 panic)。
- **错误场景**:日闸写失败指数退避重试 + 升级告警;读失败 ≥5 分钟退避;特性开关可切回内存闸兜底。

### Non-Functional Requirements

- **租户隔离**:所有读取接口从鉴权上下文取 tenantID,校验 account_id ∈ 租户账号集合,越权返回 404(不泄露账号存在性)。
- **单位归一化**:所有厂商容量字节 → GB(`types.BytesToGB` 共享函数,数量级自检 [1MB, 1PB] 门禁,零值放行打 zero_exception、非零越界拒绝)。
- **可观测性**:失败计数入 `Result["failures"]`(provider/account/error_count/last_error);必达厂商近 3 天零成功写库 → 健康告警。
- **并发安全**:账号级互斥 + bucket 有界并发(复用 NAS 的 `nasAccountGate` 模式,OSS 版或直接泛化复用)。
- **幂等**:唯一键 `(account_id, bucket_name, date)` upsert;同日首写生效不覆盖。

### Constraints & Dependencies

- 5 厂商监控 SDK 已在 go.mod(cloudwatch/monitor/cms/ces/cloudmonitor),无新增重依赖。
- 厂商监控 namespace/指标名需探测任务确认(aliyun `acs_oss`<!-- 探测定案修正为 `acs_oss_dashboard`,见文末 Drift Verification -->、华为 `SYS.OBS`、AWS `AWS/S3` `BucketSizeBytes`、腾讯 `QCE/COS`、火山 cloudmonitor)——M1 探测是发布 gate。
- OSS 是全局服务,但 bucket 有 region 属性;部分厂商容量指标可能全局聚合(OBS),探测确认。
- 复用 NAS 持久化日闸:需给日闸加 `resource_type=oss` 分支,不改既有 nas/cdn 键行为。

## Alternatives & Industry Benchmarking

### Industry Solutions

- 阿里云控制台 OSS「用量统计」:按 bucket 展示存储量趋势(天粒度),基于云监控 `acs_oss` 指标。
- AWS Console S3「Metrics」:`BucketSizeBytes`/`NumberOfObjects` 天粒度,基于 CloudWatch。
- 华为 OBS「监控」/腾讯 COS「监控」:各基于自家监控服务的 bucket 级容量指标。
- Prometheus + 云厂商 exporter(如 aliyun-exporter):把云监控指标拉成时序,可做存储量看板,但需自建采集调度与租户隔离。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing | — | 零成本 | 无趋势视图,存储量/对象数增长不可见,治理滞后 | Rejected: 与 CDN/NAS 能力不齐,运营缺口真实 |
| 各厂商控制台看 | 阿里/AWS/华为/腾讯控制台 | 免费,厂商维护 | 跨厂商割裂,无统一视图,无租户聚合 | Rejected: 无法统一呈现,不解决多云聚合 |
| Prometheus exporter | aliyun-exporter 等 | 开源,时序标准 | 需自建调度/租户隔离/多厂商适配,与既有 CDN/NAS 架构不一致 | Rejected: 重复造轮子,偏离已验证模式 |
| **平移 CDN/NAS 指标模式** | 本仓库已上线实现 | 复用已验证架构,厂商 SDK 已在,日闸可复用 | 探测成本(命名空间需确认) | **Selected: 一致性最高,复用最大化** |

## Feasibility Assessment

### Technical Feasibility

- 5 厂商监控 SDK 已依赖;CDN/NAS 的完整管线(querier 接口 → 执行器 → DAO → 读取服务 → handler → 前端趋势)本仓库已实现两遍,OSS 是第三次平移,模式成熟。
- 唯一新增:OSS 专属 namespace 探测 + OSSMetric/OSSMetricDAO + oss 日闸分支 + OSS 前端 tab/运营卡。

### Resource & Timeline

- 团队已连续完成 CDN/NAS 两套指标管线,技能完备;OSS 与前两者同型,预计与 NAS 一期工作量相当(~8 任务)。
- 风险点:厂商 namespace 实盘与文档差异(如 NAS 的 SFS_Turbo 实盘在 SYS.EFS)——探测任务是发布 gate,不可省。

### Dependency Readiness

- 厂商监控 API 对 OSS 的容量指标普遍成熟(存储量是 OSS 核心计费维度),无 SaaS 不稳定的硬风险。
- 探测任务须验证:aliyun `acs_oss`/华为 `SYS.OBS`/AWS `BucketSizeBytes`/腾讯 `QCE/COS`/火山 namespace 的实盘可用性。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| 「OSS 需要经营洞察」 | 5 Whys | Confirmed: 存储量/对象数增长是成本与容量敏感运营指标;用户已确认核心诉求是容量经营洞察(非成本监控、非仅前端) |
| 「OSS 与 NAS 同构,直接平移」 | Assumption Flip | Refined: OSS 是全局服务(无 region),Querier 签名比 NAS 简单;唯一键用 bucket_name 而非 fs_id;分层大小本期不做 |
| 「5 厂商都要适配」 | Stress Test | Refined: 先探测再分组(必达/尽力而为),避免像 volcengine NAS 指标订阅未开通导致白做;占比 ≤15% 且探测不可用则降级二期 |
| 「对象数指标值得采」 | Occam's Razor | Confirmed: 对象数是存储规模的第二维度,厂商普遍提供(如 AWS NumberOfObjects),成本低价值明确 |

## Scope

### In Scope
- M1 探测任务:5 厂商 OSS 监控 namespace/指标名实盘验证 + 实盘 bucket 数/容量分布统计 + 高增长 bucket 数 → 定必达/尽力而为分组(发布 gate)
- `OSSMetric` 模型(`bucket_name/date/storage_size/object_count/qc_status`)+ `OSSMetricQuerier` 可选接口(无 region)+ `ecam_oss_metric` DAO(唯一键 `(account_id, bucket_name, date)` + [1MB,1PB] 门禁 + zero_exception)
- 必达厂商 OSS 适配器(aliyun/huawei/aws)
- 尽力而为厂商 OSS 适配器(tencent/volcengine,探测不可用则二期补)
- `oss:collect_metrics` 采集执行器(活跃账号遍历 + 首写生效 + 账号互斥 + 失败计数入 Result)
- `scheduler_state` 日闸 `oss` 键接入 + 采集任务注册(复用 T7 持久化日闸)
- OSS 读取接口:`GET /assets/oss/metrics`(单 bucket 趋势)+ `GET /assets/oss/top`(账号 Top,租户校验 + 分页 + qc_status 闭环)
- 前端:OSS 抽屉「监控」tab 双轴趋势 + 列表页运营卡(空态/警示/数据来源统一)

### Out of Scope
- 存储分层大小(Standard/IA/Archive/ColdArchive)采集——留二期,本期聚焦总容量+对象数
- 成本估算/计费模型(存储成本月费用)——用户已确认核心诉求是容量经营洞察,成本监控单独立项
- 跨 region 聚合视图(OSS bucket 的 region 维度聚合排名)——本期按 bucket_name 全局,不做 region 下钻
- 容量告警/阈值规则(存储量超过阈值触发告警)——二期项,本期仅健康告警(零成功)
- 资产表 `ecam_instance` 的 storage_size/object_count 字段修复——仅作枚举快照,不在本期修
- 生命周期/版本控制/访问控制等配置类优化——不属于指标经营范畴

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| 厂商 namespace 实盘与文档不符(如 NAS 的 SFS_Turbo 实盘在 SYS.EFS) | H | H | M1 探测任务为发布 gate,实盘验证非零后才定 namespace;探测不可用厂商降级二期补 |
| volcengine/tencent 指标订阅未开通 | M | M | 探测任务归因(订阅未开通 vs 指标不存在);订阅未开通记「二期补+开通路径」,不伪装数据 |
| OSS 容量指标全局聚合(OBS)导致 region 语义不明 | M | M | 探测确认聚合口径;全局聚合则按账号直查,不做 region 推断 |
| 跨账号同 bucket 名并存被误聚合 | L | H | 唯一键含 account_id;Top 按 bucket_name 去重取代表行(日期 desc 再容量 desc),不跨账号求和 |
| 日闸 oss 键接入破坏既有 nas/cdn 键 | L | H | 复用 T7 日闸时按 resource_type 分键,回归测试验证 nas/cdn 行为不变;特性开关回滚兜底 |
| 前端数据来源不统一(资产表快照 vs 指标表趋势) | M | M | SC 强制 OSS 界面一律以指标表为唯一来源,资产表仅作枚举 |

## Success Criteria

- [ ] M1 探测完成:5 厂商 OSS namespace/指标名实盘验证非零通过(必达三家 100% 有真实非零数据点;尽力而为两家记录原因与二期路径),实盘 bucket 分布统计(必达占比确认或升格/降级决策记录)
- [ ] `ecam_oss_metric` 唯一键 `(account_id, bucket_name, date)` 生效:三账号同 bucket 名同日各留一行;同日幂等(首写生效不覆盖),次日补昨日覆盖更新
- [ ] `oss:collect_metrics` 执行器按活跃账号遍历采集,写库后实盘 OSS 每日 ≥1 行/bucket;容量/对象数单位归一化 GB/个,zero_exception 落库可见
- [ ] OSS 每日采集接入 `scheduler_state` oss 键:原子认领并发(多 goroutine 只一胜)、写失败退避重试+告警、读失败 ≥5 分钟退避;重启×3 各仅 1 条;特性开关切回内存闸后 NAS/CDN/OSS 调度仍可用
- [ ] `GET /assets/oss/metrics`(趋势)与 `GET /assets/oss/top`(Top):租户校验(tenant A 查不到 B 的 bucket,越权 404)、bucket_name 去重、分页/limit(days 1~90, top 默认 10 最大 50)、qc_status 传递、缺失日 data_status 标注不填假值
- [ ] 前端 OSS 抽屉「监控」tab 双轴趋势(容量柱 + 对象数线)+ 列表页运营卡(总容量/对象数/近 7 天增速);空态区分「无数据」与「采集失败」;OSS 界面数据一律来自指标表(资产表快照不显示)
- [ ] 单测:DAO(唯一键隔离/幂等/门禁)、适配器(单位换算/失败路径三分)、执行器(账号遍历/首写生效/失败计数)、日闸 oss 键(并发认领/退避)、读取服务(租户隔离/去重/分页/qc_status)、前端(nasMetrics 同型纯逻辑层);`go build ./...` + `go vet` 通过

## Next Steps

- Proceed to `/quick-tasks`(quick 模式,直接从 proposal 生成任务并执行,与 NAS 同管线)

## Drift Verification (2026-09-20, T-quick-doc-drift)

对照实际实现(commits)与测试结果逐项核对 Success Criteria,结论:**1 处文本级漂移已标注(见上方两处 SC/约束内联注),其余全项一致、无夸大**。

| SC 项 | 实测/实况 | 结论 |
|---|---|---|
| M1 探测 + 分组决策 | probe-report §1/§3:必达三家实盘非零全过(aliyun PASS 6/6、huawei 130 bucket capacity_total 与 GetBucketStat 逐字节互证、aws 通过);tencent 探测可用但占比 ≤15% 维持尽力而为;volcengine 占比 15.58% 贴线超阈但探测不可用(订阅未开通,289 组合全 not found + 阳性对照同败)→ 按规则显式降级「二期补」 | 一致(升格/降级决策均已记录) |
| namespace 文本口径 | 实现定案 aliyun = **`acs_oss_dashboard`**(proposal 假设的 `acs_oss` 实盘基本失效,ProbeReport §1.1);huawei `SYS.OBS`/`capacity_total`/`object_num_all` 与 tencent `QCE/COS`、aws `AWS/S3` 均按探测定案落地(aliyun/oss_metrics.go、huawei/obs_metrics.go 锚点) | **漂移,已标注**(探测 gate 预期内的修正,NAS SFS_Turbo→SYS.EFS 同型) |
| 唯一键 + 幂等 | DAO (account_id, bucket_name, date) 唯一索引;今日行首写生效($setOnInsert)/昨日行覆盖更新;multi-account-shared-bucket 14 测试覆盖跨账号隔离 | 一致 |
| 采集执行器 + 活跃账号 | `sync_oss_metrics.go`(oss:collect_metrics)+ auto_sync_oss_metrics.go 按 `ecam_instance` 枚举活跃账号遍历,不依赖 EnableAutoSync;Result["failures"] 计数 | 一致 |
| 持久化日闸 oss 键 | daily_gate.go `GateResourceOSS = "oss"` 分键(nas/cdn/oss 互不覆盖);run-test 记录并发认领/退避/重启×3 各 1 条/特性开关回滚全覆盖 | 一致 |
| 读取接口契约 | GET /assets/oss/metrics + /assets/oss/top;租户校验越权 404(ErrOSSAccountNotInTenant,handler 映射 404 不泄露存在性);days 1~90、top 默认 10 最大 50(parseNASBound);sort storage_size\|object_count 近 N 天均值口径;bucket_name 去重;缺失日 data_status=missing;qc_status 原样暴露映射 data_status | 一致 |
| 前端 | e-cam-web `026c02f`:OssDetailDrawer 监控 tab 双轴趋势 + 列表页运营卡;数据来源统一 ecam_oss_metric 指标表(资产表快照不展示);ossMetrics.ts 纯逻辑层 + ossMetrics.test.ts | 一致 |
| 健康监控 | oss_health_monitor.go:必达厂商连续 3 天零成功且实盘存在 bucket → AlertOSSZeroSuccess;tencent/volcano 尽力而为不参与 | 一致 |
| 测试全绿 | 110/110(6 journey 套件,含 2 例 MONGO_DSN 门控 LiveDAO)+ DAO Live 5/5 互证(tests/results/latest.md,run-test 记录) | 一致 |
| Out of Scope 未混入 | 分层大小不落库(types/oss.go OSSMetric 注释明示);无成本估算/region 下钻/容量阈值告警;资产表快照字段未修 | 一致 |
| volcengine 二期补承诺 | 探测不可用降级已固化:probe-report §1.5 + volcano TOSAdapter 桩注释(空切片+nil+INFO,重试路径三步);无独立发布说明文档(特性未发布,发布说明届时引用 probe-report §1.5) | 一致(承诺载体已持久化,发布时须引用) |

其余核对:Innovation/Alternatives 与实现相符(平移 CDN/NAS 模式,Querier 无 region、唯一键 bucket_name);`types.MBToGB` 为 tencent MB 口径新增共享辅助(规格「BytesToGB 共享函数」的扩展,非偏离)。

本次为 quick 模式特性,docs/business-rules/ 与 docs/conventions/ 项目级 spec 目录不存在,项目级 spec 无漂移对象;git diff main...HEAD 为空(本特性 commits 已落 main,核对以任务 records/代码锚点为准)。
