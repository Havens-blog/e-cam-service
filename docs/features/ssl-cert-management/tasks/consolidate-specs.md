---
id: "T-specs-consolidate"
title: "Consolidate Specs"
priority: "P2"
estimated_time: "20min"
dependencies: ["T-test-run"]
type: "doc.consolidate"
surface-key: ""
surface-type: ""
---

Extract and consolidate business rules and tech specs from the ssl-cert-management feature.

## Feature Context


## Discovery Strategy
1. Scan docs/features/ssl-cert-management/ for all feature documents (PRD, design, task records)
2. Scan docs/proposals/ssl-cert-management/ for proposal
3. Extract rules and specs from discovered documents
4. Compare against existing specs in docs/business-rules/ and docs/conventions/

Run in non-interactive mode: auto-integrate all CROSS items. Commit with [auto-specs] tag.

## Acceptance Criteria

- [ ] All acceptance criteria met

### Hard Acceptance Criteria (non-negotiable)

- [ ] Business rules extracted to docs/business-rules/ with correct domains frontmatter
- [ ] Tech specs extracted to docs/conventions/ with correct domains frontmatter
- [ ] All CROSS items auto-integrated and committed with [auto-specs] tag
