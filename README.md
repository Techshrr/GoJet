# GoJet

GoJet V5 is a greenfield short-link and resource platform implemented from specification first.

## Greenfield rule

This repository is the only implementation repository for GoJet V5. Prior GoJet repositories, branches, commits, migrations, generated artifacts, Git history, or runtime configuration are not implementation dependencies and are not normative evidence.

The specification pack defines product behavior before code:

1. [`specifications/GoJet_V5_MASTER_PLAN_OPTIMIZED.md`](specifications/GoJet_V5_MASTER_PLAN_OPTIMIZED.md) — architecture, capabilities, security invariants, implementation nodes, Gates and release definition.
2. [`specifications/GoJet_V5_BRAND_DESIGN_SYSTEM_OPTIMIZED.md`](specifications/GoJet_V5_BRAND_DESIGN_SYSTEM_OPTIMIZED.md) — brand, design tokens, components, motion, responsive behavior, accessibility and cross-surface UX patterns.
3. [`specifications/GoJet_V5_PAGE_LEVEL_IA_OPTIMIZED.md`](specifications/GoJet_V5_PAGE_LEVEL_IA_OPTIMIZED.md) — routes, page composition, states, workflows, responsive behavior and SEO contracts.

## Product surfaces

- Website
- Auth
- Docs
- Workspace
- Admin
- Installer
- Public resource, error and safety surfaces

## Core product capabilities

Links and smart routing, QR, file sharing with mandatory ClamAV, text sharing, Link in Bio, analytics, campaigns/tags, custom domains and entitlement, workspace/RBAC, notifications, billing/payments, support/mail, authentication/OAuth/Turnstile, API keys, outbound webhooks, destination/domain risk, abuse handling, operations/audit, technical SEO and native-only deployment.

## Delivery model

Implementation follows P00–P22 and must satisfy G0–G13. A capability is not considered complete because it existed elsewhere or because a UI mock exists; it is complete only when the current repository implementation and required evidence pass its Gates.
