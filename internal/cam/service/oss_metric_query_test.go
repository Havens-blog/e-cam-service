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

// fakeOSSMetricReader OSS 指标读取 mock(记录入参,按场景回发数据)
type fakeOSSMetricReader struct {
	byBucket       map[int64][]types.OSSMetric
	byAccounts     []types.OSSMetric
	err            error
	gotBktAccount  int64
	gotBktName     string
	gotBktDays     int
	gotTopAccounts []int64
	gotTopDays     int
}

func (f *fakeOSSMetricReader) ListByBucket(_ context.Context, accountID int64, bucketName string, days int) ([]types.OSSMetric, error) {
	f.gotBktAccount = accountID
	f.gotBktName = bucketName
	f.gotBktDays = days
	if f.err != nil {
		return nil, f.err
	}
	return f.byBucket[accountID], nil
}

func (f *fakeOSSMetricReader) ListByAccounts(_ context.Context, accountIDs []int64, days int) ([]types.OSSMetric, error) {
	f.gotTopAccounts = accountIDs
	f.gotTopDays = days
	if f.err != nil {
		return nil, f.err
	}
	return f.byAccounts, nil
}

// ossTodayStr 运营时区今日偏移(缺失日展开依赖当前日期,测试内统一取)
func ossTodayStr(offsetDays int) string {
	return ossToday().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

// ==================== GetBucketMetrics ====================

// 窗口逐日升序展开:缺失日 data_status=missing 不填充假值
func TestGetOSSBucketMetrics_DaysAscendingFillMissing(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byBucket: map[int64][]types.OSSMetric{
		1: {
			{BucketName: "bkt-1", AccountID: 1, Date: ossTodayStr(-3), StorageSize: 1000, ObjectCount: 10},
			{BucketName: "bkt-1", AccountID: 1, Date: ossTodayStr(-1), StorageSize: 2000, ObjectCount: 20},
		},
	}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetBucketMetrics(context.Background(), 7, 1, "bkt-1", 4)
	if err != nil {
		t.Fatalf("GetBucketMetrics: %v", err)
	}
	if len(resp.Days) != 4 {
		t.Fatalf("days len = %d, want 4", len(resp.Days))
	}
	// 升序:today-3, today-2, today-1, today
	if resp.Days[0].Date != ossTodayStr(-3) || resp.Days[3].Date != ossTodayStr(0) {
		t.Fatalf("days not ascending by window: [%s ... %s]", resp.Days[0].Date, resp.Days[3].Date)
	}
	missing := resp.Days[1]
	if missing.DataStatus != OSSDataStatusMissing {
		t.Fatalf("missing day data_status = %q, want missing", missing.DataStatus)
	}
	if missing.StorageSize != nil || missing.ObjectCount != nil {
		t.Fatalf("missing day 应为 null 不填充假值: %+v", missing)
	}
	if resp.Days[2].ObjectCount == nil || *resp.Days[2].ObjectCount != 20 {
		t.Fatalf("object_count = %v, want 20", resp.Days[2].ObjectCount)
	}
	// 最新一天 = 有数据的最大日期
	if resp.Latest == nil || resp.Latest.Date != ossTodayStr(-1) || *resp.Latest.StorageSize != 2000 {
		t.Fatalf("latest = %+v", resp.Latest)
	}
	// 均值只含有数据日:(1000+2000)/2, (10+20)/2
	if resp.Average == nil || *resp.Average.StorageSize != 1500 || *resp.Average.ObjectCount != 15 {
		t.Fatalf("average = %+v, want 1500/15", resp.Average)
	}
}

// qc_status 闭环:zero_exception 原样暴露并映射 data_status;
// 均值跳过 storage_size=0 行(不记 0 拉低)
func TestGetOSSBucketMetrics_ZeroExceptionClosedLoop(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byBucket: map[int64][]types.OSSMetric{
		1: {
			{BucketName: "bkt-hw", AccountID: 1, Date: ossTodayStr(-1), StorageSize: 0, ObjectCount: 3, QcStatus: types.OSSMetricQcZeroException},
			{BucketName: "bkt-hw", AccountID: 1, Date: ossTodayStr(0), StorageSize: 1000, ObjectCount: 5},
		},
	}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetBucketMetrics(context.Background(), 7, 1, "bkt-hw", 2)
	if err != nil {
		t.Fatalf("GetBucketMetrics: %v", err)
	}
	zeroDay := resp.Days[0]
	if zeroDay.QcStatus != types.OSSMetricQcZeroException {
		t.Fatalf("qc_status 未原样暴露: %q", zeroDay.QcStatus)
	}
	if zeroDay.DataStatus != OSSDataStatusZeroException {
		t.Fatalf("data_status = %q, want zero_exception", zeroDay.DataStatus)
	}
	if zeroDay.StorageSize == nil || *zeroDay.StorageSize != 0 {
		t.Fatalf("zero_exception 日 storage_size 应原样暴露 0, got %v", zeroDay.StorageSize)
	}
	// 均值只含 storage_size=1000 的行:object 均值 5
	if resp.Average == nil || resp.Average.StorageSize == nil || *resp.Average.StorageSize != 1000 || *resp.Average.ObjectCount != 5 {
		t.Fatalf("average = %+v, want storage 1000 / objects 5(零值异常行不参与)", resp.Average)
	}
}

// 越权:account_id 不属于租户 → ErrOSSAccountNotInTenant(handler 映射 404)
func TestGetOSSBucketMetrics_AccountNotInTenant(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}, {ID: 2}}}
	reader := &fakeOSSMetricReader{}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	_, err := svc.GetBucketMetrics(context.Background(), 7, 99, "bkt-1", 30)
	if !errors.Is(err, ErrOSSAccountNotInTenant) {
		t.Fatalf("err = %v, want ErrOSSAccountNotInTenant", err)
	}
	if reader.gotBktAccount != 0 {
		t.Fatal("越权账号不应查询指标表")
	}
}

// 窗口收敛:days 缺省 30、>90 收敛 90(handler 校验 1~90,service 层兜底收敛)
func TestGetOSSBucketMetrics_DaysClamped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byBucket: map[int64][]types.OSSMetric{}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetBucketMetrics(context.Background(), 7, 1, "bkt-1", 0); err != nil {
		t.Fatalf("days=0: %v", err)
	}
	if reader.gotBktDays != ossDefaultDays {
		t.Fatalf("days=0 → %d, want %d", reader.gotBktDays, ossDefaultDays)
	}
	if _, err := svc.GetBucketMetrics(context.Background(), 7, 1, "bkt-1", 365); err != nil {
		t.Fatalf("days=365: %v", err)
	}
	if reader.gotBktDays != ossMaxDays {
		t.Fatalf("days=365 → %d, want %d", reader.gotBktDays, ossMaxDays)
	}
}

// 窗口内全缺失:latest/average 均为空值结构,不报错
func TestGetOSSBucketMetrics_AllMissing(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byBucket: map[int64][]types.OSSMetric{}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetBucketMetrics(context.Background(), 7, 1, "bkt-none", 3)
	if err != nil {
		t.Fatalf("GetBucketMetrics: %v", err)
	}
	if len(resp.Days) != 3 {
		t.Fatalf("days len = %d, want 3", len(resp.Days))
	}
	if resp.Latest != nil {
		t.Fatalf("全缺失时 latest 应为 nil, got %+v", resp.Latest)
	}
	if resp.Average == nil || resp.Average.StorageSize != nil || resp.Average.ObjectCount != nil {
		t.Fatalf("全缺失时 average 字段应为 null, got %+v", resp.Average)
	}
}

// ==================== GetTop ====================

// bucket_name 去重:两账号并存按「日期 desc,再容量 desc」取代表行,不跨账号求和;
// account_id 列表去重升序;均值按每日代表行计算不双计
func TestGetOSSTop_DedupByBucketName(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 2}, {ID: 1}}}
	reader := &fakeOSSMetricReader{byAccounts: []types.OSSMetric{
		// bkt-shared:账号 2 今日容量最大(代表行),账号 1 今日较小,账号 1 昨日
		{BucketName: "bkt-shared", Provider: "aliyun", AccountID: 2, Date: ossTodayStr(0), StorageSize: 3000, ObjectCount: 300},
		{BucketName: "bkt-shared", Provider: "aliyun", AccountID: 1, Date: ossTodayStr(0), StorageSize: 1000, ObjectCount: 100},
		{BucketName: "bkt-shared", Provider: "aliyun", AccountID: 1, Date: ossTodayStr(-1), StorageSize: 2000, ObjectCount: 200},
	}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, OSSSortStorageSize, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 1 || len(resp.Items) != 1 {
		t.Fatalf("total=%d items=%d, want 1/1 (跨账号同 bucket 去重)", resp.Total, len(resp.Items))
	}
	item := resp.Items[0]
	if item.Latest.StorageSize == nil || *item.Latest.StorageSize != 3000 {
		t.Fatalf("代表行应为「最新日期,同日内容量最大」行, got %+v", item.Latest)
	}
	if len(item.AccountIDs) != 2 || item.AccountIDs[0] != 1 || item.AccountIDs[1] != 2 {
		t.Fatalf("account_id 列表 = %v, want [1 2]", item.AccountIDs)
	}
	// 均值:每日代表行 = today(3000/300)与 yesterday(2000/200)→ 2500 / 250
	if *item.Average.StorageSize != 2500 || *item.Average.ObjectCount != 250 {
		t.Fatalf("average = %+v, want storage 2500 objects 250 (不跨账号双计)", item.Average)
	}
}

// 无数据 bucket 自然跳过:窗口内无行不出现在 Top(不记 0)
func TestGetOSSTop_EmptyBucketsSkipped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byAccounts: []types.OSSMetric{
		{BucketName: "bkt-a", AccountID: 1, Date: ossTodayStr(0), StorageSize: 100, ObjectCount: 5},
	}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, OSSSortStorageSize, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].BucketName != "bkt-a" {
		t.Fatalf("resp = %+v, want 仅 bkt-a(无数据 bucket 不记 0)", resp)
	}
}

// qc_status 异常行:透传 data_status/qc_status;storage_size=0 不参与均值与排序
func TestGetOSSTop_ZeroExceptionRows(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byAccounts: []types.OSSMetric{
		{BucketName: "bkt-hw", AccountID: 1, Date: ossTodayStr(0), StorageSize: 0, ObjectCount: 0, QcStatus: types.OSSMetricQcZeroException},
		{BucketName: "bkt-hw", AccountID: 1, Date: ossTodayStr(-1), StorageSize: 0, ObjectCount: 0, QcStatus: types.OSSMetricQcZeroException},
	}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 1, 7, OSSSortStorageSize, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	item := resp.Items[0]
	if item.DataStatus != OSSDataStatusZeroException || item.QcStatus != types.OSSMetricQcZeroException {
		t.Fatalf("data_status/qc_status = %q/%q, want zero_exception 透传", item.DataStatus, item.QcStatus)
	}
	if item.Latest.StorageSize == nil || *item.Latest.StorageSize != 0 {
		t.Fatalf("zero_exception 代表行 storage_size 应原样为 0, got %+v", item.Latest)
	}
	if item.Average.StorageSize != nil || item.Average.ObjectCount != nil {
		t.Fatalf("零值异常行均值应为 null, got avg=%+v", item.Average)
	}
}

// storage_size 排序用近 N 天均值口径 + 分页:total 为去重后 bucket 数
func TestGetOSSTop_SortStorageSizeByAvgAndPaginate(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byAccounts: []types.OSSMetric{
		// bkt-a:今日 400 但昨日 0 外无数据 → 均值 400;bkt-b 均值 300;bkt-c 均值 200
		{BucketName: "bkt-a", AccountID: 1, Date: ossTodayStr(0), StorageSize: 400, ObjectCount: 4},
		{BucketName: "bkt-b", AccountID: 1, Date: ossTodayStr(-1), StorageSize: 300, ObjectCount: 3},
		{BucketName: "bkt-b", AccountID: 1, Date: ossTodayStr(0), StorageSize: 300, ObjectCount: 3},
		{BucketName: "bkt-c", AccountID: 1, Date: ossTodayStr(0), StorageSize: 200, ObjectCount: 2},
	}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, OSSSortStorageSize, 2, 2, 2)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Total != 3 || resp.Page != 2 || resp.PageSize != 2 {
		t.Fatalf("total/page/page_size = %d/%d/%d, want 3/2/2", resp.Total, resp.Page, resp.PageSize)
	}
	if len(resp.Items) != 1 || resp.Items[0].BucketName != "bkt-c" {
		t.Fatalf("第 2 页应只剩 bkt-c(均值降序 a>b>c), got %+v", resp.Items)
	}
}

// object_count 排序用近 N 天均值口径
func TestGetOSSTop_SortByAvgObjectCount(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byAccounts: []types.OSSMetric{
		// bkt-obj:两日各 500 → 均值 500;bkt-size:storage 大但对象少
		{BucketName: "bkt-size", AccountID: 1, Date: ossTodayStr(0), StorageSize: 9000, ObjectCount: 10},
		{BucketName: "bkt-obj", AccountID: 1, Date: ossTodayStr(-1), StorageSize: 10, ObjectCount: 500},
		{BucketName: "bkt-obj", AccountID: 1, Date: ossTodayStr(0), StorageSize: 10, ObjectCount: 500},
	}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, OSSSortObjectCount, 10, 1, 10)
	if err != nil {
		t.Fatalf("GetTop: %v", err)
	}
	if resp.Items[0].BucketName != "bkt-obj" || resp.Items[1].BucketName != "bkt-size" {
		t.Fatalf("order = [%s %s], want [bkt-obj bkt-size] (object_count 用近 N 天均值)", resp.Items[0].BucketName, resp.Items[1].BucketName)
	}
}

// 越权:top 传入非租户 account_id → ErrOSSAccountNotInTenant
func TestGetOSSTop_AccountNotInTenant(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetTop(context.Background(), 7, 42, 7, OSSSortStorageSize, 10, 1, 10); !errors.Is(err, ErrOSSAccountNotInTenant) {
		t.Fatalf("err = %v, want ErrOSSAccountNotInTenant", err)
	}
	if reader.gotTopAccounts != nil {
		t.Fatal("越权账号不应查询指标表")
	}
}

// 租户无账号 → 空结果不报错
func TestGetOSSTop_TenantWithoutAccounts(t *testing.T) {
	repo := &metricQueryAccountRepo{}
	reader := &fakeOSSMetricReader{}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	resp, err := svc.GetTop(context.Background(), 7, 0, 7, OSSSortStorageSize, 10, 1, 10)
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
func TestGetOSSTop_BoundsClamped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{byAccounts: []types.OSSMetric{}}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetTop(context.Background(), 7, 0, 7, OSSSortStorageSize, 0, 1, 999); err != nil {
		t.Fatalf("GetTop: %v", err)
	}
}

// DAO 报错向上透传
func TestGetOSSTop_PropagatesError(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeOSSMetricReader{err: errors.New("boom")}
	svc := NewOSSQueryService(repo, reader, elog.DefaultLogger)

	if _, err := svc.GetTop(context.Background(), 7, 0, 7, OSSSortStorageSize, 10, 1, 10); err == nil {
		t.Fatal("DAO 错误应向上透传")
	}
}
