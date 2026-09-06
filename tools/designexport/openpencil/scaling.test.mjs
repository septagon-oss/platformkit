import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
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
  assert.equal(graph.getChildren(instance.id).length, graph.getChildren(master.id).length)
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

const component = (graph, name) => [...graph.getAllNodes()].find(node => node.type === 'COMPONENT' && node.name === name)
const subtree = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => subtree(graph, child))]
const reopen = async graph => parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })

for (const size of [20, 16]) test(`component replacement preserves canonical ${size}px icon geometry through two saves`, async () => {
  let graph = buildFoundation(snapshot)
  const page = graph.addPage('Replacement conformance')
  const parent = graph.createNode('FRAME', page.id, { name: 'Replacement placement' })
  const original = component(graph, 'plus'), replacement = component(graph, 'x')
  const factor = Math.fround(size / 24)
  const instance = graph.createInstance(original.id, parent.id, { uniformScaleFactor: factor, x: 5, y: 7 })
  const sibling = graph.createInstance(original.id, page.id, { name: 'Replacement sibling' })
  const reference = graph.createInstance(replacement.id, page.id, { uniformScaleFactor: factor })
  const expectedPixels = await pixels(graph, reference)
  const untouched = structuredClone([subtree(graph, original), subtree(graph, replacement), subtree(graph, sibling)])
  graph.swapInstanceComponent(instance.id, replacement.id)
  assert.equal(graph.getNode(instance.id), instance, 'replacement retains the instance root')
  assert.deepEqual([instance.x, instance.y], [5, 7])
  assert.deepEqual([subtree(graph, original), subtree(graph, replacement), subtree(graph, sibling)], untouched)
  for (let cycle = 0; cycle < 3; cycle++) {
    const swapped = graph.getChildren(named(graph, 'Replacement placement').id)[0]
    assert.equal(swapped.type, 'INSTANCE')
    assert.equal(graph.getNode(swapped.componentId)?.name, 'x')
    checkScale(graph, swapped, component(graph, 'x'), factor)
    assert.deepEqual(await pixels(graph, swapped), expectedPixels, `replacement pixels at cycle ${cycle}`)
    checkScale(graph, named(graph, 'Replacement sibling'), component(graph, 'plus'), 1, null)
    if (cycle < 2) graph = await reopen(graph)
  }
})

for (const imported of [false, true]) test(`nested INSTANCE_SWAP preserves occurrence identity, history and saves; imported=${imported}`, async () => {
  let graph = buildFoundation(snapshot)
  const page = graph.addPage('Property replacement conformance')
  const original = component(graph, 'plus')
  const wrapper = graph.createNode('COMPONENT', page.id, {
    name: 'Replacement owner', width: 64, height: 32,
    componentPropertyDefinitions: [
      { id: '91:1', name: 'Leading icon', type: 'INSTANCE_SWAP', defaultValue: original.id },
      { id: '91:2', name: 'Trailing icon', type: 'INSTANCE_SWAP', defaultValue: original.id },
    ],
  })
  for (const [propertyId, size, x] of [['91:1', 20, 0], ['91:2', 16, 32]]) {
    graph.createInstance(original.id, wrapper.id, { uniformScaleFactor: size / 24, x,
      componentPropertyReferences: [{ propertyId, field: 'INSTANCE_SWAP' }] })
  }
  graph.createInstance(wrapper.id, page.id, { name: 'Property replacement edited' })
  graph.createInstance(wrapper.id, page.id, { name: 'Property replacement untouched' })
  if (imported) graph = await reopen(graph)
  const edited = named(graph, 'Property replacement edited'), actions = createEditor({ graph })
  const target = graph.getChildren(edited.id)[0], guard = graph.getChildren(edited.id)[1]
  const originalLink = target.componentId
  const unaffected = ['Replacement owner', 'Property replacement untouched'].map(name => named(graph, name))
  const before = structuredClone([subtree(graph, guard), ...unaffected.map(node => subtree(graph, node))])
  for (let cycle = 0; cycle < 2; cycle++) {
    actions.setInstanceComponentProperty(edited.id, '91:1', component(graph, 'x').id)
    await Promise.resolve()
    checkScale(graph, target, component(graph, 'x'), Math.fround(20 / 24))
    assert.equal(graph.getNode(target.id), target)
    assert.equal(target.componentId, component(graph, 'x').id)
    actions.undoAction()
    await Promise.resolve()
    assert.equal(target.componentId, originalLink, 'undo restores the original source occurrence, not just its master')
    checkScale(graph, target, component(graph, 'plus'), Math.fround(20 / 24))
    assert.deepEqual(edited.componentPropertyAssignments, {})
    actions.redoAction()
    await Promise.resolve()
    checkScale(graph, target, component(graph, 'x'), Math.fround(20 / 24))
    assert.equal(edited.componentPropertyAssignments['91:1'], component(graph, 'x').id)
    actions.undoAction()
    await Promise.resolve()
  }
  assert.deepEqual([subtree(graph, guard), ...unaffected.map(node => subtree(graph, node))], before)
  actions.setInstanceComponentProperty(edited.id, '91:1', component(graph, 'x').id)
  await Promise.resolve()
  for (let cycle = 0; cycle < 2; cycle++) {
    const encoded = await exportFigFile(graph), raw = parseFigBuffer(encoded.slice().buffer)
    const source = raw.nodeChanges.find(node => node.type === 'SYMBOL' && node.name === 'plus')
    const replacement = raw.nodeChanges.find(node => node.type === 'SYMBOL' && node.name === 'x')
    const rawOwner = raw.nodeChanges.find(node => node.name === 'Replacement owner')
    const rawEdited = raw.nodeChanges.find(node => node.name === 'Property replacement edited')
    assert.equal(rawOwner.componentPropDefs.length, 2)
    for (const definition of rawOwner.componentPropDefs) assert.deepEqual(definition.initialValue.guidValue, source.guid)
    assert.deepEqual(rawEdited.componentPropAssignments, [{ defID: { sessionID: 91, localID: 1 },
      value: { guidValue: replacement.guid } }], 'property values must use allocated file GUIDs, not live node IDs')
    const leadingSource = raw.nodeChanges.find(node => node.type === 'INSTANCE' &&
      JSON.stringify(node.parentIndex.guid) === JSON.stringify(rawOwner.guid) &&
      node.componentPropRefs.some(ref => ref.defID.sessionID === 91 && ref.defID.localID === 1))
    const swaps = rawEdited.symbolData.symbolOverrides.filter(override => override.overriddenSymbolID)
    assert.equal(swaps.length, 1, 'only the selected occurrence is replaced')
    assert.deepEqual(swaps[0].guidPath.guids, [leadingSource.guid])
    assert.deepEqual(swaps[0].overriddenSymbolID, replacement.guid)
    graph = await parseFigFile(encoded.slice().buffer, { populate: 'all' })
    const owner = named(graph, 'Property replacement edited')
    const [leading, trailing] = graph.getChildren(owner.id)
    checkScale(graph, leading, component(graph, 'x'), Math.fround(20 / 24))
    checkScale(graph, trailing, component(graph, 'plus'), Math.fround(16 / 24))
    for (const name of ['Replacement owner', 'Property replacement untouched']) {
      const [first, second] = graph.getChildren(named(graph, name).id)
      checkScale(graph, first, component(graph, 'plus'), Math.fround(20 / 24))
      checkScale(graph, second, component(graph, 'plus'), Math.fround(16 / 24))
    }
    const definition = createEditor({ graph }).getInstanceComponentPropertyDefinitions(owner.id).find(value => value.id === '91:1')
    assert.equal(createEditor({ graph }).getInstanceComponentPropertyValue(owner.id, definition), component(graph, 'x').id)
  }
})

for (const property of [false, true]) test(`unsupported scaled replacement refuses before graph or history changes; property=${property}`, async () => {
  const graph = buildFoundation(snapshot), page = graph.addPage('Rejected replacement')
  const original = component(graph, 'plus')
  const unsupported = graph.createNode('COMPONENT', page.id, { width: 24, height: 24 })
  graph.createNode('TEXT', unsupported.id, { text: 'Unsupported scaled text' })
  const owner = graph.createNode('COMPONENT', page.id, {
    componentPropertyDefinitions: [{ id: '92:1', name: 'Icon', type: 'INSTANCE_SWAP', defaultValue: original.id }],
  })
  graph.createInstance(original.id, owner.id, { uniformScaleFactor: 20 / 24,
    componentPropertyReferences: [{ propertyId: '92:1', field: 'INSTANCE_SWAP' }] })
  const outer = graph.createInstance(owner.id, page.id), target = graph.getChildren(outer.id)[0]
  const actions = createEditor({ graph }), events = []
  const before = structuredClone([...graph.getAllNodes()])
  const unsubscribe = graph.onNodeEvents({ created: () => events.push('created'),
    updated: () => events.push('updated'), deleted: () => events.push('deleted') })
  try {
    assert.throws(() => property
      ? actions.setInstanceComponentProperty(outer.id, '92:1', unsupported.id)
      : graph.swapInstanceComponent(target.id, unsupported.id), /uniform scale/i)
    await Promise.resolve()
    assert.deepEqual([...graph.getAllNodes()], before)
    assert.deepEqual(events, [], 'validation must precede every mutation notification')
    assert.equal(actions.undo.canUndo, false)
  } finally { unsubscribe() }
})

for (const scenario of ['missing component', 'edited descendant', 'imported edited descendant',
  'local descendant', 'imported local descendant', 'local instance descendant', 'local duplicate descendant']) {
  test(`replacement refuses ${scenario} without losing authored state`, async () => {
    let graph = buildFoundation(snapshot)
    const page = graph.addPage('Replacement authored-state conformance')
    const original = component(graph, 'plus')
    const owner = graph.createNode('COMPONENT', page.id, {
      componentPropertyDefinitions: [{ id: '93:1', name: 'Icon', type: 'INSTANCE_SWAP', defaultValue: original.id }],
    })
    graph.createInstance(original.id, owner.id, { uniformScaleFactor: scenario.includes('local') ? null : 20 / 24,
      componentPropertyReferences: [{ propertyId: '93:1', field: 'INSTANCE_SWAP' }] })
    graph.createInstance(owner.id, page.id, { name: 'Authored replacement instance' })
    if (scenario.startsWith('imported')) graph = await reopen(graph)
    const outer = named(graph, 'Authored replacement instance')
    if (scenario.includes('edited')) {
      const target = graph.getChildren(outer.id)[0]
      graph.updateNode(graph.getChildren(target.id)[0].id, { opacity: 0.35 })
    }
    if (scenario.includes('local')) {
      const target = graph.getChildren(outer.id)[0]
      if (scenario.includes('instance')) graph.createInstance(component(graph, 'x').id, target.id)
      else if (scenario.includes('duplicate')) graph.createNode('VECTOR', target.id, { componentId: graph.getChildren(target.id)[0].componentId })
      else graph.createNode('RECTANGLE', target.id, { name: 'Locally authored content', width: 3, height: 5 })
    }
    const actions = createEditor({ graph }), events = []
    const before = structuredClone([...graph.getAllNodes()])
    const unsubscribe = graph.onNodeEvents({ created: () => events.push('created'),
      updated: () => events.push('updated'), deleted: () => events.push('deleted') })
    try {
      const value = scenario === 'missing component' ? 'missing-native-component' : component(graph, 'x').id
      assert.throws(() => actions.setInstanceComponentProperty(outer.id, '93:1', value),
        /missing native replacement|replacement.*history|override|edited|native source identity/i)
      await Promise.resolve()
      assert.deepEqual([...graph.getAllNodes()], before)
      assert.deepEqual(events, [], 'refused history must not publish even transient graph changes')
      assert.equal(actions.undo.canUndo, false)
    } finally { unsubscribe() }
  })
}

for (const field of ['width', 'stroke weight']) test(`replacement rejects FIG float32 overflow in ${field} before effects`, () => {
  const graph = buildFoundation(snapshot), page = graph.addPage('Replacement precision refusal')
  const replacement = masterOf(graph)
  if (field === 'width') graph.updateNode(replacement.id, { width: 1e40 })
  else {
    const path = graph.getChildren(replacement.id)[1]
    graph.updateNode(path.id, { strokes: path.strokes.map(stroke => ({ ...stroke, weight: 1e40 })) })
  }
  const instance = graph.createInstance(component(graph, 'plus').id, page.id, { uniformScaleFactor: 20 / 24 })
  const before = structuredClone([...graph.getAllNodes()]), events = []
  const unsubscribe = graph.onNodeEvents({ created: () => events.push('created'),
    updated: () => events.push('updated'), deleted: () => events.push('deleted') })
  try {
    assert.throws(() => graph.swapInstanceComponent(instance.id, replacement.id), /native.*scale|finite/i)
    assert.deepEqual([...graph.getAllNodes()], before)
    assert.deepEqual(events, [])
  } finally { unsubscribe() }
})
