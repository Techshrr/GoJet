# GoJet

GoJet V10 is a greenfield short-link and resource platform implemented from specification first.

GoJet V10 is an independent greenfield product. The V10 designation is a standalone product identity; no earlier GoJet release, repository, branch, commit, migration, generated artifact, runtime configuration, or implementation is a dependency, migration source, compatibility baseline, or normative evidence.

## Greenfield rule

This repository is the only implementation repository for GoJet V10. Prior GoJet repositories, branches, commits, migrations, generated artifacts, Git history, or runtime configuration are not implementation dependencies and are not normative evidence.

The specification pack defines product behavior before code:

1. [`specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md`](specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md) — architecture, capabilities, security invariants, implementation nodes, Gates and release definition.
2. [`specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md`](specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md) — brand, design tokens, components, motion, responsive behavior, accessibility and cross-surface UX patterns.
3. [`specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md`](specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md) — routes, page composition, states, workflows, responsive behavior and SEO contracts.

## Development status

The active implementation branch is `develop/p21-native-release-package`, with
[draft PR #207](https://github.com/Techshrr/GoJet/pull/207). P21 native installation
and release acceptance remain open; the payload is not a production release.

[Current status and remaining work](docs/p21/STATUS.md) is the current project entry
point. The target acceptance system is Ubuntu 24.04.4 LTS x86_64. Final delivery must
contain runtime files only and complete Chinese installation/configuration/recovery
instructions. Do not install an old development artifact as a production candidate.

The planned public brand/domain is Linketic.com. Repository, module and existing
assets retain their GoJet identity; no live GoJet main-domain migration is required.
The [master tracker](https://github.com/Techshrr/GoJet/issues/1), frozen test plans and
signed reviews retain their respective implementation and acceptance authority.

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
