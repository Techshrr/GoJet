# P10 Text Sharing — Accountable Technical Review

Node: `P10`  
Issue: #27  
Base integration commit: `0c43b9e5fa9abb9da7231e4ab5bd6d8a76f6d9a8`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**

## 1. Review boundary

This file freezes the P10 review contract before implementation. It is not a PASS record, does not close P10, does not close release-wide G7, and does not claim P18/P19/P20/P22 release verification complete.

Legacy Text code, manually edited database rows, fixture-only browser states, screenshot-only proof, 200 placeholder pages, robots metadata without raw HTTP verification, or a sitemap assertion without inspectable output cannot substitute for current-repository P10 evidence.

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

## 8. Pending implementation review

Pending: P10-T001..P10-T020, exact implementation SHA, real MySQL/API lifecycle evidence, raw status/header/noindex evidence, Workspace/Public browser evidence, sitemap-exclusion evidence, affected exact-head matrix, P0/P1 ledger and unresolved `DECISION REQUIRED` count.

No P10 PASS or Exit claim is made in this state.

## 9. Signed-revision rule

When evidence is complete this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record the 40-hex pre-sign implementation commit, P10-T001..P10-T020 PASS evidence, accountable reviewer identity/date, P0=0, P1=0, unresolved `DECISION REQUIRED`=0, truthful P10 G3/G7 subset disposition and same-revision CI/closure rerun requirement.

If signing changes this file and therefore changes HEAD, the signed revision itself must rerun and pass the complete affected exact-head matrix before P10 can be marked complete or merged.
