# P10 Text Sharing — Accountable Technical Review

Node: `P10`  
Issue: #27  
Base integration commit: `0c43b9e5fa9abb9da7231e4ab5bd6d8a76f6d9a8`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

## 1. Review boundary

This document now records the accountable P10 technical review after the frozen contract and the pre-sign exact-head evidence completed successfully. The signature approves the P10-owned implementation and evidence identified below; it does not, by itself, make this signed revision merge-authoritative. The signed revision itself must rerun and pass the complete affected exact-head matrix and P10-T001..P10-T020 closure before P10 can be marked complete or merged.

Legacy Text code, manually edited database rows, fixture-only browser states, screenshot-only proof, 200 placeholder pages, robots metadata without raw HTTP verification, or a sitemap assertion without inspectable output cannot substitute for current-repository P10 evidence.

P10 closes only its CAP-TEXT implementation and its own G7 UGC/noindex contribution when same-revision signed closure passes. Release-wide G7 remains later-owned by P18/P19/P20/P22.

## 2. Frozen capability and ownership boundary

- `CAP-TEXT` — REQUIRED — owner P10 — Gates G3/G7.
- P10 Entry dependency: P04 resource/public pattern availability.
- Current base is the later integrated `main` at `0c43b9e5fa9abb9da7231e4ab5bd6d8a76f6d9a8`.
- P10 may close CAP-TEXT and its own G7 UGC/noindex contribution only.
- Release-wide G7 remains later-owned by P18/P19/P20/P22.

## 3. Frozen route and API authority

IA-authoritative browser/public routes:

```text
APP-TEXT         /app/text
APP-TEXT-DETAIL  /app/text/{shareId}
PUB-TEXT         /t/{slug}
PUB-ABUSE-REPORT /abuse/report
```

The IA also registers `/api/public/text/{slug}` in `API-PUBLIC-REQUIRED` and explicitly names `POST /api/public/text/{slug}` as a PUB-TEXT dependency.

For Workspace Text, the IA names `required text-share APIs` and `text detail/update/delete APIs`; it does **not** freeze an exact Workspace HTTP method/path family. P10 therefore freezes the following **current-repository implementation contract**, which must never be described as IA-exact authority:

```text
GET    /api/workspaces/{id}/text-shares
POST   /api/workspaces/{id}/text-shares
GET    /api/workspaces/{id}/text-shares/{shareId}
PATCH  /api/workspaces/{id}/text-shares/{shareId}
DELETE /api/workspaces/{id}/text-shares/{shareId}
```

Public actions remain within the IA path family:

- `GET /t/{slug}` — render the public Text lifecycle surface.
- `POST /t/{slug}` — same-route password page action; may establish an opaque HttpOnly authorization cookie.
- `POST /api/public/text/{slug}` — server-mediated authorized public Text action for copy/download/one-time consumption.
- `GET /t/{slug}?download=1` — query input on PUB-TEXT, not a second canonical route; authorized success is a `text/plain` attachment.
- Abuse entry is the canonical `/abuse/report`; P10 does not create an alternate abuse route or claim P16 `CAP-ABUSE` completion.

No legacy/compatibility alias is approved.

## 4. Frozen Text authority and lifecycle

1. Text resources are Workspace-owned and every internal read/mutation repeats server-side authentication, tenant and RBAC checks.
2. Public path authority is an opaque server-generated slug; internal IDs are not public path authority.
3. User Text content is UTF-8 data and must not execute as HTML/script on the public surface.
4. Visibility is server-authoritative: `private` never exposes content publicly; `public` remains subject to password, expiry, one-time consumption and removal.
5. Optional password protection uses only a server-side verifier; plaintext passwords never persist or enter URLs/logs.
6. Expired resources return 410 publicly with no content.
7. One-time consumption is atomic; one authorized success records durable `consumed_at`, later public access returns 410.
8. Removed/deleted resources return 410 publicly and stale writes cannot resurrect them.
9. Versioned mutation uses optimistic concurrency; stale writes return 409.
10. Copy/download cannot bypass the same access/lifecycle authority that governs the public page.
11. Unknown/malformed public slugs return 404 without internal identifier, tenant, stack or content leakage.
12. UGC responses must not be incorrectly reused through shared caching.

## 5. Frozen public UGC / G7 subset

`PUB-TEXT /t/{slug}` is permanently `noindex` in V10.

- Sitemap membership: **no**.
- Canonical: none.
- Locale alternate/hreflang: none.
- Structured data: none.
- Internal-link parent: Workspace share action only.
- Available: 200.
- Password/access gate: 401/403.
- Unknown: 404.
- Expired/removed/consumed: 410.
- HTML must expose noindex semantics.
- Public/API machine responses carry applicable `X-Robots-Tag: noindex, nofollow`.
- 200 soft-404 behavior is prohibited.
- Public Text UGC must not enter Website or Docs sitemaps.

This is a P10 G7 UGC/noindex contribution, not release-wide G7 closure.

## 6. Workspace state contract

`APP-TEXT /app/text` applicable IA states:

`loading`, `empty`, `edit`, `read-only`, `quota-reached`, `error`.

`APP-TEXT-DETAIL /app/text/{shareId}` applicable IA states:

`loading`, `edit`, `read-only`, `preview`, `conflict`, `expired`, `deleted`, `error`.

The implementation may compose only existing Design System tokens/components. Exact visual values remain owned exclusively by the Design System.

## 7. Evidence and case contract

Required P10 case range: **P10-T001..P10-T020**.

The `P10-Txxx` identifiers are frozen by this P10 contract revision. The Master Plan supplies the requirements—not these IDs—including auth, private, public, expired, not-found, noindex and status-code evidence.

Evidence root: `artifacts/v10/P10/`.

Required specialized evidence:

- `artifacts/v10/P10/api/`
- `artifacts/v10/P10/browser/`
- `artifacts/v10/P10/headers/`
- exact-head evidence index / producer bindings
- signed affected-regression closure

P10 implementation evidence must also cover the required capability implementation columns: Backend, DB/Migration, API, UI, RBAC, States, Browser, Security, Observability and Release; any genuinely non-applicable column must record `N/A` with a reason.

## 8. Accountable implementation review

Accountable reviewer identity: **GPT-5.6 Sol — CAP-TEXT Technical Review**  
Review date: **2026-08-23**  
Pre-sign exact implementation SHA: `587ba15777a584abb27a42f8e789a703f54c500b`

### Pre-sign evidence disposition

- P10-T001..P10-T018: PASS on the pre-sign exact implementation SHA.
- P10-T019: PASS — exact-head evidence coherence; run `32643414922`, artifact `9494244638`, digest `sha256:11775eea1fd309dbb709084021923126c348b3141d5cc8cc424b6d798b20ec1b`.
- P10-T020: PASS — pre-sign closure / merge-authoritative=false; run `32643414755`, artifact `9494273945`, digest `sha256:df43626a842531ec769e412165820800568ed32f0313aadef5e69852a342e8b7`.
- P10 Text Contract: PASS; run `32643414964`, artifact `9494230513`, digest `sha256:f6eec312c3e56907fe67f22e95686a21fca2eb04fd0411d418991a557df6926b`.
- P10 Real Text Integration: PASS; run `32643414999`, artifact `9494233060`, digest `sha256:9a503fade3ded6c1fcb35568beea233158e936926873d9437ee4e0641065d615`.
- P10 Workspace Text Browser: PASS; run `32643414912`, artifact `9494240369`, digest `sha256:e23eb9572f68d6aa6f862f96fc1f9fe797b0f7f041640aa4baa171a64d18d49a`.
- Pre-sign closure bound 19/19 P10 input evidence and 30/30 affected current-head workflows to `587ba15777a584abb27a42f8e789a703f54c500b`.
- Inherited P09 authority: signed source `eafa369a9c150c22c2c14c9f21848a9544f4f96a`, closure run `32618657967`, artifact `9487743843`, digest `sha256:f12aeeb5503bf375314f1d13a2d9833180d6617322765cef2aae0d728cc278d7`, `phase=signed`, `merge_authoritative=true`, defects 0/0/0.

### Role approvals

- Backend Lead: APPROVED
- Frontend Lead: APPROVED
- QA Lead: APPROVED
- Accessibility Reviewer: APPROVED
- Security Reviewer: APPROVED
- Product/API Reviewer: APPROVED

### Defect and decision ledger

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0

### Gate disposition

- G3 P10: PASS — CAP-TEXT functional/API subset only.
- G7 P10: PASS — Text UGC/noindex/sitemap-exclusion subset only.
- Full/release-wide G7 remains OPEN and later-owned by P18/P19/P20/P22.

This review signs the accountable technical disposition of the pre-sign implementation and evidence. It does not claim that the new signed HEAD is already merge-authoritative.

## 9. Signed-revision rule

This signature changes HEAD. Therefore the signed revision itself must rerun P10-T001..P10-T020 and the complete 30-workflow affected exact-head matrix, while continuing to bind the authoritative P09 signed closure.

The signed-head closure is authoritative only if it returns `status=PASS`, `phase=signed`, `merge_authoritative=true`, 19/19 input evidence, 30/30 required workflows, and the signed defect ledger remains P0=0, P1=0, `DECISION REQUIRED`=0.

Until that same-revision signed closure passes, PR #28 must remain Draft and P10 must not be marked complete or merged. SAME-REVISION CI REQUIRED.
