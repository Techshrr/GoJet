# P19 Website and Technical SEO — Accountable Technical Review

Node: `P19`  
Issue: #50  
Base integration commit: `43e693b10c0118e32d7f14c61156e0b06c155111`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**

## 1. Review boundary

This file establishes the P19 accountable-review contract before implementation. It is not a PASS record, signature, completion claim or merge authority.

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

P21/P22 retain native release/install/production validation authority; P19 must not claim those later nodes complete.

## 12. Frozen evidence range

The P19 test contract is frozen as `P19-T001..P19-T032`.

- T001-T022 cover exact Website routes, content authority, canonical/hreflang/status/sitemap/JSON-LD/social/crawler/link integrity and truthful conversion content.
- T023-T026 cover real-browser, responsive, accessibility and Design System visual authority.
- T027-T030 cover bundle/CWV/image/font/cache performance, static production runtime and deterministic bilingual manifest authority.
- T031 is exact-head evidence coherence.
- T032 is accountable exact-head closure.

All evidence must bind to one exact candidate revision. Mixed-head, stale, malformed, missing or secret-bearing evidence fails closed.

## 13. Closure discipline

Pre-sign closure may prove implementation readiness but is not merge authority.

Final merge authority requires:

1. `P19-T001..P19-T031` PASS on one pre-sign implementation SHA;
2. all applicable exact-head regression workflows PASS;
3. P19-owned G4/G5/G7/G8/G9 evidence PASS;
4. P0/P1/DECISION REQUIRED = `0/0/0`;
5. this review changed to the signed status by a direct review-only child commit;
6. the signed child independently reruns required exact-head evidence and `P19-T032`;
7. final closure reports `phase=signed`, `review_phase=signed`, `review_only_signed_child=true`, `merge_authoritative=true`.

Until those conditions hold, P19 must not be merged and P20 must not start.

## Accountable review placeholders

### SEO Owner — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as SEO Owner`  
Decision: **PENDING**

### Frontend Lead — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Frontend Lead`  
Decision: **PENDING**

### Design Lead — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Design Lead`  
Decision: **PENDING**

### Accessibility Reviewer — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Accessibility Reviewer`  
Decision: **PENDING**

### Performance Owner — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Performance Owner`  
Decision: **PENDING**

### QA Lead — PENDING
Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as QA Lead`  
Decision: **PENDING**

No signature is present in this contract-drafting state.
