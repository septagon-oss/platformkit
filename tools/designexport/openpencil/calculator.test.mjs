import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { test } from 'node:test'
import { pathToFileURL } from 'node:url'
import { CORE_TOOLS } from '@open-pencil/core/tools'

const require = createRequire(import.meta.resolve('@open-pencil/core/tools'))
const manifestURL = new URL('../package.json', pathToFileURL(require.resolve('expr-eval')))
const manifest = JSON.parse(readFileSync(manifestURL))
const { default: ExprEval } = await import(new URL(manifest.exports?.['.']?.import || manifest.module, manifestURL))
const calc = CORE_TOOLS.find(tool => tool.name === 'calc')
const calculate = expr => calc.execute(null, { expr })

test('the SDK expression dependency resolves to the reviewed published fork', () => {
  assert.equal(manifest.name, 'expr-eval-fork')
  assert.equal(manifest.version, '3.0.3')
  assert.equal(manifest.license, 'MIT')
})

test('the real SDK calculator preserves arithmetic, batches and error results', () => {
  for (const [expr, result] of [
    ['844 - 56 - 96 - 82', 610], ['(952 - 16) / 2', 468],
    ['floor(390 * 0.6)', 234], ['ceil(2.1) + round(2.6)', 6],
    ['min(4, 9) + max(4, 9)', 13], ['abs(-5) + sqrt(16)', 9],
    ['pow(2, 3) + 11 % 4', 11], ['2 ^ 3', 8],
    ['(f(x) = x * x)(5)', 25],
    ['f(x)=x*x; g(x)=x+1; f(2)+g(2)', 7],
  ]) assert.deepEqual(calculate(expr), { expr, result })
  const batch = calculate(JSON.stringify(['7 * 8', '1 / 0', 'sqrt(-1)', 'missing(1)', '1 +']))
  assert.deepEqual(batch.results[0], { expr: '7 * 8', result: 56 })
  for (const failure of batch.results.slice(1)) {
    assert.equal(typeof failure.error, 'string')
    assert.equal(failure.result, undefined)
  }
  assert.deepEqual(calculate('2 + 3'), { expr: '2 + 3', result: 5 })
})

test('expression-defined functions do not escape a calculation or batch item', () => {
  assert.equal(calculate('(f(x) = x + 17)(1)').result, 18)
  for (const expr of ['f(1)', 'lambda_NaN(1)', 'lambda_0(1)', 'lambda_1(1)']) {
    const failure = calculate(expr)
    assert.equal(failure.result, undefined)
    assert.match(failure.error, /undefined variable/)
  }
  const batch = calculate(JSON.stringify(['(f(x) = x + 17)(1)', 'lambda_0(1)']))
  assert.equal(batch.results[0].result, 18)
  assert.match(batch.results[1].error, /undefined variable/)
})

test('reviewed ESM and CommonJS parsers reject unregistered callbacks and prototype access', () => {
  for (const dependency of [ExprEval, require('expr-eval')]) {
    const parser = new dependency.Parser()
    let calls = 0
    const probe = () => { calls++; return 42 }
    const context = { probe, box: { probe } }
    for (const expression of [
      'probe()', 'box.probe()', '(f(x) = box.probe())(1)',
      'box.__proto__', 'box.prototype', 'box.constructor',
    ]) assert.throws(() => parser.evaluate(expression, context), Error, expression)
    assert.equal(calls, 0, 'rejection must happen before invoking a supplied callback')
    assert.equal(parser.evaluate('amount + 3', { amount: 4 }), 7)
    parser.functions.double = value => value * 2
    assert.equal(parser.evaluate('double(5)'), 10, 'explicitly registered functions remain supported')
    assert.equal(parser.evaluate('f(x)=x*x; g(x)=x+1; f(2)+g(2)'), 7)
  }
})
