// K8sAPIChannel：ExecutionChannel 的 K8s 实现（任务 5.6，tech-design
// Interface 1 + "K8s 管理权判定与变更后复检"）：
//   - Discover：固定枚举（ALBConfig/Ingress/Gateway/HTTPRoute，3.4 常量表共享）
//   - enabled=true CrdRegistration 遍历，按 certFieldPath 读证书引用字段产
//     CertReference（含 clusterId/namespace/kind）；未登记/enabled=false CRD
//     盲区声明（CRDBlindSpots）；
//   - Deploy 前/生成期三信号管理权探测（GitOps label / ownerReferences /
//     cert-manager annotation）→ AutoChangeable=false + Reason（经 5.2
//     ManagementProbe 端口注入清单生成）；
//   - Deploy 仅 JSON Patch 证书引用字段（Hard Rule：不得整对象覆盖）、执行前
//     读 oldCloudCertId 保留；
//   - RecheckCRDField：patch 后单轮复检（延迟调度归 5.9/7.1，本文件实现函数
//     本体；Hard Rule：复检次数固定 1，失败转人工）。
//
// 凭证说明：集群连接经 CRDClientProvider（生产实现 K8sFactoryClients → 3.4
// dynamic client 工厂，按 clusterName 解密 kubeconfig，明文仅内存用后清零）；
// creds 参数仅为接口对齐（kubeconfig 形态校验 + 用后 Zeroize），不承载连接
// 材料，永不进日志/错误文案。
package deployer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// K8s 通道哨兵错误（静态文案 + 安全参数，不含 kubeconfig/私钥片段）。
var (
	// ErrK8sTargetManaged 三信号管理权探测命中——目标由 GitOps/控制器/cert-manager
	// 管理，首期不提供越权自动变更（PRD In Scope），引导走其自身管理链路。
	ErrK8sTargetManaged = errors.New("deployer: k8s target managed by external controller")
	// ErrCloudCertIDUnresolved 新证书指纹无法解析为唯一云证书 ID（未上传任何
	// 云证书库 / 多云多账号互异映射无法消歧）——不猜测写入，防错误证书播撒。
	ErrCloudCertIDUnresolved = errors.New("deployer: new cert fingerprint has no unique cloud cert id")
)

// ---------------------------------------------------------------------
// 端口：单集群 CRD 客户端（3.4 k8s.Client 天然满足；测试注入 fake dynamic client）
// ---------------------------------------------------------------------

// CRDClient 单集群 CRD 通用操作端口（*k8s.Client 满足；连接类错误经
// domain.ErrK8sUnreachable 包装——K8S_UNREACHABLE 语义）。
type CRDClient interface {
	// List 列出 CRD 实例；namespace 为空表示全命名空间。
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error)
	// Get 读取单个 CRD 实例。
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
	// Patch 按补丁类型 patch 指定实例，返回 patch 后对象。
	Patch(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, patchType types.PatchType, data []byte) (*unstructured.Unstructured, error)
}

// CRDClientProvider 按集群名解析 CRD 客户端（kubeconfig 解密构建归 provider
// 实现，通道层不接触明文）。
type CRDClientProvider interface {
	Client(ctx context.Context, clusterName string) (CRDClient, error)
}

// K8sFactoryClients 生产 CRDClientProvider：经 3.4 dynamic client 工厂按
// clusterName 读取密文凭证、内存解密构建带缓存客户端（硬约束见 k8s 包：
// 明文仅内存、用后 Zeroize、禁入日志）。
type K8sFactoryClients struct {
	Factory *k8s.Factory
}

// Client 委托工厂获取集群客户端；未登记集群（mongo.ErrNoDocuments）与
// 集群不可达（ErrK8sUnreachable）原样透传。
func (p K8sFactoryClients) Client(ctx context.Context, clusterName string) (CRDClient, error) {
	return p.Factory.Client(ctx, clusterName)
}

// ---------------------------------------------------------------------
// 三信号管理权探测配置
// ---------------------------------------------------------------------

// ManagementSignalConfig 三信号管理权探测配置（键/前缀清单经应用 config
// 配置，tech-design"K8s 管理权判定"规则集）。
type ManagementSignalConfig struct {
	GitOpsLabelKeys     []string // GitOps 管理 label 精确键；默认 argocd.argoproj.io/instance、fluxcd.io/sync
	GitOpsLabelPrefixes []string // GitOps 管理 label 前缀；默认 argocd.argoproj.io/、fluxcd.io/
	ManagedAnnotations  []string // 证书自动管理 annotation 键；默认 cert-manager.io/issuer、cert-manager.io/cluster-issuer
}

// DefaultManagementSignalConfig 默认三信号键集（tech-design 判定规则集）。
func DefaultManagementSignalConfig() ManagementSignalConfig {
	return ManagementSignalConfig{
		GitOpsLabelKeys:     []string{"argocd.argoproj.io/instance", "fluxcd.io/sync"},
		GitOpsLabelPrefixes: []string{"argocd.argoproj.io/", "fluxcd.io/"},
		ManagedAnnotations:  []string{"cert-manager.io/issuer", "cert-manager.io/cluster-issuer"},
	}
}

// ---------------------------------------------------------------------
// K8sAPIChannel
// ---------------------------------------------------------------------

// K8sAPIChannel K8s API 执行通道（CRD patch + 管理权探测 + 复检）。
// 业务级状态机判断（success/failed 收敛、告警发送）归 5.7~5.9（Hard Rule）。
type K8sAPIChannel struct {
	clients  CRDClientProvider
	regs     domain.CrdRegistrationRepository
	mappings domain.CloudCertMappingRepository
	signals  ManagementSignalConfig
}

// NewK8sAPIChannel 创建 K8s 通道。signals 零值时套用默认三信号键集（切片
// 拷贝防调用方变更）；clients/regs/mappings 任一缺失时对应方法显式失败
// （fail-fast，防装配残缺静默误用）。
func NewK8sAPIChannel(
	clients CRDClientProvider,
	regs domain.CrdRegistrationRepository,
	mappings domain.CloudCertMappingRepository,
	signals ManagementSignalConfig,
) *K8sAPIChannel {
	if len(signals.GitOpsLabelKeys) == 0 && len(signals.GitOpsLabelPrefixes) == 0 && len(signals.ManagedAnnotations) == 0 {
		signals = DefaultManagementSignalConfig()
	}
	signals.GitOpsLabelKeys = append([]string(nil), signals.GitOpsLabelKeys...)
	signals.GitOpsLabelPrefixes = append([]string(nil), signals.GitOpsLabelPrefixes...)
	signals.ManagedAnnotations = append([]string(nil), signals.ManagedAnnotations...)
	return &K8sAPIChannel{clients: clients, regs: regs, mappings: mappings, signals: signals}
}

// Type 通道类型恒为 k8s_api。
func (c *K8sAPIChannel) Type() ChannelType { return ChannelTypeK8sAPI }

// 接口合规性编译期断言：ExecutionChannel（tech-design Interface 1）与 5.2
// service.ManagementProbe（清单生成期 AutoChangeable 回填端口；签名镜像为
// 本地契约接口防漂移，避免 service→deployer→service 环形导入）。
var _ ExecutionChannel = (*K8sAPIChannel)(nil)

type managementProbeContract interface {
	Probe(ctx context.Context, ref domain.ResourceRef) (manageable bool, reason string, err error)
}

var _ managementProbeContract = (*K8sAPIChannel)(nil)

// requireAssembled 装配完整性 fail-fast。
func (c *K8sAPIChannel) requireAssembled() error {
	if c.clients == nil {
		return errors.New("k8s channel: assembled without crd client provider")
	}
	if c.regs == nil {
		return errors.New("k8s channel: assembled without crd registration repository")
	}
	if c.mappings == nil {
		return errors.New("k8s channel: assembled without cloud cert mapping repository")
	}
	return nil
}

// clusterClient 取（并缓存）集群客户端。
func (c *K8sAPIChannel) clusterClient(ctx context.Context, cache map[string]CRDClient, cluster string) (CRDClient, error) {
	if client, ok := cache[cluster]; ok {
		return client, nil
	}
	client, err := c.clients.Client(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("k8s channel: resolve client for cluster %q: %w", cluster, err)
	}
	cache[cluster] = client
	return client, nil
}

// ---------------------------------------------------------------------
// Discover：固定枚举 + enabled 登记遍历
// ---------------------------------------------------------------------

// crdScanTarget 单 kind 发现/定位单元。
type crdScanTarget struct {
	kind          string
	certFieldPath string
	gvrs          []schema.GroupVersionResource // 内置=单候选精确；自定义=version 候选
}

// Discover 只读引用发现：按固定枚举（ALBConfig/Ingress/Gateway/HTTPRoute，
// k8s.BuiltinRegistrations 常量共享）+ enabled=true 自定义登记遍历，按
// certFieldPath 读取证书引用字段，每命中叶子一引用（多证书监听/SNI 展开）。
// scope.ClusterIDs 非空时固定枚举对范围集群全遍历；为空时遍历 enabled 登记
// 涉及的全部集群。指纹解析与 3.5 扫描同口径：映射通配反查 → 确定性占位指纹。
// 未登记 CRD 属全局盲区（通道不可枚举，视图层 referenceStatus=blind_spot 口径
// 声明）；enabled=false 登记的已知盲区经 CRDBlindSpots 显式声明。
func (c *K8sAPIChannel) Discover(ctx context.Context, creds Credential, scope DiscoverScope) ([]domain.CertReference, error) {
	defer creds.Zeroize()
	if err := validateKubeconfigCreds(creds); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if err := c.requireAssembled(); err != nil {
		return nil, err
	}

	enabled, err := c.regs.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("k8s channel: list enabled crd registrations: %w", err)
	}
	byCluster := make(map[string][]crdScanTarget)
	for _, reg := range enabled {
		if k8s.IsBuiltinRegistration(reg.APIGroup, reg.Kind) {
			continue // 内置项由固定枚举遍历（登记表播种项不重复扫）
		}
		byCluster[reg.ClusterID] = append(byCluster[reg.ClusterID], crdScanTarget{
			kind:          reg.Kind,
			certFieldPath: reg.CertFieldPath,
			gvrs:          candidateGVRs(reg.APIGroup, reg.Kind),
		})
	}
	clusters := append([]string(nil), scope.ClusterIDs...)
	if len(clusters) == 0 {
		for cluster := range byCluster {
			clusters = append(clusters, cluster)
		}
	}
	sort.Strings(clusters)
	// 固定枚举四类对全部范围集群遍历
	for _, cluster := range clusters {
		for _, b := range k8s.BuiltinRegistrations {
			byCluster[cluster] = append(byCluster[cluster], crdScanTarget{
				kind:          b.Kind,
				certFieldPath: b.CertFieldPath,
				gvrs:          []schema.GroupVersionResource{b.GVR()},
			})
		}
	}

	out := []domain.CertReference{}
	fpCache := make(map[string]string)
	clientCache := make(map[string]CRDClient)
	for _, cluster := range clusters {
		for _, target := range byCluster[cluster] {
			client, err := c.clusterClient(ctx, clientCache, cluster)
			if err != nil {
				return nil, err
			}
			list, err := listCRDObjects(ctx, cluster, target, client)
			if err != nil {
				return nil, err
			}
			for i := range list.Items {
				obj := &list.Items[i]
				if obj.GetName() == "" {
					continue
				}
				for _, leaf := range certFieldLeaves(obj.Object, target.certFieldPath) {
					out = append(out, domain.CertReference{
						CertFingerprint:       c.k8sFingerprint(ctx, cluster, leaf.value, fpCache),
						Product:               domain.ProductCRD,
						ClusterID:             cluster,
						Namespace:             obj.GetNamespace(),
						Kind:                  target.kind,
						ResourceID:            obj.GetName(),
						ReferencedCloudCertID: leaf.value,
						SnapshotID:            scope.SnapshotID,
					})
				}
			}
		}
	}
	return out, nil
}

// CRDBlindSpot 已知盲区声明（enabled=false 登记项——该 CRD 回归盲区，引用
// 不纳入扫描范围；tech-design"自定义 CRD 登记管理"扫描联动）。
type CRDBlindSpot struct {
	ClusterID string
	APIGroup  string
	Kind      string
	Reason    string
}

// CRDBlindSpots 声明范围内集群的已知盲区（全部登记中 enabled=false 项）。
// 未登记 CRD 不可枚举，属全局盲区，由视图层按 referenceStatus=blind_spot
// 口径显式声明。
func (c *K8sAPIChannel) CRDBlindSpots(ctx context.Context, clusterIDs []string) ([]CRDBlindSpot, error) {
	if err := c.requireAssembled(); err != nil {
		return nil, err
	}
	regs, err := c.regs.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("k8s channel: list crd registrations: %w", err)
	}
	filter := make(map[string]struct{}, len(clusterIDs))
	for _, id := range clusterIDs {
		filter[id] = struct{}{}
	}
	out := []CRDBlindSpot{}
	for _, reg := range regs {
		if reg.Enabled {
			continue
		}
		if len(filter) > 0 {
			if _, ok := filter[reg.ClusterID]; !ok {
				continue
			}
		}
		out = append(out, CRDBlindSpot{
			ClusterID: reg.ClusterID,
			APIGroup:  reg.APIGroup,
			Kind:      reg.Kind,
			Reason:    "登记停用（enabled=false）：该 CRD 回归扫描盲区，引用不纳入发现范围",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClusterID != out[j].ClusterID {
			return out[i].ClusterID < out[j].ClusterID
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

// ---------------------------------------------------------------------
// Probe：三信号管理权探测（5.2 清单生成 AutoChangeable 回填 + Deploy 前复核）
// ---------------------------------------------------------------------

// Probe 探测单个 K8s 资源是否可自动变更（tech-design 判定规则集，任一信号
// 命中即不可）：① GitOps 管理 label（精确键或前缀）；② ownerReferences
// 非空；③ 证书自动管理 annotation。manageable=false 时 reason 记信号类型+
// 具体键；err 非 nil（集群不可达/未登记 kind 等）由调用方按不可执行项分区
// 处理（单点不可用不阻塞清单生成，见 5.2 assessChangeable）。
func (c *K8sAPIChannel) Probe(ctx context.Context, ref domain.ResourceRef) (bool, string, error) {
	if err := validateK8sRef(ref); err != nil {
		return false, "", err
	}
	if err := c.requireAssembled(); err != nil {
		return false, "", err
	}
	obj, _, _, err := c.getCRDObject(ctx, ref)
	if err != nil {
		return false, "", err
	}
	if reason, hit := managementSignal(obj, c.signals); hit {
		return false, reason, nil
	}
	return true, "", nil
}

// managementSignal 三信号检测（次序：GitOps label → ownerReferences → 管理
// annotation，首个命中即返回；键序排序保证 Reason 确定性）。
func managementSignal(obj *unstructured.Unstructured, cfg ManagementSignalConfig) (string, bool) {
	for _, key := range sortedKeys(obj.GetLabels()) {
		if containsString(cfg.GitOpsLabelKeys, key) || hasPrefixIn(cfg.GitOpsLabelPrefixes, key) {
			return "gitops_label:" + key, true
		}
	}
	if owners := obj.GetOwnerReferences(); len(owners) > 0 {
		first := owners[0]
		return fmt.Sprintf("owner_references:%d first=%s/%s", len(owners), first.Kind, first.Name), true
	}
	for _, key := range sortedKeys(obj.GetAnnotations()) {
		if containsString(cfg.ManagedAnnotations, key) {
			return "managed_annotation:" + key, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------
// Deploy：仅 patch 证书引用字段
// ---------------------------------------------------------------------

// Deploy 部署 newCertFingerprint 证书至 target CRD（action=patch_crd）：
//  1. 新证书 ID 解析：映射表 fingerprint→cloudCertId（active 且互异唯一；
//     0 条或多条互异显式失败——接口签名固定只传指纹，值解析归通道）；
//  2. 读取对象 → 执行前保留 oldCloudCertId（回滚依据）+ Deploy 前三信号复核
//     （命中拒绝 patch，ErrK8sTargetManaged——首期不提供越权自动变更）；
//  3. JSON Patch replace 仅指向 certFieldPath 各叶子（Hard Rule：不得整对象
//     覆盖、不动其他字段），多证书监听单次动作全量覆盖。
//
// K8S_UNREACHABLE（连接类错误）以 domain.ErrK8sUnreachable 包装返回，由
// 5.7 执行引擎落 ChangeItem.status=failed + error=K8S_UNREACHABLE。
// 返回 DeployResult{NewCloudCertID=空（K8s 通道无两段式产物，tech-design）、
// OldCloudCertID、RecheckPassed=false（待复检——crd-recheck 经
// RecheckCRDField 复检后回填终值）}。
func (c *K8sAPIChannel) Deploy(ctx context.Context, creds Credential, target DeployTarget, newCertFingerprint string) (DeployResult, error) {
	defer creds.Zeroize()
	if err := validateKubeconfigCreds(creds); err != nil {
		return DeployResult{}, err
	}
	if err := target.Validate(); err != nil {
		return DeployResult{}, err
	}
	if newCertFingerprint == "" {
		return DeployResult{}, fmt.Errorf("%w: newCertFingerprint is empty", ErrInvalidTarget)
	}
	if err := c.requireAssembled(); err != nil {
		return DeployResult{}, err
	}

	newCertID, err := c.resolveNewCertID(ctx, newCertFingerprint)
	if err != nil {
		return DeployResult{}, err
	}
	ref := target.ToResourceRef()
	obj, client, crdT, err := c.getCRDObject(ctx, ref)
	if err != nil {
		return DeployResult{}, err
	}
	if reason, hit := managementSignal(obj, c.signals); hit {
		return DeployResult{}, fmt.Errorf("%w: %s/%s/%s %s (change via its own management pipeline)",
			ErrK8sTargetManaged, ref.ClusterID, ref.Namespace, ref.ResourceID, reason)
	}
	leaves := certFieldLeaves(obj.Object, crdT.certFieldPath)
	if len(leaves) == 0 {
		return DeployResult{}, fmt.Errorf("k8s channel: cert field path %q not found on %s/%s/%s",
			crdT.certFieldPath, ref.ClusterID, ref.Namespace, ref.ResourceID)
	}
	patch, err := jsonPatchReplace(leaves, newCertID)
	if err != nil {
		return DeployResult{}, fmt.Errorf("k8s channel: build json patch: %w", err)
	}
	if _, err := client.Patch(ctx, crdT.gvr, ref.Namespace, ref.ResourceID, types.JSONPatchType, patch); err != nil {
		return DeployResult{OldCloudCertID: leaves[0].value},
			fmt.Errorf("k8s channel: patch %s/%s/%s: %w", ref.ClusterID, ref.Namespace, ref.ResourceID, err)
	}
	return DeployResult{OldCloudCertID: leaves[0].value}, nil // RecheckPassed=false=待复检
}

// Rollback 将 target 证书引用字段恢复为 oldRef.ReferencedCloudCertID
// （patch 回写，同样仅作用于证书引用字段）。回滚目标有效性三判定归 5.8
// 前置校验；失败映射 ErrCode=K8S_UNREACHABLE（不可达）。
func (c *K8sAPIChannel) Rollback(ctx context.Context, creds Credential, target DeployTarget, oldRef domain.CertReference) (RollbackResult, error) {
	defer creds.Zeroize()
	if err := validateKubeconfigCreds(creds); err != nil {
		return RollbackResult{}, err
	}
	if err := target.Validate(); err != nil {
		return RollbackResult{}, err
	}
	if oldRef.ReferencedCloudCertID == "" {
		return RollbackResult{}, fmt.Errorf("%w: rollback requires oldRef.referencedCloudCertId", ErrInvalidTarget)
	}
	if err := c.requireAssembled(); err != nil {
		return RollbackResult{}, err
	}

	ref := target.ToResourceRef()
	obj, client, crdT, err := c.getCRDObject(ctx, ref)
	if err != nil {
		return RollbackResult{ErrCode: k8sErrCode(err), Reason: err.Error()}, err
	}
	leaves := certFieldLeaves(obj.Object, crdT.certFieldPath)
	if len(leaves) == 0 {
		err := fmt.Errorf("k8s channel: cert field path %q not found on %s/%s/%s",
			crdT.certFieldPath, ref.ClusterID, ref.Namespace, ref.ResourceID)
		return RollbackResult{Reason: err.Error()}, err
	}
	patch, err := jsonPatchReplace(leaves, oldRef.ReferencedCloudCertID)
	if err != nil {
		return RollbackResult{Reason: err.Error()}, fmt.Errorf("k8s channel: build json patch: %w", err)
	}
	if _, err := client.Patch(ctx, crdT.gvr, ref.Namespace, ref.ResourceID, types.JSONPatchType, patch); err != nil {
		err = fmt.Errorf("k8s channel: patch %s/%s/%s: %w", ref.ClusterID, ref.Namespace, ref.ResourceID, err)
		return RollbackResult{ErrCode: k8sErrCode(err), Reason: err.Error()}, err
	}
	return RollbackResult{
		Success:       true,
		RestoredRef:   oldRef,
		OrphanCleaned: []string{},
	}, nil
}

// ---------------------------------------------------------------------
// RecheckCRDField：单轮复检（次数固定 1，失败转人工）
// ---------------------------------------------------------------------

// CRDRecheckItem 单项复检输入（5.9 crd-recheck 消费者构造；patch 完成入队、
// 延迟 recheckDelayMinutes 消费的调度归 5.9/7.1，本类型仅承载复检所需载荷）。
type CRDRecheckItem struct {
	Ref       domain.ResourceRef // 目标资源（ChangeItem.resourceRef 重构）
	OrderID   string             // 关联变更单（失败 Reason 附告警上下文）
	ItemID    string             // 关联变更项（同上）
	NewCertID string             // patch 写入的新证书 ID（期望终态）
	OldCertID string             // patch 前旧值（回写"旧值"vs"其他值"区分）
}

// RecheckResult 单轮复检结果。RecheckPassed=true → 项标 success；false →
// failed + 告警（TLS 差异通道语义，Reason 附 orderId/itemId 由 5.9 发送）。
// 复检次数固定 1，失败不做二次自动复检（Hard Rule：转人工决策）。
type RecheckResult struct {
	RecheckPassed bool
	CurrentCertID string // 复检读到的首个当前值
	Reason        string // 未通过原因（reconcile 回写旧值/其他值/字段缺失）
}

// ResolveCloudCertID 新证书指纹 → 唯一 active 云证书 ID（任务 5.9 crd-recheck
// 消费者构造 CRDRecheckItem.NewCertID 复用：patch 写入值与复检期望终态走同一
// 解析口径 resolveNewCertID——K8s DeployResult.NewCloudCertID 恒空，期望值须
// 由消费侧重解析）。
func (c *K8sAPIChannel) ResolveCloudCertID(ctx context.Context, fingerprint string) (string, error) {
	if fingerprint == "" {
		return "", fmt.Errorf("%w: fingerprint is empty", ErrInvalidTarget)
	}
	if err := c.requireAssembled(); err != nil {
		return "", err
	}
	return c.resolveNewCertID(ctx, fingerprint)
}

// RecheckCRDField patch 后单轮复检：读取 CRD 证书引用字段——仍为新证书 ID
// → RecheckPassed=true；被 reconcile 回写为旧值/其他值（或字段缺失）→
// RecheckPassed=false + Reason（附 orderId/itemId）。读取失败（集群不可达等）
// 返回 err（调用方按 failed + 告警承接，不自动重试）。
func (c *K8sAPIChannel) RecheckCRDField(ctx context.Context, item CRDRecheckItem) (RecheckResult, error) {
	if err := validateK8sRef(item.Ref); err != nil {
		return RecheckResult{}, err
	}
	if item.NewCertID == "" {
		return RecheckResult{}, fmt.Errorf("%w: recheck requires newCertId", ErrInvalidTarget)
	}
	if err := c.requireAssembled(); err != nil {
		return RecheckResult{}, err
	}

	obj, _, crdT, err := c.getCRDObject(ctx, item.Ref)
	if err != nil {
		return RecheckResult{}, err
	}
	leaves := certFieldLeaves(obj.Object, crdT.certFieldPath)
	if len(leaves) == 0 {
		return RecheckResult{
			RecheckPassed: false,
			Reason:        fmt.Sprintf("复检失败：证书引用字段 %s 缺失（orderId=%s itemId=%s）", crdT.certFieldPath, item.OrderID, item.ItemID),
		}, nil
	}
	current := leaves[0].value
	for _, leaf := range leaves {
		if leaf.value != item.NewCertID {
			return RecheckResult{
				RecheckPassed: false,
				CurrentCertID: current,
				Reason:        recheckFailReason(item, leaf.value),
			}, nil
		}
	}
	return RecheckResult{RecheckPassed: true, CurrentCertID: current}, nil
}

// recheckFailReason 复检失败原因文案（区分回写旧值/其他值，附告警上下文）。
func recheckFailReason(item CRDRecheckItem, actual string) string {
	kind := "其他值"
	if actual == item.OldCertID {
		kind = "旧值"
	}
	return fmt.Sprintf("reconcile 回写%s：期望 %s 实际 %s（orderId=%s itemId=%s，转人工：登记接管/调整 GitOps 同步后再发起）",
		kind, item.NewCertID, actual, item.OrderID, item.ItemID)
}

// ---------------------------------------------------------------------
// 目标解析：kind → GVR + certFieldPath（内置精确 / 登记候选探测）
// ---------------------------------------------------------------------

// resolveTarget Get 命中的目标（gvr + certFieldPath）。
type resolveTarget struct {
	gvr           schema.GroupVersionResource
	certFieldPath string
}

// getCRDObject 读取目标对象并返回解析出的集群客户端与命中的 GVR、
// certFieldPath：
//   - 内置固定枚举 kind 精确命中（k8s.BuiltinRegistrations，含字段路径）；
//   - 自定义 kind 按 clusterId+kind 匹配登记（enabled 与否均可——执行期清单
//     快照固定不变，登记停用不阻断已在途变更项，5.7 Hard Rule），version
//     候选探测（NoMatch/NotFound 未命中继续，其余错误透传）。
func (c *K8sAPIChannel) getCRDObject(ctx context.Context, ref domain.ResourceRef) (*unstructured.Unstructured, CRDClient, resolveTarget, error) {
	client, err := c.clients.Client(ctx, ref.ClusterID)
	if err != nil {
		return nil, nil, resolveTarget{}, fmt.Errorf("k8s channel: resolve client for cluster %q: %w", ref.ClusterID, err)
	}
	// 内置固定枚举：精确 GVR + 常量表字段路径
	for _, b := range k8s.BuiltinRegistrations {
		if b.Kind != ref.Kind {
			continue
		}
		obj, err := client.Get(ctx, b.GVR(), ref.Namespace, ref.ResourceID)
		if err != nil {
			return nil, nil, resolveTarget{}, fmt.Errorf("k8s channel: get %s/%s/%s: %w",
				ref.ClusterID, ref.Namespace, ref.ResourceID, err)
		}
		return obj, client, resolveTarget{gvr: b.GVR(), certFieldPath: b.CertFieldPath}, nil
	}
	// 自定义登记：候选 version 探测（首个 Get 命中即用）
	regs, err := c.regs.List(ctx)
	if err != nil {
		return nil, nil, resolveTarget{}, fmt.Errorf("k8s channel: list crd registrations: %w", err)
	}
	matched := 0
	for _, reg := range regs {
		if reg.ClusterID != ref.ClusterID || reg.Kind != ref.Kind {
			continue
		}
		matched++
		for _, gvr := range candidateGVRs(reg.APIGroup, ref.Kind) {
			obj, err := client.Get(ctx, gvr, ref.Namespace, ref.ResourceID)
			if err != nil {
				if isNoMatchOrNotFound(err) {
					continue // 候选 version 未命中
				}
				return nil, nil, resolveTarget{}, fmt.Errorf("k8s channel: get %s/%s/%s: %w",
					ref.ClusterID, ref.Namespace, ref.ResourceID, err)
			}
			return obj, client, resolveTarget{gvr: gvr, certFieldPath: reg.CertFieldPath}, nil
		}
	}
	return nil, nil, resolveTarget{}, fmt.Errorf("%w: kind %q not resolvable in cluster %q (matched %d registration(s))",
		ErrInvalidTarget, ref.Kind, ref.ClusterID, matched)
}

// listCRDObjects 按扫描目标列实例（候选 GVR 探测，语义同 3.5 扫描网关：
// NoMatch/NotFound 未命中继续、其余错误透传、全部未命中按通道失败报错）。
func listCRDObjects(ctx context.Context, cluster string, target crdScanTarget, client CRDClient) (*unstructured.UnstructuredList, error) {
	for _, gvr := range target.gvrs {
		list, err := client.List(ctx, gvr, "")
		if err != nil {
			if isNoMatchOrNotFound(err) {
				continue // 候选 version 未命中，尝试下一个
			}
			return nil, fmt.Errorf("k8s channel: list %s %s in cluster %q: %w", target.kind, gvr.String(), cluster, err)
		}
		return list, nil
	}
	return nil, fmt.Errorf("k8s channel: crd kind %q not found in cluster %q (no gvr candidate matched)", target.kind, cluster)
}

// candidateGVRs 自定义登记 → GVR 候选清单：plural=lower(kind)+"s" × 常见
// version 候选（与 3.5 扫描网关同启发式，首期 PoC 5.12 校准）。
func candidateGVRs(apiGroup, kind string) []schema.GroupVersionResource {
	resource := strings.ToLower(kind) + "s"
	versions := []string{"v1", "v1beta1", "v1alpha1"}
	out := make([]schema.GroupVersionResource, 0, len(versions))
	for _, v := range versions {
		out = append(out, schema.GroupVersionResource{Group: apiGroup, Version: v, Resource: resource})
	}
	return out
}

// isNoMatchOrNotFound GVR 候选探测"未命中"判定（其余错误透传）。
func isNoMatchOrNotFound(err error) bool {
	return k8smeta.IsNoMatchError(err) || apierrors.IsNotFound(err)
}

// ---------------------------------------------------------------------
// 指纹解析（3.5 同口径：映射通配反查 → 确定性占位指纹）
// ---------------------------------------------------------------------

// k8sFingerprint K8s 引用指纹解析：CloudCertMapping 通配反查（cloud/accountKey
// 未知）；无命中或仓储异常不中断发现 → 占位指纹 sha256("certscan-unresolved:
// k8s|cluster|certID")（满足 ^[0-9a-f]{64}$ 且永不与真实证书指纹冲突；映射
// 建立后下轮扫描恢复精确关联）。
func (c *K8sAPIChannel) k8sFingerprint(ctx context.Context, cluster, certID string, cache map[string]string) string {
	if fp, ok := cache[certID]; ok {
		return fp
	}
	fp := unresolvedK8sFingerprint(cluster, certID)
	if m, err := c.mappings.FindByCloudCertID(ctx, "", "", certID); err == nil {
		fp = m.CertFingerprint
	}
	cache[certID] = fp
	return fp
}

// unresolvedK8sFingerprint 确定性占位指纹（与 3.5 resolveUncached 同口径）。
func unresolvedK8sFingerprint(cluster, certID string) string {
	cacheKey := strings.Join([]string{"k8s", cluster, certID}, "|")
	sum := sha256.Sum256([]byte("certscan-unresolved:" + cacheKey))
	return hex.EncodeToString(sum[:])
}

// resolveNewCertID 新证书指纹 → 待写入 CRD 引用字段的云证书 ID。规则：映射表
// ListByFingerprint 取 status=active 且去重后唯一；0 条（新证书尚未上传任何
// 云证书库）或多条互异（多云多账号上传，K8s 引用无云上下文无法消歧）均显式
// 失败——不猜测写入，防错误证书播撒。
func (c *K8sAPIChannel) resolveNewCertID(ctx context.Context, fingerprint string) (string, error) {
	mappings, err := c.mappings.ListByFingerprint(ctx, fingerprint)
	if err != nil {
		return "", fmt.Errorf("k8s channel: list cloud cert mappings: %w", err)
	}
	seen := make(map[string]struct{}, len(mappings))
	distinct := make([]string, 0, len(mappings))
	for _, m := range mappings {
		if m.Status != domain.MappingStatusActive {
			continue
		}
		if _, dup := seen[m.CloudCertID]; dup {
			continue
		}
		seen[m.CloudCertID] = struct{}{}
		distinct = append(distinct, m.CloudCertID)
	}
	switch len(distinct) {
	case 1:
		return distinct[0], nil
	case 0:
		return "", fmt.Errorf("%w: fingerprint=%s has no active cloud cert id mapping (upload to a cloud cert store first)", ErrCloudCertIDUnresolved, fingerprint)
	default:
		return "", fmt.Errorf("%w: fingerprint=%s has %d distinct active cloud cert ids; cannot disambiguate for k8s patch", ErrCloudCertIDUnresolved, fingerprint, len(distinct))
	}
}

// ---------------------------------------------------------------------
// certFieldPath 叶子定位与 JSON Patch 构造（Hard Rule：仅证书引用字段）
// ---------------------------------------------------------------------

// certFieldLeaf certFieldPath 叶子定位：RFC 6902 JSON Pointer + 归一化值。
type certFieldLeaf struct {
	pointer string
	value   string
}

// certFieldLeaves 按 certFieldPath 从对象内容定位全部证书引用字段叶子
// （语法见 k8s.ValidateCertFieldPath："." 分段、段名可选 "[]" 数组下钻）。
// 抽取语义与 3.5 扫描 extractCertFieldValues 一致：每命中叶子一值、空串跳过、
// 数字字面量最小精度转字符串、字符串数组逐项展开；额外产出各叶子 JSON Pointer
// （Deploy/Rollback 的 JSON Patch replace 精确指向证书引用字段——Hard Rule）。
func certFieldLeaves(obj map[string]interface{}, path string) []certFieldLeaf {
	if obj == nil || path == "" {
		return nil
	}
	type walkNode struct {
		prefix string
		val    interface{}
	}
	current := []walkNode{{"", obj}}
	for _, rawSeg := range strings.Split(path, ".") {
		seg, isArray := strings.CutSuffix(rawSeg, "[]")
		var next []walkNode
		for _, n := range current {
			m, ok := n.val.(map[string]interface{})
			if !ok {
				continue
			}
			v, ok := m[seg]
			if !ok {
				continue
			}
			child := n.prefix + "/" + escapeJSONPointer(seg)
			if isArray {
				if arr, ok := v.([]interface{}); ok {
					for i, item := range arr {
						next = append(next, walkNode{fmt.Sprintf("%s/%d", child, i), item})
					}
				} else {
					next = append(next, walkNode{child, v}) // 单值容错（部分 CRD 单元素免数组）
				}
			} else {
				next = append(next, walkNode{child, v})
			}
		}
		current = next
		if len(current) == 0 {
			return nil
		}
	}
	var out []certFieldLeaf
	for _, n := range current {
		expandLeaf(n.prefix, n.val, &out)
	}
	return out
}

// expandLeaf 叶子值归一化展开（string/数字/字符串数组；空串跳过）。
func expandLeaf(prefix string, v interface{}, out *[]certFieldLeaf) {
	switch t := v.(type) {
	case string:
		if t != "" {
			*out = append(*out, certFieldLeaf{prefix, t})
		}
	case []interface{}:
		for i, item := range t {
			expandLeaf(fmt.Sprintf("%s/%d", prefix, i), item, out)
		}
	case float64: // 数值证书 ID：最小精度表示（8089870.0 → "8089870"）
		*out = append(*out, certFieldLeaf{prefix, strconv.FormatFloat(t, 'f', -1, 64)})
	case int:
		*out = append(*out, certFieldLeaf{prefix, strconv.Itoa(t)})
	case int64:
		*out = append(*out, certFieldLeaf{prefix, strconv.FormatInt(t, 10)})
	}
}

// jsonPatchReplace 构造 JSON Patch（RFC 6902）：对每个证书引用叶子一条
// replace 操作——patch 仅作用于证书引用字段，不触碰其他字段（Hard Rule）。
func jsonPatchReplace(leaves []certFieldLeaf, newCertID string) ([]byte, error) {
	type patchOp struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value string `json:"value"`
	}
	ops := make([]patchOp, 0, len(leaves))
	for _, leaf := range leaves {
		ops = append(ops, patchOp{Op: "replace", Path: leaf.pointer, Value: newCertID})
	}
	return json.Marshal(ops)
}

// escapeJSONPointer JSON Pointer 转义（RFC 6902：~ → ~0、/ → ~1）。
func escapeJSONPointer(seg string) string {
	replaced := strings.ReplaceAll(seg, "~", "~0")
	return strings.ReplaceAll(replaced, "/", "~1")
}

// ---------------------------------------------------------------------
// 输入校验 / 小工具
// ---------------------------------------------------------------------

// validateKubeconfigCreds K8s 通道凭证形态校验（kubeconfig kind；载荷不承载
// 连接材料，连接经 CRDClientProvider 按 clusterName 解密获取）。
func validateKubeconfigCreds(creds Credential) error {
	if err := creds.Validate(); err != nil {
		return err
	}
	if creds.Kind != CredentialKindKubeconfig {
		return fmt.Errorf("%w: k8s channel requires kubeconfig credential, got %q", ErrInvalidCredential, creds.Kind)
	}
	return nil
}

// validateK8sRef k8s_api 目标定位分支校验（对齐 DeployTarget.Validate 的
// k8s_api 分支：clusterId+namespace+kind+resourceId）。
func validateK8sRef(ref domain.ResourceRef) error {
	if ref.ClusterID == "" || ref.Namespace == "" || ref.Kind == "" || ref.ResourceID == "" {
		return fmt.Errorf("%w: k8s_api requires clusterId, namespace, kind and resourceId", ErrInvalidTarget)
	}
	return nil
}

// k8sErrCode K8s 通道错误码映射（不可达 → K8S_UNREACHABLE；其余空串）。
func k8sErrCode(err error) string {
	if errors.Is(err, domain.ErrK8sUnreachable) {
		return ErrCodeK8sUnreachable
	}
	return ""
}

// sortedKeys map 键排序（Reason 确定性）。
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// containsString 见 cloud_api_channel.go（包内共享）。

// hasPrefixIn 前缀命中判定。
func hasPrefixIn(prefixes []string, key string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}
