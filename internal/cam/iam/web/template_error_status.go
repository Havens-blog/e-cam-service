package web

import (
	"errors"
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// statusForTemplateError 把策略模板的**领域错误**映射为正确的 HTTP 状态码。
//
// 为什么需要：该处理器此前把所有错误一律 `c.JSON(500, ...)`，于是
// `errs.TemplateNotFound`（错误码 404013）也被当成服务端故障返回 500 ——
// 实测 `GET /api/v1/cam/iam/templates/1`（不存在的 id）返回：
//
//	HTTP 500 {"code":404013,"msg":"policy template not found"}
//
// 前端因此**无法区分"模板不存在"与"后端故障"**，只能笼统提示失败。
// 这里只做**状态码语义**的修正（响应体 code/msg 保持不变）。
func statusForTemplateError(err error) int {
	if errors.Is(err, errs.TemplateNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, errs.ParamsError) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
