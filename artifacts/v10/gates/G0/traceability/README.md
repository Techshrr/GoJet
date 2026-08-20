# G0 — Scope and Traceability Evidence

Accountable roles: Product Owner + Backend Lead  
Current status: **PENDING — NOT SIGNED**

Canonical inputs:

- `specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md`
- `specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md`
- `contracts/traceability/capability-matrix.snapshot.md`
- `contracts/traceability/route-registry.snapshot.md`

The P00 CI validator writes commit-bound machine-readable coverage evidence into this directory in its Actions artifact. Generated JSON is intentionally not committed as static proof because the evidence must reference the exact implementation commit being tested.

G0 may be marked PASS only when capability/route/status/owner coverage reconciles with the canonical specifications, no REQUIRED capability is falsely declared complete, `DECISION REQUIRED` is zero, all P00 required tests pass and the accountable roles sign the exact evidence set.
