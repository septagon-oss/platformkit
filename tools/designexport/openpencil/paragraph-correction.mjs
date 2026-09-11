import { Rules } from '@cto.af/linebreak'
import { ownSourceLayoutRecord } from './layout-correction.mjs'
import { drawUnderlinedParagraph } from './underline-correction.mjs'

const paragraphs = new WeakMap()
const breaks = new Rules()
const encoder = new TextEncoder()
const shiftedRect = (rect, x, y) => Float32Array.from(rect, (value, i) => value + (i % 2 ? y : x))

// CanvasKit forcibly splits oversized words. CSS overflow-wrap:normal does
// not. Keep one native TEXT/string; compose its line paragraphs at this private
// provider boundary so measurement, paint, selection and hit testing agree.
// Break opportunities come from UAX #14, advances and glyphs from native Skia.
export function sourceParagraph(node, make) {
  if (ownSourceLayoutRecord(node)?.textWrap !== 'normal-v1' || !node.text ||
      node.textAutoResize === 'WIDTH_AND_HEIGHT') return make(node)
  // These authored native changes leave the converter's proven CSS subset.
  // Preserve native editing instead of applying an incompatible line model.
  if (/[\r\n\t\u00ad]/u.test(node.text) || node.textDirection === 'RTL' ||
      node.textAlignHorizontal === 'JUSTIFIED' || node.textCase !== 'ORIGINAL' ||
      node.textTruncation === 'ENDING') return make(node)
  const original = make({ ...node, textAutoResize: 'WIDTH_AND_HEIGHT' })
  const state = { lines: [], width: undefined }
  function line(start, end) {
    const text = node.text.slice(start, end)
    const styleRuns = node.styleRuns.flatMap(run => {
      const from = Math.max(start, run.start), to = Math.min(end, run.start + run.length)
      return to > from ? [{ ...run, start: from - start, length: to - from }] : []
    })
    const paragraph = make({ ...node, text, styleRuns, textAlignHorizontal: 'LEFT', textAutoResize: 'WIDTH_AND_HEIGHT' })
    paragraph.layout(1e6)
    return { paragraph, start, end, advance: paragraph.getLongestLine(), x: 0, y: 0 }
  }
  function layout(width) {
    if (width === state.width) return
    const lines = []
    let start = 0, pending
    try {
      for (const boundary of breaks.breaks(node.text)) {
        let candidate = line(start, boundary.position)
        if (pending && candidate.advance > width) {
          candidate.paragraph.delete()
          lines.push(pending)
          start = pending.end
          pending = undefined
          candidate = line(start, boundary.position)
        }
        pending?.paragraph.delete()
        pending = candidate
        if (boundary.required) {
          lines.push(pending)
          start = boundary.position
          pending = undefined
        }
      }
      if (pending) { lines.push(pending); pending = undefined }
      let y = 0, byteOffset = 0
      for (const item of lines) {
        item.y = y
        item.byteOffset = byteOffset
        byteOffset += encoder.encode(node.text.slice(item.start, item.end)).length
        item.x = Math.max(0, width - item.advance) * ({ CENTER: 0.5, RIGHT: 1 }[node.textAlignHorizontal] ?? 0)
        y += item.paragraph.getHeight()
      }
    } catch (error) {
      pending?.paragraph.delete()
      for (const item of lines) item.paragraph.delete()
      throw error
    }
    for (const item of state.lines) item.paragraph.delete()
    state.lines = lines
    state.width = width
  }
  const atIndex = index => state.lines.find(item => index >= item.start && index < item.end)
  const atY = y => state.lines.find(item => y < item.y + item.paragraph.getHeight()) ?? state.lines.at(-1)
  function glyph(info, item) {
    if (!info) return null
    const range = info.graphemeClusterTextRange
    return { ...info, graphemeClusterTextRange: { start: range.start + item.start, end: range.end + item.start },
      graphemeLayoutBounds: shiftedRect(info.graphemeLayoutBounds, item.x, item.y) }
  }
  const methods = {
    layout,
    delete() {
      for (const item of state.lines) item.paragraph.delete()
      state.lines = []
      original.delete()
    },
    getHeight: () => state.lines.reduce((height, item) => height + item.paragraph.getHeight(), 0),
    getLongestLine: () => state.lines.reduce((width, item) => Math.max(width, item.advance), 0),
    getAlphabeticBaseline: () => state.lines[0].paragraph.getAlphabeticBaseline(),
    getIdeographicBaseline: () => state.lines[0].paragraph.getIdeographicBaseline(),
    getMaxWidth: () => state.width,
    getNumberOfLines: () => state.lines.length,
    getLineNumberAt: index => state.lines.indexOf(atIndex(index)),
    getLineMetrics: () => state.lines.map((item, lineNumber) => methods.getLineMetricsAt(lineNumber)),
    getLineMetricsAt(lineNumber) {
      const item = state.lines[lineNumber]
      if (!item) return null
      const metrics = item.paragraph.getLineMetricsAt(0)
      return { ...metrics, startIndex: item.start, endIndex: item.end, endIncludingNewline: item.end,
        endExcludingWhitespaces: item.start + metrics.endExcludingWhitespaces,
        left: metrics.left + item.x, baseline: metrics.baseline + item.y, lineNumber,
        isHardBreak: item.end === node.text.length }
    },
    getGlyphPositionAtCoordinate(x, y) {
      const item = atY(y), position = item.paragraph.getGlyphPositionAtCoordinate(x - item.x, y - item.y)
      return { ...position, pos: position.pos + item.start }
    },
    getGlyphInfoAt(index) {
      const item = atIndex(index)
      return item ? glyph(item.paragraph.getGlyphInfoAt(index - item.start), item) : null
    },
    getClosestGlyphInfoAtCoordinate(x, y) {
      const item = atY(y)
      return glyph(item.paragraph.getClosestGlyphInfoAtCoordinate(x - item.x, y - item.y), item)
    },
    getRectsForRange(start, end, heightStyle, widthStyle) {
      return state.lines.filter(item => start < item.end && end > item.start).flatMap(item => item.paragraph.getRectsForRange(Math.max(0, start - item.start),
        Math.min(item.end, end) - item.start, heightStyle, widthStyle)
        .map(box => ({ ...box, rect: shiftedRect(box.rect, item.x, item.y) })))
    },
    getShapedLines() {
      return state.lines.flatMap(item => item.paragraph.getShapedLines().map(shaped => ({ ...shaped,
        textRange: { first: shaped.textRange.first + item.byteOffset, last: shaped.textRange.last + item.byteOffset },
        top: shaped.top + item.y, bottom: shaped.bottom + item.y, baseline: shaped.baseline + item.y,
        runs: shaped.runs.map(run => ({ ...run, offsets: run.offsets.map(offset => offset + item.byteOffset),
          positions: run.positions.map((value, i) => value + (i % 2 ? item.y : item.x)) })),
      })))
    },
  }
  const result = new Proxy(original, { get(target, key) {
    if (Object.hasOwn(methods, key)) return methods[key]
    const value = Reflect.get(target, key)
    return typeof value === 'function' ? value.bind(target) : value
  } })
  try { layout(node.width || 1e6) } catch (error) { original.delete(); throw error }
  paragraphs.set(result, state)
  return result
}

export function drawSourceParagraph(canvas, paragraph, x, y) {
  const state = paragraphs.get(paragraph)
  if (!state) return drawUnderlinedParagraph(canvas, paragraph, x, y)
  for (const item of state.lines) drawUnderlinedParagraph(canvas, item.paragraph, x + item.x, y + item.y)
}
