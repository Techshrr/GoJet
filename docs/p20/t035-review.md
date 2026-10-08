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
