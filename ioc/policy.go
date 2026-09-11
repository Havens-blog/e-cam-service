package ioc

import (
	"github.com/Duke1616/eiam/pkg/web/capability"
	"github.com/Duke1616/eiam/pkg/web/sdk"
)

// InitPolicySDK eiam 远程鉴权 SDK（细粒度授权 CheckAPI）。
// 读 policy.auth_url；未配置时启动期 panic（显式失败优于静默失效）。
// 认证仍由 EcmdbAuthMiddleware 本地校验共享 Redis 会话，CheckLogin 不重复接入。
func InitPolicySDK() *sdk.SDK {
	return sdk.NewSDK()
}

// InitPermSyncer 端点资产上报器。
// 读 policy.discovery_url（自动补 /api/v1/discovery/sync）；Sync 内部以
// sync.Once 启动 30s 全量 tick 协程，首调即触发首轮上报。
func InitPermSyncer() capability.Syncer {
	return capability.NewSyncer(capability.NewHttpReporter())
}

// InitProviders 额外权限点提供者。
// 当前依赖 handler 侧 capability.IRegistry 的全局自动收集（NewRegistry 所在
// 包被 import 即上报），暂无额外 Provider；后续按域补充时在此返回。
func InitProviders() []capability.PermissionProvider {
	return nil
}
