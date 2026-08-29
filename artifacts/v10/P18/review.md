# P18 Docs and Multilingual Discovery — Accountable Technical Review

Node: `P18`  
Issue: #48  
Base integration commit: `08cb39bbe54717b711e2d09840ecde04b66bb50f`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

Pre-sign exact implementation SHA: `05d6e61c45f1b58ba3f7ce3013d07d606c5d813b`

Reviewed pre-sign implementation SHA: `05d6e61c45f1b58ba3f7ce3013d07d606c5d813b`

Accountable reviewer: `GPT-5.6 Sol — P18 Technical Review`

Review date: `2026-08-29`

## 1. Review boundary

This file establishes the P18 accountable-review contract and records the accountable disposition of the reviewed pre-sign implementation. It is not by itself merge authority; the signed revision must independently satisfy the same-revision CI rule recorded below.

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

The signed P04 review remains the shell authority. P18 preserves:

- Astro/Starlight static Docs production model;
- Docs header/left navigation/article/right ToC composition under the Design System;
- shell states `article`, `search-open`, `nav-drawer`, `not-found`, `offline-static`;
- browser/responsive/focus/overflow boundaries already established by P04.

P18 adds real content and discovery behavior without replacing the P04 shell or claiming full G4/G5/G9 closure.

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

Page-Level IA remains the sole path/status/index/canonical/alternate authority. No legacy alias or additional acquisition route is invented.

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

Documentation distinguishes approved contract from released implementation.

A capability may be described before release only when visibly labelled **not released** and excluded from sitemap/internal acquisition links. Published API reference methods/paths/scopes are checked against current-repository authority.

P17's signed API Keys and generic outbound Webhooks authority may be documented as released. No later-owned P19-P22 capability is promoted to released by documentation alone.

Examples and evidence contain no raw API keys, webhook secrets, OAuth/payment secrets, sessions/tokens, database credentials or user secrets.

## 8. Search and navigation boundary

Pagefind is a build-time static search index. The reviewed implementation proves that it:

- indexes only eligible published Docs for the corresponding locale;
- excludes noindex, withdrawn and unreleased-only acquisition content;
- supports the documented shortcut, arrow navigation and Escape;
- restores focus correctly;
- avoids default personalized search tracking.

Published articles retain sidebar reachability, breadcrumbs, ToC and deterministic previous/next navigation. Indexable orphan count is zero under the frozen evidence set.

## 9. Native self-hosting documentation boundary

P18 documents only the approved native architecture: Nginx, PHP Installer boundary, eight systemd-managed Go executables, MySQL, Redis, mandatory ClamAV and local filesystem.

It does not publish a Docker production guide or imply that P21/P22 native package, fresh-install, upgrade/rollback or production validation is complete before those nodes close.

Node/Pagefind/Astro are allowed at development/build/test time only; production Docs serving remains static through Nginx.

## 10. Frozen evidence range

The P18 test contract is frozen as `P18-T001..P18-T026`.

- T001-T024 cover content, raw HTML, routes, search, navigation, API references, native guidance, browser/accessibility/responsive and G7 Docs evidence.
- T025 is exact-head evidence coherence.
- T026 is accountable exact-head closure.

All evidence binds to one exact candidate revision. Mixed-head, stale, malformed, missing or secret-bearing evidence fails closed.

## 11. Closure discipline

Pre-sign closure proves implementation readiness but is not merge authority.

Final merge authority requires:

1. `P18-T001..P18-T025` PASS on one pre-sign implementation SHA;
2. all applicable exact-head regression workflows PASS;
3. P0/P1/DECISION REQUIRED = `0/0/0`;
4. this review changed to the signed status by a direct review-only child commit;
5. the signed child independently reruns required exact-head evidence and `P18-T026`;
6. final closure reports `phase=signed`, `review_phase=signed`, `review_only_signed_child=true`, `merge_authoritative=true`.

Until the signed-child conditions hold, P18 must not be merged and P19 must not start.

## 12. Reviewed pre-sign implementation disposition

The reviewed pre-sign implementation at `05d6e61c45f1b58ba3f7ce3013d07d606c5d813b` completed the frozen P18 implementation and pre-sign evidence contract. `P18-T001..P18-T025` passed on one exact implementation head, and pre-sign `P18-T026` independently completed with the applicable affected workflow matrix `10/10` and P0/P1/DECISION REQUIRED `0/0/0`.

The review specifically confirms that P18 remains within its frozen Docs boundary: bilingual static Docs are emitted in initial HTML; canonical/hreflang/sitemap publication follows the Page-Level IA and reciprocal-translation rules; Pagefind remains a build-time static discovery layer; search/error/withdrawn states do not become acquisition URLs; API references are gated by signed released capability authority; the native self-hosting guide does not claim P21/P22 completion or introduce Docker production guidance; and the P04 static Docs shell plus P17 signed predecessor authority remain inherited rather than reinterpreted.

This signed review records that accountable disposition but does not bypass the mandatory signed-revision rerun.

## 13. Exact-head evidence disposition

- `P18-T001..P18-T007`: PASS — bilingual static Docs homes/articles, META-DOCS content ownership, canonical normalization, reciprocal hreflang/x-default, untranslated-page alternate suppression, 404/410 semantics and API-reference release-state gating.
- `P18-T008..P18-T011`: PASS — English and Simplified Chinese Pagefind indexing/discovery, noindex search/error boundaries, keyboard interaction, focus restoration and privacy-safe static search behavior.
- `P18-T012..P18-T018`: PASS — breadcrumbs/ToC/previous-next, sidebar reachability/orphan prevention, locale sitemap children/lastmod, broken-link/static-asset integrity, code-block and production-runtime restrictions, truthful native self-hosting guidance, API-source checking and secret-safe examples.
- `P18-T019..P18-T021`: PASS — inherited P04 Docs shell states, keyboard/name-role-value/focus/reduced-motion behavior and required responsive matrix including canonical viewports and 320 CSS px reflow.
- `P18-T022..P18-T024`: PASS — static-build/production-runtime boundary, G7 Docs crawler/raw-HTTP conformance, deterministic content manifest and locale-parity ledger.
- `P18-T025`: PASS — 24 producer case evidence files bound to one exact implementation head with `same_exact_head=true`, `secret_safe=true`, four digest-bound producer artifacts, inherited P17/P04 authorities live-bound, mixed-head rejection and unsafe-evidence rejection.
- Pre-sign `P18-T026`: PASS — 25 input evidence cases plus final T026 result, complete `10/10` applicable exact-head matrix, P17/P04 authority binding and zero P0/P1/DECISION REQUIRED items. This is pre-sign readiness authority only; the signed child must rerun T026 before merge authority exists.

T025 evidence run: `33258859953`

T025 evidence artifact: `9716696079`

T025 evidence digest: `sha256:5279724780be248a8e694270caf1f2edcbd4bc34a32a033229a85ee1509290cb`

Pre-sign T026 closure run: `33258860004`

Pre-sign T026 closure artifact: `9716762089`

Pre-sign T026 closure digest: `sha256:20e6225908a1dba6c51ed0c4c75d066ec5080481b6d130111b32f7106d10b4a3`

The pre-sign closure is `phase=pre-sign`, `review_phase=pending`, `review_only_signed_child=false`, `merge_authoritative=false`. Its affected exact-head matrix is `10/10`.

## 14. Defect and decision ledger

Evidence disposition: `P18-T001..P18-T025 PASS`

Pre-sign closure disposition: `P18-T026 PASS / phase=pre-sign / merge_authoritative=false`

P0/P1/DECISION REQUIRED: `0/0/0`

Review-only signed child required: `true`

Signed revision requires complete same-revision affected matrix before merge: `true`

## 15. Capability, Gate and inheritance disposition

- `CAP-TECHNICAL-SEO`: approved only for the P18 Docs/static-discovery contribution. P19 retains Website/technical-SEO ownership and P22 retains owner-controlled production/G13 authority.
- G7: P18 Docs crawler/raw-HTML/indexation contribution is approved for this node; this does not claim release-wide G7/G9/G13 completion.
- P04: Astro/Starlight static Docs shell authority remains inherited and bound to run `32392744860`, artifact `9415518410`, digest `sha256:5f6b4ec5be87d866b07599e8bd32d75171a81523d29dd86441a524bf33cbc7bb`.
- P17: immediate predecessor authority remains inherited from signed source `5818406072a131db1c7d8aa7bc5ef8a7adc8d51f`, closure run `33232541982`, artifact `9709093486`, digest `sha256:72f8256b242c4412c82cfd4e69c653e4051dc2b7a951c10c9214c2db775805c1`.
- P19-P22: no completion, release or production-validation authority is claimed by P18.

Historical predecessor closure workflows are revision-specific and are not reinterpreted as P18 exact-head merge gates; their signed authorities remain live-bound while P18 reruns its affected Docs/foundation surface.

## 16. Signed-revision rule

The signed revision must be exactly one direct child of `05d6e61c45f1b58ba3f7ce3013d07d606c5d813b` and must change only `artifacts/v10/P18/review.md`.

That signed child must independently rerun the complete P18 affected exact-head matrix and final closure. Only a final result with `phase=signed`, `review_phase=signed`, `review_only_signed_child=true`, `merge_authoritative=true`, affected matrix `10/10`, `P18-T001..P18-T026 PASS` and P0/P1/DECISION REQUIRED `0/0/0` authorizes PR #49 to leave draft state and merge.

## 17. Accountable signed review disposition

### SEO Owner — APPROVED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as SEO Owner`  
Decision: **APPROVED** — Docs canonical/hreflang/sitemap/indexation/search publication behavior remains inside the frozen P18 technical-SEO contribution.

### Frontend Lead — APPROVED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Frontend Lead`  
Decision: **APPROVED** — Astro/Starlight remains static, P04 shell authority is preserved, and responsive/browser interaction evidence satisfies the P18 boundary.

### Docs Content Owner — APPROVED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Docs Content Owner`  
Decision: **APPROVED** — bilingual content ownership, translation linkage, API-reference release gating and native self-hosting claims conform to the frozen contract.

### Accessibility Reviewer — APPROVED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as Accessibility Reviewer`  
Decision: **APPROVED** — keyboard, focus, name-role-value, reduced-motion, reflow and shell-state evidence passed on the reviewed pre-sign implementation.

### QA Lead — APPROVED

Reviewer: `GPT-5.6 Sol — AI technical reviewer acting as QA Lead`  
Decision: **APPROVED** — `P18-T001..P18-T025 PASS`, pre-sign T026 PASS, exact-head matrix `10/10`, and P0/P1/DECISION REQUIRED `0/0/0`; signed-child same-revision rerun remains mandatory.

Signed by: `GPT-5.6 Sol — P18 Accountable Technical Review`  
Signed review date: `2026-08-29`
