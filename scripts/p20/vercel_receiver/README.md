# Vercel test deployment

Authorized test hostname: `test.gojet.cc`. GoJet production stays on Linux.
This directory is external test infrastructure only.

Deployment file mapping (do not deploy the entire repository):

- `api/index.py` from this directory
- `vercel.json` from this directory
- `webhook_receiver.py` from the parent directory (shared protocol implementation)

Configure a dedicated test Redis database via the Vercel Marketplace. The adapter
uses the Upstash Redis HTTPS REST API. Set these encrypted project environment variables:

- `UPSTASH_REDIS_REST_URL`
- `UPSTASH_REDIS_REST_TOKEN`
- `P20_RECEIVER_CONTROL_TOKEN` (independent random token, at least 32 characters)

Never share this database with production. Receiver run secrets and counters have
30-minute expiry. Atomic compare-and-set updates preserve observations across
separate Vercel instances; there is no process-memory fallback. Missing configuration
returns 503 and can never yield formal PASS. `/healthz` reports configuration presence
only; an authenticated run round-trip is required to validate storage connectivity.

Attach `test.gojet.cc` to this dedicated project and use the exact DNS record Vercel
reports. The delivery path must be reachable without browser authentication/challenges.
The `/runs/*` control API remains protected by the independent token.

For GitHub Actions, configure variable `P20_WEBHOOK_RECEIVER_URL=https://test.gojet.cc`
and secret `P20_WEBHOOK_RECEIVER_TOKEN` with the same control token. The formal sender
harness still needs binding and same-head validation; deployment alone is not T024 PASS.

Receiver protocol and cleanup are documented in `../WEBHOOK_RECEIVER.md`.
