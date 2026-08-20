# GoJet V10 P00 Bootstrap Baseline

Status: `P00 / IN PROGRESS`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`  
Tracking: GitHub Issues #1 and #2

## 1. Product identity

GoJet V10 is an independent Greenfield product implemented only from the current specification pack in `Techshrr/GoJet`.

No earlier GoJet repository, release, branch, commit, migration, generated artifact, runtime configuration or implementation is a dependency, migration source, compatibility baseline, or normative evidence.

## 2. Repository boundaries

The target repository layout is frozen as follows. P00 establishes boundaries; implementation files are introduced by their owning nodes.

```text
/
├── specifications/                 normative product contracts
├── services/                       Go service source
│   ├── redirectengine/cmd/server
│   ├── analyticsworker/cmd/worker
│   ├── analyticsreconciler/cmd/reconciler
│   ├── platformapi/cmd/server
│   ├── platformapi/cmd/mailworker
│   ├── platformapi/cmd/fileworker
│   ├── platformapi/cmd/operationsmonitor
│   └── logreceiver/cmd/server
├── frontend/
│   ├── apps/site                   Website + Auth
│   ├── apps/docs                   static Docs
│   ├── apps/workspace              customer Workspace
│   ├── apps/admin                  governance/Admin
│   └── packages/
│       ├── ui
│       ├── tokens
│       ├── api-client
│       ├── auth
│       ├── charts
│       ├── icons
│       ├── domain
│       ├── motion
│       └── utils
├── installer/                      PHP 8.3 FPM installer source
├── migrations/                     Greenfield MySQL migration catalog
├── deploy/
│   ├── nginx                       native Nginx templates
│   └── systemd                     eight independent service units
├── config/                         non-secret configuration examples/schema
├── contracts/                      frozen/derived implementation contracts
├── docs/
│   ├── architecture
│   ├── engineering
│   └── security
├── scripts/                        development/evidence/release helpers
└── artifacts/v10/                  node and Gate evidence
```

Production `Dockerfile`, Compose files, `deploy/docker/`, PM2, Node HTTP/SSR servers and production `node_modules` are prohibited.

## 3. Go module strategy

GoJet V10 uses **one root Go module**: `github.com/Techshrr/GoJet`.

All eight required binaries live in the same module and remain independently buildable executables. A multi-module repository is not used unless a future approved ADR demonstrates a concrete isolation requirement without weakening reproducibility or Gate evidence.

Service packages must not import frontend, installer, artifact, or deployment directories. Shared Go packages, when introduced, must live under explicit `internal/` package boundaries and must not collapse the eight executables into a single supervisor process.

## 4. Frontend workspace strategy

Development/build stack is frozen to the specification target:

- pnpm workspace
- React 19
- TypeScript strict
- Vite
- Tailwind CSS v4
- TanStack Router / Query / Table
- React Hook Form
- Zod
- Recharts
- Motion for React
- Sonner
- cmdk
- Lucide
- Astro Starlight + Pagefind for Docs

Node exists only in development/build/test/package environments. Production runtime remains Nginx + native Go binaries + PHP 8.3 FPM installer + MySQL 8.x + Redis + ClamAV + local filesystem.

Frontend package boundaries follow the paths above. Business applications must consume the shared design tokens, UI package and API client rather than defining private colors, spacing, motion or ad-hoc transport clients.

## 5. Migration numbering contract

Greenfield migrations use a repository-global, strictly increasing six-digit identifier:

```text
migrations/000001_<short_slug>.sql
migrations/000002_<short_slug>.sql
```

Rules:

1. IDs are never reused, renumbered or edited after an applied release artifact is published.
2. Each migration has a SHA-256 recorded by the migration catalog.
3. Application is serialized under a global migration lock.
4. Applied state records ID, checksum, start/end time, result and release/commit identity.
5. Rollback capability is declared per migration; irreversible migrations must be explicit and rely on the release backup/restore contract rather than a fictitious down migration.
6. P21 must prove catalog completeness, unique numbering, empty-database application and rollback boundaries.

## 6. API and error conventions

HTTP APIs are server-authoritative. Authentication, authorization, workspace isolation, quota, payment state, destination/domain risk, file security, entitlement, rate limits and audit decisions are never delegated to the browser.

Error responses use a stable machine code and correlation ID:

```json
{
  "error": {
    "code": "STABLE_MACHINE_CODE",
    "message": "Safe user-facing summary",
    "request_id": "opaque-correlation-id",
    "details": {}
  }
}
```

Rules:

- HTTP status reflects the actual protocol outcome; successful-looking `200` errors are prohibited.
- `code` is stable and localization-independent.
- `message` and `details` must not expose secrets, provider credentials, raw tokens, internal stack traces or unsafe destination evidence.
- write endpoints that can be retried by an external caller must define idempotency behavior before implementation.
- all security-sensitive denial paths must be auditable with a correlation ID.

## 7. RBAC vocabulary

Workspace roles are frozen to:

- `owner`
- `admin`
- `member`
- `viewer`

The backend is the authority. UI visibility is convenience only. Every resource access must enforce workspace ownership/tenant boundaries. The last owner cannot be removed or downgraded through a sequence that leaves the Workspace ownerless.

Admin authorization is a separate permission system and must not infer governance privileges from Workspace roles.

## 8. Security invariants

The P00 security baseline is fail-closed:

- cross-Workspace resource access is denied server-side;
- IDs never substitute for ownership checks;
- secrets are never committed, logged, placed in evidence, frontend bundles or public configuration;
- ClamAV missing, unhealthy, stale, timed out or indeterminate never permits file publication and ultimately blocks production installation where required;
- destination/domain risk missing, stale, malformed, review or block states never resolve to a customer target;
- custom-domain entitlement, ownership, DNS ingress, HTTPS readiness and risk are independent enforced states;
- support tickets cannot themselves grant domain entitlement;
- production Docker/Compose/Node runtime paths are prohibited;
- public downloads must pass the Go authorization path; local storage is outside the web root;
- audit records must attribute security-sensitive state changes without storing secrets.

## 9. Traceability

Canonical capability status/owner/Gate data remains owned by the Master Plan. Canonical browser routes remain owned by the Page-Level IA Route Registry.

P00 creates committed snapshots under `contracts/traceability/` and verifies that they correspond to the three V10 specification IDs. No snapshot may override the specification authority.

## 10. Evidence and exit discipline

P00 evidence root: `artifacts/v10/P00/`  
G0 evidence root: `artifacts/v10/gates/G0/traceability/`

P00 is not complete merely because files exist. Exit requires all required tests to pass, evidence to be reviewable, G0 sign-off, zero P0/P1 defects and zero unresolved `DECISION REQUIRED` items.
