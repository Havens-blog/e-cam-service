package web

import (
	"context"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// OSSQueryService OSS 指标读取服务接口(web 层只依赖此接口):
// 纯读 ecam_oss_metric 指标表,租户校验在服务端完成
type OSSQueryService interface {
	// GetBucketMetrics 单 bucket 近 N 天存储量/对象数趋势(最新一天 + 近 N 天均值)
	GetBucketMetrics(ctx context.Context, tenantID, accountID int64, bucketName string, days int) (*service.OSSBucketMetricsResp, error)
	// GetTop 账号视角 Top(bucket_name 去重聚合 + 分页)
	GetTop(ctx context.Context, tenantID, accountID int64, days int, sortBy string, top, page, pageSize int) (*service.OSSTopResp, error)
}

// GetOSSBucketMetrics GET /assets/oss/metrics?bucket_name=&account_id=&days=
// 查询单 OSS bucket 近 N 天存储量/对象数趋势(纯读指标表,不走厂商 API)。
func (h *AssetHandler) GetOSSBucketMetrics(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	bucketName := ctx.Query("bucket_name")
	if bucketName == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "缺少 bucket_name"))
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

	resp, err := h.ossQuery.GetBucketMetrics(ctx.Request.Context(), tenantID, accountID, bucketName, days)
	if err != nil {
		respondQueryError(ctx, err)
		return
	}
	ctx.JSON(200, Result(resp))
}

// GetOSSTop GET /assets/oss/top?account_id=&days=&sort=&top=&page=&page_size=
// 查询账号视角 OSS 存储 Top(按 bucket_name 去重聚合,不跨账号求和/平均)。
func (h *AssetHandler) GetOSSTop(ctx *gin.Context) {
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
	sortBy := ctx.DefaultQuery("sort", service.OSSSortStorageSize)
	if sortBy != service.OSSSortStorageSize && sortBy != service.OSSSortObjectCount {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "sort 仅支持 storage_size|object_count"))
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

	resp, err := h.ossQuery.GetTop(ctx.Request.Context(), tenantID, accountID, days, sortBy, top, page, pageSize)
	if err != nil {
		respondQueryError(ctx, err)
		return
	}
	ctx.JSON(200, Result(resp))
}
