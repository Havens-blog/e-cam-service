package web

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// DiskQueryService Disk 指标读取服务接口(web 层只依赖此接口):
// 纯读 ecam_disk_metric 指标表,租户校验在服务端完成
type DiskQueryService interface {
	// GetDiskMetrics 单盘近 N 天使用率/IOPS/吞吐趋势(最新一天 + 近 N 天均值)
	GetDiskMetrics(ctx context.Context, tenantID, accountID int64, diskID string, days int) (*service.DiskMetricsResp, error)
	// GetTop 账号视角 Top(disk_id 去重聚合 + 分页)
	GetTop(ctx context.Context, tenantID, accountID int64, days int, sortBy string, top, page, pageSize int) (*service.DiskTopResp, error)
}

// GetDiskMetrics GET /assets/disk/metrics?disk_id=&account_id=&days=
// 查询单云盘近 N 天使用率/IOPS/吞吐趋势(纯读指标表,不走厂商 API)。
func (h *AssetHandler) GetDiskMetrics(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	diskID := ctx.Query("disk_id")
	if diskID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "缺少 disk_id"))
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

	resp, err := h.diskQuery.GetDiskMetrics(ctx.Request.Context(), tenantID, accountID, diskID, days)
	if err != nil {
		respondQueryError(ctx, err)
		return
	}
	ctx.JSON(200, Result(resp))
}

// GetDiskTop GET /assets/disk/top?account_id=&days=&sort=&top=&page=&page_size=
// 查询账号视角 Disk 使用 Top(按 disk_id 去重聚合,共享盘不跨账号求和/平均)。
func (h *AssetHandler) GetDiskTop(ctx *gin.Context) {
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
	sortBy := ctx.DefaultQuery("sort", service.DiskSortUsagePercent)
	if sortBy != service.DiskSortUsagePercent && sortBy != service.DiskSortIOPS && sortBy != service.DiskSortThroughput {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "sort 仅支持 usage_percent|iops|throughput"))
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

	resp, err := h.diskQuery.GetTop(ctx.Request.Context(), tenantID, accountID, days, sortBy, top, page, pageSize)
	if err != nil {
		respondQueryError(ctx, err)
		return
	}
	ctx.JSON(200, Result(resp))
}
