# Iteration 2 Report — Adversarial Rubric Scoring (CTO)

**提案**: docs/proposals/nas-ops-insight/proposal.md
**审查专家**: CTO(experts/scorer/cto.md persona)
**日期**: 2026-09-17
**类型**: proposal | **标尺**: 1000 | **目标**: 900 | **迭代**: 2/3
**模式**: Annotated Blind Review(pre-revision 已执行,marker 已用于注意力分配)

## Eval-Proposal Complete

**Final Score**: 819/1000 (target: 900)
**Iterations Used**: 2/3

### Score Progression

| Iteration | Score | Delta |
|-----------|-------|-------|
| 1 | 695 | — |
| 2 | 819 | +124 |

### Dimension Breakdown (final)

| # | Dimension | Score | Max |
|---|-----------|-------|-----|
| 1 | Problem Definition | 96 | 110 |
| 2 | Solution Clarity | 108 | 120 |
| 3 | Industry Benchmarking | 90 | 120 |
| 4 | Requirements Completeness | 98 | 110 |
| 5 | Solution Creativity | 65 | 100 |
| 6 | Feasibility | 79 | 100 |
| 7 | Scope Definition | 69 | 80 |
| 8 | Risk Assessment | 75 | 90 |
| 9 | Success Criteria | 68 | 80 |
| 10 | Logical Consistency | 71 | 90 |

### Outcome

Target NOT reached — 819 < 900,继续迭代。

---

## 上一轮 16 个攻击点处置清单

| # | iteration-1 攻击点 | 处置 | 依据 |
|---|--------------------|------|------|
| 1 | 采集语义互斥(首写生效 vs 日末态) | **resolved** | 第 85-87 行显式分离「今日行首写生效 / 昨日行次日覆盖更新 / 冻结窗口写死」,双向推导不再互斥 |
| 2 | SC-1 数量级自检 vs SC-5 capacity=0 | **resolved** | 第 71 行「零行例外放行+打标(qc_status=zero_exception)」;SC-1 改写为「非零行自检、零行走 SC-5」 |
| 3 | 运营卡「0 占位 vs 警示」边界 | **resolved** | 第 101 行判定顺序「采集失败→警示」优先于「无数据→0 占位」;SC-7 同步 |
| 4 | 华为「必达项 vs 二期补」方向冲突 | **resolved** | 第 229/256 行「华为探测失败→降级尽力而为→SC-1 条件式达标」 |
| 5 | volcengine 评级与自述矛盾 | **resolved** | Key Risks 改 M/M,正文「最大外部不确定」拉齐 |
| 6 | 紧迫性无量化 | **partially** | 第 27 行给量化公式 + 探测任务顺带产出高水位实例数;但实盘数字仍待探测,当前页面无具体数 |
| 7 | 行业引用泛化 / Prometheus 稻草人 | **partially** | 第 146-147 行具名 Zabbix/aws-cloudwatch-exporter/aliyun-exporter;Prometheus 权衡加实质 Pros/Cons;仍无来源链接 |
| 8 | 时间线无日历/里程碑 | **resolved-but-flawed** | 第 170-180 行任务表+里程碑齐备,**但里程碑日期与依赖链冲突**(新 attack #1) |
| 9 | 「监控 API 返回非零」未验证 | **resolved** | 第 255 行探测任务验收项「非零且数量级正确,零值视为探测未通过」 |
| 10 | Top 端点无契约 | **resolved** | 第 48 行完整契约(top/page/page_size/响应结构/items 字段/data_status) |
| 11 | SC-3 统计不足 | **resolved** | 第 237 行「3 次重启 × 各仅 1 条 + 写失败/读失败故障注入 + 通过判定」 |
| 12 | 双数据来源同屏矛盾 | **resolved** | 第 50 行数据来源声明(指标表唯一来源)+ Out of Scope 资产表修复另立 |
| 13 | 无回滚计划 | **resolved** | 第 46 行特性开关回退内存闸 + 记录原因/重新开启计划;SC-3b 回滚验证 |
| 14 | 回填无配额节流 | **resolved** | 第 90 行分片批大小 ≤5 实例×≤10 天、退避、01:30~06:00 错峰、配额换算留 30% 余量 |
| 15 | 无主动告警 | **resolved-but-flawed** | 第 104 行自我健康监控(连续 N 天零成功→升级告警);**但零 NAS 实例厂商会误报**(新 attack #6) |
| 16 | 必达按实现便利选 | **resolved** | 第 38 行双因素(指标标准化×实盘容量分布)+ 第 18 行证据缺口自省 + 第 257 行分布统计 |

**结论**:16 点中 13 点 resolved、2 点 partially、2 点 resolved-but-flawed(带新问题)。修订质量高,但**修订本身引入了 5 个新问题**(见 attack #1/#2/#3/#6/#10 与 blindspot #11)。

---

## Phase 1 — 推理审计(独立立场)

### 1.1 Problem → Solution

问题(5 家 NAS 无经营视角 + 资产表坏值)与方案(新指标表 + 5 厂商直采 + 持久化日闸 + 前端)现已**大体对齐**。iteration-1 的「声称解决却重蹈」模式已化解:第 50 行数据来源声明把 NAS 界面统一到指标表,资产表坏值不再同屏;资产表修复明确出界。残缺点:**「峰值由 MAX 派生」声称的缓解不成立**——日值取日末态/今日初态,MAX 对日值聚合只能得到「日值最大值」,无法恢复日内尖峰,「运营卡近 N 天峰值与趋势图尖峰」的承诺与每日快照架构不匹配(见 attack #7)。

### 1.2 Solution → Evidence

「监控 API 对实盘实例返回非零」这一关键假设已从"未声明假设当事实"升级为**显式验收项**(第 255 行),且「必达 vs 尽力而为」分组已挂上实盘容量分布的证据缺口自省(第 18 行)+ 探测任务产出(第 257 行)。证据链完整性显著提升。残余:华为/AWS 探测**通过与否直接决定 SC-1 形态**,而 SC-1 正文未内联该条件(见 attack #5)。

### 1.3 Evidence → Success Criteria

SC 可测性显著提升(3×重启、故障注入、live 测试、404 测试、回滚测试)。SC-1 仍有两处口径未闭合:「观测 ≥2 天持续写入」对窗口内瞬时故障(厂商 API 宕机)的容忍未定义(attack #8);华为条件式未内联(attack #5)。

### 1.4 自查矛盾(iteration-2 修订后复核)

**iteration-1 最高严重度矛盾(采集语义互斥)已消解。** 现语义:
- 今日行:首写生效,当日内可变,次日 00:10 前保持首写值;
- 昨日行:次日补采显式覆盖为厂商最终聚合值,补采完成后冻结不可变;
- 冻结窗口以 Asia/Shanghai 自然日为准。

双向推导均闭合,无互斥。**新发现一处矛盾(攻击 #1,日历 vs 依赖链)**:任务表依赖列「执行器 | 3~4 天 | 依赖适配器」,1 名后端串行下最早 T+10~12 完成,而里程碑声明 M2 = T+7~9;「回填+读取接口+前端 4~5 天 | 依赖执行器」串行下最早 T+15~17,而 M3 = T+12~14。里程碑日期与任务依赖链**不可同时满足**——这是修订新增时间线章节引入的新矛盾。

---

## SC 一致性深挖

按受影响区域聚类(与 iteration-1 同构):
- 聚类 A — `ecam_nas_metric` 表:SC-1/SC-4/SC-5/SC-7 + InScope-4
- 聚类 B — 调度器/日闸(`scheduler_state`/`nas:collect_metrics`):SC-3/SC-3b + InScope-3/InScope-5
- 聚类 C — 读取接口:SC-8 + InScope-4
- 聚类 D — 前端:SC-6/SC-7 + InScope-6
- 聚类 E — 5 厂商实现:SC-1/SC-2 + InScope-2

```
CLUSTER A 两两复核:
SC-1(非零行自检 + 零行走 SC-5)↔ SC-5(capacity=0 落库可见)  → 兼容(SC-1 显式把零行委托给 SC-5,不再互斥)
SC-4(唯一键 + 首写/覆盖/冻结)↔ SC-1/SC-5 → 兼容(写路径语义对任意 capacity 值一致)
SC-7(去重聚合 + 警示判定)↔ SC-5(空态区分) → 兼容(存储按账号留行,展示按 fs 去重,警示判定以任务 Result 为准)
AMBIGUITY #1: SC-1「aliyun/huawei/aws 三家(必达项)...持续写入」vs Key Risks 表后说明「华为探测失败→SC-1 改写为 aliyun/aws」。
  SC-1 正文未内联华为条件式——SC-1 只在乐观路径可满足;条件处置外置于 SC 文本。flag:ambiguous — requires author clarification。
```

```
CLUSTER B 两两复核:
SC-3(特性开启下重启无重复 + 故障注入)↔ SC-3b(回滚到内存闸后任务仍提交) → 兼容
  (SC-3 限定「持久化日闸生效」态,SC-3b 限定回滚态;两态不同时被测,无双满足冲突)
CLUSTER C:SC-8(越权 404)单条可测;接口只暴露指标表(按 account_id 过滤),fs_id 越权自然落空 → 兼容
CLUSTER D:SC-6(抽屉趋势图/空态)↔ SC-7(运营卡/Top 去重 + 警示) → 兼容
CLUSTER E:SC-1(必达)↔ SC-2(尽力而为不阻塞) → 兼容;AMBIGUITY #1 同属本簇
```

**结论**:iteration-1 的 CONTRADICTION #1/#2/#3/#4 全部消解。现存 1 处 ambiguity(SC-1 华为条件式内联),无 mutual-exclusion。SC 集内部一致性较 iteration-1(8/25)大幅改善(20/25)。

---

## Phase 2 — Rubric 评分(验证立场)

> 立场:每个断言在提供证据前视为未验证。所有扣分均引原文。独立评分,只评页面现状。

### 1. Problem Definition — 96/110
- **问题清晰 36/40**:核心问题无歧义;「资产表坏值 vs 经营视角缺位」双主线边界已由「数据来源声明」划定(指标表为唯一数据来源,资产表修复出界)。
- **证据 36/40**:实盘采样(「huawei `capacity=0, used_capacity=0`,aws `capacity=0, used_capacity=0`,aliyun `capacity=10485760, used_capacity=1`」)、grep 空、空 tab、CDN 对标均为真证据(已核对代码库:CDNMetricQuerier/ecam_cdn_metric/空 tab/`lastMetricsCollectDate` 内存态均属实)。扣:「监控 API 非零」假设的实盘验证仍待探测任务(第 255 行),高水位实例数仍未产出。
- **紧迫性 24/30**:「量化口径」公式(高水位实例数 × 冗余容量 × 单价 × 滞后天数)+「越晚修,历史水位缺失越多」时间敏感论证成立;但公式中的实盘数字(高水位实例数)待探测任务产出,当前页面无具体数。

### 2. Solution Clarity — 108/120
- **方案具体 36/40**:接口签名、唯一键、双端点契约、DAO、执行器、持久化日闸、回填节流均具体。扣:querier 签名缺 region/account(见 attack #2);qc_status 在写路径定义但不在 NASMetric 字段与读取契约中(见 attack #3)。
- **用户可见行为 41/45**:趋势图/运营卡/Top/空态区分/警示/数据来源声明描述充分。扣:「峰值由读取接口按 MAX 聚合派生」对用户可见的尖峰承诺,架构上无法兑现(见 attack #7)。
- **技术方向 31/35**:SDK 分厂商、region 独立(华为)、原子认领 findOneAndUpdate、特性开关、echarts、MongoDB 明确。扣:多 region 账号(aliyun/tencent/aws/volcengine)的查询地域未定义;「活跃账号」未定义。

### 3. Industry Benchmarking — 90/120
- **行业方案引用 28/40**:具名 Zabbix 云监控模板、aws-cloudwatch-exporter、aliyun-exporter、云厂商控制台监控——真实项目名,较 iteration-1 显著改善。扣:「均有官方文档可查」「行业标准路径」仍为断言,无文档/项目 URL 引证。
- **≥3 有意义替代 24/30**:Do nothing / 复用资产表 / Prometheus+exporter / 厂商 API 直采 = 4 个不同路径;Prometheus 不再稻草人(有「分钟级细粒度指标、可扩展告警」的真实 Pros + 两条量化 Cons)。
- **诚实权衡 18/25**:Verdict 仍全部指向自选方案;但 Cons 更实质(「约 1 套基础设施 + 持续运维人力」「两套数据源二次打通」)。
- **选中方案对照基准论证 20/25**:「对齐 CDN 已验证路径,工作量可控」+「字段权威」「无新系统,复用平台」——真实理由充分。

### 4. Requirements Completeness — 98/110
- **场景覆盖 36/40**:happy path + 边界(capacity=0、used>capacity、重启、多账号同 fs、适配器失败、回填限流、运营卡判定顺序、自我健康监控)覆盖优良。扣:「活跃账号」口径未定义(与 CDN「指标采集不依赖 EnableAutoSync」的关系未对齐);多 region 账号场景仅华为声明。
- **NFR 34/40**:租户隔离 404(days 限 1~90、sort 枚举、Top 分页上界)完备;幂等/时区/首写-覆盖-冻结语义写死。扣:读取接口无限流 NFR;可访问性未提(运营后台,弱项);自我健康监控的误报边界(零 NAS 实例厂商)未处理。
- **约束与依赖 28/30**:tencent/aws 新依赖(已核对 go.mod:monitor/cloudwatch 确未引入,volcengine-go-sdk 已在主 SDK)、volcengine 文档稀缺、华为 namespace 运行验证、region 声明、WIP/显式提交均点名。

### 5. Solution Creativity — 65/100
- **相对行业基线新颖性 22/40**:持久化日闸 + 原子认领是真实改进,但属标准分布式调度状态模式;方案自认「直接平移已验证的 CDN 经营洞察模式,不发明新架构」。
- **跨域借鉴 23/35**:findOneAndUpdate 原子认领(分布式认领/锁)、utilization 读取派生(数据完整性)、回填错峰/配额节流(运维模式)。
- **洞察简洁性 20/25**:「状态型每日快照即可表达存储水位」+ 今日/昨日冻结窗口语义简洁。

### 6. Feasibility — 79/100
- **技术可行性 34/40**:CDN 链路全在库可照抄(已核对 interfaces.go/sync_cdn_metrics.go/cdn_metric.go);SDK 依赖低。扣:querier 签名缺 region 对多 region 账号的技术路径未闭合;「峰值由 MAX 派生」是设计缺陷。
- **资源与时间线 20/30**:任务表 + 日历里程碑 + 1 后端/0.5 前端齐备,但**里程碑日期与依赖链冲突**(M2=T+7~9 需执行器,依赖适配器,1 名后端串行下最早 T+10~12;M3=T+12~14 需回填+接口,最早 T+15~17)——见 attack #1。
- **依赖就绪 25/30**:「公有云标准接口长期可用」+ volcengine 最大不确定 + 华为/AWS 非零验证进探测验收。扣:「探测任务 2 天」承载 5 个工作流(volcengine 指标名/华为双 namespace/非零验证/容量分布/高水位统计)偏紧。

### 7. Scope Definition — 69/80
- **In-scope 具体 27/30**:6 项均为可交付物。扣:In Scope-2 的必达/尽力而为分组是 M1 探测定案的**条件结果**,但 In Scope 列表未标注条件性(见 attack #10)。
- **Out-of-scope 明确 23/25**:5 项具名(容量告警二期/华为 region 修复单独件/log tab 现状/成本归因另案/资产表字段修复另立)。
- **范围有界 19/25**:CDN 闸迁移仍属 NAS 提案中的跨资产线上路径改动(范围蔓延),但已用特性开关 + SC-3b 回滚兜住;「若 tencent/volcengine 占比 >15% 且探测可用则升格为必达项」为潜在范围膨胀点,由 M1 gate 收敛但未标注其对 SC-1 的影响。

### 8. Risk Assessment — 75/90
- **风险识别 24/30**:5 行风险 + 必达项失败处置 + 覆盖量化口径。漏:自我健康监控对零 NAS 实例厂商的误报风险(新增规则自身引入);读取接口无限流/滥用风险。
- **可能性+影响 25/30**:volcengine M/M 与正文拉齐(修复 iteration-1 矛盾);华为「必达项,预期可查」评 M/M 诚实;非全 L/H 评级。
- **缓解可执行 26/30**:探测任务成败判定、原子认领、指数退避重试、告警升级、回滚开关、升格/降级阈值(>15%)均可执行。

### 9. Success Criteria — 68/80
- **可测可验证 26/30**:「观测 ≥2 天」「3 次重启×各仅 1 条」「写/读失败故障注入 + 通过判定」「≥3 账号同 fs 各留一行 live 测试」「越权 404」「回滚验证」全部可写成测试。扣:SC-1「持续写入」对观测窗口内瞬时厂商故障的容忍未定义(见 attack #8)。
- **覆盖完整 22/25**:覆盖全部 6 项 In Scope(InScope-1 接口/模型无独立 SC,隐含于 SC-1/SC-4)。
- **SC 内部一致性 20/25**:iteration-1 的 4 处矛盾全部消解;现存 1 处 ambiguity(SC-1 华为条件式未内联,AMBIGUITY #1)。

### 10. Logical Consistency — 71/90
- **方案解决所陈述问题 28/35**:经营视角经新表建立(解决);资产表坏值不再同屏(数据来源声明,解决「重蹈」)。扣:峰值缓解声称(MAX 派生)架构上不可兑现——「声称解决却未解决」的残余模式。
- **Scope↔Solution↔SC 对齐 23/30**:采集语义已闭合;SC-1 华为条件式未内联、In Scope-2 条件性未标注、tencent/volcengine 升格路径未同步 SC-1 更新——三处均为「条件逻辑外置、正文未内联」的同类残差。
- **需求↔方案连贯 20/25**:NFR「采集尽力而为」映射「失败可观测性」路径良好;但「尽力而为(单厂商失败不阻塞)」与 SC-1「必达项持续写入(观测 ≥2 天)」在观测窗口内瞬时故障情形下的边界未界定。

### 跨维度一致性检查

- 采集语义已闭合 → 不再在 D2/D9/D10 同源扣分。
- 日历时间线与任务依赖链冲突 → 新矛盾,D6 一处扣分(attack #1)。
- SC-1 华为条件式外置 → D8(处置有声明)/D9(内部一致性 ambiguity)/D10(未内联)三处同源。
- qc_status 写/读不对称 → D2(技术方向)一处扣分(attack #3)。
- 峰值 MAX 缓解不成立 → D2(用户可见行为)/D10(解决所陈述问题)两处同源。

---

## Phase 3 — Blindspot 猎取(rubric 之外)

1. **[blindspot] CDN 闸迁移的回归深度不足**:SC-3 只测「重启不再重复提交」、SC-3b 只测「回滚后任务仍提交」,均针对**提交行为**;未测迁移后 CDN 指标**值**的正确性(仍按 days=2 语义正确写入、无历史缺口)。且首部署时 `scheduler_state` 无 cdn 记录 → last_date 为空 → 触发一次提交的过渡行为未定义。CTO 关注「跨资产线上路径改动缺回归」。引文:「顺带把 CDN 的也改为持久化——一次解决同类问题」+ SC-3「重启与注入场景均无重复提交」。改进:加「迁移后 CDN 日值正确性回归(对比迁移前后同域同日值)」与首部署过渡行为定义。
2. **[blindspot] 运营卡「取最新日期、容量最大的那一行」双键优先级与跨账号口径未定义,聚合值可能高估共享容量**:同一 fs 被多账号看到时各账号容量口径可能不同(共享配额/计量口径),「容量最大」的排序键未定义优先级(先日期还是先容量?);「避免共享容量被双计」只解决重复计数,不解决口径高估。引文:「多账号并存时取最新日期、容量最大的那一行,避免共享容量被双计、Top 失真」。改进:定义排序优先级(日期 desc 后再容量 desc)与「该行容量代表物理 fs 容量」的口径声明。
3. **[blindspot] 特性开关回滚的触发判定无载体**:「若 mongo 日闸出现不可恢复故障,一键切回」——「不可恢复故障」由谁/以什么指标判定(连续 N 次写失败?读不可用多久?)未定义;回滚是手动运维动作,但监控/触发该判定的是否就是新加的自我健康监控通道,未说明。引文:「若 mongo 日闸出现不可恢复故障,一键切回 CDN/NAS 原内存闸」。改进:把回滚判定条件与自我健康监控告警通道绑定,写死触发阈值。

---

## Bias Detection Report(Annotated Blind Review)

- Annotated regions: 11 attack points / 22 paragraphs = density **0.50**
- Unannotated regions: 1 attack point / 39 paragraphs = density **0.03**
- Ratio (annotated/unannotated): **~19.4**

注解区攻击密度显著高于未注解区。解释:iteration-2 的修订把所有新逻辑集中在 22 个注解段中,而**修订引入的 5 个新问题(日历-依赖链冲突、querier 缺 region、qc_status 写读不对称、自我健康监控误报、In Scope 条件性未标注)全部落在注解区**——annotation 未对修订段落手下留情,最高严重度新攻击恰在注解区。未注解区(39 段)为 iteration-1 已打磨的存量内容,仅存「活跃账号」一处低密度残差。无 conflict-with-pre-revision 标记(本轮的 pre-revision 修订方向与我的判断一致,问题出在修订自身的新增逻辑,而非方向矛盾)。

---

## ATTACKS

1. **[Feasibility]**:日历时间线与任务依赖链冲突——里程碑声明「M2 = 指标落库 + 日闸通过重启测试(T+7~9 天)」「M3 = 前端 + 回填完成、发布评审(T+12~14 天)」 vs 任务表「执行器... | 3~4 天 | 适配器」「回填 + 读取接口 + 前端 | 4~5 天 | 执行器」,1 名后端串行下执行器最早 T+10~12、M3 最早 T+15~17,均晚于里程碑 — 重算里程碑(如 M2=T+10~12、M3=T+15~17)或明示适配器/执行器/回填的并行安排(需 >1 后端)。
2. **[Solution Clarity]**:querier 接口签名缺 region/account,而 NAS 是地域性资源 — 「`GetNASMetrics(ctx, fsID, fsName, startDate, endDate)`」(仿 CDN「CDN 为全局服务,region 不参与过滤」) vs 「华为指标路径用实例真实 region」 — 接口补 region(或定义 fs_id→region 解析),并定义 aliyun/tencent/aws/volcengine 多 region 账号的查询地域,否则非 cn-north-4 之外的多地域实例查错地域。
3. **[Solution Clarity / Logical Consistency]**:qc_status 写路径有、读路径无——「标记 qc_status=zero_exception 后正常落库」 vs NASMetric 字段「fs_id / date / capacity(GB) / used_capacity(GB)」无 qc_status、读取契约只有 data_status — 在 types.NASMetric 与读取响应中定义 qc_status(或把零异常映射进 data_status),让「capacity=0 是异常」在运营视图可分辨,否则 SC-5「落库可见」止于 DB 层。
4. **[Requirements Completeness]**:「活跃账号」未定义,与 CDN「指标采集不依赖账号的 EnableAutoSync」(auto_sync_metrics.go)口径可能不一致 — 「`nas:collect_metrics`:按活跃账号遍历 NAS 实例」 — 定义「活跃账号」口径并说明与 EnableAutoSync 的关系,否则非活跃账号下 NAS 实例静默漏采。
5. **[Success Criteria]**:SC-1 未内联华为条件式——「aliyun/huawei/aws 三家(必达项)的 NAS 容量/使用率指标持续写入...观测 ≥2 天」 vs 表后说明/Next Steps「华为探测失败→降级为尽力而为→SC-1 改写为 aliyun/aws」 — 在 SC-1 内联「(华为以探测通过为前提)」限定,并把 In Scope-2 标注「以 M1 探测定案为准」。
6. **[Feasibility / Risk Assessment]**:自我健康监控对零 NAS 实例厂商误报——「对必达厂商(aliyun/huawei/aws)增加「连续 N 天(默认 3 天)零成功采集 → 升级告警」规则」 — 增加「该厂商存在 ≥1 个 NAS 实例」前置条件,否则无 NAS 实例的厂商每 3 天稳定误报,告警疲劳反而掩盖真实失效。
7. **[Logical Consistency]**:「峰值由读取接口按 MAX 聚合派生」不能从日末态快照恢复日内尖峰——「会系统性漏报当日高水位——扩容判断依赖运营卡「近 N 天峰值」与趋势图尖峰(峰值由读取接口按 MAX 聚合派生,不落库)」 — 明确「近 N 天峰值」= 日值序列最大值(承认不捕获日内尖峰),或把日内峰值捕获(更高频采样/峰值落库)列为二期,避免缓解声明与架构不符。
8. **[Success Criteria]**:SC-1「持续写入(观测 ≥2 天)」与 NFR「采集尽力而为」对观测窗口内瞬时故障的容忍未定义——「aliyun/huawei/aws 三家...持续写入 `ecam_nas_metric`...观测 ≥2 天」 vs 「采集尽力而为:单厂商失败只影响该厂商」 — 定义 SC-1 在窗口内允许的单厂商失败次数/不变量(如「≥2 天窗口内每厂商 ≥1 天成功写库」),否则验收期一次厂商 API 宕机即致 SC-1 不可达。
9. **[Industry Benchmarking]**:具名参考仍无来源引证——「云厂商控制台均提供 NAS 监控...均有官方文档可查」 — 补官方文档 URL / exporter 项目链接与版本,把「行业标准路径」从断言转为可核查引证。
10. **[Scope Definition / Logical Consistency]**:In Scope-2 的必达/尽力而为分组未标注 M1 条件性,升格路径未同步 SC-1——「5 厂商 NAS 监控实现(必达项 aliyun/huawei/aws + 尽力而为项 tencent/volcengine)」 vs 「若 tencent/volcengine 任一占比 >15% 且探测可用,升格为必达项纳入本期」 — In Scope-2 与 SC-1 均标注「以 M1 探测定案为准」,并预写升格时 SC-1 的更新方式。
11. **[blindspot]**:CDN 闸迁移回归深度不足,只测提交行为不测指标值正确性 — 「顺带把 CDN 的也改为持久化——一次解决同类问题」 + SC-3/SC-3b 仅覆盖「不重复提交/任务仍提交」 — 加「迁移后 CDN 日值正确性回归(对比迁移前后同域同日值无缺口)」与首部署 scheduler_state 无 cdn 记录的过渡行为定义。
12. **[blindspot]**:运营卡「取最新日期、容量最大」双键优先级与跨账号容量口径未定义,聚合值可能高估共享容量 — 「多账号并存时取最新日期、容量最大的那一行,避免共享容量被双计、Top 失真」 — 定义排序优先级(日期 desc 再容量 desc)与该行容量作为物理 fs 容量的口径声明,避免「双计已除、口径仍高估」。

---

## 备注

- 本轮已核对代码库事实:CDNMetricQuerier 签名(interfaces.go)、`lastMetricsCollectDate` 内存态(auto_sync_metrics.go)、CDN「同日重采覆盖」+「当日无数据不写库」(cdn_metric.go/sync_cdn_metrics.go)、`skipped_providers` 雏形、asset_cdn_query.go 租户校验、NasDetailDrawer.vue 5 tab 中 monitor/log 无内容分支、go.mod 中 tencent/monitor 与 aws/cloudwatch 未引入——提案证据性描述基本属实(一处小夸大:抽屉 monitor/log tab 实为「功能开发中」占位,非「点击无响应」)。
- 未触碰 DOC_DIR 外文件;本报告仅写入 eval/iteration-2.md。
- 最高优先修复项:ATTACK #1(日历-依赖链冲突,本轮新引入的阻塞性时间线缺陷)+ #5(SC-1 条件式内联,SC 内部一致性唯一 ambiguity)+ #3(qc_status 读侧闭环)。
