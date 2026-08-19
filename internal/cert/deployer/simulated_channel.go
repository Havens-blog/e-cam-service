package deployer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
)

// SimulatedOutcome 模拟通道行为模式（成功/失败/限流可配置）。
type SimulatedOutcome int

const (
	// SimulatedSucceed 模拟成功路径。
	SimulatedSucceed SimulatedOutcome = iota
	// SimulatedFailure 模拟失败路径（返回显式错误）。
	SimulatedFailure
	// SimulatedRateLimit 模拟云 API 限流（错误链携带 cloudx.ErrCloudRateLimited 哨兵，
	// 供上层 errors.Is 判定与 rate_limited 状态承接验证）。
	SimulatedRateLimit
)

// simulatedMethod 调用方法名（调用记录用）。
const (
	simMethodDiscover  = "Discover"
	simMethodDeploy    = "Deploy"
	simMethodRollback  = "Rollback"
	simCertIDPrefix    = "sim-cert-"
	simFailureMessage  = "simulated channel failure"
	simRateLimitFormat = "simulated channel rate limited: %w"
)

// SimulatedCall 模拟通道调用记录（断言与 e2e 演示用）。
type SimulatedCall struct {
	Method        string // Discover | Deploy | Rollback
	Cloud         string // 凭证归属云
	Product       string // Deploy/Rollback 目标产品
	ResourceID    string // Deploy/Rollback 目标资源
	CertForDeploy string // Deploy：newCertFingerprint
}

// SimulatedChannel 模拟执行通道（mock 成功/失败/限流可配置），验证
// "堡垒机/Agent 通道零上层改动"可插拔性（PRD In Scope 执行通道抽象；e2e
// 以本通道扮演 bastion/agent 二期通道，消费方按 Type() 路由零改动）。
// 不触达任何云 SDK/仓储；并发安全（调用记录互斥）。
type SimulatedChannel struct {
	mu          sync.Mutex
	channelType ChannelType
	discover    SimulatedOutcome
	deploy      SimulatedOutcome
	rollback    SimulatedOutcome
	discoverRef domain.CertReference // Discover 成功路径返回的引用模板
	nextID      int
	calls       []SimulatedCall
}

// NewSimulatedChannel 创建模拟通道。channelType 可为任意已定义值——
// 演示 bastion/agent 可插拔性时传 ChannelTypeBastion/ChannelTypeAgent。
func NewSimulatedChannel(channelType ChannelType) *SimulatedChannel {
	return &SimulatedChannel{channelType: channelType}
}

// Type 通道类型（构造期指定）。
func (s *SimulatedChannel) Type() ChannelType { return s.channelType }

// SetDiscoverOutcome / SetDeployOutcome / SetRollbackOutcome 配置各方法行为。
func (s *SimulatedChannel) SetDiscoverOutcome(o SimulatedOutcome) *SimulatedChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discover = o
	return s
}

func (s *SimulatedChannel) SetDeployOutcome(o SimulatedOutcome) *SimulatedChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deploy = o
	return s
}

func (s *SimulatedChannel) SetRollbackOutcome(o SimulatedOutcome) *SimulatedChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollback = o
	return s
}

// SetDiscoverRef 配置 Discover 成功路径返回的引用模板（snapshotId 由
// scope 回写覆盖）。
func (s *SimulatedChannel) SetDiscoverRef(ref domain.CertReference) *SimulatedChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discoverRef = ref
	return s
}

// Calls 返回调用记录快照（写入序稳定）。
func (s *SimulatedChannel) Calls() []SimulatedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SimulatedCall(nil), s.calls...)
}

// record 追加调用记录。
func (s *SimulatedChannel) record(method, cloud, product, resourceID, certForDeploy string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, SimulatedCall{
		Method: method, Cloud: cloud, Product: product,
		ResourceID: resourceID, CertForDeploy: certForDeploy,
	})
}

// outcome 读取指定方法当前行为。
func (s *SimulatedChannel) outcome(which *SimulatedOutcome) SimulatedOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *which
}

// simErr 按行为模式构造错误；ok=true 表示成功路径。
func simErr(o SimulatedOutcome) (bool, error) {
	switch o {
	case SimulatedFailure:
		return false, errors.New(simFailureMessage)
	case SimulatedRateLimit:
		return false, fmt.Errorf(simRateLimitFormat, cloudx.ErrCloudRateLimited)
	default:
		return true, nil
	}
}

// nextCloudCertID 生成确定性模拟云证书 ID 序列（sim-cert-1、sim-cert-2 …）。
func (s *SimulatedChannel) nextCloudCertID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return fmt.Sprintf("%s%d", simCertIDPrefix, s.nextID)
}

// Discover 模拟发现：成功返回模板引用（snapshotId 按 scope 回写）；
// 限流路径错误链携带 cloudx.ErrCloudRateLimited。Clouds 范围过滤与凭证
// 校验同真实通道语义（模拟通道不做持久化，故不强制 SnapshotID 非空——
// 演示通道面向 e2e，快照归属由消费方保证）。
func (s *SimulatedChannel) Discover(ctx context.Context, creds Credential, scope DiscoverScope) ([]domain.CertReference, error) {
	defer creds.Zeroize()
	s.record(simMethodDiscover, creds.Cloud, "", "", "")
	if ok, err := simErr(s.outcome(&s.discover)); !ok {
		return nil, fmt.Errorf("simulated channel discover: %w", err)
	}
	s.mu.Lock()
	ref := s.discoverRef
	ref.Cloud = domain.Cloud(creds.Cloud)
	if ref.Cloud == "" {
		ref.Cloud = domain.Cloud("simulated")
	}
	ref.SnapshotID = scope.SnapshotID
	s.mu.Unlock()
	if ref.ResourceID == "" { // 未配置模板 → 空发现（合法成功语义）
		return []domain.CertReference{}, nil
	}
	return []domain.CertReference{ref}, nil
}

// Deploy 模拟部署：成功返回递增模拟云证书 ID（OldCloudCertID 空、
// 无孤儿候选——模拟通道不持有引用快照语义）。
func (s *SimulatedChannel) Deploy(ctx context.Context, creds Credential, target DeployTarget, newCertFingerprint string) (DeployResult, error) {
	defer creds.Zeroize()
	s.record(simMethodDeploy, creds.Cloud, target.Product, target.ResourceID, newCertFingerprint)
	if ok, err := simErr(s.outcome(&s.deploy)); !ok {
		return DeployResult{}, fmt.Errorf("simulated channel deploy: %w", err)
	}
	return DeployResult{NewCloudCertID: s.nextCloudCertID()}, nil
}

// Rollback 模拟回滚：成功恢复传入 oldRef；失败路径按行为模式映射
// ErrCode（限流 → CLOUD_API_RATELIMITED）。
func (s *SimulatedChannel) Rollback(ctx context.Context, creds Credential, target DeployTarget, oldRef domain.CertReference) (RollbackResult, error) {
	defer creds.Zeroize()
	s.record(simMethodRollback, creds.Cloud, target.Product, target.ResourceID, "")
	if ok, err := simErr(s.outcome(&s.rollback)); !ok {
		return RollbackResult{
			Success: false,
			ErrCode: rollbackErrCode(err),
			Reason:  err.Error(),
		}, fmt.Errorf("simulated channel rollback: %w", err)
	}
	return RollbackResult{Success: true, RestoredRef: oldRef, OrphanCleaned: []string{}}, nil
}

// 接口合规性编译期断言（接口演进防护）。
var (
	_ ExecutionChannel = (*CloudAPIChannel)(nil)
	_ ExecutionChannel = (*SimulatedChannel)(nil)
)
