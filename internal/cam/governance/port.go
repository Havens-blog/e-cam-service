// Package governance 租户治理配置读取端口。
//
// e-cam-service 自身不维护租户注册表（租户统一由 eiam 管理），创建云账号时
// 需要强制校验的租户配额（tenant-management R2.4）与云厂商白名单（R7）
// 从 eiam 租户详情实时读取。cam 域只依赖本端口抽象，不感知具体 eiam 客户端；
// 单元测试经 mock 适配器注入，跨服务调用一律不连真实 eiam。
package governance

import "context"

// TenantGovernancePort 租户治理配置端口（出站端口）。
type TenantGovernancePort interface {
	// GetGovernance 查询租户治理配置。
	// maxAccounts：云账号配额上限（0/负 = 无限制，R2.7）；
	// allowedVendors：允许的云厂商列表（空 = 全部允许，R7.4）。
	// 查询失败（不可达/超时/租户不存在）返回非 nil error，由调用方 fail-closed 拒绝。
	GetGovernance(ctx context.Context, tenantID int64) (maxAccounts int64, allowedVendors []string, err error)
}
