import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const baseUrl = process.env.P19_SITE_URL || 'http://127.0.0.1:4193';
const outDir = `${root}/artifacts/v10/P19/browser`;
const visualDir = `${root}/artifacts/v10/P19/visual`;
const capturesDir = `${root}/artifacts/v10/P19/captures`;
mkdirSync(outDir, { recursive: true });
mkdirSync(visualDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });

const implementationCommit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
const tokenData = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function viewport(name) {
  const value = String(tokenData[`viewport.${name}`].dimensions);
  const match = /^(\d+)×(\d+)$/.exec(value);
  if (!match) throw new Error(`invalid viewport token ${name}: ${value}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
const viewports = { desktop: viewport('desktop'), tablet: viewport('tablet'), mobile: viewport('mobile'), compact320: { width: 320, height: 800 } };
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('system Chrome/Chromium is required for P19 browser evidence');

const diagnostics = { console: [], page: [], request: [] };
function attach(page, label) {
  page.on('console', (message) => { if (message.type() === 'error') diagnostics.console.push({ label, text: message.text(), location: message.location() }); });
  page.on('pageerror', (error) => diagnostics.page.push({ label, text: String(error) }));
  page.on('requestfailed', (request) => diagnostics.request.push({ label, url: request.url(), failure: request.failure(), resourceType: request.resourceType() }));
}
function writeCase(targetDir, caseId, name, errors, details) {
  const payload = { node: 'P19', case: caseId, name, status: errors.length ? 'FAIL' : 'PASS', implementation_commit: implementationCommit, errors, details };
  writeFileSync(`${targetDir}/${caseId}.json`, JSON.stringify(payload, null, 2) + '\n');
  console.log(`${caseId}: ${payload.status}`);
  for (const error of errors) console.log(`  - ${error}`);
  return payload.status === 'PASS';
}
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });

async function t023() {
  const errors = [];
  const observations = [];
  const cases = [
    { label: 'home', path: '/', routeId: 'WEB-HOME' },
    { label: 'product', path: '/products/links', routeId: 'WEB-LINKS' },
    { label: 'solution', path: '/solutions/marketing', routeId: 'WEB-SOL-MARKETING' },
    { label: 'developers', path: '/developers', routeId: 'WEB-DEVELOPERS' },
    { label: 'pricing-unavailable', path: '/pricing?state=data-unavailable', routeId: 'WEB-PRICING', state: 'data-unavailable' },
    { label: 'security', path: '/security', routeId: 'WEB-SECURITY' },
    { label: 'guide', path: '/guides/secure-link-sharing', routeId: 'WEB-GUIDE' },
    { label: 'contact', path: '/contact', contact: true },
    { label: 'legal', path: '/legal/terms', routeId: 'WEB-LEGAL-TERMS' },
    { label: 'zh-home', path: '/zh-CN/', routeId: 'WEB-HOME' },
    { label: 'maintenance', path: '/?state=maintenance', routeId: 'WEB-HOME', state: 'maintenance' },
  ];
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1 });
  const page = await context.newPage(); attach(page, 'T023');
  for (const spec of cases) {
    const response = await page.goto(`${baseUrl}${spec.path}`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(100);
    const actual = await page.evaluate(() => ({
      routeId: document.querySelector('article.website-page')?.getAttribute('data-route-id') || null,
      state: document.querySelector('article.website-page')?.getAttribute('data-surface-state') || null,
      contactPage: document.querySelector('[data-page="contact"]')?.getAttribute('data-state') || null,
      contactForm: Boolean(document.querySelector('.contact-page form.contact-form')),
      contactSubmit: document.querySelector('.contact-page button[type="submit"]')?.textContent?.trim() || null,
      h1: document.querySelector('main h1')?.textContent?.trim() || '',
      navLinks: document.querySelectorAll('header nav a[href]').length,
      ctas: document.querySelectorAll('.website-hero-actions a[href]').length,
      statusText: document.querySelector('[role="status"]')?.textContent?.trim() || null,
      placeholder: /\b(?:TODO|Lorem ipsum|placeholder)\b/i.test(document.body.innerText),
    }));
    if (response?.status() !== 200) errors.push(`${spec.label}: expected HTTP 200, got ${response?.status()}`);
    if (!actual.h1) errors.push(`${spec.label}: missing H1`);
    if (actual.navLinks < 5) errors.push(`${spec.label}: primary navigation incomplete`);
    if (actual.placeholder) errors.push(`${spec.label}: placeholder copy detected`);
    if (spec.contact) {
      if (actual.contactPage !== 'input') errors.push(`contact: P14 ContactPage input state missing`);
      if (!actual.contactForm || actual.contactSubmit !== 'Send message') errors.push('contact: real contact conversion form/submit control missing');
    } else {
      if (actual.routeId !== spec.routeId) errors.push(`${spec.label}: route id ${actual.routeId} != ${spec.routeId}`);
      if (actual.ctas < 2) errors.push(`${spec.label}: conversion CTA set incomplete`);
      if (spec.state && (!actual.statusText || actual.state !== spec.state)) errors.push(`${spec.label}: persistent state ${spec.state} not rendered`);
    }
    observations.push({ label: spec.label, path: spec.path, status: response?.status(), ...actual });
  }
  for (const [path, expected] of [['/guides/legacy-deployment', 410], ['/definitely-not-a-gojet-route', 404]]) {
    const errorPage = await context.newPage();
    const response = await errorPage.goto(`${baseUrl}${path}`, { waitUntil: 'domcontentloaded' });
    if (response?.status() !== expected) errors.push(`${path}: expected ${expected}, got ${response?.status()}`);
    observations.push({ label: `http-${expected}`, path, status: response?.status() });
    await errorPage.close();
  }
  await context.close();
  return writeCase(outDir, 'P19-T023', 'Desktop browser route and conversion matrix', errors, { viewport: viewports.desktop, cases: observations });
}

async function inspectReflow(page) {
  return page.evaluate(() => {
    const visible = (node) => {
      const style = getComputedStyle(node); const rect = node.getBoundingClientRect();
      return style.display !== 'none' && style.visibility !== 'hidden' && Number(style.opacity) > 0 && rect.width > 0 && rect.height > 0;
    };
    const controls = [...document.querySelectorAll('a[href],button,input,select,textarea')].filter(visible);
    const clippedControls = controls.filter((node) => {
      const rect = node.getBoundingClientRect();
      return rect.left < -1 || rect.right > window.innerWidth + 1 || node.scrollWidth > node.clientWidth + 1;
    }).map((node) => (node.textContent || node.getAttribute('aria-label') || node.tagName).trim());
    const overlaps = [];
    for (let i = 0; i < controls.length; i += 1) for (let j = i + 1; j < controls.length; j += 1) {
      const a = controls[i].getBoundingClientRect(); const b = controls[j].getBoundingClientRect();
      const x = Math.min(a.right, b.right) - Math.max(a.left, b.left); const y = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
      if (x > 2 && y > 2 && !controls[i].contains(controls[j]) && !controls[j].contains(controls[i])) overlaps.push([controls[i].textContent?.trim(), controls[j].textContent?.trim()]);
    }
    return {
      viewport: { width: innerWidth, height: innerHeight },
      rootOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
      bodyOverflow: document.body.scrollWidth > document.body.clientWidth + 1,
      clippedControls,
      overlaps: overlaps.slice(0, 20),
      visibleActionCount: controls.length,
    };
  });
}
async function t024() {
  const errors = []; const matrix = [];
  const routes = ['/', '/products', '/pricing?state=data-unavailable', '/zh-CN/'];
  for (const [viewportName, size] of Object.entries(viewports)) {
    const context = await browser.newContext({ viewport: size, deviceScaleFactor: 1 });
    const page = await context.newPage(); attach(page, `T024-${viewportName}`);
    for (const path of routes) {
      await page.goto(`${baseUrl}${path}`, { waitUntil: 'networkidle' }); await page.waitForTimeout(80);
      const before = await inspectReflow(page);
      await page.mouse.move(Math.floor(size.width / 2), Math.floor(size.height / 2)); await page.waitForTimeout(60);
      const after = await inspectReflow(page);
      if (before.rootOverflow || before.bodyOverflow) errors.push(`${viewportName} ${path}: horizontal document overflow`);
      if (before.clippedControls.length) errors.push(`${viewportName} ${path}: clipped controls ${before.clippedControls.join(', ')}`);
      if (before.overlaps.length) errors.push(`${viewportName} ${path}: overlapping interactive controls ${JSON.stringify(before.overlaps.slice(0, 3))}`);
      if (after.visibleActionCount > before.visibleActionCount) errors.push(`${viewportName} ${path}: hover-only action surfaced`);
      matrix.push({ viewportName, path, before, afterVisibleActionCount: after.visibleActionCount });
    }
    await context.close();
  }
  return writeCase(outDir, 'P19-T024', 'Responsive and 320 CSS px reflow matrix', errors, { matrix });
}

async function t025() {
  const errors = []; const checks = [];
  const context = await browser.newContext({ viewport: viewports.desktop, deviceScaleFactor: 1, reducedMotion: 'reduce' });
  const page = await context.newPage(); attach(page, 'T025');
  for (const path of ['/', '/zh-CN/', '/pricing?state=data-unavailable']) {
    await page.goto(`${baseUrl}${path}`, { waitUntil: 'networkidle' }); await page.waitForTimeout(80);
    const result = await page.evaluate(() => {
      const named = [...document.querySelectorAll('a[href],button,input,select,textarea')].filter((node) => {
        const style = getComputedStyle(node); const rect = node.getBoundingClientRect(); return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
      }).filter((node) => ((node.getAttribute('aria-label') || node.textContent || node.getAttribute('title') || '').trim().length === 0)).length;
      const badImages = [...document.images].filter((img) => !img.hasAttribute('alt') || !img.complete || img.naturalWidth === 0).length;
      const animated = [...document.querySelectorAll('.website-page *')].filter((node) => {
        const s = getComputedStyle(node); return (s.animationName !== 'none' && s.animationDuration !== '0s') || (s.transitionDuration.split(',').some((v) => parseFloat(v) > 0));
      }).length;
      return {
        h1Count: document.querySelectorAll('main h1').length,
        landmarks: { header: document.querySelectorAll('header').length, nav: document.querySelectorAll('nav').length, main: document.querySelectorAll('main').length, footer: document.querySelectorAll('footer').length },
        unnamedInteractive: named,
        badImages,
        reducedMotionActiveAnimations: animated,
        statusLive: document.querySelector('[role="status"]')?.textContent?.trim() || null,
      };
    });
    if (result.h1Count !== 1) errors.push(`${path}: expected one H1, got ${result.h1Count}`);
    if (!result.landmarks.header || !result.landmarks.nav || !result.landmarks.main || !result.landmarks.footer) errors.push(`${path}: landmark set incomplete`);
    if (result.unnamedInteractive) errors.push(`${path}: ${result.unnamedInteractive} unnamed interactive controls`);
    if (result.badImages) errors.push(`${path}: ${result.badImages} images missing usable alternatives/assets`);
    if (result.reducedMotionActiveAnimations) errors.push(`${path}: reduced-motion still has ${result.reducedMotionActiveAnimations} active transitions/animations`);
    if (path.includes('state=') && !result.statusLive) errors.push(`${path}: persistent status missing`);
    checks.push({ path, ...result });
  }
  await page.goto(`${baseUrl}/`, { waitUntil: 'networkidle' });
  let focus = null;
  for (let i = 0; i < 8; i += 1) {
    await page.keyboard.press('Tab');
    focus = await page.evaluate(() => {
      const node = document.activeElement; if (!node) return null; const s = getComputedStyle(node);
      return { tag: node.tagName, text: (node.textContent || node.getAttribute('aria-label') || '').trim(), outlineStyle: s.outlineStyle, outlineWidth: s.outlineWidth, boxShadow: s.boxShadow };
    });
    if (focus && focus.tag !== 'BODY') break;
  }
  const visibleFocus = focus && ((focus.outlineStyle !== 'none' && parseFloat(focus.outlineWidth) > 0) || focus.boxShadow !== 'none');
  if (!visibleFocus) errors.push(`keyboard focus is not visibly styled: ${JSON.stringify(focus)}`);
  await context.close();
  return writeCase(outDir, 'P19-T025', 'Accessibility semantic and interaction matrix', errors, { checks, keyboardFocus: focus, visibleFocus });
}

async function t026() {
  const errors = []; const captures = [];
  const websiteCss = readFileSync(`${root}/frontend/apps/site/src/website/website.css`, 'utf8');
  const shellCss = readFileSync(`${root}/frontend/apps/site/src/shell/shell.css`, 'utf8');
  if (!websiteCss.includes('var(--gojet-') || !shellCss.includes('var(--gojet-')) errors.push('Website shell/styles are not Design System token bound');
  if (/#[0-9a-fA-F]{3,8}\b/.test(websiteCss) || /\b\d+(?:\.\d+)?px\b/.test(websiteCss)) errors.push('Website CSS contains raw governed visual values');
  const matrix = [
    ['home-en', '/', 'desktop'], ['home-zh', '/zh-CN/', 'mobile'], ['product', '/products/links', 'desktop'],
    ['solution', '/solutions/marketing', 'tablet'], ['pricing-state', '/pricing?state=data-unavailable', 'mobile'], ['security', '/security', 'desktop'], ['guide', '/guides/secure-link-sharing', 'tablet'],
  ];
  for (const [label, path, viewportName] of matrix) {
    const size = viewports[viewportName]; const context = await browser.newContext({ viewport: size, deviceScaleFactor: 1, reducedMotion: 'reduce' });
    const page = await context.newPage(); attach(page, `T026-${label}`); await page.goto(`${baseUrl}${path}`, { waitUntil: 'networkidle' }); await page.waitForTimeout(100);
    const dom = await page.evaluate(() => ({
      brokenImages: [...document.images].filter((img) => !img.complete || img.naturalWidth === 0).length,
      placeholderIcons: document.querySelectorAll('[data-placeholder], .placeholder, img[src*="placeholder"]').length,
      suspiciousCopy: /\b(?:TODO|Lorem ipsum|placeholder)\b/i.test(document.body.innerText),
      routeId: document.querySelector('article.website-page')?.getAttribute('data-route-id') || null,
    }));
    if (dom.brokenImages || dom.placeholderIcons || dom.suspiciousCopy) errors.push(`${label}: placeholder/broken visual artifact detected`);
    const file = `gjv10__website__p19__${label}__light__${viewportName}.png`; await page.screenshot({ path: `${capturesDir}/${file}`, fullPage: true });
    captures.push({ label, path, viewport: viewportName, dimensions: size, routeId: dom.routeId, file: `artifacts/v10/P19/captures/${file}` }); await context.close();
  }
  const visualDiagnostics = {
    console: diagnostics.console.filter((item) => item.label.startsWith('T026-')),
    page: diagnostics.page.filter((item) => item.label.startsWith('T026-')),
    request: diagnostics.request.filter((item) => item.label.startsWith('T026-')),
  };
  const diagnosticErrors = visualDiagnostics.console.length + visualDiagnostics.page.length + visualDiagnostics.request.length;
  if (diagnosticErrors) errors.push(`T026 screenshot matrix contains ${diagnosticErrors} console/page/request failures`);
  return writeCase(visualDir, 'P19-T026', 'Design System visual conformance', errors, { captures, diagnostics: visualDiagnostics, designTokenBound: true });
}

const pass = [await t023(), await t024(), await t025(), await t026()].every(Boolean);
await browser.close();
process.exit(pass ? 0 : 1);
