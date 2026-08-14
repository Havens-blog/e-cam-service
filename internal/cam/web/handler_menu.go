package web

import (
	"github.com/gin-gonic/gin"
)

// ==================== 菜单管理 ====================

// MenuItem 菜单项
type MenuItem struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Icon     string     `json:"icon,omitempty"`
	Path     string     `json:"path,omitempty"`
	Children []MenuItem `json:"children,omitempty"`
	Order    int        `json:"order"`
}

// GetMenus 获取菜单列表
// @Summary 获取菜单列表
// @Description 获取系统导航菜单的层级结构
// @Tags 系统管理
// @Accept json
// @Produce json
// @Success 200 {object} ginx.Result{data=[]MenuItem} "成功"
// @Router /cam/menus [get]
func (h *Handler) GetMenus(ctx *gin.Context) {
	menus := []MenuItem{
		{
			ID:    "asset-management",
			Name:  "资产管理",
			Icon:  "icon-asset",
			Path:  "/asset-management",
			Order: 1,
			Children: []MenuItem{
				{
					ID:    "cloud-accounts",
					Name:  "云账号",
					Path:  "/asset-management/cloud-accounts",
					Order: 1,
				},
				{
					ID:    "cloud-assets",
					Name:  "云资产",
					Path:  "/asset-management/cloud-assets",
					Order: 2,
				},
				{
					ID:    "asset-models",
					Name:  "云模型",
					Path:  "/asset-management/asset-models",
					Order: 3,
				},
				{
					ID:    "cost-analysis",
					Name:  "代价",
					Path:  "/asset-management/cost-analysis",
					Order: 4,
				},
				{
					ID:    "sync-management",
					Name:  "同步管理",
					Path:  "/asset-management/sync-management",
					Order: 5,
				},
			},
		},
		{
			ID:    "configuration",
			Name:  "配置中心",
			Icon:  "icon-config",
			Path:  "/configuration",
			Order: 2,
			Children: []MenuItem{
				{
					ID:    "system-config",
					Name:  "系统配置",
					Path:  "/configuration/system",
					Order: 1,
				},
				{
					ID:    "user-management",
					Name:  "用户管理",
					Path:  "/configuration/users",
					Order: 2,
				},
			},
		},
		{
			ID:    "monitoring",
			Name:  "监控中心",
			Icon:  "icon-monitor",
			Path:  "/monitoring",
			Order: 3,
			Children: []MenuItem{
				{
					ID:    "task-monitor",
					Name:  "任务监控",
					Path:  "/monitoring/tasks",
					Order: 1,
				},
				{
					ID:    "sync-logs",
					Name:  "同步日志",
					Path:  "/monitoring/sync-logs",
					Order: 2,
				},
			},
		},
	}

	ctx.JSON(200, Result(menus))
}
