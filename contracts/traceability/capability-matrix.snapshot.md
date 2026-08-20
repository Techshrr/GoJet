# GoJet V10 Capability Matrix Snapshot

Status: `P00 frozen traceability input`  
Canonical authority: `specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md` §2.2–2.3  
Specification ID: `GJ-V10-MP-GREENFIELD-2026-08-20`  
Freeze base commit: `3465148cb77e920141bbd43651ba912832dc2dd4`

This file is a traceability snapshot. It never overrides the Master Plan. A difference between this file and the canonical specification is a G0 review item, not permission to choose the snapshot.

## REQUIRED capabilities

| Capability ID | Owner node(s) | Gate(s) | Status |
|---|---|---|---|
| `CAP-LINKS` | P05 | G3/G6/G10 | REQUIRED |
| `CAP-LINK-ROUTING` | P05/P16 | G3/G6 | REQUIRED |
| `CAP-LINK-AB` | P05/P16 | G3/G6 | REQUIRED |
| `CAP-LINK-UTM` | P05 | G3/G6 | REQUIRED |
| `CAP-LINK-HISTORY` | P05 | G3/G6 | REQUIRED |
| `CAP-OFFICIAL-DOMAINS` | P05/P17 | G3/G6 | REQUIRED |
| `CAP-QR` | P08 | G3/G10 | REQUIRED |
| `CAP-TEXT` | P10 | G3/G7 | REQUIRED |
| `CAP-BIO` | P11 | G3/G7 | REQUIRED |
| `CAP-FILES` | P09/P17 | G3/G6/G10 | REQUIRED |
| `CAP-CLAMAV-REQUIRED` | P09/P22 | G6/G12/G13 | REQUIRED |
| `CAP-ANALYTICS` | P07 | G3/G9/G10 | REQUIRED |
| `CAP-CAMPAIGNS` | P07/P12 | G3 | REQUIRED |
| `CAP-FOLDERS-TAGS` | P12 | G3/G6 | REQUIRED |
| `CAP-WORKSPACE` | P12 | G3/G6/G10 | REQUIRED |
| `CAP-NOTIFICATIONS` | P12/P13-P17 | G3/G5/G6/G10 | REQUIRED |
| `CAP-BILLING` | P13 | G3/G6/G10 | REQUIRED |
| `CAP-PAYMENTS` | P13 | G3/G6/G10 | REQUIRED |
| `CAP-PAYMENT-CALLBACKS` | P13 | G6/G10/G13 | REQUIRED |
| `CAP-TICKETS` | P14 | G3/G6/G10 | REQUIRED |
| `CAP-MAIL` | P14 | G3/G6/G10 | REQUIRED |
| `CAP-AUTH` | P15 | G3/G5/G6/G10 | REQUIRED |
| `CAP-OAUTH` | P15 | G3/G6/G10/G13 | REQUIRED |
| `CAP-TURNSTILE` | P14/P15/P17 | G6/G10/G13 | REQUIRED |
| `CAP-DOMAIN-OWNERSHIP` | P06 | G3/G6 | REQUIRED |
| `CAP-DOMAIN-HTTPS` | P06 | G3/G6 | REQUIRED |
| `CAP-DOMAIN-ENTITLEMENT` | P06/P13/P14/P17 | G6/G10 | REQUIRED |
| `CAP-DOMAIN-RISK` | P06/P16/P17 | G6/G13 | REQUIRED |
| `CAP-DESTINATION-RISK` | P05/P16 | G6/G10/G13 | REQUIRED |
| `CAP-ABUSE` | P16/P17 | G6/G10 | REQUIRED |
| `CAP-ADMIN-ACCESS` | P17 | G3/G6/G10 | REQUIRED |
| `CAP-OPS-AUDIT` | P17 | G3/G6/G13 | REQUIRED |
| `CAP-ANNOUNCEMENTS-SETTINGS` | P17/P19 | G3/G6/G7 | REQUIRED |
| `CAP-API-KEYS` | P17 | G3/G6 | REQUIRED |
| `CAP-USER-WEBHOOKS` | P17 | G3/G6 | REQUIRED |
| `CAP-NATIVE-INSTALL` | P21/P22 | G1/G11/G12/G13 | REQUIRED |
| `CAP-NATIVE-ONLY-RELEASE` | P21/P22 | G1/G11/G12/G13 | REQUIRED |
| `CAP-TECHNICAL-SEO` | P18/P19 | G7/G9/G13 | REQUIRED |

**REQUIRED count: 38.** Every REQUIRED capability has at least one owner node and at least one Gate in the frozen specification.

## REMOVED / DEFERRED disposition

| ID | Status | Release handling |
|---|---|---|
| `DEP-DOCKER-PRODUCTION` | REMOVED | Production Docker/Compose path is prohibited; presence in release/installer/runbook is a hard failure. |
| `DEP-NODE-PRODUCTION` | REMOVED | Node is build/test/package only; production Node runtime is prohibited. |
| `CAP-S3-STORAGE` | DEFERRED | V10 fresh install/release uses local filesystem; S3 is not a release claim. |
| `CAP-BIO-OPT-IN-INDEX` | DEFERRED | Bio remains noindex and excluded from sitemap in V10. |
| `DEP-LEGACY-GOJET-CODE` | REMOVED | Any dependency on prior GoJet implementation/history is a G0/G1 hard failure. |

## Decision state

`DECISION REQUIRED` count at the specification freeze is **0**.

## G0 implementation-column rule

Before a REQUIRED capability can be declared complete, its implementation ledger must cover Backend, DB/Migration, API, UI, RBAC, States, Browser, Security, Observability and Release. Any genuinely non-applicable column must explicitly record `N/A` with a reason; blank cells are not allowed.
