import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { buildComponentDocument } from '../document.mjs'
import { captureExample } from './capture.mjs'
import { emptyStateFixture, suppliedFonts } from './fixtures.test.mjs'

for (const wrap of ['break-word', 'anywhere']) test(`EmptyState ${wrap} retains browser geometry after edits, resizing and two saves`, async t => {
  const source = await emptyStateFixture(t)
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
  const previous = getTextMeasurer(), id = 'fixture/empty'
  const input = { title: 'An empty collection', description: 'A description to edit.', compact: true, action: true,
    constraint: 'overflow-wrap', value: wrap }
  const snapshot = source(input)
  const options = { examples: [id], fonts: suppliedFonts([400, 500, 600]), browser, renderer, viewport: { width: 320, height: 900 } }
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
  try {
    const built = await buildComponentDocument(snapshot, options)
    let { graph } = built
    const property = built.selections[0].properties.find(item => item.name === 'description').id
    for (let cycle = 0; cycle < 3; cycle++) {
      const instance = [...graph.getAllNodes()].find(node => node.name === id && node.type === 'INSTANCE')
      const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
      setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
      for (const width of [320, 390, 1280]) for (const description of [
        'Supercalifragilisticexpialidocious'.repeat(8),
        'João café ' + 'coöperation'.repeat(20) + ' together.',
      ]) {
        options.viewport.width = width
        graph.updateNode(instance.id, { width })
        editor.setInstanceComponentProperty(instance.id, property, description)
        computeLayout(graph, instance.id)
        const observed = (await captureExample(browser, source({ ...input, description }), id, options)).roots[0]
        const expected = observed.children[1], box = graph.getChildren(instance.id)[1], text = graph.getChildren(box.id)[0]
        assert.equal(expected.style['overflow-wrap'], wrap)
        assert.equal(text.text, description, 'emergency breaks do not insert characters into editable source text')
        close(instance.height, observed.bounds.height, `component height (${cycle}/${width}/${description.slice(0, 20)}, box ${box.width}×${box.height}, text ${text.width}×${text.height})`)
        close(box.x, expected.bounds.x - observed.bounds.x, 'description placement')
        close(box.width, expected.bounds.width, 'description width')
        close(box.height, expected.bounds.height, 'description height')
        assert.ok(box.width <= instance.width, 'the unbroken word stays in the available content box')
        const paragraph = renderer.buildParagraph(text, undefined, { halfLeading: true })
        try {
          const lines = paragraph.getLineMetrics(), rects = expected.children[0].rects
          assert.ok(lines.length > 1, 'the fixture actually exercises emergency word breaks')
          assert.equal(lines.length, rects.length)
          for (const [index, line] of lines.entries()) {
            close(line.width, rects[index].width, 'shaped line width')
            close(text.x + line.left, rects[index].x - expected.bounds.x, 'centered line placement')
          }
        } finally { paragraph.delete() }
      }
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
