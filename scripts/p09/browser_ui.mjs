import { writeFileSync } from 'node:fs';
import { HEAD, executablePath, WORKSPACE_URL, ADMIN_URL, INSTALL_URL, PLATFORM_URL, MYSQL_HOST, MYSQL_PORT, MYSQL_DATABASE, REAL_CLAMD, viewports, browserDir, assert } from './browser_env.mjs';

export function diagnostics() {
  return { console_errors: [], page_errors: [], http_errors: [], request_failures: [] };
}
export function attachDiagnostics(page, report) {
  page.on('console', (message) => {
    if (message.type() === 'error') report.console_errors.push({ text: message.text(), location: message.location() });
  });
  page.on('pageerror', (error) => report.page_errors.push(String(error)));
  page.on('response', (response) => {
    if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) {
      report.http_errors.push({ status: response.status(), url: response.url() });
    }
  });
  page.on('requestfailed', (request) => report.request_failures.push({ url: request.url(), failure: request.failure() }));
}
export function allowedMatch(entry, rules) {
  return rules.some((rule) => entry.url.includes(rule.includes) && (rule.status === undefined || entry.status === rule.status));
}
export function assertDiagnostics(report, label, { allowedHttp = [] } = {}) {
  const failures = report.request_failures.filter((entry) => !entry.url.endsWith('/favicon.ico'));
  const httpErrors = report.http_errors.filter((entry) => !allowedMatch(entry, allowedHttp));
  const consoleErrors = report.console_errors.filter((entry) => {
    const url = entry.location?.url ?? '';
    return !allowedHttp.some((rule) => url.includes(rule.includes));
  });
  assert(consoleErrors.length === 0, `${label} console errors: ${JSON.stringify(consoleErrors)}`);
  assert(report.page_errors.length === 0, `${label} page errors: ${JSON.stringify(report.page_errors)}`);
  assert(httpErrors.length === 0, `${label} HTTP errors: ${JSON.stringify(httpErrors)}`);
  assert(failures.length === 0, `${label} request failures: ${JSON.stringify(failures)}`);
}
export function writeResult(caseId, status, details, errors = []) {
  const payload = {
    node: 'P09',
    case_id: caseId,
    status,
    generated_at: new Date().toISOString(),
    implementation_commit: HEAD,
    environment: {
      browser: executablePath,
      workspace: WORKSPACE_URL,
      admin: ADMIN_URL,
      installer: INSTALL_URL,
      platformapi: PLATFORM_URL,
      mysql: `${MYSQL_HOST}:${MYSQL_PORT}/${MYSQL_DATABASE}`,
      real_clamd: REAL_CLAMD,
      canonical_viewports: viewports,
      authority: 'real built Workspace/Admin + native Go platformapi/fileworker/filepreflight + PHP Installer + real MySQL/local storage/clamd; no request interception or static browser fixture',
    },
    details,
    errors,
  };
  writeFileSync(`${browserDir}/${caseId}.json`, `${JSON.stringify(payload, null, 2)}\n`);
}
export async function newPage(browser, viewport, options = {}) {
  const context = await browser.newContext({ viewport, deviceScaleFactor: 1, acceptDownloads: true, ...options });
  const page = await context.newPage();
  return { context, page };
}
export async function gotoWorkspace(page, path, waitUntil = 'networkidle') {
  await page.goto(`${WORKSPACE_URL}${path}`, { waitUntil });
}
export async function waitPageState(page, selector, state) {
  await page.locator(selector).waitFor();
  await page.waitForFunction(
    ([target, expected]) => document.querySelector(target)?.getAttribute('data-state') === expected,
    [selector, state],
  );
}
export async function layoutEvidence(page) {
  return page.evaluate(() => {
    const visible = (node) => node instanceof HTMLElement && node.offsetParent !== null;
    const clipped = [...document.querySelectorAll('main h1, main h2, main h3, main button, main a, main label, main strong, main dd, main code')]
      .filter(visible)
      .filter((node) => node.clientWidth > 0 && node.scrollWidth > node.clientWidth + 1)
      .map((node) => ({ tag: node.tagName, text: node.textContent?.trim().slice(0, 120) ?? '', clientWidth: node.clientWidth, scrollWidth: node.scrollWidth }));
    const overflowing = [...document.querySelectorAll('main *')]
      .filter(visible)
      .map((node) => ({ node, rect: node.getBoundingClientRect() }))
      .filter(({ rect }) => rect.right > innerWidth + 1 || rect.left < -1)
      .slice(0, 20)
      .map(({ node, rect }) => ({ tag: node.tagName, text: node.textContent?.trim().slice(0, 120) ?? '', left: Math.round(rect.left), right: Math.round(rect.right) }));
    return {
      viewport: { width: innerWidth, height: innerHeight },
      root_overflow_px: Math.max(0, document.documentElement.scrollWidth - document.documentElement.clientWidth),
      body_overflow_px: Math.max(0, document.body.scrollWidth - document.body.clientWidth),
      clipped_required_controls_or_text: clipped,
      overflowing_elements: overflowing,
    };
  });
}
export function assertLayout(layout, label) {
  assert(layout.root_overflow_px === 0, `${label} root overflow: ${JSON.stringify(layout)}`);
  assert(layout.body_overflow_px === 0, `${label} body overflow: ${JSON.stringify(layout)}`);
  assert(layout.clipped_required_controls_or_text.length === 0, `${label} clipped content: ${JSON.stringify(layout.clipped_required_controls_or_text)}`);
  assert(layout.overflowing_elements.length === 0, `${label} overflowing elements: ${JSON.stringify(layout.overflowing_elements)}`);
}
export async function tabUntil(page, locator, max = 50) {
  for (let count = 1; count <= max; count += 1) {
    await page.keyboard.press('Tab');
    if (await locator.evaluate((node) => node === document.activeElement)) return count;
  }
  throw new Error(`target was not keyboard reachable within ${max} Tab presses`);
}
export async function focusEvidence(locator) {
  return locator.evaluate((node) => {
    const style = getComputedStyle(node);
    return { outline_width: style.outlineWidth, outline_style: style.outlineStyle, box_shadow: style.boxShadow, active: node === document.activeElement };
  });
}
