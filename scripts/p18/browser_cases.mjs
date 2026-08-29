import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const caseId = process.argv[process.argv.indexOf('--case') + 1];
const supported = new Set(['P18-T011', 'P18-T019', 'P18-T020', 'P18-T021']);
if (!supported.has(caseId)) throw new Error(`unsupported case: ${caseId}`);
const base = (process.env.P18_HTTP_BASE || 'http://127.0.0.1:8098').replace(/\/$/, '');
const baseOrigin = new URL(base).origin;
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P18 browser evidence');

const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
function parseViewport(value, name) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`invalid canonical viewport ${name}: ${String(value)}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
const canonicalViewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'viewport.desktop'),
  tablet: parseViewport(variables['viewport.tablet'].dimensions, 'viewport.tablet'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'viewport.mobile'),
  reflow320: { width: 320, height: 800 },
};

const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });

function attachDiagnostics(page) {
  const diagnostics = { console_errors: [], page_errors: [], external_requests: [], request_failures: [] };
  page.on('console', (message) => {
    if (message.type() === 'error') diagnostics.console_errors.push(message.text());
  });
  page.on('pageerror', (error) => diagnostics.page_errors.push(String(error)));
  page.on('request', (request) => {
    try {
      if (new URL(request.url()).origin !== baseOrigin) diagnostics.external_requests.push(request.url());
    } catch {
      diagnostics.external_requests.push(request.url());
    }
  });
  page.on('requestfailed', (request) => diagnostics.request_failures.push({ url: request.url(), failure: request.failure() }));
  return diagnostics;
}

function assertCleanDiagnostics(diagnostics, label, { allowRequestFailures = false } = {}) {
  if (diagnostics.console_errors.length) throw new Error(`${label}: console errors: ${diagnostics.console_errors.join(' | ')}`);
  if (diagnostics.page_errors.length) throw new Error(`${label}: page errors: ${diagnostics.page_errors.join(' | ')}`);
  if (diagnostics.external_requests.length) throw new Error(`${label}: external requests: ${diagnostics.external_requests.join(' | ')}`);
  if (!allowRequestFailures && diagnostics.request_failures.length) throw new Error(`${label}: request failures: ${JSON.stringify(diagnostics.request_failures)}`);
}

async function openSearchWithKeyboard(page) {
  const search = page.locator('site-search').first();
  const openButton = search.locator('button[data-open-modal]').first();
  await openButton.waitFor({ state: 'visible' });
  await page.waitForFunction(() => {
    const button = document.querySelector('site-search button[data-open-modal]');
    return button instanceof HTMLButtonElement && !button.disabled && Boolean(customElements.get('site-search'));
  }, null, { timeout: 12000 });
  const documentedShortcut = await openButton.getAttribute('aria-keyshortcuts');
  if (!documentedShortcut || !/^(?:Control|Meta)\+K$/.test(documentedShortcut)) {
    throw new Error(`Starlight search exposed an unexpected documented shortcut: ${String(documentedShortcut)}`);
  }
  const modifier = documentedShortcut.startsWith('Meta+') ? 'Meta' : 'Control';
  await page.keyboard.press(`${modifier}+k`);
  const dialog = search.locator('dialog[open]').first();
  await dialog.waitFor({ state: 'visible', timeout: 5000 });
  return { dialog, documentedShortcut, openButton };
}

async function caseT011() {
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();
  const diagnostics = attachDiagnostics(page);
  await page.goto(`${base}/docs/en/`, { waitUntil: 'networkidle' });
  const before = await page.evaluate(() => document.activeElement?.outerHTML || '');
  const { dialog, documentedShortcut, openButton } = await openSearchWithKeyboard(page);
  const input = dialog.locator('input[type="search"], input').first();
  await input.waitFor({ state: 'visible', timeout: 12000 });
  await input.fill('API keys');
  const choices = dialog.locator('a[href], [role="option"]');
  await choices.first().waitFor({ state: 'visible', timeout: 12000 });
  const choiceCount = await choices.count();
  if (choiceCount < 1) throw new Error('search produced no keyboard choices');
  await page.keyboard.press('ArrowDown');
  const activeAfterArrow = await page.evaluate(() => document.activeElement?.tagName || '');
  await page.keyboard.press('Escape');
  await dialog.waitFor({ state: 'hidden', timeout: 5000 });
  const focusAfterEscape = await page.evaluate(() => ({
    tag: document.activeElement?.tagName || '',
    text: document.activeElement?.textContent?.trim() || '',
    aria: document.activeElement?.getAttribute('aria-label') || '',
  }));
  const triggerFocused = await openButton.evaluate((node) => document.activeElement === node);
  if (!triggerFocused) throw new Error(`Escape did not return focus to the search trigger: ${JSON.stringify(focusAfterEscape)}`);
  assertCleanDiagnostics(diagnostics, 'P18-T011');
  await context.close();
  return {
    shortcut: documentedShortcut,
    dialog_opened: true,
    choice_count: choiceCount,
    arrow_navigation_active_tag: activeAfterArrow,
    escape_closed: true,
    focus_return: focusAfterEscape,
    trigger_focus_returned: triggerFocused,
    external_requests: diagnostics.external_requests,
    before_focus: before,
  };
}

async function caseT019() {
  const css = readFileSync(`${root}/frontend/apps/docs/src/styles/docs-shell.css`, 'utf8');
  if (!css.includes("@import '@gojet/tokens/tokens.css'")) throw new Error('Docs shell no longer inherits GoJet Design System tokens');
  if (/#[0-9a-f]{3,8}\b/i.test(css)) throw new Error('Docs shell introduced raw color values instead of inherited Design System tokens');
  const searchSource = readFileSync(`${root}/frontend/apps/docs/src/components/SearchRoute.astro`, 'utf8');
  const nginx = readFileSync(`${root}/deploy/nginx/docs-p18.conf`, 'utf8');
  if (!searchSource.includes('offline-static')) throw new Error('offline-static boundary missing');
  if (!nginx.includes('=404')) throw new Error('not-found HTTP boundary missing');

  const context = await browser.newContext({ viewport: canonicalViewports.desktop });
  const page = await context.newPage();
  const diagnostics = attachDiagnostics(page);
  const response = await page.goto(`${base}/docs/en/getting-started`, { waitUntil: 'networkidle' });
  if (response?.status() !== 200) throw new Error(`article state returned ${response?.status()}`);
  const shell = await page.evaluate(() => ({
    h1: document.querySelectorAll('main h1').length,
    main: Boolean(document.querySelector('main')),
    nav: document.querySelectorAll('nav').length,
    header: Boolean(document.querySelector('header')),
  }));
  if (!shell.main || !shell.header || shell.h1 !== 1 || shell.nav < 1) throw new Error(`inherited P04 article shell missing: ${JSON.stringify(shell)}`);
  const { dialog } = await openSearchWithKeyboard(page);
  await page.keyboard.press('Escape');
  await dialog.waitFor({ state: 'hidden', timeout: 5000 });
  assertCleanDiagnostics(diagnostics, 'P18-T019/article');
  await context.close();

  const mobileContext = await browser.newContext({ viewport: canonicalViewports.mobile });
  const mobile = await mobileContext.newPage();
  const mobileDiagnostics = attachDiagnostics(mobile);
  await mobile.goto(`${base}/docs/en/getting-started`, { waitUntil: 'networkidle' });
  const drawerTrigger = mobile.locator('header button[aria-expanded]').first();
  if (!(await drawerTrigger.count())) throw new Error('mobile navigation drawer trigger is missing');
  await drawerTrigger.click();
  const expanded = await drawerTrigger.getAttribute('aria-expanded');
  if (expanded !== 'true') throw new Error(`mobile navigation drawer did not enter expanded state: ${String(expanded)}`);
  await drawerTrigger.click();
  assertCleanDiagnostics(mobileDiagnostics, 'P18-T019/nav-drawer');
  await mobileContext.close();

  const notFoundContext = await browser.newContext({ viewport: canonicalViewports.desktop });
  const notFound = await notFoundContext.newPage();
  const notFoundResponse = await notFound.goto(`${base}/docs/en/p18-not-found-fixture`, { waitUntil: 'domcontentloaded' });
  if (notFoundResponse?.status() !== 404) throw new Error(`not-found boundary returned ${notFoundResponse?.status()}`);
  await notFoundContext.close();

  const offlineContext = await browser.newContext({ viewport: canonicalViewports.desktop });
  await offlineContext.route('**/docs/pagefind/**', (route) => route.abort());
  const offline = await offlineContext.newPage();
  const offlineDiagnostics = attachDiagnostics(offline);
  await offline.goto(`${base}/docs/en/search?q=GoJet`, { waitUntil: 'domcontentloaded' });
  await offline.waitForFunction(() => document.querySelector('[data-gojet-search]')?.getAttribute('data-state') === 'offline-static', null, { timeout: 12000 });
  const offlineState = await offline.locator('[data-gojet-search]').getAttribute('data-state');
  assertCleanDiagnostics(offlineDiagnostics, 'P18-T019/offline-static', { allowRequestFailures: true });
  await offlineContext.close();

  return {
    inherited_p04_shell: true,
    design_system_tokens_preserved: true,
    article_state: shell,
    search_open_state: true,
    nav_drawer_state: 'expanded-and-closed',
    not_found_status: 404,
    offline_static_state: offlineState,
  };
}

async function caseT020() {
  const context = await browser.newContext({ viewport: canonicalViewports.desktop, reducedMotion: 'reduce' });
  const page = await context.newPage();
  const diagnostics = attachDiagnostics(page);
  await page.goto(`${base}/docs/en/api/api-keys`, { waitUntil: 'networkidle' });

  const semantics = await page.evaluate(() => {
    const visible = (node) => {
      const style = getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      return style.visibility !== 'hidden' && style.display !== 'none' && rect.width > 0 && rect.height > 0;
    };
    const controls = [...document.querySelectorAll('header a[href], header button, header select, main button, main a[href]')].filter(visible);
    const unnamed = controls.filter((node) => {
      const name = (node.getAttribute('aria-label') || node.getAttribute('title') || node.textContent || '').trim();
      return !name;
    });
    return {
      h1: document.querySelectorAll('main h1').length,
      heading_count: document.querySelectorAll('main h1, main h2, main h3').length,
      interactive_count: controls.length,
      unnamed_count: unnamed.length,
      code_blocks: document.querySelectorAll('main pre').length,
      reduced_motion: matchMedia('(prefers-reduced-motion: reduce)').matches,
    };
  });
  if (semantics.h1 !== 1 || semantics.heading_count < 1) throw new Error(`heading semantics failed: ${JSON.stringify(semantics)}`);
  if (semantics.interactive_count < 3 || semantics.unnamed_count !== 0) throw new Error(`name/role/value surface failed: ${JSON.stringify(semantics)}`);
  if (!semantics.reduced_motion) throw new Error('reduced-motion media preference was not honored by browser surface');

  await page.keyboard.press('Tab');
  const focus = await page.evaluate(() => {
    const node = document.activeElement;
    if (!node || node === document.body) return { valid: false };
    const style = getComputedStyle(node);
    return {
      valid: true,
      tag: node.tagName,
      name: (node.getAttribute('aria-label') || node.getAttribute('title') || node.textContent || '').trim().slice(0, 120),
      outlineStyle: style.outlineStyle,
      outlineWidth: style.outlineWidth,
      boxShadow: style.boxShadow,
    };
  });
  if (!focus.valid) throw new Error('keyboard focus did not enter an interactive control');
  const hasVisibleFocus = focus.outlineStyle !== 'none' && focus.outlineWidth !== '0px' || (focus.boxShadow && focus.boxShadow !== 'none');
  if (!hasVisibleFocus) throw new Error(`visible focus indicator missing: ${JSON.stringify(focus)}`);

  const { dialog } = await openSearchWithKeyboard(page);
  const searchInput = dialog.locator('input[type="search"], input').first();
  await searchInput.waitFor({ state: 'visible', timeout: 12000 });
  await page.keyboard.press('Escape');

  const languagePresent = await page.locator('starlight-lang-select, [data-language-select], select[aria-label*="language" i], button[aria-label*="language" i]').count();
  const themePresent = await page.locator('starlight-theme-select, [data-theme-toggle], select[aria-label*="theme" i], button[aria-label*="theme" i]').count();
  if (languagePresent < 1) throw new Error('language control is not exposed in the Docs shell');
  if (themePresent < 1) throw new Error('theme control is not exposed in the Docs shell');

  const codeControls = await page.evaluate(() => {
    const blocks = [...document.querySelectorAll('main pre')];
    const buttons = [...document.querySelectorAll('main pre button, main .expressive-code button')];
    const unnamed = buttons.filter((node) => !(node.getAttribute('aria-label') || node.getAttribute('title') || node.textContent || '').trim());
    return { blocks: blocks.length, buttons: buttons.length, unnamed: unnamed.length };
  });
  if (codeControls.buttons > 0 && codeControls.unnamed > 0) throw new Error(`unnamed code control: ${JSON.stringify(codeControls)}`);

  await page.setViewportSize({ width: 720, height: 900 });
  await page.waitForTimeout(100);
  const zoomEquivalent = await page.evaluate(() => ({
    rootOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    mainVisible: Boolean(document.querySelector('main h1')),
  }));
  if (zoomEquivalent.rootOverflow || !zoomEquivalent.mainVisible) throw new Error(`200% zoom-equivalent reflow failed: ${JSON.stringify(zoomEquivalent)}`);
  assertCleanDiagnostics(diagnostics, 'P18-T020');
  await context.close();

  return {
    keyboard: true,
    name_role_value: semantics,
    visible_focus: focus,
    language_control: true,
    theme_control: true,
    code_controls: codeControls,
    reduced_motion: true,
    zoom_200_equivalent: zoomEquivalent,
  };
}

async function caseT021() {
  const rows = [];
  for (const locale of ['en', 'zh-CN']) {
    for (const [viewportName, viewport] of Object.entries(canonicalViewports)) {
      const context = await browser.newContext({ viewport, locale: locale === 'zh-CN' ? 'zh-CN' : 'en-US' });
      const page = await context.newPage();
      const diagnostics = attachDiagnostics(page);
      const response = await page.goto(`${base}/docs/${locale}/getting-started`, { waitUntil: 'networkidle' });
      const metrics = await page.evaluate(() => ({
        width: innerWidth,
        height: innerHeight,
        rootOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
        bodyOverflow: document.body.scrollWidth > document.body.clientWidth,
        mainVisible: Boolean(document.querySelector('main h1')),
        clippedPrimaryText: [...document.querySelectorAll('header a, header button, main h1, main h2')]
          .filter((node) => node.getBoundingClientRect().width > 0 && node.scrollWidth > node.clientWidth + 1)
          .map((node) => node.textContent?.trim()).filter(Boolean),
      }));
      if (response?.status() !== 200) throw new Error(`${locale}/${viewportName}: HTTP ${response?.status()}`);
      if (metrics.width !== viewport.width || metrics.height !== viewport.height) throw new Error(`${locale}/${viewportName}: viewport mismatch ${JSON.stringify(metrics)}`);
      if (metrics.rootOverflow || metrics.bodyOverflow || !metrics.mainVisible) throw new Error(`${locale}/${viewportName}: reflow failure ${JSON.stringify(metrics)}`);
      if (metrics.clippedPrimaryText.length) throw new Error(`${locale}/${viewportName}: clipped primary text ${JSON.stringify(metrics.clippedPrimaryText)}`);
      assertCleanDiagnostics(diagnostics, `P18-T021/${locale}/${viewportName}`);
      rows.push({ locale, viewport: viewportName, dimensions: viewport, ...metrics, status: 200 });
      await context.close();
    }
  }
  return { canonical_viewports: canonicalViewports, matrix: rows, matrix_count: rows.length };
}

let details;
try {
  if (caseId === 'P18-T011') details = await caseT011();
  else if (caseId === 'P18-T019') details = await caseT019();
  else if (caseId === 'P18-T020') details = await caseT020();
  else details = await caseT021();
} finally {
  await browser.close();
}

const payload = {
  case: caseId,
  status: 'PASS',
  implementation_commit: process.env.GITHUB_SHA || null,
  ...details,
};
mkdirSync(`${root}/artifacts/v10/P18/browser`, { recursive: true });
writeFileSync(`${root}/artifacts/v10/P18/browser/${caseId}.json`, JSON.stringify(payload, null, 2) + '\n');
process.stdout.write(JSON.stringify(payload, null, 2) + '\n');
