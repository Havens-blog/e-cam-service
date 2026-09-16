// @feature cert-multicloud-deployers @api-functional
//
// Package three_cloud_product_matrix hosts the API-functional tests for the
// "three-cloud-product-matrix" journey: all nine cloud×product combinations
// ( huawei cdn/waf/alb/nlb, aws cdn/alb/nlb, azure cdn/alb ) share the same
// five-method port semantics and two-phase orchestration, with per-cloud ID
// forms ( SCM UUID / ACM ARN / KV secret ID ) and per-product bind branches.
//
// NOTE on adapter-internal boundaries: region pinning ( CloudFront us-east-1 ),
// non-ARN refusals, inline-data rejections and Key Vault target resolution
// live inside the real cloudx adapters ( covered by cloudx unit tests ); at
// this surface those outcomes are encoded through the deployer/channel/item
// propagation contract with injected adapter errors.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.
package three_cloud_product_matrix
