import { fileURLToPath } from 'node:url'

const requirePosition = (condition, message) => { if (!condition) throw new Error(`Native component: source absolute ${message}`) }
const pixel = value => typeof value === 'string' && /^-?\d+(?:\.\d+)?px$/.test(value) ? Number.parseFloat(value) : NaN
const near = (a, b) => Number.isFinite(a) && Number.isFinite(b) && Math.abs(a - b) <= 1 / 64

// CSS absolute boxes use their containing block's padding edge, not its content
// edge. Direct, untransformed flex ownership admits fixed flex boxes and
// shrink-to-fit single-text blocks; static-position and stretch need other proof.
// https://drafts.csswg.org/css-position-3/#def-cb
export function planSourceAbsolute(node, parent) {
  requirePosition(node?.style.position === 'absolute' && parent?.style.position === 'relative' &&
    ['flex', 'inline-flex'].includes(parent.style.display), 'requires a direct positioned flex containing block')
  requirePosition(['transform', 'translate', 'rotate', 'scale'].every(key => parent.style[key] === 'none') &&
    parent.style.zoom === '1' && ['x', 'y'].every(axis => ['visible', 'hidden'].includes(parent.style[`overflow-${axis}`])),
  'containing-block transforms or scrolling require further conversion')
  const autoSize = node.style.display === 'block' && node.sizing.width === 'auto' && node.sizing.height === 'auto' &&
    node.children?.length === 1 && node.children[0].kind === 'text'
  requirePosition((autoSize || node.style.display === 'flex') && node.style['box-sizing'] === 'border-box' && node.style['z-index'] === 'auto' &&
    ['x', 'y'].every(axis => node.style[`overflow-${axis}`] === 'visible') &&
    ['top', 'right', 'bottom', 'left'].every(side => pixel(node.style[`margin-${side}`]) === 0),
  'requires an unstacked flex box or automatic text block without margins, scrolling or clipping')
  const width = autoSize ? node.bounds.width : pixel(node.sizing.width), height = autoSize ? node.bounds.height : pixel(node.sizing.height)
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
    cssPosition: { version: 1, horizontal: horizontal.anchor, vertical: vertical.anchor,
      ...(autoSize ? { autoSize: { oppositeBorder: pixel(parent.style[`border-${horizontal.anchor.edge === 'left' ? 'right' : 'left'}-width`]) } } : {}) } }
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

export function sourceAbsoluteData(node, record) {
  return node.pluginData.map(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source'
    ? { ...item, value: JSON.stringify({ ...JSON.parse(item.value), cssPosition: record }) } : item)
}

export function applySourceAbsolute(graph, parent, node, computeLayout, measureText) {
  const record = sourceAbsoluteRecord(node)
  if (!record) return false
  // An absolute Yoga placeholder has no subtree. Lay out the owned content
  // independently, then anchor its actual dimensions against the resolved parent.
  if (record.autoSize && node.counterAxisSizing === 'FIXED' && node.primaryAxisSizing === 'HUG') {
    const children = graph.getChildren(node.id), content = children[0], leaves = content && graph.getChildren(content.id), text = leaves?.[0]
    requirePosition(children.length === 1 && leaves?.length === 1 && text.type === 'TEXT' && content.layoutMode === 'VERTICAL',
      'automatic size requires its owned single-text block')
    const measured = measureText?.({ ...text, textAutoResize: 'WIDTH_AND_HEIGHT' })
    requirePosition(measured && [measured.width, measured.minContentWidth, record.autoSize.oppositeBorder].every(value => Number.isFinite(value) && value >= 0),
      'automatic size requires actual intrinsic measurement and border evidence')
    const padding = content.paddingLeft + content.paddingRight
    const available = Math.max(0, parent.width - record.horizontal.inset - record.autoSize.oppositeBorder - padding)
    const width = padding + Math.max(Math.ceil(measured.minContentWidth * 64) / 64, Math.min(Math.ceil(measured.width * 64) / 64, available))
    graph.preserveSourceMetadataDuring(() => {
      graph.updateNode(node.id, { width, figmaDerivedLayout: null })
      graph.updateNode(content.id, { width, figmaDerivedLayout: null })
      graph.updateNode(text.id, { figmaDerivedLayout: null })
    })
  }
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

// Native moves update the parent-owned anchor. Older FIG files without an
// explicit placement record also use this coordinate-based import fallback.
export function reanchorSourceAbsolute(node, parent, position) {
  const record = sourceAbsoluteRecord(node)
  if (!record || !parent) return node.pluginData
  const next = structuredClone(record)
  for (const [axis, coordinate, size, start] of [['horizontal', 'x', 'width', 'left'], ['vertical', 'y', 'height', 'top']]) {
    if (!Number.isFinite(position[coordinate])) continue
    next[axis].inset = next[axis].edge === start ? position[coordinate] : parent[size] - node[size] - position[coordinate]
    requirePosition(Number.isFinite(next[axis].inset), 'native placement exceeds finite geometry')
  }
  return sourceAbsoluteData(node, next)
}

export function sourcePositionWireGeometry(fields) {
  return Object.fromEntries([['transform', ['m00', 'm01', 'm02', 'm10', 'm11', 'm12']], ['size', ['x', 'y']]].map(([field, keys]) =>
    [field, Object.fromEntries(keys.map(key => {
      const value = fields[field]?.[key]
      requirePosition(Number.isFinite(value) && Number.isFinite(Math.fround(value)), 'wire geometry requires finite transform and size fields')
      return [key, Math.fround(value)]
    }))]))
}

export function importSourceAbsolute(node, parent, position, fields) {
  const entries = (fields.pluginData ?? []).filter(item => item.pluginID === 'platformkit' && item.key === 'platformkit.source')
  if (!entries.length) return reanchorSourceAbsolute({ ...node, ...position }, parent, position)
  // A symbol override owns only the occurrence's placement. Never overwrite
  // canonical source identity or unrelated plugin evidence with its payload.
  const record = sourceAbsoluteRecord({ ...node, pluginData: entries.map(item => ({ ...item, pluginId: item.pluginID })) })
  requirePosition(record, 'override requires one valid source placement record')
  const saved = JSON.parse(entries[0].value).wireGeometry
  requirePosition(saved, 'override requires its saved wire geometry')
  const witness = sourcePositionWireGeometry(saved), current = sourcePositionWireGeometry(fields)
  // An external editor may change native geometry without updating our record.
  // Compare encoded values exactly, not geometric tolerances or inferred edits.
  if (JSON.stringify(witness) !== JSON.stringify(current)) return reanchorSourceAbsolute({ ...node, ...position }, parent, position)
  return sourceAbsoluteData(node, record)
}

export function correctSourcePositionGraph(source, replace) {
  source = `import { sourceAbsoluteRecord, reanchorSourceAbsolute } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  const mark = 'if (this.sourceMetadataPreservationDepth === 0) markSourceFieldsEdited(node, Object.keys(changes));'
  return replace(source, mark, `
    if (!this.isApplyingLayout && this.sourceMetadataPreservationDepth === 0 && sourceAbsoluteRecord(node) &&
        ["x", "y"].some(field => Object.hasOwn(changes, field) && changes[field] !== node[field])) {
      const position = Object.fromEntries(Object.entries(changes).filter(([field, value]) => ["x", "y"].includes(field) && value !== node[field]));
      changes = { ...changes, pluginData: reanchorSourceAbsolute({ ...node, ...changes }, this.getNode(node.parentId), position) };
    }
    ${mark}`)
}

export function correctSourcePositionImport(source, replace) {
  source = `import { sourceAbsoluteRecord, importSourceAbsolute } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  return replace(source, 'const props = convertOverrideToProps(fields);',
    'const props = convertOverrideToProps(fields);\n' +
    '\t\tif (fields.transform || fields.pluginData?.some(item => item.pluginID === "platformkit" && item.key === "platformkit.source")) {\n' +
    '\t\t\tconst target = ctx.graph.getNode(targetId);\n' +
    '\t\t\tif (sourceAbsoluteRecord(target)) {\n' +
    '\t\t\t\tif (fields.transform) Object.assign(props, convertFigmaTransformProps({ transform: fields.transform, size: fields.size ?? { x: target.width, y: target.height } }));\n' +
    '\t\t\t\tprops.pluginData = importSourceAbsolute(target, ctx.graph.getNode(target.parentId), props, fields);\n\t\t\t}\n\t\t}')
}

// Coordinate history owns edit marks and exact anchors. Reconstructing a prior
// anchor from imported binary32 coordinates would make undo itself an edit.
export function correctSourcePositionActions(source, replace) {
  source = `import { sourceAbsoluteRecord, sourceAbsoluteData } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, 'const previous = pick(node, Object.keys(nextChanges));',
    'const previous = pick(node, Object.keys(nextChanges));\n' +
    '\t\tconst positionSourceBefore = sourceAbsoluteRecord(node) && ["x", "y", "horizontalConstraint", "verticalConstraint"].some(field => Object.hasOwn(nextChanges, field)) ? structuredClone({ source: node.source, position: sourceAbsoluteRecord(node) }) : null;')
  source = replace(source, '\t\tctx.runLayoutForNode(id);\n\t\tctx.undo.push({',
    '\t\tctx.runLayoutForNode(id);\n\t\tconst positionSourceAfter = positionSourceBefore ? structuredClone({ source: node.source, position: sourceAbsoluteRecord(node) }) : null;\n\t\tctx.undo.push({')
  for (const [changes, state] of [['nextChanges', 'positionSourceAfter'], ['previous', 'positionSourceBefore']]) {
    source = replace(source, `\t\t\t\tctx.graph.updateNode(id, ${changes});`,
      `\t\t\t\tctx.graph.updateNode(id, ${changes});\n` +
      `\t\t\t\tif (${state}) ctx.graph.preserveSourceMetadataDuring(() => ctx.graph.updateNode(id, { source: structuredClone(${state}.source), pluginData: sourceAbsoluteData(ctx.graph.getNode(id), ${state}.position) }));`)
  }
  return source
}
