import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { buildComponentDocument, verifyComponentDocument } from '../document.mjs'
import { chain } from '../exporter-correction.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'

test('fixed and intrinsic clipped absolute compositions retain linked content, edge placement, resizing and two saves', async t => {
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
  "github.com/septagon-oss/platformkit/ui/css"
)
type Props struct { Horizontal string \`json:"horizontal"\`; Vertical string \`json:"vertical"\` }
type OwnerProps struct { Auto bool \`json:"auto"\` }
func badge(p Props, children ...g.Node) g.Node {
  return h.Div(h.Style(fmt.Sprintf("position:absolute;display:flex;width:80px;height:32px;justify-content:center;align-items:center;%s:7.25px;%s:-1.5px", p.Horizontal, p.Vertical)), g.Group(children))
}
func owner(p OwnerProps, children ...g.Node) g.Node {
  size := "height:160px"
  if p.Auto { size = "aspect-ratio:1;overflow:hidden;border-radius:12px" }
  return h.A(h.Href("/albums/fixture/7"), g.Attr("aria-label", "Open album"),
    h.Style("position:relative;display:flex;gap:16px;align-items:center;padding:12px;border:solid transparent;border-width:2px 5px 4px 3px;"+size), g.Group(children))
}
func main() {
  var input struct { Proposal *ui.PropsProposal; Auto bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  children := []g.Node{components.ExampleOf(components.ExampleInfo{ID:"body", ComponentID:"pk-ui.component.text"},
    components.TextProps{Content:"Album"}, components.Text).Node}
  sheet := css.NewSheet()
  for i, sides := range [][2]string{{"left", "top"}, {"right", "top"}, {"left", "bottom"}, {"right", "bottom"}} {
    if input.Auto {
      class := fmt.Sprintf("auto-corner-%d", i)
      sheet.Select("."+class, css.Decl("position", css.Literal("absolute")), css.Decl(sides[0], css.Literal("7.25px")),
        css.Decl(sides[1], css.Literal("-1.5px")), css.Decl("padding", css.Literal("2px 6px")),
        css.Decl("font-variant-numeric", css.Literal("tabular-nums")), css.Decl("background", css.Literal("#eee")))
      children = append(children, components.ExampleOf(components.ExampleInfo{ID:fmt.Sprintf("corner%d", i), ComponentID:"pk-ui.component.text"},
        components.TextProps{ComponentProps:components.ComponentProps{Class:class}, Element:"span", Content:"7", Size:"xs"}, components.Text).Node)
      continue
    }
    caption := components.ExampleOf(components.ExampleInfo{ID:"caption", ComponentID:"pk-ui.component.text"},
      components.TextProps{Content:"7"}, components.Text)
    children = append(children, components.ExampleWithChildren(components.ExampleInfo{
      ID:fmt.Sprintf("corner%d", i), ComponentID:"fixture.component.badge"}, Props{sides[0], sides[1]}, []g.Node{caption.Node}, badge).Node)
  }
  examples := []components.Example{components.ExampleWithChildren(components.ExampleInfo{
    ID:"fixture/positioning", ComponentID:"fixture.component.positioning"}, OwnerProps{input.Auto}, children, owner)}
  extra := ui.Extra{Sheets:[]*css.Sheet{sheet}}
  snapshot, err := ui.Export(design.Default(), examples, extra)
  if input.Proposal != nil { _, snapshot, err = ui.ProjectProps(design.Default(), examples, *input.Proposal, extra) }
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const id = 'fixture/positioning', fonts = suppliedFonts([400])
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1)), previous = getTextMeasurer()
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const root = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const child = (graph, localId) => [...graph.getAllNodes()].find(node => origin(node)?.localId === localId &&
    chain(graph, node, 'parentId').includes(root(graph)))
  function preserved(graph) {
    const geometry = node => ({
      ...Object.fromEntries(['name', 'type', 'x', 'y', 'width', 'height', 'text', 'primaryAxisSizing',
        'counterAxisSizing', 'layoutAlignSelf'].map(field => [field, node[field]])),
      component: node.componentId ? origin(chain(graph, node, 'componentId').at(-1)) : null,
      children: graph.getChildren(node.id).map(geometry),
    })
    return [...graph.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Unchanged placement')
      .map(geometry).sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)))
  }
  function matches(graph, observed) {
    const placed = root(graph)
    for (const element of observed.children) {
      const instance = child(graph, element.source.path.at(-1))
      const positioning = graph.getNode(instance.parentId), absolute = element.style.position === 'absolute'
      const box = absolute ? positioning : instance
      assert.equal(instance.type, 'INSTANCE')
      assert.equal(absolute, box.layoutPositioning === 'ABSOLUTE')
      if (absolute) {
        assert.equal(instance.x, 0); assert.equal(instance.y, 0)
        assert.ok(origin(positioning).cssPosition)
        if (element.style['font-variant-numeric'] === 'tabular-nums') {
          // FIG expands the default ligature features and uppercases tags.
          const features = graph.getChildren(instance.id)[0].fontFeatures
          assert.deepEqual(Object.fromEntries([['liga', true], ['calt', true], ...features.map(({ tag, enabled }) => [tag.toLowerCase(), enabled])]),
            { liga: true, calt: true, tnum: true })
        }
        assert.equal(element.sizing[element.sizing.left === 'auto' ? 'left' : 'right'], 'auto', 'capture retains authored auto, not a used CSSOM coordinate')
      }
      for (const field of ['x', 'y', 'width', 'height']) {
        const expected = element.bounds[field] - (['x', 'y'].includes(field) ? observed.bounds[field] : 0)
        assert.ok(Math.abs(box[field] - expected) <= 1 / 64, `${instance.name}/${field}: ${box[field]} versus ${expected}`)
      }
    }
    assert.equal(placed.height, observed.bounds.height, 'absolute children do not contribute to normal-flow size')
  }
  try {
    for (const auto of [false, true]) for (const mode of ['light', 'dark']) {
      const snapshot = run({ auto })
      const page = await browser.newPage()
      try {
        await page.setContent(`<style>${snapshot.css}</style>${snapshot.examples[0].html}`)
        const link = page.getByRole('link', { name: 'Open album', exact: true })
        assert.equal(await link.count(), 1)
        await page.keyboard.press('Tab')
        assert.equal(await link.evaluate(node => node === document.activeElement), true)
        assert.equal(await link.getAttribute('href'), '/albums/fixture/7')
        await link.evaluate(node => node.addEventListener('click', event => { event.preventDefault(); node.dataset.activated = 'yes' }))
        await page.keyboard.press('Enter')
        assert.equal(await link.getAttribute('data-activated'), 'yes')
      } finally { await page.close() }
      const options = { examples: [id], fonts, browser, renderer, mode, viewport: { width: 320, height: 900 } }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built
      const correspondence = verifyComponentDocument(graph, snapshot, [id])
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Unchanged placement', x: 1500 })
      for (let cycle = 0; cycle < 3; cycle++) {
        if (auto) {
          const placements = [...graph.getAllNodes()].filter(node => origin(node)?.cssPosition?.autoSize)
          assert.ok(placements.length >= 12, 'check all four corners in the master and both placed instances')
          for (const placement of placements) {
            const content = graph.getChildren(placement.id)[0]
            assert.equal(content.counterAxisSizing, 'FILL', `save ${cycle}: ${placement.name} retains parent-owned width`)
          }
        }
        setTextMeasurer((node, maxWidth) => renderer.measureTextNode(node, maxWidth))
        for (const width of [1280, 390, 320]) {
          graph.updateNode(root(graph).id, { width })
          computeLayout(graph, root(graph).id)
          matches(graph, (await captureExample(browser, snapshot, id, { ...options, viewport: { width, height: 900 } })).roots[0])
        }
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const before = structuredClone([...graph.getAllNodes()]), badge = child(graph, 'corner3')
        const caption = auto ? badge : graph.getChildren(badge.id).find(node => origin(node)?.localId === 'caption')
        const definition = chain(graph, caption, 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === 'content')
        const untouched = preserved(await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' }))
        for (const copy of auto ? ['99', 'Available for exchange with another collector in this album collection', '1234567890'.repeat(12)] : ['99']) {
          editor.setInstanceComponentProperty(caption.id, definition.id, copy)
          const result = extractSourceProps(graph, caption, snapshot)
          assert.equal(result.status, 'proposal')
          assert.deepEqual(result.proposal.path, auto ? [id, 'corner3'] : [id, 'corner3', 'caption'])
          const projected = run({ auto, proposal: result.proposal })
          const observed = (await captureExample(browser, projected, id, options)).roots[0]
          matches(graph, observed)
          let saved = graph
          for (let save = 0; save < 2; save++) {
            saved = await parseFigFile((await exportFigFile(saved)).slice().buffer, { populate: 'all' })
            matches(saved, observed)
            assert.deepEqual(preserved(saved), untouched, `save ${save}: masters, sibling and their component links remain unchanged`)
          }
          for (const node of before.filter(node => chain(graph, node, 'parentId').some(parent =>
            parent.type === 'COMPONENT' || parent.name === 'Unchanged placement'))) assert.deepEqual(graph.getNode(node.id), node)
          editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
          editor.redoAction(); editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        }
        const positioning = graph.getNode(badge.parentId)
        editor.updateNodeWithUndo(positioning.id, { x: 55 }, 'Move placed badge')
        assert.equal(positioning.x, 55)
        editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); assert.equal(positioning.x, 55)
        let manual = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        for (let save = 0; save < 2; save++) {
          computeLayout(manual, root(manual).id)
          assert.equal(manual.getNode(child(manual, 'corner3').parentId).x, 55, 'native occurrence placement survives saved overrides')
          manual.updateNode(root(manual).id, { width: 390 })
          computeLayout(manual, root(manual).id)
          assert.equal(manual.getNode(child(manual, 'corner3').parentId).x, 125, 'a manual move retains its new edge inset on later resize')
          manual.updateNode(root(manual).id, { width: 320 }); computeLayout(manual, root(manual).id)
          if (save === 0) manual = await parseFigFile((await exportFigFile(manual)).slice().buffer, { populate: 'all' })
        }
        editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        verifyComponentDocument(graph, snapshot, [id], correspondence)
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
    }
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
