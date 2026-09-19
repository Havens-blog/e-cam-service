# Iteration 3 Report — Adversarial Rubric Scoring (CTO)

**提案**: docs/proposals/nas-ops-insight/proposal.md
**审查专家**: CTO(experts/scorer/cto.md persona)
**日期**: 2026-09-17
**类型**: proposal | **标尺**: 1000 | **目标**: 900 | **迭代**: 3/3(最终)
**模式**: Annotated Blind Review(pre-revision 已执行,marker 已用于注意力分配)

## Eval-Proposal Complete

**Final Score**: 864/1000 (target: 900)
**Iterations Used**: 3/3

### Score Progression

| Iteration | Score | Delta |
|-----------|-------|-------|
| 1 | 695 | — |
| 2 | 819 | +124 |
| 3 | 864 | +45 |

### Dimension Breakdown (final)

| # | Dimension | Score | Max |
|---|-----------|-------|-----|
| 1 | Problem Definition | 96 | 110 |
| 2 | Solution Clarity | 114 | 120 |
| 3 | Industry Benchmarking | 103 | 120 |
| 4 | Requirements Completeness | 100 | 110 |
| 5 | Solution Creativity | 68 | 100 |
| 6 | Feasibility | 86 | 100 |
| 7 | Scope Definition | 72 | 80 |
| 8 | Risk Assessment | 79 | 90 |
| 9 | Success Criteria | 71 | 80 |
| 10 | Logical Consistency | 75 | 90 |

### Outcome

Target NOT reached — 864 < 900,迭代 3 为最终轮。修订质量高(12/12 攻击点 resolved),但修订引入/残留 4 个未闭合逻辑点(补昨日失败无重试、SC-2 未条件化、AWS 探测失败无预声明处置、探测任务 2 天过载),外加 2 个 blindspot,距 900 仍差 36 分。

---

## 上一轮 12 个攻击点处置清单

| # | iteration-2 攻击点 | 处置 | 依据 |
|---|--------------------|------|------|
| 1 | 日历时间线与依赖链冲突(M2=T+7~9/M3=T+12~14 vs 串行 T+10~12/T+15~17) | **resolved** | 里程碑改写为「M2=T+10~12(建表 T+4→适配器 T+8→执行器 T+12 串行推导)」「M3=T+15~17」;并行压缩标注「可选,需 >1 名后端」,串行按保守日期承诺 |
| 2 | querier 签名缺 region/account | **resolved** | 签名补 `region`;region 解析按「实例所在 region」逐实例查询,不做全局推断;aliyun/tencent/aws/volcengine 同规则 |
| 3 | qc_status 写读不对称 | **resolved** | 新增「qc_status 读取侧闭环」:写路径 `zero_exception` 在读取响应中原样暴露并映射进 `data_status`,前端可分辨异常零容量 |
| 4 | 「活跃账号」未定义 | **resolved** | 明确「= 租户下已纳管且存在 ≥1 个 NAS 实例的云账号」,且「不依赖 EnableAutoSync 开关」,与 CDN 口径对齐 |
| 5 | SC-1 未内联华为条件式 | **resolved** | SC-1 内联「(必达项清单以 M1 探测定案后的分组为准;华为以 M1 探测通过为前提)」+「华为探测失败条件式改写」 |
| 6 | 自我健康监控零实例误报 | **resolved** | 加前置条件「该厂商当前存在 ≥1 个 NAS 实例(实盘枚举非空)」,无实例不触发零成功告警 |
| 7 | 峰值 MAX 不能恢复日内尖峰 | **resolved** | 口径改写为「近 N 天峰值 = 日值序列最大值,不捕获日内尖峰(列二期)」,与日末态快照架构自洽 |
| 8 | SC-1 瞬时故障容忍未定义 | **resolved** | 定义不变量「每必达厂商窗口内 ≥1 天成功写库」,窗口内允许单厂商个别日期失败 |
| 9 | 行业引用无来源引证 | **resolved** | 补 8 条官方文档/项目 URL(AWS CloudWatch EFS、阿里云 CMS、腾讯云、华为 CES、火山、cloudwatch_exporter、aliyun-exporter、Zabbix)+ 版本号 |
| 10 | In Scope-2 条件性未标注 | **resolved** | In Scope-2 标注「以 M1 定案为准」,升格路径预写「SC-1 必达清单随分组同步更新/扩列」 |
| 11 | [blindspot] CDN 迁移回归深度不足 | **resolved** | SC-3 补「CDN 迁移值回归:对比迁移前后同域同日值无缺口、无重复历史」+ 首部署 `scheduler_state` 无 cdn 记录的过渡行为定义 |
| 12 | [blindspot] 运营卡双键优先级与口径 | **resolved** | 明确「日期 desc,再容量 desc」取第一行 + 口径声明「该行 capacity/used 作为物理 fs 容量口径,不做跨账号求和/平均」 |

**结论**:12/12 resolved。修订质量显著高于 iteration-2(iteration-2 是 13/16 resolved + 2 处 resolved-but-flawed + 引入 5 新问题)。但**修订本身仍引入/残留 4 个未闭合点**:① 补昨日失败无重试路径(新,见 attack #1);② SC-2 未随 M1 条件化(新,SC-1 条件化后 SC-2 变陈旧,见 attack #2);③ AWS 探测失败无预声明处置(partial,华为有降级路径 AWS 无,见 attack #4);④ 探测任务 2 天承载 5 工作流(残留,见 attack #3)。

---

## Phase 1 — 推理审计(独立立场)

### 1.1 Problem → Solution

问题(5 家 NAS 无经营视角 + 资产表坏值)与方案(指标表 + 5 厂商直采 + 持久化日闸 + 读取接口 + 前端)已完全对齐。iteration-1 的「声称解决却重蹈」(资产表坏值同屏)已由「数据来源声明」根除——NAS 界面一律以 `ecam_nas_metric` 为唯一数据来源,资产表字段修复明确出界。iteration-2 的「峰值 MAX 声称不成立」已收敛为「近 N 天峰值 = 日值序列最大值,不捕获日内尖峰(二期)」,架构承诺自洽。**残缺点**:每日补昨日失败时,该日值永久停在 00:10 初态,既非「日末态快照」亦无重试路径(见 1.4)。

### 1.2 Solution → Evidence

「监控 API 对实盘实例返回非零」已从假设升级为探测任务显式验收项(「华为/AWS 非零且数量级正确,零值视为探测未通过」);「必达 vs 尽力而为」分组已挂实盘容量分布证据(>15% 阈值 + M1 产出)。证据链闭合。**残余**:AWS 探测失败的后续处置只有「SC-1 须重新评估」的泛化表述,无华为式预声明降级路径(attack #4)。

### 1.3 Evidence → Success Criteria

SC 可测性已达高水位:SC-1 内联条件(必达清单以 M1 定案为准、华为以探测通过为前提、窗口内瞬时故障不变量、升格同步);SC-3 含 3×重启 × 各仅 1 条 + 写/读失败故障注入 + CDN 迁移值回归 + 首部署过渡;SC-4 含 ≥3 账号同 fs live 测试;SC-8 越权 404。**残余**:SC-2 未内联 M1 条件(attack #2),「升格/二期补」两种状态下的 SC-2 语义未定义。

### 1.4 自查矛盾(iteration-3 修订后复核)

**iteration-1 最高严重度矛盾(采集语义互斥)持续闭合。** iteration-2 引入的日历-依赖链冲突已消解(里程碑改为串行可推导日期)。**新发现 1 处逻辑缺口(attack #1)**:

「次日补昨日 = 覆盖更新」+「昨日行在次日补采完成后冻结(跨日不可变)」+「任一适配器失败只返回自身空,不阻塞全流程」三者合读:若某日(如 D)的次日补采(D+1 凌晨)失败,D 行保留 D 日 00:10 初态;后续调度日(D+2)只查 [D+1, D+2],D 行永不再被触碰。结果 D 日值**永久停在初态**,既不是「每日值口径 = 日末态快照」,也不满足「补采完成后冻结」的前提(补采未完成)。文档未定义:补采失败后该行是冻结、重试、还是打标。这是「修订把口径写得越精确、失败路径越暴露」的典型 CTO 场景——口径自洽,失败处置缺失。

双向可满足性复核:今日行首写生效 ↔ 昨日行覆盖更新 ↔ 冻结窗口,三者无互斥(今日行当日内可变,昨日行次日覆盖,覆盖后冻结);SC-1 非零自检 ↔ SC-5 零行可见,显式委托不冲突。**无 mutual-exclusion,1 处失败路径缺口 + 2 处条件性未内联(SC-2/AWS path)。**

---

## SC 一致性深挖

按受影响区域聚类(与 iteration-2 同构):
- 聚类 A — `ecam_nas_metric` 表:SC-1/SC-2/SC-4/SC-5/SC-7 + InScope-4
- 聚类 B — 调度器/日闸(`scheduler_state`/`nas:collect_metrics`):SC-3/SC-3b + InScope-3/InScope-5
- 聚类 C — 读取接口:SC-8 + InScope-4
- 聚类 D — 前端:SC-6/SC-7 + InScope-6
- 聚类 E — 5 厂商实现:SC-1/SC-2 + InScope-2

```
CLUSTER A 两两复核:
SC-1(必达非零自检 + 零行委托 SC-5 + 窗口内瞬时故障不变量)↔ SC-5(零行落库可见)→ 兼容
SC-1(观测 ≥2 天)↔ SC-4(首写/覆盖/冻结)→ 兼容(2 天观测窗口内昨日行补采后冻结,今日行可修正,不冲突)
SC-4(跨账号同 fs 各留一行)↔ SC-7(按 fs_id 去重)→ 兼容(存储按账号留行,展示按 fs 去重)
AMBIGUITY #1:SC-1 已内联 M1 条件(必达清单以定案为准 + 华为前置),但 SC-2「tencent/volcengine 无数据时不报错(尽力而为语义)」未内联 M1 条件。
  若 tencent/volcengine 之一升格为必达(>15% + 探测可用),SC-2 的「尽力而为」表述对升格厂商即失效;若 >15% + 探测不可用则「显式降级为二期补」,SC-2 未定义该状态下的语义。
  SC-1 的「升格同步」只扩列了 SC-1 自身,未同步改写 SC-2。flag:ambiguous — requires author clarification。
```

```
CLUSTER B 两两复核:
SC-3(持久化闸态重启无重复 + 故障注入 + CDN 迁移值回归 + 首部署过渡)↔ SC-3b(回滚内存闸态任务仍提交)→ 兼容(两态不同时被测)
CLUSTER C:SC-8(越权 404)→ 单条可测;接口按 account_id 过滤,外部 fs_id 自然落空 → 兼容
CLUSTER D:SC-6(趋势/空态)↔ SC-7(去重 + 警示判定顺序)→ 兼容
CLUSTER E:SC-1(必达)↔ SC-2(尽力而为不阻塞)→ 基本兼容,受 AMBIGUITY #1 影响(升格厂商下边界不清)
```

**结论**:iteration-2 的 4 处矛盾/ambiguity 全部消解。现存 1 处 ambiguity(SC-2 M1 条件化,新引入)、1 处失败路径缺口(补昨日,新)、1 处条件性未内联(AWS 探测失败,partial)。SC 内部一致性较 iteration-2(20/25)略降至 19/25——SC-1 条件化「更完整」的同时,让未同步的 SC-2 变陈旧,是修订带来的净一致性代价。

---

## Phase 2 — Rubric 评分(验证立场)

> 立场:每个断言在提供证据前视为未验证。所有扣分均引原文。独立评分,只评页面现状。

### 1. Problem Definition — 96/110
- **问题清晰 36/40**:核心问题无歧义;「资产表坏值 vs 经营视角缺位」双主线由「数据来源声明」划定边界。
- **证据 36/40**:实盘采样(「huawei `capacity=0, used_capacity=0`,aws `capacity=0, used_capacity=0`,aliyun `capacity=10485760, used_capacity=1`」)、grep 空、空 tab、CDN 对标均真证据(iteration-2 已核对代码库,本轮未变)。「实盘容量按厂商分布(证据缺口,探测任务补充)」自省诚实。扣:「监控 API 非零」的实盘验证仍待探测任务;高水位实例数仍未产出。
- **紧迫性 24/30**:量化公式(高水位实例数 × 冗余容量 × 容量单价 × 滞后天数)+「越晚修,历史水位缺失越多」论证成立;但公式实盘数字(高水位实例数)待探测产出,页面无具体数。

### 2. Solution Clarity — 114/120
- **方案具体 38/40**:querier 签名补 `region` 且逐实例按实例 region 查询(attack #2 闭合);5 厂商实现、执行器、持久化日闸 + 原子认领、回填节流、双端点契约、qc_status 读侧闭环均具体。扣:实例元数据缺 region 的边界未定义;每日采集(区别于回填)无分片/节流参数。
- **用户可见行为 43/45**:趋势图/运营卡/Top/空态区分/警示判定顺序/数据来源声明描述充分;「近 N 天峰值」口径诚实(不捕获日内尖峰)。
- **技术方向 33/35**:SDK 分厂商、region 独立、findOneAndUpdate 原子认领、特性开关、echarts、MongoDB 明确;多 region 规则对所有厂商统一。

### 3. Industry Benchmarking — 103/120
- **行业方案引用 36/40**:8 条具名参考 + 官方文档 URL + exporter 版本(cloudwatch_exporter ≥ v0.24、aliyun-exporter ≥ v0.9),「行业标准路径」从断言转为可核查引证。
- **≥3 有意义替代 26/30**:Do nothing / 复用资产表 / Prometheus+exporter / 厂商 API 直采 = 4 个不同路径;Prometheus 有真实 Pros(分钟级细粒度、可扩展告警)+ 量化 Cons(约 1 套基础设施 + 持续运维人力、两套数据源二次打通),非稻草人。
- **诚实权衡 20/25**:Verdict 仍全指向自选方案,但 Cons 实质且面向真实约束。
- **选中方案对照基准论证 21/25**:「对齐 CDN 已验证路径,工作量可控」+「字段权威」「无新系统,复用平台」理由充分。

### 4. Requirements Completeness — 100/110
- **场景覆盖 37/40**:happy path + 大量边界(capacity=0、used>capacity、重启、多账号同 fs、适配器失败、回填限流、运营卡判定顺序、自我健康监控零实例前置)覆盖优良。扣:**补昨日失败无重试路径**(attack #1,新缺口);每日采集大规模租户无节流参数。
- **NFR 35/40**:租户隔离 404、days 1~90、sort 枚举、Top 分页上界、幂等/时区/首写-覆盖-冻结语义、失败可观测性路径完备。扣:读取接口无限流 NFR;可访问性未提。
- **约束与依赖 28/30**:tencent/aws 新依赖、volcengine 文档稀缺、华为 namespace 运行验证、region 声明、WIP/显式提交均点名。

### 5. Solution Creativity — 68/100
- **相对行业基线新颖性 24/40**:持久化日闸 + 原子认领是真实改进,但属标准分布式调度状态模式;方案自认「直接平移已验证的 CDN 经营洞察模式,不发明新架构」。双因素(指标标准化 × 实盘容量分布)分组是增量思考。
- **跨域借鉴 24/35**:findOneAndUpdate 原子认领(分布式认领/锁)、utilization 读取派生(数据完整性)、回填错峰/配额节流(运维模式)、读侧 qc_status 闭环(数据质量贯穿读路径)。
- **洞察简洁性 20/25**:「状态型每日快照即可表达存储水位」+ 今日/昨日冻结窗口语义简洁。

### 6. Feasibility — 86/100
- **技术可行性 36/40**:CDN 链路全在库可照抄(iteration-2 已核对 interfaces.go/sync_cdn_metrics.go);region 解析闭合;5 厂商 SDK 依赖低。扣:实例元数据 region 缺失的兜底未定义。
- **资源与时间线 24/30**:里程碑改为串行可推导(M2=T+10~12、M3=T+15~17),依赖链冲突消解;并行压缩标注可选。扣:**探测任务 2 天承载 5 条工作流**(volcengine 指标名 / 华为双 namespace / 华为+AWS 非零验证 / 容量分布统计 / 高水位统计)偏紧(attack #3)。
- **依赖就绪 26/30**:「公有云标准接口长期可用」+ volcengine 最大不确定 + 华为/AWS 非零验证进探测验收。

### 7. Scope Definition — 72/80
- **In-scope 具体 28/30**:6 项均可交付物;In Scope-2 已标注「以 M1 定案为准」。
- **Out-of-scope 明确 23/25**:5 项具名(容量告警二期/华为 region 修复单独件/log tab 现状/成本归因另案/资产表字段修复另立)。
- **范围有界 21/25**:CDN 闸迁移仍属 NAS 提案内的跨资产线上路径改动,但由特性开关 + SC-3b 回滚兜住;「>15% 升格纳入本期」是条件范围膨胀,由 M1 gate 收敛且已同步 SC-1;「二期补」承诺为有界退路。

### 8. Risk Assessment — 79/90
- **风险识别 26/30**:5 行风险 + 必达项失败处置 + 覆盖量化口径 + 自我健康监控零实例前置。漏:补昨日失败(attack #1 对应风险)未入表;读取接口滥用/无限流。
- **可能性+影响 26/30**:volcengine M/M 与正文拉齐;华为「必达项,预期可查」M/M 诚实;非全 L/H。
- **缓解可执行 27/30**:探测任务成败判定、原子认领、指数退避重试、告警升级、回滚开关、>15% 升/降级阈值均可执行。

### 9. Success Criteria — 71/80
- **可测可验证 28/30**:SC-1(不变量「每必达厂商窗口内 ≥1 天成功写库」+ M1 条件 + 升格同步)、SC-3(3×重启×各仅 1 条 + 写/读失败故障注入 + CDN 迁移值回归 + 首部署过渡 + 通过判定)、SC-4(≥3 账号同 fs live 测试)、SC-8(越权 404)全部可写成测试。
- **覆盖完整 22/25**:覆盖全部 6 项 In Scope(InScope-1 接口/模型隐含于 SC-1/SC-4)。
- **SC 内部一致性 21/25**:iteration-2 的 ambiguity 全消解;新引入 AMBIGUITY #1(SC-2 未 M1 条件化)+ AWS 探测失败条件未内联 SC-1(仅「须重新评估」泛化)。

### 10. Logical Consistency — 75/90
- **方案解决所陈述问题 31/35**:经营视角经新表建立;资产表坏值不再同屏;「近 N 天峰值」承诺与架构自洽(声称解决的问题如实解决)。
- **Scope↔Solution↔SC 对齐 24/30**:SC-1/In Scope-2 条件已内联;残留三处条件逻辑外置/未闭合:SC-2 未随 M1 分组更新、AWS 探测失败处置泛化、补昨日失败与「日末态口径」的失败路径缺口。
- **需求↔方案连贯 20/25**:NFR「采集尽力而为」映射「失败可观测性」路径良好;「尽力而为」与 SC-1 瞬时故障不变量已闭合。扣:补昨日失败使「每日值口径 = 日末态快照」在个别日期上不成立,口径与失败处理未对齐。

### 跨维度一致性检查

- 采集语义已闭合 → 不再在 D2/D9/D10 同源扣分。
- 日历-依赖链冲突消解 → D6 恢复。
- 补昨日失败缺口 → D4(场景覆盖)/D8(风险缺位)/D10(口径未对齐)三处同源扣分。
- SC-2 条件化 → D9(内部一致性 ambiguity)/D10(未内联)两处同源。
- AWS 探测失败处置 → D3(对照基准)/D9(SC-1 条件不全)/D10(未内联)弱相关。
- 探测任务 2 天过载 → D6 一处扣分。

---

## Phase 3 — Blindspot 猎取(rubric 之外)

1. **[blindspot] Top/运营卡按 `fs_id` 去重假设 fs_id 全局唯一,未考虑 region-scoped fs_id 跨区域撞号**:「运营卡与 Top 均按 `fs_id` 去重后计数」+ 唯一键 `(account_id, fs_id, date)`。若厂商 fs_id 仅 region 内唯一(非 AWS EFS 的全局唯一 ID 风格),同一账号跨 region 两个不同物理文件系统可能共享同一 fs_id 字符串——存储唯一键撞键(数据覆盖),展示去重错并(物理文件系统被合并)。提案显式处理「多账号同 fs_id(多活/共享)」,却未处理「同账号跨 region 同 fs_id(不同物理 fs)」。改进:确认各厂商 fs_id 唯一域(全局 vs region-scoped);若 region-scoped,唯一键加 region、去重键改 `(region, fs_id)`。
2. **[blindspot] 每日采集无节流/分片,规模租户可能撞 CloudWatch GetMetricData 配额**:「回填配额节流」详尽(批 ≤5 实例×≤10 天、5s 退避、30% 余量),但**每日自动采集**「按活跃账号遍历 NAS 实例 → 调 querier」无任何批大小/退避/配额换算。大租户(数千 EFS 实例)下每日采集与回填虽错峰(00:10 vs 01:30~06:00),但单日采集本身即可撞 60s/5 万指标点配额。改进:每日采集也按「厂商 × 账号」分片 + 批间退避,复用回填节流参数。

---

## Bias Detection Report(Annotated Blind Review)

- Annotated regions: 6 attack points / ~37 paragraphs = density **0.16**
- Unannotated regions: 1 attack point / ~43 paragraphs = density **0.02**
- Ratio (annotated/unannotated): **~7.0**

注解区攻击密度仍高于未注解区,但 ratio 较 iteration-2(~19.4)大幅收窄——iteration-3 修订质量显著提升,每个注解段引入的新问题明显减少。注解区 6 个攻击点全部为「修订新增逻辑自身的缺口」:补昨日失败(日快照章节)、探测任务 2 天过载(时间线章节)、AWS 处置泛化(探测/SC 章节)、读取接口无限流(NFR 章节)、fs_id 撞号(聚合口径章节)、每日采集无节流(执行器章节)。未注解区唯一攻击点(SC-2 未 M1 条件化)实为注解区 SC-1 升级同步的连锁陈旧——修订让 SC-1 更完整、反衬未同步的 SC-2 更陈旧。无 conflict-with-pre-revision 标记(所有攻击与修订方向一致,均指向修订未覆盖的边界,而非方向矛盾)。注解区最高严重度攻击(补昨日失败)恰在 pre-revision 标记为 high 的段落内——annotation 未对修订段落手下留情。

---

## ATTACKS

1. **[Requirements Completeness / Logical Consistency]**:补昨日失败无重试路径,「日末态快照」口径在个别日期上不成立——「落库的每日值取日末态快照(次日凌晨补采昨日完整行,取厂商当日最终聚合值)」+「昨日行在次日补采完成后冻结(跨日不可变)」 vs 「任一适配器失败只返回自身空,不阻塞全流程」 — 若次日补采失败,该行永久停在 00:10 初态(后续调度日只查 [前日,今日],不再触碰),既非日末态也非「补采完成后」的冻结态。定义补采失败处置:后续调度日重试补采 / 打标(如 `qc_status=initial-state-stale`)/ 冻结初态并记录,并纳入 Key Risks。
2. **[Solution Clarity / Logical Consistency]**:SC-2 未随 M1 分组条件化,升格/二期补状态下语义悬空——SC-1「升格同步:若 tencent/volcengine 任一实盘占比 >15% 且探测可用升格为必达项,SC-1 必达清单相应扩列」 vs SC-2「tencent/volcengine 无数据时不报错、不阻塞(尽力而为语义)」 — SC-2 同样标注「以 M1 定案分组为准」,并预写三种状态语义:升格为必达 → 按 SC-1 观测;维持尽力而为 → 现语义;>15% 但探测不可用 → 「二期补」下本期不采集、SC-2 相应改写。
3. **[Feasibility]**:探测任务 2 天承载 5 条工作流过载,而它是发布 gate——「探测任务 | 2 天 | 无,最高优先 | M1 探测报告 + 必达/尽力而为定案(发布 gate)」 vs 验收项「volcengine 指标名 / 华为 SFS+SFS_Turbo 双 namespace / 华为+AWS 非零验证 / 实盘容量分布统计 / 高水位实例数」 — 拆分为「探测(指标名/namespace/非零验证)」与「统计(分布/高水位)」两条任务,或明示 2 天内并行执行方式与样本上限,避免 M1 gate 依赖一条必然超载的任务。
4. **[Industry Benchmarking / Success Criteria]**:AWS 探测失败无预声明处置路径(仅华为有降级路径)——「确认华为/AWS 监控 API 对实盘这批 capacity=0 的实例返回非零…零值结果视为探测未通过并记录原因」 + 「华为探测失败条件式改写:华为降级为尽力而为项,SC-1 改写为「aliyun/aws 必达」」 — AWS 零值同样预声明处置(降级为尽力而为?SC-1 改写为「aliyun 必达」?),否则「监控 API 数据是好的」前提在 AWS 上不成立时,SC-1 只有「须重新评估」的泛化退路。
5. **[Requirements Completeness]**:读取接口无限流 NFR——「GET /assets/nas/metrics?fs_id=&account_id=&days=」+「GET /assets/nas/top?…」,NFR 仅覆盖鉴权/幂等/时区,未提限流 — 补「读取接口按租户 QPS 限流」NFR,对齐安全基线(运营后台暴露于内网也应有防御上限)。
6. **[blindspot]**:Top/运营卡按 `fs_id` 去重假设全局唯一,region-scoped fs_id 跨区域撞号会导致存储撞键 / 展示错并 — 「运营卡与 Top 均按 `fs_id` 去重后计数」+ 唯一键 `(account_id, fs_id, date)` — 确认各厂商 fs_id 唯一域;若 region-scoped,唯一键加 region、去重键改 `(region, fs_id)`。
7. **[blindspot]**:每日采集无节流/分片,大租户可撞 CloudWatch GetMetricData 配额——「回填配额节流(批 ≤5 实例 × ≤10 天、5s 退避、30% 余量)」详尽,但「`nas:collect_metrics`:按活跃账号遍历 NAS 实例 → 调 querier → 写入」无批大小/退避 — 每日采集复用回填的「厂商 × 账号」分片 + 批间退避参数,避免规模租户单日采集撞配额。

---

## 备注

- 本轮已核对代码库事实与 iteration-2 一致(CDNMetricQuerier 签名、`lastMetricsCollectDate` 内存态、CDN「同日重采覆盖」、`skipped_providers` 雏形、asset_cdn_query.go 租户校验、NasDetailDrawer.vue 空 tab、go.mod 中 tencent/monitor 与 aws/cloudwatch 未引入);iteration-3 的新增内容为设计/流程/SC 改写,无新代码库声称需复核。
- 行业引用 8 条 URL(官方文档 + exporter 项目)均为真实可核查来源,版本号与撰写时态一致。
- 未触碰 DOC_DIR 外文件;本报告仅写入 eval/iteration-3.md。
- 最高优先修复项(若继续迭代):ATTACK #1(补昨日失败处置,口径自洽但失败路径缺失)+ #2(SC-2 条件化,SC 内部一致性唯一 ambiguity)+ #4(AWS 探测失败预声明处置)。
