# 设计评估报告 — Iteration 5（用户追加轮，目标：清 Breakdown-Readiness 闸门）

> 评估对象：`docs/features/ssl-cert-management/design/`（tech-design.md + er-diagram.md + schema.sql + api-handbook.md）
> 上游 PRD：prd-spec.md（916/1000 通过）、prd-user-stories.md
> 评分量表：`forge/eval/rubrics/design.md`（1000 分制，target 900，Breakdown-Readiness ★ 闸门 ≥160/180）
> 历史轨迹：iteration-1 877（BR 162）/ iteration-2 903（BR 151）/ iteration-3 925（BR 155）。本轮 ITERATION = 5，仅评分当前页面内容，无改进信用。
> 评估立场：对抗式——每项扣分附文档原文引用。

## 前置核对：Iteration-3 攻击清单消解状态

| Iteration-3 问题 | 当前状态 |
|------------------|----------|
| ChangeItem.resourceRef 无法重构 DeployTarget | 已修复：schema `resourceRef` anyOf 按 action 分支必填（cloud_api={channel,cloud,product,accountKey,resourceId}；k8s_api={channel,clusterId,namespace,kind,resourceId}） |
| 孤儿清理队列无消费者 | 已修复：scheduler `orphan-cleanup` 任务（天级批扫 + 窗口达标事件触发） |
| CRD 待复检无执行组件 | 已修复：scheduler `crd-recheck` 任务（recheckDelayMinutes 默认 5，单轮复检） |
| ChangeReport 缺孤儿清理结果/未达标清单 | 已修复：`OrphanCleanup []OrphanCleanupResult` + `UnmetDomains []string` + `Verify.Unmet/ProbeSkipped` |
| 孤儿清理失败告警无路由 | 已修复："清理失败项告警（运维处置类通知，不计入 PRD 四类业务告警口径）" |
| huawei/aws/azure 发现适配未枚举 | 已修复：依赖表"huawei/aws/azure 仅新增 ListReferences 只读发现适配（discovery-only）"+ 3 个 discovery-only deployer + 不可执行项建模（AutoChangeable=false + ERR_DISCOVERY_ONLY） |
| K8s 管理权规则占位 | 已修复："K8s 管理权判定与变更后复检"三信号规则集（GitOps label / ownerReferences / cert-manager annotation，键清单可配置）+ 复检时窗量化 |
| fingerprint_only 新证书无前置拦截 | 已修复：GenerateChangeList 四项前置校验 + 409 NEW_CERT_FINGERPRINT_ONLY |
| executing 态活性 | 已修复：heartbeatAt 心跳（30s）+ executing-timeout 任务（默认 30 分钟）+ 部分索引 |
| 扫描快照卡死 running | 已修复：scan-timeout 任务（scanTimeoutHours 默认 2，SCAN_TIMED_OUT 释放防重锁） |
| 通配符 SAN 探测 | 已修复："探测目标域来源与通配符处置"节（目标域=台账 sans[] 展开去重、wildcard_skipped、wildcardProbeOverrides） |
| 备份恢复 | 已修复："备份与恢复"三步（密文库随库天级备份/主密钥异地隔离/恢复演练 ≤1h） |
| client-go 版本锚定 / scheduler 注册 / 主密钥泄露威胁单列 | 已修复 |
| 平台自身监控（任务成功率） | **未修复**：全文档仍无落点 |
| 完整性定时复检 | **未修复**：inspection 任务仍仅"到期分级计算 + expiryAlertLevel 去重告警" |
| 项级 rollback_failed | **未修复**：status 枚举仍 ["pending","running","success","failed","rate_limited","rolled_back","skipped"] |
| 双 protectUntil 所有权 | **未修复**（见盲点 3） |
| mock 框架命名 / 渗透自查 CI 门禁 / 模拟通道可插拔演示 / 单批阈值字段 | **未修复**（小项） |

本轮新发现：**CertReference 缺 namespace/kind 导致 K8s 清单项目标不可构造**（上轮修复了 item 侧、漏了 source 侧）、**rate_limited 态逃逸活性保障**、**Execute 无防重语义**、**重试耗尽无状态迁移**。

---

## Phase 1 — Reasoning Audit（推理审计）

### 1.1 PRD→Design 覆盖追踪

PRD Coverage Map 17 行均有真实落点，六大 Story 主干全覆盖。仍存缺口：

- **【缺口】平台自身监控**：PRD Monitoring"平台自身：证书域服务健康、扫描/探测任务成功率监控"——设计无任何承载（internal/task 任务成功率如何暴露/上报未声明）。
- **【缺口】完整性定时复检**：PRD In Scope"完整性检查：…导入拦截 **+ 定时巡检**"、天级巡检四职责含"托管证书完整性复检"；设计 inspection 任务仅"到期分级计算 + expiryAlertLevel 去重告警"，完整性复检无任务、无服务、无测试场景。
- **【缺口】重试耗尽语义**：PRD 并发规则"重试耗尽方计为执行失败"；设计仅有 "CLOUD_API_RATELIMITED → ChangeItem.status=`rate_limited`（退避重试中）"，rate_limited→failed 的迁移条件（重试次数上限/总时长上限/退避算法）全文档未定义，"限流重试中"可能无限持续（见盲点 1）。
- **【缺口】模拟通道可插拔演示**：PRD In Scope"（可插拔性以模拟通道端到端演示零上层改动验证）"——测试矩阵与 E2E Flow 仍不含。
- **【小缺口】审计 action 枚举不全**："action 覆盖 create/confirm/execute/item_result/rollback/verify/orphan_cleanup"——cancel/confirm-batch 属"变更操作全量审计"却不在列。
- **【小缺口】未量化阈值**：BatchConf "false=单批全量（仅引用数 <= 阈值时允许）"的阈值、PRD Performance"单变更清单目标数上限可配"均无 thresholds 字段承载。

### 1.2 隐式耦合 / 静默错误 / 并发 / 迁移

- **【耦合缺口】CertReference 缺 namespace/kind，K8s 清单项不可构造**：变更清单由"按指纹聚合的引用项"（CertReference）生成，而 `cert_references` 字段仅 `{certFingerprint, cloud, product, clusterId, resourceId, referencedCloudCertId, accountKey, snapshotId, scannedAt}`——无 namespace、无 kind；`DeployTarget` 声明 "Namespace // k8s_api 必填（CRD 所在命名空间）"、"Kind // k8s_api 必填（CRD kind）"，item 侧 anyOf 也强制二者。同一集群多 CRD kind（固定枚举 4 种 + 登记项）下 kind 不可由 clusterId 推导、同名 CRD 实例跨 namespace 不可区分——GenerateChangeList 无法为 patch_crd 项填充 Target，上轮修复只覆盖了 item 持久化侧、漏了引用来源侧。
- **【静默风险】rate_limited 态无活性兜底**：executing-timeout 扫描与部分索引均限定 `status=running`，退避重试中（status=rate_limited）的项在 worker 崩溃后永久无人接管（见盲点 1）。
- **【并发缺口】Execute 无防重/状态前置**：scan 有"防重 409 SCAN_IN_PROGRESS"、ConfirmBatch 有"409 BATCH_NOT_CONFIRMABLE"，独 Execute 无任何状态门/防重语义（见盲点 2）。
- 消除模式复入：无（Alternatives 三项成立）。迁移：新功能域无存量迁移；主密钥轮换五步完整。通过。

---

## Phase 2 — Rubric Scoring（量表评分）

### Dimension 1: Architecture Clarity（170）

| 准则 | 得分 | 依据 |
|------|------|------|
| Layer placement explicit (0-60) | 56/60 | "新增 `internal/cert` 功能域，沿用 e-cam DDD 分层（domain/repository/service/web/module + ioc wire 注入）" + "ioc/cert.go 注入 Wire" + "注册于 internal/task" 显式。扣分：deployer/probe 子包不在 DDD 分层清单中，仅靠组件图侧挂表达；前端仅一句路由模块 |
| Component diagram present (0-60) | 54/60 | ASCII 图覆盖 web→service→{repository/deployer/scheduler}→{CloudDeployer/ExecutionChannel/cloudx/audit/alert/EIAM/crypto}，9 类定时任务入表。扣分：`internal/asset` 已是覆盖率分母的实际数据依赖（"来源为 `internal/asset` 资产同步的全量资源盘点"）仍不入图；ExecutionChannel 与 CloudDeployer 的实现/调用关系仍为并列文字 |
| Dependencies listed (0-50) | 49/50 | 依赖表 8 项含类型+用途；五云 cloudx 扩展已列（"aliyun/tencent 复用云账号凭证 + SDK…huawei/aws/azure 仅新增 ListReferences 只读发现适配"）；client-go 有锚定策略。扣分：e2e "envtest" 隐含 controller-runtime 依赖未列 |
| **小计** | **159/170** | |

### Dimension 2: Interface & Model Definitions（170，er-diagram 变体）

| 准则 | 得分 | 依据 |
|------|------|------|
| Interface signatures typed (0-40) | 38/40 | 三接口全类型化 + 16 个服务级结构体字段级定义（新增 Credential/DiscoverScope/DeployTarget/DeployResult/RollbackResult/CloudCertInfo/OrphanCleanupResult 等），Credential/Reason/RollbackResult.ErrCode 均带敏感信息约束注释。扣分：批量导入请求契约仅一句 "Request: multipart 多文件 + 逐文件可选私钥。"——多文件场景下证书/私钥文件的配对契约（字段命名/配对规则）未定义，与单导入逐字段表形成反差（-2） |
| Inline models concrete (0-40) | 35/40 | 扣分：(1) ChangeItem.status 枚举 `["pending","running","success","failed","rate_limited","rolled_back","skipped"]` 仍无项级 rollback_failed——回滚失败项在报告 Items 中与未尝试回滚的 success 项不可区分（PRD Story 5"逐项结果+回滚状态"比对不闭合，-2）；(2) BatchConf.MaxBatchRatio "(0, 0.5]；硬约束 <=0.5" 与批次分配公式 "首批 = 前 `min(BatchSize, floor(total/2))` 项" 互不相干——公式不含 ratio，MaxBatchRatio 为死配置或两处口径可分歧（-2）；(3) "仅引用数 <= 阈值时允许"阈值无名字无字段（-1） |
| ER diagram complete (0-30) | 29/30 | 13 实体 + 关系基数 + 索引策略表 + 14 条语义说明，coverageMeta(-1 口径)/batchNo/verifyExpected/expiryAlertLevel/heartbeatAt 均已反映。扣分：THRESHOLDS 独立实体再标 "embed" 表达冗余 |
| SQL DDL directly usable (0-30) | 29/30 | mongosh 可直接执行：createCollection + $jsonSchema + anyOf 分支校验 + 部分唯一索引（activeMutex）+ 两处 TTL；阈值界与 PRD 对齐且结构互锁（"verifyWindowHours … maximum: 24 … < rollbackProtectDays 下界 7d"）。扣分：默认值仅注释、靠写路径保证（文件已自我声明，轻扣） |
| Cross-layer consistency (0-30) | 24/30 | 上轮四处大冲突已消（resourceRef anyOf、cancelled 9 态三文档一致、阈值界、命名统一）。**新冲突**：(1) CertReference 无 namespace/kind 与 DeployTarget "Namespace // k8s_api 必填"、"Kind // k8s_api 必填" 及 item anyOf 强制字段冲突——清单生成时 K8s 项目标不可构造（-5）；(2) Cross-Layer Map "coverageMeta | []{cloud,covered,total}" 漏 product（schema items required ["cloud","product","covered","total"]）（-1） |
| **小计** | **155/170** | |

### Dimension 3: Error Handling（130）

| 准则 | 得分 | 依据 |
|------|------|------|
| Error types defined (0-45) | 44/45 | 17 码三列齐全，tech-design 与 api-handbook 双向闭合；新增 NEW_CERT_FINGERPRINT_ONLY/EXEC_TIMEOUT/SCAN_TIMED_OUT 语义明确。扣分：api-handbook "#### 生成清单错误"表仍混入 "BATCH_NOT_CONFIRMABLE"、"CHANGE_NOT_CANCELLABLE"（属 confirm-batch/cancel 路径），归组误导 |
| Propagation strategy clear (0-45) | 42/45 | 三层传播 + "同步错误 vs 异步子任务状态"语境映射清晰；心跳/超时/防重释放路径完整。扣分：rate_limited→failed 的耗尽迁移未定义——PRD"重试耗尽方计为执行失败"，而设计仅 "CLOUD_API_RATELIMITED → ChangeItem.status=`rate_limited`（退避重试中）"，无重试上限/退避算法/耗尽判据，异步状态机存在无出口的状态（-3） |
| HTTP status codes mapped (0-40) | 40/40 | 17 码→HTTP 全映射；503 限定同步路径的语境消歧成立；ROLLBACK_TARGET_INVALID 归位回滚端点段 |
| **小计** | **126/130** | |

### Dimension 4: Testing Strategy（130）

| 准则 | 得分 | 依据 |
|------|------|------|
| Per-layer test plan (0-45) | 43/45 | 七层矩阵 + 12 条 Key Scenarios + 4 条 E2E Flow（换证全链路/分批门控/告警路由/主密钥轮换）。扣分：缺 bootstrap"批量导入部分失败可单独重试"场景（api-handbook 已承诺"失败文件单独重试（重新 POST 单文件）"却无测试落点）；缺 PRD"模拟通道端到端零上层改动"可插拔性演示 |
| Coverage target numeric (0-45) | 45/45 | 总 80% + 逐层 85/80/85/80/80/80 全量化 |
| Test tooling named (0-40) | 38/40 | go test、httptest、mongox test、本地 TLS server、envtest/假 APIServer 均命名。扣分：deployer 行 "| go test + mock |"（第四轮）仍未指明 mock 框架（gomock/testify 等） |
| **小计** | **126/130** | |

### Dimension 5: Breakdown-Readiness ★（180 — 闸门）

| 准则 | 得分 | 依据 |
|------|------|------|
| Components enumerable (0-65) | 64/65 | 可完整枚举：internal/cert 五层 + ioc/cert.go、ExecutionChannel 2 实现 + 2 预留、CloudDeployer 6×2 部署器 + 3 discovery-only 适配、ChangeService/CertService/IntegrityService/ReferenceDiscoveryService/ProbeService/InspectionService、**9 类定时任务全表列**（scan/scan-timeout/probe/inspection/window-expiry/pause-timeout/orphan-cleanup/crd-recheck/executing-timeout）、CrdRegistration 管理、CertBatchSession、stats/dashboard 聚合、密钥轮换迁移任务、web 模块。上轮缺失的清理队列消费者与复检执行体均已入表。扣分：orphan-cleanup "天级批扫 + 事件触发（验证窗口达标关闭后即时消费该单孤儿清理队列）"的"事件触发"机制（进程内事件/队列/直接调用）未指明（-1） |
| Tasks derivable (0-65) | 61/65 | 接口→实现、模型→schema、端点→handler、9 任务→注册均可派生；K8s 管理权三信号 + crd-recheck 已定量化；批次分配算法、互斥索引、提频探测、心跳超时均已可执行级。扣分：(1) CertReference 缺 namespace/kind——GenerateChangeList 与 K8sAPIChannel 持久化契约任务开工前必须先改引用模型（-3）；(2) "仅引用数 <= 阈值"与"清单目标数上限"无配置字段，配置任务无对象（-1） |
| PRD AC coverage (0-50) | 44/50 | 六 Story 主干与绝大多数 AC 覆盖（含上轮四项大缺口已消：报告字段/告警路由/不可执行项单列/前置拦截）。扣分：(1) **-2** 平台自身监控（"扫描/探测任务成功率监控"）无落点；(2) **-2** 完整性定时复检缺位（inspection 仅"到期分级计算 + expiryAlertLevel 去重告警"）；(3) **-1** 模拟通道可插拔演示缺测试设计；(4) **-1** 审计 action 枚举缺 cancel/confirm-batch（"action 覆盖 create/confirm/execute/item_result/rollback/verify/orphan_cleanup"），"变更操作全量审计"不闭合 |
| **小计** | **169/180** | **★ 闸门通过（≥160）** |

### Dimension 6: Security Considerations（80）

| 准则 | 得分 | 依据 |
|------|------|------|
| Threat model present (0-40) | 40/40 | 五项具体威胁，"私钥集中托管：平台成为高价值目标，DB 泄露 + 主密钥泄露 = 全网私钥失守"已将主密钥泄露场景纳入 |
| Mitigations concrete (0-40) | 38/40 | 每威胁配对策；信封加密 + keyVersion 双读轮换五步迁移 + 备份恢复三步（"恢复演练小时级路径…≤1 小时完成"）+ Credential zeroing 具体可执行。扣分：(1) "渗透式自查口径：grep 全代码库无明文私钥返回点"仍无 CI 门禁/留档机制描述（第四轮）；(2) PRD"传输加密：全程 HTTPS；私钥上传通道加密"在设计中无对应落点（部署层假设未声明）（-1 各） |
| **小计** | **78/80** | |

### Dimension 7: Implementation Feasibility（140）

注入上下文：Go 1.25 + Gin + MongoDB(mongox) + Redis + Wire DI；internal/cam/alert/audit/task/asset 存在；cloudx 6 云适配；无 client-go。

| 准则 | 得分 | 依据 |
|------|------|------|
| Dependencies available (0-50) | 49/50 | cloudx/task/alert/audit/mongox/Redis/asset 均现有且扩展点声明；client-go 显式新增 + "首期兼容矩阵 1.24+，具体客户端版本经首批 PoC 任务验证后锁定"。扣分：e2e "envtest/假 APIServer"——envtest 属 controller-runtime 测试设施，依赖表未列（-1） |
| Architecture fits project structure (0-50) | 49/50 | "internal/cert（与 internal/cam 平级新域）"+ DDD 分层 + Wire 注入 + "注册于 internal/task" 完全对齐既有模式。扣分：任务节奏降至 5/10/15 分钟级（executing-timeout "每 5 分钟扫一次"、window-expiry "周期 = verifyProbeIntervalMinutes"）与现有天级任务框架能力的兼容性未验证声明（-1） |
| Technical claims grounded (0-40) | 38/40 | 提频探测（5~60 分钟）使 verifyConfirmProbes 可判定；floor(total/2) 硬约束落地；心跳/超时参数全区间化。扣分：云 API 限流"退避重试"仍无退避算法/重试上限量化（第四轮，PRD"重试耗尽方计为执行失败"未落参数）（-2） |
| **小计** | **136/140** | |

---

## Phase 3 — Blindspot Hunt（盲点狩猎）

> 量表七维之外的架构失败模式，均附原文引用。

### [blindspot] 1. rate_limited 态逃逸活性保障：worker 崩溃后无出口

活性设计仅覆盖 running："executing-timeout | ChangeItem.status=running 且 heartbeatAt + thresholds.itemHeartbeatTimeoutMinutes < now"，索引同样只建 running：`partialFilterExpression: { status: "running" }`。而限流路径明确落独立状态："CLOUD_API_RATELIMITED → ChangeItem.status=`rate_limited`（退避重试中）"。若 worker 在长时间退避（状态=rate_limited）期间崩溃/被 OOM kill，该项既不被 executing-timeout 扫描（非 running）、又无重试耗尽迁移（PRD"重试耗尽方计为执行失败"未建模）——项永久停留 rate_limited，订单卡 executing，activeMutex 永不清除，该旧证书后续变更被 CHANGE_IN_FLIGHT 永久阻塞。修复：超时扫描扩展至 rate_limited，或退避期间保持 status=running 且心跳继续。

### [blindspot] 2. Execute 无防重/状态前置语义：重复触发即重复变更

对比同文档其他触发型端点的门控——扫描有"立即扫描（防重 409 SCAN_IN_PROGRESS）"、续批有"不满足 409 BATCH_NOT_CONFIRMABLE"——Execute 仅 "Execute(ctx, orderId) error // 派发子任务执行当前批（ChangeItem.batchNo = batchInfo.currentBatch）"，api-handbook 行 "触发批量执行（执行当前批 batchNo=currentBatch 的项）"也无状态前置错误码。用户双击/前端重试/网关重放将并发派发两批同 batchNo 子任务：两段式重复 UploadCert（产生新孤儿云证书）、重复 patch CRD，且两个 worker 争抢同一 item 的 status 迁移。需补：Execute 的合法前置状态 + 状态 CAS（或防重锁）+ 幂等键，错误码入表。

### [blindspot] 3. 双 protectUntil 双源真值无传播路径（第四轮遗留）

`cert_certificates.protectUntil // 回滚保护期截止；>=now 禁删` 与 `cert_change_orders.protectUntil // 回滚保护期截止` 并存。PRD："进入回滚保护期（7~14 天）：期间**旧证书**禁止删除"。订单进入 completed/partial_completed 时保护期如何写回旧证书文档、同一旧证书多张历史单的保护期如何取 max 合并、删除拦截（"仅 no_refs_scanned 允许删除（protectUntil 保护期另计）"）读哪一份——均未定义。两处时间戳无传播路径即双源真值，实现者各取其一将产生删除拦截漏洞或保护期丢失。

### [blindspot] 4. 回滚的反向 CRD patch 无复检：检测逻辑不对称

设计对正向变更设了防回写兜底："复检次数固定 1，失败不做二次自动复检（转人工决策…）"——其存在前提是三信号探测可能漏判控制器接管。但 Rollback（"Rollback(ctx, creds, target DeployTarget, oldRef CertReference) (RollbackResult, error)"）对 patch_crd 项同样是一次 patch（写回旧引用值），crd-recheck 的触发条件却仅 "ChangeItem(action=patch_crd) patch 完成"（变更执行路径）。若漏判的控制器把回滚 patch 再 reconcile 回新证书，回滚"成功"是假象且无告警——PRD"回滚成功率 100%（回滚对象=…限通过目标有效性校验的项）"失去检测手段。需补：回滚 patch 复用 crd-recheck（旧值复检）或将回滚纳入 RecheckPassed 语义。

---

## 最终汇总

| 维度 | 得分 | 满分 |
|------|------|------|
| Architecture Clarity | 159 | 170 |
| Interface & Model Definitions | 155 | 170 |
| Error Handling | 126 | 130 |
| Testing Strategy | 126 | 130 |
| Breakdown-Readiness ★ | **169** | 180 |
| Security Considerations | 78 | 80 |
| Implementation Feasibility | 136 | 140 |
| **总计** | **949** | **1000** |

- **总分 949 ≥ 900 target：通过。**
- **Breakdown-Readiness ★ 闸门（≥160）：通过（169/180）**——iteration-3 的闸门阻断项（resourceRef、报告字段、异步消费者、五云适配、管理权规则、前置拦截）全部消解，BR 155→169。
- 剩余攻击点均属字段/枚举/参数级小改：CertReference 补 namespace/kind（K8s 清单生成前置）、rate_limited 活性与耗尽迁移、Execute 防重、protectUntil 传播、项级 rollback_failed、平台自身监控与完整性复检两个 PRD 缺口、mock 框架命名。不构成架构返工，建议随首批任务修正。

```
SCORE: 949/1000
DIMENSIONS:
  Architecture Clarity: 159/170
  Interface & Model Definitions: 155/170
  Error Handling: 126/130
  Testing Strategy: 126/130
  Breakdown-Readiness: 169/180
  Security Considerations: 78/80
  Implementation Feasibility: 136/140
ATTACKS:
1. [Interface & Model Definitions]: CertReference 缺 namespace/kind，K8s 清单项目标不可构造 — schema cert_references 仅 "{certFingerprint, cloud, product, clusterId, resourceId, referencedCloudCertId, accountKey, snapshotId, scannedAt}"，而 DeployTarget 要求 "Namespace // k8s_api 必填（CRD 所在命名空间）"、"Kind // k8s_api 必填" — 引用模型补 namespace/kind 两字段（er-diagram + schema + Discover 写入契约）
2. [Error Handling]: rate_limited 无耗尽出口 — 仅 "CLOUD_API_RATELIMITED → ChangeItem.status=`rate_limited`（退避重试中）"，PRD"重试耗尽方计为执行失败"无重试上限/退避算法/迁移条件 — 补退避参数与 rate_limited→failed 迁移规则
3. [Breakdown-Readiness]: 平台自身监控无落点 — PRD"扫描/探测任务成功率监控"，设计仅 scheduler 表无任何暴露/上报设计 — 补任务成功率指标承载
4. [Breakdown-Readiness]: 完整性定时复检缺位 — inspection 任务仅"到期分级计算 + expiryAlertLevel 去重告警"，PRD 天级巡检四职责含"托管证书完整性复检" — inspection 任务扩完整性复检或单列任务
5. [Interface & Model Definitions]: 项级回滚失败不可区分 — ChangeItem.status 枚举 `["pending","running","success","failed","rate_limited","rolled_back","skipped"]` 无 rollback_failed — 补项级回滚失败状态
6. [Interface & Model Definitions]: MaxBatchRatio 为死配置 — "首批 = 前 `min(BatchSize, floor(total/2))` 项"公式不含 "MaxBatchRatio float64 // 单批占全部引用比例上限" — 统一批大小口径（min(BatchSize, floor(total·ratio), floor(total/2))）或删除该字段
7. [Breakdown-Readiness]: 审计 action 枚举不全 — "action 覆盖 create/confirm/execute/item_result/rollback/verify/orphan_cleanup" 缺 cancel/confirm-batch — 补全"变更操作全量审计"action 清单
8. [Testing Strategy]: mock 框架第四轮未命名 — "| go test + mock |"（deployer 行）— 指明 gomock/testify 等具体框架
9. [Breakdown-Readiness]: 模拟通道可插拔演示缺失 — PRD"可插拔性以模拟通道端到端演示零上层改动验证"，测试矩阵与 E2E Flow 均无 — 补模拟通道零上层改动 E2E 场景
10. [Security Considerations]: 渗透自查无 CI 门禁 — "渗透式自查口径：grep 全代码库无明文私钥返回点"无自动化留档机制 — 补 CI 检查任务与留档口径
```
