import { fileURLToPath } from 'node:url'
import { dependentLayoutChanged, layoutNodes, ownSourceLayoutRecord, sourceCompositionLayout } from './layout-correction.mjs'
import { chain } from './exporter-correction.mjs'

const requireFragment = (condition, message) => {
  if (!condition) throw new Error(`Source fragment: ${message}`)
}

const sourceEntries = node => node?.pluginData?.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source') ?? []

function fragmentDeclarations(node) {
  const entries = sourceEntries(node)
  return entries.flatMap(entry => {
    let record
    try { record = JSON.parse(entry.value) } catch { return [] }
    return record && Object.hasOwn(record, 'cssFragment') ? [{ record, count: entries.length }] : []
  })
}

function fragmentRecord(graph, node) {
  let lineage
  try { lineage = chain(graph, node, 'componentId') } catch {
    requireFragment(fragmentDeclarations(node).length === 0, 'explicit fragment has unresolved component lineage')
    return
  }
  const declarations = lineage.flatMap(fragmentDeclarations)
  if (!declarations.length) return
  const source = ownSourceLayoutRecord(lineage.at(-1))
  requireFragment(source && Object.hasOwn(source, 'cssFragment') && lineage.every(link => sourceEntries(link).length <= 1) && declarations.every(({ record, count }) =>
    count === 1 && record.schema === source.schema && record.scope === source.scope &&
    JSON.stringify(record.cssFragment) === JSON.stringify(source.cssFragment)), 'ambiguous fragment source identity')
  return source
}

// A source invocation owns nodes without introducing a formatting box. Native
// geometry remains an editor selection envelope, not a flex/grid contribution.
export function sourceFragment(graph, node) {
  const source = fragmentRecord(graph, node)
  if (!source || !Object.hasOwn(source, 'cssFragment')) return false
  const record = source.cssFragment
  requireFragment(record && Object.keys(record).length === 1 && record.version === 1 &&
    source.scope === 'source-composition-observed-aliases' && ['COMPONENT', 'INSTANCE'].includes(node.type),
  'one versioned source component owner required')
  requireFragment(node.layoutMode === 'NONE' && node.primaryAxisSizing === 'HUG' && node.counterAxisSizing === 'HUG' &&
    node.layoutPositioning !== 'ABSOLUTE', 'the identity cannot own a layout box or absolute placement')
  requireFragment(node.layoutDirection === 'AUTO', 'direction belongs to the effective source box')
  requireFragment(node.layoutGrow === 0 && node.layoutAlignSelf === 'AUTO' && node.gridPosition == null &&
    [node.minWidth, node.maxWidth, node.minHeight, node.maxHeight].every(value => value == null) &&
    !node.gridTemplateColumns.length && !node.gridTemplateRows.length && node.itemSpacing === 0 && node.counterAxisSpacing === 0 &&
    (node.gridColumnGap ?? 0) === 0 && (node.gridRowGap ?? 0) === 0,
  'item sizing, placement and spacing belong to real source members')
  requireFragment(node.rotation === 0 && !node.flipX && !node.flipY && node.opacity === 1 && !node.clipsContent &&
    !node.isMask && node.blendMode === 'PASS_THROUGH' &&
    node.fills.length === 0 && node.strokes.length === 0 && node.effects.length === 0 &&
    [node.paddingTop, node.paddingRight, node.paddingBottom, node.paddingLeft].every(value => value === 0),
  'paint, clipping, padding or transforms require a real source box')
  return true
}

export function fragmentLayoutParent(graph, node) {
  const seen = new Set([node.id])
  let parent = graph.getNode(node.parentId)
  while (parent && sourceFragment(graph, parent)) {
    requireFragment(!seen.has(parent.id), 'cyclic ownership')
    seen.add(parent.id)
    parent = graph.getNode(parent.parentId)
  }
  return parent
}

export function validateFragmentContext(graph, node) {
  for (const ancestor of chain(graph, node, 'parentId')) sourceFragment(graph, ancestor)
}

// Import already owns the raw GUID map and ordinary node decoder. View those
// same records through the native lineage contract before this node is added;
// grid track GUIDs belong to the effective formatting parent, not the owner.
export function fragmentFigParent(id, changes, parents, decode) {
  const nodes = new Map()
  const graph = { getNode(key) {
    if (!nodes.has(key)) {
      const change = changes.get(key)
      if (!change) return
      const { nodeType, ...props } = decode(change)
      nodes.set(key, { gridTemplateColumns: [], gridTemplateRows: [], ...props, id: key, type: nodeType, parentId: parents.get(key) })
    }
    return nodes.get(key)
  } }
  const node = graph.getNode(id)
  if (sourceFragment(graph, node)) {
    const change = changes.get(id)
    requireFragment(['gridRowAnchor', 'gridColumnAnchor', 'gridRowSpan', 'gridColumnSpan',
      'gridChildHorizontalAlign', 'gridChildVerticalAlign'].every(field => change[field] === undefined),
    'the identity cannot own FIG grid-item fields')
    return
  }
  const parent = fragmentLayoutParent(graph, node)
  return changes.get(parent?.id)
}

// Return existing nodes in effective layout order. Do not flatten ownership or
// follow master links; hidden identities suppress their entire placed subtree.
export function fragmentLayoutChildren(graph, parent) {
  const result = [], seen = new Set([parent.id])
  function visit(node) {
    requireFragment(!seen.has(node.id), 'cyclic or repeated ownership')
    seen.add(node.id)
    if (!sourceFragment(graph, node)) result.push(node)
    else if (node.visible) for (const child of graph.getChildren(node.id)) visit(child)
  }
  for (const child of graph.getChildren(parent.id)) visit(child)
  return result
}

export function hasSourceFragments(graph, root) {
  if (!root) return false
  let found = false
  const pending = [root], seen = new Set()
  while (pending.length) {
    const node = pending.pop()
    requireFragment(!seen.has(node.id), 'cyclic or repeated ownership')
    seen.add(node.id)
    const fragment = sourceFragment(graph, node)
    if (fragment) {
      found = true
      requireFragment(graph.getChildren(node.id).every(child => child.layoutPositioning !== 'ABSOLUTE'),
        'absolute members need an explicit effective containing block')
    }
    pending.push(...graph.getChildren(node.id))
  }
  return found
}

export function fragmentCacheNodes(graph, root) {
  // A transparent identity does not grant ownership over unrelated imported
  // interiors. Invalidate source boxes, their immediate members and paths to
  // fragments; keep every opaque sibling's saved internal layout intact.
  const contains = new Map()
  const has = node => {
    if (!contains.has(node.id)) contains.set(node.id, hasSourceFragments(graph, node))
    return contains.get(node.id)
  }
  return layoutNodes(graph, root, node => sourceCompositionLayout(graph, node) || has(node))
    .filter(node => node === root || sourceCompositionLayout(graph, node) ||
      sourceCompositionLayout(graph, graph.getNode(node.parentId)) || has(node))
}

export function currentSourceGeometry(graph, node) {
  // Reflow invalidates imported line/box caches without claiming an authored
  // size override. Export that current geometry, not the older raw FIG box.
  // Opaque imported siblings keep their original encoding and affine terms.
  const parent = graph.getNode(node.parentId)
  return node.source.format === 'fig' && (dependentLayoutChanged(graph, node) ||
    node.layoutPositioning !== 'ABSOLUTE' && dependentLayoutChanged(graph, parent) ||
    !node.figmaDerivedLayout && (sourceCompositionLayout(graph, node) || sourceCompositionLayout(graph, parent) ||
      hasSourceFragments(graph, node)))
}

function fragmentBounds(graph, node, yoga) {
  const boxes = []
  requireFragment(graph.getChildren(node.id).length === yoga.getChildCount(), 'layout and ownership children differ')
  for (const [index, child] of graph.getChildren(node.id).entries()) {
    if (!child.visible) continue
    const layout = yoga.getChild(index)
    const box = sourceFragment(graph, child) ? fragmentBounds(graph, child, layout) : {
      x: layout.getComputedLeft(), y: layout.getComputedTop(),
      width: layout.getComputedWidth(), height: layout.getComputedHeight(),
    }
    requireFragment(Object.values(box).every(Number.isFinite) && box.width >= 0 && box.height >= 0,
      'members require finite nonnegative layout geometry')
    if (!sourceFragment(graph, child) || fragmentLayoutChildren(graph, child).some(item => item.visible)) boxes.push(box)
  }
  if (!boxes.length) return { x: 0, y: 0, width: 0, height: 0 }
  const x = Math.min(...boxes.map(box => box.x)), y = Math.min(...boxes.map(box => box.y))
  return { x, y, width: Math.max(...boxes.map(box => box.x + box.width)) - x,
    height: Math.max(...boxes.map(box => box.y + box.height)) - y }
}

// Contents children use the nearest boxed ancestor's coordinates. Keep Yoga's
// structural child handles, adapting only the immediate coordinate frame while
// the existing native apply path handles each ordinary subtree.
function localYoga(yoga, x, y, contents) {
  return new Proxy(yoga, { get(target, key) {
    if (key === 'getComputedLeft') return () => target.getComputedLeft() - x
    if (key === 'getComputedTop') return () => target.getComputedTop() - y
    if (key === 'getChild' && contents) return index => {
      const child = target.getChild(index)
      return localYoga(child, x, y, child.getDisplay() === target.getDisplay())
    }
    const value = Reflect.get(target, key)
    return typeof value === 'function' ? value.bind(target) : value
  } })
}

export function applySourceFragment(graph, node, yoga, apply) {
  const bounds = fragmentBounds(graph, node, yoga)
  graph.updateNode(node.id, bounds)
  apply(localYoga(yoga, bounds.x, bounds.y, true))
}

export function correctFragmentLayout(source, replace) {
  const helper = JSON.stringify(fileURLToPath(import.meta.url))
  source = `import { sourceFragment, fragmentLayoutParent, fragmentLayoutChildren, hasSourceFragments, fragmentCacheNodes, validateFragmentContext } from ${helper};\n` + source
  source = replace(source, 'function computeAllLayouts(graph, scopeId) {',
    'function computeAllLayouts(graph, scopeId) {\n\tvalidateFragmentContext(graph, graph.getNode(scopeId ?? graph.rootId));\n\thasSourceFragments(graph, graph.getNode(scopeId ?? graph.rootId));')
  source = replace(source, 'function computeLayoutInternal(graph, frameId) {\n  const frame = graph.getNode(frameId);',
    `function computeLayoutInternal(graph, frameId) {
  const frame = graph.getNode(frameId);
  validateFragmentContext(graph, frame);
  if (frame && sourceFragment(graph, frame)) {
    const parent = fragmentLayoutParent(graph, frame);
    if (parent && !["CANVAS", "DOCUMENT"].includes(parent.type) && parent.layoutMode !== "NONE") computeLayoutInternal(graph, parent.id);
    return;
  }
  const fragments = hasSourceFragments(graph, frame);`)
  source = replace(source, '&& !liveGridLayout(graph, frame)) return computeLayoutMeasured(graph, frameId);',
    '&& !liveGridLayout(graph, frame) && !fragments) return computeLayoutMeasured(graph, frameId);')
  source = replace(source, 'const cached = layoutNodes(graph, frame, node => liveGridLayout(graph, frame) || sourceCompositionLayout(graph, node))',
    'const cached = (fragments ? fragmentCacheNodes(graph, frame) : layoutNodes(graph, frame, node => liveGridLayout(graph, frame) || sourceCompositionLayout(graph, node)))')
  source = replace(source, '|| liveGridLayout(graph, node))', '|| liveGridLayout(graph, node) || hasSourceFragments(graph, node))')
  // All sizing, grid placement and text measurement use the effective parent;
  // Yoga Contents keeps the logical tree and owns gaps, wrapping and tracks.
  source = replace(source, 'function configureChildAsAutoLayout(yogaChild, child, parent, graph, inheritedDirection) {',
    `function configureFragment(yogaNode, owner, parent, graph, direction, createLayoutNode) {
  yogaNode.setDisplay(Display.Contents);
  for (const child of graph.getChildren(owner.id)) {
    const yogaChild = createLayoutNode();
    if (!child.visible) yogaChild.setDisplay(Display.None);
    else if (sourceFragment(graph, child)) configureFragment(yogaChild, child, parent, graph, direction, createLayoutNode);
    else if (child.layoutMode !== "NONE") configureChildAsAutoLayout(yogaChild, child, parent, graph, direction, createLayoutNode);
    else configureChildAsLeaf(yogaChild, child, parent, graph);
    configureGridPosition(yogaChild, child, parent);
    yogaNode.insertChild(yogaChild, yogaNode.getChildCount());
  }
}

function configureChildAsAutoLayout(yogaChild, child, parent, graph, inheritedDirection) {`)
  // This transform precedes the existing measured-layout ownership correction.
  // Its added helper already receives that same caller-owned allocator.
  for (const [node, yoga, parent] of [['child', 'yogaChild', 'frame'], ['gc', 'yogaGC', 'child']]) {
    source = replace(source, `else if (!${node}.visible) ${yoga}.setDisplay(Display.None);`,
      `else if (!${node}.visible) ${yoga}.setDisplay(Display.None);\n` +
      `\t\telse if (sourceFragment(graph, ${node})) configureFragment(${yoga}, ${node}, ${parent}, graph, direction, createLayoutNode);`)
  }
  for (const name of ['derivedMainAxisFitsParent', 'derivedGrowingLeafFitsParent']) {
    const start = source.indexOf(`function ${name}(`), end = source.indexOf('\nfunction ', start + 1)
    const section = source.slice(start, end)
    source = replace(source, section, section.replace('graph.getChildren(parent.id)', 'fragmentLayoutChildren(graph, parent)'))
  }
  return source
}

export function correctFragmentApply(source, replace) {
  source = `import { sourceFragment, applySourceFragment } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, 'function applyFrameSize(graph, frame, yogaNode) {',
    'function applyFrameSize(graph, frame, yogaNode) {\n\tif (sourceFragment(graph, frame)) return;')
  source = replace(source, '\t\tupdateChildFromYoga(graph, child, yogaChild);',
    `\t\tif (sourceFragment(graph, child)) {
      if (child.visible) applySourceFragment(graph, child, yogaChild, local => applyYogaLayout(graph, child, local, computeLayout));
      continue;
    }
    updateChildFromYoga(graph, child, yogaChild);`)
  return source
}

export function correctFragmentExport(source, replace) {
  source = `import { sourceFragment, currentSourceGeometry } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, 'function exportNodeSize(node) {',
    'function exportNodeSize(node, graph) {\n  if (currentSourceGeometry(graph, node)) return { x: node.width, y: node.height };')
  source = replace(source, 'size: exportNodeSize(node),', 'size: exportNodeSize(node, context.graph),')
  source = replace(source, 'function exportNodeTransform(context, node) {',
    'function exportNodeTransform(context, node) {\n  if (currentSourceGeometry(context.graph, node)) return sourcePlacementTransform(context, node);')
  source = replace(source, "parent.layoutMode !== 'NONE' && target.layoutPositioning !== 'ABSOLUTE'",
    "(parent.layoutMode !== 'NONE' || sourceFragment(context.graph, parent)) && target.layoutPositioning !== 'ABSOLUTE'")
  return replace(source, 'function serializeLayoutProps(node, nc, graph) {',
    `function serializeLayoutProps(node, nc, graph) {
  if (sourceFragment(graph, node)) {
    // NONE has no stack container, but the native sizing fields still carry
    // HUG. Omitting them would make the ordinary FIG importer default to FIXED.
    nc.stackPrimarySizing = "RESIZE_TO_FIT";
    nc.stackCounterSizing = "RESIZE_TO_FIT";
    return;
  }`)
}
