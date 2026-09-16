// @feature cert-multicloud-deployers @api-functional
//
// Package multicloud_cert_replacement hosts the API-functional tests for the
// "multicloud-cert-replacement" journey ( golden path ): a three-cloud
// certificate replacement loop — changelist generation, batched confirm and
// execute, two-phase upload/bind per cloud, CloudCertMapping writes, verify
// window probing and old-cert orphan cleanup — exercised through the
// production /api/v1/certs/changes HTTP surface.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package multicloud_cert_replacement
