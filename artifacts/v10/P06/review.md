# P06 Custom Domains Review

Node: `P06`  
Issue: #18  
Base integration commit: `ed82747f9f7ddb7696534cdda110f2f7f594b46a`  
Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**

## Review boundary

P06 must prove the current-repository Custom Domains authority model with real persistence, server-side entitlement enforcement, deterministic real DNS/TLS observations, redirect/link-assignment enforcement and Workspace browser evidence. UI state, mocked resolver return values or support-ticket existence cannot substitute for authoritative evidence.

## Frozen authority invariants

- support ticket creation is a request only and never creates active entitlement
- only structured active plan entitlement or separately recorded `manual_approval` can authorize custom domains
- entitlement, ownership, ingress DNS, HTTPS and domain risk remain independent axes
- `domain_limit` is server-enforced atomically under concurrency
- cross-Workspace hostname conflict cannot disclose the other tenant or provider evidence
- normal plan downgrade denies new mutations immediately and gives existing active domains exactly seven calendar days of grace when no other valid source exists
- abuse/fraud/ownership loss/security suspension is immediate with zero grace
- custom-domain Link assignment requires all authoritative axes ready
- custom-host redirect fails closed when entitlement/trust/risk is not current and never falls back to an official GoJet host
- official/custom hosts retain identical destination-risk target/fingerprint policy; custom-domain authority is an additional gate, not a bypass

## Accountable technical review

- Backend Lead: PENDING
- QA Lead: PENDING
- Frontend Lead: PENDING
- Accessibility Reviewer: PENDING
- Security Reviewer: PENDING

## Gate scope

- G3 P06 functional/API subset: PENDING
- G4 P06 browser/responsive subset: PENDING
- G5 P06 accessibility subset: PENDING
- G6 P06 domain authority/security subset: PENDING

Passing P06 subsets will not close the release-wide Gates; later owning nodes and P20/P22 remain required.

## Evidence contract

- test plan: `artifacts/v10/P06/test-plan.json`
- result range: `artifacts/v10/P06/results/P06-T001.json` through `P06-T024.json`
- required evidence: request/response, MySQL, entitlement resolution, audit/revalidation, DNS, TLS, redirect, browser captures and exact-head regression manifest
- mocks or UI-only fixtures do not satisfy P06 Exit Conditions where the P06 integration harness requires real server/query/handshake evidence

## Rule

The review may only be signed after P06-T001..T024 pass on one exact implementation SHA, all P00-P06 affected workflows required by the closure contract are green on that same SHA, evidence is internally consistent and reviewable, P0/P1 defects are zero and `DECISION REQUIRED` is zero. Any signed review revision must itself rerun and pass the complete affected exact-head matrix before merge.
