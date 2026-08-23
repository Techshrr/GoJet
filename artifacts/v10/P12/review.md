# P12 Workspace, Members and Organization — Accountable Technical Review

Node: `P12`  
Issue: #31  
Base integration commit: `638a6988c03eed6d287af0d2fdc63a3a3355ef68`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

Pre-sign exact implementation SHA: `d239e3c5af32fbf82121b09d0051f123b9de078d`  
Accountable reviewer identity: **GPT-5.6 Sol — P12 Technical Review**  
Review date: **2026-08-24**

## 1. Review boundary

This signed review covers the P12 Workspace, Members and Organization node against the frozen P12 contract. Required P12 case range: **P12-T001..P12-T025**.

P12 consumes the authenticated-principal boundary but does **not** implement or claim P15 authentication/session/OAuth identity lifecycle. P15 remains the later owner of production identity lifecycle. P13-P17 remain the later owners of their business notification producers. P07 analytics/conversion authority remains inherited and is not redefined by P12.

## 2. Capability and gate disposition

- `CAP-WORKSPACE` — P12-owned G3/G6/G10 subset: APPROVED.
- `CAP-FOLDERS-TAGS` — P12-owned G3/G6 subset: APPROVED.
- `CAP-CAMPAIGNS` — shared P07/P12 G3 governance: APPROVED for P12; P07 analytics authority remains inherited.
- `CAP-NOTIFICATIONS` — P12 core G3/G5/G6/G10 store/read-state/API/UI/dedupe/deep-link/redaction subset: APPROVED; P13-P17 producer events remain later-owned.

Inherited predecessor authority remains P11 signed source `b59dfbe794f7d2f7bf63fdc79116217c5d893e87`, closure run `32649713397`, artifact `9495896748`, digest `sha256:fe0edc8308cb4520929590efb261b87052423805ef02099066e818ff4cc5ae4f`, `phase=signed`, `merge_authoritative=true`, defects 0/0/0.

## 3. Route and API disposition

Reviewed routes are `/app`, `/app/notifications`, `/app/organization`, `/app/campaigns`, `/app/tags`, `/app/members`, `/invite/{token}`, and the P12-owned Workspace settings subset `/app/settings/workspace`.

There is no `/app/folders` route. Folder behavior remains resource-internal organization capability.

The IA-exact API dependencies are covered. `GET /api/invitations/{token}` remains P12 safe-inspection implementation authority: unauthenticated inspection does not disclose token existence, while authenticated inspection exposes only safe Workspace name/role/status/expiry/account-match state and never raw invited email, token hash, raw token, or another secret.

## 4. RBAC, tenant and invitation disposition

Exact-head evidence approves the owner/admin/member/viewer matrix, cross-Workspace isolation, MySQL membership/role re-resolution on every P12-owned request, admin owner-boundary restrictions, no self-escalation, last-active-owner transactional protection, invitation owner-grant prohibition, account matching, expiry/revoke/single-use behavior, and transactionally unique membership creation.

Client-side navigation state and `X-GoJet-Test-Workspace-Role` are not P12 authorization authority.

## 5. Organization, campaign, tag and folder disposition

Workspace and organization optimistic concurrency, P07 campaign-ID continuity, same-Workspace association validation, Unicode-normalized tag/folder uniqueness, explicit Link-ID bulk organization, filter behavior, and in-use deletion semantics are approved. Existing P05 Link authority and inherited P07 analytics authority remain intact.

## 6. Notification-core disposition

The P12 notification core is approved for `security`, `domains`, `billing`, `support`, and `resources`. Evidence covers internal-only producer behavior, recipient+dedupe-key idempotency, recipient-scoped read/unread/read-all, `complete`/`partial`/`stale` source state, deep-link allowlisting plus recipient reauthorization, safe fallback, and sensitive title/summary/context/deep-link/audit redaction.

No user-facing arbitrary-notification producer API was introduced. P13-P17 producer events remain outside this P12 approval.

## 7. UI, accessibility and offline disposition

The real Workspace browser evidence passed desktop/tablet/mobile coverage, 320 CSS px reflow, reduced motion, Esc close, focus return, mobile full-height Sheet, no overlay stacking, API offline/recovery, Workspace switching/settings, member/invitation flows, organization/campaign/tag/folder controls, and notification badge/popover/history/filter/read-state behavior.

The P03 Design System regression found during closure preparation was corrected in product CSS using governed tokens and intrinsic reflow; the corrected exact-head P03 run passed together with P12 browser evidence.

## 8. Exact-head evidence disposition

Pre-sign exact implementation SHA `d239e3c5af32fbf82121b09d0051f123b9de078d` passed:

- P12-T001..P12-T018 real MySQL/Redis/native platformapi evidence.
- P12-T019..P12-T023 real Workspace browser evidence.
- P12-T024 exact-head evidence coherence.
- P12-T025: PASS in pre-sign closure with 24 input evidence files and the required 35-workflow exact-head regression matrix.
- P03 Design System exact-head regression validation.

Pre-sign closure run: `32662724956`.  
Pre-sign closure artifact: `9499228990`.  
Pre-sign closure artifact digest: `sha256:b9c609b681404510ea89753cd57c88ca4cba7fc1d6c6c9c804353e79bf1b2c6e`.

The pre-sign closure is intentionally `phase=pre-sign` and `merge_authoritative=false`. This signed review records accountable approval but does not itself bypass the same-revision closure gate.

## 9. Defect and decision ledger

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0

No unresolved P0, P1, or decision-required item remains inside the frozen P12 ownership boundary.

## 10. Accountable approvals

- Backend Lead: APPROVED
- Frontend Lead: APPROVED
- QA Lead: APPROVED
- Accessibility Reviewer: APPROVED
- Security Reviewer: APPROVED
- Product/API Reviewer: APPROVED

## 11. Signed-revision rule

The signed revision itself must rerun and pass P12-T001..P12-T025 and the complete affected exact-head P00-P12 matrix. Until that signed-revision run completes with `phase=signed`, `merge_authoritative=true`, and defects 0/0/0, this review must not be used as merge authority.

After that exact signed revision passes, P12 may be marked complete for its frozen scope only. P15 identity lifecycle, P13-P17 notification producers, inherited P07 analytics authority, and broader release gates retain their existing ownership and are not silently closed by P12.
