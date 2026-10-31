package template

import (
	"net/http"
	"testing"
)

// 回归：本处理器此前把**所有**错误响应都写成 HTTP 200（实测 GET /templates/999999 →
// 200 {"code":404030}、/templates/abc → 200 {"code":400}），只按 HTTP 判成功的客户端
// 会把"不存在/参数错"当成成功。这里把"错误码段位 → HTTP 状态"的映射钉住。
func TestHTTPStatusForCode(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"模板不存在(404030) → 404", ErrTemplateNotFound.Code, http.StatusNotFound},
		{"开通任务不存在(404031) → 404", 404031, http.StatusNotFound},
		{"共享码表云账号不存在(404002) → 404", 404002, http.StatusNotFound},
		{"参数错误(400) → 400", 400, http.StatusBadRequest},
		{"云 API 错误(500030) → 500", ErrCloudAPIError.Code, http.StatusInternalServerError},
		{"系统错误 → 500", 500, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := httpStatusForCode(c.code); got != c.want {
				t.Fatalf("httpStatusForCode(%d) = %d, want %d", c.code, got, c.want)
			}
		})
	}
}
