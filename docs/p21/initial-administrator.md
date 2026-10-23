# Native initial administrator boundary

The packaged `platformapi --bootstrap-admin` mode creates the first administrator
through the existing `internal/admin.Service.BootstrapAdministrator` authority.
There is no new daemon, fixture authority, HTTP bootstrap endpoint, password
algorithm, permission vocabulary or audit schema.

Only effective UID0 can invoke this mode. The future root-owned Installer worker
must validate and seal the administrator request before invocation. PHP workers
run under `gojet-installer` and cannot invoke this mode successfully. The CLI
refuses the test-auth adapter, additional arguments, oversized/unknown-field or
trailing JSON, incomplete Admin runtime configuration and unavailable native
MySQL/Redis. It uses the same Admin access factory/schema verification as the
production API. Runtime credentials and Admin TOTP encryption/origin settings
must be supplied from the protected service configuration; never from URL/query
parameters. Existing `GOJET_REDIS_DB` configuration is honored.

Send one bounded JSON object on stdin with `email`, `display_name`, `password`,
and `correlation_id`. Passwords never appear in argv. The initial role receives
the explicit existing PermissionCatalog, not client-selected permission strings.
A database-scoped advisory lock serializes installer processes. The inherited
serializable transaction rejects an existing administrator and atomically writes
the administrator, PBKDF2 credential, initial role/assignment and `admin.bootstrap`
audit. Repeat calls refuse instead of resetting credentials. All errors are
redacted; success outputs only `{"created":true}`. An output interruption after
commit does not imply rollback: the worker must reconcile the existing audited
administrator rather than retrying with a new password or deleting records.

The native CI fixture applies all original migrations to a disposable native
MySQL database, starts native Redis and executes the real compiled platformapi
binary. It verifies non-root rejection, input bounds, a competing live database
lock, one-time creation, duplicate refusal, credential derivation, inherited
permissions and the original audit authority. It does not export credentials or
hash material. This is not yet the sealed privileged worker, complete Installer
flow, login/browser proof, or a production deployment.

Before publishing a preparation plan or executing migrations, the Installer now calls
`platformapi --validate-bootstrap-admin` on the already verified immutable binary.
It uses the same bounded stdin parser and Admin email/password/correlation rules as
creation, without loading database/Redis configuration, hashing passwords or writing
accounts. Only root may invoke it; extra arguments and test-auth mode are rejected.
Success is exactly `{"valid":true}`; errors never echo input. This is input validity,
not proof of database readiness, uniqueness, account creation or login success.

This moves malformed administrator input rejection before DDL and plan publication.
It does not change password policy or permit browser-controlled roles/permissions.
The new native regression requires an empty database and absent prepared directory
after rejected input.

For source `76f9b3e506bc7b468842b4fa79d3a894406ad3c7`, initial-administrator
run `39978105664` passed, preserving root-only access, one-time creation,
credentials, permissions and audit behavior. Migration, data bootstrap, installer
web, release package and service environment workflows also passed. Payload run
`39978105676` / job `120497936015` passed the actual native preparation regression:
`invalid_admin_rejected_before_plan_or_migration=true`, along with the inherited
browser submission, timer, replay, recovery and completion-lock checks. Original
artifact `12514032627` was independently downloaded and verified: ZIP SHA256
`759dddf16b3fd70652ead09ce6e5cb25013b3d8e7605778cca645c8235c2ad6f`, inner archive
SHA256 `f3f6f51ab59b9f80b255b81866fe9198a01f4d17fe2e3a643e796500d6e7ffcb`.
ZIP integrity, exact-source JSON and archive allowlist checks passed. This does
not close full interactive configuration or target Ubuntu 24.04.4 acceptance.
