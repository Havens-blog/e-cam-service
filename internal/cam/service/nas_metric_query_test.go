package service

import (
	"context"
	"errors"
	"testing"

	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// ==================== 测试用 mock ====================

// fakeNASMetricReader NAS 指标读取 mock(记录入参,按场景回发数据)
type fakeNASMetricReader struct {
	byFs           map[int64][]types.NASMetric
	byAccounts     []types.NASMetric
	err            error
	gotFsAccount   int64
	gotFsFsID      string
	gotFsDays      int
	gotTopAccounts []int64
	gotTopDays     int
}

func (f *fakeNASMetricReader) ListByFs(_ context.Context, accountID int64, fsID string, days int) ([]types.NASMetric, error) {
	f.gotFsAccount = accountID
	f.gotFsFsID = fsID
	f.gotFsDays = days
	if f.err != nil {
		return nil, f.err
	}
	return f.byFs[accountID], nil
}

func (f *fakeNASMetricReader) ListByAccounts(_ context.Context, accountIDs []int64, days int) ([]types.NASMetric, error) {
	f.gotTopAccounts = accountIDs
	f.gotTopDays = days
	if f.err != nil {
		return nil, f.err
	}
	return f.byAccounts, nil
}

func newNASQueryService(repo camrepository.CloudAccountRepository, reader NASMetricReader) *NASQueryService {
	return NewNASQueryService(repo, reader, elog.DefaultLogger)
}

// today 返回运营时区今日(缺失日展开依赖当前日期,测试内统一取)
func nasTodayStr(offsetDays int) string {
	return nasToday().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

// ==================== GetFsMetrics ====================

// 窗口逐日升序展开:缺失日 data_status=missing 不填充假值;正常日 utilization 派生
func TestGetFsMetrics_DaysAscendingFillMissing(t *testing.T) {
	// 取昨日与 3 日前两行,中间日(today-2)应缺失
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{byFs: map[int64][]types.NASMetric{
		1: {
			{FsID: "fs-1", AccountID: 1, Date: nasTodayStr(-3), Capacity: 1000, UsedCapacity: 100},
			{FsID: "fs-1", AccountID: 1, Date: nasTodayStr(-1), Capacity: 2000, UsedCapacity: 500},
		},
	}}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetFsMetrics(context.Background(), 7, 1, "fs-1", 4)
	if err != nil {
		t.Fatalf("GetFsMetrics: %v", err)
	}
	if len(resp.Days) != 4 {
		t.Fatalf("days len = %d, want 4", len(resp.Days))
	}
	// 升序:today-3, today-2, today-1, today
	if resp.Days[0].Date != nasTodayStr(-3) || resp.Days[3].Date != nasTodayStr(0) {
		t.Fatalf("days not ascending by window: [%s ... %s]", resp.Days[0].Date, resp.Days[3].Date)
	}
	missing := resp.Days[1]
	if missing.DataStatus != NASDataStatusMissing {
		t.Fatalf("missing day data_status = %q, want missing", missing.DataStatus)
	}
	if missing.Capacity != nil || missing.Used != nil || missing.Utilization != nil {
		t.Fatalf("missing day 应为 null 不填充假值: %+v", missing)
	}
	// utilization 读取时派生:500/2000 = 0.25
	if got := *resp.Days[2].Utilization; got != 0.25 {
		t.Fatalf("utilization = %v, want 0.25", got)
	}
	// 最新一天 = 有数据的最大日期
	if resp.Latest == nil || resp.Latest.Date != nasTodayStr(-1) || *resp.Latest.Capacity != 2000 {
		t.Fatalf("latest = %+v", resp.Latest)
	}
}

// qc_status 闭环:zero_exception 原样暴露并映射 data_status;utilization null;
// 均值跳过 capacity=0 行(不记 0 拉低)
func TestGetFsMetrics_ZeroExceptionClosedLoop(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{byFs: map[int64][]types.NASMetric{
		1: {
			{FsID: "fs-hw", AccountID: 1, Date: nasTodayStr(-1), Capacity: 0, UsedCapacity: 0, QcStatus: types.NASMetricQcZeroException},
			{FsID: "fs-hw", AccountID: 1, Date: nasTodayStr(0), Capacity: 1000, UsedCapacity: 250},
		},
	}}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetFsMetrics(context.Background(), 7, 1, "fs-hw", 2)
	if err != nil {
		t.Fatalf("GetFsMetrics: %v", err)
	}
	zeroDay := resp.Days[0]
	if zeroDay.QcStatus != types.NASMetricQcZeroException {
		t.Fatalf("qc_status 未原样暴露: %q", zeroDay.QcStatus)
	}
	if zeroDay.DataStatus != NASDataStatusZeroException {
		t.Fatalf("data_status = %q, want zero_exception", zeroDay.DataStatus)
	}
	if zeroDay.Utilization != nil {
		t.Fatalf("capacity=0 时 utilization 应为 null, got %v", *zeroDay.Utilization)
	}
	// 均值只含 capacity=1000 的行:used 均值 250,utilization 0.25
	if resp.Average == nil || resp.Average.Capacity == nil || *resp.Average.Capacity != 1000 {
		t.Fatalf("average = %+v, want capacity=1000(零容量行不参与)", resp.Average)
	}
	if *resp.Average.Utilization != 0.25 {
		t.Fatalf("average utilization = %v, want 0.25", *resp.Average.Utilization)
	}
}

// used>capacity:utilization 按 min(used, capacity) 收敛
func TestGetFsMetrics_UsedExceedsCapacityClamped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{byFs: map[int64][]types.NASMetric{
		1: {{FsID: "fs-x", AccountID: 1, Date: nasTodayStr(0), Capacity: 100, UsedCapacity: 150}},
	}}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetFsMetrics(context.Background(), 7, 1, "fs-x", 1)
	if err != nil {
		t.Fatalf("GetFsMetrics: %v", err)
	}
	if got := *resp.Days[0].Utilization; got != 1 {
		t.Fatalf("utilization = %v, want 1 (min 收敛)", got)
	}
}

// 越权:account_id 不属于租户 → ErrNASAccountNotInTenant(handler 映射 404)
func TestGetFsMetrics_AccountNotInTenant(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}, {ID: 2}}}
	reader := &fakeNASMetricReader{}
	svc := newNASQueryService(repo, reader)

	_, err := svc.GetFsMetrics(context.Background(), 7, 99, "fs-1", 30)
	if !errors.Is(err, ErrNASAccountNotInTenant) {
		t.Fatalf("err = %v, want ErrNASAccountNotInTenant", err)
	}
	if reader.gotFsAccount != 0 {
		t.Fatal("越权账号不应查询指标表")
	}
}

// 窗口收敛:days 缺省 30、>90 收敛 90(Hard: days 限 1~90 在 handler 校验,
// service 层兜底收敛)
func TestGetFsMetrics_DaysClamped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{byFs: map[int64][]types.NASMetric{}}
	svc := newNASQueryService(repo, reader)

	if _, err := svc.GetFsMetrics(context.Background(), 7, 1, "fs-1", 0); err != nil {
		t.Fatalf("days=0: %v", err)
	}
	if reader.gotFsDays != nasDefaultDays {
		t.Fatalf("days=0 → %d, want %d", reader.gotFsDays, nasDefaultDays)
	}
	if _, err := svc.GetFsMetrics(context.Background(), 7, 1, "fs-1", 365); err != nil {
		t.Fatalf("days=365: %v", err)
	}
	if reader.gotFsDays != nasMaxDays {
		t.Fatalf("days=365 → %d, want %d", reader.gotFsDays, nasMaxDays)
	}
}

// 窗口内全缺失:latest/average 均为空值结构,不报错
func TestGetFsMetrics_AllMissing(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{byFs: map[int64][]types.NASMetric{}}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetFsMetrics(context.Background(), 7, 1, "fs-none", 3)
	if err != nil {
		t.Fatalf("GetFsMetrics: %v", err)
	}
	if len(resp.Days) != 3 {
		t.Fatalf("days len = %d, want 3", len(resp.Days))
	}
	if resp.Latest != nil {
		t.Fatalf("全缺失时 latest 应为 nil, got %+v", resp.Latest)
	}
	if resp.Average == nil || resp.Average.Capacity != nil || resp.Average.Utilization != nil {
		t.Fatalf("全缺失时 average 字段应为 null, got %+v", resp.Average)
	}
}

// ==================== GetTop ====================

// fs_id 去重:两账号并存取「最新日期,同日内容量最大」代表行,不跨账号求和;
// account_id 列表去重升序;均值按每日代表行计算不双计
func TestGetTop_DedupByFsID(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 2}, {ID: 1}}}
	reader := &fakeNASMetricReader{byAccounts: []types.NASMetric{
		// fs-shared:账号 2 今日容量最大(代表行),账号 1 昨日
		{FsID: "fs-shared", FsName: "共享FS", Provider: "aliyun", AccountID: 2, Date: nasTodayStr(0), Capacity: 3000, UsedCapacity: 900},
		{FsID: "fs-shared", FsName: "共享FS", Provider: "aliyun", AccountID: 1, Date: nasTodayStr(0), Capacity: 1000, UsedCapacity: 100},
		{FsID: "fs-shared", FsName: "共享FS", Provider: "aliyun", AccountID: 1, Date: nasTodayStr(-1), Capacity: 2000, UsedCapacity: 400},
	}}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, NASSortCapacity, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 1 || len(resp.Items) != 1 {
		t.Fatalf("total=%d items=%d, want 1/1 (跨账号同 fs 去重)", resp.Total, len(resp.Items))
	}
	item := resp.Items[0]
	if item.Latest.Capacity == nil || *item.Latest.Capacity != 3000 {
		t.Fatalf("代表行应为同日容量最大行, got %+v", item.Latest)
	}
	if len(item.AccountIDs) != 2 || item.AccountIDs[0] != 1 || item.AccountIDs[1] != 2 {
		t.Fatalf("account_id 列表 = %v, want [1 2]", item.AccountIDs)
	}
	// 均值:每日代表行 = today(3000/900)与 yesterday(2000/400)→ 2500 / 650
	if *item.Average.Capacity != 2500 || *item.Average.Used != 650 {
		t.Fatalf("average = %+v, want capacity 2500 used 650 (不跨账号双计)", item.Average)
	}
}

// capacity=0 异常行不参与均值且不在 Top 排序中冒充高容量
func TestGetTop_ZeroExceptionRows(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{byAccounts: []types.NASMetric{
		{FsID: "fs-hw", AccountID: 1, Date: nasTodayStr(0), Capacity: 0, QcStatus: types.NASMetricQcZeroException},
		{FsID: "fs-hw", AccountID: 1, Date: nasTodayStr(-1), Capacity: 0, QcStatus: types.NASMetricQcZeroException},
	}}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetTop(context.Background(), 7, 1, 7, NASSortCapacity, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	item := resp.Items[0]
	if item.DataStatus != NASDataStatusZeroException || item.QcStatus != types.NASMetricQcZeroException {
		t.Fatalf("data_status/qc_status = %q/%q, want zero_exception 透传", item.DataStatus, item.QcStatus)
	}
	if item.Latest.Utilization != nil || item.Average.Capacity != nil {
		t.Fatalf("零容量行 utilization/均值应为 null, got latest=%+v avg=%+v", item.Latest, item.Average)
	}
}

// capacity 排序 + 分页:total 为去重后 fs 数,分页切片正确
func TestGetTop_SortCapacityAndPaginate(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{byAccounts: []types.NASMetric{
		{FsID: "fs-a", AccountID: 1, Date: nasTodayStr(0), Capacity: 100},
		{FsID: "fs-b", AccountID: 1, Date: nasTodayStr(0), Capacity: 300},
		{FsID: "fs-c", AccountID: 1, Date: nasTodayStr(0), Capacity: 200},
	}}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, NASSortCapacity, 2, 2, 2)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 3 || resp.Page != 2 || resp.PageSize != 2 {
		t.Fatalf("total/page/page_size = %d/%d/%d, want 3/2/2", resp.Total, resp.Page, resp.PageSize)
	}
	if len(resp.Items) != 1 || resp.Items[0].FsID != "fs-a" {
		t.Fatalf("第 2 页应只剩 fs-a, got %+v", resp.Items)
	}
}

// utilization 排序用近 N 天均值口径;无均值(null)排尾部
func TestGetTop_SortByAvgUtilization(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{byAccounts: []types.NASMetric{
		// fs-high:均值 utilization 0.5(两日各 0.5)
		{FsID: "fs-high", AccountID: 1, Date: nasTodayStr(-1), Capacity: 100, UsedCapacity: 50},
		{FsID: "fs-high", AccountID: 1, Date: nasTodayStr(0), Capacity: 100, UsedCapacity: 50},
		// fs-today:仅今日 0.4,均值 0.4
		{FsID: "fs-today", AccountID: 1, Date: nasTodayStr(0), Capacity: 100, UsedCapacity: 40},
		// fs-zero:全零容量异常行,均值 null → 排尾
		{FsID: "fs-zero", AccountID: 1, Date: nasTodayStr(0), Capacity: 0, QcStatus: types.NASMetricQcZeroException},
	}}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, NASSortUtilization, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	got := []string{resp.Items[0].FsID, resp.Items[1].FsID, resp.Items[2].FsID}
	want := []string{"fs-high", "fs-today", "fs-zero"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (utilization 用近 N 天均值)", got, want)
		}
	}
}

// 越权:top 传入非租户 account_id → ErrNASAccountNotInTenant
func TestGetTop_AccountNotInTenant(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{}
	svc := newNASQueryService(repo, reader)

	if _, err := svc.GetTop(context.Background(), 7, 42, 7, NASSortCapacity, 10, 1, 10); !errors.Is(err, ErrNASAccountNotInTenant) {
		t.Fatalf("err = %v, want ErrNASAccountNotInTenant", err)
	}
	if reader.gotTopAccounts != nil {
		t.Fatal("越权账号不应查询指标表")
	}
}

// 租户无账号 → 空结果不报错
func TestGetTop_TenantWithoutAccounts(t *testing.T) {
	repo := &metricQueryAccountRepo{}
	reader := &fakeNASMetricReader{}
	svc := newNASQueryService(repo, reader)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, NASSortCapacity, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 0 || len(resp.Items) != 0 {
		t.Fatalf("resp = %+v, want empty", resp)
	}
	if reader.gotTopAccounts != nil {
		t.Fatal("无账号时不应查询指标表")
	}
}

// top/page_size 越界收敛:top>50→50,page_size>50→50,top<=0→10
func TestGetTop_BoundsClamped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{}
	svc := newNASQueryService(repo, reader)

	if _, err := svc.GetTop(context.Background(), 7, 0, 7, NASSortCapacity, 0, 1, 999); err != nil {
		t.Fatalf("GetTop: %v", err)
	}
}

// DAO 报错向上透传
func TestGetTop_PropagatesError(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeNASMetricReader{err: errors.New("boom")}
	svc := newNASQueryService(repo, reader)

	if _, err := svc.GetTop(context.Background(), 7, 0, 7, NASSortCapacity, 10, 1, 10); err == nil {
		t.Fatal("DAO 错误应向上透传")
	}
}
