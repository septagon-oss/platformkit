import { setNativeVariableValue } from './variable-color.mjs'
import { planNumericBindings, applyNumericBindings } from './variable-binding.mjs'

const reject = message => { throw new Error(`Native number: ${message}`) }
const fields = (value, names) => value && typeof value === 'object' && !Array.isArray(value) &&
  Object.keys(value).length === names.length && names.every(name => Object.hasOwn(value, name))
const spelling = value => Object.is(value, -0) ? '-0' : String(value)

// The editable value is binary64. FIG's ordinary fallback is binary32; keep
// both finite, while retaining underflow and signed zero in the extension.
function nativeNumber(value) {
  if (typeof value !== 'number' || !Number.isFinite(value) || !Number.isFinite(Math.fround(value))) {
    reject('a finite number with a finite FIG fallback is required')
  }
  return value
}

function decimalIdentity(text) {
  const match = /^([+-]?)(?:(\d+)(?:\.(\d*))?|\.(\d+))(?:[eE]([+-]?\d+))?$/.exec(text)
  if (!match) reject('enter a complete decimal number without units')
  const fraction = match[3] ?? match[4] ?? ''
  const digits = ((match[2] ?? '') + fraction).replace(/^0+/, '')
  const sign = match[1] === '-' ? '-' : ''
  if (!digits) return sign + '0'
  const significant = digits.replace(/0+$/, '')
  const exponent = Number(match[5] ?? 0) - fraction.length + digits.length - significant.length
  return `${sign}${significant}e${exponent}`
}

// The UI accepts decimal spellings of native values, not lossy parseFloat
// prefixes or decimals rounded before the author can review them. This does
// not preserve authored source-token spelling; that is a separate contract.
export function parseNativeNumber(raw) {
  if (typeof raw !== 'string' || raw.length > 1024) reject('bounded decimal text required')
  const text = raw.trim(), identity = decimalIdentity(text)
  const value = nativeNumber(Number(text))
  if (identity !== decimalIdentity(spelling(value))) reject('decimal would lose precision in the native editor')
  return value
}

export function validateNumericVariables(graph) {
  const contexts = new Set([...graph.variableCollections.values()].flatMap(collection => collection.modes.map(mode => mode.modeId)))
  const owners = new Map()
  for (const collection of graph.variableCollections.values()) {
    for (const id of collection.variableIds) owners.set(id, (owners.get(id) ?? 0) + 1)
  }
  for (const variable of graph.variables.values()) {
    if (variable.type !== 'FLOAT') continue
    const collection = graph.variableCollections.get(variable.collectionId)
    if (!collection?.variableIds.includes(variable.id) || owners.get(variable.id) !== 1) reject('numeric collection ownership missing or ambiguous')
    const modes = new Set(collection.modes.map(mode => mode.modeId))
    if (!modes.size || modes.size !== collection.modes.length || !modes.has(collection.defaultModeId)) {
      reject('unique modes with an owned default are required by the native file path')
    }
    if (!variable.valuesByMode || !Object.hasOwn(variable.valuesByMode, collection.defaultModeId)) reject('numeric default value missing')
    for (const [mode, value] of Object.entries(variable.valuesByMode)) {
      if (!modes.has(mode)) reject('unknown numeric mode')
      if (fields(value, ['aliasId']) && typeof value.aliasId === 'string') {
        if (graph.variables.get(value.aliasId)?.type !== 'FLOAT') reject('numeric aliases require a FLOAT target')
      } else nativeNumber(value)
    }
    // A foreign collection's mode may select an alias input while the owner
    // falls back to its default. Use the SDK resolver, not a second evaluator.
    for (const mode of contexts) {
      let value
      try { value = graph.resolveVariable(variable.id, mode) }
      catch { reject('numeric dependency resolution failed') }
      nativeNumber(value)
    }
  }
}

function candidate(graph, variables) {
  const result = Object.create(graph)
  result.variables = variables
  validateNumericVariables(result)
  return result
}

// Compose the existing colour/value setter without changing its meaning.
// Neither validator sees a temporarily invalid live graph or history entry.
export function setCheckedVariableValue(graph, variable, modeId, value, present = true) {
  const valuesByMode = { ...variable.valuesByMode }
  if (present) valuesByMode[modeId] = structuredClone(value)
  else delete valuesByMode[modeId]
  const variables = new Map(graph.variables)
  variables.set(variable.id, { ...variable, valuesByMode })
  const plan = planNumericBindings(candidate(graph, variables))
  setNativeVariableValue(graph, variable, modeId, value, present)
  applyNumericBindings(graph, plan)
}

export function validateNumericRemoval(graph, ids) {
  const variables = new Map(graph.variables)
  for (const id of ids) variables.delete(id)
  candidate(graph, variables)
}

function guid(value) {
  if (!value || !Number.isSafeInteger(value.sessionID) || !Number.isSafeInteger(value.localID) ||
      value.sessionID < 0 || value.localID < 0) reject('native GUID required')
  return `${value.sessionID}:${value.localID}`
}

function mapped(ids, id) {
  const value = ids.get(id)
  if (!value) reject('exported numeric identity missing')
  return guid(value)
}

export function serializeNumbers(variable, variableIds, modeIds) {
  if (variable.type !== 'FLOAT') return []
  const values = Object.entries(variable.valuesByMode).filter(([, value]) => typeof value === 'number')
    .map(([modeId, value]) => ({ modeId: mapped(modeIds, modeId), number: spelling(nativeNumber(value)) }))
  if (!values.length) return []
  const value = JSON.stringify({ version: 1, variableId: mapped(variableIds, variable.id),
    collectionId: mapped(variableIds, variable.collectionId), values })
  if (value.length > 1048576) reject('numeric metadata exceeds its size limit')
  return [{ pluginID: 'platformkit', key: 'number-values', value }]
}

// Return detached candidate values only. The caller composes restoration and
// validates dependencies before returning its imported graph to the editor.
export function restoreNumbers(node, type, valuesByMode, collection) {
  const entries = (node.pluginData ?? []).filter(entry => entry.pluginID === 'platformkit' && entry.key === 'number-values')
  if (!entries.length) return valuesByMode
  if (entries.length !== 1 || type !== 'FLOAT' || node.variableResolvedType !== 'FLOAT' ||
      typeof entries[0].value !== 'string' || entries[0].value.length > 1048576) reject('invalid numeric metadata')
  let data
  try { data = JSON.parse(entries[0].value) } catch { reject('invalid numeric metadata JSON') }
  if (JSON.stringify(data) !== entries[0].value) reject('canonical numeric metadata JSON required')
  if (!fields(data, ['version', 'variableId', 'collectionId', 'values']) || data.version !== 1 ||
      data.variableId !== guid(node.guid) || data.collectionId !== collection?.id ||
      !Array.isArray(data.values) || !data.values.length) reject('numeric metadata ownership or version mismatch')
  const modes = new Set(collection.modes.map(mode => mode.modeId)), wire = new Map()
  for (const entry of node.variableDataValues?.entries ?? []) {
    const mode = guid(entry.modeID)
    if (!modes.has(mode) || wire.has(mode)) reject('unknown or duplicate numeric wire mode')
    const native = entry.variableData
    if (!['FLOAT', 'ALIAS'].includes(native?.dataType) || native.resolvedDataType !== 'FLOAT') reject('numeric wire type changed')
    if (native.dataType === 'ALIAS' && (!fields(valuesByMode[mode], ['aliasId']) ||
        valuesByMode[mode].aliasId !== guid(native.value?.alias?.guid))) reject('numeric alias was not imported faithfully')
    wire.set(mode, entry.variableData)
  }
  const result = { ...valuesByMode }, restored = new Set()
  for (const entry of data.values) {
    if (!fields(entry, ['modeId', 'number']) || typeof entry.modeId !== 'string' ||
        restored.has(entry.modeId) || !modes.has(entry.modeId) || !Object.hasOwn(valuesByMode, entry.modeId) ||
        typeof entry.number !== 'string') reject('invalid numeric mode record')
    const value = nativeNumber(Number(entry.number)), native = wire.get(entry.modeId)
    if (spelling(value) !== entry.number) reject('canonical native number spelling required')
    if (!native || native.dataType !== 'FLOAT' || native.resolvedDataType !== 'FLOAT' ||
        !native.value || !Object.keys(native.value).every(key => key === 'floatValue') ||
        typeof valuesByMode[entry.modeId] !== 'number' || Math.fround(value) !== valuesByMode[entry.modeId]) {
      reject('numeric fallback was changed externally')
    }
    restored.add(entry.modeId)
    result[entry.modeId] = value
  }
  for (const [mode, native] of wire) {
    if (native.dataType === 'FLOAT' && !restored.has(mode)) reject('numeric metadata is incomplete')
  }
  return result
}
