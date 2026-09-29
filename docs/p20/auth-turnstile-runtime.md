# Authentication Turnstile runtime wiring

The P15 guard existed but production authentication HTTP routes never called it.
The API now consumes the existing P17 `admin_turnstile_config` singleton before
protected account or mail mutations. No row and explicitly disabled configuration
retain P17's existing disabled default. Once enabled, incomplete configuration,
provider-error state, missing/wrong encryption keys, database errors, missing or
oversized tokens, provider rejection/outage and Redis replay/outage all deny the
request before the authentication handler. There is no testAuth bypass in this
gate. The existing rate limiter runs before external challenge verification.

Protected POST routes: password login, registration, password recovery/reset,
login email code issue/consume, verification issue/resend/consume (including their
aliases), and social registration completion. OAuth callbacks, handoff exchange
and connected-account mutations retain their separate state/PKCE/session/CSRF
contracts; this patch does not substitute Turnstile for those authorities.

Configuration is read on each protected request. Secrets are decrypted with the
existing administrator cipher and purpose and sent only through the inherited
P14 HTTP verifier. Redis stores the inherited digest-only replay key, shared with
other protected submissions. Responses expose no provider body, token or secret;
all challenge errors are account-neutral and private/no-store.

Public provider discovery exposes only required/available flags and the public
site key. Authentication pages render the real Cloudflare widget when required,
carry its token in `X-Turnstile-Token`, clear it before submission, and reset the
widget after each attempt. Expiry/error callbacks clear the token. Tokens stay
in component memory. OAuth callback navigation is not made dependent on the
widget. The administrator switch should only be enabled with a valid site key,
secret and healthy provider state.

Verification: `TestAuthChallengeMutationOrdering` checks all protected route
aliases and denial classes. `TestAuthChallengeAdministratorConfigAndRedis` uses
real migrated MySQL, encrypted persisted configuration and Redis with explicit
HTTP protocol fixtures. `auth-turnstile-browser.mjs` exercises the built page
with explicit widget/API fixtures. Artifacts: `auth-turnstile-state.jsonl` and
`auth-turnstile-browser.json`. None is represented as a live Cloudflare challenge
or full T025 formal completion. Contact and workspace ticket widget integration,
full owned-surface parity and live configured OAuth proof remain separate gaps.

Reference: https://developers.cloudflare.com/turnstile/get-started/server-side-validation/

## Official external verifier registration probe

The isolated CI database now opts into
`TestAuthChallengeAdministratorConfigAndRedis/OfficialCloudflareRegistration`.
It encrypts the official Cloudflare test credential into the existing
administrator configuration, constructs the production authentication handler
with testAuth=false, and uses the inherited HTTP verifier without an injected
client. A missing challenge must leave users/grants/audit unchanged; one official
dummy token must authorize a real registration transaction; replay must leave
all three counts at one. It uses the same real Redis digest replay store.

This proves actual server-to-Cloudflare test validation and durable registration,
not a human challenge, a live OAuth provider login or full T025 closure.
Only the isolated probe opts in; production credentials are never requested,
printed or changed. Evidence remains the exact-head auth-turnstile-state.jsonl.
