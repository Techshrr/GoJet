# P20-T036 performance verification

Tracker: #190. Legal base: `54a2881ae0d4dfce5aadfc05ce3b59e8b4b717c6`.
T035 #188 was accepted at that base after run38148230073 attempt3 and
independent review of the complete retained packet. Successful archive11713921049
has SHA-256 `dc4b1a15c5f6ee648fe626e8a6b9bb94be5927090a1d61eabea02e5f44f849e5`.
Integration advanced nonforce to that accepted base. PR189/178 remain NEVER MERGE.

## Evidence boundary

The frozen T036 oracle and all product sources remain unchanged. The new driver
collects original exact-candidate artifacts from P04 Product Shells, P07 Workspace
Analytics Browser and P19 Website Performance, plus successful same-head P20
Contract and Candidate Freeze. Collection verifies repository, run, retry attempt,
artifact SHA/head and every retained member digest; stale/missing/ambiguous
evidence fails. It independently recomputes budgets rather than trusting PASS flags.

- P04: built Website bundle isolation/inventory and21 browser layout observations,
  each at most0.02 shift, with native diagnostics and G4/G9 coherence.
- P07: real MySQL-backed Analytics page, DCL3000ms/load5000ms/1.5MB/60resources,
  matching original observations and G9 summary.
- P19: initial JS at most150KiB gzip, dynamic chunks, actual script requests,
  LCP2500ms/INP200ms/CLS0.1 under the existing1440x900/40ms/10Mbps/5Mbps lab profile.
  Trusted Event Timing entries determine the recorded interaction latency.
- Existing P19 startup/interaction measurement now also retains every native
  Long Tasks entry from navigation start, observation interval, navigation timing,
  rendered image dimensions/AVIF-WebP alternatives, font-face/resource inventories
  and existing actual cache response headers. An unsupported observer or any
  long task fails. The native threshold is defined by
  <https://www.w3.org/TR/longtasks-1/>; no slow entry is filtered or synthesized.
- Current Website uses no network font or rendered principal image. These are
  explicit observed empty inventories, not invented transfer/field measurements.
  Any future font-face/network font requires subset/header review. An added image
  must retain completed intrinsic dimensions and responsive alternatives.

The T035 accepted-base checkpoint authorizes this work; it is not relabeled as
new-candidate evidence. T036 emits only its own exact-head performance verdict.
T048/T049 still require the full candidate-wide matrix and accountable closure.
No T036 implementation or diagnostic run alone unlocks later tasks or closes P20.

## Validation

Original54a P04 and P07 ZIP digests verified; their raw evidence independently
passes the new inspections. The old P19 record correctly fails for missing native
long-task evidence. Synthetic unit fixtures test budget boundaries, stale heads,
false summaries, missing/unsupported traces, hidden bundle/font requests, image
failures and cache regressions; those fixtures are never native proof.

An isolated push-only `debug/p20-t036-performance` branch exercises the actual P19
build/Nginx/Chrome pipeline and independent Website validator before a formal
candidate push. Diagnostic results cannot replace exact formal-candidate evidence;
the diagnostic workflow and branch are NEVER MERGE.

The formal T036 branch uses a push-only candidate entry to dispatch exactly the
three required producer workflows at the same verified branch SHA. The two P20
guard workflows do not yet exist on main, so they cannot use workflow_dispatch.
Their COMPLETE canonical jobs are copied into the scoped workflow; a mandatory
byte comparison rejects any changed/omitted step before execution. Only read-only
permissions and the prerequisite dependency are added. The collector binds their
successful named jobs, exact head and definition digests. Original guard files
remain unchanged. A negative test proves removing Go tests/frontend typechecks
is rejected.

The review PR targets the accepted integration branch, avoiding the unrelated
116-workflow cumulative diff against main. No existing workflow, frozen guard or
eventual whole-product regression requirement is disabled or changed. Failed-job
retries do not redispatch successful producers. All five authorities remain
required on the candidate SHA.
