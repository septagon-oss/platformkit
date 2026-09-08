import assert from 'node:assert/strict'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'

test('Toolbar owns its private copy while linked actions, edits and two saves retain source identity', async t => {
  const id = 'fixture/toolbar'
  // Use a source-composed, licensed test font for headings. This is not proof
  // of the default display-font fallback stack or Collect's supplied Georgia.
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
  "github.com/septagon-oss/platformkit/ui/css"
)
func main() {
  var props components.ToolbarProps
  if err := json.NewDecoder(os.Stdin).Decode(&props); err != nil { panic(err) }
  action := components.ExampleOf(components.ExampleInfo{ID: "action", ComponentID: "pk-ui.component.button"},
    components.ButtonProps{Label: "Create album", Href: "/albums/new"}, components.Button)
  example := components.ExampleWithChildren(components.ExampleInfo{ID: "fixture/toolbar", ComponentID: "pk-ui.component.toolbar"},
    props, []g.Node{action.Node}, components.Toolbar)
  heading := css.NewSheet().Select("h1.font-serif", css.Decl("font-family", css.Literal(design.FontBody)))
  snapshot, err := ui.Export(design.Default(), []components.Example{example}, ui.Extra{Sheets: []*css.Sheet{heading}})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const fonts = suppliedFonts([400, 600])
  const original = { Title: 'Album library', Subtitle: 'Keep every memory.' }, snapshot = source(original)
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900)), previous = getTextMeasurer()
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
  const placement = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
  try {
    for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const viewport = { width, height: 900 }, options = { examples: [id], fonts, browser, renderer, mode, viewport }
      const built = await buildComponentDocument(snapshot, options), observed = built.selections[0].observation.roots[0]
      let { graph } = built, instance = built.selections[0].instance
      const copy = observed.children[0].children
      assert.deepEqual(copy.map(node => node.tag), ['h1', 'p'])
      assert.ok(copy.every(node => !node.component && !node.source), 'private copy does not claim independent components')
      assert.deepEqual(copy.map(node => node.children[0].property), ['Title', 'Subtitle'])
      assert.deepEqual(built.selections[0].components.map(item => item.path).toSorted((a, b) => a.length - b.length), [[id], [id, 'action']])
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Unchanged toolbar', y: 500 })
      const props = { ...original }
      for (const [field, value] of [['Title', 'A collection of memories'], ['Subtitle', 'Remember the people and places. '.repeat(8).trim()]]) {
        props[field] = value
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const master = graph.getNode(instance.componentId), definition = master.componentPropertyDefinitions.find(item => item.name === field)
        const protectedNodes = structuredClone([...graph.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Unchanged toolbar')
          .flatMap(node => descendants(graph, node)))
        const before = structuredClone([...graph.getAllNodes()])
        editor.setInstanceComponentProperty(instance.id, definition.id, value)
        const after = structuredClone([...graph.getAllNodes()])
        editor.undoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], after)
        assert.deepEqual(protectedNodes.map(node => graph.getNode(node.id)), protectedNodes)
        const expected = (await captureExample(browser, source(props), id, { fonts, mode, viewport })).roots[0]
        for (let cycle = 0; cycle < 3; cycle++) {
          close(instance.width, expected.bounds.width, 'toolbar width')
          close(instance.height, expected.bounds.height, 'toolbar height')
          for (const [index, node] of graph.getChildren(instance.id).entries()) for (const field of ['x', 'y', 'width', 'height']) {
            const value = expected.children[index].bounds[field] - (['x', 'y'].includes(field) ? expected.bounds[field] : 0)
            close(node[field], value, `toolbar child ${index}/${field}`)
          }
          const action = descendants(graph, instance).find(node => origin(node)?.localId === 'action')
          assert.equal(action.type, 'INSTANCE')
          assert.equal(descendants(graph, action).find(node => node.type === 'TEXT').text, 'Create album')
          const nativeCopy = graph.getChildren(instance.id)[0]
          for (const [index, region] of expected.children[0].children.entries()) {
            const native = descendants(graph, graph.getChildren(nativeCopy.id)[index]).find(node => node.type === 'TEXT')
            assert.equal(native.text, props[region.children[0].property])
            const paragraph = renderer.buildParagraph(native, undefined, { halfLeading: true })
            try {
              const lines = paragraph.getLineMetrics(), rects = region.children[0].rects
              assert.equal(lines.length, rects.length)
              for (const [line, metrics] of lines.entries()) close(metrics.width, rects[line].width, 'copy line width')
            } finally { paragraph.delete() }
          }
          assert.deepEqual(extractSourceProps(graph, instance, snapshot).proposal, {
            baseSHA256: snapshot.sha256, path: [id], props: Object.fromEntries(Object.entries(props).filter(([key, value]) => value !== original[key])),
          })
          if (cycle < 2) {
            graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
            instance = placement(graph)
          }
        }
      }
      const refused = structuredClone(built.selections[0].observation)
      refused.roots[0].children[0].children[1].component = 'text'
      const foundation = buildFoundation(snapshot), page = foundation.graph.addPage('Refused uncaptured component')
      foundation.graph.updateNode(page.id, { variableModes: {
        [foundation.collection.id]: foundation.collection.modes.find(item => item.name === mode).modeId,
      } })
      const before = structuredClone([...foundation.graph.getAllNodes()]), hook = getTextMeasurer()
      await assert.rejects(materializeComponent(foundation.graph, page.id, snapshot, refused, fonts, renderer, foundation.collection.id),
        /cannot flatten an uncaptured component boundary/)
      assert.deepEqual([...foundation.graph.getAllNodes()], before)
      assert.equal(getTextMeasurer(), hook)
    }
    assert.equal(browser.contexts().length, 0)
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
