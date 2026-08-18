# 设计评估报告 — Iteration 2

> 评估对象：`docs/features/ssl-cert-management/design/`（tech-design.md + er-diagram.md + schema.sql + api-handbook.md）
> 上游 PRD：prd-spec.md（916/1000 通过）、prd-user-stories.md
> 评分量表：`forge/eval/rubrics/design.md`（1000 分制，target 900，Breakdown-Readiness ★ 闸门 ≥160/180）
> Iteration = 2。仅评分当前页面上的内容，无改进信用。
> 评估立场：对抗式——每项扣分均附文档原文引用。

## 前置核对：Iteration-1 遗留问题消解状态

| Iteration-1 问题 | 当前状态 |
|------------------|----------|
| 服务级类型未定义（ChangeList/DeployResult 等） | 已修复：tech-design 新增 "Service-Level Types" 13 个结构体字段级定义 |
| schema.sql 不可执行 | 已修复：改为 mongosh 可直接执行（createCollection + createIndex + $jsonSchema 校验器） |
| 跨层命名冲突（EncryptedKey 三名） | 已修复：统一 `encryptedPrivateKey` |
| 错误码跨文档不闭合（缺 CERT_HAS_REFS/SCAN_IN_PROGRESS/FORBIDDEN） | 已修复：tech-design 13 码与 api-handbook 完全对齐 |
| 异步码 HTTP 503 映射模糊 | 已修复：新增 "同步错误 vs 异步子任务状态" 节 |
| 互斥不变量无 DB 强制（blindspot 1） | 已修复：activeMutex token + 部分唯一索引 + 原子清除 |
| 覆盖率分母来源未设计（blindspot 2） | 已修复：internal/asset 独立盘点数据源 + 数据流契约 + 失效处理 |
| 主密钥轮换迁移缺失（blindspot 3） | 已修复：五步迁移流程（双活/双读/再加密/回滚/下线） |
| 验证窗口告警路由未建模（blindspot 4） | 已修复：change_linked_diff + verifyExpected + verifyWindowRoute |
| 审计查询端点缺失 | 已修复：`GET /api/v1/certs/changes/:id/audit` |
| 引用三态（未发现引用 ≠ 无引用） | 已修复：referenceStatus 三态派生字段 |
| E2E 测试层缺失 | 已修复：e2e 行 + 4 条 Key E2E Flows |
| 到期分级计算组件未单列 | **未修复**（仍隐于 ProbeService/看板） |
| 自定义 CRD 登记机制未设计 | **未修复** |
| K8s 管理权判定规则与复检时窗未量化 | **部分修复**（新增 Reason/RecheckPassed 字段占位，规则集与时窗仍未给） |
| 项级回滚失败状态缺口 | **部分修复**（订单级 rollback_failed 已有，项级 status 枚举仍无 rollback_failed） |

本轮新增发现集中在**分批灰度语义与 PRD 直接冲突**、**PRD 覆盖率双指标缺失**、**schema 阈值界与业务界冲突**、**状态机"取消"态跨文档缺失**。

---

## Phase 1 — Reasoning Audit（推理审计）

### 1.1 PRD→Design 覆盖追踪

绝大多数 PRD 要求有设计落点（见 PRD Coverage Map 14 行）。仍存在的缺口与冲突：

- **【冲突】分批灰度语义双重违反 PRD**：
  - PRD（In Scope + Story 3 AC）："首批执行且验证通过后方可继续执行剩余批，**剩余批同样需人工确认后执行**"。设计 `BatchConf.PauseBetween` 注释为 "true=批间暂停待人工续批；**false=自动续批**"——提供了绕过人工确认的自动续批模式，直接违反 PRD 的强制人工确认 AC。
  - PRD 要求"首批执行**且验证通过后**"方可续批；设计 E2E 流为 "分批灰度续批：批间暂停→progress 轮询→人工续批→**尾批完成→verifyWindowUntil 设置**→窗口达标"——验证窗口仅在尾批完成后开启，批间无验证门控，"首批验证通过"闸门在设计流程中不存在。
- **【缺口】PRD 覆盖率双指标未设计**：Story 4 AC "展示**登记覆盖率**与**可更换托管覆盖率**两个指标，'仅指纹登记'占比单独可见"。dashboard 响应仅 `{summary{countsByLevel[5],diffAlertCount,exemptCount}, items[], lastInspectionAt}`，全文档无任何端点/模型/计算口径承载这两个指标（ScanSnapshot.coverageMeta 是"资源覆盖率"，与"证书登记覆盖率"分母口径不同）。
- **【缺口】自定义 CRD 登记机制未设计**：PRD "其余自定义资源按判定规则纳入——凡 spec 中含云托管证书 ID/名称引用字段的网关类资源，**经登记后纳入扫描范围**"。设计仅有 blind_spot 声明兜底，登记的数据模型/API/UI 均缺失。
- **【缺口】到期分级告警链路仍未建模**：30/14/7 分级计算仅隐于看板 `daysLeft/level` 字段，分级告警的触发、去重（避免每天巡检重复告警）、路由无任何设计（thresholds 无到期字段，无 lastNotifiedLevel 类状态）。
- **【缺口】回滚目标有效性校验无接口支撑**：PRD "回滚前校验回滚目标有效性（云侧证书库中旧证书仍存在且未过期）"。CloudDeployer 仅 4 方法（UploadCert/BindResource/ListReferences/CleanupOrphan），无"查询云证书库中指定证书是否存在且未过期"的能力方法；ROLLBACK_TARGET_INVALID 错误码有定义、产生它的探测能力无落点。
- **【时序矛盾】验证窗口达标确认与天级探测频率冲突**：设计要求 "verifyExpected.domains 全部探测一致且**连续达标次数 ≥ thresholds.verifyConfirmProbes（默认 2）**→ 窗口提前达标关闭"，而 PRD DF006 探测频率为 "**天级探测**"。验证窗口 2~24h 内按天级频率最多 1~2 次探测，"连续 2 次达标提前关闭"在短窗口下不可达；设计未定义验证窗口内的加密探测节奏（提频探测）。

### 1.2 隐式耦合 / 静默错误 / 消除模式复入 / 并发 / 迁移

- **互斥活性（liveness）未设计**：activeMutex 进入活跃态写入、终态清除，但 8 态枚举无"取消"态（见 Phase 2 D2），暂停在 executing 的分批单（操作者永不回来续批）将无限期持有互斥 token，阻塞该证书一切后续变更，无超时/中止路径。
- **豁免域名 ∩ 验证窗口 = 永不达标**：豁免域名不参与探测（"豁免不告警"），而窗口达标条件是 "verifyExpected.domains **全部**探测一致"——若变更目标含豁免域名，该窗口永远无法提前达标，只能超时转部分完成，形成语义死锁。设计未声明豁免域名从 verifyExpected.domains 排除或标记 skipped。
- **双 protectUntil 所有权未定义**：Certificate.protectUntil 与 ChangeOrder.protectUntil 同时存在（er-diagram 两处），禁删判定依据哪一个、两者如何同步未声明，属双源真值。
- **ChangeItem 无批次归属字段**：batchInfo 在订单级 {totalBatches, currentBatch, batchSize}，但 ChangeItem（schema 与 ChangeListItem）均无 batchNo/批次分配字段——"执行第 N 批"时哪些项属于该批无从确定，批次组成算法（排序？分组？）未设计。
- 无消除模式复入；无存量数据迁移需求（新功能域），主密钥轮换迁移已设计，此项通过。

---

## Phase 2 — Rubric Scoring（量表评分）

### Dimension 1: Architecture Clarity（170）

| 准则 | 得分 | 依据 |
|------|------|------|
| Layer placement explicit (0-60) | 55/60 | "新增 `internal/cert` 功能域，沿用 e-cam DDD 分层（domain/repository/service/web/module + ioc wire 注入）"分层显式；"ioc/cert.go 注入 Wire"指明注入位置。扣分：scheduler/定时任务、probe 子包的层归属仍未单独声明（图中悬于 service 侧） |
| Component diagram present (0-60) | 54/60 | ASCII 组件图覆盖 web→service→{repository/deployer/scheduler}→{CloudDeployer/ExecutionChannel/audit/alert/EIAM}。扣分：图排版混乱（ExecutionChannel 与 CloudDeployer 并列文字、无线连接）、新增 asset 数据源未入图 |
| Dependencies listed (0-50) | 46/50 | 依赖表 7 项含类型+用途。扣分：(1) `k8s.io/client-go` 仍无版本与 K8s API 兼容范围（"| `k8s.io/client-go` | 新增 | K8s API Server 直连，CRD patch/读取 |"）；(2) 覆盖率分母已实际依赖 `internal/asset`（"来源为 `internal/asset` 资产同步的全量资源盘点"）但未列入依赖表 |
| **小计** | **155/170** | |

### Dimension 2: Interface & Model Definitions（170，er-diagram 变体）

| 准则 | 得分 | 依据 |
|------|------|------|
| Interface signatures typed (0-40) | 37/40 | 三接口参数/返回值类型化；Service-Level Types 13 个结构体补齐。扣分：`Rollback(ctx, orderId, itemIds)` 的 itemIds 类型未注；CloudDeployer 缺回滚目标有效性校验方法（见 Phase 1） |
| Inline models concrete (0-40) | 32/40 | 持久模型 schema+ER 齐全，服务级类型字段+约束齐全。扣分：(1) ChangeItem/ChangeListItem 无批次归属字段；(2) 批量导入 `progress`/`batchId` 无持久化模型，且响应契约 `{files[]{fileName,result,errorReason,certId?}, progress}` 不含 batchId，轮询端点 `GET /api/v1/certs/batch/:batchId` 的 batchId 来源断裂；(3) 项级回滚失败终态缺失（status 枚举无 rollback_failed，回滚失败项在报告中仍显示 success） |
| ER diagram complete (0-30) | 28/30 | 11 实体、关系、基数、索引策略、语义说明齐全。扣分：THRESHOLDS 以独立实体再标 embed，表达冗余 |
| SQL DDL directly usable (0-30) | 29/30 | mongosh 可直接执行（createCollection+$jsonSchema+createIndex+TTL+部分唯一索引，语法均有效）。扣分：`description: "DEFAULT=pending"` 等默认值仅注释、靠写路径保证（已声明，可接受） |
| Cross-layer consistency (0-30) | 17/30 | 四处冲突：(1) **"取消"态跨文档缺失**——tech-design 写 "进入终态（已完成/部分完成/已回滚/回滚失败/**取消**）时 `$unset` 清除"，而 schema 枚举 `["draft","pending_confirm","executing","verifying","completed","partial_completed","rolled_back","rollback_failed"]` 与 api-handbook ChangeStatus 8 态均无"取消"，互斥清除路径引用了不存在的状态；(2) **阈值界冲突**——`verifyWindowHours: minimum: 1, maximum: 168, // DEFAULT=24，范围 2~24`、`rollbackProtectDays: minimum: 1, maximum: 90, // DEFAULT=7，范围 7~14`：校验器放行 1~168/1~90，注释自认业务界 2~24/7~14（PRD 口径），按 schema 执行将接受越 PRD 界配置；(3) Cross-Layer Map `scanFreshness … >24h 阻断清单` 硬编码 24h，与 thresholds.scanFreshnessHours 可配（1~168）矛盾；(4) 缩写不一致——Map 写 `{cipher,keyVer,algo}`、`[]{cloud,cov,total}`，schema 实为 `ciphertext/keyVersion/covered` |
| **小计** | **143/170** | |

### Dimension 3: Error Handling（130）

| 准则 | 得分 | 依据 |
|------|------|------|
| Error types defined (0-45) | 44/45 | 13 错误码三列齐全，与 api-handbook 完全闭合（iteration-1 问题已修复）。扣分：api-handbook 将 ROLLBACK_TARGET_INVALID 列入"生成清单错误"表，实际为回滚路径错误，归位错误误导实现 |
| Propagation strategy clear (0-45) | 41/45 | 三层传播 + "同步错误 vs 异步子任务状态"语境映射清晰（"项级失败不中断其他项；进度轮询/报告接口返回的是上述 status 字段"）。扣分：scheduler/probe 定时任务失败传播仍薄（ScanSnapshot.status=failed 后的重试/恢复路径、到期分级告警的去重与触发传播未描述） |
| HTTP status codes mapped (0-40) | 39/40 | 13 码→HTTP 全映射，异步语境已消歧（CLOUD_API_RATELIMITED→rate_limited 状态 vs 同步 503）。扣分：ROLLBACK_TARGET_INVALID 的 409 归属语境错位（同上） |
| **小计** | **124/130** | |

### Dimension 4: Testing Strategy（130）

| 准则 | 得分 | 依据 |
|------|------|------|
| Per-layer test plan (0-45) | 43/45 | 六层矩阵（domain/deployer/service/web/probe/e2e）+ 4 条 Key E2E Flows。扣分：测试场景缺"到期分级告警 30/14/7 触发"与"存量批量导入部分失败可单独重试（bootstrap）"场景 |
| Coverage target numeric (0-45) | 45/45 | 总 80% + 逐层 85/80/85/80/80/80 全量化 |
| Test tooling named (0-40) | 37/40 | go test、httptest、mongox test、本地 TLS server、envtest/假 APIServer 已命名。扣分：deployer 行 "go test + mock" 仍未指明 mock 框架（gomock/testify 等） |
| **小计** | **125/130** | |

### Dimension 5: Breakdown-Readiness ★（180 — 闸门）

| 准则 | 得分 | 依据 |
|------|------|------|
| Components enumerable (0-65) | 60/65 | 组件可完整枚举（internal/cert 五层+ioc、ExecutionChannel 2 实现+2 预留、CloudDeployer 6×2、五个 service、scheduler、cloudx 扩展、web 模块）。扣分：到期分级计算组件、自定义 CRD 登记组件、批量导入任务组件未单列 |
| Tasks derivable (0-65) | 59/65 | 接口/模型→任务可派生。扣分：(1) 批次→变更项分配无字段无算法，分批执行任务派生需先补设计决策；(2) 回滚目标有效性校验无接口方法，对应任务缺失；(3) 批量导入进度（batchId/progress）持久化模型缺失，进度轮询任务无从派生 |
| PRD AC coverage (0-50) | 32/50 | 四项扣分：(1) **-8** 分批 AC 双重违反——"false=自动续批"违背"剩余批同样需人工确认后执行"；"尾批完成→verifyWindowUntil 设置"缺失"首批执行且验证通过后方可继续执行剩余批"门控；(2) **-5** Story 4 AC 登记覆盖率/可更换托管覆盖率双指标无任何端点/模型承载；(3) **-3** 自定义 CRD"经登记后纳入扫描范围"的登记机制未设计（仅盲区声明兜底）；(4) **-2** 30/14/7 分级告警触发/去重链路未建模 |
| **小计** | **151/180** | **★ 闸门未通过（<160）** |

### Dimension 6: Security Considerations（80）

| 准则 | 得分 | 依据 |
|------|------|------|
| Threat model present (0-40) | 39/40 | 五项具体威胁（私钥集中托管/误操作恶意批量/云凭证滥用/审计绕过/K8s 凭证泄露）。扣分：主密钥泄露致存量密文批量暴露仍未单列独立威胁 |
| Mitigations concrete (0-40) | 37/40 | 每威胁配对策；主密钥轮换五步迁移（双活/双读/再加密/回滚/下线）具体可执行——iteration-1 盲点已消解。扣分：(1) "渗透式自查口径：grep 全代码库"仍无 CI 门禁化描述；(2) PRD 安全要求"备份恢复：平台数据丢失不得导致 EV/OV 私钥不可恢复（备份周期天级、恢复小时级）"无对应设计（主密钥备份仅在 Alternatives 一句带过，密文库备份恢复未设计） |
| **小计** | **76/80** | |

### Dimension 7: Implementation Feasibility（140）

注入上下文：Go 1.25 + Gin + MongoDB(mongox) + Redis + Wire DI；internal/cam DDD、internal/alert、internal/audit、internal/task、internal/asset 均在；cloudx 6 云适配；无 client-go。

| 准则 | 得分 | 依据 |
|------|------|------|
| Dependencies available (0-50) | 47/50 | cloudx/task/alert/audit/mongox/Redis/asset 均为现有模块且 asset 依赖已声明数据契约；client-go 显式标新增。扣分：client-go 重依赖无版本锚定与 K8s 版本兼容矩阵（Open Questions 仅列 PoC） |
| Architecture fits project structure (0-50) | 48/50 | "internal/cert（与 internal/cam 平级新域）"+ DDD + Wire 完全对齐既有模式。扣分：scheduler 挂入 internal/task 的注册方式未示 |
| Technical claims grounded (0-40) | 34/40 | 容量量级与 PRD 一致；选型基于标准库+现有 SDK。扣分：(1) "连续达标次数 ≥ verifyConfirmProbes（默认 2）"与 PRD DF006"天级探测"频率矛盾，短验证窗口（2~24h）内不可达，未定义窗口内提频探测；(2) cloudx 证书 API 适配对腾讯云 alb/nlb 产品存在性依赖 PoC（已列 Open Question，量小） |
| **小计** | **129/140** | |

---

## Phase 3 — Blindspot Hunt（盲点狩猎）

> 量表七维之外的架构失败模式，均附原文引用。

### [blindspot] 1. 互斥锁活性缺口：暂停分批单无限期持有 activeMutex，且无取消路径

"进入活跃态时写入 = oldCertFingerprint；进入终态（已完成/部分完成/已回滚/回滚失败/取消）时 `$unset` 清除"——但 schema 状态枚举 `["draft","pending_confirm","executing","verifying","completed","partial_completed","rolled_back","rollback_failed"]` 无"取消"态。分批单暂停在 executing（PauseBetween=true 且操作者不回归）将永久持有互斥 token，该旧证书一切后续变更被 CHANGE_IN_FLIGHT 阻塞，无超时/中止/接手设计。需补：取消态（与 tech-design 文本对齐）+ 暂停单超时或管理员中止路径。

### [blindspot] 2. 豁免域名与验证窗口达标的语义死锁

测试场景声明"豁免不告警"，窗口达标条件为 "verifyExpected.domains 全部探测一致且连续达标次数 ≥ thresholds.verifyConfirmProbes"——豁免域名不产生探测记录，若变更清单目标含豁免域名，"全部一致"永假，窗口必超时，变更单被误转"部分完成/待处理"（"超时未达标 → 恢复常规 diff 告警，变更单转部分完成/待处理"）。需明确豁免域名从 verifyExpected.domains 剔除或计为 skipped 达标。

### [blindspot] 3. 验证窗口探测节奏整体未定义（天级巡检无法承载 2~24h 窗口判定）

设计通篇未定义 ProbeService 调度周期；PRD DF006 为"天级探测"。verifyExpected 达标判定（"窗口内探测达标域名数"、连续 ≥2 次）与窗口关闭检测（"verifyWindowUntil > now"）都要求窗口内有确定性探测采样——天级节奏下 2h 窗口 0~1 次采样，窗口关闭检测本身也无调度载体（谁来在 windowUntil 时刻触发终态转换？）。需设计：窗口内提频探测周期 + 窗口到期检查任务。

### [blindspot] 4. K8s 管理权判定规则集与 reconcile 复检时窗仍未量化（iteration-1 遗留）

新增字段仅为占位："Reason string // AutoChangeable=false 时的判定依据（label/ownerReference）"、"RecheckPassed bool // K8s 通道：patch 后 reconcile 回写复检结果"——哪些 label/annotation 判 GitOps、ownerReference 判控制器的具体规则集，复检的时点/间隔/次数（立即？N 秒后？几次？）均未给出。复检窗口过短漏判 reconcile 回写，过长阻塞状态机流转。此为 K8s 通道任务拆分前的必答项。

### [blindspot] 5. 到期分级告警无状态去重，天级巡检将重复告警

thresholds 仅 `scanFreshnessHours/verifyWindowHours/rollbackProtectDays/verifyConfirmProbes` 四项，无任何"证书已告警级别"状态字段；天级巡检下同一张 30 天内证书将每天重复触发 30 天级告警。internal/alert 若无告警状态机则重复触达，若依赖其去重则该假设未声明（alert"复用+扩展"未列扩展点）。需补：分级告警去重状态或告警框架去重契约。

---

## 最终汇总

| 维度 | 得分 | 满分 |
|------|------|------|
| Architecture Clarity | 155 | 170 |
| Interface & Model Definitions | 143 | 170 |
| Error Handling | 124 | 130 |
| Testing Strategy | 125 | 130 |
| Breakdown-Readiness ★ | **151** | 180 |
| Security Considerations | 76 | 80 |
| Implementation Feasibility | 129 | 140 |
| **总计** | **903** | **1000** |

- Breakdown-Readiness ★ 闸门（≥160）：**未通过**（151/180）——阻断进入 breakdown-tasks，需 iteration 3 修订。
- 总分 903 ≥ 900 target，但闸门单项不达标，整体不通过。
- Iteration-1 的 12 项主要问题已修复 10 项（服务级类型、mongosh schema、互斥索引、分母数据源、密钥轮换、告警路由、审计端点、三态引用、E2E、错误码闭合），修订质量高。
- 本轮阻断项集中在四处：① 分批灰度语义与 PRD 双重冲突（自动续批选项 + 批间无验证门控 + ChangeItem 无批次归属）；② PRD 登记覆盖率/可更换托管覆盖率双指标无承载；③ "取消"态跨文档缺失 + 阈值界 schema 与业务界冲突；④ 回滚目标有效性校验无接口方法。另有一个系统性盲区：验证窗口/到期告警的**时间维度设计**（探测节奏、窗口到期触发、告警去重）整体缺位。
