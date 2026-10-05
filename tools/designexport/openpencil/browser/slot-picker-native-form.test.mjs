import assert from 'node:assert/strict'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { exportCore } from './fixtures.test.mjs'

const source = exportCore()
let browser
before(async () => { browser = await chromium.launch({ headless: true }) })
after(async () => { await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0))

for (const language of ['en', 'pt-PT']) for (const mode of ['light', 'dark']) {
  test(`native slot forms submit only eligible choices without enhancement (${language}, ${mode})`, async () => {
    const original = structuredClone(source)
    const context = await browser.newContext({ javaScriptEnabled: false, colorScheme: mode })
    try {
      const page = await context.newPage()
      for (const state of ['booked', 'full', 'unavailable', 'past', 'stale', 'confirmation-pending']) {
        const id = `pk-ui.component.slot-picker/${state}-${language}`
        const example = source.examples.find(item => item.id === id)
        assert.ok(example, `the Gallery supplies ${id}`)
        await page.setContent(`<!doctype html><html lang="${language}" data-theme="${mode}"><head><style>${source.css}</style></head><body><form id="booking" method="post">${example.html}</form></body></html>`)
        const radios = page.locator('input[type="radio"]')
        assert.equal(await radios.count(), 2, 'reach the native choices through their input elements')
        assert.equal(await radios.first().isDisabled(), true, `${state} keeps the first choice unavailable`)
        assert.equal(await radios.first().isChecked(), false)
        assert.deepEqual(await page.locator('#booking').evaluate(form => [...new FormData(form).entries()]), [])
        const late = radios.nth(1)
        if (state === 'stale' || state === 'confirmation-pending') {
          assert.equal(await late.isDisabled(), true, 'an unsettled snapshot supplies no selectable radio')
        } else {
          assert.equal(await late.isEnabled(), true, 'another eligible choice remains usable')
          await late.focus()
          await page.keyboard.press('Space')
          assert.equal(await late.isChecked(), true)
          assert.deepEqual(await page.locator('#booking').evaluate(form => [...new FormData(form).entries()]), [['slot', 'late']])
        }
      }
      assert.deepEqual(source, original, 'rendering forms keeps caller-owned examples unchanged')
    } finally { await context.close() }
  })
}
