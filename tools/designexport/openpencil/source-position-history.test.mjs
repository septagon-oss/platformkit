import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { sourceAbsoluteRecord } from './source-positioning.mjs'

const metadata = position => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
  schema: 'platformkit.design-export.v1', scope: 'source-composition-layout', example: 'canonical',
  ...(position ? { cssPosition: position } : {}),
}) }, { pluginId: 'another-owner', key: 'evidence', value: 'original' }]
const position = node => structuredClone(sourceAbsoluteRecord(node))
const snapshot = graph => structuredClone([...graph.nodes])
const measure = () => ({ width: 40, height: 20, minContentWidth: 20 })

function inspect(graph) {
  const named = name => [...graph.getAllNodes()].find(node => node.name === name)
  const master = named('Master'), edited = named('Edited'), sibling = named('Sibling'), destination = named('Destination')
  const target = [...graph.getChildren(edited.id), ...graph.getChildren(destination.id)].find(node => node.name === 'Target')
  return { graph, master, edited, sibling, destination, target, editor: createEditor({ graph }) }
}

async function reopen(graph) {
  return inspect(await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' }))
}

async function fixture(horizontal = 'left', vertical = 'top', automatic = false, standalone = false) {
  const graph = new SceneGraph(), page = graph.getPages()[0].id
  const frame = { width: 320, height: 160, layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED' }
  const master = graph.createNode('COMPONENT', page, { ...frame, name: 'Master', pluginData: metadata() })
  for (const [name, inset] of [['First', -80.4], ['Target', .1], ['Last', 184.7]]) {
    const auto = automatic && name === 'Target'
    const node = graph.createNode('FRAME', master.id, {
      name, width: 40.1, height: 20.1, layoutMode: 'VERTICAL', layoutPositioning: 'ABSOLUTE',
      primaryAxisSizing: auto ? 'HUG' : 'FIXED', counterAxisSizing: 'FIXED',
      horizontalConstraint: horizontal === 'left' ? 'MIN' : 'MAX', verticalConstraint: vertical === 'top' ? 'MIN' : 'MAX',
      pluginData: metadata({ version: 1, horizontal: { edge: horizontal, inset }, vertical: { edge: vertical, inset: name === 'Target' ? .2 : inset },
        ...(auto ? { autoSize: { oppositeBorder: 0 } } : {}) }),
    })
    if (auto) {
      const content = graph.createNode('FRAME', node.id, { layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG',
        counterAxisSizing: 'FILL', layoutAlignSelf: 'STRETCH' })
      graph.createNode('TEXT', content.id, { text: 'Badge', width: 40, height: 20, textAutoResize: 'HEIGHT' })
    }
  }
  computeLayout(graph, master.id)
  if (standalone) {
    const edited = graph.createNode('FRAME', page, { ...frame, name: 'Edited', pluginData: metadata() })
    for (const child of graph.getChildren(master.id)) graph.cloneTree(child.id, edited.id)
  } else graph.createInstance(master.id, page, { name: 'Edited' })
  graph.createInstance(master.id, page, { name: 'Sibling' })
  graph.createNode('FRAME', page, { ...frame, name: 'Destination', x: 500, pluginData: metadata() })
  return reopen(graph)
}

function protectedEvidence(f) {
  return [f.master, f.sibling].map(root => ({ width: root.width, height: root.height,
    children: f.graph.getChildren(root.id).map(node => ({ position: position(node), opacity: node.opacity,
      pluginData: node.pluginData.filter(entry => entry.pluginId !== 'open-pencil') })) }))
}

function move(f, key, value, reparent) {
  const node = f.target
  const originals = new Map([[node.id, { x: node.x, y: node.y, parentId: node.parentId,
    history: f.editor.captureNodeUpdate(node.id, ['x', 'y']) }]])
  if (reparent) f.graph.reparentNode(node.id, f.destination.id)
  f.editor.updateNode(node.id, { [key]: value })
  if (reparent) f.editor.commitMoveWithReparent(originals)
  else f.editor.commitMove(originals)
}

const operations = {
  update: (f, key, value) => f.editor.updateNodeWithUndo(f.target.id, { [key]: value }, 'Coordinate'),
  field(f, key, value) {
    const previous = { [key]: f.target[key] }, receipt = f.editor.captureNodeUpdate(f.target.id, [key])
    f.editor.updateNode(f.target.id, { [key]: value })
    f.editor.commitNodeUpdate(f.target.id, previous, 'Coordinate', receipt)
  },
  nudge(f, key) {
    f.editor.select([f.target.id])
    f.editor.nudgeSelected(key === 'x' ? 3 : 0, key === 'y' ? 3 : 0)
    f.editor.flushNudge()
  },
  align: (f, key) => f.editor.alignNodes([f.target.id], key === 'x' ? 'horizontal' : 'vertical', 'center'),
  distribute: (f, key) => f.editor.distributeNodes(f.graph.getChildren(f.edited.id).map(node => node.id), key === 'x' ? 'horizontal' : 'vertical'),
  move: (f, key, value) => move(f, key, value, false),
  reparent: (f, key, value) => move(f, key, value, true),
}

for (const [name, change] of Object.entries(operations)) {
  test(`${name} owns exact imported coordinate history and two saves`, async () => {
    for (const horizontal of ['left', 'right']) for (const vertical of ['top', 'bottom']) for (const key of ['x', 'y']) {
      let f = await fixture(horizontal, vertical, false, name === 'reparent')
      const before = position(f.target), untouched = key === 'x' ? 'vertical' : 'horizontal'
      const protectedBefore = protectedEvidence(f), marks = [...f.target.source.editedFields]
      change(f, key, 55.123456789)
      const after = position(f.target)
      assert.notDeepEqual(after, before, `${horizontal}/${vertical}/${key}: operation changes placement`)
      assert.deepEqual(after[untouched], before[untouched], 'the other axis remains source-owned')
      f.editor.undoAction()
      assert.deepEqual(position(f.target), before)
      assert.deepEqual(f.target.source.editedFields, marks)
      assert.equal(f.target.parentId, f.edited.id)
      f.editor.redoAction()
      assert.deepEqual(position(f.target), after)
      for (let save = 0; save < 2; save++) {
        f = await reopen(f.graph)
        assert.deepEqual(position(f.target), after)
        assert.deepEqual(protectedEvidence(f), protectedBefore)
        assert.equal(f.sibling.componentId, f.master.id)
      }
    }
  })
}

test('coordinate undo and cancel preserve independent axes, opacity and provenance through saves', async () => {
  for (const cancel of [false, true]) for (const key of ['x', 'y']) {
    let f = await fixture(), node = f.target
    const owned = key === 'x' ? 'horizontal' : 'vertical', other = key === 'x' ? 'vertical' : 'horizontal'
    const before = position(node), receipt = cancel ? f.editor.captureNodeUpdate(node.id, [key]) : null
    const change = { x: node.x, y: node.y, [key]: 55.123456789 }
    if (cancel) f.editor.updateNode(node.id, change)
    else f.editor.updateNodeWithUndo(node.id, change, 'Coordinate')
    const pluginData = node.pluginData.map(entry => {
      if (entry.pluginId === 'another-owner') return { ...entry, value: 'independent' }
      if (entry.pluginId === 'platformkit') return { ...entry,
        value: JSON.stringify({ ...JSON.parse(entry.value), annotation: 'independent' }) }
      return entry
    })
    f.graph.updateNode(node.id, { [key === 'x' ? 'y' : 'x']: 7.123456789, opacity: .75, pluginData })
    const independent = position(node)[other]
    if (cancel) f.editor.cancelNodeUpdate(receipt)
    else f.editor.undoAction()
    const verify = (saved = false) => {
      assert.deepEqual(position(f.target)[owned], before[owned])
      assert.deepEqual(position(f.target)[other], independent)
      assert.equal(f.target.opacity, .75)
      assert.equal(f.target.pluginData.find(entry => entry.pluginId === 'another-owner').value, 'independent')
      assert.equal(JSON.parse(f.target.pluginData.find(entry => entry.pluginId === 'platformkit').value).example, 'canonical')
      // Unknown source annotations belong to live history, not the trusted FIG occurrence payload.
      if (!saved) assert.equal(JSON.parse(f.target.pluginData.find(entry => entry.pluginId === 'platformkit').value).annotation, 'independent')
    }
    verify()
    assert.ok(f.target.source.editedFields.includes('opacity'))
    if (!cancel) {
      f.editor.redoAction()
      assert.deepEqual(position(f.target)[other], independent)
      assert.equal(JSON.parse(f.target.pluginData.find(entry => entry.pluginId === 'platformkit').value).annotation, 'independent')
      f.editor.undoAction()
    }
    for (let save = 0; save < 2; save++) { f = await reopen(f.graph); verify(true) }
  }
})

function refusesAtomically(f, action, pattern = /source|history|missing|stale|measurement/i) {
  const before = snapshot(f.graph), history = [f.editor.undo.canUndo, f.editor.undo.canRedo], events = []
  const stops = ['node:updated', 'node:created', 'node:deleted', 'node:reparented', 'node:reordered']
    .map(event => f.graph.emitter.on(event, id => events.push(id)))
  try {
    assert.throws(action, pattern)
    assert.deepEqual(snapshot(f.graph), before)
    assert.deepEqual(events, [])
    assert.deepEqual([f.editor.undo.canUndo, f.editor.undo.canRedo], history)
  } finally { for (const stop of stops) stop() }
}

test('stale position, missing node and missing original parent refuse without consuming history', async () => {
  for (const missing of ['position', 'node', 'parent']) {
    const f = await fixture('left', 'top', false, missing === 'parent')
    move(f, 'x', 55, missing === 'parent')
    const current = f.target.x, removed = missing === 'node' ? f.target : f.edited
    if (missing === 'position') f.graph.updateNode(f.target.id, { x: 99 })
    else f.graph.nodes.delete(removed.id)
    refusesAtomically(f, () => f.editor.undoAction())
    if (missing === 'position') f.graph.updateNode(f.target.id, { x: current })
    else f.graph.nodes.set(removed.id, removed)
    f.editor.undoAction()
    assert.equal(position(f.target).horizontal.inset, .1)
    assert.equal(f.target.parentId, f.edited.id)
  }
})

test('ordinary source move commits cannot reconstruct a missing capture receipt', async () => {
  const f = await fixture(), original = new Map([[f.target.id, { x: f.target.x, y: f.target.y }]])
  f.editor.updateNode(f.target.id, { x: 55 })
  refusesAtomically(f, () => f.editor.commitMove(original))
})

test('private source placements cannot leave direct or ancestor linked instances', async () => {
  for (const nested of [false, true]) {
    const f = await fixture()
    if (nested) {
      const wrapper = f.graph.createNode('FRAME', f.edited.id, { name: 'Private wrapper' })
      f.graph.insertChildAt(f.target.id, wrapper.id, 0)
    }
    refusesAtomically(f, () => f.graph.reparentNode(f.target.id, f.destination.id), /source absolute.*linked instance/)
  }
})

test('native measurement failure leaves apply, cancel and history atomic and retryable', async t => {
  const previous = getTextMeasurer()
  t.after(() => setTextMeasurer(previous))
  for (const phase of ['apply', 'undo', 'redo', 'cancel']) {
    setTextMeasurer(measure)
    const f = await fixture('left', 'top', true), receipt = f.editor.captureNodeUpdate(f.target.id, ['x'])
    if (phase === 'cancel') f.editor.updateNode(f.target.id, { x: 55 })
    else if (phase !== 'apply') f.editor.updateNodeWithUndo(f.target.id, { x: 55 }, 'Coordinate')
    if (phase === 'redo') f.editor.undoAction()
    const action = phase === 'apply' ? () => f.editor.updateNodeWithUndo(f.target.id, { x: 55 }, 'Coordinate') :
      phase === 'cancel' ? () => f.editor.cancelNodeUpdate(receipt) : () => f.editor[`${phase}Action`]()
    setTextMeasurer(() => { throw new Error('native measurement unavailable') })
    refusesAtomically(f, action)
    setTextMeasurer(measure)
    action()
    assert.equal(position(f.target).horizontal.inset, phase === 'undo' || phase === 'cancel' ? .1 : 55)
  }
})

test('mixed coordinate operations share one atomic history entry and survive two saves', async t => {
  const previous = getTextMeasurer()
  t.after(() => setTextMeasurer(previous))
  for (const key of ['x', 'y']) for (const phase of ['preview', 'undo', 'redo', 'cancel']) {
    setTextMeasurer(measure)
    let f = await fixture('left', 'top', true)
    const nodes = f.graph.getChildren(f.edited.id).slice(0, 2), protectedBefore = protectedEvidence(f)
    assert.equal(nodes[1], f.target, 'the measurable target is the second selected node')
    const originals = new Map(nodes.map(node => [node.id, { history: f.editor.captureNodeUpdate(node.id, [key]) }]))
    const before = nodes.map(position), axis = key === 'x' ? 'horizontal' : 'vertical'
    const preview = () => f.editor.updatePositionMove(originals, { [key]: 0 })
    if (phase !== 'preview') {
      preview()
      if (phase !== 'cancel') f.editor.commitMove(originals, `Change ${key}`)
      if (phase === 'redo') f.editor.undoAction()
    }
    const action = phase === 'preview' ? preview : phase === 'cancel' ? () => f.editor.cancelMove(originals) : () => f.editor[`${phase}Action`]()
    setTextMeasurer(() => { throw new Error('second selected node measurement unavailable') })
    refusesAtomically(f, action)
    setTextMeasurer(measure)
    action()
    if (phase === 'preview') f.editor.commitMove(originals, `Change ${key}`)
    if (phase === 'cancel' || phase === 'undo') {
      assert.deepEqual(nodes.map(position), before)
      assert.equal(f.editor.undo.canUndo, false)
    } else {
      const moved = nodes.map(position)
      assert.deepEqual(moved.map(value => value[axis].inset), [0, 0])
      f.editor.undoAction()
      assert.deepEqual(nodes.map(position), before, 'one undo restores every selected node')
      assert.equal(f.editor.undo.canUndo, false, 'no per-node history entry remains')
      f.editor.redoAction()
      assert.deepEqual(nodes.map(position), moved)
    }
    const expected = nodes.map(position)
    for (let save = 0; save < 2; save++) {
      f = await reopen(f.graph)
      assert.deepEqual(f.graph.getChildren(f.edited.id).slice(0, 2).map(position), expected)
      assert.deepEqual(protectedEvidence(f), protectedBefore)
    }
  }
})

test('mixed no-op cancellation emits nothing and replaced second-node receipts refuse', async () => {
  const f = await fixture(), nodes = f.graph.getChildren(f.edited.id).slice(0, 2)
  const originals = new Map(nodes.map(node => [node.id, { history: f.editor.captureNodeUpdate(node.id, ['x']) }]))
  const before = snapshot(f.graph), events = [], stop = f.graph.emitter.on('node:updated', id => events.push(id))
  try { f.editor.cancelMove(originals) } finally { stop() }
  assert.deepEqual(snapshot(f.graph), before)
  assert.deepEqual(events, [])
  assert.equal(f.editor.undo.canUndo, false)
  f.graph.nodes.set(f.target.id, structuredClone(f.target))
  for (const action of [() => f.editor.cancelMove(originals), () => f.editor.commitMove(originals),
    () => f.editor.updatePositionMove(originals, { x: 0 })]) refusesAtomically(f, action, /stale receipt identity/)
})

test('ordinary and source-positioned nodes share coordinate history without claiming other fields', async () => {
  const f = await fixture(), ordinary = f.graph.createNode('RECTANGLE', f.destination.id, { x: 12.5, y: 33.25, layoutPositioning: 'ABSOLUTE' })
  const nodes = [ordinary, f.target], before = position(f.target)
  const originals = new Map(nodes.map(node => [node.id, { history: f.editor.captureNodeUpdate(node.id, ['x']) }]))
  for (const x of [NaN, Infinity, -Infinity, 1e40, '0']) refusesAtomically(f, () => f.editor.updatePositionMove(originals, { x }), /finite|number|numeric/)
  for (const method of ['updateNode', 'updateNodeWithUndo']) refusesAtomically(f, () => f.editor[method](f.target.id, { x: NaN }), /numeric/)
  f.editor.updatePositionMove(originals, { x: 0 })
  f.editor.commitMove(originals, 'Change x')
  f.graph.updateNode(ordinary.id, { opacity: .5 })
  f.editor.undoAction()
  assert.deepEqual([ordinary.x, ordinary.y, ordinary.opacity], [12.5, 33.25, .5])
  assert.deepEqual(position(f.target), before)
  assert.equal(f.editor.undo.canUndo, false)
  f.editor.redoAction()
  assert.deepEqual(nodes.map(node => node.x), [0, 0])
})
