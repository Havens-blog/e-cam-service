package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/audit/repository/dao"
	"github.com/Havens-blog/e-cam-service/internal/audit/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/Havens-blog/e-common-go/auditlog"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEventDAO 只捕获过滤器与返回桩数据（不连存储）。
type fakeEventDAO struct {
	captured *dao.AuditEventFilter
	events   []auditlog.AuditEvent
	total    int64
}

func (f *fakeEventDAO) List(_ context.Context, filter *dao.AuditEventFilter) ([]auditlog.AuditEvent, error) {
	f.captured = filter
	return f.events, nil
}

func (f *fakeEventDAO) Count(_ context.Context, filter *dao.AuditEventFilter) (int64, error) {
	f.captured = filter
	return f.total, nil
}

func (f *fakeEventDAO) InitIndexes(_ context.Context) error { return nil }

func newEventsTestRouter(t *testing.T, fake *fakeEventDAO) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := service.NewAuditEventService(fake, elog.DefaultLogger)
	h := NewAuditEventHandler(svc, elog.DefaultLogger)
	r := gin.New()
	// 注入租户中间件桩：模拟登录会话已选定租户 3
	r.Use(func(c *gin.Context) {
		c.Set(middleware.TenantIDKey, int64(3))
		c.Next()
	})
	group := r.Group("/api/v1/cam/audit")
	h.RegisterRoutes(group)
	return r
}

func TestAuditEventHandler_ListEvents(t *testing.T) {
	stub := auditlog.AuditEvent{
		ActorType: auditlog.ActorTypeHuman, ActorID: "7", ActorName: "ops",
		At: 1700000000000, TenantID: 3,
		Service: auditlog.ServiceCAM, Domain: auditlog.DomainAudit,
		Action: "delete", ResourceType: "api", ResourceID: "/api/v1/cam/tasks/9",
		Result: auditlog.ResultSuccess,
	}

	t.Run("租户从上下文注入 + 查询参数映射到过滤器", func(t *testing.T) {
		fake := &fakeEventDAO{events: []auditlog.AuditEvent{stub}, total: 1}
		r := newEventsTestRouter(t, fake)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/cam/audit/events?actor_id=7&service=e-cam-service&domain=audit&action=delete&result=failed&start_time=1&end_time=2&offset=5&limit=50&order=asc", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		got := w.Body.String()
		assert.Contains(t, got, `"total":1`)
		assert.Contains(t, got, `"actor_id":"7"`)

		f := fake.captured
		assert.Equal(t, int64(3), f.TenantID, "租户必须来自上下文而非查询参数")
		assert.Equal(t, "7", f.ActorID)
		assert.Equal(t, "e-cam-service", f.Service)
		assert.Equal(t, "audit", f.Domain)
		assert.Equal(t, "delete", f.Action)
		assert.Equal(t, "failed", f.Result)
		require.NotNil(t, f.StartTime)
		assert.Equal(t, int64(1), *f.StartTime)
		require.NotNil(t, f.EndTime)
		assert.Equal(t, int64(2), *f.EndTime)
		assert.Equal(t, int64(5), f.Offset)
		assert.Equal(t, int64(50), f.Limit)
		assert.Equal(t, "asc", f.Order)
	})

	t.Run("limit 超限被钳制到 1000", func(t *testing.T) {
		fake := &fakeEventDAO{total: 0}
		r := newEventsTestRouter(t, fake)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/audit/events?limit=999999", http.NoBody)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, int64(1000), fake.captured.Limit, "单页上限与 audit/logs 同口径（防大查询放大内存带宽）")
	})

	t.Run("租户无法从查询参数伪造（跨租户防泄露）", func(t *testing.T) {
		fake := &fakeEventDAO{total: 0}
		r := newEventsTestRouter(t, fake)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/audit/events?tenant_id=999", http.NoBody)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, int64(3), fake.captured.TenantID, "tenant_id 查询参数必须被忽略")
	})

	t.Run("响应体为 SDK 事件的原样 JSON（envelope 契约）", func(t *testing.T) {
		fake := &fakeEventDAO{events: []auditlog.AuditEvent{stub}, total: 1}
		r := newEventsTestRouter(t, fake)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cam/audit/events", http.NoBody)
		r.ServeHTTP(w, req)

		var body struct {
			Code int `json:"code"`
			Data struct {
				Items []auditlog.AuditEvent `json:"items"`
				Total int64                 `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Len(t, body.Data.Items, 1)
		assert.Equal(t, "e-cam-service", body.Data.Items[0].Service)
		assert.Equal(t, auditlog.DomainAudit, body.Data.Items[0].Domain)
	})
}
