---
status: "completed"
started: "2026-09-17 09:49"
completed: "2026-09-17 10:16"
time_spent: "~27m"
---

# Task Record: 1 火山云证书库发现适配器

## Summary
Volcano certificate-library discovery adapter: internal/shared/cloudx/volcano/cert.go adds CertAdapter with ListCertificates (paged enumeration of CertificateGetInstanceList with TotalCount defensive upper bound, per-instance GetCertificate chain fetch, two-stage filtering of revoked/non-Issued instances at both List and Get stages, per-instance failure tolerance returning collected items plus aggregate error) and GetCertificate (CertificateGetInstance Chain PEM list -> leaf parse yielding fingerprint SHA256 lowercase hex, CN, DNS SAN, validity - same semantics as cert/domain ParseCertAndKey buildParsedCert). PrivateKey response fields are never read; returned struct has no key field; chain material passes cloudx.SanitizeCertChainPEM with Zeroize of the raw buffer; SDK narrow interface exposes only the two read methods (structural read-only guarantee). cert_test.go: 11 tests, 34/34 package-green.

## Changes

### Files Created
- internal/shared/cloudx/volcano/cert.go
- internal/shared/cloudx/volcano/cert_test.go

### Files Modified
无

### Key Decisions
- Fingerprint parsed from Chain leaf (SHA256 of leaf DER, lowercase hex) rather than cloud FingerPrintSha256 field, so the value is isomorphic with the ledger unique key (AC2 mandates chain-parse semantics; chain absent = import-impossible = error, no fallback)
- SDK narrow interface certLibraryAPI uses the WithContext variants so caller cancellation/timeouts propagate; *CERTIFICATESERVICE satisfies it, fake implements it
- List-stage ErrCertFiltered results are silently skipped (filter semantics), only genuine Get/parse failures land in the aggregate error
- Pagination terminates on short page or empty page (volcano lb.go semantics) plus TotalCount ceiling as drift guard
- No rate limiter and no write-method sentinels: existing volcano adapters have neither; read-only is enforced by the narrow interface

## Test Results
- **Tests Executed**: Yes
- **Passed**: 34
- **Failed**: 0
- **Coverage**: 88.2%

## Acceptance Criteria
- [x] ListCertificates paginates all instances (page count, instance count, field passthrough match real pagination semantics)
- [x] GetCertificate parses fingerprint (SHA256 lowercase hex)/CN/SAN/validity from Chain, matching existing CAS ParseCertAndKey semantics
- [x] Revoked (IsCertificateRevoked=true) or non-issued status instances filtered at List and Get stages, not offered for import
- [x] Single-instance failure (Get error/chain parse failure) returns collected items plus error without interrupting later entries
- [x] PrivateKey field never enters return struct or logs (private key read-then-dropped)
- [x] Only pre-existing volcengine-go-sdk v1.2.9 certificateservice package used, no new dependencies

## Notes
Static checks: go build -p 1 ./... exit 0; both new files gofmt-clean (repo-wide make fmt intentionally skipped - pre-existing CRLF files are not this task's responsibility); golangci-lint unusable on this host (go1.24 binary vs go1.25.5 module) so go vet on the package ran instead (VET OK). Tests: go test ./internal/shared/cloudx/volcano/ = 34 PASS / 0 FAIL / 0 SKIP (11 new); -race not runnable on this host (no cgo/gcc). Coverage 88.2% is cert.go-only (go tool cover -func on the cert.go-filtered profile); whole-package instrumentation normally fails because kafka.go carries a leading BOM - measured via a transient BOM strip restored byte-exact (cmp verified); whole-package statement coverage 9.2% reflects pre-existing untested adapters. File scope honored: only the two new files exist in git status; go.mod/go.sum untouched.
