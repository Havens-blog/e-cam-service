// K8sAPIChannel 单元测试（任务 5.6）：fake dynamic client 覆盖——
// Discover 固定枚举+enabled 登记遍历与盲区声明、三信号管理权探测、
// Deploy 仅 patch 证书引用字段、Rollback、RecheckCRDField 通过/回写失败。
package deployer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

// ---------------------------------------------------------------------
// 测试夹具：GVR / 对象构造 / fake provider
// ---------------------------------------------------------------------

var (
	testAlbGVR     = schema.GroupVersionResource{Group: "alb.alibabacloud.com", Version: "v1", Resource: "albconfigs"}
	testIngressGVR = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
	testGatewayGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	testRouteGVR   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	testFooGVR     = schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "foos"}
	testBarGVR     = schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "bars"}
)

// testListKinds fake dynamic client 的 GVR→list kind 映射（内置四类 + 自定义
// Foo/Bar 全部注册，未注册 GVR 的 List 不确定——通道 Discover 会遍历固定枚举）。
var testListKinds = map[schema.GroupVersionResource]string{
	testAlbGVR:     "AlbConfigList",
	testIngressGVR: "IngressList",
	testGatewayGVR: "GatewayList",
	testRouteGVR:   "HTTPRouteList",
	testFooGVR:     "FooList",
	testBarGVR:     "BarList",
}

// albObject 构造 AlbConfig 形态对象：listeners[].certificates[].certificateId
// 证书引用 + port/protocol/loadBalancerId 等无关字段（patch 仅证书字段断言依据）。
func albObject(name, namespace, certID string, mutate func(*unstructured.Unstructured)) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "alb.alibabacloud.com/v1",
		"kind":       "AlbConfig",
		"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
		"spec": map[string]interface{}{
			"loadBalancerId": "lb-1",
			"listeners": []interface{}{
				map[string]interface{}{
					"port":     float64(443),
					"protocol": "HTTPS",
					"certificates": []interface{}{
						map[string]interface{}{"certificateId": certID, "isDefault": true},
					},
				},
			},
		},
	}}
	u.SetGroupVersionKind(testAlbGVR.GroupVersion().WithKind("AlbConfig"))
	if mutate != nil {
		mutate(u)
	}
	return u
}

// fooObject 自定义 CRD 形态对象（spec.certId 标量引用字段）。
func fooObject(name, namespace, certID string, mutate func(*unstructured.Unstructured)) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "example.com/v1",
		"kind":       "Foo",
		"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
		"spec":       map[string]interface{}{"certId": certID, "replicas": float64(2)},
	}}
	u.SetGroupVersionKind(testFooGVR.GroupVersion().WithKind("Foo"))
	if mutate != nil {
		mutate(u)
	}
	return u
}

// fakeCRDClient dynamic fake 的 CRDClient 适配（复刻 k8s.Client 连接类错误
// → domain.ErrK8sUnreachable 包装语义，使通道层 errors.Is 判定与生产一致）。
type fakeCRDClient struct {
	dyn *dynamicfake.FakeDynamicClient
}

func (f fakeCRDClient) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	var (
		list *unstructured.UnstructuredList
		err  error
	)
	if namespace == "" {
		list, err = f.dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
	} else {
		list, err = f.dyn.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fakeWrapConn("list", gvr, err)
	}
	return list, nil
}

func (f fakeCRDClient) Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	obj, err := f.dyn.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fakeWrapConn("get", gvr, err)
	}
	return obj, nil
}

func (f fakeCRDClient) Patch(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, patchType types.PatchType, data []byte) (*unstructured.Unstructured, error) {
	obj, err := f.dyn.Resource(gvr).Namespace(namespace).Patch(ctx, name, patchType, data, metav1.PatchOptions{})
	if err != nil {
		return nil, fakeWrapConn("patch", gvr, err)
	}
	return obj, nil
}

func fakeWrapConn(op string, gvr schema.GroupVersionResource, err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: fake cluster %s %s: connection failed", domain.ErrK8sUnreachable, op, gvr.String())
	}
	return err
}

// fakeProvider 按集群返回 fake CRDClient。
type fakeProvider struct {
	byCluster map[string]CRDClient
}

func (f *fakeProvider) Client(_ context.Context, cluster string) (CRDClient, error) {
	c, ok := f.byCluster[cluster]
	if !ok {
		return nil, fmt.Errorf("fake provider: unknown cluster %q", cluster)
	}
	return c, nil
}

// kubeconfigCreds kubeconfig 形态凭证（通道仅做形态校验，连接经 provider）。
func kubeconfigCreds() Credential {
	return Credential{Kind: CredentialKindKubeconfig, Secret: []byte("apiVersion: v1"), KeyVersion: 1}
}

// k8sTarget k8s_api 通道 DeployTarget 夹具。
func k8sTarget(cluster, namespace, kind, resource string) DeployTarget {
	return DeployTarget{
		Channel:    string(ChannelTypeK8sAPI),
		ClusterID:  cluster,
		Namespace:  namespace,
		Kind:       kind,
		ResourceID: resource,
	}
}

// fixture 通道测试夹具：登记表 + 映射表 + 按集群 dynamic fake。
type fixture struct {
	channel *K8sAPIChannel
	regs    *certtest.FakeCrdRegistrationRepo
	maps    *certtest.FakeCloudCertMappingRepo
	prov    *fakeProvider
	fakes   map[string]*dynamicfake.FakeDynamicClient
}

// newFixture 构造夹具并按集群注册对象（cluster → runtime.Object 列表）。
func newFixture(t *testing.T, signals ManagementSignalConfig, objs map[string][]runtime.Object) *fixture {
	t.Helper()
	regs := certtest.NewFakeCrdRegistrationRepo()
	maps := certtest.NewFakeCloudCertMappingRepo()
	prov := &fakeProvider{byCluster: map[string]CRDClient{}}
	fakes := map[string]*dynamicfake.FakeDynamicClient{}
	for cluster, objects := range objs {
		dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), testListKinds, objects...)
		prov.byCluster[cluster] = fakeCRDClient{dyn: dyn}
		fakes[cluster] = dyn
	}
	return &fixture{
		channel: NewK8sAPIChannel(prov, regs, maps, signals),
		regs:    regs,
		maps:    maps,
		prov:    prov,
		fakes:   fakes,
	}
}

// register 登记自定义 CRD（enabled 缺省 true；false 时 SetEnabled 关停）。
func (f *fixture) register(t *testing.T, cluster, apiGroup, kind, certFieldPath string, enabled bool) {
	t.Helper()
	reg := &domain.CrdRegistration{ClusterID: cluster, APIGroup: apiGroup, Kind: kind, CertFieldPath: certFieldPath}
	require.NoError(t, f.regs.Create(t.Context(), reg))
	if !enabled {
		require.NoError(t, f.regs.SetEnabled(t.Context(), reg.ID.Hex(), false))
	}
}

// seedMapping 写入指纹→云证书 ID 映射（active）。
func (f *fixture) seedMapping(t *testing.T, fingerprint, cloudCertID string) {
	t.Helper()
	require.NoError(t, f.maps.Upsert(t.Context(), &domain.CloudCertMapping{
		CertFingerprint: fingerprint,
		Cloud:           "aliyun",
		AccountKey:      "acc-1",
		CloudCertID:     cloudCertID,
		Status:          domain.MappingStatusActive,
	}))
}

// writeBack 模拟 reconcile 回写：merge patch 改写对象证书引用字段。
func (f *fixture) writeBack(t *testing.T, cluster, namespace, name, certID string) {
	t.Helper()
	patch := fmt.Sprintf(`{"spec":{"listeners":[{"certificates":[{"certificateId":%q}]}]}}`, certID)
	_, err := f.prov.byCluster[cluster].Patch(t.Context(), testAlbGVR, namespace, name, types.MergePatchType, []byte(patch))
	require.NoError(t, err)
}

// ---------------------------------------------------------------------
// Discover：固定枚举 + enabled 登记遍历、盲区声明
// ---------------------------------------------------------------------

func TestK8sChannelDiscoverBuiltinAndCustom(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {
			albObject("gw-1", "prod", "cert-old-1", nil),
			albObject("gw-2", "prod", "cert-old-2", nil),
			fooObject("foo-1", "prod", "cert-foo-1", nil),
		},
	})
	fx.register(t, "c1", "example.com", "Foo", "spec.certId", true)
	fx.register(t, "c1", "example.com", "Bar", "spec.certId", false) // 停用 → 盲区
	fx.seedMapping(t, "fp-known", "cert-old-1")                      // cert-old-1 可反查指纹

	refs, err := fx.channel.Discover(t.Context(), kubeconfigCreds(), DiscoverScope{
		ClusterIDs: []string{"c1"},
		SnapshotID: "snap-1",
	})
	require.NoError(t, err)
	require.Len(t, refs, 3, "固定枚举 AlbConfig×2 + enabled 自定义 Foo×1；停用 Bar 不遍历")

	sort.Slice(refs, func(i, j int) bool { return refs[i].ResourceID < refs[j].ResourceID })
	// 排序后：foo-1 < gw-1 < gw-2
	for _, r := range refs {
		assert.Equal(t, domain.ProductCRD, r.Product)
		assert.Equal(t, "c1", r.ClusterID)
		assert.Equal(t, "prod", r.Namespace)
		assert.Equal(t, "snap-1", r.SnapshotID, "snapshotId 回写")
		assert.Empty(t, r.Cloud, "K8s 引用不归属单一云（product=crd）")
	}
	assert.Equal(t, "Foo", refs[0].Kind)
	assert.Equal(t, "foo-1", refs[0].ResourceID)
	assert.Equal(t, "cert-foo-1", refs[0].ReferencedCloudCertID)
	assert.Equal(t, unresolvedFingerprintForAssert("c1", "cert-foo-1"), refs[0].CertFingerprint, "无映射引用 → 确定性占位指纹")

	assert.Equal(t, "AlbConfig", refs[1].Kind)
	assert.Equal(t, "gw-1", refs[1].ResourceID)
	assert.Equal(t, "cert-old-1", refs[1].ReferencedCloudCertID)
	assert.Equal(t, "fp-known", refs[1].CertFingerprint, "映射可反查 → 精确指纹")

	assert.Len(t, refs[2].CertFingerprint, 64)
	assert.Equal(t, unresolvedFingerprintForAssert("c1", "cert-old-2"), refs[2].CertFingerprint)

	// 盲区声明：enabled=false 登记回归盲区
	spots, err := fx.channel.CRDBlindSpots(t.Context(), []string{"c1"})
	require.NoError(t, err)
	require.Len(t, spots, 1)
	assert.Equal(t, "c1", spots[0].ClusterID)
	assert.Equal(t, "example.com", spots[0].APIGroup)
	assert.Equal(t, "Bar", spots[0].Kind)
	assert.NotEmpty(t, spots[0].Reason, "盲区声明附原因")
}

func TestK8sChannelDiscoverClusterScope(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-a", nil)},
		"c2": {albObject("gw-2", "prod", "cert-b", nil)},
	})
	// 仅 c1 有 enabled 登记；空 ClusterIDs = enabled 登记涉及集群
	fx.register(t, "c1", "example.com", "Foo", "spec.certId", true)
	refs, err := fx.channel.Discover(t.Context(), kubeconfigCreds(), DiscoverScope{SnapshotID: "snap-1"})
	require.NoError(t, err)
	require.Len(t, refs, 1, "无 enabled 登记的集群不在默认遍历范围")
	assert.Equal(t, "c1", refs[0].ClusterID)

	refs, err = fx.channel.Discover(t.Context(), kubeconfigCreds(), DiscoverScope{
		ClusterIDs: []string{"c1", "c2"},
		SnapshotID: "snap-1",
	})
	require.NoError(t, err)
	require.Len(t, refs, 2, "显式 ClusterIDs 时固定枚举对范围集群全遍历")
}

func TestK8sChannelDiscoverUnreachable(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", nil)},
	})
	connErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	fx.fakes["c1"].PrependReactor("list", testAlbGVR.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, connErr
	})

	_, err := fx.channel.Discover(t.Context(), kubeconfigCreds(), DiscoverScope{
		ClusterIDs: []string{"c1"},
		SnapshotID: "snap-1",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrK8sUnreachable, "集群不可达 → K8S_UNREACHABLE")
}

// ---------------------------------------------------------------------
// Probe：三信号管理权探测
// ---------------------------------------------------------------------

func TestK8sChannelProbeThreeSignals(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*unstructured.Unstructured)
		signals    ManagementSignalConfig
		manageable bool
		reason     string
	}{
		{
			name: "无管理信号 → 可自动变更",
			mutate: func(u *unstructured.Unstructured) {
				u.SetLabels(map[string]string{"app": "gw"})
				u.SetAnnotations(map[string]string{"description": "gateway"})
			},
			manageable: true,
		},
		{
			name: "argocd 精确键 label 命中",
			mutate: func(u *unstructured.Unstructured) {
				u.SetLabels(map[string]string{"argocd.argoproj.io/instance": "app"})
			},
			reason: "gitops_label:argocd.argoproj.io/instance",
		},
		{
			name: "fluxcd 前缀 label 命中",
			mutate: func(u *unstructured.Unstructured) {
				u.SetLabels(map[string]string{"fluxcd.io/anything": "x"})
			},
			reason: "gitops_label:fluxcd.io/anything",
		},
		{
			name: "自定义前缀配置生效",
			mutate: func(u *unstructured.Unstructured) {
				u.SetLabels(map[string]string{"gitops.company.io/app": "x"})
			},
			signals: ManagementSignalConfig{GitOpsLabelPrefixes: []string{"gitops.company.io/"}},
			reason:  "gitops_label:gitops.company.io/app",
		},
		{
			name: "ownerReferences 非空命中",
			mutate: func(u *unstructured.Unstructured) {
				u.SetOwnerReferences([]metav1.OwnerReference{
					{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "rs-1"},
				})
			},
			reason: "owner_references:1 first=ReplicaSet/rs-1",
		},
		{
			name: "cert-manager issuer annotation 命中",
			mutate: func(u *unstructured.Unstructured) {
				u.SetAnnotations(map[string]string{"cert-manager.io/issuer": "letsencrypt"})
			},
			reason: "managed_annotation:cert-manager.io/issuer",
		},
		{
			name: "cert-manager cluster-issuer annotation 命中",
			mutate: func(u *unstructured.Unstructured) {
				u.SetAnnotations(map[string]string{"cert-manager.io/cluster-issuer": "letsencrypt"})
			},
			reason: "managed_annotation:cert-manager.io/cluster-issuer",
		},
		{
			name: "多信号并存记首个命中（label 优先于 annotation）",
			mutate: func(u *unstructured.Unstructured) {
				u.SetLabels(map[string]string{"argocd.argoproj.io/instance": "app"})
				u.SetAnnotations(map[string]string{"cert-manager.io/issuer": "le"})
				u.SetOwnerReferences([]metav1.OwnerReference{{Kind: "Deployment", Name: "d1"}})
			},
			reason: "gitops_label:argocd.argoproj.io/instance",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newFixture(t, tt.signals, map[string][]runtime.Object{
				"c1": {albObject("gw-1", "prod", "cert-old-1", tt.mutate)},
			})
			manageable, reason, err := fx.channel.Probe(t.Context(), domain.ResourceRef{
				ClusterID: "c1", Namespace: "prod", Kind: "AlbConfig", ResourceID: "gw-1",
			})
			require.NoError(t, err)
			assert.Equal(t, tt.manageable, manageable)
			if tt.reason != "" {
				assert.Contains(t, reason, tt.reason, "Reason 记信号类型+具体键")
			} else {
				assert.Empty(t, reason)
			}
		})
	}
}

func TestK8sChannelProbeInvalidRef(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{})
	_, _, err := fx.channel.Probe(t.Context(), domain.ResourceRef{Namespace: "prod", Kind: "AlbConfig"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTarget)
}

// ---------------------------------------------------------------------
// Deploy：仅 patch 证书引用字段
// ---------------------------------------------------------------------

func TestK8sChannelDeployPatchesOnlyCertField(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", nil)},
	})
	fx.seedMapping(t, "fp-new", "cert-new-1")

	res, err := fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), "fp-new")
	require.NoError(t, err)
	assert.Equal(t, "cert-old-1", res.OldCloudCertID, "执行前读 oldCloudCertId 保留")
	assert.Empty(t, res.NewCloudCertID, "K8s 通道 NewCloudCertID 为空（tech-design）")
	assert.False(t, res.RecheckPassed, "patch 产出 RecheckPassed=待复检")
	assert.False(t, res.OrphanCandidate, "K8s patch 不产生两段式孤儿候选")

	// Hard Rule：仅证书引用字段被改写，其余字段原样保留
	got, err := fx.prov.byCluster["c1"].Get(t.Context(), testAlbGVR, "prod", "gw-1")
	require.NoError(t, err)
	listeners, found, err := unstructured.NestedSlice(got.Object, "spec", "listeners")
	require.NoError(t, err)
	require.True(t, found)
	listener := listeners[0].(map[string]interface{})
	assert.EqualValues(t, 443, listener["port"], "无关字段不被覆盖（patch 往返仅数值类型归一，值不变）")
	assert.Equal(t, "HTTPS", listener["protocol"], "无关字段不被覆盖")
	certs := listener["certificates"].([]interface{})
	cert := certs[0].(map[string]interface{})
	assert.Equal(t, "cert-new-1", cert["certificateId"], "证书引用字段已替换")
	assert.Equal(t, true, cert["isDefault"], "同对象内兄弟字段保留")
	lbID, _, _ := unstructured.NestedString(got.Object, "spec", "loadBalancerId")
	assert.Equal(t, "lb-1", lbID, "非证书字段不被覆盖")
}

func TestK8sChannelDeployMultiCertLeaves(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", func(u *unstructured.Unstructured) {
			// 双证书监听（主+SNI）：单次部署动作覆盖全部引用叶子
			spec := u.Object["spec"].(map[string]interface{})
			listeners := spec["listeners"].([]interface{})
			listeners[0].(map[string]interface{})["certificates"] = []interface{}{
				map[string]interface{}{"certificateId": "cert-old-1"},
				map[string]interface{}{"certificateId": "cert-old-2"},
			}
		})},
	})
	fx.seedMapping(t, "fp-new", "cert-new-1")

	_, err := fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), "fp-new")
	require.NoError(t, err)

	got, err := fx.prov.byCluster["c1"].Get(t.Context(), testAlbGVR, "prod", "gw-1")
	require.NoError(t, err)
	listeners, found, err := unstructured.NestedSlice(got.Object, "spec", "listeners")
	require.NoError(t, err)
	require.True(t, found)
	certs := listeners[0].(map[string]interface{})["certificates"].([]interface{})
	assert.Equal(t, "cert-new-1", certs[0].(map[string]interface{})["certificateId"])
	assert.Equal(t, "cert-new-1", certs[1].(map[string]interface{})["certificateId"])
}

func TestK8sChannelDeployManagedRefused(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", func(u *unstructured.Unstructured) {
			u.SetLabels(map[string]string{"argocd.argoproj.io/instance": "app"})
		})},
	})
	fx.seedMapping(t, "fp-new", "cert-new-1")
	var patched bool
	fx.fakes["c1"].PrependReactor("patch", testAlbGVR.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
		patched = true
		return false, nil, nil
	})

	_, err := fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), "fp-new")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrK8sTargetManaged, "Deploy 前探测命中 → 拒绝越权变更")
	assert.Contains(t, err.Error(), "gitops_label:argocd.argoproj.io/instance", "错误附命中信号")
	assert.False(t, patched, "受管理资源不得发出 patch")

	got, err := fx.prov.byCluster["c1"].Get(t.Context(), testAlbGVR, "prod", "gw-1")
	require.NoError(t, err)
	leaves := certFieldLeaves(got.Object, "spec.listeners[].certificates[].certificateId")
	require.Len(t, leaves, 1)
	assert.Equal(t, "cert-old-1", leaves[0].value, "对象未被改动")
}

func TestK8sChannelDeployCertIDUnresolved(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", nil)},
	})

	// 无映射：新证书未上传任何云证书库
	_, err := fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), "fp-new")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCloudCertIDUnresolved)

	// 多条互异映射：K8s 引用无法消歧，不猜测写入
	fx.seedMapping(t, "fp-amb", "cert-aliyun-1")
	require.NoError(t, fx.maps.Upsert(t.Context(), &domain.CloudCertMapping{
		CertFingerprint: "fp-amb", Cloud: "tencent", AccountKey: "acc-2",
		CloudCertID: "cert-tencent-1", Status: domain.MappingStatusActive,
	}))
	_, err = fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), "fp-amb")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCloudCertIDUnresolved)
	assert.Contains(t, err.Error(), "2", "歧义错误附互异映射数")
}

func TestK8sChannelDeployUnreachable(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", nil)},
	})
	fx.seedMapping(t, "fp-new", "cert-new-1")
	connErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	fx.fakes["c1"].PrependReactor("get", testAlbGVR.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, connErr
	})

	_, err := fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), "fp-new")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrK8sUnreachable, "K8S_UNREACHABLE → failed")
}

func TestK8sChannelDeployCustomRegistration(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {fooObject("foo-1", "prod", "cert-foo-old", nil)},
	})
	fx.register(t, "c1", "example.com", "Foo", "spec.certId", true)
	fx.seedMapping(t, "fp-new", "cert-foo-new")

	res, err := fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "Foo", "foo-1"), "fp-new")
	require.NoError(t, err)
	assert.Equal(t, "cert-foo-old", res.OldCloudCertID)

	got, err := fx.prov.byCluster["c1"].Get(t.Context(), testFooGVR, "prod", "foo-1")
	require.NoError(t, err)
	certID, found, err := unstructured.NestedString(got.Object, "spec", "certId")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "cert-foo-new", certID)
	replicas, found, err := unstructured.NestedFieldNoCopy(got.Object, "spec", "replicas")
	require.NoError(t, err)
	require.True(t, found)
	assert.EqualValues(t, 2, replicas, "无关字段保留")
}

// ---------------------------------------------------------------------
// Rollback
// ---------------------------------------------------------------------

func TestK8sChannelRollback(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", nil)},
	})
	fx.seedMapping(t, "fp-new", "cert-new-1")
	target := k8sTarget("c1", "prod", "AlbConfig", "gw-1")
	_, err := fx.channel.Deploy(t.Context(), kubeconfigCreds(), target, "fp-new")
	require.NoError(t, err)

	oldRef := domain.CertReference{
		Product: domain.ProductCRD, ClusterID: "c1", Namespace: "prod",
		Kind: "AlbConfig", ResourceID: "gw-1", ReferencedCloudCertID: "cert-old-1",
	}
	res, err := fx.channel.Rollback(t.Context(), kubeconfigCreds(), target, oldRef)
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, oldRef, res.RestoredRef)
	assert.Empty(t, res.OrphanCleaned)

	got, err := fx.prov.byCluster["c1"].Get(t.Context(), testAlbGVR, "prod", "gw-1")
	require.NoError(t, err)
	leaves := certFieldLeaves(got.Object, "spec.listeners[].certificates[].certificateId")
	require.Len(t, leaves, 1)
	assert.Equal(t, "cert-old-1", leaves[0].value, "引用恢复为旧云证书 ID")
}

func TestK8sChannelRollbackUnreachableErrCode(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", nil)},
	})
	connErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	fx.fakes["c1"].PrependReactor("patch", testAlbGVR.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, connErr
	})
	oldRef := domain.CertReference{ReferencedCloudCertID: "cert-old-1"}

	res, err := fx.channel.Rollback(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), oldRef)
	require.Error(t, err)
	assert.False(t, res.Success)
	assert.Equal(t, ErrCodeK8sUnreachable, res.ErrCode, "不可达错误码映射 K8S_UNREACHABLE")
}

// ---------------------------------------------------------------------
// RecheckCRDField：单轮复检（次数固定 1）
// ---------------------------------------------------------------------

func TestK8sChannelRecheckPassAndWriteBack(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", nil)},
	})
	fx.seedMapping(t, "fp-new", "cert-new-1")
	target := k8sTarget("c1", "prod", "AlbConfig", "gw-1")
	_, err := fx.channel.Deploy(t.Context(), kubeconfigCreds(), target, "fp-new")
	require.NoError(t, err)

	item := CRDRecheckItem{
		Ref:       target.ToResourceRef(),
		OrderID:   "ord-1",
		ItemID:    "it-1",
		NewCertID: "cert-new-1",
		OldCertID: "cert-old-1",
	}

	// 复检通过：字段仍为新证书 ID
	res, err := fx.channel.RecheckCRDField(t.Context(), item)
	require.NoError(t, err)
	assert.True(t, res.RecheckPassed)
	assert.Equal(t, "cert-new-1", res.CurrentCertID)
	assert.Empty(t, res.Reason)

	// reconcile 回写旧值 → failed + 告警上下文（orderId/itemId）
	fx.writeBack(t, "c1", "prod", "gw-1", "cert-old-1")
	res, err = fx.channel.RecheckCRDField(t.Context(), item)
	require.NoError(t, err)
	assert.False(t, res.RecheckPassed)
	assert.Equal(t, "cert-old-1", res.CurrentCertID)
	assert.Contains(t, res.Reason, "旧值", "回写旧值与回写其他值区分")
	assert.Contains(t, res.Reason, "ord-1")
	assert.Contains(t, res.Reason, "it-1")

	// 回写为其他值（非新非旧）
	fx.writeBack(t, "c1", "prod", "gw-1", "cert-ghost")
	res, err = fx.channel.RecheckCRDField(t.Context(), item)
	require.NoError(t, err)
	assert.False(t, res.RecheckPassed)
	assert.Equal(t, "cert-ghost", res.CurrentCertID)
	assert.Contains(t, res.Reason, "其他值")
}

func TestK8sChannelRecheckUnreachable(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", nil)},
	})
	connErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	fx.fakes["c1"].PrependReactor("get", testAlbGVR.Resource, func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, connErr
	})

	_, err := fx.channel.RecheckCRDField(t.Context(), CRDRecheckItem{
		Ref:       k8sTarget("c1", "prod", "AlbConfig", "gw-1").ToResourceRef(),
		NewCertID: "cert-new-1",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrK8sUnreachable)
}

// ---------------------------------------------------------------------
// 输入校验 / 装配缺失 / 适配器
// ---------------------------------------------------------------------

func TestK8sChannelInputValidation(t *testing.T) {
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{})

	// 凭证形态：k8s 通道要求 kubeconfig
	_, err := fx.channel.Discover(t.Context(), Credential{
		Kind: CredentialKindCloudAK, Cloud: "aliyun", AccountKey: "a", AccessKey: "k",
		Secret: []byte("s"), KeyVersion: 1,
	}, DiscoverScope{SnapshotID: "snap-1"})
	assert.ErrorIs(t, err, ErrInvalidCredential)

	// Deploy 目标分支校验
	_, err = fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		DeployTarget{Channel: string(ChannelTypeK8sAPI), Namespace: "prod", Kind: "AlbConfig", ResourceID: "gw-1"}, "fp")
	assert.ErrorIs(t, err, ErrInvalidTarget)

	// Deploy 新证书指纹必填
	_, err = fx.channel.Deploy(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), "")
	assert.ErrorIs(t, err, ErrInvalidTarget)

	// 快照归属必填
	_, err = fx.channel.Discover(t.Context(), kubeconfigCreds(), DiscoverScope{})
	assert.ErrorIs(t, err, ErrInvalidScope)

	// Rollback 需旧引用证书 ID
	_, err = fx.channel.Rollback(t.Context(), kubeconfigCreds(),
		k8sTarget("c1", "prod", "AlbConfig", "gw-1"), domain.CertReference{})
	assert.ErrorIs(t, err, ErrInvalidTarget)
}

func TestK8sChannelUnassembled(t *testing.T) {
	ch := NewK8sAPIChannel(nil, nil, nil, ManagementSignalConfig{})
	_, _, err := ch.Probe(t.Context(), domain.ResourceRef{ClusterID: "c1", Namespace: "p", Kind: "AlbConfig", ResourceID: "gw"})
	require.Error(t, err, "装配缺失显式失败")
	_, err = ch.Deploy(t.Context(), kubeconfigCreds(), k8sTarget("c1", "p", "AlbConfig", "gw"), "fp")
	require.Error(t, err, "装配缺失显式失败")
}

func TestK8sFactoryClientsAdapter(t *testing.T) {
	creds := certtest.NewFakeK8sCredentialRepo()
	crypto := certtest.NewTestCrypto(t)
	ciphertext, keyVersion, err := crypto.Encrypt([]byte(`apiVersion: v1
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
    token: test-token
`))
	require.NoError(t, err)
	require.NoError(t, creds.Create(t.Context(), &domain.K8sCredential{
		ClusterName: "c1",
		Kubeconfig:  &domain.EncryptedSecret{Ciphertext: ciphertext, KeyVersion: keyVersion},
	}))
	adapter := K8sFactoryClients{Factory: k8s.NewFactory(creds, crypto)}
	client, err := adapter.Client(t.Context(), "c1")
	require.NoError(t, err)
	assert.NotNil(t, client)

	_, err = adapter.Client(t.Context(), "unknown-cluster")
	assert.Error(t, err, "未登记集群透传仓储错误")
}

func TestK8sChannelDefaultSignalsApplied(t *testing.T) {
	// 零值配置 → 默认三信号键集（argocd/flux 前缀 + cert-manager 双键）
	fx := newFixture(t, ManagementSignalConfig{}, map[string][]runtime.Object{
		"c1": {albObject("gw-1", "prod", "cert-old-1", func(u *unstructured.Unstructured) {
			u.SetAnnotations(map[string]string{"cert-manager.io/cluster-issuer": "le"})
		})},
	})
	manageable, reason, err := fx.channel.Probe(t.Context(), domain.ResourceRef{
		ClusterID: "c1", Namespace: "prod", Kind: "AlbConfig", ResourceID: "gw-1",
	})
	require.NoError(t, err)
	assert.False(t, manageable)
	assert.Contains(t, reason, "cert-manager.io/cluster-issuer")
}

// unresolvedFingerprintForAssert 与 3.5 扫描同口径的占位指纹（断言用）。
func unresolvedFingerprintForAssert(cluster, certID string) string {
	sum := sha256.Sum256([]byte("certscan-unresolved:" + fmt.Sprintf("k8s|%s|%s", cluster, certID)))
	return hex.EncodeToString(sum[:])
}
