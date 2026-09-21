package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// fakeNASQuery NAS 指标读取服务 mock(仅趋势/Top 生效,记录入参)
type fakeNASQuery struct {
	gotMetricsTenant  int64
	gotMetricsAccount int64
	gotMetricsFsID    string
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
	metrics           *service.NASFsMetricsResp
	top               *service.NASTopResp
}

func (f *fakeNASQuery) GetFsMetrics(_ context.Context, tenantID, accountID int64, fsID string, days int) (*service.NASFsMetricsResp, error) {
	f.gotMetricsTenant = tenantID
	f.gotMetricsAccount = accountID
	f.gotMetricsFsID = fsID
	f.gotMetricsDays = days
	return f.metrics, f.metricsErr
}

func (f *fakeNASQuery) GetTop(_ context.Context, tenantID, accountID int64, days int, sortBy string, top, page, pageSize int) (*service.NASTopResp, error) {
	f.gotTopTenant = tenantID
	f.gotTopAccount = accountID
	f.gotTopDays = days
	f.gotTopSort = sortBy
	f.gotTopTop = top
	f.gotTopPage = page
	f.gotTopPageSize = pageSize
	return f.top, f.topErr
}

// newNASMetricsRouter 组装仅含 nas/metrics、nas/top 路由的测试路由,注入租户 7
func newNASMetricsRouter(t *testing.T, fake *fakeNASQuery) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, fake, nil, nil)
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	grp := r.Group("/api/v1/cam/assets")
	grp.GET("/nas/metrics", h.GetNASFsMetrics)
	grp.GET("/nas/top", h.GetNASTop)
	return r
}

// metrics 正常路径:参数透传,响应结构 {fs_id, days[], latest, average}
func TestGetNASFsMetrics_OK(t *testing.T) {
	util := 0.25
	fake := &fakeNASQuery{metrics: &service.NASFsMetricsResp{
		FsID: "fs-1",
		Days: []service.NASMetricPoint{{
			Date: "2026-09-18", Capacity: ptrF(1000), Used: ptrF(250), Utilization: &util,
			DataStatus: service.NASDataStatusOK,
		}},
		Latest:  &service.NASMetricSummary{Date: "2026-09-18", Capacity: ptrF(1000), Used: ptrF(250), Utilization: &util},
		Average: &service.NASMetricSummary{Capacity: ptrF(1000), Used: ptrF(250), Utilization: &util},
	}}
	r := newNASMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/nas/metrics?fs_id=fs-1&account_id=3&days=14")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotMetricsTenant != 7 || fake.gotMetricsAccount != 3 || fake.gotMetricsFsID != "fs-1" || fake.gotMetricsDays != 14 {
		t.Fatalf("service args = tenant %d account %d fs %q days %d", fake.gotMetricsTenant, fake.gotMetricsAccount, fake.gotMetricsFsID, fake.gotMetricsDays)
	}
	data := body["data"].(map[string]any)
	if data["fs_id"] != "fs-1" {
		t.Fatalf("fs_id = %v", data["fs_id"])
	}
	days := data["days"].([]any)
	first := days[0].(map[string]any)
	if first["date"] != "2026-09-18" || first["data_status"] != "ok" || first["qc_status"] != "" {
		t.Fatalf("days[0] = %v", first)
	}
	if first["utilization"].(float64) != 0.25 {
		t.Fatalf("utilization = %v", first["utilization"])
	}
	if _, has := data["latest"]; !has {
		t.Fatal("响应缺 latest 字段")
	}
	if _, has := data["average"]; !has {
		t.Fatal("响应缺 average 字段")
	}
}

// metrics 缺 fs_id / account_id → 400
func TestGetNASFsMetrics_MissingParams(t *testing.T) {
	r := newNASMetricsRouter(t, &fakeNASQuery{})
	for _, q := range []string{"", "?account_id=3", "?fs_id=fs-1"} {
		code, body := doJSON(t, r, "/api/v1/cam/assets/nas/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400 (body=%v)", q, code, body)
		}
	}
}

// days 限 1~90:非数字/0/负数/91 → 400;90 → 通过
func TestGetNASFsMetrics_DaysRange(t *testing.T) {
	r := newNASMetricsRouter(t, &fakeNASQuery{})
	for _, q := range []string{"?fs_id=f&account_id=3&days=abc", "?fs_id=f&account_id=3&days=0", "?fs_id=f&account_id=3&days=-1", "?fs_id=f&account_id=3&days=91"} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/nas/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
	code, _ := doJSON(t, r, "/api/v1/cam/assets/nas/metrics?fs_id=f&account_id=3&days=90")
	if code != 200 {
		t.Fatalf("days=90 status = %d, want 200", code)
	}
}

// 越权:service 返回 ErrNASAccountNotInTenant → 404(不泄露账号存在性)
func TestGetNASFsMetrics_UnauthorizedAccount404(t *testing.T) {
	fake := &fakeNASQuery{metricsErr: service.ErrNASAccountNotInTenant}
	r := newNASMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/nas/metrics?fs_id=f&account_id=99")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
	if body["code"].(float64) != 404002 {
		t.Fatalf("error code = %v, want 404002", body["code"])
	}
}

// top 正常路径:参数透传,响应 {total, page, page_size, items[]}
func TestGetNASTop_OK(t *testing.T) {
	fake := &fakeNASQuery{top: &service.NASTopResp{
		Total: 1, Page: 1, PageSize: 10,
		Items: []service.NASTopItem{{
			FsID: "fs-1", FsName: "共享FS", Provider: "aliyun", AccountIDs: []int64{1, 2},
			DataStatus: service.NASDataStatusOK,
		}},
	}}
	r := newNASMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/nas/top?account_id=3&days=7&sort=utilization&top=20&page=2&page_size=5")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotTopTenant != 7 || fake.gotTopAccount != 3 || fake.gotTopDays != 7 ||
		fake.gotTopSort != "utilization" || fake.gotTopTop != 20 || fake.gotTopPage != 2 || fake.gotTopPageSize != 5 {
		t.Fatalf("service args = %+v", fake)
	}
	data := body["data"].(map[string]any)
	if data["total"].(float64) != 1 || data["page"].(float64) != 1 || data["page_size"].(float64) != 10 {
		t.Fatalf("paging fields = %v", data)
	}
	items := data["items"].([]any)
	first := items[0].(map[string]any)
	if first["fs_id"] != "fs-1" {
		t.Fatalf("items[0] = %v", first)
	}
	if _, has := first["account_id"]; !has {
		t.Fatal("items 缺 account_id 列表字段")
	}
}

// top 缺省参数:sort=capacity, top=10, page=1, page_size=10;account_id 可缺省
func TestGetNASTop_Defaults(t *testing.T) {
	fake := &fakeNASQuery{}
	r := newNASMetricsRouter(t, fake)
	code, _ := doJSON(t, r, "/api/v1/cam/assets/nas/top")
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if fake.gotTopSort != "capacity" || fake.gotTopTop != 10 || fake.gotTopPage != 1 || fake.gotTopPageSize != 10 {
		t.Fatalf("defaults = sort %q top %d page %d page_size %d, want capacity/10/1/10",
			fake.gotTopSort, fake.gotTopTop, fake.gotTopPage, fake.gotTopPageSize)
	}
}

// top 非法参数:sort 枚举外/top 越界/page_size 越界/负数 → 400
func TestGetNASTop_InvalidParams(t *testing.T) {
	r := newNASMetricsRouter(t, &fakeNASQuery{})
	for _, q := range []string{
		"?sort=bytes",
		"?top=0", "?top=51", "?top=abc",
		"?page=0", "?page=-2",
		"?page_size=0", "?page_size=51",
	} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/nas/top"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
}

// top 越权账号 → 404
func TestGetNASTop_UnauthorizedAccount404(t *testing.T) {
	fake := &fakeNASQuery{topErr: service.ErrNASAccountNotInTenant}
	r := newNASMetricsRouter(t, fake)

	code, _ := doJSON(t, r, "/api/v1/cam/assets/nas/top?account_id=99")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}

// 其他错误 → 500
func TestGetNASTop_InternalError500(t *testing.T) {
	fake := &fakeNASQuery{topErr: errors.New("boom")}
	r := newNASMetricsRouter(t, fake)

	code, _ := doJSON(t, r, "/api/v1/cam/assets/nas/top")
	if code != 500 {
		t.Fatalf("status = %d, want 500", code)
	}
}

func ptrF(v float64) *float64 { return &v }

// 路由注册冒烟:静态 /nas/metrics、/nas/top 与既有 /nas/:asset_id 参数路由
// 共存不冲突(对齐 CDN metrics/top 先例),注册 panic 即失败
func TestNASRoutes_RegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, &fakeNASQuery{}, nil, nil)
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	h.RegisterRoutesWithGroup(r.Group("/api/v1/cam/assets"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cam/assets/nas/metrics?fs_id=f&account_id=1", nil))
	if w.Code != 200 {
		t.Fatalf("GET /nas/metrics via full route table status = %d, body=%s", w.Code, w.Body.String())
	}
}
