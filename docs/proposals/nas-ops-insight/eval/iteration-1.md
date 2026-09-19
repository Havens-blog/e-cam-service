# Iteration 1 Report — Adversarial Rubric Scoring (CTO)

**提案**: docs/proposals/nas-ops-insight/proposal.md
**审查专家**: CTO(experts/scorer/cto.md persona)
**日期**: 2026-09-17
**类型**: proposal | **标尺**: 1000 | **目标**: 900 | **迭代**: 1/3
**模式**: Annotated Blind Review(pre-revision 已执行,marker 已用于注意力分配)

## Eval-Proposal Complete

**Final Score**: 695/1000 (target: 900)
**Iterations Used**: 1/3

### Score Progression

| Iteration | Score | Delta |
|-----------|-------|-------|
| Pre-Revision Baseline | (freeform 未评分,见 iteration-0-report) | — |
| 1 | 695 | — |

### Dimension Breakdown (final)

| # | Dimension | Score | Max |
|---|-----------|-------|-----|
| 1 | Problem Definition | 86 | 110 |
| 2 | Solution Clarity | 100 | 120 |
| 3 | Industry Benchmarking | 76 | 120 |
| 4 | Requirements Completeness | 84 | 110 |
| 5 | Solution Creativity | 56 | 100 |
| 6 | Feasibility | 72 | 100 |
| 7 | Scope Definition | 64 | 80 |
| 8 | Risk Assessment | 54 | 90 |
| 9 | Success Criteria | 48 | 80 |
| 10 | Logical Consistency | 55 | 90 |

### Outcome

Target NOT reached — 695 < 900,继续迭代。

---

## Phase 1 — 推理审计(独立立场)

### 1.1 Problem → Solution

问题:5 家厂商 NAS 已枚举但无容量/使用率经营视角,资产表 capacity/used_capacity 实盘质量差,无 NAS 指标接口,抽屉监控 tab 空壳。
方案:平移 CDN 指标链路 → 新建 `ecam_nas_metric` + 5 厂商适配器 + 执行器 + 持久化日闸 + 读取接口 + 前端。

判定:**大体真实解决**——经营视角通过新表建立,Problem 的主诉求被命中。但存在一处结构性残余:**资产表本身的质量 bug 未被修复**。列表页行仍展示资产表 capacity(华为/AWS=0),顶部运营卡展示指标表容量——同一页面同一实例出现矛盾数值;方案对「两表并存、以谁为准」无任何声明。这是"声称解决却重蹈"的 CTO 失败模式:数据质量 bug 在原始表面上原样保留,只是被新表绕开。

### 1.2 Solution → Evidence

证据(实盘采样 2026-09-17、grep 空、空 tab、CDN 对标)支撑问题成立。但证据到方案有一处**未声明假设**:资产表华为/AWS capacity=0 被归因为 sync 链路单位映射 bug,方案因此默认「监控 API 返回的数据是好的」。该假设未被验证——华为 CES / AWS CloudWatch 对实盘这批实例是否返回非零容量,取决于探测任务;而探测任务只覆盖 volcengine 指标名与华为 namespace,未覆盖「监控 API 对实盘实例返回非零」。若监控 API 同样返回 0,SC-1 整条不可达成且无后备。

### 1.3 Evidence → Success Criteria

SC 大体可测(观测 ≥2 天、≥3 账号同 fs、越权 404),但 SC-1 依赖上述未验证假设;SC-3「重启测试 1 次」在统计上不足以证明「不再重复提交」;SC-7「0 占位」把「未知」伪装成「0% 使用率」。

### 1.4 自查矛盾(最高严重度锚点)

**采集语义自相矛盾**。两段原文在同行 (account_id, fs_id, date) 上互斥:

- 原文 A:「落库的每日值取**日末态快照**(次日凌晨补采昨日完整行,取厂商当日最终聚合值)」
- 原文 B:「同日行**首写生效**(当日已有行则不覆盖,仅补当日缺失行),保证「每天一个值」而非「每天最后一个碰巧写到的值」」

双向推导:
- A→B:设 A 成立,则第 D 日值在 D+1 日凌晨被回填为 D 日最终聚合值——D 日行被**改写**,违反 B「首写生效/日值不可变」。B 不可满足。
- B→A:设 B 成立,则第 D 日 00:10 写入的「今日初态」行(D 日 00:10 采集区间 [昨日,今日] 所写)永久保留;D+1 日「补昨日完整行」因行已存在被补缺式 upsert 跳过——厂商当日最终聚合值**永不落库**,A 的「日末态快照」不可满足。

结论:无论哪种读法,SC-4 ↔ 日快照取值口径 存在矛盾。实际落库的每日值 = 该日 00:10 初态(≈前一日末态),且恰落在方案自述要避开的「CloudWatch/CES 分钟级聚合延迟窗口」内;「峰值由 MAX 派生」也只能派生出"每日初态"的最大值,系统性漏报日内尖峰。**这是 revision 引入的新问题**(日末态语义与首写生效分别由 pre-revision 为回应两个不同 freeform 风险而加入,二者从未对齐)。

---

## SC 一致性深挖

按受影响区域聚类:
- 聚类 A — `ecam_nas_metric` 表:SC-1/SC-4/SC-5/SC-7 + InScope-4
- 聚类 B — 调度器/日闸(`scheduler_state`/`nas:collect_metrics`):SC-3 + InScope-3/InScope-5
- 聚类 C — 读取接口:SC-8 + InScope-4
- 聚类 D — 前端:SC-6/SC-7 + InScope-6
- 聚类 E — 5 厂商实现:SC-1/SC-2 + InScope-2

```
CONTRADICTION #1: SC-4 "同日首写生效(同日重采不覆盖、日值不可变)" ↔ 日快照取值口径 "落库的每日值取日末态快照(次日凌晨补采昨日完整行,取厂商当日最终聚合值)"
| Type: mutual-exclusion
| Evidence: [双向推导见 1.4:A 成立→D 日行被改写→B 不可满足;B 成立→D 日最终聚合值永不落库→A 不可满足]
| Resolution required: [改写——要么「次日补昨日」显式覆盖昨日行、仅保护「今日」行不受同日重采;要么取消「今日初态」写入、全部由次日回填决定日值;并把「日值不可变」的变更窗口写死(当日不可变,次日回填窗口内可变,回填后不可变)]
```

```
CONTRADICTION #2: SC-1 "每行 capacity 数量级经换算自检通过(真实 >0 级数据" ↔ SC-5 "capacity=0 异常行落库可见(不继承 CDN 全零跳过过滤)"
| Type: ambiguous — 概率冲突
| Evidence: [SC-1 要求每行数量级自检通过且真实 >0;SC-5 要求 capacity=0 行必须落库。capacity=0 落在自检区间 [1MB,1PB] 之外;若自检是写门禁→SC-5 不可满足;若自检仅作审查标注→SC-1「每行通过」不可满足。方案未定义 capacity=0 行与数量级自检的关系]
| Resolution required: [声明数量级自检对 capacity=0 的处理(例外放行并打标,或把 SC-1 改写为「非零行数量级通过、零行按 SC-5 落库可见」)]
```

```
CONTRADICTION #3: SC-7 "全部无数据时显示 0 占位而非空" ↔ SC-5 "空态区分「无数据」与「采集失败/未启用」" / 失败可观测性 "采集异常时运营卡显示警示而非纯空"
| Type: ambiguous — 边界未定义
| Evidence: [当全部厂商采集失败(API 停机/鉴权失效)时,该状态属于「全部无数据」(显示 0,把未知伪装成真零容量)还是「采集异常」(显示警示)?两者判定优先级未定义]
| Resolution required: [为运营卡定义「采集失败→警示」优先于「无数据→0 占位」的判定规则]
```

```
CONTRADICTION #4: SC-1 "aliyun/huawei/aws 三家(必达项)...持续写入" ↔ Key Risks "华为 SFS namespace/指标不可查 | M | L(必达项,预期可查) | ...失败给出二期判定并告警"
| Type: direction-clash
| Evidence: [华为为必达项;若探测失败回退「二期补」,SC-1 对华为无法满足。方案未定义届时 SC-1 是降级(只验 aliyun/aws)、阻塞发布、还是把华为改列为尽力而为]
| Resolution required: [明确「必达项」在探测失败时的处置:阻塞发布 / SC-1 改写为条件式 / 华为降级为尽力而为]
```

聚类 A 其余两两(CAN_SATISFY)无冲突;聚类 B/C/D/E 组内双向推导均无互斥。SC-2 ↔ SC-5、SC-4 ↔ SC-7 兼容(存储层按账号留行、展示层按 fs_id 去重,可同时满足)。

---

## Phase 2 — Rubric 评分(验证立场)

> 立场:每个断言在提供证据前视为未验证。所有扣分均引原文。

### 1. Problem Definition — 86/110
- **问题清晰 33/40**:核心问题无歧义。扣:「资产表数据质量差」与「经营视角缺位」双主线未在问题处划定边界,导致与方案的「新表绕开旧表」关系不清。
- **证据 33/40**:实盘采样(「huawei `capacity=0, used_capacity=0`,aws `capacity=0, used_capacity=0`,aliyun `capacity=10485760, used_capacity=1`」)、grep 空、空 tab 均为真证据。扣:CDN「已上线验证」无数据支撑;「监控 API 返回非零」的假设无证据(见盲点)。
- **紧迫性 20/30**:「存储是成本与容量敏感资产:NAS 扩容/治理滞后直接带来成本浪费或容量瓶颈事故」——「成本浪费/瓶颈事故」无量化、无近失案例,属未声明假设当事实;最强论据是「越晚修,历史水位缺失越多」的时间敏感性,成立。

### 2. Solution Clarity — 100/120
- **方案具体 33/40**:接口签名 `GetNASMetrics(ctx, fsID, fsName, startDate, endDate) ([]types.NASMetric, error)`、唯一键 (account_id, fs_id, date)、双端点、前端组件均具体。扣:采集语义矛盾使「每日值到底是什么」不清晰。
- **用户可见行为 38/45**:趋势图/运营卡/空态/0 占位/警示描述充分。扣:运营卡「0 占位 vs 警示」边界未定(见 CONTRADICTION #3)。
- **技术方向 29/35**:SDK 分厂商、region 独立、echarts、MongoDB 明确。扣:日值语义矛盾;Top 端点无结果数/分页。

### 3. Industry Benchmarking — 76/120
- **行业方案引用 24/40**:「云厂商控制台均提供 NAS 监控(阿里云 CMS、腾讯云监控、华为 CES、AWS CloudWatch、火山云监控)」与「CMDB/多云平台(如 Zabbix、Prometheus 集成)」——无具名开源项目/发布模式/参考链接,「行业标准路径」为断言无引证。
- **≥3 有意义替代 22/30**:Do nothing / 复用资产表 / Prometheus+exporter / 厂商 API 直采 = 4 个不同路径。但「独立监控系统(Prometheus+云监控 exporter)」仅以「需部署/维护新系统,与资产平台割裂」一句即 Rejected,权衡不足,近稻草人(-)。
- **诚实权衡 14/25**:Pros/Cons 过短,选中方案缺点仅「弱厂商指标名需实测」被最小化;「对比表」的 Verdict 全部指向自选方案,自证倾向。
- **选中方案对照基准论证 16/25**:「对齐 CDN 已验证路径,工作量可控」是真实理由(内部已验证模式),成立。

### 4. Requirements Completeness — 84/110
- **场景覆盖 31/40**:happy path(趋势/运营卡)+ 边界(capacity=0、used>capacity、重启、多账号同 fs、适配器失败、回填)覆盖良好。扣:Top 端点无结果数/分页、多 region 账号、「活跃账号」定义缺失、读取接口响应契约(最新一天/近 N 天均值的序列化结构)未全。
- **NFR 27/40**:安全(租户隔离 404)、性能(days 限 1~90)、时区(Asia/Shanghai)、幂等(唯一键+首写生效)有。扣:读取接口无限流/分页 NFR;无告警升级路径(见盲点);可访问性未提(运营后台,弱项)。
- **约束与依赖 26/30**:新依赖(tencent monitor/aws cloudwatch)、volcengine 文档稀缺、华为 namespace 运行验证、region 声明、WIP/显式提交均点名。

### 5. Solution Creativity — 56/100
- **相对行业基线新颖性 18/40**:持久化日闸 + 原子认领是真实改进,但属标准分布式锁/调度状态模式,非行业级新颖;方案自认「直接平移已验证的 CDN 经营洞察模式,不发明新架构」。
- **跨域借鉴 20/35**:findOneAndUpdate 原子认领借鉴分布式认领/锁模式;utilization 不落库读取派生是数据完整性洞察。
- **洞察简洁性 18/25**:「状态型每日快照即可表达存储水位」+ 派生 utilization 简洁优雅。

### 6. Feasibility — 72/100
- **技术可行性 30/40**:CDN 链路(可选接口+执行器+DAO+调度+前端)全在库可照抄;SDK 依赖低(aliyun/huawei 零新增、tencent/aws 各一行)。扣:采集语义矛盾是技术缺陷;「监控 API 返回非零」未验证。
- **资源与时间 18/30**:「团队已两次完整交付同类模式(CDN 指标 T3/T4),技能就绪。预计 5 个任务与 CDN 指标 T3-T5 相当」——有技能背书但**无日历时间线/里程碑/带宽**。
- **依赖就绪 24/30**:「厂商监控 API 均为公有云标准接口长期可用。volcengine 监控文档稀缺是唯一外部不确定」——volcengine/华为有探测任务,但华为/AWS 数据可用性未纳入就绪评估。

### 7. Scope Definition — 64/80
- **In-scope 具体 26/30**:6 项均为可交付物。
- **Out-of-scope 明确 21/25**:4 项具名(容量告警二期/华为 region 修复单独件/log tab/成本归因另案)。
- **范围有界 17/25**:把 CDN 调度闸迁移纳入 NAS 提案——「顺带把 CDN 的也改为持久化——一次解决同类问题」——属跨资产线上路径改动(范围蔓延),无 CDN 回归测试/回滚计划。

### 8. Risk Assessment — 54/90
- **风险识别 16/30**:5 项有意义。漏:双来源不一致、监控 API 数据也可能坏、回填配额洪泛、采集语义逻辑风险、CDN 迁移无回滚。
- **可能性+影响 16/30**:评级与正文矛盾——「volcengine 监控指标名不可用 | M | L(仅该厂商无指标)」vs 正文「volcengine 监控指标名无公开文档」「volcengine 监控文档稀缺是唯一外部不确定」;华为「必达项」失败评 L 依赖「预期可查」假设。
- **缓解可执行 22/30**:探测任务成败判定/原子认领/退避重试/告警升级均可执行。

### 9. Success Criteria — 48/80
- **可测可验证 20/30**:「观测 ≥2 天」「≥3 账号同 fs 各留一行 live 测试」「越权传其他租户 account_id 返回 404」可测。扣:SC-3「重启测试 1 次;写失败重试、读失败退避各模拟验证 1 次」统计上弱;SC-7「0 占位」把未知当 0。
- **覆盖完整 20/25**:覆盖全部 6 项 In Scope(InScope-1 接口/模型无独立 SC,隐含于 SC-1/SC-4)。
- **SC 内部一致性 8/25**:CONTRADICTION #1 确证(mutual-exclusion);#2/#3 歧义需澄清;#4 方向冲突。SC 集内部不可同时满足。

### 10. Logical Consistency — 55/90
- **方案解决所陈述问题 22/35**:经营视角经新表建立(解决);但「资产表实盘数据质量差」在原始表面(列表页行)未解决,与运营卡同屏展示矛盾数值。
- **Scope↔Solution↔SC 对齐 16/30**:采集语义矛盾破坏 Solution↔SC(SC-4 与日快照取值口径互斥);华为「必达项↔二期补」张力。
- **需求↔方案连贯 17/25**:NFR「采集尽力而为」映射「失败可观测性」良好;但「尽力而为」语义对「必达项」SC-1 是否适用未界定(尽力而为=单厂商失败不影响全流程,SC-1=必达项必须真实写入)。

### 跨维度一致性检查

- 方案(日末态回填)↔ SC(首写生效)互斥 → 在 D2/D9/D10 三处同源扣分。
- 「必达项」→ InScope-2/SC-1 ↔「二期补」→ Key Risks/Next Steps 张力 → D8/D10。
- NFR「采集尽力而为」与 SC-1「必达项真实数据」口径未对齐 → D10。

---

## Phase 3 — Blindspot 猎取(rubric 之外)

1. **[blindspot] 无回滚计划**:持久化日闸 + CDN 闸迁移是生产调度器行为变更,方案只写了失败重试/退避(韧性),无任何回滚/降级路径(如特性开关回退到内存闸)。CTO 画像明确关注「缺失回滚计划」。引文:「顺带把 CDN 的也改为持久化——一次解决同类问题」。
2. **[blindspot] 历史回填无配额节流**:一次性拉取 14~90 天历史 × N 实例 × 3 必达厂商,与 CloudWatch GetMetricData 配额上限直接冲突;方案仅在日闸原子认领处提配额(「对 CloudWatch 这类有 GetMetricData 配额上限的 API 尤其必要」),回填任务的批大小/退避/与首日每日采集的碰撞均未设计。
3. **[blindspot] 指标管线自身无主动告警**:失败可观测性止于「任务 Result 携带」+「前端显示警示」,无「某必达厂商连续 N 天无成功采集 → 升级告警/页面」的自我健康监控;监控系统自身不被监控是经典静默失效。
4. **[blindspot] 必达厂商按实现便利选择,未按真实存储分布选择**:must-have = aliyun/huawei/aws 依据是「标准指标、SDK 已在或轻量引入」;而问题陈述「运维无法按账号/厂商对比存储利用率」未给出实盘 NAS 容量按厂商分布的证据。若租户实际存储集中在 tencent/volcengine,交付的 3/5 覆盖可能错过真实资产,「经营洞察」价值主张被高估。引文:「多云平台已能枚举/同步 5 家厂商的 NAS 文件系统,但没有容量与使用率的经营视角」。

---

## Bias Detection Report(Annotated Blind Review)

- Annotated regions: 10 attack points / ~45 paragraphs = density **0.22**
- Unannotated regions: 6 attack points / ~23 paragraphs = density **0.26**
- Ratio (annotated/unannotated): **~0.85**

注解区攻击密度略低于未注解区,无「对 pre-revision 段落手下留情」的偏向。最高严重度攻击(采集语义互斥)落在注解区(日快照取值口径,`<!-- pre-revised: medium -->`),属于「修订引入新问题」——pre-revision 分别加入「日末态回填」与「首写生效」两条语义,二者未对齐,标记 **conflict-with-pre-revision**(该攻击要求改写其中一条 pre-revision 新增语义)。

---

## ATTACKS

1. **[Logical Consistency / Success Criteria / Solution Clarity]**:采集语义互斥——SC-4「首写生效、日值不可变」与「日末态快照(次日补昨日完整行)」在同行上不可同时满足;实际日值退化为 00:10 初态,厂商最终聚合值永不落库 — 「落库的每日值取**日末态快照**(次日凌晨补采昨日完整行,取厂商当日最终聚合值)」vs「同日行**首写生效**(当日已有行则不覆盖,仅补当日缺失行)」 — 二选一:让「次日补昨日」显式覆盖昨日行且仅保护今日行,或取消「今日初态」写入全由回填决定,并把「日值不可变」的变更窗口写死。`conflict-with-pre-revision`
2. **[Success Criteria]**:SC-1 数量级自检与 SC-5 capacity=0 落库的关系未定义(capacity=0 落在自检区间外)—「每行 capacity 数量级经换算自检通过(真实 >0 级数据,观测 ≥2 天)」vs「capacity=0 异常行落库可见(不继承 CDN 全零跳过过滤)」 — 声明自检对 capacity=0 例外放行并打标,或改写 SC-1 为「非零行数量级通过」。
3. **[Success Criteria]**:运营卡「0 占位 vs 警示」边界未定义,全部采集失败时会把未知伪装成 0 —「全部无数据时显示 0 占位而非空」vs「采集异常时运营卡显示警示而非纯空」 — 定义「采集失败→警示」优先于「无数据→0 占位」。
4. **[Risk Assessment / Logical Consistency]**:华为「必达项」与「二期补」回退方向冲突 —「**必达项:aliyun/huawei/aws 三家**」vs「华为 SFS namespace/指标不可查 | ... | 失败给出二期判定并告警」 — 明确探测失败时 SC-1 处置(阻塞发布 / 条件式 / 华为降级)。
5. **[Risk Assessment]**:volcengine 风险评级与正文自述矛盾(「最大不确定性/唯一外部不确定」却评 M/L)—「volcengine 监控指标名不可用 | M | L(仅该厂商无指标)」vs「volcengine 监控文档稀缺是唯一外部不确定」 — 拉齐评级与自述,至少评 H/M 并量化「仅该厂商无指标」对租户覆盖的影响。
6. **[Problem Definition]**:紧迫性「成本浪费/容量瓶颈事故」无证据、无量化,属未声明假设当事实 —「存储是成本与容量敏感资产:NAS 扩容/治理滞后直接带来成本浪费或容量瓶颈事故」 — 补充成本浪费/瓶颈的近失案例或量化口径。
7. **[Industry Benchmarking]**:行业方案引用泛化、无具名开源/发布模式;Prometheus 替代以单句否定、近稻草人 —「独立监控系统(Prometheus+云监控 exporter) | 行业常见 | 指标丰富 | 需部署/维护新系统,与资产平台割裂 | Rejected:重资产,过度」 — 补具名参考(Zabbix/Prometheus exporter/云厂商监控最佳实践)与量化权衡。
8. **[Feasibility]**:时间线仅「5 个任务」,无日历/里程碑/带宽 —「预计 5 个任务与 CDN 指标 T3-T5 相当」 — 给出任务拆解与日历时间线。
9. **[Feasibility]**:「监控 API 返回非零」未验证假设(实盘华为/AWS 资产表为 0)—「aws 的 EFS StorageBytes 有标准指标、并非不可行,不套用 CloudFront「主动放弃」先例」 — 把「监控 API 对实盘实例返回非零」加入探测任务验收项。
10. **[Requirements Completeness]**:Top 端点无结果数/分页/响应契约 —「`GET /assets/nas/top?account_id=&days=&sort=`(账号视角 Top)」 — 补 Top N、分页与响应结构定义。
11. **[Success Criteria]**:SC-3 测试方法统计上不足以证明「不再重复提交」—「重启测试 1 次;写失败重试、读失败退避各模拟验证 1 次」 — 明确重启次数、故障注入方式与通过判定。
12. **[Logical Consistency]**:双数据来源:列表页行展示资产表坏值,运营卡展示指标表好值,同屏矛盾 —「资产表虽有 capacity/used_capacity 字段,但实盘数据质量差」+「NAS 列表页顶部加运营卡(总容量/已用容量/平均使用率,仿 CDN 近2日卡)」 — 声明以指标表为准并统一前端展示,或把资产表字段修复纳入范围。
13. **[blindspot]**:无回滚计划(持久化日闸 + CDN 闸迁移为生产行为变更)—「顺带把 CDN 的也改为持久化——一次解决同类问题」 — 加特性开关回退到内存闸 + 回滚验证步骤。
14. **[blindspot]**:历史回填无配额节流,与 CloudWatch GetMetricData 配额冲突 —「一次性历史回填任务,从厂商 API 拉取启用日前 N 天(14~90 天)历史」 — 定义回填批大小/退避/与每日采集的碰撞规避。
15. **[blindspot]**:指标管线自身无主动告警升级,静默失效可数周无人察觉 —「任务 Result 携带(扩展 CDN `skipped_providers` 雏形为含错误明细的结构),运营可查」 — 加「必达厂商连续 N 天零成功采集 → 告警升级」的健康监控。
16. **[blindspot]**:必达厂商按实现便利选而非真实存储分布,部分租户(仅 tencent/volcengine)无经营视角且无覆盖承诺 —「多云平台已能枚举/同步 5 家厂商的 NAS 文件系统,但没有容量与使用率的经营视角」 — 补充实盘容量按厂商分布证据,并对 tencent/volcengine 覆盖给出截止判定或显式降级承诺。

---

## 备注

- 未触碰 DOC_DIR 外文件;本报告仅写入 eval/iteration-1.md。
- 最高优先修复项:ATTACK #1(采集语义互斥,阻塞性逻辑缺陷)+ #4(必达项处置)+ #13(回滚)。
