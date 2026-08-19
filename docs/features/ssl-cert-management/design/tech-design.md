---
created: "2026-08-14"
prd: prd/prd-spec.md
status: Draft
---

# Technical Design: SSL 证书统一托管与更换（证书管理功能域）

> 上游：PRD（916/1000 通过）、UI 设计（972/1000 通过）。
> Override: API handbook enabled by signal "新 API/接口"（证书管理全新接口面，见 design/api-handbook.md）
> Override: Security Review enabled by signal "加密/权限/私钥"（私钥信封加密、EIAM 权限）

## Overview

新增 `internal/cert` 功能域，沿用 e-cam DDD 分层（domain/repository/service/web/module + ioc wire 注入）。复用多云 SDK 适配层（`internal/shared/cloudx`，扩展证书 API）、异步任务框架、`internal/alert` 通道框架、`internal/audit` 审计、EIAM。新增依赖 `k8s.io/client-go`；证书解析与 TLS 探测用 Go 标准库 `crypto/x509` + `crypto/tls`。

核心抽象：`ExecutionChannel` 接口隔离"发现/部署/回滚"与目标资源类型，云 API 与 K8s 两实现首期落地，堡垒机/优维 Agent 接口预留二期。

## Architecture

### Layer Placement

`internal/cert`（与 `internal/cam` 平级新域），通过 `ioc/cert.go` 注入 Wire。前端落在 e-cam-web 新增 `views/modules/cert/` 路由模块。

### Component Diagram

```
[e-cam-web] ──HTTP/Gin──▶ [cert/web (handlers)]
                              │
                       [cert/service]
            ┌─────────────────┼──────────────────────────┐
   [cert/repository→MongoDB]  [cert/deployer]        [scheduler 定时任务]
            │            ┌────────┴────────┐  (9 类定时任务,见下
       [audit/audit]   [CloudDeployer]  [ExecutionChannel 抽象]  Scheduler Tasks 表)
            │  aliyun/tencent: 部署器    ├─ CloudAPIChannel(实现)
            │  huawei/aws/azure:         ├─ K8sAPIChannel(实现, client-go)
       [alert/channel]  discovery-only    └─ BastionChannel/AgentChannel(接口预留)
            │     CDN/DCDN/WAF/LB          [cert/cloudx 扩展]
       (webhook+email)  两段式上传+绑定     上传/绑定(aliyun/tencent)
            │                            +五云 ListReferences 只读发现
            │
       [EIAM 权限/审计]          [crypto/x509 解析][crypto/tls 探测]
```

### Scheduler Tasks（定时任务清单，注册于 internal/task）

| 任务 | 触发条件 | 频率/时点 | 行为 |
|------|----------|-----------|------|
| scan | 定时 + 手动（POST /:id/scan，防重 409 SCAN_IN_PROGRESS） | 天级 | 五云引用发现，写 ScanSnapshot |
| scan-timeout | ScanSnapshot.status=running 且 now > startedAt + thresholds.scanTimeoutHours | 每 15 分钟扫一次 | 快照转 failed（failReason=SCAN_TIMED_OUT）+ 告警，释放防重锁，允许重新触发扫描（消除进程崩溃后 running 卡死的静默死锁） |
| probe | 定时 | 天级；验证窗口内对该批域名提频至 verifyProbeIntervalMinutes | 台账证书 SAN 展开域名 TLS 探测（目标域来源与通配符处置见"探测目标域来源与通配符处置"） |
| inspection | 定时 | 天级 | 到期分级计算 + expiryAlertLevel 去重告警 |
| window-expiry | status=verifying 且 verifyWindowUntil <= now | 周期 = verifyProbeIntervalMinutes | 窗口终局判定：全部达标→completed；存在未达标→partial_completed + 未达标清单写入 ChangeReport.UnmetDomains + 恢复常规告警 |
| pause-timeout | batchInfo.paused=true 且 pausedAt + pauseTimeoutHours < now | 每小时 | 批间暂停超时自动取消（未执行项标 skipped）+ 处置通知 |
| orphan-cleanup | CloudCertMapping.status=orphan 且归属变更单验证达标/终态 | 天级批扫 + 事件触发（验证窗口达标关闭后即时消费该单孤儿清理队列） | 调 CloudDeployer.CleanupOrphan 逐项清理，结果写入变更报告 OrphanCleanup；清理失败项告警（运维处置类通知，不计入 PRD 四类业务告警口径） |
| crd-recheck | ChangeItem(action=patch_crd) patch 完成 | patch 后延迟 thresholds.recheckDelayMinutes（默认 5 分钟）执行一轮 | 复检 CRD 证书引用字段：仍为新证书→success；被 reconcile 回写→failed + 告警。复检次数 1，失败转人工（见"K8s 管理权判定与变更后复检"） |
| executing-timeout | ChangeItem.status=running 且 heartbeatAt + thresholds.itemHeartbeatTimeoutMinutes < now | 每 5 分钟扫一次 | 超时项标 failed（error=EXEC_TIMEOUT）+ 告警，单据状态按剩余项重算（executing 态活性保障） |

### Dependencies

| 依赖 | 类型 | 用途 |
|------|------|------|
| `k8s.io/client-go` | 新增 | K8s API Server 直连，CRD patch/读取。版本锚定：跟随目标集群最低 K8s 版本，首期兼容矩阵 1.24+，具体客户端版本经首批 PoC 任务验证后锁定（见 Open Questions） |
| `internal/asset` | 复用 | 覆盖率分母独立盘点数据源（asset Instance 按 Provider×资源模型聚合，数据流契约见"覆盖率分母"节） |
| `crypto/x509` / `crypto/tls` | 标准库 | 证书解析、SAN/链/有效期校验、TLS 握手探测 |
| `internal/shared/cloudx` | 扩展 | 五云发现适配：aliyun/tencent 复用云账号凭证 + SDK，新增证书上传/列出/绑定方法（首期部署器云）；huawei/aws/azure 仅新增 ListReferences 只读发现适配（discovery-only，无上传/绑定扩展） |
| `internal/task` | 复用 | 异步任务框架承载批量执行/扫描/探测 |
| `internal/alert` | 复用+扩展 | webhook + 邮件通道（channel 框架已存在） |
| `internal/audit` | 复用 | 变更全量审计记录 |
| `mongox` / Redis | 复用 | 持久化 + 限流缓存 |

## Interfaces

### Interface 1: ExecutionChannel（执行通道抽象）

```
type ExecutionChannel interface {
    Discover(ctx, creds Credential, scope DiscoverScope) ([]CertReference, error)
    Deploy(ctx, creds Credential, target DeployTarget, newCertFingerprint string) (DeployResult, error)
    Rollback(ctx, creds Credential, target DeployTarget, oldRef CertReference) (RollbackResult, error)
    Type() ChannelType  // "cloud_api" | "k8s_api" | "bastion" | "agent"
}
```
实现：`CloudAPIChannel`（含两段式 UploadCert+BindResource+CleanupOrphan）、`K8sAPIChannel`（CRD patch + 管理权探测 + 复检）。K8sAPIChannel 扫描范围 = 固定枚举（ALBConfig/Ingress/Gateway/HTTPRoute）+ `cert_crd_registrations` 登记表联动：enabled=true 的登记项按 apiGroup/kind 遍历、按 certFieldPath 读取证书引用字段；未登记 CRD 属盲区并在视图声明（见"自定义 CRD 登记管理"）。

### Interface 2: CloudDeployer（per 云 per 产品）

```
type CloudDeployer interface {
    UploadCert(ctx, creds, pem, key) (cloudCertId string, err error)   // 两段式第一段
    BindResource(ctx, creds, resourceId, cloudCertId) error             // 第二段
    ListReferences(ctx, creds, product) ([]CertReference, error)        // 只读发现
    GetCert(ctx, creds, cloudCertId) (CloudCertInfo, error)            // 回滚目标有效性校验（只读）
    CleanupOrphan(ctx, creds, cloudCertId) error                       // 孤儿清理
}
```
首期实现：阿里云 + 腾讯云的 CDN/DCDN/WAF/LB（6 产品 × 2 云 = 12 个可部署 deployer）+ 华为云/AWS/Azure 三云 discovery-only 适配��3 个）：discovery-only CloudDeployer 仅实现 ListReferences/GetCert（只读发现，引用纳入台账与覆盖率分母），UploadCert/BindResource/CleanupOrphan 返回哨兵错误 ERR_DISCOVERY_ONLY（未实现）；三云引用进入变更清单时为不可执行项（AutoChangeable=false + Warnings 声明"首期无部署器"），待二期部署器落地后开放执行。

回滚目标有效性校验路径：`ChangeService.Rollback` 对每个待回滚成功项先调 `GetCert(oldCloudCertId)`——Exists=false（云侧已删除）、NotAfter < now（已过期）或 Fingerprint ≠ 订单 oldCertFingerprint（目标被替换）→ 该项标 ROLLBACK_TARGET_INVALID，不自动回滚、转人工决策并记录审计。

### Interface 3: ChangeService（变更单生命周期）

```
type ChangeService interface {
    GenerateChangeList(ctx, oldCertFingerprint, newCertId) (ChangeList, error)  // 指纹聚合+四项前置校验：扫描新鲜度、SAN⊇、在途互斥、新证书 hostingStatus=complete（fingerprint_only 无私钥无法上传云证书库 → 409 NEW_CERT_FINGERPRINT_ONLY）
    Confirm(ctx, orderId, batchConf) error                                       // 快照确认时点重校验；分批单在此固化批次分配（见"分批执行门控"）
    Execute(ctx, orderId) error                                                  // 派发子任务执行当前批（ChangeItem.batchNo = batchInfo.currentBatch）
    ConfirmBatch(ctx, orderId) error                                             // 人工续批：仅当上一批全部 success 且批级验证达标（提频探测连续 verifyConfirmProbes 次一致）放行，否则 409 BATCH_NOT_CONFIRMABLE
    Rollback(ctx, orderId, itemIds []string) error                               // 仅成功项；前置 GetCert 有效性校验（见 Interface 2），无效项 ROLLBACK_TARGET_INVALID 转人工
    Cancel(ctx, orderId) error                                                   // 取消：draft/pending_confirm/批间暂停中整单取消（未执行项标 skipped，转 cancelled 终态并清 activeMutex）；executing 态中止路径=仅 pending 项立即标 skipped，running 项不中断、等待其完成后按剩余项重算收敛至 cancelled（成功项保留结果可供回滚）；verifying 及终态 409 CHANGE_NOT_CANCELLABLE
    GetReport(ctx, orderId) (ChangeReport, error)
}
```

### Service-Level Types（非持久化载荷定义）

> 接口签名中引用的非 DB 模型结构体，字段+类型+约束如下。

```go
// Credential 执行通道凭证句柄（解密后的内存形态，用后 zeroing，禁入日志/响应）
type Credential struct {
    Kind       string // "cloud_ak" | "kubeconfig"，必填
    Cloud      string // aliyun|tencent|huawei|aws|azure；Kind=cloud_ak 时必填，kubeconfig 时空
    AccountKey string // 云账号标识；Kind=cloud_ak 时必填，kubeconfig 时空
    AccessKey  string // AK；Kind=cloud_ak 时必填
    Secret     []byte // SK 或 kubeconfig 明文；仅内存，永不落盘/序列化
    KeyVersion int    // 解密所用主密钥版本，>=1；审计追溯用
}

// DiscoverScope 单次引用发现的目标范围
type DiscoverScope struct {
    Clouds     []string // 云列表；空=全部已接入云
    Products   []string // cdn|dcdn|waf|alb|clb|nlb；空=该云全部已支持产品
    ClusterIDs []string // K8s 集群 ID 列表；仅 K8sAPIChannel 使用，云通道忽略
    SnapshotID string   // 归属扫描快照 ID，必填；发现的 CertReference.snapshotId 回写此值
}

// DeployTarget 单个部署/回滚动作的目标资源定位
type DeployTarget struct {
    Channel    string // "cloud_api" | "k8s_api"，必填
    Cloud      string // cloud_api 必填
    Product    string // cdn|dcdn|waf|alb|clb|nlb；cloud_api 必填
    AccountKey string // cloud_api 必填
    ClusterID  string // k8s_api 必填
    Namespace  string // k8s_api 必填（CRD 所在命名空间）
    Kind       string // k8s_api 必填（CRD kind，如 Certificate）；cloud_api 空
    ResourceID string // 云资源 ID 或 CRD 实例名，必填
}

// DeployResult Deploy 单项执行结果
type DeployResult struct {
    NewCloudCertID  string // 两段式第一段产物的云证书 ID；K8s 通道为空
    OldCloudCertID  string // 被替换的云侧证书 ID；回滚依据，执行前从引用快照读取
    OrphanCandidate bool   // true=旧云证书成为孤儿候选，验证达标后入清理队列（scheduler orphan-cleanup 任务消费，结果记入 ChangeReport.OrphanCleanup）
    RecheckPassed   bool   // K8s 通道：crd-recheck 复检结果（patch 后延迟 recheckDelayMinutes 复检一轮）；true→success，false=被 reconcile 回写→failed+告警（见"K8s 管理权判定与变更后复检"）
}

// RollbackResult Rollback 单项回滚结果
type RollbackResult struct {
    Success       bool          // 项级成败
    RestoredRef   CertReference // 回滚成功后的引用形态（含恢复的 cloudCertId）；失败时为零值
    OrphanCleaned []string      // 回滚中同步清理的孤儿云证书 ID 列表；无则空
    ErrCode       string        // 失败错误码：CLOUD_API_RATELIMITED|K8S_UNREACHABLE|ROLLBACK_TARGET_INVALID
    Reason        string        // 失败详情；不得含私钥/凭证片段
}

// CloudCertInfo GetCert 返回的云侧证书在库状态（回滚目标有效性校验依据）
type CloudCertInfo struct {
    Exists      bool      // 云证书库中该 cloudCertId 是否存在
    NotAfter    time.Time // 云侧证书有效期截止；Exists=false 时零值
    Fingerprint string    // 云侧证书 SHA256 指纹；复核回滚目标未被替换
}

// BatchConf 分批灰度配置（Confirm 入参）
type BatchConf struct {
    Enabled       bool    // 是否分批；false=单批全量（仅引用数 <= 阈值时允许）
    BatchSize     int     // 每批资源数，>0；Enabled=true 时必填；有效批大小 = min(BatchSize, floor(total/2))
    MaxBatchRatio float64 // 单批占全部引用比例上限，(0, 0.5]；硬约束 <=0.5（PRD 分批灰度 ≤50%）
}
// 分批一律人工续批（PRD：剩余批同样需人工确认后执行），不提供自动续批选项；
// 每批执行完成且批级验证达标后，订单转批间暂停态（batchInfo.paused=true），
// 由 ConfirmBatch 人工续批（门控见 ChangeService.ConfirmBatch）。

// ChangeList GenerateChangeList 生成的变更清单
type ChangeList struct {
    OrderID          string           // 预生成的变更单 ID（待确认态）
    OldFingerprint   string           // 旧证书指纹，SHA256 hex
    NewCertID        string           // 新证书 ID
    SnapshotID       string           // 清单绑定的扫描快照（新鲜度校验依据）
    ScanFreshnessHrs int              // 生成时扫描新鲜度（小时）；超阈值的快照直接阻断生成
    Items            []ChangeListItem // 按指纹聚合的引用项
    SANCheck         SanCheckResult   // SAN 预检结果
    Warnings         []string         // 盲区声明/不可自动变更项/覆盖率分母不可用提示
}

// ChangeListItem 清单单项
type ChangeListItem struct {
    ItemID         string       // 变更项 ID
    Target         DeployTarget // 目标资源定位；持久化完整写入 ChangeItem.resourceRef（按 action 分支必填字段，见 schema.sql cert_change_items），异步子任务仅凭持久化数据即可重构 DeployTarget，不回查台账/快照
    Action         string       // "upload_and_bind" | "patch_crd"
    AutoChangeable bool         // 可自动变更判定：K8s 三信号管理权探测（见"K8s 管理权判定与变更后复检"）或 discovery-only 云无部署器；false=不可自动变更
    Reason         string       // AutoChangeable=false 时的判定依据（命中信号类型+具体键：GitOps label / ownerReferences / 管理 annotation / ERR_DISCOVERY_ONLY）
}

// SanCheckResult SAN 预检结果
type SanCheckResult struct {
    Passed  bool     // 新证书 SAN ⊇ 全部目标域名
    Missing []string // 缺失域名列表；Passed=false 时非空
    NewSANs []string // 新增域名（提示性，不做拦截）
}

// ChangeReport GetReport 返回的变更报告
type ChangeReport struct {
    OrderID       string                // 变更单 ID
    Status        string                // 9 态状态机当前态（8 态 + cancelled 取消终态）
    Summary       ReportSummary         // 汇总计数
    Items         []ReportItem          // 与 ChangeItem 一一对应
    Verify        VerifySummary         // 验证窗口结果
    OrphanCleanup []OrphanCleanupResult // 孤儿证书补偿清理结果（逐项成功/失败，PRD Story 3/5 AC）
    UnmetDomains  []string              // 窗口关闭未达标域名清单（partial_completed 时非空；PRD 并发规则"未达标清单"）
    FinishedAt    time.Time             // 全部批次完成时间；未完成为零值
}

// OrphanCleanupResult 孤儿云证书清理单项结果
type OrphanCleanupResult struct {
    Cloud       string    // 清理动作所属云（aliyun|tencent）
    CloudCertID string    // 被清理的云侧证书 ID
    Action      string    // cleanup=执行清理 | skip_keep=暂留（保护期内/人工保留）
    Success     bool      // 清理成败；false 触发运维处置告警
    At          time.Time // 清理动作时间
}

// ReportSummary 报告汇总
type ReportSummary struct {
    Total      int // 总项数
    Success    int // 成功
    Failed     int // 失败（保留旧引用）
    Skipped    int // 跳过（不可自动变更/人工取消）
    RolledBack int // 已回滚
}

// ReportItem 报告单项
type ReportItem struct {
    ItemID    string       // 变更项 ID
    Target    DeployTarget // 目标
    Status    string       // pending|running|success|failed|rate_limited|rolled_back|skipped
    ErrCode   string       // 失败时的错误码
    LatencyMs int64        // 单项执行耗时
}

// VerifySummary 验证窗口汇总
type VerifySummary struct {
    WindowUntil  time.Time // 窗口截止（=verifyExpected.windowUntil）
    ExpectedNew  string    // 预期终态指纹（verifyExpected.newCertFingerprint 快照）
    ProbePass    int       // 窗口内探测达标域名数
    ProbeDiff    int       // 窗口内差异域名数（含 change_linked_diff）
    ProbeSkipped int       // 计 skipped 的验证项数（豁免 excludedDomains + 无 override 的通配符）
    Unmet        int       // 窗口关闭未达标域名数（与 ChangeReport.UnmetDomains 对应）
}
```

## Data Models

> DB-Schema: yes — 完整设计见独立文件。
> **ER Diagram**: design/er-diagram.md
> **SQL Schema**: design/schema.sql

### Field Quick Reference

| Model | Key Fields | Notes |
|-------|------------|-------|
| Certificate | fingerprint(唯一)、sans[]、issuer、notBefore/notAfter、hostingStatus、encryptedPrivateKey{ciphertext,keyVersion,algo}、protectUntil、expiryAlertLevel | 私钥信封加密 AES-256-GCM；expiryAlertLevel=到期告警去重状态（见到期分级告警） |
| CertReference | certFingerprint、cloud、product、clusterId、resourceId、referencedCloudCertId、snapshotId | 引用扫描发现 |
| ScanSnapshot | startedAt、finishedAt、coverageMeta[]{cloud,product,covered,total}、status | 新鲜度+资源覆盖率分母来源 |
| ChangeOrder | oldCertFingerprint、newCertId、status(9态)、batchInfo、snapshotId、verifyWindowUntil、protectUntil、creator、activeMutex、verifyExpected | 状态机见 PRD + cancelled 取消终态；activeMutex=在途互斥 token（见下）；verifyExpected=验证窗口预期终态快照（见下） |
| ChangeItem | orderId、batchNo、resourceRef、action、oldCloudCertId、newCloudCertId、status、error、heartbeatAt | 逐项执行；batchNo=Confirm 时固化的批次归属（见"分批执行门控"）；resourceRef 持久化完整 DeployTarget（按 action 分支必填，见 schema.sql）；heartbeatAt=执行心跳（executing-timeout 判据） |
| CloudCertMapping | certFingerprint、cloud、accountKey、cloudCertId、status(active/orphan) | 两段式/回滚/孤儿清理 |
| ProbeResult | domain、probeAt、onlineFingerprint、status(consistent/diff/change_linked_diff/unreachable/exempt/wildcard_skipped)、changeOrderId | TLS 探测；change_linked_diff=验证窗口内变更关联差异（见下）；wildcard_skipped=通配符 SAN 跳过拨测（见"探测目标域来源与通配符处置"） |
| Exemption | domain、reason、operator、createdAt | 探测豁免 |
| AlertConfig | webhookUrls[]、emailGroup[]、channelConfirmed、verifyWindowRoute、wildcardProbeOverrides、thresholds | 单文档；wildcardProbeOverrides=通配符 SAN→具体探测子域名（concreteSubdomainOverride） |
| K8sCredential | clusterName、kubeconfig(encrypted,keyVersion)、apiEndpoint | K8s 接入 |
| CertBatchSession | batchId、files[]{fileName,certId,result,errorReason}、status、progress{total,done,failed} | 批量导入会话，进度轮询端点数据源 |
| CrdRegistration | clusterId、apiGroup、kind、certFieldPath、enabled、operator | 自定义 CRD 扫描登记，K8sAPIChannel 扫描范围联动 |

### 引用状态语义（"未发现引用" ≠ "无引用"）

引用视图增加三态派生字段 `referenceStatus`（DTO 派生量，非存储字段）：

| 值 | 语义 | 派生条件 |
|----|------|----------|
| has_refs | 有引用 | 最新成功快照中该指纹的 CertReference 计数 > 0 |
| no_refs_scanned | 未发现引用（扫描无匹配） | 最新成功快照已覆盖该证书涉及的云/产品，且引用计数 = 0 |
| blind_spot | 盲区 | 无成功快照，或该证书涉及的云/产品未纳入本期扫描范围 |

"未发现引用"表示"已扫描但无匹配"——可能因云 API 权限不足、产品未覆盖而漏报，不等于确定性"无引用"。删除拦截：has_refs 与 blind_spot 均返回 CERT_HAS_REFS（blind_spot 附盲区原因）；仅 no_refs_scanned 允许删除（protectUntil 保护期另计）。变更清单对 blind_spot 项输出警告，不静默放行。

### 覆盖率分母：资产独立盘点数据源

ScanSnapshot.coverageMeta[].total 不来自引用扫描通道，来源为 `internal/asset` 资产同步的全量资源盘点（asset Instance 实例数据，独立于证书域维护）：

- **数据源**：`internal/asset` 资产实例盘点集合（Model 按 Provider 分类 + Instance 实例，由现有资产同步任务写入与刷新）
- **数据流契约**：扫描任务（ReferenceDiscoveryService）启动时，按 Provider × 资源模型（映射证书域 product：cdn/dcdn/waf/alb/clb/nlb）聚合 asset 在用资源计数，作为 coverageMeta[].total 随快照一次性固化；covered = 本轮 CertReference 去重资源数
- **失效处理**：asset 聚合不可用，或某云/产品计数为 0 但历史非 0 → 该项 total = -1，引用视图/看板输出"分母不可用"盲区声明，覆盖率不显示 0%
- **一致性**：covered 与 total 为异构时点数据，不强制 covered ≤ total；covered > total 时以 covered 为准并标记"asset 盘点滞后"警告

### 登记覆盖率 / 可更换托管覆盖率（双指标口径）

PRD Goals 双指标的计算与承载（与上文"资源覆盖率"（coverageMeta）分母口径不同，两者独立、互不替代）：

- **分母** = 最新成功快照 CertReference 指纹去重集合 ∪ 台账全部证书指纹（即 PRD 定义"引用扫描发现的在用证书去重集合 + 人工补充登记集合"，分母来源独立于导入通道）
- **登记覆盖率 registrationRate** = 台账证书数 / 分母；差集（扫描发现但未登记指纹）= 登记缺口，stats 响应以 missingRegistrations 计数
- **可更换托管覆盖率 replaceableRate** = hostingStatus=complete 证书数 / 分母；**仅指纹登记占比 fingerprintOnlyRate** = 台账中 fingerprint_only 数 / 台账总数，单独可见
- **承载端点**：`GET /api/v1/certs/stats`（台账统计）与 `GET /api/v1/certs/dashboard`（summary 同名字段），查询时实时聚合、无存储快照

### 在途互斥的并发强制（activeMutex token）

"同一 oldCertFingerprint 同时仅一张活跃单（待确认/执行中/验证中）"由 DB 层索引强制，而非仅应用层检查：

- ChangeOrder 增 `activeMutex` 字段：进入活跃态时写入 = oldCertFingerprint；进入终态（已完成/部分完成/已回滚/回滚失败/取消）时 `$unset` 清除
- cert_change_orders 建 `activeMutex` 部分唯一索引（partialFilterExpression: activeMutex 存在），同 token 第二张活跃单 insert 直接 duplicate key
- 创建路径条件写入：插入时携带 activeMutex=oldCertFingerprint，冲突捕获后映射 CHANGE_IN_FLIGHT；应用层预检查仅作快速失败，正确性由索引保证，check-then-insert 竞态窗口被关闭
- 状态机迁移与 token 清除在同一原子 update 中完成，防止终态单残留 token 阻塞新单
- 可选优化：Redis 锁（SETNX oldCertFingerprint + TTL）作第一道快速互斥，非正确性依赖
- **活性保障（liveness）**：暂停分批单保留互斥，但不会无限期持锁，两条释放路径——①人工取消：`Cancel` 在 draft/pending_confirm/批间暂停态可执行，转 cancelled 终态、未执行项标 skipped、同原子 update 清除 token；②超时自动中止：scheduler 定时任务（每小时）扫描 `batchInfo.paused=true 且 pausedAt + thresholds.pauseTimeoutHours < now` 的单据，转 cancelled（未执行项标 skipped）并经 alert 通道发送"变更单超时取消"处置通知（运维处置类通知，不计入 PRD 四类业务告警口径）
- **executing 态活性（心跳+超时）**：执行子任务运行期间以固定 30 秒间隔更新所属 ChangeItem.heartbeatAt；scheduler executing-timeout 任务（每 5 分钟）扫描 `status=running 且 heartbeatAt + thresholds.itemHeartbeatTimeoutMinutes（默认 30，范围 5~180）< now` 的项 → 标 failed（error=EXEC_TIMEOUT）+ 告警，单据状态按剩余项重算——worker 崩溃/云 API 挂起不会使订单永久停留 executing、activeMutex 永不清除（与 Cancel 的 executing 中止路径互补）

### 验证窗口告警路由（change_linked_diff）

ProbeResult.status 增第 5 值 `change_linked_diff`（变更关联差异），AlertConfig 增验证窗口路由：

- **ChangeOrder.verifyExpected**（批执行完成时固化快照；分批单每批按该批目标域名刷新，窗口判定依据，不随台账变化）：`{newCertFingerprint, domains[], excludedDomains[], windowUntil}`；domains 构建时剔除当前豁免清单命中的域名并记入 excludedDomains——豁免域名不参与窗口达标判定（其验证项在报告中计 skipped），避免"含豁免域名的窗口永不达标"死锁
- **判定流程**：ProbeService 探测得 diff → 查活跃"验证中"变更单（verifyWindowUntil > now）的 verifyExpected → 若 domain ∈ domains 且 onlineFingerprint == newCertFingerprint → status=change_linked_diff 并记录 changeOrderId（属预期切换，非事故差异）
- **告警路由**：AlertConfig 增 `verifyWindowRoute{enabled, webhookUrls[], emailGroup[]}`；窗口内 change_linked_diff 走"变更关联"通道（附 orderId、预期指纹、达标计数），不走常规差异通道；enabled=false 时复用常规通道但附变更关联标记。窗口关闭后该域名恢复常规 diff 判定与告警
- **窗口内探测提频**：订单进入 verifying（含批级验证）后，ProbeService 对 verifyExpected.domains 的探测周期由天级临时提至 thresholds.verifyProbeIntervalMinutes（默认 10 分钟）；窗口关闭/达标后回落天级——天级节奏下 2~24h 窗口采样 0~2 次，"连续 verifyConfirmProbes 次一致"不可判定，提频是达标判定的前提
- **达标确认**：verifyExpected.domains 全部探测一致且连续达标次数 ≥ thresholds.verifyConfirmProbes（默认 2）→ 窗口提前达标关闭；超时未达标 → 恢复常规 diff 告警，变更单转部分完成/待处理
- **窗口到期终局判定**：scheduler 定时任务（周期 = verifyProbeIntervalMinutes）扫描 `verifyWindowUntil <= now 且 status=verifying` 的单据做终局判定：全部达标→completed；存在未达标→partial_completed + 未达标清单写入 ChangeReport.UnmetDomains（计数记 Verify.Unmet）+ 恢复常规告警。窗口关闭不依赖被动探测触发

### 探测目标域来源与通配符处置

- **目标域清单**：ProbeService 探测域名 = 台账全部 Certificate.sans[] 展开去重（expectedDomain 不参与——仅提示性比对）；豁免清单命中域名仍探测但标 exempt（不告警）
- **通配符 SAN**：`*.example.com` 无法直接 DNS 解析与 SNI 拨测——默认跳过，写 ProbeResult{domain=通配符 SAN, status=wildcard_skipped}（计数可见、不告警、不计差异）；可经 AlertConfig.wildcardProbeOverrides 为指定通配符配置 concreteSubdomainOverride（通配符→具体子域名，人工指定），此时对该子域名正常拨测，结果记于该子域名、按常规状态判定
- **验证窗口交互**：verifyExpected.domains 含通配符且无 override 时，该验证项计 skipped（同豁免语义，计入 Verify.ProbeSkipped），不阻塞窗口达标判定
- **看板口径**：wildcardSkippedCount 在 dashboard summary 单独计数（"未豁免端点 TLS 探测覆盖"目标的显式缺口）

### 分批执行门控与批次分配（人工续批）

- **批次分配**（Confirm 时一次性固化，执行期清单快照不变）：items 按 (cloud, product, resourceId) 字典序稳定排序；首批 = 前 `min(BatchSize, floor(total/2))` 项（硬约束单批 ≤ floor(total/2)，对应 PRD ≤50%）；余下项按 BatchSize 均分为后续批（末批可不足额）；每项写入 `ChangeItem.batchNo`，batchInfo 记 {totalBatches, currentBatch, batchSize, paused, pausedAt}；执行仅取 batchNo=currentBatch 的项
- **门控**（PRD：首批执行且验证通过后方可继续执行剩余批，剩余批同样需人工确认）：每批执行完成 → 订单转 verifying（verifyWindowUntil 刷新为该批验证截止，verifyExpected 按该批域名刷新）→ 提频探测连续 verifyConfirmProbes 次一致 = 批级验证达标 → 订单回 executing 且 batchInfo.paused=true/pausedAt=now，等待人工续批；`ConfirmBatch` 校验"上一批全部 success 且批级验证达标"，不满足返回 409 BATCH_NOT_CONFIRMABLE。批级验证到期未达标 → 转人工决策（回滚成功项 / Cancel 取消），不自动续批
- **状态机批间循环**：分批单在 executing ↔ verifying 间循环（批级验证复用 verifying 态），activeMutex 全程持有；终批验证达标后进入 completed/partial_completed，token 清除（活性保障见上节）

### 自定义 CRD 登记管理

首期固定枚举（ALBConfig/Ingress/Gateway/HTTPRoute）之外的自定义网关 CRD，经登记纳入扫描范围（PRD"经登记后纳入扫描范围"）：

- **模型**：cert_crd_registrations（clusterId、apiGroup、kind、certFieldPath、enabled、operator、createdAt；clusterId+apiGroup+kind 唯一）
- **登记判定**：仅接受 spec 中含云托管证书 ID/名称引用字段的网关类资源；certFieldPath 声明引用字段路径（如 `spec.certificates[].certificateId`），非法路径在扫描时报错并计入门户告警
- **端点**：POST/GET/DELETE `/api/v1/certs/settings/crds`（运维主管/审计角色，见 api-handbook）
- **扫描联动**：K8sAPIChannel.Discover 按固定枚举 + enabled=true 登记项遍历；enabled=false 或未登记的 CRD 属盲区，引用视图显式声明（referenceStatus=blind_spot 口径）

### K8s 管理权判定与变更后复检

**判定规则集**（三信号任一命中即判"不可自动变更"，AutoChangeable=false，Reason 记录信号类型+具体键）：

1. **GitOps 管理 label**：资源 metadata.labels 命中管理标记——默认键 `argocd.argoproj.io/instance`、`fluxcd.io/sync`（键/前缀清单经应用 config 配置，默认含 `argocd.argoproj.io/*` 与 `fluxcd.io/*`）
2. **ownerReferences 非空**：metadata.ownerReferences 数组非空（资源由控制器属主管理，patch 会被属主控制器重建/覆盖）
3. **管理类 annotation**：命中证书自动管理标记——默认键 `cert-manager.io/issuer`、`cert-manager.io/cluster-issuer`（TLS 由 cert-manager 签发管理；键清单经应用 config 配置）

**变更后复检（crd-recheck scheduler 任务）**：patch_crd 项 patch 完成后延迟 `thresholds.recheckDelayMinutes`（默认 5 分钟，范围 1~60）执行**一轮**复检——读取 CRD 证书引用字段：仍为新证书 ID → RecheckPassed=true、项标 success；被 reconcile 回写为旧值/其他值 → RecheckPassed=false、项标 failed + 告警（TLS 差异通道语义，附 orderId/itemId）。复检次数固定 1，失败不做二次自动复检（转人工决策：登记接管/调整 GitOps 同步后再发起）。

### 到期分级告警（去重状态机）

- **分级计算**：InspectionService 天级巡检按 notAfter 计算 daysLeft，命中 thresholds.expiryLevels（默认 [30,14,7]，降序匹配取最紧急级）或已过期（expired 级）
- **去重状态**：Certificate.expiryAlertLevel（none|L30|L14|L7|expired）持久化已触发级别；仅当新级别较已触发级别更紧急（升级）才发送告警并更新状态，同级不重复触发——天级巡检下同一证书 30 天级仅告警一次；更换新证书后 daysLeft 回升，重置为 none 并重新计级
- **告警四类路由**（PRD Monitoring 四类，统一经 internal/alert webhook+email 通道，按 category 字段区分）：①到期分级（本节，去重状态机）；②TLS 差异（常规 diff，按探测事件触发）；③变更关联（change_linked_diff，走 verifyWindowRoute 通道）；④回滚失败（Rollback 自身失败立即告警转人工）。到期分级是四类中唯一需要跨巡检去重的类别

## Error Handling

### Error Types & Codes

| Error Code | Name | Description | HTTP Status |
|------------|------|-------------|-------------|
| CERT_KEY_MISMATCH | KeyMismatchError | 证书与私钥不匹配 | 400 |
| CERT_CHAIN_INCOMPLETE | ChainIncompleteError | 证书链缺失 | 400 |
| CERT_PARSE_FAIL | ParseError | SAN 结构无法解析/已过期 | 400 |
| CERT_DUPLICATE_FINGERPRINT | DuplicateFingerprintError | 重复指纹导入 | 409 |
| SCAN_STALE | ScanStaleError | 扫描超新鲜度阈值，清单生成阻断 | 409 |
| SAN_INSUFFICIENT | SanInsufficientError | 新证书 SAN 不⊇目标域名 | 409 |
| NEW_CERT_FINGERPRINT_ONLY | NewCertFingerprintOnlyError | 新证书仅指纹登记（无私钥），无法上传云证书库执行更换 | 409 |
| CHANGE_IN_FLIGHT | ChangeInFlightError | 同一旧证书存在在途变更单 | 409 |
| BATCH_NOT_CONFIRMABLE | BatchNotConfirmableError | 续批门控未满足（上一批存在失败项或批级验证未达标） | 409 |
| CHANGE_NOT_CANCELLABLE | ChangeNotCancellableError | 当前状态不可取消（draft/pending_confirm/批间暂停整单取消；executing 仅未开始项可中止、执行中项等待完成） | 409 |
| ROLLBACK_TARGET_INVALID | RollbackTargetInvalidError | 回滚目标无效（云侧已删除/已过期/被替换），转人工 | 409 |
| CERT_HAS_REFS | HasRefsError | 存在活跃引用或处于保护期，禁止删除 | 409 |
| SCAN_IN_PROGRESS | ScanInProgressError | 扫描进行中（防重触发） | 409 |
| CLOUD_API_RATELIMITED | CloudRateLimitedError | 云 API 限流，退避重试中 | 503 |
| K8S_UNREACHABLE | K8sUnreachableError | 集群不可达 | 503 |
| FORBIDDEN | ForbiddenError | EIAM 权限不足 | 403 |

### 同步错误 vs 异步子任务状态

错误码存在两种出现语境，映射关系如下：

- **同步错误（HTTP 状态码）**：仅发生在同步请求/响应路径——导入校验、清单生成阻断（SCAN_STALE/SAN_INSUFFICIENT/CHANGE_IN_FLIGHT/NEW_CERT_FINGERPRINT_ONLY）、删除拦截（CERT_HAS_REFS）、防重（SCAN_IN_PROGRESS）、权限（FORBIDDEN）。web 层映射 `HTTP status + code` 信封返回
- **异步子任务状态（status 字段）**：批量执行/扫描/探测经 internal/task 派发后不再产生 HTTP 错误响应，失败落为文档状态：
  - CLOUD_API_RATELIMITED → ChangeItem.status=`rate_limited`（退避重试中）；仅同步路径（如立即扫描触发）直接遇到限流才以 503 返回
  - K8S_UNREACHABLE → ChangeItem.status=`failed` + error=K8S_UNREACHABLE；或 ScanSnapshot.status=`failed` + reason
  - 项级失败不中断其他项；进度轮询/报告接口返回的是上述 status 字段，而非错误抛出

### Propagation Strategy

- web 层捕获 domain error → 映射 HTTP 状态码 + 错误码返回；不泄露内部细节
- service 层：业务校验失败（scan_stale/san/互斥）返回明确语义错误供前端分支处理
- deployer 层：云 API 限流/K8s 不可达 → 子任务标记失败状态 + 原因，不中断其他项
- 私钥相关错误不得在 message 中携带私钥片段

## Cross-Layer Data Map

| Field Name | Storage Layer | Backend Model | API/DTO | Frontend Type | Validation Rule |
|------------|---------------|---------------|---------|---------------|-----------------|
| fingerprint | string,索引唯一 | Certificate.Fingerprint | json:"fingerprint" | string (mono) | SHA256 hex |
| encryptedPrivateKey | {ciphertext,keyVersion,algo} | Certificate.EncryptedPrivateKey | 不返回 | 仅显示"已加密托管" | 永不外泄 |
| hostingStatus | string | Certificate.HostingStatus | json:"hostingStatus" | enum 完整/仅指纹 | complete/fingerprint_only |
| scanFreshness | int hours | 派生量（非存储字段）：now − ScanSnapshot.StartedAt | json:"lastScanAt" | 相对时间 | 超 thresholds.scanFreshnessHours 阻断清单 |
| changeStatus | string | ChangeOrder.Status | json:"status" | 9 态徽章 | 状态机约束（含 cancelled） |
| probeStatus | string | ProbeResult.Status | json:"probeStatus" | enum 6 值 | consistent/diff/change_linked_diff/unreachable/exempt/wildcard_skipped |
| coverageMeta | []{cloud,covered,total} | ScanSnapshot.CoverageMeta | json:"coverage" | 卡片 | 分母=internal/asset 资产盘点（见数据源设计） |

## Integration Specs

No existing-page integrations — not applicable.（全部为新页面，e-cam-web 新增 `views/modules/cert/` 路由模块，沿用现有导航与权限控制机制。）

## Testing Strategy

### Per-Layer Test Plan

| Layer | Test Type | Tool | What to Test | Coverage Target |
|-------|-----------|------|--------------|-----------------|
| domain | 单元 | go test | 证书解析/校验/状态机转换/指纹聚合键 | 85% |
| deployer | 集成（mock SDK） | go test + mock | 两段式上传/绑定/孤儿清理/限流退避/管理权探测 | 80% |
| service | 集成 | go test + mongox test | 变更单全生命周期/回滚语义/新鲜度阻断/SAN 预检 | 85% |
| web | API 契约 | httptest | 端点契约/权限/错误码/私钥不外泄 | 80% |
| probe | 集成 | go test + 本地 TLS server | SNI/多证书/不可达/豁免/change_linked_diff 判定 | 80% |
| e2e | 端到端（进程内） | go test + httptest + mongox test 实例 + mock 云 API server + envtest/假 APIServer 扮演 K8s | 通过完整 HTTP API 面驱动跨服务流（见 Key E2E Flows） | 80% |

### Key Test Scenarios

- 完整性校验四项拦截（不匹配/链缺失/解析失败/过期）
- 变更清单：扫描超期阻断、SAN 不⊇拦截、在途互斥阻断（含并发双请求仅一张活跃单，activeMutex 索引层拦截）
- 批量执行：部分失败→失败项保留旧引用→成功项回滚→回滚目标无效转人工
- 两段式第二段失败→孤儿证书补偿清理
- K8s CRD 管理权探测→GitOps 管理标记不可自动变更→变更后复检 reconcile 回写
- TLS 探测：线上≠台账差异告警、不可达标记、豁免不告警
- 引用三态：no_refs_scanned 与 blind_spot 在引用视图/删除拦截中的分流
- 到期分级告警：30→14→7→expired 升级逐级触发、同级天级巡检去重不重发、换证后重置为 none
- 验证窗口时间维度：窗口内提频探测达标提前关闭；窗口到期 scheduler 终局判定转 partial_completed
- 分批门控：上一批未达标续批被拒（BATCH_NOT_CONFIRMABLE）；批间暂停超 pauseTimeoutHours 由 scheduler 自动取消并通知
- 豁免域名 ∩ 验证窗口：豁免域名进 excludedDomains 计 skipped，窗口仍可正常达标
- 私钥永不外泄：API/日志/报告/前端均无明文（渗透式自查）

### Key E2E Flows

- 换证→部分失败→成功项回滚→孤儿清理→验证窗口达标关闭：httptest 驱动导入→扫描→清单→确认→执行→（注入云 API 故障）→回滚→孤儿清理→探测达标→状态机收敛，全程断言各接口响应与报告一致
- 分批灰度人工续批门控：首批执行→批级提频验证达标→订单转批间暂停（activeMutex 保留）→续批门控校验（上一批未达标 409 BATCH_NOT_CONFIRMABLE）→人工续批执行剩余批→尾批完成→验证窗口达标→单据终态；批间暂停超 pauseTimeoutHours 由 scheduler 自动取消并通知
- 验证窗口告警路由：窗口内探测差异→change_linked_diff→变更关联通道收到告警（附 orderId）→窗口超时未达标→恢复常规 diff 通道
- 主密钥轮换迁移：双版本密文并存→双读解密→批量再加密→迁移完成后旧版本密文为 0

### Overall Coverage Target

80%

## Security Considerations

### Threat Model

- **私钥集中托管**：平台成为高价值目标，DB 泄露 + 主密钥泄露 = 全网私钥失守
- **误操作/恶意批量更换**：生产 CDN/WAF/LB 被错误证书替换
- **云侧凭证滥用**：上传到云证书库的能力可被用于注入恶意证书
- **审计绕过**：变更无记录导致事后无法追溯
- **K8s 凭证泄露**：kubeconfig 可直连集群 API Server

### Mitigations

- 私钥：AES-256-GCM 信封加密，主密钥从环境变量注入独立于业务数据，`keyVersion` 支持轮换；仅在解析/校验/上传云证书库的内存中解密，用后 `zeroing`，不落盘/不进日志与报告
- 主密钥轮换与存量密文迁移：①发布 keyVersion=N+1，配置双活加载（N 与 N+1 同时驻留内存）；②在线双读：解密按文档 keyVersion 路由到对应主密钥，写路径一律以最新版本加密；③离线批量再加密迁移任务（internal/task 异步）：分批扫描 encryptedPrivateKey/kubeconfig 中 keyVersion<N+1 的文档，解密→新密钥再加密→按 (id, keyVersion) 条件更新，若期间字段已变更则跳过重扫，幂等可重入；④失败回滚：迁移期不删除任何旧版密文、不下线旧主密钥，任一批次���败可安全中止（双读保证旧密文仍可解密），回滚=停用新版写路径、恢复旧版加密；⑤迁移率 100% 且抽样解密校验通过后，经人工确认下线旧主密钥并离线归档
- 备份与恢复（PRD：平台数据丢失不得导致 EV/OV 私钥不可恢复；备份周期天级、恢复小时级）：①密文库随 MongoDB 天级备份——复用平台现有 mongodump/快照机制，cert_certificates/cert_k8s_credentials 以密文形态随库备份，无明文暴露面；②主密钥文件独立异地备份（保险柜/密码管理器），与数据库备份物理隔离——仅有密文备份而无主密钥不可解密，两者泄露面不叠加；③恢复演练小时级路径：MongoDB 备份恢复 → 注入对应 keyVersion 主密钥 → 按 keyVersion 路由解密并抽样比对指纹验证，演练目标 ≤1 小时完成
- 接口/前端永不返回明文私钥（渗透式自查口径：grep 全代码库无明文私钥返回点）
- 变更：人工确认 + 完整性前置校验 + 指纹精确匹配 + 扫描新鲜度 + 分批灰度 ≤50% + 回滚兜底 + EIAM 权限收敛 + 全量审计
- 云凭证：复用现有云账号 AK/SK 加密存储，deployer 操作限缩到证书相关 API
- K8s 凭证：kubeconfig 加密存储（同私钥加密体系），最小 RBAC（仅目标 CRD 读写）
- EIAM 三角色：运维工程师读写、运维主管/审计审计+配置、只读查看者看板只读；操作全量审计仅追加不可修改

## PRD Coverage Map

| PRD Requirement / AC | Design Component | Interface / Model |
|----------------------|------------------|-------------------|
| 证书托管台账+完整性校验 | CertService + IntegrityService | Certificate model, CERT_* errors |
| 引用关系发现 | ReferenceDiscoveryService + ExecutionChannel.Discover（五云：aliyun/tencent 部署器 + huawei/aws/azure discovery-only） | CertReference, ScanSnapshot |
| 到期监控告警 | ProbeService + alert/channel | ProbeResult, AlertConfig |
| 到期分级 30/14/7 告警去重 | InspectionService + expiryAlertLevel | thresholds.expiryLevels |
| 登记覆盖率/可更换托管覆盖率双指标 | stats/dashboard 实时聚合 | GET /api/v1/certs/stats |
| 自定义 CRD 登记纳入扫描 | CrdRegistration + K8sAPIChannel 联动 | POST/GET/DELETE /api/v1/certs/settings/crds |
| TLS 主动探测 | ProbeService (crypto/tls Dial) | ProbeResult |
| 一键批量更换 | ChangeService | ChangeOrder, ChangeItem |
| 回滚语义 | ChangeService.Rollback | ROLLBACK_TARGET_INVALID |
| 变更后验证窗口 | ChangeService verify stage | ChangeOrder.VerifyWindowUntil |
| 两段式+孤儿清理 | CloudDeployer + scheduler orphan-cleanup | CloudCertMapping, ChangeReport.OrphanCleanup |
| K8s CRD 更新+管理权探测 | K8sAPIChannel | K8sCredential, change item status |
| 执行通道抽象 | ExecutionChannel | 接口预留 bastion/agent |
| 权限审计 | EIAM + internal/audit | audit logs |
| 变更审计按单号查询比对 | ChangeService + internal/audit | GET /api/v1/certs/changes/:id/audit |
| 前端页面 | e-cam-web cert 模块 | ui-design.md 已定型 |

## Open Questions

- [ ] 阿里云/腾讯云各产品证书 API 能力差异需 PoC 验证（列为首批任务）→ 桌面验证已完成（SDK 面+官方文档+mock 全绿，发现 2 项高优偏差：腾讯云孤儿清理异步删除语义、阿里云 CAS 证书名用户级唯一），**真实账号全链路验证待执行**（无云凭证，见 [poc-notes.md](poc-notes.md) §8 检查点；结论影响 5.4/5.5，修正清单见 poc-notes.md §6）
- [ ] client-go 版本锚定与 K8s 1.24+ 兼容矩阵需 PoC 验证（首批任务，验证后锁定版本）
- [ ] K8s 集群网络可达性（平台→APIServer）需逐集群确认
- [ ] 告警渠道（webhook/邮件 SMTP）凭据来源待配置

## Appendix

### Alternatives Considered

| Approach | Pros | Cons | Why Not Chosen |
|----------|------|------|----------------|
| 云 KMS 托管主密钥 | 安全等级高 | 依赖云 KMS 可达性+延迟+成本 | 选本地信封加密，无外部依赖、部署简单；运维妥善保管+备份主密钥 |
| 自研 Nginx Agent | 能力强 | agent 分发/升级/安全维护成本高 | 选 TLS 探测零侵入覆盖监控，更换走可插拔通道（堡垒机→优维 Agent） |
| 全自动无人工换证 | 效率最高 | 证书文件错误全网播撒 | 选一键批量+人工确认+回滚 |
