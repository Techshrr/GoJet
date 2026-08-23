# P12 Workspace, Members and Organization — Accountable Technical Review

Node: `P12`  
Issue: #31  
Base integration commit: `638a6988c03eed6d287af0d2fdc63a3a3355ef68`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

Pre-sign implementation commit: `31e67b5093256c306927f99e49924a8ef0ad9f33`  
Accountable reviewer: GPT-5.6 Sol — Workspace/Organization Technical Review  
Review date: `2026-08-24`

## 1. Review boundary

This signed review covers the P12 Workspace, Members and Organization node only. It reviews the completed P12 implementation and its exact-head evidence against the frozen P12 contract. It does not claim completion of P15 authentication/session/OAuth identity lifecycle, later P13-P17 notification business producers, or release-wide gates beyond the P12-owned subsets.

P12 continues to consume the current authenticated-principal boundary while resolving Workspace membership and role from authoritative MySQL membership data on every P12-owned request. Client-side role/navigation visibility and `X-GoJet-Test-Workspace-Role` are not P12 authorization authority.

## 2. Capability and ownership disposition

- `CAP-WORKSPACE` — P12-owned G3/G6/G10 subset: APPROVED.
- `CAP-FOLDERS-TAGS` — P12-owned G3/G6 subset: APPROVED.
- `CAP-CAMPAIGNS` — shared P07/P12 G3 governance: APPROVED for P12; P07 analytics/conversion authority remains inherited and is not redefined.
- `CAP-NOTIFICATIONS` — P12 core G3/G5/G6/G10 store/read-state/API/UI/dedupe/deep-link/redaction subset: APPROVED; P13-P17 notification producers remain later-owned.
- P15 identity lifecycle remains outside P12 closure.

Inherited predecessor authority remains the signed P11 closure at source `b59dfbe794f7d2f7bf63fdc79116217c5d893e87`, run `32649713397`, artifact `9495896748`, digest `sha256:fe0edc8308cb4520929590efb261b87052423805ef02099066e818ff4cc5ae4f`, with `phase=signed`, `merge_authoritative=true`, and defects 0/0/0.

## 3. Route and API review

The reviewed P12 route set is:

```text
/app
/app/notifications
/app/organization
/app/campaigns
/app/tags
/app/members
/invite/{token}
/app/settings/workspace
```

There is no `/app/folders` route. Folder behavior remains resource-internal organization capability.

The IA-exact dependencies `GET /api/workspaces/{id}/overview`, `GET /api/workspaces/{id}/notifications`, `POST /api/invitations/accept`, and `POST /api/invitations/reject` are covered. P12 safe invitation inspection remains implementation authority and does not disclose raw invited email, token hash, raw token, or token existence to unauthenticated callers.

## 4. RBAC, tenant and invitation review

The owner/admin/member/viewer matrix, cross-Workspace isolation, server-side membership re-resolution, admin owner-boundary restrictions, no self-escalation, and last-active-owner transactional protection are approved from exact-head evidence.

Invitation create/list/revoke/inspect/accept/reject behavior is approved, including normalized account matching, expiry/revoke/single-use behavior, owner-invite prohibition, transactionally unique membership creation, and raw-token non-persistence/non-logging.

## 5. Organization, campaign, tag and folder review

Workspace and organization optimistic concurrency, P07 campaign ID continuity, same-Workspace association validation, Unicode-normalized tag/folder uniqueness, explicit Link-ID bulk organization, filter behavior, and in-use deletion semantics are approved. Existing P05 Link authority and inherited P07 analytics authority remain intact.

## 6. Notification core review

The P12 notification core is approved for categories `security`, `domains`, `billing`, `support`, and `resources`. Evidence covers internal-only producer behavior, recipient+dedupe-key idempotency, recipient-scoped read/unread/read-all, complete/partial/stale source state, safe deep-link allowlisting and read-time reauthorization, fallback behavior, and sensitive title/summary/context/deep-link/audit redaction.

No user-facing arbitrary notification producer API is introduced. Later P13-P17 producers remain outside this approval.

## 7. Workspace UI, accessibility and offline review

The reviewed browser routes and shell behavior passed real API/browser evidence across canonical desktop/tablet/mobile viewports plus 320 CSS px reflow and reduced motion. Workspace switcher authority, settings, members/invitations, organization/campaign/tag/folder controls, notification badge/popover/full history, category filtering, read-state actions, Esc close, focus return, mobile full-height Sheet, no stacked overlays, and API offline/recovery states are approved.

The P03 Design System regression discovered during pre-sign preparation was corrected in product CSS using governed design tokens and intrinsic reflow; P03 exact-head validation subsequently passed together with P12 browser evidence.

## 8. Exact-head evidence disposition

The pre-sign implementation commit `31e67b5093256c306927f99e49924a8ef0ad9f33` passed:

- P12-T001..P12-T018 real MySQL/Redis/native platformapi integration evidence.
- P12-T019..P12-T023 real Workspace browser evidence.
- P12-T024 exact-head evidence coherence with 23 input cases, producer artifact bindings, nine inspectable captures, and clean runtime error files.
- P12-T025 pre-sign closure with 24 input evidence files and a 35-workflow affected exact-head matrix.
- P03 Design System exact-head regression validation.

Pre-sign closure run: `32661594987`.  
Pre-sign closure artifact: `9498965973`.  
Pre-sign closure artifact digest: `sha256:004e2b5abbea1855e842b3f97de87d4a6854e471ac530d3ff99424ef0f2aa3d0`.

The pre-sign T025 state is intentionally `phase=pre-sign` and `merge_authoritative=false`. This review-only child must rerun the same-revision affected matrix and P12-T001..P12-T025 before signed closure becomes merge-authoritative.

## 9. Defect and decision ledger

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0

No unresolved P0/P1 or decision-required item remains within the frozen P12 ownership boundary.

## 10. Accountable approvals

- Backend Lead: APPROVED
- Frontend Lead: APPROVED
- QA Lead: APPROVED
- Accessibility Reviewer: APPROVED
- Security Reviewer: APPROVED
- Product/API Reviewer: APPROVED

The review approves P12 as implemented at the pre-sign parent, subject to the signed-revision rule below. It does not transfer ownership from P07, P15, or P13-P17 and does not broaden P12 beyond its frozen capability/gate subsets.

## 11. Signed-revision rule

This file is the only intended change in the signed review child. Because signing changes HEAD, this signed child is **not merge-authoritative merely because this document says APPROVED**.

The signed HEAD itself must rerun and pass the complete affected exact-head matrix, P03 Design System, P12 Contract, P12-T001..P12-T023 producers, P12-T024 coherence, and P12-T025 signed closure. Only the resulting T025 state `phase=signed`, `merge_authoritative=true`, defects P0=0/P1=0/`DECISION REQUIRED`=0 on that same signed HEAD authorizes P12 completion or merge.
