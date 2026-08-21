# P05 Links Vertical Slice Review

Node: `P05`  
Issue: #16  
Status: **SIGNED — PRE-MERGE EXACT-HEAD RERUN REQUIRED**

## Review boundary

P05 requires real current-repository evidence from MySQL, Redis, platformapi, redirectengine, Workspace browser flows, audit records and GoJet safety surfaces. Unit tests or UI-only screenshots cannot substitute for those dependencies.

## Reviewed implementation

Reviewed exact implementation head: `5e63fadbcfe1514e443e4fd4cfbb2e187b80a47e`

The reviewed head completed the full affected regression matrix and P05 closure:

| Workflow | Run | Conclusion |
|---|---:|---|
| P00 Bootstrap and G0 Traceability | `32494088516` | SUCCESS |
| P01 Engineering Foundation | `32494088522` | SUCCESS |
| P02 Brand Foundation | `32494088502` | SUCCESS |
| P03 Design System | `32494088523` | SUCCESS |
| P04 Product Shells | `32494088580` | SUCCESS |
| P05 Links Domain Contract | `32494088548` | SUCCESS |
| P05 Real Integration | `32494088532` | SUCCESS |
| P05 Workspace Browser | `32494088513` | SUCCESS |
| P05 Closure | `32494088555` | SUCCESS |

Exact-head artifacts reviewed:

- Integration artifact `9450943636`, digest `sha256:1781b3b8d4f41b1c9ab9052b5a2f933a8143b991dc3ca61de699801e97713dd4`.
- Browser artifact `9450966522`, digest `sha256:58dfebec1c42c834df1d01490f50f7b49211155a60498edf34267ac2baede60d`.
- Closure artifact `9450973952`, digest `sha256:af12bb38ec0d33a05d1283fee2a2557ffeb7cbec0f5233748190a0f99625ddc0`.

P05-T001 through P05-T019 are PASS and bound to the reviewed exact head. P05-T020 is PASS with 19/19 required input evidence records and 8/8 required regression workflow records bound to the same head.

## Password / access review

The password contract is accepted on the reviewed head:

- PBKDF2-SHA256 verifier contract, version 1, 600000 iterations.
- verifier is not exposed by the Workspace API and plaintext is not persisted.
- destination-risk authority precedes password evaluation.
- challenge `200`, wrong password `401`, rate limit `429`, accepted password `302`.
- password-attempt limit is 10 and successful verification clears the attempt state.
- create / replace / clear browser flow verifies versions 1 / 2 / 3 and preserves the destination-risk fingerprint for password-only mutations.
- the browser verifies the exact password textbox by role and accessible name; the expected Chrome wrong-password 401 diagnostic is observed exactly once while unexpected console/page/request errors remain zero.
- password challenge and wrong-password responses contain no destination disclosure or bypass links; correct password is the only path to the configured customer Location.
- password CSP remains restrictive while permitting legitimate HTTP(S) redirect chains: `form-action 'self' http: https:`; destination URLs are not embedded in the CSP.

## Public safety / visual review

The earlier functional candidate `a16ae14e1db8f328f08c2024112892d1156a3512` was deliberately not signed because manual artifact review found browser-default redirect safety/password HTML. That finding is resolved on the reviewed head.

Manual review of the new 1440×900 browser captures confirms:

- Password challenge is a branded GoJet protected-link surface with fixed lock icon, visible title and explanatory text, visible Password label/input, primary Continue action and Reference area.
- Review state uses a fixed warning icon, visible `Link under review` state, reason text, explicit Next step and Reference area.
- Block state uses a fixed shield/error icon, visible `Link blocked` state, reason text, explicit Next step and Reference area.
- Missing/pending state uses the warning treatment with `Link temporarily unavailable`, reason, Next step and Reference area.
- public surfaces no longer depend on browser-default presentation and do not expose destination links.
- public-surface semantic values are governed by the canonical Design System token projection; token drift and required state semantics are covered by Links package tests.
- light/dark semantic mappings are present in the governed public-surface projection. Release-wide dark/visual obligations remain owned by the later full Gate reviews.

Workspace desktop Password Access and the canonical 390×844 mobile flow were also reviewed. T018 reports root overflow = false, body overflow = false, clipped required text = 0, unnamed visible controls = 0, and keyboard tab focus transitions correctly to Analytics.

## Accountable technical review

- Backend Lead: **PASS** — real MySQL/Redis/API/redirect evidence, persistence, optimistic concurrency, audit/history and access contracts reviewed.
- QA Lead: **PASS** — P05-T001..T020 exact-head evidence and P00-P05 affected regression matrix reviewed.
- Frontend Lead: **PASS** — Workspace Links lifecycle, Access state and branded public surfaces reviewed against browser evidence.
- Accessibility Reviewer: **PASS** — accessible names/roles, keyboard path, focus behavior, non-color safety semantics and mobile overflow/clipping evidence reviewed.
- Security Reviewer: **PASS** — fail-closed destination risk, no bypass/destination leakage, CSP, password verifier/redaction/rate-limit and RBAC/concurrency evidence reviewed.

No P0/P1 defect or unresolved `DECISION REQUIRED` item surfaced in the reviewed P05 evidence or review record.

## Gate scope

- G3 P05 functional/API slice: **PASS**
- G4 P05 browser/responsive slice: **PASS**
- G5 P05 accessibility slice: **PASS**
- G6 P05 destination-risk/RBAC/concurrency slice: **PASS**

These are P05-owned subsets only. They do not close the release-wide Gates; later owning nodes and P20/P22 remain required.

## Pre-merge rule

This signoff changes the branch head. The signed revision itself must now pass the complete affected exact-head workflow matrix, including P05 Closure, before PR #17 may be marked ready or merged. No evidence from the reviewed pre-signoff head may be substituted for a failing signed-head run.
