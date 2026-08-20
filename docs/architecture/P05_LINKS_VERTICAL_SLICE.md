# P05 Links Vertical Slice — Implementation Contract

Status: **FROZEN FOR P05 IMPLEMENTATION**  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`

## 1. Purpose

P05 establishes the first complete current-repository business slice. The slice must be usable through real HTTP APIs, persisted in MySQL, use Redis for redirect/risk runtime state, resolve through `redirectengine`, render through the Workspace UI, and leave correlated audit/evidence records.

No prior GoJet implementation is a source of behavior or code.

## 2. Browser routes

Only the approved IA routes are implemented by P05:

- `APP-LINKS` — `/app/links`
- `APP-LINK-NEW` — `/app/links/new`
- `APP-LINK-DETAIL` — `/app/links/{linkId}`

P05 must not invent compatibility aliases or additional canonical browser paths.

## 3. API families

P05 owns the following current-repository route families. Exact handler implementation may be split internally, but external semantics remain stable:

```text
GET    /api/workspaces/{workspaceId}/links
POST   /api/workspaces/{workspaceId}/links
GET    /api/workspaces/{workspaceId}/links/{linkId}
PATCH  /api/workspaces/{workspaceId}/links/{linkId}
DELETE /api/workspaces/{workspaceId}/links/{linkId}
POST   /api/workspaces/{workspaceId}/links/{linkId}/restore
GET    /api/workspaces/{workspaceId}/links/{linkId}/history
POST   /api/workspaces/{workspaceId}/links/bulk
GET    /api/workspaces/{workspaceId}/links/export
```

Routing, A/B, UTM, access and settings changes are represented by the detail update contract; the service may internally expose narrower commands later only through approved change control.

## 4. Server authority

Every request carries a server-resolved actor and Workspace scope. For the P05 isolated test harness, authenticated identity may be injected only through a test-only adapter enabled by an explicit non-production environment flag. Production handlers must consume the common auth/RBAC boundary when P15/P12 provide it; they must not trust arbitrary client role claims.

Mutation authority is checked server-side. Cross-Workspace link IDs do not reveal whether the resource exists.

## 5. Data model

The initial P05 migration uses repository-global numbering and introduces the minimum authoritative records needed by the slice.

### `links`

Authoritative current state:

- numeric immutable ID
- Workspace ID
- hostname/domain binding identifier or approved official hostname
- normalized code
- title
- primary destination
- redirect status
- lifecycle status (`active`, `paused`, `deleted`)
- version integer used for optimistic concurrency
- current destination-risk fingerprint
- expiration / click limit / one-time fields where configured
- structured routing / A-B / UTM / access JSON
- created/updated timestamps

Uniqueness is enforced by the authoritative host + normalized code key, not by UI checks.

### `link_versions`

Append-only version snapshots containing:

- link ID
- version
- actor ID
- change reason
- complete behavior-relevant snapshot
- destination-risk fingerprint associated with that snapshot
- creation timestamp

Restore creates a new current version. History rows are never rewritten to pretend a restore was the original state.

### `link_audit_events`

Append-only P05 action records containing actor, Workspace, action, link ID, request correlation ID, reason where required, result and timestamp. Secrets/password values and unsafe destination-provider evidence must not be logged.

## 6. Destination target set and fingerprint

The fingerprint is computed from the complete reachable target set **before** routing/A-B/UTM/access execution.

Target set:

1. primary destination;
2. every enabled routing destination;
3. every enabled A/B destination.

Normalization produces deterministic target records. The final set is sorted deterministically and hashed using SHA-256 over a versioned canonical serialization.

The fingerprint must change when any reachable destination is added, removed or changed. Changes to title or unrelated presentation fields do not change it.

## 7. Risk state in Redis

The runtime decision key is bound to both Link identity and exact current fingerprint. A decision for a previous fingerprint is never reusable.

Conceptual key:

```text
risk:link:{linkId}:{fingerprint}
```

Stored decision schema is versioned and includes at least:

- decision: `allow | review | block`
- fingerprint
- checked timestamp
- expiry/valid-until timestamp or TTL semantics
- policy version

Runtime interpretation:

| State | Redirect behavior |
|---|---|
| exact-current `allow` | continue to target resolution |
| `review` | GoJet safety surface; destination not exposed |
| `block` | GoJet blocked safety surface; destination not exposed |
| missing | fail closed |
| malformed | fail closed |
| stale/expired | fail closed |
| fingerprint mismatch | treated as missing/stale; fail closed |

## 8. Redirect ordering invariant

`redirectengine` executes in this order:

```text
hostname/code lookup
→ lifecycle availability check
→ load exact current fingerprint
→ destination-risk decision
→ only when exact-current allow:
     routing selection
     → A/B selection
     → UTM mutation
     → password / expiry / click-limit / one-time controls as applicable
     → click/audit event boundary
     → HTTP redirect
```

No routing, A/B, UTM or access logic may create a response containing a user destination before destination-risk authorizes the exact current target set.

## 9. Routing and A/B

Routing rules are validated as structured input. Rules that cannot be normalized are rejected rather than silently ignored.

A/B weights must be positive integers and the enabled variant set must satisfy the P05 weight contract. Runtime selection is deterministic for an explicit test seed and suitable request-derived entropy in normal operation. Every enabled A/B destination is part of the fingerprint regardless of which variant is selected for one request.

## 10. UTM

UTM mutation happens after a safe destination has been selected. Existing query parameters are preserved unless the explicit UTM key is intentionally replaced by the configured Link UTM contract. UTM configuration itself cannot be used to escape the fingerprinted destination origin/path semantics.

## 11. Optimistic concurrency

Every mutable Link has a monotonically increasing `version`. PATCH/restore/bulk mutation carries the expected current version where a single-link write is involved. A stale writer receives deterministic conflict semantics and must reload; last-write-wins overwrite is prohibited.

## 12. List and bulk behavior

`APP-LINKS` supports server-authoritative count/search/filter over the Workspace scope. P05 filters include domain/status and the IA-declared campaign/tag/date inputs where data is present; unsupported future-owner data must be represented explicitly rather than silently faked.

Bulk commands are atomic per target Link and return a result per requested ID. No bulk command may cross Workspace boundaries.

## 13. UI states

### APP-LINKS

`loading`, `empty`, `filtered-empty`, `partial-risk`, `read-only`, `quota-reached`, `rate-limited`, `error`.

### APP-LINK-NEW

`input`, `submitting`, `quota-reached`, `domain-unavailable`, `risk-pending`, `conflict`, `error`.

### APP-LINK-DETAIL

`loading`, `success`, `read-only`, `risk-pending`, `risk-review`, `risk-block`, `conflict`, `deleted`, `error`.

Detail tabs are exactly: Overview, Analytics, Routing, A/B Test, UTM, Access, QR, Settings, History. P07/P08-owned deep functionality may show a truthful not-yet-owned state where P05 only provides the Links integration boundary; P05 must not fake analytics or standalone QR completion.

## 14. Safety surfaces

Risk `review`, `block`, missing, malformed and stale states return GoJet-controlled safety/unavailable responses. Safety HTML must not contain a clickable bypass to the destination. Evidence records may use redacted/hashed target identifiers; provider secrets/evidence are never rendered.

## 15. Evidence and Gate scope

P05 produces current-repository evidence for:

- G3 functional/API first vertical slice;
- G4 Links browser/responsive slice;
- G5 Links-applicable accessibility slice;
- G6 destination-risk/RBAC/concurrency slice.

Passing these P05 subsets does not close release-wide G3/G4/G5/G6 obligations owned by later nodes and P20/P22.
