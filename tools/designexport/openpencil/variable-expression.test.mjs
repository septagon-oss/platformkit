import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import { restoreCSSColors, serializeCSSColors } from './variable-color.mjs'

const rgba = (r, g, b, a = 1) => ({ r, g, b, a })
const named = (graph, name) => [...graph.variables.values()].find(variable => variable.name === name)
const close = (actual, expected) => {
  assert.ok(actual && typeof actual === 'object')
  assert.deepEqual(Object.keys(actual).sort(), ['a', 'b', 'g', 'r'])
  for (const channel of ['r', 'g', 'b', 'a']) assert.ok(Math.abs(actual[channel] - expected[channel]) < 1e-6,
    `${channel}: ${actual[channel]} != ${expected[channel]}`)
}

function fixture() {
  const graph = new SceneGraph(), collection = graph.createCollection('Source palette')
  graph.renameMode(collection.id, collection.defaultModeId, 'light')
  graph.addMode(collection.id, 'night-mode', 'dark')
  const ink = graph.createVariable('Ink', 'COLOR', collection.id, rgba(0, 0, 0))
  const paper = graph.createVariable('Paper', 'COLOR', collection.id, rgba(1, 1, 1))
  ink.valuesByMode['night-mode'] = rgba(1, 0, 0, .5)
  paper.valuesByMode['night-mode'] = rgba(0, 0, 1)
  // Non-GUID IDs force the exporter to remap every formula input.
  graph.removeVariable(ink.id)
  graph.addVariable({ ...ink, id: 'source-ink' })
  const derived = graph.createVariable('Secondary', 'COLOR', collection.id, {
    cssColor: {
      value: 'color-mix(in srgb, var(--ink) 75%, var(--surface))',
      customProperties: {
        '--ink': { aliasId: 'source-ink' }, '--surface': 'var(--paper)', '--paper': { aliasId: paper.id },
      },
    },
  })
  derived.description = 'Editable description · João\nNot expression metadata'
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Master', width: 20, height: 20,
    fills: [{ type: 'SOLID', color: rgba(.25, .25, .25), opacity: 1, visible: true }] })
  graph.bindVariable(master.id, 'fills/0/color', derived.id)
  for (const mode of collection.modes) {
    const page = graph.addPage(mode.name)
    graph.updateNode(page.id, { variableModes: { [collection.id]: mode.modeId } })
    graph.createInstance(master.id, page.id, { name: mode.name })
  }
  return graph
}

function resolved(graph, mode) {
  const node = [...graph.getAllNodes()].find(node => node.type === 'INSTANCE' && node.name === mode)
  assert.equal(node.boundVariables['fills/0/color'], named(graph, 'Secondary').id)
  assert.equal(graph.getNode(node.componentId).name, 'Master')
  return graph.resolveColorVariableForNode(node.id, node.boundVariables['fills/0/color'])
}

test('native CSS color values follow IDs, modes and ordinary palette edit history without rewriting nodes', async () => {
  let graph = fixture()
  for (let cycle = 0; cycle < 3; cycle++) {
    const nodes = structuredClone([...graph.nodes]), variables = structuredClone([...graph.variables])
    close(resolved(graph, 'light'), rgba(.25, .25, .25))
    close(resolved(graph, 'dark'), rgba(.6, 0, .4, .625))
    const actions = createEditor({ graph }), ink = named(graph, 'Ink')
    const collection = graph.variableCollections.get(ink.collectionId), light = collection.defaultModeId
    actions.updateVariableValue(ink.id, light, rgba(0, 1, 0, .5))
    close(resolved(graph, 'light'), rgba(.4, 1, .4, .625))
    close(resolved(graph, 'dark'), rgba(.6, 0, .4, .625))
    actions.undoAction()
    close(resolved(graph, 'light'), rgba(.25, .25, .25))
    actions.redoAction()
    close(resolved(graph, 'light'), rgba(.4, 1, .4, .625))
    actions.undoAction()
    actions.renameVariable(ink.id, 'No CSS name lookup')
    close(resolved(graph, 'light'), rgba(.25, .25, .25))
    actions.undoAction()
    const derived = named(graph, 'Secondary'), formula = structuredClone(derived.valuesByMode[light])
    actions.updateVariableValue(derived.id, light, rgba(1, 0, 1))
    close(resolved(graph, 'light'), rgba(1, 0, 1))
    close(resolved(graph, 'dark'), rgba(.6, 0, .4, .625))
    actions.undoAction()
    assert.deepEqual(derived.valuesByMode[light], formula)
    close(resolved(graph, 'light'), rgba(.25, .25, .25))
    assert.deepEqual([...graph.nodes], nodes, 'resolution and variable history leave masters and instances unchanged')
    assert.deepEqual([...graph.variables], variables)
    if (cycle === 2) break
    const bytes = await exportFigFile(graph), wire = parseFigBuffer(bytes.slice().buffer)
    const entry = wire.nodeChanges.find(node => node.type === 'VARIABLE' && node.name === 'Secondary')
    assert.equal(entry.description, derived.description)
    assert.equal(entry.variableResolvedType, 'COLOR')
    assert.ok(entry.variableDataValues.entries.every(mode => mode.variableData.dataType === 'COLOR'))
    assert.ok(entry.pluginData.some(item => item.pluginID === 'platformkit' && item.key === 'css-color-values'))
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    const next = named(graph, 'Secondary'), nextInk = named(graph, 'Ink'), nextPaper = named(graph, 'Paper')
    for (const value of Object.values(next.valuesByMode)) {
      assert.equal(value.cssColor.value, formula.cssColor.value)
      assert.deepEqual(value.cssColor.customProperties, {
        '--ink': { aliasId: nextInk.id }, '--surface': 'var(--paper)', '--paper': { aliasId: nextPaper.id },
      })
    }
    assert.equal(next.description, derived.description)
    assert.notEqual(nextInk.id, 'source-ink')
  }
})

test('native expressions distinguish repeated dependencies from cycles and refuse missing or invalid inputs', async () => {
  const graph = fixture(), derived = named(graph, 'Secondary'), ink = named(graph, 'Ink')
  const mode = graph.variableCollections.get(derived.collectionId).defaultModeId
  const value = derived.valuesByMode[mode]
  value.cssColor.value = 'color-mix(in srgb, var(--ink), var(--ink))'
  close(graph.resolveVariable(derived.id, mode), rgba(0, 0, 0))
  value.cssColor.customProperties['--ink'] = { aliasId: derived.id }
  assert.throws(() => graph.resolveVariable(derived.id, mode), /CSS color/)
  value.cssColor.customProperties['--ink'] = { aliasId: 'missing-native-variable' }
  assert.throws(() => graph.resolveVariable(derived.id, mode), /CSS color/)
  await assert.rejects(exportFigFile(graph), /CSS color/)
  value.cssColor.customProperties['--ink'] = { aliasId: ink.id }
  ink.valuesByMode[mode] = { aliasId: derived.id }
  assert.throws(() => graph.resolveVariable(derived.id, mode), /CSS color/)
  ink.valuesByMode[mode] = 'not a color'
  assert.throws(() => graph.resolveVariable(derived.id, mode), /CSS color/)
  ink.valuesByMode[mode] = rgba(1, 0, 0, 0)
  close(graph.resolveVariable(derived.id, mode), rgba(0, 0, 0, 0))
  value.cssColor.value = 'color-mix(in oklab, var(--ink), #fff)'
  assert.throws(() => graph.resolveVariable(derived.id, mode), /sRGB/)
})

test('FIG expression metadata refuses changed fallbacks, malformed records and stale mode identities', async () => {
  const graph = fixture(), bytes = await exportFigFile(graph)
  const node = parseFigBuffer(bytes.slice().buffer).nodeChanges.find(node => node.name === 'Secondary')
  const values = Object.fromEntries(node.variableDataValues.entries.map(entry =>
    [`${entry.modeID.sessionID}:${entry.modeID.localID}`, entry.variableData.value.colorValue]))
  const metadata = JSON.parse(node.pluginData[0].value), mode = metadata.values[0].modeId
  const restored = restoreCSSColors(node, 'COLOR', values)
  assert.ok(restored[mode].cssColor)
  assert.deepEqual(restoreCSSColors({ pluginData: [] }, 'COLOR', values), values)
  const before = structuredClone({ node, values })
  for (const mutate of [
    data => { data.version = 2 }, data => { data.values = null }, data => { data.values = [] },
    data => { data.values.push(structuredClone(data.values[0])) },
    data => { data.values[0].modeId = 'unknown' },
    data => { data.values[0].color.r = .99 },
    data => { data.values[0].cssColor = null },
    data => { data.values[0].cssColor.customProperties['--ink'] = { aliasId: 'no-guid' } },
  ]) {
    const edited = structuredClone(metadata)
    mutate(edited)
    assert.throws(() => restoreCSSColors({ ...node, pluginData: [{ ...node.pluginData[0], value: JSON.stringify(edited) }] },
      'COLOR', values), /Native CSS color/)
  }
  assert.throws(() => restoreCSSColors({ ...node, pluginData: [...node.pluginData, ...node.pluginData] }, 'COLOR', values), /metadata/)
  assert.throws(() => restoreCSSColors(node, 'FLOAT', values), /metadata/)
  const derived = named(graph, 'Secondary'), collection = graph.variableCollections.get(derived.collectionId)
  assert.throws(() => serializeCSSColors(graph, derived, new Map(), new Map()), /missing exported identity/)
  const guid = { sessionID: 5, localID: 10 }
  assert.throws(() => serializeCSSColors(graph, derived, new Map(), new Map([[collection.defaultModeId, guid]])), /missing exported identity/)
  assert.deepEqual({ node, values }, before, 'refusals preserve caller input')
})

test('native formula fan-out and depth are bounded without contaminating later resolutions', () => {
  const graph = fixture(), collection = [...graph.variableCollections.values()][0]
  let previous = named(graph, 'Ink')
  for (let index = 0; index < 16; index++) {
    previous = graph.createVariable(`Derived ${index}`, 'COLOR', collection.id, {
      cssColor: { value: 'color-mix(in srgb, var(--a), var(--b))',
        customProperties: { '--a': { aliasId: previous.id }, '--b': { aliasId: previous.id } } },
    })
  }
  assert.throws(() => graph.resolveVariable(previous.id), /resolution limit/)
  close(resolved(graph, 'light'), rgba(.25, .25, .25))
  previous = named(graph, 'Ink')
  for (let index = 0; index < 66; index++) {
    previous = graph.createVariable(`Alias ${index}`, 'COLOR', collection.id, { aliasId: previous.id })
  }
  assert.throws(() => graph.resolveVariable(previous.id), /resolution limit/)
  close(resolved(graph, 'dark'), rgba(.6, 0, .4, .625))
})

test('native formula-bound instance pixels retain premultiplied light and dark colors through two FIG saves', async () => {
  let graph = fixture()
  const ck = await initCanvasKit()
  for (let cycle = 0; cycle < 3; cycle++) {
    for (const opacity of [1, .5])
    for (const [mode, expected] of [['light', [64, 64, 64, 255]], ['dark', [153, 0, 102, 159]]]) {
      const instance = [...graph.getAllNodes()].find(node => node.type === 'INSTANCE' && node.name === mode)
      graph.updateNode(instance.id, { fills: instance.fills.map(fill => ({ ...fill, opacity })) })
      const surface = ck.MakeSurface(20, 20), renderer = new SkiaRenderer(ck, surface)
      try {
        const canvas = surface.getCanvas()
        canvas.clear(ck.TRANSPARENT)
        renderer.renderSceneToCanvas(canvas, graph, instance.parentId)
        surface.flush()
        const pixel = canvas.readPixels(10, 10, { width: 1, height: 1, alphaType: ck.AlphaType.Unpremul,
          colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
        const expectedPixel = expected.map((value, index) => index === 3 ? value * opacity : value)
        expectedPixel.forEach((value, index) => assert.ok(Math.abs(pixel[index] - value) <= 1, `${mode}: ${pixel}`))
      } finally { renderer.destroy() }
    }
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
})
