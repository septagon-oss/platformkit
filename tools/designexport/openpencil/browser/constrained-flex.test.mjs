import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { captureExample } from './capture.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'

// A schema-generated form caps the column its controls fill: `width: 100%` beside a
// maximum written in the scale's own unit. The cap is a fact about the box the person
// sees — it is why the form stops mid-page on a wide screen — so the projection carries
// it rather than flattening it into the width one viewport happened to measure. These
// cases observe the same generated form at a parent narrower than the cap and at one
// wider, so the conversion is asked at the width where the parent wins and at the width
// where the cap does, and then after the round trip that a designer actually opens.
const cappedFormFixture = t => sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/kit/crud"
  "github.com/septagon-oss/platformkit/kit/httpx"
  "github.com/septagon-oss/platformkit/ui"; "github.com/septagon-oss/platformkit/ui/export"
  "github.com/septagon-oss/platformkit/ui/components/examples"
  "github.com/septagon-oss/platformkit/ui/css"
  "github.com/septagon-oss/platformkit/ui/screens"
)
type Note struct {
  crud.Base
  Title string \`json:"title" validate:"required"\`
  Description string \`json:"description" ui:"widget:textarea"\`
}
func (Note) TableName() string { return "notes" }
func main() {
  var input struct{ Constraints [][2]string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  resource := httpx.Resource{Module: "notes", Entity: "note", Path: "/notes", Screen: "/app/notes/notes", Schema: crud.Schema{Fields: crud.Fields[*Note]()}}
  form := screens.FormExample("fixture/capped-form", resource, screens.Options{Workspace: "/app"}, "/app/notes/notes", "New note", nil, nil, "", true)
  sheet := css.NewSheet()
  for _, declaration := range input.Constraints {
    sheet.Select("form", css.Decl(declaration[0], css.Literal(declaration[1])))
  }
  snapshot, err := export.Export(design.Default(), []examples.Example{form}, ui.Extra{Sheets: []*css.Sheet{sheet}})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)

const id = 'fixture/capped-form', cap = 384, close = (actual, expected, label) =>
  assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)

test('a generated form keeps its capped column in the projection, at both sides of the cap and after the save', async t => {
  const run = await cappedFormFixture(t)
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const fonts = suppliedFonts([400, 500, 600]), previous = getTextMeasurer()
  let renderer
  try {
    const ck = await initCanvasKit()
    renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
    setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
    const snapshot = run({})
    for (const width of [320, 1440]) {
      const viewport = { width, height: 900 }
      const observed = (await captureExample(browser, snapshot, id, { viewport, fonts })).roots[0]
      assert.equal(observed.style.display, 'flex', 'the capped box is a flex composition, not a paragraph')
      assert.equal(observed.sizing['max-width'], `${cap}px`, 'the source box carries the scale cap')
      // The parent narrower than the cap leaves the form to stretch; the parent wider
      // than it is the one where the cap is the whole of the form's width.
      close(observed.bounds.width, Math.min(width, cap), `${width}px parent: the source form's own box`)
      const built = await buildComponentDocument(snapshot, { examples: [id], fonts, browser, renderer, viewport })
      const master = built.selections[0].master
      assert.equal(master.maxWidth, cap, `${width}px parent: the projection lost the form's cap`)
      close(master.width, observed.bounds.width, `${width}px parent: projected form width`)
      close(master.height, observed.bounds.height, `${width}px parent: projected form height`)
      for (const child of built.graph.getChildren(master.id)) {
        assert.ok(child.width <= cap, `${child.name} escapes its capped column: ${child.width}`)
      }
      if (width > cap) {
        const round = await parseFigFile((await exportFigFile(built.graph)).slice().buffer, { populate: 'all' })
        const saved = [...round.getAllNodes()].find(node => node.name === id && node.type === 'COMPONENT')
        assert.ok(saved, 'the generated form survives the save')
        close(saved.width, cap, 'saved form width')
        assert.equal(saved.maxWidth, cap, 'the saved form keeps its cap')
      }
    }
  } finally {
    renderer?.destroy(); setTextMeasurer(previous); await browser.close()
  }
})

// The admission gained a flex box, not a licence: every constraint shape the block case
// refuses still has to be refused here, or the cap above would stand on a box whose
// sizing the projection cannot answer for.
test('a capped flex form in a sizing the projection cannot answer for is still refused', async t => {
  const run = await cappedFormFixture(t)
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const fonts = suppliedFonts([400, 500, 600]), previous = getTextMeasurer()
  let renderer
  try {
    const ck = await initCanvasKit()
    renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
    setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
    for (const constraints of [
      [['max-width', '50%']],
      [['max-width', 'fit-content']],
      [['max-width', `${cap}px`], ['box-sizing', 'content-box']],
      [['max-width', `${cap}px`], ['max-height', '100px']],
      [['max-width', `${cap}px`], ['min-width', '20px']],
    ]) {
      await assert.rejects(buildComponentDocument(run({ constraints }), { examples: [id], fonts, browser, renderer }),
        /composition constrained sizing/, `${JSON.stringify(constraints)} must not lose its sizing semantics`)
    }
  } finally {
    renderer?.destroy(); setTextMeasurer(previous); await browser.close()
  }
})
