// Synthetic boundary fixtures; never published as native observations.
import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, writeFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {auditFile} from './t035_audit.mjs';

const head = 'a'.repeat(40);
const viewports = {desktop: {width:1440,height:900},tablet:{width:1024,height:768},mobile:{width:390,height:844},reflow320:{width:320,height:844}};
function fixture(directory) {
  const png = Buffer.from([137,80,78,71,13,10,26,10]);
  const data = {implementation_commit:head,kind:'native-accessibility-diagnostics',formal_p20_t035_claim:false,
    axe_source_sha256:'880970c081707360e64f34cea25ff91892f5bc95675b0776925b9709dd8a68bb',
    surface:'public',node:'P10',status:'PASS',errors:[],observations:[]};
  for (const [size,viewport] of Object.entries(viewports)) for (const theme of ['light','dark']) {
    const capture = `public-${size}-${theme}.png`;
    writeFileSync(join(directory,capture),png);
    data.observations.push({size,theme,viewport,capture,capture_sha256:createHash('sha256').update(png).digest('hex'),
      axe:{version:'4.10.3',passes:['label'],violations:[],incomplete:[]},supplemental_contrast:[],
      layout:{scroll_width:viewport.width,reduced_motion:true,active_animations:0,positive_tabindex:0,notices:[]},
      keyboard_inventory:[{element_index:1,tag:'BUTTON',tab_index:0,disabled:false,inert:false,visible:true,closed_details:false}],
      keyboard_expected:[1],keyboard:[{element_index:1,visible_indicator:true,in_view:true,unobscured:true,
        fragments:[{width:20,height:20,in_view:true,unobscured:true}]}]});
  }
  return data;
}

test('raw evidence audit rejects forged metadata, observations and captures', () => {
  const directory = mkdtempSync(join(tmpdir(),'gojet-audit-unit-'));
  try {
    const source = fixture(directory), path = join(directory,'public.json');
    writeFileSync(path,JSON.stringify(source));
    assert.equal(auditFile(path,head,viewports).observations,8);
    const mutations = [
      d => d.implementation_commit = 'b'.repeat(40),
      d => d.axe_source_sha256 = 'b'.repeat(64),
      d => d.observations.pop(),
      d => d.observations[0] = structuredClone(d.observations[1]),
      d => d.observations[0].keyboard_expected = [],
      d => d.observations[0].keyboard = [],
      d => d.observations[0].keyboard[0].fragments[0].unobscured = false,
      d => d.observations[0].layout.scroll_width = 2000,
      d => d.observations[0].capture_sha256 = 'b'.repeat(64),
      d => d.observations[0].supplemental_contrast = [{measurement:{pass:true}}],
      d => d.observations[0].capture = '../escape.png',
    ];
    for (const mutate of mutations) {
      const bad = structuredClone(source); mutate(bad); writeFileSync(path,JSON.stringify(bad));
      assert.throws(() => auditFile(path,head,viewports));
    }
  } finally { rmSync(directory,{recursive:true,force:true}); }
});
