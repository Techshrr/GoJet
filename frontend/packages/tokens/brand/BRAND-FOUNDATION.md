# GoJet V10 Brand Foundation

Status: **P02 implementation contract**  
Authority: `GJ-V10-DS-GREENFIELD-2026-08-20`  
Issue: #10

This directory implements P02 brand assets and reference evidence. It is **not**
a second authority for exact color, spacing, geometry or motion values. Exact
visual values remain owned by the approved Brand & Design System specification.

## Brand direction

The GoJet mark is a routed, J-shaped path with an explicit branch and event
nodes. It connects the product name to the Design System's Jet Path vocabulary
without turning routing graphics into generic decoration.

The full wordmark has light-surface and dark-surface variants. The mark remains
the same across themes; only the wordmark foreground adapts to the approved
surface contrast role.

## Usage rules

Logo layout must reserve the Design System `asset.logo.safe-area` around the
mark. Website and product surfaces consume the corresponding authoritative
logo-height tokens rather than hard-coded local values.

Jet Path is limited to the contexts approved by the Design System. It is not a
button treatment, input border, table-cell decoration or repeated card motif.
Reduced-motion users retain route state and final event position without a
continuous path animation.

## Asset provenance

GoJet-owned original assets in `assets/` are authored for this repository and
recorded in `BRAND-ASSET-LICENSES.md`. External brand marks are not bundled in
P02. If later needed, source order is Official Brand Kit, Official SVG, then
Simple Icons, with each asset recorded before use.
