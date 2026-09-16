// @feature log-query-optimization @api-functional
//
// Package logquerylifecycle contains API functional tests generated from the
// log-query-lifecycle journey (golden path: initiate search -> per-source
// progress -> read results -> recover from failure -> repeat query).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny. Input artifact: testing/log-query-lifecycle/journey.md (no
// contracts stage ran for this feature; tests were generated directly from the
// journey narrative with frontend interactions converted to API-level
// assertions, per dispatcher instruction).
//
// Frontend-to-API conversions applied:
//   - "进行中状态(N 个云账号/已耗时)" -> per-source SourceOutcome list with
//     cloud/account/duration_ms;
//   - "单源失败仅标注该源" -> response code 0 while a source outcome carries a
//     non-empty readable error (live tenant has a known broken delivery);
//   - "重试/重复查询不叠加" -> repeated identical request returns consistent
//     totals and identical merged results.
//
// Non-inducible live scenarios (federation-wide timeout/truncation, 10min TTL
// expiry, all-accounts-simultaneously-down) are covered by their readable-error
// contract paths (400 with business-readable msg) or documented; latency
// budgets are recorded via t.Logf only (see doc.go of waf-source-browsing for
// the ASSERTION_DEPTH_EXEMPT(partial) rationale).
//
// Repeat-query consistency is asserted with a CLOSED historical window plus a
// tolerance band (0.5x..2x) instead of exact equality: the live backend
// re-fans-out on every call, per-source counts vary between calls, and the
// stream ingests continuously — the journey invariant testable at API level is
// "no duplicated/stacked result blocks and window respected", not byte-stable
// reproducibility.
package logquerylifecycle
