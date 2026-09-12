import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { chain } from '../exporter-correction.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { drawSourceParagraph } from '../paragraph-correction.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'
import { captureExample } from './capture.mjs'

test('intrinsic action sizing follows source composition through edits, history and two FIG saves', async t => {
  const run = await sourceFixture(t, `package main
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
  var input struct { Labels []string; Nested bool; Shrink, Minimum string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  var actions []g.Node
  for i, label := range input.Labels {
    id := []string{"create", "cancel"}[i]
    props := components.ButtonProps{ComponentProps:components.ComponentProps{ID:id}, Label:label}
    if input.Nested { props.Href = "/albums/new" }
    action := components.ExampleOf(components.ExampleInfo{ID:id, ComponentID:"pk-ui.component.button"}, props, components.Button)
    actions = append(actions, action.Node)
  }
  row := components.ExampleWithChildren(components.ExampleInfo{ID:"actions", ComponentID:"pk-ui.component.form-actions"},
    components.FormActionsProps{}, actions, components.FormActions)
  example := components.ExampleWithChildren(components.ExampleInfo{ID:"fixture", ComponentID:"pk-ui.component.form"},
    components.FormProps{Action:"/albums", Label:"Create album"}, []g.Node{row.Node}, components.Form)
  if input.Nested {
    row = components.ExampleWithChildren(components.ExampleInfo{ID:"actions", ComponentID:"pk-ui.component.toolbar"},
      components.ToolbarProps{Title:"Album library", Subtitle:"Keep every memory."}, actions, components.Toolbar)
    example = components.ExampleWithChildren(components.ExampleInfo{ID:"fixture", ComponentID:"pk-ui.component.stack"},
      components.StackProps{Gap:"8"}, []g.Node{row.Node}, components.Stack)
  }
  theme, sheet := design.Default(), css.NewSheet()
  theme.Light.Typography.Display, theme.Dark.Typography.Display = design.FontBody, design.FontBody
  if input.Shrink != "" { sheet.Select("#create", css.Decl("flex-shrink", css.Literal(input.Shrink))) }
  if input.Minimum != "" { sheet.Select("#create", css.Decl("min-width", css.Literal(input.Minimum))) }
  snapshot, err := ui.Export(theme, []components.Example{example}, ui.Extra{Sheets:[]*css.Sheet{sheet}})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), surface = ck.MakeSurface(1280, 900), renderer = new SkiaRenderer(ck, surface), previous = getTextMeasurer()
  const fonts = suppliedFonts([400, 600]), original = ['Create album', 'Cancel']
  const sentence = 'Create a beautiful collection of all the people and places we remember together'
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
  const close = (a, b, label) => assert.ok(Math.abs(a - b) <= 1 / 64, `${label}: ${a} versus ${b}`)
  function compare(graph, node, observed, offset = { x: 0, y: 0 }, root = observed.bounds, parent) {
    const position = { x: offset.x + node.x, y: offset.y + node.y }
    if (node.type === 'TEXT') {
      assert.equal(node.text, observed.text)
      const paragraph = renderer.buildParagraph(node, undefined, { halfLeading: true })
      try {
        const lines = paragraph.getLineMetrics()
        assert.equal(lines.length, observed.rects.length, `${node.text}: line count`)
        close(node.height, lines.length * node.lineHeight, 'text line boxes')
        const inset = side => Number.parseFloat(parent.style[`padding-${side}`]) + Number.parseFloat(parent.style[`border-${side}-width`])
        const top = parent.bounds.y + inset('top') + (['flex', 'inline-flex'].includes(parent.style.display)
          ? (parent.bounds.height - inset('top') - inset('bottom') - lines.length * node.lineHeight) / 2 : 0)
        close(position.y, top - root.y, 'text line box y')
        const canvas = surface.getCanvas(); canvas.clear(ck.WHITE)
        drawSourceParagraph(canvas, paragraph, 0, 0); surface.flush()
        const pixels = canvas.readPixels(0, 0, { width: 1280, height: 900, alphaType: ck.AlphaType.Unpremul,
          colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
        for (const [i, line] of lines.entries()) {
          close(position.x + line.left, observed.rects[i].x - root.x, 'painted line x')
          close(line.width, observed.rects[i].width, 'painted line width')
          close(i * node.lineHeight, observed.rects[i].y - observed.rects[0].y, 'line spacing y')
          assert.ok(pixels.slice(i * node.lineHeight * 1280 * 4, (i + 1) * node.lineHeight * 1280 * 4)
            .some((value, index) => index % 4 !== 3 && value < 200), 'each measured line paints')
          const glyph = paragraph.getGlyphInfoAt(line.startIndex)
          const rect = paragraph.getRectsForRange(line.startIndex, line.startIndex + 1, ck.RectHeightStyle.Max, ck.RectWidthStyle.Tight)[0]
          close(rect.rect[0], glyph.graphemeLayoutBounds[0], 'selection matches glyph')
        }
      } finally { paragraph.delete() }
      return
    }
    for (const field of ['width', 'height']) close(node[field], observed.bounds[field], `${node.name}/${field}`)
    for (const field of ['x', 'y']) close(position[field], observed.bounds[field] - root[field], `${node.name}/${field}`)
    const children = graph.getChildren(node.id)
    assert.equal(children.length, observed.children.length)
    for (const [i, child] of children.entries()) compare(graph, child, observed.children[i], position, root, observed)
  }
  try {
    for (const config of [{ nested: true }, {}, { shrink: '0' }, { minimum: '0px' },
      { nested: true, shrink: '0' }, { nested: true, minimum: '0px' }, { nested: true, paired: true },
      { nested: true, paired: true, shrink: '0' }, { nested: true, paired: true, minimum: '0px' }])
      for (const mode of ['light', 'dark']) for (const width of [320, 390, 1280]) await t.test(`${JSON.stringify(config)}/${mode}/${width}`, async () => {
        const labels = config.nested && !config.paired ? original.slice(0, 1) : original, snapshot = run({ ...config, labels })
        const viewport = { width, height: 900 }, options = { examples: ['fixture'], fonts, browser, renderer, mode, viewport }
        const built = await buildComponentDocument(snapshot, options)
        let { graph } = built
        const placement = () => [...graph.getAllNodes()].find(node => origin(node)?.path?.length === 1 && origin(node).path[0] === 'fixture')
        graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Untouched', y: 600 })
        for (const next of [['Go', 'Cancel'], ['Explore packs', 'Cancel'], [sentence, 'Keep working on this draft album'], ['ABCDEFGHIJKLMNOPQRSTUVWXYZ'.repeat(4), 'Cancel']]) {
          const changed = config.nested && !config.paired ? next.slice(0, 1) : next
          for (const [index, value] of changed.entries()) {
            const action = descendants(graph, placement()).find(node => origin(node)?.localId === ['create', 'cancel'][index])
            const definition = chain(graph, action, 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === 'label')
            if (descendants(graph, action).some(node => node.type === 'TEXT' && node.text === value)) continue
            const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
            const protectedNodes = structuredClone([...graph.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Untouched')
              .flatMap(node => descendants(graph, node))), before = structuredClone([...graph.getAllNodes()])
            editor.setInstanceComponentProperty(action.id, definition.id, value)
            const after = structuredClone([...graph.getAllNodes()])
            editor.undoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], before)
            editor.redoAction(); await Promise.resolve(); assert.deepEqual([...graph.getAllNodes()], after)
            assert.deepEqual(protectedNodes.map(node => graph.getNode(node.id)), protectedNodes)
          }
          const expectedSource = run({ ...config, labels: changed })
          const expected = (await captureExample(browser, expectedSource, 'fixture', { fonts, mode, viewport })).roots[0]
          for (let cycle = 0; cycle < 3; cycle++) {
            const root = placement()
            compare(graph, root, expected, { x: -root.x, y: -root.y })
            assert.deepEqual(descendants(graph, [...graph.getAllNodes()].find(node => node.name === 'Untouched')).filter(node => node.type === 'TEXT').map(node => node.text).toSorted(),
              [...labels, ...(config.nested ? ['Album library', 'Keep every memory.'] : [])].toSorted())
            for (const [index, value] of changed.entries()) {
              const localId = ['create', 'cancel'][index], action = descendants(graph, root).find(node => origin(node)?.localId === localId)
              assert.deepEqual(extractSourceProps(graph, action, snapshot).proposal, value === labels[index] ? undefined : {
                baseSHA256: snapshot.sha256, path: ['fixture', 'actions', localId], props: { label: value },
              })
            }
            if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
          }
          const page = await browser.newPage({ viewport })
          try {
            await page.setContent(`<style>${expectedSource.css}</style>${expectedSource.examples[0].html}`)
            for (const label of changed) {
              const action = page.getByRole(config.nested ? 'link' : 'button', { name: label, exact: true })
              await page.keyboard.press('Tab')
              assert.ok(await action.evaluate(node => document.activeElement === node && node.matches(':focus-visible')))
            }
          } finally { await page.close() }
        }
      })
    assert.equal(browser.contexts().length, 0)
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
