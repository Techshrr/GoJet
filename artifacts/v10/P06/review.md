# P06 Custom Domains Review

Node: `P06`  
Issue: #18  
Base integration commit: `ed82747f9f7ddb7696534cdda110f2f7f594b46a`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

## Review boundary

P06 proves the current-repository Custom Domains authority model with real persistence, server-side entitlement enforcement, deterministic real DNS/TLS observations, redirect/link-assignment enforcement and Workspace browser evidence. UI state, mocked resolver return values or support-ticket existence cannot substitute for authoritative evidence.

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

- Backend Lead: APPROVED — server authority, persistence, transaction ordering, fail-closed redirect and mutation checkpoints reviewed
- QA Lead: APPROVED — P06-T001..T024 evidence contract, exact-head regression manifest and evidence-index consistency reviewed
- Frontend Lead: APPROVED — Domains list, seven-step wizard, detail surfaces and safe API DTO boundaries reviewed
- Accessibility Reviewer: APPROVED — mobile layout, keyboard tabs, visible status text, control naming and persistent-problem browser evidence reviewed
- Security Reviewer: APPROVED — entitlement isolation, ownership/DNS/TLS/risk independence, zero-grace safety suspension, secret handling and no-fallback redirect behavior reviewed

## Gate scope

- G3 P06 functional/API subset: PASS — P06 subset
- G4 P06 browser/responsive subset: PASS — P06 subset
- G5 P06 accessibility subset: PASS — P06 subset
- G6 P06 domain authority/security subset: PASS — P06 subset

Passing P06 subsets does not close the release-wide Gates; later owning nodes and P20/P22 remain required.

## Pre-sign closure evidence

- exact implementation SHA: `93ab3096f94a304f96f176a38d71f232041011b3`
- P06 Closure run: `32518864782`
- closure artifact ID: `9459862302`
- closure artifact digest: `sha256:0b487e0eab4e0c0441843bd078232dbc57c0a24b5c7e53c1e046c73f3c995728`
- P06-T024: PASS with `errors=[]`
- P06-T001..T023: 23/23 PASS and bound to the same pre-sign exact head
- regression manifest: 12/12 required P00-P06 workflows `completed/success`, with `missing=[]`, `pending=[]`, `failed=[]`
- evidence index: 23/23 input SHA-256 values and the P06-T024 closure digest independently rechecked against the artifact contents

This pre-sign closure satisfies the prerequisite for accountable review signing. It is not sufficient by itself for merge because this signed review changes the implementation revision.

## Defect and decision ledger

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0
- unresolved TODO/FIXME in the P06 PR diff: 0

## Evidence contract

- test plan: `artifacts/v10/P06/test-plan.json`
- result range: `artifacts/v10/P06/results/P06-T001.json` through `P06-T024.json`
- required evidence: request/response, MySQL, entitlement resolution, audit/revalidation, DNS, TLS, redirect, browser captures and exact-head regression manifest
- mocks or UI-only fixtures do not satisfy P06 Exit Conditions where the P06 integration harness requires real server/query/handshake evidence
- generated P06-T024 evidence remains an Actions artifact and is not committed as a repository PASS fixture

## Signed revision requirement

This review signature creates a new implementation revision. The signed revision is merge-authoritative only after P06-T001..T024 pass again on that exact signed SHA and all P00-P06 affected workflows required by the closure contract are green on that same SHA. Any further implementation or review edit invalidates that exact-head evidence and requires another complete rerun.
