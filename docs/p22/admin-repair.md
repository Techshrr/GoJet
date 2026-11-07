# Native administrator repair candidate — 10.0.0-rc.2

This candidate fixes native administrator authentication across Trust & Safety,
Commerce, Tickets and Mail, plus the first-install platform settings state.
The original rc.1 release and installer completion/retirement records remain intact.
It is an upgrade candidate, not a claim that the user's host has been upgraded.

## What changed

- Trust destination/domain/abuse routes use the dedicated administrator session,
  exact P17 permissions, Origin and CSRF verification. Request authority is not
  shared across users. Existing business safety decisions and audit remain.
- Commerce admin routes use administrator identity independently of workspace
  billing. Native Support/Commerce pages use real session state and CSRF.
- Unset platform general/brand settings return version zero. General settings
  can be saved through the existing version check, audit and fresh TOTP boundary.
- Users, Workspaces and Jobs show an explicit empty state. Authentication,
  permission denial and pending session checks are distinguished.

## Validation and limits

The companion CI tests the production admin build with a real migrated MySQL
and Redis, cookie login, TOTP enrollment, desktop/mobile navigation, settings,
plan and mail-settings mutations. It rejects anonymous/workspace credentials,
revoked permissions/sessions, invalid CSRF and Origin. No real mail or payment
is sent. Inspect the workflow result for this exact source; do not infer PASS
from the presence of an archive alone.

The archive includes independently hashed payload and source, with a manifest,
SBOM, and native executable version checks. Existing migration files are unchanged. New migration 000032 appends
`google_one_tap` to the settings key ENUM, preserving general/brand ordinals.
The CI exercises the 31-to-32 managed migration with existing settings,
verified backup, restore rehearsal and actual restore. Real host upgrade, reboot and business acceptance are
separate observations, not substituted by this CI result.

## Upgrade, preserving the existing installation

1. Run `sha256sum --check SHA256SUMS` in the extracted package directory. Read
   `verification.json` for `manifest_sha256`, source and archive digest. Verify
   the payload against those exact values using its package verifier.
2. Extract the payload into a **new** `/opt/gojet/releases/10.0.0-rc.2-<source>`
   directory. Do not overwrite rc.1, copy individual binaries into rc.1, delete
   completion locks, run bootstrap, or reactivate the retired installer.
3. Use `runbooks/upgrade-recovery.md` inside the payload. Confirm the existing
   sealed prepared directory and create a private database administration
   connection with the permissions required for backup/restore. Application
   credentials alone are not assumed to have these rights.
4. Prepare a new upgrade job with `installer/bin/prepare-upgrade.php`; retain
   its returned plan SHA. Review that concrete plan before execution. The
   official upgrade executor backs up and rehearses restore, retains migration
   prefix checks, rebuilds risk projections, switches releases, checks all eight
   services and HTTPS, and commits only on success.
5. If execution fails, preserve the same job and use its documented recovery
   command only after the original executor stops. Do not recreate the job or
   fabricate completion records.
6. Verify admin navigation on the upgraded host. Settings changes require
   recent TOTP verification: enroll through Access, then sign in with the code.
   Keep the Web Risk adapter and its credential file unchanged.

This package does not authorize a production deployment. Use the dedicated
acceptance host first and preserve its evidence and rollback material.
