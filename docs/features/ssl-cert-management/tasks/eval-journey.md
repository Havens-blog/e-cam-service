---
id: "T-eval-journey"
title: "Evaluate Journey Quality"
priority: "P1"
estimated_time: "20-30min"
dependencies: ["T-test-gen-journeys"]
type: "eval.journey"
surface-key: ""
surface-type: ""
mainSession: true
---

Evaluate Journey quality for the ssl-cert-management feature using the 6-dimension rubric (1000-point scale).

## Feature Paths

Discover the feature's testing directory layout before starting:
```bash
ls docs/features/ssl-cert-management/testing/                                 # journeys
ls docs/features/ssl-cert-management/testing/<journey>/contracts/              # contracts
```

## Discovery Strategy
Scan `docs/features/ssl-cert-management/testing/journeys/` for all Journey files listed in `manifest.md`.

For each Journey:
1. Run `/eval-journey` — this resolves target score and max iterations from `forge config`
2. Scoring dimensions: Completeness, Semantic Purity, Precondition Exclusivity, Fact Alignment, Surface Fitness, Internal Consistency

The eval skill's scorer-gate-revise loop handles iterative improvement within its iteration budget. Scores are recorded in the eval report for informational review.

## Acceptance Criteria

- [ ] All acceptance criteria met

### Hard Acceptance Criteria (non-negotiable)

- [ ] Eval report generated for all Journeys
