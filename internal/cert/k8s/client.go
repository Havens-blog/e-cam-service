// Package k8s 提供 K8s 接入基座（任务 3.4）：基于 client-go 的 dynamic client
// 工厂（按 clusterName 解密 kubeconfig → 构建带缓存的通用 CRD 客户端）与
// 首期固定枚举（ALBConfig/Ingress/Gateway/HTTPRoute）的 GVR/证书字段路径常量表
// （供 5.6 K8sAPIChannel 复用）。
//
// 硬约束（tech-design Security / 任务 Hard Rules）：
//   - kubeconfig 明文仅在内存中解密使用，用后 domain.Zeroize 清零；
//   - 明文/密文片段禁入日志、错误 message、响应与审计——所有错误文案均为
//     静态描述 + 安全参数（集群名、GVR、操作名），绝不拼接 kubeconfig 内容。
package k8s

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// k8sRequestTimeout dynamic client 单请求超时；不可达端点快速失败，
// 供 ErrK8sUnreachable（K8S_UNREACHABLE → 503）语义判定。
const k8sRequestTimeout = 30 * time.Second

// ---------------------------------------------------------------------
// 内置默认登记固定枚举（GVR + 证书字段路径常量表）
// ---------------------------------------------------------------------

// BuiltinRegistration 内置默认登记项：首期固定枚举四类网关 CRD 的
// GVR 与证书引用字段路径。字段路径值为首期默认（5.12 PoC / 首批验证后锁定，
// 见 tech-design Open Questions），供 5.6 K8sAPIChannel 扫描范围复用：
// K8sAPIChannel 扫描范围 = 本固定枚举 + cert_crd_registrations enabled=true 项。
type BuiltinRegistration struct {
	APIGroup      string // 如 alb.alibabacloud.com；core 组资源为空串
	Version       string // API 版本，如 v1
	Kind          string // CRD kind（K8s 注册形态，如 AlbConfig）
	Resource      string // 复数资源名（GVR 的 resource 段，显式声明避免猜测复数规则）
	CertFieldPath string // 云托管证书引用字段路径（certFieldPath 语法）
}

// GVR 返回该登记项的 GroupVersionResource。
func (b BuiltinRegistration) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: b.APIGroup, Version: b.Version, Resource: b.Resource}
}

// BuiltinRegistrations 首期固定枚举四类（ALBConfig/Ingress/Gateway/HTTPRoute）。
// Ingress 引用 secret 名称（名称引用）；Gateway 为 Gateway API 标准 TLS 引用；
// HTTPRoute 本体无证书字段，默认登记其 parentRefs（定位挂载的 Gateway，
// 5.6 联动解析证书引用）。
var BuiltinRegistrations = []BuiltinRegistration{
	{
		APIGroup:      "alb.alibabacloud.com",
		Version:       "v1",
		Kind:          "AlbConfig",
		Resource:      "albconfigs",
		CertFieldPath: "spec.listeners[].certificates[].certificateId",
	},
	{
		APIGroup:      "networking.k8s.io",
		Version:       "v1",
		Kind:          "Ingress",
		Resource:      "ingresses",
		CertFieldPath: "spec.tls[].secretName",
	},
	{
		APIGroup:      "gateway.networking.k8s.io",
		Version:       "v1",
		Kind:          "Gateway",
		Resource:      "gateways",
		CertFieldPath: "spec.listeners[].tls.certificateRef.name",
	},
	{
		APIGroup:      "gateway.networking.k8s.io",
		Version:       "v1",
		Kind:          "HTTPRoute",
		Resource:      "httproutes",
		CertFieldPath: "spec.parentRefs[].name",
	},
}

// IsBuiltinRegistration 判定 apiGroup+kind 是否命中内置固定枚举
// （内置登记删除拦截与视图 Builtin 标记依据）。
func IsBuiltinRegistration(apiGroup, kind string) bool {
	for _, b := range BuiltinRegistrations {
		if b.APIGroup == apiGroup && b.Kind == kind {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------
// certFieldPath / kubeconfig 校验
// ---------------------------------------------------------------------

var (
	// ErrInvalidCertFieldPath certFieldPath 语法非法（登记校验期拒绝，400 语义）。
	ErrInvalidCertFieldPath = errors.New("cert: invalid cert field path")
	// ErrInvalidKubeconfig kubeconfig 非法（非可解析的 kubeconfig 文档，400 语义）。
	ErrInvalidKubeconfig = errors.New("cert: invalid kubeconfig")
)

// certFieldSegmentRe 单段语法：标识符（字母/数字/'-'/'_'，首尾为字母数字）
// 可选后缀 "[]"（数组���钻，仅允许一次且紧跟段名），如 certificates[] / certificateId。
var certFieldSegmentRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_-]*[A-Za-z0-9])?(\[\])?$`)

// ValidateCertFieldPath 校验证书引用字段路径语法（任务 3.4 AC：非法路径校验期拒绝，
// 含可读错误���息）。语法形如 spec.certificates[].certificateId：
//   - 以 "." 分段，禁止空段（首尾 "."、连续 ".."）；
//   - 每段为合法标识符，可选 "[]" 数组下钻后缀；不支持 "[0]" 数字下标、
//     不允许 "[]" 单独成段或段中多次出现；
//   - 至少一段。
func ValidateCertFieldPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: path is empty", ErrInvalidCertFieldPath)
	}
	for i, seg := range strings.Split(path, ".") {
		if seg == "" {
			return fmt.Errorf("%w: empty segment at position %d in %q", ErrInvalidCertFieldPath, i+1, path)
		}
		if !certFieldSegmentRe.MatchString(seg) {
			return fmt.Errorf("%w: invalid segment %q at position %d in %q (expect name or name[])", ErrInvalidCertFieldPath, seg, i+1, path)
		}
	}
	return nil
}

// ValidateKubeconfig 校验 kubeconfig 可解析（登记集群凭证时快速失败，
// 避免密文落库后才发现 K8S_UNREACHABLE）。底层 YAML 解析错误不透出
// （可能携带 kubeconfig 内容片段，Hard Rule 禁入日志/响应）。
func ValidateKubeconfig(kubeconfig []byte) error {
	if len(kubeconfig) == 0 {
		return fmt.Errorf("%w: kubeconfig is empty", ErrInvalidKubeconfig)
	}
	if _, err := clientcmd.Load(kubeconfig); err != nil {
		return fmt.Errorf("%w: not a valid kubeconfig document", ErrInvalidKubeconfig)
	}
	return nil
}

// ---------------------------------------------------------------------
// Client：CRD 通用 list/get/patch
// ---------------------------------------------------------------------

// Client 单集群 dynamic client 封装：对任意 GVR 的 CRD 提供通���
// list/get/patch 通路（5.6 K8sAPIChannel 引用扫描与 patch_crd 变更的底座）。
// 请求期连接失败（网络不可达/拒连/超时等 net.Error 族）统一包装
// domain.ErrK8sUnreachable（K8S_UNREACHABLE → 503）；API 语义错误
// （NotFound/Conflict 等非连接类）原样透传，供调用方分支处理。
type Client struct {
	cluster string
	dyn     dynamic.Interface
}

// ClusterName 返回客户端绑定的集群名（clusterName 唯一键）。
func (c *Client) ClusterName() string { return c.cluster }

// List 列出 CRD 实例；namespace 为空表示全命名空间（all namespaces）。
func (c *Client) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	var (
		list *unstructured.UnstructuredList
		err  error
	)
	if namespace == "" {
		list, err = c.dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
	} else {
		list, err = c.dyn.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, c.wrapConnErr("list", gvr, err)
	}
	return list, nil
}

// Get 读取单个 CRD 实例。
func (c *Client) Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	obj, err := c.dyn.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, c.wrapConnErr("get", gvr, err)
	}
	return obj, nil
}

// Patch 按补丁类型 patch 指定 CRD 实例（certFieldPath 指向的证书引用字段，
// 5.6 Deploy 仅 patch 证书引用字段；JSON Patch 的 path 由调用方按 certFieldPath
// 构造）。返回 patch 后的对象。
func (c *Client) Patch(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, patchType types.PatchType, data []byte) (*unstructured.Unstructured, error) {
	obj, err := c.dyn.Resource(gvr).Namespace(namespace).Patch(ctx, name, patchType, data, metav1.PatchOptions{})
	if err != nil {
		return nil, c.wrapConnErr("patch", gvr, err)
	}
	return obj, nil
}

// wrapConnErr 连接类错误 → ErrK8sUnreachable 包装（静态文案 + 集群名/GVR/操作名），
// 其余错误原样透传。
func (c *Client) wrapConnErr(op string, gvr schema.GroupVersionResource, err error) error {
	if isConnectionError(err) {
		return fmt.Errorf("%w: cluster %q %s %s: connection failed", domain.ErrK8sUnreachable, c.cluster, op, gvr.String())
	}
	return err
}

// isConnectionError 判定是否连接类错误：client-go 传输层失败（拒连/DNS/超时/
// 断连）以 net.Error 族（含 *url.Error、*net.OpError）呈现。
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// ---------------------------------------------------------------------
// Factory：按 clusterName 解密 kubeconfig → 构建带缓存的 Client
// ---------------------------------------------------------------------

// buildClientFn 从解密后的 kubeconfig 明文构建 dynamic client（测试注入点）。
type buildClientFn func(kubeconfig []byte) (dynamic.Interface, error)

// Factory dynamic client 工厂：按 clusterName 从凭证仓储读取密文 kubeconfig，
// 内存解密后构建 Client 并缓存复用（同集群多次获取返回同一实例）。
type Factory struct {
	creds  domain.K8sCredentialRepository
	crypto *domain.EnvelopeCrypto
	build  buildClientFn

	mu    sync.RWMutex
	cache map[string]*Client
}

// NewFactory 创建 dynamic client 工厂。crypto 为信封加密组件（1.1），
// 为 nil 时 Client 调用显式失败（fail-fast，防止绕过加密体系读取）。
func NewFactory(creds domain.K8sCredentialRepository, crypto *domain.EnvelopeCrypto) *Factory {
	return &Factory{
		creds:  creds,
		crypto: crypto,
		build:  buildDynamicClient,
		cache:  make(map[string]*Client),
	}
}

// Client 获取（或复用缓存）指定集群的 dynamic client：
//  1. 命中缓存直接返回（缓存复用）；
//  2. 读取集群凭证（未命中 mongo.ErrNoDocuments 透传，404 语义）；
//  3. 按密文携带 keyVersion 解密 kubeconfig（轮换双读），明文用后 Zeroize
//     （Hard Rule：明文仅内存解密用后清零）；
//  4. 构建 dynamic client——失败统一包装 domain.ErrK8sUnreachable
//     （K8S_UNREACHABLE 语义；错误文案不携带 kubeconfig 内容）。
func (f *Factory) Client(ctx context.Context, clusterName string) (*Client, error) {
	f.mu.RLock()
	cached, ok := f.cache[clusterName]
	f.mu.RUnlock()
	if ok {
		return cached, nil
	}

	cred, err := f.creds.GetByClusterName(ctx, clusterName)
	if err != nil {
		return nil, err // mongo.ErrNoDocuments 透传
	}
	if cred.Kubeconfig == nil {
		return nil, fmt.Errorf("cert: cluster %q has no encrypted kubeconfig stored", clusterName)
	}
	if f.crypto == nil {
		return nil, fmt.Errorf("cert: cluster %q: envelope crypto not configured", clusterName)
	}
	plaintext, err := f.crypto.Decrypt(cred.Kubeconfig.Ciphertext, cred.Kubeconfig.KeyVersion)
	if err != nil {
		return nil, fmt.Errorf("cert: decrypt kubeconfig for cluster %q failed: %w", clusterName, err)
	}
	defer domain.Zeroize(&plaintext)

	dyn, err := f.build(plaintext)
	if err != nil {
		// 不透出底层构建错误（可能携带 kubeconfig 内容片段）
		return nil, fmt.Errorf("%w: build dynamic client for cluster %q failed", domain.ErrK8sUnreachable, clusterName)
	}

	client := &Client{cluster: clusterName, dyn: dyn}
	f.mu.Lock()
	defer f.mu.Unlock()
	// double-check：并发首建时保留先入缓存实例，保证"同集群同一 Client"复用语义
	if existing, ok := f.cache[clusterName]; ok {
		return existing, nil
	}
	f.cache[clusterName] = client
	return client, nil
}

// Invalidate 失效指定集群的缓存 client（删除/更换集群凭证后调用，
// 防止旧凭证连接被复用）；集群无缓存时为 no-op。
func (f *Factory) Invalidate(clusterName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cache, clusterName)
}

// buildDynamicClient 默认构建路径：kubeconfig → rest.Config（带请求超时）
// → dynamic client。
func buildDynamicClient(kubeconfig []byte) (dynamic.Interface, error) {
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	restCfg.Timeout = k8sRequestTimeout
	return dynamic.NewForConfig(restCfg)
}
