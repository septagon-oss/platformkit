import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 2e-5, `${actual} != ${expected}`)
const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
const state = graph => structuredClone({ nodes: [...graph.getAllNodes()], index: [...graph.instanceIndex],
  variables: [...graph.variables], collections: [...graph.variableCollections] })

function path(graph, parentId, x = 1.25) {
  return graph.createNode('VECTOR', parentId, {
    name: 'Path', x, y: 2.5, width: 8.5, height: 10.25,
    fills: [{ type: 'SOLID', color: { r: 0.25, g: 0.5, b: 0.75, a: 1 }, opacity: 1, visible: true }],
    strokes: [{ color: { r: 0.125, g: 0.25, b: 0.5, a: 1 }, weight: 1.5,
      align: 'CENTER', opacity: 1, visible: true, cap: 'ROUND', join: 'ROUND', dashPattern: [] }],
    vectorNetwork: {
      vertices: [{ x: 0.25, y: 0.5 }, { x: 7.5, y: 1.25 }, { x: 2.75, y: 9.5 }],
      segments: [
        { start: 0, end: 1, tangentStart: { x: 2.125, y: -0.25 }, tangentEnd: { x: -1.75, y: 0.625 } },
        { start: 1, end: 2, tangentStart: { x: 0, y: 0 }, tangentEnd: { x: 0, y: 0 } },
        { start: 2, end: 0, tangentStart: { x: 0, y: 0 }, tangentEnd: { x: 0, y: 0 } },
      ],
      regions: [{ windingRule: 'NONZERO', loops: [[0, 1, 2]] }],
    },
  })
}

function fixture() {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', page.id, { name: 'Master', width: 24, height: 24, fills: [] })
  path(graph, master.id)
  path(graph, master.id, 12.25)
  const instances = [null, 20 / 24, 16 / 24].map(uniformScaleFactor =>
    graph.createInstance(master.id, page.id, { uniformScaleFactor }))
  return { graph, page, master, instances }
}

function sourceOf(graph, child, master) {
  const seen = new Set()
  while (child && !master.childIds.includes(child.id)) {
    assert.ok(!seen.has(child.id), 'acyclic source lineage')
    seen.add(child.id)
    child = graph.getNode(child.componentId)
  }
  assert.ok(child, 'child resolves to an actual master child')
  return child
}

function checkProjection(graph, instance, master) {
  const factor = instance.uniformScaleFactor ?? 1
  const children = graph.getChildren(instance.id)
  assert.equal(children.length, master.childIds.length)
  assert.deepEqual(children.map(child => sourceOf(graph, child, master).id), master.childIds)
  close(instance.width, master.width * factor)
  close(instance.height, master.height * factor)
  for (const child of children) {
    const source = sourceOf(graph, child, master)
    for (const key of ['x', 'y', 'width', 'height']) close(child[key], source[key] * factor)
    assert.equal(child.vectorNetwork.vertices.length, source.vectorNetwork.vertices.length)
    for (const [index, vertex] of child.vectorNetwork.vertices.entries()) {
      for (const key of ['x', 'y']) close(vertex[key], source.vectorNetwork.vertices[index][key] * factor)
    }
    assert.equal(child.vectorNetwork.segments.length, source.vectorNetwork.segments.length)
    for (const [index, segment] of child.vectorNetwork.segments.entries()) {
      const expected = source.vectorNetwork.segments[index]
      assert.equal(segment.start, expected.start)
      assert.equal(segment.end, expected.end)
      for (const tangent of ['tangentStart', 'tangentEnd']) for (const key of ['x', 'y']) {
        close(segment[tangent][key], expected[tangent][key] * factor)
      }
    }
    assert.deepEqual(child.vectorNetwork.regions, source.vectorNetwork.regions)
    assert.equal(child.strokes.length, source.strokes.length)
    child.strokes.forEach((stroke, index) => close(stroke.weight, source.strokes[index].weight * factor))
  }
}

function survivingChildren(graph, instances, master) {
  return instances.map(instance => new Map(graph.getChildren(instance.id).map(child =>
    [sourceOf(graph, child, master).id, child])))
}

function checkSurvivors(graph, instances, master, before) {
  instances.forEach((instance, index) => {
    checkProjection(graph, instance, master)
    for (const child of graph.getChildren(instance.id)) {
      const previous = before[index].get(sourceOf(graph, child, master).id)
      if (previous) assert.equal(child, previous, 'surviving child retains its ID and object identity')
    }
  })
}

test('adding and reordering native paths preserves linked geometry and surviving identities', () => {
  const { graph, master, instances } = fixture()
  const before = survivingChildren(graph, instances, master)
  const added = path(graph, master.id, 5.75)
  const sourceBefore = structuredClone(descendants(graph, master))
  graph.syncInstances(master.id)
  checkSurvivors(graph, instances, master, before)
  assert.deepEqual(descendants(graph, master), sourceBefore, 'sync does not mutate its source')
  const afterAddition = survivingChildren(graph, instances, master)
  graph.reorderChild(added.id, master.id, 0)
  graph.syncInstances(master.id)
  checkSurvivors(graph, instances, master, afterAddition)
  const settled = state(graph)
  graph.syncInstances(master.id)
  assert.deepEqual(state(graph), settled, 'repeated synchronization is noncumulative and idempotent')
})

test('removing a native source path removes stale clones without replacing survivors', () => {
  const { graph, master, instances } = fixture()
  const before = survivingChildren(graph, instances, master)
  const removedId = master.childIds[0]
  const removedClones = before.map(children => children.get(removedId))
  graph.deleteNode(removedId)
  graph.syncInstances(master.id)
  checkSurvivors(graph, instances, master, before)
  for (const child of removedClones) assert.equal(graph.getNode(child.id), undefined)
})

test('ordinary editor deletion updates linked paths through deferred notifications without manual sync', async () => {
  const { graph, page, master, instances } = fixture()
  const retained = graph.getChildren(instances[1].id)[1]
  const overrides = { [`${retained.id}:opacity`]: true }
  graph.updateNode(retained.id, { opacity: 0.4 })
  graph.updateNode(instances[1].id, { overrides })
  const other = graph.createNode('COMPONENT', page.id, { name: 'Master', width: 24, height: 24, fills: [] })
  path(graph, other.id)
  const unrelated = graph.createInstance(other.id, page.id, { uniformScaleFactor: 0.5 })
  const actions = createEditor({ graph })
  const unrelatedBefore = structuredClone([other, unrelated].flatMap(node => descendants(graph, node)))
  const survivors = survivingChildren(graph, instances, master)
  const removedId = master.childIds[0]
  const removedClones = survivors.map(children => children.get(removedId))
  try {
    graph.deleteNode(removedId)
    assert.equal(graph.getChildren(instances[0].id).length, 2, 'editor notification is deferred')
    await Promise.resolve()
    await Promise.resolve()
    checkSurvivors(graph, instances, master, survivors)
    for (const child of removedClones) assert.equal(graph.getNode(child.id), undefined)
    assert.equal(graph.getChildren(instances[1].id)[0], retained)
    assert.equal(retained.opacity, 0.4)
    assert.deepEqual(instances[1].overrides, overrides)
    assert.deepEqual([other, unrelated].flatMap(node => descendants(graph, node)), unrelatedBefore)
    const settled = state(graph)
    await Promise.resolve()
    assert.deepEqual(state(graph), settled, 'notification processing settles without further graph changes')
  } finally { actions.replaceGraph(new SceneGraph()) }
})

test('unscaled native instances receive fractional path, position and tangent edits', () => {
  const { graph, master, instances } = fixture()
  const source = graph.getChildren(master.id)[0]
  const vectorNetwork = structuredClone(source.vectorNetwork)
  vectorNetwork.vertices[1].x += 0.375
  vectorNetwork.segments[0].tangentEnd.y -= 0.25
  graph.updateNode(source.id, { x: 3.125, vectorNetwork })
  const before = survivingChildren(graph, instances, master)
  graph.syncInstances(master.id)
  checkSurvivors(graph, instances, master, before)
})

test('nonfinite vector data rejects atomically even when every instance is unscaled', () => {
  const { graph, master, instances } = fixture()
  for (const instance of instances.slice(1)) graph.deleteNode(instance.id)
  const source = graph.getNode(master.childIds[0]), vectorNetwork = structuredClone(source.vectorNetwork)
  vectorNetwork.vertices[0].x = NaN
  graph.updateNode(source.id, { vectorNetwork })
  const before = state(graph), events = []
  const stop = graph.onNodeEvents({ updated: () => events.push('updated'), deleted: () => events.push('deleted'),
    created: () => events.push('created') })
  try {
    assert.throws(() => graph.syncInstances(master.id), /native|vector|finite/i)
    assert.deepEqual(state(graph), before)
    assert.deepEqual(events, [])
  } finally { stop() }
})

test('scaled geometry respects explicit root width, child position and vector overrides', () => {
  const { graph, master, instances } = fixture()
  const instance = instances[1], child = graph.getChildren(instance.id)[0]
  const vectorNetwork = structuredClone(child.vectorNetwork)
  vectorNetwork.vertices[0].x = 3.625
  const overrides = { width: true, [`${child.id}:x`]: true, [`${child.id}:vectorNetwork`]: true }
  graph.updateNode(child.id, { x: 7.125, vectorNetwork })
  graph.updateNode(instance.id, { width: 37.25, overrides })
  const source = graph.getNode(master.childIds[0])
  graph.updateNode(source.id, { y: 4.75, height: 12.125, strokes: source.strokes.map(stroke => ({ ...stroke, weight: 2.25 })) })
  graph.syncInstances(master.id)
  assert.equal(instance.width, 37.25)
  assert.equal(child.x, 7.125)
  assert.deepEqual(child.vectorNetwork, vectorNetwork)
  assert.deepEqual(instance.overrides, overrides)
  close(child.y, 4.75 * instance.uniformScaleFactor)
  close(child.height, 12.125 * instance.uniformScaleFactor)
  close(child.strokes[0].weight, 2.25 * instance.uniformScaleFactor)
  for (const sibling of [instances[0], instances[2]]) checkProjection(graph, sibling, master)
})

test('literal paint overrides do not regain source color bindings while other slots inherit', () => {
  const { graph, page, master, instances } = fixture()
  const palette = graph.createCollection('Colors')
  const ink = graph.createVariable('Ink', 'COLOR', palette.id, { r: 0.2, g: 0.3, b: 0.4, a: 1 })
  const edge = graph.createVariable('Edge', 'COLOR', palette.id, { r: 0.6, g: 0.7, b: 0.8, a: 1 })
  const source = graph.getNode(master.childIds[0])
  graph.bindVariable(source.id, 'fills/0/color', ink.id)
  graph.bindVariable(source.id, 'strokes/0/color', ink.id)
  const instance = graph.createInstance(master.id, page.id, { uniformScaleFactor: 20 / 24 })
  const child = graph.getChildren(instance.id)[0]
  const fills = [{ type: 'SOLID', color: { r: 1, g: 0, b: 0, a: 1 }, opacity: 1, visible: true }]
  const boundVariables = Object.fromEntries(Object.entries(child.boundVariables).filter(([field]) => field !== 'fills/0/color'))
  graph.updateNode(child.id, { fills, boundVariables })
  graph.updateNode(instance.id, { overrides: { [`${child.id}:fills`]: true } })
  assert.equal(child.boundVariables['fills/0/color'], undefined)
  graph.bindVariable(source.id, 'strokes/0/color', edge.id)
  graph.syncInstances(master.id)
  assert.deepEqual(child.fills, fills)
  assert.equal(child.boundVariables['fills/0/color'], undefined)
  assert.equal(child.boundVariables['strokes/0/color'], edge.id)
  assert.equal(source.boundVariables['fills/0/color'], ink.id)
  for (const sibling of instances) assert.equal(graph.getChildren(sibling.id)[0].boundVariables['fills/0/color'], ink.id)
})

test('an unrelated invalid master neither blocks synchronization nor receives mutations', () => {
  const { graph, page, master, instances } = fixture()
  graph.createNode('TEXT', master.id, { text: 'unsupported scaled content' })
  const isolatedNodes = [master, ...instances].flatMap(node => descendants(graph, node))
  const isolatedIds = new Set(isolatedNodes.map(node => node.id))
  const before = structuredClone(isolatedNodes)
  const other = graph.createNode('COMPONENT', page.id, { name: 'Master', width: 24, height: 24, fills: [] })
  const source = path(graph, other.id)
  const related = [null, 0.5].map(uniformScaleFactor => graph.createInstance(other.id, page.id, { uniformScaleFactor }))
  graph.updateNode(source.id, { x: 4.125 })
  const events = []
  const stop = graph.onNodeEvents({ updated: id => events.push(id), deleted: id => events.push(id),
    created: node => events.push(node.id), reordered: id => events.push(id) })
  try {
    assert.doesNotThrow(() => graph.syncInstances(other.id))
    for (const instance of related) checkProjection(graph, instance, other)
    assert.deepEqual(isolatedNodes, before)
    assert.ok(events.every(id => !isolatedIds.has(id)), 'no unrelated node events')
  } finally { stop() }
})

test('structural synchronization preserves instance-local paint and opacity overrides', () => {
  const { graph, master, instances } = fixture()
  const edited = instances[1], child = graph.getChildren(edited.id)[0]
  const fills = [{ type: 'SOLID', color: { r: 0.8, g: 0.1, b: 0.2, a: 1 }, opacity: 1, visible: true }]
  const overrides = { [`${child.id}:fills`]: true, [`${child.id}:opacity`]: true }
  graph.updateNode(child.id, { fills, opacity: 0.4 })
  graph.updateNode(edited.id, { overrides })
  const source = graph.getChildren(master.id)[0]
  graph.updateNode(source.id, { opacity: 0.8 })
  path(graph, master.id, 4.75)
  const sourceBefore = structuredClone(descendants(graph, master))
  graph.syncInstances(master.id)
  assert.equal(graph.getChildren(edited.id)[0], child)
  assert.deepEqual(child.fills, fills)
  assert.equal(child.opacity, 0.4)
  assert.deepEqual(edited.overrides, overrides)
  assert.deepEqual(descendants(graph, master), sourceBefore)
  for (const instance of instances) checkProjection(graph, instance, master)
  for (const instance of [instances[0], instances[2]]) assert.equal(graph.getChildren(instance.id)[0].opacity, 0.8)
})

test('new nested instance clones remap descendant override keys to their own native IDs', () => {
  const { graph, page, master } = fixture()
  const wrapper = graph.createNode('COMPONENT', page.id, { width: 24, height: 24, fills: [] })
  const outer = graph.createInstance(wrapper.id, page.id)
  const nested = graph.createInstance(master.id, wrapper.id, { uniformScaleFactor: 20 / 24 })
  const sourcePath = graph.getChildren(nested.id)[0]
  const fills = [{ type: 'SOLID', color: { r: 0.8, g: 0.1, b: 0.2, a: 1 }, opacity: 1, visible: true }]
  graph.updateNode(sourcePath.id, { fills, opacity: 0.4 })
  graph.updateNode(nested.id, { overrides: { [`${sourcePath.id}:fills`]: true, [`${sourcePath.id}:opacity`]: true } })
  const sourceBefore = structuredClone(descendants(graph, nested))
  graph.syncInstances(wrapper.id)
  const cloned = graph.getChildren(outer.id)[0], clonedPath = graph.getChildren(cloned.id)[0]
  assert.notEqual(clonedPath.id, sourcePath.id)
  assert.deepEqual(cloned.overrides, { [`${clonedPath.id}:fills`]: true, [`${clonedPath.id}:opacity`]: true })
  assert.deepEqual(clonedPath.fills, fills)
  assert.equal(clonedPath.opacity, 0.4)
  assert.deepEqual(descendants(graph, nested), sourceBefore)
  checkProjection(graph, cloned, master)
  graph.syncInstances(wrapper.id)
  assert.equal(graph.getChildren(outer.id)[0], cloned)
  assert.equal(graph.getChildren(cloned.id)[0], clonedPath)
  assert.deepEqual(cloned.overrides, { [`${clonedPath.id}:fills`]: true, [`${clonedPath.id}:opacity`]: true })
  assert.deepEqual(clonedPath.fills, fills)
  assert.equal(clonedPath.opacity, 0.4)
})

test('removing the final native path preserves empty instance roots and later additions work', () => {
  const { graph, master, instances } = fixture()
  graph.deleteNode(master.childIds[0])
  graph.syncInstances(master.id)
  const removed = instances.map(instance => graph.getChildren(instance.id)[0].id)
  const roots = instances.map(instance => ({ node: instance, id: instance.id }))
  graph.deleteNode(master.childIds[0])
  graph.syncInstances(master.id)
  assert.deepEqual(master.childIds, [])
  for (const { node, id } of roots) {
    assert.equal(graph.getNode(id), node)
    assert.deepEqual(node.childIds, [])
    checkProjection(graph, node, master)
  }
  for (const id of removed) assert.equal(graph.getNode(id), undefined)
  path(graph, master.id, 7.125)
  graph.syncInstances(master.id)
  for (const { node, id } of roots) {
    assert.equal(graph.getNode(id), node)
    assert.equal(node.childIds.length, 1)
    checkProjection(graph, node, master)
  }
})

test('invalid affected source or lineage rejects without partial graph changes or mutation events', async t => {
  const cases = [
    ['nonfinite position', ({ graph, master }) => graph.updateNode(master.childIds[0], { x: NaN })],
    ['unsupported child', ({ graph, master }) => graph.createNode('TEXT', master.id, { text: 'not a path' })],
    ['duplicate lineage', ({ graph, master, instances }) =>
      graph.updateNode(instances[1].childIds[1], { componentId: master.childIds[0] })],
    ['unknown lineage', ({ graph, instances }) =>
      graph.updateNode(instances[1].childIds[0], { componentId: 'missing-source-child' })],
    ['nonfinite vertex', ({ graph, master }) => {
      const source = graph.getNode(master.childIds[0]), vectorNetwork = structuredClone(source.vectorNetwork)
      vectorNetwork.vertices[0].x = NaN
      graph.updateNode(source.id, { vectorNetwork })
    }],
  ]
  for (const [name, mutate] of cases) await t.test(name, () => {
    const fixtureValue = fixture(), { graph, master } = fixtureValue
    mutate(fixtureValue)
    const before = state(graph), events = []
    const stop = graph.onNodeEvents({ created: () => events.push('created'), updated: () => events.push('updated'),
      deleted: () => events.push('deleted'), reordered: () => events.push('reordered') })
    try {
      assert.throws(() => graph.syncInstances(master.id), /native|scale|lineage|vector|finite/i)
      assert.deepEqual(state(graph), before)
      assert.deepEqual(events, [])
    } finally { stop() }
  })
})

test('nested and direct native instances remain editable through structural changes and two FIG saves', async () => {
  let { graph, page, master, instances } = fixture()
  let wrapper = graph.createNode('COMPONENT', page.id, { name: 'Wrapper', width: 24, height: 24, fills: [] })
  graph.createInstance(master.id, wrapper.id, { uniformScaleFactor: 20 / 24 })
  let outer = graph.createInstance(wrapper.id, page.id)
  for (let cycle = 0; cycle < 3; cycle++) {
    const sourceInstances = [...instances, graph.getChildren(wrapper.id)[0], graph.getChildren(outer.id)[0]]
    const survivors = survivingChildren(graph, sourceInstances, master)
    if (cycle === 0) path(graph, master.id, 6.125)
    else if (cycle === 1) {
      graph.reorderChild(master.childIds.at(-1), master.id, 0)
      const source = graph.getNode(master.childIds[0]), vectorNetwork = structuredClone(source.vectorNetwork)
      vectorNetwork.vertices[1].y += 0.375
      graph.updateNode(source.id, { vectorNetwork })
    } else graph.deleteNode(master.childIds[1])
    const sourceBefore = structuredClone(descendants(graph, master))
    graph.syncInstances(master.id)
    checkSurvivors(graph, sourceInstances, master, survivors)
    assert.deepEqual(descendants(graph, master), sourceBefore)
    assert.deepEqual(instances.map(instance => instance.uniformScaleFactor), [null, Math.fround(20 / 24), Math.fround(16 / 24)])
    assert.equal([...graph.getAllNodes()].filter(node => node.type === 'COMPONENT').length, 2)
    if (cycle < 2) {
      const encoded = await exportFigFile(graph)
      graph = await parseFigFile(encoded.slice().buffer, { populate: 'all' })
      page = graph.getPages()[0]
      const roots = graph.getChildren(page.id)
      master = roots[0]
      assert.equal(master.type, 'COMPONENT')
      instances = roots.filter(node => node.type === 'INSTANCE' && node.componentId === master.id)
      assert.equal(instances.length, 3)
      wrapper = roots.find(node => node.type === 'COMPONENT' && node.id !== master.id)
      outer = roots.find(node => node.type === 'INSTANCE' && node.componentId === wrapper.id)
      for (const instance of [...instances, graph.getChildren(wrapper.id)[0], graph.getChildren(outer.id)[0]]) {
        checkProjection(graph, instance, master)
      }
    }
  }
})

test('deleting a nested source removes its flattened imported occurrence and descendants', () => {
  const { graph, page, master } = fixture()
  const wrapper = graph.createNode('COMPONENT', page.id, { width: 24, height: 24, fills: [] })
  const nested = graph.createInstance(master.id, wrapper.id)
  const outer = graph.createInstance(wrapper.id, page.id), clone = graph.getChildren(outer.id)[0]
  // SDK importer recloneChildren flattens this root but retains descendant source links.
  graph.updateNode(clone.id, { componentId: master.id })
  graph.syncInstances(wrapper.id)
  assert.equal(graph.getChildren(outer.id)[0], clone)
  const removedIds = descendants(graph, clone).map(node => node.id)
  graph.deleteNode(nested.id)
  assert.doesNotThrow(() => graph.syncInstances(wrapper.id))
  assert.deepEqual(outer.childIds, [])
  for (const id of removedIds) assert.ok(!graph.getNode(id), 'deleted source occurrence leaves no dangling clone')
  assert.equal(graph.getNode(outer.id), outer)
  assert.equal(graph.getNode(master.id), master)
})
