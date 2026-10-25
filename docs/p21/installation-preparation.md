# Native installation: preparation, execution and recovery

This is the current developer/operator reference, not the final production install
manual. Acceptance and remaining work are listed in [STATUS.md](STATUS.md).
All commands below are shipped PHP 8.3 CLI entrypoints under `installer/bin/` and
require root unless explicitly described as browser operations. Paths and digests
come from reviewed server configuration, never browser input.

## Provision the first web entry

`provision-web-entry.php NEW_DIRECTORY` reads at most 8 KiB of private JSON on
stdin. The exact `gojet.web-entry.v1` schema contains `schema`, `origin`, `release`,
`manifest_sha256`, `source`, `bootstrap_sha256`, `tls` and `files`. `tls` contains
`certificate`, `private_key`, `http_port`; `files` contains `root`, `clamav_socket`.
Use a reviewed manifest/source, a canonical HTTPS DNS origin and existing matching,
currently valid root0644 certificate/root0600 key. Supply only a bootstrap token
SHA256 digest; retain the randomly generated token privately for browser login.
The new directory must be absent, beneath a protected root-owned parent, outside
all supplied inputs and host authority. Its derived Unix socket path is bounded.

Prerequisites are the verified extracted payload, dedicated gojet-installer account,
protected completion authority directory and active standard Ubuntu Nginx/PHP 8.3
FPM services. This command does not install OS packages, issue certificates, change
DNS, create service accounts or grant storage access. It refuses an existing
Installer pool/vhost, occupied port, alternate FPM include authority, service
unit overrides, existing approval or completion. It preserves other host settings.

The command creates root0600 `binding.json` and `web.json`, an installer-owned0700
`state`, and a root0755 enclosing directory. The derived preparation target is
`NEW_DIRECTORY/prepared`; the socket is `NEW_DIRECTORY/fpm.sock`. Retain `web.json`
as the original renderer input for a later configuration switch. It publishes
root0600 `/etc/php/8.3/fpm/pool.d/gojet-installer.conf` and root0644
`/etc/nginx/conf.d/gojet-installer.conf`, checks both native configurations and
reloads the real services. A local HTTPS request verifies certificate/name and
requires the unauthenticated401 response before success. No token is submitted by
this probe; no account, draft, migration, approval or installation is created.

The product activation lock and shared FPM lock serialize first entry with product
activation and later replacement. Root authority
`web-entry-started.json` binds the destination, private files, rendered host files
and TLS hashes before mutation; `web-entry-ready.json` records verified entry
activation. Publication never overwrites an existing node. A matching completed
repeat acknowledges the record without resetting state. If only the success receipt
is missing, rerunning the identical request may verify the complete publication,
reload and recheck HTTPS. Incomplete publication, changed configuration or changed
TLS remains blocked for operator inspection; never delete real receipts to retry.
This is entry readiness, not final installation acceptance. Root preparation timer,
approval and apply remain separate commands below. Bootstrap-to-product handover
and recovery of incomplete first publication remain open.

## Prepare an immutable plan

`prepare-installation.php DIRECTORY RELEASE MANIFEST_SHA SOURCE_SHA` reads bounded
private JSON on stdin: `configuration`, `admin` and `tls`. The release must match
the independently approved manifest/source before a packaged executable is used.
The native compiler derives eight environment files, systemd units, Nginx config,
data-bootstrap request and dependency settings from one profile.

Root preparation generates six independent 256-bit keys and a random key ID when
`configuration.secrets` is omitted. An explicit malformed key set is rejected.
Keys are stored only in the private plan and never printed. Validation may discard
unused in-memory keys before publication; a published plan is never replaced or
rekeyed on retry. The pure configuration compiler still requires explicit keys.

Administrator input is validated by the verified native binary before plan
publication or DDL. Storage cannot overlap the release or plan. TLS files must be
distinct, root-controlled and outside writable storage/plan paths. Publication uses
a parent lock, root0700 staging, root0600 files, fsync and an atomic rename into an
absent destination. The schema3 seal binds exact plan bytes, external TLS hashes
and modes, source and manifest. It is an integrity record under root authority,
not protection against a malicious root operator.

The plan's systemd files preserve the packaged nonroot identities and hardening.
Only verified release, private environment and configured storage paths are
substituted. Preparation does not install units, initialize data or approve apply.

## Web draft preparation and approval

See [web-configuration-boundary.md](web-configuration-boundary.md) for input schema,
request limits, freezing and approval authority.

| Entry point | Purpose |
| --- | --- |
| `prepare-web-configuration.php BINDING` | Prepare a root-supplied stdin draft against a root binding |
| `prepare-frozen-web-configuration.php BINDING STATE` | Prepare the frozen installer-owned draft |
| `run-web-preparation.php BINDING STATE` | Guarded worker with durable attempt/success receipts |
| `approve-web-configuration.php PREPARED STATE` | Approve the exact current frozen draft and sealed plan |
| `provision-web-apply.php PREPARED NEW_STATE BOOTSTRAP_SHA256` | Alternate entry for an already root-approved plan in a new state directory |

The worker waits before submission, serializes through root0600
`BINDING.worker.lock`, writes an exclusive started receipt before preparation and
a ready receipt after verifying the published plan. A completed repeat revalidates
without rewriting it. An interrupted attempt stays blocked. A retired binding is
unavailable. Root does not write into an existing FPM-owned state directory.

Approval publishes fixed root `web-apply.json` and a root:gojet-installer0640
`configuration-approval.json`. The authenticated browser confirms it with a fresh
CSRF token and then separately submits apply. Neither preparation nor approval
starts installation. Interrupted/duplicate approval records are not overwritten.

## Preparation scheduling

`render-web-preparation-schedule.php BINDING STATE [OLD_BINDING]` validates inputs
and returns unit text. `install-web-preparation-schedule.php` accepts the same
arguments and exclusively publishes, verifies and enables the units.

The original timer uses `gojet-installer-preparation.service`/`.timer`. A replacement
requires valid root retirement and replacement receipts, exact successor binding
and an activated successor directory. It uses a deterministic per-binding unit name
and separate scheduler receipts, preserving existing units and records.

The timer checks every ten seconds after an initial one-second trigger. No draft,
a started/ready/retired receipt, or final completion prevents service execution.
The worker's own guards still apply to direct invocation. A 300-second timeout
terminates the whole process group and retains the interruption barrier. Only a
plan is prepared; approval and execution are independent.

Scheduler publication rejects existing files, links, loaded fragments and drop-ins.
Started/ready receipts are durable. Partial scheduler publication requires inspection;
removing receipts is not a retry procedure.

## Execute the approved plan

`apply-installation.php DIRECTORY` orchestrates data bootstrap, eight-service
activation, site activation and final health/completion. A fixed root
`installation-apply.lock` serializes invocations. Each phase rechecks prerequisites;
failed/interrupted journals require their explicit recovery procedures. A matching
final marker is acknowledged without rewriting; another plan cannot adopt it.

For approved browser requests, `run-web-apply.php` takes no arguments. It reads the
fixed root binding, validates the unprivileged seal-digest mailbox and writes an
exclusive `web-apply-started.json` before executing. `install-web-apply-schedule.php`
enables its timer; `install-web-preflight-schedule.php` enables observation only.
See [execution-schedule.md](execution-schedule.md) for timing and execution guards.

`run-prepared-data.php DIRECTORY` runs the data phase independently after verifying
plan inventory, links, modes, hashes, TLS, package and current native dependencies.
DataBootstrap retains its own lock, immutable migrations, administrator authority
and durable journal. An identical completed phase is a no-op. Failed initialization
is not retried by editing inputs or deleting journal rows; see
[data-bootstrap.md](data-bootstrap.md) and [migration-execution.md](migration-execution.md).

## Service activation and recovery

`activate-services.php DIRECTORY` holds the fixed global activation lock, requires
completed data and current readiness, and refuses an installed marker or existing
units, aliases, enable links and overrides. It exclusively publishes the eight
sealed units and starts logreceiver before consumers. Checks verify actual nonroot
executables, enabled/running states, zero restarts, API discovery, redirect health
and the private log socket. The root-private journal is `services.json`.

Failure cleanup removes only exact owned units. It never stops unrelated listeners
or rolls back data. Direct retries of failed/interrupted activation are blocked.
`recover-services.php DIRECTORY` revalidates plan, dependencies, completed data and
all unit/fragment/override/link identities before stopping anything. It refuses a
site journal or completion marker. Successful cleanup records `recovered`, prior
phase and recovery count, permitting activation again; a matching repeat is a no-op.
Foreign/changed configuration requires operator review, not forced deletion.

## Site activation and recovery

`activate-site.php DIRECTORY` shares the activation lock, requires ready services
and current dependencies, and exclusively publishes root0644
`/etc/nginx/conf.d/gojet.conf`. Existing targets/listeners, unsupported include
authority and Nginx warnings are rejected. The Ubuntu conf.d wildcard must be active.
Native syntax is checked before reload; `site.json` records progress. A matching
success repeat checks identity and syntax without reloading.

On failure only the exact owned vhost can be removed; if reload was attempted,
restoration is also reloaded. `recover-site.php DIRECTORY` rejects a successful
site, installed marker, changed bytes, links or foreign authority. It validates
and reloads the remaining Nginx configuration before recording `recovered`.
A failed restoration remains `recovering`; the same recovery command can resume
after prerequisites are fixed. Data and service journals remain unchanged.

## Health and completion

`check-installation.php DIRECTORY` performs current checks without marking success.
`complete-installation.php DIRECTORY` repeats them and atomically commits
`/var/lib/gojet-installer-authority/installed.lock` only after success.

Checks bind the package, sealed configuration/TLS, completed phase journals,
current dependencies and all eight actual service identities/health. Local HTTPS
trusts the sealed certificate, verifies peer name/SNI, does not follow redirects or
resolve a remote host, and bounds response size/time. Packaged SPA/Docs bytes,
provider discovery, anonymous API denial, Installer404 and wrong Host421 are checked.
Native readiness is rechecked after HTTP probes.

The root0600 final record contains source, manifest and seal digests. It is synced
and published without replacing any existing node. A matching repeat preserves it.
FPM cannot write or remove this authority; even deleting writable web state cannot
reopen the Installer. Browser404 alone is not proof of successful installation.

## Recover a published plan with a missing ready receipt

`recover-web-preparation.php BINDING STATE EXPECTED_SEAL_SHA256` handles only a
complete published plan whose success receipt was not written. Stop the preparation
timer/service and review the private attempt, binding and expected seal first.

Recovery shares the normal worker lock and verifies the original started identity,
exact package/source/release, intact plan/TLS, current dependencies and frozen draft
provenance. Approval/apply artifacts, phase journals, completion, changed draft,
wrong digest or invalid existing receipt block it. It writes only a missing ready
receipt and preserves the started record, plan, web state and keys. A matching
repeat is verified without rewriting. It does not approve or execute installation.

## Replace invalid configuration before plan publication

`restart-web-configuration.php BINDING STATE NEW_DIRECTORY EXPECTED_DRAFT_SHA256
BOOTSTRAP_SHA256` requires a reviewed frozen draft and an absent successor beneath
a root-controlled parent. It rejects published plans, ready/approval/apply/completion
artifacts and mismatched started records. The shared worker lock prevents races.

It first permanently retires the old binding with a durable root record, then
creates root0600 `NEW_DIRECTORY/binding.json` and a fresh installer0700 `state`.
Initialization occurs while the enclosing directory is root0700; only newly created
files change owner. Root records replacement publication, then permits traversal
with root0755. The new plan destination is `NEW_DIRECTORY/prepared`.
Old binding, draft, state and started receipts remain; sessions, approvals and keys
are not copied. A partial restart remains retired and requires inspection.

Before new submission, root can run `switch-web-configuration.php` with no arguments
and a private JSON object on stdin. Its exact fields are `previous` (the eight
original NativeWebConfig renderer inputs), `predecessor` (retired binding path),
`binding` (successor binding path), and `bootstrap_sha256` (the new token digest).
Keep the original rendered inputs privately; do not recover secrets from browser
requests or put the token itself into this request. The root-selected new digest
becomes the successor endpoint's authentication authority.

This command supports the standard active Ubuntu `php8.3-fpm.service` and exclusively
manages `/etc/php/8.3/fpm/pool.d/gojet-installer.conf`, which must already be a
root0600 regular single-link file matching the exact original rendered pool.
It does not provision a first endpoint. Changed pools, alternate includes, duplicate
pool sections, unit overrides, changed lineage, approval or completion are refused.
Nginx, origin, socket, release and TLS remain the original endpoint configuration.

Both attempt locks and a global root lock serialize switching. Before atomically
replacing the pool, it writes successor-private `fpm-switch-started.json` binding
old/new configuration hashes. It verifies native syntax, reloads the real service
and writes `fpm-switch-ready.json`. No web state, approval or apply request is written.
A matching completed repeat is read-only. After interruption, explicitly rerun the
same request: only the exact recorded old/new pool may be continued. A mismatched
request or foreign file remains blocked. Preserve receipts; do not restore the
retired endpoint or delete barriers as a retry procedure. A durable ready receipt
records configuration/reload completion, not final installation or account health.

The optional predecessor argument to preparation scheduling validates this lineage.
Initial endpoint provisioning, restart/scheduler publication recovery and the final
bootstrap-to-product handover remain open. The old page is not automatically reset.

## Verification boundaries

The native payload regression uses real packages, MySQL/Redis, Nginx/FPM, systemd
and eight services. It covers actual service/site failures and worker termination,
explicit recovery, browser configuration/approval/apply, timer preparation/execution,
observed progress and final completion. Replacement configuration is submitted through
a real browser at the same Nginx endpoint after persistent system FPM switching
and a real service restart, then prepared by its own timer. The switch regression
also constructs a missing-ready-receipt boundary; this is not a power-loss claim.
New switch coverage requires the corresponding native CI run to pass.
Published-plan receipt interruption uses an explicitly constructed boundary fixture;
it is not a SIGKILL or machine power-loss claim. Detailed evidence and remaining
candidate/target-system obligations are summarized only in [STATUS.md](STATUS.md).
