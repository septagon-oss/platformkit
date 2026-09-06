import { lineageHelpers } from './exporter-correction.mjs'

function syncReadView(nodes, deletedParents = new Map()) {
  return {
    getNode: id => nodes.get(id) ?? (deletedParents.has(id)
      ? { id, parentId: deletedParents.get(id), componentId: null, childIds: [] } : undefined),
    getChildren: id => (nodes.get(id)?.childIds ?? []).map(child => nodes.get(child)).filter(Boolean),
  }
}

function syncSourceOccurrence(nodes, target, visiting = new Set()) {
  if (visiting.has(target.id)) throw new Error('Cyclic native sync ancestry')
  const next = new Set(visiting).add(target.id)
  const parent = nodes.get(target.parentId)
  if (!parent || parent.type === 'CANVAS' || parent.type === 'COMPONENT' || parent.type === 'COMPONENT_SET') {
    return nodes.get(target.componentId)
  }
  const sourceParent = syncSourceOccurrence(nodes, parent, next)
  if (!sourceParent) return nodes.get(target.componentId)
  let owner = parent
  const ancestry = new Set()
  while (owner && owner.type !== 'INSTANCE') {
    if (ancestry.has(owner.id)) throw new Error('Cyclic native sync ownership')
    ancestry.add(owner.id)
    owner = nodes.get(owner.parentId)
  }
  const overrides = owner?.overrides ?? {}
  if (Object.hasOwn(overrides, `${target.id}:componentId`)) return nodes.get(target.componentId)
  return sourceChild(syncReadView(nodes), sourceParent, target, overrides)
}

function syncReconciliation(nodes, source, target, overrides, deletedParents) {
  const matches = new Map(), local = [], removed = []
  const lineageView = syncReadView(nodes, deletedParents)
  const sourceWithHistory = { ...source, childIds: [...source.childIds,
    ...[...deletedParents].filter(([id, parent]) => parent === source.id && !nodes.has(id)).map(([id]) => id)] }
  const children = source.childIds.map(id => {
    const child = nodes.get(id)
    if (!child || child.parentId !== source.id) throw new Error('Invalid native sync source child')
    return child
  })
  if (new Set(source.childIds).size !== children.length) throw new Error('Duplicate native sync source child')
  if (new Set(target.childIds).size !== target.childIds.length) throw new Error('Duplicate native sync target child')
  for (const id of target.childIds) {
    const child = nodes.get(id)
    if (!child || child.parentId !== target.id) throw new Error('Invalid native sync target child')
    const explicit = overrides[`${id}:sourceComponentId`]
    if (explicit !== undefined && typeof explicit !== 'string') throw new Error('Invalid native source identity')
    const sourceId = explicit ?? child.componentId
    if (!sourceId) { local.push(id); continue }
    const original = sourceChild(lineageView, sourceWithHistory, child, overrides)
    if (!nodes.has(original.id)) {
      removed.push(id)
      continue
    }
    if (matches.has(original.id)) throw new Error('Duplicate native sync source correspondence')
    if (original.type !== child.type) throw new Error('Native sync source type replacement requires explicit replacement')
    matches.set(original.id, id)
  }
  return { children, matches, local, removed }
}

function syncProperties(source, target, keys, overrides, prefix = '') {
  if (source.type === 'VECTOR' && (![source.x, source.y, source.width, source.height].every(Number.isFinite) ||
    source.width < 0 || source.height < 0 || source.vectorNetwork && validateVectorNetwork(source.vectorNetwork).length)) {
    throw new Error('Invalid native sync vector geometry')
  }
  const changes = {}
  for (const key of keys) {
    if (Object.hasOwn(overrides, `${prefix}${key}`)) continue
    copyProp(changes, source, key)
  }
  if (!Object.hasOwn(overrides, `${prefix}boundVariables`)) {
    for (const field of ['fills', 'strokes']) {
      if (!Object.hasOwn(overrides, `${prefix}${field}`)) continue
      const belongs = key => key === field || key.startsWith(`${field}/`)
      changes.boundVariables = Object.fromEntries([
        ...Object.entries(changes.boundVariables).filter(([key]) => !belongs(key)),
        ...Object.entries(target.boundVariables).filter(([key]) => belongs(key)),
      ])
    }
  }
  return { ...target, ...structuredClone(changes) }
}

function syncNestedOverrides(target, inherited) {
  if (target.type !== 'INSTANCE') return inherited
  // Native edits belong to their nearest instance. Qualify its root fields
  // before descending; explicit outer-instance paths retain precedence.
  const own = Object.fromEntries(Object.entries(target.overrides).map(([key, value]) =>
    [key.includes(':') ? key : `${target.id}:${key}`, value]))
  return { ...own, ...inherited }
}

function syncRemapOverrides(overrides, identities, copiedSource = false) {
  return Object.fromEntries(Object.entries(overrides).map(([key, value]) => {
    const separator = key.lastIndexOf(':')
    const owner = key.slice(0, separator), field = key.slice(separator + 1)
    const mapped = identities.get(owner)
    if (mapped) key = `${mapped}:${field}`
    if (field === 'sourceComponentId') value = copiedSource && mapped ? owner : identities.get(value) ?? value
    else if (field === 'componentId') value = identities.get(value) ?? value
    return [key, value]
  }))
}

function planNativeSync(previousNodes, instanceIndex, componentId, deletedNodeParents, replacementId = null) {
  const component = previousNodes.get(componentId)
  if (component?.type !== 'COMPONENT') return null
  const replacement = replacementId === null ? null : previousNodes.get(replacementId)
  if (replacementId !== null && replacement?.type !== 'INSTANCE') throw new Error('Invalid native replacement instance')
  const nodes = new Map(previousNodes), created = [], removed = new Set(), affected = new Set()
  const deletedParents = new Map(deletedNodeParents)
  const reachable = new Set(replacement ? [replacementId] : []), queue = [replacementId ?? componentId]
  for (let index = 0; index < queue.length; index++) {
    for (const id of instanceIndex.get(queue[index]) ?? []) {
      if (reachable.has(id)) continue
      reachable.add(id)
      queue.push(id)
    }
  }
  const sources = new Map()
  for (const id of reachable) {
    const target = previousNodes.get(id)
    if (target?.type !== 'INSTANCE') throw new Error('Invalid native instance index')
    const source = id === replacementId ? component : syncSourceOccurrence(previousNodes, target)
    if (!source || !['COMPONENT', 'INSTANCE'].includes(source.type)) throw new Error('Missing native sync source')
    sources.set(id, source.id)
  }
  let serial = 0
  const originalScales = new Map(replacement ? uniformScalePlan(syncReadView(previousNodes), replacement)?.updates ?? [] : [])
  function temporaryId() {
    let id
    do { id = `native-sync:${serial++}` } while (nodes.has(id) || previousNodes.has(id))
    return id
  }
  function remove(id, ancestors = new Set()) {
    if (ancestors.has(id)) throw new Error('Cyclic native sync removal')
    const node = nodes.get(id)
    if (!node) throw new Error('Missing native sync removal target')
    if (replacement) {
      const parent = previousNodes.get(node.parentId)
      sourceChildren(syncReadView(previousNodes), chain(syncReadView(previousNodes), parent, 'componentId').at(-1), parent, parent.overrides)
    }
    const expected = replacement && { ...chain(syncReadView(previousNodes), node, 'componentId').at(-1), ...originalScales.get(id) }
    const edited = replacement && node.source.editedFields.some(field => JSON.stringify(node[field]) !== JSON.stringify(expected[field]))
    if (replacement && (edited || chain(syncReadView(previousNodes), replacement, 'parentId').some(owner =>
      Object.keys(owner.overrides).some(key => key.startsWith(`${id}:`))))) {
      throw new Error('Native replacement of edited descendants requires subtree history')
    }
    for (const child of node.childIds) {
      if (nodes.get(child)?.parentId !== id) throw new Error('Invalid native sync removal child')
      remove(child, new Set(ancestors).add(id))
    }
    deletedParents.set(id, node.parentId)
    nodes.delete(id)
    removed.add(id)
  }
  function clone(sourceId, parentId, ancestors = new Set(), identities = new Map()) {
    if (ancestors.has(sourceId)) throw new Error('Cyclic native sync clone source')
    const source = nodes.get(sourceId)
    if (!source) throw new Error('Missing native sync clone source')
    if (identities.has(sourceId)) throw new Error('Duplicate native sync clone source')
    const id = temporaryId()
    const root = identities.size === 0
    identities.set(sourceId, id)
    const node = { ...cloneNodeProps(source, sourceId), id, parentId, childIds: [] }
    nodes.set(id, node)
    created.push(id)
    if (node.type === 'INSTANCE') affected.add(id)
    node.childIds = source.childIds.map(child => {
      if (nodes.get(child)?.parentId !== sourceId) throw new Error('Invalid native sync clone child')
      return clone(child, id, new Set(ancestors).add(sourceId), identities)
    })
    if (root) for (const targetId of identities.values()) {
      const target = nodes.get(targetId)
      nodes.set(targetId, { ...target, overrides: syncRemapOverrides(target.overrides, identities, true) })
    }
    return id
  }
  function children(sourceId, targetId, overrides, ancestors = new Set()) {
    const pair = `${sourceId}/${targetId}`
    if (ancestors.has(pair)) throw new Error('Cyclic native sync composition')
    const source = nodes.get(sourceId), target = nodes.get(targetId)
    if (!source || !target) throw new Error('Missing native sync parent')
    const plan = syncReconciliation(nodes, source, target, overrides, deletedParents)
    for (const id of plan.removed) remove(id)
    const order = []
    for (const child of plan.children) {
      const id = plan.matches.get(child.id) ?? clone(child.id, targetId)
      const current = nodes.get(id)
      const childOverrides = syncNestedOverrides(current, overrides)
      const keys = [...INSTANCE_SYNC_PROPS, ...SYNC_CHILD_PROPS]
      if (child.type === 'VECTOR') keys.push('x', 'y', 'vectorNetwork', 'fillGeometry', 'strokeGeometry')
      nodes.set(id, syncProperties(child, current, keys, childOverrides, `${id}:`))
      if (current.type === 'INSTANCE') affected.add(id)
      if (!Object.hasOwn(childOverrides, `${id}:componentId`)) {
        children(child.id, id, childOverrides, new Set(ancestors).add(pair))
      }
      order.push(id)
    }
    nodes.set(targetId, { ...nodes.get(targetId), childIds: [...order, ...plan.local] })
  }
  if (replacement) {
    // Replacement discards the old child occurrence, not the canonical source.
    // Reuse the same planned clone/removal and derived-scale validation as sync.
    for (const id of replacement.childIds) remove(id)
    const previous = previousNodes.get(replacement.componentId)
    const name = previous && replacement.name !== previous.name ? replacement.name : component.name
    nodes.set(replacementId, { ...replacement, componentId, name, childIds: [] })
  }
  const completed = new Set(), active = new Set()
  function instance(id) {
    if (completed.has(id) || removed.has(id)) return
    if (active.has(id)) throw new Error('Cyclic native instance synchronization')
    active.add(id)
    const sourceId = sources.get(id)
    if (reachable.has(sourceId)) instance(sourceId)
    const target = nodes.get(id), source = nodes.get(sourceId)
    if (!target || !source) throw new Error('Missing projected native sync instance')
    nodes.set(id, syncProperties(source, target, INSTANCE_SYNC_PROPS, target.overrides))
    affected.add(id)
    children(sourceId, id, target.overrides)
    active.delete(id)
    completed.add(id)
  }
  for (const id of reachable) instance(id)
  for (const id of affected) {
    const node = nodes.get(id)
    if (!node) continue
    const overrides = Object.fromEntries(Object.entries(node.overrides).filter(([key]) =>
      ![...removed].some(removedId => key.startsWith(`${removedId}:`))))
    nodes.set(id, { ...node, overrides })
  }
  for (const [id, changes] of planDerivedInstanceScales(nodes, previousNodes, affected)) {
    if (!nodes.has(id)) throw new Error('Derived scale targets a missing projected node')
    nodes.set(id, { ...nodes.get(id), ...structuredClone(changes) })
  }
  return { nodes, created, removed, affected }
}

function applyNativeSync(graph, plan) {
  if (!plan) return
  const actualIds = new Map()
  const idFor = id => actualIds.get(id) ?? id
  function properties(node) {
    const { id, parentId, childIds, ...props } = node
    props.componentId = idFor(props.componentId)
    props.overrides = syncRemapOverrides(props.overrides, actualIds)
    return structuredClone(props)
  }
  const depth = graph.instanceSyncDepth ?? 0
  graph.instanceSyncDepth = depth + 1
  try {
    graph.preserveSourceMetadataDuring(() => {
      for (const id of plan.created) {
        const node = plan.nodes.get(id)
        if (!node) continue
        const created = graph.createNode(node.type, idFor(node.parentId), properties(node))
        actualIds.set(id, created.id)
      }
      for (const id of plan.removed) {
        if (graph.getNode(id)) graph.deleteNode(id)
      }
      for (const [id, node] of plan.nodes) {
        if (node === graph.getNode(id)) continue
        const target = graph.getNode(idFor(id))
        if (!target) throw new Error('Missing native sync commit target')
        const changes = properties(node)
        delete changes.type
        graph.updateNode(target.id, changes)
        const order = node.childIds.map(idFor)
        if (target.childIds.length !== order.length || target.childIds.some((child, index) => child !== order[index])) {
          order.forEach((child, index) => graph.insertChildAt(child, target.id, index))
        }
      }
    })
  } finally { graph.instanceSyncDepth = depth }
  const referenced = new Set()
  for (const node of graph.nodes.values()) {
    referenced.add(node.componentId)
    for (const [key, value] of Object.entries(node.overrides)) {
      if (key.endsWith(':sourceComponentId')) referenced.add(value)
    }
  }
  for (const id of referenced) {
    const parent = graph.deletedNodeParents?.get(id)
    if (parent) referenced.add(parent)
  }
  for (const id of graph.deletedNodeParents?.keys() ?? []) {
    if (!referenced.has(id)) graph.deletedNodeParents.delete(id)
  }
}

function syncInstances(graph, componentId) {
  applyNativeSync(graph, planNativeSync(graph.nodes, graph.instanceIndex, componentId, graph.deletedNodeParents))
}

function swapInstanceComponent(graph, instanceId, componentId) {
  const instance = graph.getNode(instanceId), component = graph.getNode(componentId)
  if (instance?.type !== 'INSTANCE' || component?.type !== 'COMPONENT') throw new Error('Missing native replacement instance or component')
  if (instance.componentId === componentId) return
  const plan = planNativeSync(graph.nodes, graph.instanceIndex, componentId, graph.deletedNodeParents, instanceId)
  applyNativeSync(graph, plan)
  // The native link is authored; the validated clone geometry is derived.
  graph.updateNode(instanceId, { componentId })
}

export function correctSyncGraph(source, replace) {
  const start = source.indexOf('function syncChildren(')
  const end = source.indexOf('function copyInstanceComponentProps(', start)
  const original = source.slice(start, end)
  const fields = original.match(/for \(const key of \[([\s\S]*?)\]\)/g)
  if (start < 0 || end < 0 || fields?.length !== 1) throw new Error('Native sync child field anchor changed')
  const fieldList = fields[0].slice('for (const key of '.length, -1)
  source = replace(source, original, '')
  const swapStart = source.indexOf('function swapInstanceComponent(')
  const syncStart = source.indexOf('function syncInstances(')
  const syncEnd = source.indexOf('function detachInstance(', syncStart)
  if (swapStart < 0 || syncStart < swapStart || syncEnd < 0) throw new Error('Native sync instance anchor changed')
  const helpers = [syncReadView, syncSourceOccurrence, syncReconciliation, syncProperties, syncNestedOverrides, syncRemapOverrides,
    planNativeSync, applyNativeSync, syncInstances, swapInstanceComponent].map(fn => fn.toString()).join('\n')
  return replace(source, source.slice(swapStart, syncEnd),
    `${lineageHelpers}\nconst SYNC_CHILD_PROPS = ${fieldList};\n${helpers}\n`)
}
