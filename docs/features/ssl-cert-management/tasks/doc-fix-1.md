---
id: "doc-fix-1"
title: "Fix: 5.12 live cloud PoC P1-P9 requires real credentials"
priority: "P0"
estimated_time: "30min"
dependencies: []
status: pending
breaking: false
type: "doc.fix"
---

# Fix: 5.12 live cloud PoC P1-P9 requires real credentials

## Root Cause

5.12 desk verification complete; live full-chain verification (AC-1/AC-2) pending real aliyun/tencent credentials. Run checkpoints P1-P9 per poc-notes.md, then also apply the two registered corrections: B1 tencent async DeleteCertificate polling, B2 aliyun CAS unique cert naming

## Reference Files

- Source: docs/features/ssl-cert-management/design/poc-notes.md
- Error details: blocked: no cloud credentials in environment

## Content Fix Guidance

When fixing documentation failures, observe these boundaries:

**Scope:**
- Fix only the markdown/content issues identified in the root cause
- Do not modify source code files — this is a documentation-only fix
- Do not run code quality gates (lint, compile, test) — they are irrelevant for doc fixes

**Correct workflow:**
1. Read the failing document and understand the reported issue
2. Identify the specific content problem (broken links, missing sections, incorrect terminology, formatting errors)
3. Apply the minimal fix to resolve the issue
4. Verify the document renders correctly and internal references are valid

When this task is recorded as completed via `task record`, the source task 5.12 is automatically restored to pending if all its dependencies are completed.

## Acceptance Criteria
- [ ] 使用真实阿里云/腾讯云测试凭证执行 poc-notes.md P1-P9 活体检查点全部通过，并落实 B1/B2 修正验证（详见 description）。
