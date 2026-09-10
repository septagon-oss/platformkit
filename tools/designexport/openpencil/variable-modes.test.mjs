import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import { exportModeOrder, importModeOrder } from './variable-modes.mjs'

const state = graph => structuredClone({ variables: [...graph.variables], collections: [...graph.variableCollections],
  nodes: [...graph.nodes], active: [...graph.activeMode] })
const named = (graph, name) => [...graph.variables.values()].find(variable => variable.name === name)
const reopen = async graph => parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })

function fixture() {
  const graph = new SceneGraph(), collection = graph.createCollection('Tokens')
  graph.renameMode(collection.id, collection.defaultModeId, 'light')
  graph.addMode(collection.id, 'night', 'dark')
  const number = graph.createVariable('Spacing', 'FLOAT', collection.id, 0.1)
  number.sourceToken = { version: 1, snapshot: 'a'.repeat(64), kind: 'scale', scale: 'spacing', key: '1', decimal: '0.1000', unit: 'px' }
  number.valuesByMode.night = 16777217
  graph.createVariable('Alias', 'FLOAT', collection.id, { aliasId: number.id })
  const ink = graph.createVariable('Ink', 'COLOR', collection.id, { r: 1, g: 0, b: 0, a: 1 })
  ink.valuesByMode.night = { r: 0, g: 0, b: 1, a: 1 }
  graph.createVariable('Derived', 'COLOR', collection.id, { cssColor: {
    value: 'var(--ink)', customProperties: { '--ink': { aliasId: ink.id } },
  } })
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Master', width: 20, height: 20,
    fills: [{ type: 'SOLID', color: ink.valuesByMode[collection.defaultModeId], opacity: 1, visible: true }] })
  graph.bindVariable(master.id, 'fills/0/color', ink.id)
  graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Instance' })
  return graph
}

test('a nonfirst default mode retains its meaning and independent display order through two saves', async () => {
  let graph = fixture()
  for (let cycle = 0; cycle < 3; cycle++) {
    const collection = [...graph.variableCollections.values()][0], editor = createEditor({ graph })
    const light = collection.modes.find(mode => mode.name === 'light').modeId
    const dark = collection.modes.find(mode => mode.name === 'dark').modeId
    if (cycle === 0) {
      const before = state(graph)
      editor.setDefaultMode(collection.id, dark)
      editor.undoAction()
      assert.deepEqual(state(graph), before)
      editor.redoAction()
    }
    assert.deepEqual(collection.modes.map(mode => mode.name), ['light', 'dark'])
    assert.equal(collection.defaultModeId, dark)
    assert.equal(graph.resolveVariable(named(graph, 'Alias').id, 'foreign-mode'), 16777217)
    assert.equal(graph.resolveVariable(named(graph, 'Alias').id, light), 0.1)
    assert.deepEqual(graph.resolveVariable(named(graph, 'Derived').id, 'foreign-mode'), { r: 0, g: 0, b: 1, a: 1 })
    const before = state(graph), bytes = await exportFigFile(graph)
    assert.deepEqual(state(graph), before)
    const wire = parseFigBuffer(bytes.slice().buffer).nodeChanges.find(node => node.type === 'VARIABLE_SET')
    assert.deepEqual(wire.variableSetModes.map(mode => mode.name), ['dark', 'light'], 'wire fallback uses the actual default')
    assert.deepEqual(wire.variableSetModes.toSorted((a, b) => a.sortPosition < b.sortPosition ? -1 : 1).map(mode => mode.name),
      ['light', 'dark'], 'native sort positions retain the authored column order')
    if (cycle < 2) graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
  }
})

test('removing and undoing a mode preserves absent values, order, active mode, source records and nodes', () => {
  for (const removeDefault of [false, true]) {
    const graph = fixture(), editor = createEditor({ graph }), collection = [...graph.variableCollections.values()][0]
    const light = collection.defaultModeId
    const removed = removeDefault ? light : 'night', absent = named(graph, 'Spacing')
    if (!removeDefault) delete absent.valuesByMode.night
    graph.setActiveMode(collection.id, removed)
    const before = state(graph)
    editor.removeMode(collection.id, removed)
    const after = state(graph)
    for (let attempt = 0; attempt < 2; attempt++) {
      editor.undoAction()
      assert.deepEqual(state(graph), before)
      editor.redoAction()
      assert.deepEqual(state(graph), after)
    }
  }
})

test('add and duplicate mode redo restore their captured values instead of recomputing a changed source', async () => {
  for (const duplicate of [false, true]) {
    let graph = fixture()
    const editor = createEditor({ graph }), collection = [...graph.variableCollections.values()][0]
    const number = named(graph, 'Spacing'), source = duplicate ? 'night' : collection.defaultModeId
    const mode = duplicate ? editor.duplicateMode(collection.id, source) : editor.addMode(collection.id, 'Contrast')
    const expected = number.valuesByMode[mode], nodes = structuredClone([...graph.nodes])
    editor.undoAction()
    number.valuesByMode[source] = 1e-50
    editor.redoAction()
    assert.equal(number.valuesByMode[mode], expected)
    assert.equal(number.valuesByMode[source], 1e-50, 'unrelated source edit remains')
    assert.deepEqual([...graph.nodes], nodes)
    const modeName = collection.modes.find(value => value.modeId === mode).name
    for (let cycle = 0; cycle < 2; cycle++) {
      graph = await reopen(graph)
      const next = named(graph, 'Spacing'), nextCollection = graph.variableCollections.get(next.collectionId)
      assert.equal(next.valuesByMode[nextCollection.modes.find(value => value.name === modeName).modeId], expected)
    }
  }
})

test('mode changes refuse dependency loss before variables, events, or redo history change', () => {
  const graph = new SceneGraph(), first = graph.createCollection('First'), second = graph.createCollection('Second')
  graph.addMode(first.id, 'shared-dark', 'dark')
  graph.addMode(second.id, 'shared-dark', 'dark')
  const base = graph.createVariable('Base', 'FLOAT', first.id, 1)
  const input = graph.createVariable('Input', 'FLOAT', second.id, { aliasId: base.id })
  base.valuesByMode['shared-dark'] = { aliasId: input.id }
  input.valuesByMode['shared-dark'] = 3
  const editor = createEditor({ graph })
  editor.renameVariable(base.id, 'Renamed')
  editor.undoAction()
  const before = state(graph)
  let renders = 0, updates = 0
  editor.onEditorEvent('render:requested', () => renders++)
  graph.emitter.on('node:updated', () => updates++)
  for (const remove of [() => graph.removeMode(second.id, 'shared-dark'), () => editor.removeMode(second.id, 'shared-dark')]) {
    assert.throws(remove, /Native number:|Native variable mode:/)
    assert.deepEqual(state(graph), before)
    assert.equal(renders, 0)
    assert.equal(updates, 0)
  }
  editor.redoAction()
  assert.equal(base.name, 'Renamed')
})

test('an undo whose restored mode lost an alias target refuses unchanged and remains retryable', () => {
  const graph = fixture(), editor = createEditor({ graph }), collection = [...graph.variableCollections.values()][0]
  const input = graph.createVariable('Temporary input', 'FLOAT', collection.id, 1e-50)
  named(graph, 'Spacing').valuesByMode.night = { aliasId: input.id }
  editor.removeMode(collection.id, 'night')
  const saved = structuredClone(input)
  graph.removeVariable(input.id)
  const before = state(graph)
  let renders = 0
  editor.onEditorEvent('render:requested', () => renders++)
  assert.throws(() => editor.undoAction(), /Native number:|Native variable mode:/)
  assert.deepEqual(state(graph), before)
  assert.equal(renders, 0)
  graph.addVariable(saved)
  editor.undoAction()
  assert.equal(graph.resolveVariable(named(graph, 'Alias').id, 'night'), 1e-50)
})

test('invalid or duplicate native modes refuse without making a partial collection', () => {
  const graph = fixture(), collection = [...graph.variableCollections.values()][0]
  const before = state(graph)
  assert.throws(() => graph.addMode(collection.id, 'night', 'Duplicate'), /Native variable mode:/)
  assert.deepEqual(state(graph), before)
  delete named(graph, 'Spacing').valuesByMode.night
  const sparse = state(graph)
  assert.throws(() => graph.setDefaultMode(collection.id, 'night'), /Native number:/)
  assert.deepEqual(state(graph), sparse)
})

test('native mode ordering is explicit, detached and refuses mixed or ambiguous positions', () => {
  const modes = [
    { id: { sessionID: 0, localID: 2 }, name: 'dark', sortPosition: 'b' },
    { id: { sessionID: 0, localID: 1 }, name: 'light', sortPosition: 'a' },
  ]
  const before = structuredClone(modes)
  assert.deepEqual(importModeOrder(modes).map(mode => mode.name), ['light', 'dark'])
  assert.deepEqual(modes, before)
  const legacy = modes.map(({ sortPosition, ...mode }) => mode)
  assert.deepEqual(importModeOrder(legacy), legacy, 'legacy documents retain wire order when no positions are supplied')
  assert.notEqual(importModeOrder(legacy), legacy)
  for (const change of [values => { delete values[0].sortPosition }, values => { values[0].sortPosition = 'a' },
    values => { values[0].sortPosition = '' }, values => { values[0].sortPosition = null },
    values => { values[0].sortPosition = 'x'.repeat(1025) }]) {
    const values = structuredClone(modes)
    change(values)
    const original = structuredClone(values)
    assert.throws(() => importModeOrder(values), /ambiguous native mode order/)
    assert.deepEqual(values, original)
  }
  const collection = { defaultModeId: 'dark', modes: [{ modeId: 'light', name: 'Light' }, { modeId: 'dark', name: 'Dark' }] }
  const exported = exportModeOrder(collection)
  assert.deepEqual(exported.map(({ mode, index }) => [mode.modeId, index]), [['dark', 1], ['light', 0]])
  exported[0].mode.name = 'Native copy'
  assert.equal(collection.modes[1].name, 'Dark')
})

test('mode deletion cannot erase a sparse plain color or introduce a plain color alias cycle', () => {
  for (const plainAlias of [false, true]) {
    const graph = new SceneGraph(), collection = graph.createCollection('Palette')
    graph.addMode(collection.id, 'dark', 'dark')
    const color = graph.createVariable('Color', 'COLOR', collection.id, { r: 1, g: 0, b: 0, a: 1 })
    delete color.valuesByMode.dark
    if (plainAlias) color.valuesByMode.dark = { aliasId: color.id }
    const before = state(graph)
    assert.throws(() => graph.removeMode(collection.id, collection.defaultModeId), /Native variable mode:|Native CSS color:/)
    assert.deepEqual(state(graph), before)
  }
})

test('default-mode history refuses conflicting defaults but preserves independent active-mode choices', () => {
  for (const removal of [false, true]) {
    const graph = fixture(), editor = createEditor({ graph }), collection = [...graph.variableCollections.values()][0]
    graph.addMode(collection.id, 'contrast', 'Contrast')
    const light = collection.defaultModeId
    if (removal) editor.removeMode(collection.id, light)
    else editor.setDefaultMode(collection.id, 'night')
    graph.setDefaultMode(collection.id, 'contrast')
    const before = state(graph)
    assert.throws(() => editor.undoAction(), /default mode history is stale/)
    assert.deepEqual(state(graph), before)
    graph.setDefaultMode(collection.id, 'night')
    graph.setActiveMode(collection.id, 'contrast')
    editor.undoAction()
    assert.equal(collection.defaultModeId, light)
    assert.equal(graph.activeMode.get(collection.id), 'contrast')
  }
})

test('unknown mode edits preserve the existing redo entry', () => {
  const graph = fixture(), editor = createEditor({ graph }), collection = [...graph.variableCollections.values()][0]
  editor.renameVariable(named(graph, 'Spacing').id, 'Renamed')
  editor.undoAction()
  const before = state(graph)
  for (const change of [() => editor.removeMode(collection.id, 'missing'), () => editor.setDefaultMode(collection.id, 'missing')]) {
    assert.throws(change, /unknown mode/)
    assert.deepEqual(state(graph), before)
  }
  editor.redoAction()
  assert.ok(named(graph, 'Renamed'))
})
