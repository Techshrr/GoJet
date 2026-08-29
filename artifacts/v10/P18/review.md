# P18 Docs and Multilingual Discovery — Accountable Technical Review

Node: `P18`  
Issue: #48  
Base integration commit: `08cb39bbe54717b711e2d09840ecde04b66bb50f`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**

## 1. Review boundary

This file establishes the P18 accountable-review contract before implementation. It is not a PASS record, signature, completion claim or merge authority.

P18 owns only the Docs and multilingual-discovery contribution to `CAP-TECHNICAL-SEO`: English and Simplified Chinese static Docs route families, Pagefind discovery, breadcrumbs/ToC/previous-next navigation, API reference publication controls, native self-hosting documentation, Docs sitemap children, canonical/hreflang and content ownership.

P18 does not re-own P04 shell design, P05-P17 business/security/API implementations, P19 Website/technical SEO, P20 release-wide gates, or P21/P22 native package/install/production validation.

## 2. Immediate signed predecessor authority

- P17 signed source: `5818406072a131db1c7d8aa7bc5ef8a7adc8d51f`
- P17 integration commit: `08cb39bbe54717b711e2d09840ecde04b66bb50f`
- P17 closure run: `33232541982`
- P17 closure artifact: `9709093486`
- P17 closure digest: `sha256:72f8256b242c4412c82cfd4e69c653e4051dc2b7a951c10c9214c2db775805c1`
- P17 closure: `phase=signed`, `merge_authoritative=true`, affected matrix `66/66`, P0/P1/DECISION REQUIRED `0/0/0`.

P18 starts only from the merged P17 integration commit and inherits the P17 API/admin release state without reinterpretation.

## 3. Inherited P04 Docs shell authority

The signed P04 review remains the shell authority. P18 must preserve:

- Astro/Starlight static Docs production model;
- Docs header/left navigation/article/right ToC composition under the Design System;
- shell states `article`, `search-open`, `nav-drawer`, `not-found`, `offline-static`;
- browser/responsive/focus/overflow boundaries already established by P04.

P18 may add real content and discovery behavior, but cannot silently replace the P04 shell or claim full G4/G5/G9 closure.

## 4. Frozen capability boundary

P18 contributes to:

- `CAP-TECHNICAL-SEO` — owner `P18/P19`, Gates `G7/G9/G13`.

P18 specifically closes the Docs static-rendering, Docs indexation, Docs discovery and Docs metadata/sitemap contribution. P19 remains the Website/technical-SEO owner and later performance contribution; P22 remains owner-controlled production/G13 authority.

## 5. Frozen Docs route authority

The only P18 Docs browser route families are:

- `DOCS-EN-HOME` — `/docs/en/`
- `DOCS-ZH-HOME` — `/docs/zh-CN/`
- `DOCS-ARTICLE` — `/docs/en/{slug...}`, `/docs/zh-CN/{slug...}`
- `DOCS-API` — `/docs/en/api/{resource}`, `/docs/zh-CN/api/{resource}`
- `DOCS-SEARCH` — `/docs/en/search?q={query}`, `/docs/zh-CN/search?q={query}`

Page-Level IA remains the sole path/status/index/canonical/alternate authority. No legacy alias or additional acquisition route may be invented.

## 6. Canonical, alternate and metadata authority

- `CAN-DOCS` derives canonical URLs from published Docs frontmatter.
- `ALT-DOCS` exists only for reciprocal published-200 translations.
- English Docs home supplies `x-default`.
- `META-DOCS` requires title, description, locale, lastUpdated, canonical path, translation linkage and content owner.
- Only canonical indexable 200 URLs enter locale sitemap children.
- Search routes stay noindex and outside canonical/sitemap acquisition sets.
- Query/filter/tracking parameters never create a canonical.
- Unknown pages return 404; withdrawn pages return 410; 200 soft-404 is prohibited.

## 7. Release-claim and API-reference boundary

Documentation must distinguish approved contract from released implementation.

A capability may be described before release only when visibly labelled **not released** and excluded from sitemap/internal acquisition links. Published API reference methods/paths/scopes must be checked against current-repository authority.

P17's signed API Keys and generic outbound Webhooks authority may be documented as released. No later-owned P19-P22 capability may be promoted to released by documentation alone.

Examples and evidence must never contain raw API keys, webhook secrets, OAuth/payment secrets, sessions/tokens, database credentials or user secrets.

## 8. Search and navigation boundary

Pagefind is a build-time static search index. It must:

- index only eligible published Docs for the corresponding locale;
- exclude noindex, withdrawn and unreleased-only acquisition content;
- support the documented shortcut, arrow navigation and Escape;
- restore focus correctly;
- avoid default personalized search tracking.

Published articles require sidebar reachability, breadcrumbs, ToC and deterministic previous/next navigation. Indexable orphan count must be zero.

## 9. Native self-hosting documentation boundary

P18 may document the approved native architecture: Nginx, PHP Installer boundary, eight systemd-managed Go executables, MySQL, Redis, mandatory ClamAV and local filesystem.

It must not publish a Docker production guide or imply that P21/P22 native package, fresh-install, upgrade/rollback or production validation is complete before those nodes close.

Node/Pagefind/Astro are allowed at development/build/test time only; production Docs serving remains static through Nginx.

## 10. Frozen evidence range

The P18 test contract is frozen as `P18-T001..P18-T026`.

- T001-T024 cover content, raw HTML, routes, search, navigation, API references, native guidance, browser/accessibility/responsive and G7 Docs evidence.
- T025 is exact-head evidence coherence.
- T026 is accountable exact-head closure.

All evidence must bind to one exact candidate revision. Mixed-head, stale, malformed, missing or secret-bearing evidence fails closed.

## 11. Closure discipline

Pre-sign closure may prove implementation readiness but is not merge authority.

Final merge authority requires:

1. `P18-T001..P18-T025` PASS on one pre-sign implementation SHA;
2. all applicable exact-head regression workflows PASS;
3. P0/P1/DECISION REQUIRED = `0/0/0`;
4. this review changed to the signed status by a direct review-only child commit;
5. the signed child independently reruns required exact-head evidence and `P18-T026`;
6. final closure reports `phase=signed`, `review_phase=signed`, `review_only_signed_child=true`, `merge_authoritative=true`.

Until those conditions hold, P18 must not be merged and P19 must not start.

## Accountable review placeholders

### SEO Owner — PENDING

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as SEO Owner`  
Decision: **PENDING**

### Frontend Lead — PENDING

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Frontend Lead`  
Decision: **PENDING**

### Docs Content Owner — PENDING

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Docs Content Owner`  
Decision: **PENDING**

### Accessibility Reviewer — PENDING

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Accessibility Reviewer`  
Decision: **PENDING**

### QA Lead — PENDING

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as QA Lead`  
Decision: **PENDING**

No signature is present in this contract-drafting state.
