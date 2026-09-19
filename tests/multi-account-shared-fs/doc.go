// @feature nas-ops-insight @api-functional
//
// Journey multi-account-shared-fs ( High ): one physical NAS filesystem shared
// by three cloud accounts — collect writes one row per account under the
// (account_id, fs_id, date) unique key with no cross-account overwrite, trend
// reads stay account-isolated while aggregation dedups by fs_id taking the
// "latest date then largest capacity" representative row, and Top items carry
// the deduped ascending account id list.
//
// Contract tests: one function per Contract outcome
// ( docs/features/nas-ops-insight/testing/multi-account-shared-fs/contracts/ ).
// Journey smoke: TestMultiAccountSharedFs_FullJourneySmoke.
//
// Scope note: unauthorized-401 outcomes are exempt ( global auth middleware,
// see the lifecycle journey doc.go rationale ).
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package multi_account_shared_fs
