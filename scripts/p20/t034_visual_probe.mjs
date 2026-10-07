/** Native-page diagnostics only; these observations do not admit T034. */
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { accessibilityProbe } from './t035_accessibility_probe.mjs';

// CSS minifiers may shorten hex colors without changing their value.
export function canonicalColor(value) {
  const color = value.trim().toLowerCase();
  return /^#[0-9a-f]{3}$/.test(color) ? '#' + [...color.slice(1)].map(c => c + c).join('') : color;
}
const sampled = new Set();
export async function visualProbe(page, node, surface) {
  const key = `${node}-${surface}`;
  if (sampled.has(key)) return;
  sampled.add(key);
  const root = process.cwd();
  const out = `${root}/artifacts/v10/${node}/t034`;
  mkdirSync(out, { recursive: true });
  const css = readFileSync(`${root}/frontend/packages/tokens/generated/tokens.css`, 'utf8');
  const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
  const canonical = (name) => {
    const match = /^(\d+)×(\d+)$/.exec(variables[`viewport.${name}`].dimensions);
    if (!match) throw new Error(`Invalid canonical viewport ${name}`);
    return { width: Number(match[1]), height: Number(match[2]) };
  };
  const names = ['--gojet-surface-canvas', '--gojet-surface-default', '--gojet-text-primary', '--gojet-text-secondary'];
  const expected = {};
  for (const theme of ['light', 'dark']) {
    const block = theme === 'light' ? css.split(':root {')[1].split('}')[0] : css.split(':root[data-theme="dark"] {')[1].split('}')[0];
    expected[theme] = Object.fromEntries(names.map(name => {
      const match = new RegExp(`${name}:\\s*([^;]+);`).exec(block);
      if (!match) throw new Error(`Missing canonical ${theme} token ${name}`);
      return [name, match[1].trim().toLowerCase()];
    }));
  }
  const original = await page.evaluate(() => ({theme: document.documentElement.getAttribute('data-theme'), reduced: matchMedia('(prefers-reduced-motion: reduce)').matches}));
  const viewport = page.viewportSize();
  const result = { implementation_commit: execFileSync('git', ['rev-parse', 'HEAD'], {encoding: 'utf8'}).trim(),
    surface, node, kind: 'native-visual-diagnostics', formal_p20_t034_claim: false,
    token_css_sha256: createHash('sha256').update(css).digest('hex'), observations: [], errors: [] };
  try {
    for (const size of ['desktop', 'mobile']) for (const theme of ['light', 'dark']) {
      await page.setViewportSize(canonical(size));
      await page.emulateMedia({ reducedMotion: 'reduce' });
      await page.evaluate(value => document.documentElement.setAttribute('data-theme', value), theme);
      // Sample the settled state, after bounded canonical feedback transitions.
      // Persistent/repeating motion remains observable and fails below.
      const settleMs = await page.evaluate(() => {
        const style = getComputedStyle(document.documentElement);
        const ms = name => { const value = style.getPropertyValue(name).trim(); return value.endsWith('ms') ? parseFloat(value) : parseFloat(value) * 1000; };
        return Math.max(ms('--gojet-motion-duration-feedback') || 0, ms('--gojet-motion-duration-reduced') || 0);
      });
      if (!Number.isFinite(settleMs) || settleMs > 1000) throw new Error('Invalid canonical motion duration');
      await page.waitForTimeout(settleMs);
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      const observation = await page.evaluate(names => {
        const style = getComputedStyle(document.documentElement);
        const visible = element => element.getClientRects().length > 0;
        return {
          theme: document.documentElement.getAttribute('data-theme'),
          reduced_motion: matchMedia('(prefers-reduced-motion: reduce)').matches,
          tokens: Object.fromEntries(names.map(name => [name, style.getPropertyValue(name).trim().toLowerCase()])),
          body: { background: getComputedStyle(document.body).backgroundColor, color: getComputedStyle(document.body).color,
            margin: getComputedStyle(document.body).margin },
          document_canvas: style.backgroundColor,
          native_color_scheme: style.colorScheme,
          browser_default_links: [...document.querySelectorAll('a[href]')].filter(visible).filter(link =>
            ['rgb(0, 0, 238)', 'rgb(85, 26, 139)'].includes(getComputedStyle(link).color)).length,
          overflow: document.documentElement.scrollWidth > innerWidth + 1,
          broken_images: [...document.images].filter(image => visible(image) && (!image.complete || image.naturalWidth === 0)).length,
          placeholder_elements: [...document.querySelectorAll('[data-placeholder], img[src*="placeholder"]')].filter(visible).length,
          active_animations: document.getAnimations().filter(animation => animation.playState === 'running').length,
          svg_icons: [...document.querySelectorAll('svg')].filter(visible).map(icon => ({view_box: icon.getAttribute('viewBox'), hidden: icon.getAttribute('aria-hidden'), role: icon.getAttribute('role')})),
          images: [...document.images].filter(visible).map(image => ({
            alt_present: image.hasAttribute('alt'), width: Number(image.getAttribute('width')), height: Number(image.getAttribute('height')),
            natural_width: image.naturalWidth, natural_height: image.naturalHeight, complete: image.complete,
            same_origin: new URL(image.currentSrc || image.src, location.href).origin === location.origin,
            responsive: image.hasAttribute('srcset'), sizes: !!image.getAttribute('sizes'),
            loading: image.getAttribute('loading'), priority: image.getAttribute('fetchpriority'),
          })),
          auth_state: document.querySelector('[data-auth-state]')?.getAttribute('data-auth-state') || null,
          notices: [...document.querySelectorAll('[data-notice-tone]')].filter(visible).map(notice => {
            const icon = notice.querySelector('svg');
            return { tone: notice.getAttribute('data-notice-tone'), role: notice.getAttribute('role'),
              has_text: !!notice.textContent.trim(), foreground: getComputedStyle(notice).color,
              background: getComputedStyle(notice).backgroundColor, focused: notice === document.activeElement,
              icon: icon ? { classes: [...icon.classList], hidden: icon.getAttribute('aria-hidden'),
                view_box: icon.getAttribute('viewBox'), width: icon.getBoundingClientRect().width,
                height: icon.getBoundingClientRect().height, stroke: getComputedStyle(icon).strokeWidth } : null };
          }),
        };
      }, names);
      const checks = { canonical_tokens: names.every(name => canonicalColor(observation.tokens[name]) === canonicalColor(expected[theme][name])),
        reduced_motion: observation.reduced_motion && observation.active_animations === 0,
        document_margin_reset: observation.body.margin === '0px',
        themed_links: observation.browser_default_links === 0,
        no_overflow: !observation.overflow, images_loaded: observation.broken_images === 0,
        no_placeholder_elements: observation.placeholder_elements === 0 };
      const file = `${surface}-${size}-${theme}.png`;
      const png = await page.screenshot({ path: `${out}/${file}`, fullPage: true });
      result.observations.push({ size, viewport: canonical(size), theme, expected_tokens: expected[theme], ...observation, checks,
        capture: file, capture_sha256: createHash('sha256').update(png).digest('hex') });
      for (const [check, pass] of Object.entries(checks)) if (!pass) result.errors.push(`${size}/${theme}: ${check}`);
    }
  } catch (error) {
    result.errors.push(`probe incomplete: ${error.message}`);
  } finally {
    result.status = result.errors.length ? 'FAIL' : 'PASS';
    writeFileSync(`${out}/${surface}.json`, JSON.stringify(result, null, 2) + '\n');
    await page.evaluate(theme => theme === null ? document.documentElement.removeAttribute('data-theme') : document.documentElement.setAttribute('data-theme', theme), original.theme);
    await page.emulateMedia({ reducedMotion: original.reduced ? 'reduce' : 'no-preference' });
    if (viewport) await page.setViewportSize(viewport);
  }
  await accessibilityProbe(page, node, surface);
}
