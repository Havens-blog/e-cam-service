---
status: "completed"
started: "2026-09-16 11:57"
completed: "2026-09-16 11:57"
time_spent: ""
---

# Task Record: 3 Azure 部署器：Key Vault 证书库 + CDN(Front Door)/ALB(App Gateway) 两产品绑定

## Summary
Implemented Azure five-method CloudDeployer: full CertAdapter (internal/shared/cloudx/azure/cert.go) embedding the existing discovery adapter with real write methods over Key Vault/Front Door/Application Gateway REST (no new SDK deps), plus deployer-layer AzureDeployer (internal/cert/deployer/azure_deployer.go) wired for the CloudAPIChannel two-stage flow. UploadCert imports a PEM bundle (leaf-first cert chain + private key) via the KV certificate import API and returns the KV secret ID as the cloud cert ID; BindResource routes cdn -> Front Door frontend-endpoint custom-HTTPS PATCH (AzureKeyVault source + Enabling, vault ARM resource id resolved from subscription-level list) and alb -> App Gateway listener SSL-certificate sub-resource PUT repointed to the KV secret; GetCert exposes KV existence + SHA256-aligned fingerprint for rollback validation; CleanupOrphan deletes by cert name with 404/soft-delete idempotency (purge deliberately not executed); ListReferences reuses discovery refs with the three-tier fingerprint resolution. KV reference normalization is a single point in the adapter (Hard Rule); private-key material is zeroized after use and never logged. module.go assembly left untouched for Task 4.

## Changes

### Files Created
- internal/shared/cloudx/azure/cert.go
- internal/shared/cloudx/azure/cert_test.go
- internal/cert/deployer/azure_deployer.go
- internal/cert/deployer/azure_deployer_test.go

### Files Modified
- internal/shared/cloudx/azure/cert_discovery.go

### Key Decisions
- Kept the existing no-Azure-SDK REST direct-call design of cloudx/azure (task note suggested adding the Azure SDK; the discovery adapter's documented REST-only decision is newer and package-authoritative, so the write plane extends the same net/http infrastructure and adds zero go.mod dependencies)
- Cloud cert ID = versioned KV secret ID (https://{vault}.vault.azure.net/secrets/{name}/{version}); sid missing falls back to versionless form compatible with discovery refs; App Gateway/Front Door bindings consume this ID directly per implementation notes
- PEM import form (contentType application/x-pem-file) chosen so the KV secret value reads back as PEM and the discovery GetCert sanitize/leaf-SHA256 fingerprint path holds unchanged; PFX branch (base64 PFX + pwd) documented as a single-point extension
- ALB bind updates the listener's existing SSL certificate resource keyVaultSecretId (child-resource PUT); inline data-form certificate resources are explicitly rejected (same blind-spot stance as discovery), and the idempotency check runs before any write
- Front Door bind patches the frontend endpoint with customHttpsConfiguration + provisioningState=Enabling and resolves the vault ARM resource id from the subscription-level vault list; equality idempotency (secretId direct or vault/name/version composite) is checked before vault resolution so replays do not need vault read access
- Upload target vault is configured via WithKeyVaultName/WithKeyVaultURI options with AZURE_KEY_VAULT_NAME/AZURE_KEY_VAULT_URI env fallback (mirrors tenant/subscription handling); upload fails explicitly when unset; cleanup derives the vault from the secret ID itself
- module.go/discoveryOnlyClouds left untouched - assembly convergence belongs to Task 4 per Hard Rules

## Test Results
- **Tests Executed**: Yes
- **Passed**: 173
- **Failed**: 0
- **Coverage**: 83.7%

## Acceptance Criteria
- [x] UploadCert imports into Key Vault (PFX/PEM import API with private key), returns KV cert reference (name/version); private key in-memory only and zeroized after use
- [x] BindResource routes by product - CDN(Front Door) custom-domain HTTPS config, ALB(Application Gateway) listener SSL cert reference via KV reference (no direct upload ID)
- [x] GetCert KV in-vault status (reference existence + SHA256 aligned fingerprint) usable for rollback target validation
- [x] CleanupOrphan KV cert delete with idempotency incl. KV soft-delete semantics
- [x] ListReferences reuses discovery reference shape + three-tier fingerprint resolution (mapping reverse-lookup -> GetCert fallback -> deterministic placeholder)
- [x] Unit tests: fake Key Vault/Front Door/App Gateway cover five methods x two products incl. KV reference normalization, bounded rate-limit backoff, bind-failure compensation path

## Notes
testsPassed=173 is the sum of the two changed packages' green runs (cloudx/azure 22, cert/deployer 151, includes pre-existing suites; 29 new top-level test funcs, 52 counting subtests). coverage field reports the lower of the two packages (cloudx/azure 83.7%, cert/deployer 88.0%) against the 80% target. Gate mapping per repo precedent: compile=go build -p 1 ./... (exit 0), fmt=gofmt delta 0 bytes on all 5 touched files via CRLF-stripped temp copies, lint=go vet ./... (exit 0; golangci-lint/staticcheck unavailable on this host). -race not run (no cgo/gcc on host); tests executed per-package to avoid host OOM. U+FFFD scan clean on all touched files.
