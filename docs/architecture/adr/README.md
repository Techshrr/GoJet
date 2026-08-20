# GoJet V10 Architecture Decision Record Index

Architecture decisions are subordinate to the three approved GoJet V10 specification contracts. An ADR may select an implementation detail where the specification allows a choice; it may not weaken or redefine a MUST, security invariant, route contract, design token, capability status, Node dependency, or Gate.

## P00 frozen decisions

| ADR | Decision | Status |
|---|---|---|
| ADR-0001 | Independent Greenfield implementation; no legacy GoJet dependency or migration baseline | Accepted |
| ADR-0002 | Single root Go module `github.com/Techshrr/GoJet`; eight independently buildable binaries | Accepted |
| ADR-0003 | pnpm frontend workspace with specification-defined apps/packages; Node build/test only | Accepted |
| ADR-0004 | Repository-global six-digit forward migration numbering with immutable checksums and explicit rollback boundaries | Accepted |
| ADR-0005 | Server-authoritative API/RBAC/security decisions; stable machine error codes and correlation IDs | Accepted |
| ADR-0006 | Workspace roles `owner/admin/member/viewer`; Admin authorization remains separate | Accepted |
| ADR-0007 | Native production runtime only; Docker/Compose and Node runtime prohibited | Accepted |
| ADR-0008 | Specification-derived traceability snapshots; snapshots never override canonical specifications | Accepted |

## Change discipline

A change to an Accepted P00 decision requires a new ADR that supersedes the prior record and, where applicable, the Master Plan change-control record. Decisions may not be silently edited to make a failing Gate pass.

`DECISION REQUIRED` count at the P00 baseline is **0**. New unresolved decisions must be recorded before their first dependent node begins and must be closed before that node can exit.
