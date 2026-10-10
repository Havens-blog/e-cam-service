package web

import "github.com/gin-gonic/gin"

// 本文件为 cert/web 的操作者身份读取（cert 服务抽取 · 出站解耦）：
// 原先经 internal/shared/middleware.GetUsername 读取 gin 上下文中的用户名。
// GetUsername 是读取固定上下文键的纯 helper，无需跨包依赖；cert 定义本地同名
// 键与 helper，读取同一上下文键，使 cert/web 不再 import internal/shared/middleware。
//
// 运行期契约不变：单体内该键仍由 shared 的 ecmdb_auth 鉴权中间件填充（键值
// "username" 对齐）；cert 抽为独立服务后由其自带鉴权中间件填充同键。

// ctxUsernameKey 操作者用户名在 gin 上下文中的键（与 shared/middleware
// CtxUsernameKey 取值一致："username"——跨鉴权中间件的唯一对齐点）。
const ctxUsernameKey = "username"

// operatorUsername 从 gin 上下文读取操作者用户名；缺省返回空串。
func operatorUsername(c *gin.Context) string {
	if v, ok := c.Get(ctxUsernameKey); ok {
		if u, ok := v.(string); ok {
			return u
		}
	}
	return ""
}
