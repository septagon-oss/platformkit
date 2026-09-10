import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { sourceFixture } from './fixtures.test.mjs'

test('owned scale units, ordered shadows and timing declarations retain browser meaning', async t => {
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/ui/style"
)
func main() {
  var out struct {
    Scales []style.ScaleValue
    Shadows []style.ShadowValue
    Easings []style.EasingValue
    Transitions []style.TransitionValue
    CSS string
  }
  var err error
  out.Scales, err = style.ScaleValues(); if err != nil { panic(err) }
  out.Shadows, err = style.ShadowValues(); if err != nil { panic(err) }
  out.Easings, err = style.EasingValues(); if err != nil { panic(err) }
  out.Transitions, err = style.TransitionValues(); if err != nil { panic(err) }
  classes := []string{"tracking-tighter", "leading-normal", "rounded", "max-w-prose", "max-w-screen", "max-w-full", "w-auto", "hidden"}
  for _, value := range out.Scales {
    if value.Scale == "breakpoint" { classes = append(classes, value.Key+":block") }
  }
  for _, value := range out.Shadows {
    class := "shadow-"+value.Key
    if value.Key == "base" { class = "shadow" }
    classes = append(classes, class)
  }
  for _, value := range out.Easings { classes = append(classes, "ease-"+value.Key) }
  for _, value := range out.Transitions { classes = append(classes, "transition-"+value.Key) }
  sheet, err := style.Rules(classes...); if err != nil { panic(err) }
  out.CSS = sheet.CSS()
  if err := json.NewEncoder(os.Stdout).Encode(out); err != nil { panic(err) }
}
`)
  const fixture = source({}), values = new Map(fixture.Scales.map(v => [`${v.scale}/${v.key}`, v]))
  assert.deepEqual(values.get('tracking/tighter').number, { value: -0.05, unit: 'em' })
  assert.deepEqual(values.get('radius/base').number, { value: 0.25, unit: 'rem' })
  assert.deepEqual(values.get('max-width/prose').number, { value: 65, unit: 'ch' })
  assert.deepEqual(values.get('spacing/auto'), { scale: 'spacing', key: 'auto', keyword: 'auto' })
  assert.deepEqual(fixture.Shadows.find(v => v.key === 'base').layers.map(v => [v.blur.value, v.spread.value]), [[3, 0], [2, -1]])
  const browser = await chromium.launch({ headless: true })
  t.after(() => browser.close())
  const prose = []
  for (const fontSize of [10, 20]) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
    try {
      await page.setContent(`<style>${fixture.CSS} html { font-size:20px } body { margin:0 }
        .probe { font-family:monospace; font-size:${fontSize}px } .container { width:260px } .wide { width:2000px }</style>
        <div class="container">
          <div id="units" class="probe tracking-tighter leading-normal rounded">Source</div>
          <div id="prose" class="probe max-w-prose wide"></div>
          <div id="full" class="max-w-full wide"></div><div id="auto" class="w-auto"></div>
        </div><div id="screen" class="max-w-screen wide"></div>
        <div id="shadow" class="shadow"></div><div id="inset" class="shadow-inner"></div><div id="zero" class="shadow-none"></div>
        <div id="curve" class="ease-in-out"></div><div id="transition" class="transition-colors"></div><div id="none" class="transition-none"></div>`)
      const actual = await page.evaluate(() => {
        const style = id => getComputedStyle(document.getElementById(id))
        const width = id => document.getElementById(id).getBoundingClientRect().width
        return { tracking: style('units').letterSpacing, leading: style('units').lineHeight, radius: style('units').borderRadius,
          prose: width('prose'), full: width('full'), auto: width('auto'), screen: width('screen'),
          shadow: style('shadow').boxShadow, inset: style('inset').boxShadow, zero: style('zero').boxShadow,
          curve: style('curve').transitionTimingFunction, duration: style('transition').transitionDuration,
          properties: style('transition').transitionProperty, none: style('none').transitionDuration }
      })
      assert.equal(actual.tracking, `${-0.05 * fontSize}px`)
      assert.equal(actual.leading, `${1.5 * fontSize}px`)
      assert.equal(actual.radius, '5px', 'rem follows the root, not the component font size')
      assert.equal(actual.full, 260); assert.equal(actual.auto, 260); assert.equal(actual.screen, 1280)
      assert.equal(actual.shadow, 'rgba(0, 0, 0, 0.1) 0px 1px 3px 0px, rgba(0, 0, 0, 0.1) 0px 1px 2px -1px')
      assert.equal(actual.inset, 'rgba(0, 0, 0, 0.05) 0px 2px 4px 0px inset')
      assert.equal(actual.zero, 'rgba(0, 0, 0, 0) 0px 0px 0px 0px')
      assert.equal(actual.curve, 'cubic-bezier(0.4, 0, 0.2, 1)'); assert.equal(actual.duration, '0.15s')
      assert.equal(actual.properties, fixture.Transitions.find(v => v.key === 'colors').properties.join(', '))
      assert.equal(actual.none, '0s', 'absent timing declarations retain the browser initial value')
      prose.push(actual.prose)
    } finally { await page.close() }
  }
  assert.ok(Math.abs(prose[1] - 2 * prose[0]) < 0.02, 'ch width must follow the font, not a baked pixel value')
  for (const [key, pixels] of [['sm', 640], ['md', 768], ['lg', 1024], ['xl', 1280], ['2xl', 1536]]) {
    assert.deepEqual(values.get(`breakpoint/${key}`).number, { value: pixels, unit: 'px' })
    const page = await browser.newPage()
    try {
      await page.setContent(`<style>${fixture.CSS}</style><div id="breakpoint" class="hidden ${key}:block">Responsive</div>`)
      for (const [width, display] of [[pixels - 1, 'none'], [pixels, 'block']]) {
        await page.setViewportSize({ width, height: 900 })
        const actual = await page.locator('#breakpoint').evaluate((node, pixels) => ({
          display: getComputedStyle(node).display, width: innerWidth, matches: matchMedia(`(min-width: ${pixels}px)`).matches,
        }), pixels)
        assert.deepEqual(actual, { display, width, matches: width >= pixels }, `${key} at ${width}px`)
      }
    } finally { await page.close() }
  }
})
