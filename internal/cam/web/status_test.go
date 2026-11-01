package web

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// 回归：错误响应的 HTTP 状态必须与业务码段位一致（模板 404030/400、DNS 400050/404050、云 API 500050 等）。
func TestHTTPStatusForCode(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"模板不存在(404030) → 404", 404030, http.StatusNotFound},
		{"开通任务不存在(404031) → 404", 404031, http.StatusNotFound},
		{"云账号不存在(404002) → 404", 404002, http.StatusNotFound},
		{"DNS 云账号不存在(404050) → 404", 404050, http.StatusNotFound},
		{"参数错误(400) → 400", 400, http.StatusBadRequest},
		{"DNS 记录非法(400050) → 400", 400050, http.StatusBadRequest},
		{"DNS 域名非法(400057) → 400", 400057, http.StatusBadRequest},
		{"云 API 错误(500030) → 500", 500030, http.StatusInternalServerError},
		{"DNS 云 API 失败(500050) → 500", 500050, http.StatusInternalServerError},
		{"系统错误(500) → 500", 500, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HTTPStatusForCode(c.code); got != c.want {
				t.Fatalf("HTTPStatusForCode(%d) = %d, want %d", c.code, got, c.want)
			}
		})
	}
}

func TestHTTPStatusForError(t *testing.T) {
	if got := HTTPStatusForError(errs.ErrorCode{Code: 404050}); got != http.StatusNotFound {
		t.Fatalf("领域 404 码应映射 404，实际 %d", got)
	}
	if got := HTTPStatusForError(fmt.Errorf("wrapped: %w", errs.ErrorCode{Code: 400051})); got != http.StatusBadRequest {
		t.Fatalf("包装后的领域 400 码应映射 400，实际 %d", got)
	}
	if got := HTTPStatusForError(errors.New("mongo: no documents in result")); got != http.StatusInternalServerError {
		t.Fatalf("非领域错误应映射 500，实际 %d", got)
	}
}
