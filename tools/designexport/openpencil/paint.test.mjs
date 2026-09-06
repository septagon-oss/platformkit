import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph, generateId } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

const color = (r, g, b) => ({ r: r / 255, g: g / 255, b: b / 255, a: 1 })
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const variable = (graph, name) => [...graph.variables.values()].find(value => value.name === name)
const icon = (graph, owner) => graph.getChildren(owner.id)[0]
const vector = (graph, owner) => graph.getChildren(icon(graph, owner).id)[0]
const subtree = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => subtree(graph, child))]

function fixture() {
  const graph = new SceneGraph(), collection = graph.createCollection('Palette'), dark = generateId()
  graph.renameMode(collection.id, collection.defaultModeId, 'light')
  graph.addMode(collection.id, dark, 'dark')
  for (const [name, light, night] of [['Ink', color(179, 32, 48), color(240, 112, 128)],
    ['Accent', color(16, 80, 160), color(80, 176, 240)]]) {
    const item = graph.createVariable(name, 'COLOR', collection.id, light)
    graph.addVariable({ ...item, valuesByMode: { [collection.defaultModeId]: light, [dark]: night } })
  }
  const page = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', page.id, { name: 'Glyph', width: 24, height: 24, fills: [] })
  const path = graph.createNode('VECTOR', master.id, { width: 24, height: 24,
    fills: [{ type: 'SOLID', color: color(179, 32, 48), opacity: 1, visible: true }],
    strokes: [{ color: color(179, 32, 48), weight: 2, align: 'CENTER', opacity: 1, visible: true, cap: 'NONE', join: 'MITER' }],
    vectorNetwork: { vertices: [{ x: 4, y: 4 }, { x: 20, y: 4 }, { x: 20, y: 20 }, { x: 4, y: 20 }],
      segments: [0, 1, 2, 3].map(start => ({ start, end: (start + 1) % 4,
        tangentStart: { x: 0, y: 0 }, tangentEnd: { x: 0, y: 0 } })),
      regions: [{ windingRule: 'NONZERO', loops: [[0, 1, 2, 3]] }] } })
  for (const field of ['fills/0/color', 'strokes/0/color']) graph.bindVariable(path.id, field, variable(graph, 'Ink').id)
  const control = graph.createNode('COMPONENT', page.id, { name: 'Control', width: 20, height: 20, fills: [] })
  graph.createInstance(master.id, control.id, { uniformScaleFactor: 20 / 24 })
  graph.bindVariable(vector(graph, control).id, 'fills/0/color', variable(graph, 'Accent').id)
  for (const mode of collection.modes) {
    const target = graph.addPage(mode.name)
    graph.updateNode(target.id, { variableModes: { [collection.id]: mode.modeId } })
    for (const role of ['Modified', 'Reference']) graph.createInstance(control.id, target.id, { name: `${role} ${mode.name}` })
  }
  return graph
}

function modify(graph) {
  for (const mode of ['light', 'dark']) {
    const path = vector(graph, named(graph, `Modified ${mode}`))
    graph.bindVariable(path.id, 'fills/0/color', variable(graph, 'Ink').id)
    graph.bindVariable(path.id, 'strokes/0/color', variable(graph, 'Accent').id)
  }
}

function checkBindings(graph) {
  const master = named(graph, 'Glyph')
  assert.equal(graph.getChildren(master.id)[0].boundVariables['fills/0/color'], variable(graph, 'Ink').id)
  for (const mode of ['light', 'dark']) for (const role of ['Modified', 'Reference']) {
    const owner = named(graph, `${role} ${mode}`), nested = icon(graph, owner), path = vector(graph, owner)
    let source = nested
    while (source.type === 'INSTANCE') source = graph.getNode(source.componentId)
    assert.equal(source, master)
    assert.equal(nested.uniformScaleFactor, Math.fround(20 / 24))
    assert.equal(path.boundVariables['fills/0/color'], variable(graph, role === 'Modified' ? 'Ink' : 'Accent').id)
    assert.equal(path.boundVariables['strokes/0/color'], variable(graph, role === 'Modified' ? 'Accent' : 'Ink').id)
  }
}

async function pixels(graph, owner) {
  const ck = await initCanvasKit(), surface = ck.MakeSurface(128, 128), renderer = new SkiaRenderer(ck, surface)
  try {
    const canvas = surface.getCanvas(), nested = icon(graph, owner)
    canvas.clear(ck.TRANSPARENT)
    canvas.scale(128 / nested.width, 128 / nested.height)
    renderer.renderSceneToCanvas(canvas, graph, nested.id)
    surface.flush()
    return Buffer.from(canvas.readPixels(0, 0, { width: 128, height: 128, alphaType: ck.AlphaType.Unpremul,
      colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }))
  } finally { renderer.destroy() }
}

test('containing-component sync preserves nested native color overrides and sibling independence', () => {
  const graph = fixture()
  const unaffected = ['Glyph', 'Control', 'Reference light', 'Reference dark'].flatMap(name => subtree(graph, named(graph, name)))
  const before = structuredClone(unaffected)
  modify(graph)
  const nested = icon(graph, named(graph, 'Modified light'))
  const overrides = { ...nested.overrides, opacity: true }
  graph.updateNode(nested.id, { opacity: 0.5, overrides })
  assert.deepEqual(unaffected, before)
  checkBindings(graph)
  graph.syncInstances(named(graph, 'Glyph').id)
  checkBindings(graph)
  graph.syncInstances(named(graph, 'Control').id)
  checkBindings(graph)
  assert.equal(nested.opacity, 0.5, 'nearest-instance root override survives containing sync')
  assert.deepEqual(nested.overrides, overrides, 'planning does not rewrite authored ownership')
})

test('nested fill and stroke variable overrides retain native pixels through two FIG saves and palette edits', async () => {
  let graph = fixture()
  modify(graph)
  const baseline = new Map()
  for (let cycle = 0; cycle < 3; cycle++) {
    checkBindings(graph)
    graph.syncInstances(named(graph, 'Control').id)
    graph.syncInstances(named(graph, 'Glyph').id)
    checkBindings(graph)
    for (const mode of ['light', 'dark']) for (const role of ['Modified', 'Reference']) {
      const name = `${role} ${mode}`, image = await pixels(graph, named(graph, name))
      if (cycle === 0) baseline.set(name, image)
      else assert.deepEqual(image, baseline.get(name), `${name}: FIG cycle ${cycle}`)
    }
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
  const accent = variable(graph, 'Accent'), changed = color(32, 192, 64)
  graph.addVariable({ ...accent, valuesByMode: Object.fromEntries(Object.keys(accent.valuesByMode).map(mode => [mode, changed])) })
  checkBindings(graph)
  for (const mode of ['light', 'dark']) for (const role of ['Modified', 'Reference']) {
    const name = `${role} ${mode}`, image = await pixels(graph, named(graph, name))
    assert.notDeepEqual(image, baseline.get(name), 'changed native token reaches its bound paint')
    const x = role === 'Modified' ? 21 : 64, offset = (64 * 128 + x) * 4
    assert.deepEqual([...image.subarray(offset, offset + 4)], [32, 192, 64, 255])
  }
  const master = named(graph, 'Glyph'), path = graph.getChildren(master.id)[0]
  graph.updateNode(path.id, { strokes: path.strokes.map(stroke => ({ ...stroke, weight: 4 })) })
  graph.syncInstances(master.id)
  for (const mode of ['light', 'dark']) for (const role of ['Modified', 'Reference']) {
    const owner = named(graph, `${role} ${mode}`)
    assert.equal(vector(graph, owner).strokes[0].weight, 4 * icon(graph, owner).uniformScaleFactor,
      'a binding-only edit must not acquire stroke geometry ownership on import')
  }
})

for (const field of ['fills', 'strokes']) for (const cleared of [false, true]) test(`literal ${field}, cleared=${cleared}, survive unbind, two FIG saves and sync`, async () => {
  let graph = fixture()
  modify(graph)
  const literal = { r: 1, g: 0, b: 0, a: 1 }
  const other = field === 'fills' ? 'strokes' : 'fills'
  const otherVariable = field === 'fills' ? 'Accent' : 'Ink'
  for (const mode of ['light', 'dark']) {
    const owner = named(graph, `Modified ${mode}`), nested = icon(graph, owner), path = vector(graph, owner)
    graph.unbindVariable(path.id, `${field}/0/color`)
    graph.updateNode(path.id, { [field]: cleared ? [] : [{ ...path[field][0], color: literal }], boundVariables: { ...path.boundVariables } })
    graph.updateNode(nested.id, { overrides: { ...nested.overrides, [`${path.id}:${field}`]: true } })
  }
  for (let cycle = 0; cycle < 3; cycle++) {
    graph.syncInstances(named(graph, 'Control').id)
    graph.syncInstances(named(graph, 'Glyph').id)
    for (const mode of ['light', 'dark']) {
      const owner = named(graph, `Modified ${mode}`), path = vector(graph, owner)
      assert.equal(path.boundVariables[`${field}/0/color`], undefined, `${field}: no resurrected binding in cycle ${cycle}`)
      assert.equal(path.boundVariables[`${other}/0/color`], variable(graph, otherVariable).id)
      assert.equal(path[field].length, cleared ? 0 : 1)
      if (!cleared) {
        assert.deepEqual(path[field][0].color, literal)
        const image = await pixels(graph, owner), x = field === 'fills' ? 64 : 21, offset = (64 * 128 + x) * 4
        assert.deepEqual([...image.subarray(offset, offset + 4)], [255, 0, 0, 255])
      }
      const sibling = vector(graph, named(graph, `Reference ${mode}`))
      assert.equal(sibling.boundVariables['fills/0/color'], variable(graph, 'Accent').id)
      assert.equal(sibling.boundVariables['strokes/0/color'], variable(graph, 'Ink').id)
    }
    const canonical = graph.getChildren(named(graph, 'Glyph').id)[0]
    for (const slot of ['fills', 'strokes']) assert.equal(canonical.boundVariables[`${slot}/0/color`], variable(graph, 'Ink').id)
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
})
