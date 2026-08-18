# 设计评估报告 — Iteration 3（最终预算轮）

> 评估对象：`docs/features/ssl-cert-management/design/`（tech-design.md + er-diagram.md + schema.sql + api-handbook.md）
> 上游 PRD：prd-spec.md（916/1000 通过）、prd-user-stories.md
> 评分量表：`forge/eval/rubrics/design.md`（1000 分制，target 900，Breakdown-Readiness ★ 闸门 ≥160/180）
> Iteration = 3（最终预算轮）。仅评分当前页面上的内容，无改进信用。
> 评估立场：对抗式——每项扣分均附文档原文引用。

## 前置核对：Iteration-2 遗留问题消解状态

| Iteration-2 问题 | 当前状态 |
|------------------|----------|
| 分批自动续批选项违反 PRD | 已修复：BatchConf 移除 PauseBetween，"分批一律人工续批…不提供自动续批选项" |
| 批间无验证门控 | 已修复："每批执行完成 → 订单转 verifying…提频探测连续 verifyConfirmProbes 次一致 = 批级验证达标 → …等待人工续批" |
| 登记覆盖率/可更换托管覆盖率双指标无承载 | 已修复："登记覆盖率 / 可更换托管覆盖率（双指标口径）"节 + `GET /api/v1/certs/stats` |
| 自定义 CRD 登记机制未设计 | 已修复：CrdRegistration 模型 + `POST/GET/DELETE /api/v1/certs/settings/crds` + 扫描联动 |
| 到期分级告警链路未建模 | 已修复："到期分级告警（去重状态机）"节（InspectionService + expiryAlertLevel + thresholds.expiryLevels） |
| 回滚目标有效性校验无接口 | 已修复：`CloudDeployer.GetCert` + CloudCertInfo + ChangeService.Rollback 前置校验路径 |
| verifyConfirmProbes 与天级探测矛盾 | 已修复：verifyProbeIntervalMinutes 提频（默认 10 分钟）+ 窗口到期 scheduler 终局判定 |
| 互斥活性（暂停单无限持锁） | 已修复：cancelled 终态 + pauseTimeoutHours 超时自动取消 |
| "取消"态跨文档缺失 | 已修复：schema/api-handbook/tech-design 三处 9 态含 cancelled |
| 阈值界冲突（1~168 vs 2~24 等） | 已修复：verifyWindowHours {2,24}、rollbackProtectDays {7,14}，界值与 PRD 一致且结构化保证保护期>窗口 |
| 豁免 ∩ 验证窗口死锁 | 已修复：excludedDomains + 计 skipped |
| ChangeItem 无批次归属 | 已修复：ChangeItem.batchNo（Confirm 时固化） |
| 批量导入 batchId 轮询断裂 | 已修复：CertBatchSession 持久化 + "batchId 即 cert_batch_sessions._id" |
| 批次分配算法未设计 | 已修复："(cloud, product, resourceId) 字典序稳定排序…首批 = 前 min(BatchSize, floor(total/2)) 项" |
| scanFreshness 硬编码 24h / 缩写不一致 | 已修复 |
| ROLLBACK_TARGET_INVALID 归位错误 | 已修复：api-handbook 单列"回滚路径错误（rollback 端点）"段 |
| client-go 版本无锚定策略 | 已修复："版本锚定：跟随目标集群最低 K8s 版本，首期兼容矩阵 1.24+…PoC 验证后锁定" |
| K8s 管理权判定规则集与复检时窗未量化 | **未修复（第三轮遗留）**：仍仅 "Reason string // …判定依据（label/ownerReference）" 与 "RecheckPassed bool // patch 后 reconcile 回写复检结果" 占位 |
| 项级回滚失败终态缺失 | **未修复**：ChangeItem.status 枚举仍无 rollback_failed |
| 双 protectUntil 所有权未定义 | **未修复**（见盲点 4） |
| mock 框架未命名 / scheduler 注册方式 / 备份恢复 / 渗透自查 CI 门禁 | **未修复**（小项，各扣 1~2 分） |

本轮新发现集中在：**ChangeItem.resourceRef 无法重构执行目标**、**两个承诺的异步过程无执行组件**（孤儿清理队列、CRD 待复检）、**ChangeReport 缺 PRD 明确要求的报告字段**、**executing 态活性缺口**。

---

## Phase 1 — Reasoning Audit（推理审计）

### 1.1 PRD→Design 覆盖追踪

PRD Coverage Map 16 行绝大多数有真实落点。仍存缺口：

- **【缺口】ChangeReport 缺"孤儿证书补偿清理结果"与"未达标清单"字段**：PRD Story 5 AC："报告含变更清单、逐项结果、回滚状态、验证结果、**孤儿证书补偿清理结果**"；Story 3 AC："报告含孤儿证书补偿清理结果（逐项清理成功/失败），**清理失败项触发告警**"；PRD 并发规则："记入变更报告**'未达标清单'**"。设计正文亦承诺："存在未达标→partial_completed + **未达标清单记入报告**"。但 `ChangeReport` 结构体仅有 `{OrderID, Status, Summary, Items, Verify, FinishedAt}`，`VerifySummary` 仅 `ProbePass/ProbeDiff` 计数——两字段均无处承载；孤儿清理失败告警也不在"告警四类路由"（到期分级/TLS 差异/变更关联/回滚失败）中。
- **【缺口】平台自身监控未设计**：PRD Monitoring："平台自身：证书域服务健康、扫描/探测任务成功率监控"——全文档无任何落点（internal/task 任务成功率如何暴露未声明）。
- **【缺口】完整性定时复检缺位**：PRD In Scope："完整性检查：…导入拦截 **+ 定时巡检**"；PRD 巡检四职责（完整性复检/到期分级/TLS 探测/引用扫描）中，设计 InspectionService 仅覆盖到期分级（"InspectionService 天级巡检按 notAfter 计算 daysLeft"）。
- **【缺口】华为云/AWS/Azure 引用发现适配未列入依赖**：PRD 要求五云发现（"阿里云/腾讯云/华为云/AWS/Azure 的…证书引用扫描"），依赖表仅 "internal/shared/cloudx/{aliyun,tencent} | 扩展"——三云只读发现所需的 cloudx 扩展未列，组件不可枚举。
- **【缺口】仅指纹证书作为新证书发起更换无前置拦截**：`GenerateChangeList` 注释仅 "指纹聚合+新鲜度校验+SAN预检"，无 newCertId hostingStatus=complete 校验；fingerprint_only 新证书无私钥，云上传项将在执行期必然失败。
- **【缺口】模拟通道可插拔性演示**：PRD In Scope："（可插拔性以模拟通道端到端演示零上层改动验证）"——测试场景/E2E 均未含。
- **单批阈值/清单上限未落 config**："Enabled=false 单批全量（**仅引用数 <= 阈值时允许**）"——阈值未定义且 thresholds 无对应字段；PRD Performance"单变更清单目标数上限可配"同样无字段承载。

### 1.2 隐式耦合 / 静默错误 / 并发 / 迁移

- **【冲突】持久化 resourceRef 无法重构 DeployTarget**：`DeployTarget` 声明 "Namespace // k8s_api 必填（CRD 所在命名空间）"、"Kind // k8s_api 必填（CRD kind）"、"AccountKey // cloud_api 必填"，而 schema 的 `ChangeItem.resourceRef` 仅 `{cloud, product, resourceId, clusterId}`（required: cloud/product/resourceId）——异步子任务执行时从持久化数据无法还原 patch_crd 所需 namespace/kind，也无法定位云账号凭证；`CertReference` 同样无 namespace/kind（CrdRegistration 按 cluster+group+kind 唯一，多登记下 kind 不可由 clusterId 推导）。接口层与数据模型冲突。
- **product 枚举跨层错位**：schema `cert_references.product` 枚举含 `"crd"`，而 `DeployTarget.Product` 枚举 "cdn|dcdn|waf|alb|clb|nlb；cloud_api 必填"（k8s 项 product 留空语义）——patch_crd 项在 resourceRef（product 必填）与 DeployTarget（k8s 空）两处取值规则互相矛盾。
- **孤儿清理队列与 CRD 待复检无执行组件**：`DeployResult.OrphanCandidate // true=…验证通过后进入清理队列`——scheduler 任务清单 "(scan/probe/inspection/window-expiry/pause-timeout)" 无清理队列消费者；`RecheckPassed // …false 时项标 failed 待复检`——"待复检"的后续复检执行体与时窗同样缺失。
- 静默错误路径：ScanSnapshot.status=failed 后的重试/恢复路径仍未设计（见盲点 2）。
- 消除模式复入：无（Alternatives 三项对比成立）。
- 迁移：新功能域无存量迁移；主密钥轮换五步迁移完整。通过。

---

## Phase 2 — Rubric Scoring（量表评分）

### Dimension 1: Architecture Clarity（170）

| 准则 | 得分 | 依据 |
|------|------|------|
| Layer placement explicit (0-60) | 56/60 | "新增 `internal/cert` 功能域，沿用 e-cam DDD 分层（domain/repository/service/web/module + ioc wire 注入）"分层显式；"ioc/cert.go 注入 Wire"指明注入位置。扣分：scheduler/ProbeService 子包层归属仍仅由组件图侧挂示意，未在分层清单中点名 |
| Component diagram present (0-60) | 54/60 | ASCII 组件图覆盖 web→service→{repository/deployer/scheduler}→{CloudDeployer/ExecutionChannel/audit/alert/EIAM/crypto}，scheduler 任务清单入图。扣分：`internal/asset` 已是实际数据依赖（"来源为 `internal/asset` 资产同步的全量资源盘点"）却未入图；ExecutionChannel 与 CloudDeployer 的实现/调用关系仍以并列文字表达 |
| Dependencies listed (0-50) | 48/50 | 依赖表 8 项（client-go/asset/crypto/cloudx/task/alert/audit/mongox+Redis）含类型+用途；client-go 已给版本锚定策略（"首期兼容矩阵 1.24+，具体客户端版本经首批 PoC 任务验证后锁定"）。扣分：cloudx 扩展仅列 "{aliyun,tencent}"，五云发现所需的 huawei/aws/azure 只读适配未列 |
| **小计** | **158/170** | |

### Dimension 2: Interface & Model Definitions（170，er-diagram 变体）

| 准则 | 得分 | 依据 |
|------|------|------|
| Interface signatures typed (0-40) | 38/40 | 三接口（ExecutionChannel/CloudDeployer/ChangeService）全类型化，GetCert/CloudCertInfo 补齐回滚校验，itemIds 已标 `[]string`，13+ 服务级结构体字段级定义。扣分：批量导入端点仅一句 "Request: multipart 多文件 + 逐文件可选私钥"——证书/私钥文件的配对契约（字段命名规则）未定义，与单导入的逐字段表形成反差 |
| Inline models concrete (0-40) | 34/40 | 服务级类型字段+类型+约束齐全（Credential/DiscoverScope/DeployTarget/DeployResult/RollbackResult/CloudCertInfo/BatchConf/ChangeList/ChangeListItem/SanCheckResult/ChangeReport/ReportSummary/ReportItem/VerifySummary）。扣分：(1) ChangeReport 缺正文与 PRD 承诺的孤儿清理结果与未达标清单字段（-3）；(2) ChangeItem.status 枚举 `["pending","running","success","failed","rate_limited","rolled_back","skipped"]` 仍无项级 rollback_failed，回滚失���项在报告中与未尝试回滚的 success 项不可区分（-2）；(3) BatchConf "仅引用数 <= 阈值时允许"的阈值无定义无配置字段（-1） |
| ER diagram complete (0-30) | 29/30 | 12 实体、关系、基数、索引策略表、语义说明节齐全；coverageMeta/batchNo/verifyExpected/expiryAlertLevel 均已反映。扣分：THRESHOLDS 以独立实体再标 "embed" 仍显冗余 |
| SQL DDL directly usable (0-30) | 29/30 | mongosh 可直接执行：createCollection + $jsonSchema 校验器 + createIndex + TTL + 部分唯一索引（`partialFilterExpression: { activeMutex: { $type: "string" } }`）语法有效；阈值界已与 PRD 对齐且结构化互锁（"verifyWindowHours 上限 24h < rollbackProtectDays 下界 7d"）。扣分：默认值仅注释标注靠写路径保证（文件已声明，轻扣） |
| Cross-layer consistency (0-30) | 20/30 | 上轮四处冲突已消（cancelled 9 态三文档一致、阈值界对齐、scanFreshness 引用 thresholds、ciphertext/keyVersion/covered 全名统一）。**新冲突**：(1) `ChangeItem.resourceRef` 仅 `{cloud, product, resourceId, clusterId}` 与 `DeployTarget` 的 "Namespace // k8s_api 必填"、"Kind // k8s_api 必填"、"AccountKey // cloud_api 必填" 冲突——异步执行期无法从持久化数据重构目标（-8）；(2) product 取值规则错位：schema 枚举含 "crd"（required product），DeployTarget 中 k8s 项 product 为空（-2） |
| **小计** | **150/170** | |

### Dimension 3: Error Handling（130）

| 准则 | 得分 | 依据 |
|------|------|------|
| Error types defined (0-45) | 44/45 | 15 码三列齐全，tech-design 与 api-handbook 完全闭合；新增 BATCH_NOT_CONFIRMABLE/CHANGE_NOT_CANCELLABLE/ROLLBACK_TARGET_INVALID 语义明确。扣分：api-handbook "#### 生成清单错误"表内含 "BATCH_NOT_CONFIRMABLE \| 续批门控未满足"与 "CHANGE_NOT_CANCELLABLE"——两者属 confirm-batch/cancel 路径而非清单生成，归组误导 |
| Propagation strategy clear (0-45) | 42/45 | 三层传播 + "同步错误 vs 异步子任务状态"语境映射（"CLOUD_API_RATELIMITED → ChangeItem.status=rate_limited…仅同步路径…才以 503 返回"）清晰。扣分：ScanSnapshot.status=failed 后的重试/恢复/告警路径仍缺失；扫描任务卡死 running 无超时处置（见盲点 2），属调度层静默失败风险 |
| HTTP status codes mapped (0-40) | 40/40 | 15 码→HTTP 全映射；异步语境消歧后 503 仅限同步触发路径，语义自洽；ROLLBACK_TARGET_INVALID 已归位回滚端点段落 |
| **小计** | **126/130** | |

### Dimension 4: Testing Strategy（130）

| 准则 | 得分 | 依据 |
|------|------|------|
| Per-layer test plan (0-45) | 44/45 | 七层矩阵（domain/deployer/service/web/probe/e2e）+ 4 条 Key E2E Flows；场景新增"到期分级 30→14→7→expired 升级…同级去重不重发"、"分批门控…BATCH_NOT_CONFIRMABLE"、"豁免 ∩ 验证窗口"、"主密钥轮换迁移"。扣分：缺 bootstrap"批量导入部分失败可单独重试"场景；缺 PRD"模拟通道端到端零上层改动"可插拔性演示 |
| Coverage target numeric (0-45) | 45/45 | 总 80% + 逐层 85/80/85/80/80/80 全量化 |
| Test tooling named (0-40) | 38/40 | go test、httptest、mongox test、本地 TLS server、envtest/假 APIServer 均命名。扣分：deployer 行 "go test + mock"（第三轮）仍未指明 mock 框架（gomock/testify 等） |
| **小计** | **127/130** | |

### Dimension 5: Breakdown-Readiness ★（180 — 闸门）

| 准则 | 得分 | 依据 |
|------|------|------|
| Components enumerable (0-65) | 61/65 | 可完整枚举：internal/cert 五层+ioc/cert.go、ExecutionChannel 2 实现+2 预留、CloudDeployer 6×2、ChangeService/CertService/IntegrityService/ReferenceDiscoveryService/ProbeService/InspectionService、scheduler 五类任务、CrdRegistration 管理、CertBatchSession、stats/dashboard 聚合、cloudx 证书 API 扩展、web 模块、密钥轮换迁移任务。扣分：(1) "验证通过后进入清理队列"的清理队列消费者不在任何组件/scheduler 清单（"(scan/probe/inspection/window-expiry/pause-timeout)"）中（-2）；(2) "false 时项标 failed 待复检"的复检执行体同样缺失（-1）；(3) huawei/aws/azure 三云发现适配组件未枚举（-1） |
| Tasks derivable (0-65) | 58/65 | 接口→实现、模型→schema、端点→handler 任务均可派生；批次分配算法、互斥索引、提频探测均已定量化。扣分：(1) resourceRef 无法重构 DeployTarget，执行子任务派生前须先改 schema（-3）；(2) K8s 管理权判定规则集与复检时窗第三轮仍为占位（"Reason string // …判定依据（label/ownerReference）"无具体 label/annotation 规则；复检时点/次数未给）——K8sAPIChannel 任务拆分前必答（-3）；(3) 单批阈值与"清单目标数上限"无配置字段（-1） |
| PRD AC coverage (0-50) | 36/50 | Story 1/2/4/6 全覆盖；Story 3/5 大部分覆盖（分批门控、回滚校验、审计比对端点均落实）。扣分：(1) **-4** ChangeReport 缺孤儿证书补偿清理结果与未达标清单字段（Story 3/5 AC 明列；仅审计 action "orphan_cleanup" 间接承载，报告↔审计逐条比对不闭合）；(2) **-2** "清理失败项触发告警"无路由（告警四类不含它）；(3) **-2** 华为云/AWS/Azure"不可执行项（首期无部署器）"单列未显式建模（Action 枚举 "upload_and_bind \| patch_crd" 无适合值，仅 AutoChangeable/Warnings 泛化承载）；(4) **-2** 平台自身监控（扫描/探测任务成功率）无落点；(5) **-2** 完整性定时复检缺位（InspectionService 仅 daysLeft）；(6) **-1** 模拟通道可插拔演示缺测试设计；(7) **-1** fingerprint_only 新证书发起更换无前置拦截 |
| **小计** | **155/180** | **★ 闸门未通过（<160）** |

### Dimension 6: Security Considerations（80）

| 准则 | 得分 | 依据 |
|------|------|------|
| Threat model present (0-40) | 39/40 | 五项具体威胁（私钥集中托管/误操作恶意批量更换/云侧凭证滥用/审计绕过/K8s 凭证泄露）。扣分：主密钥泄露→存量密文批量暴露仍未单列独立威胁（第三轮） |
| Mitigations concrete (0-40) | 37/40 | 每威胁配对策；主密钥轮换五步迁移（双活/双读/幂等再加密/失败回滚/人工下线）具体可执行；Credential "用后 zeroing，禁入日志/响应"。扣分：(1) PRD "备份恢复：平台数据丢失不得导致 EV/OV 私钥不可恢复（备份周期天级、恢复小时级）"无对应设计（密文库备份、主密钥备份、恢复演练均未提，第三轮）；(2) "渗透式自查口径：grep 全代码库"仍无 CI 门禁化描述 |
| **小计** | **76/80** | |

### Dimension 7: Implementation Feasibility（140）

注入上下文：Go 1.25 + Gin + MongoDB(mongox) + Redis + Wire DI；internal/cam DDD、internal/alert、internal/audit、internal/task、internal/asset 均在；cloudx 6 云适配；无 client-go。

| 准则 | 得分 | 依据 |
|------|------|------|
| Dependencies available (0-50) | 48/50 | cloudx/task/alert/audit/mongox/Redis/asset 均为现有模块且 asset 已列依赖表并定义数据流契约；client-go 显式标新增且已给锚定策略（"跟随目标集群最低 K8s 版本，首期兼容矩阵 1.24+"）+PoC 首批任务。扣分：三云发现适配的 cloudx 扩展未列（同 D1） |
| Architecture fits project structure (0-50) | 48/50 | "internal/cert（与 internal/cam 平级新域）"+ DDD 分层 + Wire 注入完全对齐既有模式；alert/audit/task/asset 复用并声明扩展点。扣分：scheduler 五类定时任务挂入 internal/task 的注册方式仍未示（第三轮小项） |
| Technical claims grounded (0-40) | 37/40 | 提频探测（verifyProbeIntervalMinutes 5~60 分钟）已使"连续 verifyConfirmProbes 次一致"在天级巡检外可达，上轮时序矛盾消解；批次 ≤50% 硬约束有算法落地（floor(total/2)）。扣分：云 API 限流"退避重试"仍无退避算法/重试上限/耗尽判据量化（PRD"重试耗尽方计为执行失败"未落参数） |
| **小计** | **133/140** | |

---

## Phase 3 — Blindspot Hunt（盲点狩猎）

> 量表七维之外的架构失败模式，均附原文引用。

### [blindspot] 1. executing 态无活性路径：worker 崩溃后互斥 token 永久持锁

活跃保障仅覆盖暂停态与验证态："暂停分批单保留互斥，但不会无限期持锁，两条释放路径——①人工取消…②超时自动中止：…扫描 `batchInfo.paused=true 且 pausedAt + thresholds.pauseTimeoutHours < now`"；verifying 有窗口到期终局判定。但 **executing 态**（internal/task 子任务派发后）既不可取消——"取消：draft/pending_confirm/批间暂停中可执行…其他状态 409 CHANGE_NOT_CANCELLABLE"——也无超时扫描：scheduler 清单 "(scan/probe/inspection/window-expiry/pause-timeout)" 无 executing-timeout 项。执行 worker 崩溃/云 API 挂起将使订单永久停留 executing，activeMutex 永不清除（"进入终态…时 `$unset` 清除"永不发生），该旧证书一切后续变更被 CHANGE_IN_FLIGHT 永久阻塞。需补：执行任务级超时/心跳 + executing 态超时自动转 partial_failed/人工中止路径。

### [blindspot] 2. 扫描快照卡死 running 无恢复：SCAN_IN_PROGRESS 永久阻断后续扫描

"SCAN_IN_PROGRESS | 扫描进行中（防重触发）" 依赖快照 status 判重，而快照状态枚举仅 `["running", "done", "failed"]` 且 "failReason // status=failed 时的原因码" 只覆盖显式失败。扫描任务进程崩溃（非优雅失败）时快照永久停留 running：新扫描 409 SCAN_IN_PROGRESS 永远被拒，最新成功快照停止刷新 → SCAN_STALE 阻断该范围内一切清单生成，形成无告警、无恢复的静默死锁。需补：running 态超时（startedAt + 上限）自动转 failed + 告警。

### [blindspot] 3. TLS 探测目标域来源与通配符 SAN 不可拨测未设计

全文档未定义探测域名清单的来源（台账全部证书 SAN 并集？去重？expectedDomain 参与与否？）——ProbeResult.domain 只有字段无枚举规则。更实质的是通配符 SAN：通配符/多 SAN 证书按证书粒度建模（PRD），则台账必然含 "*.example.com" 类 SAN，而 "ProbeService (crypto/tls Dial)" 无法对字面通配符做 DNS 解析与 SNI 拨测——通配符域名的探测处置（跳过/标记豁免/以引用资源主机名替代）完全未声明，"未豁免端点 TLS 探测覆盖"的 PRD Goal 在含通配符台账下不可判定。需补：探测目标枚举规则 + 通配符处置策略。

### [blindspot] 4. 双 protectUntil 双源真值无同步设计（第三轮遗留）

schema 中 `cert_certificates.protectUntil // 回滚保护期截止；>=now 禁删` 与 `cert_change_orders.protectUntil // 回滚保护期截止` 并存。禁删判定（"仅 no_refs_scanned 允许删除（protectUntil 保护期另计）"）读哪一份、订单进入 completed/partial_completed 时如何将保护期写回证书（回滚保护期的主体是**旧证书**："进入回滚保护期（7~14 天）：期间旧证书禁止删除"）、同一旧证书多张历史单的保护期如何合并——均未定义。两处时间戳无传播路径即双源真值，实现者各取其一将产生删除拦截漏洞。

---

## 最终汇总

| 维度 | 得分 | 满分 |
|------|------|------|
| Architecture Clarity | 158 | 170 |
| Interface & Model Definitions | 150 | 170 |
| Error Handling | 126 | 130 |
| Testing Strategy | 127 | 130 |
| Breakdown-Readiness ★ | **155** | 180 |
| Security Considerations | 76 | 80 |
| Implementation Feasibility | 133 | 140 |
| **总计** | **925** | **1000** |

- 总分 925 ≥ 900 target：**通过**。
- Breakdown-Readiness ★ 闸门（≥160）：**未通过**（155/180）——仍阻断进入 breakdown-tasks。
- Iteration-2 的四大阻断项（分批语义、双指标、CRD 登记、到期告警/时序）全部高质量消解，总分 903→925；但本轮在更深层发现新缺口：① ChangeItem.resourceRef 无法重构 DeployTarget（namespace/kind/accountKey 丢失）；② 孤儿清理队列与 CRD 待复检两个承诺的异步过程无执行组件；③ ChangeReport 缺 PRD 明列的孤儿清理结果/未达标清单字段；④ executing 态与扫描 running 态两个活性缺口。前三项直接压制闸门所测的"组件可枚举/任务可派生/AC 覆盖"。
- 本轮为最终预算轮：文档整体质量已高（925/1000），建议将上述四类缺口作为 breakdown-tasks 前置补丁（均属字段/组件级小改，不涉及架构返工）后放行。
