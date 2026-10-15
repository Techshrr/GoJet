# P20 T042 and T043 file/mail recovery

These independently inspected cases share the full same-head T039–T041 timeline.
No product, migration, frozen oracle or defect-ledger change is required.

## T042

Use the original T016 real uploaded file and a real P15 owner session with fresh
one-time CSRF for every mutation. The first scenario starts from the real T023 admin quarantine queue. Subsequent
scenarios revoke publication through the approved rescan API. Each executes the
native fileworker and proves that workspace
download, public download and publish all remain denied on uncertainty.

Five conditions are exercised: a reserved non-listening endpoint, malformed
health response, scan timeout, stale signature metadata, and indeterminate scan
response. Negative protocol fixtures are explicitly inherited from P09. They
cannot supply successful recovery authority. Every recovery executes the native
worker against the actual ClamAV daemon and its installed signatures.

A second authorized rescan must create the next generation. A clean scan must
remain unpublished until the owner explicitly publishes. Both downloaded digests
must equal the original file content digest. Ten native scan records bind file,
workspace, generation and attempt IDs to SQL observations. No scan-state rows or
claim records are manually patched.

## T043

A real authenticated support-ticket request atomically creates the authorized
mail job. Cloudflare's official test token is fixed, so the harness waits for
the inherited ten-minute replay key to expire naturally before this new request.
It never deletes or shortens the replay key. Repeating the submission with the
same token must be rejected and leave the one logical mail job unchanged.
The original native mailworker sends through the inherited P14 local SMTP protocol
sink. No claim of delivery to an external mailbox is made.

Two actual SMTP 451 responses must persist transient failures with the production
30-second then 60-second backoff. The harness waits real time; it never rewrites
queue timestamps. Worker restart before the due time must not send. After SMTP
recovery two native workers race through the real queue claim mechanism; exactly
one authorized message may be accepted. Another worker run must not resend it.
Original worker logs, durable attempt timestamps, recipient
binding booleans and safe SMTP counters/digests are archived. Addresses, mail
bodies, session cookies, CSRF tokens and raw claim material are not archived.

Each case remains unaccepted until native CI succeeds, original evidence is
independently inspected, and same-head canonical Contract/Candidate Freeze plus
native baseline gates all succeed.
