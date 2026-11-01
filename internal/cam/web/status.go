package web

import (
	"errors"
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// HTTPStatusForCode 把**业务错误码段位**映射为 HTTP 状态码。
//
// 背景（2026-10-31 起在 template / 2026-11-01 在 dns 两处修的同一类缺陷）：处理器把**所有**错误响应
// 都写成 `ctx.JSON(http.StatusOK, web.ErrorResult*(...))`，于是
//
//	GET  /api/v1/cam/templates/999999       → HTTP 200 {"code":404030,"msg":"vm template not found"}
//	POST /api/v1/cam/dns/domains/x/records/batch-delete(空 body) → HTTP 200 {"code":400,"msg":"…"}
//
// 只按 HTTP 判成功的客户端会把"不存在/参数错"当成成功（与"该 4xx 却回 5xx"恰成**相反**的错误形态）。
//
// 段位约定（见 internal/cam/errs 与各模块本地码）：
//   - 404xxx = 目标不存在 ⇒ 404（如模板 404030、云账号 404002、DNS 云账号 404050）
//   - 409xxx = 冲突（已存在 / 仍有子项）⇒ 409（如字典 409020/409021）
//   - 400xxx = 参数/校验错误 ⇒ 400（如 DNS 记录非法 400050、域名非法 400057、字典 400020）
//   - 4xx     = 参数错误 ⇒ 400（如 ParamsError=400）
//   - 其余    = 服务端/上游错误 ⇒ 500（如 SystemError=500、模板云 API 500030、DNS 云 API 500050）
//
// 【教训 2026-11-01】本函数修过两次缺口，均由用例发现：
//
//	① 最初只写 `case 400 <= code < 500`，**漏掉 400xxx 段**（DNS 400050 落到 default 变 500）；
//	② 之后处理字典时又发现 **409xxx 段未映射**（409020 冲突会变成 500）⇒ 补 409。
func HTTPStatusForCode(code int) int {
	switch {
	case code >= 404000 && code < 405000:
		return http.StatusNotFound
	case code >= 409000 && code < 410000:
		return http.StatusConflict
	case code >= 400000 && code < 401000:
		return http.StatusBadRequest
	case code >= 400 && code < 500:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// HTTPStatusForError 提取 err 中的领域错误码（errs.ErrorCode）并按段位映射；
// 非领域错误统一 500。
func HTTPStatusForError(err error) int {
	var ec errs.ErrorCode
	if errors.As(err, &ec) {
		return HTTPStatusForCode(ec.Code)
	}
	return http.StatusInternalServerError
}
