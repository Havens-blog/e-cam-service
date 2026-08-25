# Iteration 2 — Adversarial Scoring Report

- Document: `D:\Haven\e-cam-service\docs\proposals\cert-cloud-discovery-import\proposal.md`
- Rubric: proposal.md (1000 pts, target 900)
- Iteration: 2 (scored as-is on the page; no credit for improvement over iteration 1)
- Codebase reality-check performed: backend `D:\Haven\e-cam-service` (internal/cert), frontend `D:\Haven\e-cam-web`

## Iteration-1 Attack Re-verification (resolution audit, not credit)

| # | Iteration-1 attack | Revision response | Verdict (on its own merits) |
|---|---|---|---|
| 1 | SC-3 polling flow unimplementable (no snapshot-status endpoint) | New In Scope bullet: `GET /api/v1/certs/discovery/snapshot-status` returning status/startedAt/partialFailures; Dependency gap (1) names it as a new deliverable; SC-3 references "本期新增的快照状态查询端点" | RESOLVED — code-verified this iteration: a `running` snapshot is persisted at scan start (`reference_scan_service.go:222-223` `Create` with `ScanStatusRunning`), `POST /:id/scan` exists with 409 SCAN_IN_PROGRESS carrying snapshot ID (`reference_handler.go:164-178`), and repo has `LatestDone`/`LatestRunning` (`repository/scan_snapshot.go:54,65`). The guidance flow is implementable even if the synchronous scan POST is cut by a gateway timeout |
| 2 | Dependency Readiness overstated ("全部就绪…无外部阻塞") | Rewritten per-item with exactly two named gaps, both remediation-ordered | RESOLVED — both gaps code-verified accurate (no status route registered; `feat/platform-user-management` absent from origin, confirmed via `git branch -a`) |
| 3 | Orphaned schema allowance ("允许 cert_references 增加 notAfter 透传字段") | Replaced by "cert_references 表结构不变" | RESOLVED — clause deleted; no orphan remains |
| 4 | Renewal drift poisons backfill | Backfill semantics defined: "回填一律以导入时点 GetCert 为准（ACM 续期保留 ID/ARN 时回填的即现行证书指纹，非误写）；非占位（真实）指纹引用永不被回填覆盖" | RESOLVED — semantically defensible (placeholder refs only ever pointed at the cloudCertId, never at a fingerprint), and real-fingerprint refs are never overwritten |
| 5 | No rollback story for backfill | "可恢复性：占位指纹是确定性可重算值…误回填可由重扫按原口径重建" | RESOLVED — code-verified: placeholder = `sha256Hex("certscan-unresolved:" + cacheKey)` with `cacheKey = cloud\|accountKey\|certId` (`reference_scan_service.go:405,449`); rescan deterministically recomputes it |
| 6 | Degraded-flag field unspecified | "降级由可解析标记字段承载：parseable=false 归入不可选组" in SC-7 and In Scope preview bullet | RESOLVED |
| 7 | "覆盖率即时生效" overstated | Qualified: "关联即时生效的范围为四云引用，华为云（SHA-1 口径）与不可解析占位引用保持未关联" + comparison table row | RESOLVED |
| 8 | Frontend base branch local-only | Constraints + Dependency gap (2) with push/merge-before-branch ordering | RESOLVED — claim re-verified accurate |

No new contradictions introduced by fixes 1–8. One NEW low-severity process inconsistency was found in previously revised text (see Contradiction Findings #3).

## Code Verification Appendix (new checks this iteration)

| Claim in proposal | Code evidence | Verdict |
|---|---|---|
| Snapshot persists in `running` state so polling can observe it | `reference_scan_service.go:212-223` — running snapshot Created at scan start, before discovery | VERIFIED |
| Scan trigger route exists; guidance "触发扫描→轮询" backed | `reference_handler.go:34,164-178` — POST /:id/scan, 409 SCAN_IN_PROGRESS with SnapshotID/StartedAt | VERIFIED |
| "现有路由面仅 GET /reverse、GET /:id/references、POST /:id/scan，无任何状态查询端点" | Route registration at `reference_handler.go:31-35` — exactly 3 routes | VERIFIED |
| "最近快照" query feasibility | `repository/scan_snapshot.go:54,65` LatestDone/LatestRunning exist; latest-any-status (incl. failed) needs a new query — inside the new-deliverable scope | FEASIBLE (no contradiction) |
| Placeholder formula + determinism/recoverability | `reference_scan_service.go:405,432,449` — cacheKey `{cloud}\|{accountKey}\|{certId}`; sha256 of "certscan-unresolved:"+cacheKey | VERIFIED |
| IAM-hosted/huawei refs appear in snapshot (so preview can mark them) | `reference_scan_service.go:444-449` — GetCert error/非SHA256 falls through to placeholder, reference still persisted | VERIFIED (SC-7 preview treatment is not vacuous) |
| K8s refs excluded from preview (empty cloud) | `reference_scan_service.go:426` — K8s refs resolved with cloud="" | VERIFIED |
| feat/platform-user-management local-only | `git branch -a` in e-cam-web: local + checked out; origin has only cmdb-iam-modules / phase1-eiam-login / main | VERIFIED |
| Static sibling route `/certs/discovery/*` vs `/:id` feasible | Existing static siblings (`/stats`, `/reverse`, `/batch/:id`, `/changes`) already coexist with `/:id` in the same group | VERIFIED (no gin conflict) |
| "vitest 回归 289 用例全绿" | Static `it(`/`test(` count = 271 across 12 spec files; delta plausibly it.each expansions | PLAUSIBLE (not disprovable; not attacked) |

## Phase 1 — Reasoning Audit

Argument chain Problem (ledger empty → delivered domain unusable) → Solution (snapshot-driven preview-confirm import with backfill compensation) → Evidence (file:line claims, all verified across two iterations) → SC (10 testable items). Chain holds end-to-end; no self-reintroduction of the eliminated problem.

### SC Cluster Analysis

- **Cluster A — Preview + no-snapshot** (SC-1/2/3 ↔ In Scope bullets 1–2, frontend bullet): count formula ↔ aggregation spec bidirectionally consistent; seven-field enumeration counts correctly; SC-3 polling ↔ snapshot-status endpoint now **satisfiable both directions** (code-backed per Appendix). Residual ambiguity: SC-1's illustrative parenthetical lists "占位指纹条目与华为云不可选组" but omits the third non-selectable group (AWS IAM-hosted); the formula itself is total, so this is ambiguity, not contradiction.
- **Cluster B — Import session** (SC-4/5 ↔ In Scope bullets 3–4, NFR 可靠性): persist-then-async, per-entry pipeline, ErrDuplicateFingerprint→补建映射记 success, terminal states — satisfiable; verified against uk_fingerprint / uk_fp_cloud_account / 409 sentinel.
- **Cluster C — Backfill** (SC-6 ↔ In Scope bullet 4, Out of Scope bullet 6, scenarios): import-time-current-cert semantics + real-fp-never-overwritten + no-backfill-on-parse-failure + deterministic recoverability — internally consistent and consistent with scan-side placeholder mechanics.
- **Cluster D — Unsupported groups** (SC-7 ↔ preview markers, risk rows 1–2): parseable=false now carries both huawei and IAM-hosted degradation; verified non-vacuous (refs are persisted).
- **Cluster E — Security** (SC-9 ↔ NFR 安全 ↔ GetCert extension bullet): content-level assertions satisfiable.
- **Cluster F — Permissions** (SC-8 ↔ 权限 scenario): covers 预览/导入/进度 — **omits the new snapshot-status endpoint** (coverage gap, not contradiction).
- **Cluster G — Session entity** (In Scope bullet 3): fields enumerated; storage choice (new collection vs generalize) left open — acceptable proposal-level deferral.

### Contradiction Findings

1. **AMBIGUOUS (LOW)**: SC-1 parenthetical "含占位指纹条目与华为云不可选组" omits the AWS IAM-hosted non-selectable group from the count illustration; the formula ("去重后的证书数", only crd/empty-cloud exclusions) includes them. An auditor verifying SC-1 against SC-7 needs the parenthetical to be exhaustive or explicitly illustrative.
2. **AMBIGUOUS (LOW)**: SC-8 "预览/导入/进度端点…返回 403" does not enumerate the new snapshot-status endpoint, and In Scope bullet 2 states no role guard; the 权限 scenario names only "预览/导入端点". Whether snapshot-status requires RoleOpsEngineer is unstated.
3. **CONTRADICTION (LOW, process-level, conflict-with-pre-revision)**: Risk row 1 mitigation "tech-design 首任务落实'叶在前拼接 Certificate+CertificateChain'的净化拼装" (echoed by SC-4 "按风险表首任务交付") presumes a design phase/task artifact, while Next Steps mandates "quick 模式：直接进入 `/quick-tasks` 生成任务并执行（不走 PRD/design）". A reader cannot tell whether a tech-design artifact will exist. Does not break SC satisfiability, but is a real on-page inconsistency introduced in the revised risk row.

## Phase 2 — Rubric Scoring

### 1. Problem Definition — 103/110
- Problem stated (38/40): "证书台账当前为空…首次登记门槛高、易遗漏，导致台账长期空置" — single interpretation, precise.
- Evidence (38/40): "台账页空态文案…`e-cam-web/src/views/cert/ledger/index.vue:28`" + two backend claims — all verified exact across two iterations; import-channel claim verified against routes (POST /certs, /certs/batch only).
- Urgency (27/30): "每多等一天，到期监控盲区多存在一天" — cost of delay articulated; "38 任务已交付" carries no pointer (asserted, plausible from project state).

### 2. Solution Clarity — 117/120
- Approach concrete (39/40): endpoints with paths, session lifecycle, sanitization spec, idempotency semantics, entity fields.
- User-facing behavior (44/45): full journey — "空态 CTA…预览列表…默认全选未登记项，已在台账项灰选…进度轮询…完成刷新台账"; no-snapshot guidance now implementable. −1: greyed huawei/IAM-hosted entries would display notAfter as "—（导入后补全）" although those groups can never be imported — misleading placeholder for non-importable rows.
- Technical direction (34/35): per-cloud PEM channels, CertBatchSession reference, backfill mechanics with recoverability, dual-channel inLedger.

### 3. Industry Benchmarking — 107/120
- Solutions referenced (34/40): cert-manager, KeyChest, AppViewX — real, accurately characterized ("发现即登记入库存"), but each in one line.
- ≥3 alternatives (28/30): 4 rows incl. do-nothing; TLS probing is industry-validated; CAS full-list genuinely different and deferred, not straw-man.
- Honest trade-offs (23/25): selected con conceded twice (table + scope: "未被引用的库内证书不入场"); now also discloses association limits in the Selected row ("华为云与不可解析占位引用保持未关联"); TLS probing pro conceded.
- Justified vs benchmarks (22/25): "存量回溯登记" vs "部署时登记" differentiation is clear and tied to verified assets; honest "这不是新技术" framing caps the claim.

### 4. Requirements Completeness — 102/110
- Scenario coverage (38/40): 8 scenarios — happy path, no-snapshot (now mechanism-backed), concurrent duplicate, deleted cert, unsupported cloud, placeholder backfill (with full drift semantics), multi-account, permissions. −2: snapshot-status response shape when no snapshot exists at all (fresh install) unspecified; theoretical edge given trigger-then-poll ordering.
- NFR (36/40): security is constructive and quantified ("仅 CERTIFICATE 块…Zeroize…禁入日志"), performance quantified ("< 1s", rate limits, "整体限时"), reliability explicit. −4: no accessibility/compatibility note for the new Modal (carried); "整体限时防泄漏" terse until the risk table quantifies 10 minutes.
- Constraints & dependencies (28/30): now per-item and honest — "两个具体缺口——(1) 无快照引导所需的快照状态查询端点为本期新增交付物…(2) 前端基线分支…需先 push/merge"; both verified accurate; ordering stated ("先 push 或 merge…再从此基线拉出本功能分支"); doc-fix-1 boundary drawn.

### 5. Solution Creativity — 81/100
- Novelty (32/40): decoupling "扫描（只读发现）" from "登记（台账写入）" plus the backfill compensation path; honestly framed as value reuse.
- Cross-domain (26/35): borrows CLM discovery-driven registration; preview-confirm mirrors familiar import UX; no striking cross-domain leap.
- Simplicity (23/25): "无新云 API 面（除 AWS ACM 既有 GetCertificate）" — elegant minimal-increment insight.

### 6. Feasibility — 93/100
- Technical (38/40): every load-bearing code claim verified (this iteration: snapshot persistence, route surface, placeholder determinism, IAM-hosted fallthrough); known gaps named with file references; "无技术阻塞项" now defensible. −2: three of four clouds' real fullchain response shapes are unverified until 联调期 ("四云 GetCert 真实响应形态…建议在联调期附一次…手动验证清单"), disclosed but unproven; IAM-hosted certs have no path into the ledger at all.
- Resource & timeline (27/30): "单人 + AI 流水线，quick 模式预估 9-13 个任务" with risk localization ("风险集中在 AWS CertificateChain 拼接口径与前端 Modal 交互细节") — realistic; no elapsed-time estimate given.
- Dependency readiness (28/30): per-item readiness accurate; the two gaps disclosed with remediation and cross-referenced ("与 Dependency Readiness 缺口 (2) 同项"). −2: both gaps remain open prerequisites at time of writing (branch push must precede dev start) — disclosed, not resolved.

### 7. Scope Definition — 77/80
- In-scope concrete (29/30): every bullet is a deliverable with routes/fields/behaviors ("GET /api/v1/certs/discovery/preview", entity field list, terminal states). −1: "会话进度 GET" carries no concrete path.
- Out-of-scope explicit (24/25): 6 named deferrals with rationale plus a sharpened boundary ("cert_references 表结构不变").
- Bounded (24/25): quick-mode bounded; the orphaned schema allowance that previously blurred the boundary is gone; −1: session entity storage choice ("新集合或…泛型化") left as an either/or.

### 8. Risk Assessment — 83/90
- Risks identified (29/30): 6 meaningful rows, one with a code-level reference ("aws/cert_discovery.go 现仅解析 output.Certificate"). −1: renewal-drift disposition lives only in Key Scenarios; the risk table's drift row still covers deletion only ("预览后证书被删").
- Likelihood/impact honest (27/30): M/M, M/L, L/M mix; no blanket low-high.
- Mitigations actionable (27/30): rows name concrete actions ("fake 适配器补 CertificateChain 用例，不阻塞其余三云交付"). −3: (a) "tech-design 首任务" references an artifact the chosen pipeline excludes (Contradiction #3) — not actionable as written; (b) renewal-drift row absent from the table a CTO reads first.

### 9. Success Criteria — 75/80
- Measurable/testable (29/30): count formula, seven-field enumeration, structured error code, content-level PEM assertions ("不含 \"PRIVATE KEY\" 字样、含且仅含 CERTIFICATE 块"), 403 matrix, terminal states, test suites.
- Coverage (23/25): covers preview/no-snapshot/import/session/backfill/multi-account/permissions/security. −2: snapshot-status endpoint missing from SC-8's permission enumeration; no SC verifies the 10-minute session cap or rate-limit behavior declared in NFR/risk table.
- Internal consistency (23/25): clusters A–G bidirectionally satisfiable including code-level satisfiability of the polling fix (re-verified). −2: SC-1 parenthetical group-list ambiguity; SC-8 endpoint enumeration gap.

### 10. Logical Consistency — 84/90
- Solution addresses problem (34/35): closes the first-registration loop; "关联即时生效的范围为四云引用" honestly scoped.
- Scope↔Solution↔SC aligned (26/30): iteration-1 misalignments resolved and re-verified. −4: Contradiction #3 (tech-design vs quick mode) spans risk table ↔ SC-4 ↔ Next Steps; snapshot-status permission gap is a residual alignment hole between the new In Scope bullet and the permission scenario/SC-8.
- Requirements↔Solution coherent (24/25): clean two-way mapping; the orphaned allowance is gone; −1: the "托管形态" bullet's UploadKey upgrade path is referenced but no requirement/SC exercises it (acceptable — it is existing functionality).

**TOTAL: 922/1000** (target 900 — MET)

Cross-dimension coherence check: the snapshot-status permission gap manifests as D9-coverage (SC enumeration) and D10-alignment (cross-section) — distinct facets, no double counting. Contradiction #3 is charged once in D8 (actionability) and once in D10 (cross-section alignment) for its two distinct manifestations. All other deductions map 1:1 to distinct weaknesses.

## Phase 3 — Blindspot Hunt

7. **[blindspot]** Snapshot-status empty-state is unspecified. "快照状态查询端点…返回最近快照的 status（running/done/failed）/startedAt/partialFailures" — what happens on a fresh install with zero snapshots (the exact population the no-snapshot guidance targets, if polled before the trigger lands)? 404 vs structured null vs 200-with-empty is unstated; SC-3 only specifies the preview side (NO_SNAPSHOT). One sentence fixes it.
8. **[blindspot] (LOW)** Session-level concurrency unguarded. The concurrency scenario is entry-level only ("导入中并发到达同指纹 → 捕获现有 uk_fingerprint 哨兵…条目记 success"); two overlapping discovery-import sessions (double-click, two operators) are safe by per-entry idempotency but double-spend rate-limited cloud API quota and present two competing progress UIs. The reused batch pattern has no active-session guard either (`import_service.go:115-128` — Create then async process), so nothing inherited covers it. A one-line disposition (allow-and-converge, or 409-in-progress like `SCAN_IN_PROGRESS`) suffices.
9. **[blindspot] (LOW)** Import-time rescan race. Backfill writes cert_references fingerprints; a concurrent rescan rewrites the same rows ("回填由导入会话按条目成功事件承担" vs scan's "写引用（certFingerprint 解析 + snapshotId/scannedAt 写通）"). Last-writer-wins is benign (both write real fingerprints or the deterministic placeholder) but unstated; one sentence declaring benignity or a scan-vs-import mutual exclusion would close it.

Rubric-adjacent observation (not scored): the self-reported `consistency_check_result` now claims "conflicts_found: 2…已当场修正后复检通过" and names iteration-0 fixes — the iteration-1 methodological gap (intra-text check cannot catch SC↔codebase satisfiability) is mooted this iteration because the load-bearing satisfiability question (polling surface) was independently verified as code-backed.

## Attack Points

1. **[D10]** Process contradiction between risk mitigation and pipeline choice — "tech-design 首任务落实'叶在前拼接 Certificate+CertificateChain'的净化拼装" (risk row 1, echoed in SC-4 "按风险表首任务交付") vs Next Steps "quick 模式：直接进入 `/quick-tasks` 生成任务并执行（不走 PRD/design）" — the mitigation names an artifact class the plan explicitly excludes; a reader cannot tell where the chain-splicing decision lands. Must: reword to "首任务（quick-tasks 首个生成任务）落实…" or schedule an explicit design-first task. (Tagged conflict-with-pre-revision: the wording sits in a pre-revised risk row.)
2. **[D9]** Permission matrix omits the new endpoint — "预览/导入/进度端点对非 OpsEngineer 角色返回 403" — the snapshot-status endpoint (`GET /api/v1/certs/discovery/snapshot-status`, itself a new deliverable) appears in neither SC-8 nor the 权限 scenario ("非 OpsEngineer 访问预览/导入端点 → 403") nor its own In Scope bullet's role guard. Must: add the endpoint to the 403 matrix and state RequireRoles(RoleOpsEngineer) in its In Scope bullet.
3. **[D9]** SC-1 group list incomplete — "（…含占位指纹条目与华为云不可选组）" — omits the AWS IAM-hosted non-selectable group that SC-7 and the In Scope preview bullet ("华为云/占位指纹/AWS IAM-hosted 标记") both treat as a third preview group. Must: either list all three groups or state the parenthetical is illustrative and the formula exhaustive.
4. **[D9]** No SC for declared NFRs — "会话整体限时对齐批量导入 10 分钟口径" and "逐条限速调用云 API（复用各适配器 waitRateLimit）" — no success criterion verifies either behavior. Must: add a session time-cap SC or explicitly declare it test-covered only via unit tests.
5. **[D2]** Misleading placeholder on non-importable rows — "未登记条目显示'—（导入后补全）'" — applies uniformly, but huawei ("预览整组标记'该云暂不支持自动解析'，不可选") and AWS IAM-hosted entries can never be imported, so "导入后补全" promises an event that cannot occur. Must: specify distinct notAfter display for non-importable groups.
6. **[D4]** NFR dimension gap (carried) — security/performance/reliability are covered but no accessibility/compatibility consideration for the new preview Modal and its grouped/greyed interactions. Must: one line (keyboard/a11y conformance of greyed-out group semantics) or explicit waiver.
7. **[blindspot]** Snapshot-status empty-state unspecified — "返回最近快照的 status（running/done/failed）/startedAt/partialFailures" — zero-snapshot case (the guidance flow's starting population) has no defined response. Must: define the empty case.
8. **[blindspot] (LOW)** Session-level concurrency un dispositioned — entry-level idempotency ("捕获现有 uk_fingerprint 哨兵…条目记 success") covers data safety, but overlapping sessions double-spend cloud API quota and fork progress UIs. Must: one-line disposition.
9. **[blindspot] (LOW)** Import-vs-rescan write race on cert_references undeclared — both paths write fingerprints to the same rows. Likely benign (deterministic placeholders / real fingerprints), but unstated. Must: one-line benignity statement or mutual exclusion.

## Bias Detection Report

- Annotated regions: 4 attack points (1, 3, 5, 8) / 28 paragraphs = density 0.143
- Unannotated regions: 4 attack points (2, 4, 6, 7) / 40 paragraphs = density 0.100
- Ratio (annotated/unannotated): 1.43

Interpretation: density is nearly balanced (vs 2.8 in iteration 1) — the revision genuinely repaired the annotated regions' defects, and the remaining issues distribute across both regions. Attack 1 is tagged `conflict-with-pre-revision` (the revised risk row's "tech-design" wording conflicts with the pipeline section); all other attacks align with, rather than contradict, the pre-revision direction.
