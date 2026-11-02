package web

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Havens-blog/e-common-go/taskx"

	"github.com/Havens-blog/e-cam-service/internal/cam/task"
	taskservice "github.com/Havens-blog/e-cam-service/internal/cam/task/service"
)

// leakStubTaskService 只实现 ListTasks（其余由内嵌接口占位），用于验证直接写响应的路径。
type leakStubTaskService struct {
	taskservice.TaskService
	err error
}

func (s *leakStubTaskService) ListTasks(_ context.Context, _ taskx.TaskFilter) ([]taskx.Task, int64, error) {
	return nil, 0, s.err
}

const leakRawErr = "mongo: no documents in result"

// 回归（2026-11-02）：task 模块曾把 err.Error() 写进响应体（5 处）。此前 2 处还是
// `return ErrorResultWithMsg(errs.SystemError, err.Error()), nil` —— 因返回 nil，
// ginx 包装器判为"无错误" ⇒ **HTTP 200 + 错误码**（依据 e-common-go/ginx/wrap.go）。
func TestTaskHandlerDoesNotLeakRawError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1) 直接写响应的路径（ListTasks）：应为 500，且响应体不含原始错误文本
	h := NewTaskHandler(&leakStubTaskService{err: errors.New(leakRawErr)})
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("GET", "/api/v1/cam/tasks?offset=0&limit=5", nil)
	h.ListTasks(ctx)

	if w.Code != 500 {
		t.Fatalf("列表失败应返回 500，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "mongo") || strings.Contains(body, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %s", body)
	}

	// 2) 经 ginx 包装器的返回形态：处理器必须返回非 nil error（框架据此给 500）
	//    说明：此处**不通过路由**断言 —— WrapBody 在"绑定失败"时只记日志、**不写任何响应**
	//    （实测空 body + 200），那是 e-common-go/ginx 的独立问题，不属本 PR 范围，已单独记录。
	h2 := NewTaskHandler(&submitStubTaskService{err: errors.New(leakRawErr)})
	w2 := httptest.NewRecorder()
	ctx2, _ := gin.CreateTestContext(w2)
	ctx2.Request = httptest.NewRequest("POST", "/api/v1/cam/tasks/sync-assets", strings.NewReader("{}"))
	res, err := h2.SubmitSyncAssetsTask(ctx2, SubmitSyncAssetsTaskReq{})
	if err == nil {
		t.Fatal("提交失败时必须返回非 nil error（否则 ginx 包装器会回 HTTP 200 + 错误码）")
	}
	if strings.Contains(res.Msg, "mongo") || strings.Contains(res.Msg, "no documents") {
		t.Fatalf("响应体不得外泄原始错误文本，实际 %q", res.Msg)
	}
}

// submitStubTaskService 只实现提交方法（其余由内嵌接口占位）。
type submitStubTaskService struct {
	taskservice.TaskService
	err error
}

func (s *submitStubTaskService) SubmitSyncAssetsTask(_ context.Context, _ task.SyncAssetsParams, _ string) (string, error) {
	return "", s.err
}
