# Native migration execution boundary

The package migration engine is an internal PHP 8.3 component, not a public
HTTP endpoint. The privileged installer worker must authenticate the package
manifest first and supply its migration-catalog SHA256, source and version.
Do not accept those bindings from an unauthenticated web request.

MySQL 8, autocommit and a selected dedicated database are mandatory. When binary
logging is enabled, a database administrator must explicitly authorize stored
trigger creation (`log_bin_trust_function_creators=1`) before installation.
The engine checks this before writing schema/journal state and never changes the
server setting or grants itself SUPER. CI configures its disposable native server
explicitly; this does not configure a production database.

All original SQL bytes, ordering, catalog hashes and statement boundaries are
validated before mutation. The immutable 000026 trigger bodies retain their
DELIMITER semantics. Unsupported executable comments/delimiters fail closed.
A MySQL connection-scoped advisory lock serializes migration application. Each
migration records source/version/checksum and durable started/applied/failed
state. MySQL DDL is not rolled back by pretending it is one transaction.

Checksum drift, missing catalog history, interrupted started state or failed DDL
blocks further application. Preserve the error, catalog and journal; restore the
verified pre-upgrade database backup or perform an explicitly reviewed recovery.
Never delete a journal row to force retry and never switch only binaries across
an incompatible schema. The complete backup/restore/upgrade orchestration remains
pending; this engine alone does not satisfy P21 closure or P22 production recovery.

Only fixed error categories, migration ID, SQLSTATE and numeric driver code may
be exposed in diagnostics; SQL statements, credentials and raw database errors
must not be returned to the installer browser.

## Upgrade migration inspection

`Migrations::inspectUpgrade(PDO, currentRoot, currentCatalogHash,
candidateRoot, candidateCatalogHash)` checks the migration boundary before an
upgrade. The privileged caller must first verify both packages and obtain their
catalog hashes from the verified manifests, not from a browser request.

The candidate must retain every current migration's ID, filename, bytes, digest
and rollback rule. Both catalogs and all SQL are validated before the database
is inspected. Under the same MySQL advisory lock as migration application, the
database must contain exactly the current catalog's complete, applied journal.
Missing, extra, interrupted or changed history is rejected. The result lists
pending migration IDs without applying SQL or creating/updating journal rows.
The lock is released on success and failure.

This inspection is a snapshot, not a durable authorization. The upgrade executor
must revalidate packages and database history at execution time. Its result keeps
`upgrade_authorized=false` and `backup_restore_verified=false`: release version
compatibility, consistent backup and restore rehearsal, configuration preservation,
atomic release switching and service recovery remain unimplemented orchestration
requirements. A compatible migration prefix alone never permits a binary rollback.

The native migration workflow exercises a 31-to-32 migration candidate without
executing its new DDL, unchanged catalogs, removed/rewritten migrations, a competing
lock, failed/interrupted journal and active transaction rejection. It compares
the entire journal and table inventory before/after inspection. These tests are
part of the existing MySQL 8 / PHP 8.3 workflow; local Python catalog checks do not
substitute for their native execution.
