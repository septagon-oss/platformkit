import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import { createEditor } from '@open-pencil/core/editor'
import { importNodeChanges, populateAllLazyFigImportRoots } from '@open-pencil/core/kiwi'

const track = (sizing, value) => ({ sizing, value })
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const fields = node => Object.fromEntries(['layoutMode', 'primaryAxisSizing', 'counterAxisSizing',
  'gridTemplateColumns', 'gridTemplateRows', 'gridRowGap', 'gridColumnGap', 'gridPosition',
  'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft', 'width', 'height'].map(key => [key, node[key]]))

function fixture(explicitRows) {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const grid = graph.createNode('COMPONENT', page.id, {
    name: 'Reusable grid', width: 420, height: 500, layoutMode: 'GRID',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED',
    gridTemplateColumns: [track('FIXED', 50), track('FR', 2), track('AUTO', 0)],
    gridTemplateRows: explicitRows ? [track('FIXED', 40), track('AUTO', 0)] : [],
    gridColumnGap: 12, gridRowGap: 7, paddingTop: 4, paddingRight: 8, paddingBottom: 6, paddingLeft: 8,
  })
  for (let index = 0; index < 4; index++) graph.createNode('RECTANGLE', grid.id, {
    name: `Cell ${index}`, width: 30, height: 20,
    ...(explicitRows && index === 0 ? { gridPosition: { column: 2, row: 1, columnSpan: 2, rowSpan: 1 } } : {}),
  })
  computeLayout(graph, grid.id)
  graph.createInstance(grid.id, page.id, { name: 'Grid placement', x: 500 })
  return graph
}

for (const explicitRows of [false, true]) test(`native grid fields and linked cells survive two FIG saves: explicit rows=${explicitRows}`, async () => {
  let graph = fixture(explicitRows)
  const expected = fields(named(graph, 'Reusable grid'))
  const childFields = graph.getChildren(named(graph, 'Reusable grid').id).map(fields)
  for (let cycle = 0; cycle < 2; cycle++) {
    const before = structuredClone([...graph.getAllNodes()]), bytes = await exportFigFile(graph)
    assert.deepEqual([...graph.getAllNodes()], before, 'export leaves the caller graph untouched')
    const raw = (await parseFigBuffer(bytes.slice().buffer)).nodeChanges
    const owner = raw.find(node => node.name === 'Reusable grid')
    assert.equal(owner.stackMode, 'GRID', 'FIG contains a native grid, not flattened boxes')
    assert.equal(owner.gridColumnGap, 12)
    assert.equal(owner.gridRowGap, 7)
    assert.equal(owner.gridColumns.entries.length, 3)
    assert.deepEqual(owner.gridColumnsSizing.entries.map(entry => entry.trackSize.maxSizing.type), ['FIXED', 'FLEX', 'HUG'])
    assert.equal(owner.gridRows.entries.length, explicitRows ? 2 : 0)
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    const master = named(graph, 'Reusable grid'), placed = named(graph, 'Grid placement')
    assert.deepEqual(fields(master), expected)
    assert.deepEqual(fields(placed), expected)
    assert.equal(placed.componentId, master.id)
    assert.deepEqual(graph.getChildren(master.id).map(fields), childFields)
    assert.deepEqual(graph.getChildren(placed.id).map(fields), childFields)
    graph.getChildren(placed.id).forEach((child, index) => assert.equal(child.componentId, master.childIds[index]))
  }
})

test('instance grid edits remain local through history, two saves and later master changes', async () => {
  let graph = await parseFigFile((await exportFigFile(fixture(true))).slice().buffer, { populate: 'all' })
  const master = named(graph, 'Reusable grid'), placed = named(graph, 'Grid placement')
  graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Untouched grid' })
  const actions = createEditor({ graph }), original = fields(master)
  const originalOverrides = structuredClone(placed.overrides)
  actions.updateNodeWithUndo(placed.id, { gridColumnGap: 21, gridTemplateColumns: [track('FIXED', 60), track('FR', 3), track('AUTO', 0)] })
  actions.undoAction()
  assert.equal(placed.gridColumnGap, 12)
  assert.deepEqual(placed.overrides, originalOverrides, 'undo restores inheritance ownership')
  actions.redoAction()
  assert.equal(placed.gridColumnGap, 21)
  const first = graph.getChildren(placed.id)[0]
  actions.updateNodeWithUndo(first.id, { gridPosition: { row: 2, column: 1, rowSpan: 1, columnSpan: 1 } })
  await Promise.resolve()
  graph.syncInstances(master.id)
  assert.equal(placed.gridColumnGap, 21, 'authored edits own their fields before the first save')
  assert.deepEqual(first.gridPosition, { row: 2, column: 1, rowSpan: 1, columnSpan: 1 })
  for (let cycle = 0; cycle < 2; cycle++) {
    graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    const owner = named(graph, 'Reusable grid'), edited = named(graph, 'Grid placement')
    assert.deepEqual(fields(owner), original)
    assert.deepEqual(fields(named(graph, 'Untouched grid')), original)
    assert.equal(edited.gridColumnGap, 21)
    assert.deepEqual(edited.gridTemplateColumns, [track('FIXED', 60), track('FR', 3), track('AUTO', 0)])
    assert.deepEqual(graph.getChildren(edited.id)[0].gridPosition, { row: 2, column: 1, rowSpan: 1, columnSpan: 1 })
    graph.syncInstances(owner.id)
    assert.equal(edited.gridColumnGap, 21, 'reopening retains explicit local ownership, not just saved values')
    assert.deepEqual(graph.getChildren(edited.id)[0].gridPosition, { row: 2, column: 1, rowSpan: 1, columnSpan: 1 })
  }
  const owner = named(graph, 'Reusable grid')
  graph.updateNode(owner.id, { gridRowGap: 19, gridColumnGap: 25 })
  graph.syncInstances(owner.id)
  assert.equal(named(graph, 'Grid placement').gridColumnGap, 21)
  assert.equal(named(graph, 'Grid placement').gridRowGap, 19, 'unmodified fields still inherit')
  assert.equal(named(graph, 'Untouched grid').gridColumnGap, 25)
})

test('grid cell fill and HUG axes retain their native meaning after reopening', async () => {
  let graph = new SceneGraph()
  const grid = graph.createNode('FRAME', graph.getPages()[0].id, {
    name: 'Fill grid', width: 200, height: 60, layoutMode: 'GRID',
    gridTemplateColumns: [track('FR', 1)], gridTemplateRows: [track('FIXED', 60)],
  })
  const cell = graph.createNode('FRAME', grid.id, { name: 'Fill cell', layoutMode: 'VERTICAL',
    width: 20, height: 20, primaryAxisSizing: 'HUG', counterAxisSizing: 'FILL' })
  graph.createNode('RECTANGLE', cell.id, { width: 20, height: 20 })
  computeLayout(graph, grid.id)
  for (let cycle = 0; cycle < 2; cycle++) {
    graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    assert.equal(named(graph, 'Fill cell').primaryAxisSizing, 'HUG')
    assert.equal(named(graph, 'Fill cell').counterAxisSizing, 'FILL')
  }
})

for (const sourceVariant of [false, true]) test(`nested grid overrides keep exact ownership across pages and two saves: source variant=${sourceVariant}`, async () => {
  let graph = fixture(true), master = named(graph, 'Reusable grid')
  const wrapper = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Wrapper' })
  const slot = graph.createInstance(master.id, wrapper.id, { name: 'Grid slot' })
  if (sourceVariant) graph.updateNode(slot.id, {
    gridTemplateColumns: [track('FIXED', 65), track('FR', 2), track('AUTO', 0)],
    gridTemplateRows: [track('FIXED', 64), track('AUTO', 0)],
  })
  const page = graph.addPage('Unopened placements')
  graph.createInstance(wrapper.id, page.id, { name: 'Outer edited' })
  graph.createInstance(wrapper.id, page.id, { name: 'Outer untouched' })
  for (let cycle = 0; cycle < 3; cycle++) {
    graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    const inner = graph.getChildren(named(graph, 'Outer edited').id)[0], first = graph.getChildren(inner.id)[0]
    const untouched = graph.getChildren(named(graph, 'Outer untouched').id)[0]
    if (cycle === 0) {
      graph.updateNode(inner.id, { gridRowGap: 13, gridTemplateColumns: [track('FIXED', 100), track('FR', 3), track('AUTO', 0)] })
      graph.updateNode(first.id, { gridPosition: { row: 2, column: 1, rowSpan: 1, columnSpan: 1 } })
    } else {
      assert.equal(inner.gridRowGap, 13)
      assert.equal(inner.gridTemplateColumns[0].value, 100)
      assert.deepEqual(first.gridPosition, { row: 2, column: 1, rowSpan: 1, columnSpan: 1 })
      assert.equal(untouched.gridRowGap, 7)
      assert.equal(untouched.gridTemplateColumns[0].value, sourceVariant ? 65 : 50)
      graph.syncInstances(named(graph, 'Wrapper').id)
      assert.equal(inner.gridRowGap, 13)
      assert.equal(inner.gridTemplateColumns[0].value, 100)
      assert.deepEqual(first.gridPosition, { row: 2, column: 1, rowSpan: 1, columnSpan: 1 })
    }
  }
})

test('clearing an inherited cell anchor remains automatic after two saves', async () => {
  let graph = fixture(true)
  graph.updateNode(graph.getChildren(named(graph, 'Grid placement').id)[0].id, { gridPosition: null })
  for (let cycle = 0; cycle < 2; cycle++) {
    graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    assert.equal(graph.getChildren(named(graph, 'Grid placement').id)[0].gridPosition, null)
    assert.deepEqual(graph.getChildren(named(graph, 'Reusable grid').id)[0].gridPosition,
      { row: 1, column: 2, rowSpan: 1, columnSpan: 2 })
  }
})

test('cell axis edits preserve only their own sizing through sync, history and two saves', async () => {
  let graph = new SceneGraph()
  const grid = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Axis master',
    width: 200, height: 60, layoutMode: 'GRID', gridTemplateColumns: [track('FR', 1)] })
  graph.createNode('FRAME', grid.id, { name: 'Axis cell', width: 20, height: 20, layoutMode: 'VERTICAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FILL' })
  graph.createInstance(grid.id, graph.getPages()[0].id, { name: 'Axis instance' })
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  const placed = named(graph, 'Axis instance'), cell = graph.getChildren(placed.id)[0], actions = createEditor({ graph })
  const before = structuredClone(placed.overrides)
  actions.updateNodeWithUndo(cell.id, { counterAxisSizing: 'HUG' })
  actions.undoAction()
  assert.equal(cell.counterAxisSizing, 'FILL')
  assert.deepEqual(placed.overrides, before)
  actions.redoAction()
  await Promise.resolve()
  for (let cycle = 0; cycle < 3; cycle++) {
    graph.syncInstances(named(graph, 'Axis master').id)
    const edited = graph.getChildren(named(graph, 'Axis instance').id)[0]
    assert.equal(edited.counterAxisSizing, 'HUG')
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
  const source = graph.getChildren(named(graph, 'Axis master').id)[0]
  graph.updateNode(source.id, { primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED' })
  graph.syncInstances(named(graph, 'Axis master').id)
  const edited = graph.getChildren(named(graph, 'Axis instance').id)[0]
  assert.equal(edited.counterAxisSizing, 'HUG')
  assert.equal(edited.primaryAxisSizing, 'HUG', 'the other axis still inherits')
})

test('loading unopened pages does not replay saved grid fields over authored edits', async () => {
  let graph = fixture(true)
  const later = graph.addPage('Later'), master = named(graph, 'Reusable grid')
  graph.createInstance(master.id, later.id, { name: 'Deferred grid' })
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'first-page' })
  assert.equal(named(graph, 'Deferred grid').childIds.length, 0, 'deferred instance has not populated its children')
  const edited = named(graph, 'Grid placement'), actions = createEditor({ graph })
  actions.updateNodeWithUndo(edited.id, { gridRowGap: 17 })
  await Promise.resolve()
  assert.equal(populateAllLazyFigImportRoots(graph), true)
  assert.equal(edited.gridRowGap, 17)
  assert.equal(named(graph, 'Deferred grid').gridRowGap, 7)
  assert.equal(named(graph, 'Deferred grid').childIds.length, 4)
  assert.equal(populateAllLazyFigImportRoots(graph), false)
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  assert.equal(named(graph, 'Grid placement').gridRowGap, 17)
  assert.equal(named(graph, 'Deferred grid').gridRowGap, 7)
})

test('imported grid measurement clears its deep cache and restores it after a failed measure', async () => {
  let graph = new SceneGraph()
  const grid = graph.createNode('FRAME', graph.getPages()[0].id, { name: 'Measured grid', width: 200, height: 90,
    layoutMode: 'GRID', primaryAxisSizing: 'HUG', gridTemplateColumns: [track('FR', 1)] })
  const cell = graph.createNode('FRAME', grid.id, { name: 'Measured cell', width: 20, height: 90,
    layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'FILL', counterAxisAlign: 'STRETCH', minWidth: 0 })
  graph.createNode('TEXT', cell.id, { name: 'Measured text', text: 'Wrap this paragraph', width: 20, height: 90, textAutoResize: 'HEIGHT' })
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  for (const name of ['Measured grid', 'Measured cell', 'Measured text']) {
    graph.preserveSourceMetadataDuring(() => graph.updateNode(named(graph, name).id,
      { figmaDerivedLayout: { width: 20, height: 90, x: 0, y: 0 } }))
  }
  const before = structuredClone([...graph.getAllNodes()]), previous = getTextMeasurer()
  try {
    setTextMeasurer(() => { throw new Error('Grid measure unavailable') })
    assert.throws(() => computeLayout(graph, named(graph, 'Measured grid').id), /Grid measure unavailable/)
    assert.deepEqual([...graph.getAllNodes()], before)
    setTextMeasurer((_node, width) => ({ width: width ?? 280, height: Math.ceil(280 / (width ?? 280)) * 20 }))
    computeLayout(graph, named(graph, 'Measured grid').id)
    for (const name of ['Measured grid', 'Measured cell', 'Measured text']) {
      assert.deepEqual([named(graph, name).width, named(graph, name).height], [200, 40], name)
    }
  } finally { setTextMeasurer(previous) }
})

test('invalid native export values refuse without mutating the document', async t => {
  for (const [name, mutate] of [
    ['unknown track', grid => { grid.gridTemplateColumns[0].sizing = 'PERCENT' }],
    ['zero fraction', grid => { grid.gridTemplateColumns[1].value = 0 }],
    ['underflow fraction', grid => { grid.gridTemplateColumns[1].value = Number.MIN_VALUE }],
    ['overflow track', grid => { grid.gridTemplateColumns[0].value = Number.MAX_VALUE }],
    ['invalid track count', grid => { grid.gridTemplateRows = Array(4097).fill(track('AUTO', 0)) }],
    ['nonzero auto', grid => { grid.gridTemplateColumns[2].value = 1 }],
    ['negative gap', grid => { grid.gridColumnGap = -1 }],
    ['unanchorable cell', (_grid, cell) => { cell.gridPosition.column = 9 }],
    ['layout mode replacement', (_grid, _cell, placed) => { placed.layoutMode = 'NONE' }],
  ]) await t.test(name, async () => {
    const graph = fixture(true), master = named(graph, 'Reusable grid')
    mutate(master, graph.getChildren(master.id)[0], named(graph, 'Grid placement'))
    const before = structuredClone([...graph.getAllNodes()])
    await assert.rejects(exportFigFile(graph), /^Error: Native grid:/)
    assert.deepEqual([...graph.getAllNodes()], before)
  })
})

function independentChanges() {
  const id = localID => ({ sessionID: 55, localID })
  const trackSize = (type, value) => ({ minSizing: type === 'FLEX' ? { type: 'HUG', value: 0 } : { type, value }, maxSizing: { type, value } })
  return [
    { guid: id(0), type: 'DOCUMENT' },
    { guid: id(1), type: 'CANVAS', name: 'Independent page', parentIndex: { guid: id(0), position: 'a' } },
    { guid: id(10), type: 'FRAME', name: 'Independent grid', parentIndex: { guid: id(1), position: 'a' },
      size: { x: 320, y: 60 }, stackMode: 'GRID', stackPrimarySizing: 'RESIZE_TO_FIT', stackCounterSizing: 'FIXED',
      gridColumns: { entries: [{ id: id(90), position: 'b' }, { id: id(80), position: 'a' }] },
      gridColumnsSizing: { entries: [{ id: id(90), trackSize: trackSize('FIXED', 30) }, { id: id(80), trackSize: trackSize('FLEX', 2) }] },
      gridRows: { entries: [{ id: id(100), position: 'a' }] },
      gridRowsSizing: { entries: [{ id: id(100), trackSize: trackSize('HUG', 0) }] },
      gridColumnGap: 9, gridRowGap: 4 },
    { guid: id(11), type: 'RECTANGLE', name: 'Independent cell', parentIndex: { guid: id(10), position: 'a' },
      size: { x: 30, y: 20 }, gridColumnAnchor: id(90), gridRowAnchor: id(100), gridRowSpan: 1, gridColumnSpan: 1 },
  ]
}

test('independently authored FIG tracks resolve by GUID and order key, not sizing-array order', () => {
  const changes = independentChanges(), before = structuredClone(changes)
  const graph = importNodeChanges(changes)
  assert.deepEqual(named(graph, 'Independent grid').gridTemplateColumns, [track('FR', 2), track('FIXED', 30)])
  assert.deepEqual(named(graph, 'Independent cell').gridPosition, { column: 2, row: 1, columnSpan: 1, rowSpan: 1 })
  assert.deepEqual(changes, before)
})

test('malformed native FIG track sizing and foreign anchors refuse instead of guessing a layout', async t => {
  for (const [name, mutate] of [
    ['duplicate order', grid => { grid.gridColumns.entries[1].position = 'b' }],
    ['duplicate track', grid => { grid.gridColumns.entries[1].id = grid.gridColumns.entries[0].id }],
    ['duplicate sizing', grid => { grid.gridColumnsSizing.entries[1].id = grid.gridColumnsSizing.entries[0].id }],
    ['missing sizing', grid => { grid.gridColumnsSizing.entries.pop() }],
    ['foreign sizing', grid => { grid.gridColumnsSizing.entries[1].id.localID = 900 }],
    ['unsupported minmax', grid => { grid.gridColumnsSizing.entries[1].trackSize.minSizing = { type: 'PERCENT', value: 5 } }],
    ['negative gap', grid => { grid.gridRowGap = -1 }],
    ['nonfinite gap', grid => { grid.gridRowGap = Infinity }],
    ['implicit columns', grid => { grid.gridAutoTracks = 'COLUMNS' }],
    ['foreign anchor', (_grid, cell) => { cell.gridColumnAnchor.localID = 999 }],
    ['wrong axis anchor', (_grid, cell) => { cell.gridColumnAnchor = cell.gridRowAnchor }],
    ['out of bounds span', (_grid, cell) => { cell.gridColumnSpan = 3 }],
    ['zero span', (_grid, cell) => { cell.gridRowSpan = 0 }],
    ['non-grid parent', grid => { grid.stackMode = 'VERTICAL' }],
  ]) await t.test(name, () => {
    const changes = independentChanges()
    mutate(changes[2], changes[3])
    const before = structuredClone(changes)
    assert.throws(() => importNodeChanges(changes), /^Error: Native grid:/)
    assert.deepEqual(changes, before)
  })
})
