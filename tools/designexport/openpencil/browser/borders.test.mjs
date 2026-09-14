import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import pixelmatch from 'pixelmatch'
import { SceneGraph } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { cssDashIntervals } from '../border-correction.mjs'
import { createEditor } from '@open-pencil/core/editor'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { chain } from '../exporter-correction.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'

// Isolate rasterizer precision from dash placement. Chromium and the locked
// CanvasKit disagree on a few antialiased rounded-clip pixels even for a plain
// fill, with no border adapter involved. Measure that difference independently
// at each pixel. Dashed stroke endpoints also have backend-specific coverage;
// classify only partially covered edge pixels with Pixelmatch's slope detector.
// Fully covered pixels and RGB retain exact checks (apart from byte rounding).
async function clipPrecision(page, ck, width, height, weight, radius, format) {
  const browser = await page.evaluate(({ width, height, weight, radius }) => {
    const element = document.createElement('canvas')
    element.width = width; element.height = height
    const canvas = element.getContext('2d'), outer = new Path2D(), inner = new Path2D()
    outer.roundRect(0, 0, width, height, radius); canvas.clip(outer)
    inner.rect(-1, -1, width + 2, height + 2)
    inner.roundRect(weight, weight, width - 2 * weight, height - 2 * weight, Math.max(0, radius - weight))
    canvas.clip(inner, 'evenodd'); canvas.fillRect(0, 0, width, height)
    return [...canvas.getImageData(0, 0, width, height).data.filter((_, index) => index % 4 === 3)]
  }, { width, height, weight, radius })
  const surface = ck.MakeSurface(width, height), canvas = surface.getCanvas(), paint = new ck.Paint()
  try {
    paint.setColor(ck.BLACK)
    canvas.clipRRect(ck.RRectXY(ck.LTRBRect(0, 0, width, height), radius, radius), ck.ClipOp.Intersect, true)
    const inner = Math.max(0, radius - weight)
    canvas.clipRRect(ck.RRectXY(ck.LTRBRect(weight, weight, width - weight, height - weight), inner, inner), ck.ClipOp.Difference, true)
    canvas.drawRect(ck.LTRBRect(0, 0, width, height), paint); surface.flush()
    const native = canvas.readPixels(0, 0, format)
    return browser.map((alpha, i) => Math.abs(alpha - native[4 * i + 3]))
  } finally { paint.delete(); surface.delete() }
}

test('source CSS dashed borders match Chromium after resizing and two FIG saves', async () => {
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit()
  const failures = []
  try {
    for (const [width, height, weight, radius, alpha = 1] of [
      [139, 87, 2, 12], [120, 80, 2, 12], [120, 80, 2, 24], [120, 80, 2, 0], [120, 80, 1, 12],
      [120, 80, 3, 12], [120, 80, 4, 24], [120, 80, 6, 0], [120, 40, 1, 999],
      [8, 8, 1, 0], [16, 16, 3, 4], [120, 80, 2, 12, 0.25], [120, 80, 2, 0, 0.5],
    ]) {
      let graph = new SceneGraph()
      const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Dashed source', width, height, fills: [],
        cornerRadius: radius, dashPattern: cssDashIntervals(weight),
        strokes: [{ type: 'SOLID', weight, align: 'INSIDE', visible: true, opacity: 1,
          color: { r: 1, g: 0, b: 0, a: alpha }, dashPattern: cssDashIntervals(weight) }],
        pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
          schema: 'platformkit.design-export.v1', cssBorder: { version: 1, style: 'dashed', weight },
        }) }],
      })
      const pageNode = graph.addPage('Examples')
      graph.createInstance(master.id, pageNode.id, { name: 'Placed', pluginData: [] })
      for (let cycle = 0; cycle < 3; cycle++) {
        const node = [...graph.getAllNodes()].find(node => node.name === 'Placed')
        const w = width + cycle * 19, h = height + cycle * 7
        graph.updateNode(node.id, { width: w, height: h })
        const page = await browser.newPage({ viewport: { width: w, height: h } })
        const surface = ck.MakeSurface(w, h), renderer = new SkiaRenderer(ck, surface)
        let image
        try {
          await page.setContent(`<style>body{margin:0;background:transparent}div{box-sizing:border-box;width:${w}px;height:${h}px;border:${weight}px dashed rgb(255 0 0 / ${alpha});border-radius:${radius}px}</style><div></div>`)
          image = ck.MakeImageFromEncoded(await page.screenshot({ omitBackground: true }))
          const format = { width: w, height: h, alphaType: ck.AlphaType.Unpremul, colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }
          const precision = await clipPrecision(page, ck, w, h, weight, Math.min(radius, w / 2, h / 2), format)
          const expected = image.readPixels(0, 0, format), canvas = surface.getCanvas()
          canvas.clear(ck.TRANSPARENT); renderer.renderSceneToCanvas(canvas, graph, node.parentId); surface.flush()
          const differences = actual => {
            const mismatches = []
            const edgeDiff = new Uint8Array(expected.length)
            pixelmatch(actual, expected, edgeDiff, w, h, { threshold: 0, includeAA: false, diffMask: true, checkerboard: false })
            for (let i = 3; i < expected.length; i += 4) {
              if (actual[i]) assert.deepEqual([...actual.slice(i - 3, i)], [255, 0, 0], 'dash ink retains its RGB channels')
              if (Math.abs(actual[i] - expected[i]) > 1 + Math.ceil(alpha * precision[i / 4 | 0])) {
                const partial = value => value > 0 && value < Math.round(alpha * 255)
                if (!partial(actual[i]) || !partial(expected[i]) || edgeDiff[i]) mismatches.push([i / 4 | 0, actual[i], expected[i]])
              }
            }
            return mismatches
          }
          const actual = canvas.readPixels(0, 0, format), mismatches = differences(actual)
          if (mismatches.length) failures.push(`${w}/${h}/${weight}/${radius}/${alpha}: ${JSON.stringify(mismatches.slice(0, 12))} (${mismatches.length} pixels)`)
          if (cycle === 0 && width === 139) {
            const shifted = new Uint8Array(actual.length)
            for (let y = 0; y < h; y++) shifted.set(actual.subarray(y * w * 4, ((y + 1) * w - 1) * 4), (y * w + 1) * 4)
            assert.ok(differences(shifted).length > 0, 'a one-pixel displacement is not antialiasing')
            assert.ok(differences(new Uint8Array(actual.length)).length > 100, 'missing dashes cannot pass')
            const recolored = actual.slice()
            for (let i = 3; i < recolored.length; i += 4) if (recolored[i]) recolored[i - 2] = 1
            assert.throws(() => differences(recolored), /RGB/, 'paint differences cannot pass the edge classifier')
            // An unchanged fixed native pattern is visibly wrong despite the
            // same clip-precision baseline. The calibration cannot conceal it.
            const canonical = chain(graph, node, 'componentId').at(-1), metadata = canonical.pluginData
            graph.updateNode(canonical.id, { pluginData: [] })
            canvas.clear(ck.TRANSPARENT); renderer.renderSceneToCanvas(canvas, graph, node.parentId); surface.flush()
            assert.ok(differences(canvas.readPixels(0, 0, format)).length > 100)
            graph.updateNode(canonical.id, { pluginData: metadata })
          }
        } finally { image?.delete(); renderer.destroy(); await page.close() }
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
    }
    assert.deepEqual(failures, [])
  } finally { await browser.close() }
})

test('Go source dashed borders retain linked paints, nested copy edits and isolated masters through two saves', async t => {
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"; "github.com/septagon-oss/platformkit/ui/components/examples"
  "github.com/septagon-oss/platformkit/ui/css"
)
func main() {
  var input struct { Label, Border string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  var children []g.Node
  for _, item := range []struct{ id, label string }{{"first", input.Label}, {"second", "Keep this copy"}} {
    children = append(children, examples.ExampleOf(examples.ExampleInfo{
      ID: item.id, ComponentID: "pk-ui.component.button",
    }, components.ButtonProps{Label: item.label}, components.Button).Node)
  }
  example := examples.ExampleWithChildren(examples.ExampleInfo{
    ID: "fixture/dashed", ComponentID: "pk-ui.component.stack",
  }, components.StackProps{Gap: "4"}, children, components.Stack)
  sheet := css.NewSheet().Select("[data-component=button]",
    css.Decl("border", css.Literal(input.Border)), css.Decl("border-radius", css.Literal("12px")))
  snapshot, err := ui.Export(design.Default(), []examples.Example{example}, ui.Extra{Sheets: []*css.Sheet{sheet}})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const id = 'fixture/dashed', border = '1px dashed var(--pk-color-border-default)'
  const snapshot = source({ label: 'Create album', border }), fonts = suppliedFonts([400, 500, 600])
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900)), previous = getTextMeasurer()
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const root = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const child = (graph, localId) => graph.getChildren(root(graph).id).find(node => origin(node)?.localId === localId)
  const close = (a, b) => assert.ok(Math.abs(a - b) <= 1 / 64, `${a} versus ${b}`)
  const authored = ({ figmaDerivedLayout, ...node }) => node
  try {
    for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const viewport = { width, height: 900 }, options = { examples: [id], fonts, browser, renderer, mode, viewport }
      let { graph } = await buildComponentDocument(snapshot, options)
      for (let cycle = 0; cycle < 3; cycle++) {
        const instance = child(graph, 'first'), sibling = child(graph, 'second')
        const master = chain(graph, instance, 'componentId').at(-1), metadata = origin(master)
        assert.deepEqual(metadata.cssBorder, { version: 1, style: 'dashed', weight: 1 }, JSON.stringify({ metadata, strokes: master.strokes }))
        assert.equal(instance.type, 'INSTANCE'); assert.notEqual(instance.id, master.id)
        const binding = instance.boundVariables['strokes/0/color']
        assert.equal(graph.variables.get(binding).name, '--pk-color-border-default')
        assert.deepEqual(instance.dashPattern, [3, 2]); assert.deepEqual(instance.strokes[0].dashPattern, [3, 2])
        const masters = structuredClone([...graph.getAllNodes()].filter(node => node.type === 'COMPONENT'))
        const untouched = structuredClone(sibling), editor = createEditor({ graph })
        editor.setCanvasKit(ck, renderer)
        const before = structuredClone([...graph.getAllNodes()])
        const label = metadata.textBindings.find(item => item.property === 'label').id
        const text = `Create this longer album ${cycle}`
        editor.setInstanceComponentProperty(instance.id, label, text)
        const proposal = extractSourceProps(graph, instance, snapshot)
        assert.equal(proposal.status, 'proposal')
        assert.equal(proposal.proposal.props.label, text)
        const observed = await captureExample(browser, source({ label: text, border }), id, { mode, viewport, fonts })
        const first = observed.roots[0].children.find(node => node.source?.path?.at(-1) === 'first')
        close(instance.width, first.bounds.width); close(instance.height, first.bounds.height)
        editor.undoAction()
        assert.deepEqual([...graph.getAllNodes()], before, 'undo restores the exact layout cache as well as authored state')
        assert.equal(extractSourceProps(graph, instance, snapshot).status, cycle === 0 ? 'no-supported-changes' : 'proposal')
        editor.redoAction()
        assert.equal(extractSourceProps(graph, instance, snapshot).proposal.props.label, text)
        // Parent reflow invalidates imported derived-layout caches. It must
        // retain the sibling's geometry, identity, paints and authored state.
        assert.deepEqual(authored(sibling), authored(untouched), 'editing one child does not change the other occurrence')
        assert.deepEqual([...graph.getAllNodes()].filter(node => node.type === 'COMPONENT'), masters)
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
      for (const unsupported of ['1px dotted red', '3px double red']) {
        await assert.rejects(buildComponentDocument(source({ label: 'Create album', border: unsupported }), options), /border/)
      }
    }
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
  assert.equal(browser.contexts().length, 0)
})
