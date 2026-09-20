// @feature oss-ops-insight @api-functional
//
// Journey multi-account-shared-bucket ( High ): the same bucket name managed
// by multiple cloud accounts — the (account_id, bucket_name, date) unique key
// keeps one row per account ( never merged ), same-day rewrites are absorbed
// by first-write-wins, next-day backfill overwrites yesterday rows per
// account, Top dedups by bucket_name into a single representative row
// ( date desc, then capacity desc — never summed across accounts ), and trend
// reads stay precisely isolated per account.
//
// Contract tests: one function per Contract outcome
// ( docs/features/oss-ops-insight/testing/multi-account-shared-bucket/contracts/ ).
// Journey smoke: TestMultiAccountSharedBucket_FullJourneySmoke.
//
// Scope notes: unauthorized-401 outcomes are exempt ( global auth middleware,
// see per-test notes ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package multi_account_shared_bucket
