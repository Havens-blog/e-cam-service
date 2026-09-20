// @feature oss-ops-insight @api-functional
//
// Journey top-overview-insight ( Low ): the account-perspective OSS Top list
// — sorted by the near-N-day average of the chosen dimension, deduped by
// bucket_name with a deterministic representative row, paginated with
// out-of-range pages returning an empty page ( not an error ), the ops-card
// aggregation staying metric-table-only, and empty states distinguishing
// "no data" from "collect failing".
//
// Contract tests: one function per Contract outcome
// ( docs/features/oss-ops-insight/testing/top-overview-insight/contracts/ ).
// Journey smoke: TestTopOverviewInsight_FullJourneySmoke.
//
// Scope notes: unauthorized-401 outcomes are exempt ( global auth middleware,
// see per-test notes ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package top_overview_insight
