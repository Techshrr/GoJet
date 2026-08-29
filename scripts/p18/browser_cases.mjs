import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright-core';

const caseId = process.argv[process.argv.indexOf('--case') + 1];
if (caseId !== 'P18-T011') throw new Error(`unsupported case: ${caseId}`);
const base = (process.env.P18_HTTP_BASE || 'http://127.0.0.1:8098').replace(/\/$/, '');
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P18 browser evidence');
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
const page = await context.newPage();
const externalRequests = [];
page.on('request', (request) => {
  try { if (new URL(request.url()).origin !== new URL(base).origin) externalRequests.push(request.url()); }
  catch { externalRequests.push(request.url()); }
});
await page.goto(`${base}/docs/en/`, { waitUntil: 'networkidle' });
const before = await page.evaluate(() => document.activeElement?.outerHTML || '');
await page.keyboard.press('Control+K');
await page.waitForTimeout(150);
const dialog = page.locator('dialog[open], [role="dialog"]:visible').first();
if (!(await dialog.count())) throw new Error('Ctrl+K did not open Starlight search');
const input = dialog.locator('input[type="search"], input').first();
await input.fill('API keys');
await page.waitForTimeout(500);
const choices = dialog.locator('a[href], [role="option"]');
const choiceCount = await choices.count();
if (choiceCount < 1) throw new Error('search produced no keyboard choices');
await page.keyboard.press('ArrowDown');
const activeAfterArrow = await page.evaluate(() => document.activeElement?.tagName || '');
await page.keyboard.press('Escape');
await page.waitForTimeout(120);
const dialogAfterEscape = await page.locator('dialog[open], [role="dialog"]:visible').count();
const focusAfterEscape = await page.evaluate(() => ({ tag: document.activeElement?.tagName || '', text: document.activeElement?.textContent?.trim() || '', aria: document.activeElement?.getAttribute('aria-label') || '' }));
await browser.close();
if (dialogAfterEscape !== 0) throw new Error('Escape did not close search');
if (externalRequests.length) throw new Error(`unexpected external search requests: ${externalRequests.join(',')}`);
const payload = { case: caseId, status: 'PASS', shortcut: 'Control+K', dialog_opened: true, choice_count: choiceCount, arrow_navigation_active_tag: activeAfterArrow, escape_closed: true, focus_return: focusAfterEscape, external_requests: externalRequests, before_focus: before };
mkdirSync('artifacts/v10/P18/browser', { recursive: true });
writeFileSync('artifacts/v10/P18/browser/P18-T011.json', JSON.stringify(payload, null, 2) + '\n');
process.stdout.write(JSON.stringify(payload, null, 2));
