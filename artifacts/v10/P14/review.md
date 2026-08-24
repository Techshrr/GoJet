# P14 Support Tickets and Mail — Accountable Technical Review

Node: `P14`  
Issue: #35  
Base integration commit: `a94f1d9894916b995a2379571f6ab3de520fc4ba`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

Pre-sign exact implementation SHA: `f62527a10903c988a902ba502b03ff1eb3073a2b`

Accountable reviewer identity: **GPT-5.6 Sol — P14 Technical Review**

Review date: **2026-08-25**

## 1. Review boundary

This file freezes the P14 review contract before implementation. It is not a PASS record and does not close P15 Authentication/OAuth/Account, P16 Trust/Abuse, P17 administrator permission lifecycle or domain-entitlement approval UI, P19 Website/Technical SEO, or release-wide Gates.

A ticket, ticket reply, ticket closure, mail delivery, notification, frontend state, public contact record or support category can never substitute for an independent custom-domain entitlement decision.

## 2. Frozen capability and predecessor boundary

- `CAP-TICKETS` — REQUIRED — owner P14 — G3/G6/G10.
- `CAP-MAIL` — REQUIRED — owner P14 — G3/G6/G10.
- `CAP-TURNSTILE` — REQUIRED — owners P14/P15/P17 — G6/G10/G13; P14 owns only its contact/ticket request path.
- `CAP-DOMAIN-ENTITLEMENT` — REQUIRED — owners P06/P13/P14/P17 — G6/G10; P14 owns request topic/linkage only.
- `CAP-NOTIFICATIONS` — REQUIRED — owners P12/P13-P17 — G3/G5/G6/G10; P12 remains owner of notification core.

Authoritative immediate predecessor is P13 signed source `24cdbdf848bf722e53e38ed15dce12e1d42eb9d2`, integration commit `a94f1d9894916b995a2379571f6ab3de520fc4ba`, closure run `32711262325`, artifact `9514396804`, digest `sha256:494a7942272afac7588eab153c07daf5a1f557c10b58b0dbd915eeda8709e998`, `phase=signed`, `merge_authoritative=true`, defects P0=0/P1=0/`DECISION REQUIRED`=0.

Inherited P12 Workspace/notification authority remains source `9d49d5ebf0e697ae9cd6537c432c27a15edc60bd`, run `32663159008`, artifact `9499336765`, digest `sha256:72ed65c48303654b589edce23e9118ecc963940a7400e27a0f174d7e8ea07c9a`.

Inherited P06 domain authority remains source `4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23`, run `32519298309`, artifact `9460016077`, digest `sha256:21e2fe5898a047e166aac520870070e8072f00885a3c89aaf86736f6ac22a2c8`.

Inherited P09 ClamAV authority remains source `eafa369a9c150c22c2c14c9f21848a9544f4f96a`, run `32618657967`, artifact `9487743843`, digest `sha256:f12aeeb5503bf375314f1d13a2d9833180d6617322765cef2aae0d728cc278d7`.

## 3. Frozen route and API authority

IA-authoritative P14-related browser routes:

```text
WEB-CONTACT        /contact (localized peer)
APP-SUPPORT        /app/support
APP-SUPPORT-NEW    /app/support/new
APP-SUPPORT-THREAD /app/support/{ticketId}
ADMIN-TICKETS      /admin/tickets[/{ticketId}]
ADMIN-MAIL         /admin/mail
```

IA-exact API dependencies are exactly:

```text
POST /api/public/contact
GET /api/support/tickets
POST /api/support/tickets
```

Ticket detail/reply/close APIs, Admin support APIs and Admin mail/settings APIs are P14 implementation authority, not IA-exact authority. There are no invented `/app/tickets` or `/app/mail` aliases.

P14 consumes `tickets.manage` and `mail.manage` permission decisions but does not implement the P17 administrator/role/permission lifecycle. `WEB-CONTACT` functional states are P14-owned while final Website composition, canonical/hreflang and Technical SEO remain P19-owned.

## 4. Frozen ticket and request authority

Durable ticket statuses are exactly `open`, `awaiting_user`, `awaiting_support`, `closed`.

Message kinds are exactly `requester_reply`, `support_reply`, `internal_note`. Internal notes are admin-only and must never appear through requester APIs, notification summaries or outbound requester mail.

`Request access` enters `/app/support/new?category=custom-domain-access` and submission calls `POST /api/support/tickets` only.

A qualifying `custom-domain-access` ticket may be projected to P06 as `requested`, but ticket create/reply/close MUST NOT create `active` entitlement, `manual_approval`, plan grant, custom-domain row, ownership token, DNS/HTTPS/risk state advance or any equivalent authorization.

Cross-Workspace and forged ticket identifiers fail closed without existence disclosure. Every Workspace ticket request re-resolves current P12 membership and requester scope server-side.

## 5. Frozen attachment and ClamAV boundary

Attachment states are exactly `quarantined`, `scanning`, `clean`, `infected`, `scan-error`, `rejected`.

Attachment bytes enter quarantine before download. Only `clean` may be released. Infected, unavailable, timeout, stale or indeterminate scanner results fail closed.

P14 reuses the inherited P09 mandatory ClamAV boundary and MUST NOT introduce an alternate permissive scanner path. Attachment evidence records safe hash/size/state/correlation metadata, not file content or scanner secrets.

## 6. Frozen Turnstile and anti-replay boundary

Protected P14 submissions are `POST /api/public/contact` and `POST /api/support/tickets`.

Turnstile is verified server-side before durable protected submission. Missing, invalid, expired or replayed tokens fail closed and create no contact/ticket/mail/entitlement mutation.

Raw Turnstile token, secret and provider response are excluded from ordinary logs/evidence. CI may use a deterministic server-side verifier adapter; a production test bypass is prohibited.

Rate limiting and request idempotency remain server-authoritative and cannot be bypassed by UI state or repeated client requests.

## 7. Frozen mail authority

Required native service target is `SVC-MAIL-WORKER services/platformapi/cmd/mailworker`.

Mail states are exactly `queued`, `sending`, `sent`, `retrying`, `failed`.

Mail templates are versioned and use key-specific variable allowlists. Unknown/unsafe variables fail before enqueue/send. Rendered content excludes secrets, tokens, Turnstile data, SMTP credentials, provider evidence and internal notes unless a dedicated internal-only template explicitly owns them.

Mail job claim/send completion is idempotent; concurrent workers cannot send one logical job twice. Transient failure uses bounded backoff/retry and terminal failure is durable/auditable.

CI must prove SMTP protocol delivery against a deterministic local sink. Live production SMTP credentials and external-recipient delivery are not required.

Mail delivery success is communication evidence only and never authorization, entitlement or payment settlement authority.

## 8. Frozen notification ownership

P12 remains owner of notification store/read-state/API/UI/deep-link authorization. P14 adds internal-only `support` producer events:

`ticket_created`, `ticket_reply_received`, `ticket_reply_sent`, `ticket_closed`, `mail_delivery_failed`.

Producer events use P12 recipient/dedupe semantics, safe deep links and redacted title/summary. P14 must not add a public arbitrary notification emit endpoint.

## 9. Frozen browser/state contract

`WEB-CONTACT`: `input`, `submitting`, `success-persistent`, `validation-error`, `Turnstile-error`, `rate-limited`.

`APP-SUPPORT`: `loading`, `empty`, `open`, `awaiting-user`, `awaiting-support`, `closed`, `error`.

`APP-SUPPORT-NEW`: `input`, `attachment`, `Turnstile-required`, `submitting`, `success`, `rate-limited`, `error`.

`APP-SUPPORT-THREAD`: `loading`, `open`, `replying`, `awaiting`, `closed`, `forbidden`, `attachment-blocked`, `error`.

`ADMIN-TICKETS`: `loading`, `empty`, `open`, `awaiting`, `closed`, `replying`, `attachment-blocked`, `error`.

`ADMIN-MAIL`: `loading`, `empty`, `queued`, `sending`, `sent`, `failed`, `retrying`, `partial`, `error`.

Browser evidence must cover canonical desktop/tablet/mobile viewports, 320 CSS px reflow, keyboard/focus/name-role-value, reduced motion, non-color-only persistent state and offline/partial recovery where applicable. Workspace/Admin private surfaces are noindex/no-store.

## 10. Evidence and case contract

Required P14 case range: **P14-T001..P14-T025**.

The `P14-Txxx` identifiers are frozen by this P14 contract revision. The Master Plan supplies requirements—not these IDs.

Evidence root: `artifacts/v10/P14/`.

Specialized evidence must include real MySQL ticket/message/mail/audit state, real inherited ClamAV outcomes, native `platformapi`, native `mailworker`, local SMTP protocol evidence, Turnstile/rate/idempotency outcomes, notification producer evidence and browser captures.

P14-T024 is exact-head evidence coherence. P14-T025 is accountable signed closure.

## 11. Pending implementation review

Pending: P14-T001..P14-T025, exact implementation SHA, ticket/mail schema and migrations, support/admin/public-contact APIs, native mailworker, attachment/ClamAV integration, Turnstile/rate/idempotency, P06 request linkage, P12 support notification producers, browser evidence, affected exact-head matrix, P0/P1 ledger and unresolved `DECISION REQUIRED` count.

No P14 PASS or Exit claim is made in this state.

## 12. Signed-revision rule

When evidence is complete this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record:

Pre-sign exact implementation SHA: `<40-hex SHA>`

Accountable reviewer identity: **GPT-5.6 Sol — P14 Technical Review**

Review date: **YYYY-MM-DD**

It must also record P14-T001..P14-T025 PASS evidence, P0=0, P1=0, unresolved `DECISION REQUIRED`=0, truthful P14 capability/gate disposition, P06/P09/P12/P13/P15/P17/P19 ownership boundaries and the same-revision CI/closure rerun requirement.

If signing changes this file and therefore changes HEAD, the signed revision itself must rerun and pass the complete affected exact-head matrix before P14 can be marked complete or merged.

## 13. Accountable signed review disposition

The frozen sections above are retained verbatim as the P14 contract record. The top-level signed status and this section record the accountable disposition supported by the pre-sign exact-head evidence; they do not bypass the signed-revision rerun rule.

### Exact-head evidence disposition

- P14-T001..P14-T021: PASS — real MySQL 8.x/Redis/native `platformapi` and `mailworker`, local SMTP protocol sink, inherited real P09 ClamAV, Turnstile/rate/idempotency, requester/Admin support, mail, P06 request-linkage, P12 notification and audit authority.
- P14-T022: PASS — real Workspace Support browser states, authorization, accessibility, responsive/reflow and recovery evidence.
- P14-T023: PASS — real Admin Tickets/Mail and public Contact browser states, permission boundaries, accessibility, responsive/reflow and failure/recovery evidence.
- P14-T024: PASS — exact-head evidence coherence, same-head producer binding, mixed-head rejection, secret/PII-safe evidence and inspectable runtime/browser/mail/ClamAV authority.
- P14-T025: PASS — pre-sign accountable closure with 24/24 input evidence files and 43/43 affected exact-head workflows.

Pre-sign closure run: `32760609741`

Pre-sign closure artifact: `9532810647`

Pre-sign closure artifact digest: `sha256:891f3c3604cfd42ff7e829d9bf8592c8f5cd8a4b8db79d6990c4656fdc19dea6`

The pre-sign closure is `phase=pre-sign` and `merge_authoritative=false`. P13 signed predecessor authority and inherited P12/P06/P09 functional authorities were live-bound and archive-digest verified.

### Defect and decision ledger

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0

### Accountable approvals

- Backend Lead: APPROVED
- Frontend Lead: APPROVED
- QA Lead: APPROVED
- Accessibility Reviewer: APPROVED
- Security Reviewer: APPROVED
- Product/API Reviewer: APPROVED

### Capability, gate and ownership disposition

- `CAP-TICKETS` — P14-owned G3/G6/G10 subset: APPROVED.
- `CAP-MAIL` — P14-owned G3/G6/G10 subset: APPROVED.
- `CAP-TURNSTILE` — P14-owned contact/ticket-request G6/G10/G13 subset: APPROVED; P15 identity and P17 administrator lifecycle remain later-owned.
- `CAP-DOMAIN-ENTITLEMENT` — P14 request-topic/linkage contribution: APPROVED; P06 request/approval/ownership/DNS/HTTPS/risk and P13 billing entitlement remain independent inherited conjunctive authorities. Ticket existence, reply, closure or mail delivery never grants entitlement.
- `CAP-NOTIFICATIONS` — P14 support/mail producer contribution: APPROVED; P12 remains owner of notification core and recipient/deep-link authorization.
- P09 remains owner of mandatory ClamAV scanning boundary; P14 closes only its attachment integration with that inherited authority.
- P12 Workspace membership/RBAC and notification core remain inherited.
- P13 billing/payment/entitlement authority remains signed predecessor authority and is not reinterpreted by P14.
- P15 identity/session/OAuth lifecycle remains later-owned.
- P16 trust/abuse lifecycle remains later-owned.
- P17 administrator permission lifecycle, including lifecycle ownership of `tickets.manage`, `mail.manage` and domain approval permissions, remains later-owned.
- P19 final Website/Technical SEO and public-site composition remain later-owned.
- Release-wide gates and later P20/P21/P22 closure are not closed by this P14 review.

### Same-revision requirement

The signed revision itself must rerun and pass P14-T001..P14-T025 and the complete affected exact-head P00-P14 matrix. Until that signed-revision closure reports `phase=signed`, `merge_authoritative=true`, and defects 0/0/0 with the same P13/P12/P06/P09 authority bindings, this review is not merge authority.
