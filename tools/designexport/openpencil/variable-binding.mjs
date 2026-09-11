// The provider's qualified numeric consumers, not another source token table.
// Other native fields keep their existing behavior and acquire no support claim.
const dimensions = new Set(['width', 'height'])
const spacing = new Set(['itemSpacing', 'counterAxisSpacing', 'paddingLeft', 'paddingRight', 'paddingTop', 'paddingBottom'])
const boxes = new Set(['RECTANGLE', 'FRAME', 'COMPONENT', 'INSTANCE'])
export const isNumericBindingField = field => dimensions.has(field) || spacing.has(field) || field === 'cornerRadius'
const layoutScope = Symbol('native numeric layout scope')
const reject = message => { throw new Error(`Native numeric binding: ${message}`) }

function valueFor(graph, node, field, id) {
  const variable = graph.variables.get(id)
  if (variable?.type !== 'FLOAT') reject(`${node.name}.${field} requires a FLOAT variable`)
  if (spacing.has(field) && !['HORIZONTAL', 'VERTICAL'].includes(node.layoutMode)) {
    reject(`${node.name}.${field} requires a flex container`)
  }
  if (dimensions.has(field)) {
    if (!boxes.has(node.type)) reject(`${node.name}.${field} requires a fixed rectangular box`)
    const sizing = (field === 'width') === (node.layoutMode === 'HORIZONTAL') ? node.primaryAxisSizing : node.counterAxisSizing
    if (node.layoutMode !== 'NONE' && sizing !== 'FIXED' || node.type === 'TEXT' &&
        (node.textAutoResize === 'WIDTH_AND_HEIGHT' || field === 'height' && node.textAutoResize === 'HEIGHT')) {
      reject(`${node.name}.${field} is owned by intrinsic or fill layout`)
    }
    const parent = graph.getNode(node.parentId), primary = (field === 'width') === (parent?.layoutMode === 'HORIZONTAL')
    if (parent && ['HORIZONTAL', 'VERTICAL', 'GRID'].includes(parent.layoutMode) && node.layoutPositioning !== 'ABSOLUTE' &&
        (primary && node.layoutGrow > 0 || !primary && node.layoutAlignSelf === 'STRETCH')) {
      reject(`${node.name}.${field} is owned by its containing layout`)
    }
  }
  if (field === 'cornerRadius' && (!boxes.has(node.type) || node.independentCorners)) {
    reject(`${node.name}.${field} requires uniform rectangular corners`)
  }
  // Validate every dependency the native resolver actually reads, including
  // aliases into other collections. Do not reproduce its fallback evaluator.
  const reader = Object.create(graph)
  reader.variables = { get(key) {
    const input = graph.variables.get(key)
    if (input?.type !== 'FLOAT') reject(`${node.name}.${field} requires FLOAT dependencies`)
    if (input.sourceToken && (input.sourceToken.kind !== 'scale' || input.sourceToken.unit !== 'px')) {
      reject(`${node.name}.${field} requires absolute-pixel dependencies`)
    }
    validateMode(graph, input.collectionId, graph.getNodeVariableModeId(node.id, input.collectionId))
    return input
  } }
  const value = reader.resolveNumberVariableForNode(node.id, id)
  if (typeof value !== 'number' || !Number.isFinite(value) || !Number.isFinite(Math.fround(value)) || value < 0) {
    reject(`${node.name}.${field} requires a finite nonnegative pixel value`)
  }
  if (dimensions.has(field)) {
    const axis = field === 'width' ? 'Width' : 'Height', min = node[`min${axis}`], max = node[`max${axis}`]
    if (min != null && value < min || max != null && value > max) reject(`${node.name}.${field} conflicts with its size limits`)
  }
  return value
}

function validateMode(graph, collectionId, modeId) {
  if (!graph.variableCollections.get(collectionId)?.modes.some(mode => mode.modeId === modeId)) {
    reject('mode must belong to its collection')
  }
}

// Change one authored collection choice, never the resolved source/ancestor
// map. null means inherit; an explicit default is still an authored choice.
export function nodeModeChoice(graph, id, collectionId, modeId) {
  if (typeof collectionId !== 'string' || !collectionId) reject('mode collection identity is required')
  const modes = graph.getNodeExplicitVariableModes(id)
  if (modeId === null) delete modes[collectionId]
  else {
    validateMode(graph, collectionId, modeId)
    modes[collectionId] = modeId
  }
  return modes
}

function affectedLayout(graph, bound) {
  const roots = new Set()
  for (const id of bound) {
    let node = graph.getNode(id)
    const seen = new Set()
    while (node?.parentId) {
      if (seen.has(node.id)) reject('cyclic layout ancestry')
      seen.add(node.id)
      const parent = graph.getNode(node.parentId)
      if (!parent || parent.layoutMode === 'NONE' || node.layoutPositioning === 'ABSOLUTE') break
      node = parent
    }
    if (node) roots.add(node.id)
  }
  const nodes = new Set(), active = new Set()
  function visit(id) {
    if (active.has(id)) reject('cyclic layout containment')
    if (nodes.has(id)) return
    const node = graph.getNode(id)
    if (!node) reject('missing layout node')
    active.add(id)
    nodes.add(id)
    for (const child of graph.getChildren(id)) {
      if (child.visible && child.layoutPositioning !== 'ABSOLUTE') visit(child.id)
    }
    active.delete(id)
  }
  for (const id of roots) visit(id)
  return { roots: [...roots], nodes }
}

// Resolve through the existing node-aware SDK resolver. No temporary token,
// node field, cache or event is written while the complete plan is checked.
export function planNumericBindings(graph) {
  const changes = new Map(), layout = new Set()
  for (const node of graph.nodes.values()) {
    const values = {}
    for (const [field, id] of Object.entries(node.boundVariables)) {
      if (!isNumericBindingField(field)) continue
      values[field] = valueFor(graph, node, field, id)
      if (field !== 'cornerRadius') layout.add(node.id)
    }
    if (Object.keys(values).length) changes.set(node.id, values)
  }
  return { changes, ...affectedLayout(graph, layout) }
}

export function applyNumericBindings(graph, plan = planNumericBindings(graph), notify = true) {
  const patches = new Map()
  for (const [id, values] of plan.changes) {
    const node = graph.getNode(id), changes = Object.fromEntries(Object.entries(values)
      .filter(([field, value]) => !Object.is(node[field], value)))
    // Invalidate imported fallbacks without inventing authored overrides. FIG
    // serialization already distinguishes changed fields from native bindings.
    const editedFields = [...new Set([...node.source.editedFields, ...Object.keys(values)])]
    if (editedFields.length !== node.source.editedFields.length) changes.source = { ...node.source, editedFields }
    if (Object.keys(changes).length) patches.set(id, changes)
  }
  for (const id of plan.nodes) {
    if (graph.getNode(id).figmaDerivedLayout) patches.set(id, { ...patches.get(id), figmaDerivedLayout: null })
  }
  if (!patches.size) return
  graph.withLayoutMutations(() => graph.preserveSourceMetadataDuring(() => {
    for (const [id, changes] of patches) graph.updateNode(id, changes)
  }))
  if (notify) graph.emitter.emit('numericBindings:updated', plan.roots)
}

// This context belongs to one synchronous native layout call. It is neither
// saved metadata nor a retained graph registry. Ordinary imported trees outside
// the affected containment keep their existing preservation rules.
export function withNumericLayout(graph, run) {
  if (graph[layoutScope]) return run()
  const plan = planNumericBindings(graph)
  graph[layoutScope] = plan.nodes
  try {
    applyNumericBindings(graph, plan, false)
    return run()
  } finally { delete graph[layoutScope] }
}

export function usesNumericLayout(graph, node) {
  return graph[layoutScope]?.has(node.id) ?? false
}

export function planNumericNodeUpdate(graph, id, changes) {
  if (graph.isApplyingLayout || graph.sourceMetadataPreservationDepth > 0 || graph.instanceSyncDepth > 0) return
  const node = graph.getNode(id)
  if (!node) return
  if (Object.hasOwn(changes, 'variableModes')) {
    const modes = changes.variableModes
    if (!modes || typeof modes !== 'object' || Array.isArray(modes) ||
        ![Object.prototype, null].includes(Object.getPrototypeOf(modes))) reject('mode selections must be a collection map')
    for (const [collection, mode] of Object.entries(modes)) validateMode(graph, collection, mode)
  }
  for (const [field, value] of Object.entries(changes)) {
    if (isNumericBindingField(field) && node.boundVariables[field] && value !== undefined && !Object.is(node[field], value)) {
      reject(`unbind ${node.name}.${field} before editing its literal value`)
    }
  }
  if (!['variableModes', 'boundVariables', 'layoutMode', 'primaryAxisSizing', 'counterAxisSizing', 'textAutoResize', 'independentCorners',
    'layoutGrow', 'layoutAlignSelf', 'layoutPositioning', 'minWidth', 'maxWidth', 'minHeight', 'maxHeight']
    .some(field => Object.hasOwn(changes, field))) return
  const candidate = Object.create(graph)
  candidate.nodes = new Map(graph.nodes).set(id, { ...node, ...changes })
  if (Object.hasOwn(changes, 'variableModes')) {
    const scope = instanceField(candidate, candidate.getNode(id), 'variableModes')
    if (scope) candidate.nodes.set(scope.owner.id, { ...scope.owner, overrides: { ...scope.owner.overrides } })
    ownVariableModes(candidate, candidate.getNode(id))
  }
  return planNumericBindings(candidate)
}

export function planNumericBinding(graph, id, field, variableId) {
  if (!isNumericBindingField(field)) return
  const node = graph.getNode(id)
  if (!node) return
  const candidate = Object.create(graph)
  candidate.nodes = new Map(graph.nodes).set(id, { ...node,
    boundVariables: { ...node.boundVariables, [field]: variableId } })
  return planNumericBindings(candidate)
}

export function planNumericActiveMode(graph, collectionId, modeId) {
  validateMode(graph, collectionId, modeId)
  const candidate = Object.create(graph)
  candidate.activeMode = new Map(graph.activeMode).set(collectionId, modeId)
  return planNumericBindings(candidate)
}

// A removed reference becomes an authored literal, not an inherited master
// fallback. Use the SDK's existing instance override keys; rebinding releases it.
export function ownNumericLiteral(graph, node, field, literal) {
  if (!isNumericBindingField(field)) return
  ownInstanceField(graph, node, field, literal)
}

export function ownVariableModes(graph, node) {
  ownInstanceField(graph, node, 'variableModes', Object.keys(node.variableModes).length > 0)
}

function ownInstanceField(graph, node, field, owned) {
  const scope = instanceField(graph, node, field)
  if (!scope) return
  if (owned) scope.owner.overrides[scope.key] = true
  else delete scope.owner.overrides[scope.key]
}

function instanceField(graph, node, field) {
  let owner = node
  while (owner && owner.type !== 'INSTANCE') owner = graph.getNode(owner.parentId)
  return owner ? { owner, key: owner.id === node.id ? field : `${node.id}:${field}` } : null
}

// A copied nonempty mode map can still be inherited. History must retain the
// native ownership key independently, without snapshotting unrelated overrides.
export function variableModeOwnership(graph, node) {
  const scope = instanceField(graph, node, 'variableModes')
  if (!scope) return null
  const { owner, key } = scope
  return { ownerId: owner.id, key, present: Object.hasOwn(owner.overrides, key), value: structuredClone(owner.overrides[key]) }
}

export function restoreVariableModeOwnership(graph, id, snapshot) {
  if (snapshot === undefined) return
  const node = graph.getNode(id)
  if (!node) reject('mode history target no longer exists')
  const scope = instanceField(graph, node, 'variableModes')
  if ((scope?.owner.id ?? null) !== (snapshot?.ownerId ?? null) || (scope?.key ?? null) !== (snapshot?.key ?? null)) {
    reject('mode history owner changed')
  }
  if (!scope) return
  if (snapshot.present) scope.owner.overrides[scope.key] = structuredClone(snapshot.value)
  else delete scope.owner.overrides[scope.key]
}
