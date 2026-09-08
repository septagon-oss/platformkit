import { computedColor } from './computed-color.mjs'

const channels = ['r', 'g', 'b', 'a']
const percentage = '[+-]?(?:\\d+(?:\\.\\d*)?|\\.\\d+)(?:[eE][+-]?\\d+)?%'
const leadingWeight = new RegExp(`^(${percentage})\\s+(.+)$`, 's')
const trailingWeight = new RegExp(`^(.+?)\\s+(${percentage})$`, 's')
const reject = message => { throw new Error(`CSS color expression: ${message}`) }

// Split function arguments, not nested rgb()/var()/color-mix() commas.
// Strings, escapes, comments and CSS substitution fallbacks are outside this
// subset. Refusing them is not a claim that they are invalid CSS.
function argumentsOf(value) {
  const parts = []
  let depth = 0, start = 0
  for (let index = 0; index < value.length; index++) {
    if (value[index] === '(') depth++
    if (value[index] === ')' && --depth < 0) reject('unbalanced function')
    if (value[index] === ',' && depth === 0) {
      parts.push(value.slice(start, index).trim())
      start = index + 1
    }
  }
  if (depth !== 0) reject('unbalanced function')
  return [...parts, value.slice(start).trim()]
}

function weighted(value) {
  const first = leadingWeight.exec(value), last = trailingWeight.exec(value)
  const weight = first?.[1] ?? last?.[2]
  const amount = weight === undefined ? undefined : Number.parseFloat(weight) / 100
  if (amount !== undefined && (!Number.isFinite(amount) || amount < 0 || amount > 1)) reject('unsupported mix percentage')
  return { expression: first?.[2] ?? last?.[1] ?? value, amount }
}

// Evaluate the standard CSS subset emitted by the source's semantic colors.
// The caller resolves custom properties to authored expressions or native RGBA
// values. No scene graph, token registry, implicit palette or browser state is
// consulted here; dependencies remain in the original CSS and caller resolver.
// Two-color, positive-total sRGB mixes use normalized weights and premultiplied
// alpha: https://www.w3.org/TR/css-color-5/#color-mix
export function resolveColorExpression(expression, resolveVariable) {
  if (typeof resolveVariable !== 'function') reject('an explicit variable resolver is required')
  const active = new Set()
  function resolve(value, depth) {
    if (depth > 64) reject('maximum expression depth exceeded')
    if (value && typeof value === 'object' && channels.every(key => Number.isFinite(value[key]) && value[key] >= 0 && value[key] <= 1)) {
      return Object.fromEntries(channels.map(key => [key, value[key]]))
    }
    if (typeof value !== 'string' || value.length > 16384 || /[\\'";]|\/\*/.test(value)) reject('unsupported expression')
    value = value.trim()
    if (value.toLowerCase() === 'transparent') return { r: 0, g: 0, b: 0, a: 0 }
    if (/^#(?:[\da-f]{3}|[\da-f]{4}|[\da-f]{6}|[\da-f]{8})$/i.test(value)) {
      let hex = value.slice(1)
      if (hex.length < 5) hex = [...hex].map(char => char + char).join('')
      if (hex.length === 6) hex += 'ff'
      return Object.fromEntries(channels.map((key, index) => [key, Number.parseInt(hex.slice(index * 2, index * 2 + 2), 16) / 255]))
    }
    const call = /^([a-z-]+)\(([\s\S]*)\)$/i.exec(value)
    if (call?.[1].toLowerCase() === 'var') {
      const name = call[2].trim()
      if (!/^--[a-z_][\w-]*$/i.test(name)) reject('unsupported custom property reference')
      if (active.has(name)) reject(`custom property cycle at ${name}`)
      active.add(name)
      try {
        const resolved = resolveVariable(name)
        if (resolved === undefined || resolved === null) reject(`missing custom property ${name}`)
        return resolve(resolved, depth + 1)
      } finally { active.delete(name) }
    }
    if (call?.[1].toLowerCase() !== 'color-mix') return computedColor(value)
    const args = argumentsOf(call[2])
    if (args.length !== 3 || !/^in\s+srgb$/i.test(args[0])) reject('only two-color sRGB mixing is supported')
    const first = weighted(args[1]), second = weighted(args[2])
    const a = resolve(first.expression, depth + 1), b = resolve(second.expression, depth + 1)
    const p = first.amount ?? (second.amount === undefined ? .5 : 1 - second.amount)
    const q = second.amount ?? 1 - p
    const total = p + q
    if (total <= 0) reject('zero-total mixes need further conversion')
    const wa = p / total * a.a, wb = q / total * b.a, alpha = wa + wb
    return {
      r: alpha === 0 ? 0 : (a.r * wa + b.r * wb) / alpha,
      g: alpha === 0 ? 0 : (a.g * wa + b.g * wb) / alpha,
      b: alpha === 0 ? 0 : (a.b * wa + b.b * wb) / alpha,
      a: alpha * Math.min(total, 1),
    }
  }
  return resolve(expression, 0)
}
