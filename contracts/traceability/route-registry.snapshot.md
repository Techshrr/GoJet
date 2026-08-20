# GoJet V10 Route Registry Snapshot

Status: `P00 frozen traceability input`  
Canonical authority: `specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md`  
Specification ID: `GJ-V10-IA-GREENFIELD-2026-08-20`  
Source blob SHA at freeze: `20609139a0265d3f3a40a1c7c07894dc69220290`  
Freeze base commit: `3465148cb77e920141bbd43651ba912832dc2dd4`

This is a G0 traceability snapshot, not a second route authority. Exact purpose, access, capability/API dependencies, applicable states, index policy, canonical/alternate rules, internal-link parents, public SEO metadata and HTTP behavior remain normative only in the Page-Level IA. Any difference is a G0 failure requiring reconciliation against the IA.

Every row below is `REQUIRED` in the frozen V10 IA.

## Website

| Route ID | Path |
|---|---|
| `WEB-HOME` | `/` and `/zh-CN/` |
| `WEB-PRODUCTS` | `/products`, localized |
| `WEB-LINKS` | `/products/links`, localized |
| `WEB-QR` | `/products/qr-codes`, localized |
| `WEB-FILES` | `/products/files`, localized |
| `WEB-TEXT` | `/products/text-sharing`, localized |
| `WEB-BIO` | `/products/link-in-bio`, localized |
| `WEB-ANALYTICS` | `/products/analytics`, localized |
| `WEB-ROUTING` | `/products/smart-routing`, localized |
| `WEB-DOMAINS` | `/products/custom-domains`, localized |
| `WEB-SOLUTIONS` | `/solutions`, localized |
| `WEB-SOL-MARKETING` | `/solutions/marketing`, localized |
| `WEB-SOL-CREATORS` | `/solutions/creators`, localized |
| `WEB-SOL-TEAMS` | `/solutions/teams`, localized |
| `WEB-SOL-DEVELOPERS` | `/solutions/developers`, localized |
| `WEB-DEVELOPERS` | `/developers`, localized |
| `WEB-PRICING` | `/pricing`, localized |
| `WEB-SECURITY` | `/security`, localized |
| `WEB-GUIDES` | `/guides`, localized |
| `WEB-GUIDE` | `/guides/{slug}`, localized |
| `WEB-ABOUT` | `/about`, localized |
| `WEB-CONTACT` | `/contact`, localized |
| `WEB-LEGAL-TERMS` | `/legal/terms`, localized |
| `WEB-LEGAL-PRIVACY` | `/legal/privacy`, localized |
| `WEB-LEGAL-AUP` | `/legal/acceptable-use`, localized |
| `WEB-LEGAL-ABUSE` | `/legal/abuse`, localized |

## Docs

| Route ID | Path |
|---|---|
| `DOCS-EN-HOME` | `/docs/en/` |
| `DOCS-ZH-HOME` | `/docs/zh-CN/` |
| `DOCS-ARTICLE` | `/docs/en/{slug...}` and `/docs/zh-CN/{slug...}` |
| `DOCS-API` | `/docs/en/api/{resource}` and `/docs/zh-CN/api/{resource}` |
| `DOCS-SEARCH` | `/docs/en/search?q={query}` and `/docs/zh-CN/search?q={query}` |

## Public resources and public machine interfaces

| Route ID | Path |
|---|---|
| `PUB-SHORT-OFFICIAL` | `https://{official-short-host}/{code}` |
| `PUB-SHORT-CUSTOM` | `https://{custom-host}/{code}` |
| `PUB-TEXT` | `/t/{slug}` |
| `PUB-BIO` | `/p/{slug}` |
| `PUB-FILE-PAGE` | `/f/{slug}` |
| `PUB-FILE-BINARY` | `/api/public/files/{slug}` |
| `PUB-LINK-UNAVAILABLE` | `/linkunavailable?reason={allowlisted}&code={safe-code}` |
| `PUB-ABUSE-REPORT` | `/abuse/report` |
| `PUB-STATUS` | `/status` |
| `PUB-ANNOUNCEMENTS` | `/announcements` |
| `PUB-PREVIEW` | `/preview/{resourceType}/{token}` |
| `API-PUBLIC-REQUIRED` | registered `/api/public/*` family enumerated by IA §3.3 |
| `API-PUBLIC-V10` | `/api/public/contact` and `/api/public/previews/{token}` |

## Auth

| Route ID | Path |
|---|---|
| `AUTH-LOGIN` | `/login` |
| `AUTH-REGISTER` | `/register` |
| `AUTH-VERIFY` | `/verify-email` |
| `AUTH-FORGOT` | `/forgot-password` |
| `AUTH-RESET` | `/reset-password?token={opaque}` |
| `AUTH-INVITE` | `/invite/{token}` |
| `AUTH-OAUTH-CALLBACK` | `/oauth/{provider}/callback` |
| `AUTH-SOCIAL-REG` | `/social-registration?code={opaque}` |

## Workspace

| Route ID | Path |
|---|---|
| `APP-OVERVIEW` | `/app` |
| `APP-NOTIFICATIONS` | `/app/notifications` |
| `APP-LINKS` | `/app/links` |
| `APP-LINK-NEW` | `/app/links/new` |
| `APP-LINK-DETAIL` | `/app/links/{linkId}` |
| `APP-QR` | `/app/qr` |
| `APP-QR-DETAIL` | `/app/qr/{qrId}` |
| `APP-FILES` | `/app/files` |
| `APP-FILE-DETAIL` | `/app/files/{fileId}` |
| `APP-TEXT` | `/app/text` |
| `APP-TEXT-DETAIL` | `/app/text/{shareId}` |
| `APP-BIO` | `/app/bio` |
| `APP-BIO-DETAIL` | `/app/bio/{pageId}` |
| `APP-ANALYTICS` | `/app/analytics` |
| `APP-DOMAINS` | `/app/domains` |
| `APP-DOMAIN-NEW` | `/app/domains/new` |
| `APP-DOMAIN-DETAIL` | `/app/domains/{domainId}` |
| `APP-ORGANIZATION` | `/app/organization` |
| `APP-CAMPAIGNS` | `/app/campaigns` |
| `APP-TAGS` | `/app/tags` |
| `APP-API-KEYS` | `/app/api-keys` |
| `APP-WEBHOOKS` | `/app/webhooks` |
| `APP-MEMBERS` | `/app/members` |
| `APP-BILLING` | `/app/billing` |
| `APP-SETTINGS` | `/app/settings/profile`, `/security`, `/sessions`, `/connected-accounts`, `/workspace`, `/danger` under `/app/settings` |
| `APP-SUPPORT` | `/app/support` |
| `APP-SUPPORT-NEW` | `/app/support/new` |
| `APP-SUPPORT-THREAD` | `/app/support/{ticketId}` |

## Admin

| Route ID | Path |
|---|---|
| `ADMIN-LOGIN` | `/admin/login` |
| `ADMIN-OVERVIEW` | `/admin` |
| `ADMIN-USERS` | `/admin/users[/{userId}]` |
| `ADMIN-WORKSPACES` | `/admin/workspaces[/{workspaceId}]` |
| `ADMIN-LINKS` | `/admin/resources/links[/{linkId}]` |
| `ADMIN-DOMAINS` | `/admin/resources/domains[/{domainId}]` |
| `ADMIN-QR-TEXT-BIO` | `/admin/resources/qr[/{resourceId}]`, `/text[...]`, `/bio[...]` |
| `ADMIN-FILES` | `/admin/files[/{fileId}]` |
| `ADMIN-DEST-RISK` | `/admin/trust/destination-risk[/{riskId}]` |
| `ADMIN-DOMAIN-RISK` | `/admin/trust/domain-risk[/{domainId}]` |
| `ADMIN-ABUSE` | `/admin/trust/abuse[/{reportId}]` |
| `ADMIN-DOMAIN-ENTITLEMENTS` | `/admin/domain-entitlements` |
| `ADMIN-DOMAIN-ENTITLEMENT` | `/admin/domain-entitlements/{workspaceId}` |
| `ADMIN-TICKETS` | `/admin/tickets[/{ticketId}]` |
| `ADMIN-ANNOUNCEMENTS` | `/admin/announcements` |
| `ADMIN-MAIL` | `/admin/mail` |
| `ADMIN-JOBS` | `/admin/operations/jobs` |
| `ADMIN-SERVICES` | `/admin/operations/services` |
| `ADMIN-PLANS` | `/admin/commerce/plans` |
| `ADMIN-PAYMENTS` | `/admin/commerce/payments[/{paymentId}]` |
| `ADMIN-FX` | `/admin/commerce/fx` |
| `ADMIN-ADMINS` | `/admin/access/administrators[/{adminId}]` |
| `ADMIN-ROLES` | `/admin/access/roles` |
| `ADMIN-AUDIT` | `/admin/audit` |
| `ADMIN-GENERAL` | `/admin/platform/general` |
| `ADMIN-OFFICIAL-DOMAINS` | `/admin/platform/official-domains` |
| `ADMIN-OAUTH` | `/admin/platform/oauth` |
| `ADMIN-TURNSTILE` | `/admin/platform/turnstile` |
| `ADMIN-MAIL-TEMPLATES` | `/admin/platform/mail-templates[/{key}]` |
| `ADMIN-STORAGE` | `/admin/platform/storage` |

## Installer

| Route ID | Path |
|---|---|
| `INSTALL-WELCOME` | `/install/` |
| `INSTALL-ENV` | `/install/environment` |
| `INSTALL-DATA` | `/install/data` |
| `INSTALL-SITE` | `/install/site` |
| `INSTALL-ADMIN` | `/install/admin` |
| `INSTALL-SERVICES` | `/install/services` |
| `INSTALL-HEALTH` | `/install/health` |
| `INSTALL-COMPLETE` | `/install/complete` |

## Error and safety responses

| Route ID | Path / HTTP |
|---|---|
| `ERR-400` | any route / 400 |
| `ERR-401` | protected route / 401 |
| `ERR-403` | protected route / 403 |
| `ERR-404` | any route / 404 |
| `ERR-409` | write route / 409 |
| `ERR-410` | removed public/resource route / 410 |
| `ERR-429` | any limited route / 429 |
| `ERR-500` | any route / 500 |
| `ERR-MAINTENANCE` | public/protected route / 503 |
| `ERR-LINK-REVIEW` | short route → `PUB-LINK-UNAVAILABLE` |
| `ERR-LINK-BLOCKED` | short route → `PUB-LINK-UNAVAILABLE` |
| `ERR-LINK-UNAVAILABLE` | short route → `PUB-LINK-UNAVAILABLE` |
| `ERR-FILE-BLOCKED` | file route / 403 or 410 |

## Frozen registry summary

| Surface | Registry rows |
|---|---:|
| Website | 26 |
| Docs | 5 |
| Public/API | 13 |
| Auth | 8 |
| Workspace | 28 |
| Admin | 30 |
| Installer | 8 |
| Error/Safety | 13 |
| **Total registered rows** | **131** |

Some registry rows intentionally represent multiple concrete paths (for example localized Website peers, `APP-SETTINGS`, `ADMIN-QR-TEXT-BIO`, and bracketed Admin detail forms). Therefore `131` is the number of normative Route Registry rows, not the number of concrete URL strings.

## G0 invariants

- Every implementation browser route must resolve to a registered Route ID; no legacy alias is implicitly allowed.
- `REQUIRED`, `DEFERRED`, and `REMOVED` classifications must remain traceable to the IA/Master Plan.
- Auth/Workspace/Admin/Installer/API/Public UGC/Error families remain noindex as defined by IA; Website/Docs indexability remains row-specific.
- A route file, page mock or navigation item does not prove capability/API/RBAC/state/browser completion.
- Any change in Route ID/path/status requires specification change control first, then regeneration/review of this snapshot.
