// Package handler HTTP API 处理器
package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/web"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// GetCDNCost 多云 CDN 经营成本（月度 / 账号 / 域名分摊一期为空）。
// GET /api/v1/cam/cost/cdn?start_month=YYYY-MM&months=6&account_id=N
func (h *CostHandler) GetCDNCost(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)
	months, _ := strconv.Atoi(ctx.DefaultQuery("months", "6"))
	accountID, _ := strconv.ParseInt(ctx.Query("account_id"), 10, 64)

	reqCtx, cancel := context.WithTimeout(ctx.Request.Context(), 15*time.Second)
	defer cancel()

	view, err := h.cdnCostSvc.GetCDNCost(reqCtx, tenantID, ctx.Query("start_month"), months, accountID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, web.ErrorResultWithMsg(errs.SystemError, err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, web.Result(view))
}
