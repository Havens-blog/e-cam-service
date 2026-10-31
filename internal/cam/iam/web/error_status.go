package web

import (
	"errors"
	"net/http"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
)

// statusForIamError 把 IAM 各子模块的**领域错误**映射为正确的 HTTP 状态码。
//
// 为什么需要：本包的处理器此前普遍把所有错误一律 `c.JSON(500, ...)`，于是
// `errs.TemplateNotFound`（404013）/`errs.SyncTaskNotFound`（404012）/`errs.UserNotFound`（404010）这类"目标不存在"
// 也被当成服务端故障返回 500，实测：
//
//	GET /api/v1/cam/iam/templates/1  → HTTP 500 {"code":404013,...}
//	GET /api/v1/cam/iam/sync/tasks/1 → HTTP 500 {"code":404012,...}
//
// 前端因此**无法区分"对象不存在"与"后端故障"**（前者应给"返回列表"，后者才该给"重试"）。
// 本函数只做**状态码语义**的修正：响应体里的 code/msg 保持不变（仍由 Error(err) 产出）。
//
// 目前只覆盖已确认语义的 NotFound / 参数错误 / 权限组业务冲突；其余错误保持 500（不猜业务含义）。
func statusForIamError(err error) int {
	switch {
	case errors.Is(err, errs.TemplateNotFound), errors.Is(err, errs.SyncTaskNotFound), errors.Is(err, errs.UserNotFound),
		errors.Is(err, errs.PermissionGroupNotFound):
		return http.StatusNotFound
	case errors.Is(err, errs.ParamsError):
		return http.StatusBadRequest
	case errors.Is(err, errs.PermissionGroupAlreadyExist), errors.Is(err, errs.PermissionGroupHasUsers):
		// 权限组业务冲突：同名已存在 / 组内仍有用户。
		// 用 409 让前端给出可操作提示（改名、先移出成员），而不是笼统的"服务器错误"。
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
