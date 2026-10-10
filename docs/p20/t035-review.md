# T035 evidence review in progress

T035 is not accepted. PR #189 is CI-only and must never be merged. The
integration branch remains at accepted T034. This document records observations,
not a WCAG conformance certificate or replacement for the frozen oracle.

## Reviewed candidate

`6e3f13e2f1740101f2d8669aba7b21b4748d8789`:

- P10 run 37781305494 / artifact 11555146065
- P17 run 37781279124 / artifact 11552159740
- P19 run 37781275892 / artifact 11554615110
- P15 run 37781278055 / artifact 11555214034
- P18 run 37781279350 / artifact 11556231277
- T034 run 37781275930 / artifact 11556352574

The collector was replayed against the six downloaded archives after matching
the GitHub archive digests and exact implementation head. Independent validation
recomputed 72 observations and 884 Tab steps, all capture digests, and recursively
revalidated the T034 prerequisite. Nine existing native interaction records are
also retained with their original schemas and provenance. Collection digest:
`sha256:1367fadec8e93b255dc347a56ebbccd142b05174e4e1f2c43cf7ba60adeaf18f`.

## Visual and source review findings

Directly inspected 320px captures: workspace, admin, docs in light theme;
website, auth-invalid, public in dark theme. The inspected captures show readable
wrapping and visible controls. Auth error uses text plus an icon; Public Text
wraps its actual content. This sample does not establish all-state conformance.

Two gaps were found outside the clean automated diagnostic result:

1. Website mobile navigation declared `aria-modal` on an ordinary div, without
   modal focus containment, Escape dismissal or focus restoration. It now uses
   the native dialog lifecycle. P19-T025 adds actual keyboard opening, forward
   and reverse traversal, Escape, close-button activation, return to the trigger,
   and link activation for English/Chinese at mobile and 320px. Native CI must
   validate these assertions before the fix can be accepted.
2. The Website language-switch label rendered missing-glyph boxes in the Linux
   CI capture. The runner now installs Noto CJK, and the link identifies its own
   language with `lang` as well as its destination language with `hrefLang`.
   Fresh captures must confirm the glyphs are rendered.

## Automation and remaining admission work

`p20-t035-matrix.yml` collects same-head archives and recursively verifies T034,
then recomputes raw T035 diagnostics. Its PASS is evidence packet validation,
not formal T035 acceptance; `formal_p20_t035_claim` stays false. It preserves
raw captures, observations, interaction records and manifests for review.

Remaining: verify the new native menu assertions and glyph captures, finish
interactive/manual review of the representative surfaces against T035 and the
applicable G5 requirements, and wire the frozen `browser_matrix.py --case T035`
formal admission only when that evidence is sufficient. No T036+ unlock or
integration promotion is implied by diagnostic PASS.

## 7c410e6 native verdict and follow-up

P19 run 37800180630 failed at the new forward/reverse keyboard traversal:
`keyboard focus escaped mobile menu`. The previous assertion did not retain the
active element, so it does not prove whether focus reached the browser chrome,
the dialog itself or another document element. The modal lifecycle alone did
not satisfy the tested control cycle. Add explicit first/last control wrapping
for Tab and Shift+Tab; keep all containment assertions. Retain per-step element
index/tag, containment, document focus and modal state plus a failure capture,
so any further failure is diagnosable from native evidence.

T035 run 37800152559 correctly rejected the failed P19 producer. Separately,
P15 run 37800159238/job 113389898823 passed its contract/coherence checks and
T028 auth section, then artifact creation failed with `ECONNRESET`. This caused
T028 and T029–T034 to fail by dependency. It is an upload transport failure,
not an authentication assertion failure. New-head native execution and upload
must succeed; older diagnostic PASS does not substitute for these results.

## 6b65c36 native focus cycle and close-event race

Verified Website archive 11564382935 from run 37809822705, SHA-256
`067a3e9ef201abb8817e383d55831a106f91d5f3ddb1c97af691e638b6905283`.
English and Chinese mobile tests completed without errors; their traces each
contain 24 forward/reverse steps, all within the modal controls. At 320px,
English also completed all 24 steps but timed out reopening after Escape.

The controlled dialog effect calls `close()`, which queues a native close event.
The redundant `onClose -> setMenuOpen(false)` handler can overwrite a subsequent
open action. Remove that feedback path: all dismissal paths already update
React state (Escape cancel, close button, and navigation links), and the effect
owns the native dialog lifecycle. Preserve the rapid-reopen assertion and add
five immediate Enter/Escape cycles per viewport/locale, plus native lifecycle
records. Fresh CI remains required; this is not accepted by source reasoning.

At this head T028–T033, Contract and Freeze succeeded. T034/T035 still reject
the failed Website producer. No T035 acceptance or integration promotion.

## 46562e9 batch evidence review

Website run 37816439828 succeeded. Verified artifact 11567825869 digest
`a307a72a0c0af0d5cad36f54618b903f15121b6f132ddb7b8d08d05d138a7998`.
All four menu variants (en/zh-CN × mobile/320px) passed, including 96 retained
Tab/Shift+Tab steps and 20 immediate reopen cycles, Escape, close button,
focus return and actual navigation. Direct capture inspection confirms that
Chinese menu text and the Chinese language-switch label now render glyphs.

All five native archives were downloaded and verified against API digests.
The other 64 diagnostic observations pass. Website desktop/tablet passes, but
four mobile/320px observations retain `aria-valid-attr-value` incomplete items.
The fixed axe 4.10.3 evaluator deliberately reports `controlsWithinPopup` for
`aria-controls` combined with `aria-haspopup`:
https://github.com/dequelabs/axe-core/blob/v4.10.3/lib/checks/aria/aria-valid-attr-value-evaluate.js

Do not remove valid ARIA attributes or suppress the rule. Retain the exact
check message key and structural relationship (no text/IDs): button, dialog
popup type, exactly one reference and one target, native named dialog without
conflicting role, matching expanded/open state. Resolve only that specific
review reason after validating every field. Missing evidence and all other
ARIA review reasons remain unresolved. The aggregate also revalidates all four
native menu cases and their 96 raw focus steps. Negative checks reject missing
steps/controls, escaped focus and mixed head. Raw axe records remain unchanged.
Older archives lack the new structural fields and cannot be retroactively
admitted with this resolver.

P14 contract/coherence succeeded on retry; P13 evidence also succeeded on
retry after its already-successful producers had exceeded its original wait.
The downstream T028–T035 chain was restarted on this same head. These transport/
orchestration recoveries do not solve legacy expired closure archives.

Outstanding formal admission includes screen-reader evidence and complete
zoom/manual review against G5. Current diagnostics and DOM/ARIA inspection
must not be labeled actual screen-reader testing. T035 and P20 remain open.

## 6d666ab complete packet and supplemental batch

Recovered the original P14 contract job after its native producers completed;
then reran T028–T035 on the SAME head. All eight workflows succeeded, without
new commits or rerunning the already-successful product producers. T035 run
37832440226 attempt 2 produced artifact 11591136808, archive digest
`sha256:77243211b162a1a8ef1005ba878765d3ede801e9af5f5de129c1ceb427291fbc`.
The complete 292-file packet retains all 72 diagnostic observations, 884 native
Tab steps, four menu variants / 96 steps / 20 reopen cycles and the recursively
validated T034 prerequisite. All diagnostic checks pass. Directly reviewed the
nine 320px surfaces in both themes for wrapping, visible focus and textual
error/success messages. This is a sample review, not full conformance.

The next supplemental batch covers ALL nine native states together. It adds
18 200% device-metrics-equivalent captures (1440x900 physical area, 720x450 CSS
pixels at DPR 2), with explicit method and overflow checks. It does not claim
that browser-toolbar zoom was changed. The headed native sessions run under
Xvfb/Openbox/DBus with real Orca and speech-dispatcher. Two distinct controls per
state must produce fresh real Orca label/role utterances and its corresponding
dispatcher attempts after actual Tab input. Logs from a speechless Orca fallback
cannot pass. Engine version, fresh log byte intervals and relevant original
utterance lines are retained; complete desktop debug logs remain temporary.
Separate label/role voices are supported without suppressing missing output.

This is a screen-reader focus-announcement sample, not proof of audible playback,
all state announcements, or complete manual WCAG review. Its first native CI
verdict is still required. No formal T035 acceptance, integration promotion or
T036+ unlock follows from adding this tooling. Historical 6d666ab records remain
valid for their diagnostic scope; they lack the new supplemental evidence and
cannot be admitted as new-head supplemental proof.

First native result at 3a14113: Docs run 37874842147, job 113640950677.
Orca 46.1 and the AT-SPI registry start successfully. P18-T019 itself PASSES.
The following wrapper fails because `speech-dispatcher --spawn` exits 1 for an
already-running daemon. Independently, retained T035 JSON rejects dark zoom:
light reports 720x450/DPR2, but dark reports 320x844/DPR1. Playwright fullPage
capture competes with the supplemental CDP metrics session. Neither failure is
waived. Connect/preflight the real eSpeak module via SSIP; capture zoom directly
through its owning CDP session, explicitly reset each theme, and verify the PNG
physical width is 1440. Preserve the zoom assertions. Fresh native results
remain required; no screen-reader sampling PASS has yet been established.
Each desktop command also owns a private runtime directory and a real
PulseAudio virtual sink for eSpeak (the upstream audio default is PulseAudio).
This avoids requiring a physical CI sound device or substituting a dummy
speech module. It still does not claim audible playback verification.

## Native e524e73 verdict and complete admission batch

All five native producers succeeded on e524e73. Independent raw replay passed
eight states and rejected Admin: a previous Revoke button's delayed role
dispatch was retained as metadata for the current Confirm revoke control.
Current combined label/role output was present, but the stored role field did
not match its independent reconstruction. Preserve that failure. The parser
now prefers current label+role output and allows only a bare role as a separate
utterance. Another control's named utterance can never supply the current role.

Source review also found durable Text deletion executed immediately on the
first button activation. Add an inline, named confirmation with the server
resource title, irreversible-action explanation and change reason. Cancel and
Escape return focus to the trigger; real deletion requires explicit confirmation.
Native P10-T017 verifies cancellation preserves the authenticated server record,
then keyboard confirmation produces DELETE 204 and public HTTP 410. Its 320px
confirmation capture is retained alongside the nine-state packet.

Apply the SC 1.4.12 user stylesheet to all nine native states in both themes,
retaining 18 additional 320px captures and raw overflow/clipping observations.
Together with previous captures and deletion review this creates 109 images
for the current representative review. No old image or raw record is upgraded.

The frozen browser_matrix.py driver now supports T035 admission separately
from diagnostic collection. Formal admission requires a review in tracker #188
bound to the exact head, collection digest, every capture and reviewed source
digests. All 55 WCAG 2.2 A/AA criteria need a reason and evidence for PASS or
NOT_APPLICABLE; applicable obligations cannot be waived. This does not generate
review or certify audible playback. Missing or stale review fails closed. The
complete native packet must first pass independent replay and actual review;
then the same-head formal job can be rerun without another source commit.

Local validation: 11 Python admission/collection tests, 10 Node raw diagnostic
and speech parser tests, Workspace TypeScript, workflow YAML and driver syntax
pass. Fresh native CI remains required; T035/P20 are open and T036+ is locked.
The e524 T028 retry also exposed P09 Evidence's earlier producer wait timeout;
its four real producers subsequently succeeded. Recover that exact-head job,
without treating a transport/queue recovery as a product-code correction.

## Responsive functionality review and retained deletion failure

Reviewed all 90 e524 images across the nine native states, both themes and five
capture profiles. Source and images show Workspace/Admin sidebar routes vanish
below the mobile breakpoint with no replacement. Website also hides its login
header action while omitting it from the mobile menu. These are functionality
losses; no conformance review is signed for either e524 or 86d654.

Provide native details/summary navigation outside the sticky Workspace header,
using the same route arrays as desktop. Include Workspace switcher/Create and
Admin Audit/Overview. Escape closes the disclosure and returns summary focus;
activated links close it. Website's modal adds Sign in/Get started in both
locales, so native traces now require eight controls and 128 total Tab steps.
Supplemental probe compares actual mobile/desktop routes, reaches every mobile
control by Tab and retains four open-navigation captures in both themes.
Current review inventory therefore requires 113 images.

86d654 P10 run 37889587201/job 113687241909 fails at the final network diagnostic
after cancellation-preservation, DELETE 204, redirect and public HTTP 410 checks
execute. A request to text-shares/4 reports net::ERR_ABORTED. Old metadata omitted
method/status, so do not assume a cancellation, product bug or harmless 204.
Retain request method/type/navigation/observed status and complete deletion
results even on failure. Keep the request-failure assertion; no waiver is added.

React review: stable route arrays, no new dependencies or async fetching;
native disclosure and existing overlay lifecycle reused. Workspace/Admin/Site
TypeScript and 11 Python + 11 Node boundary tests pass. Fresh native navigation
and deletion-network verdicts remain required. P20/T035 stay OPEN.

86d654 Website native workflow succeeded, but its independently downloaded raw
T035 record FAILED because Orca yielded zero admitted controls after the new
spacing sample. Archive 11598455339 SHA-256
`eff4236dd6906fa2b6d0a41bb61f63da6a530970d845ad119eaae964145b314a`
verified; both zoom and both text-spacing observations pass. New current-name
parser correctly replays all historical e524 receipts, so no parser exemption
is added. Failed first focus attempts were marked visited and could never be
sampled again. Restore fresh real Tab retries with a three-attempt-per-control
bound, while only successfully announced controls become visited. Each failed
attempt retains focus state, byte interval and output/dispatcher counts without
full debug text. Two actual distinct valid announcements remain mandatory.

1b35e50 P10 run37890537382/job113690227937 proves the retained network event is
DELETE/fetch/non-navigation, HTTP204 already received, then net::ERR_ABORTED.
The client fulfilled its operation and navigated; public access is HTTP410.
Classify only this exact no-content deletion after additional real MySQL proof:
non-null deleted_at, version increment exactly one, and one successful
text.delete audit. Preserve every original requestfailed row in native evidence.
No classification is possible for GET, unknown/failed status, another URL,
missing redirect/tombstone, wrong version, duplicate/missing audit or other
transport errors. T035 independently rechecks these raw fields and durable
proof. RFC9110 §15.3.5 defines HTTP204 as complete at its headers with no body:
https://www.rfc-editor.org/rfc/rfc9110.html#section-15.3.5
This is outcome classification, not a claim that the Chromium abort cause is
fixed. Its native verdict and all remaining supplemental evidence are required.

5ffc343 Docs native run37891224538 succeeds, but independent raw T035 audit
rejects its text-spacing verdict. Archive11598328854 digest
`b128232649be60afff11d8da40c8b180c0ca9875f11b41345a14ec4c455a2fd1`
verified. The two flagged anchors correspond to Starlight heading permalinks;
source allows visible icon overflow and retains screen-reader-only label text.
Actual capture shows readable wrapping. scrollHeight/clientHeight mismatch alone
does not establish clipping when overflow is visible. Retain each sampled
node's client/scroll dimensions and computed overflow axes, and independently
recompute clipping only for hidden/clip/auto/scroll axes. Root horizontal
reflow remains mandatory; unknown metadata or forged clean clipping fails.
Negative tests still reject real hidden-overflow cropping. No product CSS,
permalink, focus or text-spacing override is removed to achieve a pass.

5ffc343 native findings are retained, not rewritten as passing evidence.
P10 run37891223026 archive11599135203 proves the cancel/confirm deletion
operation, database tombstone, version1->2, one audit and public410.
Workspace menu19 routes and Admin menu13 routes pass both theme traversals.
Workspace textarea fragment centers fall below the viewport after keyboard
focus: center the editor on focus-visible, preserving the strict fragment audit.
Admin run37891223021 archive11597958394 shows actual root width339 at320
under text spacing: allow long headings to wrap and header intrinsic shrink.

Public's hash-only style CSP correctly rejects addStyleTag. Apply the user
text-spacing override through an inspector-origin CSS stylesheet instead,
verify its actual computed spacing, retain both captures and restore that
stylesheet. Do not enable bypassCSP or change the immutable response policy.
Docs retains computed overflow dimensions so decoration is distinguished
from clipping; genuine clipping and horizontal root overflow still fail.

Auth run37891224661 archive11598139700 and Website run37891224705
archive11598578831 retain incomplete native Orca samples. Website archive
digest: 01ca4975a4c6233197222962b4d6f045133804b48c834f327040dab3a1b777b1.
DOM focus alone is insufficient evidence of native keyboard delivery. Use
X11 XTEST Tab with the active window verified against visible Chrome class
windows and DOM focus, then require the same fresh name/role and real
speech-dispatcher receipts. This is a candidate repair requiring native CI,
not an established explanation of the old silent samples. No speech is
manually dispatched and no incomplete sample is waived.

## 7f953932 native diagnostics complete; predecessor selector repair

All nine original native T035 records independently pass auditFile: 72 standard
observations and 892 Tab steps, plus all required zoom, text-spacing, mobile
navigation and native-X11 Orca samples. This is diagnostic evidence, not formal
admission. Website run37892918619 artifact11599894018 archive SHA256
3b7161b63bf874cc05077252b2554f03f685b14cc960bef631a4e3407849d976.
Docs run37892918631 attempt2 artifact11602044619 archive SHA256
52a09d7c07287d6d8d760ba2e3471252ca0965041293e2478c7dc7edfc9e0e2e.
Both archives and raw exact-head records verified. Docs attempt1 failed before
browser execution when the Orca startup process exited; no page assertions
were produced. Same-head attempt2 succeeded; do not claim that startup cause
was diagnosed or repaired.

P04 jobs113713916608 and P12 job113697651459 reveal a shared predecessor
compatibility failure: old locators match both the visible desktop navigation
and the newly retained hidden mobile tree. Preserve all existing membership,
server mutation/version, SPA marker, viewport and route assertions. Resolve
interactive switchers by their accessible combobox role/name, and SPA links by
visibility; multiple visible controls remain strict failures. In the shared
P12 driver, scope the notification deep link to the actual notification page,
so a sidebar link cannot falsely satisfy notification authority. Keep the
all-DOM unauthorized-membership inspection unchanged. This repair changes
verification targeting only; it neither deletes the mobile navigation nor
weakens business assertions. Native P04/P12 and new-head dependencies must pass
before T035 formal acceptance. T036+ remains locked.

## Manual representative review: non-text contrast and change of context

SC1.4.11: Auth text-entry boundaries incorrectly used the decorative divider
token. Source-derived unrounded sRGB ratios against adjacent canvas are
1.4076927652721558 (light #CBD5E1/#F7F9FC) and 1.345385724619138
(dark #1E293B/#070B14). Empty entry controls need a distinguishable boundary.
Use the existing border-default token: ratios4.512008200191281 light and
7.675877833480309 dark, also above3 against each input's own background.
Do not change decorative dividers or disabled-control contrast exceptions.
See https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html .
This is a manual finding outside the existing axe text-contrast checks.

SC3.2.2: the native P12 switcher reloads the page through the real Workspace
authority. Add visible prior advice to both responsive switchers and bind
each description with a unique aria-describedby ID. The visible name remains
Workspace switcher. P12-T019 now verifies that the actual rendered description
is visible before performing the same real membership switches and server
settings mutation. No switch, navigation or permission assertion is removed.

These findings prevent a clean formal review of the earlier diagnostic packet.
A new-head packet must include the corrected product styles/description before
manual admission. The earlier automated PASS remains only diagnostic evidence.

## Manual label-in-name review

Source review found two Admin controls whose aria-label overrode their visible
labels: Global search and Six-digit code. The accessible names now contain
those exact visible strings, preserving the additional search scope and TOTP
purpose. P17-T030 explicitly locates the actual searchbox and enrollment textbox
by these names before continuing its real MFA and durable revocation checks.
This repairs the SC 2.5.3 source finding; fresh native authority is still required.
Reference: https://www.w3.org/WAI/WCAG22/Techniques/general/G208

Separately, a74fd789 repairs the P12-T019 result serializer: the switch notice
was assigned through an undefined details variable. Its evidence now belongs
in the existing PASS result object. The visible/precedes-control assertion and
all real membership, settings persistence and viewer restrictions remain.

## Identify-input-purpose manual finding

On 2a404c65, native diagnostics pass but manual SC1.3.5 source review finds
`verify-email` and `social-email` without an explicit autocomplete purpose.
An email input type or browser heuristic alone does not identify the user's
own email purpose. Both fields now declare autocomplete=email. The native
P15-T024 capture path checks every rendered Auth email/password input and
retains only id/type/autocomplete metadata, never the input value.
No authentication behavior or frozen oracle changes. Fresh exact-head native
evidence and final review are still required before formal acceptance.
Reference: https://www.w3.org/WAI/WCAG22/Understanding/identify-input-purpose

2a404c65 recovery: P12 browser passed. P17-T030 initially exited at the Orca
startup process check before browser execution; same-head retry succeeded
(run37932655054/job113863097036). This is a recovered infrastructure attempt,
not evidence that the intermittent startup cause was fixed.

## Retry archive-name boundary

The recovered P17 run37932655054 retains failed merge-SHA-named archive
11621272894 and successful head-SHA-named archive11622822498. Both share the
same required prefix, so T031-T035 collectors rejected the two names before
applying their existing attempt boundary. The shared selector now narrows
multiple names to the admitted attempt start, then requires exactly one name
and artifact. No fallback to an older attempt, arbitrary newest artifact or
foreign run is allowed; SHA/digest/raw-result validation remains mandatory.
A sole retained artifact from an already-successful matrix job remains usable
when only a different matrix job was retried. Real API metadata replay selects
11622822498. Synthetic tests reject missing/ambiguous boundaries, expired and
foreign-run/head/digest evidence. All34 T03-series Python tests pass locally.
# cc40769 native verdict and T034 sampling correction

All five native workflows, P20 Contract, Candidate Freeze and T028–T033 succeed
on cc40769cf02801546c97836cf2e4ec0dd32106f0. All nine original T035 records
independently pass (72 observations / 892 Tab steps). This does not admit T035.
T034 run37968972909 artifact11636814705, verified ZIP SHA256
5956790d0bec22ff09c55c38156880524e87decf6a44f46e358917b6bba23854,
rejects Workspace mobile/dark: reduced_motion=true, active_animations=6.
Other eight native T034 states pass; T035 fails on this required predecessor.

The original record does not identify the six animations, so their exact cause
is unproven. Source review finds the settle timer starts after reading only root
tokens, before descendant transitions necessarily start. Flush document animation
styles and cross two animation frames before starting the existing bounded wait.
Keep the zero-running-animation requirement unchanged, and retain running
animation type/property/tag/timing (without text, selectors or input values) for
any subsequent failure. No animation cancellation, suppression or PASS rewrite.
New native CI is required; the sampling correction is not yet a proven fix.

Separate P18/P19 historical predecessor live-binding steps fail with HTTP404;
these are not native browser failures and are not waived by this correction.
P20/T035 remain OPEN; T036+ locked; PR189 remains CI-only/NEVER MERGE.

## c26a0c4 actual transition evidence and recovery

P09 evidence37975397003 originally timed out while three native producers queued;
all later succeeded. Same-head failed-job retry now succeeds. T028–T035 inherited
that failure; no product assertion was waived. Contract/Freeze and five native
browser workflows succeed. Original P10 artifact11643865319 SHA256
4488e570191831beddbb0008152cfa19cc33cc144ea9a25d7012f004251abd9d
still reports T034 Workspace mobile/dark FAIL. The two-frame correction alone
was insufficient. Retained details identify exactly six BUTTON CSSTransitions:
background-color, four border colors, color; each current_time=0,duration=120,
delay=0,iterations=1. Other eight native T034 records pass; all nine T035 records
report PASS (formal admission remains blocked by T034).

After the existing canonical settling interval, await actual animation.finished
only for single-iteration CSS transitions whose full duration+delay fits the
canonical budget. Preserve every sampled transition and elapsed time. Reject
noncanonical/repeating motion, cancellation, or a one-second overall timeout;
never cancel/finish animations or hide original failure. Final zero-running
assertion remains. Native verification required; no claim this candidate passed.
