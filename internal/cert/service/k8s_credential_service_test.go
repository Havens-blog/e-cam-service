package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
)

// svcKubeconfig 集群凭证服务测试夹具（合法 kubeconfig；植入 marker 断言密文/视图不泄漏明文）。
const svcKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: c
  context:
    cluster: c
    user: u
current-context: c
users:
- name: u
  user:
    token: SECRET-SVC-MARKER
`

// fakeSeeder 内置登记播种假实现（记录调用，可注入错误）。
type fakeSeeder struct {
	calls    []string
	initErr  error
	delegate BuiltinSeeder // 可选委托真实实现
}

func (f *fakeSeeder) EnsureBuiltinRegistrations(ctx context.Context, clusterID string) error {
	f.calls = append(f.calls, clusterID)
	if f.delegate != nil {
		if err := f.delegate.EnsureBuiltinRegistrations(ctx, clusterID); err != nil {
			return err
		}
	}
	return f.initErr
}

// fakeCache dynamic client 缓存失效假实现。
type fakeCache struct{ invalidated []string }

func (f *fakeCache) Invalidate(clusterName string) {
	f.invalidated = append(f.invalidated, clusterName)
}

type k8sCredFixture struct {
	svc    K8sCredentialService
	creds  *certtest.FakeK8sCredentialRepo
	crypto *domain.EnvelopeCrypto
	seeder *fakeSeeder
	cache  *fakeCache
}

func newK8sCredFixture(t *testing.T) *k8sCredFixture {
	t.Helper()
	creds := certtest.NewFakeK8sCredentialRepo()
	crypto := certtest.NewTestCrypto(t)
	seeder := &fakeSeeder{}
	cache := &fakeCache{}
	return &k8sCredFixture{
		svc:    NewK8sCredentialService(creds, crypto, seeder, cache),
		creds:  creds,
		crypto: crypto,
		seeder: seeder,
		cache:  cache,
	}
}

// ---------------------------------------------------------------------
// 新增：加密落库
// ---------------------------------------------------------------------

func TestAddClusterEncryptsKubeconfigAtRest(t *testing.T) {
	fx := newK8sCredFixture(t)

	view, err := fx.svc.AddCluster(t.Context(), AddK8sCredentialInput{
		ClusterName: "prod-cluster",
		Kubeconfig:  []byte(svcKubeconfig),
		APIEndpoint: "https://127.0.0.1:6443",
	})
	require.NoError(t, err)
	assert.Equal(t, "prod-cluster", view.ClusterName)
	assert.Equal(t, "https://127.0.0.1:6443", view.APIEndpoint)
	assert.False(t, view.CreatedAt.IsZero())

	// 落库形态：密文 + keyVersion + AES-256-GCM（schema.sql kubeconfig 对象形状）
	stored, err := fx.creds.GetByClusterName(t.Context(), "prod-cluster")
	require.NoError(t, err)
	require.NotNil(t, stored.Kubeconfig)
	assert.Equal(t, domain.AlgoAES256GCM, stored.Kubeconfig.Algo)
	assert.Equal(t, fx.crypto.LatestVersion(), stored.Kubeconfig.KeyVersion)
	assert.NotContains(t, stored.Kubeconfig.Ciphertext, "SECRET-SVC-MARKER", "密文不得含明文片段")
	assert.NotEqual(t, svcKubeconfig, stored.Kubeconfig.Ciphertext)

	// 解密回路：密文可还原明文（信封加密体系一致）
	plaintext, err := fx.crypto.Decrypt(stored.Kubeconfig.Ciphertext, stored.Kubeconfig.KeyVersion)
	require.NoError(t, err)
	domain.Zeroize(&plaintext)
}

func TestAddClusterDuplicateClusterName(t *testing.T) {
	fx := newK8sCredFixture(t)
	in := AddK8sCredentialInput{ClusterName: "dup", Kubeconfig: []byte(svcKubeconfig)}
	_, err := fx.svc.AddCluster(t.Context(), in)
	require.NoError(t, err)

	_, err = fx.svc.AddCluster(t.Context(), in)
	assert.ErrorIs(t, err, domain.ErrDuplicateClusterName, "clusterName 唯一冲突哨兵（uk_cluster_name → 409）")
	creds, err := fx.creds.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, creds, 1)
}

func TestAddClusterInputValidation(t *testing.T) {
	fx := newK8sCredFixture(t)

	cases := []struct {
		name string
		in   AddK8sCredentialInput
		want error
	}{
		{"empty clusterName", AddK8sCredentialInput{Kubeconfig: []byte(svcKubeconfig)}, nil},
		{"empty kubeconfig", AddK8sCredentialInput{ClusterName: "c1"}, k8s.ErrInvalidKubeconfig},
		{"invalid kubeconfig", AddK8sCredentialInput{ClusterName: "c1", Kubeconfig: []byte("bad: [yaml")}, k8s.ErrInvalidKubeconfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fx.svc.AddCluster(t.Context(), tc.in)
			require.Error(t, err)
			if tc.want != nil {
				assert.ErrorIs(t, err, tc.want)
			}
			creds, err := fx.creds.List(t.Context())
			require.NoError(t, err)
			assert.Empty(t, creds, "校验失败不得落库")
		})
	}
}

func TestAddClusterTrimsClusterName(t *testing.T) {
	fx := newK8sCredFixture(t)
	view, err := fx.svc.AddCluster(t.Context(), AddK8sCredentialInput{
		ClusterName: "  prod-cluster  ",
		Kubeconfig:  []byte(svcKubeconfig),
	})
	require.NoError(t, err)
	assert.Equal(t, "prod-cluster", view.ClusterName)
}

// ---------------------------------------------------------------------
// 列表：白名单视图
// ---------------------------------------------------------------------

// TestK8sCredentialViewWhitelist 视图结构体仅含 clusterName/apiEndpoint/createdAt
// 三字段（AC：任何读取路径不返回明文，仅此三字段——结构体白名单为编译期保证）。
func TestK8sCredentialViewWhitelist(t *testing.T) {
	typ := reflect.TypeOf(K8sCredentialView{})
	fields := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		fields[typ.Field(i).Name] = true
	}
	assert.Equal(t, map[string]bool{"ClusterName": true, "APIEndpoint": true, "CreatedAt": true}, fields)
}

func TestListClustersNoKubeconfigLeak(t *testing.T) {
	fx := newK8sCredFixture(t)
	for _, name := range []string{"c1", "c2"} {
		_, err := fx.svc.AddCluster(t.Context(), AddK8sCredentialInput{
			ClusterName: name, Kubeconfig: []byte(svcKubeconfig),
		})
		require.NoError(t, err)
	}

	views, err := fx.svc.ListClusters(t.Context())
	require.NoError(t, err)
	require.Len(t, views, 2)
	for _, v := range views {
		assert.NotContains(t, v.ClusterName, "SECRET-SVC-MARKER")
		assert.NotContains(t, v.APIEndpoint, "SECRET-SVC-MARKER")
		assert.False(t, v.CreatedAt.IsZero())
	}
}

// ---------------------------------------------------------------------
// 删除
// ---------------------------------------------------------------------

func TestDeleteCluster(t *testing.T) {
	fx := newK8sCredFixture(t)
	_, err := fx.svc.AddCluster(t.Context(), AddK8sCredentialInput{
		ClusterName: "prod-cluster", Kubeconfig: []byte(svcKubeconfig),
	})
	require.NoError(t, err)

	require.NoError(t, fx.svc.DeleteCluster(t.Context(), "prod-cluster"))
	_, err = fx.creds.GetByClusterName(t.Context(), "prod-cluster")
	assert.ErrorIs(t, err, mongo.ErrNoDocuments)
	assert.Equal(t, []string{"prod-cluster"}, fx.cache.invalidated, "删除后失效 dynamic client 缓存")
}

func TestDeleteClusterErrors(t *testing.T) {
	fx := newK8sCredFixture(t)

	err := fx.svc.DeleteCluster(t.Context(), "")
	require.Error(t, err, "空集群名拒绝")

	err = fx.svc.DeleteCluster(t.Context(), "unknown")
	assert.ErrorIs(t, err, mongo.ErrNoDocuments, "未命中集群 404 语义")
}

// ---------------------------------------------------------------------
// 内置登记播种联动
// ---------------------------------------------------------------------

func TestAddClusterSeedsBuiltins(t *testing.T) {
	fx := newK8sCredFixture(t)
	_, err := fx.svc.AddCluster(t.Context(), AddK8sCredentialInput{
		ClusterName: "prod-cluster", Kubeconfig: []byte(svcKubeconfig),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"prod-cluster"}, fx.seeder.calls, "AddCluster 成功后播种内置登记")
}

func TestAddClusterSeederFailureRetryable(t *testing.T) {
	fx := newK8sCredFixture(t)
	fx.seeder.initErr = errors.New("seed down")
	_, err := fx.svc.AddCluster(t.Context(), AddK8sCredentialInput{
		ClusterName: "prod-cluster", Kubeconfig: []byte(svcKubeconfig),
	})
	require.Error(t, err, "播种失败显式暴露")

	// 凭证已落库：播种幂等可重试（文档化语义）
	_, err = fx.creds.GetByClusterName(t.Context(), "prod-cluster")
	assert.NoError(t, err)
}

func TestAddClusterNilSeederAndCache(t *testing.T) {
	creds := certtest.NewFakeK8sCredentialRepo()
	svc := NewK8sCredentialService(creds, certtest.NewTestCrypto(t), nil, nil)
	_, err := svc.AddCluster(t.Context(), AddK8sCredentialInput{
		ClusterName: "c1", Kubeconfig: []byte(svcKubeconfig),
	})
	require.NoError(t, err, "可选依赖 nil 不阻塞凭证登记")
	require.NoError(t, svc.DeleteCluster(t.Context(), "c1"))
}

// TestAddClusterRealSeederEndToEnd 凭证服务 + 真实登记服务联动：
// AddCluster 后该集群四类内置登记就绪（随扫描范围生效）。
func TestAddClusterRealSeederEndToEnd(t *testing.T) {
	creds := certtest.NewFakeK8sCredentialRepo()
	regs := certtest.NewFakeCrdRegistrationRepo()
	seeder := NewCrdRegistrationService(regs)
	svc := NewK8sCredentialService(creds, certtest.NewTestCrypto(t), seeder, nil)

	_, err := svc.AddCluster(t.Context(), AddK8sCredentialInput{
		ClusterName: "prod-cluster", Kubeconfig: []byte(svcKubeconfig),
	})
	require.NoError(t, err)

	enabled, err := regs.ListEnabled(t.Context())
	require.NoError(t, err)
	assert.Len(t, enabled, 4, "内置四类登记 enabled=true")
}
