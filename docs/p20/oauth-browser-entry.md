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
login. Rainbow's server bootstrap protocol remains unsupported and fails closed;
X/LinkedIn expansion and the complete T025 OAuth/Turnstile matrix remain open.
No frozen oracle, historical migration or formal T025 completion claim changes.
