import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { computeAllLayouts, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { FigmaAPI } from '@open-pencil/core/figma-api'
import { parseFigBuffer } from '@open-pencil/fig'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

const named = (graph, name) => [...graph.nodes.values()].find(node => node.name === name)
const variable = (graph, name) => [...graph.variables.values()].find(value => value.name === name)
const state = graph => structuredClone({ nodes: [...graph.nodes], variables: [...graph.variables],
  collections: [...graph.variableCollections], modes: [...graph.activeMode] })
const reopen = async graph => parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })

function fixture(instances = false) {
  const graph = new SceneGraph(), collection = graph.createCollection('Dimensions')
  graph.renameMode(collection.id, collection.defaultModeId, 'light')
  graph.addMode(collection.id, 'binding-dark', 'dark')
  const base = graph.createVariable('Spacing', 'FLOAT', collection.id, 8)
  base.valuesByMode['binding-dark'] = 24
  const alias = graph.createVariable('Gap alias', 'FLOAT', collection.id, { aliasId: base.id })
  alias.valuesByMode['binding-dark'] = { aliasId: base.id }
  const page = graph.getPages()[0]
  for (const name of ['Bound', 'Unrelated']) {
    const frame = graph.createNode(instances && name === 'Bound' ? 'COMPONENT' : 'FRAME', page.id, {
      name, layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
      itemSpacing: 8, paddingLeft: 8, paddingRight: 8, paddingTop: 8, paddingBottom: 8,
    })
    for (const name of ['First', 'Second']) graph.createNode('RECTANGLE', frame.id, { name, width: 40, height: 24 })
    if (name === 'Bound') {
      graph.bindVariable(frame.id, 'itemSpacing', alias.id)
      graph.bindVariable(frame.id, 'paddingLeft', alias.id)
    }
  }
  if (instances) for (const [name, mode] of [['Light', collection.defaultModeId], ['Dark', 'binding-dark']]) {
    const parent = graph.createNode('FRAME', page.id, { name: `${name} scope`, width: 300, height: 180,
      variableModes: { [collection.id]: mode } })
    graph.createInstance(named(graph, 'Bound').id, parent.id, { name })
  }
  computeAllLayouts(graph)
  return graph
}

function layout(graph, name, gap) {
  const frame = named(graph, name)
  assert.equal(frame.itemSpacing, gap, `${name}: gap`)
  assert.equal(frame.paddingLeft, gap, `${name}: left padding`)
  assert.equal(frame.width, 88 + 2 * gap, `${name}: intrinsic width`)
  assert.equal(frame.height, 40, `${name}: intrinsic height`)
  assert.deepEqual(graph.getChildren(frame.id).map(node => [node.x, node.y, node.width, node.height]),
    [[gap, 8, 40, 24], [40 + 2 * gap, 8, 40, 24]], `${name}: placed children`)
}

async function settle(graph) {
  await new Promise(setImmediate)
  computeAllLayouts(graph)
}

test('numeric aliases drive live flex geometry and history without changing unbound siblings', async () => {
  const graph = fixture(), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
  const mode = graph.variableCollections.get(base.collectionId).defaultModeId
  const unrelated = structuredClone(named(graph, 'Unrelated'))
  layout(graph, 'Bound', 8)
  editor.updateVariableValue(base.id, mode, 16)
  await settle(graph)
  layout(graph, 'Bound', 16)
  editor.undoAction()
  await settle(graph)
  layout(graph, 'Bound', 8)
  editor.redoAction()
  await settle(graph)
  layout(graph, 'Bound', 16)
  assert.deepEqual(named(graph, 'Unrelated'), unrelated)
})

test('linked numeric consumers keep distinct inherited modes and geometry through edits and two saves', async () => {
  let graph = fixture(true)
  for (let cycle = 0; cycle < 3; cycle++) {
    const editor = createEditor({ graph }), base = variable(graph, 'Spacing')
    const mode = graph.variableCollections.get(base.collectionId).defaultModeId
    editor.updateVariableValue(base.id, mode, 16 + cycle * 4)
    await settle(graph)
    layout(graph, 'Bound', 16 + cycle * 4)
    layout(graph, 'Light', 16 + cycle * 4)
    layout(graph, 'Dark', 24)
    for (const name of ['Light', 'Dark']) {
      const node = named(graph, name)
      assert.equal(graph.getNode(node.componentId)?.name, 'Bound')
      assert.equal(node.boundVariables.itemSpacing, variable(graph, 'Gap alias').id)
      assert.ok(!Object.hasOwn(node.overrides, 'itemSpacing'), 'derived value is not a local override')
    }
    if (cycle < 2) graph = await reopen(graph)
  }
})

test('unbinding freezes the current effective number and undo restores the reference', async () => {
  const graph = fixture(), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
  const mode = graph.variableCollections.get(base.collectionId).defaultModeId, frame = named(graph, 'Bound')
  editor.updateVariableValue(base.id, mode, 16)
  editor.unbindVariable(frame.id, 'itemSpacing')
  await settle(graph)
  assert.equal(frame.itemSpacing, 16)
  assert.ok(!Object.hasOwn(frame.boundVariables, 'itemSpacing'))
  editor.undoAction()
  await settle(graph)
  assert.equal(frame.boundVariables.itemSpacing, variable(graph, 'Gap alias').id)
  layout(graph, 'Bound', 16)
})

test('invalid bound dimensions refuse without graph changes, events or lost redo', () => {
  const graph = fixture(), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
  const mode = graph.variableCollections.get(base.collectionId).defaultModeId
  editor.updateVariableValue(base.id, mode, 16)
  editor.undoAction()
  const before = state(graph)
  let updates = 0, renders = 0
  graph.emitter.on('node:updated', () => updates++)
  editor.onEditorEvent('render:requested', () => renders++)
  assert.throws(() => editor.updateVariableValue(base.id, mode, -1), /Native numeric binding:/)
  assert.deepEqual(state(graph), before)
  assert.equal(updates, 0)
  assert.equal(renders, 0)
  editor.redoAction()
  assert.equal(graph.resolveVariable(base.id), 16)
})

test('numeric width and radius edits change actual pixels before the first save', async () => {
  const graph = new SceneGraph(), collection = graph.createCollection('Geometry')
  const width = graph.createVariable('Width', 'FLOAT', collection.id, 40)
  const radius = graph.createVariable('Radius', 'FLOAT', collection.id, 4)
  const root = graph.createNode('FRAME', graph.getPages()[0].id, { width: 80, height: 48, fills: [] })
  const rect = graph.createNode('RECTANGLE', root.id, { width: 40, height: 40, cornerRadius: 4,
    fills: [{ type: 'SOLID', color: { r: 1, g: 0, b: 0, a: 1 }, opacity: 1, visible: true }] })
  graph.bindVariable(rect.id, 'width', width.id)
  graph.bindVariable(rect.id, 'cornerRadius', radius.id)
  const ck = await initCanvasKit(), surface = ck.MakeSurface(80, 48), renderer = new SkiaRenderer(ck, surface)
  const alpha = (x, y) => {
    const canvas = surface.getCanvas()
    canvas.clear(ck.TRANSPARENT)
    renderer.renderSceneToCanvas(canvas, graph, root.id)
    surface.flush()
    return canvas.readPixels(x, y, { width: 1, height: 1, alphaType: ck.AlphaType.Unpremul,
      colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })[3]
  }
  try {
    assert.equal(alpha(3, 3), 255)
    assert.equal(alpha(50, 20), 0)
    const editor = createEditor({ graph })
    editor.updateVariableValue(width.id, collection.defaultModeId, 64)
    editor.updateVariableValue(radius.id, collection.defaultModeId, 16)
    assert.equal(alpha(3, 3), 0)
    assert.equal(alpha(50, 20), 255)
  } finally { renderer.destroy() }
})

test('active and node-local mode changes reflow through ordinary native actions', async () => {
  const graph = fixture(), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
  const collection = graph.variableCollections.get(base.collectionId), frame = named(graph, 'Bound')
  graph.setActiveMode(collection.id, 'binding-dark')
  layout(graph, 'Bound', 24)
  graph.setActiveMode(collection.id, collection.defaultModeId)
  layout(graph, 'Bound', 8)
  editor.updateNodeWithUndo(frame.id, { variableModes: { [collection.id]: 'binding-dark' } })
  layout(graph, 'Bound', 24)
  editor.undoAction()
  layout(graph, 'Bound', 8)
  editor.redoAction()
  layout(graph, 'Bound', 24)
  new FigmaAPI(graph).setVariableValue(base.id, 'binding-dark', 28)
  await settle(graph)
  layout(graph, 'Bound', 28)
})

test('native numeric export writes current fallback fields without mutating its caller', async () => {
  let graph = fixture(true)
  for (let cycle = 0; cycle < 3; cycle++) {
    const editor = createEditor({ graph }), base = variable(graph, 'Spacing'), gap = 12 + cycle * 4
    editor.updateVariableValue(base.id, graph.variableCollections.get(base.collectionId).defaultModeId, gap)
    await settle(graph)
    const before = state(graph), bytes = await exportFigFile(graph)
    assert.deepEqual(state(graph), before)
    const wire = parseFigBuffer(bytes.slice().buffer).nodeChanges
    for (const [name, value] of [['Bound', gap], ['Light', gap], ['Dark', 24]]) {
      const node = wire.find(node => node.name === name)
      assert.equal(node.stackSpacing, value, `${name}: saved gap`)
      assert.equal(node.stackHorizontalPadding, value, `${name}: saved padding`)
      assert.equal(node.size.x, 88 + 2 * value, `${name}: saved width`)
      assert.deepEqual(node.variableConsumptionMap.entries.map(entry => entry.variableField).sort(),
        ['STACK_PADDING_LEFT', 'STACK_SPACING'])
    }
    if (cycle < 2) graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
  }
})

test('a local numeric rebind retains its value without losing its master or another instance', async () => {
  let graph = fixture(true)
  let editor = createEditor({ graph })
  const base = variable(graph, 'Spacing'), own = graph.createVariable('Own gap', 'FLOAT', base.collectionId, 5)
  editor.bindVariable(named(graph, 'Light').id, 'itemSpacing', own.id)
  for (let cycle = 0; cycle < 3; cycle++) {
    const current = variable(graph, 'Spacing')
    editor.updateVariableValue(current.id, graph.variableCollections.get(current.collectionId).defaultModeId, 16)
    await settle(graph)
    const light = named(graph, 'Light')
    assert.equal(light.itemSpacing, 5)
    assert.equal(light.paddingLeft, 16)
    assert.equal(light.width, 109)
    assert.equal(light.boundVariables.itemSpacing, variable(graph, 'Own gap').id)
    assert.equal(graph.getNode(light.componentId)?.name, 'Bound')
    layout(graph, 'Dark', 24)
    if (cycle < 2) {
      graph = await reopen(graph)
      editor = createEditor({ graph })
    }
  }
})

test('bound literal edits require explicit unbinding while ordinary numeric properties stay editable', () => {
  const graph = fixture(), editor = createEditor({ graph }), node = named(graph, 'Bound')
  const before = state(graph)
  assert.throws(() => editor.updateNodeWithUndo(node.id, { itemSpacing: 12 }), /unbind/)
  assert.deepEqual(state(graph), before)
  editor.unbindVariable(node.id, 'itemSpacing')
  editor.updateNodeWithUndo(node.id, { itemSpacing: 12 })
  assert.equal(node.itemSpacing, 12)
  editor.undoAction()
  assert.equal(node.itemSpacing, 8)
})

test('numeric consumers reject incompatible source units and layout-owned dimensions before binding', () => {
  const graph = fixture(), editor = createEditor({ graph }), base = variable(graph, 'Spacing'), node = named(graph, 'Bound')
  const weight = graph.createVariable('Font weight', 'FLOAT', base.collectionId, 400)
  weight.sourceToken = { version: 1, snapshot: 'a'.repeat(64), kind: 'scale', scale: 'font-weight', key: 'normal', decimal: '400', unit: '' }
  for (const bind of [() => editor.bindVariable(node.id, 'paddingTop', weight.id),
    () => editor.bindVariable(node.id, 'width', base.id)]) {
    const before = state(graph)
    assert.throws(bind, /Native numeric binding:/)
    assert.deepEqual(state(graph), before)
  }
})

test('a failed numeric layout measurement preserves token values, graph events and redo', async () => {
  const graph = fixture(), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
  const frame = named(graph, 'Bound'), previous = getTextMeasurer()
  const text = graph.createNode('TEXT', frame.id, { name: 'Measured', text: 'Measured', width: 40, height: 24,
    textAutoResize: 'WIDTH_AND_HEIGHT' })
  setTextMeasurer(node => ({ width: node.text.length * 5, height: 24 }))
  try {
    await settle(graph)
    const mode = graph.variableCollections.get(base.collectionId).defaultModeId
    editor.updateVariableValue(base.id, mode, 16)
    editor.undoAction()
    await settle(graph)
    const before = state(graph)
    let events = 0
    graph.emitter.on('node:updated', () => events++)
    editor.onEditorEvent('render:requested', () => events++)
    setTextMeasurer(() => { throw new Error('Measured numeric refusal') })
    assert.throws(() => editor.updateVariableValue(base.id, mode, 20), /Measured numeric refusal/)
    assert.deepEqual(state(graph), before)
    assert.equal(events, 0)
    setTextMeasurer(node => ({ width: node.text.length * 5, height: 24 }))
    editor.redoAction()
    assert.equal(graph.resolveVariable(base.id), 16)
    assert.equal(graph.getNode(text.id)?.text, 'Measured')
  } finally { setTextMeasurer(previous) }
})

for (const action of ['bind', 'rebind', 'unbind', 'active mode', 'node mode', 'default mode', 'remove mode', 'restore collection']) {
  test(`numeric ${action} preflights measurement without changing state or consuming redo`, async () => {
    const graph = fixture(), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
    const frame = named(graph, 'Bound'), collection = graph.variableCollections.get(base.collectionId)
    const own = graph.createVariable('Other gap', 'FLOAT', collection.id, 12), previous = getTextMeasurer()
    graph.createNode('TEXT', frame.id, { text: 'Measured', width: 40, height: 24, textAutoResize: 'WIDTH_AND_HEIGHT' })
    const measure = node => ({ width: node.text.length * 5, height: 24 })
    setTextMeasurer(measure)
    try {
      await settle(graph)
      if (action === 'remove mode') editor.setActiveMode(collection.id, 'binding-dark')
      editor.updateVariableValue(base.id, collection.defaultModeId, 16)
      editor.undoAction()
      await settle(graph)
      if (action === 'restore collection') {
        editor.removeCollection(collection.id)
        await settle(graph)
      }
      const operations = {
        bind: () => editor.bindVariable(frame.id, 'paddingTop', own.id),
        rebind: () => editor.bindVariable(frame.id, 'itemSpacing', own.id),
        unbind: () => editor.unbindVariable(frame.id, 'itemSpacing'),
        'active mode': () => editor.setActiveMode(collection.id, 'binding-dark'),
        'node mode': () => editor.updateNodeWithUndo(frame.id, { variableModes: { [collection.id]: 'binding-dark' } }),
        'default mode': () => editor.setDefaultMode(collection.id, 'binding-dark'),
        'remove mode': () => editor.removeMode(collection.id, 'binding-dark'),
        'restore collection': () => editor.undoAction(),
      }
      const before = state(graph)
      let events = 0
      graph.emitter.on('node:updated', () => events++)
      editor.onEditorEvent('render:requested', () => events++)
      setTextMeasurer(() => { throw new Error('Numeric action measurement refused') })
      assert.throws(operations[action], /Numeric action measurement refused/)
      assert.deepEqual(state(graph), before)
      assert.equal(events, 0)
      setTextMeasurer(measure)
      if (action === 'restore collection') {
        editor.undoAction()
        assert.equal(graph.resolveVariable(base.id), 8)
        assert.equal(frame.boundVariables.itemSpacing, variable(graph, 'Gap alias').id)
      } else {
        editor.redoAction()
        assert.equal(graph.resolveVariable(base.id, collection.defaultModeId), 16)
      }
    } finally { setTextMeasurer(previous) }
  })
}

for (const action of ['bind', 'unbind']) for (const direction of ['undo', 'redo']) {
  test(`numeric ${action} ${direction} refuses a failed measurement and remains retryable`, async () => {
    const graph = fixture(), editor = createEditor({ graph }), frame = named(graph, 'Bound')
    const base = variable(graph, 'Spacing'), previous = getTextMeasurer()
    graph.createNode('TEXT', frame.id, { text: 'Measured', width: 40, height: 24, textAutoResize: 'WIDTH_AND_HEIGHT' })
    const measure = node => ({ width: node.text.length * 5, height: 24 })
    setTextMeasurer(measure)
    try {
      await settle(graph)
      if (action === 'bind') editor.bindVariable(frame.id, 'paddingTop', base.id)
      else editor.unbindVariable(frame.id, 'itemSpacing')
      if (direction === 'redo') editor.undoAction()
      await settle(graph)
      const before = state(graph)
      setTextMeasurer(() => { throw new Error('Numeric history measurement refused') })
      assert.throws(() => editor[`${direction}Action`](), /Numeric history measurement refused/)
      assert.deepEqual(state(graph), before)
      setTextMeasurer(measure)
      editor[`${direction}Action`]()
      const field = action === 'bind' ? 'paddingTop' : 'itemSpacing'
      assert.equal(Object.hasOwn(frame.boundVariables, field), (action === 'bind') === (direction === 'redo'))
    } finally { setTextMeasurer(previous) }
  })
}

test('aliases cannot hide contextual source units and mode selection requires an owned identity', () => {
  const graph = fixture(), editor = createEditor({ graph }), frame = named(graph, 'Bound'), base = variable(graph, 'Spacing')
  const weight = graph.createVariable('Weight', 'FLOAT', base.collectionId, 400)
  weight.sourceToken = { kind: 'scale', unit: '' }
  const alias = graph.createVariable('Hidden weight', 'FLOAT', base.collectionId, { aliasId: weight.id })
  for (const action of [
    () => editor.bindVariable(frame.id, 'paddingTop', alias.id),
    () => editor.setActiveMode(base.collectionId, 'missing'),
    () => editor.updateNodeWithUndo(frame.id, { variableModes: { [base.collectionId]: 'missing' } }),
    () => editor.updateVariableValue(base.id, graph.variableCollections.get(base.collectionId).defaultModeId, { aliasId: weight.id }),
  ]) {
    const before = state(graph)
    assert.throws(action, /Native numeric binding:/)
    assert.deepEqual(state(graph), before)
  }
})

test('bound collection removal freezes effective geometry and restoration reconnects every mode', async () => {
  let graph = fixture(true), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
  editor.updateVariableValue(base.id, graph.variableCollections.get(base.collectionId).defaultModeId, 16)
  await settle(graph)
  editor.removeCollection(base.collectionId)
  await settle(graph)
  for (const [name, gap] of [['Bound', 16], ['Light', 16], ['Dark', 24]]) layout(graph, name, gap)
  editor.undoAction()
  await settle(graph)
  graph = await reopen(graph)
  editor = createEditor({ graph })
  base = variable(graph, 'Spacing')
  editor.updateVariableValue(base.id, graph.variableCollections.get(base.collectionId).defaultModeId, 20)
  await settle(graph)
  graph = await reopen(graph)
  layout(graph, 'Light', 20)
  layout(graph, 'Dark', 24)
})

for (const direction of ['HORIZONTAL', 'VERTICAL']) {
  test(`all qualified flex padding and gap consumers stay live in ${direction} layout through two saves`, async () => {
    let graph = new SceneGraph()
    const collection = graph.createCollection('Flex pixels'), base = graph.createVariable('Pixels', 'FLOAT', collection.id, 8)
    const fields = ['itemSpacing', 'counterAxisSpacing', 'paddingLeft', 'paddingRight', 'paddingTop', 'paddingBottom']
    const frame = graph.createNode('FRAME', graph.getPages()[0].id, { name: 'Flex', layoutMode: direction,
      primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG' })
    for (let index = 0; index < 2; index++) graph.createNode('RECTANGLE', frame.id, { width: 40, height: 24 })
    for (const field of fields) graph.bindVariable(frame.id, field, base.id)
    for (let cycle = 0; cycle < 3; cycle++) {
      const token = variable(graph, 'Pixels'), node = named(graph, 'Flex'), gap = 12 + 4 * cycle
      createEditor({ graph }).updateVariableValue(token.id, graph.variableCollections.get(token.collectionId).defaultModeId, gap)
      await settle(graph)
      for (const field of fields) assert.equal(node[field], gap, field)
      assert.deepEqual([node.width, node.height], direction === 'HORIZONTAL' ? [80 + 3 * gap, 24 + 2 * gap] : [40 + 2 * gap, 48 + 3 * gap])
      if (cycle < 2) graph = await reopen(graph)
    }
  })
}

test('fixed width, height and uniform radius retain effective pixels and references through two saves', async () => {
  let graph = new SceneGraph()
  const collection = graph.createCollection('Fixed pixels'), size = graph.createVariable('Size', 'FLOAT', collection.id, 40)
  const radius = graph.createVariable('Radius', 'FLOAT', collection.id, 4)
  const rect = graph.createNode('RECTANGLE', graph.getPages()[0].id, { name: 'Sized', width: 40, height: 40, cornerRadius: 4 })
  for (const field of ['width', 'height']) graph.bindVariable(rect.id, field, size.id)
  graph.bindVariable(rect.id, 'cornerRadius', radius.id)
  for (let cycle = 0; cycle < 3; cycle++) {
    const editor = createEditor({ graph }), token = variable(graph, 'Size'), round = variable(graph, 'Radius')
    const mode = graph.variableCollections.get(token.collectionId).defaultModeId
    editor.updateVariableValue(token.id, mode, 48 + cycle * 8)
    editor.updateVariableValue(round.id, mode, 8 + cycle * 4)
    await settle(graph)
    const node = named(graph, 'Sized')
    assert.deepEqual([node.width, node.height, node.cornerRadius], [48 + cycle * 8, 48 + cycle * 8, 8 + cycle * 4])
    assert.deepEqual(node.boundVariables, { width: token.id, height: token.id, cornerRadius: round.id })
    if (cycle < 2) graph = await reopen(graph)
  }
})

test('parent-owned sizes and conflicting limits cannot silently override a fixed numeric binding', () => {
  const graph = new SceneGraph(), collection = graph.createCollection('Limits')
  const value = graph.createVariable('Size', 'FLOAT', collection.id, 40)
  const frame = graph.createNode('FRAME', graph.getPages()[0].id, { layoutMode: 'HORIZONTAL', width: 300, height: 100 })
  const fill = graph.createNode('RECTANGLE', frame.id, { name: 'Fill', width: 40, height: 24, layoutGrow: 1 })
  const stretch = graph.createNode('RECTANGLE', frame.id, { name: 'Stretch', width: 40, height: 24, layoutAlignSelf: 'STRETCH' })
  const fixed = graph.createNode('RECTANGLE', frame.id, { name: 'Fixed', width: 40, height: 24 })
  const editor = createEditor({ graph })
  for (const [node, field] of [[fill, 'width'], [stretch, 'height']]) {
    const before = state(graph)
    assert.throws(() => editor.bindVariable(node.id, field, value.id), /containing layout/)
    assert.deepEqual(state(graph), before)
  }
  editor.bindVariable(fixed.id, 'width', value.id)
  for (const changes of [{ maxWidth: 32 }, { minWidth: 64 }, { layoutGrow: 1 }]) {
    const before = state(graph)
    assert.throws(() => editor.updateNodeWithUndo(fixed.id, changes), /Native numeric binding:/)
    assert.deepEqual(state(graph), before)
  }
})

test('an instance-owned mode is not replaced by its master after edits, history or two saves', async () => {
  let graph = fixture(true)
  const base = variable(graph, 'Spacing'), dark = named(graph, 'Dark')
  graph.updateNode(named(graph, 'Dark scope').id, { variableModes: {} })
  let editor = createEditor({ graph })
  editor.updateNodeWithUndo(dark.id, { variableModes: { [base.collectionId]: 'binding-dark' } })
  await settle(graph)
  layout(graph, 'Dark', 24)
  editor.undoAction()
  await settle(graph)
  layout(graph, 'Dark', 8)
  editor.redoAction()
  await settle(graph)
  for (let cycle = 0; cycle < 3; cycle++) {
    const token = variable(graph, 'Spacing'), instance = named(graph, 'Dark')
    editor.updateVariableValue(token.id, graph.variableCollections.get(token.collectionId).defaultModeId, 16 + cycle * 4)
    await settle(graph)
    layout(graph, 'Bound', 16 + cycle * 4)
    layout(graph, 'Dark', 24)
    assert.equal(instance.overrides.variableModes, true)
    assert.equal(graph.resolveNumberVariableForNode(instance.id, token.id), 24)
    if (cycle < 2) {
      graph = await reopen(graph)
      editor = createEditor({ graph })
    }
  }
})

test('equal inherited and explicit modes keep different ownership through two saves', async () => {
  let graph = fixture()
  const master = named(graph, 'Bound'), base = variable(graph, 'Spacing')
  master.type = 'COMPONENT'
  graph.updateNode(master.id, { variableModes: { [base.collectionId]: graph.variableCollections.get(base.collectionId).defaultModeId } })
  graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Inherited' })
  graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Explicit', variableModes: { ...master.variableModes } })
  for (let cycle = 0; cycle < 3; cycle++) {
    const editor = createEditor({ graph }), token = variable(graph, 'Spacing'), owner = named(graph, 'Bound')
    const collection = graph.variableCollections.get(token.collectionId), dark = collection.modes.find(mode => mode.name === 'dark').modeId
    editor.updateNodeWithUndo(owner.id, { variableModes: { [collection.id]: dark } })
    await settle(graph)
    layout(graph, 'Inherited', 24)
    layout(graph, 'Explicit', 8)
    editor.undoAction()
    await settle(graph)
    if (cycle < 2) graph = await reopen(graph)
  }
})

test('a nested explicit mode retains its native override path through two saves', async () => {
  let graph = fixture(true)
  const base = variable(graph, 'Spacing'), page = graph.getPages()[0]
  const wrapper = graph.createNode('COMPONENT', page.id, { name: 'Wrapper', width: 300, height: 180 })
  graph.createInstance(named(graph, 'Bound').id, wrapper.id, { name: 'Nested' })
  const placed = graph.createInstance(wrapper.id, page.id, { name: 'Placed wrapper' })
  let editor = createEditor({ graph })
  editor.updateNodeWithUndo(graph.getChildren(placed.id)[0].id, { variableModes: { [base.collectionId]: 'binding-dark' } })
  for (let cycle = 0; cycle < 3; cycle++) {
    const token = variable(graph, 'Spacing'), nested = graph.getChildren(named(graph, 'Placed wrapper').id)[0]
    editor.updateVariableValue(token.id, graph.variableCollections.get(token.collectionId).defaultModeId, 16 + cycle * 4)
    await settle(graph)
    assert.equal(nested.itemSpacing, 24)
    assert.equal(nested.width, 136)
    assert.equal(nested.overrides.variableModes, true)
    if (cycle < 2) {
      graph = await reopen(graph)
      editor = createEditor({ graph })
    }
  }
})

test('a saved master and its instances reopen with current child positions before any editor mutation', async () => {
  let graph = await reopen(fixture(true))
  for (let cycle = 0; cycle < 2; cycle++) {
    const editor = createEditor({ graph }), base = variable(graph, 'Spacing'), gap = 16 + 4 * cycle
    editor.updateVariableValue(base.id, graph.variableCollections.get(base.collectionId).defaultModeId, gap)
    await settle(graph)
    graph = await reopen(graph)
    layout(graph, 'Bound', gap)
    layout(graph, 'Light', gap)
    layout(graph, 'Dark', 24)
  }
})

function modeHistoryFixture(placement) {
  const graph = fixture(), master = named(graph, 'Bound'), base = variable(graph, 'Spacing')
  const page = graph.getPages()[0], collection = graph.variableCollections.get(base.collectionId)
  graph.updateNode(master.id, { variableModes: { [collection.id]: collection.defaultModeId } })
  let source = master
  if (placement !== 'descendant') master.type = 'COMPONENT'
  if (placement !== 'instance') {
    source = graph.createNode('COMPONENT', page.id, { name: 'Mode wrapper', width: 300, height: 180 })
    if (placement === 'descendant') graph.reparentNode(master.id, source.id)
    else graph.createInstance(master.id, source.id, { name: 'Nested spacing' })
  }
  graph.createInstance(source.id, page.id, { name: 'Mode target' })
  graph.createInstance(source.id, page.id, { name: 'Mode explicit' })
  const explicit = modeHistoryTarget(graph, placement, 'Mode explicit').target
  graph.updateNode(explicit.id, { variableModes: { ...master.variableModes } })
  computeAllLayouts(graph)
  return graph
}

function modeHistoryTarget(graph, placement, name = 'Mode target') {
  const root = named(graph, name), target = placement === 'instance' ? root : graph.getChildren(root.id)[0]
  const owner = target.type === 'INSTANCE' ? target : root
  return { target, owner, key: owner.id === target.id ? 'variableModes' : `${target.id}:variableModes` }
}

for (const placement of ['instance', 'nested instance', 'descendant']) {
  test(`local mode history restores ${placement} inheritance through two saves`, async () => {
    let graph = modeHistoryFixture(placement)
    for (let cycle = 0; cycle < 3; cycle++) {
      const editor = createEditor({ graph }), master = named(graph, 'Bound'), base = variable(graph, 'Spacing')
      const collection = graph.variableCollections.get(base.collectionId)
      const { target, owner, key } = modeHistoryTarget(graph, placement)
      const beforeMode = master.variableModes[collection.id]
      const nextMode = collection.modes.find(mode => mode.modeId !== beforeMode).modeId
      const beforeValue = base.valuesByMode[beforeMode], nextValue = base.valuesByMode[nextMode]
      const check = (value, owned) => {
        assert.deepEqual([target.itemSpacing, target.paddingLeft, target.width], [value, value, 88 + 2 * value])
        assert.deepEqual(graph.getChildren(target.id).map(node => node.x), [value, 40 + 2 * value])
        assert.equal(Object.hasOwn(owner.overrides, key), owned, `${placement}: mode ownership`)
        assert.equal(modeHistoryTarget(graph, placement, 'Mode explicit').target.itemSpacing, 8)
      }
      check(beforeValue, false)
      editor.updateNodeWithUndo(target.id, { variableModes: { [collection.id]: nextMode } })
      await settle(graph)
      check(nextValue, true)
      // History owns only its mode key, including on an ancestor instance.
      const opacityKey = owner.id === target.id ? 'opacity' : `${target.id}:opacity`
      owner.overrides[opacityKey] = true
      editor.undoAction()
      await settle(graph)
      check(beforeValue, false)
      assert.equal(owner.overrides[opacityKey], true)
      editor.redoAction()
      await settle(graph)
      check(nextValue, true)
      editor.undoAction()
      await settle(graph)
      editor.updateNode(master.id, { variableModes: { [collection.id]: nextMode } })
      await settle(graph)
      check(nextValue, false)
      if (cycle < 2) graph = await reopen(graph)
    }
  })
}

test('selecting the inherited mode explicitly still has reversible ownership', async () => {
  const graph = modeHistoryFixture('instance'), editor = createEditor({ graph })
  const { target, owner, key } = modeHistoryTarget(graph, 'instance')
  editor.updateNodeWithUndo(target.id, { variableModes: { ...target.variableModes } })
  await settle(graph)
  assert.equal(owner.overrides[key], true)
  editor.undoAction()
  await settle(graph)
  assert.equal(Object.hasOwn(owner.overrides, key), false)
  editor.redoAction()
  await settle(graph)
  assert.equal(owner.overrides[key], true)
})

for (const placement of ['instance', 'nested instance', 'descendant']) {
  test(`undo resumes the current master mode for the ${placement}, not a stale copied value`, async () => {
    const graph = modeHistoryFixture(placement), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
    const { target, owner, key } = modeHistoryTarget(graph, placement)
    const dark = { [base.collectionId]: 'binding-dark' }
    editor.updateNodeWithUndo(target.id, { variableModes: dark })
    editor.updateNode(named(graph, 'Bound').id, { variableModes: dark })
    await settle(graph)
    editor.undoAction()
    await settle(graph)
    assert.equal(Object.hasOwn(owner.overrides, key), false)
    assert.deepEqual([target.paddingLeft, target.itemSpacing, target.width], [24, 24, 136])
    editor.redoAction()
    await settle(graph)
    assert.equal(owner.overrides[key], true)
    assert.deepEqual([target.paddingLeft, target.itemSpacing, target.width], [24, 24, 136])
  })

  test(`clearing the ${placement} mode resumes inheritance and retains its previous explicit history`, async () => {
    const graph = modeHistoryFixture(placement), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
    const { target, owner, key } = modeHistoryTarget(graph, placement, 'Mode explicit')
    editor.updateNode(named(graph, 'Bound').id, { variableModes: { [base.collectionId]: 'binding-dark' } })
    await settle(graph)
    assert.equal(target.itemSpacing, 8)
    editor.updateNodeWithUndo(target.id, { variableModes: {} })
    assert.deepEqual([target.paddingLeft, target.itemSpacing, target.width], [24, 24, 136])
    await settle(graph)
    assert.equal(Object.hasOwn(owner.overrides, key), false)
    assert.deepEqual([target.paddingLeft, target.itemSpacing, target.width], [24, 24, 136])
    editor.undoAction()
    await settle(graph)
    assert.equal(owner.overrides[key], true)
    assert.equal(target.itemSpacing, 8)
    editor.redoAction()
    await settle(graph)
    assert.equal(Object.hasOwn(owner.overrides, key), false)
    assert.equal(target.itemSpacing, 24)
  })
}

for (const missing of [false, true]) {
  test(`mode history refuses a ${missing ? 'missing target' : 'changed override owner'} without consuming the entry`, async () => {
    const graph = modeHistoryFixture('descendant'), editor = createEditor({ graph }), base = variable(graph, 'Spacing')
    const { target, owner } = modeHistoryTarget(graph, 'descendant')
    editor.updateNodeWithUndo(target.id, { variableModes: { [base.collectionId]: 'binding-dark' } })
    await settle(graph)
    if (missing) graph.deleteNode(target.id)
    else graph.reparentNode(target.id, graph.getPages()[0].id)
    const before = state(graph)
    let events = 0
    graph.emitter.on('node:updated', () => events++)
    editor.onEditorEvent('render:requested', () => events++)
    assert.throws(() => editor.undoAction(), /mode history (target no longer exists|owner changed)/)
    assert.deepEqual(state(graph), before)
    assert.equal(events, 0)
    if (!missing) {
      graph.reparentNode(target.id, owner.id)
      await settle(graph)
      editor.undoAction()
      await settle(graph)
      assert.equal(Object.hasOwn(owner.overrides, `${target.id}:variableModes`), false)
      assert.equal(target.itemSpacing, 8)
    } else await settle(graph)
  })
}

for (const direction of ['undo', 'redo']) {
  test(`local mode ${direction} retains ownership and retryable history when measurement fails`, async () => {
    const graph = modeHistoryFixture('instance'), editor = createEditor({ graph })
    const { target, owner, key } = modeHistoryTarget(graph, 'instance'), base = variable(graph, 'Spacing')
    const previous = getTextMeasurer(), measure = node => ({ width: node.text.length * 5, height: 24 })
    graph.createNode('TEXT', target.id, { text: 'Measured', width: 40, height: 24, textAutoResize: 'WIDTH_AND_HEIGHT' })
    setTextMeasurer(measure)
    try {
      await settle(graph)
      editor.updateNodeWithUndo(target.id, { variableModes: { [base.collectionId]: 'binding-dark' } })
      if (direction === 'redo') editor.undoAction()
      await settle(graph)
      const before = state(graph)
      let events = 0
      graph.emitter.on('node:updated', () => events++)
      editor.onEditorEvent('render:requested', () => events++)
      setTextMeasurer(() => { throw new Error('Mode history measurement refused') })
      assert.throws(() => editor[`${direction}Action`](), /Mode history measurement refused/)
      assert.deepEqual(state(graph), before)
      assert.equal(events, 0)
      setTextMeasurer(measure)
      editor[`${direction}Action`]()
      await settle(graph)
      assert.equal(Object.hasOwn(owner.overrides, key), direction === 'redo')
      assert.equal(target.itemSpacing, direction === 'redo' ? 24 : 8)
    } finally { setTextMeasurer(previous) }
  })
}
