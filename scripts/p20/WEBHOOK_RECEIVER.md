# P20 controlled webhook receiver

This is temporary external test infrastructure, not a GoJet production service
or a ninth daemon in the product inventory. Its unit tests do not establish
formal P20-T024 evidence. Deploy it on a controlled test host before running
the real production sender. No external deployment is included in this PR.

## Deployment

Use Python 3 on the test host. Set `P20_RECEIVER_CONTROL_TOKEN` to an independently
generated random token of at least 32 characters through the host's protected
environment. Start `python3 scripts/p20/webhook_receiver.py`. It listens only
on `127.0.0.1:18884`; `P20_RECEIVER_PORT` can change the port.

Expose it through an existing Nginx HTTPS virtual host on port 443 with a valid
certificate and a publicly routable DNS address. Proxy to `http://127.0.0.1:18884`,
set `client_max_body_size 256k`, and disable request/access logging for this
dedicated test virtual host. Do not log request bodies or authorization headers.
No redirect or browser challenge may intercept the receiver paths. Do not
change GoJet's private-address, DNS or TLS restrictions to reach this server.

Provide the HTTPS base URL to the verification operator. Store the same control
token as repository Actions secret `P20_WEBHOOK_RECEIVER_TOKEN`, and the base URL
as repository variable `P20_WEBHOOK_RECEIVER_URL`. Never place the token in an
Issue, PR, artifact, command-line argument or chat message. The formal harness
still needs to bind this configuration; this preparation does not dispatch CI.

## Protocol

All `/runs/<run-id>` operations require `Authorization: Bearer <control-token>`.
Run IDs have 8–80 ASCII letters, digits, underscores or hyphens.

- `POST /runs/<run-id>`: JSON `secret`, `workspace_id`, `fail_first` (0–2).
  Use the real secret-once webhook response, not a synthetic producer secret.
  State expires after 30 minutes; an existing run cannot be reset by POST.
- `POST /deliver/<run-id>`: production webhook URL. Verifies GoJet's v1 HMAC
  over delivery ID, timestamp and raw body, checks timestamp freshness,
  Workspace/event correlation and the idempotency header. First configured
  attempts return 503, subsequent valid attempts return 200.
- `GET /runs/<run-id>`: protected report with attempt/acceptance counts,
  delivery IDs and body hashes. Never returns secrets, signatures or bodies.
- `PATCH /runs/<run-id>`: JSON `secret` installs a real rotated secret without
  resetting observations.
- `DELETE /runs/<run-id>`: removes state and secrets after verification.

Only 16 live runs and 16 deliveries per run are retained. State is in memory:
a receiver restart invalidates that test run; it is not durable product evidence.
The harness must fail if configuration or observations disappear. Stop the
receiver and remove its temporary HTTPS route after the verification window.

## Local protocol tests

`python3 -m unittest discover -s scripts/p20 -p test_webhook_receiver.py`

These tests check the receiver itself. Full T024 must independently correlate
real product creation, MySQL event/queue state, operationsmonitor attempts,
receiver observations and application audit on the same candidate SHA.
