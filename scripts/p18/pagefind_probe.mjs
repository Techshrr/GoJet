import { existsSync } from 'node:fs';
import { chromium } from 'playwright-core';

const args = new Map();
for (let i = 2; i < process.argv.length; i += 2) args.set(process.argv[i], process.argv[i + 1] ?? '');
const locale = args.get('--locale');
const query = args.get('--query');
const offline = args.get('--offline') === '1';
if (!['en', 'zh-CN'].includes(locale) || !query) throw new Error('locale/query required');
const base = (process.env.P18_HTTP_BASE || 'http://127.0.0.1:8098').replace(/\/$/, '');
const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P18 Pagefind evidence');
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
const context = await browser.newContext({ locale: locale === 'zh-CN' ? 'zh-CN' : 'en-US' });
const page = await context.newPage();
const requests = [];
page.on('request', (request) => requests.push(request.url()));
if (offline) await page.route('**/pagefind/**', (route) => route.abort('internetdisconnected'));
const url = `${base}/docs/${locale}/search?q=${encodeURIComponent(query)}`;
await page.goto(url, { waitUntil: 'domcontentloaded' });
await page.waitForFunction(() => {
  const state = document.querySelector('[data-gojet-search]')?.getAttribute('data-state');
  return ['results', 'empty', 'offline-static', 'invalid'].includes(state || '');
}, null, { timeout: 12000 });
const result = await page.evaluate(() => {
  const root = document.querySelector('[data-gojet-search]');
  return {
    state: root?.getAttribute('data-state') || null,
    status: root?.querySelector('[data-search-status]')?.textContent?.trim() || '',
    hrefs: [...(root?.querySelectorAll('[data-search-results] a[href]') || [])].map((node) => node.getAttribute('href')),
    text: root?.textContent?.replace(/\s+/g, ' ').trim() || '',
  };
});
const origin = new URL(base).origin;
result.external_requests = requests.filter((value) => {
  try { return new URL(value).origin !== origin; } catch { return true; }
});
result.pagefind_requests = requests.filter((value) => value.includes('/pagefind/')).length;
result.offline = offline;
await browser.close();
process.stdout.write(JSON.stringify(result));
