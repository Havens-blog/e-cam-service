package web

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// 状态码语义回归：
// 修复前"模板不存在"被返回 500（实测 GET /iam/templates/1 → 500 + code 404013），
// 导致前端无法区分"不存在"与"后端故障"。
func TestStatusForIamError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"模板不存在 → 404（而不是 500）", errs.TemplateNotFound, http.StatusNotFound},
		{"**回归** 同步任务不存在 → 404（此前实测 500 + code 404012）", errs.SyncTaskNotFound, http.StatusNotFound},
		{"被 %w 包装的同步任务不存在 → 仍 404", fmt.Errorf("获取同步任务失败: %w", errs.SyncTaskNotFound), http.StatusNotFound},
		{"被 %w 包装的模板不存在 → 仍 404", fmt.Errorf("获取模板失败: %w", errs.TemplateNotFound), http.StatusNotFound},
		{"参数错误 → 400", errs.ParamsError, http.StatusBadRequest},
		{"被 %w 包装的参数错误 → 仍 400", fmt.Errorf("创建模板失败: %w", errs.ParamsError), http.StatusBadRequest},
		{"其他错误 → 500", fmt.Errorf("连接 mongo 失败"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := statusForIamError(c.err); got != c.want {
				t.Fatalf("statusForIamError(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}
