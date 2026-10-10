package service

import (
	"context"

	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
)

// 本文件声明 cert 域对 account / asset 领域的最小只读消费端口（消费方接口）。
// cert 据此依赖接口而非对方仓储包，故不再 import internal/account/repository
// 或 internal/asset/repository（depcheck R1：跨域不得触达对方持久化层）。
// 生产实现由 account/asset 仓储经 Go 结构化类型满足，组合根（ioc）注入既有仓储，
// 无需新增 provider。

// CloudAccountLister cert 域所需的云账号只读端口（active 账号清单，用于凭证
// 解析与引用扫描）。方法集与 account 仓储 List 同签名（filter/返回取 cloudx-sdk
// 共享域类型），cert 现有调用点零改动即满足。
type CloudAccountLister interface {
	List(ctx context.Context, filter sharedomain.CloudAccountFilter) ([]sharedomain.CloudAccount, int64, error)
}

// InstanceCounter cert 域所需的资产实例计数端口（覆盖率分母逐 model_uid Count）。
//
// 签名以 model_uid 为唯一入参（cert 覆盖率分母只按 model_uid 聚合，不需要 asset
// InstanceFilter 的其余字段）——避免 cert 依赖 asset 领域类型，为 cert 服务抽取
// 清除出站耦合。生产实现由组合根用 asset 仓储（InstanceFilter{ModelUID:...}）适配。
type InstanceCounter interface {
	CountByModelUID(ctx context.Context, modelUID string) (int64, error)
}
