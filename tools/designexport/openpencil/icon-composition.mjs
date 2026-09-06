import { isDeepStrictEqual } from 'node:util'
import { DOMParser } from '@xmldom/xmldom'
import svgpath from 'svgpath'
import { prepareIcon } from './foundation.mjs'

function requireIcon(condition, message) {
  if (!condition) throw new Error(`Native icon composition: ${message}`)
}

function float32(value) {
  if (typeof value === 'number') {
    requireIcon(Number.isFinite(Math.fround(value)), 'nonfinite native geometry or paint')
    return Math.fround(value)
  }
  if (Array.isArray(value)) return value.map(float32)
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, float32(item)]))
  return value
}

const same = (a, b) => isDeepStrictEqual(float32(a), float32(b))
// CSSOM can serialize 128/255 alpha as 0.5; compare its 8-bit paint value.
const sameObservedColor = (a, b) => a && b && ['r', 'g', 'b', 'a'].every(channel =>
  Number.isFinite(a[channel]) && (channel === 'a' ? Math.round(a[channel] * 255) === Math.round(b[channel] * 255) :
    Math.abs(a[channel] - b[channel]) <= 0.00051))
const attributes = node => Object.fromEntries([...node.attributes].map(attribute => [attribute.name, attribute.value]))
const length = value => typeof value === 'string' && /^-?\d+(?:\.\d+)?px$/.test(value) ? Number.parseFloat(value) : NaN

function path(value) {
  const parsed = svgpath(value)
  requireIcon(!parsed.err, 'invalid observed SVG path')
  return parsed.abs().unshort().segments
}

function requirePlainAppearance(node) {
  const expected = {
    visibility: 'visible', opacity: '1', position: 'static', transform: 'none',
    translate: 'none', rotate: 'none', scale: 'none', zoom: '1',
    'background-color': 'rgba(0, 0, 0, 0)', 'background-image': 'none', 'box-shadow': 'none',
    'outline-style': 'none', 'animation-name': 'none', 'vector-effect': 'none',
    clip: 'auto', 'clip-path': 'none', 'clip-rule': 'nonzero', mask: 'none', filter: 'none',
    'paint-order': 'normal', 'shape-rendering': 'auto', 'color-interpolation': 'srgb',
    'mix-blend-mode': 'normal', isolation: 'auto',
    'marker-start': 'none', 'marker-mid': 'none', 'marker-end': 'none',
    'fill-opacity': '1', 'stroke-opacity': '1', 'stroke-dasharray': 'none',
    'stroke-dashoffset': '0px', 'stroke-miterlimit': '4',
  }
  requireIcon(Object.entries(expected).every(([key, value]) => node.style[key] === value),
    'SVG effects, opacity, positioning or unsupported rendering styles')
  requireIcon(['block', 'inline', 'inline-block'].includes(node.style.display), 'SVG display is unsupported')
  requireIcon(['top', 'right', 'bottom', 'left'].every(side =>
    ['margin', 'padding', 'border'].every(field => length(node.style[`${field}-${side}${field === 'border' ? '-width' : ''}`]) === 0)),
  'SVG box decoration or external spacing is unsupported')
}

// This is a pure construction plan for trusted source observations. The native
// handle is verified against canonical geometry; provenance alone grants nothing.
export function planIcon(graph, asset, svg, nativeMaster, colorCollectionId, observePaint) {
  const prepared = prepareIcon(asset)
  const collection = graph.variableCollections.get(colorCollectionId)
  requireIcon(collection && typeof observePaint === 'function', 'explicit native palette and paint observer required')
  requireIcon(svg?.kind === 'element' && svg.tag === 'svg' && svg.icon?.canonicalName === asset.name,
    'source-resolved canonical SVG identity required')
  requireIcon(nativeMaster?.type === 'COMPONENT' && graph.getNode(nativeMaster.id) === nativeMaster &&
    nativeMaster.width === 24 && nativeMaster.height === 24 && nativeMaster.uniformScaleFactor === null &&
    nativeMaster.layoutMode === 'NONE' && !nativeMaster.fills.length && !nativeMaster.strokes.length,
  'canonical unscaled 24px native master required')
  const provenance = nativeMaster.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.icon')
  requireIcon(provenance.length === 1 && isDeepStrictEqual(JSON.parse(provenance[0].value), prepared.provenance), 'native icon provenance differs')
  const vectors = graph.getChildren(nativeMaster.id)
  requireIcon(vectors.length === prepared.children.length && new Set(nativeMaster.childIds).size === vectors.length,
    'native canonical child topology differs')
  for (const node of [nativeMaster, ...vectors]) {
    requireIcon(node.visible && node.opacity === 1 && node.rotation === 0 && !node.flipX && !node.flipY &&
      !node.effects.length && !node.isMask && !node.clipsContent && !node.componentId &&
      node.cornerRadius === 0 && !node.independentCorners && !node.dashPattern.length && node.strokeMiterLimit === 4 &&
      ['PASS_THROUGH', 'NORMAL'].includes(node.blendMode) && !Object.keys(node.variableModes).length &&
      !node.componentPropertyDefinitions.length && !node.componentPropertyReferences.length,
    'native master contains unsupported transforms, effects or behavior')
  }
  requireIcon(Object.keys(nativeMaster.boundVariables).length === 0, 'native root bindings are unsupported')
  const root = new DOMParser().parseFromString(asset.svg, 'image/svg+xml').documentElement
  const shapes = [...root.childNodes].filter(node => node.nodeType === 1)
  requireIcon(shapes.length === prepared.children.length && shapes.every(node => ['path', 'circle'].includes(node.tagName) &&
    !node.hasAttribute('transform')), 'grouped or transformed SVG composition is not certified')
  const rootAttributes = attributes(root)
  const extra = ['class', 'id', 'focusable', 'role', 'aria-hidden', 'aria-label', 'data-pk-icon', 'data-pk-icon-canonical', 'data-pk-icon-fallback']
  requireIcon(svg.attributes?.['data-pk-icon-canonical'] === asset.name && svg.attributes['data-pk-icon'] === svg.icon.name,
    'captured SVG identity attributes contradict its source identity')
  requireIcon(same(Object.fromEntries(Object.entries(svg.attributes).filter(([key]) => !extra.includes(key))), rootAttributes),
    'observed SVG root attributes differ from canonical source')
  requireIcon(Array.isArray(svg.children) && svg.children.length === shapes.length, 'observed SVG topology differs')
  requirePlainAppearance(svg)
  const size = svg.bounds.width, scale = size / 24
  requireIcon(Number.isFinite(size) && size > 0 && svg.bounds.height === size &&
    length(svg.style.width) === size && length(svg.style.height) === size &&
    svg.style['flex-grow'] === '0' && svg.style['flex-shrink'] === '0' && svg.style['flex-basis'] === 'auto' &&
    ['auto', 'center'].includes(svg.style['align-self']), 'fixed square nonshrinking SVG viewport required')
  const paints = [], roles = new Map()
  for (const [index, shape] of shapes.entries()) {
    const observed = svg.children[index], sourceNode = vectors[index], canonical = prepared.children[index]
    requireIcon(observed.kind === 'element' && observed.tag === shape.tagName && observed.children.length === 0 &&
      same(observed.attributes, attributes(shape)), 'observed SVG child attributes or topology differ')
    requirePlainAppearance(observed)
    requireIcon(sourceNode.type === 'VECTOR' && sourceNode.parentId === nativeMaster.id && sourceNode.childIds.length === 0,
      'canonical native vector handle required')
    for (const field of ['x', 'y', 'width', 'height', 'vectorNetwork', 'fillGeometry', 'strokeGeometry']) {
      requireIcon(same(sourceNode[field], canonical.properties[field] ?? []), `native canonical ${field} differs`)
    }
    const margin = Math.max(0, ...sourceNode.strokes.map(stroke => stroke.weight * 2))
    requireIcon(sourceNode.x >= margin && sourceNode.y >= margin && sourceNode.x + sourceNode.width + margin <= 24 &&
      sourceNode.y + sourceNode.height + margin <= 24, 'SVG viewport clipping requires further native conversion')
    for (const field of ['x', 'y', 'width', 'height']) {
      const expected = canonical.properties[field] * scale + (['x', 'y'].includes(field) ? svg.bounds[field] : 0)
      requireIcon(Number.isFinite(observed.bounds[field]) && Math.abs(observed.bounds[field] - expected) <= 1 / 64,
        'observed SVG bounds differ from canonical geometry')
    }
    requireIcon(same(Object.keys(sourceNode.boundVariables).sort(), [...canonical.currentColorFields].sort()), 'native paint binding fields differ')
    const style = observed.style, attrs = attributes(shape)
    requireIcon(style['fill-rule'] === (attrs['fill-rule'] ?? 'nonzero') &&
      style['stroke-linecap'] === (attrs['stroke-linecap'] ?? 'butt') &&
      style['stroke-linejoin'] === (attrs['stroke-linejoin'] ?? 'miter') &&
      length(style['stroke-width']) === Number(attrs['stroke-width'] ?? 1), 'computed SVG winding or stroke geometry differs')
    if (shape.tagName === 'path') {
      const computed = /^path\("([^"\\]*)"\)$/.exec(style.d)
      requireIcon(computed && same(path(computed[1]), path(attrs.d)), 'CSS path geometry differs from canonical SVG')
    } else for (const field of ['cx', 'cy', 'r']) {
      requireIcon(length(style[field]) === Number(attrs[field] ?? 0), 'CSS circle geometry differs from canonical SVG')
    }
    for (const [nativeField, cssField] of [['fills', 'fill'], ['strokes', 'stroke']]) {
      const expected = canonical.properties[nativeField], actual = sourceNode[nativeField]
      requireIcon(expected.length <= 1 && actual.length === expected.length, 'native solid paint topology differs')
      if (!expected.length) { requireIcon(style[cssField] === 'none', 'unexpected computed SVG paint'); continue }
      const field = `${nativeField}/0/color`, variable = sourceNode.boundVariables[field]
      const nativePaint = { ...actual[0], color: expected[0].color }
      requireIcon(same(nativePaint, expected[0]), 'native canonical paint properties differ')
      if (nativeField === 'strokes') requireIcon(
        (sourceNode.strokeCap === 'NONE' || sourceNode.strokeCap === expected[0].cap) &&
        (sourceNode.strokeJoin === 'MITER' || sourceNode.strokeJoin === expected[0].join), 'native effective stroke style differs')
      const observedPaint = observePaint(observed, cssField), variableId = observedPaint.boundVariables['fills/0/color']
      if (canonical.currentColorFields.includes(field)) {
        requireIcon(graph.variables.get(variable)?.type === 'COLOR' && graph.variables.get(variable)?.collectionId === collection.id &&
          graph.variables.get(variableId)?.type === 'COLOR' && graph.variables.get(variableId)?.collectionId === collection.id &&
          same(actual[0].color, graph.resolveVariable(variable, collection.defaultModeId)), 'native currentColor variable or fallback differs')
        requireIcon(!roles.has(variable) || roles.get(variable) === variableId, 'conflicting native semantic paint roles')
        roles.set(variable, variableId)
        paints.push({ sourceNode, field, variableId })
      } else requireIcon(!variableId && same(actual[0].color, expected[0].color) && sameObservedColor(observedPaint.fills[0].color, expected[0].color),
        'canonical literal paint must remain unchanged and unbound')
    }
  }
  return { master: nativeMaster, scale, paints }
}
