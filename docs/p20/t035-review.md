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
