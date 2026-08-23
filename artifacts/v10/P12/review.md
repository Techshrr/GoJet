# P12 Workspace, Members and Organization — Accountable Technical Review

Node: `P12`  
Issue: #31  
Base integration commit: `638a6988c03eed6d287af0d2fdc63a3a3355ef68`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**

## 1. Review boundary

This file freezes the P12 review contract before implementation. It is not a PASS record and does not close P12, P15 authentication/session/OAuth, later P13-P17 notification producers, or release-wide Gates.

Client-side role/navigation visibility, a test-auth role header, manually inserted membership rows, fixture-only notifications, screenshot-only proof, or a deep link that bypasses current recipient authorization cannot substitute for P12 evidence.

## 2. Frozen capability and ownership boundary

- `CAP-WORKSPACE` — REQUIRED — owner P12 — G3/G6/G10.
- `CAP-FOLDERS-TAGS` — REQUIRED — owner P12 — G3/G6.
- `CAP-CAMPAIGNS` — REQUIRED — shared P07/P12 — G3. P12 owns Workspace campaign governance and identity; P07's completed analytics/conversion authority remains inherited and is not redefined.
- `CAP-NOTIFICATIONS` — REQUIRED — P12/P13-P17 — G3/G5/G6/G10. P12 owns the core store/read-state/API/UI, event schema, dedupe and reusable producer contract. P13-P17 later add their owned producer events without redefining the core.
- P12 consumes the current authenticated-principal boundary but does **not** implement or claim P15 login/session/OAuth identity lifecycle.
- Production Docker/Compose/Node runtime remains prohibited.

Inherited predecessor signed authority is P11 source `b59dfbe794f7d2f7bf63fdc79116217c5d893e87`, integration commit `638a6988c03eed6d287af0d2fdc63a3a3355ef68`, closure run `32649713397`, artifact `9495896748`, digest `sha256:fe0edc8308cb4520929590efb261b87052423805ef02099066e818ff4cc5ae4f`, `phase=signed`, `merge_authoritative=true`, defects P0=0/P1=0/`DECISION REQUIRED`=0.

## 3. Frozen route authority

IA-authoritative P12 routes:

```text
APP-OVERVIEW       /app
APP-NOTIFICATIONS  /app/notifications
APP-ORGANIZATION   /app/organization
APP-CAMPAIGNS      /app/campaigns
APP-TAGS           /app/tags
APP-MEMBERS        /app/members
AUTH-INVITE         /invite/{token}
APP-SETTINGS        /app/settings/workspace   (P12-owned Workspace subset only)
```

IA-exact API dependencies used by P12:

```text
GET  /api/workspaces/{id}/overview
GET  /api/workspaces/{id}/notifications
POST /api/invitations/accept
POST /api/invitations/reject
```

P12 additionally freezes `GET /api/invitations/{token}` as **P12 implementation authority, not IA-exact**. Unauthenticated inspection returns an authentication-required response without disclosing whether a token exists. Authenticated inspection exposes only safe Workspace name/role/status/expiry/account-match state and never returns the raw invited email, token hash or another secret.

The remaining Workspace/member/organization/campaign/tag/folder/notification HTTP family in `test-plan.json` is **P12 current-repository implementation authority**, not IA-exact authority.

There is **no `/app/folders` route** in the IA. P12 must implement folders as resource-internal organization data/API/filter/bulk-association behavior and must not create a legacy/compatibility page alias.

`APP-SETTINGS` is mixed ownership. P12 may implement/verify only the Workspace-owned settings context required by `CAP-WORKSPACE`; profile/security/sessions/connected-accounts remain P15.

## 4. Frozen identity, tenant and RBAC authority

1. Authentication principal identity comes from the current authentication boundary. In isolated CI, `GOJET_TEST_AUTH_ENABLED=1` may provide deterministic actor identity only.
2. Workspace membership and role are server-authoritative P12 MySQL data and are re-resolved on every P12-owned request.
3. `X-GoJet-Test-Workspace-Role` or any client/UI role claim is not authorization authority for P12-owned APIs.
4. Roles are `owner`, `admin`, `member`, `viewer`.
5. Owner has full P12 authority but the last active owner cannot be removed/demoted.
6. Admin may manage Workspace metadata/organization/campaign/tag/folder and admin/member/viewer membership/invitations, but may not grant, demote or remove owner authority or self-escalate.
7. Member may read Workspace/member context and manage campaign/tag/folder/Link-organization data, but cannot manage Workspace settings, roles or invitations.
8. Viewer is read-only.
9. Invitations may grant `admin`, `member` or `viewer`, never `owner`. Owner promotion happens only after active membership by an existing owner.
10. Cross-Workspace IDs cannot substitute ownership and denial must not leak existence.

This freezes P12 authorization semantics without claiming P15 production session/token lifecycle.

## 5. Frozen invitation authority

- Invitation tokens are opaque secrets; only a cryptographic hash is persisted.
- Invitation lookup/accept/reject is bound to Workspace, normalized invited email, role, expiry and status.
- States: `pending`, `accepted`, `rejected`, `revoked`, `expired`.
- Accept requires the authenticated account identity to match the invitation email; mismatch fails closed.
- Accepted/rejected/revoked/expired tokens cannot be reused.
- A successful accept creates exactly one membership transactionally.
- Raw invitation tokens must not appear in logs, audit metadata, notification summaries or ordinary API list responses.

## 6. Frozen organization, campaign, tag and folder authority

- Workspace/organization mutable data is versioned and validated.
- Campaign IDs are opaque same-Workspace identifiers. They are the same `campaign_id` values used by the already completed P07 analytics/conversion dimensions; P12 must not create a second analytics campaign namespace.
- Tag and folder names have same-Workspace normalized uniqueness.
- Folders are not a standalone page route.
- Campaign/folder/tag association with P05 Links is server-validated to one Workspace.
- Bulk organization mutations use explicit Link IDs. Filter/query changes cannot silently widen the mutation set.
- Existing P05 Link CRUD/risk authority and P07 analytics authority remain intact.

## 7. Frozen notification core authority

Categories: `security`, `domains`, `billing`, `support`, `resources`.

- Producer events are server-internal; no user-facing endpoint can fabricate arbitrary notifications.
- Same-recipient `dedupe_key` makes producer replay idempotent.
- `read_at` is per recipient; read/unread/mark-all-read cannot mutate another user or Workspace.
- Header badge includes numeric state and an accessible label.
- Header popover shows safe recent notifications and `View all`; full history is `/app/notifications`.
- Full page is a grouped timeline/list with All / Security / Domains / Billing / Support / Resources filtering.
- Deep links are allowlisted registered GoJet routes and must be re-authorized for the current recipient at read/render time. Unauthorized/stale targets are omitted or safely fall back to `/app/notifications`.
- Notification title/summary/context/deep-link/audit data must not expose passwords, invitation tokens, session/access/OAuth/payment/webhook secrets, raw PII, provider evidence or risk secrets.
- Read state may be optimistic; security/billing/resource facts may not be fabricated optimistically.
- Notification source state explicitly distinguishes `complete`, `partial`, `stale`; stale/partial/offline cannot be presented as current success.
- P13-P17 add their own producer events later; P12 does not claim those business modules complete.

## 8. Frozen Workspace/browser state contract

`APP-OVERVIEW`: `loading`, `empty-new-workspace`, `partial-analytics`, `attention`, `API-error`.

`APP-NOTIFICATIONS`: `loading`, `empty`, `unread`, `filtered`, `partial`, `stale`, `error`.

`APP-ORGANIZATION`: `loading`, `success`, `read-only`, `validation-error`, `conflict`, `error`.

`APP-CAMPAIGNS`: `loading`, `empty`, `edit`, `read-only`, `conflict`, `error`.

`APP-TAGS`: `loading`, `empty`, `edit`, `read-only`, `conflict`, `in-use`, `error`.

`APP-MEMBERS`: `loading`, `empty-no-invites`, `invite`, `read-only`, `last-owner-protected`, `invitation-expired`, `error`.

`AUTH-INVITE`: `unauthenticated`, `valid`, `account-mismatch`, `expired`, `revoked`, `accepted`, `rejected`.

WorkspaceSwitcher, Command and Notifications must obey Esc close, focus return, mobile full-height Sheet and no overlay stacking. P12 browser evidence must use real route/API behavior, canonical responsive viewports, 320 CSS px reflow and reduced motion.

## 9. Evidence and case contract

Required P12 case range: **P12-T001..P12-T025**.

The `P12-Txxx` identifiers are frozen by this P12 contract revision. The Master Plan supplies the requirements—not these IDs.

Evidence root: `artifacts/v10/P12/`.

Required specialized evidence includes:

- `artifacts/v10/P12/api/`
- `artifacts/v10/P12/db/`
- `artifacts/v10/P12/rbac/`
- `artifacts/v10/P12/audit/`
- `artifacts/v10/P12/security/`
- `artifacts/v10/P12/browser/`
- exact-head evidence index / producer bindings
- accountable signed affected-regression closure

P12 evidence must cover Backend, DB/Migration, API, UI, RBAC, States, Browser, Security, Observability and Release; any genuinely non-applicable column must record `N/A` with a reason.

## 10. Pending implementation review

Pending: P12-T001..P12-T025, exact implementation SHA, real MySQL/platformapi membership and invitation evidence, server-side role resolution, campaign/P07 continuity, Link organization filters, notification producer/read/deep-link/redaction/partial-state evidence, Workspace/Auth browser/mobile evidence, affected exact-head matrix, P0/P1 ledger and unresolved `DECISION REQUIRED` count.

No P12 PASS or Exit claim is made in this state.

## 11. Signed-revision rule

When evidence is complete this document may transition only to:

`Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**`

The signed form must record the 40-hex pre-sign implementation commit, P12-T001..P12-T025 PASS evidence, accountable reviewer identity/date, P0=0, P1=0, unresolved `DECISION REQUIRED`=0, truthful P12 gate/capability disposition, P07/P15/P13-P17 ownership boundaries and same-revision CI/closure rerun requirement.

If signing changes this file and therefore changes HEAD, the signed revision itself must rerun and pass the complete affected exact-head matrix before P12 can be marked complete or merged.
