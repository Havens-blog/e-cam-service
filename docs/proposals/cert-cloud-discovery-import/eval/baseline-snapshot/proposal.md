---
created: "2026-08-25"
author: "Haven"
status: Draft
intent: "new-feature"
---

# Proposal: 证书云端发现导入（cert-cloud-discovery-import）

## Problem

证书台账当前为空：已上线的证书管理功能域（台账/引用扫描/变更状态机/双云部署器）依赖台账中有证书才能运转，而现有导入通道仅有"手工收集 PEM 文件上传"（单张/批量）。运维人员需逐云登录控制台导出存量证书再上传，首次登记门槛高、易遗漏，导致台账长期空置、引用扫描结果（CertReference 已按指纹落库）无法关联到证书、覆盖率与到期监控失效。

### Evidence

- 台账页空态文案（`e-cam-web/src/views/cert/ledger/index.vue:28`）："尚未导入任何证书，可批量导入存量证书完成首次登记"——用户点击 CTA 后仍需手工备齐 PEM 文件，首次登记闭环断裂。
- 后端引用扫描（`internal/cert/service/reference_scan_service.go`）已能在五云+K8s 发现证书引用并解析指纹落库，但发现的数据只用于引用关联，无法转化为台账登记。
- 云适配层 GetCert 已能取回公钥 PEM（阿里 `response.Cert`、腾讯 `CertificatePublicKey`、Azure KeyVault secret、AWS ACM GetCertificate），当前仅用于算指纹后即丢弃。

### Urgency

证书管理功能域 38 任务已交付但台账空置 = 功能不可用状态。存量证书散落各云，每多等一天，到期监控盲区多存在一天；一旦有证书临近到期未被发现，直接产生线上事故风险。本功能是打通"发现→登记"最后一公里、让已交付功能域产生实际价值的前置条件。

## Proposed Solution

新增"从云端导入"能力：以最近一次引用扫描快照为数据源，预览-确认两步完成存量证书首次登记。

**用户旅程**：台账页（空态 CTA"从云端导入存量证书" / 工具栏按钮）→ 预览列表（基于最近 done 快照纯 DB 聚合比对，秒出：云/账号/云证书 ID/引用资源数/按指纹判定的"已在台账"标记/到期时间）→ 勾选（默认全选未登���项，已在台账项灰选）→ 确认 → 异步导入会话（逐证 GetCert 取公钥 PEM→解析→指纹登记→CloudCertMapping 幂等建档；单条失败记因不中断）→ 进度轮询（复用批量导入会话交互）→ 完成刷新台账，存量 CertReference 按指纹自动关联，覆盖率即时生效。

**托管形态**：仅指纹登记（hostingStatus=fingerprint_only），私钥不回传；后续需要托管变更时用已有"补传私钥"（UploadKey）升级 complete。权限沿用导入类端点 RoleOpsEngineer。

### Innovation Highlights

行业常规做法（ cert-manager / 各云 Cert Manager 服务）是"部署时登记"——证书进入平台托管即登记。本方案补齐的是"存量回溯登记"路径：复用引用扫描已沉淀的发现数据作为登记数据源，将"扫描（只读发现）"与"登记（台账写入）"解耦为两步可控操作，登记后又反哺引用关联形成闭环。这不是新技术，而是对已有五云发现通道的价值复用——增量工作集中在 PEM 通道暴露与会话编排，无新云 API 面（除 AWS ACM 既有 GetCertificate）。

## Requirements Analysis

### Key Scenarios

- **首次登记（主路径）**：台账空 + 已有扫描快照 → 预览全量未登记 → 确认导入 → 部分成功（partial_failed 可见逐条原因）→ 重跑仅处理剩余项（幂等）。
- **无扫描快照**：预览端点返回明确错误码（NO_SNAPSHOT），前端引导一键触发既有扫描，完成后自动进入预览。
- **重复执行/并发**：指纹已在台账 → 预览标记"已在台账"不可选；导入中并发到达同指纹 → 依赖现有 uk_fingerprint 哨兵（ErrDuplicateFingerprint）记为该条跳过，不报错。
- **云侧证书已删除**：GetCert Exists=false → 该条失败记因"云侧已不存在"。
- **华为云引用**：无 PEM 能力 → 预览整组标记"该云暂不支持自动解析"，不可选。
- **占位指纹引用**（扫描时无法解析指纹，如腾讯 SHA-1 口径）：预览标记"导入时解析"；导入时 GetCert 拿 PEM 解析成功则正常登记，失败记因。
- **多账号同证书**：同一指纹被多账号引用 → 单条台账记录（uk_fingerprint），多条 CertReference 自动关联，多账号各建 CloudCertMapping（uk_fp_cloud_account 两段去重）。
- **权限**：非 OpsEngineer 访问预览/导入端点 → 403（复用 RequireRoles）。

### Non-Functional Requirements

- **安全**：公钥 PEM 仅用于解析登记（台账 CertPEM 字段存储，与手工导入同口径）；私钥全程不触及；云凭证仅内存使用禁入日志（沿用扫描链路约束）。
- **性能**：预览端点纯 DB 聚合（快照引用 + 台账指纹比对），响应 < 1s；导入会话逐条限速调用云 API（复用各适配器 waitRateLimit），整体限时防泄漏。
- **可靠性**：导入会话先持久化再异步执行（浏览器中断不丢结果，重开可见）；单条失败/panic 不中断会话（对齐批量导入 Hard Rule）。

### Constraints & Dependencies

- 数据源依赖：必须先有 status=done 的 ScanSnapshot（前端已引导）。快照过期策略沿用现状，不做新校验。
- 云 API 依赖：阿里 CAS GetUserCertificateDetail、腾讯 SSL DescribeCertificateDetail、Azure KeyVault GetSecret、AWS ACM GetCertificate 均为已有通道或既有 SDK 面；华为云 SCM ShowCertificate 无 PEM 字段（SHA-1 指纹口径），本期不支持。
- K8s 侧 CRD 引用不纳入导入（引用关联自动生效，但证书材料不从 K8s 读取）。
- 前端依赖 e-cam-web `feat/platform-user-management` 分支现有台账页与批量导入会话进度组件。

## Alternatives & Industry Benchmarking

### Industry Solutions

- **cert-manager（K8s 生态）**：仅管理自己签发/部署的证书，存量回溯需人工；无"从云发现登记"概念。
- **商业证书生命周期管理（KeyChest、AppViewX 等）**：以网络探测（TLS 握手）+ 云 API 发现做存量盘点，发现即登记入库存。本方案与其"发现驱动登记"理念一致，但数据源复用自有引用扫描而非独立探测。

### Comparison Table

| Approach | Source | Pros | Cons | Verdict |
|----------|--------|------|------|---------|
| Do nothing（仅手工批量导入） | 现状 | 零开发成本 | 首次登记门槛高、易遗漏，台账持续空置，已交付功能域无产出 | Rejected: 功能域价值被空台账卡死 |
| TLS 探测发现登记（网络面扫端口） | 商业 CLM 常规做法 | 覆盖面最全（含自建） | 需网络可达性与探测资产管理，工程量大且与已有引用扫描重复 | Rejected: 重复建设，超出 quick 范围 |
| CAS 证书库全量列取导入 | 各云 ListCertificates API | 含未被引用证书 | 需新适配 5 云列取 API；大量无用证书入场 | Deferred: v2 按需追加 |
| **引用驱动发现导入** | 本提案 | 复用已有扫描/GetCert/会话三链路，零新云 API 面；导入即关联引用，覆盖率即时生效 | 未被引用的库内证书不入场（可手工导入兜底） | **Selected: 最小增量打通首次登记闭环** |

## Feasibility Assessment

### Technical Feasibility

高。四云 PEM 获取通道已在代码中验证可得（阿里/腾讯/Azure 已确认，AWS ACM GetCertificate 接口已在适配层定义，tech-design 复核响应映射）；台账写入复用 ImportCert 既有校验/加密/落库路径（keyPEM 为空分支即 fingerprint_only）；会话模型参照 CertBatchSession 既有实现。无技术阻塞项。

### Resource & Timeline

单人 + AI 流水线，quick 模式预估 8-12 个任务（后端 4-5、前端 3-4、测试随任务）。存量扫描/导入基建成熟，风险集中在 AWS PEM 映射确认与前端 Modal 交互细节。

### Dependency Readiness

全部就绪：五云适配器、扫描快照仓储、台账仓储、信封加密、批量会话交互均已上线；无外部阻塞（区别于 doc-fix-1 等云凭证活体验证事项——本功能单测以 fake 适配器覆盖，不依赖真实凭证）。

## Assumptions Challenged

| Assumption | Challenge Tool | Finding |
|------------|---------------|---------|
| "证书还没有"= 需要从零造导入功能 | XY Detection | 推翻：手工批量导入已存在，真实痛点是证书材料散落云侧、手工收集成本高 → 需要的是云端发现导入而非新上传通道 |
| 存量登记需覆盖云账号内全部证书 | Assumption Flip（翻转口径：全量 vs 引用） | 修正：首次登记的核心价值是"被使用的证书"可见可监控；库内未引用证书v2 按需追加（用户已确认） |
| 导入应带回私钥一步到位 complete | Stress Test（安全面放大） | 推翻：云侧私钥下载行为不一致且权限敏感；fingerprint_only + 已有补传私钥升级路径更稳（用户已确认） |
| 预览需调云 API 逐证核验 | Occam's Razor | 推翻：扫描时指纹已落库，纯 DB 比对即可秒出预览；云 API 调用后置到确认导入 |

## Scope

### In Scope

- 后端：发现预览端点（GET /api/v1/certs/discovery/preview）——基于最近 done 快照聚合唯一证书清单 + 台账指纹比对 + 华为云/占位指纹标记，无快照返回 NO_SNAPSHOT 错误码
- 后端：发现导入会话端点（POST /api/v1/certs/discovery/import + 会话进度 GET）——勾选条目异步导入，逐条 GetCert→解析→指纹登记→CloudCertMapping 幂等建档，失败记因不中断，终态 completed/partial_failed
- 后端：四云 GetCert 通道扩展暴露公钥 PEM（阿里/腾讯/Azure/AWS；华为返回不支持标记）
- 前端：台账页"从云端导入"入口（空态 CTA 增强 + 工具栏按钮）+ 预览 Modal（分组列表、已在台账灰选、华为不支持提示）+ 导入进度（复用批量导入会话进度交互）+ 无快照引导扫描
- 测试：后端单测（服务编排/适配 PEM 扩展/权限矩阵/会话终态收敛）+ 前端 vitest 组件测试

### Out of Scope

- CAS 证书库全量列取（未引用证书入场）——v2
- 私钥拉取或补传自动化——维持手动补传
- K8s CRD 证书材料导入（引用关联自动生效，不读 K8s 材料）
- 华为云 PEM 解析（等 SCM API 提供 PEM）
- 定时自动发现导入（本期仅手动触发）
- 扫描能力本身的变更（快照/引用/覆盖率逻辑不动）

## Key Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| AWS ACM GetCertificate 响应未映射 PEM（仅有接口定义） | M | M | tech-design 首任务复核 aws/cert_discovery.go 响应字段；若缺失则 AWS 降级为"暂不支持"标记，不阻塞其余四云中的三云交��� |
| 大账号证书量导致导入会话超时（逐证限速） | M | M | 会话整体限时对齐批量导入 10 分钟口径并分批记录进度；超时条目记因可重跑（幂等） |
| 预览与导入间云侧状态漂移（预览后证书被删） | M | L | 导入时逐条实时 GetCert 校验 Exists，删除记因跳过；预览明确标注"基于快照时点" |
| 占位指纹条目导入时解析失败比例高（腾讯 SHA-1 场景） | L | M | 该类条目预览标记"导入时解析"；失败逐条记因可见，不污染会话终态（partial_failed 语义已有） |
| 前端 Modal 与既有批量导入进度组件耦合改坏现有交互 | L | M | 进度组件仅复用不改内部实现；新增交互独立组件，vitest 回归 289 用例全绿为准 |

## Success Criteria

- [ ] 已有 done 快照且台账为空时，预览端点返回唯一证书清单条目数 = 快照内引用按（cloud+accountKey+cloudCertId）去重后的证书数（含占位指纹条目与华为云不可选组），响应耗时 < 1s（纯 DB 聚合，无云 API 调用）
- [ ] 预览返回的每个条目含 cloud/accountKey/cloudCertId/引用资源数/指纹存在性（inLedger 布尔）/notAfter/可解析标记 七类字段，且已在台账条目 inLedger=true
- [ ] 无 done 快照时预览端点返回 NO_SNAPSHOT 结构化错误码（非 500），前端展示"先执行扫描"引导并可直接触发既有扫描
- [ ] 确认导入后创建会话（202 语义），逐条处理：阿里/腾讯/Azure 证书 PEM 解析成功→台账新增 fingerprint_only 记录 + CloudCertMapping 建档；单条失败记 errorReason 不中断后续条目
- [ ] 同一会话重放（相同条目再次导入）不产生重复台账记录（uk_fingerprint 指纹去重生效，重复项记"已在台账"类原因）；幂等重跑收敛 completed/partial_failed 终态
- [ ] 导入完成后，存量 CertReference 按指纹自动关联（台账详情引用列表非空），多账号同证书仅 1 条台账记录且 CloudCertMapping 按账号各 1 条
- [ ] 华为云条目在预览中整组标记不可选（不支持自动解析），导入请求含该云条目时该条记因跳过
- [ ] 预览/导入/进度端点对非 OpsEngineer 角色返回 403（权限矩阵单测覆盖）
- [ ] 私钥全程不落库不入日志：导入路径仅写 CertPEM/指纹及解析字段，EncryptedPrivateKey 为空、hostingStatus=fingerprint_only（单测断言）
- [ ] 前端 vitest 全绿（含新增组件用例），后端 go test 全绿（含服务/适配/权限新增用例）

consistency_check_result:
  status: pass
  pairs_checked: 40
  conflicts_found: 2  # 均为表述口径问题（可解析口径矛盾、字段计数错误），已当场修正后复检通过

## Next Steps

- quick 模式：直接进入 `/quick-tasks` 生成任务并执行（不走 PRD/design）
