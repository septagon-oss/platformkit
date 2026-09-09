// Translate observed CSS sizing, not measured pixel tracks or component names.
// This pure plan feeds the existing component constructor and native layout.
function requireGrid(condition, message) {
  if (!condition) throw new Error(`Native component: source grid ${message}`)
}

export function sourceGridTracks(value) {
  requireGrid(typeof value === 'string' && value.length <= 65536, 'requires bounded CSS track evidence')
  if (value === 'none') return []
  const tokens = value.match(/(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?(?:px|fr)?|[a-z-]+|[(),]/g) ?? []
  requireGrid(tokens.join('') === value.replace(/\s/g, ''), 'track syntax is unsupported')
  let cursor = 0
  const take = token => requireGrid(tokens[cursor++] === token, 'track syntax is unsupported')
  function simple(allowMinmax = true) {
    const token = tokens[cursor++]
    if (token === 'minmax') {
      requireGrid(allowMinmax, 'nested sizing functions are unsupported')
      take('(')
      const minimum = tokens[cursor++]
      requireGrid(/^(?:0|\d+(?:\.\d+)?px)$/.test(minimum) && Number.isFinite(Math.fround(Number.parseFloat(minimum))),
        'minmax minimum must be a finite fixed length')
      take(',')
      const result = simple(false)
      take(')')
      return { ...result, minValue: Number.parseFloat(minimum) }
    }
    if (token === 'auto') return { sizing: 'AUTO', value: 0 }
    const match = /^(\d+(?:\.\d*)?|\.\d+)(?:e([+-]?\d+))?(px|fr)$/.exec(token ?? '')
    const number = match && Number(`${match[1]}e${match[2] ?? 0}`)
    requireGrid(match && Number.isFinite(Math.fround(number)) && (match[3] !== 'fr' || Math.fround(number) > 0),
      'requires finite fixed, fractional or automatic tracks')
    return { sizing: match[3] === 'fr' ? 'FR' : 'FIXED', value: number }
  }
  function list(repeated = false) {
    const tracks = []
    while (cursor < tokens.length && tokens[cursor] !== ')') {
      if (tokens[cursor] !== 'repeat') tracks.push(simple())
      else {
        requireGrid(!repeated, 'nested or automatic repeat is unsupported')
        cursor++
        take('(')
        const rawCount = tokens[cursor++], count = Number(rawCount)
        requireGrid(/^[1-9]\d*$/.test(rawCount) && count <= 4096, 'repeat count is unsupported')
        take(',')
        const inner = list(true)
        take(')')
        requireGrid(inner.length * count <= 4096, 'track count exceeds the native limit')
        for (let index = 0; index < count; index++) tracks.push(...inner)
      }
      requireGrid(tracks.length <= 4096, 'track count exceeds the native limit')
    }
    requireGrid(tracks.length > 0, 'requires a nonempty track list')
    return tracks
  }
  const result = list()
  requireGrid(cursor === tokens.length, 'track syntax is unsupported')
  return result
}

function placement(style, axis, count) {
  const start = style[`grid-${axis}-start`], end = style[`grid-${axis}-end`]
  if (start === 'auto' && end === 'auto') return { start: 0, span: 1 }
  const span = /^span ([1-9]\d*)$/.exec(start)
  if (span && (end === start || end === 'auto')) {
    requireGrid(Number(span[1]) <= (count || 4096), 'span exceeds its template')
    return { start: 0, span: Number(span[1]) }
  }
  const line = value => /^-?[1-9]\d*$/.test(value) ? Number(value) > 0 ? Number(value) : count + Number(value) + 2 : NaN
  const first = line(start), last = end === 'auto' ? first + 1 : line(end)
  requireGrid(first > 0 && last > first && last <= count + 1, 'requires explicit in-template lines or automatic spans')
  return { start: first, span: last - first }
}

export function planSourceGrid(node) {
  const { style, sizing } = node
  requireGrid(style['grid-auto-flow'] === 'row' && style['grid-template-areas'] === 'none' &&
    sizing['grid-auto-columns'] === 'auto' && sizing['grid-auto-rows'] === 'auto', 'automatic flow is unsupported')
  requireGrid(['normal', 'start'].includes(style['justify-content']) && ['normal', 'start'].includes(style['align-content']),
    'content distribution is unsupported')
  const columns = sourceGridTracks(sizing['grid-template-columns']), rows = sourceGridTracks(sizing['grid-template-rows'])
  requireGrid(columns.length > 0, 'needs an explicit column template')
  const gap = value => value === 'normal' ? 0 : /^\d+(?:\.\d+)?px$/.test(value) ? Number.parseFloat(value) : NaN
  const native = { layoutMode: 'GRID', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED',
    gridTemplateColumns: columns, gridTemplateRows: rows,
    gridColumnGap: gap(style['column-gap']), gridRowGap: gap(style['row-gap']) }
  requireGrid(Number.isFinite(native.gridColumnGap) && Number.isFinite(native.gridRowGap), 'gap must be a fixed length')
  const children = node.children.map(child => {
    requireGrid(child.kind === 'element' && child.style.order === '0', 'needs source-ordered element cells')
    const align = axis => {
      const value = child.style[`${axis}-self`] === 'auto' ? style[`${axis}-items`] : child.style[`${axis}-self`]
      requireGrid(['normal', 'stretch', 'start'].includes(value), 'cell alignment is unsupported')
      // Normal block-axis sizing preserves a preferred aspect ratio; it is
      // not an instruction to stretch the cell to the row's eventual height.
      // https://drafts.csswg.org/css-grid-2/#grid-item-sizing
      const ratio = child.style['aspect-ratio'] && child.style['aspect-ratio'] !== 'auto'
      return value !== 'start' && !(axis === 'align' && value === 'normal' && ratio)
    }
    const column = placement(child.style, 'column', columns.length), row = placement(child.style, 'row', rows.length)
    requireGrid(child.sizing['min-width'] === 'auto' && child.sizing['min-height'] === 'auto',
      'explicit cell minimum constraints require further conversion')
    const widthFill = align('justify') || child.sizing.width === '100%'
    requireGrid(widthFill, 'intrinsic nonstretch cell width requires further conversion')
    return { widthFill, heightFill: align('align'),
      gridPosition: column.start === 0 && row.start === 0 && column.span === 1 && row.span === 1 ? null :
        { column: column.start, row: row.start, columnSpan: column.span, rowSpan: row.span } }
  })
  return { native, children }
}
