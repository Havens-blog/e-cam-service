---
created: "2026-09-17"
author: "Haven"
status: Draft
intent: "new-feature"
---

# Proposal: 多云文件存储 NAS 经营洞察(容量/使用率指标)

## Problem

多云平台已能枚举/同步 5 家厂商(aliyun/tencent/huawei/volcengine/aws)的 NAS 文件系统,但**没有容量与使用率的经营视角**:运维无法看到存储水位趋势、无法判断扩容时机、无法按账号/厂商对比存储利用率。资产表虽有 capacity/used_capacity 字段,但实盘数据质量差(华为/AWS capacity=0、aliyun used_capacity=1 明显未正确映射),不足以支撑任何运营决策。

### Evidence

- `ecam_instance` 实盘 NAS 采样(2026-09-17):huawei `capacity=0, used_capacity=0`,aws `capacity=0, used_capacity=0`,aliyun `capacity=10485760, used_capacity=1`(used 明显失真)。仅 volcengine 填充真实(cap=10TB, used=30GB)。
- **实盘容量按厂商分布(证据缺口,探测任务补充)**:当前采样不足以支撑「必达 vs 尽力而为」分组——若租户实际容量集中在 tencent/volcengine,3/5 覆盖将错过真实资产。探测任务须统计各厂商 NAS 实例数/容量占比,作为分组与覆盖承诺的证据(见「必达厂商选择依据」)。
- NAS 抽屉(`NasDetailDrawer.vue`)声明了 5 个 tab,但 `monitor`(监控)与 `log`(操作日志)两个 tab **无任何内容分支**——监控 tab 点击无响应(空壳)。
- 后端无任何 NAS 指标读取接口(grep NASMetric / nas*metric 为空);无 `sync_nas_metrics` 执行器。
- 对标:CDN 已实现完整经营洞察(成本归因 + 天粒度指标 + 前端趋势),NAS 是唯一缺经营视角的重点资产。

### Urgency

- 存储是成本与容量敏感资产:NAS 扩容/治理滞后直接带来成本浪费或容量瓶颈事故。
- **量化口径**:「无趋势视图 → 无法发现高水位实例 → 扩容滞后」的成本 ≈ 高水位实例数 × 超阈值冗余容量 × 容量单价 × 滞后天数;上线前由探测任务顺带统计实盘 NAS 中 `used/capacity > 70%` 的高水位实例数作为近失证据——该数字即为「越晚修」的量化成本下限。
- 缺口与 CDN 不齐:用户对 CDN 已有趋势视图,NAS 缺,产品能力不完整。
- 华为/AWS 容量字段为空是现成的数据质量 bug,越晚修,历史水位缺失越多(逐日指标从启用日起才有)。

## Proposed Solution

对标 CDN 指标模式(已上线验证),为 NAS 新增容量/使用率天粒度指标采集与展示:

1. **NASMetricQuerier 可选接口**(仿 `cloudx.CDNMetricQuerier`,但**带 region**——NAS 是地域性资源,CDN 为全局服务故 CDN 签名无 region):`GetNASMetrics(ctx, fsID, fsName, region, startDate, endDate) ([]types.NASMetric, error)`。`region` 解析:由 `ecam_instance` 实例元数据取该 fs 的 region(各厂商枚举实例时已带 region),适配器按**实例所在 region** 调用对应监控 API(华为 CES 用实例真实 region,aliyun/tencent/volcengine/aws 同按实例 region 查询);对多 region 账号按「实例 → region」逐实例查询,**不做全局 region 推断**,避免非默认 region 的实例查错地域。NASMetric **落库字段**:`fs_id / date / capacity(GB) / used_capacity(GB) / qc_status`;**utilization 不落库**,读取时由 capacity/used 派生(单位与边界语义见「单位归一化与字段语义」)。
2. **5 厂商实现**:aliyun(CMS DescribeMetricList)、tencent(monitor 子包,需新增依赖)、huawei(CES BatchListMetricData,按文件系统类型分别用 `SYS.SFS`/`SYS.SFS_Turbo`)、volcengine(cloudmonitor GetMetricData,指标名经探测任务确认)、aws(CloudWatch EFS `StorageBytes`,新增一行 go.mod)。**必达项:aliyun/huawei/aws 三家**(均为标准指标,SDK 已在或轻量引入;aws 的 EFS StorageBytes 有标准指标、并非不可行,不套用 CloudFront「主动放弃」先例);**尽力而为项:tencent/volcengine**(指标可用性待探测)。任一适配器失败只返回自身空,不阻塞全流程,但须走「失败可观测性」路径(见对应章节)。**必达厂商选择依据(非仅实现便利)**:分组由「指标标准化程度 × 实盘容量分布」双因素决定——探测任务统计实盘容量按厂商占比;若 tencent/volcengine 任一占比 >15%:探测可用则升格为必达项纳入本期,探测不可用则显式降级为「二期补」并在发布说明承诺二期补采窗口;占比 ≤15% 维持尽力而为并记录理由。
3. **NAS 指标采集执行器** `nas:collect_metrics`(仿 sync_cdn_metrics.go):按**活跃账号**遍历 NAS 实例 → 调 querier → 写入 `ecam_nas_metric`(唯一键 `(account_id, fs_id, date)`,复用 CDN 修复后的多账号经验)。**「活跃账号」口径 = 租户下已纳管且存在 ≥1 个 NAS 实例的云账号**(以 `ecam_instance` 枚举为准);采集遍历**不依赖账号的 EnableAutoSync 开关**——与 CDN 一致(指标采集对已纳管账号统一执行,不因账号禁用自动同步而跳过),避免「非活跃(未启用自动同步)账号下 NAS 实例静默漏采」。同日行**首写生效**(补缺式 upsert,当日已有行不覆盖;**仅保护今日行**,昨日行由次日补采覆盖更新),与 CDN「同日重采覆盖为最新值」不同——理由见「日快照取值口径与采集窗口」。
4. **持久化日闸 + 原子认领**(本计划的改进点):CDN 现用内存 `lastMetricsCollectDate`,今天实测发现服务重启会重复提交 25+ 条待办指标任务。NAS 采用持久化日闸(记录最近触发日期到 MongoDB,如 `scheduler_state` collection),并**顺带把 CDN 的也改为持久化**——一次解决同类问题。语义:
   - **原子认领**:一次 `findOneAndUpdate`(条件 `resource_type=nas AND last_date<today`,更新为 today)原子认领当日,认领成功才提交采集任务——多副本同时触发、或手动 `nas:collect_metrics` 与每日自动任务重叠时,同一资源只被一个实例认领(对 CloudWatch 这类有 GetMetricData 配额上限的 API 尤其必要)。
   - **写失败降级**:日闸写入失败走指数退避重试并升级告警,**不是**仅记日志——否则「写失败 + 重启」会让本计划要修的重启重复提交缺陷原样回归。
   - **读失败退避**:日闸读失败设置最短退避窗口(如 5 分钟)再重读,防止挂在分钟级调度循环上逐分钟洪泛任务队列。
      - **CDN 迁移回归与首部署过渡**:迁移后新增「CDN 日值正确性回归」——对比迁移前后同域同日值(以迁移前内存闸期间已落库的 CDN 日值为基准),验证持久化日闸首日不重采、不丢历史缺口、仍按 `days=2` 语义正确写入。首部署时 `scheduler_state` 尚无 cdn 记录 → `last_date` 为空 → **过渡行为定义**:视为首次认领,认领后触发一次当日提交(不回溯补采历史,既有 CDN 历史由既有数据延续),此后按日闸「每日一次」语义运行。
      - **特性开关与回滚**:持久化日闸以特性开关(如 `SCHEDULER_PERSISTENT_GATE_ENABLED`)控制,默认开启;若 mongo 日闸出现不可恢复故障,一键切回 CDN/NAS 原内存闸(接受重启重复提交旧缺陷换取调度器可用),回滚后记录原因与重新开启计划。回滚验证步骤纳入 SC:开关切回内存闸后调度任务仍可正常提交、NAS/CDN 指标采集不中断。
5. **NAS 指标读取接口**(契约,租户校验见 Non-Functional Requirements):`GET /assets/nas/metrics?fs_id=&account_id=&days=`(单实例趋势)与 `GET /assets/nas/top?account_id=&days=&sort=&top=&page=&page_size=`(账号视角 Top)。接口从鉴权上下文取 tenantID,服务端校验客户端传入的 `account_id` ∈ 该租户账号集合,越权返回 404(不泄露账号存在性);`days` 限 1~90(回看天数);`sort` ∈ `capacity|utilization`(utilization 用近 N 天均值口径);趋势与 Top 同时返回「最新一天」与「近 N 天均值」两类值。**Top 端点契约**:`top=N`(默认 10,最大 50,按 `sort` 降序取前 N)、分页 `page`(默认 1)/`page_size`(默认 10,最大 50);响应结构 `{ total, page, page_size, items[] }`,`items[]` 每条含 `fs_id / fs_name / account_id 列表 / data_status / qc_status / 最新一天 capacity·used·utilization / 近 N 天均值 capacity·used·utilization`;趋势端点响应 `{ fs_id, days[] }`,`days[]` 按日期升序,每项 `date / capacity / used / utilization / data_status / qc_status`,缺失日以 `data_status` 标注、不填充假值。**qc_status 读取侧闭环**:写路径的 `qc_status=zero_exception`(capacity=0 异常行)在读取响应中原样暴露,并映射进 `data_status`(`data_status=zero_exception`)——使「capacity=0 是异常」在运营视图可分辨,SC-5「落库可见」从 DB 层延伸到读取层;前端据此渲染警示/异常标记,而非当作正常零容量。
6. **前端**:NAS 抽屉「监控」tab 填容量/已用/使用率趋势图(echarts,仿 CdnDetailDrawer);NAS 列表页顶部加运营卡(总容量/已用容量/平均使用率,仿 CDN 近2日卡)。空态区分「无数据」与「采集失败/未启用」,采集异常时运营卡显示警示而非纯空(见「失败可观测性」)。**数据来源声明(统一展示口径)**:NAS 相关界面(列表页行内容量/使用率、运营卡、趋势图、Top)一律以 `ecam_nas_metric` 指标表为唯一数据来源;资产表 `ecam_instance` 的 capacity/used_capacity 仅作实例枚举与 fs 元数据,不再在 NAS 界面展示其容量数值——避免「资产表坏值 vs 指标表好值」同屏矛盾;资产表字段本身修复不在本期范围(见 Out of Scope)。

### Innovation Highlights

- 直接平移已验证的 CDN 经营洞察模式,不发明新架构;唯一创新点是**持久化日闸**(修掉 CDN 遗留的重启重复提交缺陷)。
- 指标语义为状态型(capacity/used/utilization)而非 CDN 的流量型——每日快照即可表达存储水位,NAS 天然适合天粒度。

## 单位归一化与字段语义

`ecam_nas_metric` 的容量字段以 **GB(二进制 GiB)** 为唯一口径,所有适配器在**采集边界**完成「厂商原始返回 → GB」换算,禁止把字节直接写进 GB 字段。现行 `sync_nas.go` 正是如此——实盘 aliyun `capacity=10485760`(实为 10 MiB 字节)、`used_capacity=1` 均被直接写入 GB 语义字段,是数据质量 bug 的根源,**新链路不得复刻**。换算对照:

| 厂商 | 监控 API / 指标 | 原始单位 | → GB 换算 |
|------|----------------|----------|-----------|
| aliyun | CMS DescribeMetricList(NAS 容量/用量) | 字节 | /1024^3 |
| tencent | monitor 子包(存储用量) | 字节 | /1024^3 |
| huawei | CES BatchListMetricData(`SYS.SFS` / `SYS.SFS_Turbo` 容量指标) | 字节 | /1024^3 |
| volcengine | cloudmonitor GetMetricData(指标名待探测确认) | 字节(待探测确认) | /1024^3(以探测为准) |
| aws | CloudWatch EFS `StorageBytes` | 字节 | /1024^3 |

写入路径另做数量级自检(capacity 落在 [1MB, 1PB] 区间),保证审查时每行可反向验证数量级正确。**capacity=0 例外放行并打标**:自检对 `capacity=0` 行(华为/AWS 实盘现状)**不拦截、不跳过**,标记 `qc_status=zero_exception` 后正常落库——数量级自检只对**非零**行做门禁/审查标注,零行走「capacity=0 异常行落库可见」路径,与 SC-1/SC-5 不冲突。

**utilization 语义**:`utilization = used/capacity`,但**不作为独立落库字段**,读取时由 `capacity`/`used` 派生,避免重采时三字段不一致。边界约定:

- `capacity=0`(华为/AWS 实盘现状):utilization 记空值(`null`),**不 panic、不写 NaN、不跳过该行**——异常行落库可见。
- `used>capacity`(厂商刷新时点不一致/扩缩容边界):参与使用率计算时按 `min(used, capacity)` 收敛,并打 warn 日志;原始 `used` 仍落库。
- `used=0, capacity>0`:utilization 记 0。

## 日快照取值口径与采集窗口

- **每日值口径**:落库的每日值取**日末态快照**(次日凌晨补采昨日完整行,取厂商当日最终聚合值)。取舍说明:末态可稳定复现、跨日不可变,但会系统性漏报当日高水位——扩容判断依赖运营卡「近 N 天峰值」与趋势图尖峰。**「近 N 天峰值」口径明确 = 日值序列的最大值**(由读取接口对已落库日值按 MAX 聚合派生,不落库)——它表达「近 N 天里水位最高的那一天」,**不捕获日内尖峰**(日内峰值需更高频采样/峰值落库,列为二期),与日末态快照架构一致,不承诺无法从日值恢复的日内尖峰。
- **调度时刻与采集区间**:对齐 CDN 的 `days=2` 思路——调度在 00:10 后触发,采集区间为 `[昨日, 今日]`:补昨日完整行 + 今日初态,避开各厂商监控指标聚合延迟窗口(CloudWatch/CES 均有分钟级延迟)。
- **同日首写生效(仅保护「今日」行)**:同日行**首写生效**——当日(今日)已有行则不覆盖、仅补当日缺失行,保证「每天一个值」而非「每天最后一个碰巧写到的值」;叠加在唯一键 `(account_id, fs_id, date)` 之上,不影响跨账号同 fs 并存。
- **次日补昨日 = 覆盖更新**:次日凌晨补采的「昨日完整行」**显式覆盖**昨日已有行,把昨日 00:10 初态行更新为厂商当日最终聚合值——「首写生效」仅作用于**今日**行,不作用于昨日行,避免「补昨日被首写挡住 → 日值永久停在 00:10 初态、厂商最终聚合值永不落库」的退化(与「每日值口径=日末态快照」语义对齐)。
- **日值不可变的定义窗口(写死)**:**昨日行**在次日补采完成后冻结(跨日不可变,此后同日重采不再触碰);**今日行**在当日内可变(首写生效,次日补采前可被修正)。冻结窗口以 Asia/Shanghai 自然日为准。
- **一次性历史回填**:上线时新增回填任务,从厂商 API 拉取启用日前 N 天(14~90 天)历史,否则运营要等 N 天才能看到 30 天趋势,与 Urgency 自省的「越晚修,历史水位缺失越多」相悖。
- **回填配额节流(与每日采集碰撞规避)**:回填按「厂商 × 账号」分片批处理,批大小默认每批 ≤5 实例 × 连续 ≤10 天,批间退避(默认 5 秒起,遇限流指数退避至上限);回填时段与每日自动采集错峰(回填在 01:30~06:00 窗口执行,日采在 00:10 后)。对 CloudWatch 这类有 GetMetricData 配额上限的 API(每 60 秒 5 万指标点),批大小按配额换算并留 30% 余量;命中限流的厂商回填挂起、次日窗口续跑,已成功批次不重试(以唯一键幂等去重)。

## 失败可观测性

「尽力而为」不等于「不可观测」——适配器失效与真实无指标必须在结果上可分辨,否则静默失效可能持续数周无人察觉:

- **执行器维度**:`nas:collect_metrics` 按厂商/账号维护**失败计数与末次错误**,任务 Result 携带(扩展 CDN `skipped_providers` 雏形为含错误明细的结构),运营可查。
- **适配器维度**:每个适配器区分两条失败路径——「探测不支持(指标名/namespace 未知)」打 **INFO**;「调用失败返回空(API 错误/超时/鉴权失败)」打 **ERROR 并携带 error 字段**。
- **前端维度**:空态区分「无数据(指标真实为 0 或空)」与「采集失败/未启用」;运营卡在采集异常时显示警示,而非与真实无数据相同的纯空。
- **运营卡判定顺序(0 占位 vs 警示)**:当某账号/厂商全部实例采集失败(API 停机/鉴权失效/执行器连续失败)时,**优先显示警示**,其次才考虑 0 占位——「采集失败→警示」优先级高于「无数据→0 占位」;只有「任务成功执行且指标真实为 0/空」才落入 0 占位分支,避免把未知伪装成真零容量。判定以执行器任务 Result 的失败计数为准。
- **不继承 CDN 全零跳过过滤**:CDN 执行器有「当日 `Bytes==0 && Bandwidth==0 && HitRate<0` 即跳过不写库」的过滤,NAS **不继承**——`capacity=0` 的异常行必须落库可见,否则华为/AWS 首日会整表静默为空。
- **自我健康监控(主动告警)**:对必达厂商(aliyun/huawei/aws)增加「连续 N 天(默认 3 天)零成功采集 → 升级告警」规则——执行器每日检查各必达厂商当日是否有成功写库行,连续 N 天为 0 时告警升级(钉群/日志 ERROR → 页面级),避免「监控系统自身静默失效数周无人察觉」。**前置条件:该厂商当前存在 ≥1 个 NAS 实例(实盘枚举非空)**——无 NAS 实例的厂商不触发零成功告警,避免每 3 天稳定误报、告警疲劳反而掩盖真实失效。该规则与持久化日闸写失败升级告警链路共用同一告警通道。

## 聚合口径(运营卡与 Top)

- CDN 的 Top 聚合暗含「域名只归属一个账号」的前提,该前提在 NAS **不成立**(NAS 主动允许多账号同 fs_id 并存,多活/共享实例)。
- **运营卡与 Top 均按 `fs_id` 去重后计数**:同一物理文件系统只计一次;多账号并存时按**「日期 desc,再容量 desc」排序取第一行**——先取最新日期,同日多账号行内再取容量最大,避免共享容量被双计、Top 失真。**口径声明**:该行的 capacity/used 作为该物理 fs 的容量/用量口径(代表物理文件系统容量);跨账号计量口径差异(共享配额/共享文件系统视图、各账号容量口径不同)一律以最新日期行的厂商返回值为准,不做跨账号容量求和或平均,避免「双计已除、口径仍高估」。
- **「平均使用率」分母口径**:对无数据实例**跳过不参与**(而非记 0 拉低均值);`capacity=0` 行也不参与均值(作为异常单独可见)。
- 趋势接口(单实例)按账号保留各自行(多账号各看各的);聚合去重只作用于运营卡与 Top。

## Requirements Analysis

### Key Scenarios

- 运营查看某 NAS 近 30 天容量/使用率趋势(抽屉监控 tab),判断是否需要扩容。
- 列表页看整体存储水位(总容量/已用/平均使用率),识别高水位实例。
- 每日调度自动采集(持久化日闸,服务重启不重复提交)。
- 弱厂商(指标名未知/API 失败)返回空,列表与抽屉显示对应空态(区分「无数据」与「采集失败/未启用」),不阻塞其他厂商;失败计数与末次错误在任务 Result 可见。
- 多账号同 fs_id 并存(多活/共享实例)各账号独立保留(复用 CDN 多账号键经验)。

### Non-Functional Requirements

- 采集尽力而为:单厂商失败只影响该厂商,单实例失败跳过继续;但必须走「失败可观测性」路径(失败计数/末次错误可见,前端区分空态)。
- 读取接口服务端校验:从鉴权上下文取 tenantID → 取租户全部云账号 → 校验客户端传入的 `account_id` ∈ 该集合(对齐 asset_cdn_query.go「先取租户全部云账号」模式),越权返回 404 不泄露账号存在性。
- 与 CDN 一致:运营时区(Asia/Shanghai)做自然日切分;唯一键幂等,且同日行**首写生效**(仅保护今日行,区别于 CDN 的「重采覆盖为最新值」);次日补昨日为覆盖更新,昨日行补采后冻结不可变(见「日快照取值口径与采集窗口」)。

### Constraints & Dependencies

- 新增依赖:tencent `monitor` 子包、aws `service/cloudwatch`(各一行 go.mod,低风险)。
- volcengine 监控指标名无公开文档;华为普通 SFS 与 SFS_Turbo 的 CES namespace(`SYS.SFS`/`SYS.SFS_Turbo`)与容量指标公开度需运行时验证——二者均由独立**探测任务**收敛(见 Next Steps),失败给出「二期补」明确判定;华为探测失败时的 SC-1 处置按「必达项失败处置」执行(华为降级为尽力而为、SC-1 条件式达标,见 Key Risks 表后说明)。
- **华为指标路径用实例真实 region**:指标采集的 region 解析独立于「单 region 静默回退 cn-north-4」逻辑(该隐患修复为单独件,Out of Scope),避免非 cn-north-4 实例查错地域。
- 不动并行会话 WIP;显式文件提交。

## Alternatives & Industry Benchmarking

### Industry Solutions

- 云厂商控制台均提供 NAS 监控(阿里云 CMS `DescribeMetricList`、腾讯云监控、华为 CES `BatchListMetricData`/`SYS.SFS`、AWS CloudWatch EFS `StorageBytes`/命名空间 `AWS/EFS`、火山云监控),指标通过各厂商监控 API 获取是行业标准路径(来源引证见下)。
- **具名参考来源引证**(官方文档/项目链接,把「行业标准路径」从断言转为可核查引证):
  - AWS CloudWatch EFS 指标(`StorageBytes`、`AWS/EFS`):https://docs.aws.amazon.com/efs/latest/ug/monitoring-cloudwatch.html
  - 阿里云云监控 CMS `DescribeMetricList`:https://help.aliyun.com/document_detail/28616.html(云监控产品首页 https://help.aliyun.com/product/28608.html)
  - 腾讯云云监控:https://cloud.tencent.com/document/product/248
  - 华为云 CES `BatchListMetricData`:https://support.huaweicloud.com/api-ces/index.html(CES 首页 https://support.huaweicloud.com/ces/index.html)
  - 火山引擎云监控(cloudmonitor):https://www.volcengine.com/product/cloudmonitor
  - `aws-cloudwatch-exporter`(prometheus 官方 exporter):https://github.com/prometheus/cloudwatch_exporter
  - `aliyun-exporter`(社区 exporter):https://github.com/aylei/aliyun-exporter
  - Zabbix 云监控模板:https://www.zabbix.com/integrations/aws
  - 版本以各项目 release 页为准(撰写时 `cloudwatch_exporter` ≥ v0.24、`aliyun-exporter` ≥ v0.9);本方案采用「厂商监控 API 直采」,上述 exporter 仅作行业基准对照、不引入依赖。
- CMDB/多云平台(具名参考:Zabbix 云监控模板、Prometheus 生态的 `aws-cloudwatch-exporter` / `aliyun-exporter`、多云 FinOps 容量看板)通常采集各云监控指标聚合展示——本方案同源,但内嵌于已有资产平台,免额外部署。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing | — | 零成本 | NAS 无经营视角,容量数据持续失真 | Rejected:与 CDN 不齐,运营决策缺位 |
| 复用资产表容量字段 | 现有 ecam_instance | 零新依赖零采集 | 字段质量差(华为/AWS=0),无历史趋势 | Rejected:数据不可信 |
| 独立监控系统(Prometheus+云监控 exporter) | 行业常见(aws-cloudwatch-exporter / aliyun-exporter / Zabbix) | 分钟级细粒度指标、可扩展告警 | 需独立部署/维护 Prometheus + exporter 集群(约 1 套基础设施 + 持续运维人力),指标与资产平台分两套数据源,需二次打通租户/账号映射 | Rejected:本方案仅需 1 条天粒度存储水位视图,Prometheus 的细粒度指标丰富度对日粒度状态型经营洞察属过度投入 |
| **厂商监控 API 直采 + 平台内聚合** | 本项目(仿 CDN 指标模式) | 无新系统,复用平台;字段权威 | 弱厂商指标名需实测 | **Selected:对齐 CDN 已验证路径,工作量可控** |

## Feasibility Assessment

### Technical Feasibility

- 高:CDN 指标链路(可选接口+执行器+DAO+调度+前端)全部在库可照抄;5 厂商监控 SDK 经调研 aliyun/huawei 零新依赖、tencent/aws 各一行 go.mod 子包、volcengine 已在主 SDK 内(见 Evidence 调研结论)。
- 风险集中点:volcengine 指标名实测、华为 namespace 验证——由独立**探测任务**收敛(见 Next Steps):成功即固定指标名/namespace,失败给出「二期补」明确判定;华为指标路径声明使用实例真实 region,不经过单 region 回退逻辑。

### Resource & Timeline

- 团队已两次完整交付同类模式(CDN 指标 T3/T4),技能就绪。预计 5 个任务与 CDN 指标 T3-T5 相当。
- **任务拆解与日历时间线**(按 1 名后端 + 0.5 名前端估算,可并行项标注):

  | 任务 | 工期 | 依赖 | 里程碑 |
  |------|------|------|--------|
  | 探测任务(volcengine 指标名/华为 namespace/实盘非零验证/容量分布统计) | 2 天 | 无,最高优先 | M1 探测报告 + 必达/尽力而为定案(发布 gate) |
  | types/接口/DAO + `ecam_nas_metric` 建表 | 1~2 天 | 探测定案 | Schema 冻结 |
  | 5 厂商适配器(必达 3 + 尽力而为 2)+ 单位归一化 + 数量级自检 | 3~4 天 | 建表 | 适配器单测通过 |
  | `nas:collect_metrics` 执行器 + 持久化日闸(含 CDN 迁移)+ 原子认领 + 回滚开关 | 3~4 天 | 适配器 | M2 日闸重启×3 通过 |
  | 历史回填(配额节流)+ 读取接口(趋势+Top 分页)+ 前端 | 4~5 天 | 执行器 | M3 联调完成,发布评审 |

  - **里程碑(按 1 名后端串行排期,对齐依赖链)**:M1 = 探测定案(T+2 天,发布与否的 gate);M2 = 指标落库 + 日闸通过重启测试(**T+10~12 天**,由依赖链串行推导:建表最迟 T+4 → 适配器最迟 T+8 → 执行器最迟 T+12;此后观测 ≥2 天,SC-1/SC-5 可验收);M3 = 前端 + 回填完成、发布评审(**T+15~17 天**,执行器最迟 T+12 + 回填/接口/前端 4~5 天串行推导)。
  - **并行压缩(可选,需 >1 名后端)**:持久化日闸/CDN 迁移/原子认领**不依赖 NAS 适配器**,可由第 2 名后端与「适配器」并行推进;2 名后端下 M2 可提前至 T+8~9、M3 可提前至 T+12~14。若维持 1 名后端,按上述串行里程碑承诺交付,不把 M2/M3 定在依赖链不可达的日期。

### Dependency Readiness

- 厂商监控 API 均为公有云标准接口长期可用。volcengine 监控文档稀缺是**最大外部不确定**(与 Key Risks 中 volcengine 行 M/M 评级拉齐);华为/AWS 对实盘实例是否返回非零数据亦纳入就绪验证(见 Next Steps 探测任务验收项)。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| NAS 已有数据够用作经营视图 | 5 Whys / Evidence | **Overturned**:实盘能力字段华为/AWS=0、aliyun used=1,数据不足以支撑运营决策 → 需厂商监控 API 直采 |
| 5 厂商都需新监控依赖 | 代码库搜索 | **Refined**:aliyun/huawei SDK 已在 go.mod 零新增,tencent/aws 各一行,volcengine 主 SDK 内 |
| 每日调度要继承 CDN 内存闸 | XY Detection | **Overturned**:内存闸今天实证重启重复提交 25+ 条 → NAS 用持久化日闸,修 CDN |
| NAS 指标是流量型(Copy CDN bytes) | Domain common sense | **Refined**:NAS 是状态型(capacity/used/utilization),每日快照即可,字段更简单 |

## Scope

### In Scope

1. NASMetricQuerier 可选接口 + types.NASMetric 模型
2. 5 厂商 NAS 监控实现(必达项 aliyun/huawei/aws + 尽力而为项 tencent/volcengine)**——必达/尽力而为分组是 M1 探测定案的条件结果,以 M1 定案为准;若 tencent/volcengine 任一实盘占比 >15% 且探测可用,升格为必达项纳入本期(SC-1 必达清单随分组同步更新,见 Success Criteria)**
3. `nas:collect_metrics` 执行器 + 注册(module.go)
4. `ecam_nas_metric` DAO(唯一键 (account_id, fs_id, date))+ 读取接口(单实例趋势 + Top)
5. 持久化日闸(scheduler_state):NAS 采用 + CDN 改用,消除重启重复提交
6. 前端:NAS 抽屉「监控」tab 趋势图 + 列表页运营卡(总容量/已用/平均使用率)

### Out of Scope

- 容量告警/高水位提示(使用率>阈值)——二期
- 华为 SFS 单 region 静默回退 cn-north-4 隐患修复——单独件
- `log`(操作日志)tab 内容——本期仅保持现状
- NAS 成本归因(按实例分摊费用)——另案,与 CDN 成本链路同模式另排
- 资产表 `ecam_instance` 的 capacity/used_capacity 字段质量修复——本期 NAS 界面统一以指标表为数据来源(见「数据来源声明」),资产表字段修复另立任务

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| volcengine 监控指标名不可用 | M | M(拉齐「唯一外部不确定」自述) | 独立探测任务 T-2 收敛:成功固定指标名,失败明确「二期补」并给出覆盖承诺(影响量化见表后说明) |
| 华为 SFS namespace/指标不可查 | M | M(必达项,预期可查) | 独立探测任务按 SFS/SFS_Turbo 分别验证 namespace;探测失败的 SC-1 处置见表后说明(华为降级为尽力而为、SC-1 条件式达标) |
| tencent/aws 新依赖引入问题 | L | M | 各一行 go.mod 子包;版本匹配主 SDK |
| 持久化日闸引入新的 mongo 读写失败点 | L | M | findOneAndUpdate 原子认领 + 写失败指数退避重试/升级告警 + 读失败 ≥5 分钟退避重读;认领成功才提交任务 |
| 与 CDN 指标执行器并发抢同一调度循环 / 多副本或手动+自动重叠并发触发 | L | M | 日闸按资源类型分键(cdn/nas 独立)+ findOneAndUpdate 原子认领,同一 NAS 只被一个实例认领 |

> **必达项失败处置与覆盖量化**:① 华为探测失败时的 SC-1 处置 = **条件式达标**——华为降级为尽力而为项,SC-1 改写为「aliyun/aws 必达」,降级结论与影响写入发布评审,不阻塞其余厂商;② volcengine「仅该厂商无指标」对租户覆盖的影响量化口径 = volcengine 账号下确有 NAS 实例的租户数 ÷ 总租户数,分子分母由探测任务随附的实盘容量分布统计产出;若任一尽力而为厂商实盘容量占比 >15% 且探测可用,升格为必达项纳入本期,**SC-1 同步更新:必达项清单随 M1 定案后的分组扩列——升格厂商(tencent/volcengine)的指标写库纳入 SC-1 的观测与数量级自检,SC-1 以「M1 定案后的分组」为准、不在 SC-1 中写死厂商清单**(见「必达厂商选择依据」)。

## Success Criteria

- [ ] aliyun/huawei/aws 三家(必达项,**必达项清单以 M1 探测定案后的分组为准;华为以 M1 探测通过为前提**)的 NAS 容量/使用率指标持续写入 `ecam_nas_metric`;**非零行** capacity 数量级经换算自检通过(真实 >0 级数据,观测 ≥2 天);capacity=0 行按「例外放行+打标」落库可见(见 SC-5,不因数量级自检被拦截)。**华为探测失败条件式改写**:华为降级为尽力而为项,SC-1 改写为「aliyun/aws 必达」,降级结论与影响写入发布评审(见 Next Steps)。**窗口内瞬时故障容忍(与 NFR「采集尽力而为」对齐)**:观测 ≥2 天的窗口内允许单厂商个别日期失败——不变量为「每必达厂商窗口内 ≥1 天成功写库」,一次性厂商 API 宕机不使 SC-1 不可达。**升格同步**:若 tencent/volcengine 任一实盘占比 >15% 且探测可用升格为必达项,SC-1 必达清单相应扩列(升格厂商指标写库纳入本 SC 的观测与数量级自检)
- [ ] tencent/volcengine 无数据时不报错、不阻塞(尽力而为语义);且适配器失败与真实无数据在 Result/前端可分辨(失败计数 + 末次错误可见)
- [ ] 服务重启后当日不再重复提交 NAS/CDN 指标采集任务(持久化日闸 + 原子认领生效)。**验收方法**:连续 3 次重启 × 每次观察调度器当日提交的 NAS/CDN 任务各仅 1 条;故障注入:模拟写日闸失败 3 次验证指数退避重试 + 升级告警触发、模拟读日闸失败验证 ≥5 分钟退避窗口内不重复提交。**CDN 迁移值回归**:迁移后对比 CDN 同域同日值与迁移前已落库值无缺口、无重复历史(仍按 `days=2` 语义正确写入);首部署 `scheduler_state` 无 cdn 记录时按「首次认领 → 触发一次当日提交」过渡行为执行。**通过判定**:重启与注入场景均无重复提交、CDN 日值回归无缺口、告警按预期触发
- [ ] 持久化日闸回滚路径验证:特性开关切回内存闸后,NAS/CDN 调度任务仍正常提交、指标采集不中断(回滚验证,见「特性开关与回滚」)
- [ ] `ecam_nas_metric` 唯一键 (account_id, fs_id, date):同日**首写生效仅保护今日行**(今日行当日内可变),**昨日行由次日补采覆盖更新、补采后冻结不可变**(定义窗口见「日快照取值口径与采集窗口」),跨账号同 fs 并存(≥3 账号同 fs 各留一行 live 测试)
- [ ] capacity=0 异常行落库可见(不继承 CDN 全零跳过过滤);前端空态区分「无数据」与「采集失败/未启用」
- [ ] NAS 抽屉「监控」tab 有数据时渲染容量/已用/使用率趋势图;无数据时显示空态(不白屏/不报错)
- [ ] NAS 列表页运营卡与 Top 按 fs_id 去重聚合(共享 fs 不双计);判定顺序「采集失败→警示」优先于「无数据→0 占位」——全部采集失败显示警示,仅任务成功且指标真实为 0/空时显示 0 占位
- [ ] NAS 指标读取接口按账号隔离:tenant A 查不到 tenant B 的 NAS 指标,越权传其他租户 account_id 返回 404

## Next Steps

1. **探测任务(T-2 天内,独立条目,最高优先级)**:用真实账号各跑一次 volcengine cloudmonitor 与华为 CES 查询——
   - 成功:写入样例行并固定指标名/namespace;失败:记录探测错误与候选指标名,给出「二期补」的明确判定(而非实现期口头兜底)。
   - 华为按文件系统类型(普通 SFS `SYS.SFS` / SFS_Turbo `SYS.SFS_Turbo`)分别探测 namespace,不能一刀切。
      - **验收项(必达项前提验证)**:确认华为/AWS 监控 API 对实盘这批 capacity=0 的实例**返回非零且数量级正确**的容量/StorageBytes——否则「资产表坏值是 sync 单位映射 bug、监控 API 数据是好的」前提不成立,SC-1 须重新评估;零值结果视为探测未通过并记录原因。
   - **华为探测失败的 SC-1 处置(预先声明)**:华为降级为尽力而为项,SC-1 改写为「aliyun/aws 必达」条件式达标,降级结论与影响写入发布评审——不阻塞 aliyun/aws 上线,但发布说明须显式记录。
   - **实盘容量按厂商分布统计**:输出各厂商 NAS 实例数/容量占比,作为「必达 vs 尽力而为」分组的证据与覆盖承诺依据(见「必达厂商选择依据」)。
2. **一次性历史回填任务**:从厂商 API 拉取启用日前 N 天(14~90 天)历史,让 30 天趋势上线即可见。
3. Proceed to `/write-prd` to formalize requirements(含上述规格:单位归一化与字段语义、日快照取值口径、失败可观测性、聚合口径、日闸原子认领、aws 必达取舍、读取接口契约、探测任务)。