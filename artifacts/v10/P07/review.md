# P07 Analytics Review

Node: `P07`  
Issue: #21  
Base integration commit: `3aa80b566d144963130b8f61fa63a4ee677ebc99`  
Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**

## Review boundary

P07 must prove the current-repository Analytics pipeline with real Redis event transport, native Go worker/reconciler processes, authoritative MySQL persistence/aggregation, deterministic reconciliation, server-side Workspace permission/tenant isolation and route-backed Workspace browser evidence. UI counters, mocked Redis events, fabricated totals or manually edited aggregate rows cannot substitute for authoritative Exit evidence.

## Frozen analytics invariants

- accepted redirect analytics events must not be silently lost
- duplicate delivery/replay must not double-count authoritative aggregates
- `analyticsworker` consumes real Redis events and writes reviewable authoritative state
- `analyticsreconciler` recovery/reconciliation is deterministic, idempotent and safely repeatable
- known event totals must reconcile to MySQL aggregate totals
- Workspace, Link and campaign analytics remain tenant-isolated
- analytics permission is server-authoritative; frontend hiding is not authorization
- time-zone/date-range filtering must have exact boundary semantics and cannot mutate stored event identity
- complete-empty, partial, stale and retention-limited states are distinct and must not be collapsed into a misleading zero state
- P07 does not introduce invented/predictive metrics
- redirect destination-risk/custom-domain authority ordering from P05/P06 must remain unchanged

## Accountable technical review

- Backend Lead: PENDING
- Analytics/Data Lead: PENDING
- QA Lead: PENDING
- Frontend Lead: PENDING
- Accessibility Reviewer: PENDING
- Performance Reviewer: PENDING
- Security Reviewer: PENDING

## Gate scope

- G3 P07 functional/API subset: PENDING
- G9 P07 performance subset: PENDING
- G10 release-wide full-stack Gate: remains later-owned; P07 provides only its required evidence contribution

Passing P07 subsets will not close release-wide Gates owned by later nodes/P20/P22.

## Evidence contract

- test plan: `artifacts/v10/P07/test-plan.json`
- result range: `artifacts/v10/P07/results/P07-T001.json` through `P07-T020.json`
- required evidence: Redis event records, MySQL source/aggregate records, worker/reconciler logs, known-event totals, recovery/reconciliation records, API request/response records, browser captures and exact-head regression manifest
- mocks or UI-only fixtures do not satisfy Redis/MySQL/reconciliation/permission Exit Conditions where deterministic real dependencies are available

## Defect / decision ledger

- P0: PENDING REVIEW
- P1: PENDING REVIEW
- `DECISION REQUIRED`: 0 at node entry

## Rule

The review may only be signed after P07-T001..T020 pass on one exact implementation SHA, required P00-P07 affected workflows are green on that same SHA, evidence is internally consistent and reviewable, P0/P1 defects are zero and `DECISION REQUIRED` is zero. Any signed review revision must itself rerun and pass the complete affected exact-head matrix before merge.
