# GoJet V10 Page-Level IA — Greenfield Route, State and Flow Contract

**Document ID:** `GJ-V10-IA-GREENFIELD-2026-08-20`  
**Status:** APPROVED PAGE, ROUTE, STATE AND SEO CONTRACT  
**Product contract:** `GoJet V10`  
**Implementation repository:** `Techshrr/GoJet`  
**Implementation remote:** `https://github.com/Techshrr/GoJet.git`  
**Implementation branch:** `main`  
**Development model:** `GREENFIELD / NO LEGACY ROUTE DEPENDENCY`  
**Specification pack:** `specifications/`  
**Master contract:** `GJ-V10-MP-GREENFIELD-2026-08-20`  
**Design authority:** `GJ-V10-DS-GREENFIELD-2026-08-20`

> 本文负责页面路由、任务、能力依赖、适用状态、响应式组成、交互流程和页面级 SEO。所有精确视觉值引用 Design System，不在本文重新定义。所有路由都是 GoJet V10 在当前仓库中的目标合同；不存在“旧路由已实现所以自动视为完成”的规则。

---

## 1. Authority, greenfield classification and route-record schema

### 1.1 Authority and classification rule

本合同不引用旧 GoJet 代码判断页面是否存在。Route Registry 每一行只允许以下状态：

| Status | Meaning in this document |
|---|---|
| `REQUIRED` | GoJet V10 release 必须在当前仓库中实现该页面、API/行为依赖和全部适用状态。 |
| `DEFERRED` | 明确排除在 V10；MUST NOT be linked, shipped or described as available. |
| `REMOVED` | 明确禁止进入正式产品或兼容层。 |

页面存在、API 存在、UI 可见均不能互相替代证明；Route、Capability、API、RBAC、状态和浏览器证据必须分别闭环。User API keys、generic outbound webhooks、domain entitlement、domain risk、native-only installer/release、technical SEO 和 in-product notifications 与其他能力一样都是 `REQUIRED`，不存在旧实现豁免。

### 1.2 Greenfield implementation ledger

- Route Registry 是 browser route 的唯一权威；实现不得从旧 SPA/router 自动导入额外路径。
- Capability dependency 必须引用 Master Plan 的 `REQUIRED` capability；API dependency 必须写成当前仓库要实现的精确 route family 或明确的 static/no-request dependency。
- Prior GoJet URL compatibility is not required. 未经 change control 批准不得添加 legacy alias、silent redirect 或旧 query contract。
- 每个 route 必须同时拥有 loading/empty/error/permission/conflict/long-content/mobile 中实际适用的状态定义；不得用 shared blanket state 代替页面合同。

### 1.3 Normative Route Registry row

Every route row MUST carry all fields below. Surface tables may combine adjacent cells, but no field may be omitted.

| Field | Contract |
|---|---|
| Route ID | Globally unique stable identifier. |
| Path | Exact canonical path pattern, host pattern or system-selected error response. Query keys are inputs, not separate canonical URLs. |
| Surface | Website, Auth, Docs, Workspace, Admin, Public, Installer or Error. |
| Purpose | One outcome a user or operator can verify. |
| Access | `public`, authenticated user, Workspace role, dedicated admin permission or system-only. |
| Status | Exactly one of `REQUIRED`, `DEFERRED` or `REMOVED`. |
| Capability dependency | Master Plan Capability ID. |
| API dependency | Exact required HTTP route family, static dependency or `none`. |
| Applicable states | Only states the page can actually enter; no shared blanket list. |
| Index policy | Exactly `index`, `noindex` or `conditional`. |
| Canonical source | Authoritative URL derivation or `none` for noindex/non-HTML/error responses. |
| Locale alternate | Reciprocal mapping rule or `none`. |
| Internal-link parents | Crawlable parents for index pages; navigation/task parents for protected pages. |

Public route rows additionally MUST provide search intent/topic, metadata source, structured-data eligibility, sitemap membership and HTTP status behavior.

### 1.4 URL and metadata identifiers

- `CAN-WEB`: `PUBLIC_BASE_URL + normalized localized path`; each language self-canonical.
- `CAN-DOCS`: `PUBLIC_BASE_URL + /docs/{locale}/{published path}` from Docs frontmatter.
- `ALT-WEB`: English and `zh-CN` alternates are reciprocal only when both canonical pages return 200; `x-default` is the English canonical.
- `ALT-DOCS`: frontmatter translation linkage is reciprocal only when both published documents return 200; Docs home includes English `x-default`.
- `META-WEB`: versioned page content record containing title, description, H1, locale, updated time and content owner.
- `META-DOCS`: versioned Docs frontmatter containing title, description, locale, lastUpdated, canonical path, translation linkage and content owner.
- `META-SYSTEM`: fixed, reviewed copy keyed by allowlisted reason/status; it MUST NOT contain unsafe targets, provider evidence or secrets.

Canonical paths use lowercase segments and no trailing slash, except `/`, `/docs/en/`, `/docs/zh-CN/` and `/install/`. Tracking/query/filter parameters never create a canonical。

## 2. 全局路由树

```text
Website
├── /
├── /products/*
├── /solutions/*
├── /developers
├── /pricing
├── /security
├── /guides/*
├── /about
├── /contact
└── /legal/*

Auth
├── /login
├── /register
├── /verify-email
├── /forgot-password
├── /reset-password
├── /invite/{token}
├── /oauth/{provider}/callback
└── /social-registration

Docs
├── /docs/en/*
└── /docs/zh-CN/*

Workspace
└── /app/*

Admin
└── /admin/*

Public resources
├── https://{official-short-host}/{code}
├── https://{custom-host}/{code}
├── /t/{slug}
├── /p/{slug}
├── /f/{slug}
├── /api/public/files/{slug}
├── /linkunavailable
├── /abuse/report
├── /status
├── /announcements
└── /preview/{resourceType}/{token}

Errors
└── system-selected 4xx/5xx and safety responses

Installer
└── /install/*
```

API routes are dependencies, not browser-page canonicals, except the explicitly registered non-HTML public file response. 简体中文 Website 镜像使用 `/zh-CN/...`；英文为根路径。页面不存在对应语言版本时不得生成假的 hreflang 目标。

---

## 3. Public SEO Route Matrix

### 3.1 Website registry

`localized` means the English path shown plus its `/zh-CN`-prefixed peer. All rows are `REQUIRED` because the specification does not contain this complete bilingual, pre-rendered route set and SEO contract.

| Route ID | Path | Surface; purpose; access | Status | Capability / API dependency | Applicable states | Index | Canonical / alternate | Internal-link parents |
|---|---|---|---|---|---|---|---|---|
| `WEB-HOME` | `/` and `/zh-CN/` | Website; product category and task entry; public | `REQUIRED` | `CAP-TECHNICAL-SEO`, `CAP-ANNOUNCEMENTS-SETTINGS`; required public settings/plans/announcement APIs may supply reviewed data | default, announcement-partial, pricing-partial, maintenance | `index` | `CAN-WEB / ALT-WEB` | root |
| `WEB-PRODUCTS` | `/products`, localized | Website; product-family index; public | `REQUIRED` | product capabilities; static build content | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Home main navigation |
| `WEB-LINKS` | `/products/links`, localized | Website; explain link creation and controls; public | `REQUIRED` | `CAP-LINKS`, `CAP-LINK-ROUTING`, `CAP-LINK-AB`, `CAP-LINK-UTM`; none at request time | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Products, Home |
| `WEB-QR` | `/products/qr-codes`, localized | Website; explain QR lifecycle; public | `REQUIRED` | `CAP-QR`; none at request time | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Products, Links |
| `WEB-FILES` | `/products/files`, localized | Website; explain file quarantine and publication; public | `REQUIRED` | `CAP-FILES`, `CAP-CLAMAV-REQUIRED`; none at request time | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Products, Security |
| `WEB-TEXT` | `/products/text-sharing`, localized | Website; explain text sharing controls; public | `REQUIRED` | `CAP-TEXT`; none at request time | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Products |
| `WEB-BIO` | `/products/link-in-bio`, localized | Website; explain Bio editing and default noindex boundary; public | `REQUIRED` | `CAP-BIO`, `CAP-BIO-OPT-IN-INDEX DEFERRED`; none at request time | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Products, Solutions/Creators |
| `WEB-ANALYTICS` | `/products/analytics`, localized | Website; explain measured click dimensions and retention; public | `REQUIRED` | `CAP-ANALYTICS`; none at request time | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Products, Links |
| `WEB-ROUTING` | `/products/smart-routing`, localized | Website; explain conditional routing and A/B safety; public | `REQUIRED` | `CAP-LINK-ROUTING`, `CAP-LINK-AB`, `CAP-DESTINATION-RISK` | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Products, Developers |
| `WEB-DOMAINS` | `/products/custom-domains`, localized | Website; explain entitlement, verification and routing constraints; public | `REQUIRED` | `CAP-DOMAIN-OWNERSHIP`, `CAP-DOMAIN-HTTPS`, `CAP-DOMAIN-ENTITLEMENT`, `CAP-DOMAIN-RISK` | default, pricing-partial, maintenance | `index` | `CAN-WEB / ALT-WEB` | Products, Security, Pricing |
| `WEB-SOLUTIONS` | `/solutions`, localized | Website; scenario index; public | `REQUIRED` | product capabilities; static build content | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Home main navigation |
| `WEB-SOL-MARKETING` | `/solutions/marketing`, localized | Website; campaign, UTM, A/B and analytics workflow; public | `REQUIRED` | `CAP-CAMPAIGNS`, `CAP-LINK-UTM`, `CAP-LINK-AB`, `CAP-ANALYTICS` | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Solutions, Links |
| `WEB-SOL-CREATORS` | `/solutions/creators`, localized | Website; Bio, QR and links workflow; public | `REQUIRED` | `CAP-BIO`, `CAP-QR`, `CAP-LINKS` | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Solutions, Bio |
| `WEB-SOL-TEAMS` | `/solutions/teams`, localized | Website; workspace and role workflow; public | `REQUIRED` | `CAP-WORKSPACE`, `CAP-OPS-AUDIT` | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Solutions |
| `WEB-SOL-DEVELOPERS` | `/solutions/developers`, localized | Website; released API and automation entry; public | `REQUIRED` | `CAP-API-KEYS`, `CAP-USER-WEBHOOKS`, `CAP-DOMAIN-ENTITLEMENT` | default, capability-not-released, maintenance | `index` | `CAN-WEB / ALT-WEB` | Solutions, Developers |
| `WEB-DEVELOPERS` | `/developers`, localized | Website; developer documentation entry; public | `REQUIRED` | `CAP-TECHNICAL-SEO` plus published API capability set | default, capability-not-released, maintenance | `index` | `CAN-WEB / ALT-WEB` | Home navigation, Docs |
| `WEB-PRICING` | `/pricing`, localized | Website; plan, quota and domain-entitlement comparison; public | `REQUIRED` | `CAP-BILLING`; `GET /api/public/plans` plus V10 structured entitlement fields | loading-data, success, data-unavailable, maintenance | `index` | `CAN-WEB / ALT-WEB` | Home navigation, product CTAs |
| `WEB-SECURITY` | `/security`, localized | Website; describe account, destination, file, domain and abuse controls without unverified claims; public | `REQUIRED` | `CAP-AUTH`, `CAP-DESTINATION-RISK`, `CAP-FILES`, `CAP-ABUSE`, V10 domain controls | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Home navigation, Files, Domains |
| `WEB-GUIDES` | `/guides`, localized | Website; approved guide index; public | `REQUIRED` | `CAP-TECHNICAL-SEO`; content manifest | default, empty-unpublished, maintenance | `index` | `CAN-WEB / ALT-WEB` | Home, Docs |
| `WEB-GUIDE` | `/guides/{slug}`, localized | Website; one task-specific guide; public | `REQUIRED` | `CAP-TECHNICAL-SEO`; published content record | published, withdrawn-410, not-found-404 | `conditional` | `CAN-WEB / ALT-WEB` only when approved translation returns 200 | Guides, related product and Docs |
| `WEB-ABOUT` | `/about`, localized | Website; maintained product identity and operating principles; public | `REQUIRED` | `CAP-TECHNICAL-SEO`; static build content | default, maintenance | `index` | `CAN-WEB / ALT-WEB` | Footer |
| `WEB-CONTACT` | `/contact`, localized | Website; route contact reason to an accountable channel; public | `REQUIRED` | `CAP-TICKETS`; `REQUIRED POST /api/public/contact` | input, submitting, success-persistent, validation-error, Turnstile-error, rate-limited | `index` | `CAN-WEB / ALT-WEB` | Footer, Security |
| `WEB-LEGAL-TERMS` | `/legal/terms`, localized | Website; service terms; public | `REQUIRED` | `CAP-TECHNICAL-SEO`; versioned legal content | published, superseded-redirect, withdrawn-410 | `index` | `CAN-WEB / ALT-WEB` | Footer |
| `WEB-LEGAL-PRIVACY` | `/legal/privacy`, localized | Website; privacy notice; public | `REQUIRED` | `CAP-TECHNICAL-SEO`; versioned legal content | published, superseded-redirect, withdrawn-410 | `index` | `CAN-WEB / ALT-WEB` | Footer |
| `WEB-LEGAL-AUP` | `/legal/acceptable-use`, localized | Website; acceptable-use policy; public | `REQUIRED` | `CAP-ABUSE`; versioned legal content | published, superseded-redirect, withdrawn-410 | `index` | `CAN-WEB / ALT-WEB` | Footer, Security |
| `WEB-LEGAL-ABUSE` | `/legal/abuse`, localized | Website; abuse policy and report process; public | `REQUIRED` | `CAP-ABUSE`; versioned legal content | published, superseded-redirect, withdrawn-410 | `index` | `CAN-WEB / ALT-WEB` | Footer, Security, Abuse report |

### 3.1.1 Website SEO extension

| Route ID | Search intent / unique topic | Metadata source | Structured-data eligibility | Sitemap | HTTP contract |
|---|---|---|---|---|---|
| `WEB-HOME` | GoJet category and supported tasks | `META-WEB` | `WebSite` and `Organization` | Website child | 200 canonical; 503 maintenance |
| `WEB-PRODUCTS` | GoJet product index | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-LINKS` | controlled short links | `META-WEB` | `BreadcrumbList`; `Product` only after eligibility review | Website child | 200 or 404 |
| `WEB-QR` | managed QR codes | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-FILES` | quarantined file sharing | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-TEXT` | controlled text sharing | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-BIO` | Bio page management | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-ANALYTICS` | link analytics dimensions | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-ROUTING` | conditional link routing and A/B | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-DOMAINS` | custom-domain entitlement and verification | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-SOLUTIONS` | GoJet scenario index | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-SOL-MARKETING` | campaign link workflow | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-SOL-CREATORS` | creator link workflow | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-SOL-TEAMS` | team link governance | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-SOL-DEVELOPERS` | API-based link workflow | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-DEVELOPERS` | GoJet developer entry | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-PRICING` | GoJet plans and quotas | `META-WEB` plus authoritative plan snapshot | `BreadcrumbList`; `Product` only after eligibility review | Website child | 200 with server/build data; 503 if authoritative data cannot be produced |
| `WEB-SECURITY` | GoJet security control boundaries | `META-WEB` | `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-GUIDES` | GoJet task guides | `META-WEB` plus guide manifest | `BreadcrumbList` | Guides child | 200 or 404 |
| `WEB-GUIDE` | one approved task | guide frontmatter and visible content | `BreadcrumbList`; `Article` only when eligible | Guides child only while published | 200 published; 404 unknown; 410 permanently withdrawn |
| `WEB-ABOUT` | GoJet maintained identity | `META-WEB` | `Organization` and `BreadcrumbList` | Website child | 200 or 404 |
| `WEB-CONTACT` | GoJet contact route | `META-WEB` | `BreadcrumbList` | Website child | 200 page; POST uses 2xx/4xx/429 without changing canonical |
| `WEB-LEGAL-TERMS` | GoJet service terms | versioned legal record | `BreadcrumbList` | Website child | 200 current; 308 superseded path; 410 withdrawn |
| `WEB-LEGAL-PRIVACY` | GoJet privacy notice | versioned legal record | `BreadcrumbList` | Website child | 200 current; 308 superseded path; 410 withdrawn |
| `WEB-LEGAL-AUP` | GoJet acceptable use | versioned legal record | `BreadcrumbList` | Website child | 200 current; 308 superseded path; 410 withdrawn |
| `WEB-LEGAL-ABUSE` | GoJet abuse process | versioned legal record | `BreadcrumbList` | Website child | 200 current; 308 superseded path; 410 withdrawn |

Every `index` route has one visible H1, unique title/description, raw-HTML primary content and at least one crawlable parent. Only canonical, indexable 200 URLs enter a sitemap; `lastmod` comes from the content record, not build time.

### 3.2 Docs registry and SEO extension

| Route ID | Path | Surface; purpose; access | Status | Capability / API dependency | Applicable states | Index | Canonical / alternate | Internal-link parents |
|---|---|---|---|---|---|---|---|---|
| `DOCS-EN-HOME` | `/docs/en/` | Docs; English documentation entry; public | `REQUIRED` | `CAP-TECHNICAL-SEO`; static Docs build | published, build-error, maintenance | `index` | `CAN-DOCS / ALT-DOCS` | Developers, Footer |
| `DOCS-ZH-HOME` | `/docs/zh-CN/` | Docs; Simplified Chinese documentation entry; public | `REQUIRED` | `CAP-TECHNICAL-SEO`; static Docs build | published, build-error, maintenance | `index` | `CAN-DOCS / ALT-DOCS` | Developers, Footer |
| `DOCS-ARTICLE` | `/docs/en/{slug...}` and `/docs/zh-CN/{slug...}` | Docs; one guide or reference task; public | `REQUIRED` | documented capability; static Docs build | published, untranslated, withdrawn-410, not-found-404 | `conditional` | `CAN-DOCS / ALT-DOCS` only for published 200 translations | Docs sidebar, previous/next, related product/guide |
| `DOCS-API` | `/docs/en/api/{resource}` and `/docs/zh-CN/api/{resource}` | Docs; production API contract; public | `REQUIRED` | referenced API capability; generated/checked API source | published-api, capability-not-released, withdrawn-410, not-found-404 | `conditional` | `CAN-DOCS / ALT-DOCS` only when the API is released and page is 200 | API index, capability docs, Developers |
| `DOCS-SEARCH` | `/docs/en/search?q={query}` and `/docs/zh-CN/search?q={query}` | Docs; local search results; public | `REQUIRED` | `CAP-TECHNICAL-SEO`; Pagefind static index | loading-index, results, empty, query-error, offline-static | `noindex` | none / none | Docs search control |

| Route ID | Search intent / unique topic | Metadata source | Structured data | Sitemap | HTTP contract |
|---|---|---|---|---|---|
| `DOCS-EN-HOME` | English GoJet documentation | `META-DOCS` | `BreadcrumbList` | Docs EN child | 200 or 404 |
| `DOCS-ZH-HOME` | 简体中文 GoJet 文档 | `META-DOCS` | `BreadcrumbList` | Docs zh-CN child | 200 or 404 |
| `DOCS-ARTICLE` | one published implementation task or reference | `META-DOCS` | `BreadcrumbList`; `Article` only when eligible | locale child only while published | 200 published; 404 unknown; 410 withdrawn |
| `DOCS-API` | one released API resource | `META-DOCS` plus checked API source | `BreadcrumbList`; no unsupported product schema | locale child only after API release | 200 released; 404 unknown; 410 removed |
| `DOCS-SEARCH` | no acquisition intent; internal search only | query UI source | none | no | 200 results page with noindex; malformed query 400 |

API Keys and generic outbound Webhooks documentation MUST remain release-labelled `REQUIRED` until their APIs pass G3/G6. A document may describe the approved contract before implementation only when it is visibly labelled “not released” and excluded from sitemap/internal acquisition links.

### 3.3 Non-indexable public/resource registry

| Route ID | Path | Surface; purpose; access | Status | Capability / API dependency | Applicable states | Index | Canonical / alternate | Internal-link parents |
|---|---|---|---|---|---|---|---|---|
| `PUB-SHORT-OFFICIAL` | `https://{official-short-host}/{code}` | Public; resolve an official-host short link; public | `REQUIRED` | `CAP-LINKS`, `CAP-DESTINATION-RISK`; redirectengine `GET /{code}` | allow-redirect, password, expired, exhausted, paused, risk-safety, not-found | `noindex` | none / none | generated link only; never main navigation |
| `PUB-SHORT-CUSTOM` | `https://{custom-host}/{code}` | Public; resolve a custom-host short link; public | `REQUIRED` | `CAP-LINKS`/`CAP-DOMAIN-OWNERSHIP`/`CAP-DOMAIN-HTTPS`/`CAP-DESTINATION-RISK`; V10 `CAP-DOMAIN-ENTITLEMENT`/`CAP-DOMAIN-RISK` enforcement | allow-redirect, password, expired, exhausted, domain-unavailable, risk-safety, not-found | `noindex` | none / none | generated link only |
| `PUB-TEXT` | `/t/{slug}` | Public; render an authorized text share; public/password as configured | `REQUIRED` | `CAP-TEXT`; `GET/POST /t/{slug}` and `POST /api/public/text/{slug}` | available, password-required, consumed, expired, removed, not-found | `noindex` | none / none | Workspace share action only |
| `PUB-BIO` | `/p/{slug}` | Public; render a published Bio page; public | `REQUIRED` | `CAP-BIO`, `CAP-BIO-OPT-IN-INDEX DEFERRED`; `GET /p/{slug}` and `GET /api/public/bio/{slug}` | available, paused, risk-blocked-child-link, removed, not-found | `noindex` | none / none | Workspace publish action only |
| `PUB-FILE-PAGE` | `/f/{slug}` | Public; render file-share gate and metadata; public/password as configured | `REQUIRED` | `CAP-FILES`; required Nginx rewrite to file public API | available, password-required, scan-pending, blocked, expired, download-limit, removed | `noindex` | none / none | Workspace share action only |
| `PUB-FILE-BINARY` | `/api/public/files/{slug}` | Public; stream an allowed file or deny; public/password as configured | `REQUIRED` | `CAP-FILES`; `GET /api/public/files/{slug}` | download, password-required, quarantined, scan-error, blocked, expired, removed | `noindex` | none / none | `PUB-FILE-PAGE` |
| `PUB-LINK-UNAVAILABLE` | `/linkunavailable?reason={allowlisted}&code={safe-code}` | Public; explain a fail-closed link/domain state without target disclosure; public | `REQUIRED` | `CAP-DESTINATION-RISK` required page/risk redirect plus V10 domain controls; static page with reason allowlist | pending, review, blocked, domain-suspended, domain-revoked, domain-expired, operational-unavailable | `noindex` | none / none | redirectengine/system only |
| `PUB-ABUSE-REPORT` | `/abuse/report` | Public; submit an abuse report; public | `REQUIRED` | `CAP-ABUSE`, `CAP-TURNSTILE`; V10 page using `POST /api/public/abuse-reports` | input, submitting, success-persistent, validation-error, Turnstile-error, rate-limited | `noindex` | none / none | Security, Legal/Abuse, safety surface |
| `PUB-STATUS` | `/status` | Public; show public operational status; public | `REQUIRED` | operations status; `GET /api/public/status` | loading-data, current, partial-degradation, unavailable, stale | `noindex` | none / none | Footer, error/maintenance pages |
| `PUB-ANNOUNCEMENTS` | `/announcements` | Public; show operational notices; public | `REQUIRED` | `CAP-ANNOUNCEMENTS-SETTINGS`; `GET /api/public/announcements` | loading-data, empty, current, service-error | `noindex` | none / none | Website announcement bar, Footer |
| `PUB-PREVIEW` | `/preview/{resourceType}/{token}` | Public; owner-authorized unpublished preview; signed token | `REQUIRED` | relevant resource capability; `REQUIRED` signed preview API | valid, expired-token, revoked-token, forbidden, not-found | `noindex` | none / none | owner editor only |
| `API-PUBLIC-REQUIRED` | `/api/public/settings`, `/status`, `/announcements`, `/plans`, `/announcement-bar`, `/invoice-download/{ticket}`, `/account-policy`, `/auth/*`, `/email-code`, `/register-email-code`, `/login-email-code`, `/turnstile`, `/campaigns/{campaign}/conversion`, `/text/{slug}`, `/bio/{slug}`, `/files/{slug}` and `/abuse-reports` | Public; registered machine response/callback set; endpoint-specific | `REQUIRED` | `CAP-ANNOUNCEMENTS-SETTINGS` where applicable; exact method/path registration in `SRC-HTTP`; owning Auth/Public/Website route supplies the other capability | endpoint-specific success/error/rate states | `noindex` | none / none | owning product/task route only |
| `API-PUBLIC-V10` | `/api/public/contact` and `/api/public/previews/{token}` | Public; contact submission and signed preview read; endpoint-specific | `REQUIRED` | `CAP-TICKETS` and resource capability; release-gated `POST /api/public/contact` and `GET /api/public/previews/{token}` controllers | success, validation-error, forbidden, expired-token, revoked-token, not-found, rate-limited | `noindex` | none / none | `WEB-CONTACT` or owner preview action only |

| Route IDs | Search intent / topic | Metadata source | Structured data | Sitemap | HTTP and robots contract |
|---|---|---|---|---|---|
| `PUB-SHORT-OFFICIAL`, `PUB-SHORT-CUSTOM` | none; redirect surface | none | none | no | 3xx only after current-fingerprint allow; response carries applicable `X-Robots-Tag: noindex, nofollow` |
| `PUB-TEXT` | none; UGC resource | resource-safe title only | none | no | 200 available; 401/403 gate; 404 unknown; 410 expired/removed; HTML and header noindex |
| `PUB-BIO` | none; UGC resource | resource-safe title only | none | no | 200 published; 404 unknown; 410 removed; noindex; rendered outbound links use `rel="ugc nofollow"` |
| `PUB-FILE-PAGE` | none; UGC file gate | safe filename/type only | none | no | 200 allowed gate; 401/403 gate; 404/410 lifecycle; HTML/header noindex |
| `PUB-FILE-BINARY` | none; non-HTML resource | none | none | no | endpoint lifecycle status plus `X-Robots-Tag: noindex, nofollow` on every response |
| `PUB-LINK-UNAVAILABLE` | none; safety explanation | `META-SYSTEM` | none | no | safety page 200 with noindex; source redirect remains fail closed |
| `PUB-ABUSE-REPORT` | none; trust form | `META-SYSTEM` | none | no | 200 form; POST 2xx/4xx/429; no-store and noindex |
| `PUB-STATUS`, `PUB-ANNOUNCEMENTS` | none; operational information | `META-SYSTEM` or published notice record | none | no | 200 current, 503 when unavailable; noindex |
| `PUB-PREVIEW` | none; private preview | none | none | no | 200 only with valid scoped token; otherwise 403/404/410; no-store and noindex |
| `API-PUBLIC-REQUIRED`, `API-PUBLIC-V10` | none; machine interface | none | none | no | endpoint status plus `X-Robots-Tag: noindex, nofollow` |

### 3.4 Canonicalization and compatibility rule

GoJet V10 Greenfield release has no required legacy browser aliases. Only canonicalization explicitly defined by this IA may redirect, using one 308 hop. Future compatibility aliases require change control, unique Route ID, query allowlist, noindex behavior, tests and a removal owner/date.

---

## 4. Shell contracts

### 4.1 Website Shell

使用 Design System §6 Website 布局 token 和 §7 Motion token。Header 包含 Logo、Products、Solutions、Developers、Pricing、Docs、Sign in、Get started；mobile 使用全高 Sheet。Header sticky 不能遮挡 anchor target，所有主要导航是真实链接。

适用状态：normal、menu-open、locale-switch、announcement、maintenance-banner。Loading、empty、permission denied 不适用于 Shell。

### 4.2 Auth Shell

Desktop 为品牌/产品视觉与认证表单分区；tablet/mobile 转单栏品牌 header。表单使用 Design System Auth control tokens，可见 label 和 persistent error。Ambient motion 仅品牌侧且遵循 reduced motion。

适用状态：normal、submitting、server-error、rate-limited、provider-error、maintenance。无 empty state。

### 4.3 Docs Shell

使用 Design System Docs header/left nav/article/right ToC tokens。Header 提供 Search、Language、Theme、Go to Workspace。Pagefind 搜索支持快捷键、方向键和 Escape；不默认做个性化搜索追踪。

适用状态：article、search-open、nav-drawer、not-found、offline-static。无 quota/billing state。

### 4.4 Workspace Shell

Sidebar：Logo、WorkspaceSwitcher、Create、Navigation、Support、User。Header：breadcrumbs/context、Command、Help、Notifications、Avatar。导航分 CREATE、INSIGHTS、MANAGE、DEVELOPER、WORKSPACE；Folders、A/B、UTM、Routing、Access 保持资源内部能力。

适用状态：loading-workspace、workspace-empty、read-only-role、workspace-suspended、API-offline、notification-attention。Shell 不展示资源级 empty 文案。

- **Global Create**：Link/QR/File/Text/Bio 五类入口；无创建权限、quota reached 或 Workspace suspended 时显示具体原因与 remediation，禁止死入口。
- **Command palette**：`Ctrl/Cmd+K`；支持 Navigation、Create、可访问资源和 Settings；RBAC/tenant 过滤后才展示结果。网络失败时保留本地导航命令并显示 partial state。
- **Notifications**：Header badge 同时有数字/accessible label；Popover 提供最近通知与 View all，完整历史进入 `/app/notifications`；Security/Billing/Domain/Ticket 通知 deep-link 到可操作上下文。
- WorkspaceSwitcher、Command、Notifications 均支持 Esc 关闭、focus return、mobile full-height Sheet；禁止 overlay 叠层打开。

### 4.5 Admin Shell

高密度导航分 Customers、Resources、Trust & Safety、Operations、Commerce、Access、Platform。显示当前管理员、权限范围、环境和审计入口。高风险菜单不因前端隐藏而替代后端 RBAC。

适用状态：admin-auth-required、permission-denied、maintenance、partial-service-degradation、normal。无客户 quota state。

- Admin 全局搜索只返回当前权限可见的 user/workspace/resource/risk/ticket，并显示类型标签与安全 disambiguation。
- 高风险 action 永不只放 hover menu；必须有可见 trigger、reason/impact confirmation 与 audit result。
- Dense table 默认保留 3–5 个决策关键字段，其余进入 column chooser/detail drawer；用户偏好可持久化，但 mandatory risk/permission/payment state 列不可隐藏。

### 4.6 Installer Shell

不复用 Marketing Hero。显示步骤、依赖状态、masked credentials、retry 和明确硬失败。完成后锁定 Installer，CTA 指向 Admin Login。

适用状态：session-ready、step-checking、step-pass、hard-failure、retryable-failure、install-running、lock-failed、complete、already-locked。Installer 没有业务资源 empty、customer quota、offline-success 或 stale-success 状态。

### 4.7 Shell-to-page state rule

Shell state describes navigation and shared service boundaries only. Route tables in §§3、6、7、10、13 are authoritative for page states. A page MUST NOT inherit states merely because another page in the same shell supports them. Loading is used only for client-protected data regions; indexable Website/Docs content MUST be present in initial HTML and cannot use a loading-only document.

---

## 5. Website page contracts

### 5.1 Home `WEB-HOME`

页面顺序：Hero → Capability Ribbon → Create → Control → Understand → Operate → Use Cases → Security → Developer → CTA → Footer。

- Hero 使用真实 Workspace frame、QR、Analytics、Jet Path；主 CTA Get started，次 CTA Explore products。允许轻量 breathing/Jet Path/产品浮层循环，但离开 viewport 或页面 hidden 时必须暂停，文字与 CTA 本身不持续漂移；
- Capability Ribbon 为 Links/QR/Files/Text/Bio，不做巨大 card；
- Create 展示真实 link creation；Control 展示 custom domain/routing/A-B/access/expiry。Product Stage 使用 release-candidate UI 或明确标记的 deterministic demo data；hover/click 可切换步骤，但自动演示不得抢 focus 或阻断 CTA；
- Understand 使用真实 analytics；Operate 展示 workspace/members/campaign/tags/audit；
- Security 明确 ClamAV 只负责文件，destination risk 负责 URL；
- Developer 只展示已经通过所属 Gate 的接口；API Keys 与 generic webhook 在 Gate 通过前不得作可用演示。

适用状态：default、announcement、pricing/API partial failure（相关模块降级而主内容保持）、maintenance。禁止 loading-only 初始 HTML。

### 5.2 Product pages

共用结构：purpose Hero + real product stage → workflow → control/security → measurable outcome → related products/guides/docs → CTA。各页独有重点：

| Route | Required sections |
|---|---|
| WEB-LINKS route reference | create, domain, routing, A-B, access, analytics, history, API |
| WEB-QR route reference | build, target, download formats, analytics, domain relationship |
| WEB-FILES route reference | upload, quarantine, ClamAV, password/expiry, download, audit |
| WEB-TEXT route reference | plain/Markdown/code, reader, password/expiry, custom code/domain |
| WEB-BIO route reference | builder, theme, social, analytics, domain, default noindex safety |
| WEB-ANALYTICS route reference | clicks, geo, device, referrer, campaign, retention/recovery |
| WEB-ROUTING route reference | country/device/language/source, A-B, fallback, server authority |
| WEB-DOMAINS route reference | entitlement, TXT ownership, ingress DNS, HTTPS, domain risk, assignment |

`WEB-DOMAINS` 必须说明不是任何账户都能直接添加：Business 自动获得资格，其他套餐可 Request access；即使域名已验证，其短链仍执行 destination risk。

### 5.3 Solutions, Pricing, Security and content

- Solutions 每页组合真实产品 UI 与适用场景摄影，不使用纯图库页；
- Pricing 从同一 plan source/build snapshot 读取价格、周期、currency、quota 和 `domain_limit`；Business custom-domain 权益明确，人工批准不作为公开套餐承诺；
- Security 分 Account、Platform、Destination、File、Domain/Abuse、Audit 和 disclosure；不展示未验证认证 Logo；
- Contact 使用真实 backend、Turnstile 和 persistent success；
- Legal 使用长文布局、last updated、print-friendly、无 marketing animation；
- Guides 只发布有 owner、独特任务和完整内容的静态页面，禁止批量薄内容。

---

## 6. Auth routes and page contracts

| Route ID | Path | Surface; purpose; access | Status | Capability / API dependency | Applicable states | Index | Canonical / alternate | Internal-link parents |
|---|---|---|---|---|---|---|---|---|
| `AUTH-LOGIN` | `/login` | Auth; authenticate a customer; public | `REQUIRED` | `CAP-AUTH`; `POST /api/auth/login`, `POST /api/public/login-email-code`, `GET /api/public/auth/providers` | input, submitting, invalid, account-locked, verification-required, Turnstile-required, rate-limited, provider-error, success | `noindex` | none / none | Website header, Register, password reset |
| `AUTH-REGISTER` | `/register` | Auth; create and verify a customer account; public | `REQUIRED` | `CAP-AUTH`; `POST /api/auth/register`, `POST /api/public/email-code`, `POST /api/public/register-email-code` | input, code-sent, code-expired, conflict, Turnstile-required, rate-limited, success | `noindex` | none / none | Website CTA, Login |
| `AUTH-VERIFY` | `/verify-email` | Auth; complete email verification; public token | `REQUIRED` | `CAP-AUTH`; V10 route using `POST /api/auth/verifyemail` and `POST /api/mail/verification` | verifying, success, invalid-token, expired-token, reused-token, resend-limited | `noindex` | none / none | verification email, Login |
| `AUTH-FORGOT` | `/forgot-password` | Auth; request a password reset without account enumeration; public | `REQUIRED` | `CAP-AUTH`; V10 route using `POST /api/auth/forgotpassword` | input, submitting, submitted-neutral, Turnstile-error, rate-limited | `noindex` | none / none | Login |
| `AUTH-RESET` | `/reset-password?token={opaque}` | Auth; set a new password using a valid reset grant; public token | `REQUIRED` | `CAP-AUTH`; V10 route using `POST /api/auth/resetpassword` | token-check, input, submitting, invalid-token, expired-token, reused-token, success | `noindex` | none / none | reset email, Forgot password |
| `AUTH-INVITE` | `/invite/{token}` | Auth; inspect and accept/reject a Workspace invitation; public token then authenticated user | `REQUIRED` | `CAP-WORKSPACE`; V10 route using `POST /api/invitations/accept` and `POST /api/invitations/reject` | unauthenticated, valid, account-mismatch, expired, revoked, accepted, rejected | `noindex` | none / none | invitation email, Login/Register |
| `AUTH-OAUTH-CALLBACK` | `/oauth/{provider}/callback` | Auth; complete a browser handoff after the provider API callback; public state-bound handoff | `REQUIRED` | `CAP-OAUTH`; `GET /api/public/auth/{provider}/callback` and `POST /api/public/auth/handoff` | processing, state-error, provider-error, handoff-expired, registration-required, binding-success, login-success | `noindex` | none / none | provider start only |
| `AUTH-SOCIAL-REG` | `/social-registration?code={opaque}` | Auth; complete a provider-created account; public handoff | `REQUIRED` | `CAP-OAUTH`, `CAP-AUTH`; V10 route using `GET /api/public/auth/social-registration` and `POST /api/public/auth/social-registration/complete` | loading-handoff, form, missing-provider-email, email-code, conflict, expired-handoff, success | `noindex` | none / none | OAuth callback only |

All Auth routes use `no-store`, stay outside every sitemap and emit page/header noindex. Formal auth/session tokens MUST NOT be placed in localStorage. Forgot-password success is response-neutral; OAuth errors expose an allowlisted category, not provider tokens or raw callback parameters.

---

## 7. Workspace Route Registry

All rows use the Workspace surface, authenticated access, `noindex`, no canonical/locale alternate, `no-store` and no sitemap membership. The last column is the task-navigation parent.

| Route ID | Path | Surface; purpose; access | Status | Capability / exact API dependency | Applicable states | Index | Canonical / alternate | Internal parent |
|---|---|---|---|---|---|---|---|---|
| `APP-OVERVIEW` | `/app` | Workspace; summarize actionable workspace state; member | `REQUIRED` | `CAP-WORKSPACE`, `CAP-ANALYTICS`; V10 route using `GET /api/workspaces/{id}/overview` | loading, empty-new-workspace, partial-analytics, attention, API-error | `noindex` | none / none | Workspace shell |
| `APP-NOTIFICATIONS` | `/app/notifications` | Workspace; review actionable product notifications; member | `REQUIRED` | `CAP-NOTIFICATIONS`; `GET /api/workspaces/{id}/notifications`, read/mark-all-read actions | loading, empty, unread, filtered, partial, stale, error | `noindex` | none / none | Workspace header Notifications |
| `APP-LINKS` | `/app/links` | Workspace; find and manage links; role by action | `REQUIRED` | `CAP-LINKS`; `GET /api/workspaces/{id}/links` and bulk/export routes | loading, empty, filtered-empty, partial-risk, read-only, quota-reached, rate-limited, error | `noindex` | none / none | Workspace navigation |
| `APP-LINK-NEW` | `/app/links/new` | Workspace; create one link; link-manage role | `REQUIRED` | `CAP-LINKS`, `CAP-OFFICIAL-DOMAINS`, custom-domain capabilities, `CAP-DESTINATION-RISK`; route using create/link-domain APIs | input, submitting, quota-reached, domain-unavailable, risk-pending, conflict, error | `noindex` | none / none | Links, global Create |
| `APP-LINK-DETAIL` | `/app/links/{linkId}` | Workspace; inspect and change one link; read/manage by action | `REQUIRED` | `CAP-LINKS`, `CAP-LINK-HISTORY`, `CAP-OFFICIAL-DOMAINS`, routing/A-B/UTM/risk; route using link/detail/version/analytics/risk APIs | loading, success, read-only, risk-pending, risk-review, risk-block, conflict, deleted, error | `noindex` | none / none | Links |
| `APP-QR` | `/app/qr` | Workspace; list/create QR resources; role by action | `REQUIRED` | `CAP-QR`; `GET/POST /api/workspaces/{id}/qr-codes` | loading, empty, create, risk-denied, quota-reached, error | `noindex` | none / none | Workspace navigation |
| `APP-QR-DETAIL` | `/app/qr/{qrId}` | Workspace; preview/download/delete a QR resource; role by action | `REQUIRED` | `CAP-QR`; route using QR/share-QR/delete APIs | loading, ready, source-link-review, source-link-block, deleted, error | `noindex` | none / none | QR list |
| `APP-FILES` | `/app/files` | Workspace; upload and manage file shares; role by action | `REQUIRED` | `CAP-FILES`, `CAP-CLAMAV-REQUIRED`; fileshare APIs | loading, empty, uploading, quarantined, scanning, safe, blocked, scan_error, quota-reached | `noindex` | none / none | Workspace navigation |
| `APP-FILE-DETAIL` | `/app/files/{fileId}` | Workspace; inspect one file lifecycle and access policy; role by action | `REQUIRED` | `CAP-FILES`, `CAP-CLAMAV-REQUIRED`; route using file/delete APIs plus V10 scan evidence fields | loading, quarantined, scanning, safe, blocked, scan_error, expired, deleted | `noindex` | none / none | Files |
| `APP-TEXT` | `/app/text` | Workspace; list/create text shares; role by action | `REQUIRED` | `CAP-TEXT`; required text-share APIs | loading, empty, edit, read-only, quota-reached, error | `noindex` | none / none | Workspace navigation |
| `APP-TEXT-DETAIL` | `/app/text/{shareId}` | Workspace; edit one text share; role by action | `REQUIRED` | `CAP-TEXT`; route using text detail/update/delete APIs | loading, edit, read-only, preview, conflict, expired, deleted, error | `noindex` | none / none | Text list |
| `APP-BIO` | `/app/bio` | Workspace; list/create Bio pages; role by action | `REQUIRED` | `CAP-BIO`; required Bio APIs | loading, empty, edit, preview, publish-error, quota-reached | `noindex` | none / none | Workspace navigation |
| `APP-BIO-DETAIL` | `/app/bio/{pageId}` | Workspace; edit/publish one Bio page; role by action | `REQUIRED` | `CAP-BIO`, `CAP-BIO-OPT-IN-INDEX DEFERRED`, `CAP-DESTINATION-RISK`; route using Bio APIs | loading, draft, preview, published, child-link-review, child-link-block, conflict, deleted | `noindex` | none / none | Bio list |
| `APP-ANALYTICS` | `/app/analytics` | Workspace; inspect measured link/resource activity; analytics permission | `REQUIRED` | `CAP-ANALYTICS`; link analytics and overview APIs | loading, empty, partial, stale, retention-limited, error | `noindex` | none / none | Workspace navigation, Link detail |
| `APP-DOMAINS` | `/app/domains` | Workspace; inspect entitlement and domain axes; workspace member, manage for mutations | `REQUIRED` | required route/domain list plus `CAP-DOMAIN-ENTITLEMENT` and `CAP-DOMAIN-RISK` dependencies | locked, requested, active-empty, active-list, verification, grace_period, suspended, expired, revoked, partial-axis | `noindex` | none / none | Workspace navigation |
| `APP-DOMAIN-NEW` | `/app/domains/new` | Workspace; register and verify a hostname; workspace manage plus active entitlement | `REQUIRED` | all domain capabilities; required create/verify APIs gated by V10 entitlement APIs | entitlement-denied, input, conflict, ownership-pending, DNS-invalid, HTTPS-error, risk-review, ready, error | `noindex` | none / none | Domains |
| `APP-DOMAIN-DETAIL` | `/app/domains/{domainId}` | Workspace; inspect/revalidate one domain; member/read, manage for actions | `REQUIRED` | all domain capabilities; V10 detail/revalidation contract plus required verify | loading, verification, ready, ownership-failed, DNS-invalid, HTTPS-error, risk-review, grace_period, suspended, revoked | `noindex` | none / none | Domains |
| `APP-ORGANIZATION` | `/app/organization` | Workspace; manage organization metadata; role by action | `REQUIRED` | `CAP-WORKSPACE`, `CAP-CAMPAIGNS`, `CAP-FOLDERS-TAGS`; required organization API | loading, success, read-only, validation-error, conflict, error | `noindex` | none / none | Workspace navigation |
| `APP-CAMPAIGNS` | `/app/campaigns` | Workspace; list/create campaigns; role by action | `REQUIRED` | `CAP-CAMPAIGNS`; route using workspace campaign APIs | loading, empty, edit, read-only, conflict, error | `noindex` | none / none | Organization, Workspace navigation |
| `APP-TAGS` | `/app/tags` | Workspace; list/create tags; role by action | `REQUIRED` | `CAP-FOLDERS-TAGS`; route using workspace tag APIs | loading, empty, edit, read-only, conflict, in-use, error | `noindex` | none / none | Organization, Workspace navigation |
| `APP-API-KEYS` | `/app/api-keys` | Workspace; issue and revoke scoped user API keys; dedicated role | `REQUIRED` | `CAP-API-KEYS`; `REQUIRED /api/workspaces/{id}/api-keys*` | loading, empty, create, secret-once, expired, revoked, forbidden, error | `noindex` | none / none | Developer navigation |
| `APP-WEBHOOKS` | `/app/webhooks` | Workspace; configure generic outbound webhooks; dedicated role | `REQUIRED` | `CAP-USER-WEBHOOKS`; `REQUIRED /api/workspaces/{id}/webhooks*` | loading, empty, create, delivery, retrying, disabled, secret-rotate, forbidden, error | `noindex` | none / none | Developer navigation |
| `APP-MEMBERS` | `/app/members` | Workspace; manage members, roles and invitations; role by action | `REQUIRED` | `CAP-WORKSPACE`; route using member/invitation APIs | loading, empty-no-invites, invite, read-only, last-owner-protected, invitation-expired, error | `noindex` | none / none | Workspace navigation |
| `APP-BILLING` | `/app/billing` | Workspace; inspect plan, usage, invoices and payments; owner/manage billing | `REQUIRED` | `CAP-BILLING`, `CAP-PAYMENTS`; required billing/invoice/payment APIs | loading, active, payment-pending, payment-failed, overdue, canceled, provider-partial, error | `noindex` | none / none | Workspace navigation |
| `APP-SETTINGS` | `/app/settings/profile`, `/app/settings/security`, `/app/settings/sessions`, `/app/settings/connected-accounts`, `/app/settings/workspace` and `/app/settings/danger` | Workspace; manage account/workspace settings; role by section | `REQUIRED` | `CAP-AUTH`, `CAP-OAUTH`, `CAP-WORKSPACE`; required route family and `/api/me*`, social identity and workspace APIs | loading, success, read-only, validation-error, session-revoked, provider-error, destructive-confirm | `noindex` | none / none | User menu, Workspace navigation |
| `APP-SUPPORT` | `/app/support` | Workspace; list support tickets; ticket owner/workspace member | `REQUIRED` | `CAP-TICKETS`; `GET /api/support/tickets` | loading, empty, open, awaiting-user, awaiting-support, closed, error | `noindex` | none / none | Workspace navigation |
| `APP-SUPPORT-NEW` | `/app/support/new` | Workspace; create a ticket or domain-access request; authenticated user | `REQUIRED` | `CAP-TICKETS`; V10 route uses `POST /api/support/tickets`; custom-domain category creates a ticket only | input, attachment, Turnstile-required, submitting, success, rate-limited, error | `noindex` | none / none | Support, Domains Request access |
| `APP-SUPPORT-THREAD` | `/app/support/{ticketId}` | Workspace; read/reply to one ticket; ticket owner/workspace member | `REQUIRED` | `CAP-TICKETS`, `CAP-MAIL`; route using support detail/reply/state APIs | loading, open, replying, awaiting, closed, forbidden, attachment-blocked, error | `noindex` | none / none | Support list, Domain request status |

The UI guard is not authorization. Every direct URL load and every underlying API request repeats authentication, tenant and role checks.

---

## 8. Workspace page composition

### 8.1 Overview

Greeting + Create（Link/QR/File/Text/Bio）→ Recent → 单一 7d/30d Performance trend → Attention → Activity。Attention 汇总 domain verification/entitlement grace、quota、failed payment、security 和 ticket reply，不做 KPI card wall。

### 8.1.1 Notifications

`APP-NOTIFICATIONS` 使用 grouped timeline/list，而不是 dashboard card grid。默认 unread first + time，可筛选 All / Security / Domains / Billing / Support / Resources。每条包含 category icon、标题、时间、Workspace/resource context、最多两行安全摘要和一个 primary deep-link。支持单条 mark read/unread 与 mark all read；读取状态可 optimistic，但安全/计费事实不得由前端 optimistic 推断。

### 8.2 Links list and create

List：PageHeader、count、Create link、search、domain/status/campaign/tag/date filters、columns、DataTable。Mobile 转 resource list row。Bulk actions 为 pause/activate/tag/folder/export/delete。

Create 使用 route-backed Sheet/full-screen mobile。默认 destination/domain/code/title；advanced 为 expiration/password/click limit/one-time/campaign/tags。Routing/A-B 在创建后 Detail 配置。选择自定义域名时，服务端必须再次检查 active entitlement、ownership、DNS、HTTPS 和 domain risk。

### 8.3 Link detail and safety

Tabs：Overview、Analytics、Routing、A/B Test、UTM、Access、QR、Settings、History。主 destination、routing 和 A/B 的任何编辑均使当前 fingerprint 失效，页面显示 pending，直到新 decision 为 allow。

自定义域名不改变 destination risk 状态机。review/block/missing/malformed/stale 进入 safety surface，且在 smart routing、A/B、UTM、password、expiry、click limit 之前执行。页面不得提供“仍然访问目标”的绕过链接。

### 8.4 QR, Text, Bio and Analytics

- QR：list/create/detail/preview/download/analytics；只显示代码实际支持的样式；
- Text：plain/Markdown/code editor、preview、password、expiry、code/domain；public noindex；
- Bio：Content/Appearance/Social/Domain/Analytics/SEO/Settings；默认 noindex；
- Analytics：date/compare/export + resource/domain/campaign/country/device/referrer；trend、top resources、referrers、countries、devices、browsers、OS；partial/stale 明确，不伪装实时。

### 8.5 Files

List 显示 file/size/security state/downloads/expiry/created。Upload 接受 drag/paste/browse，提交前显示限额和 scan policy。状态使用 Design System 的 quarantined/scanning/safe/blocked/scan_error；只有 safe 可公开。scan_error 时保持 private，禁止 optimistic publish。

### 8.6 Billing and Settings

Billing 显示 current plan、usage、renewal、plans、quota、payment methods、orders/invoices、currency。支付 pending/failed 不做 optimistic success。Settings 分 Profile、Security、Sessions、Notifications、Connected Accounts、Workspace、Danger Zone。

### 8.7 Cross-surface product UX

**First-run / empty Workspace**：`empty-new-workspace` 使用真实状态 checklist：Create first link → configure optional domain → invite member → inspect analytics；不显示虚构百分比，非必要步骤可跳过，完成后自动让出主内容空间。

**Search / filter / table state**：Links、Analytics、Files、Domains、Campaigns、Tags、Support 与 Admin list 的 query/filter/sort/page 尽量编码到 URL search params；secret、原始 PII 与 risk evidence 不进入 URL。Clear all 始终可达，`filtered-empty` 与 true empty 分开。

**Unsaved work**：Link create、Settings 和高风险表单 dirty 离开时提示；Text/Bio 长编辑器使用 server-backed draft/autosave + optimistic concurrency，显示 `Saving… / Saved / Offline—changes not synced / Conflict`。Password、OAuth、payment、API-key secret 禁止 autosave 到 browser storage。

**Long-running operations**：File upload/scan、analytics export、domain revalidation、risk rescan、bulk action 显示 queued/running/progress/result；离开页面后通过 Notifications 恢复结果上下文。未知时长不得只显示无限 spinner。

**Bulk actions**：persistent selection bar 显示 `N selected` 和跨页范围；筛选变化不得静默扩大选择，destructive bulk action 显示分项成功/失败。

**Copy/download feedback**：copy 使用就地 `Copied` 状态；secret 明确只显示一次。Download/export 显示文件名或任务结果，不以 Toast 为唯一结果载体。

---

## 9. Custom-domain user flow

### 9.1 `/app/domains` state contract

| State | Page content | Primary actions | Forbidden behavior |
|---|---|---|---|
| not entitled / `locked` | 资格说明、当前 plan、两种开通路径 | `Upgrade to Business`; `Request access` | 显示 Add domain；只靠前端阻止 API |
| request submitted / `requested` | ticket number、submitted time、pending copy | View ticket | 暗示工单已授权；显示验证步骤 |
| `active` | source (`plan`/`manual_approval`)、`domain_limit`、remaining、expiry、domain table | Add domain | 隐藏限额/来源 |
| `grace period` | exact deadline、原因、现有域名影响 | Renew/upgrade；Request manual review | 新增域名；模糊截止时间 |
| `suspended` | security/abuse reason category、effective time | View support/appeal path | 立即重新激活 |
| `expired` | expired date、现有 routing 状态 | Upgrade；Request access | silent official-domain fallback |
| `revoked` | decision category、audit reference when user-visible | Support path when allowed | 无权 retry |

`Request access` 进入 `/app/support/new?category=custom-domain-access`，提交只调用 `POST /api/support/tickets` 并创建一个带固定类别的工单。entitlement queue 将合格工单投影为 `requested`，不创建 active entitlement；任何 direct create-domain API 在 entitlement inactive 时返回明确拒绝。

### 9.2 Add Domain Wizard

```text
1 Entitlement preflight
2 Hostname normalization and conflict check
3 DNS TXT ownership
4 DNS ingress target
5 HTTPS readiness
6 Domain risk decision
7 Ready for links
```

每步显示当前 axis，不把 entitlement、ownership、DNS、HTTPS 和 domain risk 合成一个 status。DNS example 由服务端生成并可复制；用户触发 verify 后显示查询时间和结果。只有五个权威条件全部满足才显示 Ready。

### 9.3 Domain detail

区域：Overview、Entitlement、Ownership record、Ingress DNS、HTTPS、Domain risk、Assigned resources、Revalidation history、Settings/Danger。所有权丢失、DNS 漂移、证书错误、risk review/block 和 entitlement suspension 分别给出原因和恢复权限。

周期复验发生时，页面可显示 previous verified time 和 next policy check，不承诺固定到秒的扫描时间。

### 9.4 Entitlement authority and bypass contract

The entitlement resolver is `REQUIRED` and returns a structured server record with `capability=custom_domains`, `source=none|plan|manual_approval`, `status=requested|active|suspended|expired|revoked`, `domain_limit`, `starts_at`, `expires_at`, `granted_by`, `support_ticket_id` and `decision_reason`. A ticket-projected request uses `source=none, status=requested`; only a valid plan or recorded manual approval can resolve active.

- An active `business` subscription in good account standing automatically resolves `source=plan, status=active` with an enforced positive `domain_limit` from structured entitlement data. Public plan feature text is not authority.
- Every other plan can select `Request access`. That action creates one categorized support ticket only; the queue projects it as requested/no access until a different administrator with `domains.entitlements.manage` records a reasoned `manual_approval`.
- A valid plan entitlement and a valid manual approval may coexist. The resolver uses the valid sources according to the Master Plan without weakening the stricter security state.
- Normal downgrade immediately denies new registration, activation, restoration, rotation and new link assignment. Existing active domains receive exactly seven calendar days of derived `grace_period`. At the deadline the plan entitlement becomes expired unless another valid source exists; routing then uses the branded unavailable surface and never falls back to an official GoJet host.
- Abuse, fraud, ownership loss and security suspension take effect immediately without grace. Suspended/revoked pages show only an allowlisted reason category and support path; they provide no unsafe self-reactivation.

| Attempt | Required server check | Denial behavior |
|---|---|---|
| Load `/app/domains` | authenticated Workspace membership | render locked/requested/read-only state; do not expose mutation controls |
| Deep-link `/app/domains/new` | Workspace manage + active entitlement + remaining `domain_limit` | route renders the locked/forbidden boundary; it MUST NOT mount or prefill the wizard |
| `POST /api/support/tickets` with `category=custom-domain-access` | membership, eligible request state, Turnstile, rate/idempotency policy | create one support ticket only; entitlement queue projection is `requested` and never active |
| `POST /api/workspaces/{id}/domains` | manage + active entitlement + limit + hostname policy | `403 entitlement_required` or `409 domain_limit_reached`; no domain row/token is created |
| Start/complete verification, activate, restore or rotate | manage + current entitlement + domain ownership | `403 entitlement_required` or axis-specific denial; no state advance |
| Assign custom domain during link create/update | active entitlement + remaining/existing authorization + ownership verified + DNS valid + HTTPS active + domain risk allow | same explicit denial for UI and crafted API; no link mutation |
| Resolve custom-host redirect | current entitlement/grace policy + domain trust + current target-fingerprint allow | fail closed to unavailable/safety surface; never resolve the customer target or official-host fallback |

Every denial response uses the shared error model with an allowlisted code, correlation ID and safe remediation. It MUST NOT reveal another Workspace, provider evidence or whether a cross-tenant hostname exists.

---

## 10. Admin Route Registry

Every row is Admin surface, uses `noindex`, no canonical/locale alternate, `no-store` and no sitemap membership. The permission in each row is a required server-side permission, not a navigation-display rule.

| Route ID | Path | Purpose; access | Status | Capability / exact API dependency | Applicable states | Index; canonical; alternate | Internal parent |
|---|---|---|---|---|---|---|---|
| `ADMIN-LOGIN` | `/admin/login` | authenticate an administrator; public to admin auth | `REQUIRED` | admin identity; V10 route using `POST /api/admin/auth/login` | input, submitting, invalid, TOTP-required, locked, rate-limited, success | `noindex`; none; none | Admin entry only |
| `ADMIN-OVERVIEW` | `/admin` | triage platform attention; `platform.read` | `REQUIRED` | `CAP-OPS-AUDIT`; required entry using `GET /api/admin/overview` and analytics overview | loading, normal, partial-service-degradation, stale, error | `noindex`; none; none | Admin shell |
| `ADMIN-USERS` | `/admin/users[/{userId}]` | govern user lifecycle; `users.manage` | `REQUIRED` | `CAP-AUTH`; `/api/admin/users*` | loading, empty, detail, suspended, deleted, destructive-confirm, error | `noindex`; none; none | Customers |
| `ADMIN-WORKSPACES` | `/admin/workspaces[/{workspaceId}]` | govern workspace owner/member/plan/status; `workspaces.manage` | `REQUIRED` | `CAP-WORKSPACE`, `CAP-BILLING`; workspace admin APIs | loading, empty, detail, partial-resource-counts, suspended, destructive-confirm, error | `noindex`; none; none | Customers |
| `ADMIN-LINKS` | `/admin/resources/links[/{linkId}]` | inspect and act on cross-workspace links; `links.manage` | `REQUIRED` | `CAP-LINKS`, `CAP-DESTINATION-RISK`; `/api/admin/links*` plus link risk API | loading, empty, detail, risk-pending, risk-review, risk-block, destructive-confirm, error | `noindex`; none; none | Resources |
| `ADMIN-DOMAINS` | `/admin/resources/domains[/{domainId}]` | inspect domain inventory without granting entitlement; `domains.manage` | `REQUIRED` | `CAP-DOMAIN-OWNERSHIP`/`HTTPS`; `/api/admin/domains*` | loading, empty, detail, ownership-failed, HTTPS-error, suspended, destructive-confirm, error | `noindex`; none; none | Resources |
| `ADMIN-QR-TEXT-BIO` | `/admin/resources/qr[/{resourceId}]`, `/admin/resources/text[/{resourceId}]` and `/admin/resources/bio[/{resourceId}]` | inspect resource inventory/action; dedicated resource permission | `REQUIRED` | `CAP-QR`/`CAP-TEXT`/`CAP-BIO`; admin resource inventory/actions | loading, empty, detail, quarantined, restored, deleted, error | `noindex`; none; none | Resources |
| `ADMIN-FILES` | `/admin/files[/{fileId}]` | govern scan/quarantine/restore/delete; `files.manage` | `REQUIRED` | `CAP-FILES`, `CAP-CLAMAV-REQUIRED`; file/resource actions | loading, empty, quarantined, scanning, safe, blocked, scan_error, destructive-confirm | `noindex`; none; none | Resources |
| `ADMIN-DEST-RISK` | `/admin/trust/destination-risk[/{riskId}]` | review target decisions, rescan and override; `security.manage` | `REQUIRED` | `CAP-DESTINATION-RISK`; V10 route using `/api/admin/destination-risks*` | loading, empty, pending, allow, review, block, stale-fingerprint, provider-partial, destructive-confirm | `noindex`; none; none | Trust & Safety |
| `ADMIN-DOMAIN-RISK` | `/admin/trust/domain-risk[/{domainId}]` | inspect ownership/DNS/HTTPS/reputation/revalidation evidence; `domains.risk.manage` | `REQUIRED` | `CAP-DOMAIN-RISK`; `REQUIRED /api/admin/domain-risks*` | loading, empty, pending, allow, review, block, revalidating, stale, provider-partial | `noindex`; none; none | Trust & Safety |
| `ADMIN-ABUSE` | `/admin/trust/abuse[/{reportId}]` | review abuse evidence and action; `security.manage` | `REQUIRED` | `CAP-ABUSE`; V10 route using `/api/admin/abuse*` | loading, empty, open, investigating, resolved, dismissed, destructive-confirm, error | `noindex`; none; none | Trust & Safety |
| `ADMIN-DOMAIN-ENTITLEMENTS` | `/admin/domain-entitlements` | triage entitlement requests; `domains.entitlements.manage` | `REQUIRED` | `CAP-DOMAIN-ENTITLEMENT`; `GET /api/admin/domain-entitlements` | loading, empty, queued, filtered-empty, stale, error | `noindex`; none; none | Access |
| `ADMIN-DOMAIN-ENTITLEMENT` | `/admin/domain-entitlements/{workspaceId}` | make an independent entitlement decision; `domains.entitlements.manage` | `REQUIRED` | `CAP-DOMAIN-ENTITLEMENT`; detail/decision APIs in §11.2 | loading, requested, active-plan, active-manual, expired, suspended, revoked, conflict, destructive-confirm | `noindex`; none; none | Entitlement queue, linked ticket |
| `ADMIN-TICKETS` | `/admin/tickets[/{ticketId}]` | manage ticket thread/SLA/internal notes; `tickets.manage` | `REQUIRED` | `CAP-TICKETS`, `CAP-MAIL`; V10 route using `/api/admin/support/tickets*` | loading, empty, open, awaiting, closed, replying, attachment-blocked, error | `noindex`; none; none | Operations |
| `ADMIN-ANNOUNCEMENTS` | `/admin/announcements` | manage scoped notices; `content.manage` | `REQUIRED` | `CAP-ANNOUNCEMENTS-SETTINGS`; route using announcements and `/api/admin/announcements*` | loading, empty, draft, scheduled, published, archived, validation-error | `noindex`; none; none | Operations |
| `ADMIN-MAIL` | `/admin/mail` | inspect queue/log/settings/test; `mail.manage` | `REQUIRED` | `CAP-MAIL`; route using mail/settings APIs | loading, empty, queued, sending, sent, failed, retrying, partial, error | `noindex`; none; none | Operations |
| `ADMIN-JOBS` | `/admin/operations/jobs` | inspect/requeue operational jobs; `operations.manage` | `REQUIRED` | service operations; required diagnostics/requeue APIs | loading, empty, running, failed, retrying, stale, error | `noindex`; none; none | Operations |
| `ADMIN-SERVICES` | `/admin/operations/services` | inspect eight service health records; `operations.manage` | `REQUIRED` | eight service IDs; required diagnostics/operations APIs | loading, healthy, partial-degradation, unavailable, restart-confirm, stale | `noindex`; none; none | Operations |
| `ADMIN-PLANS` | `/admin/commerce/plans` | manage plans, quotas and structured entitlements; `billing.manage` | `REQUIRED` | `CAP-BILLING` plus V10 entitlement fields; V10 route using `/api/admin/plans*` | loading, empty, draft, active, archived, validation-error, conflict | `noindex`; none; none | Commerce |
| `ADMIN-PAYMENTS` | `/admin/commerce/payments[/{paymentId}]` | inspect transactions and provider callbacks; `billing.manage` | `REQUIRED` | `CAP-PAYMENTS`, `CAP-PAYMENT-CALLBACKS`; required invoices/payment-callback APIs | loading, empty, pending, paid, failed, refunded, callback-invalid, partial | `noindex`; none; none | Commerce |
| `ADMIN-FX` | `/admin/commerce/fx` | inspect rates/provider/override history; `billing.manage` | `REQUIRED` | `CAP-PAYMENTS`; required billing settings/FX store | loading, current, stale, provider-error, override-confirm, validation-error | `noindex`; none; none | Commerce |
| `ADMIN-ADMINS` | `/admin/access/administrators[/{adminId}]` | govern administrators/MFA/sessions; `admins.manage` | `REQUIRED` | `CAP-ADMIN-ACCESS`; V10 route using `/api/admin/administrators*` and admin auth APIs | loading, empty, detail, active, suspended, TOTP, session-revoke-confirm, error | `noindex`; none; none | Access |
| `ADMIN-ROLES` | `/admin/access/roles` | inspect role templates and explicit permissions; `admins.manage` | `REQUIRED` | `CAP-ADMIN-ACCESS`; required administrator permission catalog | loading, role-list, permission-conflict, in-use, error | `noindex`; none; none | Access |
| `ADMIN-AUDIT` | `/admin/audit` | query immutable audit events; `platform.read` | `REQUIRED` | `CAP-OPS-AUDIT`; V10 route using `GET /api/admin/audit` | loading, empty, filtered-empty, detail, partial-diff, stale, error | `noindex`; none; none | Admin shell |
| `ADMIN-GENERAL` | `/admin/platform/general` | manage general platform settings and brand assets; `settings.manage` | `REQUIRED` | `CAP-ANNOUNCEMENTS-SETTINGS`; V10 route using `/api/admin/settings*` | loading, success, validation-error, conflict, maintenance-confirm, error | `noindex`; none; none | Platform |
| `ADMIN-OFFICIAL-DOMAINS` | `/admin/platform/official-domains` | manage official short hosts; `domains.manage` | `REQUIRED` | `CAP-OFFICIAL-DOMAINS`; V10 route using `/api/admin/official-domains*` | loading, empty, active, disabled, default-conflict, HTTPS-error, destructive-confirm | `noindex`; none; none | Platform |
| `ADMIN-OAUTH` | `/admin/platform/oauth` | configure customer OAuth providers; `settings.manage` | `REQUIRED` | `CAP-OAUTH`; route using settings/provider APIs | loading, empty, configured, incomplete, provider-error, secret-masked, test-result | `noindex`; none; none | Platform |
| `ADMIN-TURNSTILE` | `/admin/platform/turnstile` | configure bot-protection policy; `settings.manage` | `REQUIRED` | `CAP-TURNSTILE`; V10 route using `/api/admin/bot-protection` | loading, disabled, configured, incomplete, provider-error, secret-masked | `noindex`; none; none | Platform |
| `ADMIN-MAIL-TEMPLATES` | `/admin/platform/mail-templates[/{key}]` | edit localized mail templates; `mail.manage` | `REQUIRED` | `CAP-MAIL`; V10 route using `/api/admin/mail/templates*` | loading, empty, edit, preview, validation-error, test-result, conflict | `noindex`; none; none | Mail, Platform |
| `ADMIN-STORAGE` | `/admin/platform/storage` | inspect storage/quarantine health; `settings.manage` | `REQUIRED` | `CAP-FILES`; diagnostics plus native storage health contract | loading, healthy, quota-warning, unavailable, permission-error, stale | `noindex`; none; none | Platform, Files |

Front-end permission guards MUST NOT replace API permissions. Ticket handling, domain inventory, domain risk and domain entitlement are separate authorities; membership in one does not grant another.

---

## 11. Admin custom-domain entitlement and risk flows

### 11.1 Entitlement queue

Queue columns：Workspace、owner、current plan、account standing、requested `domain_limit`、linked ticket、abuse/security summary、requester、submitted age、assignee、state。Filters：state、plan、risk、assignee、age。

`manual_approval` detail 需要 Workspace summary、existing domains、current plan entitlement、request reason、linked ticket thread、payment/account standing、abuse history、prior decisions 和 audit。

### 11.2 Decision contract

| Action | Required fields | Effect |
|---|---|---|
| Approve | domain_limit, starts_at, expires_at, reason, ticket link；manual approval 的 `expires_at` 必填且晚于 `starts_at` | create active `manual_approval` |
| Deny | reason, user-visible category | request denied; no entitlement |
| Suspend | reason, effective time, scope | immediate block; no grace |
| Revoke | reason, confirmation, existing-link impact | entitlement revoked |
| Restore | reason, valid security/ownership evidence | active only if other axes pass |

提交前显示 destructive/impact confirmation；提交后显示 actor、timestamp、audit event。处理工单的权限不足以执行 decision。

REQUIRED API contract：

```text
GET  /api/admin/domain-entitlements
GET  /api/admin/domain-entitlements/{workspaceId}
POST /api/admin/domain-entitlements/{workspaceId}/decisions
GET  /api/admin/domain-risks
GET  /api/admin/domain-risks/{domainId}
POST /api/admin/domain-risks/{domainId}/revalidate
```

所有端点必须有 tenant/RBAC、idempotency where applicable、reason/audit、rate limit 和一致错误模型。

### 11.3 Domain risk page

展示 normalized hostname、registrable domain、workspace、ownership、ingress DNS、HTTPS、denylist/platform history、risk decision、last/next check、affected links 和 history。用户界面只显示安全需要的原因类别，不暴露内部 provider evidence 或可绕过规则。

Domain-risk actions require `domains.risk.manage`. Entitlement decisions require `domains.entitlements.manage`. Ticket actions require `tickets.manage`. Destination overrides require `security.manage`. No one permission implies another, and each action writes its own immutable audit event.

### 11.4 Hostname-independent destination-safety contract

Official and custom hostname links are records in the same `short_links` capability and MUST use the same target-risk contract:

1. Build the reachable target set from primary destination, every routing-rule destination and every A/B destination.
2. Normalize that set and calculate the target fingerprint.
3. Load the risk decision bound to that exact fingerprint.
4. Continue only for exact `allow`. Missing, malformed, unknown, stale, `pending`, `review` and `block` fail closed.
5. Only after allow may smart routing, A/B selection, UTM mutation, password, expiry, click-limit and one-time logic execute.

Editing any reachable target invalidates the previous fingerprint immediately. `APP-LINK-DETAIL` returns to risk-pending, QR generation/distribution is denied and redirect requests use the safety surface until a new exact fingerprint receives allow. Reordering an unchanged target set does not create a security bypass or false approval.

Custom-domain ownership verified, DNS valid, HTTPS active or domain-risk allow never substitutes for destination allow. Conversely, destination allow never substitutes for entitlement or domain trust. If any layer fails, the public response exposes neither the unsafe target nor a “continue anyway” link.

`PUB-LINK-UNAVAILABLE` bypasses smart routing, A/B and UTM, uses allowlisted reason copy, returns applicable `X-Robots-Tag: noindex, nofollow` and remains absent from every sitemap. Acceptance evidence MUST compare the same primary/routing/A-B target set on an official host and a custom host, including edit-driven fingerprint invalidation.

---

## 12. Other Admin page contracts

- Overview 只保留 compact metrics，主体为 service health、risk attention、failed jobs/payments、ticket SLA、recent audit；
- Users/Workspaces 使用 list → detail，敏感动作要求 reason；
- Files 不 iframe 未知文件，显示 MIME/hash/storage/scan/risk/audit；
- Destination Risk 显示 normalized target、score/category/source、decision/history/affected resources；override 与 rescan 分权并审计；
- Abuse detail 关联 target/user/workspace/risk/internal notes/actions；
- Tickets desktop 可 split，tablet/mobile 独立 detail；
- Services 明确列出 redirectengine、analyticsworker、analyticsreconciler、platformapi、mailworker、fileworker、operationsmonitor、logreceiver；restart 必须强权限和审计；
- Plans 是 Pricing 的同一数据源，并配置结构化 entitlements 而不是只编辑 feature 文案；
- Payments payload、Mail log、OAuth/Storage secret 必须 masked/redacted；
- Audit 记录 time/actor/action/resource/workspace/result/IP metadata/request ID/before-after/reason，禁止 secret。

---

## 13. Installer, error and safety surfaces

### 13.1 Installer `/install/*`

All rows use Installer surface, an installer session plus CSRF/access preflight, `noindex`, no canonical/alternate, no sitemap and `X-Robots-Tag: noindex, nofollow`.

| Route ID | Path | Purpose; access | Status | Capability / controller dependency | Applicable states | Index; canonical; alternate | Internal parent |
|---|---|---|---|---|---|---|---|
| `INSTALL-WELCOME` | `/install/` | verify package/version and start a scoped session; installer access policy | `REQUIRED` | `CAP-NATIVE-INSTALL`; required PHP entry | ready, package-incomplete, already-locked, CSRF-error | `noindex`; none; none | direct installer entry |
| `INSTALL-ENV` | `/install/environment` | test Nginx/PHP 8.3/systemd/filesystem/ClamAV; installer session | `REQUIRED` | `CAP-NATIVE-INSTALL`, `CAP-NATIVE-ONLY-RELEASE`, `CAP-CLAMAV-REQUIRED`; native preflight controller | checking, pass, wrong-PHP, systemd-missing, permission-error, ClamAV-unhealthy | `noindex`; none; none | Welcome |
| `INSTALL-DATA` | `/install/data` | test MySQL 8.x and Redis without persisting visible secrets; installer session | `REQUIRED` | `CAP-NATIVE-INSTALL`, `CAP-NATIVE-ONLY-RELEASE`; V10 database/cache preflight | input, checking, pass, version-error, auth-error, read-write-error | `noindex`; none; none | Environment |
| `INSTALL-SITE` | `/install/site` | validate public URL/Nginx/TLS/storage paths; installer session | `REQUIRED` | `CAP-NATIVE-INSTALL`, `CAP-NATIVE-ONLY-RELEASE`; V10 config renderer | input, validation-error, path-conflict, TLS-error, pass | `noindex`; none; none | Data |
| `INSTALL-ADMIN` | `/install/admin` | create the initial administrator input; installer session | `REQUIRED` | `CAP-NATIVE-INSTALL`, `CAP-NATIVE-ONLY-RELEASE`, `CAP-ADMIN-ACCESS`; V10 sealed install request | input, validation-error, duplicate-email, pass | `noindex`; none; none | Site |
| `INSTALL-SERVICES` | `/install/services` | render/enable/start all eight systemd units; system installer worker | `REQUIRED` | `CAP-NATIVE-INSTALL`, `CAP-NATIVE-ONLY-RELEASE` and eight service IDs; V10 root-owned worker | queued, running, partial-progress, unit-error, unhealthy, pass | `noindex`; none; none | Admin |
| `INSTALL-HEALTH` | `/install/health` | verify migration, storage, DB/Redis, ClamAV, Nginx and HTTP; installer session/system | `REQUIRED` | `CAP-NATIVE-INSTALL`, `CAP-NATIVE-ONLY-RELEASE`, `CAP-CLAMAV-REQUIRED`; native health controller | checking, partial-progress, hard-failure, retryable-failure, pass | `noindex`; none; none | Services |
| `INSTALL-COMPLETE` | `/install/complete` | confirm lock and expose Admin login link; installer session | `REQUIRED` | `CAP-NATIVE-INSTALL`, `CAP-NATIVE-ONLY-RELEASE`; V10 lock verification | locking, lock-failed, complete | `noindex`; none; none | Health |

ClamAV is never a warning: missing, unhealthy, stale beyond policy, timeout or indeterminate state blocks completion. The production installer exposes no Docker choice. After a verified lock, every Installer path returns 404 except a locally controlled recovery procedure defined outside the public web route set.

### 13.2 Error and safety registry

All rows use Error surface, system-selected access, `noindex`, no canonical/alternate and no sitemap. The internal parent is the request that produced the response; error pages MUST NOT be navigation destinations.

| Route ID | Path / HTTP | Purpose | Status | Capability / dependency | Applicable states | Index; canonical; alternate | Internal parent |
|---|---|---|---|---|---|---|---|
| `ERR-400` | any route / 400 | explain invalid request safely | `REQUIRED` | page contract; request validation | invalid-input, malformed-request | `noindex`; none; none | originating request |
| `ERR-401` | protected route / 401 | require authentication without leaking resource existence | `REQUIRED` | page contract; `CAP-AUTH` | signed-out, session-expired | `noindex`; none; none | protected route |
| `ERR-403` | protected route / 403 | explain forbidden action with safe recovery | `REQUIRED` | page contract; RBAC/tenant/entitlement | role-denied, tenant-denied, entitlement-required | `noindex`; none; none | protected route |
| `ERR-404` | any route / 404 | report unknown resource | `REQUIRED` | page contract; router/resource lookup | not-found | `noindex`; none; none | originating route family |
| `ERR-409` | write route / 409 | report conflict or stale write | `REQUIRED` | page contract; optimistic concurrency | conflict, stale-version | `noindex`; none; none | originating detail/form |
| `ERR-410` | removed public/resource route / 410 | report permanent removal | `REQUIRED` | page contract; lifecycle policy | expired, removed, consumed | `noindex`; none; none | originating public/resource route |
| `ERR-429` | any limited route / 429 | provide retry guidance without bypass | `REQUIRED` | page contract; rate limiter | rate-limited, Retry-After-present | `noindex`; none; none | originating request |
| `ERR-500` | any route / 500 | report server failure with safe correlation ID | `REQUIRED` | page contract; observability | server-error | `noindex`; none; none | originating request |
| `ERR-MAINTENANCE` | public/protected route / 503 | report maintenance/dependency outage | `REQUIRED` | page contract; operations | maintenance, dependency-unavailable, Retry-After-present | `noindex`; none; none | originating request |
| `ERR-LINK-REVIEW` | short route → `PUB-LINK-UNAVAILABLE` | block pending/review destinations | `REQUIRED` | `CAP-DESTINATION-RISK` | pending, review, missing, malformed, stale | `noindex`; none; none | official/custom short route |
| `ERR-LINK-BLOCKED` | short route → `PUB-LINK-UNAVAILABLE` | block denied destinations | `REQUIRED` | `CAP-DESTINATION-RISK` | block | `noindex`; none; none | official/custom short route |
| `ERR-LINK-UNAVAILABLE` | short route → `PUB-LINK-UNAVAILABLE` | block operational/domain-unavailable links | `REQUIRED` | required risk surface plus V10 domain-control reasons | domain-suspended, domain-revoked, domain-expired, service-unavailable | `noindex`; none; none | official/custom short route |
| `ERR-FILE-BLOCKED` | file route / 403 or 410 | deny unsafe/unavailable file content | `REQUIRED` | `CAP-FILES` | quarantined, scanning, blocked, scan_error, removed | `noindex`; none; none | public file route |

The rendered page never changes the real HTTP status; soft 404 is prohibited. Safety and error surfaces use `META-SYSTEM` and do not echo destination URLs, provider names, evidence, thresholds, secrets or bypass instructions.

---

## 14. SEO family policy matrix

This is the release projection of the Route Registry. Each family has exactly one policy value. Any generated route that cannot map to exactly one row is a G7 failure.

| Route family | Route IDs | Policy | Canonical / hreflang | Sitemap | HTTP/robots rule | Required parent logic |
|---|---|---|---|---|---|---|
| Website fixed pages | all `WEB-*` except `WEB-GUIDE` | `index` | `CAN-WEB / ALT-WEB` | Website child; Guides index in Guides child | canonical 200 only; maintenance 503 noindex | every row's registered crawlable parent |
| Website guide article | `WEB-GUIDE` | `conditional` | self canonical and reciprocal available translations only while published | Guides child only while canonical 200 | 200 published; 404 unknown; 410 removed | Guides plus related product/Docs |
| Docs locale homes | `DOCS-EN-HOME`, `DOCS-ZH-HOME` | `index` | `CAN-DOCS / ALT-DOCS` | locale child | canonical 200 only | Developers/Footer |
| Docs articles/API references | `DOCS-ARTICLE`, `DOCS-API` | `conditional` | self canonical and reciprocal published translations; API page additionally requires released API | locale child only while canonical 200 | 200 published/released; 404 unknown; 410 removed | sidebar + adjacent/related content |
| Docs search | `DOCS-SEARCH` | `noindex` | none | no | HTML/header noindex; malformed query 400 | search UI only |
| Auth | `AUTH-*` | `noindex` | none | no | no-store; HTML/header noindex | task-entry parent only |
| Workspace | `APP-*` | `noindex` | none | no | authenticated no-store; HTML/header noindex | Workspace navigation/task parent |
| Admin | `ADMIN-*` | `noindex` | none | no | admin no-store; HTML/header noindex | Admin navigation/task parent |
| Installer | `INSTALL-*` | `noindex` | none | no | `X-Robots-Tag: noindex, nofollow`; 404 after lock | previous step only |
| API/machine responses | `API-*` and all `/api/*` | `noindex` | none | no | `X-Robots-Tag: noindex, nofollow` on all statuses | owning page/task only |
| Official/custom short redirects | `PUB-SHORT-OFFICIAL`, `PUB-SHORT-CUSTOM` | `noindex` | none | no | applicable noindex header; no landing 200 | generated link only |
| Text UGC | `PUB-TEXT` | `noindex` | none | no | HTML/header noindex; accurate 401/403/404/410 | share action only |
| Bio UGC | `PUB-BIO` | `noindex` | none | no | HTML/header noindex; outbound UGC links nofollow | publish action only |
| File page/binary | `PUB-FILE-PAGE`, `PUB-FILE-BINARY` | `noindex` | none | no | HTML/header or `X-Robots-Tag` on every lifecycle response | share action/file page |
| Safety/unavailable | `PUB-LINK-UNAVAILABLE`, `ERR-LINK-*` | `noindex` | none | no | fail-closed source response plus safety-page noindex | system redirect only |
| Abuse report | `PUB-ABUSE-REPORT` | `noindex` | none | no | no-store; HTML/header noindex | Security/Legal/Safety |
| Operations pages | `PUB-STATUS`, `PUB-ANNOUNCEMENTS` | `noindex` | none | no | HTML/header noindex; 503 when unavailable | Footer/announcement/error |
| Signed preview | `PUB-PREVIEW` | `noindex` | none | no | no-store; 403/404/410 when token invalid | owner editor only |
| Error responses | `ERR-*` excluding duplicated safety family | `noindex` | none | no | preserve actual 4xx/5xx; no soft 404 | originating route |

Canonical, internal-link, Open Graph, hreflang and sitemap URLs MUST be byte-identical after normalization. Hreflang targets that redirect, fail, are noindex or are absent are prohibited. Public search/filter combinations remain noindex unless a separately approved static route is added to this registry.

---

## 15. Page state applicability matrix

不是所有页面都有同一状态列表。实现只覆盖下表标记的状态并提供原因：

| Page family | Loading | Empty | Partial | Read-only/Permission | Quota | Rate | Destructive | Offline/Stale |
|---|---|---|---|---|---|---|---|---|
| Static Website/Legal | no initial shell | n/a | pricing/API only | n/a | n/a | contact only | n/a | static offline possible |
| Auth | submitting only | n/a | provider list | auth-specific | n/a | yes | session revoke only | no |
| Docs | search/article nav | search empty | translation/search | n/a | n/a | n/a | n/a | static offline |
| Resource lists | yes | yes | risk/analytics | yes | create actions | API | bulk/delete | analytics stale |
| Resource detail | yes | deleted/410 | analytics/risk | yes | mutations | API | delete/restore | version conflict |
| Files | yes | yes | scan service | yes | upload | upload | delete/quarantine | scan stale/error |
| Domains | yes | no domains | axis degradation | entitlement/RBAC | domain_limit | verify | remove/revoke | DNS/TLS/risk stale |
| Billing | yes | no invoices/methods | provider | owner only | usage | payment | cancel | FX/payment stale |
| Admin queues | yes | yes | service degradation | permission | n/a | admin API | security actions | queue stale |
| Installer | step progress | n/a | no, required checks hard fail | filesystem/root privilege | disk | retry policy | reset install only | dependency stale |

Persistent problems（domain、billing、security、ticket）必须留在页面/Attention 区域；Toast 只用于短暂成功确认。

---

## 16. Responsive and evidence contract

所有精确 viewport、spacing、shell dimensions 和 density 引用 Design System §6 与 §14，不在此重复数值。

- Desktop：完整 sidebar/table/detail split；
- Tablet：sidebar/drawer，复杂 split 转 route detail；
- Mobile：单栏，核心字段，filter Sheet，主要动作可 sticky，禁止整页横向滚动；
- Website 内容重新排布，不是等比压缩；Hero 移除 parallax 并减少浮层；
- Docs left nav 变 Drawer，right ToC 折叠；
- Admin 高密表格在 mobile 转核心 list-row + detail route。

核心截图矩阵必须覆盖 Website template families、Auth flows、Docs home/article/API、Workspace shell、Links list/create/detail、Files five security states、Domains locked/requested/active/verification/grace/suspended/revoked、Billing payment states、Admin entitlement decision、Destination/Domain risk、Installer hard failure 和 error/safety pages。

证据文件名严格使用 Design System §14.2。每张截图关联 route ID、state、theme、locale、viewport、当前仓库 `implementation_commit` 和 test run ID；不记录任何旧仓库提交作为完成依据。

---

## 17. Route and SEO completion criteria

Page-Level IA 只有在以下全部成立时完成：

- 每条页面 route 有唯一 Route ID、access、status、capability 和 states；
- 每条 public route 有 search intent、index policy、canonical source、locale alternate、metadata source、structured-data eligibility、internal parents 和 sitemap decision；
- Auth、Workspace、Admin、Installer、API、search、preview、UGC、redirect 和 safety surfaces 均 noindex 且不进 sitemap；
- 每个 index route 在 raw HTML 中有最终 title/description/canonical/robots/lang/H1/content/links/JSON-LD；
- 404/410/redirect/canonical/hreflang 行为无 soft 404、chain、loop 或 redirect target；
- API Keys、generic Webhooks、custom-domain entitlement 和 domain risk 正确标注 `REQUIRED`；
- Request access 只创建申请/linked ticket，只有 `manual_approval` 或 active Business entitlement 能获得资格；
- 自定义域名所有短链与官方域名使用相同 destination risk，目标变化触发 fingerprint invalidation；
- 视觉值全部引用 Design System，未在页面合同中复制第二套精确值；
- 三类响应式和要求的安全/错误/空/部分/权限状态均有证据。
