import { fileURLToPath } from 'node:url'

const requirePosition = (condition, message) => { if (!condition) throw new Error(`Native component: source absolute ${message}`) }
const pixel = value => typeof value === 'string' && /^-?\d+(?:\.\d+)?px$/.test(value) ? Number.parseFloat(value) : NaN
const near = (a, b) => Number.isFinite(a) && Number.isFinite(b) && Math.abs(a - b) <= 1 / 64

// CSS absolute boxes use their containing block's padding edge, not its content
// edge. Only direct, untransformed flex ownership and explicit border-box sizes
// are admitted here; static-position, stretch and shrink-to-fit need other proof.
// https://drafts.csswg.org/css-position-3/#def-cb
export function planSourceAbsolute(node, parent) {
  requirePosition(node?.style.position === 'absolute' && parent?.style.position === 'relative' &&
    ['flex', 'inline-flex'].includes(parent.style.display), 'requires a direct positioned flex containing block')
  requirePosition(['transform', 'translate', 'rotate', 'scale'].every(key => parent.style[key] === 'none') &&
    parent.style.zoom === '1' && ['x', 'y'].every(axis => parent.style[`overflow-${axis}`] === 'visible'),
  'containing-block transforms, clipping or scrolling require further conversion')
  requirePosition(node.style.display === 'flex' && node.style['box-sizing'] === 'border-box' && node.style['z-index'] === 'auto' &&
    ['x', 'y'].every(axis => node.style[`overflow-${axis}`] === 'visible') &&
    ['top', 'right', 'bottom', 'left'].every(side => pixel(node.style[`margin-${side}`]) === 0),
  'requires an unstacked flex box without margins, scrolling or clipping')
  const width = pixel(node.sizing.width), height = pixel(node.sizing.height)
  requirePosition(width > 0 && height > 0 && near(width, node.bounds.width) && near(height, node.bounds.height),
    'requires explicit fixed border-box dimensions')
  function axis(start, end, coordinate, size) {
    const edges = [start, end].filter(edge => node.sizing[edge] !== 'auto')
    requirePosition(edges.length === 1, 'requires one explicit inset per axis')
    const edge = edges[0], value = pixel(node.sizing[edge]), border = pixel(parent.style[`border-${edge}-width`])
    requirePosition(Number.isFinite(value) && Number.isFinite(border) && border >= 0, 'requires finite pixel insets and borders')
    const inset = border + value, position = edge === start ? inset : parent.bounds[size] - node.bounds[size] - inset
    requirePosition(near(position, node.bounds[coordinate] - parent.bounds[coordinate]), 'observed placement differs from its containing block')
    return { anchor: { edge, inset }, position, constraint: edge === start ? 'MIN' : 'MAX' }
  }
  const horizontal = axis('left', 'right', 'x', 'width'), vertical = axis('top', 'bottom', 'y', 'height')
  return { x: horizontal.position, y: vertical.position, width, height, layoutPositioning: 'ABSOLUTE',
    horizontalConstraint: horizontal.constraint, verticalConstraint: vertical.constraint,
    cssPosition: { version: 1, horizontal: horizontal.anchor, vertical: vertical.anchor } }
}

// Placement is a private parent-owned frame, never a property of the reusable
// component inside it. Ordinary native absolute nodes keep the SDK's behavior.
export function sourceAbsoluteRecord(node) {
  if (node.layoutPositioning !== 'ABSOLUTE') return
  const entries = node.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
  if (entries.length !== 1) return
  let source
  try { source = JSON.parse(entries[0].value) } catch { return }
  const record = source?.cssPosition
  if (source?.schema !== 'platformkit.design-export.v1' || source.scope !== 'source-composition-layout' || record?.version !== 1 ||
      !['left', 'right'].includes(record.horizontal?.edge) || !['top', 'bottom'].includes(record.vertical?.edge) ||
      ![record.horizontal.inset, record.vertical.inset].every(Number.isFinite)) return
  return record
}

export function applySourceAbsolute(graph, parent, node, computeLayout) {
  const record = sourceAbsoluteRecord(node)
  if (!record) return false
  // An absolute Yoga placeholder has no subtree. Lay out the owned content
  // independently, then anchor its actual dimensions against the resolved parent.
  computeLayout(graph, node.id)
  const updates = {}
  for (const [axis, coordinate, size, start] of [['horizontal', 'x', 'width', 'left'], ['vertical', 'y', 'height', 'top']]) {
    const { edge, inset } = record[axis], constraint = `${axis}Constraint`
    if (node[constraint] === (edge === start ? 'MIN' : 'MAX')) {
      updates[coordinate] = edge === start ? inset : parent[size] - node[size] - inset
    }
  }
  graph.updateNode(node.id, updates)
  return true
}

// FIG stores absolute occurrence coordinates as ordinary transform overrides.
// Rebase those coordinates into the existing edge record at import/export, so
// saved native moves retain their new inset without becoming fixed screenshots.
export function reanchorSourceAbsolute(node, parent, position) {
  const record = sourceAbsoluteRecord(node)
  if (!record || !parent) return node.pluginData
  const next = structuredClone(record)
  for (const [axis, coordinate, size, start] of [['horizontal', 'x', 'width', 'left'], ['vertical', 'y', 'height', 'top']]) {
    if (!Number.isFinite(position[coordinate])) continue
    next[axis].inset = next[axis].edge === start ? position[coordinate] : parent[size] - node[size] - position[coordinate]
    requirePosition(Number.isFinite(next[axis].inset), 'native placement exceeds finite geometry')
  }
  return node.pluginData.map(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source'
    ? { ...item, value: JSON.stringify({ ...JSON.parse(item.value), cssPosition: next }) } : item)
}

export function correctSourcePositionGraph(source, replace) {
  source = `import { sourceAbsoluteRecord, reanchorSourceAbsolute } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  const mark = 'if (this.sourceMetadataPreservationDepth === 0) markSourceFieldsEdited(node, Object.keys(changes));'
  return replace(source, mark, `
    if (!this.isApplyingLayout && this.sourceMetadataPreservationDepth === 0 && sourceAbsoluteRecord(node) &&
        ["x", "y"].some(field => Object.hasOwn(changes, field) && changes[field] !== node[field])) {
      changes = { ...changes, pluginData: reanchorSourceAbsolute({ ...node, ...changes }, this.getNode(node.parentId), changes) };
    }
    ${mark}`)
}

export function correctSourcePositionImport(source, replace) {
  source = `import { sourceAbsoluteRecord, reanchorSourceAbsolute } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  return replace(source, 'const props = convertOverrideToProps(fields);',
    'const props = convertOverrideToProps(fields);\n' +
    '\t\tif (fields.transform) {\n' +
    '\t\t\tconst target = ctx.graph.getNode(targetId);\n' +
    '\t\t\tif (sourceAbsoluteRecord(target)) {\n' +
    '\t\t\t\tObject.assign(props, convertFigmaTransformProps({ transform: fields.transform, size: fields.size ?? { x: target.width, y: target.height } }));\n' +
    '\t\t\t\tprops.pluginData = reanchorSourceAbsolute({ ...target, ...props }, ctx.graph.getNode(target.parentId), props);\n\t\t\t}\n\t\t}')
}

// Coordinate history also owns source edit marks; a replay must not leave a
// reverted authored change behind in the source correspondence.
export function correctSourcePositionActions(source, replace) {
  source = `import { sourceAbsoluteRecord } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, 'const previous = pick(node, Object.keys(nextChanges));',
    'const previous = pick(node, Object.keys(nextChanges));\n' +
    '\t\tconst positionSourceBefore = sourceAbsoluteRecord(node) && ["x", "y", "horizontalConstraint", "verticalConstraint"].some(field => Object.hasOwn(nextChanges, field)) ? structuredClone(node.source) : null;')
  source = replace(source, '\t\tctx.runLayoutForNode(id);\n\t\tctx.undo.push({',
    '\t\tctx.runLayoutForNode(id);\n\t\tconst positionSourceAfter = positionSourceBefore ? structuredClone(node.source) : null;\n\t\tctx.undo.push({')
  for (const [changes, state] of [['nextChanges', 'positionSourceAfter'], ['previous', 'positionSourceBefore']]) {
    source = replace(source, `\t\t\t\tctx.graph.updateNode(id, ${changes});`,
      `\t\t\t\tctx.graph.updateNode(id, ${changes});\n` +
      `\t\t\t\tif (${state}) ctx.graph.preserveSourceMetadataDuring(() => ctx.graph.updateNode(id, { source: structuredClone(${state}) }));`)
  }
  return source
}
