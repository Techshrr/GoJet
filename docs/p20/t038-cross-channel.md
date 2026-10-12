# P20 T038 cross-channel verification

The frozen oracle is verified against the same actual T020 administrator support
reply, not independently invented notification, mail or audit fixtures. Collection
requires same-run exact-head T020, T022 and T037 PASS evidence.

The reply message identifies its ticket, requester, workspace, administrator and
request correlation. The collector queries its unique server-produced support
notification, unique durable mail job and independent support audit record. It
checks the mail recipient against the requester membership without storing the
address, verifies the completed mail attempt and real SMTP sink state, and binds
the job to T037's original native mail record.

A real P15 recipient session lists that notification and accesses its authorized
support thread. Anonymous notification/thread requests and recipient access to
administrator mail/audit APIs must be denied. Notification read/unread operations
use real one-time CSRF authority and are checked in SQL. Full safe audit snapshots,
mail status/attempt counts and SMTP counters/digest must remain unchanged. A read
notification therefore cannot acknowledge or replace delivery or audit authority.

The retained packet contains safe identifiers, notification copy, response status
observations and before/after authority snapshots, with a SHA-256 inventory. No
session, CSRF token, recipient email, reply body or SMTP message body is retained.
An independent inspector recomputes bindings and rejects wrong-head, incomplete,
tampered, unauthorized or inconsistent evidence. Passing this case does not close
P20 or claim recovery behavior from T039 onward.

T038 acceptance requires the original runtime artifact and complete unchanged
canonical Contract/Candidate Freeze jobs at the same implementation SHA. Product
semantics, migrations and the frozen test plan are unchanged by this harness.
