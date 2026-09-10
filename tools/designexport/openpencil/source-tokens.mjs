import { resolveColorExpression } from './color-expression.mjs'
import { parseNativeNumber } from './variable-number.mjs'
import { validateTokenOrigin } from './variable-source.mjs'

const reject = message => { throw new Error(`Source tokens: ${message}`) }
const record = value => value && typeof value === 'object' && !Array.isArray(value)
const colorName = value => typeof value === 'string' && value.length <= 256 && /^--[a-z_][\w-]*$/i.test(value)
function shape(value, required, optional = []) {
  if (!record(value) || !required.every(key => Object.hasOwn(value, key)) ||
      Object.keys(value).some(key => ![...required, ...optional].includes(key))) reject('missing or unsupported field')
}
function list(value, label) {
  if (value === undefined) return []
  if (!Array.isArray(value)) reject(`${label} must be an array`)
  return value
}

// Preserve only the source Scalar decimal evidence as an explicit, addressed
// companion to ordinary JSON data. No global object sidecar or token registry
// is retained; callers must pass these spellings with the decoded snapshot.
export function decodeSnapshot(bytes) {
  if (!(bytes instanceof Uint8Array) || bytes.byteLength > 32 * 1024 * 1024) reject('snapshot exceeds the byte limit')
  let text
  try { text = new TextDecoder('utf-8', { fatal: true }).decode(bytes) } catch { reject('expected UTF-8 JSON') }
  const stack = []
  let previous = ''
  for (const [token] of text.matchAll(/"(?:[^"\\]|\\.)*"|[{}\[\]:,]|[^\s{}\[\]:,]+/g)) {
    if (token === '{' || token === '[') stack.push(token === '{' ? new Set() : null)
    if (stack.length > 256) reject('snapshot nesting limit exceeded')
    if (token === '}' || token === ']') stack.pop()
    if (token === ':' && previous.startsWith('"') && stack.at(-1)) {
      let key
      try { key = JSON.parse(previous) } catch { reject('expected one JSON snapshot') }
      if (stack.at(-1).has(key)) reject('duplicate JSON field')
      stack.at(-1).add(key)
    }
    previous = token
  }
  const scalars = new WeakMap()
  let snapshot
  try {
    snapshot = JSON.parse(text, function (key, value, context) {
      if (key === 'value' && typeof value === 'number') scalars.set(this, context.source)
      return value
    })
  } catch { reject('expected one JSON snapshot') }
  const selected = snapshot?.sourceTokens?.scales
  const scalarSpellings = Array.isArray(selected) ? selected.map(scale => ({
    scale: scale?.scale, key: scale?.key, decimal: scalars.get(scale?.number),
  })) : []
  return { snapshot, scalarSpellings }
}

function colorCSS(value, inputs, depth = 0) {
  if (depth > 32) reject('colour declaration nesting limit exceeded')
  shape(value, [], ['literal', 'reference', 'mix'])
  if (Object.keys(value).length !== 1) reject('colour requires one literal, reference or mix')
  if (Object.hasOwn(value, 'literal')) {
    if (typeof value.literal !== 'string' || !/^(?:transparent|#(?:[a-f0-9]{3}|[a-f0-9]{4}|[a-f0-9]{6}|[a-f0-9]{8}))$/i.test(value.literal)) reject('unsupported colour literal')
    return value.literal
  }
  if (Object.hasOwn(value, 'reference')) {
    if (!colorName(value.reference)) reject('unsupported colour reference identity')
    inputs.add(value.reference)
    return `var(${value.reference})`
  }
  shape(value.mix, ['first', 'firstPercent', 'second'])
  const { first, firstPercent, second } = value.mix
  if (!Number.isFinite(firstPercent) || firstPercent < 0 || firstPercent > 100) reject('invalid colour percentage')
  return `color-mix(in srgb, ${colorCSS(first, inputs, depth + 1)} ${firstPercent}%, ${colorCSS(second, inputs, depth + 1)})`
}

function sourceColors(snapshot, tokens, modes) {
  const themes = new Map()
  for (const theme of list(snapshot.themes, 'themes')) {
    if (!record(theme) || themes.has(theme.mode)) reject('duplicate or invalid captured theme')
    const names = new Map()
    for (const token of list(theme.tokens, 'theme tokens')) {
      if (!record(token) || names.has(token.name)) reject('duplicate captured token identity')
      names.set(token.name, token)
    }
    themes.set(theme.mode, names)
  }
  const declarations = new Map()
  for (const token of list(tokens.colors, 'colours')) {
    shape(token, ['name', 'value'])
    if (!colorName(token.name) || declarations.has(token.name)) reject('duplicate or unsupported colour identity')
    const inputs = new Set(), css = colorCSS(token.value, inputs)
    declarations.set(token.name, { css, inputs: [...inputs], reference: token.value.reference })
  }
  if (declarations.size && !modes.length) reject('shared colours require a selected mode')
  const perMode = [], ordered = []
  for (const [index, mode] of modes.entries()) {
    const values = new Map()
    for (const token of list(mode.colors, 'mode colours')) {
      shape(token, ['name', 'type', 'value'])
      if (token.type !== 'color' || !colorName(token.name) || values.has(token.name) || declarations.has(token.name)) reject('duplicate or unsupported colour identity')
      const original = themes.get(mode.mode)?.get(token.name)
      if (original?.type !== token.type || original.value !== token.value) reject('selected colour differs from its capture')
      values.set(token.name, { css: colorCSS({ literal: token.value }, new Set()), inputs: [] })
    }
    if (index === 0) ordered.push(...values.keys())
    else if (values.size !== ordered.length || ordered.some(name => !values.has(name))) reject('mode colour identities differ')
    for (const [name, value] of declarations) values.set(name, value)
    for (const value of values.values()) {
      value.resolved = resolveColorExpression(value.css, name => values.get(name)?.css)
    }
    perMode.push(values)
  }
  return [...ordered, ...declarations.keys()].map(name => ({
    name, type: 'COLOR', values: perMode.map(values => {
      const { css, inputs, reference, resolved } = values.get(name)
      if (reference) return { alias: reference }
      if (inputs.length || declarations.get(name)?.css.startsWith('color-mix(')) return { formula: { css, inputs: [...inputs] } }
      return { ...resolved }
    }), sourceToken: { version: 1, snapshot: snapshot.sha256, kind: 'color', name },
  }))
}

function sourceScales(snapshot, scales, spellings, modeCount) {
  if ((scales.length || spellings !== undefined) && (!Array.isArray(spellings) || spellings.length !== scales.length)) reject('source decimal evidence required')
  const evidence = new Map()
  for (const value of spellings ?? []) {
    shape(value, ['scale', 'key', 'decimal'])
    const address = JSON.stringify([value.scale, value.key])
    if (evidence.has(address)) reject('duplicate source decimal identity')
    evidence.set(address, value.decimal)
  }
  const seen = new Set()
  return scales.map(scale => {
    shape(scale, ['scale', 'key'], ['number', 'keyword'])
    if (scale.keyword !== undefined) reject(`unsupported keyword at ${scale.scale}/${scale.key}`)
    shape(scale.number, ['value', 'unit'])
    const { unit, value } = scale.number
    if (!['px', ''].includes(unit)) reject(`unsupported unit ${unit} at ${scale.scale}/${scale.key}`)
    // This is the provider's numeric storage domain, not a copy of the Go
    // scale-key tables and not certification of component/layout bindings.
    const lengths = ['spacing', 'font-size', 'tracking', 'radius', 'max-width', 'breakpoint']
    const length = lengths.includes(scale.scale), unitless = ['leading', 'font-weight'].includes(scale.scale)
    if (!length && !unitless && scale.scale !== 'line-height') reject('unsupported scale meaning')
    if (length && unit !== 'px' || unitless && unit !== '') reject('unsupported scale unit meaning')
    const decimal = evidence.get(JSON.stringify([scale.scale, scale.key])), native = parseNativeNumber(decimal)
    if (!Object.is(native, value)) reject('source decimal differs from decoded value')
    if (scale.scale !== 'tracking' && native < 0 || scale.scale === 'font-weight' && (native < 1 || native > 1000)) reject('invalid scale range')
    const sourceToken = { version: 1, snapshot: snapshot.sha256, kind: 'scale', scale: scale.scale,
      key: scale.key, decimal, unit }
    validateTokenOrigin(sourceToken, 'FLOAT')
    const name = `${scale.scale}/${scale.key}`
    if (seen.has(name)) reject('duplicate scale identity')
    seen.add(name)
    return { name, type: 'FLOAT', values: Array(modeCount || 1).fill(native), sourceToken }
  })
}

// Pure provider admission. Check all requested meaning before construction;
// never strip required features to call a legacy consumer or choose a subset.
export function planSourceTokens(snapshot, scalarSpellings) {
  if (snapshot?.schema !== 'platformkit.design-export.v2' || !Array.isArray(snapshot.requiredFeatures) ||
      snapshot.requiredFeatures.length !== 1 || snapshot.requiredFeatures[0] !== 'source-tokens.v1') reject('unsupported snapshot schema or required feature set')
  if (typeof snapshot.sha256 !== 'string' || !/^[a-f0-9]{64}$/.test(snapshot.sha256)) reject('source snapshot identity missing')
  if (list(snapshot.measurements, 'measurements').length) reject('unadvertised measurement feature')
  const pending = [...list(snapshot.examples, 'examples')], visited = new WeakSet()
  let count = 0
  while (pending.length) {
    const example = pending.pop()
    if (!record(example) || example.layout !== undefined) reject('unadvertised layout feature')
    if (visited.has(example) || ++count > 10000) reject('cyclic or oversized example traversal')
    visited.add(example)
    for (const child of list(example.children, 'children')) pending.push(child?.description)
  }
  const tokens = snapshot.sourceTokens
  shape(tokens, [], ['modes', 'colors', 'scales', 'shadows', 'easings', 'transitions', 'assets', 'faces'])
  for (const kind of ['shadows', 'easings', 'transitions', 'assets', 'faces']) {
    if (list(tokens[kind], kind).length) reject(`unsupported selected ${kind}; no partial library is produced`)
  }
  const modes = list(tokens.modes, 'modes'), names = new Set()
  for (const mode of modes) {
    shape(mode, ['mode'], ['colors', 'fonts'])
    if (!['light', 'dark'].includes(mode.mode) || names.has(mode.mode)) reject('duplicate or unsupported mode')
    if (list(mode.fonts, 'fonts').length) reject('unsupported ordered font fallback semantics')
    names.add(mode.mode)
  }
  const colors = sourceColors(snapshot, tokens, modes)
  const scales = sourceScales(snapshot, list(tokens.scales, 'scales'), scalarSpellings, modes.length)
  if (!colors.length && !scales.length) reject('select at least one supported token')
  return { modes: [...names], variables: [...colors, ...scales] }
}
