# P09 Files and Mandatory ClamAV — Accountable Technical Review

Node: `P09`  
Issue: #25  
Base integration commit: `418277613cf4336273b19f5d0da8a47bc1d403d6`  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`, `GJ-V10-IA-GREENFIELD-2026-08-20`, `GJ-V10-DS-GREENFIELD-2026-08-20`  
Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**

## 1. Review boundary

This document now records the accountable P09 technical review after the frozen contract and pre-sign evidence completed successfully. The signature approves the P09-owned implementation and evidence identified below; it does not by itself make the signed revision merge-authoritative. The signed revision must still rerun the complete affected exact-head matrix and P09-T001..P09-T027 before P09 may be marked complete or merged.

This review does not complete the later P17/P22 ownership portions of `CAP-FILES` / `CAP-CLAMAV-REQUIRED`, and does not close release-wide G10/G12/G13.

Legacy file code, extension-only validation, client-side scan claims, screenshot-only evidence, a fake scanner, a manually edited state row or direct filesystem assumptions cannot substitute for current-repository P09 evidence.

## 2. Frozen capability and ownership boundary

- `CAP-FILES` — REQUIRED — owners P09/P17 — Gates G3/G6/G10.
- `CAP-CLAMAV-REQUIRED` — REQUIRED — owners P09/P22 — Gates G6/G12/G13.
- P09 predecessors: P01 and P04.
- P09 provides reusable authoritative file/scan/storage/health/preflight state for later P17/P22; it must not claim those nodes complete.

## 3. Frozen security invariants

1. Pipeline: `upload → allowlist/MIME/magic/quota → randomized private name → quarantine → ClamAV → policy → publish`.
2. Clean/EICAR Exit evidence must exercise real ClamAV through the production scan client/path; client scanning or extension trust cannot establish clean authority.
3. Daemon/socket unavailable, timeout, stale signatures, malformed/indeterminate response and uncertain restart state fail closed and remain private.
4. `quarantined`, `scanning`, `safe`, `blocked`, `scan_error` are server-authoritative; frontend derivation/override is prohibited.
5. `safe` is scan-safe, not automatically published or authorized. Publish/download remain separately permission/policy gated.
6. Rescan immediately invalidates prior scan authority for new distribution until a new current clean verdict exists.
7. P09 release storage is native local filesystem. `CAP-S3-STORAGE` is DEFERRED and is not a P09 release dependency.
8. Original filename is metadata, not path authority. Storage identity is server-randomized; private/quarantine/published locations are outside direct public access.
9. File bytes are served only through authorized owner/public handlers or internal server handoff; traversal/path disclosure/direct storage access is prohibited.
10. List/detail/update/delete/rescan/publish/download repeat server-side tenant/RBAC checks; quota/download limits must be concurrency-safe.
11. Duplicate claims/retries/restarts cannot double-process/publish/count, silently lose work or convert uncertainty to safe.
12. Public password/expiry/download-limit/removal/scan/publication policy is server-enforced; passwords never enter URLs/logs/plaintext storage.
13. ClamAV is mandatory for installer/upgrade readiness; unavailable/timeout/stale/indeterminate states hard-fail.
14. Health/status output may expose actionable state/freshness/correlation but not private paths, socket secrets or bypass detail.
15. Production Docker/Compose and production Node HTTP runtime remain prohibited.

## 4. Route and API authority boundary

Approved IA routes:

```text
APP-FILES        /app/files
APP-FILE-DETAIL  /app/files/{fileId}
PUB-FILE-PAGE    /f/{slug}
PUB-FILE-BINARY  GET /api/public/files/{slug}
ADMIN-FILES      /admin/files[/{fileId}]
ADMIN-STORAGE    /admin/platform/storage
INSTALL-ENV      /install/environment
INSTALL-SERVICES /install/services
INSTALL-HEALTH   /install/health
```

The approved IA names Workspace dependencies as `fileshare APIs` and `file/delete APIs`; it does **not** freeze exact Workspace HTTP methods/paths. P09 therefore freezes this current-repository **implementation API contract**, which must never be described as an IA-exact family:

```text
GET    /api/workspaces/{id}/files
POST   /api/workspaces/{id}/files
GET    /api/workspaces/{id}/files/{fileId}
PATCH  /api/workspaces/{id}/files/{fileId}
DELETE /api/workspaces/{id}/files/{fileId}
POST   /api/workspaces/{id}/files/{fileId}/publish
POST   /api/workspaces/{id}/files/{fileId}/rescan
GET    /api/workspaces/{id}/files/{fileId}/download
```

Public password verification is a same-route P09 page action on `POST /f/{slug}` and may establish an opaque HttpOnly authorization cookie. The IA-registered public binary remains `GET /api/public/files/{slug}`. No legacy/compatibility alias is approved.

P09 public lifecycle evidence freezes: unknown=404; non-safe/non-published/blocked/scan-error/password-denied binary=403 with zero bytes; expired/removed=410.

## 5. File safety UI contract

| State | Alias | Icon | Required headline |
|---|---|---|---|
| quarantined | `file-quarantined` | `PackageLock` | `File quarantined` |
| scanning | `file-scanning` | `LoaderCircle` | `Security scan in progress` |
| safe | `file-safe` | `ShieldCheck` | `File is safe to publish` |
| blocked | `file-blocked` | `ShieldX` | `File blocked` |
| scan_error | `file-scan-error` | `TriangleAlert` | `Scan unavailable; file remains private` |

Every safety surface requires fixed icon + visible state name + specific reason/next step + structural status region. Color-only safety meaning is a hard failure; reduced-motion must preserve first-frame semantics.

## 6. Evidence and case contract

Required P09 case range: **P09-T001..P09-T027**.

The `P09-Txxx` IDs are frozen by this P09 contract revision. The Master Plan supplies the requirements—not these IDs—including EICAR, clean, timeout, daemon down, signature stale, indeterminate response, rescan, duplicate claim, service restart, direct quarantine/public path access and Installer/upgrade hard fail.

Evidence root: `artifacts/v10/P09/`. Required evidence includes real MySQL state, redacted native local filesystem records, real ClamAV clean/EICAR evidence under `artifacts/v10/P09/clamav/`, controlled fail-closed scan faults, native fileworker claim/restart/rescan records, HTTP/file-permission records, Workspace/Public/Installer/Admin-status browser evidence, canonical 1440×900 / 1024×768 / 390×844 captures, keyboard/non-color/reduced-motion/320 CSS px reflow evidence, exact-head coherence and affected regression closure.

## 7. Gate scope

P09 may contribute only its owned subsets: G3 `CAP-FILES` functional/API; G6 file-security/mandatory-ClamAV; G10 file full-stack; and only the P09 ClamAV preflight/runtime contribution to later G12/G13 verification. P09 does not complete P17 Admin ownership, P22 fresh-install/production-candidate ownership, or release-wide G10/G12/G13.

## 8. Accountable implementation review

Accountable reviewer identity: **GPT-5.6 Sol — CAP-FILES/CAP-CLAMAV Technical Review**  
Review date: **2026-08-23**  
Pre-sign exact implementation SHA: `0f961b4f8c8dd320fce7ecd1e65125289d8fbb68`

### Pre-sign evidence disposition

- P09-T001..P09-T025: PASS on the pre-sign exact implementation SHA.
- P09-T026: PASS — exact-head evidence coherence; 25 producer case records, 9 canonical captures and producer run bindings were validated on the same SHA.
- P09-T027: PASS — pre-sign closure / merge-authoritative=false
- P09 evidence-coherence run: `32618410618`; artifact: `9487675914`; digest: `sha256:4e6c0969733b8878810e2b0005c1a972d4a3909ded251e7e7f42b67c4aa15ed2`.
- P09 pre-sign closure run: `32618410607`; artifact: `9487678669`; digest: `sha256:ead0ce0381c406a551174836816f2416407d9c434bfd8775b7599694693d1daf`.
- The pre-sign closure records 26/26 P09 evidence inputs and 26/26 required affected workflows green on `0f961b4f8c8dd320fce7ecd1e65125289d8fbb68`.
- Inherited P08 authority remains the already signed closure from source `dc055fbd52cde5d0aada0b912373cc182660f105`, run `32579404081`, artifact `9477523498`, digest `sha256:aede44ebad79a78adc872f33a874f8aea1ae24bb430b0e06eaa2da32739c0a2d`, with `phase=signed`, `merge_authoritative=true`, and P0/P1/Decision Required all zero. It is inherited authority, not a requirement to reinterpret P08's pre-sign SHA against the P09 HEAD.

### Accountable approvals

- Backend Lead: APPROVED
- Frontend Lead: APPROVED
- QA Lead: APPROVED
- Accessibility Reviewer: APPROVED
- Security Reviewer: APPROVED
- Product/API Reviewer: APPROVED

The review approves the current P09 implementation contract and evidence for the P09-owned portions only. It does not waive the signed-revision rerun below.

### Defect and decision ledger

- P0 defects: 0
- P1 defects: 0
- `DECISION REQUIRED`: 0

### Gate-subset disposition

- G3 P09: PASS — `CAP-FILES` functional/API subset only.
- G6 P09: PASS — file security and mandatory-ClamAV subset only.
- G10 P09: PASS — file full-stack subset only; release-wide G10 remains later-owned.
- G12/G13 P09: PASS — ClamAV installer/preflight/runtime contribution only; release-wide G12/G13 remain later-owned.
- P17/P22 and all release-wide closure authority remain OPEN/later-owned. P09 does not claim those nodes complete.

## 9. Signed-revision rule

This review signature changes `review.md` and therefore changes HEAD. The signed revision itself must rerun and pass P09-T001..P09-T027 and the complete affected exact-head matrix before this review becomes merge-authoritative.

The signed revision must produce `phase=signed / merge_authoritative=true` with P0=0, P1=0 and unresolved `DECISION REQUIRED`=0. Until that exact signed-revision closure artifact exists, P09 must not be marked complete or merged.
