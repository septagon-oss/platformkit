import assert from 'node:assert/strict'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { captureExample } from './capture.mjs'
import { exportCore } from './fixtures.test.mjs'

// The settling wait answers two opposite questions with one sentence, and this file
// is the reason they stay opposite. Run 56095 job 56717 refused `pk-ui.component.detail-sheet/pending-pt-PT`
// with `Capture source animation settling timed out`, and job 56693 of the same
// workflow on another task's head refused `pk-ui.component.product-card/loading-en`
// with those same five words. A bound that runs out because the source is still
// animating and a bound that runs out because the page was never given an animation
// frame are not the same event and do not want the same fix, but the log could hold
// no difference between them. What the refusal now prints is asserted here, in the
// same breath: which example, and which animation, at what point of its own timing.
//
// The witnesses are inline `!important` declarations, which the directory already
// uses for the same reason in motion-floor-invisible-content.test.mjs: the supplied
// stylesheet's reduced-motion floor rewrites any animation the source does not hold
// `!important`, so an animation declared without it would be floored to one iteration
// of a hundredth of a millisecond and settle, which is the floor working and not this
// question being asked.
const source = exportCore()
const pending = 'pk-ui.component.detail-sheet/pending-pt-PT'
let browser
before(async () => { browser = await chromium.launch({ headless: true, args: ['--enable-automation'] }) })
after(async () => { await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0, 'a capture releases its context whichever way it answers'))

function animating(animation) {
  const snapshot = structuredClone(source)
  snapshot.css += '@keyframes settling-witness { from { opacity: .2; } to { opacity: 1; } }'
  const example = snapshot.examples.find(item => item.id === pending)
  assert.ok(example, 'the real Gallery supplies the pending detail sheet')
  example.html = example.html.replace(/<span ([^>]*role="status"[^>]*)>/,
    `<span style="animation: ${animation} !important" $1>`)
  assert.notEqual(example.html, source.examples.find(item => item.id === pending).html,
    'the witness really reaches the pending status element')
  return snapshot
}

test('a capture that runs out of its bound names the source animation it was waiting on', async () => {
  const before = structuredClone(source)
  await assert.rejects(captureExample(browser, animating('settling-witness 4s linear 1'), pending), error => {
    const line = error.message.split('\n')[0]
    assert.match(line, /^page\.evaluate: Error: Capture source animation settling timed out on \S*pending-pt-PT/,
      `the refusal does not name the example it ran out on: ${line}`)
    assert.match(line, /settling-witness on span/, `the refusal names no animation: ${line}`)
    assert.match(line, /playState=running/, `the animation's own state is missing: ${line}`)
    assert.match(line, /startTime=\d/, 'a frame was produced and this animation is genuinely still running')
    assert.match(line, /endTime=4000/, `the bound is not being read against the effect's own end: ${line}`)
    assert.match(line, /document's own timeline is at \d+ms/, `no timeline reading: ${line}`)
    return true
  })
  assert.deepEqual(source, before, 'a refused capture changes no caller-owned source')
})

test('a capture that refuses an animation that cannot end still refuses it, by name', async () => {
  const before = structuredClone(source)
  await assert.rejects(captureExample(browser, animating('settling-witness 2s linear infinite'), pending), error => {
    const line = error.message.split('\n')[0]
    assert.match(line, /Capture requires finite, running source animations to settle/,
      'the source-owned refusal must stay the sentence it has always been')
    assert.match(line, /settling-witness on span/, `the refusal names no animation: ${line}`)
    assert.match(line, /endTime=Infinity/, `an endless animation is not reported as finite: ${line}`)
    return true
  })
  assert.deepEqual(source, before, 'a refused capture changes no caller-owned source')
})

test('the pending detail sheet the two refusals met still settles', async () => {
  const observation = await captureExample(browser, source, pending)
  assert.equal(observation.exampleId, pending, 'the example the captures lost is an example as it was')
})
