package service

import (
	"context"
	"errors"
	"testing"

	camdomain "github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camrepository "github.com/Havens-blog/e-cam-service/internal/cam/repository"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cloudx-sdk/domain"
	"github.com/gotomicro/ego/core/elog"
)

// ==================== 测试用 mock ====================

// fakeRDSMetricReader RDS 指标读取 mock(记录入参,按账号+rds_id 回发数据)
type fakeRDSMetricReader struct {
	byRDS      map[int64][]types.RDSMetric
	err        error
	gotAccount int64
	gotRDSID   string
	gotDays    int
}

func (f *fakeRDSMetricReader) ListByRDS(_ context.Context, accountID int64, rdsID string, days int) ([]types.RDSMetric, error) {
	f.gotAccount = accountID
	f.gotRDSID = rdsID
	f.gotDays = days
	if f.err != nil {
		return nil, f.err
	}
	// 模拟 DAO 按 rds_id 过滤(同一账号下多个 RDS 实例各取各的行)
	var out []types.RDSMetric
	for _, m := range f.byRDS[accountID] {
		if m.RdsID == rdsID {
			out = append(out, m)
		}
	}
	return out, nil
}

// rdsInstRepoMock 实例仓储 mock(仅 Search 生效,按租户+账号过滤 rds 资产)
type rdsInstRepoMock struct {
	camrepository.InstanceRepository
	instances []camdomain.Instance
	searchErr error
}

func (m *rdsInstRepoMock) Search(_ context.Context, f camdomain.SearchFilter) ([]camdomain.Instance, int64, error) {
	if m.searchErr != nil {
		return nil, 0, m.searchErr
	}
	var out []camdomain.Instance
	for _, inst := range m.instances {
		if inst.TenantID != f.TenantID || inst.AccountID != f.AccountID {
			continue
		}
		out = append(out, inst)
	}
	// 分页(offset/limit)最小实现:测试规模 < 500,仅裁剪 offset 后行
	total := int64(len(out))
	if f.Offset > 0 && f.Offset < total {
		out = out[f.Offset:]
	}
	if f.Limit > 0 && int64(len(out)) > f.Limit {
		out = out[:f.Limit]
	}
	return out, total, nil
}

// rdsTestInstanceWithStatus 构造带 attributes["status"] 的 RDS 资产实例
func rdsTestInstanceWithStatus(tenantID, accountID int64, rdsID, status string) camdomain.Instance {
	attrs := map[string]interface{}{}
	if status != "" {
		attrs["status"] = status
	}
	return camdomain.Instance{
		ModelUID:   "aliyun_rds",
		AssetID:    rdsID,
		AssetName:  rdsID,
		TenantID:   tenantID,
		AccountID:  accountID,
		Attributes: attrs,
	}
}

// rdsTodayStr 运营时区今日偏移(缺失日展开依赖当前日期,测试内统一取)
func rdsTodayStr(offsetDays int) string {
	return rdsToday().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

func newTestRDSQueryService(repo *metricQueryAccountRepo, instRepo camrepository.InstanceRepository, reader RDSMetricReader) *RDSQueryService {
	return NewRDSQueryService(repo, instRepo, reader, elog.DefaultLogger)
}

// ==================== GetRdsMetrics ====================

// 窗口逐日升序展开:缺失日 data_status=missing 各指标 null 不填充假值;
// latest = 最后有数行,average = 有数行均值
func TestGetRDSMetrics_DaysAscendingFillMissing(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeRDSMetricReader{byRDS: map[int64][]types.RDSMetric{
		1: {
			{RdsID: "rm-1", AccountID: 1, Date: rdsTodayStr(-3), CPUPercent: 12, MemoryPercent: 45, DiskPercent: 7, Connections: 20},
			{RdsID: "rm-1", AccountID: 1, Date: rdsTodayStr(-1), CPUPercent: 14, MemoryPercent: 47, DiskPercent: 8, Connections: 24},
		},
	}}
	svc := newTestRDSQueryService(repo, &rdsInstRepoMock{}, reader)

	resp, err := svc.GetRdsMetrics(context.Background(), 7, 1, "rm-1", 4)
	if err != nil {
		t.Fatalf("GetRdsMetrics: %v", err)
	}
	if resp.RdsID != "rm-1" {
		t.Fatalf("rds_id = %q, want rm-1", resp.RdsID)
	}
	if len(resp.Days) != 4 {
		t.Fatalf("days len = %d, want 4", len(resp.Days))
	}
	// 升序:today-3, today-2, today-1, today
	if resp.Days[0].Date != rdsTodayStr(-3) || resp.Days[3].Date != rdsTodayStr(0) {
		t.Fatalf("days not ascending by window: [%s ... %s]", resp.Days[0].Date, resp.Days[3].Date)
	}
	missing := resp.Days[1]
	if missing.DataStatus != RDSDataStatusMissing {
		t.Fatalf("missing day data_status = %q, want missing", missing.DataStatus)
	}
	if missing.CPUPercent != nil || missing.MemoryPercent != nil || missing.DiskPercent != nil || missing.Connections != nil {
		t.Fatalf("missing day 应为 null 不填充假值: %+v", missing)
	}
	// days[2] = today-1 有数据行;days[3] = today 缺失(不填充)
	d1 := resp.Days[2]
	if d1.CPUPercent == nil || *d1.CPUPercent != 14 || d1.Connections == nil || *d1.Connections != 24 {
		t.Fatalf("days[2] = %+v, want cpu 14 / conn 24", d1)
	}
	if d1.DataStatus != RDSDataStatusOK || d1.QcStatus != "" {
		t.Fatalf("days[2] status = %q/%q, want ok/空", d1.DataStatus, d1.QcStatus)
	}
	// 最新一天 = 有数据的最大日期
	if resp.Latest == nil || resp.Latest.Date != rdsTodayStr(-1) || *resp.Latest.CPUPercent != 14 || *resp.Latest.Connections != 24 {
		t.Fatalf("latest = %+v", resp.Latest)
	}
	// 均值只含有数据日:cpu (12+14)/2, conn (20+24)/2
	if resp.Average == nil || *resp.Average.CPUPercent != 13 || *resp.Average.MemoryPercent != 46 ||
		*resp.Average.DiskPercent != 7.5 || *resp.Average.Connections != 22 {
		t.Fatalf("average = %+v, want 13/46/7.5/22", resp.Average)
	}
	if resp.Average.Date != "" {
		t.Fatalf("average.date = %q, want 空串(仅 latest 携带日期,契约锁定)", resp.Average.Date)
	}
}

// 停用态甄别(locked contract):qc=zero_exception 且实例非停用态 → zero_exception;
// 停用/重启中实例 → ok(四 0 是真停机事实)
func TestGetRDSMetrics_ZeroExceptionVsStopped(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	today := rdsTodayStr(0)
	zeroRow := types.RDSMetric{RdsID: "rm-stopped", AccountID: 1, Date: today,
		CPUPercent: 0, MemoryPercent: 0, DiskPercent: 0, Connections: 0,
		QcStatus: types.RDSMetricQcZeroException}
	reader := &fakeRDSMetricReader{byRDS: map[int64][]types.RDSMetric{1: {zeroRow}}}

	// 停用中 → ok
	svc := newTestRDSQueryService(repo, &rdsInstRepoMock{instances: []camdomain.Instance{
		rdsTestInstanceWithStatus(7, 1, "rm-stopped", types.RDSStatusStopped),
	}}, reader)
	resp, err := svc.GetRdsMetrics(context.Background(), 7, 1, "rm-stopped", 1)
	if err != nil {
		t.Fatalf("GetRdsMetrics: %v", err)
	}
	if resp.Days[0].DataStatus != RDSDataStatusOK || resp.Days[0].QcStatus != types.RDSMetricQcZeroException {
		t.Fatalf("停用实例零异常行应上 data_status=ok、qc 原样暴露: %+v", resp.Days[0])
	}

	// 重启中 → ok
	svc2 := newTestRDSQueryService(repo, &rdsInstRepoMock{instances: []camdomain.Instance{
		rdsTestInstanceWithStatus(7, 1, "rm-stopped", types.RDSStatusRestarting),
	}}, reader)
	resp2, err := svc2.GetRdsMetrics(context.Background(), 7, 1, "rm-stopped", 1)
	if err != nil {
		t.Fatalf("GetRdsMetrics: %v", err)
	}
	if resp2.Days[0].DataStatus != RDSDataStatusOK {
		t.Fatalf("重启中实例零异常行应上 data_status=ok: %+v", resp2.Days[0])
	}

	// running(非停用族)→ zero_exception
	svc3 := newTestRDSQueryService(repo, &rdsInstRepoMock{instances: []camdomain.Instance{
		rdsTestInstanceWithStatus(7, 1, "rm-stopped", types.RDSStatusRunning),
	}}, reader)
	resp3, err := svc3.GetRdsMetrics(context.Background(), 7, 1, "rm-stopped", 1)
	if err != nil {
		t.Fatalf("GetRdsMetrics: %v", err)
	}
	if resp3.Days[0].DataStatus != RDSDataStatusZeroException {
		t.Fatalf("running 实例零异常行应上 data_status=zero_exception: %+v", resp3.Days[0])
	}

	// 资产缺失(非停用态兜底,保守暴露)→ zero_exception
	svc4 := newTestRDSQueryService(repo, &rdsInstRepoMock{}, reader)
	resp4, err := svc4.GetRdsMetrics(context.Background(), 7, 1, "rm-stopped", 1)
	if err != nil {
		t.Fatalf("GetRdsMetrics: %v", err)
	}
	if resp4.Days[0].DataStatus != RDSDataStatusZeroException {
		t.Fatalf("资产缺失应保守暴露 zero_exception: %+v", resp4.Days[0])
	}
}

// 窗口内无任何行:days 全 missing、latest/average 为 null(契约)
func TestGetRDSMetrics_NoRowsAllMissing(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	svc := newTestRDSQueryService(repo, &rdsInstRepoMock{}, &fakeRDSMetricReader{})

	resp, err := svc.GetRdsMetrics(context.Background(), 7, 1, "rm-x", 3)
	if err != nil {
		t.Fatalf("GetRdsMetrics: %v", err)
	}
	if len(resp.Days) != 3 {
		t.Fatalf("days len = %d, want 3", len(resp.Days))
	}
	for _, d := range resp.Days {
		if d.DataStatus != RDSDataStatusMissing {
			t.Fatalf("无行窗口应全 missing: %+v", d)
		}
	}
	if resp.Latest != nil {
		t.Fatalf("无可用行 latest 应为 null, got %+v", resp.Latest)
	}
	if resp.Average != nil {
		t.Fatalf("无可用行 average 应为 null, got %+v", resp.Average)
	}
}

// 越权:account_id ∉ 租户账号集合 → ErrRDSAccountNotInTenant(handler 映射 404)
func TestGetRDSMetrics_AccountNotInTenant(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	svc := newTestRDSQueryService(repo, &rdsInstRepoMock{}, &fakeRDSMetricReader{})

	_, err := svc.GetRdsMetrics(context.Background(), 7, 99, "rm-x", 30)
	if !errors.Is(err, ErrRDSAccountNotInTenant) {
		t.Fatalf("err = %v, want ErrRDSAccountNotInTenant", err)
	}
}

// 参数校验:缺 rds_id / account_id<=0 → 错误
func TestGetRDSMetrics_ParamValidation(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	svc := newTestRDSQueryService(repo, &rdsInstRepoMock{}, &fakeRDSMetricReader{})
	if _, err := svc.GetRdsMetrics(context.Background(), 7, 1, "", 30); err == nil {
		t.Fatal("缺 rds_id 应报错")
	}
	if _, err := svc.GetRdsMetrics(context.Background(), 7, 0, "rm-x", 30); err == nil {
		t.Fatal("account_id<=0 应报错")
	}
}

// 均值口径:零异常行跳过不参与(不记 0 拉低均值);连接数均值取整
func TestGetRDSMetrics_AverageSkipsZeroException(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeRDSMetricReader{byRDS: map[int64][]types.RDSMetric{
		1: {
			{RdsID: "rm-1", AccountID: 1, Date: rdsTodayStr(-1), CPUPercent: 10, MemoryPercent: 40, DiskPercent: 5, Connections: 9},
			{RdsID: "rm-1", AccountID: 1, Date: rdsTodayStr(0), CPUPercent: 0, MemoryPercent: 0, DiskPercent: 0, Connections: 0, QcStatus: types.RDSMetricQcZeroException},
		},
	}}
	svc := newTestRDSQueryService(repo, &rdsInstRepoMock{instances: []camdomain.Instance{
		rdsTestInstanceWithStatus(7, 1, "rm-1", types.RDSStatusRunning),
	}}, reader)

	resp, err := svc.GetRdsMetrics(context.Background(), 7, 1, "rm-1", 2)
	if err != nil {
		t.Fatalf("GetRdsMetrics: %v", err)
	}
	if resp.Average == nil || *resp.Average.CPUPercent != 10 || *resp.Average.Connections != 9 {
		t.Fatalf("零异常行应跳过均值: average = %+v, want cpu 10 / conn 9", resp.Average)
	}
	// latest 取日期最大行(含零异常行,原样暴露)
	if resp.Latest == nil || resp.Latest.Date != rdsTodayStr(0) || *resp.Latest.CPUPercent != 0 {
		t.Fatalf("latest = %+v, want 最新日期行原样", resp.Latest)
	}
}

// 资产枚举失败:保守按非停用态处理(暴露 zero_exception,不因读资产失败掩盖口径问题)
func TestGetRDSMetrics_SearchFailConservative(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeRDSMetricReader{byRDS: map[int64][]types.RDSMetric{
		1: {{RdsID: "rm-1", AccountID: 1, Date: rdsTodayStr(0), CPUPercent: 0, MemoryPercent: 0, DiskPercent: 0, Connections: 0, QcStatus: types.RDSMetricQcZeroException}},
	}}
	svc := newTestRDSQueryService(repo, &rdsInstRepoMock{searchErr: errors.New("mongo down")}, reader)

	resp, err := svc.GetRdsMetrics(context.Background(), 7, 1, "rm-1", 1)
	if err != nil {
		t.Fatalf("资产枚举失败不阻断指标读取: %v", err)
	}
	if resp.Days[0].DataStatus != RDSDataStatusZeroException {
		t.Fatalf("资产枚举失败应保守暴露 zero_exception: %+v", resp.Days[0])
	}
}

// days 归一:1 最小、90 最大、负值/超上限收敛(服务端同口径 normalizeNASDays)
func TestGetRDSMetrics_DaysClamp(t *testing.T) {
	repo := &metricQueryAccountRepo{accounts: []domain.CloudAccount{{ID: 1}}}
	reader := &fakeRDSMetricReader{}
	svc := newTestRDSQueryService(repo, &rdsInstRepoMock{}, reader)

	if _, err := svc.GetRdsMetrics(context.Background(), 7, 1, "rm-x", 1); err != nil {
		t.Fatalf("days=1: %v", err)
	}
	if reader.gotDays != 1 {
		t.Fatalf("days=1 归一后 = %d, want 1", reader.gotDays)
	}
	if _, err := svc.GetRdsMetrics(context.Background(), 7, 1, "rm-x", 999); err != nil {
		t.Fatalf("days=999: %v", err)
	}
	if reader.gotDays != 90 {
		t.Fatalf("days=999 归一后 = %d, want 90", reader.gotDays)
	}
	if _, err := svc.GetRdsMetrics(context.Background(), 7, 1, "rm-x", 0); err != nil {
		t.Fatalf("days=0(负值同): %v", err)
	}
	if reader.gotDays != 30 {
		t.Fatalf("days=0 归一后 = %d, want 默认 30", reader.gotDays)
	}
}
