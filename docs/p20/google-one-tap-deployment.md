# Google One Tap deployment boundary

The default Website build is a static, API-independent P19 surface. It must not
make background authentication requests. Auth pages discover One Tap through
`GET /api/public/auth/providers`; only an explicit `google_one_tap_enabled: true`
response permits creating a browser challenge and loading Google Identity Services.
Missing capability fields mean disabled, including older servers and static previews.

For an integrated Linux deployment that also wants the prompt on public Website
pages, build the site with `VITE_GOJET_WEBSITE_AUTH_ENABLED=1` and route `/api` on
that same origin to platformapi. This is a deployment capability, not a replacement
for the administrator's feature switch. Google OAuth must be configured/enabled
and the audited `google_one_tap` setting must be enabled. Disabling the setting
also blocks challenge completion on the server. No Google secret belongs in Vite
variables. The static-only Website build remains usable without an API.

Configure the exact HTTPS origin in Google's authorized JavaScript origins and
in `GOJET_AUTH_ALLOWED_ORIGIN`. The prompt uses a Secure, HttpOnly, host-only state
cookie; do not claim a plain HTTP preview proves the live browser flow. Production
Google login, replay/browser binding, and enabled/disabled browser evidence are
still required before T025 closure.
