import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { FigmaAPI } from '@open-pencil/core/figma-api'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import { parseNativeNumber, restoreNumbers, validateNumericVariables } from './variable-number.mjs'

const named = (graph, name) => [...graph.variables.values()].find(variable => variable.name === name)
const state = graph => structuredClone({ variables: [...graph.variables], collections: [...graph.variableCollections],
  nodes: [...graph.nodes], modes: [...graph.activeMode] })
const reopen = async graph => parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })

test('numeric text edits accept complete decimals without silent truncation or decimal rounding', () => {
  for (const [text, expected] of [['0', 0], ['-0', -0], ['-0.00e12', -0], [' .1000 ', 0.1],
    ['+0001.e2', 100], ['1e-50', 1e-50], ['5e-324', Number.MIN_VALUE],
    ['0.1234567890123456', 0.1234567890123456], ['9007199254740992', 9007199254740992]]) {
    assert.ok(Object.is(parseNativeNumber(text), expected), text)
  }
  for (const text of ['', ' ', '12px', '1 + 2', '0x10', '1e', '1_000', 'Infinity', 'NaN',
    '1e39', '-1e39', '1e-999', '-1e-999', '9007199254740993', '0.10000000000000001',
    '4.9e-324', '1'.repeat(1025), null, 1]) {
    assert.throws(() => parseNativeNumber(text), /Native number:/, String(text))
  }
})

function fixture() {
  const graph = new SceneGraph(), collection = graph.createCollection('Numeric tokens')
  graph.renameMode(collection.id, collection.defaultModeId, 'light')
  graph.addMode(collection.id, 'numeric-dark', 'dark')
  const base = graph.createVariable('Base', 'FLOAT', collection.id, 0.1)
  base.valuesByMode['numeric-dark'] = 16777217
  graph.removeVariable(base.id)
  graph.addVariable({ ...base, id: 'source-number' })
  const alias = graph.createVariable('Alias', 'FLOAT', collection.id, { aliasId: 'source-number' })
  alias.valuesByMode['numeric-dark'] = { aliasId: 'source-number' }
  const page = graph.getPages()[0], master = graph.createNode('COMPONENT', page.id, { name: 'Unchanged master' })
  graph.createInstance(master.id, page.id, { name: 'Unchanged instance' })
  return graph
}

test('numeric variables retain exact values, modes and remapped aliases through two native saves', async () => {
  let graph = fixture()
  const collection = [...graph.variableCollections.values()][0]
  const literals = [0, -0, 0.1, -0.1, 1e-50, -1e-50, Number.MIN_VALUE, 16777217,
    Number.MAX_SAFE_INTEGER, 3.4028234663852886e38]
  for (const [index, value] of literals.entries()) graph.createVariable(`Literal ${index}`, 'FLOAT', collection.id, value)
  for (let cycle = 0; cycle < 3; cycle++) {
    for (const [index, value] of literals.entries()) {
      assert.ok(Object.is(graph.resolveVariable(named(graph, `Literal ${index}`).id), value), `literal ${index}, cycle ${cycle}`)
    }
    const base = named(graph, 'Base'), alias = named(graph, 'Alias')
    const modes = graph.variableCollections.get(base.collectionId).modes
    for (const [name, value] of [['light', 0.1], ['dark', 16777217]]) {
      const mode = modes.find(mode => mode.name === name).modeId
      assert.equal(graph.resolveVariable(alias.id, mode), value)
      assert.deepEqual(alias.valuesByMode[mode], { aliasId: base.id })
    }
    const before = state(graph), bytes = await exportFigFile(graph)
    assert.deepEqual(state(graph), before, 'export must not quantize the caller graph')
    const wire = parseFigBuffer(bytes.slice().buffer)
    const entry = wire.nodeChanges.find(node => node.type === 'VARIABLE' && node.name === 'Base')
    assert.equal(entry.variableResolvedType, 'FLOAT')
    assert.equal(entry.variableDataValues.entries[0].variableData.value.floatValue, 0.10000000149011612)
    assert.ok(entry.pluginData.some(item => item.pluginID === 'platformkit' && item.key === 'number-values'))
    if (cycle < 2) graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
  }
})

test('ordinary numeric edits and undo/redo remain exact after reopening without changing nodes', async () => {
  let graph = fixture()
  for (let cycle = 0; cycle < 3; cycle++) {
    const editor = createEditor({ graph }), api = new FigmaAPI(graph), base = named(graph, 'Base'), alias = named(graph, 'Alias')
    const mode = graph.variableCollections.get(base.collectionId).defaultModeId, nodes = structuredClone([...graph.nodes])
    assert.equal(graph.resolveVariable(alias.id, mode), 0.1)
    editor.updateVariableValue(base.id, mode, 1e-50)
    assert.equal(graph.resolveVariable(alias.id, mode), 1e-50)
    editor.undoAction()
    assert.equal(graph.resolveVariable(alias.id, mode), 0.1)
    editor.redoAction()
    assert.equal(graph.resolveVariable(alias.id, mode), 1e-50)
    api.setVariableValue(base.id, mode, -0)
    assert.ok(Object.is(graph.resolveVariable(alias.id, mode), -0))
    api.setVariableValue(base.id, mode, 0.1)
    assert.deepEqual([...graph.nodes], nodes)
    if (cycle < 2) graph = await reopen(graph)
  }
})

test('invalid numeric value edits preserve native state, render events and redo history', () => {
  const graph = fixture(), editor = createEditor({ graph }), api = new FigmaAPI(graph)
  const base = named(graph, 'Base'), alias = named(graph, 'Alias'), mode = graph.variableCollections.get(base.collectionId).defaultModeId
  const wrongType = graph.createVariable('Not a number', 'STRING', base.collectionId, 7)
  editor.updateVariableValue(base.id, mode, 0.2)
  editor.undoAction()
  const before = state(graph)
  let renders = 0, updates = 0
  editor.onEditorEvent('render:requested', () => renders++)
  graph.emitter.on('node:updated', () => updates++)
  for (const change of [value => editor.updateVariableValue(base.id, mode, value),
    value => api.setVariableValue(base.id, mode, value)]) {
    for (const value of [NaN, Infinity, -Infinity, 1e39, '7', null, undefined, [], {},
      { aliasId: 'missing' }, { aliasId: wrongType.id }, { aliasId: alias.id }, { aliasId: base.id },
      { aliasId: alias.id, extra: true }]) {
      assert.throws(() => change(value), /Native number:/)
      assert.deepEqual(state(graph), before)
      assert.equal(renders, 0)
      assert.equal(updates, 0)
    }
  }
  editor.redoAction()
  assert.equal(graph.resolveVariable(alias.id, mode), 0.2)
})

test('numeric dependency deletion refuses unchanged while complete dependency closures can be removed', () => {
  const graph = fixture(), editor = createEditor({ graph }), api = new FigmaAPI(graph), base = named(graph, 'Base')
  const outer = graph.createCollection('Outside')
  graph.createVariable('Outside alias', 'FLOAT', outer.id, { aliasId: base.id })
  editor.renameVariable(base.id, 'Changed name')
  editor.undoAction()
  const before = state(graph)
  for (const remove of [() => graph.removeVariable(base.id), () => editor.removeVariable(base.id),
    () => api.deleteVariable(base.id), () => graph.removeCollection(base.collectionId),
    () => editor.removeCollection(base.collectionId), () => api.deleteVariableCollection(base.collectionId)]) {
    assert.throws(remove, /Native number:/)
    assert.deepEqual(state(graph), before)
  }
  editor.redoAction()
  assert.equal(base.name, 'Changed name')
  graph.removeCollection(outer.id)
  graph.removeCollection(base.collectionId)
  assert.equal(graph.variables.size, 0)
})

test('numeric export refuses unsavable graph values and unknown modes without changing its input', async () => {
  for (const value of [NaN, Infinity, -Infinity, 1e39, '4', null, {}, { aliasId: 'missing' }]) {
    const graph = fixture(), variable = named(graph, 'Base'), mode = graph.variableCollections.get(variable.collectionId).defaultModeId
    variable.valuesByMode[mode] = value
    const before = state(graph)
    await assert.rejects(exportFigFile(graph), /Native number:/)
    assert.deepEqual(state(graph), before)
  }
  const graph = fixture(), base = named(graph, 'Base')
  base.valuesByMode['unknown-mode'] = 1
  const before = state(graph)
  await assert.rejects(exportFigFile(graph), /Native number:/)
  assert.deepEqual(state(graph), before)
})

test('numeric metadata validates its entire closed mode mapping without mutating imported inputs', async () => {
  const wire = parseFigBuffer((await exportFigFile(fixture())).slice().buffer)
  const node = wire.nodeChanges.find(node => node.type === 'VARIABLE' && node.name === 'Base')
  const set = wire.nodeChanges.find(node => node.type === 'VARIABLE_SET' && node.name === 'Numeric tokens')
  const id = guid => `${guid.sessionID}:${guid.localID}`
  const collection = { id: id(set.guid), modes: set.variableSetModes.map(mode => ({ modeId: id(mode.id) })) }
  const values = Object.fromEntries(node.variableDataValues.entries.map(entry => [id(entry.modeID), entry.variableData.value.floatValue ?? 0]))
  const original = structuredClone({ node, values, collection })
  const restored = restoreNumbers(node, 'FLOAT', values, collection)
  assert.deepEqual(Object.values(restored), [0.1, 16777217])
  assert.notEqual(restored, values)
  assert.deepEqual({ node, values, collection }, original)
  const data = JSON.parse(node.pluginData.find(entry => entry.key === 'number-values').value)
  for (const change of [
    data => { data.version = 2 },
    data => { data.variableId = '0:999' },
    data => { data.collectionId = '0:999' },
    data => { data.extra = true },
    data => { data.values = [] },
    data => { data.values.pop() },
    data => { data.values.push(data.values[0]) },
    data => { data.values[0].modeId = '0:999' },
    data => { data.values[0].number = 0.1 },
    data => { data.values[0].number = '0.10000000000000001' },
    data => { data.values[0].number = 'NaN' },
    data => { data.values[0].number = '1e+39' },
    data => { data.values[0].number = '1e-999' },
    data => { data.values[0].extra = true },
  ]) {
    const altered = structuredClone(node), metadata = structuredClone(data)
    change(metadata)
    altered.pluginData.find(entry => entry.key === 'number-values').value = JSON.stringify(metadata)
    const before = structuredClone({ altered, values, collection })
    assert.throws(() => restoreNumbers(altered, 'FLOAT', values, collection), /Native number:/)
    assert.deepEqual({ altered, values, collection }, before)
  }
  for (const change of [
    node => { node.pluginData.push(node.pluginData.find(entry => entry.key === 'number-values')) },
    node => { node.variableResolvedType = 'STRING' },
    node => { node.variableDataValues.entries.push(node.variableDataValues.entries[0]) },
    node => { node.variableDataValues.entries.pop() },
    node => { node.pluginData.find(entry => entry.key === 'number-values').value = '{' },
    node => { node.pluginData.find(entry => entry.key === 'number-values').value = ' '.repeat(1048577) },
    node => { node.pluginData.find(entry => entry.key === 'number-values').value = JSON.stringify(data).replace('{', '{"version":0,') },
    node => { node.variableDataValues.entries[0].variableData.dataType = 'ALIAS' },
    node => { node.variableDataValues.entries[0].variableData.value.textValue = '0.1' },
  ]) {
    const altered = structuredClone(node)
    change(altered)
    assert.throws(() => restoreNumbers(altered, 'FLOAT', values, collection), /Native number:/)
  }
  const mode = Object.keys(values)[0]
  assert.throws(() => restoreNumbers(node, 'FLOAT', { ...values, [mode]: 0.2 }, collection), /changed externally/)
  assert.throws(() => restoreNumbers(node, 'STRING', values, collection), /metadata/)
  const legacy = structuredClone(node)
  legacy.pluginData = legacy.pluginData.filter(entry => entry.key !== 'number-values')
  assert.deepEqual(restoreNumbers(legacy, 'FLOAT', values, collection), values, 'legacy fallback precision is not invented')
})

test('numeric graph validation refuses ambiguous ownership and mode fallbacks', () => {
  for (const change of [
    (graph, base, collection) => graph.variableCollections.delete(collection.id),
    (graph, base, collection) => collection.variableIds.push(base.id),
    (graph, base) => graph.createCollection('Conflicting owner').variableIds.push(base.id),
    (graph, base, collection) => collection.modes.push(collection.modes[0]),
    (graph, base, collection) => { collection.defaultModeId = collection.modes[1].modeId },
    (graph, base, collection) => { delete base.valuesByMode[collection.defaultModeId] },
  ]) {
    const graph = fixture(), base = named(graph, 'Base'), collection = graph.variableCollections.get(base.collectionId)
    change(graph, base, collection)
    const before = state(graph)
    assert.throws(() => validateNumericVariables(graph), /Native number:/)
    assert.deepEqual(state(graph), before)
  }
  const graph = fixture(), base = named(graph, 'Base'), outer = graph.createCollection('Outside')
  const alias = graph.createVariable('Outside alias', 'FLOAT', outer.id, { aliasId: base.id })
  validateNumericVariables(graph)
  assert.equal(graph.resolveVariable(alias.id, outer.defaultModeId), 0.1)
  assert.equal(graph.resolveVariable(alias.id, 'numeric-dark'), 16777217)
  base.valuesByMode['numeric-dark'] = { aliasId: alias.id }
  const before = state(graph)
  assert.equal(graph.resolveVariable(alias.id, outer.defaultModeId), 0.1, 'a default-only probe would miss this cycle')
  assert.throws(() => validateNumericVariables(graph), /Native number:/)
  assert.deepEqual(state(graph), before)
})

test('numeric history restores absent modes and refuses stale redo without consuming it', () => {
  const graph = fixture(), base = named(graph, 'Base'), collection = graph.variableCollections.get(base.collectionId)
  const value = graph.createVariable('Default only', 'FLOAT', collection.id, 0.3), editor = createEditor({ graph })
  delete value.valuesByMode['numeric-dark']
  editor.updateVariableValue(value.id, 'numeric-dark', 0.7)
  editor.undoAction()
  assert.equal(Object.hasOwn(value.valuesByMode, 'numeric-dark'), false)
  editor.redoAction()
  assert.equal(graph.resolveVariable(value.id, 'numeric-dark'), 0.7)
  editor.updateVariableValue(base.id, collection.defaultModeId, { aliasId: value.id })
  editor.undoAction()
  value.type = 'STRING'
  const before = state(graph)
  assert.throws(() => editor.redoAction(), /Native number:/)
  assert.deepEqual(state(graph), before)
  value.type = 'FLOAT'
  editor.redoAction()
  assert.equal(graph.resolveVariable(base.id, collection.defaultModeId), 0.3)
})
