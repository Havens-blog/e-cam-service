// Package paging 提供分页参数的统一归一（单页上限）。
//
// 【为什么需要】实测（2026-10-30，真实后端 8888）：`limit=999999` 会一次性返回全量 ——
// 资产 DISK 5228 条 / 6.4MB、审计日志 7360 条 / 4.5MB、资产 ECS 2813 条 / 4.8MB。
// 这绕过了分页语义、放大内存与带宽（轻量请求即可被放大成 DoS 面）。
//
// 【取值依据】前端当前**最大只请求 200**（`limit` / `pageSize`），故 1000 的硬上限
// 对现有客户端**零影响**，同时把最坏单页体积压到 ~1MB 量级。
//
// 【为什么放在 shared】原实现只落在资产列表与审计日志上（`internal/cam/web/page_limit.go`），
// 本次按维护者确认**铺开到全部模块**；为避免各包各写一套（历史上 provider 展示名就出现过
// 多处副本分叉），统一收敛到本包，各 handler 只调用不复制逻辑。
package paging

import "strconv"

// MaxLimit 单页返回条数上限（所有列表接口共用；见包注释的取值依据）。
const MaxLimit = 1000

// Clamp 把 limit 归一到 (0, MaxLimit]。
// 注意：<=0（含解析失败的 0）**保持原值不动**，交由各 handler 既有的
// "非法/缺省 → 默认值" 语义处理，避免在这里悄悄改变既有行为。
func Clamp(limit int) int {
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// ClampInt64 是 Clamp 的 int64 版本（多处 handler 用 ParseInt 取 limit）。
func ClampInt64(limit int64) int64 {
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// Normalize 解析并归一 limit：非法或 <=0 → 默认值；超过上限 → 上限。
// 供新写的 handler 直接使用；既有 handler 仍走各自 Atoi/ParseInt + Clamp，避免大范围改写。
func Normalize(raw string, defaultLimit int) int {
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		v = defaultLimit
	}
	return Clamp(v)
}

// NormalizeInt64 是 Normalize 的 int64 版本。
func NormalizeInt64(raw string, defaultLimit int64) int64 {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		v = defaultLimit
	}
	return ClampInt64(v)
}
