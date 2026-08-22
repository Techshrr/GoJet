# P08 QR — Accountable Technical Review

Node: **P08 — QR**  
Issue: **#23**  
Base integration commit: `04941afc59db763e6c7db8a67721dea542c72a43`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**

## 1. Review boundary

This file freezes the P08 review contract before implementation. It is not a PASS record, does not close P08, and does not close release-wide G10.

The reviewer must evaluate the implementation and evidence produced by the exact P08 candidate revision. No legacy repository, screenshot-only claim, manual visual assertion or prior node result can substitute for current-repository P08 evidence.

## 2. Frozen correctness and security invariants

1. **Authoritative source association** — each P08 QR resource belongs to exactly one Workspace and references one authoritative same-Workspace source Link. The client cannot choose an alternate raw encoded destination that bypasses Link authority.
2. **Server-derived encoded value** — the QR payload is the current GoJet public short URL derived by the server from authoritative source-Link state. It is not the customer's primary/routing/A-B destination and therefore cannot become an alternate destination-risk bypass path.
3. **Source safety remains live** — create, preview and download are available only when the source Link satisfies the current P05/P06 authority chain. Destination-risk `pending`, `review`, `block`, `missing`, `malformed` and `stale` deny QR generation/distribution. A previously allowed target fingerprint cannot authorize a changed reachable-target set.
4. **Domain authority is independent** — when the source uses a custom hostname, applicable P06 entitlement/ownership/DNS/HTTPS/domain-risk authority remains required. Domain trust never substitutes for destination allow, and destination allow never substitutes for domain authority.
5. **Real QR artifacts** — supported P08 download formats are PNG and SVG. Production/server rendering must generate real QR bytes. Placeholder art, decorative grids, DOM-only representations and client-side fake payloads are not acceptable evidence.
6. **Independent machine decode** — at least one decoder path that is independent from the production encoder must decode the generated artifacts to the exact authoritative short URL. Renderer metadata or visible text is not decode evidence.
7. **Deterministic download contract** — for unchanged QR identity, format and authoritative source state, repeated downloads are byte-deterministic and have stable SHA-256 evidence. Response media type, filename and cache/index policy are explicit and safe.
8. **Tenant/RBAC/quota are server-authoritative** — read, create, delete, preview and download repeat server-side membership/action checks. Quota denial creates neither an authoritative row nor generated artifact. UI hiding is never treated as authorization.
9. **Deletion is truthful** — after deletion, detail, preview and download for the QR ID are unavailable and no new artifact can be generated. Previously downloaded media is not falsely described as remotely revocable; when scanned later it still resolves through the live GoJet short-link authority.
10. **One QR authority** — `/app/qr`, `/app/qr/{qrId}` and the Link Detail QR integration use the same P08 records and APIs. P08 must remove the truthful P05 placeholder without creating a second client-side QR state machine.
11. **IA state fidelity** — `APP-QR` implements `loading`, `empty`, `create`, `risk-denied`, `quota-reached`, `error`; `APP-QR-DETAIL` implements `loading`, `ready`, `source-link-review`, `source-link-block`, `deleted`, `error`. States are server-derived and visibly distinct.
12. **Design System evidence** — canonical evidence viewports are desktop 1440×900, tablet 1024×768 and mobile 390×844. There is no root/body horizontal overflow, required-content clipping, private breakpoint system or color-only safety meaning.
13. **Exact-head evidence** — API results, generated assets, artifact digests, scanner output, browser evidence, accessibility evidence and signed review must identify one exact P08 implementation commit. Final closure reruns the complete affected matrix on the signed revision.
14. **Production boundary** — production Docker/Compose and production Node HTTP/SSR/dev-server runtime remain prohibited. P08 cannot weaken any P00-P07 security or native-runtime invariant merely to pass QR tests.

## 3. Frozen API and product boundary

P08 owns the current-repository QR collection/detail/render/download family:

```text
GET    /api/workspaces/{id}/qr-codes
POST   /api/workspaces/{id}/qr-codes
GET    /api/workspaces/{id}/qr-codes/{qrId}
DELETE /api/workspaces/{id}/qr-codes/{qrId}
GET    /api/workspaces/{id}/qr-codes/{qrId}/preview?format={format}
GET    /api/workspaces/{id}/qr-codes/{qrId}/download?format={format}
```

Browser routes remain exactly:

```text
/app/qr
/app/qr/{qrId}
```

No compatibility alias or legacy QR route is approved by this review contract.

## 4. Evidence and case contract

Required case range: **P08-T001..P08-T016**.

The final evidence package must contain current-repository evidence for:

- authoritative MySQL QR persistence and tenant isolation;
- server-side permission and quota enforcement;
- P05/P06 source-Link risk/domain authority integration;
- real PNG/SVG generation and download bytes;
- independent scanner/decoder results and SHA-256 digests;
- deletion/error lifecycle behavior;
- real Workspace `/app/qr` and `/app/qr/{qrId}` browser behavior;
- Link Detail QR integration;
- desktop/tablet/mobile responsive evidence;
- keyboard/name-role-value/non-color state evidence;
- one-exact-head P08 and affected P00-P07 regression closure.

Evidence root: `artifacts/v10/P08/`.

## 5. Gate scope

P08 contributes only its owned subset to:

- **G3** — real functional/API/resource evidence for CAP-QR;
- **G10** — machine-decodable generated-asset correctness and QR resource behavior.

A signed P08 review does **not** by itself close release-wide G3 or G10 obligations owned by later nodes/gates.

## 6. Pending implementation review

The following cannot be signed during contract freeze and remain pending until the implementation candidate exists:

- P08-T001..P08-T016 results;
- exact implementation SHA;
- generated PNG/SVG artifact digests;
- independent decoder result;
- browser/accessibility manifests;
- affected P00-P07 exact-head regression matrix;
- final P0/P1 defect ledger;
- final unresolved `DECISION REQUIRED` count.

No P08 PASS or Exit claim is made in this state.

## 7. Signed-revision rule

When implementation evidence is complete, this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record the 40-hex implementation commit, P08-T001..P08-T016 PASS evidence, accountable reviewer identity/date, P0=0, P1=0, unresolved `DECISION REQUIRED`=0, G3/G10 P08-subset disposition and the required same-revision CI/closure rerun.

If signing changes this file and therefore changes HEAD, the signed revision itself must rerun and pass the full affected exact-head matrix before P08 can be marked complete or merged.
