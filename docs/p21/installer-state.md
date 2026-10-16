# Private Installer state boundary

`installer/src/InstallState.php` is an internal PHP 8.3 component, not yet an
HTTP endpoint or a complete privileged installation worker. It does not change
the existing Installer shell, claim T009/T012/T013 complete, or make the payload
installable.

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

The pending HTTP adapter must enforce TLS, POST-only mutation, exact trusted
Origin, request size limits, secure/HttpOnly/SameSite=Strict cookies scoped to
`/install`, no-store/noindex, and uniform post-lock 404 before rendering or
starting sessions. Bootstrap authentication needs request throttling. None of
those HTTP/browser properties are claimed by the CLI component tests.

Native CI exercises real filesystem persistence, process termination during a
validator, independent file-lock contention, secret omission, session rotation
and expiry, CSRF replay, step skipping, corrupted/symlink state and lock denial.
The JSON output explicitly states that web/privileged apply is not verified.
