import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

const state = graph => structuredClone({ variables: [...graph.variables], collections: [...graph.variableCollections],
  nodes: [...graph.nodes], active: [...graph.activeMode] })
const named = (graph, name) => [...graph.nodes.values()].find(node => node.name === name)

function fixture() {
  const graph = new SceneGraph(), collection = graph.createCollection('Tokens')
  graph.addMode(collection.id, 'dark', 'Dark')
  const number = graph.createVariable('Spacing', 'FLOAT', collection.id, 0.1)
  number.sourceToken = { version: 1, snapshot: 'a'.repeat(64), kind: 'scale', scale: 'spacing', key: '1', decimal: '0.1000', unit: 'px' }
  number.valuesByMode.dark = 1e-50
  const ink = graph.createVariable('Ink', 'COLOR', collection.id, { r: 1, g: 0, b: 0, a: 1 })
  const last = graph.createVariable('Trailing', 'FLOAT', collection.id, 16777217)
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Master', width: 24, height: 24 })
  const child = graph.createNode('RECTANGLE', master.id, { name: 'Bound child', width: 24, height: 24,
    fills: [{ type: 'SOLID', color: ink.valuesByMode[collection.defaultModeId], visible: true, opacity: 1 }] })
  graph.bindVariable(child.id, 'fills/0/color', ink.id)
  const first = graph.createInstance(master.id, graph.getPages()[0].id, { name: 'First instance' })
  const second = graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Sibling instance' })
  graph.updateNode(second.id, { x: 100 })
  graph.setActiveMode(collection.id, 'dark')
  return { graph, collection, number, ink, last, master, child, first, second, editor: createEditor({ graph }) }
}

test('variable and collection deletion undo restores order, linked bindings, overrides, source records and active mode', async () => {
  for (const collectionRemoval of [false, true]) {
    const f = fixture(), { graph, editor, collection, ink } = f
    const before = state(graph)
    if (collectionRemoval) editor.removeCollection(collection.id)
    else editor.removeVariable(ink.id)
    const after = state(graph)
    assert.equal(graph.variables.has(ink.id), false)
    assert.deepEqual(f.child.boundVariables, {})
    for (let repeat = 0; repeat < 2; repeat++) {
      editor.undoAction()
      assert.deepEqual(state(graph), before)
      editor.redoAction()
      assert.deepEqual(state(graph), after)
    }
    editor.undoAction()
    let reopened = graph
    for (let cycle = 0; cycle < 2; cycle++) {
      reopened = await parseFigFile((await exportFigFile(reopened)).slice().buffer, { populate: 'all' })
      const variables = [...reopened.variables.values()], color = variables.find(variable => variable.name === 'Ink')
      assert.deepEqual(reopened.getVariablesForCollection(color.collectionId).map(variable => variable.name), ['Spacing', 'Ink', 'Trailing'])
      assert.deepEqual(variables.find(variable => variable.name === 'Spacing').sourceToken, f.number.sourceToken)
      for (const owner of ['Master', 'First instance', 'Sibling instance']) {
        const node = named(reopened, owner), child = reopened.getChildren(node.id)[0]
        assert.equal(child.boundVariables['fills/0/color'], color.id, owner)
        assert.ok(!Object.keys(node.overrides).some(key => /(^|:)boundVariables$/.test(key)), owner)
        if (node.type === 'INSTANCE') assert.equal(reopened.getNode(node.componentId).name, 'Master')
      }
    }
  }
})

test('undo with a lost numeric or color dependency refuses unchanged, emits nothing and remains retryable', () => {
  for (const type of ['FLOAT', 'COLOR']) {
    const { graph, collection, editor } = fixture()
    const input = graph.createVariable('Input', type, collection.id, type === 'FLOAT' ? 1e-50 : { r: 0, g: 1, b: 0, a: 1 })
    const alias = graph.createVariable('Alias', type, collection.id, { aliasId: input.id })
    editor.removeVariable(alias.id)
    const saved = structuredClone(input)
    graph.removeVariable(input.id)
    const before = state(graph)
    let renders = 0, updates = 0
    editor.onEditorEvent('render:requested', () => renders++)
    graph.emitter.on('node:updated', () => updates++)
    assert.throws(() => editor.undoAction(), /Native (number|CSS color|variable history):/)
    assert.deepEqual(state(graph), before)
    assert.equal(renders, 0)
    assert.equal(updates, 0)
    graph.addVariable(saved)
    editor.undoAction()
    assert.deepEqual(graph.resolveVariable(alias.id), saved.valuesByMode[collection.defaultModeId])
  }
})

test('restoration refuses conflicting bindings and missing nodes without overwriting independent edits', () => {
  for (const conflict of ['binding', 'node', 'identity', 'mode']) {
    const f = fixture(), { graph, collection, editor, ink, child } = f
    editor.removeVariable(ink.id)
    if (conflict === 'binding') {
      const replacement = graph.createVariable('Replacement', 'COLOR', collection.id, { r: 0, g: 0, b: 1, a: 1 })
      graph.bindVariable(child.id, 'fills/0/color', replacement.id)
    } else if (conflict === 'node') graph.deleteNode(child.id)
    else if (conflict === 'identity') graph.addVariable({ ...structuredClone(ink), name: 'Another author' })
    else graph.removeMode(collection.id, 'dark')
    const before = state(graph)
    assert.throws(() => editor.undoAction(), /Native (number|CSS color|variable history):/)
    assert.deepEqual(state(graph), before)
  }
})

test('restoration preserves unrelated geometry, override keys and newly added variables', () => {
  const f = fixture(), { graph, collection, editor, ink, first, second } = f
  editor.removeVariable(ink.id)
  graph.updateNode(first.id, { x: 45 })
  second.overrides.visible = true
  const added = graph.createVariable('Later variable', 'FLOAT', collection.id, -0)
  editor.undoAction()
  assert.equal(first.x, 45)
  assert.equal(second.overrides.visible, true)
  assert.equal(graph.variables.get(added.id), added)
  assert.deepEqual(graph.getVariablesForCollection(collection.id).map(variable => variable.name), ['Spacing', 'Ink', 'Trailing', 'Later variable'])
  // A second deletion records its current affected bindings, not a snapshot
  // object that was handed to the graph and later mutated by another action.
  editor.redoAction()
  editor.undoAction()
  assert.equal(first.x, 45)
  assert.equal(second.overrides.visible, true)
  assert.ok(Object.is(graph.resolveVariable(added.id), -0))
})

test('a collection with internal numeric and color aliases is restored as a complete dependency closure', () => {
  const { graph, collection, editor, ink, number } = fixture()
  graph.createVariable('Numeric alias', 'FLOAT', collection.id, { aliasId: number.id })
  graph.createVariable('Color alias', 'COLOR', collection.id, { aliasId: ink.id })
  const before = state(graph)
  editor.removeCollection(collection.id)
  editor.undoAction()
  assert.deepEqual(state(graph), before)
})

test('deleting a plain color alias input refuses without consuming redo', () => {
  const { graph, collection, editor, ink } = fixture()
  graph.createVariable('Color alias', 'COLOR', collection.id, { aliasId: ink.id })
  editor.renameVariable(ink.id, 'Renamed')
  editor.undoAction()
  const before = state(graph)
  assert.throws(() => editor.removeVariable(ink.id), /Native CSS color:/)
  assert.deepEqual(state(graph), before)
  editor.redoAction()
  assert.equal(ink.name, 'Renamed')
})

test('restored masters update consumers added after deletion without changing their independent geometry', async () => {
  const { graph, editor, ink, master } = fixture()
  editor.removeVariable(ink.id)
  const later = graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Later instance', x: 200 })
  editor.undoAction()
  await new Promise(queueMicrotask)
  assert.equal(later.x, 200)
  assert.equal(graph.getChildren(later.id)[0].boundVariables['fills/0/color'], ink.id)
  editor.redoAction()
  editor.undoAction()
  await new Promise(queueMicrotask)
  assert.equal(graph.getChildren(later.id)[0].boundVariables['fills/0/color'], ink.id)
})

test('collection history preserves fresh restored values and refuses a reused collection identity', () => {
  const { graph, editor, collection, number } = fixture()
  editor.removeCollection(collection.id)
  const saved = structuredClone(collection)
  graph.addCollection({ ...saved, name: 'Another collection', variableIds: [] })
  const before = state(graph)
  assert.throws(() => editor.undoAction(), /Native variable history:/)
  assert.deepEqual(state(graph), before)
  graph.removeCollection(collection.id)
  editor.undoAction()
  graph.variables.get(number.id).valuesByMode[collection.defaultModeId] = 0.2
  editor.redoAction()
  editor.undoAction()
  assert.equal(graph.variables.get(number.id).valuesByMode[collection.defaultModeId], 0.2)
  assert.equal(graph.variables.get(number.id).sourceToken.decimal, '0.1000')
})

test('whole collection removal validates the remaining mode contexts, not contexts it removes', () => {
  for (const type of ['FLOAT', 'COLOR']) {
    const graph = new SceneGraph(), retired = graph.createCollection('Retired')
    for (const [id, defaultModeId] of [['A', 'a'], ['B', 'b']]) graph.addCollection({ id, name: id, defaultModeId,
      modes: [{ modeId: 'a', name: 'A' }, { modeId: 'b', name: 'B' }], variableIds: [] })
    const literal = type === 'FLOAT' ? 0.1 : { r: 1, g: 0, b: 0, a: 1 }
    const first = graph.createVariable('First', type, 'A', literal)
    const second = graph.createVariable('Second', type, 'B', literal)
    first.valuesByMode.a = { aliasId: second.id }
    second.valuesByMode.b = { aliasId: first.id }
    // Both shared modes resolve. The retired foreign mode alone selects both
    // defaults and exposes a cycle; removing that context repairs the graph.
    const editor = createEditor({ graph })
    editor.removeCollection(retired.id)
    const after = state(graph)
    assert.throws(() => editor.undoAction(), /Native (number|CSS color):/)
    assert.deepEqual(state(graph), after)
    first.valuesByMode.a = literal
    editor.undoAction()
    assert.deepEqual(graph.resolveVariable(first.id), literal)
  }
})
