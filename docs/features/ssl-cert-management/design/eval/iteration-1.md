# 设计评估报告 — Iteration 1

> 评估对象：`docs/features/ssl-cert-management/design/`（tech-design.md + er-diagram.md + schema.sql + api-handbook.md）
> 上游 PRD：prd-spec.md（916/1000 通过）、prd-user-stories.md
> 评分量表：`forge/eval/rubrics/design.md`（1000 分制，target 900，Breakdown-Readiness ★ 闸门 ≥160/180）
> Iteration = 1（无前置报告）
> 评估立场：对抗式——寻找缺陷，不寻找加分理由；每项扣分均附文档原文引用。

---

## Phase 1 — Reasoning Audit（推理审计）

### 1.1 PRD→Design 覆盖追踪

逐条核对 PRD In-Scope / AC 与设计组件：

| PRD 要求 | 设计落点 | 状态 |
|---------|---------|------|
| 证书托管台账+完整性校验 | CertService+IntegrityService, Certificate, CERT_* errors | 覆盖 |
| 引用关系发现（全云+K8s CRD） | ExecutionChannel.Discover, CertReference, ScanSnapshot | 覆盖 |
| 到期监控告警 30/14/7 分级 | ProbeService+alert/channel | 部分覆盖（见 1.2） |
| TLS 主动探测+豁免 | ProbeService(crypto/tls Dial), ProbeResult, Exemption | 覆盖 |
| 一键批量更换+回滚 | ChangeService, ChangeOrder, ChangeItem | 覆盖 |
| 验证窗口 | ChangeService verify stage, VerifyWindowUntil | 覆盖 |
| 两段式+孤儿清理 | CloudDeployer, CloudCertMapping | 覆盖 |
| K8s CRD+管理权探测+复检 | K8sAPIChannel | 覆盖 |
| 执行通道抽象+预留 | ExecutionChannel（bastion/agent 接口预留） | 覆盖 |
| 权限审计 | EIAM+internal/audit | 覆盖（查询接口缺，见 1.2） |
| 前端页面 | e-cam-web cert 模块 | 覆盖 |

### 1.2 隐式耦合 / 静默错误路径 / 消除模式复入 / 并发 / 迁移

- **到期监控与 TLS 探测概念混用**：PRD Coverage Map 将"到期监控告警"映射到 `ProbeService + alert/channel`，但 ProbeResult.status 枚举（consistent/diff/unreachable/exempt）描述的是 TLS 探测结果，而非证书 notAfter 到期分级。到期分级（30/14/7）的计算逻辑与告警路由在设计中未单独建模，仅在看板响应 `countsByLevel[5]/daysLeft/level` 隐式出现，属隐式耦合——到期监控与探测被塞进同一服务却无独立职责描述。

- **覆盖率分母来源无设计**：Cross-Layer Data Map 注明"分母=资产同步独立盘点"，但全文无任何 service / data-flow / model 描述该"资产同步独立盘点"数据从何而来、由谁写入 ScanSnapshot.coverageMeta.total。这是一个被引用却未被设计的隐式外部依赖，且该分母直接承载 PRD Goal（覆盖率 ≥90%）的成功标准。

- **互斥索引无法强制互斥不变量**：索引策略表写 `cert_change_orders | oldCertFingerprint + status | 在途互斥查询`。但 PRD 要求"同一旧证书同一时刻仅允许一张活跃（待确认/执行中/验证中）变更单绑定"——活跃态是三值集合，对 (fingerprint, status) 建普通索引允许不同 status 的两单并存（如一张待确认+一张执行中），无法强制互斥。CHANGE_IN_FLIGHT 拦截在并发请求下存在竞态窗口。

- **回滚自身部分失败的状态缺口**：状态机有"已回滚 / 回滚失败(转人工)"两终态，无"部分回滚"中间态。ChangeItem.status 含 `rolled_back`，但 Rollback 接口 `Rollback(ctx, creds, target, oldRef)` 对逐项回滚的部分成功/部分失败语义未定义项级状态机与补偿路径（PRD 允许转人工，但项级终态须明确以免报告与台账不一致）。

- **错误表跨文档不一致**：tech-design 的 Error Types 表列 10 个码，api-handbook 额外返回 CERT_HAS_REFS、SCAN_IN_PROGRESS、FORBIDDEN 三个码——tech-design 缺这三个码的定义，属跨文档冲突。

- **无数据迁移**：PRD 明确"无存量数据迁移（新功能域）"，设计无需迁移策略，此项无问题。

- **消除模式复入**：Alternatives 对比本地信封加密 vs 云 KMS，选择本地信封加密，未复入被否决模式。无问题。

---

## Phase 2 — Rubric Scoring（量表评分）

### Dimension 1: Architecture Clarity（170）

| 准则 | 得分 | 依据 |
|------|------|------|
| Layer placement explicit (0-60) | 55/60 | "新增 `internal/cert` 功能域，沿用 e-cam DDD 分层（domain/repository/service/web/module + ioc wire 注入）"——分层显式且与 internal/cam 对齐；"ioc/cert.go 注入 Wire" 指明注入位置。扣分点：scheduler/定时任务、probe 子组件的层归属未单独声明。 |
| Component diagram present (0-60) | 54/60 | 存在 ASCII 组件图，展示 e-cam-web→cert/web→cert/service→{repository/deployer/scheduler}→{CloudDeployer/ExecutionChannel/audit/alert/EIAM/crypto}，关系可读。扣分点：图排版略乱，ExecutionChannel 与 CloudDeployer 的实现/调用关系在图中以文字并列、未用线连接，层次区分不严格。 |
| Dependencies listed (0-50) | 48/50 | 依赖表列 7 项（client-go、crypto/x509+tls、cloudx、task、alert、audit、mongox/Redis），类型+用途齐全。扣分点：`k8s.io/client-go` 未标版本；alert 标"复用+扩展"但未列扩展点。 |
| **小计** | **157/170** | |

### Dimension 2: Interface & Model Definitions（170，er-diagram 变体）

| 准则 | 得分 | 依据 |
|------|------|------|
| Interface signatures typed (0-40) | 36/40 | 三个接口（ExecutionChannel/CloudDeployer/ChangeService）参数与返回值均有类型标注而非散文。扣分点：`batchConf`、`itemIds` 大小写不一致且类型未注；`ChangeList`/`ChangeReport`/`DeployResult`/`RollbackResult` 等返回类型为命名但未定义的结构体（见下行）。 |
| Inline models concrete (0-40) | 23/40 | 持久模型在 schema.sql + er-diagram.md 中字段/类型/约束齐全。但非 DB 服务级类型完全未定义：`ChangeList`、`ChangeReport`、`DeployResult`、`RollbackResult`、`BatchConf`、`DiscoverScope`、`DeployTarget`、`Credential`、`CertReference`(DB 但 service 形态) 均无字段级定义——开发者须猜测这些载荷的形状。 |
| ER diagram complete (0-30) | 28/30 | Mermaid erDiagram 含 10 实体、关系、基数（`||--o{` / `}o--||`）、索引策略表齐全。扣分点：`THRESHOLDS` 嵌入 `ALERT_CONFIG` 用独立实体再标 embed，表达冗余且易误读为独立集合。 |
| SQL DDL directly usable (0-30) | 15/30 | 文件首行声明"实际无关系表，'表'对应 MongoDB 集合"——即文档伪 DDL，不可执行。无 DEFAULT 子句；FK 仅以注释说明非约束；索引不在 schema.sql 而在 er-diagram.md。MongoDB 语境下可理解，但量表要求"executed as-is"，此件不满足。 |
| Cross-layer consistency (0-30) | 18/30 | 命名冲突：Cross-Layer Map 写 `Certificate.EncryptedKey`，Quick Reference 写 `encryptedPrivateKey`，er-diagram 写 `encryptedPrivateKey`，schema.sql 写 `encrypted_private_key`——同一字段四处三名。语义错配：`scanFreshness`(int hours) 的 Backend Model 标为 `ScanSnapshot.StartedAt`(timestamp)，派生量与存储量混填。 |
| **小计** | **120/170** | |

### Dimension 3: Error Handling（130）

| 准则 | 得分 | 依据 |
|------|------|------|
| Error types defined (0-45) | 40/45 | 10 个错误码（CERT_KEY_MISMATCH 等）含 Name/Description/HTTP Status 三列。扣分点：tech-design 表缺 api-handbook 实际返回的 CERT_HAS_REFS、SCAN_IN_PROGRESS、FORBIDDEN——错误码定义跨文档不闭合。 |
| Propagation strategy clear (0-45) | 38/45 | "web 层捕获 domain error→映射 HTTP…；service 层…返回明确语义错误；deployer 层…子任务标记失败状态+原因，不中断其他项"——三层传播策略清晰。扣分点：scheduler/probe 层（定时扫描/探测任务）失败传播未描述；scan_snapshot.status=failed 之后的恢复/重试路径未定义，属静默失败风险。 |
| HTTP status codes mapped (0-40) | 33/40 | 错误码→HTTP 状态映射表存在。扣分点：CLOUD_API_RATELIMITED/K8S_UNREACHABLE 标 503，但批量执行走异步任务框架（internal/task），这两个码实际是子任务状态而非 HTTP 响应——异步语境下 503 映射语义模糊；CERT_HAS_REFS 等三码未在本表。 |
| **小计** | **111/130** | |

### Dimension 4: Testing Strategy（130）

| 准则 | 得分 | 依据 |
|------|------|------|
| Per-layer test plan (0-45) | 43/45 | 五层测试矩阵（domain/deployer/service/web/probe）含 Test Type/Tool/What/Coverage。扣分点：缺 E2E 层——PRD user stories 含"换证→回滚→孤儿清理""分批灰度"等端到端关键流，测试矩阵未列 E2E/验收层。 |
| Coverage target numeric (0-45) | 45/45 | 总目标 80% + 逐层目标（85/80/85/80/80%）均量化。 |
| Test tooling named (0-40) | 34/40 | 命名 go test、httptest、mongox test、本地 TLS server。扣分点："go test + mock"未指明 mock 框架（gomock/testify-mock 等）；K8s 管理权探测的测试是否用 envtest/假 APIServer 未说明。 |
| **小计** | **122/130** | |

### Dimension 5: Breakdown-Readiness ★（180 — 闸门）

| 准则 | 得分 | 依据 |
|------|------|------|
| Components enumerable (0-65) | 62/65 | 组件可枚举：internal/cert（domain/repository/service/web + ioc/cert.go）、ExecutionChannel 2 实现（CloudAPIChannel/K8sAPIChannel）+ 2 预留、CloudDeployer（6 deployer×2 云）、ChangeService、CertService、IntegrityService、ReferenceDiscoveryService、ProbeService、scheduler、cloudx 扩展、e-cam-web cert 模块。扣分点：到期分级计算组件未单列（隐于 ProbeService）。 |
| Tasks derivable (0-65) | 58/65 | 接口→实现任务、模型→schema 任务可派生。扣分点：未定义的服务级类型（ChangeList/ChangeReport 等）派生任务时须先补类型定义，存在猜测面；cloudx 各云各产品扩展方法的拆分粒度未给（6 deployer×2 云=12 个适配任务是否按云×产品拆？未明确）。 |
| PRD AC coverage (0-50) | 42/50 | 绝大多数 AC 有设计落点。未覆盖/部分覆盖：(1) Story 2 AC"标记为'未发现引用'（区别于'无引用'）"——设计未显式区分两态；(2) Story 5 AC"可通过审计接口按变更单号查询并与报告逐条比对一致"——api-handbook 无审计查询端点、tech-design 未描述审计查询接口；(3) 自定义 CRD"经登记后纳入扫描范围"的登记机制未设计；(4) 覆盖率分母来源未设计（见 Phase 1）。 |
| **小计** | **162/180** | ≥160 闸门通过（险过） |

### Dimension 6: Security Considerations（80）

| 准则 | 得分 | 依据 |
|------|------|------|
| Threat model present (0-40) | 38/40 | 五项具体威胁：私钥集中托管（高价值目标）、误操作/恶意批量更换、云侧凭证滥用、审计绕过、K8s 凭证泄露。扣分点：未列"主密钥泄露后存量密文批量暴露"作为独立威胁（仅隐于私钥集中托管条目内）。 |
| Mitigations concrete (0-40) | 38/40 | 每威胁配对策：AES-256-GCM 信封加密+keyVersion 轮换+内存解密+zeroing；接口永不返回明文（渗透式自查）；人工确认+完整性前置+指纹精确匹配+扫描新鲜度+分批≤50%+回滚兜底+EIAM+全量审计；复用云 AK/SK 加密存储；kubeconfig 加密+最小 RBAC；EIAM 三角色+仅追加审计。扣分点：主密钥轮换的存量密文再加密迁移流程未细化；"渗透式自查"无具体 grep 规则/CI 门禁描述。 |
| **小计** | **76/80** | |

### Dimension 7: Implementation Feasibility（140）

注入上下文：Go 1.25 + Gin + MongoDB(mongox) + Redis + Wire DI；现有 internal/cam（DDD）、internal/alert、internal/audit、internal/task、internal/shared/cloudx（aliyun/tencent/huawei/aws/azure/volcano）；尚无 client-go 依赖。

| 准则 | 得分 | 依据 |
|------|------|------|
| Dependencies available (0-50) | 45/50 | 引用模块均存在：cloudx/task/alert/audit/mongox/Redis 均为现有；client-go 标"新增"且项目无该依赖——设计已显式承认新增。crypto/x509+tls 为标准库。无与约定冲突项。扣分点：client-go 是重依赖，未标版本与 K8s API 兼容范围。 |
| Architecture fits project structure (0-50) | 48/50 | "internal/cert（与 internal/cam 平级新域）"+ DDD 分层 + Wire 注入，完全对齐现有 internal/cam 模式；cloudx 扩展复用现有适配层；alert/audit/task 复用现有框架。未引入与既有架构矛盾的模式。扣分点：scheduler 定时任务挂入 internal/task 的具体注册方式未示。 |
| Technical claims grounded (0-40) | 36/40 | 容量量级"证书百张、引用点数千、单轮扫描分钟到小时级"与 PRD 一致；技术选型基于 Go 标准库+云 SDK。扣分点：Alternatives 称可插拔通道"零上层改动验证"未给验证口径；云 API 限流退避的具体策略（退避算法/重试上限）未量化。 |
| **小计** | **129/140** | |

---

## Phase 3 — Blindspot Hunt（盲点狩猎）

> 以下为量表七维之外的架构失败模式，均附文档原文引用。

### [blindspot] 1. 互斥不变量缺乏强制机制（并发访问未同步）

PRD 要求"同一旧证书同一时刻仅允许一张活跃变更单绑定"。设计的索引 `cert_change_orders | oldCertFingerprint + status | 在途互斥查询` 无法强制此不变量——活跃态是 {待确认,执行中,验证中} 三值集合，对 (fingerprint, status) 建普通索引允许不同 status 的两单并存（如一张待确认 + 一张执行中均通过唯一性检查）。CHANGE_IN_FLIGHT 的应用层检查在并发请求下存在 check-then-insert 竞态窗口。需改为：分布式锁（Redis）或部分唯一索引（MongoDB 无部分唯一索引，可用"活跃互斥 token"字段 + 条件 upsert）。

### [blindspot] 2. 覆盖率分母来源为未设计的隐式外部依赖

Cross-Layer Data Map 注明 `coverageMeta | 分母=资产同步独立盘点`，但全文无 service / data-flow / model 描述该"资产同步独立盘点"数据的来源、写入时机与同步触发器。ScanSnapshot.coverageMeta.total 被存储却无填充路径设计。该分母直接承载 PRD Goal（登记覆盖率/可更换托管覆盖率 ≥90%）的成功标准——分母来源不可靠则覆盖率指标失真。需设计独立的资产盘点数据源（如 CMDB 同步任务或云资源清单 API 汇总）及其与 ScanSnapshot 的写入契约。

### [blindspot] 3. 主密钥轮换的存量密文迁移流程缺失

安全缓解措施声明"keyVersion 支持轮换"，但未描述轮换流程：旧 keyVersion 的存量密文如何再加密迁移到新 keyVersion？迁移期间的读路径如何按 keyVersion 路由解密？迁移失败回滚？这是"私钥集中托管"威胁下的关键运维路径，缺失则 keyVersion 字段沦为装饰。需补存量密文再加密迁移任务设计（离线批量 + 在线双读）。

### [blindspot] 4. 验证窗口内"变更关联告警"与"常规差异告警"的路由切换未建模

PRD 要求验证窗口内差异以"变更关联告警"呈现、窗口关闭或达标后恢复"常规告警"。设计的 ProbeResult.status 仅 4 值（consistent/diff/unreachable/exempt），无"变更关联 diff"子类型；AlertConfig 无窗口期路由规则字段；ChangeService verify stage 与 alert/channel 之间如何传递"当前处于验证窗口+本变更单的预期终态"以切换告警路由，全程未设计。这是探测→告警链路的隐式耦合，开发者无从实现差异化告警。

### [blindspot] 5. K8s 管理权探测的判定规则与回写复检的并发安全未细化

K8sAPIChannel 描述"更新前探测资源管理权…判定为受管理的目标标记不可自动变更…变更后复检防 reconcile 回写"。但：(a) "基于资源元数据的标注/注解与清单来源信息"判定管理权的具体规则集未给（哪些 label/annotation 判 GitOps？哪些判控制器 ownerReference？）；(b) patch 后 reconcile 回写复检的时间窗口与重试策略未量化（立即复检？N 秒后？几次复检？）——若复检窗口过短会漏判 reconcile 回写，过长则阻塞变更单流转。

---

## 最终汇总

| 维度 | 得分 | 满分 |
|------|------|------|
| Architecture Clarity | 157 | 170 |
| Interface & Model Definitions | 120 | 170 |
| Error Handling | 111 | 130 |
| Testing Strategy | 122 | 130 |
| Breakdown-Readiness ★ | 162 | 180 |
| Security Considerations | 76 | 80 |
| Implementation Feasibility | 129 | 140 |
| **总计** | **877** | **1000** |

- Breakdown-Readiness 闸门（≥160）：**通过**（162/180，险过）。
- 总分（≥900 target）：**未达**，需进入 iteration 2 修订。
- 主要失分集中在 Dimension 2（服务级类型未定义、schema.sql 不可执行、跨层命名冲突）与 Dimension 3（错误表跨文档不闭合、异步码 HTTP 映射模糊）。Phase 3 盲点 1/2/4 为阻断性并发与数据流缺陷，建议优先修订。
