# Test Report: cert-volcano-import-sync

**Date**: 2026-09-17
**Duration**: 5 packages sequential, ~0.3s test time each (fresh `-count=1` run); full loop incl. module compile < 5 min

## Summary

| Type  | Total | Pass | Fail | Skip |
|-------|-------|------|------|------|
| api (top-level test functions) | 82 | 81 | 0 | 1 |
| api (subtests, all passing) | 7 | 7 | 0 | 0 |
| **All** | **89** | **88** | **0** | **1** |

**Result**: PASS — 5/5 journey packages `ok` under `go test -count=1 -v -timeout 180s`, 0 failures, 1 documented skip (see Skip Detail).

Surface: api (scalar, recipe-prefix empty). Journeys: first-sync-backfill, incremental-skip-drift, manual-scheduler-race, cloud-failure-isolation, manual-sync-endpoint-guard.
Sequence executed: stale-state check (none) → surface detection (task frontmatter: api) → journey discovery (5) → api orchestration rules → env readiness → env compile check (`go build -p 1 ./...`, exit 0) → per-journey test loop → teardown.

No justfile in this repo: the dev/probe/test/teardown lifecycle is mapped per established repo precedent to hermetic in-process gin harnesses (wire-produced `web.DiscoveryHandler` + stub cloud adapters); server/DB/auth readiness is satisfied inside each test process, so dev/probe have no external lifecycle and teardown is a no-op (`.forge/test-state.json` written before the loop, removed after clean completion). Raw verbose outputs: `tests/results/volcano-<journey>-raw.txt` (gitignored).

---

## Confidence Rating

**Level**: LOW (REVIEW)
**Confirmed Fact Ratio**: 0.00 (0 of 77 contract outcomes covered by runtime+confirmed facts)

### Confidence Distribution

| Level  | Count | Percentage |
|--------|-------|------------|
| HIGH   | 0     | 0%         |
| MEDIUM | 0     | 0%         |
| LOW    | 77    | 100%       |

### Verification Summary

| Mark   | Count |
|--------|-------|
| VERIFY | 0 |
| REVIEW | 77 |

Basis: `.forge/fact-table.json` holds 90 facts, all `source=static`, zero `runtime`/`confirmed` entries; in addition, the quick-mode pipeline skipped the eval gates (forced downgrade per confidence rules). run-tests does not write runtime facts back, so the ratio is unchanged by this run — this is process state, not a defect. LOW confidence tests still execute and are not blocked; they are flagged REVIEW for human verification.

---

## Results by Test Case

Suites named `api-functional/<journey>`. Evidence: raw verbose output per journey in `tests/results/volcano-<journey>-raw.txt`.

### api-functional/first-sync-backfill — 18 PASS / 1 SKIP / 0 FAIL

- PASS: TestFirstSyncBackfill_FullJourneySmoke
- PASS: TestFirstSyncBackfill_Step1_SchedulerTriggerSuccess
- PASS: TestFirstSyncBackfill_Step1_CASAlreadyRunningSkips
- PASS: TestFirstSyncBackfill_Step1_NilSyncServiceDegrades
- PASS: TestFirstSyncBackfill_Step2_EnumerateCloudsAccounts
- PASS: TestFirstSyncBackfill_Step2_EmptyAccountSkip
- PASS: TestFirstSyncBackfill_Step2_ListerGapCloudSkip
- PASS: TestFirstSyncBackfill_Step3_FirstRunFullBackfill
- PASS: TestFirstSyncBackfill_Step3_AlreadyMappedSkip
- PASS: TestFirstSyncBackfill_Step3_RevokedFiltered
- PASS: TestFirstSyncBackfill_Step4_ImportUnlistedSuccess
- PASS: TestFirstSyncBackfill_Step4_CrossCloudSameFingerprint
- PASS: TestFirstSyncBackfill_Step4_ChainFetchFailed
- PASS: TestFirstSyncBackfill_Step5_EstablishMappingSuccess
- PASS: TestFirstSyncBackfill_Step5_RerunNoDuplicateMapping
- PASS: TestFirstSyncBackfill_Step5_MappingBindingConsistency
- PASS: TestFirstSyncBackfill_Step6_SessionConvergenceSuccess
- PASS: TestFirstSyncBackfill_Step6_SecondRunIdempotentEmpty
- SKIP: TestFirstSyncBackfill_Step6_TimeoutPartialConvergence (see Skip Detail)

### api-functional/incremental-skip-drift — 19 PASS / 0 SKIP / 0 FAIL (+2 subtests PASS)

- PASS: TestIncrementalSkipDrift_FullJourneySmoke
- PASS: TestIncrementalSkipDrift_Step1_DeltaSetDetection
- PASS: TestIncrementalSkipDrift_Step1_AllMappedZeroAction
- PASS: TestIncrementalSkipDrift_Step1_EmptyFingerprintDegraded
  - PASS subtest: mapped_instance_degrades_to_skip
  - PASS subtest: unmapped_instance_degrades_to_import
- PASS: TestIncrementalSkipDrift_Step2_MappedSkipSuccess
- PASS: TestIncrementalSkipDrift_Step2_VolcanoSkipSemantics
- PASS: TestIncrementalSkipDrift_Step2_SkipNotFailure
- PASS: TestIncrementalSkipDrift_Step3_BackfillMappingSuccess
- PASS: TestIncrementalSkipDrift_Step3_UniqueKeyReplay
- PASS: TestIncrementalSkipDrift_Step3_UpsertFailedStaticReason
- PASS: TestIncrementalSkipDrift_Step4_DriftRefreshSuccess
- PASS: TestIncrementalSkipDrift_Step4_DuplicateFingerprintRedirect
- PASS: TestIncrementalSkipDrift_Step4_ResignedThenRevoked
- PASS: TestIncrementalSkipDrift_Step5_ReverseLookupLatest
- PASS: TestIncrementalSkipDrift_Step5_MultiHistoryOrder
- PASS: TestIncrementalSkipDrift_Step5_OldMappingNotDeleted
- PASS: TestIncrementalSkipDrift_Step6_SessionReconcileSuccess
- PASS: TestIncrementalSkipDrift_Step6_DriftEventObservable
- PASS: TestIncrementalSkipDrift_Step6_NoFailedEntriesAfterDrift

### api-functional/manual-scheduler-race — 16 PASS / 0 SKIP / 0 FAIL

- PASS: TestManualSchedulerRace_FullJourneySmoke
- PASS: TestManualSchedulerRace_Step1_SchedulerRoundStarts
- PASS: TestManualSchedulerRace_Step1_CASAlreadyRunningSkips
- PASS: TestManualSchedulerRace_Step1_CASReleasesAfterTerminal
- PASS: TestManualSchedulerRace_Step2_ManualTriggerSuccess
- PASS: TestManualSchedulerRace_Step2_Conflict409
- PASS: TestManualSchedulerRace_Step2_Unauthorized
- PASS: TestManualSchedulerRace_Step3_OverlappingRoundsConverge
- PASS: TestManualSchedulerRace_Step3_CrossCloudSameFingerprint
- PASS: TestManualSchedulerRace_Step3_ImportPathNotBlockedByCAS
- PASS: TestManualSchedulerRace_Step4_DuplicateRedirectSuccess
- PASS: TestManualSchedulerRace_Step4_BothSessionsSuccessNoFailed
- PASS: TestManualSchedulerRace_Step4_NoSecondLedgerRow
- PASS: TestManualSchedulerRace_Step5_RaceConvergenceSuccess
- PASS: TestManualSchedulerRace_Step5_PollProgressViaSessionID
- PASS: TestManualSchedulerRace_Step5_OperatorAttribution

### api-functional/cloud-failure-isolation — 13 PASS / 0 SKIP / 0 FAIL

- PASS: TestCloudFailureIsolation_FullJourneySmoke
- PASS: TestCloudFailureIsolation_Step1_TriggerRoundSuccess
- PASS: TestCloudFailureIsolation_Step1_GuardReleasedAfterPartialFailed
- PASS: TestCloudFailureIsolation_Step2_EnumerateUnitsSuccess
- PASS: TestCloudFailureIsolation_Step2_EmptyAccountSkip
- PASS: TestCloudFailureIsolation_Step2_InvalidAccountIsolated
- PASS: TestCloudFailureIsolation_Step3_ListFailedIsolatedStaticReason
- PASS: TestCloudFailureIsolation_Step3_AccountLoadFailedCloudLevel
- PASS: TestCloudFailureIsolation_Step4_RemainingCloudsComplete
- PASS: TestCloudFailureIsolation_Step4_RerunBackfillsFailedCloud
- PASS: TestCloudFailureIsolation_Step5_PartialFailedTerminalSummary
- PASS: TestCloudFailureIsolation_Step5_TerminalStateBinarySemantics
- PASS: TestCloudFailureIsolation_Step5_RerunConvergesIdempotent

### api-functional/manual-sync-endpoint-guard — 15 PASS / 0 SKIP / 0 FAIL (+5 subtests PASS)

- PASS: TestManualSyncEndpointGuard_FullJourneySmoke
- PASS: TestManualSyncEndpointGuard_Step1_Success200Summary
- PASS: TestManualSyncEndpointGuard_Step1_Conflict409Running
- PASS: TestManualSyncEndpointGuard_Step1_ForbiddenNonOpsEngineer
  - PASS subtest: viewer_rejected_403
  - PASS subtest: auditor_rejected_403
  - PASS subtest: ops_supervisor_rejected_403
  - PASS subtest: unknown_explicit_cert_role_rejected_403
  - PASS subtest: capability_code_only_(_cert:settings_)_rejected_403
- PASS: TestManualSyncEndpointGuard_Step1_Unauthorized
- PASS: TestManualSyncEndpointGuard_Step1_ForbiddenNoRoleSignal
- PASS: TestManualSyncEndpointGuard_Step2_PollViaSessionID
- PASS: TestManualSyncEndpointGuard_Step2_ConflictNoPollHandle
- PASS: TestManualSyncEndpointGuard_Step2_EmptySessionIDNoImportSession
- PASS: TestManualSyncEndpointGuard_Step3_FailureSummaryWhitelist
- PASS: TestManualSyncEndpointGuard_Step3_ImportFailureDecoupled
- PASS: TestManualSyncEndpointGuard_Step3_ZeroFailureCompletedBaseline
- PASS: TestManualSyncEndpointGuard_Step4_RepeatWithNewInstances
- PASS: TestManualSyncEndpointGuard_Step4_RepeatZeroDeltaConverged
- PASS: TestManualSyncEndpointGuard_Step4_ServiceNotWired500

---

## Failed Tests Detail

None.

## Skip Detail

- **TestFirstSyncBackfill_Step6_TimeoutPartialConvergence** — test-emitted skip reason: "overall sync budget is not exhaustible at the API layer (service-internal 10min context, no external knob); timeout semantics covered by unit-layer TestCertSync_OverallTimeout with an injected budget". This is the single skip recorded at gen-test-scripts time (dispatcher pre-accepted for this run per the recorded handoff). Not a failure; recorded here for traceability.

## Environment Notes

- Whole-module compile check `go build -p 1 ./...` passed (exit 0) immediately before the run — includes `internal/shared/cloudx/logquery/*` currently being modified by a parallel session; no cross-session breakage at run time.
- Host `-race` unavailable (no cgo): concurrency assertions are carried by unit-layer fence tests (per pipeline record); sequential per-package execution avoids host memory pressure (no link.exe stalls encountered this run).
- `-timeout 180s` per package per pipeline convention; all packages completed in ~0.3s.
