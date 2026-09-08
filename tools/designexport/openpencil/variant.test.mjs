import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const reopen = async graph => parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
const values = ['baseline', '', ' padded,a ']

function fixture() {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const owner = graph.createNode('COMPONENT_SET', page.id, { name: 'Choices',
    componentPropertyDefinitions: [
      { id: '30:1', name: 'value', type: 'VARIANT', defaultValue: '', variantOptions: values },
      { id: '30:2', name: 'density', type: 'VARIANT', defaultValue: 'normal', variantOptions: ['normal'] },
    ] })
  const variants = values.map((value, index) => {
    const node = graph.createNode('COMPONENT', owner.id, { name: `Independent name ${index}`, x: index * 100,
      width: 80, height: 24, componentPropertyValues: { value, density: 'normal' },
      variantPropSpecs: [{ propDefId: '30:1', value }, { propDefId: '30:2', value: 'normal' }] })
    graph.createNode('RECTANGLE', node.id, { name: 'Same visible content', width: 80, height: 24 })
    return node
  })
  graph.createInstance(variants[2].id, page.id, { name: 'Edited' })
  graph.createInstance(variants[0].id, page.id, { name: 'Untouched' })
  return graph
}

test('invalid or colliding native variant renames refuse before graph or history changes', () => {
  const graph = fixture(), actions = createEditor({ graph }), owner = named(graph, 'Choices')
  try {
    for (const name of [undefined, null, 42, '', 'density']) {
      const before = structuredClone([...graph.nodes]), events = []
      const stop = graph.emitter.on('node:updated', id => events.push(id))
      try { assert.throws(() => actions.renamePropertyDefinition(owner.id, '30:1', name), /Native variant:/) }
      finally { stop() }
      assert.deepEqual([...graph.nodes], before)
      assert.deepEqual(events, [])
      assert.equal(actions.undo.canUndo, false)
    }
    actions.renamePropertyDefinition(owner.id, '30:1', 'value')
    assert.equal(actions.undo.canUndo, false, 'renaming to the same name is not an edit')
  } finally { actions.replaceGraph(new SceneGraph()) }
})

test('native variant history preserves unrelated appearance and source edits', () => {
  const graph = fixture(), actions = createEditor({ graph }), owner = named(graph, 'Choices')
  try {
    actions.renamePropertyDefinition(owner.id, '30:1', 'renamed')
    const variant = named(graph, 'Independent name 2')
    graph.updateNode(variant.id, { opacity: .5, name: 'New independent display name' })
    actions.undoAction()
    assert.equal(variant.opacity, .5)
    assert.equal(variant.name, 'New independent display name')
    assert.deepEqual(variant.componentPropertyValues, { value: ' padded,a ', density: 'normal' })
    assert.deepEqual(variant.source.editedFields, ['opacity', 'name'])
    actions.redoAction()
    assert.equal(variant.opacity, .5)
    assert.equal(variant.name, 'New independent display name')
    assert.deepEqual(variant.componentPropertyValues, { renamed: ' padded,a ', density: 'normal' })
    assert.deepEqual(new Set(variant.source.editedFields), new Set(['opacity', 'name', 'componentPropertyValues']))
  } finally { actions.replaceGraph(new SceneGraph()) }
})

for (const direction of ['undo', 'redo']) {
  test(`stale native variant ${direction} refuses atomically and retains retryable history`, () => {
    const graph = fixture(), actions = createEditor({ graph }), owner = named(graph, 'Choices')
    try {
      actions.renamePropertyDefinition(owner.id, '30:1', 'renamed')
      if (direction === 'redo') actions.undoAction()
      const variant = named(graph, 'Independent name 2'), original = structuredClone(variant.componentPropertyValues)
      graph.updateNode(variant.id, { componentPropertyValues: { ...original, density: 'other' } })
      const before = structuredClone([...graph.nodes]), events = []
      const stop = graph.emitter.on('node:updated', id => events.push(id))
      try { assert.throws(() => actions[`${direction}Action`](), /stale definition history/) }
      finally { stop() }
      assert.deepEqual([...graph.nodes], before)
      assert.deepEqual(events, [])
      assert.equal(actions.undo[direction === 'undo' ? 'canUndo' : 'canRedo'], true)
      graph.updateNode(variant.id, { componentPropertyValues: original })
      actions[`${direction}Action`]()
      assertValues(graph, direction === 'undo' ? 'value' : 'renamed')
    } finally { actions.replaceGraph(new SceneGraph()) }
  })

  test(`failed native variant ${direction} restores state and leaves history retryable`, () => {
    const graph = fixture(), actions = createEditor({ graph }), owner = named(graph, 'Choices')
    const update = graph.updateNode.bind(graph)
    try {
      actions.removePropertyDefinition(owner.id, '30:1')
      if (direction === 'redo') actions.undoAction()
      const before = structuredClone([...graph.nodes])
      let fail = true
      graph.updateNode = (id, props) => {
        update(id, props)
        if (fail && id === named(graph, 'Independent name 2').id) {
          fail = false
          throw new Error('Injected history failure')
        }
      }
      assert.throws(() => actions[`${direction}Action`](), /Injected history failure/)
      assert.deepEqual([...graph.nodes], before)
      assert.equal(actions.undo[direction === 'undo' ? 'canUndo' : 'canRedo'], true)
      actions[`${direction}Action`]()
      assert.equal(owner.componentPropertyDefinitions.some(item => item.id === '30:1'), direction === 'undo')
    } finally { graph.updateNode = update; actions.replaceGraph(new SceneGraph()) }
  })
}

for (const operation of ['rename', 'remove']) {
  test(`failed native variant ${operation} rolls back definitions, children and source metadata`, () => {
    const graph = fixture(), actions = createEditor({ graph }), owner = named(graph, 'Choices')
    const update = graph.updateNode.bind(graph), before = structuredClone([...graph.nodes])
    let fail = true
    graph.updateNode = (id, props) => {
      update(id, props)
      if (fail && id === named(graph, 'Independent name 2').id) {
        fail = false
        throw new Error('Injected write failure')
      }
    }
    try {
      assert.throws(() => operation === 'rename' ? actions.renamePropertyDefinition(owner.id, '30:1', 'renamed') :
        actions.removePropertyDefinition(owner.id, '30:1'), /Injected write failure/)
      assert.deepEqual([...graph.nodes], before)
      assert.equal(actions.undo.canUndo, false)
      assert.equal(actions.undo.canRedo, false)
    } finally { graph.updateNode = update; actions.replaceGraph(new SceneGraph()) }
  })
}

function metadata(graph) {
  return [named(graph, 'Choices'), ...values.map((_, index) => named(graph, `Independent name ${index}`))]
    .map(node => structuredClone({ id: node.id, definitions: node.componentPropertyDefinitions,
      values: node.componentPropertyValues, specs: node.variantPropSpecs, source: node.source }))
}

function assertValues(graph, property = 'value') {
  for (const [index, value] of values.entries()) {
    const node = named(graph, `Independent name ${index}`)
    assert.deepEqual(node.componentPropertyValues, { [property]: value, density: 'normal' })
    assert.deepEqual(node.variantPropSpecs, [{ propDefId: '30:1', value }, { propDefId: '30:2', value: 'normal' }])
  }
}

test('FIG variant specifications also preserve prototype-looking names on a direct definition owner', async () => {
  let graph = new SceneGraph()
  graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Direct definition owner',
    componentPropertyDefinitions: [{ id: '31:1', name: '__proto__', type: 'VARIANT', defaultValue: ' padded,a ' }],
    componentPropertyValues: { ['__proto__']: ' padded,a ' },
    variantPropSpecs: [{ propDefId: '31:1', value: ' padded,a ' }] })
  for (let cycle = 0; cycle < 2; cycle++) {
    graph = await reopen(graph)
    const owner = named(graph, 'Direct definition owner')
    assert.deepEqual(owner.componentPropertyValues, { ['__proto__']: ' padded,a ' })
    assert.deepEqual(owner.variantPropSpecs, [{ propDefId: '31:1', value: ' padded,a ' }])
  }
})

for (const imported of [false, true]) {
  test(`native empty variant defaults are explicit and independent of canvas order: imported=${imported}`, async () => {
    const graph = imported ? await reopen(fixture()) : fixture(), actions = createEditor({ graph })
    try {
      const before = structuredClone([...graph.nodes])
      assert.equal(actions.getDefaultVariantForComponentSet(named(graph, 'Choices').id).componentPropertyValues.value, '')
      assert.deepEqual([...graph.nodes], before)
      assert.equal(actions.undo.canUndo, false)
    } finally { actions.replaceGraph(new SceneGraph()) }
  })

  for (const operation of ['rename', 'remove']) {
    test(`native variant ${operation} history restores owned metadata and exact values through two saves: imported=${imported}`, async () => {
      let graph = imported ? await reopen(fixture()) : fixture()
      const actions = createEditor({ graph }), owner = named(graph, 'Choices'), before = metadata(graph)
      const instances = [...graph.getAllNodes()].filter(node => node.type === 'INSTANCE')
      const unchanged = structuredClone(instances)
      try {
        if (operation === 'rename') actions.renamePropertyDefinition(owner.id, '30:1', 'renamed')
        else actions.removePropertyDefinition(owner.id, '30:1')
        await Promise.resolve()
        if (operation === 'rename') assertValues(graph, 'renamed')
        else for (const value of values.keys()) {
          const node = named(graph, `Independent name ${value}`)
          assert.deepEqual(node.componentPropertyValues, { density: 'normal' })
          assert.deepEqual(node.variantPropSpecs, [{ propDefId: '30:2', value: 'normal' }])
        }
        const after = metadata(graph)
        actions.undoAction()
        await Promise.resolve()
        assert.deepEqual(metadata(graph), before)
        assertValues(graph)
        actions.redoAction()
        await Promise.resolve()
        assert.deepEqual(metadata(graph), after)
        actions.undoAction()
        assert.deepEqual(instances, unchanged, 'definition history leaves linked placements untouched')
        for (let cycle = 0; cycle < 2; cycle++) {
          graph = await reopen(graph)
          assertValues(graph)
          assert.equal(graph.getNode(named(graph, 'Edited').componentId).componentPropertyValues.value, ' padded,a ')
          assert.equal(graph.getNode(named(graph, 'Untouched').componentId).componentPropertyValues.value, 'baseline')
        }
      } finally { actions.replaceGraph(new SceneGraph()) }
    })

    test(`applied native variant ${operation} survives two saves without display-name inference: imported=${imported}`, async () => {
      let graph = imported ? await reopen(fixture()) : fixture()
      const actions = createEditor({ graph }), owner = named(graph, 'Choices')
      try {
        if (operation === 'rename') actions.renamePropertyDefinition(owner.id, '30:1', '__proto__')
        else actions.removePropertyDefinition(owner.id, '30:1')
        for (let cycle = 0; cycle < 2; cycle++) {
          graph = await reopen(graph)
          const definitions = named(graph, 'Choices').componentPropertyDefinitions
          assert.deepEqual(definitions.map(item => [item.id, item.name]), operation === 'rename' ?
            [['30:1', '__proto__'], ['30:2', 'density']] : [['30:2', 'density']])
          if (operation === 'rename') assertValues(graph, '__proto__')
          else for (const index of values.keys()) {
            const variant = named(graph, `Independent name ${index}`)
            assert.deepEqual(variant.componentPropertyValues, { density: 'normal' })
            assert.deepEqual(variant.variantPropSpecs, [{ propDefId: '30:2', value: 'normal' }])
          }
          assert.equal(graph.getNode(named(graph, 'Edited').componentId).name, 'Independent name 2')
          assert.equal(graph.getNode(named(graph, 'Untouched').componentId).name, 'Independent name 0')
        }
      } finally { actions.replaceGraph(new SceneGraph()) }
    })
  }
}
