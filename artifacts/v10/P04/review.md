# P04 Product Shells Review

Node: `P04`  
Issue: #14  
PR: #15  
Status: **PENDING**

## Current verification state

- Six shell implementation families are present: Website, Auth, Docs, Workspace, Admin and native PHP Installer.
- The P04 browser evidence toolchain dependency `playwright-core@1.62.1` is now frozen in `pnpm-lock.yaml`.
- Browser, G4 subset and G9 subset evidence are not approved until the current human-owned exact head completes the full workflow matrix.

## Accountable review

- Frontend Lead: PENDING
- QA / Browser-Responsive Reviewer: PENDING
- Accessibility Reviewer: PENDING
- Performance Owner (G9 shell subset): PENDING

## Rule

P04 may be signed only after the exact implementation head has reviewable browser evidence for all six shell families, G4 browser/responsive subset and G9 shell/bundle subset are PASS, P0/P1 defects are zero, `DECISION REQUIRED` is zero, and P00/P01/P02/P03/P04 workflows pass on the same pre-signoff SHA. The signed revision must then pass all five workflows again before merge.
