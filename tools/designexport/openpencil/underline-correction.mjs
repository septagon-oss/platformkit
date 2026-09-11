/*!
 * CSS underline geometry and ink skipping adapted from Blink.
 * Copyright 2014, 2017, 2020 The Chromium Authors.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are met:
 *
 * 1. Redistributions of source code must retain the above copyright notice,
 *    this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright notice,
 *    this list of conditions and the following disclaimer in the documentation
 *    and/or other materials provided with the distribution.
 * 3. Neither the name of Google Inc. nor the names of its contributors may be
 *    used to endorse or promote products derived from this software without
 *    specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
 * AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE
 * LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
 * CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
 * SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
 * INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
 * CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
 * POSSIBILITY OF SUCH DAMAGE.
 */
import { weightToStyle } from '@open-pencil/scene-graph'
import { ownSourceLayoutRecord } from './layout-correction.mjs'

const decorated = new WeakMap()
const encoder = new TextEncoder()
const fields = ['fontFamily', 'fontWeight', 'fontSize', 'italic', 'textDecoration',
  'textDecorationStyle', 'textDecorationThickness', 'textUnderlineOffset', 'textDecorationSkipInk', 'textDecorationFills']

export function fontUnderlineMetrics(font, fontSize) {
  const scale = fontSize / font.unitsPerEm, post = font.tables.post
  const thickness = post?.underlineThickness > 0 ? post.underlineThickness * scale : fontSize / 14
  return { thickness, position: post?.underlinePosition < 0 ? -post.underlinePosition * scale : thickness }
}

function rangesFor(node, fontFor, colorFor) {
  const base = Object.fromEntries(fields.map(field => [field, node[field]])), ranges = []
  const source = ownSourceLayoutRecord(node)?.cssUnderline
  function append(start, end, override = {}) {
    if (end <= start) return
    const style = { ...base, ...override }
    let underline
    if (style.textDecoration === 'UNDERLINE') {
      const font = fontFor(style.fontFamily, weightToStyle(style.fontWeight, style.italic))
      if (!font || font.tables.fvar || !['SOLID', 'DOTTED', 'WAVY'].includes(style.textDecorationStyle)) return false
      const metrics = fontUnderlineMetrics(font, style.fontSize)
      const thickness = style.textDecorationThickness ?? metrics.thickness
      // Native offsets move the font's underline position; they are not CSS
      // text-underline-offset values. CSS admission must convert that boundary.
      const offset = metrics.position + (style.textUnderlineOffset ?? 0)
      if (![thickness, offset].every(Number.isFinite) || thickness < 0) return false
      underline = { thickness, offset, skipInk: style.textDecorationSkipInk !== false,
        style: style.textDecorationStyle, color: colorFor(style),
        families: new Set(Object.values(font.names).flatMap(names => Object.values(names.fontFamily ?? {}))) }
      if (source?.version === 1 && Number.isFinite(Math.fround(source.nominalThickness)) && source.nominalThickness >= 1 &&
          Math.floor(source.nominalThickness) === thickness && style.textDecorationStyle === 'SOLID' &&
          source.native && ['fontFamily', 'fontWeight', 'fontSize', 'italic', 'textDecoration', 'textDecorationStyle',
            'textDecorationThickness', 'textUnderlineOffset', 'textDecorationSkipInk'].every(key => source.native[key] === style[key])) {
        underline.cssThickness = source.nominalThickness
      }
    }
    ranges.push({ from: start, to: end, start: encoder.encode(node.text.slice(0, start)).length,
      end: encoder.encode(node.text.slice(0, end)).length, underline })
    return true
  }
  let start = 0
  for (const run of node.styleRuns) {
    if (run.start < start || run.start + run.length > node.text.length ||
      append(start, run.start) === false || append(run.start, run.start + run.length, run.style) === false) return
    start = run.start + run.length
  }
  if (append(start, node.text.length) === false) return
  return ranges
}

function releaseLines(lines) {
  for (const line of lines) for (const run of line.runs) run.typeface?.delete()
}

// Paint native underlines from the same shaped glyphs used for text, selection
// and hit testing. Nothing is added to the scene graph or serialized as paths.
// Unresolved/variable faces and case transforms retain the upstream paragraph;
// the source converter must still refuse them, not claim this as CSS admission.
export function underlineParagraph(ck, node, make, fontFor, colorFor) {
  if (!node.text || node.textCase !== 'ORIGINAL' ||
    ![node, ...node.styleRuns.map(run => ({ ...node, ...run.style }))].some(style => style.textDecoration === 'UNDERLINE' &&
      (style.textUnderlineOffset != null || style.textDecorationThickness != null || style.textDecorationSkipInk === false))) return make(node)
  const ranges = rangesFor(node, fontFor, colorFor)
  if (!ranges?.some(range => range.underline)) return make(node)
  const original = make(node)
  let retained = false
  try {
    const lines = original.getShapedLines()
    try {
      retained = !lines.every(line => line.runs.every(run => Array.from(run.offsets.slice(0, -1)).every(offset => {
        const range = ranges.find(range => offset >= range.start && offset < range.end)
        return range && (!range.underline || range.underline.families.has(run.typeface?.getFamilyName()))
      })))
    } finally { releaseLines(lines) }
    if (retained) return original
    const paragraph = make({ ...node, textDecoration: node.textDecoration === 'UNDERLINE' ? 'NONE' : node.textDecoration,
      styleRuns: node.styleRuns.map(run => run.style.textDecoration === 'UNDERLINE'
        ? { ...run, style: { ...run.style, textDecoration: 'NONE' } } : run) })
    decorated.set(paragraph, { ck, ranges })
    return paragraph
  } finally { if (!retained) original.delete() }
}

function paintSpan(ck, canvas, font, run, span, box, line) {
  if (span.thickness === 0) return
  const positions = Array.from(run.positions).filter((_, index) => index % 2 === 0)
  const x1 = Math.max(line.left, box[0], positions.reduce((a, b) => Math.min(a, b), Infinity))
  const x2 = Math.min(line.left + line.width, box[2], positions.reduce((a, b) => Math.max(a, b), -Infinity))
  if (x2 <= x1) return
  const half = span.thickness / 2, centre = run.positions[1] + span.offset
  const y = span.cssThickness === undefined ? centre : Math.floor(centre - half + 0.5) + half
  const extent = span.style === 'WAVY' ? span.thickness : half
  const paint = new ck.Paint()
  canvas.save()
  try {
    paint.setAntiAlias(true)
    paint.setColor(span.color)
    paint.setStyle(ck.PaintStyle.Stroke)
    paint.setStrokeWidth(span.thickness)
    canvas.clipRect(ck.LTRBRect(x1, y - extent - 1, x2, y + extent + 1), ck.ClipOp.Intersect, true)
    if (span.skipInk) {
      // Chromium tests the nominal stripe inset by half a pixel, then expands
      // the exclusion to cover the painted line. The resolved native thickness
      // alone loses this information for CSS auto (e.g. 1.4px paints as 1px).
      const upper = span.cssThickness === undefined ? y - extent : y - half + 0.5
      const lower = span.cssThickness === undefined ? y + extent : y - half + span.cssThickness - 0.5
      const dilation = span.cssThickness === undefined ? span.thickness : Math.min(span.cssThickness, 13)
      const clipPadding = span.cssThickness === undefined ? 0 : 1
      const intercepts = font.getGlyphIntercepts(run.glyphs, run.positions.slice(0, 2 * run.glyphs.length), upper, lower)
      for (let i = 0; i < intercepts.length; i += 2) {
        canvas.clipRect(ck.LTRBRect(intercepts[i] - dilation, y - extent - clipPadding,
          intercepts[i + 1] + dilation, y + extent + clipPadding), ck.ClipOp.Difference, span.cssThickness === undefined)
      }
    }
    if (span.style === 'DOTTED') {
      const effect = ck.PathEffect.MakeDash([font.getSize() / 14, font.getSize() * 1.5 / 14], 0)
      try { paint.setPathEffect(effect); canvas.drawLine(x1, y, x2, y, paint) }
      finally { paint.setPathEffect(null); effect?.delete() }
    } else if (span.style === 'WAVY') {
      const path = new ck.Path(), step = span.thickness * 2
      try {
        if ((x2 - x1) / step > 4096) throw new Error('Native underline wave complexity limit')
        path.moveTo(x1, y)
        let x = x1, sign = -1
        while (x + step <= x2) {
          path.quadTo(x + span.thickness, y + sign * span.thickness, x + step, y)
          x += step; sign = -sign
        }
        const rest = x2 - x
        if (rest > 0) path.quadTo(x + rest / 2, y + sign * rest / 2, x2, y + sign * (rest - rest * rest / step))
        canvas.drawPath(path, paint)
      } finally { path.delete() }
    } else {
      paint.setStyle(ck.PaintStyle.Fill)
      canvas.drawRect(ck.LTRBRect(x1, y - half, x2, y + half), paint)
    }
  } finally { canvas.restore(); paint.delete() }
}

export function drawUnderlinedParagraph(canvas, paragraph, x, y) {
  const state = decorated.get(paragraph)
  if (!state) return canvas.drawParagraph(paragraph, x, y)
  const { ck, ranges } = state, lines = paragraph.getShapedLines(), metrics = paragraph.getLineMetrics()
  canvas.save()
  try {
    canvas.translate(x, y)
    for (const line of lines) for (const run of line.runs) {
      // Skia omits empty lines from shaped output. Its line lookup consumes
      // UTF-8 offsets, unlike getRectsForRange's UTF-16 bounds below.
      const metric = metrics[paragraph.getLineNumberAt(line.textRange.first)]
      const font = new ck.Font(run.typeface, run.size)
      try {
        if (run.scaleX) font.setScaleX(run.scaleX)
        if (run.fakeBold) font.setEmbolden(true)
        if (run.fakeItalic) font.setSkewX(-0.25)
        for (const range of ranges.filter(range => range.underline)) {
          const start = Math.max(range.from, metric.startIndex), end = Math.min(range.to, metric.endIndex)
          if (start >= end || !run.offsets.some((offset, i) => i < run.glyphs.length &&
            Math.min(offset, run.offsets[i + 1]) < range.end && Math.max(offset, run.offsets[i + 1]) > range.start)) continue
          // A ligature can span a decoration boundary. Native range geometry,
          // not whole-glyph advances, owns the decorated fraction of that glyph.
          for (const box of paragraph.getRectsForRange(start, end, ck.RectHeightStyle.Tight, ck.RectWidthStyle.Tight)) {
            paintSpan(ck, canvas, font, run, range.underline, box.rect, metric)
          }
        }
      } finally { font.delete() }
    }
    canvas.drawParagraph(paragraph, 0, 0)
  } finally { canvas.restore(); releaseLines(lines) }
}
