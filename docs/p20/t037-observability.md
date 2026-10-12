# P20 T037 native observability verification

The frozen oracle requires eight-service logs, operations and audit to correlate
without leaking prohibited material. This work repairs missing product logging
and the missing native logreceiver executable; it does not change that oracle.

The original P0 Support timeline runs against real MySQL, Redis, ClamAV, authenticated
platformapi, SMTP and the authorized external webhook receiver. The native receiver
starts before those processes. `t037_business.py` binds actual committed T020 mail
jobs and T023 Admin audit records to original producer logs and received records.
It never inserts notification, mail, event or audit fixtures to manufacture the
correlation. `t037_case.py` then binds all eight executable identities:

- redirectengine's accepted click to the original durable analytics outbox;
- analyticsworker's processed event to the same click and published stream ID;
- both analyticsreconciler cycles to their SQL reconciliation runs;
- fileworker's clean verdict to the original T016 scan attempt and T023 file;
- mailworker's post-commit outcome to the real T020 mail job and message;
- platformapi's request fingerprint to the authenticated T023 audit action;
- operationsmonitor's retry and delivery to the real T024 webhook records and audits;
- logreceiver's own lifecycle and unchanged receipt of every selected producer record.

Raw request/correlation headers are fingerprinted with a fixed domain prefix. HTTP
completion is not a successful-mutation claim. Mail and webhook outcome logs are
emitted only after the corresponding durable store operation succeeds. Authoritative
audit remains independent; operations logging cannot stand in for an audit write.
Endpoint URLs, bodies, recipients, credentials and delivery signatures are excluded.

The archive retains original cases, bounded native log snapshots, selected SQL
bindings and SHA-256 file inventories. Independent inspection rechecks each binding
and producer/receiver equality. Unrelated forwarding warnings remain counted and
visible; every required selected business record must actually reach the receiver.
The transport deliberately retains originals locally and does not claim durable
aggregation or replay. Later systemd packaging/retention remains P21/P22-owned.

A native test workflow separately exercises redaction, real socket round trips,
restart, permission/occupied-path rejection and all eight actual executables.
It includes byte-compared complete copies of unchanged canonical Contract and
Candidate Freeze jobs. Formal acceptance requires both workflows at the same SHA
and independent original-artifact review. No P20 exit or T038 acceptance follows
merely from a single test job passing. The T024 source here is its real outbound
runtime evidence, not a relabelled full T024 closure.
