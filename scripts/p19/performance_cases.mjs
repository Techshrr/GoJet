import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { gzipSync } from 'node:zlib';
import { chromium } from 'playwright-core';

const root = process.cwd();
const dist = `${root}/frontend/apps/site/dist`;
const outDir = `${root}/artifacts/v10/P19/performance`;
mkdirSync(outDir, { recursive: true });
const baseUrl = process.env.P19_SITE_URL || 'http://127.0.0.1:4194';
const implementationCommit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
function emit(caseId, name, errors, details) {
  const data = { node: 'P19', case: caseId, name, status: errors.length ? 'FAIL' : 'PASS', implementation_commit: implementationCommit, errors, details };
  writeFileSync(`${outDir}/${caseId}.json`, JSON.stringify(data, null, 2) + '\n');
  console.log(`${caseId}: ${data.status}`); for (const error of errors) console.log(`  - ${error}`); return !errors.length;
}

function collectInitialJs() {
  const manifest = JSON.parse(readFileSync(`${dist}/.vite/manifest.json`, 'utf8'));
  const byFile = new Map(Object.entries(manifest).map(([key, value]) => [key, value]));
  const entries = Object.entries(manifest).filter(([, value]) => value.isEntry);
  const files = new Set();
  const visit = (key) => {
    const record = byFile.get(key); if (!record) return;
    if (record.file?.endsWith('.js')) files.add(record.file);
    for (const imported of record.imports || []) visit(imported);
  };
  for (const [key] of entries) visit(key);
  const dynamic = Object.values(manifest).filter((value) => value.isDynamicEntry || value.name?.includes('Page')).map((value) => value.file).filter(Boolean);
  return { manifest, entryKeys: entries.map(([key]) => key), files: [...files].sort(), dynamic: [...new Set(dynamic)].sort() };
}

const chromeCandidates = [process.env.CHROME_BIN, '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].filter(Boolean);
const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('system Chrome/Chromium is required for P19 performance evidence');
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] });

async function t027() {
  const errors = [];
  const initial = collectInitialJs(); let gzipBytes = 0; const sizes = [];
  for (const file of initial.files) {
    const bytes = readFileSync(`${dist}/${file}`); const gzip = gzipSync(bytes, { level: 9 }).length; gzipBytes += gzip; sizes.push({ file, rawBytes: bytes.length, gzipBytes: gzip });
  }
  const budget = 150 * 1024;
  if (gzipBytes > budget) errors.push(`Website initial JS gzip ${gzipBytes} exceeds ${budget}`);
  if (!initial.dynamic.length) errors.push('no route-split dynamic Website chunks found');
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  const page = await context.newPage(); const loaded = [];
  page.on('response', (response) => { if (response.request().resourceType() === 'script') loaded.push(response.url()); });
  await page.goto(`${baseUrl}/`, { waitUntil: 'networkidle' });
  const forbidden = loaded.filter((url) => /(?:workspace|admin)(?:[-./]|$)/i.test(new URL(url).pathname));
  if (forbidden.length) errors.push(`Website loaded Workspace/Admin bundle candidates: ${forbidden.join(', ')}`);
  await context.close();
  return emit('P19-T027', 'Website bundle isolation and initial JS budget', errors, { budgetBytes: budget, initialGzipBytes: gzipBytes, initialFiles: sizes, dynamicChunks: initial.dynamic, loadedScriptUrls: loaded, forbiddenBundleRequests: forbidden });
}

async function t028() {
  const errors = [];
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  await context.addInitScript(() => {
    window.__p19Perf = { lcp: 0, cls: 0, events: [] };
    if (PerformanceObserver.supportedEntryTypes.includes('largest-contentful-paint')) new PerformanceObserver((list) => { for (const entry of list.getEntries()) window.__p19Perf.lcp = Math.max(window.__p19Perf.lcp, entry.startTime || entry.renderTime || entry.loadTime || 0); }).observe({ type: 'largest-contentful-paint', buffered: true });
    if (PerformanceObserver.supportedEntryTypes.includes('layout-shift')) new PerformanceObserver((list) => { for (const entry of list.getEntries()) if (!entry.hadRecentInput) window.__p19Perf.cls += entry.value; }).observe({ type: 'layout-shift', buffered: true });
    if (PerformanceObserver.supportedEntryTypes.includes('event')) new PerformanceObserver((list) => { for (const entry of list.getEntries()) if (entry.interactionId) window.__p19Perf.events.push({ name: entry.name, duration: entry.duration, interactionId: entry.interactionId }); }).observe({ type: 'event', durationThreshold: 0 });
  });
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 40, downloadThroughput: Math.floor(10 * 1024 * 1024 / 8), uploadThroughput: Math.floor(5 * 1024 * 1024 / 8), connectionType: 'cellular4g' });
  const started = Date.now(); await page.goto(`${baseUrl}/`, { waitUntil: 'networkidle' }); await page.waitForTimeout(250);
  await page.evaluate(() => document.querySelector('.site-primary-link')?.addEventListener('click', (event) => event.preventDefault(), { once: true }));
  await page.locator('.site-primary-link').first().click(); await page.waitForTimeout(250);
  const metrics = await page.evaluate(() => ({
    ...window.__p19Perf,
    supported: PerformanceObserver.supportedEntryTypes,
    resources: performance.getEntriesByType('resource').map((entry) => ({ name: entry.name, initiatorType: entry.initiatorType, duration: entry.duration, transferSize: entry.transferSize })),
    renderedImages: [...document.images].map((img) => ({ src: img.currentSrc || img.src, width: img.width, height: img.height, naturalWidth: img.naturalWidth, naturalHeight: img.naturalHeight })),
  }));
  const interactions = new Map(); for (const event of metrics.events) interactions.set(event.interactionId, Math.max(interactions.get(event.interactionId) || 0, event.duration));
  const inp = interactions.size ? Math.max(...interactions.values()) : null;
  if (!(metrics.lcp > 0) || metrics.lcp > 2500) errors.push(`LCP ${metrics.lcp}ms is missing or exceeds 2500ms`);
  if (metrics.cls > 0.1) errors.push(`CLS ${metrics.cls} exceeds 0.1`);
  if (inp === null) errors.push('no trusted Event Timing interaction was captured for INP evidence');
  else if (inp > 200) errors.push(`INP ${inp}ms exceeds 200ms`);
  const fontResources = metrics.resources.filter((resource) => resource.initiatorType === 'font' || /\.(?:woff2?|ttf|otf)(?:\?|$)/i.test(resource.name));
  const remoteFonts = fontResources.filter((resource) => new URL(resource.name).origin !== new URL(baseUrl).origin);
  if (remoteFonts.length) errors.push(`remote font requests violate Website font boundary: ${remoteFonts.map((x) => x.name).join(', ')}`);
  const initial = collectInitialJs(); const hashedAsset = initial.files[0];
  const hashedResponse = await context.request.get(`${baseUrl}/${hashedAsset}`); const hashedCache = hashedResponse.headers()['cache-control'] || '';
  if (!/max-age=31536000/i.test(hashedCache) || !/immutable/i.test(hashedCache)) errors.push(`hashed JS cache policy is not one-year immutable: ${hashedCache}`);
  const socialResponse = await context.request.get(`${baseUrl}/assets/social/gojet-en.png`); const socialCache = socialResponse.headers()['cache-control'] || '';
  if (!/max-age=86400/i.test(socialCache) || /immutable/i.test(socialCache)) errors.push(`stable social-card cache policy is not bounded one-day cache: ${socialCache}`);
  const totalMs = Date.now() - started;
  await context.close();
  return emit('P19-T028', 'CWV image font and cache budgets', errors, {
    labProfile: { viewport: '1440x900', latencyMs: 40, downloadMbps: 10, uploadMbps: 5, browser: executablePath },
    lcpMs: metrics.lcp, inpMs: inp, cls: metrics.cls, wallClockMs: totalMs,
    eventEntries: metrics.events, renderedPrincipalImageCount: metrics.renderedImages.length,
    imageContract: metrics.renderedImages.length ? 'rendered images must provide intrinsic dimensions' : 'no principal rendered Website image; responsive AVIF/WebP transfer is not invoked',
    fontStrategy: fontResources.length ? 'same-origin font assets only' : 'no network webfont dependency; Design System local/system fallback avoids invisible-text blocking',
    fontResources, cache: { hashedAsset, hashedCache, socialCard: '/assets/social/gojet-en.png', socialCache },
  });
}

const pass = (await t027()) && (await t028());
await browser.close();
process.exit(pass ? 0 : 1);
