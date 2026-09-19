// Media exists because a failed print job and a picture somebody may not look at
// used to arrive as the same broken-image glyph. Construction accepts both error
// panels, so the coverage ledger would stay green on nothing being thrown; these
// are the witnesses that the panel is the border-box the browser actually painted,
// that the caller's sentence survives as editable text, and that the two states
// stay visibly different from each other and from an empty shelf.
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument, verifyComponentDocument } from '../document.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { exportCore as source, suppliedFonts } from './fixtures.test.mjs'

const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
const rgb = color => `rgb(${[color.r, color.g, color.b].map(channel => Math.round(channel * 255)).join(', ')})`
const surfaceOf = node => rgb(node.fills[0].color)
const borderOf = node => rgb(node.strokes[0].color)
const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')

test('a Media failure and a Media refusal build the source panel and keep the caller\'s own words through two saves', async t => {
  const snapshot = source(), fonts = suppliedFonts([400, 500, 600, 700]), previous = getTextMeasurer()
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(640, 640))
  const surfaces = {}
  t.after(async () => { renderer.destroy(); setTextMeasurer(previous); await browser?.close() })
  for (const id of ['pk-ui.component.media/failed', 'pk-ui.component.media/refused']) {
    const reason = snapshot.examples.find(item => item.id === id).props.reason
    const built = await buildComponentDocument(snapshot, { examples: [id], fonts, browser, renderer, viewport: { width: 320, height: 900 } })
    let graph = built.graph, { instance, observation } = built.selections[0]
    const observed = observation.roots[0]
    // The panel is what the browser painted at this width, not a box this adapter guessed.
    for (const field of ['width', 'height']) close(instance[field], observed.bounds[field], `${id}/${field}`)
    // One child, the paragraph, inset by exactly the source padding plus its 1px border.
    const [paragraph] = graph.getChildren(instance.id), [word] = graph.getChildren(paragraph.id)
    assert.equal(observed.children[0].tag, 'p')
    assert.equal(graph.getChildren(paragraph.id).length, 1, `${id}: one paragraph, no invented decoration`)
    for (const field of ['x', 'y', 'width', 'height']) close(paragraph[field], observed.children[0].bounds[field], `${id}/paragraph/${field}`)
    // The news is the caller's sentence, in one text node, wrapping inside the panel.
    assert.equal(word.text, reason, `${id}: the panel keeps the caller's words`)
    assert.ok(word.height > word.fontSize * 2, `${id}: the sentence wraps rather than truncating`)
    // And the state stays visible: the browser's own computed surface and border, per state.
    assert.equal(surfaceOf(instance), observed.style['background-color'], `${id} surface`)
    assert.equal(borderOf(instance), observed.style['border-top-color'], `${id} border`)
    assert.equal(instance.strokes[0].weight, parseFloat(observed.style['border-top-width']), `${id} border width`)
    assert.equal(instance.topLeftRadius, parseFloat(observed.style['border-top-left-radius']), `${id} corner`)
    surfaces[id] = surfaceOf(instance)
    // The message stays an editable source string, so a designer edits the words and
    // the export reads them back rather than receiving pixels it cannot name.
    const editable = extractSourceProps(graph, instance, snapshot)
    assert.equal(editable.status, 'no-supported-changes')
    assert.deepEqual(editable.properties, ['reason'], `${id}: the reason is the editable property`)
    for (let cycle = 0; cycle < 2; cycle++) {
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      verifyComponentDocument(graph, snapshot, [id])
      const saved = [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
      close(saved.height, observed.bounds.height, `${id}: height after save ${cycle + 1}`)
      assert.equal(graph.getChildren(graph.getChildren(saved.id)[0].id)[0].text, reason, `${id}: the words after save ${cycle + 1}`)
    }
  }
  // The claim Media was written for, asked of the construction: a refusal is not a
  // failure, and a colour difference is the only place that difference can live once
  // the picture is gone.
  assert.notEqual(surfaces['pk-ui.component.media/failed'], surfaces['pk-ui.component.media/refused'],
    'a permission hidden behind a failure is the lie Media exists to stop')
})
