# Native dependency preflight

Run the trusted PHP 8.3 CLI entrypoint `installer/bin/preflight.php` as root,
passing a bounded JSON request on stdin. The strict schema is demonstrated by
`scripts/p21/native_dependency_preflight_test.py`; credentials must never be put
in command arguments. The CLI returns only named boolean checks and release
identity. Exit 0 means all probes passed, 2 means dependency failure, and 1 means
an invalid or unauthorized request. No raw command output or credentials are
returned.

The initial supported profile is Ubuntu 24.04 with native systemd service names
`nginx.service` and `php8.3-fpm.service`, loopback MySQL/Redis, and a local ClamAV
Unix socket. Configuration variants outside this profile require explicit support.

Checks include the root-approved complete release manifest, a responsive systemd
manager, installed/config-valid/active Nginx and PHP 8.3 FPM, PHP PDO MySQL/OpenSSL,
a real MySQL8 connection with migration-compatible autocommit/trigger settings,
and authenticated Redis SELECT/PING. ClamAV freshness/version and storage probes
reuse the shipped Go filepreflight helper, executed as the actual `gojet` account
only after the entire release passes integrity verification. Provision storage,
quarantine and published directories with gojet ownership and mode0700; grant
gojet access to the actual clamd socket via its service group. The clamd
configuration must permit its read-only `VERSION` command (`EnableVersionCommand
yes` on versions supporting this directive), because the inherited authority
requires actual engine/signature metadata and freshness. A running daemon with
`VERSION` disabled is not healthy for GoJet. Do not enable administrative RELOAD
or SHUTDOWN commands for this probe. The verifier does
not grant permissions, change database settings, install packages or start units.

A missing or failed check blocks aggregate readiness. Results are snapshots;
recheck immediately before privileged apply. This CLI is not exposed to the web
worker and does not advance Installer state or write its final completion lock.
The sealed web-to-root apply worker revalidates this authority before execution.
The complete T010 wrong-version/absent-dependency acceptance matrix and
authenticated browser status handoff remain pending.

The native fixture additionally removes each real Nginx, PHP-FPM, systemctl and
runuser executable in turn on the disposable CI host, without substituting fake
version output. Each absence must block readiness and the corresponding checks;
the exact original inode/mode is restored in a finally block and readiness is
verified again. This covers missing executables independently of stopped units.
Wrong-version coverage and the authenticated browser status handoff remain open;
these probes do not authorize exposing the root preflight command to PHP-FPM.

Native payload CI uses the actual compiled release, real freshclam signature
updates and the native daemon; it does not mock scanner health. It exercises
stopped Nginx/FPM, unreachable or wrong-credential databases/cache, unavailable
scanner socket, unsafe storage and untrusted helper permissions, then restores
the dependencies and checks readiness again. Tests run only in disposable runners.

## Protected snapshot read

After root provisions the sealed web apply binding, root may run
`php8.3 installer/bin/refresh-web-preflight.php` with no arguments. It uses only
the fixed root binding, validates sealed preparation, and publishes named boolean
checks to root-owned `preflight.json` in the completion authority directory.
Refreshes serialize on the immutable binding. The previous snapshot is removed
and the directory synced before probing, so an interrupted refresh leaves no
previous green result. Publication uses an atomic rename and directory sync.
Integrity failures publish unavailable without private diagnostics.

An authenticated `GET /install/services` with `Accept: application/json`, while
the durable installer step is services, returns `step`, `status` and `preflight`.
The preflight field contains only status, checked_at, eight boolean checks, ready,
and installation_complete (always false). A snapshot is bound to the state
directory and expires 120 seconds after probing starts. Missing, malformed,
unsafe-owner/mode, symlink, wrong-binding, or future-dated snapshots cannot return
ready; expired snapshots report stale with no cached green checks. This read
neither advances state nor creates an apply request. All existing origin,
session, completion-lock and step-order checks still apply. Root apply always
rechecks live dependencies independently of this advisory snapshot.

The payload fixture exercises the shipped HTTP adapter as gojet-installer and
real root refreshes, including stopped Redis and altered sealed preparation.
This is adapter coverage, not a browser/TLS handoff claim. Environment/health route integration and the complete
T024 acceptance gate remain pending. The installer remains installable=false.


The interactive installer reads the protected services endpoint after successful
authentication and on Refresh status. It displays eight named checks, observation
time, and explicit failed/unavailable/expired states. A local expiry timer clears
old green checks; each request clears previous observations, including offline
and authentication failures. The snapshot is advisory and never bypasses root
apply verification. Refresh status reads only; it does not run privileged probes.
Desktop/mobile Chromium tests use root-owned snapshot fixtures over real native
HTTP/FPM to cover these states and passive expiry. Actual root probing and Redis
failure remain independently covered by the payload test, not simulated by this
browser fixture.


## Native refresh scheduler

Root can generate pinned units after provisioning the sealed web binding:
`php8.3 installer/bin/render-web-preflight-schedule.php`. This no-argument command
revalidates the approved seal, complete package and live prerequisites, then emits
a JSON object containing `gojet-installer-preflight.service` and
`gojet-installer-preflight.timer`. The executable path uses the verified canonical
release directory, never a mutable `current` link or browser-supplied path.

This command renders configuration only. Root provisioning must exclusively create
the two corresponding files in `/etc/systemd/system` with root ownership and mode
0644; refuse existing files, symlinks or drop-in overrides and inspect conflicts
instead of overwriting them. Run `systemd-analyze verify` on both generated files,
then `systemctl daemon-reload` and
`systemctl enable --now gojet-installer-preflight.timer`.

The timer first runs after one second and schedules another observation 30 seconds
after the preceding service becomes inactive. The 110-second execution limit
kills the entire probe process group on timeout. A slow or failed probe can still
produce an unavailable/expired observation; no timer guarantee replaces the
120-second freshness check. The existing root binding lock serializes manual and
scheduled refreshes. The service only observes dependencies, never applies an
installation or reads commands from the web state.

Both units require the root binding and absence of the final completion lock.
After completion, future service starts are skipped. Disable the unused timer
with `systemctl disable --now gojet-installer-preflight.timer`; stop the service
before removing only these provisioned units, then daemon-reload. Keep root
approval, completion and phase journals intact. This does not install an apply
worker, retry failed installations or switch the bootstrap vhost.

The disposable native payload fixture installs the generated units, verifies them
with systemd, starts the real timer and observes subsequent ticks detect stopped
Redis and recovery. It also checks non-root renderer refusal, completion-condition
skipping and unchanged installer state/mailbox, then removes its own unit files.
