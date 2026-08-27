# P17 Admin, Permissions and Audit — Accountable Technical Review

Node: `P17`  
Issue: #46  
Base integration commit: `62d682a25532eef3cc207a5e9964a62f6072ede7`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**

## 1. Review boundary

This file establishes the P17 accountable-review contract before implementation. It is not a PASS record, signature, completion claim or merge authority.

P17 owns the administrator access/permission lifecycle, broader operations/audit governance, independent domain-entitlement Admin decision workflow, user API-key lifecycle and generic outbound-webhook lifecycle. P17 also closes only the explicitly shared contributions assigned by the Master Plan.

P17 does not re-own P06 domain identity/entitlement resolver, P09 ClamAV/file scan authority, P12 Workspace membership/notification core, P13 billing/payment callback authority, P14 support/mail authority, P15 customer Authentication/OAuth lifecycle, P16 destination/domain risk and abuse decision authority, P18 Docs, P19 Website/SEO or release-wide P20-P22 work.

## 2. Immediate signed predecessor authority

- P16 signed source: `c22d87102a8a691b5d1d1a31506def21112700e7`
- P16 integration commit: `62d682a25532eef3cc207a5e9964a62f6072ede7`
- P16 closure run: `33010844881`
- P16 closure artifact: `9630819391`
- P16 closure digest: `sha256:00dbba2180f88ecdb6b369cb97abfdcafd211789088837d39e02a2d331a75722`
- P16 closure: `phase=signed`, `merge_authoritative=true`, affected matrix `55/55`, P0/P1/DECISION REQUIRED `0/0/0`.

P17 starts only from that merged integration commit. The P16 Trust/Destination Risk/Domain Risk/Abuse safety authority is inherited and may not be reinterpreted.

## 3. Frozen capability boundary

P17-owned:

- `CAP-ADMIN-ACCESS` — G3/G6/G10.
- `CAP-OPS-AUDIT` — G3/G6/G13.
- `CAP-API-KEYS` — G3/G6.
- `CAP-USER-WEBHOOKS` — G3/G6.

P17-shared contributions only:

- `CAP-OFFICIAL-DOMAINS` — P05/P17.
- `CAP-FILES` — P09/P17.
- `CAP-NOTIFICATIONS` — P12/P13-P17.
- `CAP-TURNSTILE` — P14/P15/P17.
- `CAP-DOMAIN-ENTITLEMENT` — P06/P13/P14/P17.
- `CAP-DOMAIN-RISK` — P06/P16/P17.
- `CAP-ABUSE` — P16/P17.
- `CAP-ANNOUNCEMENTS-SETTINGS` — P17/P19.

No capability in this list permits P17 to replace predecessor security or business authority.

## 4. Permission separation boundary

Dedicated server permissions are mandatory. An authenticated administrator, UI-visible navigation item or broad `is_admin`/superuser flag is not sufficient authorization.

At minimum these IA permissions remain separate: `platform.read`, `admins.manage`, `users.manage`, `workspaces.manage`, `links.manage`, `domains.manage`, `domains.risk.manage`, `domains.entitlements.manage`, `security.manage`, `files.manage`, `tickets.manage`, `operations.manage`, `billing.manage`, `mail.manage`, `settings.manage`, `content.manage`.

Hard separations include:

- `tickets.manage` does not grant domain entitlement decisions;
- `domains.manage` does not grant domain risk or entitlement decisions;
- `security.manage` does not grant administrator/role lifecycle or operations authority;
- frontend hiding never substitutes for an API permission decision;
- Workspace API-key/webhook authority remains tenant- and role-bound through server-side Workspace checks.

## 5. Domain-entitlement governance boundary

P17 supplies the Admin queue/detail/decision governance around the already authoritative P06/P13 resolver and P14 request linkage.

IA exact APIs:

```text
GET  /api/admin/domain-entitlements
GET  /api/admin/domain-entitlements/{workspaceId}
POST /api/admin/domain-entitlements/{workspaceId}/decisions
```

Approve requires `domain_limit`, `starts_at`, `expires_at`, `reason` and an existing support-ticket link. A manual approval expiry is mandatory and later than start. Deny grants nothing. Suspend/revoke are immediate. Restore requires current valid security/ownership evidence and succeeds only when all independent axes permit.

Ticket creation, reply, assignment or close never creates entitlement. Normal plan downgrade grace and inherited source precedence remain unchanged. P16 domain-risk/abuse safety can independently block an otherwise valid entitlement.

## 6. Administrator identity, MFA and session boundary

Administrator identity/permission lifecycle is separate from customer account/Workspace membership authority. P17 may reuse reviewed P15 session/CSRF/Origin/rate primitives but cannot reinterpret customer OAuth/account completion.

Admin login, TOTP, lock/rate behavior and session rotation/revocation are server-authoritative. Raw password, TOTP secret, recovery material, session token and CSRF secret are excluded from logs, audit and ordinary evidence. A revoked administrator session must immediately lose authority even if a browser UI is stale.

## 7. API-key boundary

User API keys are Workspace-owned capabilities. Creation requires server-authorized Workspace authority and reviewed scopes. Raw secret is generated server-side, returned exactly once and never recoverable from storage. Stored metadata may expose a safe prefix/key ID only.

Rotation atomically invalidates the old secret. Revocation and expiry immediately remove authority. Scope and rate limits are enforced by the server, not client metadata. Every lifecycle mutation is auditable without logging the raw key.

## 8. Generic outbound-webhook boundary

Generic user outbound webhooks are not P13 payment callbacks. P17 must not reuse payment callback ingress semantics as outbound webhook authority.

Webhook endpoint ownership, signing secret, enable/disable state and delivery history are Workspace-bound. The signing contract must bind reviewed delivery ID/timestamp/body semantics. Raw webhook secret is never present in ordinary logs/evidence and rotation invalidates old signing authority.

Every outbound connection validates scheme, host and resolved address before connect and after each redirect. Loopback, private, link-local, metadata, reserved, mixed public/private and DNS-rebound results fail closed. Response size, redirect count and total time are bounded.

Delivery retry/backoff is durable and idempotent. Disabled endpoints stop new delivery authority. Restart recovery cannot duplicate authoritative delivery state.

P17 must not create a ninth production daemon. Generic webhook delivery contributes to the fixed `SVC-OPS-MONITOR` / `operationsmonitor` service identity.

## 9. Operations and audit boundary

The Admin services surface recognizes exactly these eight identities:

`redirectengine`, `analyticsworker`, `analyticsreconciler`, `platformapi`, `mailworker`, `fileworker`, `operationsmonitor`, `logreceiver`.

Service restart and operational job requeue require `operations.manage`, visible impact/reason confirmation and immutable audit. Operational actions never fabricate business success or silently bypass a failed queue/security state.

Audit query is append-only authority. It may expose time, actor, action, resource, Workspace, result, safe IP metadata, request/correlation ID, safe before/after and reason. Passwords, sessions/tokens, OAuth/payment/webhook/API-key secrets, DB credentials, ClamAV private paths and user content are prohibited.

## 10. Inherited product governance

- P09 remains the only mandatory ClamAV/file malware authority; Admin restore/publish cannot bypass it.
- P16 remains exact-current destination/domain risk and abuse action authority; P17 only supplies broader administrator governance.
- P13 payment callbacks remain separate from user webhooks.
- P14 ticket/mail actions remain communication/request authority only.
- P12 notification store/read-state/dedupe/deep-link core remains inherited; P17 only emits its producer events.
- P05/P16 destination-risk parity remains mandatory for official-domain governance.
- P14/P15 Turnstile behavior remains inherited; P17 Admin settings may configure policy only through masked/fail-closed authority.

## 11. Route and browser boundary

P17 browser evidence is limited to Page-Level IA routes. Workspace developer surfaces are `/app/api-keys` and `/app/webhooks`. Admin Access uses `/admin/access/administrators[/{adminId}]` and `/admin/access/roles`; Audit uses `/admin/audit`; Operations uses `/admin/operations/jobs` and `/admin/operations/services`; domain-entitlement governance uses `/admin/domain-entitlements[/{workspaceId}]`; P17 shared platform/governance routes use the IA registry only.

All Workspace/Admin routes are no-store/noindex and absent from sitemaps. Direct URL load and each API request repeat server authentication/tenant/permission checks.

The Design System remains the only exact visual authority. Browser evidence covers applicable desktop/tablet/mobile and 320 CSS px reflow, keyboard/focus/name-role-value, reduced motion, persistent error/result states and visible high-risk confirmation. Hover-only destructive actions and toast-only persistent state are insufficient.

## 12. Fixed architecture and environment boundary

P17 implementation evidence uses real MySQL 8.x, Redis where coordination/rate/idempotency needs it, native Go `platformapi`, fixed `operationsmonitor` for durable webhook/operations work and deterministic local DNS/HTTP fixtures for webhook SSRF/rebinding tests.

Production Docker/Compose/Node runtime remains prohibited. Test containers are evidence infrastructure only and do not create production architecture.

## 13. Frozen evidence and case contract

Required case range: **P17-T001..P17-T035**.

- T001..T021: administrator/permission/domain-entitlement/resource/operations/settings/audit authority.
- T022..T024: API-key lifecycle/security/tenant authority.
- T025..T029: outbound-webhook ownership/signing/SSRF/retry/RBAC/audit authority.
- T030: Admin Access browser authority.
- T031: Admin governance/domain-entitlement browser authority.
- T032: Workspace API-key/webhook browser authority.
- T033: Operations/Audit/Platform browser authority.
- T034: exact-head evidence coherence.
- T035: accountable signed exact-head closure.

Evidence root: `artifacts/v10/P17/`.

Real server/database/browser evidence is required where frozen. Mocks, screenshots alone, predecessor claims or legacy repositories cannot satisfy P17 completion.

## 14. Pending implementation review

Pending: P17-T001..P17-T035, exact implementation SHA, durable admin/role/permission state, domain-entitlement governance, API-key lifecycle, webhook delivery/security, operations/audit authority, real browser evidence, exact-head coherence, affected regression matrix and zero defect/decision ledger.

No P17 PASS, Gate closure, Ready-for-review or merge authority is claimed in this state.

## 15. Signed-revision rule

When all required evidence is complete, this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record the reviewed pre-sign exact implementation SHA, accountable reviewer/date, P17-T001..P17-T035 disposition, P0=0, P1=0, `DECISION REQUIRED`=0 and truthful ownership/Gate disposition.

If signing this file changes HEAD, the signed revision itself must rerun and pass the complete required affected exact-head matrix before P17 can be marked complete or merged. P18 starts only from the resulting merged P17 integration commit.
