// @feature cert-volcano-import-sync @api-functional
//
// Journey manual-sync-endpoint-guard ( High ): the manual trigger endpoint
// POST /api/v1/certs/discovery/sync contract — OpsEngineer whitelist
// ( 403/401 boundaries ), the one-shot 200 terminal summary shape, the
// running-conflict 409 semantics, sessionId continuation into the existing
// progress endpoint, and the whitelist-only failure summary.
//
// Contract tests: one function per Contract outcome
// ( docs/features/cert-volcano-import-sync/testing/manual-sync-endpoint-guard/contracts/ ).
// Journey smoke: TestManualSyncEndpointGuard_FullJourneySmoke.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package manual_sync_endpoint_guard
