// Synthetic boundary fixtures; never published as native observations.
import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, writeFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {auditFile, auditAssistive} from './t035_audit.mjs';

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
  data.assistive = {zoom:[], text_spacing:[], screen_reader:{engine:'Orca',engine_version:'46.1',scope:'synthetic unit fixture; not native evidence',steps:[]}};
  for (const theme of ['light','dark']) {
    const capture = `public-zoom200-${theme}.png`;
    const zoomPng = Buffer.alloc(24);png.copy(zoomPng);zoomPng.writeUInt32BE(1440,16);
    writeFileSync(join(directory,capture),zoomPng);
    data.assistive.zoom.push({theme,method:'200%-device-metrics-equivalent',physical_viewport:{width:1440,height:900},
      layout:{width:720,height:450,dpr:2,scroll_width:720,main_visible:true},capture,
      capture_sha256:createHash('sha256').update(zoomPng).digest('hex')});
  }
  for(const theme of ['light','dark']) {
    const capture=`public-textspacing320-${theme}.png`;writeFileSync(join(directory,capture),png);
    data.assistive.text_spacing.push({theme,method:'WCAG-1.4.12-user-stylesheet',layout:{width:320,scroll_width:320,clipped:[]},capture,capture_sha256:createHash('sha256').update(png).digest('hex')});
  }
  for (const [i,name] of ['Open plain text','Download text'].entries()) data.assistive.screen_reader.steps.push({
    name,role:'link',element_index:i+1,input:'Tab',log_start:i*100,log_end:i*100+90,speech_output:`SPEECH OUTPUT: '${name} link' {}`,
    dispatcher_output:`SPEECH DISPATCHER: Speaking '${name} link' as string`,
    role_speech_output:`SPEECH OUTPUT: '${name} link' {}`,
    role_dispatcher_output:`SPEECH DISPATCHER: Speaking '${name} link' as string`});
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
      d => delete d.assistive,
      d => d.assistive.zoom.pop(),
      d => d.assistive.zoom[0].layout.dpr = 1,
      d => d.assistive.zoom[0].layout.scroll_width = 1000,
      d => d.assistive.zoom[0].capture_sha256 = 'b'.repeat(64),
      d => d.assistive.text_spacing[0].layout.clipped.push({element_index:1,tag:'BUTTON'}),
      d => d.assistive.screen_reader.steps[0].dispatcher_output = '',
      d => d.assistive.screen_reader.steps[0].log_end = 0,
      d => d.assistive.screen_reader.steps[1] = structuredClone(d.assistive.screen_reader.steps[0]),
    ];
    for (const mutate of mutations) {
      const bad = structuredClone(source); mutate(bad); writeFileSync(path,JSON.stringify(bad));
      assert.throws(() => auditFile(path,head,viewports));
    }
  } finally { rmSync(directory,{recursive:true,force:true}); }
});

test('mobile navigation cannot omit desktop routes, Tab controls or Escape return', () => {
  const directory=mkdtempSync(join(tmpdir(),'gojet-nav-unit-'));
  try {
    const source=fixture(directory); source.surface='admin';
    // Supplemental capture names bind to the actual surface.
    for(const rows of [source.assistive.zoom, source.assistive.text_spacing]) for(const row of rows) {
      const old=row.capture;row.capture=old.replace('public-','admin-');
      const png=old.includes('zoom200')?Buffer.alloc(24):Buffer.from([137,80,78,71,13,10,26,10]);
      Buffer.from([137,80,78,71,13,10,26,10]).copy(png);if(old.includes('zoom200'))png.writeUInt32BE(1440,16);
      writeFileSync(join(directory,row.capture),png);row.capture_sha256=createHash('sha256').update(png).digest('hex');
    }
    source.assistive.navigation=['light','dark'].map(theme=>{
      const capture=`admin-navigation320-${theme}.png`,png=Buffer.from([137,80,78,71,13,10,26,10]);
      writeFileSync(join(directory,capture),png);
      return {theme,inventory:{count:13,expected:13,same_routes:true,controls:Array.from({length:13},(_,i)=>i)},
        steps:Array.from({length:13},(_,i)=>({element_index:i,inside:true,tag:'A'})),escape_focus_return:true,
        capture,capture_sha256:createHash('sha256').update(png).digest('hex')};
    });
    const path=join(directory,'admin.json');auditAssistive(source,path);
    for(const mutate of [d=>d.assistive.navigation.pop(),d=>d.assistive.navigation[0].inventory.same_routes=false,
      d=>d.assistive.navigation[0].steps.pop(),d=>d.assistive.navigation[0].escape_focus_return=false,
      d=>d.assistive.navigation[0].inventory.count=12,d=>d.assistive.navigation[0].capture_sha256='forged']) {
      const bad=structuredClone(source);mutate(bad);assert.throws(()=>auditAssistive(bad,path));
    }
  } finally {rmSync(directory,{recursive:true,force:true});}
});
