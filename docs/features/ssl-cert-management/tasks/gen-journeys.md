---
id: "T-test-gen-journeys"
title: "Generate Test Journeys"
priority: "P1"
estimated_time: "20-30min"
dependencies: ["T-review-doc", "T-clean-code", "6.5", "6.6", "7.2", "1.3", "2.1", "3.2", "4.5", "2.2", "3.4", "3.6", "4.2", "5.7", "5.10", "5.12", "3.3", "5.11", "5.2", "5.5", "6.7", "2.3", "3.5", "4.3", "6.4", "1.2", "5.4", "5.8", "6.2", "1.1", "3.1", "4.1", "4.4", "5.3", "5.6", "6.3", "7.1", "5.1", "5.9", "6.1"]
type: "test.gen-journeys"
surface-key: ""
surface-type: ""
---

Generate test Journey documents for the ssl-cert-management feature.
Mode: breakdown


## Discovery Strategy

Discover the feature's testing directory layout before starting:
```bash
ls docs/features/ssl-cert-management/testing/                                 # journeys
ls docs/features/ssl-cert-management/testing/<journey>/contracts/              # contracts
```

Invoke the `/gen-journeys` skill to extract Journey narratives from specification documents.

### Input Source by Mode

- **Breakdown mode**: Read PRD user stories from `docs/features/ssl-cert-management/prd/prd-user-stories.md` and functional specs from `docs/features/ssl-cert-management/prd/prd-spec.md`. These are the primary input sources.
- **Quick mode**: Read the proposal from `docs/proposals/ssl-cert-management/proposal.md`. Extract Key Scenarios as Journey candidates. If the proposal lacks `scope` or `success criteria` sections, abort the task with a diagnostic message — Journey generation requires these minimum inputs.

## Process

Follow the `/gen-journeys` skill process flow:

1. **Surface Detection**: Detect the project surface type and persist to `.forge/config.yaml`
2. **Read Sources**: Read PRD user stories (Breakdown) or proposal.md (Quick)
3. **Identify Workflows**: Map each user story or key scenario to a Journey candidate
4. **Classify Risk**: Assign High/Medium/Low risk to each Journey based on workflow characteristics
5. **Generate Files**: Output one `journey.md` per Journey to `docs/features/ssl-cert-management/testing/<journey-name>/journey.md`
6. **Validate Output**: Check each Journey for required fields (name, risk level, happy path steps, edge cases, invariants)

## AUTO_COMMIT Directive

When this task runs as an automated pipeline task (not invoked manually by the user), AUTO_COMMIT=true is in effect:

- **If AUTO_COMMIT=true**: Skip the user review-and-approval step. After validation passes, directly commit all generated Journey files:
  ```bash
  git add docs/features/ssl-cert-management/testing/
  git commit -m "docs: generate journeys for ssl-cert-management"
  ```
- **If AUTO_COMMIT is not set** (manual invocation): Present all Journey files to the user for review. Wait for explicit approval before committing.

## Acceptance Criteria

- [ ] All acceptance criteria met

### Hard Acceptance Criteria (non-negotiable)

- [ ] At least 1 Journey file generated under `docs/features/ssl-cert-management/testing/`
- [ ] Each Journey has: name, risk level, happy path steps, edge cases, invariants
- [ ] High-risk Journeys have edge case count >= happy path step count
- [ ] All Journey files committed (AUTO_COMMIT=true) or awaiting user review (manual mode)
