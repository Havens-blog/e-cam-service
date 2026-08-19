// Package deployer 提供变更执行通道抽象（ExecutionChannel，tech-design
// Interface 1）与云 API 通道编排（CloudAPIChannel，任务 5.3）。
//
// 分层定位：本包位于 cloudx 五方法适配（3.1/3.2，SDK 单次调用封装）之上、
// 变更执行引擎（5.7）之下——只做通道编排（两段式、映射写入、孤儿补偿标记），
// 不做业务级状态机判断（成功/失败/回滚语义归 5.7/5.8，Hard Rule）。
//
// 可插拔性（PRD In Scope 执行通道抽象）：云 API 与 K8s API 首期落地，
// 堡垒机/优维 Agent 二期实现——接口签名固定，新增通道仅新增实现
// （SimulatedChannel 验证零上层改动可插拔）。
package deployer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
)

// ChannelType 执行通道类型。对齐 tech-design Interface 1 Type() 取值；
// bastion/agent 为二期接口预留（仅常量，无实现）。
type ChannelType string

const (
	// ChannelTypeCloudAPI 云 API 通道（两段式 UploadCert→BindResource，本任务实现）。
	ChannelTypeCloudAPI ChannelType = "cloud_api"
	// ChannelTypeK8sAPI K8s API 通道（CRD patch + 管理权探测 + 复检，任务 5.6 实现）。
	ChannelTypeK8sAPI ChannelType = "k8s_api"
	// ChannelTypeBastion 堡垒机通道（二期接口预留，仅 Type 常量）。
	ChannelTypeBastion ChannelType = "bastion"
	// ChannelTypeAgent 优维 Agent 通道（二期接口预留，仅 Type 常量）。
	ChannelTypeAgent ChannelType = "agent"
)

// 通道层哨兵错误（输入校验/装配缺失；全部为静态文案，永不携带凭证/私钥片段）。
var (
	// ErrInvalidCredential 凭证载荷分支校验失败（Kind/必填分支，见 Credential.Validate）。
	ErrInvalidCredential = errors.New("deployer: invalid credential")
	// ErrInvalidTarget 部署目标分支校验失败（channel 分支必填，见 DeployTarget.Validate）。
	ErrInvalidTarget = errors.New("deployer: invalid deploy target")
	// ErrInvalidScope 发现范围校验失败（DiscoverScope.Validate）。
	ErrInvalidScope = errors.New("deployer: invalid discover scope")
	// ErrInvalidBatchConf 分批配置校验失败（ValidateBatchConf）。
	ErrInvalidBatchConf = errors.New("deployer: invalid batch conf")
	// ErrDeployerNotRegistered 目标云未注册 CloudDeployer 实例（5.4/5.5 组装缺失）。
	ErrDeployerNotRegistered = errors.New("deployer: no cloud deployer registered for cloud")
	// ErrCertMaterialUnavailable 证书材料不可用（未登记/无私钥，无法执行两段式第一段）。
	ErrCertMaterialUnavailable = errors.New("deployer: certificate material unavailable")
)

// ExecutionChannel 执行通道抽象（tech-design Interface 1，签名固定）���
// 隔离"发现/部署/回滚"与目标资源类型。Hard Rule——接口须容纳 bastion/agent
// 二期实现而不改签名（仅新增实现）；SimulatedChannel 验证零上层改动可插拔性。
type ExecutionChannel interface {
	// Discover 只读引用发现（scope.SnapshotID 回写至各 CertReference.snapshotId）。
	Discover(ctx context.Context, creds Credential, scope DiscoverScope) ([]domain.CertReference, error)
	// Deploy 部署 newCertFingerprint 证书至 target（云通道=两段式上传+绑定）。
	Deploy(ctx context.Context, creds Credential, target DeployTarget, newCertFingerprint string) (DeployResult, error)
	// Rollback 将 target 引用恢复为 oldRef（云通道=重新绑定旧云证书 ID）。
	Rollback(ctx context.Context, creds Credential, target DeployTarget, oldRef domain.CertReference) (RollbackResult, error)
	// Type 通道类型（cloud_api | k8s_api | bastion | agent）。
	Type() ChannelType
}

// ---------------------------------------------------------------------
// Service-Level Types（tech-design Service-Level Types 节，字段+类型+约束逐项对齐）
// ---------------------------------------------------------------------

// Credential 凭证 Kind 常量。
const (
	// CredentialKindCloudAK 云账号 AK/SK 凭证。
	CredentialKindCloudAK = "cloud_ak"
	// CredentialKindKubeconfig K8s 集群 kubeconfig 凭证。
	CredentialKindKubeconfig = "kubeconfig"
)

// Credential 执行通道凭证句柄（解密后的内存形态，用后 zeroing，禁入日志/响应）。
//
// Hard Rule（明文零生命周期）：Secret 仅内存传递——通道方法返回前对本副本
// Zeroize（值传递副本与调用方共享底层数组，调用方可观测清零）；禁序列化为
// string、禁写入任何结构体字段逃逸、永不进日志/响应/审计。完整生命周期管理
// 由调用方（5.7 执行引擎）负责：解密→逐项传递→全部用毕最终 Zeroize。
type Credential struct {
	Kind       string // "cloud_ak" | "kubeconfig"，必填
	Cloud      string // aliyun|tencent|huawei|aws|azure；Kind=cloud_ak 时必填，kubeconfig 时空
	AccountKey string // 云账号标识；Kind=cloud_ak 时必填，kubeconfig 时空
	AccessKey  string // AK；Kind=cloud_ak 时必填
	Secret     []byte // SK 或 kubeconfig 明文；仅内存，永不落盘/序列化
	KeyVersion int    // 解密所用主密钥版本，>=1；审计追溯用
}

// Zeroize 清零明文 Secret（复用 1.1 domain.Zeroize）；幂等，对 nil 安全。
// 用法：defer creds.Zeroize()。
func (c *Credential) Zeroize() {
	domain.Zeroize(&c.Secret)
}

// String 实现 Stringer：仅输出非敏感元数据（Kind/Cloud/AccountKey/KeyVersion），
// 防御性保证 fmt 打印结构体时 Secret 明文不进日志。
func (c Credential) String() string {
	return fmt.Sprintf("Credential{kind:%s cloud:%s accountKey:%s keyVersion:%d}",
		c.Kind, c.Cloud, c.AccountKey, c.KeyVersion)
}

// Validate 分支校验（tech-design Service-Level Types 约束）：
//   - Kind 必填（cloud_ak | kubeconfig）；
//   - cloud_ak：Cloud/AccountKey/AccessKey 必填、Secret 非空、KeyVersion>=1；
//   - kubeconfig：Cloud/AccountKey 须空、Secret 非空、KeyVersion>=1。
func (c Credential) Validate() error {
	switch c.Kind {
	case CredentialKindCloudAK:
		if c.Cloud == "" || c.AccountKey == "" || c.AccessKey == "" {
			return fmt.Errorf("%w: cloud_ak requires cloud, accountKey and accessKey", ErrInvalidCredential)
		}
	case CredentialKindKubeconfig:
		if c.Cloud != "" || c.AccountKey != "" {
			return fmt.Errorf("%w: kubeconfig requires empty cloud and accountKey", ErrInvalidCredential)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidCredential, c.Kind)
	}
	if len(c.Secret) == 0 {
		return fmt.Errorf("%w: empty secret", ErrInvalidCredential)
	}
	if c.KeyVersion < 1 {
		return fmt.Errorf("%w: keyVersion %d out of range", ErrInvalidCredential, c.KeyVersion)
	}
	return nil
}

// DiscoverScope 单次引用发现的目标范围。
type DiscoverScope struct {
	Clouds     []string // 云列表；空=全部已接入云
	Products   []string // cdn|dcdn|waf|alb|clb|nlb；空=该云全部已支持产品
	ClusterIDs []string // K8s 集群 ID 列表；仅 K8sAPIChannel 使用，云通道忽略
	SnapshotID string   // 归属扫描快照 ID，必填；发现的 CertReference.snapshotId 回写此值
}

// Validate 校验发现范围（SnapshotID 必填——发现结果必须可归属快照）。
func (s DiscoverScope) Validate() error {
	if s.SnapshotID == "" {
		return fmt.Errorf("%w: snapshotID is required", ErrInvalidScope)
	}
	return nil
}

// DeployTarget 单个部署/回滚动作的目标资源定位。
// 字段与 domain.ResourceRef（cert_change_items.resourceRef 持久化形态，任务 1.2）
// 1:1 对齐；按 Channel 分支必填（对齐 schema.sql anyOf 校验器，见 Validate）。
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

// Validate 按 channel 分支校验必填（对齐 schema.sql cert_change_items.resourceRef
// anyOf：upload_and_bind(cloud_api) 分支 channel+cloud+product+accountKey+resourceId；
// patch_crd(k8s_api) 分支 channel+clusterId+namespace+kind+resourceId）。
func (t DeployTarget) Validate() error {
	if t.ResourceID == "" {
		return fmt.Errorf("%w: resourceId is required", ErrInvalidTarget)
	}
	switch ChannelType(t.Channel) {
	case ChannelTypeCloudAPI:
		if t.Cloud == "" || t.Product == "" || t.AccountKey == "" {
			return fmt.Errorf("%w: cloud_api requires cloud, product and accountKey", ErrInvalidTarget)
		}
	case ChannelTypeK8sAPI:
		if t.ClusterID == "" || t.Namespace == "" || t.Kind == "" {
			return fmt.Errorf("%w: k8s_api requires clusterId, namespace and kind", ErrInvalidTarget)
		}
	default:
		return fmt.Errorf("%w: unknown channel %q", ErrInvalidTarget, t.Channel)
	}
	return nil
}

// ToResourceRef 转换为持久化形态（5.7 子任务凭 resourceRef 重构 DeployTarget，
// 不回查台账/快照）。
func (t DeployTarget) ToResourceRef() domain.ResourceRef {
	return domain.ResourceRef{
		Channel:    domain.Channel(t.Channel),
		Cloud:      t.Cloud,
		Product:    t.Product,
		AccountKey: t.AccountKey,
		ClusterID:  t.ClusterID,
		Namespace:  t.Namespace,
		Kind:       t.Kind,
		ResourceID: t.ResourceID,
	}
}

// DeployTargetFromResourceRef 从持久化 resourceRef 重构 DeployTarget
// （ToResourceRef 的逆变换，5.7 执行子任务入口）。
func DeployTargetFromResourceRef(ref domain.ResourceRef) DeployTarget {
	return DeployTarget{
		Channel:    string(ref.Channel),
		Cloud:      ref.Cloud,
		Product:    ref.Product,
		AccountKey: ref.AccountKey,
		ClusterID:  ref.ClusterID,
		Namespace:  ref.Namespace,
		Kind:       ref.Kind,
		ResourceID: ref.ResourceID,
	}
}

// DeployResult Deploy 单项执行结果。
type DeployResult struct {
	NewCloudCertID  string // 两段式第一段产物的云证书 ID；K8s 通道为空
	OldCloudCertID  string // 被替换的云侧证��� ID；回滚依据，执行前从引用快照读取
	OrphanCandidate bool   // true=旧云证书成为孤儿候选，验证达标后入清理队列（scheduler orphan-cleanup 任务消费，结果记入 ChangeReport.OrphanCleanup）
	RecheckPassed   bool   // K8s 通道：crd-recheck 复检结果；云通道恒 false
}

// RollbackResult 失败错误码（tech-design RollbackResult.ErrCode 取值）。
const (
	// ErrCodeCloudRateLimited 云 API 限流（CLOUD_API_RATELIMITED，503 语义）。
	ErrCodeCloudRateLimited = "CLOUD_API_RATELIMITED"
	// ErrCodeK8sUnreachable 集群不可达（K8S_UNREACHABLE，503 语义）。
	ErrCodeK8sUnreachable = "K8S_UNREACHABLE"
	// ErrCodeRollbackTargetInvalid 回滚目标无效（ROLLBACK_TARGET_INVALID，409 语义；
	// 由 5.8 GetCert 三判定产生，通道自身不产生该码）。
	ErrCodeRollbackTargetInvalid = "ROLLBACK_TARGET_INVALID"
)

// RollbackResult Rollback 单项回滚结果。
type RollbackResult struct {
	Success       bool                 // 项级成败
	RestoredRef   domain.CertReference // 回滚成功后的引用形态（含恢复的 cloudCertId）；失败时为零值
	OrphanCleaned []string             // 回滚中同步清理的孤儿云证书 ID 列表；无则空（云通道标记入 5.9 队列异步清理，由 5.8 编排填充）
	ErrCode       string               // 失败错误码：CLOUD_API_RATELIMITED|K8S_UNREACHABLE|ROLLBACK_TARGET_INVALID
	Reason        string               // 失败详情；不得含私钥/凭证片段
}

// CloudCertInfo GetCert 返回的云侧证书在库状态（回滚目标有效性校验依据）。
type CloudCertInfo struct {
	Exists      bool      // 云证书库中该 cloudCertId 是否存在
	NotAfter    time.Time // 云侧证书有效期截止；Exists=false 时零值
	Fingerprint string    // 云侧证书 SHA256 指纹；复核回滚目标未被替换
}

// BatchConf 分批灰度配置（Confirm 入参）。分批一律人工续批（不提供自动续批）；
// 每批执行完成且批级验证达标后订单转批间暂停，由 ConfirmBatch 人工续批。
type BatchConf struct {
	Enabled       bool    // 是否分批；false=单批全量（仅引用数 <= 阈值时允许）
	BatchSize     int     // 每批资源数，>0；Enabled=true 时必填；有效批大小 = min(BatchSize, floor(total/2))
	MaxBatchRatio float64 // 单批占全部引用比例上限，(0, 0.5]；硬约束 <=0.5（PRD 分批灰度 ≤50%）
}

// MaxBatchRatioLimit 单批占全部引用比例硬上限（PRD 分批灰度 ≤50%）。
const MaxBatchRatioLimit = 0.5

// ValidateBatchConf 校验分批配置（供 5.7 Confirm 复用）：
//   - Enabled=true：BatchSize>0 必填，MaxBatchRatio ∈ (0, 0.5]；
//   - Enabled=false：单批全量，分批字段不参与约束（零值合法）。
func ValidateBatchConf(conf BatchConf) error {
	if !conf.Enabled {
		return nil
	}
	if conf.BatchSize <= 0 {
		return fmt.Errorf("%w: batchSize must be > 0 when batching enabled", ErrInvalidBatchConf)
	}
	if conf.MaxBatchRatio <= 0 || conf.MaxBatchRatio > MaxBatchRatioLimit {
		return fmt.Errorf("%w: maxBatchRatio %g out of range (0, %.1f]",
			ErrInvalidBatchConf, conf.MaxBatchRatio, MaxBatchRatioLimit)
	}
	return nil
}

// EffectiveBatchSize 有效批大小 = min(BatchSize, floor(total/2))
// （tech-design 分批执行门控：首批硬约束单批 ≤ floor(total/2)，对应 PRD ≤50%）。
// total < 2 时返回 total：单引用无灰度拆分意义，调用方（5.7 Confirm）应将其
// 走单批全量或拒绝分批，而非产生空首批。
func EffectiveBatchSize(batchSize, total int) int {
	if total < 2 {
		return total
	}
	half := total / 2 // floor(total/2)
	return int(math.Min(float64(batchSize), float64(half)))
}

// ---------------------------------------------------------------------
// CloudDeployer 端口与配套来源端口
// ---------------------------------------------------------------------

// CloudDeployer per 云 per 产品部署器端口（tech-design Interface 2）。
// 3.1/3.2 cloudx SDK 适配经 5.4/5.5 组装为本接口实例注入 CloudAPIChannel
// （本任务仅提供通用编排，不绑定具体云 SDK）；discovery-only 三云
// （huawei/aws/azure）的桥接实现 UploadCert/BindResource/CleanupOrphan 返回
// cloudx.ErrDiscoveryOnly，进入变更清单时已被 5.2 判不可执行项，不会触达。
// 限流退避重试有界策略属 5.4/5.5 部署器实现（Hard Rule：禁止无限重试），
// 本端口仅承载单次动作语义。
type CloudDeployer interface {
	// UploadCert 两段式第一段：上传证书束至云证书库，返回云证书 ID。
	// keyPEM 为解密后明文私钥（仅内存传递，实现方用后不得留存）。
	UploadCert(ctx context.Context, creds Credential, certPEM string, keyPEM []byte) (string, error)
	// BindResource 两段式第二段：将云证书绑定至目标产品资源。
	BindResource(ctx context.Context, creds Credential, product, resourceID, cloudCertID string) error
	// ListReferences 只读发现（返回完整 CertReference 形态，含指纹解析；
	// 指纹解析口径同 3.5：映射反查 → GetCert 要素 → 确定性占位指纹）。
	ListReferences(ctx context.Context, creds Credential, product string) ([]domain.CertReference, error)
	// GetCert 查询云侧证书在库状态（回滚目标有效性校验依据，只读）。
	GetCert(ctx context.Context, creds Credential, cloudCertID string) (CloudCertInfo, error)
	// CleanupOrphan 孤儿证书清理（对已删除证书幂等成功，见 3.1/3.2 口径）。
	CleanupOrphan(ctx context.Context, creds Credential, cloudCertID string) error
}

// CertMaterialSource 证书材料来源：按指纹取证书束 PEM 与解密后的私钥明文。
// 生产实现见 LedgerMaterialSource（台账仓储 + 信封加密解密，任务 1.1 体系）；
// keyPEM 仅内存传递（[]byte 保证可 Zeroize），通道用毕即清零，永不落日志。
type CertMaterialSource interface {
	// Material 返回证书束 PEM（公开材料）与明文私钥及其主密钥版本。
	// 证书未登记或无私钥（fingerprint_only）时返回 ErrCertMaterialUnavailable。
	Material(ctx context.Context, fingerprint string) (certPEM string, keyPEM []byte, keyVersion int, err error)
}

// OldRefSource 执行前引用快照来源：读取目标资源当前引用（DeployResult.
// OldCloudCertID 与孤儿候选判定依据，tech-design"执行前从引用快照读取"）。
// 生产实现见 SnapshotOldRefSource（最新成功扫描快照）；found=false 表示无
// 已知引用（首次部署，OldCloudCertID 为空、不构成孤儿候选）。
type OldRefSource interface {
	// CurrentRef 返回目标资源最新引用；found=false 表示无已知引用。
	CurrentRef(ctx context.Context, cloud, product, resourceID string) (domain.CertReference, bool, error)
}
