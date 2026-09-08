import assert from 'node:assert/strict'
import { test } from 'node:test'
import { computedColor } from './computed-color.mjs'

test('computed sRGB paints retain fractional channels and alpha without 8-bit rounding', () => {
  for (const [value, expected] of [
    ['rgb(0, 127.5, 255)', [0, 0.5, 1, 1]],
    ['rgba(255, 0, 127.5, 0.125)', [1, 0, 0.5, 0.125]],
    ['rgb(0% 50% 100% / 25%)', [0, 0.5, 1, 0.25]],
    ['color(srgb 0.284235 0.322275 0.307922)', [0.284235, 0.322275, 0.307922, 1]],
    ['color(srgb .125 37.5% +6.25e-1 / .5)', [0.125, 0.375, 0.625, 0.5]],
    ['color(srgb 0 0 0 / 0)', [0, 0, 0, 0]],
  ]) assert.deepEqual(computedColor(value), Object.fromEntries(['r', 'g', 'b', 'a'].map((key, index) => [key, expected[index]])))
})

test('invalid or unsupported computed paints never silently become black or clipped sRGB', () => {
  for (const value of [
    undefined, null, {}, '', '#123456', 'transparent', 'rgb(1)', 'rgb(1, 2)', 'rgb(1, 2, 3, 4)',
    'rgb(256, 0, 0)', 'rgb(-1, 0, 0)', 'rgb(1%, 2, 3)', 'rgb(0 0 0 / 101%)',
    'color(srgb 0 0)', 'color(srgb 0 0 0 1)', 'color(srgb 0 0 0 /)', 'color(srgb NaN 0 0)',
    'color(srgb 1e309 0 0)', 'color(srgb -0.1 0 0)', 'color(srgb 1.01 0 0)',
    'color(srgb 0 0 0 / -0.1)', 'color(srgb none 0 0)', 'color(display-p3 0.5 0.5 0.5)',
    'color(srgb-linear 0.5 0.5 0.5)', 'color(srgb 0 0 0) garbage', 'rgb(1, 2, 3); color: red',
  ]) assert.throws(() => computedColor(value), /unsupported computed paint/)
})
