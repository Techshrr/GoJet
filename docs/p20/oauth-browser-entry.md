# T025 browser OAuth entry repair

The customer login/register links already targeted
`GET /api/public/auth/{provider}/start`, but neither the platform API's root
router nor its authentication router registered that endpoint. Configuration
persistence and callback-adapter tests could pass while ordinary sign-in failed
before reaching the provider.

The route now issues the existing durable OAuth state and encrypted PKCE verifier,
then sends a 302 to the configured authorization URL with the S256 challenge.
Only login/register intents are accepted; account binding still uses the existing
authenticated, CSRF-protected account mutation.

Each provider gets a Secure, HttpOnly, SameSite=Lax, host-only browser-state
cookie. Production callback processing requires the matching browser state before
exchanging a provider code. The account-binding entry issues the same binding.
Successful handoff creation clears the cookie. The explicit historical test-auth
adapter keeps its existing fixtures; production cannot use that bypass.

The isolated administrator persistence database also exercises the actual nested
HTTP routers for login and registration starts. It checks the 302, browser cookie,
persisted intent, encrypted verifier/S256 relationship, non-cacheable response,
absence of secret/verifier output, and disabled-provider rejection. Separate
unit tests check invalid intent and missing/mismatched/cross-provider cookies.

Evidence: `artifacts/v10/P20/runtime/t025/oauth-browser-start.jsonl`.
This proves entry routing and browser-state binding, not an external-provider
login. Rainbow's server bootstrap protocol is described in `rainbow-login.md`;
X/LinkedIn direct-provider expansion and the complete T025 OAuth/Turnstile matrix remain open.
No frozen oracle, historical migration or formal T025 completion claim changes.

## Binding completion and callback UI

The public callback previously always called `CreateBrowserHandoff`, which
explicitly rejects `bind`. A read-only state dispatch now routes binding to
`POST /api/me/connected-accounts/{provider}/complete`. That mutation requires
Origin, one-time CSRF, a matching browser-state cookie, and the exact initiating
session before exchanging the code. It calls the existing callback and binding
authorities and never creates a login handoff or replaces the current session.

The callback page fetches a fresh CSRF token only for binding, finishes the
mutation and returns to connected accounts. Login returns to `/app`; new
identities retain the registration continuation. A component-local promise keeps
React effect replay from consuming callback/handoff twice, and callback query
credentials are removed from browser history immediately.

`TestOAuthBrowserBindingLifecycle` exercises real database state, real sessions,
Redis CSRF replay, binding/unbinding and audit with an explicitly injected
provider fixture. `scripts/p20/oauth-callback-browser.mjs` exercises the built
callback page with intercepted API responses for login, registration and binding.
These tests cover local lifecycle and UI protocol behavior; neither claims a
live provider exchange. The frozen P15 browser suite remains unchanged.
