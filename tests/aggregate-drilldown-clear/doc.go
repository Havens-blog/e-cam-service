// @feature log-query-optimization @api-functional
//
// Package aggregatedrilldownclear contains API functional tests generated from
// the aggregate-drilldown-clear journey (TopN grouped stats -> click-to-filter
// drilldown -> replace/clear semantics).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny. Input artifact: testing/aggregate-drilldown-clear/journey.md
// (no contracts stage ran for this feature; tests were generated directly from
// the journey narrative with frontend interactions converted to API-level
// assertions, per dispatcher instruction).
//
// Frontend-to-API conversions applied:
//   - "浏览 TopN 分组图" -> POST /aggregate with dimension/metric returns
//     buckets + topn (TopN sorted by value descending when present);
//   - "点击条形项下钻" -> search with filters=[{dimension,eq,topn-name}],
//     every entry must match the picked group value;
//   - "替换语义(非叠加)" -> stacking two identical filters on the same field
//     must be result-identical to one (no duplicated condition effect);
//   - "一键清除恢复原视图" -> unfiltered re-query restores baseline total.
//
// Aggregate latency is recorded via t.Logf only (ASSERTION_DEPTH_EXEMPT(partial)
// for the timing dimension; see waf-source-browsing/doc.go rationale).
//
// Cross-call restore/coexist assertions use tolerance bands (0.5x..2x) instead
// of exact totals: the live backend re-fans-out on every call and per-source
// counts vary between calls (see log-query-lifecycle/doc.go).
package aggregatedrilldownclear
