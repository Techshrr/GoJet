# P04 Product Shells Review

Node: `P04`  
Issue: #14  
PR: #15  
Status: **SIGNED — POST-SIGNOFF EXACT-HEAD REGRESSION REQUIRED**

## Reviewed implementation candidate

- Pre-signoff implementation commit: `659e25e3c5e263ffb3dd74cde953e812b75a7439`
- P04 workflow run: `32392744860`
- P04 evidence artifact: `9415518410`
- Artifact SHA-256: `5f6b4ec5be87d866b07599e8bd32d75171a81523d29dd86441a524bf33cbc7bb`
- Same-SHA regression matrix: P00 PASS / P01 PASS / P02 PASS / P03 PASS / P04 PASS
- P04 required tests: `10/10 PASS`
- P0/P1 defects: `0`
- `DECISION REQUIRED`: `0`

## Evidence reviewed

- Six shell implementation families: Website, Auth, Docs, Workspace, Admin and native PHP Installer.
- 19 canonical browser captures using the Design System viewport contract.
- Canonical runtime viewport equality verified for every capture: requested viewport == Playwright viewport == browser `innerWidth/innerHeight`.
- Desktop `1440x900`, tablet `1024x768`, mobile `390x844` evidence executed as real Chrome viewports.
- Three SPA transitions preserve the active document and record transition CLS = `0`.
- Horizontal overflow findings: `0`.
- Required clipped-text findings: `0`.
- Console errors: `0`.
- Page errors: `0`.
- HTTP >=400 evidence requests: `0`.
- Request failures: `0`.
- Maximum observed shell CLS: approximately `0.000147`, below the P04 shell threshold `0.02`.
- Workspace command overlay is modal, Escape closes it, and focus returns to the initiating control.
- Auth mobile form controls remain within the canonical mobile viewport after shared primitive `box-sizing` correction.
- Site, Workspace, Admin and Installer serve the canonical P02 favicon asset; Docs serves the canonical P02 SVG favicon under `/docs/`.
- Workspace/Admin shell routes retain route-level lazy chunks required by the P01 code-splitting contract.
- G4 browser/responsive P04 subset: PASS. Full G4 remains open for later owning nodes.
- G9 shell/bundle-isolation P04 subset: PASS. Full G9 remains open for later owning nodes.

## Accountable technical review

The reviewer identity below is intentionally explicit: these are evidence-based AI technical reviews acting in the named accountable roles. They are not represented as approvals by unnamed human reviewers.

### Frontend Lead — SIGNED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Frontend Lead`  
Decision: **PASS**

Basis:
- all six shell families are implemented on the approved P01/P03 package and token foundations;
- Website/Auth, Workspace and Admin use the intended React/TanStack routing model; Docs remains Astro/Starlight static; Installer remains PHP 8.3 native;
- P01 fixed workspace topology remains intact;
- Workspace/Admin restore real route-level code splitting instead of weakening the P01 smoke contract;
- navigation and responsive behavior are exercised in real browser evidence without document reload or horizontal overflow.

### QA / Browser-Responsive Reviewer — SIGNED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as QA / Browser-Responsive Reviewer`  
Decision: **PASS**

Basis:
- P04 required cases are `10/10 PASS` on the reviewed implementation commit;
- browser matrix covers all six surfaces at canonical desktop/tablet/mobile viewports plus localized Docs evidence;
- runtime viewport hard assertions prevent mislabeled responsive evidence;
- console/page/HTTP/request-failure findings are zero;
- navigation, focus return, overflow, clipping, no-reload and layout-shift contracts are directly exercised.

### Accessibility Reviewer — SIGNED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Accessibility Reviewer`  
Decision: **PASS for the P04 shell-applicable subset**

Basis:
- P03 Design System accessibility regression is green on the same implementation SHA;
- shell browser evidence verifies keyboard Escape handling and focus restoration for the Workspace modal interaction;
- visible mobile layouts do not clip required content or create horizontal scrolling;
- responsive evidence includes Chinese Docs mobile rendering with the CI CJK font prerequisite retained from P03.

This signature does **not** close full G5 Accessibility; G5 continues through later UI nodes and P20 as required by the Master Plan.

### Performance Owner — SIGNED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Performance Owner`  
Decision: **PASS for the P04 G9 shell subset**

Basis:
- shell route transitions preserve the document;
- observed route-transition CLS is `0` and maximum measured shell CLS is approximately `0.000147`;
- P01 route-level dynamic chunk contract remains green;
- G9 P04 bundle-isolation evidence is PASS and Website does not rely on Workspace/Admin runtime bundles.

This signature does **not** close full G9 Performance; later P07/P19/P20 evidence remains required.

## Sign-off decision

**P04 is approved for post-signoff exact-head regression.**

The signatures above review the exact implementation candidate `659e25e3c5e263ffb3dd74cde953e812b75a7439` and evidence artifact `9415518410`. This review-only commit changes the repository SHA but does not alter the reviewed implementation behavior. P04 Exit Conditions may be marked complete only if the signed revision subsequently passes P00/P01/P02/P03/P04 workflows on the same exact post-signoff SHA. If any affected Gate fails, this sign-off is suspended until the failure is resolved and reviewed.
