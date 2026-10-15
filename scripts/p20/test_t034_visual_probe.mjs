import test from 'node:test';
import assert from 'node:assert/strict';
import { canonicalColor } from './t034_visual_probe.mjs';
test('equivalent minified hex values compare equally; missing or different colors do not', () => {
  assert.equal(canonicalColor('#fff'), canonicalColor('#FFFFFF'));
  assert.equal(canonicalColor(' #AbC '), '#aabbcc');
  assert.notEqual(canonicalColor(''), canonicalColor('#fff'));
  assert.notEqual(canonicalColor('#070b14'), canonicalColor('#f7f9fc'));
});
