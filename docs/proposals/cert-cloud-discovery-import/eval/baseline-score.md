# Baseline Score — cert-cloud-discovery-import

- Iteration: baseline
- Date: 2026-08-25
- Scorer: CTO persona, adversarial rubric protocol
- Target: 900/1000

## Codebase Reality-Check Performed

The proposal makes many concrete code claims. Verified against the actual repositories:

| Claim | Verdict | Evidence |
|---|---|---|
| Ledger empty-state copy at `e-cam-web/src/views/cert/ledger/index.vue:28` | ACCURATE | exact text found at line 28 |
| `reference_scan_service.go` discovers refs across five clouds + K8s | ACCURATE | file exists, `ListReferences`/`GetCert` interfaces |
| GetCert channels: Aliyun `response.Cert`, Tencent `CertificatePublicKey`, Azure KV secret, AWS ACM GetCertificate | ACCURATE | `cloudx/aliyun/cert.go:325`, `cloudx/tencent/cert.go:224`, `cloudx/azure/cert_discovery.go`, `cloudx/aws/cert_discovery.go:380` |
| "当前仅用于算指纹后即丢弃" (PEM not retained) | ACCURATE | all four `CloudCertInfo` structs carry only `Exists/NotAfter/Fingerprint` — no PEM field |
| **"AWS ACM GetCertificate 响应未映射 PEM（仅有接口定义）"** | **INACCURATE** | `aws/cert_discovery.go:380-397` already calls `GetCertificate`, parses `output.Certificate` via `parseCertLeafPEM` into fingerprint+NotAfter, with fake-based unit test (`cert_discovery_test.go:140-146`). Only the struct-level PEM field is missing — same gap as the other three clouds. |
| `uk_fingerprint` / `ErrDuplicateFingerprint` | ACCURATE | `domain/repository.go:12-14` |
| `uk_fp_cloud_account` two-phase dedup | ACCURATE | `domain/repository.go:244`, `domain/cloud_cert_mapping.go:21` |
| ImportCert `keyPEM` empty → `fingerprint_only` branch | ACCURATE | `import_service.go:96` |
| UploadKey endpoint for later upgrade | ACCURATE | `web/cert_handler.go:33,172` |
| Batch session 10-min timeout, persist-then-async, 202 semantics | ACCURATE | `import_service.go:20-21,146-171`, `cert_handler.go:159` |
| Placeholder fingerprint mechanism (Tencent SHA-1 etc.) | ACCURATE | `cert_reference.go:39`, `reference_scan_service.go:432` (`sha256("certscan-unresolved:"+cacheKey)`) |
| Huawei SCM SHA-1, no PEM | ACCURATE | `huawei/cert_discovery.go:91,454` |
| Frontend branch `feat/platform-user-management` | ACCURATE | current branch of e-cam-web |

## Phase 1 — Reasoning Audit

**Problem → Solution**: The empty-ledger problem is real and the reference-driven import genuinely closes the first-registration loop for *referenced* certificates. The un-referenced-cert gap is honestly acknowledged and deferred with user confirmation.

**Solution → Evidence**: Unusually strong — nearly every code claim checks out (see table). One material mischaracterization: the AWS "unmapped response" risk premise is false; the mapping is implemented and tested.

**Evidence → Success Criteria**: Two SC promises are **unsatisfiable as written** given the codebase and the proposal's own constraints (see contradictions below). Both stem from the same root: the preview/import design assumes data (notAfter per reference; placeholder-fingerprint backfill) that no existing collection stores and that Out-of-Scope explicitly freezes.

**Self-contradiction check**: The solution does **partially reintroduce the stated problem** for one subset: for placeholder-fingerprint references ("引用扫描结果无法关联到证书"), import writes a *real* fingerprint to the ledger while `CertReference` keeps its *placeholder* fingerprint — the ledger-detail join (`ledger_service.go:363` `ListByFingerprint`) never matches, so those references remain unassociated after import. No backfill step exists in scope, and Out-of-Scope ("引用…逻辑不动") blocks adding one.

### Contradictions

```
CONTRADICTION: SC-1 "响应耗时 < 1s（纯 DB 聚合，无云 API 调用）" + SC-2 "预览返回的每个条目含 …notAfter" ↔ Assumptions Challenged "预览需调云 API 逐证核验 → 推翻：纯 DB 比对即可秒出" + Out of Scope "扫描能力本身的变更（快照/引用/覆盖率逻辑不动）"
Type: direction-clash
Evidence: CertReference (cert_reference.go:37-50), CloudCertMapping, ScanSnapshot/CoverageMeta 均无 notAfter 字段；notAfter 只存在于台账 Certificate（主场景"台账为空"时必然为空）。三个可能的供给路径被文档同时堵死：预览调云 API（被 Assumptions Challenged 推翻）、扫描时落库（被 Out of Scope 冻结）、台账反查（主场景无数据）。主路径下预览"到期时间"列必然全空。
Resolution required: 三选一——(a) 预览条目 notAfter 标注"仅已登记条目可得"并从用户旅程列清单中降级；(b) 将"扫描落库 notAfter"纳入 In Scope；(c) 接受预览逐条 GetCert 并放弃 <1s/无云 API 承诺。
```

```
CONTRADICTION: SC-6 "导入完成后，存量 CertReference 按指纹自动关联（台账详情引用列表非空）"（无条件表述）↔ Key Scenario "占位指纹引用…导入时 GetCert 拿 PEM 解析成功则正常登记" + Out of Scope "引用…逻辑不动"
Type: mutual-exclusion
Evidence: 占位指纹 = sha256("certscan-unresolved:"+cacheKey)（reference_scan_service.go:432）；导入成功写入台账的是真实指纹；台账详情按 refs.ListByFingerprint(真实指纹) 查询（ledger_service.go:363）→ 占位指纹引用永远匹配不上。In Scope 仅列"GetCert→解析→指纹登记→CloudCertMapping 幂等建档"，无 CertReference 指纹回填步骤。该子集的引用在导入后仍不关联——正是 Problem 声称要解决的"引用扫描结果无法关联到证书"。
Resolution required: 在 In Scope 增加"导入解析成功后回填同 (cloud+accountKey+cloudCertId) 引用的真实指纹"步骤，或将 SC-6 收窄为"真实指纹引用自动关联"并明示占位子集豁免。
```

Additional pre-score anchors:
- Risk #1 premise ("仅有接口定义") contradicts implemented+tested code — risk register chases a non-problem while two real risks (placeholder non-association, async credential sourcing) are absent.
- Async session needs per-account cloud creds for GetCert after the HTTP request ends; batch import never needed creds (files ride the request). Scan solves this via `ScanAccountSource.ActiveByCloud` — the proposal never says the import session will source creds this way. Solvable, but unstated.

## Phase 2 — Rubric Scores

### 1. Problem Definition — 102/110

- Problem stated clearly: **37/40**. Unambiguous, specific, one paragraph a stranger can replay. Minor: "台账当前为空" is a runtime-state assertion evidenced only by the UI's designed empty-state copy, not by data.
- Evidence provided: **38/40**. Three code-level citations, all located and accurate ("台账页���态文案…"、reference_scan_service、四云 GetCert). Among the strongest evidence sections I've reviewed.
- Urgency justified: **27/30**. "每多等一天，到期监控盲区多存在一天" plus "38 任务已交付但台账空置" gives concrete cost-of-delay; stops short of quantifying exposure (how many certs, nearest expiry).

### 2. Solution Clarity — 111/120

- Approach is concrete: **38/40**. Endpoint paths, per-item pipeline (GetCert→解析→指纹登记→CloudCertMapping), hosting semantics, dedup keys all named.
- User-facing behavior: **40/45**. Journey covers entry points, preview columns, default selection, grey-out, NO_SNAPSHOT guidance, progress polling, partial-failure visibility. Deduction: the preview "到期时间" column describes behavior that cannot exist in the primary (empty-ledger) scenario under the proposal's own constraints — the clearest UX claim is unimplementable as specified.
- Technical direction clear: **33/35**. Reuse map is precise and code-verified. Deduction: credential sourcing for the async import goroutine (request-independent) is never named.

### 3. Industry Benchmarking — 109/120

- Industry solutions referenced: **36/40**. cert-manager, KeyChest, AppViewX named with the CLM "TLS probe + cloud API discovery" pattern correctly characterized.
- ≥3 meaningful alternatives: **28/30**. Do-nothing / TLS probe / CAS full listing / reference-driven — four genuinely distinct options, one industry-validated.
- Honest trade-off comparison: **22/25**. Selected approach's con ("未被引用的库内证书不入场") is stated plainly. "大量无用证书入场" for CAS listing is asserted without support.
- Chosen approach justified: **23/25**. Minimal-increment rationale tied to concrete reuse inventory.

### 4. Requirements Completeness — 99/110

- Scenario coverage: **37/40**. Eight scenarios incl. concurrency, cloud-side deletion, Huawei limitation, placeholder fingerprints, multi-account, 403. Missing: partial-failure snapshot (`PartialFailures` exists in ScanSnapshot) presented as complete inventory.
- NFRs: **34/40**. Security/performance/reliability all concrete. Missing: audit-trail requirement for discovery imports (system has an audit bridge; manual import path is presumably audited — unstated here); no data-freshness NFR for the snapshot-basis preview beyond "沿用现状".
- Constraints & dependencies: **28/30**. Data source, per-cloud API status, K8s exclusion, frontend branch — all named and verified.

### 5. Solution Creativity — 78/100

- Novelty over baseline: **32/40**. "存量回溯登记" vs deploy-time registration is a genuine positioning; honest that it "不是新技术".
- Cross-domain inspiration: **24/35**. Borrows the CLM discovery-driven-registration idea but nothing beyond the immediate domain.
- Simplicity of insight: **22/25**. "扫描与登记解耦两步 + 快照秒出预览" is an elegant reuse move.

### 6. Feasibility — 84/100

- Technical feasibility: **33/40**. Channels and write-path reuse verified real. Deductions: (a) AWS premise false — "AWS ACM GetCertificate 接口已在适配层定义，tech-design 复核响应映射" understates existing readiness and misdirects the first tech-design task; (b) preview notAfter infeasible as specified; (c) async credential sourcing unstated.
- Resource & timeline: **23/30**. Task-count estimate only ("quick 模式预估 8-12 个任务") — no calendar duration, no milestone; the async session + 4-cloud PEM exposure + new modal is optimistic-but-unquantified.
- Dependency readiness: **28/30**. "全部就绪" verified; clean separation from doc-fix-1 credential blockers; fake-adapter test strategy stated.

### 7. Scope Definition — 74/80

- In-scope concrete: **28/30**. Five deliverable-level bullets with endpoint paths and behaviors.
- Out-of-scope explicit: **24/25**. Six named deferrals with destinations.
- Scope bounded: **22/25**. Bounded by reuse and v2 deferrals — but the "快照/引用/覆盖率逻辑不动" freeze is exactly what makes SC-2/SC-6 unsatisfiable; a bounded scope that contradicts its own success criteria.

### 8. Risk Assessment — 78/90

- Risks identified: **27/30**. Five substantive risks.
- Likelihood + impact honest: **27/30**. Varied ratings, no drama inflation.
- Mitigations actionable: **24/30**. Each mitigation is a real action (first-task review + degrade path, timeout+idempotent re-run, import-time Exists check, vitest regression gate). Deductions: Risk #1's premise is factually wrong, so its mitigation spends review budget on a non-problem; the two verified real risks (placeholder references never re-associating; async credential sourcing) are entirely absent from the register.

### 9. Success Criteria — 63/80

- Measurable/testable: **27/30**. Counts, field lists, 403 matrix, hosting-status assertions, idempotent replay — nearly all directly testable. SC-1's "<1s" inside a unit-test context is soft.
- Coverage complete: **20/25**. No SC for the cloud-side-deleted drift scenario (Key Scenario line 43 promises "记因'云侧已不存在'" but no SC verifies it); no SC for placeholder-entry post-import association state (and SC-6 as written cannot hold for that subset).
- SC internal consistency: **16/25**. Two verified intra-set contradictions (SC-1↔SC-2 notAfter; SC-6↔placeholder scenario). The document's own `consistency_check_result: pass` (40 pairs, "conflicts_found: 2 …已当场修正") demonstrably missed both — the pass claim itself is evidence the check was shallow.

### 10. Logical Consistency — 72/90

- Solution addresses stated problem: **29/35**. Core loop closes for real-fingerprint references. For placeholder references the solution reintroduces the stated problem ("引用扫描结果无法关联到证书") — import succeeds, association still fails, and a later re-import just hits `ErrDuplicateFingerprint` "已在台账" skip while the reference stays orphaned.
- Scope ↔ Solution ↔ SC aligned: **22/30**. Two cross-section contradictions anchored above (notAfter; placeholder association), both entangled with the Out-of-Scope freeze.
- Requirements ↔ Solution coherent: **21/25**. Scenarios map cleanly to solution mechanics except the placeholder scenario's downstream association effect, which is not carried through.

## Cross-Dimension Coherence Check

The two contradictions are one root cause viewed from two dimensions: the proposal freezes the scan data model ("快照/引用/覆盖率逻辑不动") while simultaneously promising preview/import outputs (per-entry notAfter; unconditional reference auto-association) that require exactly that data model to change. Feasibility ("无技术阻塞项") and the risk register inherit the blind spot. Scores in D2/D6/D9/D10 were docked for their respective manifestations; no double-counting beyond that.

## Phase 3 — Blindspot Hunt

1. **[blindspot] Partial-snapshot inventory illusion** — "快照过期策略沿用现状，不做新校验" (Constraints) and preview "基于最近 done 快照纯 DB 聚合比对" (Solution). `ScanSnapshot` carries `PartialFailures` (scan_snapshot.go:74-76): a done snapshot can silently lack whole cloud/product channels. The preview then presents a partial inventory as the full "全量未登记" list (Key Scenario line 40), and the user has no way to know certs are missing before clearing the empty state. Needs a partial-failure surfacing rule (banner or per-channel gap notice), or an explicit SC.
2. **[blindspot] Single-shot completion overstated for large accounts** — journey claims "确认 → …完成刷新台账，覆盖率即时生效", while NFR/risk admit "逐条限速调用云 API…整体限时" with a 10-minute cap (verified `batchProcessTimeout = 10 * time.Minute`). A few hundred certs across rate-limited clouds exceeds 600s; first runs will end partial_failed and require repeated user-driven re-runs. The retry UX (does the user re-open preview and re-select each round? does the session remember remaining items?) is unspecified — only "超时条目记因可重跑（幂等）".
3. **[blindspot] AWS IAM-hosted certs silently become per-item failures** — `aws/cert_discovery.go:370-372` explicitly errors for non-ARN cert IDs ("IAM-hosted certificate not covered"); CloudFront legacy references carry exactly that shape. These will surface as cryptic per-item import errors. One sentence in Constraints ("AWS 仅 ARN 形态证书 ID 支持") would fix it.
4. **[blindspot] No audit requirement** — the codebase has a cert audit bridge (`internal/cert/auditbridge.go`, change audit services); discovery import is a bulk ledger-write operation whose audit posture (which actor imported what, from which snapshot) is nowhere specified in NFRs or In Scope.

## Attack Summary

1. [9. Success Criteria]: notAfter promised for every preview entry is unsatisfiable under pure-DB constraint — "预览返回的每个条目含 cloud/accountKey/cloudCertId/引用资源数/指纹存在性（inLedger 布尔）/notAfter/可解析标记" + "纯 DB 聚合，无云 API 调用" — no scan-side collection stores notAfter; pick one: nullable/degraded notAfter, persist-at-scan in scope, or drop the no-API promise.
2. [9. Success Criteria]: SC-6 unconditional auto-association is false for placeholder-fingerprint references — "导入完成后，存量 CertReference 按指纹自动关联" vs "占位指纹引用…导入时 GetCert 拿 PEM 解析成功则正常登记" — ledger gets real fingerprint, CertReference keeps `sha256("certscan-unresolved:"+…)`; add a fingerprint backfill step to In Scope or narrow the SC.
3. [10. Logical Consistency]: Out-of-Scope freeze blocks both fixes — "扫描能力本身的变更（快照/引用/覆盖率逻辑不动）" — this lock must be relaxed for reference-fingerprint backfill (and/or notAfter persistence), or the conflicting promises removed.
4. [8. Risk Assessment / 6. Feasibility]: AWS risk premise is factually wrong — "AWS ACM GetCertificate 响应未映射 PEM（仅有接口定义）" — `aws/cert_discovery.go:380-397` already maps and unit-tests the response; re-baseline the risk to "struct lacks PEM field (same as other three clouds)" and spend the review budget on the two missing real risks (placeholder non-association, async credential sourcing).
5. [6. Feasibility]: async import session credential sourcing unstated — "导入会话逐条限速调用云 API（复用各适配器 waitRateLimit）" — the session outlives the HTTP request; name the cred source (the scan path's `ScanAccountSource.ActiveByCloud` pattern) in the technical direction.
6. [4. Requirements Completeness]: partial-failure snapshots presented as complete inventory — "快照过期策略沿用现状，不做新校验" — define preview behavior when `PartialFailures` is non-empty.
7. [6. Feasibility]: no calendar estimate — "quick 模式预估 8-12 个任务（后端 4-5、前端 3-4、测试随任务）" — add a duration/sequence estimate or state that quick-mode task count is the only commitment.
8. [4. Requirements Completeness]: audit trail for bulk discovery imports unspecified — NFR section covers 私钥/凭证/性能/可靠性 but not who-imported-what accounting on a ledger bulk-write path.
9. [blindspot]: single-shot completion vs 10-min cap arithmetic (attack 2 in blindspot hunt above).
10. [blindspot]: AWS IAM-hosted non-ARN IDs become cryptic per-item failures (attack 3 above).
11. [blindspot]: no audit requirement for bulk ledger writes (attack 4 above).

## Total

**870 / 1000** (target 900 — not met at baseline).

Gate decision: REVISE. Highest-leverage fixes, in order: (1) resolve the notAfter contradiction, (2) resolve the placeholder-fingerprint association contradiction (or narrow SC-6), (3) correct the AWS risk premise and add the two missing risks, (4) address partial-snapshot preview honesty. Fixes 1–3 are document-level; none require scope growth beyond explicitly moving (or declining) the reference-backfill step.
