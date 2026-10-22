import assert from 'node:assert/strict';
import { refusals, notes } from './design_floor.mjs';
const clean = () => ({width: 1440, left_edges: [0, 24], body_font_sizes: [14, 16],
  all_font_sizes: [12,14,16,18,20,24], primary_ctas_above_fold: 1,
  worst_contrast: {ratio: 4.5, size: 23, text: 'Readable'}, section_gaps: [0,8,16,24],
  measures_ch: {max: 75}, scroll_width_overflow: false,
  stylesheet_hex: [{href: '/client.css', defines: 19, hex: 0}]});
assert.deepEqual(refusals(clean()), []);
for (const [field, value, why] of [
  ['left_edges', [0,24,48], 'left edges'],
  ['body_font_sizes', [12,14,16], 'body text'],
  ['all_font_sizes', [12,14,16,18,20,24,32], 'font sizes'],
  ['primary_ctas_above_fold', 2, 'primary buttons'],
  ['worst_contrast', {ratio: 4.49, size: 23, text: 'Dim'}, 'contrast'],
  ['section_gaps', [0,4,8,16,24], 'section gaps'],
  ['measures_ch', {max: 76}, 'body measure'],
  ['scroll_width_overflow', true, 'sideways'],
  ['stylesheet_hex', [{href: '/client.css', defines: 19, hex: 1}], 'raw hex'],
]) {
  const failed = refusals({...clean(), [field]: value});
  assert.equal(failed.length, 1, field);
  assert.ok(failed[0].includes(why), field);
}
assert.deepEqual(refusals({...clean(), width: 390, measures_ch: {max: 76}}), []);
assert.equal(refusals({...clean(), width: 1280, measures_ch: {max: 76}}).length, 1);
assert.deepEqual(refusals({...clean(), worst_contrast: {ratio: 4.49, size: 24}}), []);
assert.deepEqual(refusals({...clean(), stylesheet_hex: [{href: '/tokens.css', defines: 20, hex: 1}]}), []);
assert.deepEqual(refusals({...clean(), stylesheet_hex: [{href: '/missing.css', error: 'HTTP 404'}]}), []);
assert.deepEqual(notes({...clean(), stylesheet_hex: [{href: '/missing.css', error: 'HTTP 404'}]}), ['1440px: could not read /missing.css: HTTP 404']);
assert.equal(refusals({...clean(), stylesheet_hex: [{href: '/client.css', hex: 1}]}).length, 1);
console.log('nine design-floor boundaries and exemptions pass');
