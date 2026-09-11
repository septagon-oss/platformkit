import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { suppliedFonts } from './fixtures.test.mjs'
import { loadFonts } from '../fonts.mjs'

const width = 420, height = 256
const fill = (r, g, b, a = 1) => ({ type: 'SOLID', color: { r, g, b, a }, visible: true, opacity: 1 })
const red = fill(1, 0, 0), black = fill(0, 0, 0)

async function fixture(t) {
  await loadFonts(suppliedFonts([400, 600]), [400, 600].map(weight => ({ family: 'IBM Plex Sans', weight,
    style: 'normal', text: 'Typography gyjp João café office memories' })))
  const ck = await initCanvasKit(), surface = ck.MakeSurface(width, height), renderer = new SkiaRenderer(ck, surface)
  t.after(() => renderer.destroy())
  await renderer.loadFonts()
  function render(graph) {
    const before = structuredClone([...graph.getAllNodes()]), canvas = surface.getCanvas()
    canvas.clear(ck.WHITE)
    renderer.renderSceneToCanvas(canvas, graph, graph.getPages()[0].id)
    surface.flush()
    const pixels = Uint8Array.from(canvas.readPixels(0, 0, { width, height, alphaType: ck.AlphaType.Unpremul,
      colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }))
    assert.deepEqual([...graph.getAllNodes()], before, 'painting must not create paths, literal overrides or source edits')
    return pixels
  }
  const node = (graph, props = {}, parent = graph.getPages()[0].id) => graph.createNode('TEXT', parent, {
    name: 'Editable underline', text: 'Typography gyjp', x: 12, y: 12, width: 380, height: 230,
    fontFamily: 'IBM Plex Sans', fontWeight: 400, fontSize: 24, lineHeight: 40, textAutoResize: 'NONE',
    textDecoration: 'UNDERLINE', textDecorationStyle: 'SOLID', textDecorationThickness: 2,
    textUnderlineOffset: 0, textDecorationSkipInk: true, textDecorationFills: [red], fills: [black], ...props,
  })
  return { ck, renderer, render, node }
}

function ink(pixels, channel = 0) {
  const result = new Map()
  for (let i = 0; i < pixels.length; i += 4) {
    const amount = pixels[i + channel] - Math.max(pixels[i + (channel + 1) % 3], pixels[i + (channel + 2) % 3])
    if (amount > 0) result.set(i / 4, amount)
  }
  return result
}

const area = values => [...values.values()].reduce((sum, value) => sum + value, 0)
const translated = (values, dy) => new Map([...values].map(([position, amount]) => [position + dy * width, amount]))

test('native underline offsets translate actual paint and pixel thickness is independent of font size', async t => {
  const { node, render } = await fixture(t)
  for (const weight of [400, 600]) for (const size of [16, 24, 32]) {
    const sample = (offset, thickness) => {
      const graph = new SceneGraph()
      node(graph, { text: 'HHHH', fontWeight: weight, fontSize: size, textUnderlineOffset: offset,
        textDecorationThickness: thickness, textDecorationSkipInk: false })
      return ink(render(graph))
    }
    const original = sample(8, 2), moved = sample(16, 2), thick = sample(8, 4)
    assert.ok(original.size > 0)
    assert.deepEqual(moved, translated(original, 8), `${weight}/${size} moves eight pixels, not merely a stored field`)
    assert.ok(Math.abs(area(thick) / area(original) - 2) < 0.02, `${weight}/${size} uses pixel thickness, not a font-dependent multiplier`)
    assert.equal(sample(8, 0).size, 0, 'zero thickness paints no decoration')
  }
})

test('native ink skipping cuts underline ink without changing glyphs or paragraph geometry', async t => {
  const { node, render, renderer } = await fixture(t)
  for (const style of ['SOLID', 'DOTTED', 'WAVY']) {
    const graph = new SceneGraph(), text = node(graph, { textDecorationStyle: style })
    const paragraph = renderer.buildParagraph(text), metrics = paragraph.getLineMetrics()
    paragraph.delete()
    const gaps = render(graph)
    graph.updateNode(text.id, { textDecorationSkipInk: false })
    const through = render(graph), next = renderer.buildParagraph(text)
    try { assert.deepEqual(next.getLineMetrics(), metrics) } finally { next.delete() }
    assert.ok(area(ink(through)) > area(ink(gaps)), `${style} actually skips the descenders`)
    graph.updateNode(text.id, { textDecoration: 'NONE' })
    const plain = render(graph)
    for (let i = 0; i < plain.length; i += 4) if (plain[i] < 100) {
      assert.equal(gaps[i], plain[i], `${style} retains glyph coverage`)
      assert.equal(through[i], plain[i], `${style} paints decoration underneath the glyphs`)
    }
  }
})

test('mixed Unicode runs underline every wrapped line at the native range geometry', async t => {
  const { ck, node, render, renderer } = await fixture(t)
  for (const align of ['LEFT', 'CENTER', 'RIGHT']) for (const boxWidth of [130, 310])
  for (const value of ['João café memories. Typography gyjp.', '\nJoão café memories.\n\nTypography gyjp.']) {
    const graph = new SceneGraph()
    const text = node(graph, { text: value, width: boxWidth, height: 244, textAlignHorizontal: align, textDecorationSkipInk: false,
      textUnderlineOffset: 8, styleRuns: [{ start: 5, length: 4, style: { fontWeight: 600, textDecorationFills: [fill(0, 0, 1)] } }] })
    const paragraph = renderer.buildParagraph(text, ck.BLACK, { halfLeading: true })
    try {
      const before = paragraph.getLineMetrics(), pixels = render(graph)
      assert.ok(ink(pixels, 2).size > 0, 'the accented run keeps its own underline color')
      for (const line of before) {
        if (line.endExcludingWhitespaces <= line.startIndex) continue
        const top = Math.floor(text.y + line.baseline), bottom = Math.ceil(top + 14)
        assert.ok([...ink(pixels)].some(([position]) => Math.floor(position / width) >= top && Math.floor(position / width) <= bottom),
          `${align}/${boxWidth}: each line retains its own baseline`)
      }
      assert.deepEqual(paragraph.getLineMetrics(), before, 'painting does not change line breaking or alignment')
      assert.ok(paragraph.getRectsForRange(5, 9, ck.RectHeightStyle.Max, ck.RectWidthStyle.Tight).length)
      assert.equal(paragraph.getGlyphInfoAt(5).graphemeClusterTextRange.start, 5)
    } finally { paragraph.delete() }
  }
})

test('underline color inherits the resolved text paint and preserves opacity instead of rereading base fills', async t => {
  const { ck, node, renderer } = await fixture(t)
  const { drawSourceParagraph } = await import('../paragraph-correction.mjs')
  const graph = new SceneGraph(), text = node(graph, { text: 'HHHH', textDecorationFills: [], textUnderlineOffset: 8 })
  const surface = ck.MakeSurface(width, height), canvas = surface.getCanvas()
  function sample(alpha) {
    const paragraph = renderer.buildParagraph(text, ck.Color4f(1, 0, 0, alpha), { halfLeading: true })
    try {
      canvas.clear(ck.WHITE); drawSourceParagraph(canvas, paragraph, 12, 12); surface.flush()
      const pixels = canvas.readPixels(0, 0, { width, height, colorType: ck.ColorType.RGBA_8888,
        alphaType: ck.AlphaType.Unpremul, colorSpace: ck.ColorSpace.SRGB })
      const below = 12 + paragraph.getLineMetricsAt(0).baseline + 6
      return new Map([...ink(pixels)].filter(([position]) => Math.floor(position / width) >= below))
    } finally { paragraph.delete() }
  }
  try {
    const opaque = sample(1), translucent = sample(0.5)
    assert.ok(opaque.size > 0, 'the underline must use the resolved red paint, not the stored black fallback')
    assert.ok(Math.abs(area(translucent) / area(opaque) - 0.5) < 0.02)
  } finally { surface.delete() }
})

test('excessive underline wave complexity refuses without changing the graph or leaking canvas state', async t => {
  const { node, render } = await fixture(t), graph = new SceneGraph()
  const text = node(graph, { textDecorationStyle: 'WAVY', textDecorationThickness: 1e-9 })
  const before = structuredClone([...graph.getAllNodes()])
  assert.throws(() => render(graph), /underline wave complexity limit/)
  assert.deepEqual([...graph.getAllNodes()], before)
  graph.updateNode(text.id, { textDecorationThickness: 2 })
  assert.ok(ink(render(graph)).size > 0, 'a subsequent valid draw still uses the original canvas coordinate system')
})

test('a decoration-only range inside a ligature is not lost when native shaping coalesces runs', async t => {
  const { ck, node, render, renderer } = await fixture(t)
  const graph = new SceneGraph(), text = node(graph, { text: 'office', textDecoration: 'NONE',
    styleRuns: [{ start: 2, length: 1, style: { textDecoration: 'UNDERLINE', textUnderlineOffset: 8 } }] })
  const paragraph = renderer.buildParagraph(text, ck.BLACK, { halfLeading: true })
  try {
    const boxes = paragraph.getRectsForRange(2, 3, ck.RectHeightStyle.Tight, ck.RectWidthStyle.Tight)
    const pixels = ink(render(graph))
    assert.ok(pixels.size > 0, 'the selected UTF-16 range must still paint an underline')
    for (const [position] of pixels) {
      const x = position % width - text.x
      assert.ok(boxes.some(box => x + 1 >= box.rect[0] && x < box.rect[2]), 'underline stays within its native text range')
    }
  } finally { paragraph.delete() }
})

test('linked underlined text retains property editing, exact history, unrelated nodes and two saves', async t => {
  const { ck, node, render, renderer } = await fixture(t)
  let graph = new SceneGraph()
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Reusable text', x: 430, width: 400, height: 240,
    componentPropertyDefinitions: [{ id: '30:1', name: 'Label', type: 'TEXT', defaultValue: 'Typography gyjp' }] })
  node(graph, { componentPropertyReferences: [{ propertyId: '30:1', field: 'TEXT' }] }, master.id)
  const target = graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Edited', x: 0 })
  graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Sibling', x: 860 })
  const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
  const before = structuredClone([...graph.getAllNodes()]), pixelsBefore = render(graph)
  editor.setInstanceComponentProperty(target.id, '30:1', 'João café memories gyjp')
  const after = structuredClone([...graph.getAllNodes()]), pixelsAfter = render(graph)
  assert.notDeepEqual(pixelsAfter, pixelsBefore)
  editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before); assert.deepEqual(render(graph), pixelsBefore)
  editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after); assert.deepEqual(render(graph), pixelsAfter)
  for (const previous of before.filter(item => ![target.id, ...graph.getChildren(target.id).map(item => item.id)].includes(item.id))) {
    assert.deepEqual(graph.getNode(previous.id), previous)
  }
  for (let save = 0; save < 2; save++) {
    graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    assert.deepEqual(render(graph), pixelsAfter, `save ${save + 1} retains paint without adding line objects`)
    const reopened = [...graph.getAllNodes()].find(item => item.name === 'Edited')
    assert.equal(reopened.type, 'INSTANCE')
    assert.equal(graph.getChildren(reopened.id)[0].text, 'João café memories gyjp')
    assert.equal([...graph.getAllNodes()].filter(item => ['VECTOR', 'LINE'].includes(item.type)).length, 0)
  }
})

test('native underline values affect live paint as well as surviving FIG transport', async t => {
  const fonts = suppliedFonts([400]), family = 'IBM Plex Sans', text = 'Typography gyjp'
  await loadFonts(fonts, [{ family, weight: 400, style: 'normal', text }])
  const ck = await initCanvasKit(), width = 320, height = 64
  const surface = ck.MakeSurface(width, height), renderer = new SkiaRenderer(ck, surface)
  const format = { width, height, alphaType: ck.AlphaType.Unpremul, colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }
  async function sample(offset, skipInk, decoration = 'UNDERLINE') {
    let graph = new SceneGraph()
    graph.createNode('TEXT', graph.getPages()[0].id, {
      name: 'Underline probe', text, x: 8, y: 8, width: 300, height: 40,
      fontFamily: family, fontWeight: 400, fontSize: 24, lineHeight: 32, textAutoResize: 'NONE',
      textDecoration: decoration, textUnderlineOffset: offset, textDecorationSkipInk: skipInk,
      fills: [{ type: 'SOLID', color: { r: 0, g: 0, b: 0, a: 1 }, opacity: 1, visible: true }],
    })
    const pixels = []
    for (let save = 0; save < 3; save++) {
      const restored = [...graph.getAllNodes()].find(node => node.type === 'TEXT')
      assert.equal(restored.textDecoration, decoration)
      assert.equal(restored.textUnderlineOffset, offset)
      assert.equal(restored.textDecorationSkipInk, skipInk)
      const canvas = surface.getCanvas()
      canvas.clear(ck.WHITE)
      renderer.renderSceneToCanvas(canvas, graph, graph.getPages()[0].id)
      surface.flush()
      pixels.push(Uint8Array.from(canvas.readPixels(0, 0, format)))
      if (save < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
    assert.deepEqual(pixels[1], pixels[0], 'the first save must retain live underline paint')
    assert.deepEqual(pixels[2], pixels[0], 'the second save must retain live underline paint')
    return pixels
  }
  try {
    await renderer.loadFonts()
    const baseline = await sample(0, true), plain = await sample(0, true, 'NONE')
    assert.notDeepEqual(baseline[0], plain[0], 'the control must actually paint an underline')
    for (const [name, offset, skipInk] of [['offset', 8, true], ['ink skipping', 0, false]]) {
      await t.test(name, async () => {
        const changed = await sample(offset, skipInk)
        for (const [save, pixels] of changed.entries()) {
          assert.equal(pixels.some((value, index) => value !== baseline[save][index]), true,
            `${name} is stored but ignored by live paint after ${save} saves`)
        }
      })
    }
  } finally { renderer.destroy() }
})
