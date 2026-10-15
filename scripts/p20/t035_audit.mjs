/** Recompute native T035 diagnostics. This is not formal WCAG admission. */
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve, dirname, basename } from 'node:path';
import { pathToFileURL } from 'node:url';
import { auditVerdict, nativeTabCandidate, fragmentsUnobscured, textareaContrast } from './t035_accessibility_probe.mjs';
import { speechReceipt, textSpacingClipped } from './t035_assistive_probe.mjs';

const require = (value, message) => { if (!value) throw new Error(message); };
const hash = data => createHash('sha256').update(data).digest('hex');
const stable = value => JSON.stringify(value, Object.keys(value).sort());
export function auditAssistive(data, path) {
  const a = data.assistive;
  require(a?.zoom?.length === 2 && new Set(a.zoom.map(x => x.theme)).size === 2, 'missing zoom matrix');
  for (const row of a.zoom) {
    require(['light','dark'].includes(row.theme) && row.method === '200%-device-metrics-equivalent', 'unsupported zoom evidence');
    require(row.physical_viewport.width === 1440 && row.physical_viewport.height === 900 &&
      row.layout.width === 720 && row.layout.height === 450 && row.layout.dpr === 2 &&
      row.layout.scroll_width <= 721 && row.layout.main_visible === true, 'zoom reflow failed');
    const capture = `${data.surface}-zoom200-${row.theme}.png`;
    require(row.capture === capture, 'invalid zoom capture');
    const png = readFileSync(resolve(dirname(path), capture));
    require(png.subarray(0,8).equals(Buffer.from([137,80,78,71,13,10,26,10])) &&
      png.length >= 24 && png.readUInt32BE(16) === 1440 && hash(png) === row.capture_sha256, 'zoom capture digest/physical width mismatch');
  }
  const sr = a.screen_reader;
  if (['workspace','admin'].includes(data.surface)) {
    require(a.navigation?.length===2 && new Set(a.navigation.map(x=>x.theme)).size===2,'missing mobile navigation evidence');
    for(const row of a.navigation) {
      const expected=data.surface==='workspace'?19:13;
      require(['light','dark'].includes(row.theme) && row.inventory.count===expected && row.inventory.expected===expected &&
        row.inventory.same_routes===true && row.escape_focus_return===true && row.steps.length>0 &&
        row.steps.every(s=>s.inside===true) && row.inventory.controls.length>=expected &&
        new Set(row.inventory.controls).size===row.inventory.controls.length &&
        row.inventory.controls.every(i=>row.steps.some(s=>s.element_index===i)),'mobile navigation lost routes/keyboard coverage');
      const capture=`${data.surface}-navigation320-${row.theme}.png`;
      require(row.capture===capture && hash(readFileSync(resolve(dirname(path),capture)))===row.capture_sha256,'mobile navigation capture mismatch');
    }
  }
  require(a.text_spacing?.length === 2 && new Set(a.text_spacing.map(x=>x.theme)).size === 2, 'missing text-spacing matrix');
  for (const row of a.text_spacing) {
    require(['light','dark'].includes(row.theme) && row.method==='WCAG-1.4.12-inspector-user-stylesheet' && row.origin==='inspector' &&
      row.layout.width===320 && row.layout.scroll_width<=321 && row.layout.clipped.length===0, 'text-spacing clipping/overflow');
    const override=row.layout.override;
    require(override && override.font_size>0 &&
      Math.abs(override.line_height/override.font_size-1.5)<0.01 &&
      Math.abs(override.letter_spacing/override.font_size-0.12)<0.001 &&
      Math.abs(override.word_spacing/override.font_size-0.16)<0.001,'text-spacing override not actually applied');
    require(Array.isArray(row.layout.nodes) && row.layout.nodes.length>0 &&
      row.layout.nodes.every(n=>Number.isInteger(n.element_index) && n.element_index>=0 &&
        ['client_width','client_height','scroll_width','scroll_height'].every(k=>Number.isFinite(n[k])&&n[k]>=0) &&
        ['overflow_x','overflow_y'].every(k=>['visible','hidden','clip','auto','scroll'].includes(n[k]))) &&
      JSON.stringify(row.layout.nodes.filter(textSpacingClipped))===JSON.stringify(row.layout.clipped),
      'unproven/forged text-spacing clipping verdict');
    const capture=`${data.surface}-textspacing320-${row.theme}.png`;
    require(row.capture===capture, 'invalid text-spacing capture');
    const png=readFileSync(resolve(dirname(path),capture));
    require(png.subarray(0,8).equals(Buffer.from([137,80,78,71,13,10,26,10])) && hash(png)===row.capture_sha256,'text-spacing capture digest mismatch');
  }
  require(sr?.engine === 'Orca' && sr.input_authority==='X11 XTEST keyboard; active Chrome window and DOM focus checked' && typeof sr.engine_version === 'string' && /\d+\.\d+/.test(sr.engine_version) && sr.steps?.length === 2 &&
    new Set(sr.steps.map(x => x.element_index)).size === 2, 'missing native screen-reader sample');
  for (const step of sr.steps) {
    const receipt = speechReceipt([step.speech_output,step.dispatcher_output,step.role_speech_output,step.role_dispatcher_output].join('\n'), step.name, step.role);
    require(step.input === 'native-X11-Tab' && Number.isInteger(step.element_index) && step.element_index >= 0 &&
      Number.isInteger(step.log_start) && Number.isInteger(step.log_end) && step.log_start >= 0 && step.log_end > step.log_start &&
      receipt && ['speech_output','dispatcher_output','role_speech_output','role_dispatcher_output'].every(key => receipt[key] === step[key]), 'invalid speech sample');
  }
  return {zoom_observations:2, text_spacing_observations:2, native_screen_reader_controls:2, screen_reader_engine:sr.engine_version, screen_reader_scope:sr.scope};
}
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
  const assistive = auditAssistive(data, path);
  return {surface: data.surface, node: data.node, observations: pairs.size, tab_steps: tabSteps,
    axe_source_sha256: data.axe_source_sha256, assistive, formal_p20_t035_claim: false};
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
