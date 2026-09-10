import { parseNativeNumber } from './variable-number.mjs'

const reject = message => { throw new Error(`Source token metadata: ${message}`) }
const fields = (value, names) => value && typeof value === 'object' && !Array.isArray(value) &&
  Object.keys(value).length === names.length && names.every(name => Object.hasOwn(value, name))
const identity = value => typeof value === 'string' && /^[a-zA-Z0-9_.-]+$/.test(value) && value.length <= 256

// Captured source evidence, not a binding or a proposed source edit. Native
// renames and value changes leave this baseline intact. Unitless is explicit.
export function validateTokenOrigin(value, type) {
  const common = ['version', 'snapshot', 'kind']
  if (value?.version !== 1 || typeof value.snapshot !== 'string' || !/^[a-f0-9]{64}$/.test(value.snapshot)) reject('version or snapshot identity missing')
  if (value.kind === 'color') {
    if (!fields(value, [...common, 'name']) || type !== 'COLOR' || typeof value.name !== 'string' ||
        value.name.length > 256 || !/^--[a-z_][\w-]*$/i.test(value.name)) reject('invalid colour identity')
  } else if (value.kind === 'scale') {
    if (!fields(value, [...common, 'scale', 'key', 'decimal', 'unit']) || type !== 'FLOAT' ||
        !identity(value.scale) || !identity(value.key) || !['px', ''].includes(value.unit) ||
        typeof value.decimal !== 'string' || !/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$/.test(value.decimal)) {
      reject('invalid scale, unit or source decimal')
    }
    parseNativeNumber(value.decimal)
  } else reject('unknown token kind')
}

function guid(value) {
  if (!value || !Number.isSafeInteger(value.sessionID) || !Number.isSafeInteger(value.localID) ||
      value.sessionID < 0 || value.localID < 0) reject('native identity missing')
  return `${value.sessionID}:${value.localID}`
}

export function serializeTokenOrigin(variable, ids) {
  if (variable.sourceToken === undefined) return []
  validateTokenOrigin(variable.sourceToken, variable.type)
  return [{ pluginID: 'platformkit', key: 'source-token', value: JSON.stringify({
    variableId: guid(ids.get(variable.id)), source: variable.sourceToken,
  }) }]
}

export function restoreTokenOrigin(node, type) {
  const entries = (node.pluginData ?? []).filter(entry => entry.pluginID === 'platformkit' && entry.key === 'source-token')
  if (!entries.length) return undefined
  if (entries.length !== 1 || typeof entries[0].value !== 'string' || entries[0].value.length > 16384) reject('invalid source record')
  let data
  try { data = JSON.parse(entries[0].value) } catch { reject('invalid JSON') }
  if (JSON.stringify(data) !== entries[0].value || !fields(data, ['variableId', 'source']) ||
      data.variableId !== guid(node.guid)) reject('ambiguous or mismatched ownership')
  validateTokenOrigin(data.source, type)
  return structuredClone(data.source)
}
