// @feature log-query-optimization @api-functional
//
// Package fieldfilterquickvalues contains API functional tests generated from
// the field-filter-quick-values journey (quick values derived from the current
// sample, one-click narrowing, AND-stacked multi-field filters, clear/restore).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny. Input artifact: testing/field-filter-quick-values/journey.md
// (no contracts stage ran for this feature; tests were generated directly from
// the journey narrative with frontend interactions converted to API-level
// assertions, per dispatcher instruction).
//
// Frontend-to-API conversions applied:
//   - "快捷值来自样本、零额外请求" -> quick values are derived in-test from a
//     first unfiltered search sample (no extra endpoint), then replayed as
//     structured filters;
//   - "点选快捷值重查收窄" -> search with filters=[{field,eq,value}] where
//     value comes from the sample; every returned entry must match;
//   - "清除筛选恢复完整结果" -> unfiltered re-query restores the original
//     total;
//   - invariant "筛选不改变日志类型与时间范围" -> identical log_type/window on
//     every filtered request.
package fieldfilterquickvalues
