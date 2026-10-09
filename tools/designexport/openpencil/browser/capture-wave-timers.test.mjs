import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { captureExample } from './capture.mjs'
import { exportCore } from './fixtures.test.mjs'

test('capture clears every completed wave timer before closing its context', async () => {
  const snapshot = exportCore()
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation'] })
  let timers
  // Observe the browser boundary without admitting script into exported examples.
  const observedBrowser = {
    async newContext(options) {
      const context = await browser.newContext(options)
      const newPage = context.newPage.bind(context)
      const close = context.close.bind(context)
      context.newPage = async () => {
        const page = await newPage()
        const setContent = page.setContent.bind(page)
        page.setContent = async (...args) => {
          await setContent(...args)
          await page.evaluate(() => {
            const pending = new Set()
            const schedule = window.setTimeout.bind(window)
            const cancel = window.clearTimeout.bind(window)
            window.waveTimers = { created: 0, pending }
            window.setTimeout = (callback, delay, ...args) => {
              const id = schedule(callback, delay, ...args)
              if (delay === 5000) { pending.add(id); window.waveTimers.created++ }
              return id
            }
            window.clearTimeout = id => { pending.delete(id); cancel(id) }
            const animations = document.getAnimations.bind(document)
            let started = false
            document.getAnimations = () => {
              if (!started && document.querySelector('button')) {
                started = true
                const button = document.querySelector('button')
                const first = button.animate([{ opacity: 0.5 }, { opacity: 1 }], 100)
                first.finished.then(() => button.animate([{ opacity: 0.5 }, { opacity: 1 }], 100))
              }
              return animations()
            }
          })
        }
        context.close = async () => {
          try {
            timers = await page.evaluate(() => ({ created: window.waveTimers.created, pending: window.waveTimers.pending.size }))
          } finally { await close() }
        }
        return page
      }
      return context
    },
  }
  try {
    await captureExample(observedBrowser, snapshot, 'pk-ui.component.button/primary')
    assert.ok(timers.created >= 2, 'two real animation waves reached the settle loop')
    assert.equal(timers.pending, 0, 'every wave cleared its timeout before context disposal')
    assert.equal(browser.contexts().length, 0)
  } finally { await browser.close() }
})
