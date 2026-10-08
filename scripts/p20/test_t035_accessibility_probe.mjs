import test from 'node:test';
import assert from 'node:assert/strict';
import { auditVerdict, AXE_VERSION } from './t035_accessibility_probe.mjs';

const fixture = () => ({axe: {version: AXE_VERSION, passes: ['label'], violations: [], incomplete: []},
  viewport: {width: 320}, layout: {scroll_width: 320, reduced_motion: true, active_animations: 0,
    positive_tabindex: 0, notices: [{has_text: true, role: 'alert'}]},
  keyboard: [{visible_indicator: true, in_view: true, unobscured: true}]});

test('unresolved or unexecuted axe scans cannot become a clean diagnostic', () => {
  assert.ok(Object.values(auditVerdict(fixture())).every(Boolean));
  for (const key of ['violations', 'incomplete']) {
    const row = fixture(); row.axe[key].push({id: 'color-contrast'});
    assert.ok(Object.values(auditVerdict(row)).some(x => !x));
  }
  const row = fixture(); row.axe.passes = [];
  assert.equal(auditVerdict(row).axe_executed, false);
});
test('raw overflow, motion, keyboard obstruction and silent messages fail despite clean axe', () => {
  for (const mutate of [
    r => r.layout.scroll_width = 340,
    r => r.layout.active_animations = 1,
    r => r.layout.reduced_motion = false,
    r => r.layout.positive_tabindex = 1,
    r => r.layout.notices[0].has_text = false,
    r => r.layout.notices[0].role = null,
    r => r.keyboard = [],
    r => r.keyboard[0].visible_indicator = false,
    r => r.keyboard[0].in_view = false,
    r => r.keyboard[0].unobscured = false,
  ]) {
    const row = fixture(); mutate(row);
    assert.ok(Object.values(auditVerdict(row)).some(x => !x));
  }
});

const { textareaContrast, unresolvedAxeRules, fragmentsUnobscured } = await import('./t035_accessibility_probe.mjs');
const textarea = () => ({tag: 'TEXTAREA', checks: [{id: 'color-contrast'}], computed: {
  foreground: 'rgb(0, 0, 0)', background: 'rgb(255, 255, 255)', has_value: true,
  text_fill: 'rgb(0, 0, 0)', text_shadow: 'none', text_stroke_width: '0px',
  ancestors: [{opacity: '1', background_image: false, filter: 'none', blend: 'normal'}],
}});
test('independent textarea contrast rejects low contrast and unsupported compositing', () => {
  assert.equal(textareaContrast(textarea()).ratio, 21);
  const low = textarea(); low.computed.foreground = low.computed.text_fill = 'rgb(119, 119, 119)';
  assert.ok(textareaContrast(low).ratio < 4.5);
  assert.equal(textareaContrast(low).pass, false);
  for (const mutate of [n => n.tag = 'DIV', n => n.computed.has_value = false,
    n => n.computed.background = 'rgba(255, 255, 255, 0.5)',
    n => n.computed.text_fill = 'rgb(255, 255, 255)',
    n => n.computed.text_shadow = 'rgb(0, 0, 0) 1px 1px',
    n => n.computed.text_stroke_width = '1px',
    n => n.computed.ancestors = [], n => n.computed.ancestors[0].opacity = '0.5',
    n => n.computed.ancestors[0].background_image = true,
    n => n.computed.ancestors[0].filter = 'blur(1px)',
    n => n.computed.ancestors[0].blend = 'multiply']) {
    const n = textarea(); mutate(n); assert.equal(textareaContrast(n), null);
  }
  const rule = {id: 'color-contrast', nodes: [textarea()]};
  assert.equal(unresolvedAxeRules({incomplete: [rule]}).length, 0);
  assert.equal(rule.nodes.length, 1); // original evidence is retained
  rule.nodes.push(low);
  assert.equal(unresolvedAxeRules({incomplete: [rule]}).length, 1);
  assert.equal(unresolvedAxeRules({incomplete: [{id: 'other', nodes: [textarea()]}]}).length, 1);
});
test('wrapped focus requires every actual fragment visible and unobstructed', () => {
  const visible = {width: 77, height: 17, in_view: true, unobscured: true};
  assert.equal(fragmentsUnobscured([visible, {...visible, width: 51}]), true);
  assert.equal(fragmentsUnobscured([]), false);
  for (const bad of [{width: 0}, {height: 0}, {in_view: false}, {unobscured: false}]) {
    assert.equal(fragmentsUnobscured([visible, {...visible, ...bad}]), false);
  }
});
