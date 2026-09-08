import assert from 'node:assert/strict'
import { test } from 'node:test'
import { resolveColorExpression } from './color-expression.mjs'

const rgba = (r, g, b, a = 1) => ({ r, g, b, a })
const close = (actual, expected) => {
  for (const key of ['r', 'g', 'b', 'a']) assert.ok(Math.abs(actual[key] - expected[key]) < 1e-12, `${key}: ${actual[key]} != ${expected[key]}`)
}

test('authored sRGB expressions normalize weights and interpolate premultiplied alpha', () => {
  for (const [expression, expected] of [
    ['#1234', rgba(1 / 15, 2 / 15, 3 / 15, 4 / 15)],
    ['#12345680', rgba(18 / 255, 52 / 255, 86 / 255, 128 / 255)],
    ['color-mix(in srgb, #000, #fff)', rgba(.5, .5, .5)],
    ['color-mix(in srgb, #000 78%, #fff)', rgba(.22, .22, .22)],
    ['color-mix(in srgb, #000, 22% #fff)', rgba(.22, .22, .22)],
    ['color-mix(in srgb, #f00 80%, #00f 80%)', rgba(.5, 0, .5)],
    ['color-mix(in srgb, #f00 20%, #00f 30%)', rgba(.4, 0, .6, .5)],
    ['color-mix(in srgb, #f00 55%, transparent)', rgba(1, 0, 0, .55)],
    ['color-mix(in srgb, rgb(255 0 0 / .25), #00f)', rgba(.2, 0, .8, .625)],
    ['color-mix(in srgb, #f000, #00f0)', rgba(0, 0, 0, 0)],
    ['color-mix(in srgb, color-mix(in srgb, #000, #fff), #fff)', rgba(.75, .75, .75)],
  ]) close(resolveColorExpression(expression, () => undefined), expected)
})

test('variable resolution is caller-owned, repeatable and preserves dependency identities', () => {
  const values = new Map([['--role', 'color-mix(in srgb, var(--ink) 78%, var(--paper))'], ['--ink', '#000'], ['--paper', '#fff']])
  const before = structuredClone(values), names = []
  const resolve = name => { names.push(name); return values.get(name) }
  close(resolveColorExpression('var(--role)', resolve), rgba(.22, .22, .22))
  assert.deepEqual(names, ['--role', '--ink', '--paper'])
  assert.deepEqual(values, before)
  const native = Object.freeze(rgba(.25, .5, .75, .5))
  values.set('--ink', native)
  close(resolveColorExpression('var(--role)', resolve), rgba(127 / 244, 83 / 122, 205 / 244, .61))
  assert.notEqual(resolveColorExpression('var(--ink)', resolve), native)
  close(resolveColorExpression('color-mix(in srgb, var(--ink), var(--ink))', resolve), native)
  values.set('--paper', 'var(--role)')
  assert.throws(() => resolveColorExpression('var(--role)', resolve), /cycle/)
})

test('unsupported expressions, missing variables and unbounded recursion refuse explicitly', () => {
  for (const expression of [
    null, '', '#12', 'currentColor', 'red', 'var(--missing)', 'var(--missing, #fff)',
    'color-mix(in oklab, #000, #fff)', 'color-mix(in srgb, #000)',
    'color-mix(in srgb, #000 0%, #fff 0%)', 'color-mix(in srgb, #000 -1%, #fff)',
    'color-mix(in srgb, #000 101%, #fff)', 'color-mix(in srgb, #000, #fff, #f00)',
    'color-mix(in srgb, #000, #fff))', 'color-mix(in srgb, var(--ink), #fff',
    'color-mix(in srgb, #000 calc(50%), #fff)', 'rgb(256 0 0)', { r: NaN, g: 0, b: 0, a: 1 },
  ]) assert.throws(() => resolveColorExpression(expression, () => undefined))
  assert.throws(() => resolveColorExpression('var(--v0)', name => `var(--v${Number(name.slice(3)) + 1})`), /depth/)
})
