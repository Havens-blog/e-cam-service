# Iteration 1 — Adversarial Scoring Report

- Document: `D:\Haven\e-cam-service\docs\proposals\cert-cloud-discovery-import\proposal.md`
- Rubric: proposal.md (1000 pts, target 900)
- Iteration: 1 (scored as-is on the page)
- Codebase reality-check performed: backend `D:\Haven\e-cam-service` (internal/cert, internal/shared/cloudx), frontend `D:\Haven\e-cam-web`

## Code Verification Appendix (claims → verdicts)

| Claim in proposal | Code evidence | Verdict |
|---|---|---|
| 空态文案 `e-cam-web/src/views/cert/ledger/index.vue:28` | Exact text present at line 28 | VERIFIED |
| 五云+K8s 引用扫描指纹落库 | `internal/cert/service/reference_scan_service.go` exists; 5 cloud adapters + k8s channel | VERIFIED |
| 阿里 `response.Cert` / 腾讯 `CertificatePublicKey` / Azure KeyVault secret / AWS ACM GetCertificate | `aliyun/cert.go:325`, `tencent/cert.go:325`, `azure/cert_discovery.go:439-452`, `aws/cert_discovery.go:380` | VERIFIED |
| GetCert 当前仅算指纹即丢弃（无 PEM 字段） | `CloudCertInfo` carries only Fingerprint/NotAfter/Exists in all four adapters | VERIFIED |
| AWS 仅解析 `output.Certificate`，CertificateChain 未读取 | `aws/cert_discovery.go:390-397` — only `output.Certificate` read | VERIFIED |
| AWS IAM-hosted（非 ARN）显式报错不支持 | `aws/cert_discovery.go:370-372` | VERIFIED |
| 华为 SCM ShowCertificate 无 PEM（SHA-1 口径） | `huawei/cert_discovery.go:456-484` — only `response.Fingerprint` (SHA-1) + NotAfter | VERIFIED |
| ImportCert keyPEM 空 → fingerprint_only | `web/cert_handler_test.go:162` TestImportCertAPIFingerprintOnly | VERIFIED |
| UploadKey 补传私钥升级 | `web/cert_handler.go:33` `POST /:id/key` RoleOpsEngineer | VERIFIED |
| CloudCertMapping FindByCloudCertID(cloud,accountKey,cloudCertId)、uk_fp_cloud_account | `certtest/change_fakes.go:754`, `:711` | VERIFIED |
| ErrDuplicateFingerprint 哨兵 + GetByFingerprint | `certtest/certtest.go:234`, `:252`; `web/response.go:107` → 409 | VERIFIED |
| 占位指纹公式 certscan-unresolved:{cloud}\|{accountKey}\|{certId} | `reference_scan_service.go:449` | VERIFIED |
| 批量会话 10 分钟整体限时 | `service/import_service.go:21` `batchProcessTimeout = 10 * time.Minute` | VERIFIED |
| 快照 status running/done/failed、partialFailures、LatestDone | `repository/validators.go:106`, `domain/scan_snapshot.go:76`, `repository/scan_snapshot.go:52` | VERIFIED |
| product=crd（K8s 引用，cloud 空） | `domain/cert_reference.go:32`, `coverage_meta.go:25` | VERIFIED |
| certtest fullchain 口径 leaf+中间CA+自签根 | `certtest/certtest.go:40,162` | VERIFIED |
| 前端分支 feat/platform-user-management + 批量导入进度组件 | Branch exists and is checked out; `BatchImportModal.vue` has poll+progress | VERIFIED (branch is local-only — not on origin; see Attack 8) |
| **"触发扫描→轮询快照状态（running→done/failed）" 可行** | `web/reference_handler.go:27-35`: only 3 routes (`GET /reverse`, `GET /:id/references`, `POST /:id/scan`); `service/reference_scan_service.go:172` "StartScan 全量引用扫描编排（**同步至终态**）"; **no snapshot-status GET endpoint exists anywhere** | **REFUTED — see Attack 1** |
| cert_references 有/可加 notAfter 透传消费方 | `domain/cert_reference.go` has **no NotAfter field**; no requirement/SC consumes one | **ORPHAN — see Attack 3** |

---

## Phase 1 — Reasoning Audit

Argument chain: Problem (ledger empty → delivered features unusable) → Solution (scan-snapshot-driven preview-confirm import) → Evidence (verified file:line claims) → Success Criteria (11 testable items). The chain holds; no self-reintroduction of the eliminated problem (manual PEM collection is replaced by cloud-fetch, and manual import remains as fallback, not as the primary path).

### SC Cluster Analysis

- **Cluster A — Preview** (SC-1/2/3 ↔ In Scope bullet 1, frontend bullet): SC-1 count formula (dedup by cloud+accountKey+cloudCertId, exclude crd + empty-cloud, include placeholder/huawei groups) ↔ In Scope "聚合唯一证书清单（排除 product=crd 引用，空 cloud 条目不计入）" — **satisfiable both directions**. SC-2 seven-field enumeration counts correctly (cloud/accountKey/cloudCertId/引用资源数/inLedger/notAfter/可解析标记) + response-level snapshotStartedAt — consistent. **SC-3 fails satisfiability**: the guidance flow requires polling snapshot status (running→done/failed + partialFailures on failed), but no in-scope backend item provides a pollable status endpoint, and none exists in code (see Attack 1).
- **Cluster B — Import session** (SC-4/5 ↔ In Scope bullets 2/3, NFR 可靠性): persist-before-async ↔ "浏览器中断不丢结果"; ErrDuplicateFingerprint→GetByFingerprint→补建映射记 success ↔ SC-5 幂等重跑终态 — **satisfiable**, mechanism verified against uk_fingerprint/uk_fp_cloud_account constraints.
- **Cluster C — Backfill** (SC-6 ↔ In Scope bullet 3, Out of Scope bullet 6, scenarios 占位指纹/多账号): backfill-by-(cloud,accountKey,cloudCertId) after per-entry success ↔ SC-6 分口径验收 — **satisfiable**, but introduces an unassessed data-correctness hazard (renewal drift, Attack 4) and an unassessed rollback question (Attack 5).
- **Cluster D — Unsupported groups** (SC-7 ↔ preview/frontend bullets, risk rows 2): huawei group + AWS IAM-hosted downgrade — behavior consistent; minor ambiguity on which SC-2 field carries the IAM-hosted flag (Attack 6).
- **Cluster E — Security** (SC-9 ↔ NFR 安全 ↔ In Scope GetCert extension): content-level assertions (no PRIVATE KEY, CERTIFICATE-only, leaf-first fullchain, EncryptedPrivateKey empty, fingerprint_only) — **satisfiable and strong**.
- **Cluster F — Permissions** (SC-8 ↔ scenario 权限 ↔ RequireRoles): verified pattern exists; **satisfiable**.

### Contradiction Findings

1. **CONTRADICTION (HIGH)**: SC-3 "触发扫描后轮询快照状态（running→done/failed）…不依赖单次长请求同步返回" ↔ In Scope backend set (no snapshot-status endpoint) + Dependency Readiness "全部就绪…无外部阻塞". Type: SC↔InScope satisfiability failure (implementation-level). Evidence: `reference_handler.go` route list (3 routes only); `reference_scan_service.go:172` scan is synchronous-to-terminal. Resolution required: either scope a snapshot-status GET (or async scan trigger), or redefine the guidance flow against the documented synchronous trigger.
2. **CONTRADICTION (MEDIUM)**: Out of Scope "扫描编排变更…逻辑不动…允许 cert_references 增加 notAfter 透传字段" ↔ no consumer anywhere (SC-2 explicitly shows "—（导入后补全）" for unregistered notAfter; backfill writes fingerprints only). Type: orphaned allowance / scope-creep seed inside an exclusion item. Resolution required: delete the clause or name its consumer.
3. **AMBIGUOUS (LOW)**: SC-7 "AWS IAM-hosted（非 ARN）条目同语义降级不可选" ↔ SC-2's seven fields contain no explicit unsupported/degraded flag (presumably folded into 可解析标记; also derivable client-side from the arn: prefix). Requires author clarification of which field carries it.

---

## Phase 2 — Rubric Scoring

### 1. Problem Definition — 103/110
- Problem stated (38/40): "证书台账当前为空…首次登记门槛高、易遗漏，导致台账长期空置" — precise, single interpretation.
- Evidence (38/40): three file:line claims, all verified exact (including field names `response.Cert`/`CertificatePublicKey`).
- Urgency (27/30): "每多等一天，到期监控盲区多存在一天" articulates cost of delay; "38 任务已交付" and "直接产生线上事故风险" are asserted without pointer, mild rhetoric.

### 2. Solution Clarity — 109/120
- Approach concrete (38/40): endpoints, session lifecycle, sanitization, idempotency paths fully specified.
- User-facing behavior (38/45): preview fields, default selection, grey-out, progress, refresh all described — but the no-snapshot guidance describes a UX that cannot be implemented as scoped (Attack 1).
- Technical direction (33/35): PEM channel exposure + session orchestration + backfill mechanics clear; reuse points named.

### 3. Industry Benchmarking — 106/120
- Solutions referenced (34/40): cert-manager, KeyChest, AppViewX — real named solutions.
- ≥3 meaningful alternatives (28/30): 4 rows incl. do-nothing; TLS probing is industry-validated; CAS full-list is genuinely different and deferred (not straw-man).
- Honest trade-offs (22/25): selected approach's con admitted ("未被引用的库内证书不入场"); TLS probing's pro ("覆盖面最全（含自建）") conceded.
- Justified vs benchmarks (22/25): "存量回溯登记" vs "部署时登记" differentiation articulated; rationale tied to verified existing assets.

### 4. Requirements Completeness — 94/110
- Scenario coverage (34/40): 8 scenarios incl. concurrent, deleted, unsupported-cloud, placeholder-fingerprint, multi-account. Deducted: the no-snapshot scenario's mechanism is unbacked (Attack 1).
- NFR (36/40): security (constructive sanitization, Zeroize, no-credential-logging), performance (<1s, rate limits, 10-min session cap), reliability (persist-before-async, panic isolation) — all quantified. Minor: no accessibility/compatibility note for the new Modal.
- Constraints & dependencies (24/30): per-cloud API dependencies accurate; but "全部就绪" is false for the guidance sub-flow, and the frontend base branch is local-only/unmerged (Attack 8).

### 5. Solution Creativity — 81/100
- Novelty over baseline (32/40): decoupling "扫描（只读发现）" from "登记（台账写入）" with preview-confirm; honestly framed ("这不是新技术，而是…价值复用").
- Cross-domain inspiration (26/35): borrows CLM "discovery-driven registration"; preview-confirm mirrors familiar import UX — no striking cross-domain leap.
- Simplicity of insight (23/25): reusing the scan snapshot as registration source with zero new cloud-API surface is elegant.

### 6. Feasibility — 80/100
- Technical feasibility (34/40): every code claim spot-checked accurate (incl. AWS chain gap and IAM-hosted behavior). Deducted: "无技术阻塞项" overlooks the unscoped snapshot-status surface (Attack 1).
- Resource & timeline (26/30): single dev + AI pipeline, 9-13 quick-mode tasks — realistic against verified existing infra.
- Dependency readiness (20/30): repos/adapters/session/encryption verified online; but "全部就绪…无外部阻塞" contradicted by the polling gap, and `feat/platform-user-management` is not pushed to origin (local-only branch).

### 7. Scope Definition — 72/80
- In-scope concrete (27/30): each bullet is a deliverable with routes/fields/behaviors. Minor: preview bullet lists "华为云/占位指纹标记" but omits the IAM-hosted marker (only frontend bullet and SC-7 carry it).
- Out-of-scope explicit (24/25): 6 named deferrals with rationale.
- Bounded (21/25): quick-mode bounded except the orphaned "允许 cert_references 增加 notAfter 透传字段" clause blurs the "扫描编排不动" boundary (Attack 3).

### 8. Risk Assessment — 79/90
- Risks identified (27/30): 6 meaningful risks incl. a code-level gap with file reference.
- Likelihood/impact honest (27/30): M/M, M/L, L/M mix — no blanket low/high.
- Mitigations actionable (25/30): each names a concrete action. Deducted: drift row covers only deletion, not same-ID renewal which poisons the backfill (Attack 4); no rollback story for the only pre-existing-data mutation (Attack 5).

### 9. Success Criteria — 68/80
- Measurable/testable (28/30): count formula, seven-field enumeration, 403 matrix, content-level PEM assertions, terminal states, test suites — objectively verifiable.
- Coverage complete (22/25): covers preview/import/session/backfill/permissions/security. Gaps: no SC for the 10-min session time-cap NFR or rate-limit behavior.
- SC internal consistency (18/25): clusters A–F mostly bidirectionally satisfiable, but SC-3 ↔ In Scope fails (Attack 1), and SC-7's degraded-flag field is absent from SC-2's enumeration (Attack 6). The document's own `consistency_check_result` (40 pairs, pass) did not catch the SC-3 satisfiability failure — its check scope was intra-text, not against code scope surface.

### 10. Logical Consistency — 73/90
- Solution addresses problem (32/35): directly closes the first-registration loop; huawei/unresolvable subset honestly excluded but slightly tempers the "覆盖率即时生效" claim (Attack 7).
- Scope↔Solution↔SC aligned (20/30): Attack 1 (polling) is the main misalignment; Attack 3 orphaned allowance second.
- Requirements↔Solution coherent (21/25): clean mapping; one reverse-orphan (allowance with no requirement).

**TOTAL: 865/1000** (target 900 — not met)

Cross-dimension coherence check: the single root cause (snapshot-status polling) is distributed across D2/D4/D6/D9/D10 for its distinct manifestations (UX description accuracy, scenario backing, dependency claim, SC satisfiability, cross-section alignment); no double-counting of the same facet.

---

## Phase 3 — Blindspot Hunt

4. **[blindspot]** Same-ID certificate renewal poisons the backfill. The drift risk row covers only deletion: "预览与导入间云侧状态漂移（预览后证书被删）| M | L". But ACM (and most CAS) **renewal preserves the certificate ID/ARN** — GetCert succeeds, fingerprint differs from scan-time. For real-fingerprint references this only leaves a coverage gap; for **placeholder references the backfill stamps the NEW cert's fingerprint onto references observed for the OLD cert**: "按 (cloud,accountKey,cloudCertId) 将 cert_references 中仍为占位指纹的引用批量回填为真实指纹" — a silent wrong-association. Needs: compare import-time fingerprint against scan-time real fingerprint when present, and a drift disposition for the placeholder path (skip + record reason, or rescan prompt).
5. **[blindspot]** No rollback story for the only destructive operation. The backfill overwrites persisted scan data (`cert_references` fingerprints). Nothing addresses reversibility or audit trail of the rewrite ("回填由导入会话按条目成功事件承担" — but no undo/compensation if an import is later found wrong, e.g. via Attack 4). Low likelihood, but this is the one mutation of pre-existing data in an otherwise additive feature; a CTO would ask "if the backfill is wrong, how do we recover?" Answer should be one sentence (e.g., placeholder values are deterministic and recomputable → restorable by formula, state it).
8. **[blindspot]** Frontend base branch is local-only. "前端依赖 e-cam-web `feat/platform-user-management` 分支现有台账页与批量导入会话进度组件" — `git branch -a` in e-cam-web shows the branch exists locally but is **absent from origin** (remotes: cmdb-iam-modules, phase1-eiam-login, main). Building on an unpushed branch is a merge-ordering risk worth one line in Constraints.

Rubric-adjacent observation (not scored as attack): the document's self-reported `consistency_check_result` checked 40 intra-text pairs but its method cannot catch SC↔codebase satisfiability failures — exactly what Attack 1 is. The note's claim of 4 prior fixed SC↔code contradictions suggests the check was supposed to include code reality; iteration 0's method missed the endpoint-surface gap.

---

## Attack Points

1. **[D10] (HIGH, conflict-with-pre-revision)**: SC-3's guidance flow is unimplementable as scoped — "触发扫描后轮询快照状态（running→done/failed），done 后进入预览、failed 展示 partialFailures（不依赖单次长请求同步返回）" — but the codebase has no snapshot-status endpoint (`reference_handler.go` registers only `/reverse`, `/:id/references`, `POST /:id/scan`) and StartScan is "同步至终态" (`reference_scan_service.go:172`); re-POSTing as a poll would start a fresh synchronous scan once idle, so partialFailures is only obtainable from the original long request the SC explicitly forbids depending on. In Scope adds no status endpoint. Must: scope a snapshot-status GET (or async trigger), or restage the guidance on the documented synchronous trigger with timeout handling. Tagged conflict-with-pre-revision: the polling language was introduced in pre-revision (medium) to fix the long-request issue — the fix created the gap.
2. **[D6]**: Dependency Readiness overstated — "全部就绪…无外部阻塞" — false for the guidance sub-flow per Attack 1, and the frontend base branch is not pushed to origin. Must: qualify readiness with the two concrete gaps.
3. **[D7]**: Orphaned schema allowance — "允许 cert_references 增加 notAfter 透传字段" — `domain/cert_reference.go` has no notAfter and no requirement/SC consumes one (SC-2 shows "—（导入后补全）" for unregistered entries; backfill writes fingerprints only). Must: delete the clause or name the consumer and add it to In Scope/SC.
4. **[blindspot]**: Renewal drift poisons backfill — risk table covers only "预览后证书被删"; same-ID renewal (ACM ARN-preserving renewal) silently stamps wrong fingerprints onto placeholder references. Must: add drift disposition for the placeholder backfill path.
5. **[blindspot]**: No rollback/recovery note for the cert_references fingerprint overwrite. Must: one-line recoverability statement (placeholders are deterministically recomputable).
6. **[D9] (LOW)**: Degraded-flag field unspecified — SC-7 requires "AWS IAM-hosted（非 ARN）条目同语义降级不可选" but SC-2's seven fields include no unsupported/degraded flag (folded into 可解析标记? unstated), and the backend preview In Scope bullet omits the IAM-hosted marker. Must: state which field carries it (or that the client derives it from the arn: prefix).
7. **[D2] (LOW)**: "覆盖率即时生效" slightly overstated — huawei (SHA-1) references and any never-resolvable placeholders remain permanently unlinked; the claim holds only for the four supported clouds. Must: qualify scope of "即时生效" (or keep, given huawei exclusion is honestly marked elsewhere).
8. **[blindspot] (LOW)**: Frontend dependency on a local-only branch — "前端依赖 e-cam-web `feat/platform-user-management` 分支" — branch absent from origin. Must: note push/merge ordering in Constraints.

## Bias Detection Report

- Annotated regions: 5 attack points (1, 3, 4, 6, 7) / 25 paragraphs = density 0.20
- Unannotated regions: 3 attack points (2, 5, 8) / 42 paragraphs = density 0.071
- Ratio (annotated/unannotated): 2.8

Interpretation: attack density concentrates on annotated regions because the pre-revised regions carry the mechanism-bearing claims (SC set, backfill, session semantics) where the real defects live — not because of revision-targeting bias. Notably, the single HIGH flaw (Attack 1) is an issue the pre-revision itself introduced (polling language added without backend surface), i.e., the revision direction and rubric judgment genuinely conflict there, tagged for review.
