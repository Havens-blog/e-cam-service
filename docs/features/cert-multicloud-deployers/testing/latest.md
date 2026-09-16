# Test Report: cert-multicloud-deployers

**Date**: 2026-09-16
**Duration**: ~1.2s test execution (4 sequential `go test -count=1 -v` package runs; compile excluded)
**Surface**: api (single-surface, recipe-prefix: none - repo has no justfile; mapped to per-journey `go test` per established repo convention)
**Runner**: go test (text-verbose format), hermetic in-process harness (tests/multicloudtest: real ChangeService/Execute/Rollback/Query/OrphanCleanup/VerifyWindow + gin HTTP surface; only EIAM auth and three-cloud SDK adapters stubbed)
**Command**: `go test -count=1 -v ./tests/<journey>/...` (no -race: unavailable on this host, no cgo)

## Summary

| Type | Total | Pass | Fail | Skip |
|------|-------|------|------|------|
| api-functional/bind-failure-compensation | 14 | 14 | 0 | 0 |
| api-functional/multicloud-cert-replacement | 21 | 21 | 0 | 0 |
| api-functional/rollback-restore-old-cert | 14 | 14 | 0 | 0 |
| api-functional/three-cloud-product-matrix | 18 | 18 | 0 | 0 |
| **All** | **67** | **67** | **0** | **0** |

**Result**: ALL PASS (67/67, 0 failed, 0 skipped; 69 leaf PASS lines including 2 subtests of TestRebind_MultiResourceAllReboundOrExplicitFailure)

Test count matches the staged pipeline inventory: 63 contract outcomes + 4 smoke tests = 67 (21/14/14/18 per journey).

---

## Confidence Rating

**Level**: LOW (REVIEW)
**Confirmed Fact Ratio**: 0.00 (0 runtime+confirmed facts / 63 contract outcomes)

### Confidence Distribution

| Level  | Count | Percentage |
|--------|-------|------------|
| HIGH   | 0     | 0%         |
| MEDIUM | 0     | 0%         |
| LOW    | 67    | 100%       |

### Verification Summary

| Mark   | Count |
|--------|-------|
| VERIFY | 0     |
| REVIEW | 67    |

Note: ratio reflects .forge/fact-table.json state (60 entries, all source=static, confidence=inferred; no runtime+confirmed entries). Runtime evidence does exist - all 67 tests passed at generation time (T-test-gen-scripts, b67ebd8) and again in this fresh -count=1 run - but the run-tests skill does not write runtime facts back to the fact table. Fact entries may be upgraded to runtime+confirmed by later live-verification tasks to raise the rating. Informational only; LOW confidence does not block or skip execution.

---

## Results by Test Case

### api-functional/bind-failure-compensation (14 PASS, 0.286s)

- TestBindFailureCompensation_FullLoopSmoke: PASS
- TestBindFailure_ExplicitFailureMarksItemFailed: PASS
- TestBindFailure_RateLimitedRetriesBounded: PASS
- TestBindFailure_InterruptedBeforeBindRecoveredByTimeout: PASS
- TestBindFailure_UnauthenticatedRejected401: PASS
- TestCompensation_CleanupOrphanDeletesUploadedCert: PASS
- TestCompensation_DoubleInvocationIdempotent: PASS
- TestCompensation_MappingLookupMissKeepsOrphanCandidate: PASS
- TestCompensation_OrphanTransitionEnqueuesCleanup: PASS
- TestCompensation_DeleteFailureKeepsOrphanAndRetries: PASS
- TestCleanupQueue_ConsumesOrphanDeletesMapping: PASS
- TestCleanupQueue_RerunProducesNewIdsAndActiveMapping: PASS
- TestCleanupQueue_AlreadyDeletedIsIdempotentSuccess: PASS
- TestCleanupQueue_SweepIsolatesSingleFailure: PASS

### api-functional/multicloud-cert-replacement (21 PASS, 0.295s)

- TestMulticloudCertReplacement_FullLifecycleSmoke: PASS
- TestGenerateChangeList_ThreeCloudReferencesExecutable: PASS
- TestGenerateChangeList_K8sManagedReferenceSkipped: PASS
- TestGenerateChangeList_UnauthenticatedRejected401: PASS
- TestConfirmAndExecute_BatchedTwoPhaseRuns: PASS
- TestConfirmBatch_GateBlocksWhenPreviousBatchIncomplete: PASS
- TestConfirmAndExecute_UnauthenticatedRejected401: PASS
- TestExecuteUploadPhase_UploadsMaterialAndWritesMapping: PASS
- TestExecuteUploadPhase_RateLimitedRetriesWithNewName: PASS
- TestExecuteUploadPhase_CrashBeforeBindRecoveredByTimeout: PASS
- TestExecuteBindPhase_BindsTargetResource: PASS
- TestExecuteBindPhase_AwsNlbListenerCertificatesBranch: PASS
- TestExecuteBindPhase_AzureAppGatewayKvReference: PASS
- TestExecuteMappingWrittenActivePerCloudForm: PASS
- TestExecuteMappingAbnormalIdExplicitRejection: PASS
- TestExecuteMappingUpsertIdempotentOverwrite: PASS
- TestVerifyWindow_ConsecutiveProbesConfirmCompletion: PASS
- TestVerifyWindow_ProbeMismatchNotPassedAndExpiryFinalization: PASS
- TestOrphanCleanup_ConsumesOrphanAfterTerminalState: PASS
- TestOrphanCleanup_SkipsWhenOwnedByInFlightOrder: PASS
- TestOrphanCleanup_ProtectPeriodSkipKeep: PASS

### api-functional/rollback-restore-old-cert (14 PASS, 0.273s)

- TestRollbackRestoreOldCert_FullLoopSmoke: PASS
- TestRollbackRequest_AcceptedForCompletedReplacement: PASS
- TestRollbackRequest_EntryStateInvalidRejected: PASS
- TestRollbackRequest_ScopeEmptyOrNoSuccessItemsRejected: PASS
- TestRollbackRequest_UnauthenticatedRejected401: PASS
- TestRollbackPrecheck_PassesForValidOldCert: PASS
- TestRollbackPrecheck_TargetInvalidBlocksWholeRollback: PASS
- TestRollbackPrecheck_CrossCloudCredentialMismatchRejected: PASS (expected negative-path ERROR log: credential cloud "aws" is not huawei)
- TestRebind_RestoresOldCloudCertReference: PASS
- TestRebind_RepeatedRollbackIsIdempotent: PASS
- TestRebind_MultiResourceAllReboundOrExplicitFailure: PASS (subtests: all_rebind_succeeds PASS, one_rebind_failure_fails_loudly PASS)
- TestRollbackObservation_ProbeConsistentWithOldCert: PASS
- TestRollbackObservation_CleanupQueueRaceProtected: PASS
- TestRollbackObservation_ProbeLagConvergesWithoutStateChange: PASS

### api-functional/three-cloud-product-matrix (18 PASS, 0.291s)

- TestGenerateChangeList_AllNineCombosExecutable: PASS
- TestGenerateChangeList_EmptyComboProducesNoItems: PASS
- TestGenerateChangeList_UnauthenticatedRejected401: PASS
- TestHuawei_FourProductsTwoPhaseSuccess: PASS
- TestHuawei_BindResolveFailureIsolated: PASS
- TestHuawei_UploadNameConflictImmediateRetry: PASS
- TestAwsCloudFront_UploadPinnedToUseast1: PASS
- TestAwsCloudFront_DefaultRegionIndependent: PASS
- TestAwsCloudFront_RebindIdempotentTerminalState: PASS
- TestAwsAlbNlb_ListenerCertificatesBranch: PASS
- TestAwsNlb_ExplicitProductBranchNotCloudFront: PASS
- TestAwsListener_RegionMismatchRejected: PASS
- TestAzure_TwoProductsTwoPhaseSuccess: PASS
- TestAzure_AppGwInlineFormRejected: PASS
- TestAzure_KvMissingExplicitFailure: PASS
- TestAllCombos_MappingConsistentAfterExecution: PASS
- TestCrossCloudMixingRejected: PASS
- TestThreeCloudProductMatrix_FullMatrixSmoke: PASS

---

## Failed Tests Detail

None.
