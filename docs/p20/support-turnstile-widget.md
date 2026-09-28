# Support challenge widget remediation

Contact previously submitted an empty token outside the explicit P14 fixture.
New-ticket UI consumed only the fixture token. Both now use a shared component
hook that loads the real Cloudflare widget with VITE_GOJET_TURNSTILE_SITE_KEY,
keeps tokens only in memory, clears them before submission or on expiry/error,
and resets after each request. Missing configuration or a failed script blocks
submission and offers a retry. Site keys must match the GOJET_TURNSTILE_SECRET
used by the inherited P14 server verifier. The P14 verifier remains mandatory;
no server-side bypass or secret is added to the frontend.

Fixture tokens and test identity headers are limited to explicit test authority.
The shared component is not rendered in fixture mode. Contact browser support
coverage uses the built production widget path with explicit widget and HTTP
fixtures, tests denial/no-success, token consumption, reset and expiry. It does
not claim a live Cloudflare interaction.

Remaining production blocker discovered during this change: the workspace
readWorkspaceRuntime function still returns null outside test mode. Therefore
adding the ticket widget does not itself make production workspace support
usable. Session-backed workspace selection and unsafe-request CSRF wiring must
be completed and verified separately before claiming full T025 coverage. This
change makes no T025 closure claim and does not advance T026.
