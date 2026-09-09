import { fileURLToPath } from 'node:url'
import { chain } from './exporter-correction.mjs'

/*!
 * Dash fitting and border geometry adapted from Blink.
 * Copyright (C) 2013 Google Inc. All rights reserved.
 * Copyright 2015 The Chromium Authors.
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
// CSS dashes fit the available edge/perimeter, unlike a fixed native pattern.
// Fitting follows the source browser; rasterizer clip precision is measured
// independently by browser conformance, not folded into the dash geometry.
// https://chromium.googlesource.com/chromium/src/+/151.0.7922.34/third_party/blink/renderer/platform/graphics/styled_stroke_data.cc
export function cssDashPattern(length, weight, closed = false) {
  const [dash, preferred] = cssDashIntervals(weight)
  if (length <= 2 * dash) return []
  const pair = 2 * dash + preferred * (closed ? 2 : 1)
  if (length <= pair) return [dash * length / pair, preferred * length / pair]
  const count = Math.floor((length + (closed ? 0 : preferred)) / (dash + preferred))
  const gaps = [count, count + 1].map(n => (length - n * dash) / (n - (closed ? 0 : 1)))
  return [dash, gaps[1] <= 0 || Math.abs(gaps[0] - preferred) < Math.abs(gaps[1] - preferred) ? gaps[0] : gaps[1]]
}

export function cssDashIntervals(weight) {
  return [weight * (weight < 3 ? 3 : 2), weight * (weight < 3 ? 2 : 1)]
}

export function cssBorderRecord(graph, node, stroke) {
  let master, source
  try {
    master = chain(graph, node, 'componentId').at(-1)
    const entries = master.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
    if (entries.length !== 1) return
    source = JSON.parse(entries[0].value)
  } catch { return }
  const record = source?.cssBorder, weight = record?.weight
  if (source?.schema !== 'platformkit.design-export.v1' || record?.version !== 1 || record.style !== 'dashed' ||
      !Number.isFinite(weight) || weight <= 0 || stroke.weight !== weight || stroke.align !== 'INSIDE' || ![undefined, 'SOLID'].includes(stroke.type) ||
      node.strokes.length !== 1 || node.independentStrokeWeights || node.cornerSmoothing || node.strokeGeometry?.length ||
      !['FRAME', 'COMPONENT', 'INSTANCE', 'RECTANGLE'].includes(node.type) ||
      ![node.width, node.height].every(value => Number.isFinite(value) && value > 2 * weight)) return
  const fallback = cssDashIntervals(weight)
  if (![node.dashPattern, stroke.dashPattern].every(pattern => Array.isArray(pattern) && pattern.length === 2 &&
      pattern.every((value, i) => value === fallback[i]))) return
  const radii = node.independentCorners
    ? [node.topLeftRadius, node.topRightRadius, node.bottomRightRadius, node.bottomLeftRadius] : Array(4).fill(node.cornerRadius)
  if (!radii.every(value => Number.isFinite(value) && value >= 0 && value === radii[0])) return
  return { weight, radius: Math.min(radii[0], node.width / 2, node.height / 2) }
}

function roundedCenterline(ck, width, height, inset, radius) {
  const path = new ck.Path(), r = Math.max(0, radius - inset)
  // Chromium starts after the upper-left corner. CanvasKit addRRect starts
  // elsewhere, which changes every dash, even for identical radii and length.
  path.addRRect(ck.RRectXY(ck.LTRBRect(inset, inset, height - inset, width - inset), r, r))
  path.transform(0, -1, width, 1, 0, 0, 0, 0, 1)
  return path
}

export function drawCSSBorder(renderer, canvas, graph, node, stroke, color) {
  const record = cssBorderRecord(graph, node, stroke)
  if (!record) return false
  const { ck } = renderer, { weight, radius } = record, { width, height } = node
  const alpha = color.a * stroke.opacity
  if (alpha === 0) return true
  const paint = new ck.Paint(), opacity = new ck.Paint()
  paint.setAntiAlias(true); paint.setStyle(ck.PaintStyle.Stroke)
  paint.setStrokeCap(ck.StrokeCap.Butt); paint.setStrokeJoin(ck.StrokeJoin.Miter)
  paint.setColor(ck.Color4f(color.r, color.g, color.b, 1))
  opacity.setAlphaf(alpha)
  function draw(path, length, closed) {
    const pattern = cssDashPattern(length, weight, closed)
    const effect = pattern.length ? ck.PathEffect.MakeDash(pattern, 0) : null
    try { paint.setPathEffect(effect); canvas.drawPath(path, paint) }
    finally { paint.setPathEffect(null); effect?.delete() }
  }
  const saves = canvas.getSaveCount()
  canvas.save()
  try {
    if (radius > 0) {
      const outerPath = roundedCenterline(ck, width, height, 0, radius)
      canvas.clipPath(outerPath, ck.ClipOp.Intersect, true); outerPath.delete()
      const innerPath = roundedCenterline(ck, width, height, weight, radius)
      canvas.clipPath(innerPath, ck.ClipOp.Difference, true); innerPath.delete()
      if (alpha < 1) canvas.saveLayer(opacity, ck.LTRBRect(0, 0, width, height))
      const path = roundedCenterline(ck, width, height, Math.floor(weight / 2), radius)
      const iterator = new ck.ContourMeasureIter(path, false, 1), measure = iterator.next()
      try {
        // Blink computes this multiplier in float32 before multiplication.
        // A double-precision 2.2 rounds differently for odd border widths.
        paint.setStrokeWidth(Math.fround(2 * Math.fround(1.1)) * weight)
        const c = (radius + weight) / 2
        for (const points of [[[0, 0], [c, c], [width - c, c], [width, 0]],
          [[width, 0], [width - c, c], [width - c, height - c], [width, height]],
          [[width, height], [width - c, height - c], [c, height - c], [0, height]],
          [[0, height], [c, height - c], [c, c], [0, 0]]]) {
          const clip = new ck.Path()
          canvas.save()
          try {
            clip.moveTo(...points[0]); for (const point of points.slice(1)) clip.lineTo(...point)
            canvas.clipPath(clip, ck.ClipOp.Intersect, false)
            draw(path, Math.floor(measure.length()), true)
          } finally { canvas.restore(); clip.delete() }
        }
      } finally { measure?.delete(); iterator.delete(); path.delete(); if (alpha < 1) canvas.restore() }
    } else {
      // Square corners use four open edges with whole end dashes. Clip each
      // corner to its miter so translucent adjacent edges never overlap.
      if (alpha < 1) canvas.saveLayer(opacity, ck.LTRBRect(0, 0, width, height))
      paint.setStrokeWidth(weight)
      for (const [length, turns] of [[width, 0], [height, 1], [width, 2], [height, 3]]) {
        const path = new ck.Path(), clip = new ck.Path()
        canvas.save()
        try {
          if (turns === 1) { canvas.translate(width, 0); canvas.rotate(90, 0, 0) }
          if (turns === 2) { canvas.translate(width, height); canvas.rotate(180, 0, 0) }
          if (turns === 3) { canvas.translate(0, height); canvas.rotate(270, 0, 0) }
          clip.moveTo(0, 0); clip.lineTo(length, 0); clip.lineTo(length - weight, weight); clip.lineTo(weight, weight); clip.close()
          canvas.clipPath(clip, ck.ClipOp.Intersect, false)
          path.moveTo(0, weight / 2); path.lineTo(length, weight / 2)
          draw(path, length, false)
        } finally { canvas.restore(); path.delete(); clip.delete() }
      }
      if (alpha < 1) canvas.restore()
    }
  } finally { canvas.restoreToCount(saves); paint.delete(); opacity.delete() }
  return true
}

export function correctCSSBorders(source, replace) {
  source = `import { drawCSSBorder } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  return replace(source, '\t\tdrawNodeStroke(r, canvas, node, rect, hasRadius, stroke, color, sg, vectorPaths, vectorStroke);',
    '\t\tif (!drawCSSBorder(r, canvas, graph, node, stroke, color)) drawNodeStroke(r, canvas, node, rect, hasRadius, stroke, color, sg, vectorPaths, vectorStroke);')
}
