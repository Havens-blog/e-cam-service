// Package mcp 提供基于 Model Context Protocol 的多云资产管理 MCP Server
package mcp

import (
	"context"
	"time"

	accountservice "github.com/Havens-blog/e-cam-service/internal/account/service"
	"github.com/Havens-blog/e-cam-service/internal/cam/domain"
	camservice "github.com/Havens-blog/e-cam-service/internal/cam/service"
	"github.com/Havens-blog/e-cloudx-sdk"
	"github.com/Havens-blog/e-common-go/mongox"
	"github.com/gotomicro/ego/core/elog"
)

// Dependencies MCP Server 所需的全部依赖（字段均为服务层接口 + 共享句柄）。
// 具体仓储/DAO 的装配属组合根职责，置于命令层 cmd/mcp-server（depcheck R1：
// internal 领域包不得触达其它域的 repository/dao），本包只消费注入后的服务。
type Dependencies struct {
	AccountSvc  accountservice.CloudAccountService
	InstanceSvc camservice.InstanceService
	Factory     *cloudx.AdapterFactory
	Logger      *elog.Component
	mongoDB     *mongox.Mongo
}

// NewDependencies 由命令层组合根注入已装配的服务与 Mongo 句柄构造依赖容器。
func NewDependencies(
	accountSvc accountservice.CloudAccountService,
	instanceSvc camservice.InstanceService,
	factory *cloudx.AdapterFactory,
	logger *elog.Component,
	mongoDB *mongox.Mongo,
) *Dependencies {
	return &Dependencies{
		AccountSvc:  accountSvc,
		InstanceSvc: instanceSvc,
		Factory:     factory,
		Logger:      logger,
		mongoDB:     mongoDB,
	}
}

// Close 关闭依赖资源
func (d *Dependencies) Close() {
	if d.mongoDB != nil && d.mongoDB.DBClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.mongoDB.DBClient.Disconnect(ctx)
	}
}

// listInstances 通用的资产列表查询辅助方法
func (d *Dependencies) listInstances(ctx context.Context, filter domain.InstanceFilter) ([]domain.Instance, int64, error) {
	return d.InstanceSvc.List(ctx, filter)
}

// searchInstances 通用的资产搜索辅助方法
func (d *Dependencies) searchInstances(ctx context.Context, filter domain.SearchFilter) ([]domain.Instance, int64, error) {
	return d.InstanceSvc.Search(ctx, filter)
}
