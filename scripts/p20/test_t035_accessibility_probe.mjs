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
