# P05 Links Vertical Slice Review

Node: `P05`  
Issue: #16  
Status: **PENDING — IMPLEMENTATION NOT YET REVIEWABLE**

## Review boundary

P05 requires real current-repository evidence from MySQL, Redis, platformapi, redirectengine, Workspace browser flows, audit records and GoJet safety surfaces. Unit tests or UI-only screenshots cannot substitute for those dependencies.

## Pre-signoff review history

- Exact head `a16ae14e1db8f328f08c2024112892d1156a3512` reached full functional closure: P00-P05 affected workflows were green, T001-T019 were exact-head PASS and P05-T020 closure passed. It was **not signed** because manual artifact review found redirect safety/password surfaces still rendered as browser-default HTML and therefore did not satisfy the GoJet V10 Design System contract for public safety/resource surfaces.
- Public safety/password surfaces were subsequently rebuilt as branded, token-governed GoJet surfaces with fixed state iconography, visible state/title/reason/next-step/reference structure, light/dark semantic token projection, focus treatment and an accessible password form. Token-drift and public-surface semantic contract tests were added.
- Exact head `4d9bdf2e179d437266d4158c5726fb6891e8b4f5` passed P00/P01/P02/P03/P04/P05 Domain/P05 Real Integration. Browser baseline T017/T018/T019 also passed, but the password extension stopped because the prior fuzzy `getByLabel('Password')` locator matched both the newly named `Password required` region and the actual password textbox.
- Commit `9e841ce637297df6833a4d47a0e1b7cde27e5792` corrected the evidence locator only, using exact role/name lookup for the password textbox while preserving the public-surface accessibility structure. Its workflow runs were marked `action_required` because the commit was produced by `github-actions[bot]`; that state is not accepted as execution evidence.
- This review remains PENDING until a non-bot exact head containing the same product and evidence changes completes the full affected workflow matrix and the new branded browser captures pass manual visual review.

## Accountable technical review

- Backend Lead: PENDING
- QA Lead: PENDING
- Frontend Lead: PENDING
- Accessibility Reviewer: PENDING
- Security Reviewer: PENDING

## Gate scope

- G3 P05 functional/API slice: PENDING
- G4 P05 browser/responsive slice: PENDING
- G5 P05 accessibility slice: PENDING
- G6 P05 destination-risk/RBAC/concurrency slice: PENDING

Passing P05 subsets will not close the release-wide Gates; later owning nodes and P20/P22 remain required.

## Rule

The review may only be signed after all P05 required tests pass on one exact implementation SHA, evidence is internally consistent and reviewable, P00-P04 affected regressions are green, P0/P1 defects are zero and `DECISION REQUIRED` is zero. Any signed review revision must itself pass the full affected exact-head workflow matrix before merge.
