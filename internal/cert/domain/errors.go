package domain

import (
	"errors"
	"time"
)

// CERT_* 同步拦截错误码（tech-design Error Types & Codes）。
// CERT_DUPLICATE_FINGERPRINT 由仓储层哨兵 ErrDuplicateFingerprint 承接（uk_fingerprint
// 冲突映射，任务 1.2），此处仅声明码值供 web 层统一映射 409。
const (
	// CodeCertKeyMismatch 证书与私钥不匹配（400）。
	CodeCertKeyMismatch = "CERT_KEY_MISMATCH"
	// CodeCertChainIncomplete 证书链缺失（400）。
	CodeCertChainIncomplete = "CERT_CHAIN_INCOMPLETE"
	// CodeCertParseFail SAN 结构无法解析/已过期（400）。
	CodeCertParseFail = "CERT_PARSE_FAIL"
	// CodeCertDuplicateFingerprint 重复指纹导入（409，仓储哨兵承接）。
	CodeCertDuplicateFingerprint = "CERT_DUPLICATE_FINGERPRINT"
	// CodeCertHasRefs 存在活跃引用或处于保护期，禁止删除（409，任务 2.3 删除拦截）。
	CodeCertHasRefs = "CERT_HAS_REFS"
	// CodeScanInProgress 扫描进行中（防重触发，409，任务 3.5）。
	CodeScanInProgress = "SCAN_IN_PROGRESS"
	// CodeChangeNotCancellable 当前状态不可取消（409，任务 5.1 Cancel 语义）。
	CodeChangeNotCancellable = "CHANGE_NOT_CANCELLABLE"
	// CodeScanStale 扫描超新鲜度阈值，清单生成阻断（409，任务 5.2）。
	CodeScanStale = "SCAN_STALE"
	// CodeSanInsufficient 新证书 SAN 不⊇目标域名，清单生成阻断（409，任务 5.2）。
	CodeSanInsufficient = "SAN_INSUFFICIENT"
	// CodeNewCertFingerprintOnly 新证书仅指纹登记（无私钥），无法上传云证书库
	// 执行更换，清单生成阻断（409，任务 5.2）。
	CodeNewCertFingerprintOnly = "NEW_CERT_FINGERPRINT_ONLY"
	// CodeK8sUnreachable K8s 集群不可达（503，任务 3.4 动态客户端工厂/请求期连接失败）。
	CodeK8sUnreachable = "K8S_UNREACHABLE"
	// CodeBatchNotConfirmable 续批门控未满足（409，任务 5.7 ConfirmBatch：上一批
	// 存在失败项或批级验证未达标）。
	CodeBatchNotConfirmable = "BATCH_NOT_CONFIRMABLE"
	// CodeExecTimeout 变更项执行心跳超时（executing-timeout 恢复产物，任务 5.7：
	// ChangeItem.status=failed + error=EXEC_TIMEOUT；非 HTTP 错误码——异步子任务
	// 状态载体，进度轮询/报告接口以 status 字段呈现，不产生同步错误响应）。
	CodeExecTimeout = "EXEC_TIMEOUT"
	// CodeCloudApiRateLimited 云 API 限流（CLOUD_API_RATELIMITED，任务 5.7 项级
	// 退避上限耗尽终因；deployer.ErrCodeCloudRateLimited 同值，此处声明项级
	// error 字段口径）。
	CodeCloudApiRateLimited = "CLOUD_API_RATELIMITED"
	// CodeChangeInFlight 同一旧证书存在在途变更单（409，uk_active_mutex 冲突；
	// 哨兵 ErrChangeInFlight 在 repository.go——仓储层错误，非 CertError）。
	CodeChangeInFlight = "CHANGE_IN_FLIGHT"
	// CodeRollbackTargetInvalid 回滚目标无效（409，任务 5.8 GetCert 三判定）：
	// Exists=false（云侧已删除）、NotAfter < now（已过期）或 Fingerprint ≠
	// 订单 oldCertFingerprint（目标被替换）。无效目标绝不自动回滚（Hard Rule）。
	CodeRollbackTargetInvalid = "ROLLBACK_TARGET_INVALID"
)

// CertError 携带 CERT_* 错误码的证书域错误，供 web 层映射 HTTP status + code 信封。
// message 为静态文案/安全参数（时间、算法名等），永不包含私钥、密文或 PEM 片段。
// 以指针哨兵形式定义（var Err* = &CertError{...}），配套 errors.Is / errors.As 使用；
// 抛出带上下文的错误一律 fmt.Errorf("%w: ...", ErrXxx) 包装，上下文同样不得含���感材料。
type CertError struct {
	code string
	msg  string
}

func newCertError(code, msg string) *CertError {
	return &CertError{code: code, msg: msg}
}

// Error 实现 error 接口；消息统一 "cert: " 前缀（与仓储层哨兵风格一致）。
func (e *CertError) Error() string { return "cert: " + e.msg }

// Code 返回 CERT_* 错误码。
func (e *CertError) Code() string { return e.code }

// 完整性校验四项拦截对应的三个 400 类领域错误（第四项指纹重复由仓储层哨兵承接）。
var (
	// ErrKeyMismatch 证书公钥与私钥派生公钥不匹配（RSA 模数/ECDSA 曲线点比对失败）。
	ErrKeyMismatch = newCertError(CodeCertKeyMismatch, "certificate does not match private key")
	// ErrChainIncomplete 证书链缺失（leaf+中间链无法构建到自签根的可验证链）。
	ErrChainIncomplete = newCertError(CodeCertChainIncomplete, "certificate chain incomplete")
	// ErrParseFail 证书解析失败（PEM/x509 非法、SAN 结构无法解析、有效期外、私钥 PEM 非法）。
	ErrParseFail = newCertError(CodeCertParseFail, "certificate parse failed")
)

// ErrBatchNotConfirmable 续批门控未满足（409 BATCH_NOT_CONFIRMABLE，任务 5.7）：
// 上一批存在失败项、批级验证未达标（提频探测连续 verifyConfirmProbes 次一致，
// 判定归 5.10）、或当前状态不在续批窗口（verifying / executing+批间暂停）。
var ErrBatchNotConfirmable = newCertError(CodeBatchNotConfirmable, "batch continuation gate not satisfied")

// ErrK8sUnreachable 集群不可达（503 K8S_UNREACHABLE，tech-design Error Types & Codes）。
// 由 internal/cert/k8s 动态客户端工厂在 kubeconfig 构建失败或请求期连接失败
// （网络不可达/拒连/超时等 net.Error 族）时以 %w 包装抛出；message 永不携带
// kubeconfig 明文/密文片段（Hard Rule：禁入日志/响应/审计）。
var ErrK8sUnreachable = newCertError(CodeK8sUnreachable, "kubernetes cluster unreachable")

// ErrScanInProgress 扫描进行中（409 SCAN_IN_PROGRESS，任务 3.5 防重）：
// 已存在 status=running 的扫描快照时拒绝再次触发（释放需等待完成或
// scan-timeout 恢复）。
var ErrScanInProgress = newCertError(CodeScanInProgress, "scan already in progress")

// ErrChangeNotCancellable 当前状态不可取消（409 CHANGE_NOT_CANCELLABLE，任务 5.1）：
// verifying 与全部终态不可取消——draft/pending_confirm/批间暂停走整单取消、
// executing 仅中止未开始项（执行中项等待完成后收敛）。
var ErrChangeNotCancellable = newCertError(CodeChangeNotCancellable, "change order not cancellable in current state")

// 清单生成四阻断中的三个 CertError 哨兵（第四项 CHANGE_IN_FLIGHT 由仓储层
// 哨兵 ErrChangeInFlight 承接，uk_active_mutex 索引强制；任务 5.2）。
var (
	// ErrScanStale 扫描超新鲜度阈值（409 SCAN_STALE）：无成功快照，或最新成功
	// 快照 now-startedAt > thresholds.scanFreshnessHours——清单强制绑定最近扫描，
	// 超期阻断生成并提示先扫描。
	ErrScanStale = newCertError(CodeScanStale, "scan snapshot exceeds freshness threshold")
	// ErrSanInsufficient 新证书 SAN 不覆盖目标域名（409 SAN_INSUFFICIENT）：
	// 基准 = 变更清单目标域名集合（旧证书 SAN，即清单项所服务域名并集），
	// 防通配符/多 SAN 证书更换时"静默丢域名"漏换。
	ErrSanInsufficient = newCertError(CodeSanInsufficient, "new certificate SAN does not cover target domains")
	// ErrNewCertFingerprintOnly 新证书仅指纹登记（409 NEW_CERT_FINGERPRINT_ONLY）：
	// hostingStatus≠complete 无私钥，无法执行两段式第一段（上传云证书库）。
	ErrNewCertFingerprintOnly = newCertError(CodeNewCertFingerprintOnly, "new certificate is fingerprint-only registration")
)

// ErrRollbackTargetInvalid 回滚目标无效（409 ROLLBACK_TARGET_INVALID，任务 5.8）：
// ChangeService.Rollback 对每个待回滚成功项前置 GetCert(oldCloudCertId) 三判定，
// 任一命中即整体阻断——不自动回滚、转人工决策并记录审计（Hard Rule）。
var ErrRollbackTargetInvalid = newCertError(CodeRollbackTargetInvalid, "rollback target invalid (deleted, expired or replaced)")

// AsCertError 从错误链（含 fmt.Errorf %w 包装层）提取 CertError；
// 非 CertError 错误（如 mongo.ErrNoDocuments）返回 false，调用方走默认映射。
func AsCertError(err error) (*CertError, bool) {
	var ce *CertError
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}

// DeleteBlockedError 删除拦截错误（409 CERT_HAS_REFS，任务 2.3）。
// tech-design 删除拦截规则：has_refs 与 blind_spot 一律拦截（blind_spot 附盲区原因）；
// 仅 no_refs_scanned 且不在保护期（protectUntil < now）允许删除，保护期内拦截附截止时间。
// Reason 为静态文案+安全参数（引用计数/云产品枚举/RFC3339 时间），不含任何敏感材料。
type DeleteBlockedError struct {
	ReferenceStatus ReferenceStatus // 拦截时的引用三态
	RefCount        int             // has_refs 时为最新成功快照引用计数，否则 0
	Reason          string          // 拦截原因（盲区原因/引用计数/保护期截止）
	ProtectUntil    *time.Time      // 保护期拦截时携带截止时间，其余为 nil
}

// Error 实现 error 接口；消息统一 "cert: " 前缀（与仓储层哨兵风格一致）。
func (e *DeleteBlockedError) Error() string { return "cert: " + e.Reason }
