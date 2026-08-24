# P13 Billing, Payments and Entitlements — Accountable Technical Review

Node: `P13`  
Issue: #33  
Base integration commit: `7f39da389052b08f145e69dac2a715b9d303294d`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**

## 1. Review boundary

This file freezes the P13 review contract before implementation. It is not a PASS record and does not close P17 Admin permission lifecycle, P19 Website/SEO, P15 production identity lifecycle, or release-wide Gates.

Frontend price cards, navigation visibility, cached client state, browser-return success pages, generic `features` JSON, unauthenticated provider payloads, screenshot-only proof, or manually edited entitlement rows cannot substitute for authoritative P13 evidence.

## 2. Frozen capability and predecessor boundary

- `CAP-BILLING` — REQUIRED — owner P13 — G3/G10.
- `CAP-PAYMENTS` — REQUIRED — owner P13 — G3/G6/G10.
- `CAP-PAYMENT-CALLBACKS` — REQUIRED — owner P13 — G3/G6/G10.
- `CAP-DOMAIN-ENTITLEMENT` — REQUIRED — owners P06/P13 — G3/G6.
- P13 consumes P12 Workspace membership/notification-core authority but does not redefine it.
- P13 extends effective entitlement/quota state without duplicating P06 domain request/approval/ownership/DNS/HTTPS/risk authority.
- P13 consumes current principal and `billing.manage` permission boundaries without claiming P15/P17 lifecycle completion.
- Production Docker/Compose/Node runtime remains prohibited.

Authoritative predecessor is P12 signed source `9d49d5ebf0e697ae9cd6537c432c27a15edc60bd`, integration commit `7f39da389052b08f145e69dac2a715b9d303294d`, closure run `32663159008`, artifact `9499336765`, digest `sha256:72ed65c48303654b589edce23e9118ecc963940a7400e27a0f174d7e8ea07c9a`, `phase=signed`, `merge_authoritative=true`, defects P0=0/P1=0/`DECISION REQUIRED`=0.

Inherited P06 domain authority remains source `4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23`, integration `3aa80b566d144963130b8f61fa63a4ee677ebc99`, closure run `32519298309`, artifact `9460016077`, digest `sha256:21e2fe5898a047e166aac520870070e8072f00885a3c89aaf86736f6ac22a2c8`.

## 3. Frozen route and authority layers

IA-authoritative P13-related routes:

```text
APP-BILLING     /app/billing
WEB-PRICING     /pricing
ADMIN-PLANS     /admin/commerce/plans
ADMIN-PAYMENTS  /admin/commerce/payments[/{paymentId}]
ADMIN-FX        /admin/commerce/fx
```

IA-exact API dependency:

```text
GET /api/public/plans
```

P13 implementation authority freezes the remaining billing namespaces and `POST /api/payments/callbacks/{provider}`; these are **not IA-exact**. `/api/admin/plans*`, payment/invoice families and FX families remain P13 implementation API authority.

There are no invented `/app/billing/plans`, `/app/billing/usage` or `/app/billing/invoices` page aliases. Provider callback routes are payment-subsystem routes and must not be conflated with generic user-configurable `APP-WEBHOOKS`.

P13 may supply safe public plan data to `WEB-PRICING`, but P19 retains final Website/SEO ownership.

## 4. Frozen payment-provider and callback authority

Provider identifiers are exactly:

`alipay`, `wechat`, `epay`, `paypal`, `stripe`, `crypto`.

The callback implementation route is `POST /api/payments/callbacks/{provider}`, explicitly P13 implementation authority rather than IA-exact authority.

- Unknown provider fails closed.
- Provider-specific signature/credential verification occurs before durable mutation.
- Authenticated callbacks normalize to durable provider transaction/event identity.
- Duplicate/reordered callbacks are idempotent and cannot double-apply money or entitlement.
- Browser return/success state is never settlement authority.
- Raw callback body, signature, secret/token/credential, full payer identity and provider evidence are excluded from ordinary logs, API output and evidence artifacts.
- CI uses deterministic authenticated provider fixtures/adapters; no live production credential or real charge is required.

## 5. Frozen monetary and lifecycle authority

All money is integer minor units plus ISO currency; floating-point money is prohibited.

Plan lifecycle: `draft`, `active`, `archived`.

Subscription lifecycle: `pending`, `active`, `grace`, `overdue`, `canceled`, `expired`.

Order lifecycle: `pending`, `processing`, `paid`, `failed`, `canceled`, `refunded`.

Invoice lifecycle: `open`, `paid`, `void`, `refunded`.

Transaction lifecycle: `pending`, `paid`, `failed`, `refunded`.

Callback event state: `accepted`, `duplicate`, `invalid`, `ignored`, `processed`.

FX state: `current`, `stale`, `provider-error`, `override`.

Paid/refund/upgrade/downgrade transitions must be durable, idempotent, auditable and driven by authenticated server authority.

## 6. Frozen entitlement precedence

One durable server-side resolver is authoritative. Generic `features` JSON is display/supporting metadata only and **must not** be queried as entitlement authority.

Precedence:

1. hard security/Workspace/admin suspension or explicit durable revoke denies;
2. active durable manual/inherited grant with provenance;
3. active billing-plan grant with provenance and term/grace boundaries;
4. baseline/free default.

Active numeric grant limits use an explicit non-additive maximum unless a capability-specific frozen rule says otherwise. Billing refund/expiry removes only the billing-source contribution; unrelated manual/inherited grants survive.

Custom-domain use requires **both** effective P13 domain entitlement/quota **and** inherited P06 request/approval/ownership/DNS/HTTPS/risk authority. Payment cannot bypass P06 safety.

Downgrade/expiry may deny new over-limit mutations but does not silently destroy existing resources unless the owning capability contract explicitly requires cleanup.

## 7. Frozen RBAC and tenant authority

- Workspace owner: read/manage P13 Workspace billing.
- Workspace admin: read plan/usage/status summary only; no plan change, payable-order creation or sensitive provider detail.
- Member/viewer: no financial ledger or billing mutation; capability enforcement exposes only safe entitlement state/reason.
- Admin commerce routes require current principal with `billing.manage`; P13 consumes that permission but does not implement P17 permission lifecycle.
- Every Workspace financial request re-resolves P12 membership and tenant scope server-side.
- Client/test role headers never authorize billing.
- Cross-Workspace IDs fail closed without revealing foreign financial existence.

## 8. Frozen notification ownership

P12 remains owner of notification store/read-state/API/UI/deep-link authorization. P13 adds internal-only `billing` producer events such as payment success/failure, refund, upgrade, scheduled downgrade and entitlement expiry.

P13 must not add a public arbitrary notification emit endpoint, and producer content must remain deduped, allowlisted, reauthorized and secret/PII-redacted through the P12 core.

## 9. Frozen browser/state contract

`APP-BILLING`: `loading`, `active`, `payment-pending`, `payment-failed`, `overdue`, `canceled`, `provider-partial`, `error`.

`ADMIN-PLANS`: `loading`, `empty`, `draft`, `active`, `archived`, `validation-error`, `conflict`.

`ADMIN-PAYMENTS`: `loading`, `empty`, `pending`, `paid`, `failed`, `refunded`, `callback-invalid`, `partial`.

`ADMIN-FX`: `loading`, `current`, `stale`, `provider-error`, `override-confirm`, `validation-error`.

P13 browser evidence must cover canonical desktop/tablet/mobile viewports, 320 CSS px reflow, keyboard/focus semantics, reduced motion, no color-only financial status, authenticated authorization, noindex/no-store, partial/offline/provider failure and recovery.

## 10. Evidence and case contract

Required P13 case range: **P13-T001..P13-T027**.

The `P13-Txxx` identifiers are frozen by this P13 contract revision. The Master Plan supplies requirements—not these IDs.

Evidence root: `artifacts/v10/P13/`.

Specialized evidence must include real DB/API/entitlement/security/audit/browser/runtime evidence, provider callback redaction, exact-head producer bindings, coherence and accountable affected-regression closure.

P13-T026 is exact-head evidence coherence. P13-T027 is accountable signed closure.

## 11. Pending implementation review

Pending: P13-T001..P13-T027, exact implementation SHA, billing schema/migrations, provider adapters, callback verification/idempotency, authoritative entitlement resolver, P06 domain continuity, P12 billing notification producers, Workspace/Admin browser evidence, affected exact-head matrix, P0/P1 ledger and unresolved `DECISION REQUIRED` count.

No P13 PASS or Exit claim is made in this state.

## 12. Signed-revision rule

When evidence is complete this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record:

Pre-sign exact implementation SHA: `<40-hex SHA>`

Accountable reviewer identity: **GPT-5.6 Sol — P13 Technical Review**

Review date: **YYYY-MM-DD**

It must also record P13-T001..P13-T027 PASS evidence, P0=0, P1=0, unresolved `DECISION REQUIRED`=0, truthful P13 gate/capability disposition, P06/P12/P15/P17/P19 ownership boundaries and the same-revision CI/closure rerun requirement.

If signing changes this file and therefore changes HEAD, the signed revision itself must rerun and pass the complete affected exact-head matrix before P13 can be marked complete or merged.
