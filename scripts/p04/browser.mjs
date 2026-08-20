import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const root = process.cwd();
const outDir = `${root}/artifacts/v10/P04/browser`;
const capturesDir = `${root}/artifacts/v10/P04/captures`;
mkdirSync(outDir, { recursive: true });
mkdirSync(capturesDir, { recursive: true });
const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
const parseViewport = (value) => { const [width, height] = String(value).split('×').map(Number); return { width, height }; };
const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions),
  tablet: parseViewport(variables['viewport.tablet'].dimensions),
  mobile: parseViewport(variables['viewport.mobile'].dimensions),
};
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P04 evidence');
const targets = [
  { surface: 'website', url: 'http://127.0.0.1:4173/', state: 'normal' },
  { surface: 'auth', url: 'http://127.0.0.1:4173/login', state: 'normal' },
  { surface: 'docs', url: 'http://127.0.0.1:4176/docs/en/', state: 'article' },
  { surface: 'workspace', url: 'http://127.0.0.1:4174/app', state: 'notification-attention' },
  { surface: 'admin', url: 'http://127.0.0.1:4175/admin', state: 'normal' },
  { surface: 'installer', url: 'http://127.0.0.1:4177/', state: 'session-ready' },
];
const report = { generated_at: new Date().toISOString(), chrome: executablePath, targets: [], route_transitions: [], overlay: null, console_errors: [], page_errors: [], captures: [] };
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
const installLayoutObserver = async (context) => context.addInitScript(() => {
  window.__gojetP04LayoutShift = 0;
  new PerformanceObserver((list) => {
    for (const entry of list.getEntries()) if (!entry.hadRecentInput) window.__gojetP04LayoutShift += entry.value;
  }).observe({ type: 'layout-shift', buffered: true });
});

async function inspectTarget(target, viewportName, viewport) {
  const context = await browser.newContext({ viewportSize: viewport });
  await installLayoutObserver(context);
  const page = await context.newPage();
  page.on('console', (message) => { if (message.type() === 'error') report.console_errors.push({ surface: target.surface, text: message.text() }); });
  page.on('pageerror', (error) => report.page_errors.push({ surface: target.surface, text: String(error) }));
  await page.goto(target.url, { waitUntil: 'networkidle' });
  const metrics = await page.evaluate(() => ({
    rootOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    bodyOverflow: document.body.scrollWidth > document.body.clientWidth,
    layoutShift: window.__gojetP04LayoutShift || 0,
    title: document.title,
    anchors: [...document.querySelectorAll('a[href]')].map((node) => node.getAttribute('href')).filter(Boolean),
    clippedText: [...document.querySelectorAll('nav a, header a, header button, main h1, main h2')].filter((node) => node.clientWidth > 0 && node.scrollWidth > node.clientWidth).map((node) => node.textContent?.trim()).filter(Boolean),
  }));
  const file = `gjv10__${target.surface}__p04-shell__${target.state}__light__en__${viewportName}.png`;
  await page.screenshot({ path: `${capturesDir}/${file}`, fullPage: false });
  report.captures.push({ surface: target.surface, viewport: viewportName, path: `artifacts/v10/P04/captures/${file}` });
  report.targets.push({ ...target, viewport: viewportName, ...metrics });
  await context.close();
}
for (const target of targets) for (const [viewportName, viewport] of Object.entries(viewports)) await inspectTarget(target, viewportName, viewport);

async function verifySpa(url, selector, expectedPath) {
  const context = await browser.newContext({ viewportSize: viewports.desktop });
  await installLayoutObserver(context);
  const page = await context.newPage();
  await page.goto(url, { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.__gojetP04LayoutShift = 0; });
  const marker = `p04-${Date.now()}-${Math.random()}`;
  await page.evaluate((value) => { window.__gojetP04Marker = value; }, marker);
  await page.locator(selector).click();
  await page.waitForURL((next) => next.pathname === expectedPath);
  const result = await page.evaluate((value) => ({ preserved: window.__gojetP04Marker === value, layoutShift: window.__gojetP04LayoutShift || 0 }), marker);
  report.route_transitions.push({ url, selector, expectedPath, ...result, finalUrl: page.url() });
  await context.close();
}
await verifySpa('http://127.0.0.1:4173/', 'a[href="/pricing"]', '/pricing');
await verifySpa('http://127.0.0.1:4174/app', 'a[href="/app/settings"]', '/app/settings');
await verifySpa('http://127.0.0.1:4175/admin', 'a[href="/admin/operations"]', '/admin/operations');

{
  const context = await browser.newContext({ viewportSize: viewports.desktop });
  const page = await context.newPage();
  await page.goto('http://127.0.0.1:4174/app', { waitUntil: 'networkidle' });
  const trigger = page.getByRole('button', { name: 'Command' });
  await trigger.focus(); await trigger.click();
  const dialogCount = await page.locator('dialog[open]').count();
  await page.keyboard.press('Escape'); await page.waitForTimeout(80);
  const dialogAfterEscape = await page.locator('dialog[open]').count();
  const focusReturned = await trigger.evaluate((node) => document.activeElement === node);
  report.overlay = { dialogCount, dialogAfterEscape, focusReturned };
  await context.close();
}
{
  const context = await browser.newContext({ viewportSize: viewports.mobile, locale: 'zh-CN' });
  const page = await context.newPage();
  await page.goto('http://127.0.0.1:4176/docs/zh-CN/', { waitUntil: 'networkidle' });
  const file = 'gjv10__docs__p04-shell__article__light__zh-cn__mobile.png';
  await page.screenshot({ path: `${capturesDir}/${file}`, fullPage: false });
  report.captures.push({ surface: 'docs', viewport: 'mobile', locale: 'zh-cn', path: `artifacts/v10/P04/captures/${file}` });
  await context.close();
}
await browser.close();
writeFileSync(`${outDir}/browser-report.json`, JSON.stringify(report, null, 2) + '\n');
console.log(`P04 browser evidence: ${report.captures.length} captures, ${report.route_transitions.length} SPA transitions`);
