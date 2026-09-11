import { isDeepStrictEqual } from 'node:util'
import { parseColor } from '@open-pencil/core/color'
import { computedColor } from './computed-color.mjs'
import { resolveColorExpression } from './color-expression.mjs'

const requirePaint = (condition, message) => { if (!condition) throw new Error(`Native component: paint ${message}`) }
export const sameColor = (a, b, tolerance = 1e-6) => a && b && ['r', 'g', 'b', 'a'].every(channel =>
  Number.isFinite(a[channel]) && Number.isFinite(b[channel]) && Math.abs(a[channel] - b[channel]) <= tolerance)

function sourceVariable(graph, collection, snapshot, name) {
  const variables = graph.getVariablesForCollection(collection.id).filter(item => item.name === name)
  requirePaint(variables.length === 1 && variables[0].type === 'COLOR', 'one matching native color variable required')
  const variable = variables[0]
  for (const theme of snapshot.themes) {
    const tokens = theme.tokens.filter(item => item.name === name), modes = collection.modes.filter(item => item.name === theme.mode)
    requirePaint(tokens.length === 1 && tokens[0].type === 'color' && modes.length === 1,
      'source color and native mode identities must be unambiguous')
    requirePaint(sameColor(graph.resolveVariable(variable.id, modes[0].modeId), parseColor(tokens[0].value)),
      'native color variable differs from the source palette')
  }
  return variable
}

function expressionPaint(graph, collection, snapshot, observation, source, fill) {
  const candidate = source.expressionCandidate, names = source.tokens.toSorted()
  requirePaint(candidate && /^--[a-z_][\w-]*$/i.test(candidate.customProperty) && typeof candidate.value === 'string' &&
    candidate.customProperties && typeof candidate.customProperties === 'object' && !Array.isArray(candidate.customProperties) &&
    Object.values(candidate.customProperties).every(value => typeof value === 'string') &&
    new Set(names).size === names.length && collection.modes.length === snapshot.themes.length &&
    snapshot.themes.every(theme => !theme.tokens.some(token => token.name === candidate.customProperty)),
  'mixed or derived paint dependencies need an unambiguous authored expression')
  const inputs = new Map(names.map(name => [name, sourceVariable(graph, collection, snapshot, name)]))
  requirePaint(names.every(name => !Object.hasOwn(candidate.customProperties, name)), 'expression shadows a source token')
  for (const mode of collection.modes) {
    const used = new Set()
    const expected = resolveColorExpression(candidate.value, name => {
      if (!inputs.has(name)) return Object.hasOwn(candidate.customProperties, name) ? candidate.customProperties[name] : undefined
      used.add(name)
      return graph.resolveVariable(inputs.get(name).id, mode.modeId)
    })
    requirePaint(isDeepStrictEqual([...used].toSorted(), names), 'expression dependencies differ from the observation')
    if (mode.name === observation.mode) {
      requirePaint(sameColor(fill.color, expected), 'observed paint differs from its authored expression')
      fill.color = expected
    }
  }
  const value = { cssColor: { value: candidate.value, customProperties: Object.fromEntries([
    ...Object.entries(candidate.customProperties), ...[...inputs].map(([name, variable]) => [name, { aliasId: variable.id }]),
  ].sort(([a], [b]) => a.localeCompare(b))) } }
  return { fills: [fill], boundVariables: {}, expressionBindings: {
    'fills/0/color': { name: candidate.customProperty, value, inputs: [...inputs].map(([name, variable]) => [name, variable.id]) },
  } }
}

// Pure source/native validation. Deferred formulas are construction records,
// not native IDs or another role registry; authored CSS owns their identities.
export function observedPaint(graph, collection, snapshot, observation, root, property) {
  const fill = { type: 'SOLID', color: computedColor(root.style[property]), opacity: 1, visible: true }
  const source = root.paintSources[property]
  requirePaint(Array.isArray(source?.tokens), 'paint dependency evidence required')
  if (source.tokens.length === 0) {
    requirePaint(source.directCandidate === null && !source.expressionCandidate, 'literal paint has contradictory alias evidence')
    return { fills: [fill], boundVariables: {} }
  }
  if (source.directCandidate === null && source.expressionCandidate) return expressionPaint(graph, collection, snapshot, observation, source, fill)
  requirePaint(source.tokens.length === 1 && source.directCandidate === source.tokens[0] && !source.expressionCandidate,
    'mixed or derived paint dependencies need further native conversion')
  const variable = sourceVariable(graph, collection, snapshot, source.directCandidate)
  const mode = collection.modes.find(item => item.name === observation.mode)
  const expected = graph.resolveVariable(variable.id, mode.modeId)
  // Legacy CSSOM alpha uses rounded 8-bit serialization; color(srgb) does not.
  const observed = /^rgba?\(/.test(root.style[property]) ? { ...fill.color, a: Math.round(fill.color.a * 255) / 255 } : fill.color
  requirePaint(sameColor(observed, expected), 'observed paint differs from its source token')
  fill.color = expected
  return { fills: [fill], boundVariables: { 'fills/0/color': variable.id } }
}

export function createPaintedNode(graph, type, parentId, props, pending) {
  const { expressionBindings, cssBorder, cssPosition, cssBox, cssUnderline, ...native } = props
  if (cssBorder || cssPosition || cssBox || cssUnderline) {
    const entries = native.pluginData?.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
    requirePaint(entries?.length === 1, 'CSS presentation needs its existing source correspondence')
    native.pluginData = native.pluginData.map(item => item === entries[0]
      ? { ...item, value: JSON.stringify({ ...JSON.parse(item.value), ...(cssBorder ? { cssBorder } : {}), ...(cssPosition ? { cssPosition } : {}),
        ...(cssBox ? { cssBox } : {}), ...(cssUnderline ? { cssUnderline } : {}) }) } : item)
  }
  const node = graph.createNode(type, parentId, native)
  if (expressionBindings) pending.push({ node, bindings: expressionBindings })
  return node
}

// Called synchronously only after all font loading, construction and geometry
// checks. Reuse matching source roles without overwriting edited native values.
export function bindPaintExpressions(graph, collection, snapshot, pending, created) {
  if (!pending.length) return
  requirePaint(collection.modes.length === snapshot.themes.length, 'native modes changed during construction')
  const roles = new Map()
  for (const { bindings } of pending) for (const role of Object.values(bindings)) {
    requirePaint(!roles.has(role.name) || isDeepStrictEqual(roles.get(role.name).value, role.value), 'conflicting authored color role')
    roles.set(role.name, role)
    for (const [name, id] of role.inputs) requirePaint(sourceVariable(graph, collection, snapshot, name).id === id,
      'source color identity changed during construction')
  }
  const variables = new Map()
  for (const [name, role] of roles) {
    const matches = graph.getVariablesForCollection(collection.id).filter(variable => variable.name === name)
    requirePaint(matches.length <= 1 && matches.every(variable => variable.type === 'COLOR' &&
      isDeepStrictEqual(variable.valuesByMode, Object.fromEntries(collection.modes.map(mode => [mode.modeId, role.value])))),
    'native authored color role is ambiguous or was edited')
    if (matches[0]) variables.set(name, matches[0])
  }
  for (const [name, role] of roles) if (!variables.has(name)) {
    const variable = graph.createVariable(name, 'COLOR', collection.id, role.value)
    created.push(variable.id)
    variables.set(name, variable)
  }
  for (const { node, bindings } of pending) for (const [field, role] of Object.entries(bindings)) {
    graph.bindVariable(node.id, field, variables.get(role.name).id)
  }
}
