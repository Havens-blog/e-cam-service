---
id: "T-eval-contract"
title: "Evaluate Contract Quality"
priority: "P1"
estimated_time: "20-30min"
dependencies: ["T-test-gen-contracts"]
type: "eval.contract"
surface-key: ""
surface-type: ""
mainSession: true
---

Evaluate Contract quality for the ssl-cert-management feature using the 6-dimension rubric (1000-point scale).

## Feature Paths

Discover the feature's testing directory layout before starting:
```bash
ls docs/features/ssl-cert-management/testing/                                 # journeys
ls docs/features/ssl-cert-management/testing/<journey>/contracts/              # contracts
```

## Discovery Strategy
Scan `tests/<journey>/_contracts/` for all Contract files per Journey.

For each Journey's Contracts:
1. Run `/eval-contract` — this resolves target score and max iterations from `forge config`
2. Scoring dimensions: Completeness, Semantic Purity, Precondition Exclusivity, Fact Alignment, Surface Fitness, Internal Consistency

The eval skill's scorer-gate-revise loop handles iterative improvement within its iteration budget. Scores are recorded in the eval report for informational review.

## Acceptance Criteria

- [ ] All acceptance criteria met

### Hard Acceptance Criteria (non-negotiable)

- [ ] Eval report generated for all Contracts
