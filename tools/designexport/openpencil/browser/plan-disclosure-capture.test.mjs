import assert from 'node:assert/strict'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { captureExample } from './capture.mjs'
import { exportCore } from './fixtures.test.mjs'

const source = exportCore()
const exampleId = 'pk-ui.component.plan-comparison/pending-en'
let browser

before(async () => { browser = await chromium.launch({ headless: true, args: ['--enable-automation'] }) })
after(async () => { await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0, 'each capture releases its context'))

function descendants(nodes) {
  return nodes.flatMap(node => [node, ...descendants(node.children ?? [])])
}

test('a closed plan disclosure does not conceal an unsettled visible summary', async () => {
  const snapshot = structuredClone(source)
  snapshot.css += `
    @keyframes review-summary { from { opacity: .2; } to { opacity: 1; } }
  `
  // The important base-layer motion floor now makes an unlayered animation
  // finite. Inline authorship reaches the infinite-animation refusal, as in
  // capture.test.mjs; the separate motion-floor pin measures both cases.
  const example = snapshot.examples.find(item => item.id === exampleId)
  assert.ok(example?.html.includes('<summary '), 'the real plans contain a native summary')
  example.html = example.html.replaceAll('<summary ',
    '<summary style="animation: review-summary 2s linear infinite !important" ')
  const before = structuredClone(snapshot)
  await assert.rejects(captureExample(browser, snapshot, exampleId),
    /requires finite, running source animations to settle/)
  assert.deepEqual(snapshot, before, 'a refused capture changes no caller-owned source')
})

test('a paused visible summary refuses even beside an invisible spinner', async () => {
  const snapshot = structuredClone(source)
  snapshot.css += `
    @keyframes review-summary { from { opacity: .2; } to { opacity: 1; } }
    details > summary { animation: review-summary 2s linear paused !important; }
  `
  await assert.rejects(captureExample(browser, snapshot, exampleId),
    /requires finite, running source animations to settle/)
})

test('a finite visible summary settles while the closed disclosure stays closed', async () => {
  const baseline = await captureExample(browser, source, exampleId)
  const snapshot = structuredClone(source)
  snapshot.css += `
    @keyframes review-summary { from { opacity: .2; } to { opacity: 1; } }
    details > summary { animation: review-summary 30ms linear forwards !important; }
  `
  const before = structuredClone(snapshot)
  const result = await captureExample(browser, snapshot, exampleId)
  assert.equal(result.exampleId, exampleId)
  const nodes = descendants(result.roots)
  const summaries = nodes.filter(node => node.tag === 'summary')
  assert.equal(summaries.length, 2, 'the real shared component supplies both plan summaries')
  for (const summary of summaries) assert.equal(summary.style.opacity, '1')
  const disclosures = nodes.filter(node => node.tag === 'details')
  assert.equal(disclosures.length, 2)
  assert.deepEqual(disclosures.map(node => node.bounds),
    descendants(baseline.roots).filter(node => node.tag === 'details').map(node => node.bounds),
    'settling a summary does not expand its closed disclosure')
  assert.deepEqual(snapshot, before, 'successful capture changes no caller-owned source')
})
