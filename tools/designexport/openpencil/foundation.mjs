import { createHash } from 'node:crypto'
import { isDeepStrictEqual } from 'node:util'
import { DOMParser } from '@xmldom/xmldom'
import svgpath from 'svgpath'
import { SceneGraph, generateId } from '@open-pencil/scene-graph'
import { parseColor } from '@open-pencil/core/color'
import { svgToVectorPaths, createVectorFrameChildren } from '@open-pencil/core/vector'

const hex = /^#(?:[\da-f]{3}|[\da-f]{4}|[\da-f]{6}|[\da-f]{8})$/i
const number = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/
const digest = value => createHash('sha256').update(value).digest('hex')
const provenance = (key, value) => [{ pluginId: 'platformkit', key, value: JSON.stringify(value) }]

function requireValue(condition, message) {
  if (!condition) throw new Error(`Foundation: ${message}`)
}

function indexed(items, key, label) {
  requireValue(Array.isArray(items) && items.length > 0, `${label} must be a nonempty array`)
  const result = new Map()
  for (const item of items) {
    const name = item?.[key]
    requireValue(typeof name === 'string' && name.trim() === name && name !== '', `${label} identity missing`)
    requireValue(!result.has(name), `duplicate ${label} identity: ${name}`)
    result.set(name, item)
  }
  return result
}

function tokenValue(token) {
  requireValue(typeof token.value === 'string' && token.value.trim() !== '', `empty token: ${token.name}`)
  if (token.type === 'fontFamily') return token.value
  requireValue(token.type === 'color' && hex.test(token.value), `unsupported token: ${token.name}`)
  return parseColor(token.value)
}

// Fail explicitly when the canonical set grows beyond the supported SVG subset.
// This is a fidelity boundary for trusted Go exports, not an upload sanitizer.
function inspectSVG(svg) {
  const document = new DOMParser({ onError: (_level, message) => { throw new Error(message) } })
    .parseFromString(svg, 'image/svg+xml')
  const root = document.documentElement
  requireValue(root?.tagName === 'svg' && !document.doctype, 'invalid SVG document')
  requireValue(root.hasAttribute('fill'), 'SVG requires an explicit root fill')
  const viewport = (root.getAttribute('viewBox') ?? '').trim().split(/[\s,]+/)
  requireValue(viewport.length === 4 && viewport.every(value => number.test(value) && Number.isFinite(Number(value))) && Number(viewport[2]) > 0 && Number(viewport[3]) > 0, 'invalid SVG viewBox')
  const attributes = {
    svg: ['xmlns', 'viewBox', 'fill', 'stroke'],
    g: ['transform', 'fill', 'stroke'],
    path: ['d', 'transform', 'fill', 'stroke', 'fill-rule', 'stroke-width', 'stroke-linecap', 'stroke-linejoin'],
    circle: ['cx', 'cy', 'r', 'fill', 'stroke', 'stroke-width'],
  }
  let paths = 0
  const presentation = new Map()
  for (const element of [root, ...root.getElementsByTagName('*')]) {
    const allowed = attributes[element.tagName]
    requireValue(allowed && (element === root || element.tagName !== 'svg'), `unsupported SVG element: ${element.tagName}`)
    requireValue(element.namespaceURI === 'http://www.w3.org/2000/svg', 'unsupported SVG namespace')
    const inherited = presentation.get(element.parentNode)
    const paint = {
      fill: element.getAttribute('fill') ?? inherited?.fill,
      stroke: element.getAttribute('stroke') ?? inherited?.stroke ?? 'none',
      transformed: element.hasAttribute('transform') || inherited?.transformed,
    }
    presentation.set(element, paint)
    for (const attribute of element.attributes) {
      requireValue(allowed.includes(attribute.name), `unsupported SVG attribute: ${attribute.name}`)
      const value = attribute.value
      if (['fill', 'stroke'].includes(attribute.name)) {
        requireValue(['currentColor', 'none'].includes(value) || hex.test(value), 'unsupported SVG paint')
      }
      if (attribute.name === 'transform') {
        const commands = [...value.matchAll(/(matrix|translate|scale|rotate|skewX|skewY)\(([^()]*)\)/g)]
        requireValue(commands.length > 0 && value.replace(/(matrix|translate|scale|rotate|skewX|skewY)\([^()]*\)/g, '').trim() === '', 'invalid SVG transform')
        for (const [, command, args] of commands) {
          const numbers = args.trim().split(/[\s,]+/)
          const arity = { matrix: [6], translate: [1, 2], scale: [1, 2], rotate: [1, 3], skewX: [1], skewY: [1] }
          requireValue(numbers.every(value => number.test(value) && Number.isFinite(Number(value))) && arity[command].includes(numbers.length), 'invalid SVG transform arguments')
        }
      }
      if (['cx', 'cy', 'r', 'stroke-width'].includes(attribute.name)) {
        requireValue(number.test(value.trim()) && Number.isFinite(Number(value)), 'invalid SVG dimension')
        if (['r', 'stroke-width'].includes(attribute.name)) requireValue(Number(value) > 0, 'invalid SVG dimension')
      }
      const choices = { 'fill-rule': ['nonzero', 'evenodd'], 'stroke-linecap': ['butt', 'round', 'square'], 'stroke-linejoin': ['miter', 'round', 'bevel'] }
      if (choices[attribute.name]) requireValue(choices[attribute.name].includes(value), 'unsupported SVG stroke or winding')
    }
    if (element.tagName === 'path') {
      const parsed = svgpath(element.getAttribute('d') ?? '')
      requireValue(!parsed.err && parsed.segments.length > 1, 'invalid or empty SVG path')
      paths++
    }
    if (element.tagName === 'circle') {
      requireValue(Number(element.getAttribute('r')) > 0, 'invalid SVG circle')
      paths++
    }
    const shape = ['path', 'circle'].includes(element.tagName)
    if (shape) {
      requireValue(paint.fill !== 'none' || paint.stroke !== 'none', 'invisible SVG shape is unsupported')
      requireValue(paint.stroke === 'none' || !paint.transformed, 'transformed SVG strokes are unsupported')
    }
    for (const child of element.childNodes) {
      requireValue((child.nodeType === 1 && !shape) || child.nodeType === 8 || (child.nodeType === 3 && child.textContent.trim() === ''), 'unsupported SVG content')
    }
  }
  requireValue(paths > 0, 'empty SVG')
  return paths
}

function createIcon(graph, parent, icon, foreground) {
  for (const field of ['svg', 'sha256', 'source', 'license']) {
    requireValue(typeof icon[field] === 'string' && icon[field].trim() !== '', `missing icon ${field}: ${icon.name}`)
  }
  requireValue(digest(icon.svg) === icon.sha256, `icon SHA256 mismatch: ${icon.name}`)
  const count = inspectSVG(icon.svg)
  const convert = defaultColor => svgToVectorPaths(icon.svg, { width: 24, height: 24 }, { defaultColor, preserveAspectRatio: true })
  const first = convert('#123456')
  const second = convert('#abcdef')
  requireValue(first?.paths.length === count && second?.paths.length === count, `lost SVG paths: ${icon.name}`)
  const { name, sha256, source, license } = icon
  const master = graph.createNode('COMPONENT', parent.id, {
    name, width: 24, height: 24, fills: [],
    pluginData: provenance('platformkit.icon', { name, sha256, source, license }),
  })
  // Do not flatten adjacent paths: region-local paints bypass native variable bindings.
  createVectorFrameChildren(graph, master.id, first, { x: 0, y: 0, width: 24, height: 24, offsetX: 0, offsetY: 0 })
  const children = graph.getChildren(master.id)
  requireValue(children.length === count, `empty SVG geometry: ${name}`)
  for (const [index, node] of children.entries()) {
    requireValue([node.x, node.y, node.width, node.height].every(Number.isFinite) && node.vectorNetwork.segments.length > 0, `invalid SVG geometry: ${name}`)
    const a = first.paths[index]
    const b = second.paths[index]
    requireValue(isDeepStrictEqual(a.vectorNetwork, b.vectorNetwork), `unstable SVG geometry: ${name}`)
    for (const field of ['fills', 'strokes']) {
      requireValue(a[field].length === b[field].length, `unstable SVG paints: ${name}`)
      for (const [paintIndex, paint] of a[field].entries()) {
        const witness = b[field][paintIndex]
        requireValue(isDeepStrictEqual({ ...paint, color: null }, { ...witness, color: null }), `unstable SVG paint topology: ${name}`)
        if (!isDeepStrictEqual(paint.color, witness.color)) {
          graph.bindVariable(node.id, `${field}/${paintIndex}/color`, foreground.id)
          paint.color = structuredClone(foreground.valuesByMode[graph.variableCollections.get(foreground.collectionId).defaultModeId])
        }
      }
    }
  }
  return master
}

// The caller supplies ui.Export's existing contract. Its SHA is producer
// provenance, not a JS reimplementation of Go's canonical JSON encoding.
export function buildFoundation(snapshot) {
  requireValue(snapshot?.schema === 'platformkit.design-export.v1', 'unsupported snapshot schema')
  requireValue(/^[\da-f]{64}$/.test(snapshot.sha256), 'missing source SHA256')
  requireValue(snapshot.fontPolicy === 'system-fallback-stacks' && typeof snapshot.notices === 'string' && snapshot.notices.trim() !== '', 'missing font policy or notices')
  const themes = indexed(snapshot.themes, 'mode', 'theme')
  requireValue(themes.size === 2 && themes.has('light') && themes.has('dark'), 'expected light and dark themes')
  const light = indexed(themes.get('light').tokens, 'name', 'token')
  const dark = indexed(themes.get('dark').tokens, 'name', 'token')
  requireValue(light.size === dark.size, 'theme token sets differ')
  const icons = indexed(snapshot.icons, 'name', 'icon')
  const graph = new SceneGraph()
  const page = graph.getPages()[0]
  graph.updateNode(page.id, { name: 'Foundation' })
  const collection = graph.createCollection('Foundation')
  const modes = { light: collection.defaultModeId, dark: generateId() }
  graph.renameMode(collection.id, modes.light, 'light')
  graph.addMode(collection.id, modes.dark, 'dark')
  const variables = new Map()
  for (const [name, token] of light) {
    const counterpart = dark.get(name)
    requireValue(counterpart?.type === token.type, `theme token contract differs: ${name}`)
    const variable = graph.createVariable(name, token.type === 'color' ? 'COLOR' : 'STRING', collection.id, tokenValue(token))
    graph.addVariable({ ...variable, valuesByMode: { [modes.light]: tokenValue(token), [modes.dark]: tokenValue(counterpart) } })
    variables.set(name, graph.variables.get(variable.id))
  }
  const foreground = variables.get('--pk-color-text-primary')
  const background = variables.get('--pk-color-surface-canvas')
  requireValue(foreground?.type === 'COLOR' && background?.type === 'COLOR', 'missing semantic foreground or canvas token')
  const { schema, sha256, fontPolicy, notices } = snapshot
  const height = Math.ceil(icons.size / 6) * 64 + 32
  const foundation = graph.createNode('FRAME', page.id, {
    name: 'Foundation', width: 1328, height: height + 64, fills: [],
    pluginData: provenance('platformkit.source', { schema, sha256, fontPolicy, notices, scope: 'tokens-and-icons' }),
  })
  const masters = graph.createNode('FRAME', foundation.id, { name: 'Icon masters', x: 32, y: 32, width: 400, height, fills: [] })
  const previews = Object.entries(modes).map(([mode, modeId], index) => {
    const frame = graph.createNode('FRAME', foundation.id, {
      name: mode, x: 464 + index * 432, y: 32, width: 400, height,
      variableModes: { [collection.id]: modeId },
      fills: [{ type: 'SOLID', color: background.valuesByMode[modeId], opacity: 1, visible: true }],
    })
    graph.bindVariable(frame.id, 'fills/0/color', background.id)
    return frame
  })
  for (const [index, icon] of [...icons.values()].entries()) {
    const position = { x: 24 + (index % 6) * 64, y: 24 + Math.floor(index / 6) * 64 }
    const master = createIcon(graph, masters, icon, foreground)
    graph.updateNode(master.id, position)
    for (const frame of previews) graph.createInstance(master.id, frame.id, { ...position, name: icon.name })
  }
  return graph
}
