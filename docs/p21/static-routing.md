# Compiled static application routing

The native payload builder emits `deploy/nginx/static-apps.conf` from the actual
four compiled apps and the inherited P19 Website/P18 Docs Nginx fragments. Include
it inside the final TLS server. The fixed `/opt/gojet/current/static` prefix matches
the release layout used by the shipped units. No Node process serves these apps.

Website and Docs keep their existing route/status rules and separate roots.
Workspace `/app` and `/app/` and Admin `/admin` and `/admin/` serve their own compiled
shells with no-store/noindex headers. Backend authentication and authorization
remain required for every API; delivering a public shell does not grant access.

All three Vite applications emit root-relative `/assets/` URLs. The builder maps
actual assets with exact Nginx locations, rejects different bytes at the same URL,
and coalesces identical shared files deterministically. JS/CSS retain the existing
immutable cache policy; other assets use the existing bounded cache duration.
No frontend source, route logic or design asset is rewritten.

Native CI extracts the verified real archive, checks Nginx syntax, requests all
compiled assets and representative shells/Docs pages, and compares actual HTTP
bodies with packaged bytes. It checks private shell headers, JS/CSS MIME types,
Docs search validation, inherited410 responses and missing/private-path404s.
Only the deployment prefix is substituted for the disposable test extraction.
This fixture validates static serving over loopback HTTP; the separate native
Installer workflow verifies TLS/FPM. API/Auth/public-resource/redirect proxy routes,
full TLS server composition, root apply, browser interaction and final P21 closure
remain pending. This fragment is not a complete production virtual host.

The package also ships `deploy/nginx/platform-proxy.conf` for `/api/`, `/f/`,
`/t/` and `/p/` to the inherited loopback platformapi port8081. Include it in the
same TLS server; keep the Go listener loopback-only. It preserves methods, request
URIs/query strings/bodies, auth cookies and backend status/headers. It disables
proxy caching, error interception, automatic redirects and upstream retries. The
final site settings must set upload/body limits consistent with product quotas.

Nginx overwrites forwarded client IP/protocol/host/port using its own connection
and server values and clears RFC Forwarded. This default assumes direct ingress;
CDN real-IP trust requires explicit validated configuration before enabling it.
Authentication relies on the inherited Go loopback proxy trust policy, not on
client-supplied forwarding headers. Access logs are disabled for these sensitive
routes and the existing Website authentication/OAuth/trust shells, which now have
explicit no-store/noindex locations.

CI uses a clearly identified local transport probe to verify byte/header/status
forwarding and spoofed-header replacement through real Nginx. This is transport
coverage only, not real Go login/payment/file or full TLS integration evidence.
The four real compiled apps remain separately verified in the same fixture.
Redirectengine still requires client-address integration review before composing
its public short-link proxy; it is not covered by this fragment.
