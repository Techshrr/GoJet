# Root data bootstrap

`php8.3 installer/bin/data-bootstrap.php /absolute/root-job-directory` runs the
native database migration and inherited initial administrator commands in order.
This is the data phase only: it does not configure/start services, switch releases,
perform final health verification, or write `installed.lock`.

The root operator must provision a canonical root-owned 0700 job directory and a
single-link, regular 0600 `request.json`. The strict request schema is
`gojet.root-data-bootstrap.v1`; its exact fields and bounds are defined by
`DataBootstrap::plan`. It includes the root-approved release directory, source and
version, catalog and platformapi SHA-256 digests, local MySQL/Redis settings,
initial administrator credentials, Admin encryption key and HTTPS origin.
The fixture in `scripts/p21/native_data_bootstrap_test.py` shows the schema with
CI-only credentials. Never copy CI credentials into an installation.

Request digests are trust inputs approved by root, not signatures. Provision the
release from a verified archive before approving them. The worker checks root
ownership, write permissions, canonical paths, original SQL hashes and the native
binary's offline release identity before database mutation. Dependencies currently
use loopback TCP only. The web/FPM identity cannot provision this root request;
web-to-root approval and the complete installation executor remain pending.

An exclusive job lock and fsynced atomic journal serialize each job. Migration
execution also uses its existing database lock and durable per-migration journal.
Admin creation retains the existing database lock, transaction, password hashing,
permissions and audit authority. Secrets pass through private input/environment,
never command arguments, public output or the journal. Keep the request private
through the recovery retention period and remove it according to operator policy.

Only the identical completed request returns an idempotent data-phase result.
Failure or interruption is a recovery barrier, including an administrator commit
followed by a journal write failure. Do not delete/reset the journal to retry.
Inspect the private phase, original migration journal and Admin audit; verify a
backup/restore or otherwise operator-approved recovery before creating another
job. There is no automatic DDL rollback or administrator password reset.

The native CI fixture verifies all 31 original migrations followed by one actual
administrator/audit creation, repeat no-op, rejected inputs/lock/non-root access,
and real Redis failure after migrations with blocked retries. The interruption test holds a real MySQL global read lock in its disposable
CI instance, observes the worker waiting on native DDL, sends SIGKILL, and
verifies that the original durable journal blocks retries after releasing the
database lock. No synthetic progress journal is substituted. None of
these checks constitutes complete P21 installation or G11 acceptance.
