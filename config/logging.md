# Native structured logs

Eight fixed Go executables emit JSON. Event messages are developer-owned constants;
producer attributes must be operational identifiers or scalar metrics, not arbitrary
user prose. Passwords, tokens, payment signatures, webhook secrets, TXT verification
material, raw errors, filesystem paths and opaque objects must not be logged. The
shared logger redacts sensitive attributes and suppresses opaque error/object values.
Authoritative business/security audit records remain in their existing stores.

`services/logreceiver/cmd/server` receives sanitized JSON over `GOJET_LOG_SOCKET`,
an absolute Unix socket in an existing directory owned by the service account with
mode `0700`. The socket is mode `0600`; this transport is for processes of that same
trusted native account. It is not a multi-user authentication protocol or public API.
No network port, new product route, credential or database schema is introduced.
An occupied path is rejected, never unlinked on startup. After an unclean termination,
the operator must establish that no live receiver owns a stale socket before removing it.

Set the same `GOJET_LOG_SOCKET` on the seven producers to enable forwarding. Without
it, their existing sanitized stdout remains available. Every record is written locally
first. Forwarding has a 100 ms dial timeout and a 100 ms I/O deadline; failure preserves
local output and emits a fixed warning at most once per 30 seconds per logger. There
is no durable forwarding queue and no replay guarantee. Native log retention must
therefore retain each service's stdout; receiver output is a secondary aggregate,
not audit authority. A receiver acknowledgement means its output writer accepted the
record, not that storage was synced. Output failure is not acknowledged.

The receiver bounds records to 64 KiB, clients to 16, client I/O to one second. It validates
service identity, time and level and applies the shared redaction again. It preserves
original producer time as `source_time` and correlatable identifier attributes; it adds
its receipt time. Object/array payloads are suppressed. Messages/identifiers are trusted
producer fields, not an arbitrary text-secret detection interface.

`logreceiver --check` checks the actual private receiver protocol. SIGINT/SIGTERM stop
accepting, drain bounded clients and remove the socket. P21/P22 own systemd packaging,
independent-machine installation and retention configuration. These are not claimed
complete by the P20 local transport tests.

P20-T037 still requires original business, operations and audit correlation evidence.
Eight startup records and transport tests alone do not close that oracle.
