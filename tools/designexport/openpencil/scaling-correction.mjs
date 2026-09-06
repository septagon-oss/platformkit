// Honor FIG's native uniformScaleFactor as authored instance state. Derived
// vector coordinates are recomputed from the canonical master, never from a
// previously scaled clone. No plugin data or fabricated import history is used.
function uniformScaleFactorValue(value) {
  if (value === null || value === undefined) return null
  const factor = typeof value === 'number' ? Math.fround(value) : NaN
  if (!Number.isFinite(factor) || factor <= 0) throw new Error('Invalid native uniform scale factor')
  return factor
}

function uniformScaleSource(graph, first) {
  const seen = new Set()
  let node = first
  while (node?.type === 'INSTANCE') {
    if (seen.has(node.id)) throw new Error('Cyclic native uniform scale source')
    seen.add(node.id)
    node = graph.getNode(node.componentId)
  }
  if (node?.type !== 'COMPONENT') throw new Error('Missing native uniform scale master')
  return node
}

function uniformScaleMaster(graph, master, factor) {
  const finite = values => values.every(value => Number.isFinite(Math.fround(value)))
  if (master.layoutMode !== 'NONE' || !finite([master.width, master.height]) || master.width <= 0 || master.height <= 0 ||
    !finite([master.width * factor, master.height * factor]) || master.effects.length || master.strokes.length || master.fills.length) {
    throw new Error('Native uniform scale requires a finite freeform vector master')
  }
  const children = graph.getChildren(master.id)
  if (children.some(child => child.type !== 'VECTOR' || child.childIds.length || child.effects.length ||
    child.rotation !== 0 || child.flipX || child.flipY || !child.vectorNetwork || child.dashPattern.length ||
    validateVectorNetwork(nativeScaleGeometry(child).vectorNetwork).length ||
    !finite([child.x, child.y, child.width, child.height].flatMap(value => [value, value * factor])) || child.width < 0 || child.height < 0 ||
    child.fills.some(fill => fill.type !== 'SOLID') ||
    child.strokes.some(stroke => !finite([stroke.weight, stroke.weight * factor]) || stroke.weight < 0 || stroke.dashPattern?.length))) {
    throw new Error('Native uniform scale currently requires flat solid-painted vector children')
  }
  return children
}

function hasScaleOverride(graph, instance, target, field) {
  if (target.id === instance.id && Object.hasOwn(instance.overrides, field)) return true
  return chain(graph, target, 'parentId').some(owner => owner.type === 'INSTANCE' &&
    Object.hasOwn(owner.overrides, `${target.id}:${field}`))
}

function nativeScaleGeometry(source) {
  // FIG and CanvasKit store these coordinates as float32. Round the canonical
  // inputs before multiplying, so reopening does not change the derived shape.
  const vectorNetwork = structuredClone(source.vectorNetwork)
  for (const point of [...vectorNetwork.vertices, ...vectorNetwork.segments.flatMap(segment =>
    [segment.tangentStart, segment.tangentEnd])]) {
    point.x = Math.fround(point.x)
    point.y = Math.fround(point.y)
    if (point.cornerRadius !== undefined) point.cornerRadius = Math.fround(point.cornerRadius)
  }
  return { ...source, vectorNetwork, ...Object.fromEntries(['x', 'y', 'width', 'height'].map(field => [field, Math.fround(source[field])])) }
}

function uniformScalePlan(graph, instance, value = instance.uniformScaleFactor) {
  const factor = uniformScaleFactorValue(value)
  if (factor === null) return null
  if (instance.type !== 'INSTANCE') throw new Error('Native uniform scale belongs to an instance')
  const master = uniformScaleSource(graph, instance)
  const sources = uniformScaleMaster(graph, master, factor).map(nativeScaleGeometry)
  const byId = new Map(sources.map(source => [source.id, source]))
  const seen = new Set(), updates = []
  for (const child of graph.getChildren(instance.id)) {
    let source = child
    const chain = new Set()
    while (source && !byId.has(source.id)) {
      if (chain.has(source.id)) throw new Error('Cyclic native uniform scale child lineage')
      chain.add(source.id)
      source = graph.getNode(source.componentId)
    }
    source = source && byId.get(source.id)
    const ownsStrokes = hasScaleOverride(graph, instance, child, 'strokes')
    if (!source || seen.has(source.id) || child.type !== 'VECTOR' ||
      (!ownsStrokes && child.strokes.length !== source.strokes.length)) {
      throw new Error('Ambiguous or unsupported native uniform scale child lineage')
    }
    seen.add(source.id)
    const vectorNetwork = transformVectorNetwork([factor, 0, 0, 0, factor, 0, 0, 0, 1], source.vectorNetwork)
    for (const vertex of vectorNetwork.vertices) if (vertex.cornerRadius !== undefined) vertex.cornerRadius *= factor
    if (validateVectorNetwork(vectorNetwork).length) throw new Error('Nonfinite scaled native vector geometry')
    const derived = {
      x: source.x * factor, y: source.y * factor, width: source.width * factor, height: source.height * factor,
      vectorNetwork, fillGeometry: scaleGeometryPaths(source.fillGeometry, factor, factor),
      strokeGeometry: scaleGeometryPaths(source.strokeGeometry, factor, factor),
      strokes: ownsStrokes ? child.strokes : child.strokes.map((stroke, index) =>
        ({ ...stroke, weight: Math.fround(source.strokes[index].weight) * factor })),
      dashPattern: source.dashPattern.map(length => length * factor), boundVariables: { ...child.boundVariables },
    }
    const patch = Object.fromEntries(Object.entries(derived).filter(([field]) => !hasScaleOverride(graph, instance, child, field)))
    const result = { ...child, ...patch }
    if (![result.x, result.y, result.width, result.height].every(Number.isFinite) ||
      validateVectorNetwork(result.vectorNetwork).length) throw new Error('Invalid native scaled geometry override')
    updates.push([child.id, patch])
  }
  if (seen.size !== sources.length) throw new Error('Missing native uniform scale children')
  const width = hasScaleOverride(graph, instance, instance, 'width') ? instance.width : Math.fround(master.width) * factor
  const height = hasScaleOverride(graph, instance, instance, 'height') ? instance.height : Math.fround(master.height) * factor
  if (![width, height].every(Number.isFinite)) throw new Error('Invalid native scale dimensions')
  return { factor, width, height, updates }
}

function applyNativeUniformScale(graph, instance, value = instance.uniformScaleFactor) {
  const plan = uniformScalePlan(graph, instance, value)
  if (!plan) return
  graph.updateNode(instance.id, { width: plan.width, height: plan.height })
  for (const [id, patch] of plan.updates) graph.updateNode(id, patch)
}

// Reconciliation supplies a projected graph, not the live graph. Geometry is
// derived from that planned state before any graph API emits a mutation event.
function planDerivedInstanceScales(nodes, previousNodes, affectedIds) {
  const view = {
    getNode: id => nodes.get(id),
    getChildren: id => (nodes.get(id)?.childIds ?? []).map(childId => nodes.get(childId)),
  }
  const updates = []
  for (const id of affectedIds) {
    const instance = nodes.get(id)
    if (instance?.type !== 'INSTANCE') continue
    const factor = instance.uniformScaleFactor ?? (previousNodes.get(id)?.uniformScaleFactor != null ? 1 : null)
    const plan = uniformScalePlan(view, instance, factor)
    if (plan) updates.push([id, { width: plan.width, height: plan.height }], ...plan.updates)
  }
  return updates
}

const factorSource = uniformScaleFactorValue.toString()
const graphSource = [uniformScaleFactorValue, uniformScaleSource, uniformScaleMaster, hasScaleOverride, nativeScaleGeometry, uniformScalePlan,
  applyNativeUniformScale, planDerivedInstanceScales]
  .map(fn => fn.toString()).join('\n')

function effectiveStrokeStyle(node, field, fallback) {
  // Vector rendering uses stroke-local styles, then its fixed defaults. Other
  // shapes use the node style when the individual stroke leaves it unset.
  const values = new Set(node.strokes.map(stroke => stroke[field] ??
    (node.type === 'VECTOR' ? fallback : node[field === 'cap' ? 'strokeCap' : 'strokeJoin'])))
  if (values.size > 1) throw new Error('Conflicting native stroke styles cannot be represented in FIG')
  return [...values][0]
}

export function correctScaleDefaults(source, replace) {
  source = replace(source, 'function createDefaultNode(generateId, type, overrides = {}) {', factorSource + String.raw`
function createDefaultNode(generateId, type, overrides = {}) {
  if (overrides.uniformScaleFactor != null) {
    if (type !== 'INSTANCE') throw new Error('Native uniform scale belongs to an instance');
    overrides = { ...overrides, uniformScaleFactor: uniformScaleFactorValue(overrides.uniformScaleFactor) };
  }`)
  return replace(source, 'figmaDerivedLayout: null,', 'figmaDerivedLayout: null,\n\t\tuniformScaleFactor: null,')
}

export function correctScaleGraph(source, replace) {
  source = replace(source, 'import { CONTAINER_TYPES,',
    'import { transformVectorNetwork, scaleGeometryPaths, validateVectorNetwork, CONTAINER_TYPES,')
  source = replace(source, 'const INSTANCE_SYNC_PROPS =', graphSource + '\nconst INSTANCE_SYNC_PROPS =')
  source = replace(source, '\t\t\t"name",\n\t\t\t"text",', '\t\t\t"uniformScaleFactor",\n\t\t\t"name",\n\t\t\t"text",')
  source = replace(source, 'if (component?.type !== "COMPONENT") return null;', String.raw`if (component?.type !== "COMPONENT") return null;
  const scale = uniformScaleFactorValue(overrides.uniformScaleFactor);
  if (scale !== null) uniformScaleMaster(graph, component, scale);`)
  source = replace(source, 'cloneChildrenWithMapping(graph, component.id, instance.id);\n\treturn instance;', String.raw`
  try {
    cloneChildrenWithMapping(graph, component.id, instance.id);
    applyNativeUniformScale(graph, instance);
    return instance;
  } catch (error) {
    graph.deleteNode(instance.id);
    throw error;
  }`)
  source = replace(source, '\tsyncInstances(componentId) {', String.raw`
  applyInstanceScale(id) {
    const instance = this.getNode(id);
    if (!instance) throw new Error('Missing native uniform scale instance');
    applyNativeUniformScale(this, instance);
  }
  syncInstances(componentId) {`)
  source = replace(source, '\tupdateNode(id, changes) {', String.raw`
  updateNode(id, changes) {
    let resetUniformScale = false;
    if (!this.instanceSyncDepth && Object.hasOwn(changes, 'uniformScaleFactor')) {
      const node = this.nodes.get(id);
      if (changes.uniformScaleFactor != null || node?.uniformScaleFactor != null) {
        if (!node) throw new Error('Missing native uniform scale instance');
        resetUniformScale = changes.uniformScaleFactor == null;
        const plan = uniformScalePlan(this, node, resetUniformScale ? 1 : changes.uniformScaleFactor);
        changes = { ...changes, uniformScaleFactor: resetUniformScale ? null : plan.factor };
      }
    }`)
  return replace(source, 'this.emitter.emit("node:updated", id, changes);', String.raw`
    if (!this.instanceSyncDepth && Object.hasOwn(changes, 'uniformScaleFactor')) applyNativeUniformScale(this, node, resetUniformScale ? 1 : node.uniformScaleFactor);
    this.emitter.emit("node:updated", id, changes);`)
}

export function correctScaleNodeChange(source, replace) {
  source = replace(source, 'function nodeChangeToProps(nc, blobs) {',
    factorSource + '\n' + effectiveStrokeStyle.toString() + '\nfunction nodeChangeToProps(nc, blobs) {')
  source = replace(source, 'componentId: extractSymbolId(nc),',
    'componentId: extractSymbolId(nc),\n\t\tuniformScaleFactor: uniformScaleFactorValue(nc.symbolData?.uniformScaleFactor),')
  source = replace(source, 'if (node.strokeCap !== "NONE") nc.strokeCap = node.strokeCap;',
    'if (node.strokes.length) nc.strokeCap = effectiveStrokeStyle(node, "cap", "NONE");\n' +
    '\telse if (node.strokeCap !== "NONE") nc.strokeCap = node.strokeCap;')
  source = replace(source, 'if (node.strokeJoin !== "MITER" || "strokeJoin" in rawNodeFields) nc.strokeJoin = node.strokeJoin;',
    'if (node.strokes.length) nc.strokeJoin = effectiveStrokeStyle(node, "join", "MITER");\n' +
    '\telse if (node.strokeJoin !== "MITER" || "strokeJoin" in rawNodeFields) nc.strokeJoin = node.strokeJoin;')
  return replace(source, 'if (node.source.fig.uniformScaleFactor != null) symbolData.uniformScaleFactor = node.source.fig.uniformScaleFactor;',
    'if (node.uniformScaleFactor != null) symbolData.uniformScaleFactor = uniformScaleFactorValue(node.uniformScaleFactor);')
}

export function correctScaleImport(source, replace) {
  return replace(source, '\tapplyGeneratedFreeformStretch(ctx);\n}',
    '\tapplyGeneratedFreeformStretch(ctx);\n' +
    '\tfor (const node of overrideCandidates(graph, ctx.activeNodeIds)) if (node.uniformScaleFactor != null) graph.applyInstanceScale(node.id);\n}')
}
