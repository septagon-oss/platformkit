import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { computeAllLayouts, setTextMeasurer } from '@open-pencil/core/layout'

function fixture() {
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
    layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    itemSpacing: 7,
  })
  const instance = graph.createInstance(master.id, row.id)
  const sibling = graph.createNode('RECTANGLE', row.id, { width: 30, height: 20 })
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
  const actions = createEditor({ graph, skipInitialGraphSetup: true })
  const originalNodes = new Map([...graph.getAllNodes()].map(node => [node.id, node]))
  const before = snapshot(graph)
  actions.setInstanceComponentProperty(instance.id, '80:1', 'Longer caption')
  const after = snapshot(graph)
  assert.notEqual(sibling.x, before.find(node => node.id === sibling.id).x)
  assert.equal(label.textPicture, null)
  assert.equal(label.figmaDerivedTextGlyphs, null)
  actions.undo.undo()
  await Promise.resolve()
  assert.deepEqual(snapshot(graph), before)
  actions.undo.redo()
  await Promise.resolve()
  assert.deepEqual(snapshot(graph), after)
  for (const [id, node] of originalNodes) assert.equal(graph.getNode(id), node)
})

test('successive edits and repeated history replay do not alias snapshots or invoke text measurement', () => {
  const { graph, instance } = fixture()
  const actions = createEditor({ graph, skipInitialGraphSetup: true })
  const before = snapshot(graph)
  actions.setInstanceComponentProperty(instance.id, '80:1', 'First longer caption')
  const first = snapshot(graph)
  actions.setInstanceComponentProperty(instance.id, '80:1', 'Second')
  const second = snapshot(graph)
  setTextMeasurer(() => { throw new Error('undo/redo must not measure text') })
  try {
    for (let cycle = 0; cycle < 2; cycle++) {
      actions.undo.undo()
      assert.deepEqual(snapshot(graph), first)
      actions.undo.undo()
      assert.deepEqual(snapshot(graph), before)
      actions.undo.redo()
      assert.deepEqual(snapshot(graph), first)
      actions.undo.redo()
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
  const actions = createEditor({ graph, skipInitialGraphSetup: true })
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
  const actions = createEditor({ graph, skipInitialGraphSetup: true })
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
