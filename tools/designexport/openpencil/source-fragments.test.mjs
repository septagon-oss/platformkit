import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeAllLayouts, computeLayout } from '@open-pencil/core/layout'

const source = (id, extra = {}) => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
  schema: 'platformkit.design-export.v1', sha256: 'a'.repeat(64), exampleId: id,
  componentId: 'fixture.fragment', scope: 'source-composition-observed-aliases',
  mode: 'light', props: {}, definitionPath: [id], bindingVersion: 1, textBindings: [], slotBindings: [],
  ...extra,
}) }]
const box = node => [node.x, node.y, node.width, node.height]

function fragment(graph, parent, name, extra = {}) {
  return graph.createNode('COMPONENT', parent.id, {
    name, width: 0, height: 0, layoutMode: 'NONE',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG', counterAxisAlign: 'MIN',
    fills: [], strokes: [], effects: [], clipsContent: false,
    pluginData: source(name, { cssFragment: { version: 1 } }), ...extra,
  })
}

function leaf(graph, parent, name, width, height, extra = {}) {
  return graph.createNode('RECTANGLE', parent.id, { name, width, height, ...extra })
}

function frame(graph, extra = {}) {
  return graph.createNode('FRAME', graph.getPages()[0].id, {
    x: 41, y: 29, width: 310, height: 0, layoutMode: 'VERTICAL',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED', counterAxisAlign: 'MIN',
    itemSpacing: 32, ...extra,
  })
}

function parentBox(graph, node, parent) {
  let x = node.x, y = node.y, current = graph.getNode(node.parentId)
  while (current && current !== parent) {
    x += current.x; y += current.y
    current = graph.getNode(current.parentId)
  }
  assert.equal(current, parent, 'the original native ownership path remains intact')
  return [x, y, node.width, node.height]
}

function geometry(graph) {
  return [...graph.getAllNodes()].map(node => ({ id: node.id, parentId: node.parentId,
    childIds: [...node.childIds], box: box(node), visible: node.visible, pluginData: structuredClone(node.pluginData) }))
}

test('source fragment children participate individually in a real parent column with gap32', () => {
  const graph = new SceneGraph(), root = frame(graph)
  const a = leaf(graph, root, 'A', 60, 20), group = fragment(graph, root, 'Pair')
  const b = leaf(graph, group, 'B', 70, 30), c = leaf(graph, group, 'C', 80, 40)
  const d = leaf(graph, root, 'D', 90, 10)
  const ownership = [...graph.getAllNodes()].map(node => [node.id, node.parentId, [...node.childIds], node.pluginData])
  computeLayout(graph, root.id)
  assert.deepEqual([a, b, c, d].map(node => parentBox(graph, node, root)), [
    [0, 0, 60, 20], [0, 52, 70, 30], [0, 114, 80, 40], [0, 186, 90, 10],
  ])
  assert.deepEqual(box(group), [0, 52, 80, 102], 'logical owner uses the children union, not a layout item')
  assert.deepEqual([box(b), box(c)], [[0, 0, 70, 30], [0, 62, 80, 40]], 'children become owner-local exactly once')
  assert.equal(root.height, 196)
  assert.deepEqual([...graph.getAllNodes()].map(node => [node.id, node.parentId, [...node.childIds], node.pluginData]), ownership)
  const before = geometry(graph)
  computeAllLayouts(graph, root.id)
  assert.deepEqual(geometry(graph), before, 'bottom-up layout does not turn the logical owner into another flex container')
})

test('source fragment union and local coordinates follow parent wrapping and repeated resizing', () => {
  const graph = new SceneGraph(), root = frame(graph, { width: 200, layoutMode: 'HORIZONTAL',
    layoutWrap: 'WRAP', primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG', itemSpacing: 13, counterAxisSpacing: 11 })
  const a = leaf(graph, root, 'A', 80, 20), group = fragment(graph, root, 'Wrapping pair')
  const b = leaf(graph, group, 'B', 90, 30), c = leaf(graph, group, 'C', 100, 40)
  const d = leaf(graph, root, 'D', 70, 25)
  for (const [width, expected, union, height] of [
    [200, [[0, 0, 80, 20], [93, 0, 90, 30], [0, 41, 100, 40], [113, 41, 70, 25]], [0, 0, 183, 81], 81],
    [310, [[0, 0, 80, 20], [93, 0, 90, 30], [196, 0, 100, 40], [0, 51, 70, 25]], [93, 0, 203, 40], 76],
    [200, [[0, 0, 80, 20], [93, 0, 90, 30], [0, 41, 100, 40], [113, 41, 70, 25]], [0, 0, 183, 81], 81],
  ]) {
    graph.updateNode(root.id, { width })
    computeLayout(graph, group.id)
    assert.deepEqual([a, b, c, d].map(node => parentBox(graph, node, root)), expected)
    assert.deepEqual(box(group), union)
    assert.equal(root.height, height)
    assert.deepEqual([root.x, root.y], [41, 29], 'page placement is not mixed into parent-local Yoga coordinates')
    const before = geometry(graph)
    for (let pass = 0; pass < 3; pass++) computeAllLayouts(graph)
    assert.deepEqual(geometry(graph), before, 'no accumulated remapping or bottom-up drift')
  }
})

test('nested and empty source owners add neither parent gaps nor artificial union extents', () => {
  const graph = new SceneGraph(), root = frame(graph)
  const a = leaf(graph, root, 'A', 60, 20), empty = fragment(graph, root, 'Empty before')
  const outer = fragment(graph, root, 'Outer'), b = leaf(graph, outer, 'B', 70, 30)
  const innerEmpty = fragment(graph, outer, 'Empty inside'), inner = fragment(graph, outer, 'Inner')
  const c = leaf(graph, inner, 'C', 80, 40), d = leaf(graph, root, 'D', 90, 10)
  computeAllLayouts(graph)
  assert.deepEqual([a, b, c, d].map(node => parentBox(graph, node, root)), [
    [0, 0, 60, 20], [0, 52, 70, 30], [0, 114, 80, 40], [0, 186, 90, 10],
  ])
  assert.deepEqual(box(outer), [0, 52, 80, 102])
  assert.deepEqual(box(inner), [0, 62, 80, 40])
  assert.deepEqual(box(c), [0, 0, 80, 40])
  for (const owner of [empty, innerEmpty]) {
    assert.deepEqual([owner.width, owner.height, owner.visible], [0, 0, true])
    assert.ok(box(owner).every(Number.isFinite), 'Yoga contents dimensions cannot leak NaN into native nodes')
  }
  const added = leaf(graph, empty, 'New output', 50, 12)
  computeLayout(graph, empty.id)
  assert.deepEqual([a, added, b, c, d].map(node => parentBox(graph, node, root)), [
    [0, 0, 60, 20], [0, 52, 50, 12], [0, 96, 70, 30], [0, 158, 80, 40], [0, 230, 90, 10],
  ])
  assert.deepEqual(box(empty), [0, 52, 50, 12])
  assert.equal(root.height, 240)
  graph.deleteNode(added.id)
  computeAllLayouts(graph)
  assert.deepEqual([empty.width, empty.height, empty.visible, root.height], [0, 0, true, 196])
  assert.deepEqual(box(outer), [0, 52, 80, 102], 'returning to zero output restores parent participation without a visibility toggle')
})

test('source fragment grid children retain individual track placement and spans after parent resize', () => {
  const graph = new SceneGraph(), root = frame(graph, { width: 320, height: 80, layoutMode: 'GRID', itemSpacing: 0,
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED',
    gridTemplateColumns: Array.from({ length: 3 }, () => ({ sizing: 'FR', value: 1 })),
    gridTemplateRows: [30, 40].map(value => ({ sizing: 'FIXED', value })), gridColumnGap: 10, gridRowGap: 10 })
  const place = (column, row, columnSpan = 1) => ({ layoutAlignSelf: 'STRETCH',
    gridPosition: { column, row, columnSpan, rowSpan: 1 } })
  const a = leaf(graph, root, 'A', 20, 20, place(1, 1)), group = fragment(graph, root, 'Grid pair')
  const b = leaf(graph, group, 'B spanning', 20, 30, place(2, 1, 2))
  const c = leaf(graph, group, 'C', 20, 40, place(1, 2)), d = leaf(graph, root, 'D', 20, 10, place(2, 2))
  for (const [width, track, span] of [[320, 100, 210], [212, 64, 138], [320, 100, 210]]) {
    graph.updateNode(root.id, { width })
    computeAllLayouts(graph, root.id)
    assert.deepEqual([a, b, c, d].map(node => parentBox(graph, node, root)), [
      [0, 0, track, 30], [track + 10, 0, span, 30], [0, 40, track, 40], [track + 10, 40, track, 40],
    ])
    assert.deepEqual(box(group), [0, 0, width, 80])
    assert.equal(root.height, 80)
    assert.deepEqual(b.gridPosition, place(2, 1, 2).gridPosition)
    assert.deepEqual(group.childIds, [b.id, c.id], 'grid placement must not reparent source-owned children')
  }
})

test('automatic grid placement counts fragment members and spans without counting empty owners', () => {
  const graph = new SceneGraph(), root = frame(graph, { width: 320, height: 77, layoutMode: 'GRID',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED',
    gridTemplateColumns: [100, 100, 100].map(value => ({ sizing: 'FIXED', value })),
    gridTemplateRows: [30, 40].map(value => ({ sizing: 'FIXED', value })), gridColumnGap: 10, gridRowGap: 7 })
  const place = columnSpan => ({ layoutAlignSelf: 'STRETCH', gridPosition: { column: 0, row: 0, columnSpan, rowSpan: 1 } })
  const a = leaf(graph, root, 'A spanning', 20, 20, place(2)), empty = fragment(graph, root, 'Empty cell owner')
  const group = fragment(graph, root, 'Automatic pair'), b = leaf(graph, group, 'B', 20, 30, place(1))
  const c = leaf(graph, group, 'C spanning', 20, 40, place(2)), d = leaf(graph, root, 'D', 20, 10, place(1))
  computeAllLayouts(graph, root.id)
  assert.deepEqual([a, b, c, d].map(node => parentBox(graph, node, root)), [
    [0, 0, 210, 30], [220, 0, 100, 30], [0, 37, 210, 40], [220, 37, 100, 40],
  ])
  assert.deepEqual(box(group), [0, 0, 320, 77])
  assert.deepEqual([empty.width, empty.height, empty.visible], [0, 0, true])
})

test('source fragment children inherit the effective row parent for native grow and align-self', () => {
  const graph = new SceneGraph(), root = frame(graph, { width: 310, height: 80, layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', itemSpacing: 10 })
  const a = leaf(graph, root, 'A', 40, 20, { layoutGrow: 1 }), group = fragment(graph, root, 'Growing pair')
  const b = leaf(graph, group, 'B', 50, 30, { layoutGrow: 2, layoutAlignSelf: 'MAX' })
  const c = leaf(graph, group, 'C', 30, 40, { layoutGrow: 1, layoutAlignSelf: 'CENTER' })
  const d = leaf(graph, root, 'D', 20, 10)
  computeAllLayouts(graph, root.id)
  assert.deepEqual([a, b, c, d].map(node => parentBox(graph, node, root)), [
    [0, 0, 65, 20], [75, 50, 130, 30], [215, 20, 65, 40], [290, 0, 20, 10],
  ])
  assert.deepEqual(box(group), [75, 20, 205, 60])
  assert.deepEqual([box(b), box(c)], [[0, 30, 130, 30], [140, 0, 65, 40]])
  assert.deepEqual([b.layoutGrow, b.layoutAlignSelf, c.layoutGrow, c.layoutAlignSelf], [2, 'MAX', 1, 'CENTER'])
})

test('source flex shrink uses the boxed parent across a transparent ownership boundary', () => {
  const graph = new SceneGraph(), root = frame(graph, { width: 150, height: 80, layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', itemSpacing: 10, pluginData: source('Shrink parent') })
  const item = (parent, name, width, height) => {
    const node = graph.createNode('FRAME', parent.id, { name, width, height, layoutMode: 'HORIZONTAL',
      primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED', counterAxisAlign: 'MIN',
      pluginData: source(name, { cssFlex: { version: 1, shrink: 1, autoMinimum: false } }) })
    leaf(graph, node, `${name} content`, width, height)
    return node
  }
  const a = item(root, 'A', 80, 20), group = fragment(graph, root, 'Shrinking pair')
  const b = item(group, 'B', 90, 30), c = item(group, 'C', 100, 40), d = item(root, 'D', 70, 25)
  computeLayout(graph, root.id)
  const expected = [[0, 0, 480 / 17, 20], [650 / 17, 0, 540 / 17, 30],
    [80, 0, 600 / 17, 40], [2130 / 17, 0, 420 / 17, 25]]
  for (const [index, node] of [a, b, c, d].entries()) {
    for (const [axis, actual] of parentBox(graph, node, root).entries()) {
      assert.ok(Math.abs(actual - expected[index][axis]) < .0001, `${node.name} axis ${axis} retains CSS-scaled shrink`)
    }
  }
  assert.ok(Math.abs(group.x - 650 / 17) < .0001)
  assert.ok(Math.abs(group.width - 1310 / 17) < .0001)
  assert.equal(group.height, 40)
})

test('hidden fragment children preserve native state without creating an empty parent layout item', () => {
  const graph = new SceneGraph(), root = frame(graph)
  const a = leaf(graph, root, 'A', 60, 20), group = fragment(graph, root, 'Hidden pair')
  const b = leaf(graph, group, 'B', 70, 30, { visible: false })
  const c = leaf(graph, group, 'C', 80, 40, { visible: false }), d = leaf(graph, root, 'D', 90, 10)
  computeAllLayouts(graph, root.id)
  assert.deepEqual([a.y, d.y, root.height], [0, 52, 62])
  assert.deepEqual([group.width, group.height, group.visible], [0, 0, true])
  assert.deepEqual([b.visible, c.visible], [false, false])
  graph.updateNode(b.id, { visible: true })
  computeLayout(graph, group.id)
  assert.deepEqual([parentBox(graph, b, root), box(group)], [[0, 52, 70, 30], [0, 52, 70, 30]])
  assert.deepEqual([d.y, root.height, c.visible], [114, 124, false])
})

test('boxed descendants keep their own flex and grid coordinate frames inside a source fragment', () => {
  const graph = new SceneGraph(), root = frame(graph)
  leaf(graph, root, 'Before', 60, 20)
  const group = fragment(graph, root, 'Formatting boxes')
  const row = graph.createNode('FRAME', group.id, { layoutMode: 'HORIZONTAL', itemSpacing: 7,
    primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG', counterAxisAlign: 'MIN' })
  const a = leaf(graph, row, 'Row A', 20, 10), b = leaf(graph, row, 'Row B', 30, 20)
  const grid = graph.createNode('FRAME', group.id, { width: 80, height: 40, layoutMode: 'GRID',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED',
    gridTemplateColumns: [40, 40].map(value => ({ sizing: 'FIXED', value })),
    gridTemplateRows: [{ sizing: 'FIXED', value: 40 }] })
  const c = leaf(graph, grid, 'Grid C', 20, 10, { layoutAlignSelf: 'STRETCH' })
  const d = leaf(graph, grid, 'Grid D', 20, 10, { layoutAlignSelf: 'STRETCH' })
  const after = leaf(graph, root, 'After', 90, 10)
  computeAllLayouts(graph, root.id)
  assert.deepEqual([parentBox(graph, row, root), parentBox(graph, grid, root), box(after)], [
    [0, 52, 57, 20], [0, 104, 80, 40], [0, 176, 90, 10],
  ])
  assert.deepEqual(box(group), [0, 52, 80, 92])
  assert.deepEqual([a, b, c, d].map(box), [[0, 0, 20, 10], [27, 0, 30, 20], [0, 0, 40, 40], [40, 0, 40, 40]])
  assert.equal(root.height, 186)
})

test('placed source fragment instances resolve their own parent context without resizing the master or sibling', () => {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const master = fragment(graph, page, 'Reusable pair')
  leaf(graph, master, 'B', 90, 30); leaf(graph, master, 'C', 100, 40)
  const row = frame(graph, { width: 200, layoutMode: 'HORIZONTAL', layoutWrap: 'WRAP',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG', itemSpacing: 13, counterAxisSpacing: 11 })
  const column = frame(graph, { x: 700 })
  const instances = []
  for (const parent of [row, column]) {
    const a = leaf(graph, parent, 'A', 80, 20)
    const instance = graph.createInstance(master.id, parent.id, { pluginData: [{
      pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({ path: [parent.id] }),
    }] })
    const d = leaf(graph, parent, 'D', 70, 25)
    instances.push({ parent, instance, leaves: [a, ...graph.getChildren(instance.id), d] })
  }
  const definition = structuredClone([master, ...graph.getChildren(master.id)])
  computeAllLayouts(graph)
  assert.deepEqual(instances[0].leaves.map(node => parentBox(graph, node, row)), [
    [0, 0, 80, 20], [93, 0, 90, 30], [0, 41, 100, 40], [113, 41, 70, 25],
  ])
  assert.deepEqual(instances[1].leaves.map(node => parentBox(graph, node, column)), [
    [0, 0, 80, 20], [0, 52, 90, 30], [0, 114, 100, 40], [0, 186, 70, 25],
  ])
  assert.deepEqual([master, ...graph.getChildren(master.id)], definition, 'definition on a canvas has no invented layout context')
  const untouched = structuredClone([column, instances[1].instance, ...instances[1].leaves])
  graph.updateNode(row.id, { width: 310 })
  computeLayout(graph, instances[0].instance.id)
  assert.deepEqual(box(instances[0].instance), [93, 0, 203, 40])
  assert.deepEqual([column, instances[1].instance, ...instances[1].leaves], untouched)
  assert.deepEqual([master, ...graph.getChildren(master.id)], definition)
  for (const { instance } of instances) {
    assert.equal(instance.componentId, master.id)
    assert.deepEqual(JSON.parse(instance.pluginData[0].value), { path: [instance.parentId] })
    assert.deepEqual(instance.overrides, {}, 'derived fragment bounds do not become deliberate native overrides')
  }
})

test('ordinary native components with no fragment witness retain their own layout boundary', () => {
  for (const pluginData of [[], source('Ordinary'), source('Ordinary', { unrelated: { version: 1 } })]) {
    const graph = new SceneGraph(), root = frame(graph)
    const a = leaf(graph, root, 'A', 60, 20)
    const owner = fragment(graph, root, 'Ordinary', { layoutMode: 'VERTICAL', pluginData })
    const b = leaf(graph, owner, 'B', 70, 30), c = leaf(graph, owner, 'C', 80, 40)
    const d = leaf(graph, root, 'D', 90, 10)
    computeAllLayouts(graph)
    assert.deepEqual([a, b, c, d].map(node => parentBox(graph, node, root)), [
      [0, 0, 60, 20], [0, 52, 70, 30], [0, 82, 80, 40], [0, 154, 90, 10],
    ])
    assert.equal(root.height, 164)
  }
})

test('an empty source fragment does not invalidate an unrelated imported sibling subtree', () => {
  for (const addEmptyFragment of [false, true]) {
    const graph = new SceneGraph(), root = frame(graph)
    const opaque = graph.createNode('FRAME', root.id, { name: 'Unrelated imported frame', width: 150, height: 60,
      layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
      figmaDerivedLayout: { x: 11, y: 13, width: 150, height: 60 } })
    opaque.source = { ...opaque.source, format: 'fig', editedFields: [] }
    const child = leaf(graph, opaque, 'Imported child', 20, 10, { x: 5, y: 7,
      figmaDerivedLayout: { x: 5, y: 7, width: 20, height: 10 } })
    child.source = { ...child.source, format: 'fig', editedFields: [] }
    if (addEmptyFragment) fragment(graph, root, 'Empty sibling')
    const caches = structuredClone([opaque.figmaDerivedLayout, child.figmaDerivedLayout])
    computeLayout(graph, root.id)
    assert.deepEqual([box(opaque), box(child), root.height], [[11, 13, 150, 60], [5, 7, 20, 10], 60])
    assert.deepEqual([opaque.figmaDerivedLayout, child.figmaDerivedLayout], caches,
      'fragment support does not acquire ownership of opaque sibling internals')
  }
})

test('unsupported source fragment boxes and metadata refuse before changing native geometry', () => {
  const paint = { type: 'SOLID', color: { r: 1, g: 0, b: 0, a: 1 }, opacity: 1, visible: true }
  const cases = [
    ['padding', { paddingLeft: 1 }], ['fill', { fills: [paint] }], ['stroke', { strokes: [{ ...paint, weight: 1 }] }],
    ['effect', { effects: [{ type: 'DROP_SHADOW', visible: true, color: paint.color, offset: { x: 1, y: 1 }, radius: 1 }] }],
    ['rotation', { rotation: 15 }], ['flip X', { flipX: true }], ['flip Y', { flipY: true }],
    ['opacity', { opacity: .5 }], ['clipping', { clipsContent: true }], ['absolute owner', { layoutPositioning: 'ABSOLUTE' }],
    ['layout box', { layoutMode: 'HORIZONTAL' }], ['primary sizing', { primaryAxisSizing: 'FIXED' }],
    ['counter sizing', { counterAxisSizing: 'FIXED' }], ['item grow', { layoutGrow: 1 }],
    ['item alignment', { layoutAlignSelf: 'STRETCH' }],
    ['grid placement', { gridPosition: { column: 1, row: 1, columnSpan: 1, rowSpan: 1 } }],
    ...['minWidth', 'maxWidth', 'minHeight', 'maxHeight'].map(field => [field, { [field]: 10 }]),
    ...['itemSpacing', 'counterAxisSpacing', 'gridColumnGap', 'gridRowGap'].map(field => [field, { [field]: 1 }]),
    ...['gridTemplateColumns', 'gridTemplateRows'].map(field => [field, { [field]: [{ sizing: 'FIXED', value: 20 }] }]),
    ['mask', { isMask: true }], ['blend', { blendMode: 'MULTIPLY' }],
    ...[{ version: 2 }, { version: '1' }, null, { version: 1, inferred: true }].map(cssFragment =>
      [`metadata ${JSON.stringify(cssFragment)}`, { pluginData: source('Unsupported', { cssFragment }) }]),
  ]
  for (const [reason, changes] of cases) for (const layout of [computeLayout, computeAllLayouts]) {
    const graph = new SceneGraph(), root = frame(graph)
    const beforeOwner = graph.createNode('FRAME', root.id, { width: 60, height: 9, x: 77, y: 88,
      layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED' })
    leaf(graph, beforeOwner, 'Before', 60, 20)
    const owner = fragment(graph, root, 'Unsupported', changes)
    leaf(graph, owner, 'Child', 70, 30)
    leaf(graph, root, 'After', 90, 10)
    const before = structuredClone([...graph.getAllNodes()])
    assert.throws(() => layout(graph, root.id), /fragment/i, `${reason}/${layout.name}`)
    assert.deepEqual([...graph.getAllNodes()], before, `${reason} refusal is atomic`)
  }
})

test('absolute fragment members refuse until their effective containing block is represented', () => {
  const graph = new SceneGraph(), root = frame(graph)
  leaf(graph, root, 'Before', 60, 20)
  const group = fragment(graph, root, 'Positioned pair'), nested = fragment(graph, group, 'Nested owner')
  leaf(graph, nested, 'Absolute member', 70, 30, { x: 7, y: 9, layoutPositioning: 'ABSOLUTE' })
  const before = structuredClone([...graph.getAllNodes()])
  assert.throws(() => computeLayout(graph, root.id), /fragment.*(absolute|containing)/i)
  assert.deepEqual([...graph.getAllNodes()], before)
})

test('logical source fragments refuse explicit direction instead of inventing a CSS owner', () => {
  for (const layoutDirection of ['LTR', 'RTL']) for (const layout of [computeLayout, computeAllLayouts]) for (const descendant of [false, true]) {
    const graph = new SceneGraph(), root = frame(graph, { layoutDirection: 'LTR' })
    const owner = fragment(graph, root, 'Directional invocation', { layoutDirection })
    const row = graph.createNode('FRAME', owner.id, { layoutMode: 'HORIZONTAL',
      primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG' })
    leaf(graph, row, 'First', 20, 10); leaf(graph, row, 'Second', 30, 10)
    const before = structuredClone([...graph.getAllNodes()])
    assert.throws(() => layout(graph, descendant ? row.id : root.id), /fragment/i,
      `${layoutDirection}/${layout.name}/${descendant ? 'boxed descendant' : 'root'}`)
    assert.deepEqual([...graph.getAllNodes()], before, 'unsupported inherited direction is rejected before any layout')
  }
})

test('duplicate or conflicting canonical fragment records cannot fall back to ordinary native layout', () => {
  for (const conflicting of [false, true]) for (const layout of [computeLayout, computeAllLayouts]) {
    const graph = new SceneGraph(), root = frame(graph)
    const master = fragment(graph, graph.getPages()[0], 'Ambiguous master')
    leaf(graph, master, 'First', 20, 10); leaf(graph, master, 'Second', 30, 10)
    const placed = graph.createInstance(master.id, root.id)
    const duplicate = conflicting ? source('Ambiguous master')[0] : structuredClone(master.pluginData[0])
    graph.updateNode(master.id, { pluginData: [...master.pluginData, duplicate] })
    const before = structuredClone([...graph.getAllNodes()])
    assert.throws(() => layout(graph, root.id), /fragment/i, `${conflicting}/${layout.name}`)
    assert.deepEqual([...graph.getAllNodes()], before)
    assert.equal(placed.componentId, master.id, 'rejection does not repair source correspondence')
  }
})

test('placed fragment instances refuse duplicate local source identity records', () => {
  for (const identity of [{ path: ['Root'] }, { localId: 'nested', slot: 'children' }]) {
    for (const layout of [computeLayout, computeAllLayouts]) {
      const graph = new SceneGraph(), root = frame(graph)
      const master = fragment(graph, graph.getPages()[0], 'Exact master')
      leaf(graph, master, 'First', 20, 10); leaf(graph, master, 'Second', 30, 10)
      const local = { pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify(identity) }
      graph.createInstance(master.id, root.id, { pluginData: [local, structuredClone(local)] })
      const before = structuredClone([...graph.getAllNodes()])
      assert.throws(() => layout(graph, root.id), /fragment/i, `${JSON.stringify(identity)}/${layout.name}`)
      assert.deepEqual([...graph.getAllNodes()], before, 'canonical metadata does not conceal ambiguous placed ownership')
    }
  }
})

test('broken explicit fragment lineage refuses instead of producing an ordinary zero-sized owner', () => {
  for (const cycle of [false, true]) for (const layout of [computeLayout, computeAllLayouts]) {
    const graph = new SceneGraph(), root = frame(graph)
    const owner = graph.createNode('INSTANCE', root.id, { width: 0, height: 0, layoutMode: 'NONE',
      primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG', pluginData: source('Broken', { cssFragment: { version: 1 } }) })
    leaf(graph, owner, 'First', 20, 10); leaf(graph, owner, 'Second', 30, 10)
    owner.componentId = cycle ? owner.id : 'missing-master'
    const before = structuredClone([...graph.getAllNodes()])
    assert.throws(() => layout(graph, root.id), /fragment/i, `${cycle}/${layout.name}`)
    assert.deepEqual([...graph.getAllNodes()], before)
  }
})

test('boxed replacements migrate inherited placement but refuse incompatible authored fragment fields atomically', async t => {
  const paint = { type: 'SOLID', color: { r: 1, g: 0, b: 0, a: 1 }, opacity: 1, visible: true }
  const cases = [
    ['inherited fill', {}, false], ['inherited grow and stretch', { layoutGrow: 1, layoutAlignSelf: 'STRETCH' }, false],
    ...Object.entries({ primaryAxisSizing: 'FILL', counterAxisSizing: 'FIXED', layoutGrow: 1,
      layoutAlignSelf: 'STRETCH', fills: [paint], rotation: 15 }).map(([field, value]) => [field, { [field]: value }, true]),
  ]
  for (const [name, changes, authored] of cases) await t.test(name, () => {
    const graph = new SceneGraph(), root = frame(graph), page = graph.getPages()[0]
    const original = graph.createNode('COMPONENT', page.id, { name: 'Original box', width: 40, height: 10,
      layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG' })
    leaf(graph, original, 'Original leaf', 40, 10)
    const replacement = fragment(graph, page, 'Replacement pair')
    leaf(graph, replacement, 'First', 40, 10)
    leaf(graph, replacement, 'Second', 70, 20)
    const target = graph.createInstance(original.id, root.id, { primaryAxisSizing: 'FILL', ...changes,
      overrides: authored ? Object.fromEntries(Object.keys(changes).map(field => [field, true])) : {} })
    const before = structuredClone([...graph.getAllNodes()]), index = structuredClone(graph.instanceIndex), events = []
    const stop = graph.onNodeEvents({ created: () => events.push('create'), updated: () => events.push('update'),
      deleted: () => events.push('delete'), reordered: () => events.push('reorder') })
    try {
      if (authored) {
        assert.throws(() => graph.swapInstanceComponent(target.id, replacement.id), /fragment/i)
        assert.deepEqual([...graph.getAllNodes()], before, 'refusal preserves the complete native graph')
        assert.deepEqual(graph.instanceIndex, index, 'refusal preserves component reachability')
        assert.deepEqual(events, [], 'refusal precedes every native mutation event')
      } else {
        graph.swapInstanceComponent(target.id, replacement.id)
        assert.equal(graph.getNode(target.id), target, 'destination identity stays stable')
        assert.deepEqual([target.layoutMode, target.primaryAxisSizing, target.counterAxisSizing,
          target.layoutGrow, target.layoutAlignSelf], ['NONE', 'HUG', 'HUG', 0, 'AUTO'])
        computeLayout(graph, root.id)
        assert.deepEqual(graph.getChildren(target.id).map(node => parentBox(graph, node, root)),
          [[0, 0, 40, 10], [0, 42, 70, 20]], 'members keep their own dimensions and participate individually')
        assert.deepEqual(box(target), [0, 0, 70, 62], 'owner geometry is only the member selection union')
      }
    } finally { stop() }
  })
})
