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
import { chain } from '../exporter-correction.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'

test('source block flow retains intrinsic row sizing, linked paragraphs, property history and two saves', async t => {
  const id = 'fixture/block'
  const run = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  h "maragu.dev/gomponents/html"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
  "github.com/septagon-oss/platformkit/ui/style"
)
func main() {
  var input struct { Content string; Row bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  var children []g.Node
  for _, item := range []struct{ id, content string }{
    {"first", "Collection notes"}, {"body", input.Content}, {"last", "Keep these details"},
  } {
    props := components.TextProps{Content: item.content}
    if item.id != "last" { props.Class = "mb-0.5" }
    children = append(children, components.ExampleOf(components.ExampleInfo{
      ID: item.id, ComponentID: "pk-ui.component.text",
    }, props, components.Text).Node)
  }
  blockID := "fixture/block"
  if input.Row { blockID = "copy" }
  example := components.ExampleWithChildren(components.ExampleInfo{
    ID: blockID, ComponentID: "fixture.component.block",
  }, struct{}{}, children, func(_ struct{}, nodes ...g.Node) g.Node {
    return h.Div(h.Class("space-y-1"), g.Group(nodes))
  })
  if input.Row {
    action := components.ExampleOf(components.ExampleInfo{ID: "action", ComponentID: "pk-ui.component.button"},
      components.ButtonProps{Label: "Continue"}, components.Button)
    example = components.ExampleWithChildren(components.ExampleInfo{
      ID: "fixture/row", ComponentID: "pk-ui.component.flex",
    }, components.FlexProps{Wrap: true, Justify: "between", Align: "start"}, []g.Node{example.Node, action.Node}, components.Flex)
  }
  snapshot, err := ui.Export(design.Default(), []components.Example{example},
    ui.Extra{Lists: []style.ClassList{style.New().MarginBottom(style.S0_5)}})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const source = (content = 'An album begins here.', row = false) => run({ content, row })
  const fonts = suppliedFonts([400, 600])
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1)), previous = getTextMeasurer()
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const root = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.length === 1)
  const child = (graph, localId) => [...graph.getAllNodes()].find(node => origin(node)?.localId === localId &&
    chain(graph, node, 'parentId').some(ancestor => ancestor.id === root(graph).id))
  function matches(graph, observed, placed = root(graph)) {
    for (const field of ['width', 'height']) assert.ok(Math.abs(placed[field] - observed.bounds[field]) <= 1 / 64,
      `${observed.source.path}/${field}: ${placed[field]} versus ${observed.bounds[field]}`)
    for (const element of observed.children.filter(node => node.kind === 'element')) {
      const placedChild = graph.getChildren(placed.id).find(node => origin(node)?.localId === element.source.path.at(-1))
      assert.equal(placedChild.type, 'INSTANCE')
      if (placed.layoutMode === 'VERTICAL' && placedChild.layoutMode === 'VERTICAL') {
        assert.equal(placedChild.counterAxisSizing, 'FILL', 'native cross-axis fill survives import')
        assert.equal(Object.hasOwn(placedChild.overrides, 'width'), false, 'fill width remains derived, not an authored fixed size')
      }
      for (const field of ['x', 'y', 'width', 'height']) {
        const expected = element.bounds[field] - (field === 'x' || field === 'y' ? observed.bounds[field] : 0)
        assert.ok(Math.abs(placedChild[field] - expected) <= 1 / 64, `${element.source.path}/${field}: ${placedChild[field]} versus ${expected}`)
      }
      matches(graph, element, placedChild)
    }
  }
  try {
    for (const row of [false, true]) for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const snapshot = source(undefined, row), selected = row ? 'fixture/row' : id
      const viewport = { width, height: 900 }, options = { examples: [selected], fonts, browser, renderer, mode, viewport }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built
      assert.equal((row ? child(graph, 'copy') : root(graph)).itemSpacing, 4)
      const observed = built.selections[0].observation.roots[0]
      const observedChildren = (row ? observed.children[0] : observed).children
      assert.deepEqual([observedChildren[0].style['margin-bottom'], observedChildren[1].style['margin-top']], ['2px', '4px'])
      assert.equal(observedChildren[1].bounds.y - observedChildren[0].bounds.y - observedChildren[0].bounds.height, 4,
        'independent CSS collapses adjacent margins to their maximum, not their sum')
      matches(graph, built.selections[0].observation.roots[0])
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Unchanged block', x: 500 })
      for (const content of ['Remember the people and the places. '.repeat(12).trim(), 'A short note.']) {
        const target = child(graph, 'body'), master = chain(graph, target, 'componentId').at(-1)
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const before = structuredClone([...graph.getAllNodes()])
        if (row) {
          const measurer = getTextMeasurer()
          try {
            setTextMeasurer(() => null)
            assert.throws(() => editor.setInstanceComponentProperty(target.id,
              master.componentPropertyDefinitions.find(item => item.name === 'content').id, content), /actual measurement/)
            assert.deepEqual([...graph.getAllNodes()], before, 'unavailable intrinsic measurement restores the entire property scope')
          } finally { setTextMeasurer(measurer) }
        }
        const protectedIds = new Set([...graph.getAllNodes()].filter(node => chain(graph, node, 'parentId')
          .some(ancestor => ancestor.type === 'COMPONENT' || ancestor.name === 'Unchanged block')).map(node => node.id))
        editor.setInstanceComponentProperty(target.id, master.componentPropertyDefinitions.find(item => item.name === 'content').id, content)
        const after = structuredClone([...graph.getAllNodes()])
        editor.undoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], after)
        for (const node of before.filter(node => protectedIds.has(node.id))) {
          assert.deepEqual(graph.getNode(node.id), node, 'masters and independent placements remain unchanged')
        }
        const expected = (await captureExample(browser, source(content, row), selected, { fonts, mode, viewport })).roots[0]
        matches(graph, expected)
        graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        matches(graph, expected)
        assert.deepEqual(extractSourceProps(graph, child(graph, 'body'), snapshot).proposal,
          { baseSHA256: snapshot.sha256, path: row ? [selected, 'copy', 'body'] : [id, 'body'], props: { content } })
      }
    }
    for (const snapshot of [source(), source(undefined, true)]) {
      const selected = snapshot.examples[0].id, observation = await captureExample(browser, snapshot, selected, { fonts })
      const changes = selected === id ? [
        node => { node.style.float = 'left' }, node => { node.style.clear = 'both' },
        node => { node.style['column-count'] = '2' }, node => { node.style['column-width'] = '200px' },
        node => { node.children[0].style['margin-top'] = '4px' },
        node => { node.children[2].style['margin-bottom'] = '4px' },
        node => { node.children[1].style['margin-left'] = '4px' },
        node => { node.children[1].style['margin-top'] = '-4px' },
        node => { node.children[2].style['margin-top'] = '8px' },
      ] : [node => { node.style['flex-wrap'] = 'nowrap' }, node => { node.children[0].sizing.width = '100%' }]
      for (const change of changes) {
        const altered = structuredClone(observation); change(altered.roots[0])
        const { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused block')
        const before = structuredClone([...graph.getAllNodes()]), measurer = getTextMeasurer()
        await assert.rejects(materializeComponent(graph, page.id, snapshot, altered, fonts, renderer, collection.id), /block|margin|length/)
        assert.deepEqual([...graph.getAllNodes()], before)
        assert.equal(getTextMeasurer(), measurer)
      }
    }
    assert.equal(browser.contexts().length, 0)
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
