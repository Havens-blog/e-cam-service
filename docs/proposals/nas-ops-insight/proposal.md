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
- NAS 抽屉(`NasDetailDrawer.vue`)声明了 5 个 tab,但 `monitor`(监控)与 `log`(操作日志)两个 tab **无任何内容分支**——监控 tab 点击无响应(空壳)。
- 后端无任何 NAS 指标读取接口(grep NASMetric / nas*metric 为空);无 `sync_nas_metrics` 执行器。
- 对标:CDN 已实现完整经营洞察(成本归因 + 天粒度指标 + 前端趋势),NAS 是唯一缺经营视角的重点资产。

### Urgency

- 存储是成本与容量敏感资产:NAS 扩容/治理滞后直接带来成本浪费或容量瓶颈事故。
- 缺口与 CDN 不齐:用户对 CDN 已有趋势视图,NAS 缺,产品能力不完整。
- 华为/AWS 容量字段为空是现成的数据质量 bug,越晚修,历史水位缺失越多(逐日指标从启用日起才有)。

## Proposed Solution

<!-- pre-revised: high -->
对标 CDN 指标模式(已上线验证),为 NAS 新增容量/使用率天粒度指标采集与展示:

1. **NASMetricQuerier 可选接口**(仿 `cloudx.CDNMetricQuerier`):`GetNASMetrics(ctx, fsID, fsName, startDate, endDate) ([]types.NASMetric, error)`。NASMetric **落库字段**:`fs_id / date / capacity(GB) / used_capacity(GB)`;**utilization 不落库**,读取时由 capacity/used 派生(单位与边界语义见「单位归一化与字段语义」)。
2. **5 厂商实现**:aliyun(CMS DescribeMetricList)、tencent(monitor 子包,需新增依赖)、huawei(CES BatchListMetricData,按文件系统类型分别用 `SYS.SFS`/`SYS.SFS_Turbo`)、volcengine(cloudmonitor GetMetricData,指标名经探测任务确认)、aws(CloudWatch EFS `StorageBytes`,新增一行 go.mod)。**必达项:aliyun/huawei/aws 三家**(均为标准指标,SDK 已在或轻量引入;aws 的 EFS StorageBytes 有标准指标、并非不可行,不套用 CloudFront「主动放弃」先例);**尽力而为项:tencent/volcengine**(指标可用性待探测)。任一适配器失败只返回自身空,不阻塞全流程,但须走「失败可观测性」路径(见对应章节)。
3. **NAS 指标采集执行器** `nas:collect_metrics`(仿 sync_cdn_metrics.go):按活跃账号遍历 NAS 实例 → 调 querier → 写入 `ecam_nas_metric`(唯一键 `(account_id, fs_id, date)`,复用 CDN 修复后的多账号经验)。同日行**首写生效**(补缺式 upsert,当日已有行不覆盖),与 CDN「同日重采覆盖为最新值」不同——理由见「日快照取值口径与采集窗口」。
4. **持久化日闸 + 原子认领**(本计划的改进点):CDN 现用内存 `lastMetricsCollectDate`,今天实测发现服务重启会重复提交 25+ 条待办指标任务。NAS 采用持久化日闸(记录最近触发日期到 MongoDB,如 `scheduler_state` collection),并**顺带把 CDN 的也改为持久化**——一次解决同类问题。语义:
   - **原子认领**:一次 `findOneAndUpdate`(条件 `resource_type=nas AND last_date<today`,更新为 today)原子认领当日,认领成功才提交采集任务——多副本同时触发、或手动 `nas:collect_metrics` 与每日自动任务重叠时,同一资源只被一个实例认领(对 CloudWatch 这类有 GetMetricData 配额上限的 API 尤其必要)。
   - **写失败降级**:日闸写入失败走指数退避重试并升级告警,**不是**仅记日志——否则「写失败 + 重启」会让本计划要修的重启重复提交缺陷原样回归。
   - **读失败退避**:日闸读失败设置最短退避窗口(如 5 分钟)再重读,防止挂在分钟级调度循环上逐分钟洪泛任务队列。
5. **NAS 指标读取接口**(契约,租户校验见 Non-Functional Requirements):`GET /assets/nas/metrics?fs_id=&account_id=&days=`(单实例趋势)与 `GET /assets/nas/top?account_id=&days=&sort=`(账号视角 Top)。接口从鉴权上下文取 tenantID,服务端校验客户端传入的 `account_id` ∈ 该租户账号集合,越权返回 404(不泄露账号存在性);`days` 限 1~90(回看天数);`sort` ∈ `capacity|utilization`(utilization 用近 N 天均值口径);趋势与 Top 同时返回「最新一天」与「近 N 天均值」两类值。
6. **前端**:NAS 抽屉「监控」tab 填容量/已用/使用率趋势图(echarts,仿 CdnDetailDrawer);NAS 列表页顶部加运营卡(总容量/已用容量/平均使用率,仿 CDN 近2日卡)。空态区分「无数据」与「采集失败/未启用」,采集异常时运营卡显示警示而非纯空(见「失败可观测性」)。

### Innovation Highlights

- 直接平移已验证的 CDN 经营洞察模式,不发明新架构;唯一创新点是**持久化日闸**(修掉 CDN 遗留的重启重复提交缺陷)。
- 指标语义为状态型(capacity/used/utilization)而非 CDN 的流量型——每日快照即可表达存储水位,NAS 天然适合天粒度。

## 单位归一化与字段语义

<!-- pre-revised: high -->
`ecam_nas_metric` 的容量字段以 **GB(二进制 GiB)** 为唯一口径,所有适配器在**采集边界**完成「厂商原始返回 → GB」换算,禁止把字节直接写进 GB 字段。现行 `sync_nas.go` 正是如此——实盘 aliyun `capacity=10485760`(实为 10 MiB 字节)、`used_capacity=1` 均被直接写入 GB 语义字段,是数据质量 bug 的根源,**新链路不得复刻**。换算对照:

| 厂商 | 监控 API / 指标 | 原始单位 | → GB 换算 |
|------|----------------|----------|-----------|
| aliyun | CMS DescribeMetricList(NAS 容量/用量) | 字节 | /1024^3 |
| tencent | monitor 子包(存储用量) | 字节 | /1024^3 |
| huawei | CES BatchListMetricData(`SYS.SFS` / `SYS.SFS_Turbo` 容量指标) | 字节 | /1024^3 |
| volcengine | cloudmonitor GetMetricData(指标名待探测确认) | 字节(待探测确认) | /1024^3(以探测为准) |
| aws | CloudWatch EFS `StorageBytes` | 字节 | /1024^3 |

写入路径另做数量级自检(capacity 落在 [1MB, 1PB] 区间),保证审查时每行可反向验证数量级正确。

**utilization 语义**:`utilization = used/capacity`,但**不作为独立落库字段**,读取时由 `capacity`/`used` 派生,避免重采时三字段不一致。边界约定:

- `capacity=0`(华为/AWS 实盘现状):utilization 记空值(`null`),**不 panic、不写 NaN、不跳过该行**——异常行落库可见。
- `used>capacity`(厂商刷新时点不一致/扩缩容边界):参与使用率计算时按 `min(used, capacity)` 收敛,并打 warn 日志;原始 `used` 仍落库。
- `used=0, capacity>0`:utilization 记 0。

## 日快照取值口径与采集窗口

<!-- pre-revised: medium -->
- **每日值口径**:落库的每日值取**日末态快照**(次日凌晨补采昨日完整行,取厂商当日最终聚合值)。取舍说明:末态可稳定复现、跨日不可变,但会系统性漏报当日高水位——扩容判断依赖运营卡「近 N 天峰值」与趋势图尖峰(峰值由读取接口按 MAX 聚合派生,不落库)。
- **调度时刻与采集区间**:对齐 CDN 的 `days=2` 思路——调度在 00:10 后触发,采集区间为 `[昨日, 今日]`:补昨日完整行 + 今日初态,避开各厂商监控指标聚合延迟窗口(CloudWatch/CES 均有分钟级延迟)。
- **同日不可变**:同日行**首写生效**(当日已有行则不覆盖,仅补当日缺失行),保证「每天一个值」而非「每天最后一个碰巧写到的值」;叠加在唯一键 `(account_id, fs_id, date)` 之上,不影响跨账号同 fs 并存。
- **一次性历史回填**:上线时新增回填任务,从厂商 API 拉取启用日前 N 天(14~90 天)历史,否则运营要等 N 天才能看到 30 天趋势,与 Urgency 自省的「越晚修,历史水位缺失越多」相悖。

## 失败可观测性

<!-- pre-revised: high -->
「尽力而为」不等于「不可观测」——适配器失效与真实无指标必须在结果上可分辨,否则静默失效可能持续数周无人察觉:

- **执行器维度**:`nas:collect_metrics` 按厂商/账号维护**失败计数与末次错误**,任务 Result 携带(扩展 CDN `skipped_providers` 雏形为含错误明细的结构),运营可查。
- **适配器维度**:每个适配器区分两条失败路径——「探测不支持(指标名/namespace 未知)」打 **INFO**;「调用失败返回空(API 错误/超时/鉴权失败)」打 **ERROR 并携带 error 字段**。
- **前端维度**:空态区分「无数据(指标真实为 0 或空)」与「采集失败/未启用」;运营卡在采集异常时显示警示,而非与真实无数据相同的纯空。
- **不继承 CDN 全零跳过过滤**:CDN 执行器有「当日 `Bytes==0 && Bandwidth==0 && HitRate<0` 即跳过不写库」的过滤,NAS **不继承**——`capacity=0` 的异常行必须落库可见,否则华为/AWS 首日会整表静默为空。

## 聚合口径(运营卡与 Top)

<!-- pre-revised: medium -->
- CDN 的 Top 聚合暗含「域名只归属一个账号」的前提,该前提在 NAS **不成立**(NAS 主动允许多账号同 fs_id 并存,多活/共享实例)。
- **运营卡与 Top 均按 `fs_id` 去重后计数**:同一物理文件系统只计一次;多账号并存时取**最新日期、容量最大**的那一行,避免共享容量被双计、Top 失真。
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

<!-- pre-revised: high -->
- 采集尽力而为:单厂商失败只影响该厂商,单实例失败跳过继续;但必须走「失败可观测性」路径(失败计数/末次错误可见,前端区分空态)。
- 读取接口服务端校验:从鉴权上下文取 tenantID → 取租户全部云账号 → 校验客户端传入的 `account_id` ∈ 该集合(对齐 asset_cdn_query.go「先取租户全部云账号」模式),越权返回 404 不泄露账号存在性。
- 与 CDN 一致:运营时区(Asia/Shanghai)做自然日切分;唯一键幂等,且同日行**首写生效**(区别于 CDN 的「重采覆盖为最新值」)。

### Constraints & Dependencies

<!-- pre-revised: medium -->
- 新增依赖:tencent `monitor` 子包、aws `service/cloudwatch`(各一行 go.mod,低风险)。
- volcengine 监控指标名无公开文档;华为普通 SFS 与 SFS_Turbo 的 CES namespace(`SYS.SFS`/`SYS.SFS_Turbo`)与容量指标公开度需运行时验证——二者均由独立**探测任务**收敛(见 Next Steps),失败给出「二期补」明确判定。
- **华为指标路径用实例真实 region**:指标采集的 region 解析独立于「单 region 静默回退 cn-north-4」逻辑(该隐患修复为单独件,Out of Scope),避免非 cn-north-4 实例查错地域。
- 不动并行会话 WIP;显式文件提交。

## Alternatives & Industry Benchmarking

### Industry Solutions

- 云厂商控制台均提供 NAS 监控(阿里云 CMS、腾讯云监控、华为 CES、AWS CloudWatch、火山云监控),指标通过监控 API 获取是行业标准路径。
- CMDB/多云平台(如 Zabbix、Prometheus 集成)通常采集各云监控指标聚合展示——本方案同源,但内嵌于已有资产平台,免额外部署。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing | — | 零成本 | NAS 无经营视角,容量数据持续失真 | Rejected:与 CDN 不齐,运营决策缺位 |
| 复用资产表容量字段 | 现有 ecam_instance | 零新依赖零采集 | 字段质量差(华为/AWS=0),无历史趋势 | Rejected:数据不可信 |
| 独立监控系统(Prometheus+云监控 exporter) | 行业常见 | 指标丰富 | 需部署/维护新系统,与资产平台割裂 | Rejected:重资产,过度 |
| **厂商监控 API 直采 + 平台内聚合** | 本项目(仿 CDN 指标模式) | 无新系统,复用平台;字段权威 | 弱厂商指标名需实测 | **Selected:对齐 CDN 已验证路径,工作量可控** |

## Feasibility Assessment

### Technical Feasibility

- 高:CDN 指标链路(可选接口+执行器+DAO+调度+前端)全部在库可照抄;5 厂商监控 SDK 经调研 aliyun/huawei 零新依赖、tencent/aws 各一行 go.mod 子包、volcengine 已在主 SDK 内(见 Evidence 调研结论)。
- 风险集中点:volcengine 指标名实测、华为 namespace 验证——由独立**探测任务**收敛(见 Next Steps):成功即固定指标名/namespace,失败给出「二期补」明确判定;华为指标路径声明使用实例真实 region,不经过单 region 回退逻辑。

### Resource & Timeline

- 团队已两次完整交付同类模式(CDN 指标 T3/T4),技能就绪。预计 5 个任务与 CDN 指标 T3-T5 相当。

### Dependency Readiness

- 厂商监控 API 均为公有云标准接口长期可用。volcengine 监控文档稀缺是唯一外部不确定。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| NAS 已有数据够用作经营视图 | 5 Whys / Evidence | **Overturned**:实盘能力字段华为/AWS=0、aliyun used=1,数据不足以支撑运营决策 → 需厂商监控 API 直采 |
| 5 厂商都需新监控依赖 | 代码库搜索 | **Refined**:aliyun/huawei SDK 已在 go.mod 零新增,tencent/aws 各一行,volcengine 主 SDK 内 |
| 每日调度要继承 CDN 内存闸 | XY Detection | **Overturned**:内存闸今天实证重启重复提交 25+ 条 → NAS 用持久化日闸,修 CDN |
| NAS 指标是流量型(Copy CDN bytes) | Domain common sense | **Refined**:NAS 是状态型(capacity/used/utilization),每日快照即可,字段更简单 |

## Scope

<!-- pre-revised: high -->
### In Scope

1. NASMetricQuerier 可选接口 + types.NASMetric 模型
2. 5 厂商 NAS 监控实现(必达项 aliyun/huawei/aws + 尽力而为项 tencent/volcengine)
3. `nas:collect_metrics` 执行器 + 注册(module.go)
4. `ecam_nas_metric` DAO(唯一键 (account_id, fs_id, date))+ 读取接口(单实例趋势 + Top)
5. 持久化日闸(scheduler_state):NAS 采用 + CDN 改用,消除重启重复提交
6. 前端:NAS 抽屉「监控」tab 趋势图 + 列表页运营卡(总容量/已用/平均使用率)

### Out of Scope

- 容量告警/高水位提示(使用率>阈值)——二期
- 华为 SFS 单 region 静默回退 cn-north-4 隐患修复——单独件
- `log`(操作日志)tab 内容——本期仅保持现状
- NAS 成本归因(按实例分摊费用)——另案,与 CDN 成本链路同模式另排

## Key Risks

<!-- pre-revised: medium -->
| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| volcengine 监控指标名不可用 | M | L(仅该厂商无指标) | 独立探测任务 T-2 收敛:成功固定指标名,失败明确「二期补」 |
| 华为 SFS namespace/指标不可查 | M | L(必达项,预期可查) | 独立探测任务按 SFS/SFS_Turbo 分别验证 namespace;失败给出二期判定并告警 |
| tencent/aws 新依赖引入问题 | L | M | 各一行 go.mod 子包;版本匹配主 SDK |
| 持久化日闸引入新的 mongo 读写失败点 | L | M | findOneAndUpdate 原子认领 + 写失败指数退避重试/升级告警 + 读失败 ≥5 分钟退避重读;认领成功才提交任务 |
| 与 CDN 指标执行器并发抢同一调度循环 / 多副本或手动+自动重叠并发触发 | L | M | 日闸按资源类型分键(cdn/nas 独立)+ findOneAndUpdate 原子认领,同一 NAS 只被一个实例认领 |

## Success Criteria

<!-- pre-revised: high -->
- [ ] aliyun/huawei/aws 三家(必达项)的 NAS 容量/使用率指标持续写入 `ecam_nas_metric`,每行 capacity 数量级经换算自检通过(真实 >0 级数据,观测 ≥2 天)
- [ ] tencent/volcengine 无数据时不报错、不阻塞(尽力而为语义);且适配器失败与真实无数据在 Result/前端可分辨(失败计数 + 末次错误可见)
- [ ] 服务重启后当日不再重复提交 NAS/CDN 指标采集任务(持久化日闸 + 原子认领生效,重启测试 1 次;写失败重试、读失败退避各模拟验证 1 次)
- [ ] `ecam_nas_metric` 唯一键 (account_id, fs_id, date):同日**首写生效**(同日重采不覆盖、日值不可变),跨账号同 fs 并存(≥3 账号同 fs 各留一行 live 测试)
- [ ] capacity=0 异常行落库可见(不继承 CDN 全零跳过过滤);前端空态区分「无数据」与「采集失败/未启用」
- [ ] NAS 抽屉「监控」tab 有数据时渲染容量/已用/使用率趋势图;无数据时显示空态(不白屏/不报错)
- [ ] NAS 列表页运营卡与 Top 按 fs_id 去重聚合(共享 fs 不双计);全部无数据时显示 0 占位而非空
- [ ] NAS 指标读取接口按账号隔离:tenant A 查不到 tenant B 的 NAS 指标,越权传其他租户 account_id 返回 404

## Next Steps

<!-- pre-revised: medium -->
1. **探测任务(T-2 天内,独立条目,最高优先级)**:用真实账号各跑一次 volcengine cloudmonitor 与华为 CES 查询——
   - 成功:写入样例行并固定指标名/namespace;失败:记录探测错误与候选指标名,给出「二期补」的明确判定(而非实现期口头兜底)。
   - 华为按文件系统类型(普通 SFS `SYS.SFS` / SFS_Turbo `SYS.SFS_Turbo`)分别探测 namespace,不能一刀切。
2. **一次性历史回填任务**:从厂商 API 拉取启用日前 N 天(14~90 天)历史,让 30 天趋势上线即可见。
3. Proceed to `/write-prd` to formalize requirements(含上述规格:单位归一化与字段语义、日快照取值口径、失败可观测性、聚合口径、日闸原子认领、aws 必达取舍、读取接口契约、探测任务)。