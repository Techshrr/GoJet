# P16 Trust, Destination Risk and Abuse — Accountable Technical Review

Node: `P16`  
Issue: #39  
Base integration commit: `dd70eacf02d4dd79fe82063f3d43610ab11885e8`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

Pre-sign exact implementation SHA: `b1965430acf69174863a871e42523bb8f176f9e2`

Reviewed pre-sign implementation SHA: `b1965430acf69174863a871e42523bb8f176f9e2`

Accountable reviewer: `GPT-5.6 Sol — P16 Technical Review`

Review date: `2026-08-27`

## 1. Review boundary

This file establishes the P16 accountable-review contract before implementation. It is not by itself merge authority; the signed revision must independently satisfy the same-revision CI rule recorded below.

P16 owns the Trust & Safety implementation contribution for destination-risk scanning/rescanning, provider/policy decisions, exact-fingerprint runtime projection, review/override governance, domain reputation/revalidation, public abuse intake and risk-controlled abuse actions. It must preserve signed predecessor seams rather than reimplement them.

P16 does not re-own P05 routing/A-B product behavior, P06 domain entitlement/ownership/ingress-DNS/HTTPS, P09 file quarantine/ClamAV, P12 notification-center core, P15 authentication/session lifecycle, P17 administrator/role/permission lifecycle, P19 final Website/Technical SEO, or release-wide P20-P22 gates.

## 2. Immediate signed predecessor authority

- P15 signed source: `6f39d87f1d94f71590fd79d4551cdd1cea652a76`
- P15 integration commit: `dd70eacf02d4dd79fe82063f3d43610ab11885e8`
- P15 closure run: `32931945354`
- P15 closure artifact: `9593689993`
- P15 closure digest: `sha256:5a43c87ea26f86081523d371de260e100a20c5c05b3581f48223fb70e68cd233`
- P15 closure: `phase=signed`, `merge_authoritative=true`, affected matrix `50/50`, P0/P1/DECISION REQUIRED `0/0/0`.

P16 starts only from that merged integration commit.

## 3. Inherited behavioral seams

P16 must preserve these already integrated authorities:

- P05 reachable-target fingerprint: normalized primary destination plus every enabled routing destination and every enabled A/B destination form one deterministic exact fingerprint.
- P05 risk runtime seam: only exact-current `allow` proceeds; missing/malformed/stale/review/block fail closed.
- P05 redirect ordering: link/domain state → exact-current destination risk → routing/A-B → UTM → password/access/counters → customer redirect.
- P06 custom-domain axes: entitlement, ownership, ingress DNS, HTTPS and domain risk are independent; all applicable axes must pass.
- P09 mandatory ClamAV remains file malware authority only; it is never a URL classifier.
- P12 notification center/read-state/dedupe/deep-link authority remains inherited.
- P15 authentication/session/CSRF/Origin/rate authority remains inherited.

Target-set mutation must invalidate old approval immediately. Semantically equivalent target reordering/deduplication must not create either a bypass or a false new approval. Official and custom hostnames use the same destination-risk contract.

## 4. Frozen capability ownership to be validated

- `CAP-DESTINATION-RISK` — REQUIRED — P05/P16 — P16 closes durable scan/provider/policy/review/override/runtime-projection contribution.
- `CAP-DOMAIN-RISK` — REQUIRED — P06/P16/P17 — P16 closes reputation/revalidation contribution only.
- `CAP-ABUSE` — REQUIRED — P16/P17 — P16 closes public report/evidence/risk-controlled resource-action contribution only.
- `CAP-LINK-ROUTING` / `CAP-LINK-AB` — P16 owns only risk-fingerprint safety contribution.
- `CAP-NOTIFICATIONS` — P16 contributes security/domain/resource producer events only.

P17 retains administrator-account, role, permission and broader operations/audit lifecycle. P16 consumes existing server permission names such as `security.manage` and `domains.risk.manage`; it cannot create alternate permission authority.

## 5. Route and surface boundary

Public P16 surfaces:

```text
PUB-SHORT-OFFICIAL  https://{official-short-host}/{code}
PUB-SHORT-CUSTOM    https://{custom-host}/{code}
PUB-LINK-UNAVAILABLE /linkunavailable?reason={allowlisted}&code={safe-code}
PUB-ABUSE-REPORT     /abuse/report
POST                  /api/public/abuse-reports
```

Admin P16 surfaces:

```text
ADMIN-DEST-RISK   /admin/trust/destination-risk[/{riskId}]
ADMIN-DOMAIN-RISK /admin/trust/domain-risk[/{domainId}]
ADMIN-ABUSE       /admin/trust/abuse[/{reportId}]
```

IA-exact domain-risk APIs are:

```text
GET  /api/admin/domain-risks
GET  /api/admin/domain-risks/{domainId}
POST /api/admin/domain-risks/{domainId}/revalidate
```

Destination-risk and abuse API families are specified by IA as `/api/admin/destination-risks*` and `/api/admin/abuse*`; their exact P16 implementation methods are frozen in `test-plan.json` and must not be misrepresented as IA-exact where IA only defines the family.

Inherited Workspace pages remain owned by their original nodes; P16 supplies real risk states to Link, QR, Bio and Domain surfaces without claiming those pages wholesale.

## 6. Destination-risk safety boundary

- Every reachable target variant belongs to one exact normalized fingerprint.
- Durable observations and decisions bind the exact fingerprint and policy version.
- Only exact-current `allow` may be projected as redirect authority.
- Missing, unknown, malformed, stale, pending, review, block and provider-unavailable states fail closed.
- Destination risk executes before routing, A/B, UTM, password, expiry, click-limit and one-time behavior.
- Custom-domain trust never substitutes for destination allow; destination allow never substitutes for entitlement/domain trust.
- Safety responses expose no unsafe destination, provider evidence, threshold or `continue anyway` bypass.

## 7. Provider and SSRF boundary

External semantic/reputation providers are signal sources, not sole control authority. Timeout, transport error, partial response, malformed payload or provider outage cannot become implicit allow.

Target/provider inspection is server-authoritative and rejects unsafe schemes, userinfo authority, loopback, private, link-local, reserved/metadata destinations and DNS-rebinding paths. Redirect chains are revalidated hop-by-hop. Provider endpoints come only from reviewed server configuration, never arbitrary client input.

CI uses deterministic local provider/DNS/HTTP fixtures to exercise real policy paths; live production credentials and unsafe external private-network probes remain prohibited.

## 8. Manual override boundary

Destination override requires `security.manage`, exact fingerprint, explicit decision, non-empty reason, actor, correlation, bounded validity and policy context. It writes immutable before/after audit.

An override cannot survive a different target fingerprint, expiry or incompatible policy authority. It cannot bypass entitlement, ownership, DNS, HTTPS or independent domain-risk authority.

## 9. Domain-risk boundary

P06 remains authority for hostname identity, entitlement, ownership, ingress DNS and HTTPS axes. P16 supplies reputation/provider/revalidation risk state without collapsing those axes.

Domain provider partial/outage/malformed/stale evidence never becomes allow. Revalidation records current state/history and does not erase other axes. Security or abuse suspension is immediate and has no billing/domain grace path. User-facing reason text is allowlisted and provider evidence remains internal.

## 10. Abuse boundary

Public `/abuse/report` and `POST /api/public/abuse-reports` enforce reviewed field validation, server-side Turnstile, rate/idempotency controls, no-store/noindex and persistent safe success.

P16 abuse actions are limited to P16 risk-controlled resources such as exact destination fingerprints, short-link risk and custom-domain risk. Broader user/workspace/admin lifecycle remains P17-owned.

High-risk actions require server permission, actor, reason, correlation and immutable audit. Recovery is conditional on current safety authority and cannot blindly restore an unsafe resource.

## 11. Notification and evidence boundary

P16 emits security/domain/resource notifications through the inherited P12 notification core. Notifications never become risk-decision or audit authority. Deep links and summaries remain authorization-safe and redact provider evidence, secrets and unnecessary PII.

Evidence root is `artifacts/v10/P16/`. Exact-head evidence includes real MySQL/Redis/native Go boundaries where required, durable risk records, Redis runtime projection, worker/provider/SSRF records, audit, API/security logs and real browser/interstitial captures.

## 12. Fixed service architecture

P16 creates no ninth production daemon. Risk queue execution contributes to the fixed eight-service architecture using the Master Plan `SVC-OPS-MONITOR` / `services/platformapi/cmd/operationsmonitor` identity for risk-task/recovery work while leaving broader P17 operations governance separately owned.

Production Docker/Compose/Node runtime remains prohibited.

## 13. Reviewed implementation disposition

The reviewed pre-sign implementation at `b1965430acf69174863a871e42523bb8f176f9e2` completed the frozen P16 implementation and evidence contract without unresolved P0, P1 or decision-required items. This signed review records that accountable disposition but does not bypass the mandatory signed-revision rerun.

## 14. Signed-revision rule

The signed revision must rerun and pass the complete required affected exact-head matrix before P16 can be marked complete or merged.

The only authorized review status is:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form records the reviewed pre-sign exact implementation SHA, accountable reviewer identity/date, the full frozen P16 case disposition, P0=0, P1=0, `DECISION REQUIRED`=0, truthful ownership/Gate disposition and same-revision closure requirement.

If signing this file changes HEAD, the signed revision itself must independently prove all required evidence and closure authority.

## 15. Accountable signed review disposition

The frozen sections above remain the P16 contract record. This section records the accountable disposition supported by the exact pre-sign evidence and does not replace the signed-revision CI requirement.

### Exact-head evidence disposition

- P16-T001..P16-T014: PASS — durable destination-risk schema/enqueue, normalization, SSRF/rebinding resistance, provider/policy mapping, outage/partial/malformed fail-closed behavior, worker retry/recovery, exact-fingerprint Redis projection, runtime non-allow matrix, official/custom parity, target-mutation invalidation, bounded override/audit and redirect safety ordering/non-disclosure.
- P16-T015..P16-T018: PASS — durable domain reputation authority, independent P06 axes, revalidation/stale lifecycle, provider partial/failure handling and immediate security/abuse suspension without grace.
- P16-T019..P16-T023: PASS — public abuse validation/Turnstile/rate authority, evidence correlation/redaction, Admin abuse lifecycle, risk-controlled resource action/recovery/audit and inherited P12 notification producer contribution.
- P16-T024..P16-T025: PASS — real Admin destination-risk/domain-risk API authority, server-side session/CSRF/RBAC separation, redaction, idempotency and fail-closed action behavior.
- P16-T026: PASS — real Admin Trust & Safety browser authority for destination risk, domain risk and abuse, including required states, responsive/accessibility behavior and high-risk confirmations.
- P16-T027: PASS — real public safety/abuse-report browser authority, allowlisted reasons, no target/provider/bypass disclosure, no-store/noindex/no-referrer behavior, Turnstile/rate/error and persistent-success states.
- P16-T028: PASS — 27 producer cases bound to one exact head across five coherent producers with mixed-head rejection, secret-safe evidence and required browser captures.
- P16-T029: PASS — pre-sign accountable closure with 28/28 input evidence files and 55/55 required affected exact-head workflows.

Pre-sign T029 closure run: `33009747796`

Pre-sign T029 closure artifact: `9622329850`

Pre-sign T029 closure digest: `sha256:58ea0dc2c5bcee376fe672049ec40602bfc4b33a8ebfa5c9e4ea312051c6fcfc`

The pre-sign closure is `phase=pre-sign`, `merge_authoritative=false`. P15 signed predecessor authority was live-bound and archive-digest verified.

### Defect and decision ledger

Evidence disposition: `P16-T001..P16-T029 PASS`

P0/P1/DECISION REQUIRED: `0/0/0`

Review-only signed child: `true`

Signed revision requires complete same-revision affected matrix before merge: `true`

### Capability and ownership disposition

- `CAP-DESTINATION-RISK`: P16 contribution is complete for durable scan/rescan, provider/policy decision, exact-current runtime projection and bounded review/override governance while P05 target fingerprint/routing semantics remain inherited.
- `CAP-DOMAIN-RISK`: only the P16 reputation/revalidation contribution is approved. P06 entitlement/ownership/ingress-DNS/HTTPS authority and P17 later administrator governance remain intact.
- `CAP-ABUSE`: only the P16 public intake/evidence and risk-controlled resource action/recovery contribution is approved. Broader administrator/user/workspace governance remains P17-owned.
- `CAP-NOTIFICATIONS`: P16 producer contribution is approved; P12 notification core/read-state/deep-link authority remains inherited.
- P09 ClamAV, P15 identity/session authority, P17 administrator/role/permission and operations/audit lifecycle, P19 final Website/Technical SEO and release-wide P20-P22 remain outside P16 ownership.

This signature approves the reviewed pre-sign P16 implementation for the mandatory signed-revision CI rerun only. It is not merge authority until this review-only child independently proves `P16-T001..P16-T029`, the complete 55-workflow affected exact-head matrix, `phase=signed`, `merge_authoritative=true`, and the zero defect/decision ledger.
