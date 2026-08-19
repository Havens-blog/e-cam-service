package k8s

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

// testKubeconfig 最小合法 kubeconfig 夹具；SECRET-MARKER 植入 token，
// 用于断言错误信息/密文不携带明文片段（Hard Rule）。
const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test-cluster
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test-cluster
    user: test-user
current-context: test
users:
- name: test-user
  user:
    token: SECRET-MARKER
`

var albGVR = schema.GroupVersionResource{
	Group: "alb.alibabacloud.com", Version: "v1", Resource: "albconfigs",
}

// albConfigObject 构造 AlbConfig 形态的非结构化对象夹具。
func albConfigObject(name, namespace, certID string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group: albGVR.Group, Version: albGVR.Version, Kind: "AlbConfig",
	})
	u.SetName(name)
	u.SetNamespace(namespace)
	_ = unstructured.SetNestedSlice(u.Object, []interface{}{
		map[string]interface{}{"certificateId": certID},
	}, "spec", "certificates")
	return u
}

// newFakeDynamic 构造带自定义 list kinds 的 fake dynamic client。
func newFakeDynamic(t *testing.T, objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	t.Helper()
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{albGVR: "AlbConfigList"},
		objs...,
	)
}

// ---------------------------------------------------------------------
// 内置默认登记常量表
// ---------------------------------------------------------------------

func TestBuiltinRegistrationsTable(t *testing.T) {
	require.Len(t, BuiltinRegistrations, 4, "首期固定枚举四类")

	byKind := map[string]BuiltinRegistration{}
	for _, b := range BuiltinRegistrations {
		assert.NoError(t, ValidateCertFieldPath(b.CertFieldPath),
			"内置项 %s 的 certFieldPath 必须语法合法", b.Kind)
		assert.NotEmpty(t, b.Version)
		assert.NotEmpty(t, b.Resource)
		gvr := b.GVR()
		assert.Equal(t, b.APIGroup, gvr.Group)
		assert.Equal(t, b.Version, gvr.Version)
		assert.Equal(t, b.Resource, gvr.Resource)
		assert.True(t, IsBuiltinRegistration(b.APIGroup, b.Kind))
		byKind[b.Kind] = b
	}
	// 四类固定枚举齐备（PRD 首期扫描范围）
	for _, kind := range []string{"AlbConfig", "Ingress", "Gateway", "HTTPRoute"} {
		assert.Contains(t, byKind, kind)
	}
	// 未命中枚举
	assert.False(t, IsBuiltinRegistration("example.com", "Foo"))
	assert.False(t, IsBuiltinRegistration("alb.alibabacloud.com", "Ingress"), "跨组不命中")
}

// ---------------------------------------------------------------------
// certFieldPath 语法校验
// ---------------------------------------------------------------------

func TestValidateCertFieldPath(t *testing.T) {
	valid := []string{
		"spec.certificates[].certificateId",
		"spec.tls[].secretName",
		"spec.listeners[].tls.certificateRef.name",
		"spec.parentRefs[].name",
		"spec.certId",
		"spec.a[].b[].c",
		"a",
		"a-b.c_d.e",
	}
	for _, p := range valid {
		assert.NoErrorf(t, ValidateCertFieldPath(p), "valid path rejected: %q", p)
	}

	invalid := []string{
		"",                                   // 空
		".",                                  // 仅分隔符
		"a..b",                               // 连续分隔符（空段）
		".a",                                 // 首段空
		"a.",                                 // 末段空
		"spec..certificateId",                // 中间空段
		"a b",                                // 空白
		"a.[b]",                              // 段内数组语法非法
		"a[]x",                               // [] 后有内容
		"a[][]",                              // 段内多次 []
		"[]",                                 // [] 单独成段
		"spec.certificates[0].certificateId", // 数字下标不支持
		"-a.b",                               // 首字符 '-'
		"a_.b",                               // 末字符 '_'
	}
	for _, p := range invalid {
		err := ValidateCertFieldPath(p)
		require.Errorf(t, err, "invalid path accepted: %q", p)
		assert.ErrorIs(t, err, ErrInvalidCertFieldPath)
		assert.Contains(t, err.Error(), p, "错误信息应包含被拒绝的路径（可读错误）")
	}
}

// ---------------------------------------------------------------------
// kubeconfig 校验
// ---------------------------------------------------------------------

func TestValidateKubeconfig(t *testing.T) {
	assert.NoError(t, ValidateKubeconfig([]byte(testKubeconfig)))
	assert.ErrorIs(t, ValidateKubeconfig(nil), ErrInvalidKubeconfig)
	assert.ErrorIs(t, ValidateKubeconfig([]byte("")), ErrInvalidKubeconfig)
	// YAML 语法错误（未闭合序列）
	assert.ErrorIs(t, ValidateKubeconfig([]byte("clusters: [unclosed")), ErrInvalidKubeconfig)
}

// TestValidateKubeconfigNoPlaintextLeak 错误信息不得携带 kubeconfig 内容片段。
func TestValidateKubeconfigNoPlaintextLeak(t *testing.T) {
	err := ValidateKubeconfig([]byte("token: [SECRET-MARKER\nbad"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET-MARKER")
}

// ---------------------------------------------------------------------
// Factory：解密构建 + 缓存复用 + 不可达哨兵
// ---------------------------------------------------------------------

// fakeBuildFn 记录构建调用与收到的明文 kubeconfig。
type fakeBuildFn struct {
	calls     atomic.Int32
	plaintext [][]byte
	dyn       dynamic.Interface
	err       error
}

func (f *fakeBuildFn) build(kubeconfig []byte) (dynamic.Interface, error) {
	f.calls.Add(1)
	cp := append([]byte(nil), kubeconfig...)
	f.plaintext = append(f.plaintext, cp)
	if f.err != nil {
		return nil, f.err
	}
	return f.dyn, nil // 缓存/解密路径测试不发起请求，dyn 可为 nil
}

// newFactoryFixture 构造工厂夹具：已登记加密凭证的假仓储 + 注入 build。
func newFactoryFixture(t *testing.T, clusterName string, build buildClientFn) (*Factory, *certtest.FakeK8sCredentialRepo, *domain.EnvelopeCrypto) {
	t.Helper()
	crypto := certtest.NewTestCrypto(t)
	creds := certtest.NewFakeK8sCredentialRepo()
	if clusterName != "" {
		ciphertext, keyVersion, err := crypto.Encrypt([]byte(testKubeconfig))
		require.NoError(t, err)
		require.NoError(t, creds.Create(t.Context(), &domain.K8sCredential{
			ClusterName: clusterName,
			Kubeconfig: &domain.EncryptedSecret{
				Ciphertext: ciphertext, KeyVersion: keyVersion, Algo: domain.AlgoAES256GCM,
			},
		}))
	}
	f := NewFactory(creds, crypto)
	f.build = build
	return f, creds, crypto
}

func TestFactoryClientBuildAndCache(t *testing.T) {
	build := &fakeBuildFn{}
	f, _, _ := newFactoryFixture(t, "prod-cluster", build.build)
	ctx := t.Context()

	c1, err := f.Client(ctx, "prod-cluster")
	require.NoError(t, err)
	assert.Equal(t, "prod-cluster", c1.ClusterName())

	// 缓存复用：同集群多次获取同一实例，仅构建一次
	c2, err := f.Client(ctx, "prod-cluster")
	require.NoError(t, err)
	assert.Same(t, c1, c2)
	assert.Equal(t, int32(1), build.calls.Load())

	// 构建收到的明文与原始 kubeconfig 一致（解密路径正确）
	require.Len(t, build.plaintext, 1)
	assert.Equal(t, testKubeconfig, string(build.plaintext[0]))
}

func TestFactoryClusterMissing(t *testing.T) {
	f, _, _ := newFactoryFixture(t, "prod-cluster", (&fakeBuildFn{}).build)
	_, err := f.Client(t.Context(), "unknown")
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
}

func TestFactoryBuildFailureUnreachable(t *testing.T) {
	build := &fakeBuildFn{err: errors.New("rest config build failed")}
	f, _, _ := newFactoryFixture(t, "prod-cluster", build.build)

	_, err := f.Client(t.Context(), "prod-cluster")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrK8sUnreachable)
	// Hard Rule：错误信息不得携带 kubeconfig 明文片段
	assert.NotContains(t, err.Error(), "SECRET-MARKER")
	// 失败不缓存：下次调用重试构建
	_, _ = f.Client(t.Context(), "prod-cluster")
	assert.Equal(t, int32(2), build.calls.Load())
}

func TestFactoryNilKubeconfigStored(t *testing.T) {
	creds := certtest.NewFakeK8sCredentialRepo()
	require.NoError(t, creds.Create(t.Context(), &domain.K8sCredential{ClusterName: "broken"}))
	f := NewFactory(creds, certtest.NewTestCrypto(t))
	f.build = (&fakeBuildFn{}).build

	_, err := f.Client(t.Context(), "broken")
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrK8sUnreachable, "缺密文属数据缺陷，非集群不可达")
}

func TestFactoryNilCryptoFailFast(t *testing.T) {
	creds := certtest.NewFakeK8sCredentialRepo()
	ciphertext, keyVersion, err := certtest.NewTestCrypto(t).Encrypt([]byte(testKubeconfig))
	require.NoError(t, err)
	require.NoError(t, creds.Create(t.Context(), &domain.K8sCredential{
		ClusterName: "prod-cluster",
		Kubeconfig:  &domain.EncryptedSecret{Ciphertext: ciphertext, KeyVersion: keyVersion},
	}))
	f := NewFactory(creds, nil) // 未装配加密组件
	f.build = (&fakeBuildFn{}).build

	_, err = f.Client(t.Context(), "prod-cluster")
	require.Error(t, err)
}

func TestFactoryInvalidate(t *testing.T) {
	build := &fakeBuildFn{}
	f, _, _ := newFactoryFixture(t, "prod-cluster", build.build)
	ctx := t.Context()

	c1, err := f.Client(ctx, "prod-cluster")
	require.NoError(t, err)

	f.Invalidate("prod-cluster")
	c2, err := f.Client(ctx, "prod-cluster")
	require.NoError(t, err)
	assert.NotSame(t, c1, c2)
	assert.Equal(t, int32(2), build.calls.Load())

	f.Invalidate("never-existed") // no-op 不 panic
}

// TestBuildDynamicClientReal 真实构建路径（clientcmd 解析，无网络请求）。
func TestBuildDynamicClientReal(t *testing.T) {
	dyn, err := buildDynamicClient([]byte(testKubeconfig))
	require.NoError(t, err)
	assert.NotNil(t, dyn)

	_, err = buildDynamicClient([]byte("not a kubeconfig: ["))
	assert.Error(t, err)
}

// ---------------------------------------------------------------------
// Client：fake dynamic client 覆盖 list/get/patch 通路
// ---------------------------------------------------------------------

func TestClientListGetPatch(t *testing.T) {
	objs := []runtime.Object{
		albConfigObject("gw-1", "prod", "cert-old-1"),
		albConfigObject("gw-2", "prod", "cert-old-2"),
		albConfigObject("gw-3", "default", "cert-old-3"),
	}
	c := &Client{cluster: "prod-cluster", dyn: newFakeDynamic(t, objs...)}
	ctx := t.Context()

	// List：全命名空间
	all, err := c.List(ctx, albGVR, "")
	require.NoError(t, err)
	assert.Len(t, all.Items, 3)

	// List：指定命名空间
	prod, err := c.List(ctx, albGVR, "prod")
	require.NoError(t, err)
	assert.Len(t, prod.Items, 2)

	// Get
	got, err := c.Get(ctx, albGVR, "prod", "gw-1")
	require.NoError(t, err)
	slice, found, err := unstructured.NestedSlice(got.Object, "spec", "certificates")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "cert-old-1", slice[0].(map[string]interface{})["certificateId"])

	// Get：不存在 → API 语义错误透传（非 K8S_UNREACHABLE）
	_, err = c.Get(ctx, albGVR, "prod", "nope")
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrK8sUnreachable)

	// Patch：merge patch 替换证书引用字段（5.6 Deploy 仅 patch 证书引用字段）
	patched, err := c.Patch(ctx, albGVR, "prod", "gw-1", types.MergePatchType,
		[]byte(`{"spec":{"certificates":[{"certificateId":"cert-new-1"}]}}`))
	require.NoError(t, err)
	slice, found, err = unstructured.NestedSlice(patched.Object, "spec", "certificates")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "cert-new-1", slice[0].(map[string]interface{})["certificateId"])
}

func TestClientConnectionErrorsMapUnreachable(t *testing.T) {
	connErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	dyn := newFakeDynamic(t, albConfigObject("gw-1", "prod", "cert-old"))
	dyn.PrependReactor("list", albGVR.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, connErr
	})
	dyn.PrependReactor("get", albGVR.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, connErr
	})
	dyn.PrependReactor("patch", albGVR.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, connErr
	})
	c := &Client{cluster: "prod-cluster", dyn: dyn}
	ctx := t.Context()

	_, err := c.List(ctx, albGVR, "")
	require.ErrorIs(t, err, domain.ErrK8sUnreachable, "list 连接失败 → K8S_UNREACHABLE")
	_, err = c.Get(ctx, albGVR, "prod", "gw-1")
	require.ErrorIs(t, err, domain.ErrK8sUnreachable, "get 连接失败 → K8S_UNREACHABLE")
	_, err = c.Patch(ctx, albGVR, "prod", "gw-1", types.MergePatchType, []byte(`{}`))
	require.ErrorIs(t, err, domain.ErrK8sUnreachable, "patch 连接失败 → K8S_UNREACHABLE")
}

func TestIsConnectionError(t *testing.T) {
	assert.False(t, isConnectionError(nil))
	assert.False(t, isConnectionError(errors.New("plain error")))
	assert.True(t, isConnectionError(&net.OpError{Op: "dial", Err: errors.New("refused")}))
	assert.True(t, isConnectionError(&net.DNSError{Err: "no such host", Name: "x", IsTimeout: false}))
}
