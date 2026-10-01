import assert from 'node:assert/strict'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { captureExample } from './capture.mjs'
import { exportCore } from './fixtures.test.mjs'

const source = exportCore()
let browser
before(async () => { browser = await chromium.launch({ headless: true, args: ['--enable-automation'] }) })
after(async () => { await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0, 'observations and captures release their contexts'))

function animatedSummary(exampleId, inline) {
  const snapshot = structuredClone(source)
  snapshot.css += '@keyframes review-summary { from { opacity: .2; } to { opacity: 1; } }'
  const animation = 'review-summary 2s linear infinite !important'
  const example = snapshot.examples.find(item => item.id === exampleId)
  assert.ok(example, 'the real Gallery supplies the selected plan comparison')
  // Keep the closed pending animation genuinely infinite under reduced motion
  // too, so ignoring invisible content is tested independently of the CSS floor.
  example.html = example.html.replace(/<span ([^>]*role="status"[^>]*)>/g,
    `<span style="animation: ${animation}" $1>`)
  if (inline) {
    example.html = example.html.replaceAll('<summary ', `<summary style="animation: ${animation}" `)
  } else {
    snapshot.css += `details > summary { animation: ${animation}; }`
  }
  return snapshot
}

async function observe(snapshot, exampleId, mode, reducedMotion) {
  const context = await browser.newContext({ colorScheme: mode, reducedMotion })
  try {
    const page = await context.newPage()
    const example = snapshot.examples.find(item => item.id === exampleId)
    await page.setContent(`<!doctype html><html data-theme="${mode}"><head><style>${snapshot.css}</style></head><body>${example.html}</body></html>`)
    return await page.evaluate(() => ({
      summaries: [...document.querySelectorAll('summary')].map(summary => ({
        visible: summary.checkVisibility(),
        duration: getComputedStyle(summary).animationDuration,
        iterations: getComputedStyle(summary).animationIterationCount,
      })),
      open: document.querySelectorAll('details[open]').length,
      hiddenSpinners: [...document.querySelectorAll('[data-loading] [role="status"]')].filter(node => !node.checkVisibility()).length,
      hiddenInfiniteSpinners: [...document.querySelectorAll('[data-loading] [role="status"]')].filter(node => !node.checkVisibility() && getComputedStyle(node).animationIterationCount === 'infinite').length,
    }))
  } finally { await context.close() }
}

for (const language of ['en', 'pt-PT']) for (const mode of ['light', 'dark']) {
  const exampleId = `pk-ui.component.plan-comparison/pending-${language}`

  test(`review 7: reduced motion settles a stylesheet animation beside closed plan content (${language}, ${mode})`, async () => {
    const snapshot = animatedSummary(exampleId, false)
    const beforeSnapshot = structuredClone(snapshot)
    const ordinary = await observe(snapshot, exampleId, mode, 'no-preference')
    assert.ok(ordinary.summaries.length > 0, 'reach the visible plan summaries through their native element')
    assert.ok(ordinary.summaries.every(item => item.visible && item.iterations === 'infinite'))
    assert.equal(ordinary.open, 0)
    assert.ok(ordinary.hiddenSpinners > 0, 'the real pending plan retains a spinner in its closed disclosure')
    const reduced = await observe(snapshot, exampleId, mode, 'reduce')
    assert.ok(reduced.hiddenInfiniteSpinners > 0, 'the invisible spinner remains infinite despite the motion floor')
    assert.equal(reduced.summaries.length, ordinary.summaries.length)
    assert.ok(reduced.summaries.every(item => item.visible && item.iterations === '1' && parseFloat(item.duration) <= 0.00001), JSON.stringify(reduced))
    const result = await captureExample(browser, snapshot, exampleId, { mode })
    assert.equal(result.exampleId, exampleId)
    assert.deepEqual(snapshot, beforeSnapshot, 'settling does not change caller-owned source')
  })

  test(`review 7: a truly infinite visible plan summary still refuses capture (${language}, ${mode})`, async () => {
    const snapshot = animatedSummary(exampleId, true)
    const beforeSnapshot = structuredClone(snapshot)
    const measured = await observe(snapshot, exampleId, mode, 'reduce')
    assert.ok(measured.summaries.length > 0, 'the native summaries are present before testing capture')
    assert.ok(measured.summaries.every(item => item.visible && item.iterations === 'infinite' && item.duration === '2s'), JSON.stringify(measured))
    assert.equal(measured.open, 0)
    assert.ok(measured.hiddenSpinners > 0)
    assert.ok(measured.hiddenInfiniteSpinners > 0)
    await assert.rejects(captureExample(browser, snapshot, exampleId, { mode }), /requires finite, running source animations to settle/)
    assert.deepEqual(snapshot, beforeSnapshot, 'a refused capture changes no caller-owned source')
  })
}
