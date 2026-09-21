package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/shared/cloudx/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/domain"
	"github.com/gotomicro/ego/core/elog"
)

// ==================== 测试用 mock ====================

// fakeDiskMetricReader Disk 指标读取 mock(记录入参,按场景回发数据)
type fakeDiskMetricReader struct {
	byDisk         map[int64][]types.DiskMetric
	byAccounts     []types.DiskMetric
	err            error
	gotDiskAccount int64
	gotDiskID      string
	gotDiskDays    int
	gotTopAccounts []int64
	gotTopDays     int
}

func (f *fakeDiskMetricReader) ListByDisk(_ context.Context, accountID int64, diskID string, days int) ([]types.DiskMetric, error) {
	f.gotDiskAccount = accountID
	f.gotDiskID = diskID
	f.gotDiskDays = days
	if f.err != nil {
		return nil, f.err
	}
	// 模拟 DAO 按 disk_id 过滤(同一账号下多块盘各取各的行)
	var out []types.DiskMetric
	for _, m := range f.byDisk[accountID] {
		if m.DiskID == diskID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeDiskMetricReader) ListByAccounts(_ context.Context, accountIDs []int64, days int) ([]types.DiskMetric, error) {
	f.gotTopAccounts = accountIDs
	f.gotTopDays = days
	if f.err != nil {
		return nil, f.err
	}
	return f.byAccounts, nil
}

// diskTodayStr 运营时区今日偏移(缺失日展开依赖当前日期,测试内统一取)
func diskTodayStr(offsetDays int) string {
	return diskToday().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

// ==================== GetDiskMetrics ====================

// 窗口逐日升序展开:缺失日 data_status=missing 不填充假值
func TestGetDiskMetrics_DaysAscendingFillMissing(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{byDisk: map[int64][]types.DiskMetric{
		1: {
			{DiskID: "d-1", AccountID: 1, Date: diskTodayStr(-3), UsagePercent: 55.5, IOPS: 120, Throughput: 8.5},
			{DiskID: "d-1", AccountID: 1, Date: diskTodayStr(-1), UsagePercent: 62.5, IOPS: 240, Throughput: 12.5},
		},
	}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetDiskMetrics(context.Background(), 7, 1, "d-1", 4)
	if err != nil {
		t.Fatalf("GetDiskMetrics: %v", err)
	}
	if len(resp.Days) != 4 {
		t.Fatalf("days len = %d, want 4", len(resp.Days))
	}
	// 升序:today-3, today-2, today-1, today
	if resp.Days[0].Date != diskTodayStr(-3) || resp.Days[3].Date != diskTodayStr(0) {
		t.Fatalf("days not ascending by window: [%s ... %s]", resp.Days[0].Date, resp.Days[3].Date)
	}
	missing := resp.Days[1]
	if missing.DataStatus != DiskDataStatusMissing {
		t.Fatalf("missing day data_status = %q, want missing", missing.DataStatus)
	}
	if missing.UsagePercent != nil || missing.IOPS != nil || missing.Throughput != nil {
		t.Fatalf("missing day 应为 null 不填充假值: %+v", missing)
	}
	if resp.Days[2].IOPS == nil || *resp.Days[2].IOPS != 240 {
		t.Fatalf("iops = %v, want 240", resp.Days[2].IOPS)
	}
	// 最新一天 = 有数据的最大日期
	if resp.Latest == nil || resp.Latest.Date != diskTodayStr(-1) || *resp.Latest.UsagePercent != 62.5 {
		t.Fatalf("latest = %+v", resp.Latest)
	}
	// 均值只含有数据日:usage (55.5+62.5)/2, iops (120+240)/2, throughput (8.5+12.5)/2
	if resp.Average == nil || *resp.Average.UsagePercent != 59 || *resp.Average.IOPS != 180 || *resp.Average.Throughput != 10.5 {
		t.Fatalf("average = %+v, want 59/180/10.5", resp.Average)
	}
}

// qc_status 闭环 + usage_scope 甄别(T2 记录):
//   - busy_share 合法闲盘 0 → data_status=ok,qc_status 原样暴露,0 参与均值;
//   - 口径缺失 0(scope 空)→ data_status=zero_exception,均值跳过。
func TestGetDiskMetrics_ZeroExceptionScopeDiscrimination(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{byDisk: map[int64][]types.DiskMetric{
		1: {
			// d-busy:AWS 全闲盘 busy_share 派生 0.00%(合法)今日 + 有数据昨日
			{DiskID: "d-busy", AccountID: 1, Date: diskTodayStr(0), UsagePercent: 0, UsageScope: types.DiskUsageScopeBusyShare, IOPS: 2, Throughput: 0.1, QcStatus: types.DiskMetricQcZeroException},
			{DiskID: "d-busy", AccountID: 1, Date: diskTodayStr(-1), UsagePercent: 40, UsageScope: types.DiskUsageScopeBusyShare, IOPS: 10, Throughput: 0.5},
			// d-missing:口径缺失 0 异常行(厂商无该盘指标,scope 空)
			{DiskID: "d-missing", AccountID: 1, Date: diskTodayStr(0), UsagePercent: 0, IOPS: 0, Throughput: 0, QcStatus: types.DiskMetricQcZeroException},
		},
	}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	// busy_share 合法闲盘 0:data_status=ok(是真数据),qc_status 仍原样暴露
	resp, err := svc.GetDiskMetrics(context.Background(), 7, 1, "d-busy", 2)
	if err != nil {
		t.Fatalf("GetDiskMetrics d-busy: %v", err)
	}
	idle := resp.Days[1]
	if idle.QcStatus != types.DiskMetricQcZeroException {
		t.Fatalf("busy_share 0 的 qc_status 未原样暴露: %q", idle.QcStatus)
	}
	if idle.DataStatus != DiskDataStatusOK {
		t.Fatalf("busy_share 合法闲盘 0 data_status = %q, want ok(前端不当异常渲染)", idle.DataStatus)
	}
	if idle.UsagePercent == nil || *idle.UsagePercent != 0 {
		t.Fatalf("busy_share 0 应原样暴露 0, got %v", idle.UsagePercent)
	}
	// 均值:合法 0 参与 → (40+0)/2 = 20(不是只算 40)
	if resp.Average == nil || resp.Average.UsagePercent == nil || *resp.Average.UsagePercent != 20 {
		t.Fatalf("average = %+v, want 20(busy_share 合法 0 参与均值)", resp.Average)
	}

	// 口径缺失 0:data_status=zero_exception,均值跳过(不记 0 拉低)
	resp2, err := svc.GetDiskMetrics(context.Background(), 7, 1, "d-missing", 2)
	if err != nil {
		t.Fatalf("GetDiskMetrics d-missing: %v", err)
	}
	bad := resp2.Days[1]
	if bad.DataStatus != DiskDataStatusZeroException || bad.QcStatus != types.DiskMetricQcZeroException {
		t.Fatalf("口径缺失 0 data_status/qc_status = %q/%q, want zero_exception 透传", bad.DataStatus, bad.QcStatus)
	}
	if resp2.Average == nil || resp2.Average.UsagePercent != nil {
		t.Fatalf("口径缺失 0 均值应为 null(不记 0), got %+v", resp2.Average)
	}
}

// 越权:account_id 不属于租户 → ErrDiskAccountNotInTenant(handler 映射 404);
// 租户 A 查不到租户 B 的账号(reader 不被调用)
func TestGetDiskMetrics_AccountNotInTenant(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}, {ID: 2}}}
	reader := &fakeDiskMetricReader{}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	_, err := svc.GetDiskMetrics(context.Background(), 7, 99, "d-1", 30)
	if !errors.Is(err, ErrDiskAccountNotInTenant) {
		t.Fatalf("err = %v, want ErrDiskAccountNotInTenant", err)
	}
	if reader.gotDiskAccount != 0 {
		t.Fatal("越权账号不应查询指标表")
	}
}

// 窗口收敛:days 缺省 30、>90 收敛 90(handler 校验 1~90,service 层兜底收敛)
func TestGetDiskMetrics_DaysClamped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{byDisk: map[int64][]types.DiskMetric{}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetDiskMetrics(context.Background(), 7, 1, "d-1", 0); err != nil {
		t.Fatalf("days=0: %v", err)
	}
	if reader.gotDiskDays != diskDefaultDays {
		t.Fatalf("days=0 → %d, want %d", reader.gotDiskDays, diskDefaultDays)
	}
	if _, err := svc.GetDiskMetrics(context.Background(), 7, 1, "d-1", 365); err != nil {
		t.Fatalf("days=365: %v", err)
	}
	if reader.gotDiskDays != diskMaxDays {
		t.Fatalf("days=365 → %d, want %d", reader.gotDiskDays, diskMaxDays)
	}
}

// 窗口内全缺失:latest/average 均为空值结构,不报错
func TestGetDiskMetrics_AllMissing(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{byDisk: map[int64][]types.DiskMetric{}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetDiskMetrics(context.Background(), 7, 1, "d-none", 3)
	if err != nil {
		t.Fatalf("GetDiskMetrics: %v", err)
	}
	if len(resp.Days) != 3 {
		t.Fatalf("days len = %d, want 3", len(resp.Days))
	}
	if resp.Latest != nil {
		t.Fatalf("全缺失时 latest 应为 nil, got %+v", resp.Latest)
	}
	if resp.Average == nil || resp.Average.UsagePercent != nil || resp.Average.IOPS != nil || resp.Average.Throughput != nil {
		t.Fatalf("全缺失时 average 字段应为 null, got %+v", resp.Average)
	}
}

// ==================== GetTop ====================

// disk_id 去重:共享盘两账号并存按「日期 desc,再使用率 desc」取代表行,不跨账号求和;
// account_id 列表去重升序;均值按每日代表行计算不双计
func TestGetDiskTop_DedupByDiskID(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 2}, {ID: 1}}}
	reader := &fakeDiskMetricReader{byAccounts: []types.DiskMetric{
		// d-shared:账号 2 今日使用率最高(代表行),账号 1 今日较低,账号 1 昨日
		{DiskID: "d-shared", Provider: "aliyun", AccountID: 2, Date: diskTodayStr(0), UsagePercent: 80, IOPS: 800, Throughput: 40},
		{DiskID: "d-shared", Provider: "aliyun", AccountID: 1, Date: diskTodayStr(0), UsagePercent: 30, IOPS: 300, Throughput: 15},
		{DiskID: "d-shared", Provider: "aliyun", AccountID: 1, Date: diskTodayStr(-1), UsagePercent: 60, IOPS: 600, Throughput: 30},
	}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, DiskSortUsagePercent, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 1 || len(resp.Items) != 1 {
		t.Fatalf("total=%d items=%d, want 1/1 (跨账号同盘去重)", resp.Total, len(resp.Items))
	}
	item := resp.Items[0]
	if item.Latest.UsagePercent == nil || *item.Latest.UsagePercent != 80 {
		t.Fatalf("代表行应为「最新日期,同日内使用率最大」行, got %+v", item.Latest)
	}
	if len(item.AccountIDs) != 2 || item.AccountIDs[0] != 1 || item.AccountIDs[1] != 2 {
		t.Fatalf("account_id 列表 = %v, want [1 2]", item.AccountIDs)
	}
	// 均值:每日代表行 = today(80)与 yesterday(60)→ 70
	if item.Average.UsagePercent == nil || *item.Average.UsagePercent != 70 {
		t.Fatalf("average = %+v, want usage 70 (不跨账号双计)", item.Average)
	}
}

// 口径缺失 0 异常行:data_status/qc_status 透传,均值 null;busy_share 合法 0 均值含 0
func TestGetDiskTop_ZeroExceptionScopeRows(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{byAccounts: []types.DiskMetric{
		// d-missing:两日全为口径缺失 0 异常行
		{DiskID: "d-missing", AccountID: 1, Date: diskTodayStr(0), UsagePercent: 0, QcStatus: types.DiskMetricQcZeroException},
		{DiskID: "d-missing", AccountID: 1, Date: diskTodayStr(-1), UsagePercent: 0, QcStatus: types.DiskMetricQcZeroException},
		// d-idle:busy_share 合法闲盘,两日全 0(真数据)
		{DiskID: "d-idle", AccountID: 1, Date: diskTodayStr(0), UsagePercent: 0, UsageScope: types.DiskUsageScopeBusyShare, QcStatus: types.DiskMetricQcZeroException},
	}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 1, 7, DiskSortUsagePercent, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 2 || len(resp.Items) != 2 {
		t.Fatalf("total=%d items=%d, want 2/2 (无数据不记 0 但异常行盘仍在)", resp.Total, len(resp.Items))
	}
	byID := map[string]DiskTopItem{}
	for _, it := range resp.Items {
		byID[it.DiskID] = it
	}
	missing := byID["d-missing"]
	if missing.DataStatus != DiskDataStatusZeroException || missing.QcStatus != types.DiskMetricQcZeroException {
		t.Fatalf("d-missing data_status/qc_status = %q/%q, want zero_exception 透传", missing.DataStatus, missing.QcStatus)
	}
	if missing.Average.UsagePercent != nil {
		t.Fatalf("口径缺失 0 均值应为 null, got %+v", missing.Average)
	}
	idle := byID["d-idle"]
	if idle.DataStatus != DiskDataStatusOK {
		t.Fatalf("d-idle data_status = %q, want ok(busy_share 合法闲盘)", idle.DataStatus)
	}
	if idle.Average.UsagePercent == nil || *idle.Average.UsagePercent != 0 {
		t.Fatalf("d-idle 合法 0 应参与均值得 0, got %+v", idle.Average)
	}
}

// usage_percent 排序用近 N 天均值口径 + 分页:total 为去重后磁盘数
func TestGetDiskTop_SortByAvgUsageAndPaginate(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{byAccounts: []types.DiskMetric{
		// d-a:今日 80 单日 → 均值 80;d-b 两日各 60 → 均值 60;d-c 单日 40 → 均值 40
		{DiskID: "d-a", AccountID: 1, Date: diskTodayStr(0), UsagePercent: 80},
		{DiskID: "d-b", AccountID: 1, Date: diskTodayStr(-1), UsagePercent: 60},
		{DiskID: "d-b", AccountID: 1, Date: diskTodayStr(0), UsagePercent: 60},
		{DiskID: "d-c", AccountID: 1, Date: diskTodayStr(0), UsagePercent: 40},
	}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, DiskSortUsagePercent, 2, 2, 2)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 3 || resp.Page != 2 || resp.PageSize != 2 {
		t.Fatalf("total/page/page_size = %d/%d/%d, want 3/2/2", resp.Total, resp.Page, resp.PageSize)
	}
	if len(resp.Items) != 1 || resp.Items[0].DiskID != "d-c" {
		t.Fatalf("第 2 页应只剩 d-c(均值降序 a>b>c), got %+v", resp.Items)
	}
}

// iops/throughput 排序均用近 N 天均值口径
func TestGetDiskTop_SortByAvgIOPSAndThroughput(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{byAccounts: []types.DiskMetric{
		// d-iops:今日峰值高但均值低(单日 900);d-tp:吞吐大
		{DiskID: "d-iops", AccountID: 1, Date: diskTodayStr(0), IOPS: 900, Throughput: 5},
		{DiskID: "d-tp", AccountID: 1, Date: diskTodayStr(-1), IOPS: 100, Throughput: 50},
		{DiskID: "d-tp", AccountID: 1, Date: diskTodayStr(0), IOPS: 100, Throughput: 50},
	}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	respIOPS, err := svc.GetTop(context.Background(), 7, 0, 7, DiskSortIOPS, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop iops: %v", err)
	}
	// iops 均值 900 > 100 → d-iops 领先(单日峰值即均值,只此一日)
	if respIOPS.Items[0].DiskID != "d-iops" || respIOPS.Items[1].DiskID != "d-tp" {
		t.Fatalf("iops order = [%s %s], want [d-iops d-tp] (均值 900>100)", respIOPS.Items[0].DiskID, respIOPS.Items[1].DiskID)
	}

	respTP, err := svc.GetTop(context.Background(), 7, 0, 7, DiskSortThroughput, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop throughput: %v", err)
	}
	if respTP.Items[0].DiskID != "d-tp" {
		t.Fatalf("throughput order[0] = %s, want d-tp", respTP.Items[0].DiskID)
	}
}

// 越权:top 传入非租户 account_id → ErrDiskAccountNotInTenant(租户隔离)
func TestGetDiskTop_AccountNotInTenant(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetTop(context.Background(), 7, 42, 7, DiskSortUsagePercent, 10, 1, 10); !errors.Is(err, ErrDiskAccountNotInTenant) {
		t.Fatalf("err = %v, want ErrDiskAccountNotInTenant", err)
	}
	if reader.gotTopAccounts != nil {
		t.Fatal("越权账号不应查询指标表")
	}
}

// account_id 缺省 = 全部租户账号(范围收敛为租户集合,天然租户隔离)
func TestGetDiskTop_DefaultScopeAllTenantAccounts(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 2}, {ID: 1}}}
	reader := &fakeDiskMetricReader{}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetTop(context.Background(), 7, 0, 7, DiskSortUsagePercent, 10, 1, 10); err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if len(reader.gotTopAccounts) != 2 || reader.gotTopAccounts[0] != 2 || reader.gotTopAccounts[1] != 1 {
		t.Fatalf("scope = %v, want 租户账号集合 [2 1]", reader.gotTopAccounts)
	}
}

// 租户无账号 → 空结果不报错
func TestGetDiskTop_TenantWithoutAccounts(t *testing.T) {
	repo := &metricQueryAccountRepo{}
	reader := &fakeDiskMetricReader{}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, DiskSortUsagePercent, 10, 1, 10)
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

// top/page_size 越界收敛 + sort 非法回默认
func TestGetDiskTop_BoundsClamped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{byAccounts: []types.DiskMetric{}}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetTop(context.Background(), 7, 0, 7, "bogus", 0, 1, 999); err != nil {
		t.Fatalf("GetTop: %v", err)
	}
}

// DAO 报错向上透传
func TestGetDiskTop_PropagatesError(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeDiskMetricReader{err: errors.New("boom")}
	svc := NewDiskQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetTop(context.Background(), 7, 0, 7, DiskSortUsagePercent, 10, 1, 10); err == nil {
		t.Fatal("DAO 错误应向上透传")
	}
}
