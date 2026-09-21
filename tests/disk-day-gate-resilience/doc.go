// @feature disk-ops-insight @api-functional
//
// Journey disk-day-gate-resilience ( High ): the disk key of the persistent
// daily gate — atomic claim, commit after a successful collect, restart
// consistency ( x3 restarts each trigger exactly one collect ), and the four
// resource keys ( nas/cdn/oss/disk ) reusing the shared gate without
// interference — plus the failure paths: write-failure exponential backoff
// with escalation alert, read-failure 5-minute backoff, feature-flag rollback
// to the memory gate, and concurrent claim races with a single winner.
//
// Contract tests: one function per Contract outcome
// ( docs/features/disk-ops-insight/testing/disk-day-gate-resilience/contracts/ ).
// Journey smoke: TestDiskDayGateResilience_FullJourneySmoke.
//
// Scope notes ( asserted exemptions / anchored code-reality ):
//   - feature-flag-rollback ( step-3 ) is exempt here: the memory-gate
//     rollback branch lives in the unexported scheduler method
//     checkDiskMetricsCollectionMemory and is covered by the internal unit
//     suite internal/cam/scheduler/auto_sync_disk_metrics_test.go; the
//     exported gate surface cannot observe the flag.
//   - no-gate-assembly-safe-skip ( step-3 ) is exempt for the same reason
//     ( unexported scheduler method, internal suite coverage ).
//   - Key isolation holds at the scheduler_state store layer ( every claim
//     write is scoped by resource_type ). The in-process write-backoff window
//     is gate-instance scoped ( shared by all keys on one gate instance,
//     bounded at 5 minutes ) — asserted where observable via the exported
//     surface.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package disk_day_gate_resilience
