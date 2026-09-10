import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { parseColor } from '@open-pencil/core/color'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { computeAllLayouts, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'

const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
const placed = (graph, id) => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)

async function borderPixelsAndSemantics(browser, ck, graph, snapshot, expected, options) {
  const width = options.viewport.width, height = Math.ceil(expected.bounds.height) + 12
  const background = snapshot.themes.find(theme => theme.mode === options.mode).tokens.find(token => token.name === '--pk-color-surface-canvas').value
  const page = await browser.newPage({ viewport: { width, height }, colorScheme: options.mode, reducedMotion: 'reduce' })
  const surface = ck.MakeSurface(width, height), draw = new SkiaRenderer(ck, surface)
  let image
  try {
    const fontCSS = options.fonts.map(face => `@font-face { font-family: "${face.family}"; font-weight: ${face.weight}; src: url(data:font/woff;base64,${face.bytes.toString('base64')}); }`).join('')
    await page.setContent(`<html data-theme="${options.mode}"><head><style>${fontCSS}${snapshot.css} body { background: ${background}; }</style></head><body>${snapshot.examples.find(example => example.id === 'fixture/validation').html}</body></html>`)
    await page.evaluate(async () => { await document.fonts.ready })
    const field = page.getByRole('textbox', { name: /^Title/ })
    assert.equal(await field.count(), 1)
    assert.equal(await field.getAttribute('aria-invalid'), 'true')
    const description = await field.getAttribute('aria-describedby')
    assert.ok(description && await page.locator(`[id="${description}"]`).textContent())
    const summary = page.getByRole('alert').filter({ hasText: 'That could not be saved' })
    assert.equal(await summary.getAttribute('aria-live'), 'assertive')
    assert.equal(await summary.getAttribute('aria-atomic'), 'true')
    await page.keyboard.press('Tab')
    assert.equal(await field.evaluate(node => node === document.activeElement), true)
    await field.evaluate(node => node.blur())
    image = ck.MakeImageFromEncoded(await page.screenshot())
    const format = { width, height, alphaType: ck.AlphaType.Unpremul, colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }
    const source = image.readPixels(0, 0, format), canvas = surface.getCanvas(), bg = parseColor(background)
    canvas.clear(ck.Color4f(bg.r, bg.g, bg.b, bg.a))
    await draw.loadFonts(); draw.renderSceneToCanvas(canvas, graph, placed(graph, 'fixture/validation').id); surface.flush()
    const native = canvas.readPixels(0, 0, format), alert = expected.children[0].bounds
    // Straight border, padding and clipped-away corner interiors: exact solid
    // source paint, avoiding rasterizer-specific antialiased curve edges.
    for (const [x, y] of [[0, 0], [1, 1], [0, 8], [1, 12], [3, 20], [4, 20], [8, 12],
      [0, alert.height - 1], [1, alert.height - 2], [1, alert.height - 12]]) {
      const offset = (Math.floor(alert.y + y) * width + Math.floor(alert.x + x)) * 4
      for (let channel = 0; channel < 4; channel++) assert.ok(Math.abs(source[offset + channel] - native[offset + channel]) <= 1,
        `Alert border ${options.mode}/${x},${y}/${channel}: ${native[offset + channel]} versus ${source[offset + channel]}`)
    }
  } finally { image?.delete(); draw.destroy(); await page.close() }
}

test('real refusal forms inherit a centered editable Alert with optional consumer margins through two saves', async t => {
  const run = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/kit/crud"
  "github.com/septagon-oss/platformkit/kit/httpx"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
  "github.com/septagon-oss/platformkit/ui/css"
  "github.com/septagon-oss/platformkit/ui/screens"
)
func main() {
  var input struct {
    Proposal *ui.PropsProposal
    IconMargin bool
  }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  resource := httpx.Resource{Module: "notes", Entity: "note", Path: "/api/v1/notes",
    Schema: crud.Schema{Fields: []crud.Field{{Name: "title", Type: crud.TypeString, Required: true}}}}
  examples := []components.Example{
    screens.FormExample("fixture/validation", resource, screens.Options{Root: "/admin"}, "/admin/notes", "New note",
      nil, map[string]string{"title": "A title is required."}, "A title is required.", true),
    screens.FormExample("fixture/conflict", resource, screens.Options{Root: "/admin"}, "/admin/notes/1", "Edit note",
      map[string]any{"title": "Field notes"}, nil, "A record already uses one of these values.", false),
  }
  var extras []ui.Extra
  if input.IconMargin {
    sheet := css.NewSheet()
    sheet.Select("[data-alert-icon]", css.Decl("margin-top", css.Literal("2px")))
    extras = append(extras, ui.Extra{Sheets: []*css.Sheet{sheet}})
  }
  snapshot, err := ui.Export(design.Default(), examples, extras...)
  if input.Proposal != nil { _, snapshot, err = ui.ProjectProps(design.Default(), examples, *input.Proposal, extras...) }
  if err != nil { panic(err) }; if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const fonts = suppliedFonts([400, 500, 600, 700])
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
  try {
    for (const IconMargin of [false, true]) for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const snapshot = run({ IconMargin })
      const options = { examples: snapshot.examples.map(example => example.id), fonts, browser, renderer, mode, viewport: { width, height: 900 } }
      const observation = await captureExample(browser, snapshot, 'fixture/validation', options)
      const alert = observation.roots[0].children[0]
      assert.equal(alert.style['border-left-width'], '4px', 'the actual source accent must be retained')
      assert.equal(alert.style['align-items'], 'center')
      assert.equal(alert.children[0].style['margin-top'], IconMargin ? '2px' : '0px')
      let { graph } = await buildComponentDocument(snapshot, options)
      // Baseline the editable file's float32 geometry, not the generator's
      // double-precision SVG coordinates, before checking master isolation.
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      const original = structuredClone(snapshot)
      const protectedNodes = [...graph.getAllNodes()].filter(node => node.type === 'COMPONENT')
        .map(node => [node.name, descendants(graph, node).map(child => [child.name, child.text, child.x, child.y, child.width, child.height])])
      const previous = getTextMeasurer()
      try {
        // Page population can run before the renderer can measure a face.
        // Repeating unchanged layout must retain the imported label advances.
        const pending = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        const textGeometry = () => [...pending.getAllNodes()].filter(node => node.type === 'TEXT')
          .map(node => [node.id, node.text, node.x, node.y, node.width, node.height])
        const measured = textGeometry()
        setTextMeasurer(() => null); computeAllLayouts(pending); computeAllLayouts(pending)
        assert.deepEqual(textGeometry(), measured)
      } finally { setTextMeasurer(previous) }
      const target = () => descendants(graph, placed(graph, 'fixture/validation')).find(node => origin(node)?.localId === 'error')
      const message = 'Please add a title before saving. '.repeat(4).trim()
      const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
      const property = editor.getInstanceComponentPropertyDefinitions(target().id).find(item => item.name === 'message')
      assert.ok(property, 'Alert message is a source-owned editable property')
      editor.setInstanceComponentProperty(target().id, property.id, message)
      editor.undoAction(); assert.equal(extractSourceProps(graph, target(), snapshot).status, 'no-supported-changes')
      editor.redoAction()
      const proposal = { baseSHA256: snapshot.sha256, path: ['fixture/validation', 'error'], props: { message } }
      const changed = run({ proposal, IconMargin }), expected = (await captureExample(browser, changed, 'fixture/validation', options)).roots[0]
      for (let cycle = 0; cycle < 3; cycle++) {
        assert.deepEqual(extractSourceProps(graph, target(), snapshot).proposal, proposal)
        assert.equal(extractSourceProps(graph, placed(graph, 'fixture/conflict'), snapshot).status, 'no-supported-changes')
        for (const [name, children] of protectedNodes) assert.deepEqual(descendants(graph, [...graph.getAllNodes()].find(node => node.type === 'COMPONENT' && node.name === name))
          .map(child => [child.name, child.text, child.x, child.y, child.width, child.height]), children)
        assert.ok(Math.abs(placed(graph, 'fixture/validation').height - expected.bounds.height) <= 1 / 64)
        assert.ok(Math.abs(target().height - expected.children[0].bounds.height) <= 1 / 64)
        const [box, body] = graph.getChildren(target().id), margin = IconMargin ? box : null
        const icon = margin ? graph.getChildren(margin.id)[0] : box
        const iconCenter = icon.y + (margin?.y ?? 0) + icon.height / 2 - (IconMargin ? 1 : 0)
        assert.ok(Math.abs(iconCenter - body.y - body.height / 2) <= 1 / 64, 'Alert centers retain the source margin')
        for (const [node, source, parent] of [[icon, expected.children[0].children[0], margin], [body, expected.children[0].children[1], null]]) {
          for (const field of ['x', 'y', 'width', 'height']) {
            const position = field === 'x' || field === 'y'
            const actual = node[field] + (position ? parent?.[field] ?? 0 : 0)
            const wanted = source.bounds[field] - (position ? expected.children[0].bounds[field] : 0)
            assert.ok(Math.abs(actual - wanted) <= 1 / 64, `${node.name}/${field}: ${actual} versus ${wanted}`)
          }
        }
        if (cycle === 2) await borderPixelsAndSemantics(browser, ck, graph, changed, expected, options)
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
      assert.deepEqual(snapshot, original)
    }
    const snapshot = run({}), observation = await captureExample(browser, snapshot, 'fixture/validation', { fonts })
    for (const change of [
      alert => { alert.children[0].style['margin-top'] = '-2px' },
      alert => { alert.children[0].style['margin-left'] = 'auto' },
      alert => { alert.children[0].sizing.width = '35px' },
      alert => { alert.children[0].sizing.height = '35px' },
      alert => { alert.children[0].style['box-sizing'] = 'content-box' },
      alert => { alert.children[1].style['flex-grow'] = '2' },
      alert => { alert.children[1].style['flex-basis'] = 'auto' },
      alert => { alert.children[1].style['flex-shrink'] = '2' },
      alert => { alert.children[1].bounds.x += 1 },
    ]) {
      const captured = structuredClone(observation); change(captured.roots[0].children[0])
      const { graph, collection, icons } = buildFoundation(snapshot), page = graph.addPage('Refused layout')
      const before = structuredClone([...graph.getAllNodes()])
      const targets = node => node.tag === 'svg' ? [{ region: node, master: icons.get(node.icon.canonicalName) }] : (node.children ?? []).flatMap(targets)
      await assert.rejects(materializeComponent(graph, page.id, snapshot, captured, fonts, renderer, collection.id,
        captured.roots.flatMap(targets)), /Native component:/)
      assert.deepEqual([...graph.getAllNodes()], before, 'unsupported layout leaves no partial definitions')
    }
  } finally { renderer.destroy(); await browser.close() }
})
