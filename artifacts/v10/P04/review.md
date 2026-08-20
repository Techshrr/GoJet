# P04 Product Shells Review

Node: `P04`  
Issue: #14  
PR: #15  
Status: **PENDING**

## Current verification state

- Six shell implementation families are present: Website, Auth, Docs, Workspace, Admin and native PHP Installer.
- The P04 browser evidence toolchain dependency `playwright-core@1.62.1` is frozen in `pnpm-lock.yaml`.
- The P04 responsive runtime is implemented inside the existing `@gojet/ui` package; the temporary standalone `@gojet/shell-runtime` package was removed so the P01 frozen workspace topology remains authoritative.
- P01 scoped-package subpath validation now normalizes imports such as `@gojet/ui/styles.css` to the declared workspace package root without expanding the fixed package allowlist.
- Read-only topology diagnostic run `32390368329` passed clean P03 source lint, strict package/app typecheck including Astro check, P01-T004 package boundaries, all four production app builds and native PHP syntax.
- Verified topology-fix artifact `9414581299` had SHA-256 `4b317f3f0211e53f9e550aa633ff8a01b2e0dadf7db8a20adb8cb27dd2a971a7`; its proposed `p01-validate.py` and `pnpm-lock.yaml` were persisted only after byte-identical SHA checks.
- Temporary topology workflow has self-deleted and is no longer part of the branch.
- Browser, G4 subset and G9 subset evidence remain unapproved until the current human-owned exact head completes the full P00/P01/P02/P03/P04 workflow matrix.

## Accountable review

- Frontend Lead: PENDING
- QA / Browser-Responsive Reviewer: PENDING
- Accessibility Reviewer: PENDING
- Performance Owner (G9 shell subset): PENDING

## Rule

P04 may be signed only after the exact implementation head has reviewable browser evidence for all six shell families, G4 browser/responsive subset and G9 shell/bundle subset are PASS, P0/P1 defects are zero, `DECISION REQUIRED` is zero, and P00/P01/P02/P03/P04 workflows pass on the same pre-signoff SHA. The signed revision must then pass all five workflows again before merge.
