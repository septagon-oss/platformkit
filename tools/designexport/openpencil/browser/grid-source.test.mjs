import assert from 'node:assert/strict'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { chain } from '../exporter-correction.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'

test('source Grid retains linked text, wrapping, property edits and two saves at narrow and wide widths', async t => {
  const id = 'fixture/grid'
  const run = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
  "github.com/septagon-oss/platformkit/ui/style"
)
func main() {
  var input struct { Content string; Columns string; Span bool; Controls bool; Label string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  var children []g.Node
  for _, item := range []struct{ id, content string }{
    {"first", "First"}, {"body", input.Content}, {"last", "Last"}, {"next", "Next"},
  } {
    if input.Controls && item.id != "body" {
      label := item.content
      if item.id == "first" { label = input.Label }
      children = append(children, components.ExampleOf(components.ExampleInfo{
        ID: item.id, ComponentID: "pk-ui.component.button",
      }, components.ButtonProps{Label: label}, components.Button).Node)
      continue
    }
    props := components.TextProps{Content: item.content}
    if input.Span && item.id == "body" { props.Class = "col-span-full" }
    children = append(children, components.ExampleOf(components.ExampleInfo{
      ID: item.id, ComponentID: "pk-ui.component.text",
    }, props, components.Text).Node)
  }
  example := components.ExampleWithChildren(components.ExampleInfo{
    ID: "fixture/grid", ComponentID: "pk-ui.component.grid",
  }, components.GridProps{Columns: input.Columns, Gap: "4"}, children, components.Grid)
  standalone := components.ExampleOf(components.ExampleInfo{
    ID: "fixture/button", ComponentID: "pk-ui.component.button",
  }, components.ButtonProps{Label: input.Label}, components.Button)
  snapshot, err := ui.Export(design.Default(), []components.Example{example, standalone},
    ui.Extra{Lists: []style.ClassList{style.New().ColSpanFull()}})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const source = (content = 'Album', columns = '2', span = false, controls = false, label = 'First') => run({ content, columns, span, controls, label })
  const fonts = suppliedFonts([400, 500, 600])
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900)), previous = getTextMeasurer()
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const root = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.length === 1)
  function matches(graph, observed) {
    const placed = root(graph)
    assert.equal(placed.layoutMode, 'GRID')
    for (const field of ['width', 'height']) assert.ok(Math.abs(placed[field] - observed.bounds[field]) <= 1 / 64,
      `grid ${field}: ${placed[field]} versus ${observed.bounds[field]}`)
    for (const element of observed.children) {
      const node = graph.getChildren(placed.id).find(child => origin(child)?.localId === element.source.path.at(-1))
      assert.equal(node.type, 'INSTANCE')
      assert.ok(placed.gridTemplateColumns.every(track => track.minValue === 0), 'CSS zero track minimum remains explicit')
      for (const field of ['x', 'y', 'width', 'height']) {
        const value = element.bounds[field] - (['x', 'y'].includes(field) ? observed.bounds[field] : 0)
        assert.ok(Math.abs(node[field] - value) <= 1 / 64, `${element.source.path}/${field}: ${node[field]} versus ${value}`)
      }
      if (element.tag === 'button') {
        const text = graph.getChildren(node.id)[0], region = element.children[0]
        const expected = { x: region.bounds.x - element.bounds.x, width: region.bounds.width,
          y: region.bounds.y - element.bounds.y - (text.height - region.bounds.height) / 2,
          height: region.rects.length * Number.parseFloat(element.style['line-height']) }
        for (const field of ['x', 'y', 'width', 'height']) assert.ok(Math.abs(text[field] - expected[field]) <= 1 / 64,
          `${element.source.path}/label ${field}: ${text[field]} versus ${expected[field]}`)
      }
    }
  }
  try {
    for (const controls of [true, false]) for (const mode of ['light', 'dark']) for (const columns of ['2', '3']) for (const span of [false, true]) {
      const initial = columns === '3' ? 'A collection begins with the people and places we remember.' : 'Album'
      const snapshot = source(initial, columns, span, controls), viewport = { width: 320, height: 900 }
      const options = { examples: [id], fonts, browser, renderer, mode, viewport }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built
      assert.equal(built.selections[0].components.length, 5)
      if (controls) {
        const standalone = await captureExample(browser, snapshot, 'fixture/button', options)
        const master = built.selections[0].components.find(item => item.path.at(-1) === 'first').master
        assert.equal(master.primaryAxisSizing, 'HUG')
        assert.equal(master.counterAxisSizing, 'HUG')
        for (const field of ['width', 'height']) assert.ok(Math.abs(master[field] - standalone.roots[0].bounds[field]) <= 1 / 64,
          `reusable button ${field} matches standalone source, not its stretched placement`)
        if (columns === '2' && !span) {
          const page = await browser.newPage({ colorScheme: mode, reducedMotion: 'reduce', viewport })
          try {
            await page.setContent(`<html data-theme="${mode}"><head><style>${snapshot.css}</style></head><body>${snapshot.examples.find(item => item.id === id).html}</body></html>`)
            assert.equal(await page.getByRole('button').count(), 3)
            for (const name of ['First', 'Last', 'Next']) {
              const button = page.getByRole('button', { name, exact: true })
              assert.equal(await button.ariaSnapshot(), `- button "${name}"`)
              await button.evaluate(node => { node.dataset.activations = '0'; node.addEventListener('click', () => node.dataset.activations++) })
              const before = await button.evaluate(node => getComputedStyle(node).boxShadow)
              await page.keyboard.press('Tab')
              assert.ok(await button.evaluate(node => document.activeElement === node && node.matches(':focus-visible')))
              assert.notEqual(await button.evaluate(node => getComputedStyle(node).boxShadow), before)
              for (const key of ['Enter', 'Space']) await page.keyboard.press(key)
              assert.equal(await button.getAttribute('data-activations'), '2')
            }
          } finally { await page.close() }
        }
      }
      assert.equal(built.selections[0].observation.roots[0].sizing['grid-template-columns'], `repeat(${columns}, minmax(0px, 1fr))`)
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Unchanged grid', x: 1500 })
      matches(graph, built.selections[0].observation.roots[0])
      for (const width of [1280, 390, 320]) {
        setTextMeasurer((node, maxWidth) => renderer.measureTextNode(node, maxWidth))
        graph.updateNode(root(graph).id, { width })
        computeLayout(graph, root(graph).id)
        const expected = await captureExample(browser, snapshot, id, { ...options, viewport: { width, height: 900 } })
        matches(graph, expected.roots[0])
      }
      const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
      const before = structuredClone([...graph.getAllNodes()]), content = 'Remember the people and places. '.repeat(4).trim()
      const label = 'Save this album', edits = [['body', 'content', content], ...(controls ? [['first', 'label', label]] : [])]
      const protectedNodes = before.filter(node => chain(graph, node, 'parentId').some(parent =>
        parent.type === 'COMPONENT' || parent.name === 'Unchanged grid'))
      for (const [localId, property, value] of edits) {
        const edited = graph.getChildren(root(graph).id).find(node => origin(node)?.localId === localId)
        const definition = chain(graph, edited, 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === property)
        editor.setInstanceComponentProperty(edited.id, definition.id, value)
      }
      const after = structuredClone([...graph.getAllNodes()])
      for (const node of protectedNodes) assert.deepEqual(graph.getNode(node.id), node, `protected master/sibling ${node.name}`)
      for (const _ of edits) { editor.undoAction(); await Promise.resolve() }
      assert.deepEqual([...graph.getAllNodes()], before)
      for (const _ of edits) { editor.redoAction(); await Promise.resolve() }
      assert.deepEqual([...graph.getAllNodes()], after)
      const expected = await captureExample(browser, source(content, columns, span, controls, label), id, options)
      for (let cycle = 0; cycle < 3; cycle++) {
        matches(graph, expected.roots[0])
        for (const { path, master: original } of built.selections[0].components) {
          const master = [...graph.getAllNodes()].find(node => node.type === 'COMPONENT' &&
            JSON.stringify(origin(node)?.definitionPath) === JSON.stringify(path))
          for (const field of ['width', 'height', 'primaryAxisSizing', 'counterAxisSizing']) assert.equal(master[field], original[field],
            `${path}/${field} remains intrinsic after save ${cycle}`)
        }
        const sibling = [...graph.getAllNodes()].find(node => node.name === 'Unchanged grid')
        for (const localId of ['first', 'body']) {
          const cell = graph.getChildren(sibling.id).find(node => origin(node)?.localId === localId)
          assert.equal(graph.getChildren(cell.id)[0].text, localId === 'body' ? initial : 'First')
        }
        for (const [localId, property, value] of edits) {
          const target = graph.getChildren(root(graph).id).find(node => origin(node)?.localId === localId)
          assert.deepEqual(extractSourceProps(graph, target, snapshot).proposal,
            { baseSHA256: snapshot.sha256, path: [id, localId], props: { [property]: value } })
        }
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
      const observation = built.selections[0].observation
      for (const mutate of [
        node => { node.style['grid-auto-flow'] = 'column' },
        node => { node.sizing['grid-template-columns'] = 'repeat(auto-fit, 1fr)' },
        node => { node.children[0].style.order = '1' },
        node => { node.children[0].bounds.width += 2 },
        node => { node.children[0].children[0].bounds.x += 2 },
      ]) {
        const altered = structuredClone(observation), { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused grid')
        graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
        mutate(altered.roots[0])
        const before = structuredClone([...graph.getAllNodes()])
        await assert.rejects(materializeComponent(graph, page.id, snapshot, altered, fonts, renderer, collection.id),
          /source grid|composition native|composition text placement/, `${controls}/${mode}/${columns}/${span}: ${mutate}`)
        assert.deepEqual([...graph.getAllNodes()], before)
      }
    }
    assert.equal(browser.contexts().length, 0)
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
