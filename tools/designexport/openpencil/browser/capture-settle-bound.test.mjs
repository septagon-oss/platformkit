import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { chromium } from 'playwright'
import { captureExample } from './capture.mjs'
import { exportCore } from './fixtures.test.mjs'

const primary = 'pk-ui.component.button/primary'
const source = exportCore()
let browser
before(async () => { browser = await chromium.launch({ headless: true, args: ['--enable-automation'] }) })
after(async () => { await browser?.close() })

// The settle wait is bounded per wave, so a slow finite animation is waited for. The bound
// itself must survive: a finite animation that outlasts one wave is still refused, rather
// than captured mid-flight or waited for without end.
test('browser capture refuses a finite source animation that outlasts one wave', async () => {
  const snapshot = structuredClone(source)
  const example = snapshot.examples.find(item => item.id === primary)
  example.html = example.html.replace(/^<button /, '<button style="animation: pk-spin 30s linear 1 !important" ')
  const beforeSnapshot = structuredClone(snapshot)
  const started = Date.now()
  await assert.rejects(captureExample(browser, snapshot, primary), /source animation settling timed out/)
  assert.ok(Date.now() - started < 25000, 'the refusal arrives inside one wave, not after the animation ends')
  assert.deepEqual(snapshot, beforeSnapshot)
  assert.equal(browser.contexts().length, 0)
})
