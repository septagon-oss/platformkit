import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph, UndoManager } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
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
