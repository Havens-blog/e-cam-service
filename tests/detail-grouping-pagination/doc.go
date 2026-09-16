// @feature log-query-optimization @api-functional
//
// Package detailgrouppagination contains API functional tests generated from
// the detail-grouping-pagination journey (details grouped by cloud·account,
// per-group counts/duration in group headers, in-group paging without re-query).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny. Input artifact: testing/detail-grouping-pagination/journey.md
// (no contracts stage ran for this feature; tests were generated directly from
// the journey narrative with frontend interactions converted to API-level
// assertions, per dispatcher instruction).
//
// Frontend-to-API conversions applied:
//   - "按云·账号折叠分组,组头条数/耗时" -> every entry carries meta.cloud /
//     meta.account_name; group headers are the per-source SourceOutcome list
//     (count = 组内总数, duration_ms = 耗时), readable without expansion;
//   - "组内翻页不触发整页重查/组间独立" -> collapse/expand and paging are
//     client-side over the fetched sample; API contract is that a repeat of
//     the same query returns a stable, reproducible group shape (no drift,
//     no duplication);
//   - "组头条数始终反映总数" -> sum of successful source counts equals the
//     merged total (when not truncated).
//
// Zero-entry groups are observable live (known non-flowing delivery reports
// count=0 with a readable error note).
//
// Cross-call stability assertions use tolerance bands (0.5x..2x) instead of
// exact equality: the live backend re-fans-out on every call, per-source
// counts vary between calls, and the stream ingests continuously (see
// log-query-lifecycle/doc.go).
package detailgrouppagination
