import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { buildComponentDocument } from '../document.mjs'
import { captureExample } from './capture.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'
import { chain } from '../exporter-correction.mjs'

test('source grid cells retain width-led ratios, linked content and isolated edits through two saves', async t => {
  const run = await sourceFixture(t, `package main
import (
  "encoding/json"
  "fmt"
  "os"
  g "maragu.dev/gomponents"
  h "maragu.dev/gomponents/html"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
)
type Props struct { Ratio float64 }
func tile(p Props, children ...g.Node) g.Node {
  return h.A(h.Href("/albums/fixture"), h.Style(fmt.Sprintf("display:flex;flex-direction:column;align-items:stretch;justify-content:center;overflow:hidden;aspect-ratio:%g;border:1px solid", p.Ratio)), g.Group(children))
}
func main() {
  var input struct { Ratio float64; Copy string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  var tiles []g.Node
  for i := range 7 {
    copy := "Album"
    if i == 0 { copy = input.Copy }
    text := components.ExampleOf(components.ExampleInfo{ID:"caption", ComponentID:"pk-ui.component.text"}, components.TextProps{Content:copy}, components.Text)
    tiles = append(tiles, components.ExampleWithChildren(components.ExampleInfo{ID:fmt.Sprintf("tile%d", i), ComponentID:"fixture.component.tile"}, Props{input.Ratio}, []g.Node{text.Node}, tile).Node)
  }
  example := components.ExampleWithChildren(components.ExampleInfo{ID:"fixture/grid", ComponentID:"pk-ui.component.grid"}, components.GridProps{Columns:"6", Gap:"3"}, tiles, components.Grid)
  snapshot, err := ui.Export(design.Default(), []components.Example{example})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1)), previous = getTextMeasurer()
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const root = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === 'fixture/grid')
  function matches(graph, node, observed) {
    for (const field of ['width', 'height']) assert.ok(Math.abs(node[field] - observed.bounds[field]) <= 1 / 64,
      `${node.name}/${field}: ${node[field]} versus ${observed.bounds[field]}`)
    for (const [i, child] of observed.children.entries()) {
      if (child.kind !== 'element') continue
      const native = graph.getChildren(node.id)[i]
      assert.equal(native.type, 'INSTANCE')
      for (const field of ['x', 'y']) assert.ok(Math.abs(native[field] - child.bounds[field] + observed.bounds[field]) <= 1 / 64)
      matches(graph, native, child)
    }
  }
  try {
    for (const ratio of [1, 1.5]) for (const mode of ['light', 'dark']) {
      const snapshot = run({ ratio, copy: 'Album' }), options = { examples: ['fixture/grid'], fonts: suppliedFonts([400]), browser, renderer, mode, viewport: { width: 320, height: 900 } }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built
      for (let cycle = 0; cycle < 3; cycle++) {
        setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
        for (const width of [1280, 390, 320]) {
          graph.updateNode(root(graph).id, { width }); computeLayout(graph, root(graph).id)
          matches(graph, root(graph), (await captureExample(browser, snapshot, 'fixture/grid', { ...options, viewport: { width, height: 900 } })).roots[0])
        }
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const caption = graph.getChildren(graph.getChildren(root(graph).id)[0].id)[0]
        const definition = chain(graph, caption, 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === 'content')
        assert.ok(definition, 'the editable property belongs to the canonical linked component')
        const before = structuredClone([...graph.getAllNodes()]), copy = 'The people and places we remember together.'
        editor.setInstanceComponentProperty(caption.id, definition.id, copy)
        matches(graph, root(graph), (await captureExample(browser, run({ ratio, copy }), 'fixture/grid', options)).roots[0])
        for (const node of before.filter(node => node.type === 'COMPONENT')) assert.deepEqual(graph.getNode(node.id), node)
        editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
    }
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
