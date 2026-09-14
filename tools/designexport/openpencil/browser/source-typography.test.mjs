import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { sourceFixture } from './fixtures.test.mjs'

test('source fallback order and literal/generic distinction agree with both CSS theme modes', async t => {
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"; "github.com/septagon-oss/platformkit/ui/style"
)
func main() {
  var input struct { Light, Dark string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  pair := design.Default()
  pair.Light.Typography.Body, pair.Dark.Typography.Body = input.Light, input.Dark
  var out struct { Light, Dark []design.FontFamilyToken; CSS string }
  var err error
  out.Light, err = pair.Light.FontFamilies(); if err != nil { panic(err) }
  out.Dark, err = pair.Dark.FontFamilies(); if err != nil { panic(err) }
  out.CSS = style.ThemeVars(pair.Light, pair.Dark).CSS()
  if err := json.NewEncoder(os.Stdout).Encode(out); err != nil { panic(err) }
}
`)
  const fixture = source({ Light: `"Example, Sans", 'serif', SeRiF`, Dark: 'Example  \tSans, sans-serif' })
  assert.deepEqual(fixture.Light.find(v => v.name === '--pk-font-body').families,
    [{ name: 'Example, Sans' }, { name: 'serif' }, { name: 'serif', generic: true }])
  assert.deepEqual(fixture.Dark.find(v => v.name === '--pk-font-body').families,
    [{ name: 'Example Sans' }, { name: 'sans-serif', generic: true }])
  const browser = await chromium.launch({ headless: true })
  t.after(() => browser.close())
  const page = await browser.newPage()
  await page.setContent(`<style>${fixture.CSS} #probe { font-family:var(--pk-font-body) }</style><p id="probe">Source typography</p>`)
  for (const [mode, expected] of [['light', '"Example, Sans", "serif", serif'], ['dark', '"Example Sans", sans-serif']]) {
    await page.evaluate(mode => { document.documentElement.dataset.theme = mode }, mode)
    assert.equal(await page.locator('#probe').evaluate(node => getComputedStyle(node).fontFamily), expected)
  }
  await page.evaluate(() => { delete document.documentElement.dataset.theme })
  await page.emulateMedia({ colorScheme: 'dark' })
  assert.equal(await page.locator('#probe').evaluate(node => getComputedStyle(node).fontFamily), '"Example Sans", sans-serif')
})
