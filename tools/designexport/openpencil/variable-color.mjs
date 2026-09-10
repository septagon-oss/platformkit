import { resolveColorExpression } from './color-expression.mjs'

const reject = message => { throw new Error(`Native CSS color: ${message}`) }
const record = value => value && typeof value === 'object' && !Array.isArray(value)
const channels = ['r', 'g', 'b', 'a']

export function validateResolvedColor(color) {
  if (!record(color) || !channels.every(key => Number.isFinite(color[key]) && color[key] >= 0 && color[key] <= 1)) {
    reject('unresolved native color')
  }
}

export function validateResolvedColors(graph) {
  const contexts = new Set([...graph.variableCollections.values()].flatMap(owner => owner.modes.map(mode => mode.modeId)))
  for (const variable of graph.variables.values()) {
    if (variable.type !== 'COLOR') continue
    const owner = graph.variableCollections.get(variable.collectionId)
    if (!owner?.variableIds.includes(variable.id) || !Object.hasOwn(variable.valuesByMode, owner.defaultModeId)) reject('color default or owner missing')
    for (const [mode, value] of Object.entries(variable.valuesByMode)) {
      if (!owner.modes.some(item => item.modeId === mode)) reject('unknown native mode')
      if (record(value) && 'aliasId' in value) {
        if (Object.keys(value).length !== 1 || graph.variables.get(value.aliasId)?.type !== 'COLOR') reject('color aliases require a COLOR target')
      } else if (!record(value) || !('cssColor' in value)) validateResolvedColor(value)
    }
    for (const mode of contexts) validateResolvedColor(graph.resolveVariable(variable.id, mode))
  }
}

// The expression is authored CSS, not a native expression language. Only its
// external custom-property inputs become ordinary native alias identities.
function mapInputs(formula, mapAlias) {
  if (!record(formula) || Object.keys(formula).length !== 2 || typeof formula.value !== 'string' ||
      !record(formula.customProperties)) reject('invalid authored expression')
  const entries = Object.entries(formula.customProperties)
  if (entries.length > 256) reject('too many expression inputs')
  const customProperties = Object.fromEntries(entries.map(([name, value]) => {
    if (!/^--[a-z_][\w-]*$/i.test(name)) reject('invalid custom property identity')
    if (typeof value === 'string') return [name, value]
    if (!record(value) || Object.keys(value).length !== 1 || typeof value.aliasId !== 'string' ||
        value.aliasId.length === 0) reject('invalid native alias input')
    return [name, mapAlias(value.aliasId)]
  }))
  return { value: formula.value, customProperties }
}

export function resolveCSSColor(formula, resolveAlias) {
  const mapped = mapInputs(formula, id => {
    const color = resolveAlias(id)
    validateResolvedColor(color)
    return color
  })
  return resolveColorExpression(mapped.value, name => Object.hasOwn(mapped.customProperties, name) ?
    mapped.customProperties[name] : undefined)
}

function mappedId(ids, id) {
  const guid = ids.get(id)
  if (!guid) reject(`missing exported identity: ${id}`)
  return `${guid.sessionID}:${guid.localID}`
}

// FIG has no verified CSS color-mix operation. Retain authored CSS in native
// plugin data, alongside a resolved COLOR for readers without this extension.
// GUIDs are assigned by the native exporter; never rewrite CSS text or names.
export function serializeCSSColors(graph, variable, varIds, modeIds) {
  const values = Object.entries(variable.valuesByMode).filter(([, value]) => record(value) && 'cssColor' in value)
    .map(([modeId, value]) => {
      if (variable.type !== 'COLOR') reject('expressions require a COLOR variable')
      return {
        modeId: mappedId(modeIds, modeId),
        cssColor: mapInputs(value.cssColor, id => ({ aliasId: mappedId(varIds, id) })),
        color: graph.resolveVariable(variable.id, modeId),
      }
    })
  return values.length ? [{ pluginID: 'platformkit', key: 'css-color-values', value: JSON.stringify({ version: 1, values }) }] : []
}

export function restoreCSSColors(node, type, valuesByMode) {
  const entries = (node.pluginData ?? []).filter(entry => entry.pluginID === 'platformkit' && entry.key === 'css-color-values')
  if (!entries.length) return valuesByMode
  if (entries.length !== 1 || type !== 'COLOR' || typeof entries[0].value !== 'string' ||
      entries[0].value.length > 1048576) reject('invalid expression metadata')
  let data
  try { data = JSON.parse(entries[0].value) } catch { reject('invalid expression JSON') }
  if (!record(data) || data.version !== 1 || !Array.isArray(data.values) || !data.values.length) reject('unsupported expression metadata')
  const result = { ...valuesByMode }, modes = new Set()
  for (const entry of data.values) {
    if (!record(entry) || typeof entry.modeId !== 'string' || modes.has(entry.modeId) ||
        !Object.hasOwn(valuesByMode, entry.modeId)) reject('missing or duplicate expression mode')
    modes.add(entry.modeId)
    // A foreign editor may change the literal while retaining old metadata.
    // Refuse that conflict rather than resurrecting the old authored formula.
    if (!channels.every(key => Number.isFinite(entry.color?.[key]) && entry.color[key] >= 0 && entry.color[key] <= 1 &&
        Math.fround(entry.color[key]) === valuesByMode[entry.modeId]?.[key])) reject('expression fallback was changed externally')
    result[entry.modeId] = { cssColor: mapInputs(entry.cssColor, id => {
      if (!/^\d+:\d+$/.test(id)) reject('invalid imported alias identity')
      return { aliasId: id }
    }) }
  }
  return result
}

export function validateCSSColors(graph) {
  const modes = new Set([...graph.variableCollections.values()].flatMap(collection => collection.modes.map(mode => mode.modeId)))
  for (const variable of graph.variables.values()) for (const [mode, value] of Object.entries(variable.valuesByMode)) {
    if (!record(value) || !('cssColor' in value)) continue
    if (!graph.variableCollections.get(variable.collectionId)?.modes.some(item => item.modeId === mode)) reject('unknown native mode')
    // A different collection's mode can select this formula's fallback while
    // still selecting a different input value. Check those native contexts too.
    for (const requestedMode of modes) graph.resolveVariable(variable.id, requestedMode)
  }
}

// Validation sees an ephemeral variable map, never temporarily invalid live
// state. Native resolution remains the only dependency/mode implementation.
function validateCandidate(graph, variables) {
  const candidate = Object.create(graph)
  candidate.variables = variables
  validateCSSColors(candidate)
}

export function setNativeVariableValue(graph, variable, modeId, value, present = true) {
  if (present && !graph.variableCollections.get(variable.collectionId)?.modes.some(mode => mode.modeId === modeId)) {
    reject('unknown native mode')
  }
  const valuesByMode = { ...variable.valuesByMode }
  if (present) valuesByMode[modeId] = structuredClone(value)
  else delete valuesByMode[modeId]
  const variables = new Map(graph.variables)
  variables.set(variable.id, { ...variable, valuesByMode })
  validateCandidate(graph, variables)
  variable.valuesByMode = valuesByMode
}

export function validateCSSColorRemoval(graph, ids) {
  const variables = new Map(graph.variables)
  for (const id of ids) variables.delete(id)
  validateCandidate(graph, variables)
  const candidate = Object.create(graph)
  candidate.variables = variables
  validateResolvedColors(candidate)
}
