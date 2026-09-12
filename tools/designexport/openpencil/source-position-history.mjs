import { sourceAbsoluteData, sourceAbsoluteRecord } from './source-positioning.mjs'

const equal = (a, b) => JSON.stringify(a) === JSON.stringify(b)
const finiteCoordinate = value => Number.isFinite(value) && Number.isFinite(Math.fround(value))
const axisOf = field => ({ x: 'horizontal', y: 'vertical', horizontalConstraint: 'horizontal', verticalConstraint: 'vertical' })[field]
const requireHistory = (condition, message) => { if (!condition) throw new Error(`Native position history: ${message}`) }
export const isPositionEdit = (node, changes) => node && sourceAbsoluteRecord(node) && Object.keys(changes).some(axisOf)

// A receipt belongs to one interaction, not to the graph or a second history
// stack. Keep native values and exact source evidence separate.
export function captureNodeUpdate(ctx, id, fields) {
  const node = ctx.graph.getNode(id)
  requireHistory(node, 'missing node')
  return { id, identity: node, parentId: node.parentId, values: structuredClone(Object.fromEntries(fields.map(field => [field, node[field]]))),
    position: structuredClone(sourceAbsoluteRecord(node)), edited: [...node.source.editedFields], pluginData: structuredClone(node.pluginData) }
}

function pairsFor(ctx, originals) {
  return [...originals].map(([id, original]) => {
    const node = ctx.graph.getNode(id)
    requireHistory(node, 'missing move node')
    requireHistory(original.history || !sourceAbsoluteRecord(node), 'source move requires its initial receipt')
    const before = original.history ?? { ...captureNodeUpdate(ctx, id, ['x', 'y']),
      parentId: original.parentId ?? node.parentId, values: { x: original.x, y: original.y } }
    requireHistory(before.id === id, 'receipt belongs to another node')
    requireHistory(node === before.identity, 'stale receipt identity')
    return [before, captureNodeUpdate(ctx, id, Object.keys(before.values))]
  })
}

function ownedFields(before, after) {
  return Object.keys(before.values).filter(field => {
    const axis = ['x', 'y'].includes(field) && before.position && after.position && axisOf(field)
    return axis ? !equal(before.position[axis], after.position[axis]) : !equal(before.values[field], after.values[field])
  })
}

function withoutAxes(pluginData, axes) {
  return pluginData.map(item => {
    if (item.pluginId !== 'platformkit' || item.key !== 'platformkit.source') return item
    const data = JSON.parse(item.value)
    if (data.cssPosition) for (const axis of axes) delete data.cssPosition[axis]
    return { ...item, value: JSON.stringify(data) }
  })
}

function replay(ctx, pairs, project, inverse) {
  // Preflight the entire operation before publishing any graph or layout event.
  const plans = pairs.filter(([before, after]) => ownedFields(before, after).length || before.parentId !== after.parentId).map(([before, after]) => {
    const expected = inverse ? after : before, target = inverse ? before : after
    const node = ctx.graph.getNode(target.id), fields = ownedFields(before, after), axes = [...new Set(fields.map(axisOf).filter(Boolean))]
    requireHistory(node && node === before.identity && node === after.identity && node.parentId === expected.parentId, 'missing node or stale parent/identity')
    requireHistory(ctx.graph.getNode(node.parentId)?.childIds.includes(node.id), 'stale parent containment')
    requireHistory(ctx.graph.getNode(target.parentId), 'missing target parent')
    const position = sourceAbsoluteRecord(node)
    for (const field of fields) {
      const axis = axisOf(field), rounded = axis && position && equal(position[axis], expected.position?.[axis]) &&
        ['x', 'y'].includes(field) && Math.fround(node[field]) === Math.fround(expected.values[field])
      requireHistory(equal(node[field], expected.values[field]) || rounded, `stale ${field}`)
    }
    for (const axis of axes) requireHistory(equal(position?.[axis], expected.position?.[axis]), 'stale source anchor')
    for (let parent = ctx.graph.getNode(target.parentId), seen = new Set(); parent; parent = ctx.graph.getNode(parent.parentId)) {
      requireHistory(parent.id !== node.id && !seen.has(parent.id), 'cyclic target parent')
      seen.add(parent.id)
    }
    return { target, expected, fields, axes }
  })
  if (!plans.length) return
  project(ctx, planned => {
    for (const { target, expected, fields, axes } of plans) {
      const node = planned.graph.getNode(target.id), position = sourceAbsoluteRecord(node)
      const values = structuredClone(Object.fromEntries(fields.map(field => [field, target.values[field]])))
      if (target.parentId !== node.parentId) planned.graph.reparentNode(node.id, target.parentId)
      const edited = new Set(node.source.editedFields)
      for (const field of fields) target.edited.includes(field) ? edited.add(field) : edited.delete(field)
      if (position && axes.length) {
        const next = { ...position }
        for (const axis of axes) next[axis] = structuredClone(target.position[axis])
        values.pluginData = sourceAbsoluteData(node, next)
        // pluginData is shared by independent owners, including the other axis.
        if (equal(withoutAxes(node.pluginData, axes), withoutAxes(target.pluginData, axes))) {
          target.edited.includes('pluginData') ? edited.add('pluginData') : edited.delete('pluginData')
        }
      }
      values.source = { ...node.source, editedFields: [...edited] }
      planned.graph.preserveSourceMetadataDuring(() => planned.graph.updateNode(node.id, values))
    }
    for (const { target } of plans) planned.runLayoutForNode(target.id)
  })
}

function pushHistory(ctx, label, pairs, project) {
  pairs = pairs.filter(([before, after]) => ownedFields(before, after).length || before.parentId !== after.parentId)
  if (!pairs.length) return
  pairs = pairs.map(pair => pair.map(({ identity, ...state }) => ({ ...structuredClone(state), identity })))
  ctx.undo.push({ label, forward: () => replay(ctx, pairs, project, false), inverse: () => replay(ctx, pairs, project, true) })
}

export function commitPositionMove(ctx, label, originals, project) {
  pushHistory(ctx, label, pairsFor(ctx, originals), project)
}

export function updatePositionMove(ctx, originals, changes, project) {
  const pairs = pairsFor(ctx, originals)
  for (const [before, current] of pairs) requireHistory(before.parentId === current.parentId &&
    ctx.graph.getNode(current.parentId)?.childIds.includes(current.id) &&
    Object.keys(changes).every(field => ['x', 'y'].includes(field) && Object.hasOwn(before.values, field) &&
      finiteCoordinate(changes[field])), 'stale parent or invalid/uncaptured numeric coordinate')
  const changed = pairs.filter(([, current]) => Object.entries(changes).some(([field, value]) => !equal(current.values[field], value)))
  if (!changed.length) return
  project(ctx, planned => {
    for (const [, current] of changed) planned.graph.updateNode(current.id, changes)
    for (const [, current] of changed) planned.runLayoutForNode(current.id)
  })
  ctx.requestRender()
}

export function cancelPositionMove(ctx, originals, project) {
  replay(ctx, pairsFor(ctx, originals), project, true)
  ctx.requestRender()
}

export function commitNodeReceipt(ctx, id, receipt, label, project) {
  requireHistory(receipt.id === id, 'receipt belongs to another node')
  requireHistory(ctx.graph.getNode(id) === receipt.identity, 'stale receipt identity')
  pushHistory(ctx, label, [[receipt, captureNodeUpdate(ctx, id, Object.keys(receipt.values))]], project)
}

export function cancelNodeUpdate(ctx, receipt, project) {
  cancelPositionMove(ctx, new Map([[receipt.id, { history: receipt }]]), project)
}

export function updatePositionNode(ctx, id, changes, history, project) {
  requireHistory(['x', 'y'].every(field => !Object.hasOwn(changes, field) || finiteCoordinate(changes[field])), 'invalid numeric coordinate')
  const before = captureNodeUpdate(ctx, id, Object.keys(changes))
  if (Object.keys(changes).every(field => equal(before.values[field], changes[field]))) return
  project(ctx, planned => { planned.graph.updateNode(id, changes); planned.runLayoutForNode(id) })
  if (history) commitNodeReceipt(ctx, id, before, history, project)
  ctx.requestRender()
}
