# Private Installer state boundary

`installer/src/InstallState.php` is an internal PHP 8.3 component, now protected by the
HTTP entrypoint, but not a complete privileged installation worker. It retains the existing Installer shell design and does not claim
T009/T012/T013 complete or make the payload installable.

Provision an absolute canonical directory outside every document root, mode
0700, owned by the dedicated Installer process account. Its ancestors must be
administrator controlled. Provision the bootstrap token from at least 32 random
bytes; only its SHA256 digest is supplied to the component. Never put the token
in a URL, release package, log, or public environment response. `initialize()`
is an explicit provisioning action, not request-time auto-repair. Missing,
corrupt, linked, multiply linked, or non-private state fails closed.

A successful authentication rotates the session and CSRF secrets, stores only
digests, and grants a fixed 30-minute session. Every check requires the current
session and a one-time CSRF token. Invalid/out-of-order transitions cannot run
validation. The token is durably consumed before validation. State files are
written with exclusive temporary creation, fsync, atomic rename and directory
fsync; a stable flock serializes all operations. Termination while checking
leaves a durable `checking` barrier that re-authentication cannot erase.
Validator errors become a generic failed status and do not persist exception
text, credentials, or form input. Successful checks advance only one step.

Only the four pre-apply steps (environment/data/site/admin) can advance through
this component, using trusted server-side validators. No callable name or
success value may be selected by client input. Services, health, and completion
require the still-pending privileged worker integration. This component cannot
create an installed lock or report completed installation. An existing
`installed.lock` blocks every operation. The future worker must use the same
stable state lock when committing completion, and must own the final lock
boundary so a compromised web process cannot reopen installation.

The public entrypoint now requires provisioned `GOJET_INSTALL_STATE_DIR`,
`GOJET_INSTALL_BOOTSTRAP_SHA256`, and an exact HTTPS `GOJET_INSTALL_ORIGIN`
without a trailing slash. Missing/unsafe provisioning fails closed. State schema
2 includes the durable bootstrap throttle (10 attempts per 60-second window);
older provisional state is rejected, never implicitly reset. This is not an
upgrade mechanism for an installed system.

Authenticate with a same-origin JSON POST to `/install/` containing only
`bootstrap_token`. The response sets an opaque Secure/HttpOnly/SameSite=Strict
cookie scoped to `/install` and returns the CSRF token, current step and status.
It never returns the session token in JSON. Do not log request/response bodies.
All mutations require exact Origin and a bounded JSON body. Only the web
server's `HTTPS=on` variable is trusted; forwarded headers are insufficient.
The trusted Nginx/FPM configuration must preserve this boundary. Authentication
is throttled durably and rotates prior sessions. Responses are no-store/noindex
with frame/base/form restrictions. Installation locks yield 404 on every route.

Authenticated GET can render the existing welcome/current-step shell. Future
steps return409. POST step execution currently returns503 without running any
validator or applying any configuration; no arbitrary client success field can
advance state. Complete/health/services are unavailable until worker integration.
The interactive login/step forms, native configuration/apply/health, real TLS
server configuration and desktop/mobile browser verification remain outstanding.

Native CI exercises real filesystem persistence, process termination during a
validator, independent file-lock contention, secret omission, session rotation
and expiry, CSRF replay, step skipping, corrupted/symlink state and lock denial.
A real PHP HTTP server additionally tests the production entrypoint, cookie
flags/rotation, request bounds/origin, route order, bootstrap throttling and
post-lock404. Its unshipped router sets a test TLS server variable; this does not
claim real TLS or browser verification. A separate HTTP probe with that variable
off rejects spoofed forwarded HTTPS headers. Evidence flags preserve these limits.

## Root-owned completion authority

The public entrypoint now requires `GOJET_INSTALL_COMPLETION_FILE`, pointing to
`installed.lock` in a canonical root-owned directory that is not group/world
writable. Provision `/var/lib/gojet-installer-authority` through the packaged
tmpfiles configuration. Regenerate older provisional FPM configurations; missing
completion authority fails closed with503. No implicit migration/reset occurs.

Before writing FPM configuration, NativeWebConfig verifies root ownership of the
ancestor chain as well. FPM's restricted filesystem access permits reading only
the release, private state and completion directory. HTTP checks the root lock
before authentication/rendering, and state operations check it again under their
state mutex. Any existing lock node, including a dangling symlink, closes the
entrypoint. The old writable-state lock remains an additional fail-closed local
barrier but cannot replace the independently configured root authority.

There is deliberately no public or PHP-service-account finalize operation.
The pending root apply worker must perform real service/dependency health checks
and atomically commit the root lock. Tests inject the lock from the isolated
root fixture solely to verify the read boundary; they do not claim health or
installation completion. Native TLS tests prove that the actual FPM account
cannot delete the lock, create files in its directory, or rename that directory.
Even deleting all PHP-writable state cannot reopen the Installer.
