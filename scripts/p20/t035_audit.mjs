/** Recompute native T035 diagnostics. This is not formal WCAG admission. */
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve, dirname, basename } from 'node:path';
import { pathToFileURL } from 'node:url';
import { auditVerdict, nativeTabCandidate, fragmentsUnobscured, textareaContrast } from './t035_accessibility_probe.mjs';

const require = (value, message) => { if (!value) throw new Error(message); };
const hash = data => createHash('sha256').update(data).digest('hex');
const stable = value => JSON.stringify(value, Object.keys(value).sort());
export function auditFile(path, head, viewports) {
  const data = JSON.parse(readFileSync(path, 'utf8'));
  require(data.implementation_commit === head, 'mixed implementation head');
  require(data.kind === 'native-accessibility-diagnostics' && data.formal_p20_t035_claim === false, 'invalid diagnostic scope');
  require(data.axe_source_sha256 === '880970c081707360e64f34cea25ff91892f5bc95675b0776925b9709dd8a68bb', 'unpinned axe source digest');
  require(data.observations.length === 8, 'incomplete matrix');
  const pairs = new Set();
  let tabSteps = 0;
  for (const row of data.observations) {
    require(viewports[row.size] && ['light', 'dark'].includes(row.theme), 'unexpected observation');
    const key = `${row.size}/${row.theme}`;
    require(!pairs.has(key), 'duplicate observation'); pairs.add(key);
    require(stable(row.viewport) === stable(viewports[row.size]), 'viewport drift');
    require(Array.isArray(row.keyboard_inventory), 'missing raw keyboard inventory');
    const expected = row.keyboard_inventory.filter(nativeTabCandidate).map(x => x.element_index);
    require(JSON.stringify(expected) === JSON.stringify(row.keyboard_expected), 'forged keyboard inventory');
    require(row.keyboard.length > 0 && row.keyboard.length <= 256, 'invalid keyboard traversal');
    for (const item of row.keyboard) {
      require(fragmentsUnobscured(item.fragments) === item.unobscured, 'forged fragment verdict');
      require(item.element_index >= 0, 'unknown focus target');
    }
    require(Object.values(auditVerdict(row)).every(Boolean), `raw diagnostics failed: ${key}`);
    const measurements = row.axe.incomplete.filter(r => r.id === 'color-contrast')
      .flatMap(r => r.nodes.map(n => ({element_index: n.element_index, measurement: textareaContrast(n)})));
    require(JSON.stringify(measurements) === JSON.stringify(row.supplemental_contrast), 'forged contrast measurement');
    const capture = `${data.surface}-${row.size}-${row.theme}.png`;
    require(row.capture === capture && basename(capture) === capture, 'invalid capture path');
    const png = readFileSync(resolve(dirname(path), capture));
    require(png.subarray(0, 8).equals(Buffer.from([137,80,78,71,13,10,26,10])), 'invalid PNG');
    require(hash(png) === row.capture_sha256, 'capture digest mismatch');
    tabSteps += row.keyboard.length;
  }
  require(data.status === 'PASS' && data.errors.length === 0, 'producer diagnostics failed');
  return {surface: data.surface, node: data.node, observations: pairs.size, tab_steps: tabSteps,
    axe_source_sha256: data.axe_source_sha256, formal_p20_t035_claim: false};
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const [head, ...files] = process.argv.slice(2);
  require(/^[a-f0-9]{40}$/.test(head || ''), 'exact head required');
  const tokens = JSON.parse(readFileSync('frontend/packages/tokens/generated/design-variables.json', 'utf8')).tokens.composite;
  const viewports = Object.fromEntries(['desktop', 'tablet', 'mobile'].map(size => {
    const [width, height] = tokens[`viewport.${size}`].dimensions.split('×').map(Number);
    return [size, {width, height}];
  }));
  viewports.reflow320 = {width: 320, height: viewports.mobile.height};
  const rows = files.map(path => auditFile(path, head, viewports));
  const mapping = {website:'P19', docs:'P18', auth:'P15', 'auth-invalid':'P15', 'auth-code-sent':'P15', 'auth-verified':'P15', workspace:'P10', public:'P10', admin:'P17'};
  require(rows.length === 9 && new Set(rows.map(x => x.surface)).size === 9 && rows.every(x => mapping[x.surface] === x.node), 'incomplete native surfaces');
  require(new Set(rows.map(x => x.axe_source_sha256)).size === 1, 'mixed axe engines');
  console.log(JSON.stringify({head, observations: 72, surfaces: rows, formal_p20_t035_claim: false,
    outstanding: ['formal source/provenance collection', 'interactive and manual WCAG review']}, null, 2));
}
