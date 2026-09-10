import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { buildFoundation } from '../foundation.mjs'
import { sourceFixture } from './fixtures.test.mjs'

test('opt-in source declarations match real flex/stack rendering without changing keyboard order', async t => {
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  c "github.com/septagon-oss/platformkit/ui/components"
  g "maragu.dev/gomponents"
)
func main() {
  var input struct { Stack bool; Props c.FlexProps }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  var children []g.Node
  for _, label := range []string{"Save", "Cancel"} {
    child := c.ExampleOf(c.ExampleInfo{ID:label, ComponentID:"button"}, c.ButtonProps{Label:label}, c.Button)
    children = append(children, child.Node)
  }
  info := c.ExampleInfo{ID:"root", ComponentID:"layout"}
  example := c.ExampleWithChildren(info, input.Props, children, c.Flex)
  if input.Stack { example = c.ExampleWithChildren(info, c.StackProps{Gap:"2", Align:"center"}, children, c.Stack) }
  snapshot, err := ui.ExportWithLayout(design.Default(), []c.Example{example})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const browser = await chromium.launch({ headless: true })
  t.after(() => browser.close())
  const cases = [
    [{ Props: {} }, { direction: 'row', gap: '4', align: 'normal', justify: 'normal', wrap: false }, ['row', '16px', 'normal', 'normal', 'nowrap']],
    [{ Props: { direction: 'column', gap: '2', align: 'end', justify: 'around', wrap: true } },
      { direction: 'col', gap: '2', align: 'end', justify: 'around', wrap: true }, ['column', '8px', 'flex-end', 'space-around', 'wrap']],
    [{ Stack: true }, { direction: 'col', gap: '2', align: 'center', justify: 'normal', wrap: false }, ['column', '8px', 'center', 'normal', 'nowrap']],
  ]
  for (const [input, declared, computed] of cases) {
    const snapshot = source(input), before = structuredClone(snapshot)
    assert.deepEqual(snapshot.examples[0].layout, { kind: 'flex', flex: declared })
    assert.deepEqual(snapshot.requiredFeatures, ['source-flex-declarations.v1', 'source-measurements.v1'])
    const gap = snapshot.measurements.find(m => m.scale === 'spacing' && m.key === declared.gap)
    assert.deepEqual(gap, { scale: 'spacing', key: declared.gap, value: declared.gap === '4' ? 1 : 0.5, unit: 'rem' })
    assert.throws(() => buildFoundation(snapshot), /unsupported snapshot schema/)
    assert.deepEqual(snapshot, before, 'legacy adapter refuses the new contract unchanged')
    for (const width of [320, 1280]) for (const theme of ['light', 'dark']) {
      const page = await browser.newPage({ viewport: { width, height: 900 } })
      try {
        await page.setContent(`<html data-theme="${theme}"><style>${snapshot.css}</style><body>${snapshot.examples[0].html}</body></html>`)
        const root = page.locator('body > div')
        assert.deepEqual(await root.evaluate(node => {
          const s = getComputedStyle(node)
          return [s.flexDirection, s.gap, s.alignItems, s.justifyContent, s.flexWrap]
        }), computed)
        for (const name of ['Save', 'Cancel']) {
          await page.keyboard.press('Tab')
          const button = page.getByRole('button', { name, exact: true })
          assert.ok(await button.evaluate(node => node === document.activeElement && node.matches(':focus-visible')))
          assert.match(await button.ariaSnapshot(), new RegExp(name))
        }
      } finally { await page.close() }
    }
  }
})
