const channel = '([+-]?(?:\\d+(?:\\.\\d*)?|\\.\\d+)(?:[eE][+-]?\\d+)?%?)'
const legacy = new RegExp(`^rgba?\\(\\s*${channel}\\s*,\\s*${channel}\\s*,\\s*${channel}(?:\\s*,\\s*${channel})?\\s*\\)$`)
const modern = new RegExp(`^rgba?\\(\\s*${channel}\\s+${channel}\\s+${channel}(?:\\s*/\\s*${channel})?\\s*\\)$`)
const srgb = new RegExp(`^color\\(srgb\\s+${channel}\\s+${channel}\\s+${channel}(?:\\s*/\\s*${channel})?\\s*\\)$`)

// CSS Color 4 serializes color(srgb ...) in normalized channels. Preserve
// those values rather than converting through 8-bit RGB or the SDK's fallback.
// Extended gamut, other color spaces and missing components need conversion
// rules; rejecting them is different from clamping them to a different color.
// https://www.w3.org/TR/css-color-4/#serializing-color-function-values
export function computedColor(value) {
  const reject = () => { throw new Error(`Native component: unsupported computed paint ${String(value)}`) }
  if (typeof value !== 'string') return reject()
  const comma = legacy.exec(value), normalized = srgb.exec(value)
  const match = comma ?? normalized ?? modern.exec(value)
  if (!match || comma && !match.slice(1, 4).every(part => part.endsWith('%') === match[1].endsWith('%'))) return reject()
  const values = match.slice(1).map((part, index) => part === undefined ? 1 :
    Number.parseFloat(part) / (part.endsWith('%') ? 100 : index === 3 || normalized ? 1 : 255))
  if (!values.every(channel => Number.isFinite(channel) && channel >= 0 && channel <= 1)) return reject()
  return Object.fromEntries(['r', 'g', 'b', 'a'].map((key, index) => [key, values[index]]))
}
