import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph, UndoManager } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import { convertFigmaTransformProps } from '@open-pencil/fig/node-change'
import { importNodeChanges, populateAllLazyFigImportRoots } from '@open-pencil/core/kiwi'
import { ancestryOverrides, ownsRotationOverride } from './exporter-correction.mjs'
import { computeAllLayouts, computeLayout } from '@open-pencil/core/layout'

const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const subtree = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => subtree(graph, child))]
const reopen = async graph => parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
const canonical = (graph, node) => node.componentId ? canonical(graph, graph.getNode(node.componentId)) : node
const guidKey = guid => `${guid.sessionID}:${guid.localID}`

function fixture(depth) {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  for (const [name, width, r] of [['Original', 40, 0.25], ['Replacement', 60, 0.75]]) {
    const master = graph.createNode('COMPONENT', page.id, { name, width, height: 20 })
    graph.createNode('RECTANGLE', master.id, { name: 'Paint', width, height: 20,
      fills: [{ type: 'SOLID', color: { r, g: 0.5, b: 0.25, a: 1 }, opacity: 1, visible: true }] })
  }
  const owner = graph.createNode('COMPONENT', page.id, { name: 'Property owner', width: 120, height: 24,
    componentPropertyDefinitions: [{ id: '91:1', name: 'Action', type: 'INSTANCE_SWAP', defaultValue: named(graph, 'Original').id }] })
  graph.createInstance(named(graph, 'Original').id, owner.id, { name: 'Action',
    componentPropertyReferences: [{ propertyId: '91:1', field: 'INSTANCE_SWAP' }] })
  graph.createInstance(named(graph, 'Original').id, owner.id, { name: 'Guard', x: 70 })
  let outer = owner
  for (let level = 1; level <= depth; level++) {
    const wrapper = graph.createNode('COMPONENT', page.id, { name: `Wrapper ${level}`, width: 120, height: 24 })
    graph.createInstance(outer.id, wrapper.id, { name: `Nested ${level}` })
    outer = wrapper
  }
  graph.createInstance(outer.id, page.id, { name: 'Edited', x: 200, y: 100 })
  graph.createInstance(outer.id, page.id, { name: 'Untouched', x: 400, y: 100 })
  return graph
}

function propertyOwner(graph, depth) {
  let owner = named(graph, 'Edited')
  for (let level = 0; level < depth; level++) owner = graph.getChildren(owner.id)[0]
  return owner
}

function checkContent(graph, depth, expected) {
  const owner = propertyOwner(graph, depth), [action, guard] = graph.getChildren(owner.id)
  assert.equal(canonical(graph, owner).name, 'Property owner')
  assert.equal(canonical(graph, action).name, expected)
  assert.equal(canonical(graph, guard).name, 'Original')
  assert.equal(action.width, expected === 'Original' ? 40 : 60)
  assert.equal(action.height, 20)
  assert.equal(graph.getChildren(action.id)[0].fills[0].color.r, expected === 'Original' ? 0.25 : 0.75)
  assert.deepEqual([guard.x, guard.width], [70, 40])
  assert.deepEqual([named(graph, 'Edited').x, named(graph, 'Edited').y], [200, 100])
}

function checkFile(bytes, depth) {
  const { nodeChanges: nodes } = parseFigBuffer(bytes.slice().buffer)
  const master = name => nodes.find(node => node.type === 'SYMBOL' && node.name === name)
  const childOf = parent => nodes.filter(node => node.parentIndex?.guid && guidKey(node.parentIndex.guid) === guidKey(parent.guid))
  const path = []
  for (let level = depth; level > 0; level--) path.push(childOf(master(`Wrapper ${level}`))[0].guid)
  const action = childOf(master('Property owner')).find(node => node.componentPropRefs?.some(ref => ref.defID.localID === 1))
  path.push(action.guid)
  const changes = nodes.find(node => node.name === 'Edited').symbolData.symbolOverrides
  const swaps = changes.filter(override => override.overriddenSymbolID)
  assert.equal(swaps.length, 1, 'one replacement, never the adjacent occurrence')
  assert.deepEqual(swaps[0].guidPath.guids, path, 'file path belongs to the original nested slot')
  assert.deepEqual(swaps[0].overriddenSymbolID, master('Replacement').guid)
}

for (const depth of [0, 1, 2]) for (const imported of [false, true]) for (const property of [false, true]) {
  test(`nested replacement retains slot identity and two saves: depth=${depth}, imported=${imported}, property=${property}`, async () => {
    let graph = fixture(depth)
    if (imported) graph = await reopen(graph)
    const owner = propertyOwner(graph, depth), target = graph.getChildren(owner.id)[0]
    const originalLink = target.componentId
    const untouched = [...graph.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Untouched')
    const before = structuredClone(untouched.map(node => subtree(graph, node)))
    const editor = createEditor({ graph })
    try {
      if (property) {
        editor.setInstanceComponentProperty(owner.id, '91:1', named(graph, 'Replacement').id)
        await Promise.resolve()
        checkContent(graph, depth, 'Replacement')
        editor.undoAction()
        await Promise.resolve()
        checkContent(graph, depth, 'Original')
        assert.equal(target.componentId, originalLink, 'history restores the occurrence link')
        assert.deepEqual(owner.componentPropertyAssignments, {})
        assert.equal(editor.undo.canRedo, true)
        editor.redoAction()
      } else graph.swapInstanceComponent(target.id, named(graph, 'Replacement').id)
      await Promise.resolve()
      checkContent(graph, depth, 'Replacement')
      assert.equal(graph.getNode(target.id), target)
      assert.deepEqual(untouched.map(node => subtree(graph, node)), before, 'masters and other placements remain byte-for-byte intact')
      for (let cycle = 0; cycle < 2; cycle++) {
        const bytes = await exportFigFile(graph)
        checkFile(bytes, depth)
        graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
        checkContent(graph, depth, 'Replacement')
      }
      const reopenedOwner = propertyOwner(graph, depth), reopenedEditor = createEditor({ graph })
      try {
        reopenedEditor.setInstanceComponentProperty(reopenedOwner.id, '91:1', named(graph, 'Original').id)
        await Promise.resolve()
        checkContent(graph, depth, 'Original')
        reopenedEditor.undoAction()
        await Promise.resolve()
        checkContent(graph, depth, 'Replacement')
      } finally { reopenedEditor.replaceGraph(new SceneGraph()) }
    } finally { editor.replaceGraph(new SceneGraph()) }
  })
}

test('refused history operations retain their entry and can be retried in order', () => {
  const undo = new UndoManager(), calls = []
  let refuse = true
  undo.push({ label: 'First', forward: () => calls.push('first forward'), inverse: () => calls.push('first inverse') })
  undo.push({ label: 'Second', forward: () => {
    if (refuse) throw new Error('Refused forward')
    calls.push('second forward')
  }, inverse: () => {
    if (refuse) throw new Error('Refused inverse')
    calls.push('second inverse')
  } })
  assert.throws(() => undo.undo(), /Refused inverse/)
  assert.equal(undo.canUndo, true)
  assert.equal(undo.canRedo, false)
  assert.deepEqual(calls, [])
  refuse = false
  assert.equal(undo.undo(), 'Second')
  assert.equal(undo.undo(), 'First')
  assert.equal(undo.redo(), 'First')
  refuse = true
  assert.throws(() => undo.redo(), /Refused forward/)
  assert.equal(undo.canUndo, true)
  assert.equal(undo.canRedo, true)
  refuse = false
  assert.equal(undo.redo(), 'Second')
  assert.deepEqual(calls, ['second inverse', 'first inverse', 'first forward', 'second forward'])
})

for (const imported of [false, true]) test(`containing master updates preserve nested replacements and live replacement updates: imported=${imported}`, async () => {
  let graph = fixture(2), owner = propertyOwner(graph, 2)
  graph.swapInstanceComponent(graph.getChildren(owner.id)[0].id, named(graph, 'Replacement').id)
  if (imported) graph = await reopen(graph)
  owner = propertyOwner(graph, 2)
  const target = graph.getChildren(owner.id)[0], before = structuredClone(subtree(graph, target))
  for (const name of ['Property owner', 'Wrapper 1', 'Wrapper 2']) {
    graph.updateNode(named(graph, name).id, { cornerRadius: 4 })
    graph.syncInstances(named(graph, name).id)
    assert.deepEqual(subtree(graph, target), before, `${name} must not restore the replaced source's inputs`)
  }
  const replacement = named(graph, 'Replacement'), rectangle = graph.getChildren(replacement.id)[0]
  graph.updateNode(rectangle.id, { opacity: 0.5 })
  graph.syncInstances(replacement.id)
  assert.equal(graph.getChildren(target.id)[0].opacity, 0.5, 'replacement link remains live')
  checkContent(graph, 2, 'Replacement')
  graph = await reopen(graph)
  checkContent(graph, 2, 'Replacement')
  assert.equal(graph.getChildren(graph.getChildren(propertyOwner(graph, 2).id)[0].id)[0].opacity, 0.5)
})

test('a refused nested replacement undo leaves graph and history intact for recovery', async () => {
  const graph = fixture(2), owner = propertyOwner(graph, 2), editor = createEditor({ graph })
  try {
    editor.setInstanceComponentProperty(owner.id, '91:1', named(graph, 'Replacement').id)
    await Promise.resolve()
    const action = graph.getChildren(owner.id)[0], paint = graph.getChildren(action.id)[0]
    graph.updateNode(paint.id, { opacity: 0.25 })
    await Promise.resolve()
    const before = structuredClone([...graph.getAllNodes()]), events = []
    const unsubscribe = graph.onNodeEvents({ created: () => events.push('create'), updated: () => events.push('update'), deleted: () => events.push('delete') })
    try {
      assert.throws(() => editor.undoAction(), /edited descendants requires subtree history/)
      await Promise.resolve()
      assert.deepEqual([...graph.getAllNodes()], before)
      assert.deepEqual(events, [], 'refusal precedes every mutation')
      assert.equal(editor.undo.canUndo, true)
      assert.equal(editor.undo.canRedo, false)
    } finally { unsubscribe() }
    graph.updateNode(paint.id, { opacity: 1 })
    editor.undoAction()
    await Promise.resolve()
    checkContent(graph, 2, 'Original')
    editor.redoAction()
    await Promise.resolve()
    checkContent(graph, 2, 'Replacement')
  } finally { editor.replaceGraph(new SceneGraph()) }
})

test('placed derived sibling positions override template positions after a size-changing replacement', async () => {
  let graph = fixture(2)
  const definition = named(graph, 'Property owner')
  graph.updateNode(definition.id, { layoutMode: 'HORIZONTAL', primaryAxisAlign: 'MAX', width: 160, itemSpacing: 8 })
  graph.reorderChild(definition.childIds[0], definition.id, 1)
  graph.syncInstances(definition.id)
  graph.withLayoutMutations(() => graph.preserveSourceMetadataDuring(() => computeAllLayouts(graph)))
  const owner = propertyOwner(graph, 2), [guard, target] = graph.getChildren(owner.id)
  const oldX = guard.x
  graph.swapInstanceComponent(target.id, named(graph, 'Replacement').id)
  graph.withLayoutMutations(() => graph.preserveSourceMetadataDuring(() => computeLayout(graph, owner.id)))
  assert.equal(guard.x, oldX - 20)
  const positions = graph.getChildren(owner.id).map(node => [node.x, node.y, node.width, node.height])
  for (let cycle = 0; cycle < 2; cycle++) {
    graph = await reopen(graph)
    assert.deepEqual(graph.getChildren(propertyOwner(graph, 2).id).map(node => [node.x, node.y, node.width, node.height]), positions)
  }
})

for (const positioned of [false, true]) for (const angle of [-30, 30]) test(`native rotation preserves centers, inheritance and three saves: positioned=${positioned}, angle=${angle}`, async () => {
  let graph = new SceneGraph()
  const page = graph.getPages()[0]
  const child = graph.createNode('COMPONENT', page.id, { name: 'Rotation guard', width: 40, height: 20 })
  const master = graph.createNode('COMPONENT', page.id, { name: 'Rotation master', width: 200, height: 40,
    rotation: 73, layoutMode: 'HORIZONTAL', itemSpacing: 8 })
  graph.createInstance(child.id, master.id)
  const positionData = positioned ? [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
    schema: 'platformkit.design-export.v1', scope: 'source-composition-layout', cssPosition: {
      version: 1, horizontal: { edge: 'left', inset: 8 }, vertical: { edge: 'top', inset: 12 },
    } }) }] : []
  graph.createNode('RECTANGLE', master.id, { name: 'Rotation leaf', width: 40, height: 20, rotation: angle,
    x: 8, y: 12, layoutPositioning: positioned ? 'ABSOLUTE' : 'AUTO', pluginData: positionData })
  graph.createInstance(master.id, page.id, { name: 'Inherited rotation', x: 200, y: 100, rotation: 17 })
  graph.createInstance(master.id, page.id, { name: 'Authored rotation', x: 400, y: 100, rotation: -17 })
  graph.createNode('RECTANGLE', page.id, { name: 'Direct rotation', x: 300, y: 50, width: 40, height: 20, rotation: angle })
  graph.withLayoutMutations(() => graph.preserveSourceMetadataDuring(() => computeAllLayouts(graph)))
  const leaf = name => graph.getChildren(named(graph, name).id)[1]
  const parts = () => [named(graph, 'Rotation master'), leaf('Rotation master'),
    named(graph, 'Inherited rotation'), leaf('Inherited rotation'), named(graph, 'Authored rotation'),
    leaf('Authored rotation'), named(graph, 'Direct rotation')]
  const fields = ['x', 'y', 'width', 'height', 'rotation']
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) < 1e-4,
    `${label}: ${actual} versus ${expected}`)
  const guardGeometry = () => ['Rotation master', 'Inherited rotation', 'Authored rotation'].map(name =>
    graph.getChildren(named(graph, name).id)[0]).map(node => fields.map(field => node[field]))
  const guards = guardGeometry()
  for (const edited of [false, true]) {
    if (edited) {
      const target = leaf('Authored rotation'), owner = named(graph, 'Authored rotation')
      graph.updateNode(target.id, { rotation: 60 })
      graph.updateNode(owner.id, { overrides: { ...owner.overrides, [`${target.id}:rotation`]: true } })
      graph.updateNode(leaf('Rotation master').id, { rotation: -angle, width: 60, height: 33 })
      graph.updateNode(named(graph, 'Direct rotation').id, { rotation: 45, width: 60, height: 33 })
      graph.syncInstances(named(graph, 'Rotation master').id)
    }
    function checkOwnership() {
      for (const name of ['Rotation master', 'Inherited rotation', 'Authored rotation']) {
        const node = leaf(name)
        close(node.rotation, edited ? name === 'Authored rotation' ? 60 : -angle : angle, `${name}: inherited or owned angle`)
        assert.deepEqual([node.width, node.height], edited ? [60, 33] : [40, 20])
        assert.equal(Object.hasOwn(ancestryOverrides(graph, node), `${node.id}:rotation`), edited && name === 'Authored rotation')
      }
      assert.deepEqual(guardGeometry(), guards, 'sibling instances remain unchanged')
      close(named(graph, 'Inherited rotation').rotation, 17, 'placed orientation is independent of the master')
      close(named(graph, 'Authored rotation').rotation, -17, 'other placed orientation remains independent')
    }
    checkOwnership()
    const expected = parts().map(node => Object.fromEntries(fields.map(field => [field, node[field]])))
    for (let save = 0; save < 3; save++) {
      const bytes = await exportFigFile(graph)
      const { nodeChanges } = parseFigBuffer(bytes.slice().buffer)
      const wire = name => nodeChanges.find(node => node.name === name)
      const wireLeaf = wire('Rotation leaf')
      const derived = name => (positioned ? wire(name).symbolData.symbolOverrides : wire(name).derivedSymbolData).find(record =>
        guidKey(record.guidPath.guids.at(-1)) === guidKey(wireLeaf.guid))
      const records = [wire('Rotation master'), wireLeaf, wire('Inherited rotation'), derived('Inherited rotation'),
        wire('Authored rotation'), derived('Authored rotation'), wire('Direct rotation')]
      if (positioned && save === 0) for (const invalid of [null, 'rotation', ['width'], ['rotation', 'rotation']]) {
        const override = structuredClone(records[3]), entry = override.pluginData.find(item => item.pluginID === 'platformkit')
        entry.value = JSON.stringify({ ...JSON.parse(entry.value), authoredTransformFields: invalid })
        assert.throws(() => ownsRotationOverride(override), /rotation override intent/)
      }
      for (const [index, record] of records.entries()) {
        const node = expected[index], transform = record.transform, radians = node.rotation * Math.PI / 180
        const label = `${edited}/${save}/${index}`
        // This wire oracle is independent of the importer: rotation fixes the native rectangle's center.
        close(transform.m00 * node.width / 2 + transform.m01 * node.height / 2 + transform.m02,
          node.x + node.width / 2, `${label}: center x`)
        close(transform.m10 * node.width / 2 + transform.m11 * node.height / 2 + transform.m12,
          node.y + node.height / 2, `${label}: center y`)
        for (const [field, value] of Object.entries({ m00: Math.cos(radians), m01: -Math.sin(radians),
          m10: Math.sin(radians), m11: Math.cos(radians) })) close(transform[field], value, `${label}: ${field}`)
      }
      graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
      checkOwnership()
      graph.syncInstances(named(graph, 'Rotation master').id)
      checkOwnership()
      for (const [index, node] of parts().entries()) {
        for (const field of fields) close(node[field], expected[index][field], `${edited}/${save}/${index}: ${field}`)
        assert.equal(node.flipX || node.flipY, false, 'saving rotation does not introduce reflection')
      }
    }
  }
})

const scaledRotationMatrix = transform => Object.fromEntries(Object.entries(transform).map(([field, value]) =>
  [field, ['m00', 'm01', 'm10', 'm11'].includes(field) ? value * 0.1 : value]))
for (const [label, angle, change, owned] of [
  ['rotation', 0, matrix => ({ ...matrix, m00: .5, m01: -Math.sqrt(3) / 2, m10: Math.sqrt(3) / 2, m11: .5 }), true],
  ['translation', 0, matrix => ({ ...matrix, m02: matrix.m02 + 10 }), false],
  ['scale', 0, scaledRotationMatrix, false], ['shear', 0, matrix => ({ ...matrix, m01: .25 }), false],
  ['scaled angle', 30, scaledRotationMatrix, true],
]) test(`external source-position ${label} preserves exact native rotation ownership`, async () => {
  let graph = new SceneGraph()
  const page = graph.getPages()[0], source = graph.createNode('COMPONENT', page.id, {
    name: 'External master', width: 200, height: 40, layoutMode: 'HORIZONTAL',
  })
  graph.createNode('RECTANGLE', source.id, { name: 'External leaf', x: 8, y: 12, width: 40, height: 20,
    rotation: angle, layoutPositioning: 'ABSOLUTE', pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source',
      value: JSON.stringify({ schema: 'platformkit.design-export.v1', scope: 'source-composition-layout',
        cssPosition: { version: 1, horizontal: { edge: 'left', inset: 8 }, vertical: { edge: 'top', inset: 12 } } }),
    }] })
  graph.createInstance(source.id, page.id, { name: 'External edited' })
  graph.createInstance(source.id, page.id, { name: 'External sibling' })
  const { nodeChanges } = parseFigBuffer((await exportFigFile(graph)).slice().buffer)
  const override = nodeChanges.find(node => node.name === 'External edited').symbolData.symbolOverrides.find(node => node.transform)
  const originalAngle = convertFigmaTransformProps({ transform: override.transform }).rotation
  override.transform = Object.fromEntries(Object.entries(change(override.transform)).map(([field, value]) => [field, Math.fround(value)]))
  const expected = convertFigmaTransformProps({ transform: override.transform, size: override.size })
  assert.equal((expected.rotation - originalAngle) % 360 !== 0, owned, 'ownership follows the encoded native angle')
  graph = importNodeChanges(nodeChanges)
  populateAllLazyFigImportRoots(graph)
  const guards = () => ['External master', 'External sibling'].flatMap(name => subtree(graph, named(graph, name)))
    .map(node => [node.x, node.y, node.width, node.height, node.rotation])
  const before = guards()
  for (let save = 0; save < 3; save++) {
    const target = graph.getChildren(named(graph, 'External edited').id)[0]
    assert.equal(Object.hasOwn(ancestryOverrides(graph, target), `${target.id}:rotation`), owned)
    const rotation = target.rotation
    graph.syncInstances(named(graph, 'External master').id)
    assert.equal(target.rotation, rotation, 'source synchronization never clobbers the external angle')
    for (const field of ['x', 'y', 'width', 'height', 'rotation']) assert.ok(Math.abs(target[field] - expected[field]) < 1e-5, field)
    assert.deepEqual(guards(), before, 'master and sibling geometry stay untouched')
    if (save < 2) graph = await reopen(graph)
  }
})
