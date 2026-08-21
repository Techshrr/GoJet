package links

import "html/template"

// publicSurfaceTokenDefinitions is a deliberately small, checked-in projection
// of the canonical P03 token output required by redirectengine's self-contained
// public surfaces. public_surface_test.go prevents this projection from drifting
// from frontend/packages/tokens/generated/tokens.css.
const publicSurfaceTokenDefinitions = `
:root {
  color-scheme: light;
  --gojet-surface-canvas: #F7F9FC;
  --gojet-surface-default: #FFFFFF;
  --gojet-surface-muted: #F1F5F9;
  --gojet-text-primary: #0F172A;
  --gojet-text-secondary: #334155;
  --gojet-text-muted: #475569;
  --gojet-border-divider: #CBD5E1;
  --gojet-border-default: #64748B;
  --gojet-action-primary: #2563EB;
  --gojet-action-primary-hover: #1D4ED8;
  --gojet-action-on-primary: #FFFFFF;
  --gojet-focus-ring: #2563EB;
  --gojet-focus-backplate: #FFFFFF;
  --gojet-status-warning-bg: #FFFBEB;
  --gojet-status-warning-border: #B45309;
  --gojet-status-warning-fg: #92400E;
  --gojet-status-danger-bg: #FEF2F2;
  --gojet-status-danger-border: #B91C1C;
  --gojet-status-danger-fg: #991B1B;
  --gojet-status-info-bg: #F0F9FF;
  --gojet-status-info-border: #0369A1;
  --gojet-status-info-fg: #075985;
  --gojet-font-family-latin: InterVariable, Inter, ui-sans-serif, system-ui;
  --gojet-font-family-mono: ui-monospace, "SFMono-Regular", Consolas, "Liberation Mono", monospace;
  --gojet-border-width-default: 1px;
  --gojet-focus-ring-width: 2px;
  --gojet-focus-ring-offset: 2px;
  --gojet-content-dialog-md: 640px;
  --gojet-control-height-lg: 40px;
  --gojet-icon-size-marketing: 24px;
  --gojet-radius-md: 8px;
  --gojet-radius-lg: 12px;
  --gojet-space-1: 4px;
  --gojet-space-2: 8px;
  --gojet-space-3: 12px;
  --gojet-space-4: 16px;
  --gojet-space-5: 20px;
  --gojet-space-6: 24px;
  --gojet-space-8: 32px;
  --gojet-type-body-desktop-size: 16px;
  --gojet-type-body-line-height: 1.65;
  --gojet-type-body-weight: 400;
  --gojet-type-label-size: 14px;
  --gojet-type-label-line-height: 1.4;
  --gojet-type-label-weight: 600;
  --gojet-type-meta-desktop-size: 14px;
  --gojet-type-meta-line-height: 1.5;
  --gojet-type-product-page-title-size: 24px;
  --gojet-type-product-page-title-line-height: 1.25;
  --gojet-type-product-page-title-weight: 650;
  --gojet-elevation-2: 0 8px 24px rgba(15,23,42,.08);
}
@media (prefers-color-scheme: dark) {
  :root {
    color-scheme: dark;
    --gojet-surface-canvas: #070B14;
    --gojet-surface-default: #0D1422;
    --gojet-surface-muted: #121B2C;
    --gojet-text-primary: #F8FAFC;
    --gojet-text-secondary: #CBD5E1;
    --gojet-text-muted: #94A3B8;
    --gojet-border-divider: #1E293B;
    --gojet-border-default: #94A3B8;
    --gojet-action-primary: #2563EB;
    --gojet-action-primary-hover: #1D4ED8;
    --gojet-action-on-primary: #FFFFFF;
    --gojet-focus-ring: #38BDF8;
    --gojet-focus-backplate: #0D1422;
    --gojet-status-warning-bg: #2B200B;
    --gojet-status-warning-border: #D97706;
    --gojet-status-warning-fg: #FCD34D;
    --gojet-status-danger-bg: #301419;
    --gojet-status-danger-border: #EF4444;
    --gojet-status-danger-fg: #FCA5A5;
    --gojet-status-info-bg: #0C2535;
    --gojet-status-info-border: #0EA5E9;
    --gojet-status-info-fg: #7DD3FC;
    --gojet-elevation-2: 0 8px 24px rgba(0,0,0,.32);
  }
}
`

const publicSurfaceCSS = publicSurfaceTokenDefinitions + `
* { box-sizing: border-box; }
html { min-height: 100%; background: var(--gojet-surface-canvas); }
body {
  min-height: 100vh;
  margin: 0;
  display: grid;
  place-items: center;
  padding: var(--gojet-space-6);
  background: var(--gojet-surface-canvas);
  color: var(--gojet-text-primary);
  font-family: var(--gojet-font-family-latin);
  font-size: var(--gojet-type-body-desktop-size);
  font-weight: var(--gojet-type-body-weight);
  line-height: var(--gojet-type-body-line-height);
}
main {
  width: min(100%, var(--gojet-content-dialog-md));
  background: var(--gojet-surface-default);
  border: var(--gojet-border-width-default) solid var(--gojet-border-divider);
  border-radius: var(--gojet-radius-lg);
  box-shadow: var(--gojet-elevation-2);
  padding: var(--gojet-space-8);
}
.gj-public-brand {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--gojet-space-4);
  margin-bottom: var(--gojet-space-6);
}
.gj-public-wordmark {
  font-size: var(--gojet-type-product-page-title-size);
  font-weight: var(--gojet-type-product-page-title-weight);
  line-height: var(--gojet-type-product-page-title-line-height);
  letter-spacing: -0.015em;
}
.gj-public-context {
  color: var(--gojet-text-muted);
  font-size: var(--gojet-type-meta-desktop-size);
  line-height: var(--gojet-type-meta-line-height);
}
.gj-public-state {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: var(--gojet-space-4);
  align-items: start;
  padding: var(--gojet-space-5);
  border: var(--gojet-border-width-default) solid var(--gojet-status-info-border);
  border-radius: var(--gojet-radius-md);
  background: var(--gojet-status-info-bg);
  color: var(--gojet-status-info-fg);
}
.gj-public-state[data-tone="warning"] {
  border-color: var(--gojet-status-warning-border);
  background: var(--gojet-status-warning-bg);
  color: var(--gojet-status-warning-fg);
}
.gj-public-state[data-tone="danger"] {
  border-color: var(--gojet-status-danger-border);
  background: var(--gojet-status-danger-bg);
  color: var(--gojet-status-danger-fg);
}
.gj-public-icon {
  width: var(--gojet-icon-size-marketing);
  height: var(--gojet-icon-size-marketing);
  margin-top: var(--gojet-space-1);
  flex: none;
}
.gj-public-state h1 {
  margin: 0;
  color: currentColor;
  font-size: var(--gojet-type-product-page-title-size);
  font-weight: var(--gojet-type-product-page-title-weight);
  line-height: var(--gojet-type-product-page-title-line-height);
}
.gj-public-state p { margin: var(--gojet-space-2) 0 0; }
.gj-public-next {
  margin: var(--gojet-space-5) 0 0;
  color: var(--gojet-text-secondary);
}
.gj-public-meta {
  margin-top: var(--gojet-space-5);
  padding-top: var(--gojet-space-4);
  border-top: var(--gojet-border-width-default) solid var(--gojet-border-divider);
  color: var(--gojet-text-muted);
  font-size: var(--gojet-type-meta-desktop-size);
  line-height: var(--gojet-type-meta-line-height);
}
.gj-public-meta code {
  color: var(--gojet-text-secondary);
  font-family: var(--gojet-font-family-mono);
}
.gj-public-form {
  display: grid;
  gap: var(--gojet-space-3);
  margin-top: var(--gojet-space-5);
}
.gj-public-form label {
  color: var(--gojet-text-primary);
  font-size: var(--gojet-type-label-size);
  font-weight: var(--gojet-type-label-weight);
  line-height: var(--gojet-type-label-line-height);
}
.gj-public-form input {
  width: 100%;
  min-height: var(--gojet-control-height-lg);
  padding: 0 var(--gojet-space-3);
  border: var(--gojet-border-width-default) solid var(--gojet-border-default);
  border-radius: var(--gojet-radius-md);
  background: var(--gojet-surface-default);
  color: var(--gojet-text-primary);
  font: inherit;
}
.gj-public-form button {
  min-height: var(--gojet-control-height-lg);
  justify-self: start;
  padding: 0 var(--gojet-space-4);
  border: var(--gojet-border-width-default) solid var(--gojet-action-primary);
  border-radius: var(--gojet-radius-md);
  background: var(--gojet-action-primary);
  color: var(--gojet-action-on-primary);
  font-family: inherit;
  font-size: var(--gojet-type-label-size);
  font-weight: var(--gojet-type-label-weight);
  cursor: pointer;
}
.gj-public-form button:hover { background: var(--gojet-action-primary-hover); border-color: var(--gojet-action-primary-hover); }
.gj-public-form input:focus-visible,
.gj-public-form button:focus-visible {
  outline: var(--gojet-focus-ring-width) solid var(--gojet-focus-ring);
  outline-offset: var(--gojet-focus-ring-offset);
  box-shadow: 0 0 0 var(--gojet-focus-ring-offset) var(--gojet-focus-backplate);
}
.gj-public-alert {
  margin-top: var(--gojet-space-4);
  padding: var(--gojet-space-3) var(--gojet-space-4);
  border: var(--gojet-border-width-default) solid var(--gojet-status-danger-border);
  border-radius: var(--gojet-radius-md);
  background: var(--gojet-status-danger-bg);
  color: var(--gojet-status-danger-fg);
}
`

const safetySurfaceTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>{{.Title}} · GoJet</title>
<style>` + publicSurfaceCSS + `</style>
</head>
<body>
<main data-gojet-public-surface="safety" data-safety-state="{{.Reason}}">
  <header class="gj-public-brand">
    <span class="gj-public-wordmark">GoJet</span>
    <span class="gj-public-context">Secure redirect</span>
  </header>
  <section class="gj-public-state" data-tone="{{if or (eq .Reason "review") (eq .Reason "pending") (eq .Reason "stale") (eq .Reason "malformed")}}warning{{else if or (eq .Reason "blocked") (eq .Reason "removed")}}danger{{else}}info{{end}}" aria-labelledby="gojet-public-title">
    {{if or (eq .Reason "review") (eq .Reason "pending") (eq .Reason "stale") (eq .Reason "malformed")}}
    <svg class="gj-public-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m21.73 18-8-14a2 2 0 0 0-3.46 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>
    {{else if or (eq .Reason "blocked") (eq .Reason "removed")}}
    <svg class="gj-public-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 13c0 5-3.5 7.5-8 9-4.5-1.5-8-4-8-9V5l8-3 8 3v8Z"/><path d="m9.5 9 5 5"/><path d="m14.5 9-5 5"/></svg>
    {{else}}
    <svg class="gj-public-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><path d="m4.93 4.93 14.14 14.14"/></svg>
    {{end}}
    <div>
      <h1 id="gojet-public-title">{{.Title}}</h1>
      <p>{{.Message}}</p>
    </div>
  </section>
  <p class="gj-public-next"><strong>Next step:</strong> {{if eq .Reason "review"}}Try again later after the destination review completes.{{else if eq .Reason "blocked"}}Contact the link owner if you believe this safety decision is unexpected.{{else}}Try again later or contact the link owner if this condition persists.{{end}}</p>
  <footer class="gj-public-meta">Reference: <code>{{.Code}}</code></footer>
</main>
</body>
</html>`

const passwordSurfaceTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>Password required · GoJet</title>
<style>` + publicSurfaceCSS + `</style>
</head>
<body>
<main data-gojet-public-surface="password">
  <header class="gj-public-brand">
    <span class="gj-public-wordmark">GoJet</span>
    <span class="gj-public-context">Protected link</span>
  </header>
  <section class="gj-public-state" aria-labelledby="gojet-public-title">
    <svg class="gj-public-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
    <div>
      <h1 id="gojet-public-title">Password required</h1>
      <p>This link is protected. Enter its password to continue.</p>
    </div>
  </section>
  {{if .Message}}<div class="gj-public-alert" role="alert">{{.Message}}</div>{{end}}
  <form class="gj-public-form" method="post" action="">
    <label for="gojet-link-password">Password</label>
    <input id="gojet-link-password" name="password" type="password" minlength="8" maxlength="256" autocomplete="current-password" required>
    <button type="submit">Continue</button>
  </form>
  <footer class="gj-public-meta">Reference: <code>{{.Code}}</code></footer>
</main>
</body>
</html>`

func init() {
	safetyTemplate = template.Must(template.New("link-safety").Parse(safetySurfaceTemplate))
	passwordTemplate = template.Must(template.New("link-password").Parse(passwordSurfaceTemplate))
}
