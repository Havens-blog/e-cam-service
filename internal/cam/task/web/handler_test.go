package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// 任务提交归属单测：认证中间件把 JWT 用户注入 gin 上下文（CtxUsernameKey）后，
// 两个提交接口应以真实用户作为 createdBy 落库；上下文无用户名时兜底 "system"。

// stubTaskService 仅覆盖被测的两个提交方法，其余方法由嵌入接口兜底（不会被调用）。
type stubTaskService struct {
	taskservice.TaskService
	submitSyncFn     func(ctx context.Context, params task.SyncAssetsParams, createdBy string) (string, error)
	submitDiscoverFn func(ctx context.Context, params task.DiscoverAssetsParams, createdBy string) (string, error)
}

func (s *stubTaskService) SubmitSyncAssetsTask(ctx context.Context, params task.SyncAssetsParams, createdBy string) (string, error) {
	return s.submitSyncFn(ctx, params, createdBy)
}

func (s *stubTaskService) SubmitDiscoverAssetsTask(ctx context.Context, params task.DiscoverAssetsParams, createdBy string) (string, error) {
	return s.submitDiscoverFn(ctx, params, createdBy)
}

// newTaskTestRouter 构建测试路由；username 非空时模拟认证中间件注入 CtxUsernameKey。
func newTaskTestRouter(t *testing.T, stub *stubTaskService, username string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if username != "" {
			c.Set(middleware.CtxUsernameKey, username)
		}
		c.Next()
	})
	h := NewTaskHandler(stub)
	h.RegisterRoutes(router.Group("/cam"))
	return router
}

// doTaskRequest 发送 JSON 请求并解析 ginx.Result 信封。
func doTaskRequest(t *testing.T, router *gin.Engine, method, path, body string) (int, struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
}) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v, body=%s", err, w.Body.String())
	}
	return w.Code, resp
}

// TestSubmitSyncAssetsTaskCreatedBy 同步任务提交归属：取上下文用户，空则兜底 system
func TestSubmitSyncAssetsTaskCreatedBy(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     string
	}{
		{name: "上下文有用户名则归属该用户", username: "zhangsan", want: "zhangsan"},
		{name: "上下文无用户名兜底 system", username: "", want: "system"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured string
			stub := &stubTaskService{
				submitSyncFn: func(_ context.Context, _ task.SyncAssetsParams, createdBy string) (string, error) {
					captured = createdBy
					return "task-sync-1", nil
				},
			}
			router := newTaskTestRouter(t, stub, tt.username)

			httpCode, resp := doTaskRequest(t, router, http.MethodPost, "/cam/tasks/sync-assets", `{"provider":"aws"}`)

			if httpCode != http.StatusOK {
				t.Fatalf("http status = %d, want 200, body: code=%d msg=%q", httpCode, resp.Code, resp.Msg)
			}
			if resp.Code != 0 {
				t.Fatalf("in-body code = %d, want 0, msg=%q", resp.Code, resp.Msg)
			}
			if captured != tt.want {
				t.Errorf("createdBy = %q, want %q", captured, tt.want)
			}
			if resp.Data.TaskID != "task-sync-1" {
				t.Errorf("task_id = %q, want %q", resp.Data.TaskID, "task-sync-1")
			}
		})
	}
}

// TestSubmitDiscoverAssetsTaskCreatedBy 发现任务提交归属：取上下文用户，空则兜底 system
func TestSubmitDiscoverAssetsTaskCreatedBy(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     string
	}{
		{name: "上下文有用户名则归属该用户", username: "lisi", want: "lisi"},
		{name: "上下文无用户名兜底 system", username: "", want: "system"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured string
			stub := &stubTaskService{
				submitDiscoverFn: func(_ context.Context, _ task.DiscoverAssetsParams, createdBy string) (string, error) {
					captured = createdBy
					return "task-discover-1", nil
				},
			}
			router := newTaskTestRouter(t, stub, tt.username)

			httpCode, resp := doTaskRequest(t, router, http.MethodPost, "/cam/tasks/discover-assets", `{"provider":"alicloud","region":"cn-hangzhou"}`)

			if httpCode != http.StatusOK {
				t.Fatalf("http status = %d, want 200, body: code=%d msg=%q", httpCode, resp.Code, resp.Msg)
			}
			if resp.Code != 0 {
				t.Fatalf("in-body code = %d, want 0, msg=%q", resp.Code, resp.Msg)
			}
			if captured != tt.want {
				t.Errorf("createdBy = %q, want %q", captured, tt.want)
			}
			if resp.Data.TaskID != "task-discover-1" {
				t.Errorf("task_id = %q, want %q", resp.Data.TaskID, "task-discover-1")
			}
		})
	}
}
