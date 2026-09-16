// @feature cert-multicloud-deployers @api-functional
//
// Package rollback_restore_old_cert hosts the API-functional tests for the
// "rollback-restore-old-cert" journey: rollback by old cloud cert ID —
// request gates, GetCert target precheck, rebind, verify observation and
// cleanup coordination.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package rollback_restore_old_cert
