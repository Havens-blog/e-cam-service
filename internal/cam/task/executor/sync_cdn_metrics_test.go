package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/cam/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/Havens-blog/e-cam-service/pkg/taskx"
)

// ==================== 测试用 mock ====================

// cdnMetricAccountRepo 账号仓储 mock(仅 List/GetByID 生效)
type cdnMetricAccountRepo struct {
	camrepository.CloudAccountRepository
	accounts []domain.CloudAccount
}

func (m *cdnMetricAccountRepo) List(_ context.Context, _ domain.CloudAccountFilter) ([]domain.CloudAccount, int64, error) {
	return m.accounts, int64(len(m.accounts)), nil
}

func (m *cdnMetricAccountRepo) GetByID(_ context.Context, id int64) (domain.CloudAccount, error) {
	for _, a := range m.accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return domain.CloudAccount{}, fmt.Errorf("account not found: %d", id)
}

// cdnMetricDAOMock 指标 DAO mock(记录 upsert 调用)
type cdnMetricDAOMock struct {
	dao.CDNMetricDAO
	upserts []types.CDNMetric
}

func (m *cdnMetricDAOMock) UpsertMetric(_ context.Context, metric types.CDNMetric) error {
	m.upserts = append(m.upserts, metric)
	return nil
}

// cdnMetricTaskRepo 任务仓储 mock(仅 UpdateProgress 生效)
type cdnMetricTaskRepo struct {
	taskx.TaskRepository
}

func (m *cdnMetricTaskRepo) UpdateProgress(_ context.Context, _ string, _ int, _ string) error {
	return nil
}

// metricCapableCDN 支持指标查询的 CDN 适配器 mock
type metricCapableCDN struct {
	instances []types.CDNInstance
	metrics   []types.CDNMetric
	lastStart string
	lastEnd   string
}

func (c *metricCapableCDN) ListInstances(_ context.Context, _ string) ([]types.CDNInstance, error) {
	return c.instances, nil
}
func (c *metricCapableCDN) GetInstance(_ context.Context, _, _ string) (*types.CDNInstance, error) {
	return nil, nil
}
func (c *metricCapableCDN) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.CDNInstance, error) {
	return c.instances, nil
}
func (c *metricCapableCDN) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "online", nil
}
func (c *metricCapableCDN) ListInstancesWithFilter(_ context.Context, _ string, _ *types.CDNInstanceFilter) ([]types.CDNInstance, error) {
	return c.instances, nil
}
func (c *metricCapableCDN) GetDomainMetrics(_ context.Context, domainName, _ string, startDate, endDate string) ([]types.CDNMetric, error) {
	c.lastStart, c.lastEnd = startDate, endDate
	out := make([]types.CDNMetric, 0, len(c.metrics))
	for _, m := range c.metrics {
		copied := m
		copied.Domain = domainName
		out = append(out, copied)
	}
	return out, nil
}

// plainCDN 不支持指标查询的 CDN 适配器 mock(未实现 CDNMetricQuerier,
// 注意不得内嵌 metricCapableCDN,否则提升的方法集会使其"实现"接口)
type plainCDN struct{}

func (c *plainCDN) ListInstances(_ context.Context, _ string) ([]types.CDNInstance, error) {
	return []types.CDNInstance{{DomainName: "x.example.com", Status: "online"}}, nil
}
func (c *plainCDN) GetInstance(_ context.Context, _, _ string) (*types.CDNInstance, error) {
	return nil, nil
}
func (c *plainCDN) ListInstancesByIDs(_ context.Context, _ string, _ []string) ([]types.CDNInstance, error) {
	return nil, nil
}
func (c *plainCDN) GetInstanceStatus(_ context.Context, _, _ string) (string, error) {
	return "online", nil
}
func (c *plainCDN) ListInstancesWithFilter(_ context.Context, _ string, _ *types.CDNInstanceFilter) ([]types.CDNInstance, error) {
	return nil, nil
}

var _ cloudx.CDNAdapter = (*plainCDN)(nil)
var _ cloudx.CDNAdapter = (*metricCapableCDN)(nil)
var _ cloudx.CDNMetricQuerier = (*metricCapableCDN)(nil)

// metricCloudAdapter 支持/不支持指标的 CloudAdapter mock
type metricCloudAdapter struct {
	provider domain.CloudProvider
	cdn      cloudx.CDNAdapter
}

func (m *metricCloudAdapter) GetProvider() domain.CloudProvider           { return m.provider }
func (m *metricCloudAdapter) Asset() cloudx.AssetAdapter                  { return nil }
func (m *metricCloudAdapter) ECS() cloudx.ECSAdapter                      { return nil }
func (m *metricCloudAdapter) SecurityGroup() cloudx.SecurityGroupAdapter  { return nil }
func (m *metricCloudAdapter) Image() cloudx.ImageAdapter                  { return nil }
func (m *metricCloudAdapter) Disk() cloudx.DiskAdapter                    { return nil }
func (m *metricCloudAdapter) Snapshot() cloudx.SnapshotAdapter            { return nil }
func (m *metricCloudAdapter) RDS() cloudx.RDSAdapter                      { return nil }
func (m *metricCloudAdapter) Redis() cloudx.RedisAdapter                  { return nil }
func (m *metricCloudAdapter) MongoDB() cloudx.MongoDBAdapter              { return nil }
func (m *metricCloudAdapter) VPC() cloudx.VPCAdapter                      { return nil }
func (m *metricCloudAdapter) EIP() cloudx.EIPAdapter                      { return nil }
func (m *metricCloudAdapter) VSwitch() cloudx.VSwitchAdapter              { return nil }
func (m *metricCloudAdapter) LB() cloudx.LBAdapter                        { return nil }
func (m *metricCloudAdapter) CDN() cloudx.CDNAdapter                      { return m.cdn }
func (m *metricCloudAdapter) WAF() cloudx.WAFAdapter                      { return nil }
func (m *metricCloudAdapter) DNS() cloudx.DNSAdapter                      { return nil }
func (m *metricCloudAdapter) ENI() cloudx.ENIAdapter                      { return nil }
func (m *metricCloudAdapter) NAS() cloudx.NASAdapter                      { return nil }
func (m *metricCloudAdapter) OSS() cloudx.OSSAdapter                      { return nil }
func (m *metricCloudAdapter) Kafka() cloudx.KafkaAdapter                  { return nil }
func (m *metricCloudAdapter) Elasticsearch() cloudx.ElasticsearchAdapter  { return nil }
func (m *metricCloudAdapter) IAM() cloudx.IAMAdapter                      { return nil }
func (m *metricCloudAdapter) Tag() cloudx.TagAdapter                      { return nil }
func (m *metricCloudAdapter) ECSCreate() cloudx.ECSCreateAdapter          { return nil }
func (m *metricCloudAdapter) ResourceQuery() cloudx.ResourceQueryAdapter  { return nil }
func (m *metricCloudAdapter) ValidateCredentials(_ context.Context) error { return nil }

var _ cloudx.CloudAdapter = (*metricCloudAdapter)(nil)

// 测试用厂商键(全局注册表,用独特前缀避免与其他用例冲突)
const (
	testProviderWithMetrics    = domain.CloudProvider("cdnmetric-yes")
	testProviderWithoutMetrics = domain.CloudProvider("cdnmetric-no")
)

func init() {
	cloudx.RegisterAdapter(testProviderWithMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &metricCloudAdapter{
			provider: testProviderWithMetrics,
			cdn: &metricCapableCDN{
				instances: []types.CDNInstance{
					{DomainName: "live.example.com", Status: "online"},
					{DomainName: "offline.example.com", Status: "offline"}, // 不采集
					{DomainName: "nostatus.example.com", Status: ""},       // 空状态按在线采
				},
				metrics: []types.CDNMetric{{Date: "2026-09-14", Bytes: 100, Bandwidth: 50, HitRate: 0.9}},
			},
		}, nil
	})
	cloudx.RegisterAdapter(testProviderWithoutMetrics, func(account *domain.CloudAccount) (cloudx.CloudAdapter, error) {
		return &metricCloudAdapter{
			provider: testProviderWithoutMetrics,
			cdn:      &plainCDN{},
		}, nil
	})
}

func newTestCDNMetricsExecutor(t *testing.T, accounts []domain.CloudAccount) (*SyncCDNMetricsExecutor, *cdnMetricDAOMock) {
	t.Helper()
	daoMock := &cdnMetricDAOMock{}
	e := NewSyncCDNMetricsExecutor(
		&cdnMetricAccountRepo{accounts: accounts},
		daoMock,
		&cdnMetricTaskRepo{},
		testLogger(),
	)
	return e, daoMock
}

func cdnMetricTestAccount(id int64, provider domain.CloudProvider) domain.CloudAccount {
	return domain.CloudAccount{
		ID:              id,
		Name:            "acc-" + string(provider),
		Provider:        provider,
		Status:          domain.CloudAccountStatusActive,
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
	}
}

// 跳过不实现 CDNMetricQuerier 的厂商:不写入、不报错
func TestSyncCDNMetrics_SkipsProviderWithoutMetricSupport(t *testing.T) {
	e, daoMock := newTestCDNMetricsExecutor(t, []domain.CloudAccount{
		cdnMetricTestAccount(1, testProviderWithoutMetrics),
	})
	task := &taskx.Task{ID: "t1", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(daoMock.upserts) != 0 {
		t.Fatalf("upserts = %d, want 0", len(daoMock.upserts))
	}
	skipped, _ := task.Result["no_metric_support"].([]string)
	if len(skipped) != 1 || skipped[0] != string(testProviderWithoutMetrics) {
		t.Fatalf("no_metric_support = %v", task.Result["no_metric_support"])
	}
}

// 正常采集:在线域名逐日 upsert,账号/厂商回填,离线域名跳过
func TestSyncCDNMetrics_CollectsActiveDomains(t *testing.T) {
	e, daoMock := newTestCDNMetricsExecutor(t, []domain.CloudAccount{
		cdnMetricTestAccount(2, testProviderWithMetrics),
	})
	task := &taskx.Task{ID: "t2", Params: map[string]any{"days": 1}}
	if err := e.Execute(context.Background(), task); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// 2 个可采域名 × 1 天 = 2 条
	if len(daoMock.upserts) != 2 {
		t.Fatalf("upserts = %d, want 2: %+v", len(daoMock.upserts), daoMock.upserts)
	}
	for _, m := range daoMock.upserts {
		if m.AccountID != 2 || m.Provider != string(testProviderWithMetrics) {
			t.Fatalf("metric not enriched: %+v", m)
		}
		if m.Domain == "offline.example.com" {
			t.Fatalf("offline domain should be skipped: %+v", m)
		}
	}
	if got := task.Result["metrics_total"]; got != 2 {
		t.Fatalf("metrics_total = %v, want 2", got)
	}
}

// GetDomainMetrics 收到的区间与任务参数一致(days 展开)
func TestSyncCDNMetrics_PassesDateRange(t *testing.T) {
	e, _ := newTestCDNMetricsExecutor(t, []domain.CloudAccount{
		cdnMetricTestAccount(3, testProviderWithMetrics),
	})
	account := cdnMetricTestAccount(3, testProviderWithMetrics)
	written, skipped, err := e.collectAccount(context.Background(), &account, "2026-09-13", "2026-09-15")
	if err != nil {
		t.Fatalf("collectAccount: %v", err)
	}
	if skipped {
		t.Fatal("should not skip metric-capable provider")
	}
	adapter, _ := e.cloudxFactory.CreateAdapter(&account)
	cdn := adapter.CDN().(*metricCapableCDN)
	if cdn.lastStart != "2026-09-13" || cdn.lastEnd != "2026-09-15" {
		t.Fatalf("date range = %s ~ %s", cdn.lastStart, cdn.lastEnd)
	}
	_ = written
}

// 账号级互斥:同账号并发采集只放行一个
func TestSyncCDNMetrics_AccountMutex(t *testing.T) {
	e, _ := newTestCDNMetricsExecutor(t, nil)
	if !e.tryAcquireAccount(1, "task-a") {
		t.Fatal("first acquire should succeed")
	}
	if e.tryAcquireAccount(1, "task-b") {
		t.Fatal("second acquire should fail")
	}
	if !e.tryAcquireAccount(1, "task-a") {
		t.Fatal("re-entrant acquire by owner should succeed")
	}
	e.releaseAccount(1, "task-a")
	if e.tryAcquireAccount(1, "task-c") != true {
		t.Fatal("acquire after release should succeed")
	}
	e.releaseAccount(1, "task-c")
	// 释放非持有者不误删
	e.tryAcquireAccount(2, "task-d")
	e.releaseAccount(2, "task-e")
	if _, busy := e.syncingNow[2]; !busy {
		t.Fatal("release by non-owner should not clear owner")
	}
}

// days 参数边界:默认 1 天、上限收敛
func TestSyncCDNMetrics_DaysBounds(t *testing.T) {
	if defaultCollectDays != 1 {
		t.Fatalf("defaultCollectDays = %d", defaultCollectDays)
	}
	if maxCollectDays != 31 {
		t.Fatalf("maxCollectDays = %d", maxCollectDays)
	}
	// 采集窗口含今日且符合运营时区
	today := time.Now().In(cdnMetricsCSTZone).Format("2006-01-02")
	yesterday := time.Now().In(cdnMetricsCSTZone).AddDate(0, 0, -30).Format("2006-01-02")
	start, end := cdnMetricsDateRange(31)
	if end != today || start != yesterday {
		t.Fatalf("cdnMetricsDateRange(31) = %s ~ %s, want %s ~ %s", start, end, yesterday, today)
	}
}
