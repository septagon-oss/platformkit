import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { computeAllLayouts, setTextMeasurer } from '@open-pencil/core/layout'
import { parseFigBuffer } from '@open-pencil/fig'

// Deliberately deterministic: these are native state tests, not font/visual tests.
setTextMeasurer(node => ({ width: node.text.length * 10, height: 20 }))

function named(graph, name) {
  const matches = [...graph.getAllNodes()].filter(node => node.name === name)
  assert.equal(matches.length, 1, `one node named ${name}`)
  return matches[0]
}

function descendants(graph, node) {
  return [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
}

function labels(graph, node) {
  return descendants(graph, node).filter(child => child.type === 'TEXT').map(child => child.text)
}

async function save(graph) {
  return exportFigFile(graph)
}

async function reopen(graph) {
  const bytes = await save(graph)
  return parseFigFile(bytes.slice().buffer, { populate: 'all' })
}

function editor(graph) {
  return createEditor({ graph, skipInitialGraphSetup: true })
}

function nestedFixture() {
  const graph = new SceneGraph()
  const page = graph.getPages()[0]
  const leaf = graph.createNode('COMPONENT', page.id, { name: 'Leaf' })
  graph.createNode('TEXT', leaf.id, { text: 'Default', width: 70, height: 20 })
  const wrapper = graph.createNode('COMPONENT', page.id, {
    name: 'Wrapper',
    componentPropertyDefinitions: [
      { id: '30:1', name: 'Label', type: 'TEXT', defaultValue: 'Default' },
    ],
  })
  const nested = graph.createInstance(leaf.id, wrapper.id, { name: 'Nested' })
  graph.updateNode(graph.getChildren(nested.id)[0].id, {
    componentPropertyReferences: [{ propertyId: '30:1', field: 'TEXT' }],
  })
  graph.createInstance(wrapper.id, page.id, { name: 'Edited' })
  graph.createInstance(wrapper.id, page.id, { name: 'Untouched' })
  return graph
}

test('native nested property edits survive successive saves without changing siblings or masters', async () => {
  let graph = nestedFixture()
  for (const value of ['First edit', 'Second edit', '']) {
    editor(graph).setInstanceComponentProperty(named(graph, 'Edited').id, '30:1', value)
    graph = await reopen(graph)
    const edited = named(graph, 'Edited')
    assert.deepEqual(labels(graph, edited), [value])
    assert.equal(edited.componentPropertyAssignments['30:1'], value)
    for (const name of ['Untouched', 'Wrapper', 'Leaf']) {
      assert.deepEqual(labels(graph, named(graph, name)), ['Default'], name)
    }
    for (const name of ['Edited', 'Untouched']) {
      const instance = named(graph, name)
      assert.equal(instance.type, 'INSTANCE')
      assert.equal(graph.getNode(instance.componentId)?.name, 'Wrapper')
    }
    assert.equal(named(graph, 'Wrapper').type, 'COMPONENT')
    assert.equal(named(graph, 'Leaf').type, 'COMPONENT')
  }
})

test('repeated FIG saves do not accumulate duplicate native override paths', async () => {
  let graph = nestedFixture()
  const counts = []
  for (let cycle = 0; cycle < 4; cycle++) {
    editor(graph).setInstanceComponentProperty(named(graph, 'Edited').id, '30:1', `Edit ${cycle}`)
    const bytes = await save(graph)
    const raw = parseFigBuffer(bytes.slice().buffer)
    let count = 0
    for (const change of raw.nodeChanges) {
      const overrides = change.symbolData?.symbolOverrides ?? []
      const paths = overrides.map(override => JSON.stringify(override.guidPath))
      assert.equal(new Set(paths).size, paths.length, `unique paths for ${change.name}`)
      count += paths.length
    }
    counts.push(count)
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
  }
  assert.ok(counts[0] > 0, 'fixture must exercise native overrides')
  assert.deepEqual(counts, [counts[0], counts[0], counts[0], counts[0]])
})

async function buttonFixture() {
  const graph = new SceneGraph()
  const page = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', page.id, {
    name: 'Button', layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    paddingLeft: 12, paddingRight: 12,
    componentPropertyDefinitions: [
      { id: '40:1', name: 'Label', type: 'TEXT', defaultValue: 'Save' },
    ],
  })
  graph.createNode('TEXT', master.id, {
    text: 'Save', width: 40, height: 20, textAutoResize: 'WIDTH_AND_HEIGHT',
    componentPropertyReferences: [{ propertyId: '40:1', field: 'TEXT' }],
  })
  graph.createInstance(master.id, page.id, { name: 'Edited' })
  graph.createInstance(master.id, page.id, { name: 'Untouched' })
  computeAllLayouts(graph)
  return reopen(graph)
}

function geometry(graph, node) {
  return descendants(graph, node).map(child => ({
    x: child.x, y: child.y, width: child.width, height: child.height,
    textAutoResize: child.textAutoResize,
  }))
}

test('imported HUG frame and child resize, undo, redo and reopen with intact master links', async () => {
  const graph = await buttonFixture()
  const actions = editor(graph)
  const untouched = structuredClone(descendants(graph, named(graph, 'Untouched')))
  const master = structuredClone(descendants(graph, named(graph, 'Button')))
  const initial = geometry(graph, named(graph, 'Edited'))

  function check(current, text, width) {
    const instance = named(current, 'Edited')
    const label = current.getChildren(instance.id)[0]
    assert.equal(instance.width, width + 24)
    assert.equal(instance.height, 20)
    assert.equal(instance.type, 'INSTANCE')
    assert.equal(instance.source.format, 'fig')
    assert.equal(current.getNode(instance.componentId)?.name, 'Button')
    assert.equal(label.text, text)
    assert.deepEqual(geometry(current, label), [{
      x: 12, y: 0, width, height: 20, textAutoResize: 'WIDTH_AND_HEIGHT',
    }])
    for (const name of ['Untouched', 'Button']) {
      assert.equal(named(current, name).width, 64)
      assert.deepEqual(labels(current, named(current, name)), ['Save'])
    }
    if (current === graph) {
      assert.deepEqual(descendants(graph, named(graph, 'Untouched')), untouched)
      assert.deepEqual(descendants(graph, named(graph, 'Button')), master)
    }
  }

  check(graph, 'Save', 40)
  actions.setInstanceComponentProperty(named(graph, 'Edited').id, '40:1', 'Longer caption')
  check(graph, 'Longer caption', 140)
  actions.undo.undo()
  check(graph, 'Save', 40)
  assert.deepEqual(geometry(graph, named(graph, 'Edited')), initial)
  actions.undo.redo()
  check(graph, 'Longer caption', 140)
  check(await reopen(graph), 'Longer caption', 140)
})

test('undo restores unusual imported geometry exactly rather than remeasuring it', async () => {
  const graph = await buttonFixture()
  const instance = named(graph, 'Edited')
  const label = graph.getChildren(instance.id)[0]
  graph.updateNode(instance.id, { width: 99.25, height: 27.5 })
  graph.updateNode(label.id, { x: 17.25, y: 3.5, width: 71.75, height: 23.5 })
  const before = geometry(graph, instance)
  const actions = editor(graph)
  actions.setInstanceComponentProperty(instance.id, '40:1', 'Longer caption')
  const after = geometry(graph, instance)
  assert.notDeepEqual(after, before)
  actions.undo.undo()
  assert.deepEqual(geometry(graph, instance), before)
  actions.undo.redo()
  assert.deepEqual(geometry(graph, instance), after)
})
