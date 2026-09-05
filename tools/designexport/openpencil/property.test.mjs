import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'
import { pathToFileURL } from 'node:url'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { computeAllLayouts, computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'

function fixture({ layoutMode = 'HORIZONTAL', siblingType = 'RECTANGLE' } = {}) {
  setTextMeasurer(node => ({ width: node.text.length * 10, height: 20 }))
  const graph = new SceneGraph()
  const page = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', page.id, {
    name: 'Button', layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    paddingLeft: 12, paddingRight: 12,
    componentPropertyDefinitions: [
      { id: '80:1', name: 'Label', type: 'TEXT', defaultValue: 'Save' },
    ],
  })
  graph.createNode('TEXT', master.id, {
    text: 'Save', width: 40, height: 20, textAutoResize: 'WIDTH_AND_HEIGHT',
    componentPropertyReferences: [{ propertyId: '80:1', field: 'TEXT' }],
  })
  const row = graph.createNode('FRAME', page.id, {
    layoutMode, primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    itemSpacing: 7,
  })
  const instance = graph.createInstance(master.id, row.id)
  const sibling = graph.createNode(siblingType, row.id, { width: 30, height: 20 })
  computeAllLayouts(graph)
  const label = graph.getChildren(instance.id)[0]
  graph.updateNode(instance.id, { width: 99.25, height: 27.5 })
  graph.updateNode(label.id, {
    x: 17.25, y: 3.5, width: 71.75, height: 23.5,
    figmaDerivedLayout: { x: 17.25, y: 3.5, width: 71.75, height: 23.5 },
    textPicture: new Uint8Array([1, 2, 3]),
    figmaDerivedTextGlyphs: [],
  })
  graph.updateNode(sibling.id, { x: 117.75, y: 2.25 })
  return { graph, master, instance, label, row, sibling }
}

function snapshot(graph) {
  return structuredClone([...graph.getAllNodes()])
}

test('property history restores sibling layout, source metadata and text caches without replacing nodes', async () => {
  const { graph, instance, label, sibling } = fixture()
  const actions = createEditor({ graph })
  const originalNodes = new Map([...graph.getAllNodes()].map(node => [node.id, node]))
  const before = snapshot(graph)
  actions.setInstanceComponentProperty(instance.id, '80:1', 'Longer caption')
  const after = snapshot(graph)
  assert.notEqual(sibling.x, before.find(node => node.id === sibling.id).x)
  assert.equal(label.textPicture, null)
  assert.equal(label.figmaDerivedTextGlyphs, null)
  actions.undoAction()
  await Promise.resolve()
  assert.deepEqual(snapshot(graph), before)
  actions.redoAction()
  await Promise.resolve()
  assert.deepEqual(snapshot(graph), after)
  for (const [id, node] of originalNodes) assert.equal(graph.getNode(id), node)
})

test('successive edits and repeated history replay do not alias snapshots or invoke text measurement', () => {
  const { graph, instance } = fixture()
  const actions = createEditor({ graph })
  const before = snapshot(graph)
  actions.setInstanceComponentProperty(instance.id, '80:1', 'First longer caption')
  const first = snapshot(graph)
  actions.setInstanceComponentProperty(instance.id, '80:1', 'Second')
  const second = snapshot(graph)
  setTextMeasurer(() => { throw new Error('undo/redo must not measure text') })
  try {
    for (let cycle = 0; cycle < 2; cycle++) {
      actions.undoAction()
      assert.deepEqual(snapshot(graph), first)
      actions.undoAction()
      assert.deepEqual(snapshot(graph), before)
      actions.redoAction()
      assert.deepEqual(snapshot(graph), first)
      actions.redoAction()
      assert.deepEqual(snapshot(graph), second)
    }
  } finally {
    setTextMeasurer(node => ({ width: node.text.length * 10, height: 20 }))
  }
})

test('master-owned text property edits refuse before assignments, text or history change', async () => {
  const { graph, master } = fixture()
  const owner = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Owner' })
  const nested = graph.createInstance(master.id, owner.id)
  const actions = createEditor({ graph })
  const before = snapshot(graph)
  assert.throws(
    () => actions.setInstanceComponentProperty(nested.id, '80:1', 'Forbidden'),
    /page-placed instances/,
  )
  await Promise.resolve()
  assert.deepEqual(snapshot(graph), before)
  assert.equal(actions.undo.canUndo, false)
})

test('failed initial layout restores original data and records no history entry', () => {
  const { graph, instance } = fixture()
  const actions = createEditor({ graph })
  const before = snapshot(graph)
  setTextMeasurer(() => { throw new Error('measurement failed') })
  try {
    assert.throws(() => actions.setInstanceComponentProperty(instance.id, '80:1', 'Change'), /measurement failed/)
    assert.deepEqual(snapshot(graph), before)
    assert.equal(actions.undo.canUndo, false)
  } finally {
    setTextMeasurer(node => ({ width: node.text.length * 10, height: 20 }))
  }
})

test('subscribed editor keeps exact undo geometry after sibling component notifications settle', async () => {
  const { graph, instance, sibling } = fixture({ siblingType: 'COMPONENT' })
  const actions = createEditor({ graph })
  const before = snapshot(graph)
  actions.setInstanceComponentProperty(instance.id, '80:1', 'Longer caption')
  await Promise.resolve()
  const after = snapshot(graph)
  assert.notEqual(sibling.x, before.find(node => node.id === sibling.id).x)

  actions.undoAction()
  assert.deepEqual(snapshot(graph), before, 'undo must restore the imported state immediately')
  await Promise.resolve()
  assert.deepEqual(snapshot(graph), before, 'deferred component sync must not recompute restored geometry')

  actions.redoAction()
  assert.deepEqual(snapshot(graph), after, 'redo must restore the accepted edit immediately')
  await Promise.resolve()
  assert.deepEqual(snapshot(graph), after, 'deferred component sync must not change the replayed edit')
})

test('late grid measurement failure restores temporary sizing modes and leaves no undo entry', async () => {
  const { graph, instance, master } = fixture({ layoutMode: 'GRID' })
  const actions = createEditor({ graph })
  const before = snapshot(graph)
  assert.equal(instance.primaryAxisSizing, 'HUG')
  assert.equal(instance.counterAxisSizing, 'HUG')
  setTextMeasurer(node => {
    if (instance.primaryAxisSizing === 'FIXED') {
      throw new Error('failure during grid child recompute')
    }
    return { width: node.text.length * 10, height: 20 }
  })
  try {
    assert.throws(
      () => actions.setInstanceComponentProperty(instance.id, '80:1', 'Changed label'),
      /failure during grid child recompute/,
    )
    assert.equal(actions.undo.canUndo, false)
    assert.equal(instance.primaryAxisSizing, 'HUG', 'rollback must restore the temporary primary sizing mode')
    assert.equal(instance.counterAxisSizing, 'HUG', 'rollback must restore the temporary counter sizing mode')
    assert.deepEqual(snapshot(graph), before)
    await Promise.resolve()
    assert.deepEqual(snapshot(graph), before, 'rollback must remain exact after subscribed notifications settle')
  } finally {
    setTextMeasurer(node => ({ width: node.text.length * 10, height: 20 }))
  }
  graph.updateNode(graph.getChildren(master.id)[0].id, { text: 'Recovery' })
  await Promise.resolve()
  assert.equal(graph.getChildren(instance.id)[0].text, 'Recovery',
    'failed property layout must release the scheduler guard for later authored edits')
})

test('subscribed master edits still propagate while property overrides and history remain independent', async () => {
  const { graph, master, instance, label } = fixture()
  const follower = graph.createInstance(master.id, graph.getPages()[0].id)
  const followerLabel = graph.getChildren(follower.id)[0]
  const masterLabel = graph.getChildren(master.id)[0]
  const actions = createEditor({ graph })
  graph.updateNode(masterLabel.id, { text: 'Master caption' })
  assert.equal(followerLabel.text, 'Save', 'authored master changes should use the normal deferred scheduler')
  await Promise.resolve()
  assert.equal(followerLabel.text, 'Master caption')
  assert.equal(label.text, 'Master caption')

  const before = snapshot(graph)
  actions.setInstanceComponentProperty(instance.id, '80:1', 'Private caption')
  await Promise.resolve()
  const after = snapshot(graph)
  const renderVersion = actions.state.renderVersion
  const updatedNodes = new Set()
  const unsubscribe = actions.onEditorEvent('node:updated', id => updatedNodes.add(id))
  try {
    actions.undoAction()
    await Promise.resolve()
    assert.deepEqual(snapshot(graph), before)
    actions.redoAction()
    await Promise.resolve()
    assert.deepEqual(snapshot(graph), after)
    assert.ok(updatedNodes.has(label.id), 'history replay must still emit normal node invalidation events')
    assert.ok(actions.state.renderVersion > renderVersion, 'history replay must still request rendering')
  } finally {
    unsubscribe()
  }

  graph.updateNode(masterLabel.id, { text: 'Revised master caption' })
  await Promise.resolve()
  assert.equal(followerLabel.text, 'Revised master caption', 'genuine master edits must not be suppressed')
  assert.equal(label.text, 'Private caption', 'a master edit must not overwrite the explicit instance property')
  assert.equal(instance.componentPropertyAssignments['80:1'], 'Private caption')
  assert.equal(instance.overrides[`${label.id}:text`], 'Private caption')
})

test('failed property measurement explicitly frees every allocated Yoga node', async t => {
  const coreRequire = createRequire(import.meta.resolve('@open-pencil/core/editor'))
  const { default: Yoga } = await import(pathToFileURL(coreRequire.resolve('yoga-layout')).href)
  for (const layoutMode of ['HORIZONTAL', 'GRID']) {
    await t.test(layoutMode, () => {
      const originalCreate = Yoga.Node.create
      const originalMeasurer = getTextMeasurer()
      let created = 0
      let freed = 0
      try {
        const { graph, instance } = fixture({ layoutMode })
        const actions = createEditor({ graph })
        Yoga.Node.create = function (...args) {
          const node = originalCreate.apply(this, args)
          created++
          const originalFree = node.free
          node.free = function (...freeArgs) {
            const result = originalFree.apply(this, freeArgs)
            freed++
            return result
          }
          return node
        }
        setTextMeasurer(node => {
          if (layoutMode === 'HORIZONTAL' || instance.primaryAxisSizing === 'FIXED') {
            throw new Error('measurement cleanup failure')
          }
          return { width: node.text.length * 10, height: 20 }
        })
        assert.throws(
          () => actions.setInstanceComponentProperty(instance.id, '80:1', 'Changed label'),
          /measurement cleanup failure/,
        )
        assert.ok(created > 0, 'the failure must exercise the actual SDK Yoga allocator')
        assert.equal(freed, created, 'every allocated node must be explicitly freed before returning')
      } finally {
        Yoga.Node.create = originalCreate
        setTextMeasurer(originalMeasurer)
      }
    })
  }
})

test('grid layout restores its temporary sizing modes even without property rollback', () => {
  const { graph, instance, row } = fixture({ layoutMode: 'GRID' })
  graph.updateNode(graph.getChildren(instance.id)[0].id, { figmaDerivedLayout: null })
  const originalMeasurer = getTextMeasurer()
  setTextMeasurer(node => {
    if (instance.primaryAxisSizing === 'FIXED') throw new Error('grid recompute failed')
    return { width: node.text.length * 10, height: 20 }
  })
  try {
    assert.throws(() => computeLayout(graph, row.id), /grid recompute failed/)
    assert.equal(instance.primaryAxisSizing, 'HUG')
    assert.equal(instance.counterAxisSizing, 'HUG')
  } finally {
    setTextMeasurer(originalMeasurer)
  }
})

test('authored parent resizing still propagates computed master dimensions to instances', async () => {
  const graph = new SceneGraph()
  const page = graph.getPages()[0]
  const parent = graph.createNode('FRAME', page.id, {
    width: 100, height: 100, layoutMode: 'VERTICAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED',
  })
  const master = graph.createNode('COMPONENT', parent.id, {
    width: 40, height: 20, layoutAlignSelf: 'STRETCH',
  })
  computeAllLayouts(graph)
  const instance = graph.createInstance(master.id, page.id)
  const actions = createEditor({ graph })
  assert.equal(instance.width, 100)
  actions.updateNodeWithUndo(parent.id, { width: 200 })
  await Promise.resolve()
  assert.equal(master.width, 200)
  assert.equal(instance.width, 200)
})

test('unsupported property-driven master resizing rolls back without stale external instances', async () => {
  const { graph, instance, row, sibling } = fixture({ siblingType: 'COMPONENT' })
  graph.updateNode(row.id, { layoutMode: 'VERTICAL' })
  graph.updateNode(sibling.id, { layoutAlignSelf: 'STRETCH' })
  computeAllLayouts(graph)
  const external = graph.createInstance(sibling.id, graph.getPages()[0].id)
  const actions = createEditor({ graph })
  const before = snapshot(graph)
  assert.throws(() => actions.setInstanceComponentProperty(instance.id, '80:1', 'Longer caption'),
    /requires downstream instance history/)
  await Promise.resolve()
  assert.deepEqual(snapshot(graph), before)
  assert.equal(external.width, sibling.width)
  assert.equal(actions.undo.canUndo, false)

  graph.updateNode(sibling.id, { layoutAlignSelf: 'MIN' })
  await Promise.resolve()
  actions.setInstanceComponentProperty(instance.id, '80:1', 'Longer caption')
  await Promise.resolve()
  assert.equal(graph.getChildren(instance.id)[0].text, 'Longer caption')
  assert.equal(external.width, sibling.width)
})
