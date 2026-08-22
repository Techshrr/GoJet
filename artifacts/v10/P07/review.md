# P07 Analytics Review

Node: `P07`  
Issue: #21  
Base integration commit: `3aa80b566d144963130b8f61fa63a4ee677ebc99`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

## Review boundary

P07 proves the current-repository Analytics pipeline with real Redis event transport, native Go worker/reconciler processes, authoritative MySQL persistence/aggregation, deterministic reconciliation, server-side Workspace permission/tenant isolation and route-backed Workspace browser evidence. UI counters, mocked Redis events, fabricated totals or manually edited aggregate rows do not substitute for authoritative Exit evidence.

## Frozen analytics invariants

- accepted redirect analytics events must not be silently lost
- duplicate delivery/replay must not double-count authoritative aggregates
- `analyticsworker` consumes real Redis events and writes reviewable authoritative state
- `analyticsreconciler` recovery/reconciliation is deterministic, idempotent and safely repeatable
- known event totals reconcile to MySQL aggregate totals
- Workspace, Link and campaign analytics remain tenant-isolated
- analytics permission is server-authoritative; frontend hiding is not authorization
- time-zone/date-range filtering has exact boundary semantics and cannot mutate stored event identity
- complete-empty, partial, stale and retention-limited states remain distinct and are not collapsed into a misleading zero state
- P07 does not introduce invented/predictive metrics
- redirect destination-risk/custom-domain authority ordering from P05/P06 remains unchanged

## Accountable technical review

- Backend Lead: APPROVED
- Analytics/Data Lead: APPROVED
- QA Lead: APPROVED
- Frontend Lead: APPROVED
- Accessibility Reviewer: APPROVED
- Performance Reviewer: APPROVED
- Security Reviewer: APPROVED

## Gate scope

- G3 P07 functional/API subset: PASS — P07 subset
- G9 P07 performance subset: PASS — P07 subset
- G10 release-wide full-stack Gate: remains later-owned; P07 provides only its required evidence contribution

Passing P07 subsets does not close release-wide Gates owned by later nodes/P20/P22.

## Pre-sign exact-head closure evidence

- exact implementation SHA: `66ad75788c3fc470c55926c4f8f68eda6f8cb2e4`
- P07-T020: PASS
- P07 Closure run: `32570360271`
- closure artifact ID: `9475183271`
- closure artifact digest: `sha256:5357479bc95257106bcedaa56b579d105353e4cc161bbcd81112205240554209`
- closure T020 digest: `sha256:d8890d570e28599ddd856bbe7600bcb07deef836bfc6ce2ca42df3327c4a2b3d`
- exact-head evidence inputs: 19/19 PASS with `errors=[]`
- exact-head required regression workflows: 16/16 completed/success
- P07 Real Integration run: `32570360327`
- real integration artifact ID: `9475169619`
- real integration artifact digest: `sha256:87bda1c0110653944ce8171c086a0025f7d043c10d83395d3b4b65710af5092d`
- P07 Workspace Analytics Browser run: `32570360186`
- browser artifact ID: `9475173658`
- browser artifact digest: `sha256:e83e69cfbcdbca18d16fd5a725241dbdbfa0b7c95c2b0657c1ddf9a4355398f3`

The pre-sign closure artifact records P07-T001..T019 as PASS on the same implementation SHA and P07-T020 as PASS with `errors=[]`. T018 proves the route-backed Workspace success/empty/partial/stale/retention-limited/error states against real MySQL-backed API state. T019 proves the canonical 390×844 mobile layout, keyboard reachability, named controls, Link Detail analytics reuse and the P07 G9 budget subset. The Real Integration evidence covers real Redis transport, authoritative MySQL persistence/aggregation, duplicate/replay logical-once behavior, restart/retry recovery, reconciliation idempotency and known-event-total closure.

## Review findings

- P05 redirect destination-risk and P06 custom-domain authority ordering remain upstream of P07 analytics measurement and are covered by the same-head regression matrix.
- unsafe or unrepresentable request dimensions are normalized away rather than converting an otherwise-authorized redirect into an analytics-induced 503; authoritative event identity and worker validation remain strict.
- Workspace Analytics and Link Detail use the same authoritative analytics client/report model; no client-generated or predictive totals are accepted as evidence.
- the Link Detail mobile overflow found during T019 was traced with real Chrome to the full-width secondary action content-box and corrected with border-box sizing; the temporary probe workflow/script were removed before closure.
- Analytics responsive behavior consumes the existing token-driven Workspace `data-viewport` contract; P03 raw visual value validation is green.
- PR increment review found no unresolved TODO, FIXME or placeholder markers. Mock references are prohibitions against mock Exit evidence, not accepted substitutes.

## Evidence contract

- test plan: `artifacts/v10/P07/test-plan.json`
- result range: `artifacts/v10/P07/results/P07-T001.json` through `P07-T020.json`
- required evidence: Redis event records, MySQL source/aggregate records, worker/reconciler logs, known-event totals, recovery/reconciliation records, API request/response records, browser captures and exact-head regression manifest
- mocks or UI-only fixtures do not satisfy Redis/MySQL/reconciliation/permission Exit Conditions where deterministic real dependencies are available

## Defect / decision ledger

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0

## Signed-revision rule

This review signs the immutable pre-sign evidence above; it does not authorize merge by itself. The signed revision is merge-authoritative only after P07-T001..T020 pass again on the exact review-signing revision and the complete affected P00-P07 exact-head workflow matrix is completed successfully. Any new code, contract or review change after that rerun invalidates merge authority and requires another exact-head rerun.
