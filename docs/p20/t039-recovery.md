# P20 T039 Redis interruption and native recovery

The harness uses the actual T013 Link and already-drained analytics timeline after
same-head T038. A transparent TCP proxy forwards original Redis command frames and
server replies; its only fault behavior is to close connections. It never returns
synthetic Redis data. The real Redis server and MySQL remain the authorities.

The scenario proves:

1. Total Redis connection interruption denies navigation and adds no click/event.
2. Interruption specifically at XADD occurs after the real redirect transaction:
   the accepted click and unpublished outbox survive, with no invented event row.
3. The native reconciler reports partial while publication fails, then republishes
   the same event after connectivity returns. The native worker consumes it once.
4. Repeated reconciliation leaves source totals and Redis stream length unchanged.
5. Loss of the exact risk cache key denies navigation without adding a click. The
   native operationsmonitor rebuilds from the current durable P16 decision, with
   the same fingerprint/policy and a validity bound no later than that decision.
6. A real subsequent redirect succeeds and its new click converges through the
   worker/reconciler. Final outbox, consumed events and aggregate totals agree.

For the cache-rebuild precondition, `t039_prepare` reuses P16's controlled provider
observation, native local-policy evaluation and durable decision APIs on the real
T013 Link. This is explicitly a controlled provider fixture, not a live external
provider claim or a direct Redis allow write replacing recovery. After cache loss,
only the production operationsmonitor/projector may restore the runtime decision.
No Link, click, outbox, consumed event or aggregate row is manually patched.

The archive retains original native logs, SQL snapshots, safe source identifiers,
HTTP outcomes, fault counters and a digest manifest. Source-head/predecessor checks
and independent inspection are mandatory. This case does not claim worker crash,
MySQL restart, mail retry or provider-failure coverage from later recovery cases.
