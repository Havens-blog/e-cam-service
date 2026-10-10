package ioc

import (
	"context"

	assetdomain "github.com/Havens-blog/e-cam-service/internal/asset/domain"
)

// 本文件为 cert 覆盖率分母的 asset 侧适配（cert 服务抽取 · 出站解耦）：
// cert 的 InstanceCounter 端口以 model_uid 为唯一入参，不再依赖 asset 领域的
// InstanceFilter 类型；此 adapter 用 asset 仓储既有的 Count(InstanceFilter) 满足
// 该端口，使 cert 不再 import internal/asset/domain。

// assetInstanceCounter 把 cert InstanceCounter 适配到 asset 仓储的 Count。
type assetInstanceCounter struct {
	repo instanceCountRepo
}

// instanceCountRepo 为 asset 仓储 Count 方法的最小结构化契约（避免 ioc 绑定
// 具体仓储类型）。
type instanceCountRepo interface {
	Count(ctx context.Context, filter assetdomain.InstanceFilter) (int64, error)
}

func (a assetInstanceCounter) CountByModelUID(ctx context.Context, modelUID string) (int64, error) {
	return a.repo.Count(ctx, assetdomain.InstanceFilter{ModelUID: modelUID})
}
