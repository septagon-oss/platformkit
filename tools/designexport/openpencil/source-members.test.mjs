import assert from 'node:assert/strict'
import { test } from 'node:test'
import { memberSelection, planSourceMembers } from './source-members.mjs'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { ancestryOverrides } from './exporter-correction.mjs'

const key = value => JSON.stringify(value)
const rootPath = ['fixture/root']
const source = (path, componentId = 'fixture.component', slot) => ({ path, componentId, ...(slot ? { slot } : {}) })
const element = (index, bounds = { x: 20, y: 30, width: 40, height: 24 }) => ({
  kind: 'element', tag: 'div', domPath: [0, index], bounds,
  style: { display: 'flex', position: 'static' }, children: [],
})

function fixture() {
  const children = [element(2), element(10, { x: 70, y: 30, width: 60, height: 24 }), element(11)]
  const root = { ...element(0), domPath: [0], children, source: source(rootPath) }
  const observation = { roots: [root], sourceOccurrences: [] }, occurrences = new Map()
  function add(path, nodes, { slot = path.length > 1 ? 'Children' : undefined, componentId = 'fixture.component' } = {}) {
    const identity = source(path, componentId, slot)
    const occurrence = { path, slot, description: { id: path.at(-1), componentId } }
    occurrences.set(key(path), occurrence)
    const record = { ...identity, correspondence: 'observed', members: nodes.map(node => ({
      kind: 'element', tag: node.tag, domPath: [...node.domPath], presentation: { state: 'box-observed', inert: false },
    })) }
    observation.sourceOccurrences.push(record)
    if (nodes.length === 1) nodes[0].source = identity
    return occurrence
  }
  const owner = add(rootPath, [root]), group = add([...rootPath, 'group'], children.slice(0, 2))
  const plans = children.map(node => ({ kind: 'frame', observation: node, native: { width: node.bounds.width },
    placement: { counterAxisSizing: 'FILL' } }))
  return { observation, occurrences, root, owner, group, plans, add }
}

function inputState(f) { return structuredClone({ observation: f.observation, occurrences: f.occurrences, plans: f.plans }) }

test('legacy captures retain the exact existing plan handles and owner without adding fragments', () => {
  const f = fixture()
  delete f.observation.sourceOccurrences
  const before = inputState(f), adapter = planSourceMembers(f.observation, f.occurrences), seen = new Set()
  assert.equal(adapter.hasFragments, false)
  assert.equal(adapter.owner(f.root.children[0], f.owner), f.owner)
  assert.equal(adapter.group(f.root, f.plans, f.owner, seen), f.plans)
  assert.deepEqual(inputState(f), before)
  assert.equal(seen.size, 0)
})

test('exact DOM addresses group private members and preserve existing margin contribution handles', () => {
  const f = fixture(), child = f.add([...f.group.path, 'field'], [f.root.children[1]])
  const original = f.plans[0]
  f.plans[0] = { kind: 'frame', observation: { ...original.observation, bounds: { x: 16, y: 25, width: 48, height: 34 } },
    children: [original], native: { paddingLeft: 4 }, placement: { counterAxisSizing: 'FILL' } }
  f.plans[1].placement = { gridPosition: { column: 2, row: 1, columnSpan: 2, rowSpan: 1 } }
  const before = inputState(f), adapter = planSourceMembers(f.observation, f.occurrences), seen = new Set()
  assert.equal(adapter.hasFragments, true)
  assert.equal(adapter.owner(f.root.children[0], f.owner), f.group)
  assert.equal(adapter.owner(f.root.children[1], f.owner), f.group)
  assert.equal(adapter.owner(f.root.children[2], f.owner), f.owner)
  assert.deepEqual(f.root.children[1].source.path, child.path, 'boxed child remains for the owning element handler')
  const [group, untouched] = adapter.group(f.root, f.plans, f.owner, seen)
  assert.equal(group.occurrence, f.group)
  assert.equal(group.children[0], f.plans[0])
  assert.equal(group.children[0].children[0], original)
  assert.equal(group.children[1], f.plans[1])
  assert.equal(untouched, f.plans[2])
  assert.equal(Object.hasOwn(group, 'observation'), false, 'selection envelopes are not browser boxes')
  assert.equal(Object.hasOwn(group, 'placement'), false)
  assert.equal(group.native.layoutMode, 'NONE')
  assert.equal(group.native.primaryAxisSizing, 'HUG')
  assert.equal(group.native.counterAxisSizing, 'HUG')
  assert.deepEqual(memberSelection(group.children), { x: 16, y: 25, width: 114, height: 34 })
  assert.deepEqual(seen, new Set([key(f.group.path)]))
  assert.deepEqual(inputState(f), before)
})

test('equal multi-root intervals nest by declared source ancestry rather than record order', () => {
  for (const reverse of [false, true]) {
    const f = fixture(), nested = f.add([...f.group.path, 'inner'], f.root.children.slice(0, 2))
    if (reverse) f.observation.sourceOccurrences.reverse()
    const before = inputState(f), adapter = planSourceMembers(f.observation, f.occurrences), seen = new Set()
    assert.equal(adapter.owner(f.root.children[0], f.owner), nested)
    const [outer, sibling] = adapter.group(f.root, f.plans, f.owner, seen)
    assert.equal(outer.occurrence, f.group)
    assert.equal(outer.children.length, 1)
    assert.equal(outer.children[0].occurrence, nested)
    assert.deepEqual(outer.children[0].children, f.plans.slice(0, 2))
    assert.equal(sibling, f.plans[2])
    assert.deepEqual(memberSelection([outer]), { x: 20, y: 30, width: 110, height: 24 })
    assert.deepEqual(seen, new Set([key(f.group.path), key(nested.path)]))
    assert.deepEqual(inputState(f), before)
  }
})

test('explicit suppressed comments do not create native boxes or hide unknown members', () => {
  const f = fixture(), record = f.observation.sourceOccurrences[1]
  record.members.splice(1, 0, { kind: 'comment', domPath: [0, 5], presentation: { state: 'suppressed', reason: 'comment' } })
  const before = inputState(f), adapter = planSourceMembers(f.observation, f.occurrences)
  assert.equal(adapter.group(f.root, f.plans, f.owner, new Set())[0].children.length, 2)
  assert.deepEqual(inputState(f), before)
  record.members[1].presentation.reason = 'template-content'
  assert.throws(() => planSourceMembers(f.observation, f.occurrences), /source members/)
})

test('missing, ambiguous, dormant, text and unsupported member evidence refuses without changing inputs', () => {
  const mutations = [
    f => { f.observation.sourceOccurrences = null },
    f => { f.observation.sourceOccurrences.pop() },
    f => { f.observation.sourceOccurrences.push(structuredClone(f.observation.sourceOccurrences[1])) },
    f => { f.observation.sourceOccurrences[1].componentId = 'wrong' },
    f => { f.observation.sourceOccurrences[1].slot = 'wrong' },
    f => { f.observation.sourceOccurrences[1].correspondence = 'unresolved' },
    f => { f.observation.sourceOccurrences[1].members = [] },
    f => { f.observation.sourceOccurrences[1].members[0].presentation.state = 'unresolved' },
    f => { f.observation.sourceOccurrences[1].members[0].presentation = { state: 'suppressed', reason: 'display-none' } },
    f => { f.observation.sourceOccurrences[1].members[0].domPath = [0, 'content', 0] },
    f => { Object.assign(f.observation.sourceOccurrences[1].members[0], { kind: 'text', start: 1, end: 3 }) },
    f => { f.observation.sourceOccurrences[1].members[0].tag = 'span' },
    f => { f.observation.sourceOccurrences[1].members[0].domPath = [0, 99] },
    f => { f.observation.sourceOccurrences[1].members.reverse() },
    f => { f.observation.sourceOccurrences[1].members[0].domPath = [0, -1] },
    f => { f.observation.sourceOccurrences[1].members[1] = structuredClone(f.observation.sourceOccurrences[1].members[0]) },
    f => { f.root.children.push(structuredClone(f.root.children[0])) },
    f => { f.root.style.display = 'block' },
    f => { f.root.children[0].style.position = 'absolute' },
    f => { delete f.root.source },
    f => { f.root.source.componentId = 'wrong' },
    f => { f.add([...rootPath, 'unknown'], [f.root.children[2]]); f.occurrences.delete(key([...rootPath, 'unknown'])) },
    f => { f.add([...rootPath, 'overlap'], f.root.children.slice(1)) },
    f => { f.add([...f.group.path, 'escaping'], f.root.children.slice(1)) },
    f => { f.add([...f.group.path, 'same-box'], [f.root]) },
  ]
  for (const [index, mutate] of mutations.entries()) {
    const f = fixture()
    mutate(f)
    const before = inputState(f)
    assert.throws(() => planSourceMembers(f.observation, f.occurrences), /source members/, `case ${index}`)
    assert.deepEqual(inputState(f), before, `case ${index} changed caller input`)
  }
})

test('standalone multi-root members cannot invent an enclosing browser formatting context', () => {
  const f = fixture()
  f.observation.roots = f.root.children.slice(0, 2)
  f.observation.roots.forEach((node, index) => { node.domPath = [index] })
  f.observation.sourceOccurrences = [{ ...source(rootPath), correspondence: 'observed', members: f.observation.roots.map(node => ({
    kind: 'element', tag: node.tag, domPath: node.domPath, presentation: { state: 'box-observed' },
  })) }]
  f.occurrences.delete(key(f.group.path))
  assert.throws(() => planSourceMembers(f.observation, f.occurrences), /containing box/)
})

test('grouping refuses incomplete contributions, foreign ownership and repeated owners without input changes', () => {
  for (const which of ['missing', 'wrong-owner', 'repeated', 'noncontiguous']) {
    const f = fixture(), seen = new Set(), adapter = planSourceMembers(f.observation, f.occurrences)
    if (which === 'missing') f.plans.pop()
    if (which === 'repeated') seen.add(key(f.group.path))
    if (which === 'noncontiguous') {
      f.root.children.splice(1, 0, f.root.children.pop())
      f.plans.splice(1, 0, f.plans.pop())
    }
    const before = inputState(f), priorSeen = new Set(seen)
    assert.throws(() => adapter.group(f.root, f.plans, which === 'wrong-owner' ? { path: ['foreign'] } : f.owner, seen), /source members/)
    assert.deepEqual(inputState(f), before)
    assert.deepEqual(seen, priorSeen)
  }
})

test('a late outer grouping refusal does not retain a successfully staged inner occurrence', () => {
  const f = fixture(), last = element(12)
  f.root.children.push(last)
  f.plans.push({ kind: 'frame', observation: last, native: {} })
  f.observation.sourceOccurrences[1].members.push({ kind: 'element', tag: last.tag, domPath: last.domPath,
    presentation: { state: 'box-observed' } })
  f.add([...f.group.path, 'inner'], f.root.children.slice(0, 2))
  const adapter = planSourceMembers(f.observation, f.occurrences), before = inputState(f), seen = new Set(['already-seen'])
  assert.throws(() => adapter.group(f.root, f.plans, f.owner, seen), /contiguous/)
  assert.deepEqual(inputState(f), before)
  assert.deepEqual(seen, new Set(['already-seen']))
})

test('selection geometry requires finite nonnegative member boxes before native construction', () => {
  for (const [field, value] of [['x', NaN], ['y', Infinity], ['width', -1], ['height', -1], ['width', Infinity]]) {
    const f = fixture()
    f.root.children[0].bounds[field] = value
    const before = inputState(f), seen = new Set()
    assert.throws(() => {
      const adapter = planSourceMembers(f.observation, f.occurrences)
      adapter.group(f.root, f.plans, f.owner, seen)
    }, /source members|selection|geometry/, `${field}=${value}`)
    assert.deepEqual(inputState(f), before)
    assert.equal(seen.size, 0)
  }
  assert.throws(() => memberSelection([]), /source members|selection|geometry/)
  for (const extent of [1e308, 3e38]) {
    const plans = [-extent, extent].map(x => ({ observation: { bounds: { x, y: 0, width: extent, height: 20 } } }))
    const before = structuredClone(plans)
    assert.throws(() => memberSelection(plans), /source members|selection|geometry/, `union extent ${extent}`)
    assert.deepEqual(plans, before)
  }
})

test('fragment ownership cannot skip a separately boxed source ancestor', () => {
  for (const stage of ['owner', 'group']) {
    const f = fixture(), previousPath = f.group.path
    const boxed = f.add([...rootPath, 'boxed'], [f.root.children[2]])
    f.group.path = [...boxed.path, 'group']
    f.occurrences.delete(key(previousPath))
    f.occurrences.set(key(f.group.path), f.group)
    f.observation.sourceOccurrences[1].path = f.group.path
    f.plans[2] = { ...f.plans[2], kind: 'component', occurrence: boxed, children: [] }
    const before = inputState(f), seen = new Set([key(rootPath), key(boxed.path)]), priorSeen = new Set(seen)
    assert.throws(() => {
      const adapter = planSourceMembers(f.observation, f.occurrences)
      if (stage === 'owner') adapter.owner(f.root.children[0], f.owner)
      else adapter.group(f.root, f.plans, f.owner, seen)
    }, /source members/, stage)
    assert.deepEqual(inputState(f), before)
    assert.deepEqual(seen, priorSeen)
  }
})

test('private contribution wrappers cannot hide a component belonging to another source owner', () => {
  const f = fixture(), foreign = f.add([...rootPath, 'sibling'], [f.root.children[2]])
  const original = f.plans[0]
  f.plans[0] = { ...original, children: [{ kind: 'frame', observation: original.observation, children: [
    { kind: 'component', occurrence: foreign, observation: f.root.children[2], children: [] },
  ] }] }
  f.plans[2] = { ...f.plans[2], kind: 'component', occurrence: foreign, children: [] }
  const before = inputState(f), seen = new Set()
  assert.throws(() => {
    const adapter = planSourceMembers(f.observation, f.occurrences)
    adapter.group(f.root, f.plans, f.owner, seen)
  }, /source members/)
  assert.deepEqual(inputState(f), before)
  assert.equal(seen.size, 0)
})

test('derived fragment grid FILL survives two saves without acquiring authored inheritance ownership', async () => {
  const metadata = extra => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
    schema: 'platformkit.design-export.v1', scope: 'source-composition-observed-aliases', ...extra,
  }) }]
  let graph = new SceneGraph()
  const page = graph.getPages()[0], fragment = graph.createNode('COMPONENT', page.id, {
    name: 'Fragment', layoutMode: 'NONE', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    clipsContent: false, fills: [], pluginData: metadata({ cssFragment: { version: 1 } }),
  })
  const cell = graph.createNode('FRAME', fragment.id, { name: 'Cell', layoutMode: 'HORIZONTAL',
    width: 20, height: 20, primaryAxisSizing: 'FILL', counterAxisSizing: 'HUG', pluginData: metadata({}) })
  graph.createNode('RECTANGLE', cell.id, { width: 20, height: 20 })
  const grid = graph.createNode('COMPONENT', page.id, { name: 'Grid', layoutMode: 'GRID', width: 200, height: 20,
    primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED', gridTemplateColumns: [{ sizing: 'FR', value: 1 }], pluginData: metadata({}) })
  graph.createInstance(fragment.id, grid.id, { name: 'Fragment placement' })
  graph.createInstance(grid.id, page.id, { name: 'Edited' })
  graph.createInstance(grid.id, page.id, { name: 'Untouched' })
  const named = name => [...graph.getAllNodes()].find(node => node.name === name)
  const member = name => graph.getChildren(graph.getChildren(named(name).id)[0].id)[0]
  async function reopen() {
    const before = structuredClone([...graph.getAllNodes()]), bytes = await exportFigFile(graph)
    assert.deepEqual([...graph.getAllNodes()], before, 'serialization does not create source ownership')
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
  }
  for (let cycle = 0; cycle < 2; cycle++) {
    await reopen()
    for (const name of ['Edited', 'Untouched']) {
      const node = member(name)
      assert.equal(node.primaryAxisSizing, 'FILL')
      assert.equal(Object.hasOwn(ancestryOverrides(graph, node), `${node.id}:primaryAxisSizing`), false)
    }
  }
  const editor = createEditor({ graph }), before = structuredClone(ancestryOverrides(graph, member('Edited')))
  editor.updateNodeWithUndo(member('Edited').id, { primaryAxisSizing: 'HUG' })
  editor.undoAction()
  assert.equal(member('Edited').primaryAxisSizing, 'FILL')
  assert.deepEqual(ancestryOverrides(graph, member('Edited')), before)
  editor.redoAction()
  await Promise.resolve()
  for (let cycle = 0; cycle < 2; cycle++) {
    await reopen()
    assert.equal(member('Edited').primaryAxisSizing, 'HUG', 'explicit axis sizing wins over derived context')
    assert.equal(member('Untouched').primaryAxisSizing, 'FILL')
  }
  graph.updateNode(member('Grid').id, { primaryAxisSizing: 'FIXED' })
  graph.syncInstances(named('Grid').id)
  assert.equal(member('Edited').primaryAxisSizing, 'HUG', 'authored axis remains local')
  assert.equal(member('Untouched').primaryAxisSizing, 'FIXED', 'derived evidence does not freeze later source changes')
})
