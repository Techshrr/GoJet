# P15 Authentication, OAuth and Account — Accountable Technical Review

Node: `P15`  
Issue: #37  
Base integration commit: `9258cb0f3f913b37b03aa8cf3c2938711314d3aa`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**

## 1. Review boundary

This file freezes the P15 review contract before implementation. It is not a PASS record, signature, completion claim or merge authority.

P15 owns customer authentication/account lifecycle and `CAP-OAUTH`. It owns only the authentication-facing contribution to shared `CAP-TURNSTILE`. P15 does not re-own P04 Auth shell, P12 invitation/Workspace membership, P14 mail core or contact/ticket Turnstile paths, P17 administrator/role/permission lifecycle, P19 final Website/Technical SEO, or release-wide P20-P22 gates.

## 2. Frozen predecessor authority

Immediate signed predecessor:

- P14 signed source: `f079c938dbe49d0f55b8b09995e72201cd0aab6e`
- P14 integration commit: `9258cb0f3f913b37b03aa8cf3c2938711314d3aa`
- P14 closure run: `32763705854`
- P14 closure artifact: `9533837642`
- P14 closure digest: `sha256:3f334718539e8fdd9cf5896fffdca9c00b8d0fc9a57b03d39795e97e6af853a8`
- P14 closure: `phase=signed`, `merge_authoritative=true`, affected matrix `43/43`, P0/P1/DECISION REQUIRED `0/0/0`.

Inherited P04 Auth-shell authority:

- signed source `694cc4d50c13fa76f3d35571287a146f4dc04025`
- integration `16cddfa89279d698f30607f4dec79f3ed2f55b59`
- signed-head P04 workflow run `32395638418`
- artifact `9416550011`
- digest `sha256:90a4c29844ced6ae934d769785085723c10840106417bbf0df2899e0f5b8fcdd`

P15 may extend Auth/account behavior only inside the specification authority; it may not reinterpret P04 shell completion or P14 mail/Turnstile evidence.

## 3. Frozen capability boundary

- `CAP-AUTH` — REQUIRED — owner P15 — G3/G5/G6/G10.
- `CAP-OAUTH` — REQUIRED — owner P15 — G3/G6/G10/G13.
- `CAP-TURNSTILE` — REQUIRED — owners P14/P15/P17 — P15 owns auth-facing protected scenarios only.
- `CAP-MAIL` remains P14-owned. Verification, recovery and login-code delivery must reuse P14 durable templates/queue/mailworker and must not introduce a second SMTP authority.
- P12 owns `AUTH-INVITE` and Workspace membership/RBAC. P15 may consume current membership/account context but does not redefine it.
- P17 owns administrator/role/permission lifecycle. P15 may consume `settings.manage` for `ADMIN-OAUTH`, but cannot create an alternate permission lifecycle.

## 4. Frozen route authority

P15-owned Auth routes:

```text
AUTH-LOGIN          /login
AUTH-REGISTER       /register
AUTH-VERIFY         /verify-email
AUTH-FORGOT         /forgot-password
AUTH-RESET          /reset-password?token={opaque}
AUTH-OAUTH-CALLBACK /oauth/{provider}/callback
AUTH-SOCIAL-REG     /social-registration?code={opaque}
```

`AUTH-INVITE /invite/{token}` remains P12-owned and is explicitly excluded from P15 completion evidence except as an inherited regression dependency.

P15 account settings authority is limited to the account-oriented concrete members of `APP-SETTINGS`:

```text
/app/settings/profile
/app/settings/security
/app/settings/sessions
/app/settings/connected-accounts
```

`ADMIN-OAUTH /admin/platform/oauth` is part of P15 `CAP-OAUTH` provider runtime/configuration evidence. P15 consumes server permission decisions but does not own P17 permission lifecycle.

IA-exact API dependencies are exactly the list frozen in `artifacts/v10/P15/test-plan.json`; additional profile/session/connected-account/Admin OAuth routes are P15 implementation authority and must not be described as IA-exact.

All Auth routes are noindex, outside sitemaps and no-store. Authenticated private account/Admin OAuth surfaces are no-store/noindex. No legacy alias is authorized.

## 5. Frozen authentication/session boundary

- Formal identity/session authority is server-side. Formal auth/session tokens MUST NOT be stored in localStorage.
- Successful authentication establishes/rotates a fresh server session; client identity assertions are not authorization.
- Cookie-authenticated unsafe mutations enforce CSRF and approved Origin policy before durable state change.
- Verification, reset, login-email and social handoff grants are opaque, expiry-bound and one-time. Reuse/replay fails closed.
- Forgot-password behavior is response-neutral across account existence.
- Session list/revoke is owner-scoped; forged/foreign session identifiers fail closed. A revoked session cannot remain authoritative because the UI is stale.
- Passwords, raw codes, reset/verification tokens and session material are excluded from ordinary logs, audit and CI evidence.

## 6. Frozen OAuth boundary

Provider identifiers are exactly:

`google`, `facebook`, `github`, `qq`, `wechat`, `rainbow`.

- OAuth state is unpredictable, server-bound, expiry-bound and one-time.
- PKCE is required where applicable to the implemented provider flow.
- Redirect URI is selected from reviewed server configuration; arbitrary client redirect authority is prohibited.
- Raw provider callback parameters, authorization codes, access/refresh tokens and provider secrets are excluded from logs/evidence.
- Browser callback completion uses a short-lived opaque one-time handoff. Provider credentials are never browser-storage authority.
- Social registration cannot silently bind an existing account from an unverified provider/email claim.
- Connected-account bind/unbind requires current authenticated account authority and cannot steal an external identity already bound to another account.
- Provider secrets are masked in Admin UI and absent from frontend bundles/ordinary evidence.

## 7. Frozen Turnstile/rate boundary

P15 owns only auth-facing Turnstile scenarios. Server-side verification and anti-replay are mandatory before protected durable auth mutation. Missing/invalid/expired/replayed/provider-failure states fail closed. CI may use a deterministic server-side adapter; production bypass is prohibited.

Rate limiting is server-authoritative and account-enumeration safe. It cannot be bypassed by alternate UI routes, repeated clients or stale frontend state.

P14 contact/ticket Turnstile ownership remains inherited and unchanged.

## 8. Frozen mail boundary

Verification, recovery and login-code messages use P14 `CAP-MAIL` authority and native `services/platformapi/cmd/mailworker`.

P15 may add auth-owned allowlisted template keys/variables, but it must preserve P14 queue durability, retry/backoff, idempotency and secret redaction. Mail delivery success is communication evidence only and never proves authentication, token verification, password reset or session authority.

## 9. Frozen UI/accessibility boundary

P15 must use the Design System as the sole exact visual/interaction authority. It must not duplicate token values into the P15 contract.

Auth forms preserve one clear primary submit, consistent client/server error model, focus transfer to error summary/first invalid control, correct Auth control height, visible labels, non-color-only persistent failures and keyboard/focus semantics. OTP/code input supports whole-code paste and correct autocomplete/password-manager semantics where applicable.

Password/OAuth/API secret material must not use unsafe browser autosave storage. Browser evidence must cover canonical desktop/tablet/mobile viewports plus 320 CSS px reflow where applicable, reduced motion and direct-route behavior.

## 10. Frozen evidence and case contract

Required case range: **P15-T001..P15-T029**.

- T001..T023 and T027: real integration/security/OAuth/mail/audit authority.
- T024: Auth route browser authority.
- T025: Workspace account-settings browser authority.
- T026: Admin OAuth browser authority.
- T028: exact-head evidence coherence.
- T029: accountable signed exact-head closure.

Evidence root: `artifacts/v10/P15/`.

The Master Plan explicitly requires HTTP headers, API/browser/security logs. Evidence must bind the exact implementation SHA, use real MySQL/Redis/native Go boundaries where the case requires them, preserve P14 mail authority, and exclude production credentials/secrets.

## 11. Pending implementation review

Pending: P15-T001..P15-T029, exact implementation SHA, durable identity/session/OAuth state, auth/account APIs, provider adapters/configuration, verification/recovery mail integration, auth Turnstile/rate/CSRF/Origin controls, Auth/Workspace/Admin browser evidence, exact-head coherence, affected workflow matrix, P0/P1 ledger and unresolved `DECISION REQUIRED` count.

No P15 PASS, Gate closure, Ready-for-review or merge authority is claimed in this state.

## 12. Signed-revision rule

When all required evidence is complete, this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record the reviewed pre-sign exact implementation SHA, accountable reviewer identity/date, P15-T001..P15-T029 evidence disposition, P0=0, P1=0, `DECISION REQUIRED`=0, truthful Gate/ownership disposition, and same-revision closure requirement.

If signing this file changes HEAD, the signed revision itself must rerun and pass the complete required affected exact-head matrix before P15 can be marked complete or merged.
