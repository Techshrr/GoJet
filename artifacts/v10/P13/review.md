# P13 Billing, Payments and Entitlements — Accountable Technical Review

Node: `P13`  
Issue: #33  
Base integration commit: `7f39da389052b08f145e69dac2a715b9d303294d`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

Pre-sign exact implementation SHA: `bf8c0eb186d9d3e230ada03c56d7114e830f63fb`  
Accountable reviewer identity: **GPT-5.6 Sol — P13 Technical Review**  
Review date: **2026-08-24**

## 1. Review boundary

This signed review covers the frozen P13 Billing, Payments and Entitlements scope only. It approves the P13-owned billing/payment/callback/entitlement implementation and its shared P06/P13 domain-entitlement contribution against the frozen P13 contract.

It does **not** close P15 production identity lifecycle, P17 Admin permission lifecycle, P19 final Website/SEO composition, inherited P06 domain safety ownership, inherited P12 notification-core ownership, or release-wide Gates. Production Docker/Compose/Node runtime remains outside the approved production authority.

Frontend price cards, navigation visibility, cached client state, browser-return success pages, generic `features` JSON, unauthenticated provider payloads, screenshot-only proof, or manually edited entitlement rows are not billing, payment-settlement, RBAC, or entitlement authority.

## 2. Capability and gate disposition

- `CAP-BILLING` — P13-owned G3/G10 subset: APPROVED.
- `CAP-PAYMENTS` — P13-owned G3/G6/G10 subset: APPROVED.
- `CAP-PAYMENT-CALLBACKS` — P13-owned G3/G6/G10 subset: APPROVED.
- `CAP-DOMAIN-ENTITLEMENT` — shared P06/P13 G3/G6 subset: APPROVED for P13 billing/quota contribution; inherited P06 request/approval/ownership/DNS/HTTPS/risk authority remains conjunctive and independently required.

These are P13-scoped gate dispositions only and do not mark release-wide G3/G6/G10 complete.

## 3. Predecessor and shared authority

Authoritative predecessor remains P12 signed source `9d49d5ebf0e697ae9cd6537c432c27a15edc60bd`, integration commit `7f39da389052b08f145e69dac2a715b9d303294d`, closure run `32663159008`, artifact `9499336765`, digest `sha256:72ed65c48303654b589edce23e9118ecc963940a7400e27a0f174d7e8ea07c9a`, `phase=signed`, `merge_authoritative=true`, defects 0/0/0.

Inherited P06 domain functional authority remains signed source `4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23`, integration commit `3aa80b566d144963130b8f61fa63a4ee677ebc99`, closure run `32519298309`, artifact `9460016077`, digest `sha256:21e2fe5898a047e166aac520870070e8072f00885a3c89aaf86736f6ac22a2c8`, with defects 0/0/0.

P13 consumes P12 Workspace membership and notification core without redefining them. P13 adds billing producer events only. P13 consumes current principal and `billing.manage` permission boundaries without claiming P15/P17 lifecycle completion.

## 4. Route, API and provider disposition

Reviewed P13-related routes are:

- `APP-BILLING /app/billing`
- `WEB-PRICING /pricing`
- `ADMIN-PLANS /admin/commerce/plans`
- `ADMIN-PAYMENTS /admin/commerce/payments[/{paymentId}]`
- `ADMIN-FX /admin/commerce/fx`

Only `GET /api/public/plans` is IA-exact. The Workspace billing/order/invoice/payment families, Admin plan/payment/invoice/FX families, and `POST /api/payments/callbacks/{provider}` remain P13 implementation authority. No invented `/app/billing/plans`, `/app/billing/usage`, or `/app/billing/invoices` aliases are approved. P19 retains final public Website/SEO ownership.

Frozen provider identifiers are exactly `alipay`, `wechat`, `epay`, `paypal`, `stripe`, and `crypto`. Provider-specific callback authentication occurs before durable mutation; duplicate/reordered callbacks are idempotent; browser success/return state is not settlement authority; ordinary APIs/logs/evidence exclude raw callback payloads, signatures, credentials, full payer identity, and provider secrets.

## 5. Monetary and entitlement disposition

All authoritative money uses integer minor units plus ISO currency. Plan, subscription, order, invoice, transaction, callback-event, and FX lifecycle transitions were reviewed through real MySQL/native API evidence.

The durable server-side entitlement resolver is authoritative. Hard suspension/revoke wins; durable manual/inherited grants retain provenance; billing grants respect term/grace boundaries; baseline/free is last; numeric grants combine by the frozen non-additive maximum rule.

Billing refund/expiry removes only billing-source contribution. Existing over-quota resources are preserved on normal downgrade while new violating mutations are denied. Custom-domain use requires both P13 effective quota/entitlement and inherited P06 approval/ownership/DNS/TLS/risk authority; payment cannot bypass P06 safety.

## 6. RBAC, audit and notification disposition

Workspace billing reads and mutations re-resolve current P12 membership and tenant scope server-side. Owner mutation authority, safe admin read boundaries, member/viewer denial, cross-Workspace fail-closed behavior, and Admin `billing.manage` enforcement are approved for the P13 scope.

Billing/admin/callback state changes are auditable without raw provider secrets or unnecessary PII. P13 billing notification producers use the inherited P12 notification core with dedupe, allowlisted/re-authorized links, and redacted summaries; no arbitrary public notification emit authority is introduced.

## 7. Browser, accessibility and resilience disposition

Real Workspace/Admin browser evidence approves APP-BILLING, ADMIN-PLANS, ADMIN-PAYMENTS, and ADMIN-FX state coverage; canonical desktop/tablet/mobile plus 320 CSS px reflow; keyboard/focus semantics; reduced motion; non-color-only status; authenticated authorization; noindex/no-store; partial/provider-failure/offline states; and recovery behavior.

The safe public-plan substrate is approved only as P13 data authority and does not claim P19 final pricing-page composition.

## 8. Exact-head evidence disposition

Pre-sign exact implementation SHA `bf8c0eb186d9d3e230ada03c56d7114e830f63fb` passed **P13-T001..P13-T027**:

- P13-T001..P13-T020: real MySQL/Redis/native platformapi billing/payment/entitlement evidence.
- P13-T021..P13-T025: real Workspace/Admin browser evidence.
- P13-T026: exact-head evidence coherence with same-head producer binding, inspectable runtime/browser evidence, mixed-head rejection, and clean runtime error files.
- P13-T027: PASS in pre-sign accountable closure with 26 input evidence files and the required 39-workflow exact-head affected matrix.
- P12 signed predecessor authority and P06 signed functional authority were live-bound and archive-digest verified.

Pre-sign closure run: `32707945039`  
Pre-sign closure artifact: `9513174699`  
Pre-sign closure artifact digest: `sha256:78d65e54a815e212b1a93be6545e1453c563e7665575ad904380d3c662811fc6`

The pre-sign closure is intentionally `phase=pre-sign` and `merge_authoritative=false`. This signed review records accountable approval but does not bypass the same-revision closure requirement.

## 9. Defect and decision ledger

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0

No unresolved P0, P1, or decision-required item remains inside the frozen P13 ownership boundary based on the pre-sign exact-head evidence.

## 10. Accountable approvals

- Backend Lead: APPROVED
- Frontend Lead: APPROVED
- QA Lead: APPROVED
- Accessibility Reviewer: APPROVED
- Security Reviewer: APPROVED
- Product/API Reviewer: APPROVED

## 11. Ownership retained by later/inherited nodes

- P06 retains domain request/approval/ownership/DNS/HTTPS/risk and redirect safety authority.
- P12 retains Workspace membership and notification-core store/read-state/API/UI/deep-link authority.
- P15 retains production identity/session/OAuth lifecycle.
- P17 retains Admin permission lifecycle, including the lifecycle that governs `billing.manage`.
- P19 retains final public Website/SEO and pricing-page composition.
- Release-wide gates and later P20/P22 closure remain outside this P13 review.

## 12. Signed-revision rule

The signed revision itself must rerun and pass P13-T001..P13-T027 and the complete affected exact-head P00-P13 matrix. Until that signed-revision closure completes with `phase=signed`, `merge_authoritative=true`, defects 0/0/0, and the same predecessor bindings, this review must not be used as merge authority.

If any implementation, workflow, validator, contract, or review file changes after this signature, the exact-head evidence becomes revision-specific history and another complete affected rerun is required.

After the signed revision passes, P13 may be marked complete for its frozen scope only; P06/P12 inherited ownership, P15/P17/P19 later ownership, and broader release gates remain unchanged.
