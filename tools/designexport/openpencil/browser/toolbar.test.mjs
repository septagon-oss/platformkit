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
import { chain } from '../exporter-correction.mjs'
import { captureExample } from './capture.mjs'

test('standalone and page-nested Toolbar retain linked actions, reflow, source identity and two saves', async t => {
  // A configured, supplied test font is not the default system display stack.
  const run = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
)
func main() {
  var input struct { Props components.ToolbarProps; Page bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  action := components.ExampleOf(components.ExampleInfo{ID: "action", ComponentID: "pk-ui.component.button"},
    components.ButtonProps{Label: "Create album", Href: "/albums/new"}, components.Button)
  id := "fixture/toolbar"
  if input.Page { id = "toolbar" }
  example := components.ExampleWithChildren(components.ExampleInfo{ID: id, ComponentID: "pk-ui.component.toolbar"},
    input.Props, []g.Node{action.Node}, components.Toolbar)
  if input.Page {
    body := components.ExampleOf(components.ExampleInfo{ID: "description", ComponentID: "pk-ui.component.text"},
      components.TextProps{Content: "Small discoveries, collected together."}, components.Text)
    type slots struct { Header, Body g.Node }
    example = components.ExampleWithSlots(components.ExampleInfo{ID: "fixture/page", ComponentID: "fixture.page"},
      struct{}{}, slots{Header: example.Node, Body: body.Node}, func(_ struct{}, content slots) g.Node {
        return components.Stack(components.StackProps{Gap: "8"}, content.Header, content.Body)
      })
  }
  theme := design.Default()
  theme.Light.Typography.Display, theme.Dark.Typography.Display = design.FontBody, design.FontBody
  snapshot, err := ui.Export(theme, []components.Example{example})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const source = (props, page) => run({ props, page })
  const fonts = suppliedFonts([400, 600])
  const original = { Title: 'Album library', Subtitle: 'Keep every memory.' }
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900)), previous = getTextMeasurer()
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
  const placement = (graph, id) => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const toolbar = (graph, id, composed) => composed ? graph.getChildren(placement(graph, id).id).find(node => origin(node)?.localId === 'toolbar') : placement(graph, id)
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
  try {
    for (const composed of [false, true]) for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const id = composed ? 'fixture/page' : 'fixture/toolbar', path = composed ? [id, 'toolbar'] : [id], snapshot = source(original, composed)
      const viewport = { width, height: 900 }, options = { examples: [id], fonts, browser, renderer, mode, viewport }
      const built = await buildComponentDocument(snapshot, options), observedRoot = built.selections[0].observation.roots[0]
      const observed = composed ? observedRoot.children[0] : observedRoot
      let { graph } = built, instance = toolbar(graph, id, composed)
      const copy = observed.children[0].children
      assert.deepEqual(copy.map(node => node.tag), ['h1', 'p'])
      assert.ok(copy.every(node => !node.component && !node.source), 'private copy does not claim independent components')
      assert.deepEqual(copy.map(node => node.children[0].property), ['Title', 'Subtitle'])
      assert.deepEqual(built.selections[0].components.map(item => item.path).toSorted(),
        (composed ? [[id], [id, 'description'], path, [...path, 'action']] : [path, [...path, 'action']]).toSorted())
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Unchanged toolbar', y: 500 })
      const props = { ...original }
      for (const [field, value] of [['Title', 'A collection of memories'], ['Subtitle', 'Remember the people and places. '.repeat(8).trim()]]) {
        props[field] = value
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const master = chain(graph, instance, 'componentId').at(-1), definition = master.componentPropertyDefinitions.find(item => item.name === field)
        const protectedNodes = structuredClone([...graph.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Unchanged toolbar')
          .flatMap(node => descendants(graph, node)))
        const before = structuredClone([...graph.getAllNodes()])
        editor.setInstanceComponentProperty(instance.id, definition.id, value)
        const after = structuredClone([...graph.getAllNodes()])
        editor.undoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], after)
        assert.deepEqual(protectedNodes.map(node => graph.getNode(node.id)), protectedNodes)
        const changed = source(props, composed), expectedRoot = (await captureExample(browser, changed, id, { fonts, mode, viewport })).roots[0]
        const expected = composed ? expectedRoot.children[0] : expectedRoot
        const page = await browser.newPage({ viewport, colorScheme: mode })
        try {
          await page.setContent(`<html data-theme="${mode}"><head><style>${changed.css}</style></head><body>${changed.examples[0].html}</body></html>`)
          assert.equal(await page.getByRole('heading').count(), 1)
          assert.equal(await page.getByRole('heading', { level: 1, name: props.Title, exact: true }).count(), 1)
          const link = page.getByRole('link', { name: 'Create album', exact: true })
          assert.equal(await link.getAttribute('href'), '/albums/new')
          await page.keyboard.press('Tab')
          assert.ok(await link.evaluate(node => document.activeElement === node && node.matches(':focus-visible')))
        } finally { await page.close() }
        for (let cycle = 0; cycle < 3; cycle++) {
          const sibling = [...graph.getAllNodes()].find(node => node.name === 'Unchanged toolbar')
          assert.deepEqual(descendants(graph, sibling).filter(node => node.type === 'TEXT').map(node => node.text).toSorted(),
            [original.Title, original.Subtitle, 'Create album', ...(composed ? ['Small discoveries, collected together.'] : [])].toSorted())
          for (const field of ['width', 'height']) close(sibling[field], before.find(node => node.name === 'Unchanged toolbar')[field], `protected sibling ${field}`)
          const root = placement(graph, id)
          for (const field of ['width', 'height']) close(root[field], expectedRoot.bounds[field], `page ${field}`)
          if (composed) for (const [index, child] of graph.getChildren(root.id).entries()) {
            assert.equal(child.type, 'INSTANCE')
            for (const field of ['x', 'y', 'width', 'height']) close(child[field], expectedRoot.children[index].bounds[field] -
              (['x', 'y'].includes(field) ? expectedRoot.bounds[field] : 0), `page child ${index}/${field}`)
          }
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
            baseSHA256: snapshot.sha256, path, props: Object.fromEntries(Object.entries(props).filter(([key, value]) => value !== original[key])),
          })
          if (cycle < 2) {
            graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
            instance = toolbar(graph, id, composed)
          }
        }
      }
      const refused = structuredClone(built.selections[0].observation)
      const refusedToolbar = composed ? refused.roots[0].children[0] : refused.roots[0]
      refusedToolbar.children[0].children[1].component = 'text'
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
