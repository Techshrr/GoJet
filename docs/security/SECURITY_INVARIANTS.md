# GoJet V10 Security Invariants

Status: P00 baseline  
Authority: `GJ-V10-MP-GREENFIELD-2026-08-20`

These are implementation invariants, not optional hardening suggestions.

## Authority and tenancy

1. Authentication, authorization, tenant isolation, RBAC, quota, payment state, destination risk, file security, custom-domain entitlement, rate limiting and audit are server-authoritative.
2. Every Workspace-owned resource access enforces the Workspace boundary on the server. Resource IDs never replace an ownership check.
3. Workspace role UI is not authorization. Admin governance permissions are separate from Workspace roles.
4. The last Workspace owner is protected from removal/downgrade sequences that would leave the Workspace ownerless.

## Secrets and evidence

5. Production secrets, API keys, OAuth secrets, payment credentials, raw reset/verification tokens, private keys and database passwords must never be committed.
6. Logs, diagnostics, audit, CI output and Gate evidence must redact sensitive fields while preserving correlation and attribution.
7. Frontend bundles and public/runtime settings must never contain privileged secrets.

## File safety

8. Uploaded files remain outside the web root and move through upload/quarantine/scan/published states.
9. ClamAV missing, unhealthy, timed out, stale or indeterminate is fail-closed. Such a state never permits publication; where the installer requires ClamAV it must not complete installation.
10. Public downloads are served through a Go authorization path and cannot directly expose the local published directory.

## Destination and domain safety

11. Destination risk is enforced consistently for official and custom hosts and for primary, routing and A/B destinations.
12. Missing, stale, malformed, review or blocked risk state cannot reach a customer target.
13. Custom-domain entitlement, ownership, DNS ingress, HTTPS readiness and domain risk are independent states and must all be valid where required.
14. A support ticket is a request/evidence channel only and cannot itself grant custom-domain entitlement.
15. Cross-Workspace hostname ownership conflicts fail closed.

## Web and request security

16. Sessions, CSRF/Origin policy, OAuth state/PKCE where applicable, Turnstile verification and rate limits are verified server-side.
17. User-provided outbound URLs and webhook endpoints must be protected against SSRF and unsafe scheme/host resolution.
18. Security-sensitive state changes require auditable actor/reason/correlation information.
19. Authenticated or sensitive HTML/API responses use no-store/private no-store where required by the Master Plan; unsafe/UGC responses must not be incorrectly shared from cache.

## Production boundary

20. Production Docker/Compose paths are prohibited.
21. Node is a development/build/test/package tool only; no production Node HTTP/SSR/PM2/dev-server runtime is permitted.
22. The production target is native Nginx + eight independent Go binaries + PHP 8.3 FPM installer + MySQL 8.x + Redis + ClamAV + local filesystem + systemd.
23. Each Go binary is managed by its own non-root systemd unit; a single supervisor script cannot substitute for eight services.

A change that weakens any applicable invariant requires approved specification change control; it cannot be justified solely to make CI or a Gate pass.
