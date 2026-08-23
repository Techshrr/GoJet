# P11 Bio — Accountable Technical Review

Node: `P11`  
Issue: #29  
Base integration commit: `4d2186da8b2958c7618a233f53908f2914c389a3`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**

## 1. Review boundary

This file freezes the P11 review contract before implementation. It is not a PASS record, does not close P11, does not close release-wide G7, does not claim P16 destination-risk admin/review completion, and does not claim P18/P19/P20/P22 release verification complete.

Legacy Bio code, manually edited database rows, fixture-only child-risk states, mocked browser requests, screenshot-only proof, a clickable blocked destination, robots metadata without raw HTTP verification, or a sitemap assertion without inspectable diff cannot substitute for current-repository P11 evidence.

## 2. Frozen capability and ownership boundary

- `CAP-BIO` — REQUIRED — owner P11 — Gates G3/G7.
- Master Plan P11 Entry: public resource and Design System patterns available; dependency ledger names P05.
- Integrated base is `main` at `4d2186da8b2958c7618a233f53908f2914c389a3`, containing the merged P10 implementation.
- P11 may close CAP-BIO and its own Bio UGC/noindex contribution only.
- `CAP-BIO-OPT-IN-INDEX` is **DEFERRED**: P11 must not expose an indexing toggle, field, persisted authority or public opt-in path.
- P11 consumes current-repository destination-risk safety authority for child links but does not claim the later P16 review/admin lifecycle complete.
- Release-wide G7 remains later-owned by P18/P19/P20/P22.

Inherited predecessor signed authority is P10 source `7db4fca49ba3fd8e60600ecdf41847c7e2f94776`, closure run `32643830718`, artifact `9494371271`, digest `sha256:6a4bcaed870c6432df40e1fe71cb38dd05a84789d3539ab10dabcbfefe450c50`.

## 3. Frozen route and API authority

IA-authoritative routes/dependencies:

```text
APP-BIO         /app/bio
APP-BIO-DETAIL  /app/bio/{pageId}
PUB-BIO         /p/{slug}
PUB-BIO API     GET /api/public/bio/{slug}
```

For Workspace Bio, the IA names required Bio APIs and route-using Bio APIs; it does **not** freeze an exact Workspace HTTP method/path family. P11 therefore freezes the following **current-repository implementation contract**, which must never be described as IA-exact authority:

```text
GET    /api/workspaces/{id}/bio-pages
POST   /api/workspaces/{id}/bio-pages
GET    /api/workspaces/{id}/bio-pages/{pageId}
PATCH  /api/workspaces/{id}/bio-pages/{pageId}
DELETE /api/workspaces/{id}/bio-pages/{pageId}
POST   /api/workspaces/{id}/bio-pages/{pageId}/publish
POST   /api/workspaces/{id}/bio-pages/{pageId}/pause
```

Child links are managed as server-authoritative ordered page data. No extra legacy route family or compatibility alias is approved.

## 4. Frozen Bio and child-link authority

1. Bio pages are Workspace-owned and every internal read/mutation/publish/pause action repeats server-side authentication, tenant and RBAC checks.
2. Public path authority is an opaque server-generated slug; internal page IDs are not public path authority.
3. Title, profile text and child labels are UTF-8 data and must not execute as active markup/script.
4. Only normalized `http`/`https` destinations are eligible child links; malformed, executable, credential-bearing or unsupported targets are rejected server-side.
5. Every child destination is bound to a normalized fingerprint and current-repository destination-risk result.
6. Changing a destination invalidates its previous allow authority; a stale allow result cannot authorize the new target.
7. `review` or `blocked` children must not remain active public navigation targets. A published Bio page may remain available while those children are rendered as non-navigable safety states.
8. P11 does not claim P16 completion of provider review, operator adjudication or broader destination-risk admin workflows.
9. Draft Bio pages are not public and return 404. Published active pages return 200. Paused pages use the explicit IA paused public state, return 200 noindex, and expose no active child navigation. Removed pages return 410.
10. Versioned mutation uses optimistic concurrency; stale writes return 409.
11. Removed pages cannot be resurrected by stale writes.
12. Public API output mirrors publication/removal and child-link safety authority and must not expose Workspace IDs, provider evidence, internal risk reasons or secrets.

## 5. Frozen public UGC / G7 subset

`PUB-BIO /p/{slug}` is permanently `noindex` in GoJet V10 P11.

- Sitemap membership: **no**.
- Canonical: none.
- Locale alternate/hreflang: none.
- Structured data: none.
- Metadata source: resource-safe title only.
- Internal-link parent: Workspace publish action only.
- Published active: 200.
- Paused: 200 explicit noindex state with no active child navigation.
- Unknown/unpublished/draft: 404.
- Removed: 410.
- HTML must expose noindex semantics.
- Public/API machine responses carry applicable `X-Robots-Tag: noindex, nofollow`.
- Every rendered outbound navigation link must include `rel="ugc nofollow"`.
- Review/blocked child destinations cannot appear as active href/navigation targets.
- Public Bio UGC must not enter Website or Docs sitemaps.
- `CAP-BIO-OPT-IN-INDEX` remains DEFERRED; no owner opt-in indexing behavior is permitted.

This is a P11 G7 Bio UGC/noindex contribution, not release-wide G7 closure.

## 6. Workspace state contract

`APP-BIO /app/bio` applicable IA states:

`loading`, `empty`, `edit`, `preview`, `publish-error`, `quota-reached`.

`APP-BIO-DETAIL /app/bio/{pageId}` applicable IA states:

`loading`, `draft`, `preview`, `published`, `child-link-review`, `child-link-block`, `conflict`, `deleted`.

`PUB-BIO /p/{slug}` applicable IA states:

`available`, `paused`, `risk-blocked-child-link`, `removed`, `not-found`.

The implementation may compose only existing Design System tokens/components. Exact visual values remain owned exclusively by the Design System.

## 7. Evidence and case contract

Required P11 case range: **P11-T001..P11-T020**.

The `P11-Txxx` identifiers are frozen by this P11 contract revision. The Master Plan supplies the requirements—not these IDs—including ownership, risk-blocked link, mobile, noindex and sitemap exclusion evidence.

Evidence root: `artifacts/v10/P11/`.

Required specialized evidence:

- `artifacts/v10/P11/api/`
- `artifacts/v10/P11/browser/`
- `artifacts/v10/P11/headers/`
- `artifacts/v10/P11/sitemap/` with inspectable sitemap diff
- exact-head evidence index / producer bindings
- signed affected-regression closure

P11 implementation evidence must also cover the required capability implementation columns: Backend, DB/Migration, API, UI, RBAC, States, Browser, Security, Observability and Release; any genuinely non-applicable column must record `N/A` with a reason.

## 8. Pending implementation review

Pending: P11-T001..P11-T020, exact implementation SHA, real MySQL/API publication evidence, current destination-risk child-link evidence, raw robots/UGC-rel headers/body evidence, Workspace/Public browser/mobile evidence, sitemap diff, affected exact-head matrix, P0/P1 ledger and unresolved `DECISION REQUIRED` count.

No P11 PASS or Exit claim is made in this state.

## 9. Signed-revision rule

When evidence is complete this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record the 40-hex pre-sign implementation commit, P11-T001..P11-T020 PASS evidence, accountable reviewer identity/date, P0=0, P1=0, unresolved `DECISION REQUIRED`=0, truthful P11 G3/G7 subset disposition, the deferred indexing boundary, P16 later-owner boundary and same-revision CI/closure rerun requirement.

If signing changes this file and therefore changes HEAD, the signed revision itself must rerun and pass the complete affected exact-head matrix before P11 can be marked complete or merged.
