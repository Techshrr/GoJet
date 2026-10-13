# P20 T040 and T041 native restart recovery

Both cases run sequentially in one isolated Actions job after the complete
same-head T039 timeline. Each emits its own original evidence manifest and formal
result. Sharing setup does not substitute one case's outcome for the other.

## T040: actual MySQL interruption before payment commit

The authenticated original P15 owner creates a new order through the production
API. The external provider-intent fixture boundary is identical to T021; the
callback still passes the real Stripe signature verifier. No transaction,
notification, settlement, subscription or grant is manually inserted.

A separate MySQL session holds a write lock on the final billing audit table.
The signed callback performs its real transaction. A READ UNCOMMITTED observer
must see the transaction insert and paid order while an ordinary read still sees
the initial pending order. Only after both observations does the harness kill
the specific MySQL container supplied by `job.services.mysql.id`. Execution is
restricted to GitHub Actions and the expected mysql:8.4.11 test image.

Restarting that same container must restore the exact initial committed state.
Replaying the valid callback must then produce one atomic settlement. Four
concurrent duplicates and an unsigned callback must leave it unchanged. Evidence
includes both SQL visibility levels, actual container restart timestamps, HTTP
outcomes and tenant-bound settlement/notification totals. The interrupted callback
must not acknowledge a successful payment.

## T041: actual worker interruption after persist and before ACK

A real redirect creates one actual click and outbox event. The transparent Redis
proxy closes XACK connections only; it never fabricates a Redis response. After
the native worker's committed insert is visible and Redis still has the pending
delivery, the harness sends SIGKILL. A new native worker using the same consumer
must replay that exact event/stream identity with inserted=false, ACK it, and
leave all durable totals unchanged.

The native continuous reconciler is separately killed after a committed cycle
and before the next scheduled cycle. This is explicitly a between-cycle restart
claim, not a fabricated mid-transaction failure. Native restart and a repeat run
must both converge without repair or manual data patching. Original JSON logs,
SQL snapshots, pending counts, actual process exit statuses and hashes are retained.

These test-service interruptions do not operate on any external or production
database. Frozen oracle, product code, migrations and defect ledger are unchanged.
Acceptance requires independent inspection plus same-head full runtime, native
baseline and unchanged canonical Contract/Candidate Freeze jobs.
