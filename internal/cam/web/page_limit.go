package web

import "github.com/Havens-blog/e-cam-service/internal/shared/paging"

// 本文件是 internal/cam/web 各 handler 的**薄适配层**：真正的上限逻辑在
// `internal/shared/paging`（全仓单源），这里只保留本包内既有的调用名，
// 避免各包各写一套（历史上 provider 展示名就出现过副本分叉）。
//
// 【背景】实测（2026-10-30，真实后端 8888）：`limit=999999` 会一次性返回全量 ——
// 资产 DISK 5228 条 / 6.4MB、审计日志 7360 条 / 4.5MB。取值依据见 shared/paging 包注释
// （前端最大只请求 200，故上限 1000 对现有客户端零影响）。
//
// 【范围】已按维护者确认**铺开到全部模块**：本包 17 处资产类解析点 + 其余
// cost/dns/tag/task/template/dictionary/cert/alert/audit 等模块统一走 shared/paging。

// MaxPageLimit 保留旧名以兼容本包既有调用（值与 shared/paging.MaxLimit 同源）。
const MaxPageLimit = paging.MaxLimit

// clampLimit 把请求的 limit 归一到 (0, MaxPageLimit]；<=0 保持原值，
// 由调用方既有的"非法/缺省 → 默认 20"语义处理。
func clampLimit(limit int) int {
	return paging.Clamp(limit)
}

// normalizeLimit 解析并归一 limit：非法或 <=0 → 默认值；超过上限 → 上限。
func normalizeLimit(raw string, defaultLimit int) int {
	return paging.Normalize(raw, defaultLimit)
}
