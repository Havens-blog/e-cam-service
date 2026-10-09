package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cloudx-sdk/types"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// fakeDiskQuery Disk 指标读取服务 mock(仅趋势/Top 生效,记录入参)
type fakeDiskQuery struct {
	gotMetricsTenant  int64
	gotMetricsAccount int64
	gotMetricsDisk    string
	gotMetricsDays    int
	gotTopTenant      int64
	gotTopAccount     int64
	gotTopDays        int
	gotTopSort        string
	gotTopTop         int
	gotTopPage        int
	gotTopPageSize    int
	metricsErr        error
	topErr            error
	metrics           *service.DiskMetricsResp
	top               *service.DiskTopResp
}

func (f *fakeDiskQuery) GetDiskMetrics(_ context.Context, tenantID, accountID int64, diskID string, days int) (*service.DiskMetricsResp, error) {
	f.gotMetricsTenant = tenantID
	f.gotMetricsAccount = accountID
	f.gotMetricsDisk = diskID
	f.gotMetricsDays = days
	return f.metrics, f.metricsErr
}

func (f *fakeDiskQuery) GetTop(_ context.Context, tenantID, accountID int64, days int, sortBy string, top, page, pageSize int) (*service.DiskTopResp, error) {
	f.gotTopTenant = tenantID
	f.gotTopAccount = accountID
	f.gotTopDays = days
	f.gotTopSort = sortBy
	f.gotTopTop = top
	f.gotTopPage = page
	f.gotTopPageSize = pageSize
	return f.top, f.topErr
}

// newDiskMetricsRouter 组装仅含 disk/metrics、disk/top 路由的测试路由,注入租户 7
func newDiskMetricsRouter(t *testing.T, fake *fakeDiskQuery) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, nil, nil, fake)
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	grp := r.Group("/api/v1/cam/assets")
	grp.GET("/disk/metrics", h.GetDiskMetrics)
	grp.GET("/disk/top", h.GetDiskTop)
	return r
}

// metrics 正常路径:参数透传,响应结构 {disk_id, days[], latest, average}
func TestGetDiskMetrics_OK(t *testing.T) {
	fake := &fakeDiskQuery{metrics: &service.DiskMetricsResp{
		DiskID: "d-1",
		Days: []service.DiskMetricPoint{{
			Date: "2026-09-18", UsagePercent: ptrF(55.5), IOPS: ptrF(120), Throughput: ptrF(8.5),
			UsageScope: types.DiskUsageScopeInstanceLevel, DataStatus: service.DiskDataStatusOK,
		}},
		Latest:  &service.DiskMetricSummary{Date: "2026-09-18", UsagePercent: ptrF(55.5), IOPS: ptrF(120), Throughput: ptrF(8.5)},
		Average: &service.DiskMetricSummary{UsagePercent: ptrF(55.5), IOPS: ptrF(120), Throughput: ptrF(8.5)},
	}}
	r := newDiskMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/disk/metrics?disk_id=d-1&account_id=3&days=14")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotMetricsTenant != 7 || fake.gotMetricsAccount != 3 || fake.gotMetricsDisk != "d-1" || fake.gotMetricsDays != 14 {
		t.Fatalf("service args = tenant %d account %d disk %q days %d", fake.gotMetricsTenant, fake.gotMetricsAccount, fake.gotMetricsDisk, fake.gotMetricsDays)
	}
	data := body["data"].(map[string]any)
	if data["disk_id"] != "d-1" {
		t.Fatalf("disk_id = %v", data["disk_id"])
	}
	days := data["days"].([]any)
	first := days[0].(map[string]any)
	if first["date"] != "2026-09-18" || first["data_status"] != "ok" || first["qc_status"] != "" {
		t.Fatalf("days[0] = %v", first)
	}
	if first["usage_percent"].(float64) != 55.5 {
		t.Fatalf("usage_percent = %v", first["usage_percent"])
	}
	if first["usage_scope"] != types.DiskUsageScopeInstanceLevel {
		t.Fatalf("usage_scope = %v (前端甄别 0 值语义依据)", first["usage_scope"])
	}
	if _, has := data["latest"]; !has {
		t.Fatal("响应缺 latest 字段")
	}
	if _, has := data["average"]; !has {
		t.Fatal("响应缺 average 字段")
	}
}

// qc_status 闭环在 JSON 层可见:口径缺失 0 的 zero_exception 原样暴露 + data_status 映射,
// 前端可分辨异常而非当正常空盘
func TestGetDiskMetrics_ZeroExceptionJSON(t *testing.T) {
	fake := &fakeDiskQuery{metrics: &service.DiskMetricsResp{
		DiskID: "d-zero",
		Days: []service.DiskMetricPoint{{
			Date: "2026-09-18", UsagePercent: ptrF(0),
			DataStatus: service.DiskDataStatusZeroException,
			QcStatus:   "zero_exception",
		}},
	}}
	r := newDiskMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/disk/metrics?disk_id=d-zero&account_id=3")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	first := body["data"].(map[string]any)["days"].([]any)[0].(map[string]any)
	if first["usage_percent"].(float64) != 0 || first["data_status"] != "zero_exception" || first["qc_status"] != "zero_exception" {
		t.Fatalf("days[0] = %v (前端须可分辨异常而非当正常空盘)", first)
	}
}

// metrics 缺 disk_id / account_id → 400
func TestGetDiskMetrics_MissingParams(t *testing.T) {
	r := newDiskMetricsRouter(t, &fakeDiskQuery{})
	for _, q := range []string{"", "?account_id=3", "?disk_id=d-1"} {
		code, body := doJSON(t, r, "/api/v1/cam/assets/disk/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400 (body=%v)", q, code, body)
		}
	}
}

// days 限 1~90:非数字/0/负数/91 → 400;90 → 通过
func TestGetDiskMetrics_DaysRange(t *testing.T) {
	r := newDiskMetricsRouter(t, &fakeDiskQuery{})
	for _, q := range []string{"?disk_id=d&account_id=3&days=abc", "?disk_id=d&account_id=3&days=0", "?disk_id=d&account_id=3&days=-1", "?disk_id=d&account_id=3&days=91"} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/disk/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
	code, _ := doJSON(t, r, "/api/v1/cam/assets/disk/metrics?disk_id=d&account_id=3&days=90")
	if code != 200 {
		t.Fatalf("days=90 status = %d, want 200", code)
	}
}

// 越权:service 返回 ErrDiskAccountNotInTenant → 404(不泄露账号存在性)
func TestGetDiskMetrics_UnauthorizedAccount404(t *testing.T) {
	fake := &fakeDiskQuery{metricsErr: service.ErrDiskAccountNotInTenant}
	r := newDiskMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/disk/metrics?disk_id=d&account_id=99")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
	if body["code"].(float64) != 404002 {
		t.Fatalf("error code = %v, want 404002", body["code"])
	}
}

// top 正常路径:参数透传,响应 {total, page, page_size, items[]}
func TestGetDiskTop_OK(t *testing.T) {
	fake := &fakeDiskQuery{top: &service.DiskTopResp{
		Total: 1, Page: 1, PageSize: 10,
		Items: []service.DiskTopItem{{
			DiskID: "d-1", Provider: "aliyun", AccountIDs: []int64{1, 2},
			DataStatus: service.DiskDataStatusOK,
		}},
	}}
	r := newDiskMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/disk/top?account_id=3&days=7&sort=iops&top=20&page=2&page_size=5")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotTopTenant != 7 || fake.gotTopAccount != 3 || fake.gotTopDays != 7 ||
		fake.gotTopSort != "iops" || fake.gotTopTop != 20 || fake.gotTopPage != 2 || fake.gotTopPageSize != 5 {
		t.Fatalf("service args = %+v", fake)
	}
	data := body["data"].(map[string]any)
	if data["total"].(float64) != 1 || data["page"].(float64) != 1 || data["page_size"].(float64) != 10 {
		t.Fatalf("paging fields = %v", data)
	}
	items := data["items"].([]any)
	first := items[0].(map[string]any)
	if first["disk_id"] != "d-1" {
		t.Fatalf("items[0] = %v", first)
	}
	if _, has := first["account_id"]; !has {
		t.Fatal("items 缺 account_id 列表字段")
	}
}

// top 缺省参数:sort=usage_percent, top=10, page=1, page_size=10;account_id 可缺省
func TestGetDiskTop_Defaults(t *testing.T) {
	fake := &fakeDiskQuery{}
	r := newDiskMetricsRouter(t, fake)
	code, _ := doJSON(t, r, "/api/v1/cam/assets/disk/top")
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if fake.gotTopSort != "usage_percent" || fake.gotTopTop != 10 || fake.gotTopPage != 1 || fake.gotTopPageSize != 10 {
		t.Fatalf("defaults = sort %q top %d page %d page_size %d, want usage_percent/10/1/10",
			fake.gotTopSort, fake.gotTopTop, fake.gotTopPage, fake.gotTopPageSize)
	}
}

// top 非法参数:sort 枚举外/top 越界/page_size 越界/负数 → 400
func TestGetDiskTop_InvalidParams(t *testing.T) {
	r := newDiskMetricsRouter(t, &fakeDiskQuery{})
	for _, q := range []string{
		"?sort=bytes",
		"?top=0", "?top=51", "?top=abc",
		"?page=0", "?page=-2",
		"?page_size=0", "?page_size=51",
	} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/disk/top"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
}

// top 越权账号 → 404;其他错误 → 500
func TestGetDiskTop_ErrorMapping(t *testing.T) {
	r := newDiskMetricsRouter(t, &fakeDiskQuery{topErr: service.ErrDiskAccountNotInTenant})
	code, _ := doJSON(t, r, "/api/v1/cam/assets/disk/top?account_id=99")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}

	r2 := newDiskMetricsRouter(t, &fakeDiskQuery{topErr: errors.New("boom")})
	code, _ = doJSON(t, r2, "/api/v1/cam/assets/disk/top")
	if code != 500 {
		t.Fatalf("status = %d, want 500", code)
	}
}

// 路由注册冒烟:静态 /disk/metrics、/disk/top 与既有 /disk、/disk/:asset_id
// 参数路由共存不冲突(对齐 NAS/OSS metrics/top 先例),注册 panic 即失败
func TestDiskRoutes_RegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, nil, nil, &fakeDiskQuery{})
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	h.RegisterRoutesWithGroup(r.Group("/api/v1/cam/assets"))

	for _, url := range []string{
		"/api/v1/cam/assets/disk/metrics?disk_id=d&account_id=1",
		"/api/v1/cam/assets/disk/top",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != 200 {
			t.Fatalf("GET %s via full route table status = %d, body=%s", url, w.Code, w.Body.String())
		}
	}
}
