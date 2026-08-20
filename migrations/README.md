# GoJet V10 Migration Catalog

Authority: P00 baseline and `GJ-V10-MP-GREENFIELD-2026-08-20`.

## Naming

Every Greenfield MySQL migration uses one repository-global, strictly increasing six-digit ID:

```text
000001_<short_slug>.sql
000002_<short_slug>.sql
```

## Invariants

- IDs are unique and monotonic across the entire repository.
- Published/applied migrations are immutable; corrections use a new migration.
- Every release migration catalog records SHA-256 for each migration.
- Application is serialized under a global migration lock.
- Applied state records migration ID, checksum, release/commit identity, timestamps and result.
- A migration declares whether rollback is mechanically safe. Irreversible changes are explicit and rely on the tested database backup/restore boundary instead of a fake down migration.
- Empty-database full migration and catalog consistency are release requirements.
- No migration may be copied or adapted from any prior GoJet repository as implementation evidence.

The first schema migration is owned by the node that first introduces persistent application state; P00 freezes the convention only.
