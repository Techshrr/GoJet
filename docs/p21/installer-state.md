# Private Installer state boundary

`InstallState` is the unprivileged state authority behind the PHP 8.3 HTTP entrypoint.
It does not execute installation or write the root completion lock. The root worker
and approval boundaries are described in [web-configuration-boundary.md](web-configuration-boundary.md).

## Provisioning and authentication

Provision a canonical directory outside document roots, owned by gojet-installer,
mode0700, beneath administrator-controlled ancestors. `initialize()` is an explicit
provisioning operation, never request-time repair. Missing, corrupt, linked,
multiply linked or non-private state fails closed.

Use a bootstrap token generated from at least 32 random bytes; supply only its
SHA256 digest to FPM. Never put tokens in URLs, logs, packages or public responses.
A same-origin JSON POST to `/install/` accepts only `bootstrap_token`. Authentication
is durably limited to ten attempts per sixty-second window. Success rotates the
session and CSRF, stores digests and grants a thirty-minute session. The opaque
session cookie is Secure/HttpOnly/SameSite=Strict and scoped to `/install`; it is
not returned in JSON. Responses use no-store/noindex and frame/base/form restrictions.

FPM requires `GOJET_INSTALL_STATE_DIR`, `GOJET_INSTALL_BOOTSTRAP_SHA256`, exact HTTPS
`GOJET_INSTALL_ORIGIN` and `GOJET_INSTALL_COMPLETION_FILE`. Missing/unsafe provisioning
fails closed. Only real web-server HTTPS is trusted, not forwarded headers.
Schema2 state is required; old provisional state is never implicitly migrated/reset.

## State transitions

A stable flock serializes operations. State writes use exclusive temporary creation,
fsync, atomic rename and directory sync. Mutations consume the one-time CSRF before
validation, including rejected transitions. Exceptions do not persist raw secrets.

- Configuration submission validates fields, freezes environment/checking and
  exclusively publishes the private draft. Reauthentication cannot erase it.
- Approval confirmation validates a root-owned record against the frozen draft,
  exclusively publishes local approval, advances to services/ready and rotates CSRF.
- Separate apply submission consumes CSRF, saves services/checking and creates the
  bounded approved-plan mailbox. HTTP202 means queued, not installed.
- Authenticated GET is read-only. Protected services status includes dependency and
  execution observations; those snapshots grant neither completion nor retry rights.
- Internal pre-apply `check()` accepts only trusted server callbacks. HTTP clients
  cannot choose callbacks or report their own successful validation.

Interrupted publication retains a barrier. Do not delete state, approval or request
files to retry. New-attempt recovery uses separate state and rejects old sessions;
see [installation-preparation.md](installation-preparation.md).

## Completion authority

The root completion directory is canonical, outside the release and writable state,
and not group/world writable. NativeWebConfig checks protected ancestors. FPM has
read access to release, its state and this authority, never root write access.
HTTP checks completion before authentication/rendering; state operations recheck it
under their lock. Any completion node, including a dangling symlink, closes entry
with404. A local writable-state lock is only an additional fail-closed barrier.

The privileged completion command independently checks actual package, data,
services and HTTPS health before atomically writing the root lock. Deleting all
FPM-owned state cannot reopen installation. There is no public finalize operation.

## Verification

State/HTTP tests cover persistence, termination, contention, session/CSRF rotation,
expiry/replay, step skipping, corruption/links and request bounds. Native TLS/FPM and
Chromium tests separately exercise the actual entrypoint, desktop/mobile flows and
lock closure. Explicit state/observation fixtures prove only their stated boundary;
real root execution and completion are verified in the payload chain. Current
acceptance limits are maintained in [STATUS.md](STATUS.md).
