# P08 QR — Accountable Technical Review

Node: **P08 — QR**  
Issue: **#23**  
Base integration commit: `04941afc59db763e6c7db8a67721dea542c72a43`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

Review date: **2026-08-22**  
Accountable reviewer identity: **GPT-5.6 Sol — CAP-QR Technical Review**  
Pre-sign exact implementation SHA: `7b5c0f095c2a5852cd55697cbb08bdad1dc65263`

## 1. Review boundary and disposition

This review signs the P08 CAP-QR implementation only after the pre-sign exact-head candidate completed P08-T001..P08-T016 and the affected P00-P08 regression matrix. The pre-sign closure is evidence for signing; it is deliberately **not** merge-authoritative because this signed review changes HEAD.

No legacy repository, screenshot-only claim, manual visual assertion or prior-node artifact is used as current P08 correctness authority. The signed revision must produce its own exact-head evidence before P08 can be marked complete or merged.

The review approves only the P08-owned G3/G10 contribution. It does **not** close release-wide G3 or release-wide G10 obligations owned by later nodes or gates.

## 2. Correctness and security invariant review

1. **Authoritative source association: APPROVED** — each QR belongs to one Workspace and references one authoritative same-Workspace Link; the client cannot substitute an alternate raw destination.
2. **Server-derived encoded value: APPROVED** — generated QR payloads encode the current server-derived GoJet public short URL, never the customer's primary/routing/A-B destination.
3. **Source safety remains live: APPROVED** — create/preview/download fail closed for destination-risk `pending`, `review`, `block`, `missing`, `malformed` and `stale`; changed target fingerprints do not inherit prior allow authority.
4. **Domain authority remains independent: APPROVED** — applicable P06 entitlement/ownership/DNS/HTTPS/domain-risk authority remains independently required for custom hostnames.
5. **Real generated artifacts: APPROVED** — PNG and SVG are real server-generated QR bytes, not decorative/client-side substitutes.
6. **Independent machine decode: APPROVED** — ZBar independently decodes both generated PNG and SVG evidence to the exact authoritative GoJet short URL; SVG is rasterized independently before decode.
7. **Deterministic download contract: APPROVED** — unchanged identity/source/format produces stable bytes and SHA-256 evidence with explicit media type, filename, cache and index policy.
8. **Tenant/RBAC/quota authority: APPROVED** — server-side tenant, membership/action and quota enforcement is demonstrated; denied quota does not mutate authoritative resource counts.
9. **Deletion truthfulness: APPROVED** — deleted QR detail/preview/download return truthful unavailable states while already-downloaded bytes remain locally hashable and resolve through live Link authority.
10. **One QR authority: APPROVED** — `/app/qr`, `/app/qr/{qrId}` and Link Detail use the same P08 resources/APIs; the P05 placeholder is removed without creating a second client state machine.
11. **IA state fidelity: APPROVED** — list/detail loading, empty, create, risk-denied, quota, ready, review, block, deleted and error states are server-backed and visibly distinct.
12. **Design System/accessibility evidence: APPROVED** — canonical 1440×900, 1024×768 and 390×844 evidence is clean; 320×800 reflow has zero root/body overflow; keyboard-only source selection, visible focus, reduced-motion and non-color safety semantics are demonstrated.
13. **Exact-head evidence: APPROVED** — integration, scanner, browser, coherence and pre-sign closure all identify the same pre-sign implementation SHA.
14. **Production boundary: APPROVED** — production Docker/Compose and production Node HTTP/SSR/dev-server runtime prohibitions remain intact; P00-P07 authority was not weakened to satisfy P08.

## 3. API and product boundary

Approved P08 API family remains:

```text
GET    /api/workspaces/{id}/qr-codes
POST   /api/workspaces/{id}/qr-codes
GET    /api/workspaces/{id}/qr-codes/{qrId}
DELETE /api/workspaces/{id}/qr-codes/{qrId}
GET    /api/workspaces/{id}/qr-codes/{qrId}/preview?format={format}
GET    /api/workspaces/{id}/qr-codes/{qrId}/download?format={format}
```

Approved Workspace routes remain exactly:

```text
/app/qr
/app/qr/{qrId}
```

No compatibility alias or legacy QR route is approved.

## 4. Pre-sign exact-head evidence record

Pre-sign exact implementation SHA: `7b5c0f095c2a5852cd55697cbb08bdad1dc65263`

- **P08-T001..P08-T010: PASS** — `P08 Real QR Integration` run `32579156491`; artifact `9477346993`; artifact digest `sha256:4d33adb08cc4fd92dc4c338953637d88efd6e97005af819ca2f719ee7c949daf`.
- **P08-T011..P08-T014: PASS** — `P08 Workspace QR Browser` run `32579156490`; artifact `9477357785`; artifact digest `sha256:49829b1ef05516f2f75e40a51a089a70d10034c15b098006f06ecc18468426c4`.
- **P08-T015: PASS** — `P08 Evidence Coherence` run `32579156484`; artifact `9477361846`; artifact digest `sha256:d3c979c27c4f78f1035200697e083058c6660d2e727d1d60b7cc65a0f5d93014`; `P08-T015.json` digest `sha256:5fe9f69a30da591f52a87965910e3d99ea606de8fab8e2dc2bf62c40c69e4682`.
- **P08-T016: PASS — pre-sign closure / merge-authoritative=false** — `P08 Closure` run `32579156499`; artifact `9477364669`; artifact digest `sha256:0d99a9b8124864811760b053571587ad186be5911e31c723d0fccc46b1116a83`.
- `P08-T016.json` digest: `sha256:0979bfd35e496c873ee651b8338c2a6437796a02176277f6f0ce59200373697e`.
- `closure.json` digest: `sha256:b36ae09dfbffcb072cfdc3187f53468c85e10c6a48e28fbd09aae436b0461a20`.
- `regression-manifest.json` digest: `sha256:7f03f7c61f2fc9102d88cd74825561dba961e16bc31f3b0121ddea6ea6dd8fcc`; required workflows `21/21`, missing `0`, pending `0`, failed `0`.
- `evidence-index.json` digest: `sha256:129d6991f7efb26e6d4a1da74706820ed232ba9d492f58cfbe7c7c61f727eff7`.
- Independent decoder authority: `zbarimg (ZBar) independent from github.com/skip2/go-qrcode`; PNG and SVG decode to the exact authoritative short URL and the decoded URL follows the live redirect authority.

The pre-sign candidate therefore records P08-T001..P08-T016 PASS evidence without claiming that the pre-sign T016 is merge-authoritative.

## 5. Accountable approvals

- Backend Lead: APPROVED
- Frontend Lead: APPROVED
- QA Lead: APPROVED
- Accessibility Reviewer: APPROVED
- Security Reviewer: APPROVED
- Product/API Reviewer: APPROVED

These approval roles are consolidated under the accountable reviewer identity above for this technical review record; they do not represent separate human signatures.

## 6. Defect ledger and gate disposition

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0
- G3 P08 functional/API subset: PASS — P08 CAP-QR subset only
- G10 P08 QR/generated-asset subset: PASS — P08 QR/generated-asset subset only
- release-wide G10: OPEN — owned by later release gates; this P08 review does not close it

The evidence establishes the P08-owned contribution only. No release-wide completion claim is made here.

## 7. Signed-revision rule

This signature changes repository HEAD. Therefore the **signed revision itself must rerun** and pass the complete affected exact-head P00-P08 matrix, regenerate P08-T001..P08-T015 evidence on that signed SHA, and rerun P08-T016 with this signed review present.

Only the signed-revision T016 result with `phase=signed`, `merge_authoritative=true`, P0=0, P1=0 and unresolved `DECISION REQUIRED`=0 is authoritative for P08 completion and merge readiness.
