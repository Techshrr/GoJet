# P16 Trust, Destination Risk and Abuse — Accountable Technical Review

Node: `P16`  
Issue: #39  
Base integration commit: `dd70eacf02d4dd79fe82063f3d43610ab11885e8`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**

## 1. Review boundary

This file establishes the P16 accountable-review contract before implementation. It is not a PASS record, signature, completion claim or merge authority.

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

Destination-risk and abuse API families are specified by IA as `/api/admin/destination-risks*` and `/api/admin/abuse*`; their exact P16 implementation methods must be frozen in `test-plan.json` and must not be misrepresented as IA-exact where IA only defines the family.

Inherited Workspace pages remain owned by their original nodes; P16 must supply real risk states to Link, QR, Bio and Domain surfaces without claiming those pages wholesale.

## 6. Destination-risk safety boundary

- Every reachable target variant belongs to one exact normalized fingerprint.
- Durable observations and decisions must bind the exact fingerprint and policy version.
- Only exact-current `allow` may be projected as redirect authority.
- Missing, unknown, malformed, stale, pending, review, block and provider-unavailable states fail closed.
- Destination risk executes before routing, A/B, UTM, password, expiry, click-limit and one-time behavior.
- Custom-domain trust never substitutes for destination allow; destination allow never substitutes for entitlement/domain trust.
- Safety responses expose no unsafe destination, provider evidence, threshold or `continue anyway` bypass.

## 7. Provider and SSRF boundary

External semantic/reputation providers are signal sources, not sole control authority. Timeout, transport error, partial response, malformed payload or provider outage cannot become implicit allow.

Target/provider inspection is server-authoritative and must reject unsafe schemes, userinfo authority, loopback, private, link-local, reserved/metadata destinations and DNS-rebinding paths. Redirect chains must be revalidated hop-by-hop. Provider endpoints come only from reviewed server configuration, never arbitrary client input.

CI may use deterministic local provider/DNS/HTTP fixtures to exercise real policy paths; live production credentials and unsafe external private-network probes are prohibited.

## 8. Manual override boundary

Destination override requires `security.manage`, exact fingerprint, explicit decision, non-empty reason, actor, correlation, bounded validity and policy context. It writes immutable before/after audit.

An override cannot survive a different target fingerprint, expiry or incompatible policy authority. It cannot bypass entitlement, ownership, DNS, HTTPS or independent domain-risk authority.

## 9. Domain-risk boundary

P06 remains authority for hostname identity, entitlement, ownership, ingress DNS and HTTPS axes. P16 supplies reputation/provider/revalidation risk state without collapsing those axes.

Domain provider partial/outage/malformed/stale evidence never becomes allow. Revalidation records current state/history and does not erase other axes. Security or abuse suspension is immediate and has no billing/domain grace path. User-facing reason text is allowlisted and provider evidence remains internal.

## 10. Abuse boundary

Public `/abuse/report` and `POST /api/public/abuse-reports` must enforce reviewed field validation, server-side Turnstile, rate/idempotency controls, no-store/noindex and persistent safe success.

P16 abuse actions are limited to P16 risk-controlled resources such as exact destination fingerprints, short-link risk and custom-domain risk. Broader user/workspace/admin lifecycle remains P17-owned.

High-risk actions require server permission, actor, reason, correlation and immutable audit. Recovery is conditional on current safety authority and cannot blindly restore an unsafe resource.

## 11. Notification and evidence boundary

P16 may emit security/domain/resource notifications through the inherited P12 notification core. Notifications never become risk-decision or audit authority. Deep links and summaries must remain authorization-safe and redact provider evidence, secrets and unnecessary PII.

Evidence root is `artifacts/v10/P16/`. Exact-head evidence must include real MySQL/Redis/native Go boundaries where required, durable risk records, Redis runtime projection, worker/provider/SSRF records, audit, API/security logs and real browser/interstitial captures.

## 12. Fixed service architecture

P16 must not create a ninth production daemon. Risk queue execution is contributed to the fixed eight-service architecture, using the Master Plan `SVC-OPS-MONITOR` / `services/platformapi/cmd/operationsmonitor` identity for risk-task/recovery work while leaving broader P17 operations governance separately owned.

Production Docker/Compose/Node runtime remains prohibited.

## 13. Pending implementation review

Pending: frozen P16 case range, exact contract authority, durable risk/abuse schema, queue/worker, provider policy adapter, SSRF controls, Redis projection, domain revalidation, override/audit, abuse/report lifecycle, P12 notification producers, Admin/Public APIs, browser evidence, exact-head coherence, affected regression matrix, P0/P1 ledger and unresolved `DECISION REQUIRED` count.

No P16 PASS, Gate closure, Ready-for-review or merge authority is claimed in this state.

## 14. Signed-revision rule

When all required evidence is complete, this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record the reviewed pre-sign exact implementation SHA, accountable reviewer identity/date, the full frozen P16 case disposition, P0=0, P1=0, `DECISION REQUIRED`=0, truthful ownership/Gate disposition and same-revision closure requirement.

If signing this file changes HEAD, the signed revision itself must rerun and pass the complete required affected exact-head matrix before P16 can be marked complete or merged.
