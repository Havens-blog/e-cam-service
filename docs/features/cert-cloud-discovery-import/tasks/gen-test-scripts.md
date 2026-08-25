---
id: "T-test-gen-scripts"
title: "Generate API Functional Test Scripts"
priority: "P1"
estimated_time: "1-2h"
dependencies: ["T-test-gen-contracts"]
type: "test.gen-scripts"
surface-key: "."
surface-type: "api"
---

Generate executable test scripts for the cert-cloud-discovery-import feature.
Test type: api.

## Feature Paths

Discover the feature's testing directory layout before starting:
```bash
ls docs/features/cert-cloud-discovery-import/testing/                                 # journeys
ls docs/features/cert-cloud-discovery-import/testing/<journey>/contracts/              # contracts
```

Read the approved test cases and generate scripts using the framework from the surface.

## Acceptance Criteria

- [ ] All acceptance criteria met

Type: **api**
