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
