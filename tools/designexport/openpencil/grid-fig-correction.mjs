import { fileURLToPath } from 'node:url'
import { ancestryOverrides, chain, sourceChildren } from './exporter-correction.mjs'
import { fragmentLayoutParent, sourceFragment } from './source-fragments.mjs'

const helper = () => JSON.stringify(fileURLToPath(import.meta.url))
const axes = ['Columns', 'Rows']
const sizingTypes = { FIXED: 'FIXED', FR: 'FLEX', AUTO: 'HUG' }
const gridFields = ['gridTemplateColumns', 'gridTemplateRows', 'gridColumnGap', 'gridRowGap', 'gridPosition']
const gridSizingFields = ['primaryAxisSizing', 'counterAxisSizing', 'layoutAlignSelf', 'layoutGrow']
export const gridTrackFields = ['gridColumns', 'gridRows', 'gridColumnsSizing', 'gridRowsSizing']
export const gridFigFields = [...gridTrackFields, 'gridColumnGap', 'gridRowGap', 'gridAutoTracks',
  'gridRowAnchor', 'gridColumnAnchor', 'gridRowSpan', 'gridColumnSpan', 'gridChildHorizontalAlign', 'gridChildVerticalAlign']

function refuse(reason) { throw new Error(`Native grid: ${reason}`) }

function number(value, name, positive = false) {
  if (!Number.isFinite(value) || !Number.isFinite(Math.fround(value)) || value < 0 || positive && Math.fround(value) === 0) {
    refuse(`invalid ${name}`)
  }
  return value
}

function guidKey(guid) {
  if (!guid || !['sessionID', 'localID'].every(key => Number.isInteger(guid[key]) && guid[key] >= 0 && guid[key] <= 4294967295)) {
    refuse('invalid track GUID')
  }
  return `${guid.sessionID}:${guid.localID}`
}

function trackEntries(nc, axis) {
  const entries = nc[`grid${axis}`]?.entries ?? []
  if (!Array.isArray(entries) || entries.length > 4096) refuse('invalid track count')
  const ids = new Set(), positions = new Set()
  for (const entry of entries) {
    const id = guidKey(entry.id)
    if (ids.has(id) || typeof entry.position !== 'string' || !entry.position || positions.has(entry.position)) {
      refuse('ambiguous track order or identity')
    }
    ids.add(id)
    positions.add(entry.position)
  }
  return [...entries].sort((a, b) => a.position < b.position ? -1 : a.position > b.position ? 1 : 0)
}

function importTracks(nc, axis) {
  const entries = trackEntries(nc, axis), sizes = nc[`grid${axis}Sizing`]?.entries ?? []
  if (!Array.isArray(sizes) || sizes.length !== entries.length) refuse('incomplete track sizing')
  const byId = new Map()
  for (const entry of sizes) {
    const key = guidKey(entry.id)
    if (byId.has(key)) refuse('duplicate track sizing')
    byId.set(key, entry.trackSize)
  }
  return entries.map(entry => {
    const size = byId.get(guidKey(entry.id)), min = size?.minSizing, max = size?.maxSizing
    const sizing = Object.keys(sizingTypes).find(key => sizingTypes[key] === max?.type)
    if (!sizing || !min || !(min.type === 'FIXED' || min.type === max.type && min.value === max.value ||
        sizing === 'FR' && min.type === 'HUG' && min.value === 0)) refuse('unsupported track sizing function')
    const value = number(max.value, 'track value', sizing === 'FR')
    if (sizing === 'AUTO' && value !== 0) refuse('nonzero automatic track value')
    const explicitMinimum = min.type === 'FIXED' && (max.type !== 'FIXED' || min.value !== max.value)
    return { sizing, value, ...(explicitMinimum ? { minValue: number(min.value, 'track minimum') } : {}) }
  })
}

export function importGridFields(nc) {
  const props = {}
  if (nc.gridAutoTracks !== undefined && !['NONE', 'ROWS'].includes(nc.gridAutoTracks)) refuse('unsupported automatic track direction')
  if (nc.stackMode === 'GRID') props.layoutMode = 'GRID'
  for (const axis of axes) if (nc[`grid${axis}`] !== undefined || nc[`grid${axis}Sizing`] !== undefined) {
    props[`gridTemplate${axis}`] = importTracks(nc, axis)
  }
  for (const axis of ['Row', 'Column']) if (nc[`grid${axis}Gap`] !== undefined) {
    props[`grid${axis}Gap`] = number(nc[`grid${axis}Gap`], 'gap')
  }
  return props
}

export function importGridPlacement(nc, parent, props, mode = props.layoutMode, baseline = props) {
  if (parent?.stackMode === 'GRID') {
    const horizontal = nc.gridChildHorizontalAlign, vertical = nc.gridChildVerticalAlign
    if (horizontal !== undefined && !['AUTO', 'MIN'].includes(horizontal) ||
        vertical !== undefined && !['AUTO', 'MIN', 'CENTER', 'MAX'].includes(vertical)) refuse('unsupported cell alignment')
    const stretch = (props.layoutAlignSelf ?? baseline.layoutAlignSelf) === 'STRETCH' || (props.layoutGrow ?? baseline.layoutGrow) > 0
    if (!stretch && mode !== 'NONE') {
      if (horizontal === 'AUTO') props[mode === 'HORIZONTAL' ? 'primaryAxisSizing' : 'counterAxisSizing'] = 'FILL'
      if (vertical === 'AUTO') props[mode === 'HORIZONTAL' ? 'counterAxisSizing' : 'primaryAxisSizing'] = 'FILL'
    } else if (!stretch && horizontal === 'AUTO' && vertical === 'AUTO') props.layoutAlignSelf = 'STRETCH'
    else if (!stretch && (horizontal === 'AUTO' || vertical === 'AUTO')) refuse('independent leaf stretch is not representable')
    if (['CENTER', 'MAX'].includes(vertical)) props.layoutAlignSelf = vertical
  }
  const positioned = ['gridRowAnchor', 'gridColumnAnchor', 'gridRowSpan', 'gridColumnSpan'].some(key => nc[key] !== undefined)
  if (!positioned) return props
  if (parent?.stackMode !== 'GRID') refuse('cell anchors require their owning grid')
  const position = {}
  for (const [axis, plural] of [['Row', 'Rows'], ['Column', 'Columns']]) {
    const entries = trackEntries(parent, plural), anchor = nc[`grid${axis}Anchor`]
    const index = anchor === undefined ? -1 : entries.findIndex(entry => guidKey(entry.id) === guidKey(anchor))
    if (anchor !== undefined && index < 0) refuse(`cell ${axis.toLowerCase()} anchor ${guidKey(anchor)} is outside its owning grid`)
    const key = axis.toLowerCase(), span = nc[`grid${axis}Span`] ?? 1
    if (!Number.isInteger(span) || span < 1 || span > 4096 || index >= 0 && index + span > entries.length) refuse('invalid cell span')
    position[key] = index + 1
    position[`${key}Span`] = span
  }
  props.gridPosition = position.row === 0 && position.column === 0 && position.rowSpan === 1 && position.columnSpan === 1 ? null : position
  return props
}

function exportTracks(node, context, counter) {
  // Track GUIDs are construction state for this one export, shared with the
  // existing node GUID allocator. They do not become scene nodes or a registry.
  counter.gridTracks ??= new Map()
  if (counter.gridTracks.has(node.id)) {
    const tracks = counter.gridTracks.get(node.id)
    if (!tracks) refuse('cyclic grid track inheritance')
    return tracks
  }
  counter.gridTracks.set(node.id, null)
  const result = {}
  for (const axis of axes) {
    const tracks = node[`gridTemplate${axis}`]
    if (!Array.isArray(tracks) || tracks.length > 4096) refuse('invalid track count')
    const source = context.graph.getNode(node.componentId)
    if (source?.layoutMode === 'GRID' && JSON.stringify(tracks) === JSON.stringify(source[`gridTemplate${axis}`])) {
      result[axis] = exportTracks(source, context, counter)[axis]
      continue
    }
    result[axis] = tracks.map((track, index) => {
      const type = sizingTypes[track?.sizing]
      if (!type) refuse('unsupported native track type')
      const value = number(track.value, 'track value', track.sizing === 'FR')
      if (type === 'HUG' && value !== 0) refuse('nonzero automatic track value')
      let id
      do {
        id = { sessionID: 1, localID: counter.value++ }
        guidKey(id)
      } while (context.assignedGuidValues.has(guidKey(id)))
      context.assignedGuidValues.add(guidKey(id))
      return { id, position: context.fractionalPosition(index), trackSize: {
        minSizing: track.minValue !== undefined ? { type: 'FIXED', value: number(track.minValue, 'track minimum') } :
          type === 'FLEX' ? { type: 'HUG', value: 0 } : { type, value },
        maxSizing: { type, value },
      } }
    })
  }
  counter.gridTracks.set(node.id, result)
  return result
}

export function serializeGridFields(node, nc, context, counter) {
  // A source identity owns its children, never a track or grid-item alignment.
  if (sourceFragment(context.graph, node)) return
  if (node.layoutMode === 'GRID') {
    const tracks = exportTracks(node, context, counter)
    for (const axis of axes) {
      nc[`grid${axis}`] = { entries: tracks[axis].map(({ id, position }) => ({ id, position })) }
      nc[`grid${axis}Sizing`] = { entries: tracks[axis].map(({ id, trackSize }) => ({ id, trackSize })) }
    }
    nc.gridRowGap = number(node.gridRowGap, 'row gap')
    nc.gridColumnGap = number(node.gridColumnGap, 'column gap')
    nc.gridAutoTracks = node.gridTemplateRows.length ? 'NONE' : 'ROWS'
  }
  const parent = fragmentLayoutParent(context.graph, node)
  if (parent?.layoutMode === 'GRID') {
    const widthSizing = node.layoutMode === 'HORIZONTAL' ? node.primaryAxisSizing : node.counterAxisSizing
    const heightSizing = node.layoutMode === 'HORIZONTAL' ? node.counterAxisSizing : node.primaryAxisSizing
    const stretch = node.layoutAlignSelf === 'STRETCH' || node.layoutGrow > 0
    nc.gridChildHorizontalAlign = stretch || node.layoutMode !== 'NONE' && widthSizing === 'FILL' ? 'AUTO' : 'MIN'
    nc.gridChildVerticalAlign = stretch || node.layoutMode !== 'NONE' && heightSizing === 'FILL' ? 'AUTO' :
      ['CENTER', 'MAX'].includes(node.layoutAlignSelf) ? node.layoutAlignSelf : 'MIN'
  }
  if (!node.gridPosition) return
  if (parent?.layoutMode !== 'GRID') refuse('positioned cell requires a grid parent')
  const tracks = exportTracks(parent, context, counter), pos = node.gridPosition
  for (const [axis, plural] of [['Row', 'Rows'], ['Column', 'Columns']]) {
    const key = axis.toLowerCase(), start = pos[key], span = pos[`${key}Span`]
    if (!Number.isInteger(start) || start < 0 || !Number.isInteger(span) || span < 1 || span > 4096 ||
        start > 0 && start + span - 1 > tracks[plural].length) refuse('cell placement requires representable track anchors')
    if (start > 0) nc[`grid${axis}Anchor`] = tracks[plural][start - 1].id
    nc[`grid${axis}Span`] = span
  }
}

export function serializeGridOverrides(context, instance, counter, path, derived = []) {
  const result = [], graph = context.graph
  function visit(target, source) {
    if (!source) refuse('missing instance source')
    if (target.layoutMode !== source.layoutMode && [target.layoutMode, source.layoutMode].includes('GRID')) {
      refuse('instance layout-mode replacement is not supported')
    }
    const overrides = ancestryOverrides(graph, target)
    const contextual = !sourceFragment(graph, target) && sourceFragment(graph, graph.getNode(target.parentId)) &&
      fragmentLayoutParent(graph, target)?.layoutMode === 'GRID'
    const changed = ownedGridFields(graph, target).filter(field => {
      if (Object.hasOwn(overrides, `${target.id}:${field}`)) return true
      // Reopening a contextless definition decodes FILL as FIXED. Its placed
      // derived FILL must not become authored merely because those differ.
      if (contextual && field.endsWith('AxisSizing') && target[field] === 'FILL' && !target.source.editedFields.includes(field)) return false
      return JSON.stringify(target[field]) !== JSON.stringify(source[field])
    })
    if (contextual) {
      // A contextless definition cannot encode inherited grid FILL in the
      // two-value stack sizing enum. Preserve the placed native alignment as
      // derived evidence, without turning inherited sizing into an override.
      const all = {}, entry = { guidPath: path(target) }
      serializeGridFields(target, all, context, counter)
      for (const axis of ['Horizontal', 'Vertical']) {
        const field = (axis === 'Horizontal') === (target.layoutMode === 'HORIZONTAL') ? 'primaryAxisSizing' : 'counterAxisSizing'
        entry[`gridChild${axis}Align`] = [field, 'layoutGrow', 'layoutAlignSelf'].some(field => changed.includes(field))
          ? undefined : all[`gridChild${axis}Align`]
      }
      derived.push(entry)
    }
    if (changed.length) {
      const all = {}, entry = { guidPath: path(target) }
      context.serializeLayoutProps(target, all)
      serializeGridFields(target, all, context, counter)
      all.stackPrimarySizing = target.primaryAxisSizing === 'HUG' ? 'RESIZE_TO_FIT' : 'FIXED'
      all.stackCounterSizing = target.counterAxisSizing === 'HUG' ? 'RESIZE_TO_FIT' : 'FIXED'
      for (const field of changed) {
        if (gridSizingFields.includes(field)) {
          const axis = (field === 'primaryAxisSizing') === (target.layoutMode === 'HORIZONTAL') ? 'Horizontal' : 'Vertical'
          const keys = field.endsWith('AxisSizing') ?
            [field === 'primaryAxisSizing' ? 'stackPrimarySizing' : 'stackCounterSizing', `gridChild${axis}Align`] :
            [field === 'layoutGrow' ? 'stackChildPrimaryGrow' : 'stackChildAlignSelf', 'gridChildHorizontalAlign', 'gridChildVerticalAlign']
          for (const key of keys) entry[key] = all[key]
        } else if (field === 'gridPosition') {
          // An explicit automatic position also clears a previous source cell
          // anchor. Omitted anchors in this fresh override mean automatic.
          for (const key of ['gridRowAnchor', 'gridColumnAnchor', 'gridRowSpan', 'gridColumnSpan']) entry[key] = all[key]
          entry.gridRowSpan ??= 1
          entry.gridColumnSpan ??= 1
        } else if (field.startsWith('gridTemplate')) {
          const axis = field.slice('gridTemplate'.length)
          entry[`grid${axis}`] = all[`grid${axis}`]
          entry[`grid${axis}Sizing`] = all[`grid${axis}Sizing`]
        } else entry[field] = all[field]
      }
      result.push(entry)
    }
    const owner = target.type === 'INSTANCE' ? chain(graph, target, 'componentId').at(-1) : source
    const children = sourceChildren(graph, owner, target, overrides)
    for (const child of graph.getChildren(target.id)) visit(child, children.get(child.id))
  }
  visit(instance, chain(graph, instance, 'componentId').at(-1))
  return result
}

export function importGridOverride(graph, target, fields, props) {
  if (sourceFragment(graph, target)) {
    if (gridFigFields.some(field => fields[field] !== undefined)) refuse('logical source owner has no grid box')
    return
  }
  Object.assign(props, importGridFields(fields))
  const parent = fragmentLayoutParent(graph, target)
  // Linked clones deliberately start with empty source metadata. Missing axes
  // inherit their native track identities; an occurrence's supplied axis wins.
  const parentFields = parent && Object.assign({}, ...chain(graph, parent, 'componentId').reverse()
    .map(node => node.source.fig.rawNodeFields), { stackMode: parent.layoutMode })
  importGridPlacement(fields, parentFields, props,
    props.layoutMode ?? target.layoutMode, target)
  const raw = Object.fromEntries(gridTrackFields.filter(key => fields[key] !== undefined).map(key => [key, structuredClone(fields[key])]))
  if (Object.keys(raw).length) props.source = { ...target.source, fig: { ...target.source.fig,
    rawNodeFields: { ...target.source.fig.rawNodeFields, ...raw } } }
}

export function importDerivedGridPlacement(graph, target, fields, updates) {
  if (!sourceFragment(graph, graph.getNode(target.parentId)) || fragmentLayoutParent(graph, target)?.layoutMode !== 'GRID') return
  const overrides = ancestryOverrides(graph, target)
  const owns = field => Object.hasOwn(overrides, `${target.id}:${field}`)
  if (owns('layoutGrow') || owns('layoutAlignSelf')) return
  const alignment = {}
  for (const axis of ['Horizontal', 'Vertical']) {
    const field = (axis === 'Horizontal') === (target.layoutMode === 'HORIZONTAL') ? 'primaryAxisSizing' : 'counterAxisSizing'
    if (!owns(field)) alignment[`gridChild${axis}Align`] = fields[`gridChild${axis}Align`]
  }
  const props = {}
  importGridOverride(graph, target, alignment, props)
  Object.assign(updates, props)
}

function ownedGridFields(graph, target) {
  if (sourceFragment(graph, target)) return []
  return target.layoutMode === 'GRID' || fragmentLayoutParent(graph, target)?.layoutMode === 'GRID' ?
    [...gridFields, ...gridSizingFields] : gridFields
}

function gridOwnership(graph, target, props) {
  const fields = ownedGridFields(graph, target).filter(field => Object.hasOwn(props, field))
  if (!fields.length) return null
  const owner = chain(graph, target, 'parentId').find(node => node.type === 'INSTANCE')
  return { id: owner?.id, targetId: target.id, fields, editedBefore: [...target.source.editedFields],
    ...(owner ? { before: structuredClone(owner.overrides), after: { ...structuredClone(owner.overrides),
      ...Object.fromEntries(fields.map(field => [owner.id === target.id ? field : `${target.id}:${field}`, true])) } } : {}) }
}

function restoreGridOwnership(graph, ownership, direction) {
  if (ownership?.id) graph.preserveSourceMetadataDuring(() => graph.updateNode(ownership.id,
    { overrides: structuredClone(ownership[direction]) }))
}

function restoreGridEditedFields(graph, ownership, direction) {
  if (!ownership) return
  const node = graph.getNode(ownership.targetId), current = node.source.editedFields
  const desired = direction === 'before' ? ownership.editedBefore : ownership.editedAfter
  // This action owns only its grid fields. A later unrelated authored marker
  // and the rest of the source record must survive undo and redo unchanged.
  const editedFields = desired.filter(field => ownership.fields.includes(field) || current.includes(field))
  editedFields.push(...current.filter(field => !ownership.fields.includes(field) && !editedFields.includes(field)))
  graph.preserveSourceMetadataDuring(() => graph.updateNode(node.id, { source: { ...node.source, editedFields } }))
}

export function retainGridOwnership(graph, target, props) {
  restoreGridOwnership(graph, gridOwnership(graph, target, props), 'after')
}

export function correctGridActions(source, replace) {
  source = `import { gridOwnership, restoreGridOwnership, restoreGridEditedFields } from ${helper()};\n` + source
  source = replace(source, '\t\tconst previous = pick(node, Object.keys(nextChanges));',
    '\t\tconst previous = pick(node, Object.keys(nextChanges));\n' +
    '\t\tconst ownership = gridOwnership(ctx.graph, node, nextChanges);\n' +
    '\t\trestoreGridOwnership(ctx.graph, ownership, "after");')
  source = replace(source, '\t\tctx.undo.push({',
    '\t\tif (ownership) ownership.editedAfter = [...ctx.graph.getNode(id).source.editedFields];\n\t\tctx.undo.push({')
  for (const [changes, direction] of [['nextChanges', 'after'], ['previous', 'before']]) {
    source = replace(source, `\t\t\t\tctx.graph.updateNode(id, ${changes});`,
      `\t\t\t\trestoreGridOwnership(ctx.graph, ownership, "${direction}");\n\t\t\t\tctx.graph.updateNode(id, ${changes});\n` +
      `\t\t\t\trestoreGridEditedFields(ctx.graph, ownership, "${direction}");`)
  }
  return source
}

export { gridOwnership, restoreGridOwnership, restoreGridEditedFields }

export function correctGridOverrides(source, replace) {
  source = `import { importGridOverride, importDerivedGridPlacement, retainGridOwnership } from ${helper()};\n` + source
  source = replace(source, 'const { updates, hasSize } = buildDsdLayoutUpdates(ctx, visibleSiblingCount, d, target);',
    'const { updates, hasSize } = buildDsdLayoutUpdates(ctx, visibleSiblingCount, d, target);\n' +
    '  importDerivedGridPlacement(ctx.graph, target, d, updates);')
  source = replace(source, 'const props = convertOverrideToProps(fields);',
    'const props = convertOverrideToProps(fields);\n    importGridOverride(ctx.graph, ctx.graph.getNode(targetId), fields, props);')
  source = replace(source, '\t\t\tprotectPatchProps(ctx.protectedFields, patch.targetId, props);',
    '\t\t\tprotectPatchProps(ctx.protectedFields, patch.targetId, props);\n' +
    '      if (patch.source === "symbol-override") retainGridOwnership(ctx.graph, target, props);')
  return replace(source, 'const overrides = nc.symbolData?.symbolOverrides;\n\t\tif (!overrides?.length) continue;\n\t\tconst nodeId =', String.raw`
    const depth = ov => (ov.guidPath?.guids?.length ?? 0) -
      (JSON.stringify(ov.guidPath?.guids?.[0]) === JSON.stringify(nc.symbolData?.symbolID) ? 1 : 0);
    const overrides = nc.symbolData?.symbolOverrides?.toSorted((a, b) => depth(a) - depth(b));
    if (!overrides?.length) continue;
    const nodeId =`)
}

export function correctGridNodeChange(source, replace) {
  source = `import { importGridFields, serializeGridFields, serializeGridOverrides, gridTrackFields, gridFigFields } from ${helper()};\n` + source
  source = replace(source, 'const FIGMA_RAW_NODE_FIELD_KEYS = [', 'const FIGMA_RAW_NODE_FIELD_KEYS = [\n  ...gridTrackFields,')
  source = replace(source, 'if (RAW_FIELDS_OVERRIDE_BLOCKLIST.has(String(key))) continue;',
    'if (RAW_FIELDS_OVERRIDE_BLOCKLIST.has(String(key)) || gridFigFields.includes(key)) continue;')
  source = replace(source, 'function applyInstancePayload(context, node, nc, localIdCounter) {',
    'function applyInstancePayload(context, node, nc, localIdCounter) {\n  const gridDerived = [];')
  source = replace(source, 'const layout = serializeDerivedLayout(context, node, localIdCounter);',
    'const layout = serializeDerivedLayout(context, node, localIdCounter);\n  mergeTextOverrides(layout, gridDerived);')
  source = replace(source, 'mergeTextOverrides(symbolOverrides, serializeRootSizing(node, symbolID));', String.raw`
    for (const override of symbolOverrides) for (const field of gridFigFields) delete override[field];
    mergeTextOverrides(symbolOverrides, serializeGridOverrides(context, node, localIdCounter,
      target => target.id === node.id ? { guids: [symbolID] } : nativeOverridePath(context, node, target, localIdCounter), gridDerived));
    mergeTextOverrides(symbolOverrides, serializeRootSizing(node, symbolID));`)
  source = replace(source, 'const layoutMode = mapStackMode(nc.stackMode);',
    'const layoutMode = nc.stackMode === "GRID" ? "GRID" : mapStackMode(nc.stackMode);')
  source = replace(source, '\t\titemSpacing: nc.stackSpacing ?? 0,',
    '\t\t...importGridFields(nc),\n\t\titemSpacing: nc.stackSpacing ?? 0,')
  source = replace(source, 'const figLayout = node.source.fig.layout;',
    'const figLayout = node.layoutMode === "GRID" || node.source.fig.layout?.stackMode === "GRID" ? null : node.source.fig.layout;')
  source = replace(source, 'if (node.layoutMode !== "NONE" && node.layoutMode !== "GRID") {',
    'if (node.layoutMode !== "NONE") {')
  return replace(source, 'context.serializeLayoutProps(node, nc);',
    'context.serializeLayoutProps(node, nc);\n\tserializeGridFields(node, nc, context, localIdCounter);')
}

export function correctGridImport(source, replace) {
  source = `import { fragmentFigParent } from ${JSON.stringify(fileURLToPath(new URL('./source-fragments.mjs', import.meta.url)))};\n` + source
  source = `import { importGridPlacement } from ${helper()};\n` + source
  return replace(source, 'const { nodeType, ...props } = nodeChangeToProps(nc, blobs);',
    'const { nodeType, ...props } = nodeChangeToProps(nc, blobs);\n' +
    '\t\timportGridPlacement(nc, fragmentFigParent(ncId, changeMap, parentMap, entry => nodeChangeToProps(entry, blobs)), props);')
}
