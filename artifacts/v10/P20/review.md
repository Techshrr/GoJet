# P20 Whole Product Verification — Accountable Technical Review

Node: `P20`  
Issue: #53  
Base integration commit: `6e628b9879eb4dddf335a324e4f4d7ae3a77cd5c`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**

## 1. Review boundary

This file establishes the P20 accountable-review contract before whole-product verification begins. It is not a PASS record, signature, release-candidate decision or merge authority.

P20 owns unified verification across the already integrated P00-P19 product, not new feature implementation. It must freeze and verify the candidate implementation commit, schema catalog, frontend build inventory, capability/route traceability, release-candidate evidence index, defect ledger, real correlated P0 workflows, cross-surface consistency and failure/recovery behavior.

P20 must not reinterpret signed predecessor authority, invent routes or features, replace required real workflows with mocks, weaken fail-closed/security behavior, or claim P21/P22 native package/fresh-install/production obligations complete.

## 2. Immediate signed predecessor authority

- P19 signed source: `44ea701ae464550ce920c5f2131428270e22fb41`
- P19 integration commit: `6e628b9879eb4dddf335a324e4f4d7ae3a77cd5c`
- P19 Closure run: `33268403700`
- P19 closure artifact: `9719405957`
- P19 closure digest: `sha256:de62ca1484b7eeedc7249303fa525885584f49d5983ca9378000eb7bb82e7bd2`
- P19 closure: `phase=signed`, `review_phase=signed`, `review_only_signed_child=true`, `merge_authoritative=true`, applicable matrix `19/19`, G4/G5/G7/G8/G9 `5/5`, P0/P1/DECISION REQUIRED `0/0/0`.

P20 starts only from this verified integration commit and inherits P00-P19 authority without reinterpretation.

## 3. Candidate freeze boundary

Before P20 may claim verification readiness, one exact candidate revision must bind:

- current repository implementation commit;
- current schema/migration catalog;
- `admin`, `docs`, `site` and `workspace` frontend build inventory;
- frozen Capability Matrix and Route Registry authority plus integrated status/disposition ledgers;
- exact-head P20 evidence index;
- defect and decision ledger.

Mixed-head or stale evidence is prohibited.

## 4. Real P0 workflow authority

The required Master Plan sequence is:

`register → verify → login → link → redirect → analytics → QR → file → text → bio → domain → ticket → billing → notification → admin`

P20 must preserve one machine-readable correlated timeline across browser, HTTP/API, MySQL, Redis, workers, mail and audit where applicable. Stable identifiers must connect the steps without exposing credentials, verification tokens, payment signatures, webhook secrets, TXT secrets or other prohibited material.

A mock, stub, screenshot-only or disconnected per-node replay cannot substitute for required real P0 authority.

## 5. Cross-surface consistency boundary

P20 must verify server-authoritative tenant/RBAC, destination/domain risk, ClamAV, entitlement, Auth/Session/CSRF/Origin, Turnstile, payment, notification, audit, HTTP status, navigation/deep-link, SEO/indexation, locale, Design System, accessibility, responsive and performance behavior across their applicable Website, Docs, Auth, Workspace, Admin, API, redirect and Public surfaces.

A frontend success state cannot override a server deny/review/not-ready/failed state.

## 6. Failure and recovery boundary

P20 must verify deterministic fail-closed and recovery behavior for Redis, MySQL transaction/idempotency boundaries, analytics worker/reconciler, fileworker/ClamAV, mailworker, risk providers, payment callback replay/order, and representative partial dependency degradation.

Recovery evidence must demonstrate convergence and idempotency without manual data patching, secret leakage or contract weakening.

## 7. G0-G10 release-candidate ledger

The Master Plan requires P20 Exit Conditions to close the G0-G10 release-candidate verification ledger while preserving each Gate's execution-stage ownership.

P20 must provide release-wide PASS evidence for `G0`, `G3`, `G4`, `G5`, `G6`, `G7`, `G8`, `G9` and `G10`.

`G2` remains the signed P03 Design System authority and must be live-bound/revalidated by applicable current UI evidence.

`G1` is an explicit carry-forward row: its execution stages are P01/P21/P22 and `CAP-NATIVE-INSTALL` / `CAP-NATIVE-ONLY-RELEASE` remain P21/P22-owned. P20 must live-bind the current native-architecture baseline and preserve those obligations, but must not falsely claim the later-owned native package/fresh-install work complete.

`G11`, `G12` and `G13` remain outside P20.

Conditional PASS or a missing Gate disposition is prohibited.

## 8. Environment boundary

GoJet remains the native architecture defined by the Master Plan: Nginx, eight Go programs, PHP 8.3 FPM installer, MySQL 8.x, Redis, ClamAV and local filesystem.

Production Node HTTP/SSR/PM2 and Docker/Compose remain prohibited. Node/Vite are build/test tools only. PHP must not become the business API.

P20 verification must not create a ninth production daemon or substitute a development runtime for the production contract.

## 9. Frozen evidence range

The P20 test contract is frozen as `P20-T001..P20-T049`.

- T001-T008 cover candidate, traceability, schema/build, signed authority, evidence-index and defect/decision freeze.
- T009-T026 cover the real correlated P0 workflow and critical integrated protection/lifecycle behavior.
- T027-T038 cover cross-surface consistency, browser, accessibility, visual, SEO, performance and observability parity.
- T039-T046 cover failure and recovery.
- T047 is the G0-G10 release-candidate Gate ledger.
- T048 is exact-head release-candidate evidence coherence.
- T049 is accountable exact-head closure.

All P20-owned evidence must bind to one exact candidate revision. Inherited signed authority must be live/digest bound. Mixed, stale, malformed, missing or secret-bearing evidence fails closed.

## 10. Closure discipline

Pre-sign closure may prove whole-product verification readiness but is not merge authority.

Final merge authority requires:

1. `P20-T001..P20-T048` PASS on one pre-sign implementation SHA;
2. complete applicable exact-head regression PASS;
3. complete G0-G10 release-candidate disposition ledger with the G1 carry-forward semantics above and no fabricated later-node completion;
4. P0/P1/DECISION REQUIRED = `0/0/0`;
5. this review changed to signed status by exactly one direct review-only child commit;
6. the signed child independently reruns the required exact-head evidence and `P20-T049`;
7. final closure reports `phase=signed`, `review_phase=signed`, `review_only_signed_child=true`, `merge_authoritative=true`.

Until those conditions hold, P20 must not be merged and P21 must not start.

## Accountable review placeholders

### Product Owner — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Product Owner`  
Decision: **PENDING**

### Backend Lead — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Backend Lead`  
Decision: **PENDING**

### Frontend Lead — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Frontend Lead`  
Decision: **PENDING**

### Security Reviewer — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Security Reviewer`  
Decision: **PENDING**

### SEO Owner — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as SEO Owner`  
Decision: **PENDING**

### Design Lead — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Design Lead`  
Decision: **PENDING**

### Accessibility Reviewer — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Accessibility Reviewer`  
Decision: **PENDING**

### Performance Owner — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Performance Owner`  
Decision: **PENDING**

### QA Lead — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as QA Lead`  
Decision: **PENDING**

No signature is present in this contract-drafting state.
