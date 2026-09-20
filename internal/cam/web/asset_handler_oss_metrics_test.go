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

// fakeOSSQuery OSS 指标读取服务 mock(仅趋势/Top 生效,记录入参)
type fakeOSSQuery struct {
	gotMetricsTenant  int64
	gotMetricsAccount int64
	gotMetricsBkt     string
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
	metrics           *service.OSSBucketMetricsResp
	top               *service.OSSTopResp
}

func (f *fakeOSSQuery) GetBucketMetrics(_ context.Context, tenantID, accountID int64, bucketName string, days int) (*service.OSSBucketMetricsResp, error) {
	f.gotMetricsTenant = tenantID
	f.gotMetricsAccount = accountID
	f.gotMetricsBkt = bucketName
	f.gotMetricsDays = days
	return f.metrics, f.metricsErr
}

func (f *fakeOSSQuery) GetTop(_ context.Context, tenantID, accountID int64, days int, sortBy string, top, page, pageSize int) (*service.OSSTopResp, error) {
	f.gotTopTenant = tenantID
	f.gotTopAccount = accountID
	f.gotTopDays = days
	f.gotTopSort = sortBy
	f.gotTopTop = top
	f.gotTopPage = page
	f.gotTopPageSize = pageSize
	return f.top, f.topErr
}

// newOSSMetricsRouter 组装仅含 oss/metrics、oss/top 路由的测试路由,注入租户 7
func newOSSMetricsRouter(t *testing.T, fake *fakeOSSQuery) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, nil, fake)
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	grp := r.Group("/api/v1/cam/assets")
	grp.GET("/oss/metrics", h.GetOSSBucketMetrics)
	grp.GET("/oss/top", h.GetOSSTop)
	return r
}

// metrics 正常路径:参数透传,响应结构 {bucket_name, days[], latest, average}
func TestGetOSSBucketMetrics_OK(t *testing.T) {
	fake := &fakeOSSQuery{metrics: &service.OSSBucketMetricsResp{
		BucketName: "bkt-1",
		Days: []service.OSSMetricPoint{{
			Date: "2026-09-18", StorageSize: ptrF(1000), ObjectCount: ptrI(42),
			DataStatus: service.OSSDataStatusOK,
		}},
		Latest:  &service.OSSMetricSummary{Date: "2026-09-18", StorageSize: ptrF(1000), ObjectCount: ptrF(42)},
		Average: &service.OSSMetricSummary{StorageSize: ptrF(1000), ObjectCount: ptrF(42)},
	}}
	r := newOSSMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/oss/metrics?bucket_name=bkt-1&account_id=3&days=14")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotMetricsTenant != 7 || fake.gotMetricsAccount != 3 || fake.gotMetricsBkt != "bkt-1" || fake.gotMetricsDays != 14 {
		t.Fatalf("service args = tenant %d account %d bucket %q days %d", fake.gotMetricsTenant, fake.gotMetricsAccount, fake.gotMetricsBkt, fake.gotMetricsDays)
	}
	data := body["data"].(map[string]any)
	if data["bucket_name"] != "bkt-1" {
		t.Fatalf("bucket_name = %v", data["bucket_name"])
	}
	days := data["days"].([]any)
	first := days[0].(map[string]any)
	if first["date"] != "2026-09-18" || first["data_status"] != "ok" || first["qc_status"] != "" {
		t.Fatalf("days[0] = %v", first)
	}
	if first["object_count"].(float64) != 42 {
		t.Fatalf("object_count = %v", first["object_count"])
	}
	if _, has := data["latest"]; !has {
		t.Fatal("响应缺 latest 字段")
	}
	if _, has := data["average"]; !has {
		t.Fatal("响应缺 average 字段")
	}
}

// qc_status 闭环在 JSON 层可见:zero_exception 原样暴露 + data_status 映射
func TestGetOSSBucketMetrics_ZeroExceptionJSON(t *testing.T) {
	fake := &fakeOSSQuery{metrics: &service.OSSBucketMetricsResp{
		BucketName: "bkt-zero",
		Days: []service.OSSMetricPoint{{
			Date: "2026-09-18", StorageSize: ptrF(0),
			DataStatus: service.OSSDataStatusZeroException,
			QcStatus:   "zero_exception",
		}},
	}}
	r := newOSSMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/oss/metrics?bucket_name=bkt-zero&account_id=3")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	first := body["data"].(map[string]any)["days"].([]any)[0].(map[string]any)
	if first["storage_size"].(float64) != 0 || first["data_status"] != "zero_exception" || first["qc_status"] != "zero_exception" {
		t.Fatalf("days[0] = %v (前端须可分辨异常而非当正常空桶)", first)
	}
}

// metrics 缺 bucket_name / account_id → 400
func TestGetOSSBucketMetrics_MissingParams(t *testing.T) {
	r := newOSSMetricsRouter(t, &fakeOSSQuery{})
	for _, q := range []string{"", "?account_id=3", "?bucket_name=bkt-1"} {
		code, body := doJSON(t, r, "/api/v1/cam/assets/oss/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400 (body=%v)", q, code, body)
		}
	}
}

// days 限 1~90:非数字/0/负数/91 → 400;90 → 通过
func TestGetOSSBucketMetrics_DaysRange(t *testing.T) {
	r := newOSSMetricsRouter(t, &fakeOSSQuery{})
	for _, q := range []string{"?bucket_name=b&account_id=3&days=abc", "?bucket_name=b&account_id=3&days=0", "?bucket_name=b&account_id=3&days=-1", "?bucket_name=b&account_id=3&days=91"} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/oss/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
	code, _ := doJSON(t, r, "/api/v1/cam/assets/oss/metrics?bucket_name=b&account_id=3&days=90")
	if code != 200 {
		t.Fatalf("days=90 status = %d, want 200", code)
	}
}

// 越权:service 返回 ErrOSSAccountNotInTenant → 404(不泄露账号存在性)
func TestGetOSSBucketMetrics_UnauthorizedAccount404(t *testing.T) {
	fake := &fakeOSSQuery{metricsErr: service.ErrOSSAccountNotInTenant}
	r := newOSSMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/oss/metrics?bucket_name=b&account_id=99")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
	if body["code"].(float64) != 404002 {
		t.Fatalf("error code = %v, want 404002", body["code"])
	}
}

// top 正常路径:参数透传,响应 {total, page, page_size, items[]}
func TestGetOSSTop_OK(t *testing.T) {
	fake := &fakeOSSQuery{top: &service.OSSTopResp{
		Total: 1, Page: 1, PageSize: 10,
		Items: []service.OSSTopItem{{
			BucketName: "bkt-1", Provider: "aliyun", AccountIDs: []int64{1, 2},
			DataStatus: service.OSSDataStatusOK,
		}},
	}}
	r := newOSSMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/oss/top?account_id=3&days=7&sort=object_count&top=20&page=2&page_size=5")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotTopTenant != 7 || fake.gotTopAccount != 3 || fake.gotTopDays != 7 ||
		fake.gotTopSort != "object_count" || fake.gotTopTop != 20 || fake.gotTopPage != 2 || fake.gotTopPageSize != 5 {
		t.Fatalf("service args = %+v", fake)
	}
	data := body["data"].(map[string]any)
	if data["total"].(float64) != 1 || data["page"].(float64) != 1 || data["page_size"].(float64) != 10 {
		t.Fatalf("paging fields = %v", data)
	}
	items := data["items"].([]any)
	first := items[0].(map[string]any)
	if first["bucket_name"] != "bkt-1" {
		t.Fatalf("items[0] = %v", first)
	}
	if _, has := first["account_id"]; !has {
		t.Fatal("items 缺 account_id 列表字段")
	}
}

// top 缺省参数:sort=storage_size, top=10, page=1, page_size=10;account_id 可缺省
func TestGetOSSTop_Defaults(t *testing.T) {
	fake := &fakeOSSQuery{}
	r := newOSSMetricsRouter(t, fake)
	code, _ := doJSON(t, r, "/api/v1/cam/assets/oss/top")
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if fake.gotTopSort != "storage_size" || fake.gotTopTop != 10 || fake.gotTopPage != 1 || fake.gotTopPageSize != 10 {
		t.Fatalf("defaults = sort %q top %d page %d page_size %d, want storage_size/10/1/10",
			fake.gotTopSort, fake.gotTopTop, fake.gotTopPage, fake.gotTopPageSize)
	}
}

// top 非法参数:sort 枚举外/top 越界/page_size 越界/负数 → 400
func TestGetOSSTop_InvalidParams(t *testing.T) {
	r := newOSSMetricsRouter(t, &fakeOSSQuery{})
	for _, q := range []string{
		"?sort=bytes",
		"?top=0", "?top=51", "?top=abc",
		"?page=0", "?page=-2",
		"?page_size=0", "?page_size=51",
	} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/oss/top"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
}

// top 越权账号 → 404;其他错误 → 500
func TestGetOSSTop_ErrorMapping(t *testing.T) {
	r := newOSSMetricsRouter(t, &fakeOSSQuery{topErr: service.ErrOSSAccountNotInTenant})
	code, _ := doJSON(t, r, "/api/v1/cam/assets/oss/top?account_id=99")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}

	r2 := newOSSMetricsRouter(t, &fakeOSSQuery{topErr: errors.New("boom")})
	code, _ = doJSON(t, r2, "/api/v1/cam/assets/oss/top")
	if code != 500 {
		t.Fatalf("status = %d, want 500", code)
	}
}

// 路由注册冒烟:静态 /oss/metrics、/oss/top 与既有 /oss/:asset_id 参数路由
// 共存不冲突(对齐 NAS metrics/top 先例),注册 panic 即失败
func TestOSSRoutes_RegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, nil, &fakeOSSQuery{})
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	h.RegisterRoutesWithGroup(r.Group("/api/v1/cam/assets"))

	for _, url := range []string{
		"/api/v1/cam/assets/oss/metrics?bucket_name=b&account_id=1",
		"/api/v1/cam/assets/oss/top",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != 200 {
			t.Fatalf("GET %s via full route table status = %d, body=%s", url, w.Code, w.Body.String())
		}
	}
}

func ptrI(v int64) *int64 { return &v }
