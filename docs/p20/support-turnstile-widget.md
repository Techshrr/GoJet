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

The inherited workspace runtime was test-only. Support now resolves `/api/me`
and `/api/workspaces` using the real session, limits selection to returned active
workspaces, and requires a choice when multiple are available. A requested
workspace ID is accepted only when returned by the authenticated list. No local
storage grants workspace authority. Server membership checks remain decisive.
Each unsafe Support request fetches fresh CSRF from `/api/me` and verifies the
same user before sending it. Test identity headers are fixture-only. The shared
Support client now preserves per-request headers, including Idempotency-Key.

`support-session-browser.mjs` runs against a separate build with test auth off,
using explicit API/widget fixtures. It verifies workspace choice, fresh CSRF,
idempotency, absence of test identity headers and challenge denial lifecycle.
This complements inherited real-session server tests but is not itself a live
end-to-end browser/session proof. Full T025 coverage remains open.
