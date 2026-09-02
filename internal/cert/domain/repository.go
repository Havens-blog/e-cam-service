package domain

import (
	"context"
	"errors"
	"time"
)

// 仓储层哨兵错误：供上层（service/web）映射 HTTP 状态码与错误码。
// 读取类方法未命中时按 e-cam 惯例返回 mongo.ErrNoDocuments（不额外包装）。
var (
	// ErrDuplicateFingerprint 证书指纹已存在（uk_fingerprint 冲突）。
	// 上层映射 409 CERT_DUPLICATE_FINGERPRINT（导入去重，任务 2.2）。
	ErrDuplicateFingerprint = errors.New("cert: duplicate certificate fingerprint")

	// ErrChangeInFlight 同一旧证书存在在途变更单（uk_active_mutex 冲突）。
	// 上层映射 409 CHANGE_IN_FLIGHT（清单生成互斥，任务 5.2）。
	ErrChangeInFlight = errors.New("cert: change order already in flight for fingerprint")

	// ErrDuplicateClusterName K8s 集群名已存在（uk_cluster_name 冲突）。
	ErrDuplicateClusterName = errors.New("cert: duplicate k8s cluster name")

	// ErrDuplicateCrdRegistration CRD 登记已存在（uk_cluster_group_kind 冲突）。
	ErrDuplicateCrdRegistration = errors.New("cert: duplicate crd registration")

	// ErrBuiltinCrdRegistration 内置默认登记项（ALBConfig/Ingress/Gateway/HTTPRoute
	// 固定枚举）不可删除（任务 3.4）：内置登记随集群接入初始化且幂等，删除将破坏
	// 固定扫描范围；停用可经 SetEnabled（enabled=false 回归盲区并显式声明）。
	ErrBuiltinCrdRegistration = errors.New("cert: builtin crd registration cannot be deleted")

	// ErrInvalidID 非法的文档 ID（非 ObjectID hex）。
	ErrInvalidID = errors.New("cert: invalid document id")
)

// CertificateRepository 证书台账仓储（cert_certificates）。
type CertificateRepository interface {
	// Create 写入证书；fingerprint 冲突返回 ErrDuplicateFingerprint。
	// DEFAULT 填充：createdAt=now、expiryAlertLevel=none。
	Create(ctx context.Context, cert *Certificate) error
	GetByFingerprint(ctx context.Context, fingerprint string) (Certificate, error)
	// GetByID 按文档 ID 查询（详情/补传私钥定位）；非法 ID 返回 ErrInvalidID，
	// 未命中返回 mongo.ErrNoDocuments。
	GetByID(ctx context.Context, id string) (Certificate, error)
	List(ctx context.Context) ([]Certificate, error)
	// ListPage 服务端分页+筛选（任务 2.3 台账列表）：notAfter 升序（最快到期优先，
	// _id 升序稳定排序）；返回当页数据与筛选命中总数。
	// skip<0 视为 0；limit<=0 视为不限（与 Mongo Find 语义一致）。
	ListPage(ctx context.Context, filter CertListFilter, skip, limit int) ([]Certificate, int64, error)
	// AttachPrivateKey 补传私钥升级：同一原子 update 写入 encryptedPrivateKey 并将
	// hostingStatus 置 complete（幂等：已 complete 的证书重传匹配私钥时覆盖密文）。
	AttachPrivateKey(ctx context.Context, id string, secret *EncryptedSecret) error
	// UpdateCertPEM 更新证书 PEM 材料（重复指纹幂等导入补链：早期导入仅叶子
	// 证书，重导完整证书链时以新材料覆盖；按文档 ID 定位）。
	UpdateCertPEM(ctx context.Context, id string, certPEM string) error
	// UpdateExpiryAlertLevel 更新到期分级去重状态（仅升级触发时调用，任务 4.2）。
	UpdateExpiryAlertLevel(ctx context.Context, fingerprint string, level ExpiryAlertLevel) error
	// SetProtectUntil 设置回滚保护期截止（任务 5.1：变更单进入 completed/
	// partial_completed 时按 rollbackProtectDays 固化旧证书保护期，2.3 删除
	// 拦截依据）。仅当不存在或新截止更晚时写入（保护期只延长不缩短）；
	// 证书不存在时无操作。
	SetProtectUntil(ctx context.Context, fingerprint string, until time.Time) error
	DeleteByFingerprint(ctx context.Context, fingerprint string) error
}

// CertListFilter 台账列表筛选条件（任务 2.3，仓储层查询形状）。
// Search 为原始子串（域名/SAN/指纹片段，不区分大小写）；正则元字符转义由仓储实现
// 内部处理（对齐 internal/cam/tag 的 $regex+QuoteMeta 用法）。
type CertListFilter struct {
	HostingStatus HostingStatus // 空=不筛
	NotAfterFrom  *time.Time    // notAfter 下界（不含）；nil=不限
	NotAfterTo    *time.Time    // notAfter 上界（含）；nil=不限
	Search        string        // commonName/sans/fingerprint 子串匹配
}

// CertReferenceRepository 引用扫描发现仓储（cert_references）。
type CertReferenceRepository interface {
	// CreateMulti 批量写入本轮发现引用；DEFAULT 填充：scannedAt=now。
	CreateMulti(ctx context.Context, refs []CertReference) (int, error)
	ListByFingerprint(ctx context.Context, fingerprint string) ([]CertReference, error)
	// ListBySnapshotID 按快照查询全部引用（任务 2.3：refCount 派生与 stats
	// 分母聚合的数据源；idx_snapshot）。
	ListBySnapshotID(ctx context.Context, snapshotID string) ([]CertReference, error)
	// BackfillFingerprint 占位指纹引用回填（cert-cloud-discovery-import 任务 4）：
	// 将 (cloud,accountKey,referencedCloudCertId) 定位且当前指纹仍为 fromFingerprint
	//（扫描侧占位公式派生值）的引用批量更新为 toFingerprint（导入时点 GetCert 解析
	// 的真实指纹）。filter 含 fromFingerprint 构成 CAS 语义——真实指纹引用永不被
	// 覆盖；fromFingerprint==toFingerprint 时无操作。返回回填条数。
	// 与重扫描并发写为良性竞争：占位指纹是确定性可重算值，同值幂等（任务 4
	// Implementation Notes）。
	BackfillFingerprint(ctx context.Context, cloud, accountKey, cloudCertID, fromFingerprint, toFingerprint string) (int64, error)
	// DeleteBySnapshotID 按快照清理（idx_snapshot）。
	DeleteBySnapshotID(ctx context.Context, snapshotID string) (int64, error)
}

// ScanSnapshotRepository 扫描快照仓储（cert_scan_snapshots）。
type ScanSnapshotRepository interface {
	// Create 写入快照并返回其 ID（hex）；DEFAULT 填充：startedAt=now、status=running。
	Create(ctx context.Context, snap *ScanSnapshot) (string, error)
	GetByID(ctx context.Context, id string) (ScanSnapshot, error)
	// LatestDone 最新成功快照（status=done，startedAt 降序取首条；idx_started_at_desc）；
	// 无成功快照返回 mongo.ErrNoDocuments。任务 2.3 引用三态派生与 stats 分母数据源。
	LatestDone(ctx context.Context) (ScanSnapshot, error)
	// LatestRunning 当前运行中快照（status=running，startedAt 降序取首条）；
	// 无运行中快照返回 mongo.ErrNoDocuments。任务 3.5 扫描防重（409 SCAN_IN_PROGRESS）。
	LatestRunning(ctx context.Context) (ScanSnapshot, error)
	// Latest 最新快照（不限状态，startedAt 降序取首条；idx_started_at_desc）；
	// 无任何快照返回 mongo.ErrNoDocuments。cert-cloud-discovery-import 任务 3
	// snapshot-status 端点数据源（running/done/failed 均需可见——failed 快照
	// 携带 partialFailures 供引导失败呈现，LatestDone 无法覆盖该面）。
	Latest(ctx context.Context) (ScanSnapshot, error)
	// ListRunningBefore 运行中且 startedAt 早于 before 的快照
	// （任务 3.5 scan-timeout 恢复函数扫描集）。
	ListRunningBefore(ctx context.Context, before time.Time) ([]ScanSnapshot, error)
	// MarkFinished 结束快照：同原子 update 写 status/failReason/finishedAt。
	MarkFinished(ctx context.Context, id string, status ScanStatus, failReason string) error
	// FinishScan 扫描收敛（任务 3.5）：status/failReason/finishedAt 与最终
	// coverageMeta（covered 固化+lagging 标记）、partialFailures 同一原子 update。
	FinishScan(ctx context.Context, id string, status ScanStatus, failReason string, meta []CoverageMeta, partials []ScanChannelFailure) error
}

// ChangeOrderRepository 变更单仓储（cert_change_orders）。
//
// activeMutex 原子迁移原语（在途互斥并发强制，状态机逻辑在任务 5.1）：
//   - 进入活跃态：TransitionActive（$set status + $set activeMutex=token 同一原子 update）
//   - 进入终态：  TransitionTerminal（$set status + $unset activeMutex 同一原子 update，
//     防止终态单残留 token 阻塞新单）
type ChangeOrderRepository interface {
	// Create 写入变更单并返回其 ID（hex）；DEFAULT 填充：createdAt=now。
	// 创建路径条件写入：插入时携带 activeMutex，uk_active_mutex 冲突返回 ErrChangeInFlight。
	Create(ctx context.Context, order *ChangeOrder) (string, error)
	GetByID(ctx context.Context, id string) (ChangeOrder, error)
	// GetByMutexToken 按互斥 token 查活跃单（应用层预检查仅作快速失败，正确性由索引保证）。
	GetByMutexToken(ctx context.Context, token string) (ChangeOrder, error)
	// ListVerifyingActive 查询验证中的活跃变更单（status=verifying 且
	// verifyWindowUntil > after），createdAt 升序稳定返回。
	// 任务 4.1 change_linked_diff 判定数据源：探测得 diff 后匹配 verifyExpected
	//（domain ∈ domains 且 onlineFingerprint == newCertFingerprint）。
	ListVerifyingActive(ctx context.Context, after time.Time) ([]ChangeOrder, error)
	// TransitionActive 进入活跃态：状态迁移与 token 写入同一原子 update。
	// token 与其他活跃单冲突（uk_active_mutex）返回 ErrChangeInFlight
	//（update 路径同样受索引强制，check-then-update 竞态窗口被关闭）。
	TransitionActive(ctx context.Context, id string, to ChangeStatus, mutexToken string) error
	// TransitionTerminal 进入终态：状态迁移与 token 清除（$unset）同一原子 update。
	TransitionTerminal(ctx context.Context, id string, to ChangeStatus) error
	// TransitionTerminalWithProtect 进入完成类终态（completed/partial_completed，
	// 任务 5.1）：状态迁移、protectUntil 固化与 token 清除（$unset）同一原子 update。
	TransitionTerminalWithProtect(ctx context.Context, id string, to ChangeStatus, protectUntil time.Time) error
	// ListPausedBefore 批间暂停且 pausedAt 早于 before 的变更单（任务 5.1
	// CancelByTimeout 扫描集：status=executing 且 batchInfo.paused=true 且
	// pausedAt + pauseTimeoutHours < now 的超时暂停单；executing 限定排除
	// 已取消等终态单重复扫描），createdAt 升序稳定返回。
	ListPausedBefore(ctx context.Context, before time.Time) ([]ChangeOrder, error)
	// ListByNewCertID 按新证书 ID 查询变更单（任务 5.9 orphan-cleanup 归属单
	// 判定：新证书映射的孤儿，其归属变更单验证达标/终态后才可清理），createdAt
	// 降序稳定返回（首条=最近归属单，报告承载依据）。
	ListByNewCertID(ctx context.Context, newCertID string) ([]ChangeOrder, error)
	// ListPage 变更单列表分页查询（任务 5.11 GET /changes 状态 Tab 筛选）：
	// status 非空时按状态过滤，空=全部；createdAt 降序（最新在前，_id 作
	// 确定性 tie-breaker）；skip/limit 分页，limit<=0 返回空切片。返回当前页
	// 数据与满足筛选条件的总数（独立 CountDocuments）。
	ListPage(ctx context.Context, status ChangeStatus, skip, limit int) ([]ChangeOrder, int64, error)
	// SetBatchInfo Confirm 固化批次分配（任务 5.7）：batchInfo 整体写入。
	// CAS status=pending_confirm——已执行/已迁移单上的固化写入静默失效
	//（ModifiedCount=0），调用方以返回的写入布尔感知竞态并拒绝。
	SetBatchInfo(ctx context.Context, id string, batch *BatchInfo) (bool, error)
	// EnterVerify 进入验证窗口（任务 5.7 批间流转/终批进入验证）：
	// CAS executing→verifying，同一原子 update 写入 verifyWindowUntil；
	// batchInfo 存在（分批单）时一并固化批间暂停标记（paused=true、
	// pausedAt=pausedAt——pause-timeout 调度与 Cancel 整单取消判据），
	// batchInfo 不存在（未分批单）时保持缺失不产生残缺子文档。
	// CAS 未命中（非 executing）返回 false。
	EnterVerify(ctx context.Context, id string, verifyWindowUntil, pausedAt time.Time) (bool, error)
	// AdvanceBatch 人工续批放行（任务 5.7 ConfirmBatch）：
	// CAS fromStatus（verifying=批级验证达标后放行 | executing=批间暂停态放行）
	// → executing，同一原子 update 写 batchInfo.currentBatch=nextBatch、
	// paused=false 并 $unset pausedAt（门控校验归 service 层，本原语只承载
	// 放行的原子性——并发双 ConfirmBatch 仅一个生效）。CAS 未命中返回 false。
	AdvanceBatch(ctx context.Context, id string, fromStatus ChangeStatus, nextBatch int) (bool, error)
	// SetVerifyExpected 固化验证窗口预期终态快照（任务 5.10 AC-1）：CAS
	// status=verifying，同一原子 update 写 verifyExpected 并对齐 verifyWindowUntil
	//（=expected.WindowUntil——扫描与判定两口径一致）；分批单每批进入 verifying
	// 时覆盖刷新（固化后不随台账变化）。CAS 未命中（状态已并发迁移）返回 false。
	SetVerifyExpected(ctx context.Context, id string, expected *VerifyExpected) (bool, error)
	// PauseAfterVerify 批级窗口收敛回批间暂停（任务 5.10）：CAS verifying→executing，
	// 同一原子写 batchInfo.paused=true、pausedAt=pausedAt（当前批保持不变——
	// 人工决策（回滚/取消）与 ConfirmBatch 续批门控均在批间暂停态可用）。
	// 仅分批单适用（filter 含 batchInfo.currentBatch 存在性；未分批/终批窗口
	// 收敛走 TransitionTerminal* 终态原语）。CAS 未命中返回 false。
	PauseAfterVerify(ctx context.Context, id string, pausedAt time.Time) (bool, error)
	// ListVerifyingExpired 窗口到期扫描集（任务 5.10 window-expiry，Hard Rule：
	// 窗口达标判定不依赖被动探测触发——终局判定由 scheduler 主动扫描）：
	// status=verifying 且 verifyWindowUntil 非空且 <= before，createdAt 升序
	// 稳定返回。
	ListVerifyingExpired(ctx context.Context, before time.Time) ([]ChangeOrder, error)
}

// ItemBatchAssignment Confirm 批次分配的单项指派（任务 5.7）。
type ItemBatchAssignment struct {
	ItemID  string // 变更项 ID（hex）
	BatchNo int    // 目标批次号，>=1
}

// ChangeItemRepository 变更项仓储（cert_change_items）。
type ChangeItemRepository interface {
	// CreateMulti 批量写入变更项；DEFAULT 填充：status=pending。
	CreateMulti(ctx context.Context, items []ChangeItem) (int, error)
	ListByOrder(ctx context.Context, orderID string) ([]ChangeItem, error)
	// GetByID 按文档 ID 查询单个变更项（任务 5.7 子任务按项取载）；非法 ID
	// 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
	GetByID(ctx context.Context, itemID string) (ChangeItem, error)
	// ListByOrderAndBatch 当前批执行取项（idx_order_batch，batchNo=currentBatch）。
	ListByOrderAndBatch(ctx context.Context, orderID string, batchNo int) ([]ChangeItem, error)
	// AssignBatches Confirm 固化批次归属（任务 5.7）：逐项写入 batchNo
	//（仅 pending_confirm 确认时点一次性固化，执行期清单快照固定不变）。
	AssignBatches(ctx context.Context, orderID string, assignments []ItemBatchAssignment) error
	// UpdateHeartbeat 刷新执行心跳（子任务运行期 30s 间隔；executing-timeout 判据）。
	UpdateHeartbeat(ctx context.Context, itemID string, at time.Time) error
	// ClaimForExecution 子任务领取执行权（任务 5.7）：CAS status=pending→running
	// 并同原子写 heartbeatAt/heartbeat 基准与 executedAt。返回 false=未命中
	//（已被领取或已终态——任务框架重投递时幂等跳过），非错误。
	ClaimForExecution(ctx context.Context, itemID string, at time.Time) (bool, error)
	// MarkRateLimited 项遇云 API 限流（任务 5.7）：status→rate_limited（进度
	// 轮询可见"限流重试中"）并刷新 heartbeatAt（退避期间保活，防
	// executing-timeout 误判）。
	MarkRateLimited(ctx context.Context, itemID string, at time.Time) error
	// FinishItem 项级终态收敛（任务 5.7）：CAS status ∈ {running, rate_limited}
	// → 终态（success/failed），同原子写 error、newCloudCertId（成功项两段式
	// 产物，回滚/报告关联）与 heartbeatAt 终值。CAS 未命中返回 false
	//（超时恢复与子任务完成竞争时的落败方，幂等）。
	FinishItem(ctx context.Context, itemID string, status ChangeItemStatus, errMsg, newCloudCertID string, at time.Time) (bool, error)
	// FinishRollback 回滚项级终态收敛（任务 5.8）：CAS status=success →
	// rolled_back | rollback_failed，同原子写 error（回滚失败错误码+详情；
	// rolled_back 不覆盖既有字段）。status 仅接受这两个回滚终态，其余入参
	// 返回错误。CAS 未命中返回 false（重复回滚/并发回滚落败方，幂等跳过）。
	FinishRollback(ctx context.Context, itemID string, status ChangeItemStatus, errMsg string) (bool, error)
	// ListRunningBefore running 且心跳早于 before 的变更项（任务 5.7
	// executing-timeout 扫描集：heartbeatAt + itemHeartbeatTimeoutMinutes < now；
	// heartbeatAt 缺失的 running 项视为超时——领取即写心跳，缺失即异常残留）。
	ListRunningBefore(ctx context.Context, before time.Time) ([]ChangeItem, error)
	// MarkPendingSkipped 将订单全部未执行项（status=pending，含未到期批次）
	// 标记为 skipped（任务 5.1 Cancel：整单取消/批间暂停超时/执行中止共用）；
	// running 及已完结项不受影响。返回标记条数。
	MarkPendingSkipped(ctx context.Context, orderID string) (int64, error)
	// ListPatchCRDDueRecheck patch_crd 项到期复检扫描集（任务 5.9 crd-recheck）：
	// status=success 且 recheckedAt 缺失（单轮复检幂等标记）且 executedAt 早于
	// before（before = now - thresholds.recheckDelayMinutes）；executedAt 缺失的
	// 成功项不构成候选（领取执行权即写 executedAt，缺失即异常残留，交由既有
	// 超时/对账路径处理）。返回按 executedAt 升序稳定排序。
	ListPatchCRDDueRecheck(ctx context.Context, before time.Time) ([]ChangeItem, error)
	// MarkRechecked 复检结果回填（任务 5.9）：CAS status=success——passed=true
	// 保持 success 并写 recheckedAt；passed=false 转 failed 并同原子写 error +
	// recheckedAt（reconcile 回写/读取失败，Hard Rule：复检次数固定 1，失败
	// 转人工）。CAS 未命中（项已 failed/回滚终态/已复检）返回 false（幂等跳过，
	// 不重复告警）。
	MarkRechecked(ctx context.Context, itemID string, passed bool, errMsg string, at time.Time) (bool, error)
}

// CloudCertMappingRepository 平台证书↔云证书映射仓储（cert_cloud_cert_mappings）。
type CloudCertMappingRepository interface {
	// Upsert 按 uk_fp_cloud_account（certFingerprint+cloud+accountKey）两段式去重写入；
	// DEFAULT 填充：uploadedAt=now、status=active。
	Upsert(ctx context.Context, m *CloudCertMapping) error
	ListByFingerprint(ctx context.Context, fingerprint string) ([]CloudCertMapping, error)
	// FindByCloudCertID 反查映射（任务 3.5 指纹解析：referencedCloudCertId →
	// certFingerprint）。cloud/accountKey 传空串时不参与过滤（K8s 引用跨云反查）；
	// 多条命中按 uploadedAt 最新一条返回（换证后映射刷新）；无命中返回
	// mongo.ErrNoDocuments。
	FindByCloudCertID(ctx context.Context, cloud, accountKey, cloudCertID string) (CloudCertMapping, error)
	// UpdateStatus 映射状态迁移（active→orphan，孤儿清理队列标记）。
	UpdateStatus(ctx context.Context, id string, status MappingStatus) error
	// ListByStatus 按状态查询映射（任务 5.9 orphan-cleanup 天级批扫扫描集：
	// status=orphan 即清理队列成员；集合规模有限，schema.sql 未定义专用索引）。
	ListByStatus(ctx context.Context, status MappingStatus) ([]CloudCertMapping, error)
	// DeleteByID 清理成功后删除映射（任务 5.9 CloudCertMapping 状态流转
	// orphan→清理成功：schema.sql status enum 仅 active/orphan，"标 cleaned"
	// 以删除承载，保留 active/orphan 二值语义不引入第三态）。
	DeleteByID(ctx context.Context, id string) error
}

// ProbeResultRepository TLS 探测结果仓储（cert_probe_results，TTL 90 天）。
type ProbeResultRepository interface {
	// Create 写入探测结果；DEFAULT 填充：probeAt=now。
	Create(ctx context.Context, r *ProbeResult) error
	// LatestByDomain 最近一次探测（idx_domain_probe_desc）。
	LatestByDomain(ctx context.Context, domain string) (ProbeResult, error)
	// LatestPerDomain 每个域名的最近一次探测（domain 去重、probeAt 最新）；
	// 任务 4.5 看板 diffAlertCount 计数与 items.probeStatus 数据源（单查询，
	// 避免逐域 N+1）。空集合返回空切片。
	LatestPerDomain(ctx context.Context) ([]ProbeResult, error)
	// ListRecentByDomains 批量查询各域名最近探测记录（任务 5.10 验证窗口达标
	// 判定）：domain ∈ domains 过滤，每域名按 probeAt 降序至多 limit 条
	//（"连续 verifyConfirmProbes 次一致"判据的数据源）；domain 字典序、同域
	// probeAt 降序稳定返回。domains 为空或 limit<=0 返回空切片。
	ListRecentByDomains(ctx context.Context, domains []string, limit int) ([]ProbeResult, error)
}

// ExemptionRepository 探测豁免清单仓储（cert_exemptions，domain 唯一）。
type ExemptionRepository interface {
	// Upsert 按唯一 domain 写入；DEFAULT 填充：createdAt=now。
	Upsert(ctx context.Context, e *Exemption) error
	List(ctx context.Context) ([]Exemption, error)
	DeleteByDomain(ctx context.Context, domain string) error
}

// AlertConfigRepository 全局告警配置仓储（cert_alert_config，单文档 _id="global"）。
type AlertConfigRepository interface {
	// Get 读取全局配置；文档不存在时返回 schema.sql DEFAULT 填充的默认配置
	//（MongoDB 无列级 DEFAULT，默认值由 repository 写入路径保证）。
	Get(ctx context.Context) (AlertConfig, error)
	// Save 以 _id="global" upsert 保存。
	Save(ctx context.Context, cfg *AlertConfig) error
}

// K8sCredentialRepository K8s 集群接入凭证仓储（cert_k8s_credentials）。
// 不提供任何返回 kubeconfig 明文的方法（解密仅发生在 service 层内存中）。
type K8sCredentialRepository interface {
	// Create 写入集群凭证；clusterName 冲突返回 ErrDuplicateClusterName；createdAt=now。
	Create(ctx context.Context, c *K8sCredential) error
	GetByClusterName(ctx context.Context, clusterName string) (K8sCredential, error)
	List(ctx context.Context) ([]K8sCredential, error)
	DeleteByClusterName(ctx context.Context, clusterName string) error
}

// CertBatchSessionRepository 批量导入会话仓储（cert_batch_sessions，TTL 30 天）。
type CertBatchSessionRepository interface {
	// Create 写入会话并返回 batchId（hex）；DEFAULT 填充：createdAt=now、status=running、files 空数组。
	Create(ctx context.Context, s *CertBatchSession) (string, error)
	GetByID(ctx context.Context, id string) (CertBatchSession, error)
	// RecordFileResult 记录单文件结果并原子递增 progress：
	// files[fileIndex] 结果更新与 progress.done/failed $inc 在同一原子 update。
	RecordFileResult(ctx context.Context, id string, fileIndex int, result BatchFileResult, certID, errorReason string) error
	// MarkFinished 终态收敛：同原子 update 写 status/finishedAt。
	MarkFinished(ctx context.Context, id string, status BatchSessionStatus) error
}

// DiscoveryImportSessionRepository 云端发现导入会话仓储
// （cert_discovery_import_sessions，TTL 30 天；cert-cloud-discovery-import 任务 2）。
type DiscoveryImportSessionRepository interface {
	// Create 写入会话并返回会话 ID（hex）；DEFAULT 填充：createdAt=now、
	// status=running、items 空数组。先持久化再异步执行：Create 返回即可轮询
	//（浏览器中断不丢结果）。
	Create(ctx context.Context, s *DiscoveryImportSession) (string, error)
	GetByID(ctx context.Context, id string) (DiscoveryImportSession, error)
	// RecordItemResult 记录单条目结果并原子递增 progress：
	// items[itemIndex] 结果更新与 progress.succeeded/failed 的 $inc 在同一原子 update。
	RecordItemResult(ctx context.Context, id string, itemIndex int, result DiscoveryItemResult, mappedCertID, errorReason string) error
	// MarkFinished 终态收敛（按失败计数）：以库内 progress.failed 判定终态——
	// failed>0 → partial_failed，否则 completed；status 与 finishedAt 同一原子
	// update（聚合管道更新，无读-写竞态窗口）。
	MarkFinished(ctx context.Context, id string) error
}

// CrdRegistrationRepository 自定义 CRD 扫描登记仓储（cert_crd_registrations）。
type CrdRegistrationRepository interface {
	// Create 写入登记；clusterId+apiGroup+kind 冲突返回 ErrDuplicateCrdRegistration；createdAt=now、enabled=true。
	Create(ctx context.Context, r *CrdRegistration) error
	// GetByID 按文档 ID 查询（删除/启停前定位登记项，内置登记判定依据）；
	// 非法 ID 返回 ErrInvalidID，未命中返回 mongo.ErrNoDocuments。
	GetByID(ctx context.Context, id string) (CrdRegistration, error)
	List(ctx context.Context) ([]CrdRegistration, error)
	// ListEnabled enabled=true 登记项（K8sAPIChannel 扫描范围联动）。
	ListEnabled(ctx context.Context) ([]CrdRegistration, error)
	// SetEnabled 启停登记（enabled=false 时该 CRD 回归盲区，视图显式声明）。
	SetEnabled(ctx context.Context, id string, enabled bool) error
	DeleteByID(ctx context.Context, id string) error
}
