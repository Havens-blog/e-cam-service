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

// fakeRDSQuery RDS 指标读取服务 mock(仅 GetRdsMetrics 生效,记录入参)
type fakeRDSQuery struct {
	gotTenant  int64
	gotAccount int64
	gotRDSID   string
	gotDays    int
	resp       *service.RDSMetricsResp
	err        error
}

func (f *fakeRDSQuery) GetRdsMetrics(_ context.Context, tenantID, accountID int64, rdsID string, days int) (*service.RDSMetricsResp, error) {
	f.gotTenant = tenantID
	f.gotAccount = accountID
	f.gotRDSID = rdsID
	f.gotDays = days
	return f.resp, f.err
}

// newRDSMetricsRouter 组装仅含 rds/metrics 路由的测试路由,注入租户 7
func newRDSMetricsRouter(t *testing.T, fake *fakeRDSQuery) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, nil, nil, nil)
	h.SetRDSQueryService(fake)
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	grp := r.Group("/api/v1/cam/assets")
	grp.GET("/rds/metrics", h.GetRDSMetrics)
	return r
}

// 正常路径:参数透传 + locked contract 响应结构 {rds_id, days[]/latest/average}
func TestGetRDSMetrics_OK(t *testing.T) {
	conn := int64(21)
	cpu := 12.3
	fake := &fakeRDSQuery{resp: &service.RDSMetricsResp{
		RdsID: "rm-xxx",
		Days: []service.RDSMetricPoint{{
			Date: "2026-09-01", CPUPercent: &cpu, MemoryPercent: ptrF(45.6), DiskPercent: ptrF(7.8), Connections: &conn,
			DataStatus: service.RDSDataStatusOK,
		}},
		Latest:  &service.RDSMetricSummary{Date: "2026-09-01", CPUPercent: &cpu, MemoryPercent: ptrF(45.6), DiskPercent: ptrF(7.8), Connections: &conn},
		Average: &service.RDSMetricSummary{Date: "", CPUPercent: &cpu, MemoryPercent: ptrF(45.6), DiskPercent: ptrF(7.8), Connections: &conn},
	}}
	r := newRDSMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/rds/metrics?rds_id=rm-xxx&account_id=3&days=14")
	if code != 200 {
		t.Fatalf("status = %d, body=%s", code, body)
	}
	if fake.gotTenant != 7 || fake.gotAccount != 3 || fake.gotRDSID != "rm-xxx" || fake.gotDays != 14 {
		t.Fatalf("service args = tenant %d account %d rds %q days %d",
			fake.gotTenant, fake.gotAccount, fake.gotRDSID, fake.gotDays)
	}
	data := body["data"].(map[string]any)
	if data["rds_id"] != "rm-xxx" {
		t.Fatalf("rds_id = %v", data["rds_id"])
	}
	days := data["days"].([]any)
	first := days[0].(map[string]any)
	if first["date"] != "2026-09-01" || first["data_status"] != "ok" || first["qc_status"] != "" {
		t.Fatalf("days[0] = %v", first)
	}
	if first["cpu_percent"].(float64) != 12.3 || first["memory_percent"].(float64) != 45.6 ||
		first["disk_percent"].(float64) != 7.8 || first["connections"].(float64) != 21 {
		t.Fatalf("days[0] 字段缺失或错值: %v", first)
	}
	latest := data["latest"].(map[string]any)
	if latest["date"] != "2026-09-01" {
		t.Fatalf("latest = %v", latest)
	}
	average := data["average"].(map[string]any)
	if average["date"] != "" {
		t.Fatalf("average.date = %v, want 空串(契约锁定)", average["date"])
	}
	// 契约 7 键检查:days 行对象只含 date/cpu/memory/disk/connections/data_status/qc_status
	for k := range first {
		switch k {
		case "date", "cpu_percent", "memory_percent", "disk_percent", "connections", "data_status", "qc_status":
		default:
			t.Fatalf("days 行出现契约外字段 %q", k)
		}
	}
}

// 缺 rds_id / account_id → 400
func TestGetRDSMetrics_MissingParams(t *testing.T) {
	r := newRDSMetricsRouter(t, &fakeRDSQuery{})
	for _, q := range []string{"", "?account_id=3", "?rds_id=rm-x"} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/rds/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
}

// days 限 1~90:非数字/0/负数/91 → 400;90 → 通过;缺省透传 30
func TestGetRDSMetrics_DaysRange(t *testing.T) {
	fake := &fakeRDSQuery{}
	r := newRDSMetricsRouter(t, fake)
	for _, q := range []string{"?rds_id=rm&account_id=3&days=abc", "?rds_id=rm&account_id=3&days=0", "?rds_id=rm&account_id=3&days=-1", "?rds_id=rm&account_id=3&days=91"} {
		code, _ := doJSON(t, r, "/api/v1/cam/assets/rds/metrics"+q)
		if code != 400 {
			t.Fatalf("query %q status = %d, want 400", q, code)
		}
	}
	code, _ := doJSON(t, r, "/api/v1/cam/assets/rds/metrics?rds_id=rm&account_id=3&days=90")
	if code != 200 {
		t.Fatalf("days=90 status = %d, want 200", code)
	}
	if fake.gotDays != 90 {
		t.Fatalf("days 透传 = %d, want 90", fake.gotDays)
	}
	code, _ = doJSON(t, r, "/api/v1/cam/assets/rds/metrics?rds_id=rm&account_id=3")
	if code != 200 || fake.gotDays != 30 {
		t.Fatalf("缺省 days 应透传 30, status=%d got %d", code, fake.gotDays)
	}
}

// 越权:service 返回 ErrRDSAccountNotInTenant → 404(不泄露账号存在性)
func TestGetRDSMetrics_UnauthorizedAccount404(t *testing.T) {
	fake := &fakeRDSQuery{err: service.ErrRDSAccountNotInTenant}
	r := newRDSMetricsRouter(t, fake)

	code, body := doJSON(t, r, "/api/v1/cam/assets/rds/metrics?rds_id=rm&account_id=99")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
	if body["code"].(float64) != 404002 {
		t.Fatalf("error code = %v, want 404002", body["code"])
	}
}

// 其他错误 → 500
func TestGetRDSMetrics_InternalError500(t *testing.T) {
	fake := &fakeRDSQuery{err: errors.New("boom")}
	r := newRDSMetricsRouter(t, fake)

	code, _ := doJSON(t, r, "/api/v1/cam/assets/rds/metrics?rds_id=rm&account_id=3")
	if code != 500 {
		t.Fatalf("status = %d, want 500", code)
	}
}

// 未装配 RDSQueryService(未调 SetRDSQueryService)→ 500 带明确错误
func TestGetRDSMetrics_ServiceNotWired500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, nil, nil, nil) // 未 SetRDSQueryService
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	grp := r.Group("/api/v1/cam/assets")
	grp.GET("/rds/metrics", h.GetRDSMetrics)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cam/assets/rds/metrics?rds_id=rm&account_id=3", nil))
	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// 路由注册冒烟:静态 /rds/metrics 与既有 /rds/:asset_id 参数路由共存不冲突
// (对齐 disk/nas metrics 先例),注册 panic 即失败;全量路由表下 metrics 可达
func TestRDSRoutes_RegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAssetHandler(nil, nil, nil, nil, nil, nil)
	h.SetRDSQueryService(&fakeRDSQuery{resp: &service.RDSMetricsResp{RdsID: "rm-1", Days: []service.RDSMetricPoint{}}})
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(7))
		c.Next()
	})
	h.RegisterRoutesWithGroup(r.Group("/api/v1/cam/assets"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cam/assets/rds/metrics?rds_id=rm-1&account_id=1", nil))
	if w.Code != 200 {
		t.Fatalf("GET /rds/metrics via full route table status = %d, body=%s", w.Code, w.Body.String())
	}
}
