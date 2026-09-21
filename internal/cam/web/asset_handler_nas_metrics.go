package web

import (
	"context"
	"errors"
	"strconv"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// NASQueryService NAS 指标读取服务接口(web 层只依赖此接口):
// 纯读 ecam_nas_metric 指标表,租户校验在服务端完成
type NASQueryService interface {
	// GetFsMetrics 单实例近 N 天容量/使用率趋势(最新一天 + 近 N 天均值)
	GetFsMetrics(ctx context.Context, tenantID, accountID int64, fsID string, days int) (*service.NASFsMetricsResp, error)
	// GetTop 账号视角 Top(fs_id 去重聚合 + 分页)
	GetTop(ctx context.Context, tenantID, accountID int64, days int, sortBy string, top, page, pageSize int) (*service.NASTopResp, error)
}

// NAS 读取参数边界(days 限 1~90;top/page_size 最大 50,规格「Proposed Solution」第 5 点)
const (
	nasQueryDefaultDays = 30
	nasQueryMaxDays     = 90
	nasQueryDefaultTop  = 10
	nasQueryMaxTop      = 50
	nasQueryDefaultPage = 1
	nasQueryDefaultPSet = 10
	nasQueryMaxPSet     = 50
)

// GetNASFsMetrics GET /assets/nas/metrics?fs_id=&account_id=&days=
// 查询单 NAS 文件系统近 N 天容量/使用率趋势(纯读指标表,不走厂商 API)。
func (h *AssetHandler) GetNASFsMetrics(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	fsID := ctx.Query("fs_id")
	if fsID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "缺少 fs_id"))
		return
	}
	accountID, ok := parseNASAccountID(ctx)
	if !ok {
		return
	}
	days, ok := parseNASDays(ctx)
	if !ok {
		return
	}

	resp, err := h.nasQuery.GetFsMetrics(ctx.Request.Context(), tenantID, accountID, fsID, days)
	if err != nil {
		respondQueryError(ctx, err)
		return
	}
	ctx.JSON(200, Result(resp))
}

// GetNASTop GET /assets/nas/top?account_id=&days=&sort=&top=&page=&page_size=
// 查询账号视角 NAS 容量/使用率 Top(按 fs_id 去重聚合,不跨账号求和/平均)。
func (h *AssetHandler) GetNASTop(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	// account_id 可选:缺省 = 全部租户账号;传入则服务端校验归属(越权 404)
	var accountID int64
	if ctx.Query("account_id") != "" {
		var ok bool
		accountID, ok = parseNASAccountID(ctx)
		if !ok {
			return
		}
	}
	days, ok := parseNASDays(ctx)
	if !ok {
		return
	}
	sortBy := ctx.DefaultQuery("sort", service.NASSortCapacity)
	if sortBy != service.NASSortCapacity && sortBy != service.NASSortUtilization {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "sort 仅支持 capacity|utilization"))
		return
	}
	top, okTop := parseNASBound(ctx, "top", nasQueryDefaultTop, nasQueryMaxTop)
	if !okTop {
		return
	}
	page, ok2 := parseNASBound(ctx, "page", nasQueryDefaultPage, 0)
	if !ok2 {
		return
	}
	pageSize, ok3 := parseNASBound(ctx, "page_size", nasQueryDefaultPSet, nasQueryMaxPSet)
	if !ok3 {
		return
	}

	resp, err := h.nasQuery.GetTop(ctx.Request.Context(), tenantID, accountID, days, sortBy, top, page, pageSize)
	if err != nil {
		respondQueryError(ctx, err)
		return
	}
	ctx.JSON(200, Result(resp))
}

// parseNASAccountID 解析必填 account_id(缺失/非法 → 400 并写响应)
func parseNASAccountID(ctx *gin.Context) (int64, bool) {
	v, err := strconv.ParseInt(ctx.Query("account_id"), 10, 64)
	if err != nil || v <= 0 {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "account_id 必须为正整数"))
		return 0, false
	}
	return v, true
}

// parseNASDays 解析 days(1~90,缺省 30;越界 → 400)
func parseNASDays(ctx *gin.Context) (int, bool) {
	return parseNASBound(ctx, "days", nasQueryDefaultDays, nasQueryMaxDays)
}

// parseNASBound 解析正整数参数并做范围校验:max>0 时要求 v<=max(max=0 不设上限);
// 缺省回 def;非法/越界 → 400 并写响应,返回 ok=false。
func parseNASBound(ctx *gin.Context, name string, def, max int) (int, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		return def, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 || (max > 0 && v > max) {
		if max > 0 {
			ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, name+" 必须为 1~"+strconv.Itoa(max)+" 的整数"))
		} else {
			ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, name+" 必须为正整数"))
		}
		return 0, false
	}
	return v, true
}

// respondQueryError 指标读取错误统一映射(NAS/OSS/Disk 共用):越权 404(不泄露账号
// 存在性),其余 500
func respondQueryError(ctx *gin.Context, err error) {
	if errors.Is(err, service.ErrNASAccountNotInTenant) || errors.Is(err, service.ErrOSSAccountNotInTenant) ||
		errors.Is(err, service.ErrDiskAccountNotInTenant) {
		ctx.JSON(404, ErrorResultWithMsg(errs.AccountNotFound, errs.AccountNotFound.Msg))
		return
	}
	ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
}
