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

对标 CDN 指标模式(已上线验证),为 NAS 新增容量/使用率天粒度指标采集与展示:

1. **NASMetricQuerier 可选接口**(仿 `cloudx.CDNMetricQuerier`):`GetNASMetrics(ctx, fsID, fsName, startDate, endDate) ([]types.NASMetric, error)`。NASMetric 字段:`fs_id / date / capacity(GB) / used_capacity(GB) / utilization(0-1, used/capacity)`。
2. **5 厂商实现**:aliyun(CMS DescribeMetricList)、tencent(monitor 子包,需新增依赖)、huawei(CES BatchListMetricData)、volcengine(cloudmonitor GetMetricData,指标名需实测)、aws(CloudWatch,需新增依赖,**已有主动放弃先例**——若不可行返回空不阻塞)。沿用 CDN 原则:**弱厂商尽力而为,失败/指标名未知返回空,不阻塞全流程**。
3. **NAS 指标采集执行器** `nas:collect_metrics`(仿 sync_cdn_metrics.go):按活跃账号遍历 NAS 实例 → 调 querier → 写入 `ecam_nas_metric`(唯一键 `(account_id, fs_id, date)`,复用 CDN 修复后的多账号经验)。
4. **持久化日闸**(本计划的改进点):CDN 现用内存 `lastMetricsCollectDate`,今天实测发现服务重启会重复提交 25+ 条待办指标任务。NAS 采用持久化日闸(记录最近触发日期到 MongoDB,如 `scheduler_state` collection),并**顺带把 CDN 的也改为持久化**——一次解决同类问题。
5. **NAS 指标读取接口**:`GET /assets/nas/metrics?fs_id=&account_id=&days=`(按账号隔离,仿 CDN 读取),同 `GET /assets/nas/top`(账号视角 Top by 容量或使用率)。
6. **前端**:NAS 抽屉「监控」tab 填容量/已用/使用率趋势图(echarts,仿 CdnDetailDrawer);NAS 列表页顶部加运营卡(总容量/已用容量/平均使用率,仿 CDN 近2日卡)。

### Innovation Highlights

- 直接平移已验证的 CDN 经营洞察模式,不发明新架构;唯一创新点是**持久化日闸**(修掉 CDN 遗留的重启重复提交缺陷)。
- 指标语义为状态型(capacity/used/utilization)而非 CDN 的流量型——每日快照即可表达存储水位,NAS 天然适合天粒度。

## Requirements Analysis

### Key Scenarios

- 运营查看某 NAS 近 30 天容量/使用率趋势(抽屉监控 tab),判断是否需要扩容。
- 列表页看整体存储水位(总容量/已用/平均使用率),识别高水位实例。
- 每日调度自动采集(持久化日闸,服务重启不重复提交)。
- 弱厂商(指标名未知/API 失败)返回空,列表与抽屉显示空态,不阻塞其他厂商。
- 多账号同 fs_id 并存(多活/共享实例)各账号独立保留(复用 CDN 多账号键经验)。

### Non-Functional Requirements

- 采集尽力而为:单厂商失败只影响该厂商,单实例失败跳过继续。
- 读取接口按账号隔离(tenant → accounts → filter),防跨租户越权。
- 与 CDN 一致:运营时区(Asia/Shanghai)做自然日切分;唯一键幂等 upsert。

### Constraints & Dependencies

- 新增依赖:tencent `monitor` 子包、aws `service/cloudwatch`(各一行 go.mod,低风险)。
- volcengine 监控指标名无公开文档,需运行时探测(最大不确定性)。
- 华为 SFS Turbo 的 CES namespace(SYS.SFS_Turbo)与容量指标公开度需运行时验证。
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
- 风险集中点:volcengine 指标名实测、华为 namespace 验证——均走「尽力而为返回空」兜底。

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

### In Scope

1. NASMetricQuerier 可选接口 + types.NASMetric 模型
2. 5 厂商 NAS 监控实现(aliyun/tencent/huawei/volcengine/aws,尽力而为)
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

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| volcengine 监控指标名不可用 | M | L(仅该厂商无指标) | 运行时探测;失败返回空不阻塞,二期补 |
| 华为 SFS namespace/指标不可查 | M | L | 同上;实际调用验证 |
| tencent/aws 新依赖引入问题 | L | M | 各一行 go.mod 子包;版本匹配主 SDK |
| 持久化日闸引入新的 mongo 读写失败点 | L | M | 读失败按「上次日期未知」触发一次(安全侧);写失败记录日志 |
| 与 CDN 指标执行器并发抢同一调度循环 | L | M | 日闸按资源类型分键(cdn/nas 独立) |

## Success Criteria

- [ ] aliyun 与 huawei 的 NAS 容量/使用率指标持续写入 `ecam_nas_metric`(≥2 家主力厂商有真实 bytes>0 级数据,观测 ≥2 天)
- [ ] volcengine/tencent/aws 无数据时不报错、不阻塞(尽力而为语义:单厂商失败仅自身空)
- [ ] 服务重启后当日不再重复提交 NAS/CDN 指标采集任务(持久化日闸生效,重启测试 1 次)
- [ ] `ecam_nas_metric` 唯一键 (account_id, fs_id, date):同账号同日 upsert 幂等,跨账号同 fs 并存(≥3 账号同 fs 各留一行 live 测试)
- [ ] NAS 抽屉「监控」tab 有数据时渲染容量/已用/使用率趋势图;无数据时显示空态(不白屏/不报错)
- [ ] NAS 列表页运营卡显示总容量/已用容量/平均使用率(有数据时);全部无数据时显示 0 占位而非空
- [ ] NAS 指标读取接口按账号隔离(tenant A 查不到 tenant B 的 NAS 指标)

## Next Steps

- Proceed to `/write-prd` to formalize requirements