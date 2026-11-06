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
approval and apply remain separate commands below. Same-origin product handover
uses the explicit root authorization below. Recovery of incomplete first publication
remains open.

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

## Authorize a same-origin product handover

After preparing and reviewing the sealed plan, root runs
`authorize-entry-handover.php PREPARED ORIGINAL_ENTRY_DIRECTORY` before starting
apply. Use the original directory created by `provision-web-entry.php`, including
when its web state has since been replaced. This command does not approve a browser
draft or start installation. It writes only root0600 `PREPARED/entry-handover.json`.
A matching repeat before site execution acknowledges the existing authorization.

The command binds the plan seal, verified release/source/manifest, HTTPS origin,
TLS content, original root receipts and exact generated Nginx entry bytes. It
refuses changed files, links, unsafe ownership, an existing site attempt or a
completed installation. The shared root activation lock serializes it with entry
provisioning and site operations. Without this record, the bootstrap listener is
still treated as an existing site conflict.

Site activation journals the handover identity before moving only the matching
`/etc/nginx/conf.d/gojet-installer.conf` to the protected sibling
`gojet-installer.conf.handover`, which the normal `*.conf` include excludes.
It rechecks the expanded Nginx configuration for all remaining listener conflicts,
then publishes `gojet.conf`, validates syntax and reloads. Other sites are never
adopted or removed. Normal apply uses this same site phase automatically.

A failed reload restores the original entry when its bytes and paths remain safe;
the failed journal still blocks retry. After a failure or killed worker, root uses
`recover-site.php PREPARED`, checks the restored service, then explicitly resumes
the approved plan. Recovery removes only an exact owned product file and restores
only an exact saved entry. Changed/missing/ambiguous files require inspection;
never delete journals or overwrite intervening operator files to retry. A successful
site is not recoverable through this teardown command.

The original FPM pool, private state and receipts are retained for failure recovery.
Only the public vhost is retired here; the product returns404 for `/install`.
The result observation and explicit post-completion retirement procedures below
close the public feedback and owned runtime-resource lifecycle; their new native CI
must pass before treating them as verified.
Final installation authority still requires the existing live product health checks
and atomic root completion lock, not this handover receipt.

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

After verifying the original retirement record, explicitly run
`recover-configuration-restart.php BINDING STATE ORIGINAL_NEW_DIRECTORY
EXPECTED_DRAFT_SHA256 BOOTSTRAP_SHA256` from the same verified release. It validates
the original binding, frozen draft, predecessor started identity and exact retired
successor identity before touching the new tree. No new successor can be selected.
It uses the predecessor worker lock and, when present, the successor worker lock.
The command never enables a timer, switches FPM, approves a draft or executes data.

Recovery completes missing files only while the tree is still root0700, including
initial state creation and interrupted ownership transfer. Exact completed state can
finish the ready receipt/traversal boundary. A root0755 successor with missing state
or binding is refused; its existing state is never rewritten. Authenticated/used state,
submitted drafts, prepared plans, FPM-switch records, extra files, unsafe permissions,
links or changed bytes are also refused. Old attempts stay retired; their drafts,
sessions and journals are preserved. A matching unused completed repeat is read-only.
The bootstrap digest remains initialization input, not copied session authority;
the separate FPM switch still selects and binds the actual new endpoint token.

New binding/ready files are staged and published without replacement. Missing or
corrupt retirement authority, truncated existing files, unknown temporary files and
ambiguous nodes require manual inspection; do not delete retirement to retry. The
native regression constructs ten publication/receipt boundaries, verifies refusal
of altered identity and busy workers, then continues the same recovered successor
through the actual FPM switch, browser submission and root preparation timer.
These are boundary fixtures, not machine power-loss tests; new CI must be checked.

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
First entry, its partial-publication recovery, scheduler publication recovery and the
continuous first-install journey have passed at the source recorded in STATUS. The new
replacement-publication recovery needs its own CI. The old page is never automatically
reset; complete operational recovery and exact target-system acceptance remain open.

## Confirm the browser result

A404 response from `/install` is not installation success. While authenticated,
`GET /install/services` exposes a non-secret result identifier derived from the
root-approved seal, only when the approval belongs to the current state. The page
retains this identifier in memory. After the entry closes, it reads only the same
origin's `/.well-known/gojet-installation-result`, with no credentials, URL token,
redirect following, cache or automatic installation retry.

After live product health checks and atomic publication of the private completion
lock, root publishes `/var/lib/gojet-installation-result/result.json`. Its protected
root0755 directory and root0644 file contain only a version, opaque result identifier
and true completion flag. Nginx serves this exact file as JSON with no-store/nosniff
headers, rejects symlinks and accepts only GET/HEAD. It never serves private state,
credentials, the seal or completion-lock contents.

The page shows completion and fixed relative Admin/site links only for the matching
receipt. Missing, malformed, oversized, redirected, offline or unrelated results
remain unconfirmed, with an explicit Check installation result button. A page that
never obtained its approved identity cannot infer success from closure. This is an
observation of the completed installation, not a continuous service-health claim.

If root was interrupted after the completion lock but before receipt publication,
rerun `complete-installation.php PREPARED` with the same verified plan. It preserves
the private lock and may publish the missing receipt. An existing different, linked
or unsafe public file is refused. Do not remove a real completion lock to retry.

## Retire the completed Installer

Once the root executor has finished, run
`retire-installer.php PREPARED CURRENT_WEB_JSON` as root. For an unchanged entry,
CURRENT_WEB_JSON is the original entry's `web.json`. After a configuration replacement,
provide a root0600 JSON file containing the exact current eight NativeWebConfig
renderer inputs; preserve the original entry directory and receipts. The command
requires a matching completion lock and successful same-origin handover. It never
starts an incomplete installation or turns a client flag into completion authority.

The command takes the apply, activation and FPM locks; verifies the current pool's
exact rendered bytes; and checks each discovered shipped scheduler's started/ready
receipts, unit bytes, loaded fragment and absence of drop-ins. Active worker services,
partial scheduler publication or changed resources require inspection. After a durable
root retirement-started receipt, it disables/stops the registered timers, parks only
`gojet-installer.conf` as `gojet-installer.conf.retired`, validates/reloads standard
FPM and observes closure of the Installer socket. Other FPM pools and product services
remain running. Unit files, private state, secrets, original web configuration,
completion and recovery records are retained; this command is not a data eraser.

A matching completed repeat verifies the retired resources. For an interrupted
attempt, rerun the same command only after inspecting the private records. It can
resume exact active/parked pool states and already-disabled timers without changing
identity. Foreign/missing/ambiguous files are refused. The ready receipt is written
only after timers are disabled and the socket is closed. No rollback reopens a
completed public Installer. The independent public result remains available after
FPM retirement. These retirement interruption cases are boundary fixtures in CI,
not a claim of machine power-loss testing.

## Verification boundaries

The native payload regression uses real packages, MySQL/Redis, Nginx/FPM, systemd
and eight services. It covers actual service/site failures and worker termination,
explicit recovery, browser configuration/approval/apply, timer preparation/execution,
observed progress and final completion. Replacement configuration is submitted through
a real browser at the same Nginx endpoint after persistent system FPM switching
and a real service restart, then prepared by its own timer. The switch regression
also constructs a missing-ready-receipt boundary; this is not a power-loss claim.
Same-origin handover coverage passed at the commit recorded in STATUS. Result
and retirement coverage has also passed at the commit recorded in STATUS.
It exercises actual reload failure, a worker killed during native reload, explicit
restoration of the Installer, protected foreign files, Nginx restart and product
health/completion. Pre/post-rename interruption cases are constructed boundary
fixtures; they do not claim machine power-loss testing.
Published-plan receipt interruption uses an explicitly constructed boundary fixture;
it is not a SIGKILL or machine power-loss claim. Detailed evidence and remaining
candidate/target-system obligations are summarized only in [STATUS.md](STATUS.md).

The same-origin desktop/mobile result test uses an explicitly preapproved readiness
state fixture and negative network fixtures. Its successful completion comes from
actual Nginx handover, service/TLS health and the real root lock/receipt publisher;
it does not claim that this new test repeats the entire interactive configuration
journey. The first-entry regression now continues through a separate uninterrupted
browser journey using an empty database and the same persistent endpoint: actual form
submission, scheduled root preparation, explicit root approval and handover authorization,
browser acknowledgement, one apply POST, actual root apply timer, desktop/mobile result
observation, then explicit retirement of preparation/preflight/apply timers and FPM.
No web state, approval, progress or completion is injected in this continuous journey.
The parent verifies that approval does not mutate the database, the actual migration and
administrator counts, executor exit before retirement, and preservation of private records.
Browser TLS uses a disposable self-signed test certificate; root health checks verify it.
Evidence is `payload/browser-journey/result.json` with source/seal identity and screenshots.
This continuous coverage has passed at the source recorded in STATUS; it does not
replace account-security or exact target-system acceptance.


## Explicit first-entry publication recovery

`recover-web-entry.php ORIGINAL_ENTRY_DIRECTORY` reads the same bounded JSON on stdin
as `provision-web-entry.php`. Use the original root-private request (mode 0600), the
same verified release and the same directory. For example, after inspecting the
original entry receipt and remaining files:

```sh
sudo sh -c 'exec php8.3 /verified-release/installer/bin/recover-web-entry.php /original-entry < /root/original-entry-request.json'
```

A complete matching root started receipt is mandatory. Before changing anything, the
command validates the source, package, TLS, every existing private file, pool and state
node, and the absence of conflicting listeners. It can complete an unpublished prefix:
started only, a protected root directory, one or both private inputs, initial state
creation/ownership transfer, or the completed state with the pool/vhost still missing.
Only absent files are published. A root-only 0700 entry can finish initial state creation;
a previously traversable 0755 entry with missing state or inputs is refused. Changed,
linked, writable, used or unknown state is not reset. Existing authenticated/advanced
state is not copied into a fresh attempt. The ordinary provisioning command retains its
partial-publication refusal; the existing fully-published missing-ready recovery remains.

The recovery takes the existing activation/FPM locks and writes ready only after native
syntax/reload and verified HTTPS health. It never prepares data, approves a draft or
executes installation. Completed handover/retirement cannot be reversed. Corrupt/missing
root authority, truncated/foreign nodes, unknown staging files and ambiguous socket or
publication order require inspection; preserve them instead of deleting records to retry.

New native tests construct ten entry publication prefixes, changed bindings/files,
unsafe permissions/links and missing live state. Scheduler tests cover absent/partial/full
unit publication, missing ready, changed root inputs and serialization. They preserve a
real completed preparation barrier and reject recovery after actual installation completion.
These are publication-boundary fixtures, not machine power-loss claims. The continuous
first-install browser journey remains a separate regression and has passed at the source
recorded in STATUS; entry/scheduler recovery has passed at the source recorded in STATUS. The subsequent
replacement-directory recovery is tracked separately.
