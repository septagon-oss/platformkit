import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
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
  const directory = await mkdtemp(join(tmpdir(), 'platformkit-grid-source-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const file = join(directory, 'main.go'), id = 'fixture/grid'
  await writeFile(file, `package main
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
  var input struct { Content string; Columns string; Span bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  var children []g.Node
  for _, item := range []struct{ id, content string }{
    {"first", "First"}, {"body", input.Content}, {"last", "Last"}, {"next", "Next"},
  } {
    props := components.TextProps{Content: item.content}
    if input.Span && item.id == "body" { props.Class = "col-span-full" }
    children = append(children, components.ExampleOf(components.ExampleInfo{
      ID: item.id, ComponentID: "pk-ui.component.text",
    }, props, components.Text).Node)
  }
  example := components.ExampleWithChildren(components.ExampleInfo{
    ID: "fixture/grid", ComponentID: "pk-ui.component.grid",
  }, components.GridProps{Columns: input.Columns, Gap: "4"}, children, components.Grid)
  snapshot, err := ui.Export(design.Default(), []components.Example{example},
    ui.Extra{Lists: []style.ClassList{style.New().ColSpanFull()}})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`, { flag: 'wx' })
  const source = (content = 'Album', columns = '2', span = false) => JSON.parse(execFileSync('go', ['run', file], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', input: JSON.stringify({ content, columns, span }),
  }))
  const fonts = [400].map(weight => {
    const bytes = readFileSync(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
    return { family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: createHash('sha256').update(bytes).digest('hex') }
  })
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
    }
  }
  try {
    for (const mode of ['light', 'dark']) for (const columns of ['2', '3']) for (const span of [false, true]) {
      const initial = columns === '3' ? 'A collection begins with the people and places we remember.' : 'Album'
      const snapshot = source(initial, columns, span), viewport = { width: 320, height: 900 }
      const options = { examples: [id], fonts, browser, renderer, mode, viewport }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built
      assert.equal(built.selections[0].components.length, 5)
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
      const edited = graph.getChildren(root(graph).id).find(node => origin(node)?.localId === 'body')
      const definition = chain(graph, edited, 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === 'content')
      const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
      const before = structuredClone([...graph.getAllNodes()]), content = 'Remember the people and places. '.repeat(4).trim()
      const protectedNodes = before.filter(node => chain(graph, node, 'parentId').some(parent =>
        parent.type === 'COMPONENT' || parent.name === 'Unchanged grid'))
      editor.setInstanceComponentProperty(edited.id, definition.id, content)
      const after = structuredClone([...graph.getAllNodes()])
      for (const node of protectedNodes) assert.deepEqual(graph.getNode(node.id), node, `protected master/sibling ${node.name}`)
      editor.undoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], before)
      editor.redoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], after)
      const expected = await captureExample(browser, source(content, columns, span), id, options)
      for (let cycle = 0; cycle < 3; cycle++) {
        matches(graph, expected.roots[0])
        const target = graph.getChildren(root(graph).id).find(node => origin(node)?.localId === 'body')
        assert.deepEqual(extractSourceProps(graph, target, snapshot).proposal,
          { baseSHA256: snapshot.sha256, path: [id, 'body'], props: { content } })
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
      const observation = built.selections[0].observation
      for (const mutate of [
        node => { node.style['grid-auto-flow'] = 'column' },
        node => { node.sizing['grid-template-columns'] = 'repeat(auto-fit, 1fr)' },
        node => { node.children[0].style.order = '1' },
      ]) {
        const altered = structuredClone(observation), { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused grid')
        graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
        mutate(altered.roots[0])
        const before = structuredClone([...graph.getAllNodes()])
        await assert.rejects(materializeComponent(graph, page.id, snapshot, altered, fonts, renderer, collection.id), /source grid/)
        assert.deepEqual([...graph.getAllNodes()], before)
      }
    }
    assert.equal(browser.contexts().length, 0)
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
