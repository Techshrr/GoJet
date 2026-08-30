# Authentication Rate-Protection Runtime Contract

When `GOJET_AUTH_ENABLED=1`, native `platformapi` requires an explicit server-side Redis rate policy for the P15-owned authentication abuse surfaces.

Required non-secret runtime settings:

- `GOJET_AUTH_RATE_LIMIT` — positive integer request limit for the shared identity/IP bucket policy.
- `GOJET_AUTH_RATE_WINDOW_SECONDS` — rate window in whole seconds, from `1` through `86400` inclusive.

There is intentionally **no production default** in source control. Deployment/release authority must select reviewed values for the production environment rather than inheriting a CI fixture or an implicit application default.

The single policy is enforced server-side for the frozen P15 surfaces:

- registration
- password login
- login email-code request/consume
- password-recovery request

Redis keys contain hashed identity/IP material only. Redis/configuration failure is fail-closed and does not fall through to the protected authentication operation.
