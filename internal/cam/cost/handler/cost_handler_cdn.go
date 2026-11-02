// Package handler HTTP API 处理器
package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	camservice "github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// GetCDNCost 多云 CDN 经营成本（月度 / 账号 / 域名分摊一期为空）。
// GET /api/v1/cam/cost/cdn?start_month=YYYY-MM&months=6&account_id=N
func (h *CostHandler) GetCDNCost(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	months, err := strconv.Atoi(ctx.DefaultQuery("months", "6"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, web.ErrorResultWithMsg(errs.ParamsError, "months 应为整数"))
		return
	}
	accountID, err := strconv.ParseInt(ctx.Query("account_id"), 10, 64)
	if ctx.Query("account_id") != "" && err != nil {
		ctx.JSON(http.StatusBadRequest, web.ErrorResultWithMsg(errs.ParamsError, "account_id 应为整数"))
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 15*time.Second)
	defer cancel()

	view, err := h.cdnCostSvc.GetCDNCost(reqCtx, tenantID, ctx.Query("start_month"), months, accountID)
	if err != nil {
		if errors.Is(err, camservice.ErrInvalidStartMonth) {
			ctx.JSON(http.StatusBadRequest, web.ErrorResultWithMsg(errs.ParamsError, "start_month 格式应为 YYYY-MM"))
			return
		}
		// 服务端内部错误不向客户端透出 err.Error() 细节
		ctx.JSON(http.StatusInternalServerError, web.ErrorResult(errs.SystemError))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(view))
}
