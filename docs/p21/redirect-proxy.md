# Native short-link ingress

Include the packaged `deploy/nginx/redirect-proxy.conf` alongside the static and
platform fragments in the final TLS server. Static locations retain precedence;
the fallback forwards only a single1–64 character code in the inherited letters,
digits, hyphen and underscore alphabet. Internal `/healthz` is never exposed by
this public fragment. Invalid/multi-segment paths return404. The default upstream
is loopback port8080 and retains the same header, method, body and retry policy as
platformapi ingress.

Set `GOJET_REDIRECT_TRUST_LOOPBACK_PROXY=1` only for this native local ingress and
keep redirectengine bound to loopback. The default is off, preserving existing
direct-request behavior. With this explicit setting, only an actual loopback TCP
peer can supply one literal IP in X-Forwarded-For; duplicate headers, chains,
invalid/unspecified/multicast/scoped addresses reject. Non-loopback peers cannot
change identity with forwarded headers. Direct local health probes without the
header retain their peer. Nginx replaces client-supplied headers before forwarding.
Do not enable this mode with an ingress that appends untrusted chains. Local
processes with access to the listener are part of this trust boundary.

A cloned request carries the normalized client address to the inherited password
attempt key and A/B seed implementations; the original request and downstream
business policy are unchanged. Tests compare direct and forwarded client keys,
separate two visitors, exercise IPv4/IPv6 and malformed/untrusted cases. Real Nginx
transport tests use two distinct explicitly labelled probe listeners to verify
platform-vs-short-link selection and header replacement. These tests do not claim
real database/cache/payment/login business verification or full TLS/apply closure.
