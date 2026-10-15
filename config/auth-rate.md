# Authentication Rate-Protection Runtime Contract

When `GOJET_AUTH_ENABLED=1`, native `platformapi` requires an explicit server-side Redis rate policy for the P15-owned authentication abuse surfaces.

Required non-secret runtime settings:

- `GOJET_AUTH_RATE_LIMIT` — positive integer request limit for the shared identity/IP bucket policy.
- `GOJET_AUTH_RATE_WINDOW_SECONDS` — rate window in whole seconds, from `1` through `86400` inclusive.

There is intentionally **no production rate-policy default** in source control. Deployment/release authority must select reviewed values for the production environment rather than inheriting a CI fixture or an implicit application default.

The single policy is enforced server-side for the frozen P15 surfaces:

- registration
- password login
- login email-code request/consume
- password-recovery request

## Trusted client-address boundary

The IP bucket must represent the originating client, not an intermediate reverse proxy.

`platformapi` trusts `X-Forwarded-For` only when the immediate TCP peer is a trusted proxy. Loopback peers (`127.0.0.0/8` and `::1/128`) are the built-in local reverse-proxy boundary. Additional trusted proxy networks may be supplied through the optional comma-separated `GOJET_AUTH_TRUSTED_PROXY_CIDRS` setting. Every configured value must be an explicit CIDR prefix; malformed configuration prevents Auth rate middleware from starting.

When the immediate peer is not trusted, all forwarding headers are ignored and the socket peer address remains authoritative. When a trusted proxy provides `X-Forwarded-For`, `platformapi` walks the chain from right to left, skips only known trusted proxies, and uses the first untrusted address as the client IP. Malformed chains fail safely to the immediate peer instead of accepting spoofable input.

A reverse proxy must append or replace `X-Forwarded-For` from its observed downstream socket address; it must never pass a client-controlled forwarding value as authoritative without adding the observed peer. Repository Vite development/preview proxies enable their forwarding-header support for this reason. P21/P22 deployment authority must preserve the same boundary in the production Nginx configuration and configure any non-loopback proxy networks explicitly.

Redis keys contain hashed identity/IP material only. Redis/configuration failure is fail-closed and does not fall through to the protected authentication operation.
