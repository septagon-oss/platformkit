import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { computeAllLayouts } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { parseFigBuffer } from '@open-pencil/fig'
import { buildFoundation } from './foundation.mjs'

const snapshot = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
  cwd: new URL('../../../', import.meta.url), encoding: 'utf8',
}))
const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor">' +
  '<circle cx="5.5" cy="11.25" r="3.25" fill="#fedcba"/>' +
  '<path d="M12.25 5.5C18 4 15 16 20.5 18.25" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"/>' +
  '<path d="M14 19.25h5.5v2.25h-5.5Z"/></svg>'
snapshot.icons.push({ name: 'uniform-scale-fixture', svg, source: 'platformkit/native-conformance-fixture',
  license: 'Apache-2.0', sha256: createHash('sha256').update(svg).digest('hex') })
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const masterOf = graph => [...graph.getAllNodes()].find(node => node.type === 'COMPONENT' && node.name === 'uniform-scale-fixture')
const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-5, `${actual} != ${expected}`)

function checkScale(graph, instance, master, factor, storedFactor = factor) {
  close(instance.width, master.width * factor)
  close(instance.height, master.height * factor)
  assert.equal(instance.uniformScaleFactor, storedFactor)
  for (const [index, child] of graph.getChildren(instance.id).entries()) {
    const source = graph.getChildren(master.id)[index]
    for (const key of ['x', 'y', 'width', 'height']) close(child[key], source[key] * factor)
    for (const [i, vertex] of child.vectorNetwork.vertices.entries()) {
      close(vertex.x, source.vectorNetwork.vertices[i].x * factor)
      close(vertex.y, source.vectorNetwork.vertices[i].y * factor)
    }
    for (const [i, segment] of child.vectorNetwork.segments.entries()) for (const key of ['tangentStart', 'tangentEnd']) {
      close(segment[key].x, source.vectorNetwork.segments[i][key].x * factor)
      close(segment[key].y, source.vectorNetwork.segments[i][key].y * factor)
    }
    assert.deepEqual(child.vectorNetwork.regions, source.vectorNetwork.regions)
    for (const [i, stroke] of child.strokes.entries()) {
      close(stroke.weight, source.strokes[i].weight * factor)
      assert.equal(stroke.cap, source.strokes[i].cap)
      assert.equal(stroke.join, source.strokes[i].join)
    }
    assert.deepEqual(child.fills, source.fills)
    assert.deepEqual(child.boundVariables, source.boundVariables)
  }
}

async function pixels(graph, instance) {
  const ck = await initCanvasKit(), surface = ck.MakeSurface(128, 128)
  const renderer = new SkiaRenderer(ck, surface)
  try {
    const canvas = surface.getCanvas()
    canvas.clear(ck.TRANSPARENT)
    canvas.scale(128 / instance.width, 128 / instance.height)
    renderer.renderSceneToCanvas(canvas, graph, instance.id)
    surface.flush()
    return Buffer.from(canvas.readPixels(0, 0, { width: 128, height: 128,
      alphaType: ck.AlphaType.Unpremul, colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }))
  } finally { renderer.destroy() }
}

for (const size of [20, 16]) test(`native uniform scale ${size}/24 retains live master links, HUG layout and two FIG saves`, async () => {
  let graph = buildFoundation(snapshot), master = masterOf(graph)
  const page = graph.addPage('Scaled icon conformance')
  const parent = graph.createNode('COMPONENT', page.id, { name: 'Composed master', layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG', itemSpacing: 8, fills: [] })
  const original = structuredClone([master, ...graph.getChildren(master.id)])
  const sibling = graph.createInstance(master.id, page.id, { name: 'Unscaled sibling' })
  const siblingBefore = structuredClone([sibling, ...graph.getChildren(sibling.id)])
  const factor = Math.fround(size / 24)
  let scaled = graph.createInstance(master.id, parent.id, { name: 'Scaled icon', uniformScaleFactor: size / 24 })
  close(scaled.width, size)
  checkScale(graph, scaled, master, factor)
  assert.deepEqual([master, ...graph.getChildren(master.id)], original)
  assert.deepEqual([sibling, ...graph.getChildren(sibling.id)], siblingBefore)
  const spacer = graph.createNode('RECTANGLE', parent.id, { name: 'Spacer', width: 10, height: 10 })
  const outer = graph.createInstance(parent.id, page.id, { name: 'Composed instance' })
  computeAllLayouts(graph, page.id)
  graph.updateNode(spacer.id, { width: 30 })
  graph.syncInstances(parent.id)
  computeAllLayouts(graph, page.id)
  close(parent.width, scaled.width + 38)
  close(outer.width, parent.width)
  checkScale(graph, graph.getChildren(outer.id)[0], master, factor)
  const sourcePath = graph.getChildren(master.id)[1]
  const changed = structuredClone(sourcePath.vectorNetwork)
  changed.vertices[0].x += 0.25
  graph.updateNode(sourcePath.id, { vectorNetwork: changed, strokes: sourcePath.strokes.map(stroke => ({ ...stroke, weight: 2.25 })),
    boundVariables: { ...sourcePath.boundVariables } })
  graph.syncInstances(master.id)
  let baseline
  for (let cycle = 0; cycle < 3; cycle++) {
    master = masterOf(graph)
    assert.equal(graph.getChildren(master.id)[1].strokes[0].cap, 'ROUND')
    assert.equal(graph.getChildren(master.id)[1].strokes[0].join, 'ROUND')
    scaled = graph.getChildren(named(graph, 'Composed master').id)[0]
    checkScale(graph, scaled, master, factor)
    checkScale(graph, graph.getChildren(named(graph, 'Composed instance').id)[0], master, factor)
    const image = await pixels(graph, scaled)
    if (cycle === 0) baseline = image
    else assert.deepEqual(image, baseline, `rendered cycle ${cycle}`)
    if (cycle < 2) {
      const encoded = await exportFigFile(graph), raw = parseFigBuffer(encoded.slice().buffer)
      assert.ok(raw.nodeChanges.some(node => node.symbolData?.uniformScaleFactor === factor))
      graph = await parseFigFile(encoded.slice().buffer, { populate: 'all' })
    }
  }
})

test('FIG refuses conflicting per-stroke cap and join styles instead of silently changing them', async () => {
  for (const field of ['cap', 'join']) {
    const graph = buildFoundation(snapshot), path = graph.getChildren(masterOf(graph).id)[1]
    graph.updateNode(path.id, { strokes: [...path.strokes, { ...path.strokes[0], [field]: field === 'cap' ? 'SQUARE' : 'BEVEL' }] })
    await assert.rejects(exportFigFile(graph), /conflicting native stroke styles/i)
  }
})

test('factor edits propagate through composition and clearing survives imported source metadata', async () => {
  let graph = buildFoundation(snapshot), master = masterOf(graph)
  const page = graph.addPage('Editable scale')
  const parent = graph.createNode('COMPONENT', page.id, { name: 'Editable composition' })
  const icon = graph.createInstance(master.id, parent.id, { name: 'Editable icon', uniformScaleFactor: 20 / 24 })
  const outer = graph.createInstance(parent.id, page.id, { name: 'Editable composition instance' })
  graph.updateNode(icon.id, { uniformScaleFactor: 16 / 24 })
  graph.syncInstances(parent.id)
  checkScale(graph, graph.getChildren(outer.id)[0], master, Math.fround(16 / 24))
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  master = masterOf(graph)
  const reopened = graph.getChildren(named(graph, 'Editable composition').id)[0]
  assert.equal(reopened.source.fig.uniformScaleFactor, Math.fround(16 / 24))
  graph.updateNode(reopened.id, { uniformScaleFactor: null })
  graph.syncInstances(named(graph, 'Editable composition').id)
  close(reopened.width, master.width)
  checkScale(graph, graph.getChildren(named(graph, 'Editable composition instance').id)[0], master, 1, null)
  assert.equal(graph.getChildren(named(graph, 'Editable composition instance').id)[0].uniformScaleFactor, null)
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  const cleared = graph.getChildren(named(graph, 'Editable composition').id)[0]
  assert.equal(cleared.uniformScaleFactor, null)
  close(cleared.width, masterOf(graph).width)
  checkScale(graph, graph.getChildren(named(graph, 'Editable composition instance').id)[0], masterOf(graph), 1, null)
})

test('invalid factors and unsupported masters reject before creating an instance', () => {
  const graph = buildFoundation(snapshot), master = masterOf(graph), page = graph.addPage('Rejected scale')
  for (const factor of [0, -1, NaN, Infinity, '0.5', Number.MIN_VALUE]) {
    const before = structuredClone([...graph.getAllNodes()])
    assert.throws(() => graph.createInstance(master.id, page.id, { uniformScaleFactor: factor }), /uniform scale/i)
    assert.deepEqual([...graph.getAllNodes()], before)
  }
  const unsupported = graph.createNode('COMPONENT', page.id, { width: 24, height: 24 })
  graph.createNode('TEXT', unsupported.id, { text: 'not a vector icon' })
  const before = structuredClone([...graph.getAllNodes()])
  assert.throws(() => graph.createInstance(unsupported.id, page.id, { uniformScaleFactor: 0.5 }), /uniform scale/i)
  assert.deepEqual([...graph.getAllNodes()], before)
  const path = graph.getChildren(master.id)[1]
  graph.updateNode(path.id, { strokes: path.strokes.map(stroke => ({ ...stroke, dashPattern: [2, 3] })) })
  const dashed = structuredClone([...graph.getAllNodes()])
  assert.throws(() => graph.createInstance(master.id, page.id, { uniformScaleFactor: 0.5 }), /uniform scale/i)
  assert.deepEqual([...graph.getAllNodes()], dashed)
})
