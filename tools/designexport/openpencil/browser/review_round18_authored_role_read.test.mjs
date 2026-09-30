// Review round 18 (platformkit, T-0108): the design tool decides whether a custom
// property is an *authored* definition from where its rule sits, and `8809a57` made
// that rule "a cascade layer states no condition, a media or supports block does".
// Nothing in the suite says it: the cases that pass today reach their formulas at a
// top-level `:root` (an unlayered rule appended to the sheet), so a descent that
// swallowed conditional blocks, or one that stopped descending at layers, would keep
// them green. Both cases here go through captureExample, so they judge the read in
// browser/capture.mjs rather than a copy of it.
import assert from 'node:assert/strict'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { captureExample } from './capture.mjs'
import { exportCore } from './fixtures.test.mjs'

const primary = 'pk-ui.component.button/primary'
const accent = '--pk-color-accent-default', surface = '--pk-color-surface-primary'
const value = `color-mix(in srgb, var(${accent}) 27%, var(${surface}))`
const source = exportCore()
let browser
before(async () => { browser = await chromium.launch({ headless: true, args: ['--enable-automation'] }) })
after(async () => { await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0, 'capture closed every disposable context'))

function paintOf(stylesheet) {
  const snapshot = structuredClone(source)
  snapshot.css += stylesheet
  return captureExample(browser, snapshot, primary, { mode: 'light' })
    .then(result => result.roots[0].paintSources['background-color'])
}

// A layer states no condition, so a :root formula reached through @layer is the
// authored definition and capture has to claim it. Before the descent was fixed, the
// same sheet came back with no expressionCandidate at all.
test('review round 18: an authored formula inside a cascade layer is still authored', async () => {
  const paint = await paintOf(`
    @layer client { :root { --product-tint: ${value}; } }
    [data-component="button"] { background-color: var(--product-tint); }`)
  assert.deepEqual(paint.expressionCandidate, { customProperty: '--product-tint', value, customProperties: {} })
})

// The same formula, one level deeper, is no longer authored: the media block states a
// condition on the rule it carries, and descending through a layer must not carry the
// caller past that. Claiming it would put a value the page may never compute into the
// snapshot as the definition of the paint.
test('review round 18: a formula authored only inside a media block stays unclaimed, inside a layer or out', async () => {
  for (const stylesheet of [
    `@media (min-width: 1px) { :root { --product-tint: ${value}; } }`,
    `@layer client { @media (min-width: 1px) { :root { --product-tint: ${value}; } } }`,
    `@layer client { @supports (color: color-mix(in srgb, red, blue)) { :root { --product-tint: ${value}; } } }`,
  ].map(block => `${block}\n[data-component="button"] { background-color: var(--product-tint); }`)) {
    const paint = await paintOf(stylesheet)
    assert.equal(paint.expressionCandidate, undefined, stylesheet)
    assert.equal(paint.directCandidate, null, stylesheet)
  }
})
