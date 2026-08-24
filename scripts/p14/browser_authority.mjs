// P14 exact-head evidence authority trigger.
// This file is intentionally non-runtime. Its browser*.mjs name binds governance-only
// authority commits to both existing P14 browser producer path filters.
// Update the phase marker only when an authority commit must regenerate T022/T023 evidence.
export const p14BrowserAuthorityPhase = 'T024-coherence';
