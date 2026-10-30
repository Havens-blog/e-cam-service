package web

// maxPageSize 单页条数上限（cert 模块内的**本地**实现）。
//
// 【为什么不直接 import internal/shared/paging】仓库的 depcheck 规则 R4 要求
// `internal/cert` 保持"**可抽取、零跨域出向**"：不得 import `internal/shared/**`，
// 否则会被判定为新的跨域边界违规（CI 的 depcheck 步骤会失败）。
//
// 【数值一致性】与 `internal/shared/paging.MaxLimit` 相同（1000）。两处互相以注释指引：
// 调整上限时必须同时改这两处（shared/paging 那侧也有同样的反向指引）。
// 取值依据见 shared/paging 包注释：前端当前最大只请求 200（limit/pageSize），故 1000 对现有客户端零影响。
const maxPageSize = 1000

// clampPageSize 把请求的 pageSize 归一到 (0, maxPageSize]。
// <=0（含解析失败的 0）保持原值不动，交由调用方既有的"非法/缺省 → 默认值"语义处理。
func clampPageSize(n int) int {
	if n > maxPageSize {
		return maxPageSize
	}
	return n
}
