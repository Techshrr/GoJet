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
and authenticated Redis SELECT/PING plus a write/read/delete round trip in the
configured database. Each probe uses an independent random
`gojet:installer:preflight:*` key, `SET NX EX 15`, exact value comparison and
confirmed deletion. It never overwrites application keys; if a connection or
delete permission fails, the key expires within 15 seconds. Read-only replicas
and accounts lacking SET, GET or DEL cannot report ready. This is a bounded
dependency probe, not a workload benchmark or proof of every runtime Redis command.
ClamAV freshness/version and storage probes
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
The complete T010 wrong-version/absent-dependency acceptance matrix remains
pending; the authenticated status handoff is described below.

The native fixture additionally removes each real Nginx, PHP-FPM, systemctl and
systemd-run executable in turn on the disposable CI host, without substituting fake
version output. Each absence must block readiness and the corresponding checks;
the exact original inode/mode is restored in a finally block and readiness is
verified again. This covers missing executables independently of stopped units.
Complete wrong-version coverage remains open;
these probes do not authorize exposing the root preflight command to PHP-FPM.

Native payload CI uses the actual compiled release, real freshclam signature
updates and the native daemon; it does not mock scanner health. It exercises
stopped Nginx/FPM, unreachable or wrong-credential databases/cache, unavailable
scanner socket, unsafe storage and untrusted helper permissions, then restores
the dependencies and checks readiness again. Tests run only in disposable runners.

The Redis access fixture starts a separate loopback Redis with its own private
ACL file and no persistence. It individually denies SET, GET and DEL while PING
still succeeds, then exercises a real read-only replica and restored write access.
It verifies cleanup, actual expiry when DEL is denied, and preservation of existing
keys in both the configured and default databases. The fixture never changes the
host Redis ACL or replication state. These cases passed on source
`5b921a1509d5cf169bd6c66417e28f97eae527ca` in native payload run
[40325314726](https://github.com/Techshrr/GoJet/actions/runs/40325314726), including
restored readiness and independent key expiry. The same run passed the continuous
first-install/browser/root-timer/retirement regression. Full T010 and target-host
acceptance remain open; see [STATUS](STATUS.md).

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
This fixture covers the adapter; browser coverage is described below. T024 protected desktop/mobile browser evidence passed on source `485487a` in
[run 40373645402](https://github.com/Techshrr/GoJet/actions/runs/40373645402).
P09 coherence/closure and the complete P21 gates remain open. See [current status](STATUS.md) for the
installation integration and remaining acceptance work.


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

The renderer returns configuration only. For manual provisioning, root must exclusively create
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


### Root provisioning command

After provisioning the approved web binding, root can run
`php8.3 installer/bin/install-web-preflight-schedule.php` with no arguments. This
uses the same verified renderer, refuses existing unit files, loaded units and
drop-in overrides, exclusively writes root-owned 0644 units, runs systemd syntax
verification, reloads systemd, checks the loaded fragment paths and absence of
overrides, and enables/starts only `gojet-installer-preflight.timer`. It does not
start an installation worker or accept configuration from the browser.

Before any unit publication it creates root-private, fsynced
`preflight-schedule-started.json` with the exact unit hashes. After confirming the
timer is enabled and active it writes `preflight-schedule-ready.json`. Success
reports scheduler_enabled=true and installation_complete=false; it does not
assert that a fresh probe already passed. Repeat or interrupted attempts refuse
instead of replacing files, deleting receipts or automatically rolling back.

On failure, inspect the two root receipts, loaded unit fragments/drop-ins and
timer status. A failure after activation can leave the observation timer running;
stop/disable it before manual repair. Preserve the receipts for diagnosis and
never remove approval, apply or completion records as part of scheduler repair.
Only remove units proven to match the recorded hashes. Automatic recovery and
automatic removal after completion remain separate work.

Native CI now provisions through this shipped command instead of manually writing
its units. It asserts non-root denial, preservation of existing operator files,
interrupted-attempt/repeat barriers, exact unit ownership/modes, durable receipts,
and enabled/active timer before the existing real outage/recovery checks.
