import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { TextEditor } from '@open-pencil/core/text'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { buildComponentDocument } from '../document.mjs'
import { drawSourceParagraph } from '../paragraph-correction.mjs'
import { exportCore, suppliedFonts } from './fixtures.test.mjs'

test('normal source wrapping preserves words, Unicode offsets, alignment, painting and native text navigation', async () => {
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), surface = ck.MakeSurface(300, 400), renderer = new SkiaRenderer(ck, surface)
  const fonts = suppliedFonts([400]), id = 'pk-ui.component.text/muted'
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
  const samples = ['Album', 'The people and places we remember together.', 'João café coöperation abc-def 12\u00a034']
  try {
    for (const content of samples) {
      const snapshot = exportCore(['--example', id, '--props'], { content, size: 'base' })
      const built = await buildComponentDocument(snapshot, { examples: [id], fonts, browser, renderer, viewport: { width: 320, height: 900 } })
      const native = built.graph.getChildren(built.selections[0].instance.id)[0]
      const page = await browser.newPage({ viewport: { width: 300, height: 400 } })
      try {
        const fontCSS = fonts.map(face => `@font-face { font-family: "${face.family}"; font-weight: ${face.weight}; src: url(data:font/woff;base64,${face.bytes.toString('base64')}); }`).join('')
        await page.setContent(`<style>${fontCSS}${snapshot.css} body { background:white; }</style>${snapshot.examples[0].html}`)
        await page.evaluate(async () => { await document.fonts.ready })
        for (const width of [41.328125, 75, 180]) for (const align of ['LEFT', 'CENTER', 'RIGHT']) {
          const observed = await page.locator('p').evaluate((p, { width, align }) => {
            Object.assign(p.style, { width: `${width}px`, textAlign: align.toLowerCase() })
            const bounds = p.getBoundingClientRect(), text = [...p.childNodes].find(node => node.nodeType === Node.TEXT_NODE), range = document.createRange(), lines = []
            for (let i = 0; i < text.length; i++) {
              range.setStart(text, i); range.setEnd(text, i + 1)
              const rect = range.getBoundingClientRect()
              let line = lines.find(line => line.y === rect.y)
              if (!line) lines.push(line = { y: rect.y, start: i, end: i, left: rect.x - bounds.x, right: rect.right - bounds.x })
              line.end = i + 1; line.right = Math.max(line.right, rect.right - bounds.x)
            }
            return { height: bounds.height, lines }
          }, { width, align })
          const node = { ...native, width, textAlignHorizontal: align }, paragraph = renderer.buildParagraph(node)
          try {
            const metrics = paragraph.getLineMetrics()
            close(paragraph.getHeight(), observed.height, 'paragraph height')
            assert.equal(metrics.length, observed.lines.length, 'same browser line count')
            for (const [i, line] of metrics.entries()) {
              assert.equal(line.startIndex, observed.lines[i].start, 'UTF-16 line start')
              close(line.left, observed.lines[i].left, 'aligned line start')
              close(line.width, observed.lines[i].right - observed.lines[i].left, 'shaped line width')
              assert.equal(paragraph.getLineNumberAt(line.startIndex), i)
              const glyph = paragraph.getGlyphInfoAt(line.startIndex)
              assert.equal(glyph.graphemeClusterTextRange.start, line.startIndex)
              const [x, y, right, bottom] = glyph.graphemeLayoutBounds
              const hit = paragraph.getGlyphPositionAtCoordinate(x + (right - x) / 4, (y + bottom) / 2)
              assert.equal(hit.pos, line.startIndex, 'hit testing uses original string offsets')
              assert.equal(paragraph.getClosestGlyphInfoAtCoordinate(x + 1, (y + bottom) / 2).graphemeClusterTextRange.start, line.startIndex)
              const rect = paragraph.getRectsForRange(line.startIndex, line.startIndex + 1, ck.RectHeightStyle.Max, ck.RectWidthStyle.Tight)[0]
              close(rect.rect[0], x, 'selection starts at painted glyph')
            }
            assert.equal(paragraph.getGlyphInfoAt(content.length), null)
            assert.ok(paragraph.getWordBoundary(0).end > 0)
            assert.deepEqual(paragraph.getRectsForRange(content.length, content.length, ck.RectHeightStyle.Max, ck.RectWidthStyle.Tight), [])
            const shaped = paragraph.getShapedLines()
            assert.equal(shaped.at(-1).textRange.last, new TextEncoder().encode(content).length, 'shaping retains UTF-8 offsets')
            const canvas = surface.getCanvas(); canvas.clear(ck.WHITE)
            drawSourceParagraph(canvas, paragraph, 80, 0); surface.flush()
            const pixels = canvas.readPixels(0, 0, { width: 300, height: 400, alphaType: ck.AlphaType.Unpremul,
              colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
            for (const line of metrics) {
              const top = Math.round(line.baseline - line.ascent), bottom = Math.round(top + line.height)
              assert.ok(pixels.slice(top * 300 * 4, bottom * 300 * 4).some((value, i) => i % 4 !== 3 && value < 200), 'every measured line is painted')
            }
            paragraph.layout(300)
            paragraph.layout(width)
            assert.deepEqual(paragraph.getLineMetrics(), metrics, 'resizing does not accumulate offsets')
          } finally { paragraph.delete() }
          const editing = new TextEditor(ck); editing.setRenderer(renderer); editing.start(node)
          try {
            editing.selectAll()
            assert.equal(editing.getSelectedText(), content, 'copy returns original text without injected line breaks')
            assert.ok(editing.getSelectionRects().length >= observed.lines.length)
            assert.ok(editing.getCaretRect(), 'end-of-text caret remains available')
            editing.moveLeft()
            assert.equal(editing.state.cursor, 0)
            if (observed.lines.length > 1) {
              editing.moveDown()
              assert.equal(editing.state.paragraph.getLineNumberAt(editing.state.cursor), 1, 'Down reaches the next painted line')
              editing.moveUp()
              assert.equal(editing.state.paragraph.getLineNumberAt(editing.state.cursor), 0, 'Up reaches the previous painted line')
            }
            editing.selectLine(0)
            assert.equal(editing.getSelectedText(), content.slice(0, editing.state.paragraph.getLineMetricsAt(0).endExcludingWhitespaces))
            editing.selectAll(); editing.insert('Album', node)
            assert.equal(editing.state.text, 'Album')
            assert.equal(editing.state.paragraph.getNumberOfLines(), 1)
            assert.ok(editing.getCaretRect())
          } finally { editing.stop() }
          assert.equal(native.text, content, 'line planning never mutates source nodes')
        }
        const ordinary = renderer.buildParagraph({ ...native, text: 'Album', width: 1, pluginData: [] })
        try { assert.ok(ordinary.getNumberOfLines() > 1, 'unmarked native paragraphs retain the upstream wrapping policy') }
        finally { ordinary.delete() }
        for (const changes of [{ text: 'Album\nnotes' }, { text: '\n' }, { text: 'Album\tpeople' },
          { text: 'Album', textCase: 'UPPER' }, { text: 'Album', textTruncation: 'ENDING', maxLines: 1 }]) {
          const changed = { ...native, ...changes, width: 41.328125 }
          const legacy = changed.pluginData.map(item => ({ ...item, value: JSON.stringify({ ...JSON.parse(item.value), textWrap: undefined }) }))
          const marked = renderer.buildParagraph(changed), unmarked = renderer.buildParagraph({ ...changed, pluginData: legacy })
          try { assert.deepEqual(marked.getLineMetrics(), unmarked.getLineMetrics(), 'unsupported presentation retains native editing semantics') }
          finally { marked.delete(); unmarked.delete() }
        }
      } finally { await page.close() }
    }
  } finally { renderer.destroy(); await browser.close() }
})
