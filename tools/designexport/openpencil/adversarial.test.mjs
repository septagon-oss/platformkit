import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'

function descendants(graph, node) {
  return [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
}

function named(graph, parent, name) {
  const matches = descendants(graph, parent).filter(node => node.name === name)
  assert.equal(matches.length, 1, name)
  return matches[0]
}

function fixture(deep) {
  const graph = new SceneGraph()
  const page = graph.getPages()[0]
  const leaf = graph.createNode('COMPONENT', page.id, { name: 'Leaf' })
  const content = graph.createNode('FRAME', leaf.id, { name: 'Content' })
  graph.createNode('TEXT', content.id, { name: 'Label', text: 'Default', width: 70, height: 20 })
  graph.createNode('TEXT', content.id, { name: 'Guard', text: 'Unchanged', width: 90, height: 20 })
  let source = leaf
  if (deep) {
    source = graph.createNode('COMPONENT', page.id, { name: 'Middle' })
    graph.createInstance(leaf.id, source.id, { name: 'Inner' })
  }
  const wrapper = graph.createNode('COMPONENT', page.id, {
    name: 'Wrapper',
    componentPropertyDefinitions: [
      { id: '30:1', name: 'First label', type: 'TEXT', defaultValue: 'Default' },
      { id: '30:2', name: 'Second label', type: 'TEXT', defaultValue: 'Default' },
    ],
  })
  for (const [name, propertyId] of [['First', '30:1'], ['Second', '30:2']]) {
    const nested = graph.createInstance(source.id, wrapper.id, { name })
    graph.updateNode(named(graph, nested, 'Label').id, {
      componentPropertyReferences: [{ propertyId, field: 'TEXT' }],
    })
  }
  graph.createInstance(wrapper.id, page.id, { name: 'Edited' })
  graph.createInstance(wrapper.id, page.id, { name: 'Untouched' })
  return graph
}

async function reopen(graph) {
  const bytes = await exportFigFile(graph)
  return parseFigFile(bytes.slice().buffer, { populate: 'all' })
}

for (const deep of [false, true]) {
  for (const imported of [false, true]) {
    test(`identity paths retain reordered repeated siblings, deep=${deep}, imported=${imported}`, async () => {
      let graph = fixture(deep)
      if (imported) graph = await reopen(graph)
      for (let cycle = 0; cycle < 2; cycle++) {
        const page = graph.getPages()[0]
        const edited = named(graph, page, 'Edited')
        const first = named(graph, edited, 'First')
        const label = named(graph, first, 'Label')
        // Reorder both nested instances and text siblings before invoking the
        // editor: serializer-only fixes must not hide editor mis-targeting.
        graph.reorderChild(first.id, edited.id, 2)
        graph.reorderChild(label.id, label.parentId, 2)
        const editor = createEditor({ graph, skipInitialGraphSetup: true })
        for (const [name, id] of [['First', '30:1'], ['Second', '30:2']]) {
          editor.setInstanceComponentProperty(edited.id, id, `${name} ${cycle}`)
          assert.equal(named(graph, named(graph, edited, name), 'Label').text, `${name} ${cycle}`)
        }
        const bytes = await exportFigFile(graph)
        const raw = parseFigBuffer(bytes.slice().buffer)
        const guid = value => `${value.sessionID}:${value.localID}`
        const names = new Map(raw.nodeChanges.map(node => [guid(node.guid), node.name]))
        const overrides = raw.nodeChanges.find(node => node.name === 'Edited').symbolData.symbolOverrides
        for (const override of overrides.filter(value => value.textData)) {
          const expected = override.textData.characters.split(' ')[0]
          assert.equal(names.get(guid(override.guidPath.guids[0])), expected)
          assert.equal(override.guidPath.guids.length, deep ? 4 : 3)
        }
        graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
        const restored = named(graph, graph.getPages()[0], 'Edited')
        for (const name of ['First', 'Second']) {
          assert.equal(named(graph, named(graph, restored, name), 'Label').text, `${name} ${cycle}`)
        }
        for (const node of graph.getAllNodes()) {
          if (node.name === 'Guard') assert.equal(node.text, 'Unchanged')
        }
        for (const name of ['Untouched', 'Wrapper', 'Leaf', ...(deep ? ['Middle'] : [])]) {
          const source = named(graph, graph.getPages()[0], name)
          assert(descendants(graph, source).filter(node => node.name === 'Label')
            .every(node => node.text === 'Default'), name)
        }
      }
    })
  }
}

test('ambiguous imported repeated-instance lineage rejects before mutation and export', async () => {
  let graph = new SceneGraph()
  const page = graph.getPages()[0]
  const leaf = graph.createNode('COMPONENT', page.id, { name: 'Empty' })
  const wrapper = graph.createNode('COMPONENT', page.id, {
    name: 'Wrapper',
    componentPropertyDefinitions: [{ id: '31:1', name: 'Visible', type: 'BOOLEAN', defaultValue: 'true' }],
  })
  graph.createInstance(leaf.id, wrapper.id, {
    name: 'First', componentPropertyReferences: [{ propertyId: '31:1', field: 'VISIBLE' }],
  })
  graph.createInstance(leaf.id, wrapper.id, { name: 'Second' })
  graph.createInstance(wrapper.id, page.id, { name: 'Edited' })
  graph = await reopen(graph)
  const edited = named(graph, graph.getPages()[0], 'Edited')
  const importedLeaf = [...graph.getAllNodes()].find(node => node.type === 'COMPONENT' && node.name === 'Empty')
  // Model imported correspondence loss explicitly. No descendant witness can
  // disambiguate these empty instances; names and child order are not identity.
  for (const child of graph.getChildren(edited.id)) graph.updateNode(child.id, { componentId: importedLeaf.id })
  const editor = createEditor({ graph, skipInitialGraphSetup: true })
  const before = structuredClone([...graph.getAllNodes()])
  assert.throws(() => editor.setInstanceComponentProperty(edited.id, '31:1', 'false'), /native source identity/)
  assert.deepEqual([...graph.getAllNodes()], before)
  await assert.rejects(() => exportFigFile(graph), /native source identity/)
  assert.deepEqual([...graph.getAllNodes()], before)
})

test('invalid explicit and component-chain source identities reject without edits', async () => {
  for (const explicit of [false, 'missing', 'unrelated']) {
    const graph = fixture(false)
    const edited = named(graph, graph.getPages()[0], 'Edited')
    const first = named(graph, edited, 'First')
    if (explicit) {
      const source = explicit === 'missing' ? 'missing' : named(graph, graph.getPages()[0], 'Leaf').id
      graph.updateNode(edited.id, { overrides: { [`${first.id}:sourceComponentId`]: source } })
    } else graph.updateNode(first.id, { componentId: 'missing' })
    const editor = createEditor({ graph, skipInitialGraphSetup: true })
    const before = structuredClone([...graph.getAllNodes()])
    assert.throws(() => editor.setInstanceComponentProperty(edited.id, '30:1', 'No edit'), /native source identity/)
    assert.deepEqual([...graph.getAllNodes()], before)
    await assert.rejects(() => exportFigFile(graph), /native source identity/)
    assert.deepEqual([...graph.getAllNodes()], before)
  }
})

test('native property identifiers and reference fields fail closed', async () => {
  for (const propertyId of ['label', '01:1', '1:-1', '4294967296:1']) {
    const graph = new SceneGraph()
    graph.createNode('COMPONENT', graph.getPages()[0].id, {
      componentPropertyDefinitions: [{ id: propertyId, name: 'Label', type: 'TEXT', defaultValue: '' }],
    })
    await assert.rejects(() => exportFigFile(graph), /component property ID/)
  }
  const graph = fixture(false)
  const wrapper = named(graph, graph.getPages()[0], 'Wrapper')
  graph.updateNode(named(graph, named(graph, wrapper, 'First'), 'Label').id, {
    componentPropertyReferences: [{ propertyId: '30:1', field: 'UNKNOWN' }],
  })
  await assert.rejects(() => exportFigFile(graph), /Unsupported native component property field/)
})
