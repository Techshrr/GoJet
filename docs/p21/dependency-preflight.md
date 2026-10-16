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
The sealed web-to-root apply integration and complete T010 wrong-version/absent
binary acceptance matrix are still pending.

Native payload CI uses the actual compiled release, real freshclam signature
updates and the native daemon; it does not mock scanner health. It exercises
stopped Nginx/FPM, unreachable or wrong-credential databases/cache, unavailable
scanner socket, unsafe storage and untrusted helper permissions, then restores
the dependencies and checks readiness again. Tests run only in disposable runners.
