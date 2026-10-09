/** Supplemental native evidence, not a WCAG conformance declaration. */
import { readFileSync, statSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';

const hash = data => createHash('sha256').update(data).digest('hex');

// Orca logs SPEECH OUTPUT even without a speech server. Require the real
// speech-dispatcher path too; a debug-only utterance cannot pass this sample.
export function speechReceipt(segment, name, role) {
  if (!name || name.length > 80 || /[@\r\n]|https?:/i.test(name)) return null;
  if (!['link', 'button'].includes(role)) return null;
  const output = segment.split('\n').filter(line => line.includes('SPEECH OUTPUT:'));
  const dispatch = segment.split('\n').filter(line => line.includes('SPEECH DISPATCHER:') && line.includes('Speaking'));
  // Orca can use separate voices/utterances for the label and role. Both must
  // come from the fresh focus interval and both must reach its dispatcher path.
  const named = line => line.toLowerCase().includes(name.toLowerCase());
  const typed = line => new RegExp(`\\b${role}\\b`, 'i').test(line);
  const utterance = output.find(named), sent = dispatch.find(named);
  const roleUtterance = output.find(typed), roleSent = dispatch.find(typed);
  if (!utterance || !sent || !roleUtterance || !roleSent) return null;
  return {name, role, speech_output: utterance, dispatcher_output: sent,
    role_speech_output: roleUtterance, role_dispatcher_output: roleSent};
}

export async function assistiveProbe(page, surface, out) {
  const savedViewport = page.viewportSize();
  const savedTheme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
  const savedScroll = await page.evaluate(() => ({x: scrollX, y: scrollY}));
  const focused = await page.evaluateHandle(() => document.activeElement);
  const session = await page.context().newCDPSession(page);
  const result = {zoom: [], screen_reader: {engine: 'Orca', engine_version: process.env.P20_ORCA_VERSION,
    scope: 'two distinct native Tab control announcements and dispatcher attempts; not audio playback or complete manual review', steps: []}};
  try {
    // Same 1440x900 physical area at 200%: 720x450 CSS pixels, DPR 2.
    // This is explicitly device-metrics-equivalent evidence, not a claim that
    // the browser toolbar zoom setting was changed.
    for (const theme of ['light', 'dark']) {
      await page.setViewportSize({width:720,height:450});
      await session.send('Emulation.clearDeviceMetricsOverride');
      await session.send('Emulation.setDeviceMetricsOverride', {
        width: 720, height: 450, deviceScaleFactor: 2, mobile: false,
        screenWidth: 1440, screenHeight: 900,
      });
      await page.evaluate(value => {
        document.documentElement.setAttribute('data-theme', value);
        scrollTo(0, 0);
      }, theme);
      await page.waitForTimeout(400);
      const layout = await page.evaluate(() => ({width: innerWidth, height: innerHeight,
        dpr: devicePixelRatio, scroll_width: document.documentElement.scrollWidth,
        scroll_height: Math.max(innerHeight, document.documentElement.scrollHeight),
        main_visible: [...document.querySelectorAll('main')].some(el => el.getClientRects().length > 0)}));
      const capture = `${surface}-zoom200-${theme}.png`;
      // Playwright fullPage restores its own metrics, which competes with this
      // supplemental CDP session. Capture directly on the owning session.
      const shot = await session.send('Page.captureScreenshot', {format:'png',fromSurface:true,
        captureBeyondViewport:true,clip:{x:0,y:0,width:720,height:layout.scroll_height,scale:1}});
      const png = Buffer.from(shot.data, 'base64');
      writeFileSync(`${out}/${capture}`, png);
      result.zoom.push({theme, method: '200%-device-metrics-equivalent', physical_viewport: {width:1440,height:900},
        layout, capture, capture_sha256: hash(png)});
      if (layout.width !== 720 || layout.height !== 450 || layout.dpr !== 2 || png.readUInt32BE(16) !== 1440 ||
          layout.scroll_width > 721 || !layout.main_visible) throw new Error(`200% equivalent reflow failed: ${theme}`);
    }
    await session.send('Emulation.clearDeviceMetricsOverride');
    if (savedViewport) await page.setViewportSize(savedViewport);
    const log = process.env.P20_ORCA_LOG;
    if (!log) throw new Error('native Orca session was not started');
    const orcaPid = Number(process.env.P20_ORCA_PID);
    if (!Number.isInteger(orcaPid) || orcaPid <= 0) throw new Error('native Orca process binding missing');
    process.kill(orcaPid, 0);
    await page.bringToFront();
    await page.waitForTimeout(1000);
    const visited = new Set();
    for (let i = 0; i < 128 && result.screen_reader.steps.length < 2; i++) {
      const start = statSync(log).size;
      process.kill(orcaPid, 0);
      await page.keyboard.press('Tab');
      const target = await page.evaluate(() => {
        const el = document.activeElement;
        if (!el || !['A', 'BUTTON'].includes(el.tagName)) return null;
        const name = (el.getAttribute('aria-label') || el.textContent || '').trim().replace(/\s+/g, ' ');
        return {name, role: el.tagName === 'A' ? 'link' : 'button', element_index: [...document.querySelectorAll('*')].indexOf(el)};
      });
      if (!target || visited.has(target.element_index) || !target.name || target.name.length > 80 || /[@\r\n]|https?:/i.test(target.name)) continue;
      let receipt;
      for (let n = 0; n < 12 && !receipt; n++) {
        await page.waitForTimeout(250);
        receipt = speechReceipt(readFileSync(log).subarray(start).toString('utf8'), target.name, target.role);
      }
      if (!receipt) continue;
      visited.add(target.element_index);
      result.screen_reader.steps.push({...receipt, element_index: target.element_index, input: 'Tab',
        log_start: start, log_end: statSync(log).size});
    }
    if (result.screen_reader.steps.length !== 2) throw new Error('Orca did not produce two fresh named-control speech receipts');
    return result;
  } catch (error) {
    error.assistive = result;
    throw error;
  } finally {
    await session.send('Emulation.clearDeviceMetricsOverride');
    await session.detach();
    if (savedViewport) await page.setViewportSize(savedViewport);
    await page.evaluate(theme => theme === null ? document.documentElement.removeAttribute('data-theme') : document.documentElement.setAttribute('data-theme', theme), savedTheme);
    await focused.evaluate(el => {if (el instanceof HTMLElement && el.isConnected) el.focus({preventScroll:true});});
    await focused.dispose();
    await page.evaluate(({x,y}) => scrollTo(x,y), savedScroll);
  }
}
