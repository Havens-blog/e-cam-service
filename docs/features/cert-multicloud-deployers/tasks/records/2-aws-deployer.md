---
status: "completed"
started: "2026-09-16 10:35"
completed: "2026-09-16 11:07"
time_spent: "~32m"
---

# Task Record: 2 AWS 部署器：ACM 证书库 + CloudFront/ALB/NLB 三产品绑定

## Summary
AWS CloudDeployer: full five-method CertAdapter (ACM ImportCertificate/DescribeCertificate/DeleteCertificate + CloudFront GetDistribution/UpdateDistribution + ELBv2 DescribeListenerCertificates/AddListenerCertificates) plus deployer-layer AwsDeployer wired to the existing CloudAPIChannel two-phase orchestration, with fake-SDK unit tests covering five methods x three products.

## Changes

### Files Created
- internal/shared/cloudx/aws/cert.go
- internal/shared/cloudx/aws/cert_test.go
- internal/cert/deployer/aws_deployer.go
- internal/cert/deployer/aws_deployer_test.go

### Files Modified
无

### Key Decisions
- Full CertAdapter embeds *CertDiscoveryAdapter (task-1 huawei pattern): read-only ListReferences/GetCert reused as real implementations, three write methods overridden; discovery-only sentinel behavior of CertDiscoveryAdapter itself preserved (existing sentinel test untouched and green)
- CloudFront upload region locked to us-east-1 via dedicated constant certCloudFrontUploadRegion (semantically independent from certDefaultRegion); product-aware region routing in adapter (cdn -> us-east-1, alb/nlb -> account primary region)
- Fixed CloudDeployer.UploadCert port has no product arg, so deployer uploads with CDN rule (us-east-1) making every artifact CloudFront-bindable; ALB/NLB binds validate cert-ARN region == listener-ARN region and fail explicitly on cross-region targets (proposal 'explicit failure, no guessing')
- NLB binding uses AddListenerCertificates like ALB: ELBv2 v2 SDK has no UpdateListenerAttribute certificate API (documented in elbBindCertAPI); single-cert add without IsDefault, default certificate slot untouched, idempotent pre-check via DescribeListenerCertificates pagination
- CloudFront bind: GetDistribution ETag + IfMatch concurrency control, ViewerCertificate-only replacement (Aliases CNAME surface untouched), SSLv3/TLSv1 bumped to TLSv1.2_2021 for custom certs, idempotent skip when viewer already references target ARN
- CleanupOrphan: DescribeCertificate.Type identifies AWS-managed certs (AMAZON_ISSUED/PRIVATE) and refuses deletion explicitly; already-deleted normalized to idempotent success at both describe and delete stages
- ACM has no certificate-name field: upload name carried via Name tag (C7 per-attempt unique formula unchanged); ImportCertificate private key passed as []byte and cloudx.Zeroize'd after call (asserted in tests via fake-held slice)
- module.go intentionally untouched: register/scan-adapter convergence is Task 4 scope (task-1 precedent); go.mod untouched (acm/cloudfront/elbv2 modules already present)

## Test Results
- **Tests Executed**: Yes
- **Passed**: 169
- **Failed**: 0
- **Coverage**: 87.6%

## Acceptance Criteria
- [x] UploadCert: ImportCertificate uploads bundle to ACM returning ARN; CloudFront certs pinned to us-east-1 (other products use account primary region); private key plaintext memory-only and Zeroize'd after use
- [x] BindResource: per-product routing - CloudFront distribution (ViewerCertificate), ALB listener cert (AddListenerCertificates), NLB listener cert (AddListenerCertificates as the actual ELBv2 API form, UpdateListenerAttribute does not exist in v2 SDK - documented)
- [x] GetCert: ACM in-library status (ARN existence + SHA256 fingerprint alignment) + rollback-target validity inputs; IAM-hosted ids surface structured degradation marker (fail-safe)
- [x] CleanupOrphan: ACM imported cert deletion (DeleteCertificate, idempotent success for already-deleted; AWS-managed certs explicitly identified and refused)
- [x] ListReferences: reuse discovery adapter reference form + fingerprint resolution (mapping lookup -> GetCert fallback -> deterministic placeholder)
- [x] Unit tests: fake ACM/CloudFront/ELBv2 SDK covering five methods x three products (incl. CloudFront us-east-1 region assertion, rate-limit backoff bounds, bind-failure compensation path via CloudAPIChannel)

## Notes
Coverage: deployer package 87.6% (80% target met); new cert.go ~87% average per-function (real-client factories exercised offline); cloudx/aws package-level 15.0% dominated by ~30 pre-existing untested infra adapter files (ecs/rds/s3/kafka...) out of task scope. Test counts: 169 = 37 (cloudx/aws: 14 new + 23 pre-existing unchanged) + 132 (deployer: 20 new + 112 pre-existing unchanged). Gates: compile via go build -p 1 ./... exit 0; go vet on both changed packages clean (golangci-lint not installed per Makefile fallback; staticcheck unusable on this host); gofmt verified clean on all four new files via CRLF-neutral temp-file check - repo-wide make fmt deliberately skipped (would rewrite hundreds of pre-existing CRLF files, out of single-task scope). -race not used: no cgo/gcc on this host (known limitation, correctness via code review + bounded CAS-free design). go.mod unchanged: all required AWS SDK v2 modules already present.
