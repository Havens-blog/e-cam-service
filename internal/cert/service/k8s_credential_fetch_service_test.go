package service

import (
	"context"
	"errors"
	"testing"

	certtest "github.com/Havens-blog/e-cam-service/internal/cert/certtest"
	sharedomain "github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCSGateway AliyunCSGateway 假实现（清单/拉取逐 clusterId 控制行为）。
type fakeCSGateway struct {
	clusters   []AliyunCluster
	listErr    error
	kubeconfig map[string]string // clusterId → yaml（缺条目=拉取失败）
	fetchErr   map[string]error
}

func (f *fakeCSGateway) ListClusters(_ context.Context, _ *sharedomain.CloudAccount) ([]AliyunCluster, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.clusters, nil
}

func (f *fakeCSGateway) GetKubeconfig(_ context.Context, _ *sharedomain.CloudAccount, clusterID string, _ bool) (string, error) {
	if err, ok := f.fetchErr[clusterID]; ok {
		return "", err
	}
	return f.kubeconfig[clusterID], nil
}

func TestListAliyunClusters_AccountNotFound(t *testing.T) {
	svc := NewK8sCredentialFetchService(&fakeCSGateway{}, &fakeAccountRepo{}, nil)
	_, err := svc.ListAliyunClusters(context.Background(), "no-such-account")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFetchAndRegister_BatchSemantics(t *testing.T) {
	validCfg := []byte("apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: https://1.2.3.4\nusers:\n- name: u\n  user:\n    token: t\ncurrent-context: c\ncontexts:\n- name: c\n  context:\n    cluster: c\n    user: u\n")
	gw := &fakeCSGateway{
		clusters: []AliyunCluster{
			{ClusterID: "c-1", Name: "prod-cluster", RegionID: "cn-shenzhen", State: "running", ClusterType: "ManagedKubernetes"},
			{ClusterID: "c-2", Name: "test-cluster", RegionID: "cn-shenzhen", State: "running", ClusterType: "ManagedKubernetes"},
		},
		kubeconfig: map[string]string{"c-1": string(validCfg)}, // c-2 拉取失败
		fetchErr:   map[string]error{"c-2": errors.New("Forbidden")},
	}
	accounts := &fakeAccountRepo{accounts: []sharedomain.CloudAccount{{
		Name: "集团-阿里云", Provider: sharedomain.CloudProviderAliyun, Status: sharedomain.CloudAccountStatusActive,
		Regions: []string{"cn-shenzhen"},
	}}}
	creds := NewK8sCredentialService(certtest.NewFakeK8sCredentialRepo(), certtest.NewTestCrypto(t), nil, nil)
	svc := NewK8sCredentialFetchService(gw, accounts, creds)

	// 账号缺失 → 整批同因失败
	res := svc.FetchAndRegister(context.Background(), FetchK8sCredentialInput{AccountKey: "ghost", ClusterIDs: []string{"c-1"}})
	require.Len(t, res, 1)
	assert.Equal(t, FetchStatusFailed, res[0].Status)
	assert.Equal(t, reasonK8sFetchAccountMissing, res[0].Reason)

	// 批次：c-1 成功登记，c-2 拉取失败逐条记因不中断
	res = svc.FetchAndRegister(context.Background(), FetchK8sCredentialInput{AccountKey: "集团-阿里云", ClusterIDs: []string{"c-1", "c-2"}})
	require.Len(t, res, 2)
	assert.Equal(t, FetchStatusRegistered, res[0].Status)
	assert.Equal(t, "c-1", res[0].ClusterName)
	assert.Equal(t, FetchStatusFailed, res[1].Status)
	assert.Equal(t, reasonK8sFetchFailed, res[1].Reason)

	// 重名单幂等 → duplicate
	res = svc.FetchAndRegister(context.Background(), FetchK8sCredentialInput{AccountKey: "集团-阿里云", ClusterIDs: []string{"c-1"}})
	require.Len(t, res, 1)
	assert.Equal(t, FetchStatusDuplicate, res[0].Status)
}

// TestFetchAndRegister_MetaBackfill 幂等重复命中旧版本登记的存量行时回填
// 可读集群名与 APIServer endpoint（仅空缺时生效；登记键 ClusterName 不变——
// 列表"集群名/集群ID"两列数据源）。
func TestFetchAndRegister_MetaBackfill(t *testing.T) {
	validCfg := []byte("apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: https://1.2.3.4\nusers:\n- name: u\n  user:\n    token: t\ncurrent-context: c\ncontexts:\n- name: c\n  context:\n    cluster: c\n    user: u\n")
	gw := &fakeCSGateway{
		clusters:   []AliyunCluster{{ClusterID: "c-1", Name: "prod-cluster", RegionID: "cn-shenzhen", State: "running"}},
		kubeconfig: map[string]string{"c-1": string(validCfg)},
	}
	accounts := &fakeAccountRepo{accounts: []sharedomain.CloudAccount{{
		Name: "集团-阿里云", Provider: sharedomain.CloudProviderAliyun, Status: sharedomain.CloudAccountStatusActive,
		Regions: []string{"cn-shenzhen"},
	}}}
	repo := certtest.NewFakeK8sCredentialRepo()
	creds := NewK8sCredentialService(repo, certtest.NewTestCrypto(t), nil, nil)
	svc := NewK8sCredentialFetchService(gw, accounts, creds)

	// 存量行：仅登记键（无 DisplayName，模拟旧版本拉取的行）
	_, err := creds.AddCluster(context.Background(), AddK8sCredentialInput{ClusterName: "c-1", Kubeconfig: validCfg})
	require.NoError(t, err)

	res := svc.FetchAndRegister(context.Background(), FetchK8sCredentialInput{AccountKey: "集团-阿里云", ClusterIDs: []string{"c-1"}})
	require.Len(t, res, 1)
	assert.Equal(t, FetchStatusDuplicate, res[0].Status)

	cred, err := repo.GetByClusterName(context.Background(), "c-1")
	require.NoError(t, err)
	assert.Equal(t, "prod-cluster", cred.DisplayName)
	assert.Equal(t, "https://1.2.3.4", cred.APIEndpoint) // kubeconfig server 行回填
	assert.Equal(t, "c-1", cred.ClusterName)             // 登记键不被覆盖
}

// TestFetchAndRegister_SkipsBlankIDs 空白集群 ID 跳过（入参防御）。
func TestFetchAndRegister_SkipsBlankIDs(t *testing.T) {
	accounts := &fakeAccountRepo{accounts: []sharedomain.CloudAccount{{
		Name: "acc", Provider: sharedomain.CloudProviderAliyun, Status: sharedomain.CloudAccountStatusActive,
	}}}
	svc := NewK8sCredentialFetchService(&fakeCSGateway{}, accounts,
		NewK8sCredentialService(certtest.NewFakeK8sCredentialRepo(), certtest.NewTestCrypto(t), nil, nil))
	res := svc.FetchAndRegister(context.Background(), FetchK8sCredentialInput{AccountKey: "acc", ClusterIDs: []string{"", "  "}})
	assert.Empty(t, res)
}
