package web

import "strconv"

// MaxPageLimit 单页返回条数上限。
//
// 【为什么需要】实测（2026-10-30，真实后端 8888）：`limit=999999` 会一次性返回全量 ——
// 资产 DISK 5228 条 / 6.4MB、审计日志 7360 条 / 4.5MB、资产 ECS 2813 条 / 4.8MB。
// 这绕过了分页语义，放大内存/带宽（轻量请求即可被放大成 DoS 面）。
//
// 【取值依据】前端当前**最大只请求 200**（`limit` / `pageSize`），故 1000 的硬上限
// 对现有客户端**零影响**；同时把最坏单页体积压到 ~1MB 量级。
//
// 【范围】本次仅在**已实测最重**的资产列表与审计日志上生效（草稿 PR，待确认后统一铺开
// 到其余模块 —— `limit`/`size` 读取点目前分布于 25 个文件）。
const MaxPageLimit = 1000

// clampLimit 把请求的 limit 归一到 [1, MaxPageLimit]。
// limit <= 0（含解析失败的 0）交由调用方原有的默认值逻辑处理，这里保持原值不动，
// 避免悄悄改变"非法/缺省 → 默认 20"的既有语义。
func clampLimit(limit int) int {
	if limit > MaxPageLimit {
		return MaxPageLimit
	}
	return limit
}

// normalizeLimit 解析并归一 limit：非法或 <=0 → 默认值；上限见 MaxPageLimit。
// 供新增 handler 直接使用（既有 handler 仍走各自 Atoi + clampLimit，避免大范围改写）。
func normalizeLimit(raw string, defaultLimit int) int {
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		v = defaultLimit
	}
	return clampLimit(v)
}
