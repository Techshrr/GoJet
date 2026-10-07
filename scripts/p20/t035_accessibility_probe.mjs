/** Native accessibility diagnostics. Automated checks alone do not admit T035. */
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';

const sha256 = value => createHash('sha256').update(value).digest('hex');
const sampled = new Set();
export const AXE_VERSION = '4.10.3';
export const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

// Keep unresolved checks visible. Zero reported violations is insufficient.
export function auditVerdict(row) {
  return {
    axe_executed: row.axe.version === AXE_VERSION && row.axe.passes.length > 0,
    no_axe_violations: row.axe.violations.length === 0,
    no_unresolved_axe_checks: row.axe.incomplete.length === 0,
    reflow: row.layout.scroll_width <= row.viewport.width + 1,
    reduced_motion: row.layout.reduced_motion && row.layout.active_animations === 0,
    keyboard_sample: row.keyboard.length > 0 && row.keyboard.every(x => x.visible_indicator && x.in_view && x.unobscured),
    no_positive_tabindex: row.layout.positive_tabindex === 0,
    state_messages: row.layout.notices.every(x => x.has_text && ['alert', 'status'].includes(x.role)),
  };
}

export async function accessibilityProbe(page, node, surface) {
  const key = `${node}/${surface}`;
  if (sampled.has(key)) return;
  sampled.add(key);
  const root = process.cwd();
  const out = `${root}/artifacts/v10/${node}/t035`;
  mkdirSync(out, { recursive: true });
  const result = { implementation_commit: execFileSync('git', ['rev-parse', 'HEAD'], {encoding: 'utf8'}).trim(),
    node, surface, kind: 'native-accessibility-diagnostics', formal_p20_t035_claim: false,
    manual_review_required: ['complete keyboard interactions and focus order', 'clipping and focus obstruction', 'WCAG criteria not automated by axe'],
    observations: [], errors: [] };
  const viewport = page.viewportSize();
  const original = await page.evaluate(() => ({theme: document.documentElement.getAttribute('data-theme'),
    reduced: matchMedia('(prefers-reduced-motion: reduce)').matches, x: scrollX, y: scrollY}));
  const focused = await page.evaluateHandle(() => document.activeElement);
  try {
    if (!process.env.P20_AXE_SOURCE) throw new Error('Pinned axe source was not installed');
    const source = readFileSync(process.env.P20_AXE_SOURCE, 'utf8');
    result.axe_source_sha256 = sha256(source);
    await page.evaluate(source);
    const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
    const sizes = Object.fromEntries(['desktop', 'tablet', 'mobile'].map(size => {
      const dimensions = variables[`viewport.${size}`].dimensions.split('×').map(Number);
      if (dimensions.length !== 2 || !dimensions.every(x => Number.isInteger(x) && x > 0)) throw new Error('Invalid canonical viewport');
      return [size, {width: dimensions[0], height: dimensions[1]}];
    }));
    sizes.reflow320 = {width: 320, height: sizes.mobile.height};
    for (const [size, dimensions] of Object.entries(sizes)) for (const theme of ['light', 'dark']) {
      await page.setViewportSize(dimensions);
      await page.emulateMedia({reducedMotion: 'reduce'});
      await page.evaluate(value => document.documentElement.setAttribute('data-theme', value), theme);
      await page.waitForTimeout(400);
      const axe = await page.evaluate(async tags => {
        const report = await window.axe.run(document, {runOnly: {type: 'tag', values: tags}});
        const elements = [...document.querySelectorAll('*')];
        // Omit HTML, text, input values, URLs and CSS selectors from evidence.
        const rules = rows => rows.map(rule => ({id: rule.id, impact: rule.impact, tags: rule.tags,
          nodes: rule.nodes.map(n => {
            let el = null;
            try { if (n.target.length === 1 && typeof n.target[0] === 'string') el = document.querySelector(n.target[0]); } catch {}
            const style = el ? getComputedStyle(el) : null;
            const ancestors = [];
            for (let p = el; p; p = p.parentElement) {
              const s = getComputedStyle(p);
              ancestors.push({opacity: s.opacity, background: s.backgroundColor,
                background_image: s.backgroundImage !== 'none', filter: s.filter,
                blend: s.mixBlendMode});
            }
            return {element_index: elements.indexOf(el), tag: el?.tagName || null,
              computed: style ? {foreground: style.color, background: style.backgroundColor,
                font_size: style.fontSize, font_weight: style.fontWeight,
                has_value: el instanceof HTMLTextAreaElement ? !!el.value : null,
                ancestors} : null,
              checks: [...n.any, ...n.all, ...n.none].map(c => ({id: c.id, impact: c.impact}))};
          })}));
        return {version: report.testEngine.version, violations: rules(report.violations), incomplete: rules(report.incomplete),
          passes: report.passes.map(x => x.id), inapplicable: report.inapplicable.map(x => x.id)};
      }, WCAG_TAGS);
      const layout = await page.evaluate(() => {
        const visible = el => el.getClientRects().length > 0 && getComputedStyle(el).visibility !== 'hidden';
        return {scroll_width: document.documentElement.scrollWidth, viewport_width: innerWidth,
          reduced_motion: matchMedia('(prefers-reduced-motion: reduce)').matches,
          active_animations: document.getAnimations().filter(x => x.playState === 'running').length,
          positive_tabindex: [...document.querySelectorAll('[tabindex]')].filter(el => visible(el) && el.tabIndex > 0).length,
          notices: [...document.querySelectorAll('[data-notice-tone]')].filter(visible).map(el => ({
            role: el.getAttribute('role'), live: el.getAttribute('aria-live'), has_text: !!el.textContent.trim()}))};
      });
      const capture = `${surface}-${size}-${theme}.png`;
      const png = await page.screenshot({path: `${out}/${capture}`, fullPage: true});
      const keyboard = [];
      // Real Tab input; no click, synthetic focus target or test-only tabindex.
      // Bounded sample is retained explicitly, not claimed as complete coverage.
      for (let n = 0; n < 12; n++) {
        await page.keyboard.press('Tab');
        const item = await page.evaluate(() => {
          const el = document.activeElement;
          if (!el || el === document.body || el === document.documentElement) return null;
          const style = getComputedStyle(el), rect = el.getBoundingClientRect();
          const x = Math.max(0, Math.min(innerWidth - 1, rect.x + rect.width / 2));
          const y = Math.max(0, Math.min(innerHeight - 1, rect.y + rect.height / 2));
          const hit = document.elementFromPoint(x, y);
          return {element_index: [...document.querySelectorAll('*')].indexOf(el), tag: el.tagName,
            visible_indicator: (style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0) || style.boxShadow !== 'none',
            in_view: rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.left < innerWidth && rect.bottom > 0 && rect.top < innerHeight,
            unobscured: hit === el || el.contains(hit),
            hit_tag: hit?.tagName || null, hit_element_index: [...document.querySelectorAll('*')].indexOf(hit),
            rectangle: {x: rect.x, y: rect.y, width: rect.width, height: rect.height},
            outline_width: style.outlineWidth, outline_style: style.outlineStyle};
        });
        if (item) {
          if (keyboard.some(x => x.element_index === item.element_index)) break;
          keyboard.push(item);
        }
      }
      const row = {size, theme, viewport: dimensions, axe, layout, keyboard, keyboard_scope: 'up to 12 successive Tab steps',
        capture, capture_sha256: sha256(png)};
      row.checks = auditVerdict(row);
      result.observations.push(row);
      for (const [check, pass] of Object.entries(row.checks)) if (!pass) result.errors.push(`${size}/${theme}: ${check}`);
    }
  } catch (error) {
    result.errors.push(`probe incomplete: ${error.message}`);
  } finally {
    result.status = result.errors.length ? 'FAIL' : 'PASS';
    writeFileSync(`${out}/${surface}.json`, JSON.stringify(result, null, 2) + '\n');
    await page.evaluate(theme => theme === null ? document.documentElement.removeAttribute('data-theme') : document.documentElement.setAttribute('data-theme', theme), original.theme);
    await page.emulateMedia({reducedMotion: original.reduced ? 'reduce' : 'no-preference'});
    if (viewport) await page.setViewportSize(viewport);
    await focused.evaluate(el => { if (el instanceof HTMLElement && el.isConnected) el.focus({preventScroll: true}); });
    await focused.dispose();
    await page.evaluate(({x, y}) => scrollTo(x, y), original);
  }
}
