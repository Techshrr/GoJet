# P19 Website and Technical SEO — Accountable Technical Review

Node: `P19`  
Issue: #50  
Base integration commit: `43e693b10c0118e32d7f14c61156e0b06c155111`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

## 1. Review boundary

This signed review records the accountable technical decision for the exact P19 pre-sign implementation identified below. It does not by itself create merge authority: the direct review-only child carrying this signature must independently rerun the complete exact-head matrix and `P19-T032` before merge.

P19 owns only the Website and technical-SEO contribution defined by the Master Plan and Page-Level IA: real public Website content, bilingual indexable Website route families, conversion paths, `META-WEB`, `CAN-WEB`, `ALT-WEB`, Website sitemap child, eligible JSON-LD, internal links, social cards, asset attribution, Website browser/accessibility/visual evidence and the P19 G9 performance contribution.

P19 does not re-own P18 Docs, P05-P17 product/security/API implementations, P20 whole-product verification, or P21/P22 native package/fresh-install/production-candidate authority.

## 2. Immediate signed predecessor authority

- P18 signed source: `e8746159b02c729a877e3dcbd9655d415a5cc269`
- P18 integration commit: `43e693b10c0118e32d7f14c61156e0b06c155111`
- P18 closure run: `33260817755`
- P18 closure artifact: `9717210947`
- P18 closure digest: `sha256:3e403765409b3ab273be1c35a9d88b565505c416a47364d9a6f0339cc130efe4`
- P18 closure: `phase=signed`, `review_phase=signed`, `review_only_signed_child=true`, `merge_authoritative=true`, affected matrix `10/10`, P0/P1/DECISION REQUIRED `0/0/0`.

P19 starts only from the merged P18 integration commit and inherits P18 Docs discovery/SEO authority without reinterpretation.

## 3. Public capability claim boundary

P19 may describe only capability behavior already backed by signed/integrated GoJet V10 authority. Website copy is not implementation authority.

The Website must not fabricate product states, dashboards, customer counts, ratings, reviews, endorsements, certifications, compliance claims, price/availability, security guarantees or release status. Deferred or later-owned capabilities must not be promoted as released.

P05-P17 remain the product/security/API authorities for Links, routing, QR, Files/ClamAV, Text, Bio, Analytics, Workspace, billing/payments/entitlements, support/mail, Auth/OAuth, destination/domain risk, abuse, Admin/Operations/Audit, API Keys and user Webhooks. P18 remains Docs authority.

## 4. Frozen Website route authority

P19 Website routes are exactly the 26 Page-Level IA §3.1 Route IDs frozen in `artifacts/v10/P19/test-plan.json`, with English root paths and the approved `/zh-CN` peers.

No legacy alias, keyword landing page or additional acquisition path may be invented. Dynamic guides exist only under the approved `/guides/{slug}` family and only when the content registry authorizes publication.

## 5. Canonical, alternate, metadata and sitemap authority

- `CAN-WEB` is `PUBLIC_BASE_URL + normalized localized path`; each language self-canonical.
- `ALT-WEB` is reciprocal only when both canonical language pages return 200.
- English canonical supplies `x-default`.
- `META-WEB` requires title, description, H1, locale, updated time, content owner, canonical path and translation linkage.
- Tracking/query/filter parameters never create canonicals.
- Only canonical indexable 200 Website URLs enter the Website sitemap child.
- Unknown routes return 404; withdrawn content returns 410 where IA requires; superseded legal content follows reviewed redirect semantics.
- 200 soft-404, redirect/canonical chains and indexable orphan pages are prohibited.

## 6. Structured data and social discovery authority

JSON-LD may be emitted only for IA-approved eligible types and must match visible raw-HTML content. Fabricated ratings, reviews, offers or organization facts are prohibited.

OpenGraph/Twitter social cards must be deterministic, correctly dimensioned, locally controlled or explicitly attributed, metadata-consistent and non-broken. Social metadata cannot substitute for canonical Website content.

## 7. Raw HTML and crawler parity

Indexable Website primary content, metadata, canonical, hreflang and eligible structured data must be present without client execution. Crawler-only rendering or differential content is prohibited.

Crawler policy, sitemap membership and internal-link acquisition must agree. Private/Auth/Workspace/Admin/UGC/error routes must not enter the Website acquisition set.

## 8. Content and conversion boundary

Home, Products, Solutions, Developers, Pricing, Security, Guides, About, Contact and Legal surfaces must represent real product behavior and approved conversion paths.

Pricing must fail truthfully when structured public data is unavailable. Security content must be grounded in signed controls without unverified compliance claims. Contact must preserve inherited Turnstile/rate/validation/secret-safety authority and persistent success/error states. Guides and legal records must use deterministic versioned content and correct publication/withdrawal behavior.

Thin keyword pages are prohibited.

## 9. Browser, accessibility and visual boundary

P19 must provide real browser evidence for representative Website templates and their required partial/error/maintenance states. Desktop, tablet, mobile and 320 CSS px reflow must not clip controls, lose content, overlap regions or rely on hover-only actions.

Keyboard order, visible focus, landmarks/headings, names/roles/values, reduced motion, image alternatives and persistent status/error messaging remain mandatory.

The Design System is the sole exact visual authority. P19 must satisfy Design System §§1-14 and the Page-Level IA §16 screenshot matrix. Placeholder icons, fabricated product UI, random illustration, broken images and uncontrolled motion are hard failures.

## 10. Performance boundary

P19 owns the Website G9 contribution and must prove the frozen Master Plan budgets:

- Website initial JavaScript: `<=150 KB gzip`;
- LCP: `<=2.5 s` under the frozen lab gate;
- INP: `<=200 ms`;
- CLS: `<=0.1`;
- principal images: AVIF + WebP fallback, explicit dimensions and responsive `srcset`;
- fonts: self-hosted subsets without invisible-text blocking;
- Website must not load Workspace/Admin bundles.

Bundle/CWV/image/font/cache evidence must be reproducible and cannot be weakened merely to pass CI.

## 11. Static production runtime boundary

P19 Website production delivery is static/pre-rendered through Nginx. Node/Vite may be used only for development/build/test.

Production Node HTTP/SSR/PM2 and Docker/Compose remain prohibited. A crawler-only server path is not allowed.

P21/P22 retain native release/install/production validation authority; P19 does not claim those later nodes complete.

## 12. Frozen evidence range

The P19 test contract remains frozen as `P19-T001..P19-T032`.

- T001-T022 cover exact Website routes, content authority, canonical/hreflang/status/sitemap/JSON-LD/social/crawler/link integrity and truthful conversion content.
- T023-T026 cover real-browser, responsive, accessibility and Design System visual authority.
- T027-T030 cover bundle/CWV/image/font/cache performance, static production runtime and deterministic bilingual manifest authority.
- T031 is exact-head evidence coherence.
- T032 is accountable exact-head closure.

All evidence must bind to one exact candidate revision. Mixed-head, stale, malformed, missing or secret-bearing evidence fails closed.

## 13. Pre-sign implementation authority reviewed

Reviewed pre-sign implementation SHA: `ab6fbf7ed99f069d5aefff4a6591ecc334efbc11`  
Pre-sign T032 closure run: `33268124398`  
Pre-sign T032 closure artifact: `9719330281`  
Pre-sign T032 closure digest: `sha256:ee885a2e98ec6c6f1bb08a176a0c8f435a9505ef6e5396d3b221a242b5ea2ce2`  
Evidence disposition: `P19-T001..P19-T031 PASS`  
Applicable exact-head regression: `19/19 PASS`  
P19 Gate contribution: `G4/G5/G7/G8/G9 = 5/5 PASS`  
P0/P1/DECISION REQUIRED: `0/0/0`

The pre-sign closure is explicitly `phase=pre-sign`, `review_phase=pending`, `review_only_signed_child=false`, `merge_authoritative=false`. This review therefore approves the reviewed implementation but does not supersede the same-revision CI requirement for the signed child.

## 14. Accountable reviewer decisions

### SEO Owner — APPROVED
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as SEO Owner`  
Decision: **APPROVED**  
Basis: exact bilingual route inventory, `META-WEB`/`CAN-WEB`/`ALT-WEB`, HTTP semantics, sitemap/index policy, internal-link graph, JSON-LD, robots/raw-HTML parity and broken-link/static-asset evidence are same-head PASS; P19 G7 contribution is PASS.

### Frontend Lead — APPROVED
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Frontend Lead`  
Decision: **APPROVED**  
Basis: static Website build, approved route/conversion surfaces, desktop/tablet/mobile/320 CSS px browser matrix and static Nginx runtime boundary are same-head PASS; no crawler-only or production Node/SSR/PM2 serving path is accepted.

### Design Lead — APPROVED
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Design Lead`  
Decision: **APPROVED**  
Basis: deterministic social assets/attribution and Design System §§1-14 / IA §16 screenshot conformance are same-head PASS; P19 G8 contribution is PASS.

### Accessibility Reviewer — APPROVED
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Accessibility Reviewer`  
Decision: **APPROVED**  
Basis: keyboard order, visible focus, semantic structure, names/roles/values, reduced motion, image alternatives, persistent status behavior and reflow evidence are same-head PASS; P19 G5 contribution is PASS.

### Performance Owner — APPROVED
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Performance Owner`  
Decision: **APPROVED**  
Basis: initial JS budget, Website bundle isolation, frozen-lab LCP/INP/CLS, image/font/cache contracts and deterministic clean-build evidence are same-head PASS; P19 G9 contribution is PASS.

### QA Lead — APPROVED
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as QA Lead`  
Decision: **APPROVED**  
Basis: P19-T001..P19-T031 is complete, exact-head, digest-bound and secret-safe; P18 and P05-P18 public capability authorities are live-bound; mixed/stale/malformed evidence fails closed; applicable regression is 19/19 PASS and defects are 0/0/0.

## 15. Signed-child closure requirement

This signature is valid only on the direct child of `ab6fbf7ed99f069d5aefff4a6591ecc334efbc11` whose sole changed path is `artifacts/v10/P19/review.md`.

That signed child must independently rerun all required exact-head P19 producers, T031, the 19-workflow applicable regression matrix and T032. Merge authority exists only if the resulting closure reports all of the following simultaneously:

- `phase=signed`
- `review_phase=signed`
- `review_only_signed_child=true`
- `merge_authoritative=true`
- `P19-T001..P19-T032 PASS`
- `G4/G5/G7/G8/G9 = 5/5 PASS`
- P0/P1/DECISION REQUIRED = `0/0/0`

Until that signed-child closure succeeds, P19 must not be merged and P20 must not start.
