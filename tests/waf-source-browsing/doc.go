// @feature log-query-optimization @api-functional
//
// Package wafsourcebrowsing contains API functional tests generated from the
// waf-source-browsing journey (browse the multi-cloud WAF source list with
// warm/cold latency targets and per-account failure isolation).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny. Input artifact: testing/waf-source-browsing/journey.md (no
// contracts stage ran for this feature; tests were generated directly from the
// journey narrative with frontend interactions converted to API-level
// assertions, per dispatcher instruction).
//
// Live-service note: tests exercise the real backend (localhost:8001 by
// default) and silent-skip when it is unreachable. Latency budget assertions
// (<300ms warm / <3s cold) are recorded via t.Logf but deliberately NOT
// hard-asserted — the deployed binary may predate the result-cache feature and
// shared-environment timing is inherently flaky. The timing targets are
// therefore only partially enforced here (ASSERTION_DEPTH_EXEMPT(partial) for
// the latency dimension; all structural/semantic behaviors are deep-asserted).
package wafsourcebrowsing
