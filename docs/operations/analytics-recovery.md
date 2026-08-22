# GoJet V10 Analytics Recovery Runbook

Scope: P07 analyticsworker / analyticsreconciler. This runbook does not change redirect safety, destination-risk, custom-domain authority, or Link access ordering.

## Data authority order

1. `links.click_count` plus the same-transaction `analytics_outbox` row prove an accepted redirect click.
2. Redis Stream `gojet:analytics:clicks:v1` is the delivery transport, not the final counting authority.
3. `analytics_events` is the logical-once consumed event set keyed by deterministic `event_id` and `(link_id, click_sequence)`.
4. `analytics_hourly_aggregates` is derived materialized data and may be rebuilt from `analytics_events`.
5. `analytics_workspace_state` communicates `complete`, `partial`, or `stale`; unavailable data must never be presented as a complete zero.

## Normal restart

`analyticsworker` uses a stable consumer name per host and reads its pending messages before new messages. A process restart therefore retries unacknowledged delivery. MySQL persistence occurs before Redis ACK, and duplicate event delivery is safe because `analytics_events.event_id` is unique and aggregate increment only occurs on the first insert.

## Redis publication failure after accepted redirect

The redirect result is not changed after the accepted click transaction commits. The durable outbox row remains unpublished and records the publish failure. Run `analyticsreconciler`; it republishes unpublished outbox events to Redis. If Redis accepted a message but marking the outbox published failed, a later replay is safe because the deterministic event identity prevents double counting.

## Aggregate mismatch

Before an operator-initiated aggregate repair, stop or quiesce `analyticsworker` for the short repair window. Run `analyticsreconciler` with `GOJET_ANALYTICS_RECONCILE_REPAIR=1` and `GOJET_ANALYTICS_RECONCILER_ONCE=1`. The reconciler compares `COUNT(analytics_events)` with `SUM(analytics_hourly_aggregates.clicks)` and rebuilds the derived aggregate only when they differ. Start the worker again after the repair.

A second repair run with unchanged source events must report no mismatch and no repair. Every run is appended to `analytics_reconciliation_runs` with source, before, and after totals.

## Closure checks

For a fully drained deterministic environment:

- `COUNT(analytics_outbox)` equals `COUNT(analytics_events)`.
- `COUNT(analytics_events)` equals `SUM(analytics_hourly_aggregates.clicks)`.
- unpublished outbox count is zero.
- Redis consumer pending count is zero after workers have drained.
- Workspace analytics state is `complete` only after reconciled totals agree; otherwise it remains `partial` or explicitly `stale`.

## Logging and sensitive data

Worker/reconciler logs may include event ID, stream ID, Link ID, click sequence, counts, run ID, and safe state. They must not include destination URLs, passwords, provider evidence, ownership secrets, or unrelated user content.
