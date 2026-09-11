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
import { isDeepStrictEqual } from 'node:util'
import OpenType from 'opentype.js'
import { fontUnderlineMetrics } from './underline-correction.mjs'

const requireUnderline = (condition, message) => {
  if (!condition) throw new Error(`Native component: underline ${message}`)
}
const round = value => Math.sign(value) * Math.floor(Math.abs(value) + 0.5)

function length(value, fontSize) {
  requireUnderline(/^-?\d+(?:\.\d+)?(?:px|%)$/.test(value), `length ${value} requires further conversion`)
  const result = Number.parseFloat(value) * (value.endsWith('%') ? fontSize / 100 : 1)
  requireUnderline(Number.isFinite(Math.fround(result)), 'requires finite geometry')
  return result
}

// Decoration propagation follows boxes, not computed CSS inheritance. Keep
// the decorating owner even when an inline child says text-decoration:none.
// This is ephemeral observation evidence, not a second component/paint registry.
export function sourceUnderlines(observation, paintFor) {
  const ancestry = new Map()
  function visit(node, parents) {
    if (node.kind === 'element') {
      ancestry.set(node, parents)
      parents = [...parents, node]
    }
    for (const child of node.children ?? []) visit(child, parents)
  }
  for (const root of observation.roots) visit(root, [])
  return (node, face, { control = false, text } = {}) => {
    let origins = []
    const path = [...ancestry.get(node), node]
    for (const item of path) {
      const style = item.style
      if (['absolute', 'fixed'].includes(style.position) || style.float !== 'none' ||
          ['inline-block', 'inline-table', 'inline-flex', 'inline-grid'].includes(style.display)) origins = []
      if (style.display !== 'contents' && style['text-decoration-line'] !== 'none') origins.push(item)
    }
    if (!origins.length) return {}
    requireUnderline(!control && origins.length === 1 && origins[0].style['text-decoration-line'] === 'underline',
      'requires one ordinary text decorating box')
    const origin = origins[0], style = origin.style, descendants = path.slice(path.indexOf(origin))
    requireUnderline(observation.environment?.protocol === '1.3' &&
      /^(?:Headless)?Chrome\/151\.0\.7922\.34$/.test(observation.environment.browser), 'requires the verified Chromium environment')
    requireUnderline(style['text-decoration-style'] === 'solid' && style['text-underline-position'] === 'auto' &&
      ['auto', 'none'].includes(style['text-decoration-skip-ink']), 'style, position or ink skipping requires further conversion')
    // CSS auto deliberately excludes some scripts (including CJK) from ink
    // skipping. Do not replace that policy with the native all-glyph test.
    requireUnderline(style['text-decoration-skip-ink'] === 'none' || typeof text === 'string' &&
      /^(?:\p{Script=Latin}|[\u0300-\u036f]|[\p{Script=Common}&&[\u0000-\u2e7f]])*$/v.test(text),
    'automatic ink skipping currently requires Latin text and common punctuation or symbols')
    requireUnderline(descendants.every(item => ['font-family', 'font-size', 'font-weight', 'font-style', 'line-height', 'text-decoration-skip-ink']
      .every(key => item.style[key] === style[key]) && item.style['writing-mode'] === 'horizontal-tb' &&
      item.style.direction === 'ltr' && (item.style.display !== 'inline' || item.style['vertical-align'] === 'baseline')),
    'mixed decorating-box fonts, baselines or ink-skipping policies require further conversion')
    // Native decoration paint inherits the resolved text fill. Equal baseline
    // RGB is not proof: distinct aliases must remain independently editable.
    requireUnderline(isDeepStrictEqual(paintFor(origin, 'text-decoration-color'), paintFor(node, 'color')),
      'colour must share the exact text paint dependency')
    const size = length(style['font-size'], 1), font = OpenType.parse(face.bytes.buffer)
    const metrics = fontUnderlineMetrics(font, size), declared = style['text-decoration-thickness']
    requireUnderline(declared !== 'from-font', 'font-derived CSS thickness requires further conversion')
    const nominal = Math.max(1, declared === 'auto' ? size / 10 : round(length(declared, size)))
    const thickness = Math.floor(nominal), offset = style['text-underline-offset']
    const top = offset === 'auto' ? Math.max(1, Math.ceil(nominal / 2)) : round(length(offset, size))
    // Chromium's auto offset locates the TOP edge from the alphabetic
    // baseline; OpenPencil shifts the font's underline CENTRE. The pinned
    // DecorationLinePainter floors solid thickness after computing the gap.
    const native = { textDecoration: 'UNDERLINE', textDecorationStyle: 'SOLID', textDecorationThickness: thickness,
      textUnderlineOffset: Math.fround(top + thickness / 2 - metrics.position),
      textDecorationSkipInk: style['text-decoration-skip-ink'] !== 'none' }
    // CSS ink skipping uses the nominal (pre-floor) stripe. Retain that
    // evidence in the existing source record; native property edits outside
    // this projection must resume ordinary native underline semantics.
    return { ...native, cssUnderline: { version: 1, nominalThickness: nominal, native: {
      ...native, fontFamily: face.family, fontWeight: face.weight, italic: face.style === 'italic', fontSize: size,
    } } }
  }
}
