// Package web RDS 指标读取 handler(GET /assets/rds/metrics)。
//
// 平移蓝本:asset_handler_nas_metrics.go(参数解析/错误映射/NAS 路由旁注册模式
// 同型);契约(locked contract):GET /assets/rds/metrics?rds_id=&account_id=&days=
// days 默认 30、范围 1~90;响应 {rds_id, days[], latest, average} 由 service 层
// 组装。错误映射独立于 respondQueryError 就地实现(不反向改动 NAS/OSS/Disk
// 既有函数):越权 404(ErrRDSAccountNotInTenant,不泄露账号存在性),其余 500。
package web

import (
	"context"
	"errors"

	"github.com/Havens-blog/e-cam-service/internal/cam/errs"
	"github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cam-service/internal/shared/middleware"
	"github.com/gin-gonic/gin"
)

// RDSQueryService RDS 指标读取服务接口(web 层只依赖此接口):
// 纯读 ecam_rds_metric 指标表,租户校验与停用态甄别在服务端完成
type RDSQueryService interface {
	// GetRdsMetrics 单实例近 N 天 CPU/内存/磁盘/连接数趋势(最新一天 + 近 N 天均值)
	GetRdsMetrics(ctx context.Context, tenantID, accountID int64, rdsID string, days int) (*service.RDSMetricsResp, error)
}

// GetRDSMetrics GET /assets/rds/metrics?rds_id=&account_id=&days=
// 查询单 RDS 实例近 N 天指标趋势(纯读指标表,不走厂商 API)。
// 参数解析复用 NAS 的 parseNASAccountID/parseNASDays(同口径:account_id 正整数、
// days 1~90 缺省 30),参数名与契约一致。
func (h *AssetHandler) GetRDSMetrics(ctx *gin.Context) {
	tenantID := middleware.GetTenantID(ctx)

	rdsID := ctx.Query("rds_id")
	if rdsID == "" {
		ctx.JSON(400, ErrorResultWithMsg(errs.FieldInvalid, "缺少 rds_id"))
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

	if h.rdsQuery == nil {
		ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, "RDS 指标读取服务未装配"))
		return
	}

	resp, err := h.rdsQuery.GetRdsMetrics(ctx.Request.Context(), tenantID, accountID, rdsID, days)
	if err != nil {
		respondRDSError(ctx, err)
		return
	}
	ctx.JSON(200, Result(resp))
}

// respondRDSError RDS 指标读取错误映射:越权 404(不泄露账号存在性),其余 500。
// 独立于 respondQueryError(NAS/OSS/Disk 映射),避免跨资源错误集耦合。
func respondRDSError(ctx *gin.Context, err error) {
	if errors.Is(err, service.ErrRDSAccountNotInTenant) {
		ctx.JSON(404, ErrorResultWithMsg(errs.AccountNotFound, errs.AccountNotFound.Msg))
		return
	}
	ctx.JSON(500, ErrorResultWithMsg(errs.SystemError, err.Error()))
}
