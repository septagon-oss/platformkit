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
  const overrides = ancestryOverrides(syncReadView(nodes), parent)
  if (Object.hasOwn(overrides, `${target.id}:componentId`)) return nodes.get(target.componentId)
  if (!target.componentId && !Object.hasOwn(overrides, `${target.id}:sourceComponentId`)) return
  return sourceChild(syncReadView(nodes), sourceParent, target, overrides)
}

function explicitVariableModes(nodes, target) {
  const source = syncSourceOccurrence(nodes, target)
  const overrides = ancestryOverrides(syncReadView(nodes), target)
  return source && !Object.hasOwn(overrides, `${target.id}:variableModes`) ? {} : target.variableModes
}

function inheritedVariableModes(nodes, target, visiting = new Set()) {
  if (visiting.has(target.id)) throw new Error('Cyclic native mode inheritance')
  const source = syncSourceOccurrence(nodes, target)
  return source ? { ...inheritedVariableModes(nodes, source, new Set(visiting).add(target.id)),
    ...explicitVariableModes(nodes, target) } : target.variableModes
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
  if (changes.strokes && (source.dashPattern.length || target.dashPattern.length ||
      Object.hasOwn(overrides, `${prefix}dashPattern`))) changes.strokes = changes.strokes.map(stroke => ({ ...stroke,
    dashPattern: [...(changes.dashPattern ?? target.dashPattern)],
  }))
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
  // Imported fallbacks describe the previous occurrence. Inherited changes
  // invalidate those fields too, without claiming a local instance override.
  // Stage the markers on a fresh source record so refused plans stay pure.
  const projected = { ...target, ...structuredClone(changes),
    source: { ...target.source, editedFields: [...target.source.editedFields] } }
  markSourceFieldsEdited(projected, Object.keys(changes).filter(key => !isEqual(target[key], changes[key])))
  return projected
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

function syncReplacementOccurrence(nodes, target) {
  const view = syncReadView(nodes), parent = nodes.get(target.parentId)
  if (!parent || ['CANVAS', 'COMPONENT', 'COMPONENT_SET'].includes(parent.type)) return null
  const sourceParent = syncSourceOccurrence(nodes, parent)
  if (!sourceParent) return null
  const ancestry = chain(view, parent, 'parentId')
  const owner = ancestry.find(node => node.type === 'INSTANCE')
  const overrides = ancestryOverrides(view, parent)
  return { owner, occurrence: sourceChild(view, sourceParent, target, overrides) }
}

function syncPaintRoles(nodes, target) {
  const view = syncReadView(nodes)
  const occurrence = syncReplacementOccurrence(nodes, target)?.occurrence
  if (!occurrence) return new Map()
  if (occurrence.type !== 'INSTANCE') return new Map()
  const canonical = chain(view, occurrence, 'componentId').at(-1)
  const pairs = sourceChildren(view, canonical, occurrence, occurrence.overrides)
  const roles = new Map()
  for (const [id, source] of pairs) {
    const effective = nodes.get(id)
    for (const field of new Set([...Object.keys(source.boundVariables), ...Object.keys(effective.boundVariables)])) {
      if (!/^(fills|strokes)\/\d+\/color$/.test(field)) continue
      const from = source.boundVariables[field], to = effective.boundVariables[field]
      if (!from || !to || roles.has(from) && roles.get(from) !== to) {
        throw new Error('Ambiguous or partial native paint role correspondence')
      }
      roles.set(from, to)
    }
  }
  return new Map([...roles].filter(([from, to]) => from !== to))
}

function syncRoleBindings(source, roles) {
  return Object.fromEntries(Object.entries(source.boundVariables).map(([field, variable]) =>
    [field, /^(fills|strokes)\/\d+\/color$/.test(field) ? roles.get(variable) ?? variable : variable]))
}

// Shared set properties identify corresponding content across variants. Walk
// from those explicit anchors to retain their layout ancestry too; incompatible
// merges, splits or depths need subtree history, never name/order matching.
function syncVariantCorrespondence(nodes, instance, component) {
  const view = syncReadView(nodes), previous = chain(view, instance, 'componentId').at(-1)
  const owner = nodes.get(component.parentId)
  const matches = new Map(), used = new Map()
  if (owner?.type !== 'COMPONENT_SET' || previous?.parentId !== owner.id) return matches
  const definitions = owner.componentPropertyDefinitions.filter(definition => definition.type === 'TEXT')
  if (definitions.some(definition => owner.componentPropertyDefinitions.filter(item => item.id === definition.id).length !== 1 ||
      owner.componentPropertyDefinitions.filter(item => item.name === definition.name).length !== 1 ||
      [previous, component].some(node => node.componentPropertyDefinitions.some(item => item.id === definition.id)))) {
    throw new Error('Ambiguous shared variant property definition')
  }
  function target(root, definition) {
    const pending = [...view.getChildren(root.id)], found = [], seen = new Set()
    while (pending.length) {
      const node = pending.pop()
      if (seen.has(node.id)) throw new Error('Cyclic shared variant property subtree')
      seen.add(node.id)
      const refs = node.componentPropertyReferences.filter(ref => ref.propertyId === definition.id)
      if (refs.length) {
        if (refs.length !== 1 || refs[0].field !== 'TEXT' || node.type !== 'TEXT' || node.childIds.length) {
          throw new Error('Shared variant property requires one literal text target')
        }
        found.push(node)
      }
      if (!['COMPONENT', 'INSTANCE'].includes(node.type)) pending.push(...view.getChildren(node.id))
    }
    if (found.length !== 1) throw new Error('Missing or ambiguous shared variant property target')
    return found[0]
  }
  function path(root, leaf) {
    const ancestry = chain(view, leaf, 'parentId'), end = ancestry.indexOf(root)
    if (end < 1) throw new Error('Shared variant property lies outside its owner')
    return ancestry.slice(0, end).reverse()
  }
  for (const definition of definitions) {
    const before = path(previous, target(previous, definition)), after = path(component, target(component, definition))
    if (before.length !== after.length) throw new Error('Shared variant property ancestry requires subtree history')
    let source = previous, placed = instance
    for (const [index, child] of before.entries()) {
      const links = sourceChildren(view, source, placed, ancestryOverrides(view, placed))
      const occurrences = [...links].filter(([, linked]) => linked === child)
      if (occurrences.length !== 1) throw new Error('Ambiguous shared variant property occurrence')
      placed = nodes.get(occurrences[0][0])
      const next = after[index]
      if (placed.type !== next.type || placed.type !== child.type ||
          JSON.stringify(placed.componentPropertyReferences) !== JSON.stringify(child.componentPropertyReferences) ||
          matches.has(next.id) && matches.get(next.id) !== placed.id ||
          used.has(placed.id) && used.get(placed.id) !== next.id) {
        throw new Error('Incompatible shared variant property ancestry')
      }
      matches.set(next.id, placed.id)
      used.set(placed.id, next.id)
      source = child
    }
  }
  return matches
}

function syncApplyPaintRoles(nodes, instance, roles) {
  if (!roles.size) return
  const view = syncReadView(nodes), used = new Set(), overrides = { ...instance.overrides }
  for (const child of view.getChildren(instance.id)) {
    const source = chain(view, child, 'componentId').at(-1)
    if (source.type !== 'VECTOR' || source.childIds.length) throw new Error('Native paint roles require flat vector correspondence')
    const matches = Object.entries(source.boundVariables).filter(([field, variable]) =>
      /^(fills|strokes)\/\d+\/color$/.test(field) && roles.has(variable))
    if (!matches.length) continue
    const boundVariables = syncRoleBindings(source, roles)
    for (const [, variable] of matches) used.add(variable)
    nodes.set(child.id, { ...child, boundVariables })
    overrides[`${child.id}:boundVariables`] = true
  }
  if ([...roles.keys()].some(variable => !used.has(variable))) throw new Error('Missing native replacement paint role')
  nodes.set(instance.id, { ...instance, overrides })
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
  const replacementOccurrence = replacement && syncReplacementOccurrence(previousNodes, replacement)
  const paintRoles = replacement ? syncPaintRoles(previousNodes, replacement) : new Map()
  const variantMatches = replacement ? syncVariantCorrespondence(previousNodes, replacement, component) : new Map()
  const retained = new Set(variantMatches.values())
  const removalSources = new Map(replacement ? [[replacementId, chain(syncReadView(previousNodes), replacement, 'componentId').at(-1)]] : [])
  function removalSource(node) {
    if (!removalSources.has(node.id)) {
      const view = syncReadView(previousNodes), parent = previousNodes.get(node.parentId)
      if (!parent) throw new Error('Missing native removal source parent')
      for (const [id, source] of sourceChildren(view, removalSource(parent), parent, ancestryOverrides(view, parent))) {
        removalSources.set(id, source)
      }
    }
    if (!removalSources.has(node.id)) throw new Error('Missing native removal source')
    return removalSources.get(node.id)
  }
  function temporaryId() {
    let id
    do { id = `native-sync:${serial++}` } while (nodes.has(id) || previousNodes.has(id))
    return id
  }
  function remove(id, ancestors = new Set()) {
    if (retained.has(id)) return
    if (ancestors.has(id)) throw new Error('Cyclic native sync removal')
    const node = nodes.get(id)
    if (!node) throw new Error('Missing native sync removal target')
    // Compare with the owning occurrence, not the terminal asset: containing
    // components can intentionally specialize their private icons and layout.
    const source = replacement && removalSource(node)
    const expected = replacement && { ...source, ...originalScales.get(id), boundVariables: syncRoleBindings(source, paintRoles) }
    // Yoga and FIG round used dimensions to float32; a scaled 16px instance
    // can otherwise differ from its source by less than one representable bit.
    const unchanged = field => ['width', 'height'].includes(field) && Number.isFinite(Math.fround(node[field])) &&
      Math.fround(node[field]) === Math.fround(expected[field]) || JSON.stringify(node[field]) === JSON.stringify(expected[field])
    const derivedPaint = field => paintRoles.size && ['fills', 'strokes', 'boundVariables'].includes(field) &&
      JSON.stringify(node[field]) === JSON.stringify(expected[field])
    const edited = replacement && node.source.editedFields.some(field => !unchanged(field))
    if (replacement && (edited || chain(syncReadView(previousNodes), replacement, 'parentId').some(owner =>
      Object.keys(owner.overrides).some(key => key.startsWith(`${id}:`) && !derivedPaint(key.slice(id.length + 1)))))) {
      throw new Error(`Native replacement of edited descendants requires subtree history: ${node.type} ${node.name}`)
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
      const childOverrides = scopedOverrides(current, overrides)
      if (Object.hasOwn(childOverrides, `${id}:componentId`)) {
        // The containing slot owns identity/order, not the replacement's
        // inputs. Its new master propagates through the native instance index.
        order.push(id)
        continue
      }
      const keys = [...INSTANCE_SYNC_PROPS, ...SYNC_CHILD_PROPS]
      // Reuse the SDK's complete typography/rendering fields, not its partial
      // instance list: retained variant text must inherit the new line box too.
      if (child.type === 'TEXT') keys.push(...TEXT_STYLE_KEYS, ...TEXT_PICTURE_KEYS)
      if (child.type === 'VECTOR') keys.push('x', 'y', 'vectorNetwork', 'fillGeometry', 'strokeGeometry')
      nodes.set(id, syncProperties(child, current, keys, childOverrides, `${id}:`))
      if (current.type === 'INSTANCE') affected.add(id)
      children(child.id, id, childOverrides, new Set(ancestors).add(pair))
      order.push(id)
    }
    nodes.set(targetId, { ...nodes.get(targetId), childIds: [...order, ...plan.local] })
  }
  if (replacement) {
    // Replacement discards the old child occurrence, not the canonical source.
    // Reuse the same planned clone/removal and derived-scale validation as sync.
    for (const id of replacement.childIds) remove(id)
    for (const [sourceId, id] of variantMatches) {
      const current = nodes.get(id)
      for (const childId of current.childIds) remove(childId)
      nodes.set(id, { ...current, componentId: sourceId, childIds: current.childIds.filter(child => retained.has(child)) })
    }
    // Imported overrides may explicitly name the previous source occurrence.
    // Retained native IDs keep authored edits; only their source anchors move.
    const identities = new Map([...variantMatches].map(([sourceId, id]) => [id, sourceId]))
    const owners = new Set([...retained, ...chain(syncReadView(previousNodes), replacement, 'parentId').map(node => node.id)])
    const suffix = ':sourceComponentId'
    for (const id of owners) {
      const current = nodes.get(id)
      if (!Object.keys(current.overrides).some(key => key.endsWith(suffix) && identities.has(key.slice(0, -suffix.length)))) continue
      const overrides = Object.fromEntries(Object.entries(current.overrides).map(([key, value]) =>
        [key, key.endsWith(suffix) ? identities.get(key.slice(0, -suffix.length)) ?? value : value]))
      nodes.set(id, { ...current, overrides })
    }
    const previous = previousNodes.get(replacement.componentId)
    const name = previous && replacement.name !== previous.name ? replacement.name : component.name
    nodes.set(replacementId, { ...nodes.get(replacementId), componentId, name,
      childIds: replacement.childIds.filter(id => retained.has(id)) })
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
    const overrides = ancestryOverrides(syncReadView(nodes), target)
    const effective = { ...source }
    if (source.type === 'COMPONENT') {
      // A canonical master owns intrinsic layout, not its occurrence's fill
      // relationship to a parent. An enclosing INSTANCE template still owns
      // that placement and can change it; do not invent a local override.
      const mode = Object.hasOwn(overrides, `${id}:layoutMode`) ? target.layoutMode : source.layoutMode
      for (const width of [true, false]) {
        const before = width === (target.layoutMode === 'HORIZONTAL') ? 'primaryAxisSizing' : 'counterAxisSizing'
        const after = width === (mode === 'HORIZONTAL') ? 'primaryAxisSizing' : 'counterAxisSizing'
        if (target[before] === 'FILL') {
          effective[after] = 'FILL'
          // The containing layout owns the resolved dimension as well as the
          // sizing mode; copying the master size detaches positioned children.
          const dimension = width ? 'width' : 'height'
          effective[dimension] = target[dimension]
        }
      }
    }
    nodes.set(id, syncProperties(effective, target, INSTANCE_SYNC_PROPS, overrides, `${id}:`))
    affected.add(id)
    children(sourceId, id, overrides)
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
  if (replacement) syncApplyPaintRoles(nodes, nodes.get(replacementId), paintRoles)
  if (replacementOccurrence?.owner) {
    const owner = nodes.get(replacementOccurrence.owner.id)
    nodes.set(owner.id, { ...owner, overrides: { ...owner.overrides,
      [`${replacementId}:sourceComponentId`]: replacementOccurrence.occurrence.id,
      [`${replacementId}:componentId`]: componentId,
    } })
  }
  for (const [id, changes] of planDerivedInstanceScales(nodes, previousNodes, affected)) {
    if (!nodes.has(id)) throw new Error('Derived scale targets a missing projected node')
    nodes.set(id, { ...nodes.get(id), ...structuredClone(changes) })
  }
  return { nodes, created, removed, affected }
}

export function applyNativeSync(graph, plan) {
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
  source = 'import { isEqual } from "es-toolkit";\n' + source
  // The SDK already distinguishes layout mutations from authored operations.
  // Computed positions must not masquerade as local descendant edits. Keep
  // preexisting edit markers and changed dimensions required by FIG sizing.
  // Repeated identical sizes are not edits: they would invalidate imported
  // inline advances on the next layout pass, even while fonts are unavailable.
  source = replace(source, 'if (this.sourceMetadataPreservationDepth === 0) markSourceFieldsEdited(node, Object.keys(changes));',
    'if (this.sourceMetadataPreservationDepth === 0) markSourceFieldsEdited(node, Object.keys(changes).filter(key => !this.isApplyingLayout || !["x", "y"].includes(key) && (!["width", "height"].includes(key) || changes[key] !== node[key])));')
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
  const helpers = [syncReadView, syncSourceOccurrence, explicitVariableModes, inheritedVariableModes, syncReconciliation, syncProperties, syncRemapOverrides,
    syncReplacementOccurrence, syncPaintRoles, syncRoleBindings, syncVariantCorrespondence, syncApplyPaintRoles,
    planNativeSync, applyNativeSync, syncInstances, swapInstanceComponent].map(fn => fn.toString()).join('\n')
  return replace(source, source.slice(swapStart, syncEnd),
    `${lineageHelpers}\nconst SYNC_CHILD_PROPS = ${fieldList};\n${helpers}\n`)
}
