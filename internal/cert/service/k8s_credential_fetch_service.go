package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	accountrepo "github.com/Havens-blog/e-cam-service/internal/account/repository"
	certdomain "github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cs"
)

// ---------------------------------------------------------------------
// K8s 集群凭证云端拉取（cert-alb-ingress-managed 第一层配套）：
// ACK DescribeClustersV1 + DescribeClusterUserKubeconfig，凭证登记链路
// （校验/信封加密/内置播种）完整复用 K8sCredentialService.AddCluster。
// ---------------------------------------------------------------------

// AliyunCluster ACK 集群清单条目（DescribeClustersV1 解析字段白名单）。
type AliyunCluster struct {
	ClusterID   string
	Name        string
	RegionID    string
	State       string
	ClusterType string
}

// AliyunCSGateway 阿里云 CS API 窄接口（测试注入；生产实现经 cs.Client）。
type AliyunCSGateway interface {
	// ListClusters 账号全部 ACK 集群。
	ListClusters(ctx context.Context, creds *domain.CloudAccount) ([]AliyunCluster, error)
	// GetKubeconfig 拉取集群 kubeconfig（YAML 明文；privateIP 决定 endpoint 形态）。
	GetKubeconfig(ctx context.Context, creds *domain.CloudAccount, clusterID string, privateIP bool) (string, error)
}

// 拉取在 HTTP 请求生命周期内同步完成（量级 = 集群数 × RTT，秒级），
// 超时由请求 ctx 承载，无需独立编排时限。

// K8sCredentialFetchService 集群凭证云端拉取编排：列集群 / 拉取并登记。
type K8sCredentialFetchService interface {
	// ListAliyunClusters 列出指定阿里云云账号（accountKey=CloudAccount.Name）
	// 名下全部 ACK 集群。
	ListAliyunClusters(ctx context.Context, accountKey string) ([]AliyunCluster, error)
	// FetchAndRegister 批量拉取 kubeconfig 并登记（复用 AddCluster 全链路：
	// 校验失败/重名单/拉取失败逐条记因，不中断批次）。
	FetchAndRegister(ctx context.Context, in FetchK8sCredentialInput) []FetchK8sCredentialResult
}

// FetchK8sCredentialInput 批量拉取登记入参。
type FetchK8sCredentialInput struct {
	AccountKey string   // 云账号（CloudAccount.Name）
	ClusterIDs []string // 勾选的 ACK 集群 ID
	PrivateIP  bool     // true=拉内网 endpoint kubeconfig（e-cam 与集群同网时）
}

// FetchK8sCredentialResult 逐集群拉取登记结果。
type FetchK8sCredentialResult struct {
	ClusterID   string
	ClusterName string
	Status      string // registered / duplicate / failed
	APIEndpoint string
	Reason      string // failed 时静态文案（云端错误细节只进日志）
}

// fetch 结果状态常量。
const (
	FetchStatusRegistered = "registered"
	FetchStatusDuplicate  = "duplicate"
	FetchStatusFailed     = "failed"
)

// 静态失败文案（Hard Rule：云端错误细节不进响应，仅日志）。
const (
	reasonK8sFetchAccountMissing = "ACCOUNT_NOT_FOUND: 云账号不存在或未启用"
	reasonK8sFetchListFailed     = "K8S_CLUSTER_LIST_FAILED: 集群列表获取失败"
	reasonK8sFetchFailed         = "K8S_KUBECONFIG_FETCH_FAILED: kubeconfig 拉取失败"
	reasonK8sFetchEmpty          = "K8S_KUBECONFIG_EMPTY: 云侧未返回 kubeconfig 内容"
	reasonK8sFetchRegisterFailed = "K8S_REGISTER_FAILED: 凭证登记失败"
)

type k8sCredentialFetchService struct {
	csGateway AliyunCSGateway
	accounts  accountrepo.CloudAccountRepository
	creds     K8sCredentialService
}

// NewK8sCredentialFetchService 创建拉取编排服务。
func NewK8sCredentialFetchService(
	csGateway AliyunCSGateway,
	accounts accountrepo.CloudAccountRepository,
	creds K8sCredentialService,
) K8sCredentialFetchService {
	return &k8sCredentialFetchService{csGateway: csGateway, accounts: accounts, creds: creds}
}

// ListAliyunClusters 列集群：accountKey → active 账号 → CS API。
func (s *k8sCredentialFetchService) ListAliyunClusters(ctx context.Context, accountKey string) ([]AliyunCluster, error) {
	creds, err := s.accountByKey(ctx, accountKey)
	if err != nil {
		return nil, err
	}
	if creds == nil {
		return nil, fmt.Errorf("cert: account %q not found", accountKey)
	}
	return s.csGateway.ListClusters(ctx, creds)
}

// FetchAndRegister 批量拉取登记：单集群失败/重名不中断批次（逐条记因）。
func (s *k8sCredentialFetchService) FetchAndRegister(ctx context.Context, in FetchK8sCredentialInput) []FetchK8sCredentialResult {
	creds, err := s.accountByKey(ctx, in.AccountKey)
	if err != nil {
		return batchFailure(in.ClusterIDs, reasonK8sFetchAccountMissing)
	}
	if creds == nil {
		return batchFailure(in.ClusterIDs, reasonK8sFetchAccountMissing)
	}
	// 可读集群名映射（DescribeClustersV1 尽力获取；失败不阻塞拉取，列表列展示为空）
	names := map[string]string{}
	if clusters, err := s.csGateway.ListClusters(ctx, creds); err == nil {
		for _, c := range clusters {
			names[c.ClusterID] = c.Name
		}
	} else {
		slog.Warn("cert k8s credential fetch: cluster name lookup failed",
			slog.String("accountKey", in.AccountKey), slog.Any("err", err))
	}
	results := make([]FetchK8sCredentialResult, 0, len(in.ClusterIDs))
	for _, id := range in.ClusterIDs {
		clusterID := strings.TrimSpace(id)
		if clusterID == "" {
			continue
		}
		results = append(results, s.fetchOne(ctx, creds, clusterID, names[clusterID], in))
	}
	registered, duplicate, failed := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case FetchStatusRegistered:
			registered++
		case FetchStatusDuplicate:
			duplicate++
		default:
			failed++
		}
	}
	slog.Info("cert k8s credential fetch: batch done",
		slog.String("accountKey", in.AccountKey),
		slog.Int("total", len(results)), slog.Int("registered", registered),
		slog.Int("duplicate", duplicate), slog.Int("failed", failed))
	return results
}

// fetchOne 单集群：拉取 → AddCluster（校验/加密/播种复用）。displayName 为
// ACK 可读集群名（登记与幂等回填共用；空串=名录获取失败，仅缺展示列）。
func (s *k8sCredentialFetchService) fetchOne(ctx context.Context, creds *domain.CloudAccount, clusterID, displayName string, in FetchK8sCredentialInput) FetchK8sCredentialResult {
	res := FetchK8sCredentialResult{ClusterID: clusterID}
	cfg, err := s.csGateway.GetKubeconfig(ctx, creds, clusterID, in.PrivateIP)
	if err != nil {
		slog.Error("cert k8s credential fetch: kubeconfig fetch failed",
			slog.String("accountKey", in.AccountKey), slog.String("clusterId", clusterID), slog.Any("err", err))
		return FetchK8sCredentialResult{ClusterID: clusterID, Status: FetchStatusFailed, Reason: reasonK8sFetchFailed}
	}
	if strings.TrimSpace(cfg) == "" {
		return FetchK8sCredentialResult{ClusterID: clusterID, Status: FetchStatusFailed, Reason: reasonK8sFetchEmpty}
	}
	view, err := s.creds.AddCluster(ctx, AddK8sCredentialInput{
		ClusterName: clusterID, // 登记键=ACK clusterId（跨账号唯一）
		DisplayName: displayName,
		Kubeconfig:  []byte(cfg),
		APIEndpoint: kubeconfigServer(cfg),
	})
	if err != nil {
		if isDuplicateClusterErr(err) {
			// 幂等重复：为存量行回填可读集群名（仅空缺时生效，失败不影响结果）
			if err := s.creds.UpdateDisplayNameIfEmpty(ctx, clusterID, displayName); err != nil {
				slog.Warn("cert k8s credential fetch: display name backfill failed",
					slog.String("clusterId", clusterID), slog.Any("err", err))
			}
			return FetchK8sCredentialResult{ClusterID: clusterID, Status: FetchStatusDuplicate, Reason: err.Error()}
		}
		slog.Error("cert k8s credential fetch: register failed",
			slog.String("accountKey", in.AccountKey), slog.String("clusterId", clusterID), slog.Any("err", err))
		return FetchK8sCredentialResult{ClusterID: clusterID, Status: FetchStatusFailed, Reason: reasonK8sFetchRegisterFailed}
	}
	res.ClusterName = view.ClusterName
	res.Status = FetchStatusRegistered
	res.APIEndpoint = view.APIEndpoint
	slog.Info("cert k8s credential fetch: registered",
		slog.String("accountKey", in.AccountKey), slog.String("clusterId", clusterID))
	return res
}

// kubeconfigServer 从 kubeconfig 提取 APIServer endpoint（首个 server: 行，
// 行级解析足够——ACK kubeconfig 结构固定；提取失败返回空串不阻塞登记）。
func kubeconfigServer(cfg string) string {
	for _, line := range strings.Split(cfg, "\n") {
		trimmed := strings.TrimSpace(line)
		if url, ok := strings.CutPrefix(trimmed, "server:"); ok {
			return strings.TrimSpace(url)
		}
	}
	return ""
}

// isDuplicateClusterErr 重名幂等判定（ErrDuplicateClusterName）。
func isDuplicateClusterErr(err error) bool {
	return errors.Is(err, certdomain.ErrDuplicateClusterName)
}

// accountByKey accountKey（CloudAccount.Name）→ active 阿里云账号凭证。
func (s *k8sCredentialFetchService) accountByKey(ctx context.Context, accountKey string) (*domain.CloudAccount, error) {
	list, _, err := s.accounts.List(ctx, domain.CloudAccountFilter{
		Provider: domain.CloudProviderAliyun,
		Status:   domain.CloudAccountStatusActive,
	})
	if err != nil {
		return nil, fmt.Errorf("cert: list aliyun accounts: %w", err)
	}
	for i := range list {
		if list[i].Name == accountKey {
			return &list[i], nil
		}
	}
	return nil, nil
}

// batchFailure 整批同因失败（账号缺失等前置失败）。
func batchFailure(clusterIDs []string, reason string) []FetchK8sCredentialResult {
	out := make([]FetchK8sCredentialResult, 0, len(clusterIDs))
	for _, id := range clusterIDs {
		out = append(out, FetchK8sCredentialResult{ClusterID: id, Status: FetchStatusFailed, Reason: reason})
	}
	return out
}

// primaryRegion 账号主地域（CS API 为全局清单语义，endpoint 地域不影响结果集）。
func primaryRegion(creds *domain.CloudAccount) string {
	if len(creds.Regions) > 0 {
		return creds.Regions[0]
	}
	return "cn-hangzhou"
}

// ---------------------------------------------------------------------
// 生产 CS 网关（cs.Client 薄封装；CS API 响应体无类型化 struct，解析原始 JSON）
// ---------------------------------------------------------------------

// aliyunCSGateway AliyunCSGateway 生产实现。
type aliyunCSGateway struct{}

// NewAliyunCSGateway 创建生产网关。
func NewAliyunCSGateway() AliyunCSGateway { return aliyunCSGateway{} }

// ListClusters 账号全部 ACK 集群（DescribeClustersV1；分页拉全量）。
// 响应体 SDK 无类型化 struct，解析原始 JSON（clusters[].cluster_id/name/
// region_id/state/cluster_type）。
func (aliyunCSGateway) ListClusters(ctx context.Context, creds *domain.CloudAccount) ([]AliyunCluster, error) {
	if creds == nil {
		return nil, fmt.Errorf("cert: aliyun cs list clusters: nil creds")
	}
	client, err := cs.NewClientWithAccessKey(primaryRegion(creds), creds.AccessKeyID, creds.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("cert: aliyun cs client: %w", err)
	}
	var out []AliyunCluster
	page := 1
	for {
		req := cs.CreateDescribeClustersV1Request()
		req.Scheme = "https"
		req.PageSize = requests.NewInteger(50)
		req.PageNumber = requests.NewInteger(page)
		resp, err := client.DescribeClustersV1(req)
		if err != nil {
			return nil, fmt.Errorf("cert: aliyun cs list clusters: %w", err)
		}
		var body struct {
			Clusters []struct {
				ClusterID   string `json:"cluster_id"`
				Name        string `json:"name"`
				RegionID    string `json:"region_id"`
				State       string `json:"state"`
				ClusterType string `json:"cluster_type"`
			} `json:"clusters"`
		}
		if err := json.Unmarshal(resp.GetHttpContentBytes(), &body); err != nil {
			return nil, fmt.Errorf("cert: aliyun cs clusters decode: %w", err)
		}
		for _, c := range body.Clusters {
			out = append(out, AliyunCluster{
				ClusterID: c.ClusterID, Name: c.Name, RegionID: c.RegionID,
				State: c.State, ClusterType: c.ClusterType,
			})
		}
		if len(body.Clusters) < 50 {
			break
		}
		page++
	}
	return out, nil
}

// GetKubeconfig 拉取集群 kubeconfig（DescribeClusterUserKubeconfig）。
// privateIP=true 拉内网 endpoint 形态（e-cam 与集群同网时）。
func (aliyunCSGateway) GetKubeconfig(ctx context.Context, creds *domain.CloudAccount, clusterID string, privateIP bool) (string, error) {
	if creds == nil {
		return "", fmt.Errorf("cert: aliyun cs kubeconfig: nil creds")
	}
	client, err := cs.NewClientWithAccessKey(primaryRegion(creds), creds.AccessKeyID, creds.AccessKeySecret)
	if err != nil {
		return "", fmt.Errorf("cert: aliyun cs client: %w", err)
	}
	req := cs.CreateDescribeClusterUserKubeconfigRequest()
	req.Scheme = "https"
	req.ClusterId = clusterID
	req.PrivateIpAddress = requests.NewBoolean(privateIP)
	resp, err := client.DescribeClusterUserKubeconfig(req)
	if err != nil {
		return "", fmt.Errorf("cert: aliyun cs kubeconfig: %w", err)
	}
	return resp.Config, nil
}
