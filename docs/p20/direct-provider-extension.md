# Optional direct X and LinkedIn providers

Production discovery and P17 administrator configuration now expose eight
providers. X and LinkedIn are disabled by default. Migration 000031 appends
provider enum values in all four OAuth tables and seeds disabled configuration;
no historical migration or frozen oracle is modified. Apply migrations before
running the new API. Once new-provider records exist, disable the providers to
roll back exposure; removing enum values requires the release backup/restore
boundary and must not silently discard records.

The original `Providers` and `ListProviderConfigs` retain a six-provider
compatibility view for historical P15 fixtures. Production uses
`RuntimeProviders` and `ListRuntimeProviderConfigs`. Historical six-provider
proofs do not establish production eight-provider correctness. The isolated P20
administrator probe and production HTTP tests independently check the expanded
inventory, disabled defaults, audited configuration, encrypted PKCE, browser
cookies, durable state/callback/handoff and replay rejection. The built workspace
navigation fixture covers X and LinkedIn as well as Google and Rainbow.

## Configuration

Use the administrator OAuth page with recent MFA and settings permission. Save
credentials there, not in issue comments or chat. Both integrations require an
exact application-registered HTTPS callback matching the configured redirect URI,
for example `https://<site-host>/oauth/x/callback` or
`https://<site-host>/oauth/linkedin/callback`. Enable only after configuring the
corresponding provider application. Secret retention, version checks, CSRF,
idempotency and audit use the existing administrator authority.

X uses a confidential Web App, fixed X OAuth 2 endpoints, Basic client
authentication and S256 PKCE. Scopes are `tweet.read users.read`. Profile badges
and email-like data never establish email ownership. X direct login is separate
from the Rainbow `twitter` channel.

LinkedIn requires the Sign In with LinkedIn using OpenID Connect product. Scopes
are `openid profile`, optionally `email`. Identity comes from authenticated
userinfo, never an unverified ID-token payload; absent or unverified email uses
the existing registration verification path. The shared local S256 state/verifier
machinery remains in place; upstream PKCE enforcement is not established by a
fixture response.

Only Google has One Tap. X, Facebook, LinkedIn and Rainbow use ordinary configured
provider buttons and redirects.

## Evidence limits

`oauth-admin-governance.json`, `oauth-browser-start.jsonl`,
`optional-provider-state.jsonl` and `oauth-connect-browser.json` are support
evidence. Tests use real migrated MySQL and explicit upstream/browser protocol
fixtures. They are not live-provider login evidence and do not close T025 or
D020. The complete OAuth/Turnstile formal matrix remains required before T026.

Protocol references:
- https://docs.x.com/fundamentals/authentication/oauth-2-0/authorization-code
- https://learn.microsoft.com/en-us/linkedin/consumer/integrations/self-serve/sign-in-with-linkedin-v2
- https://learn.microsoft.com/en-us/linkedin/shared/authentication/authorization-code-flow
