import assert from 'node:assert/strict'
import { test } from 'node:test'
import { isDeepStrictEqual } from 'node:util'
import { SceneGraph } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
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

function composedFixture(depth = 0) {
  const graph = fixture(), owner = named(graph, 'Choices')
  graph.updateNode(owner.id, { componentPropertyDefinitions: [...owner.componentPropertyDefinitions,
    { id: '30:3', name: 'Shared label', type: 'TEXT', defaultValue: 'Label' }] })
  for (const [index, component] of graph.getChildren(owner.id).entries()) {
    let parent = component
    for (let level = 0; level < depth; level++) parent = graph.createNode('FRAME', parent.id,
      { name: `Layout ${index}/${level}`, width: 80, height: 24 })
    const parts = [
      { name: 'Lookalike', text: 'Label' },
      { name: `Different display name ${index}`, text: 'Label', componentPropertyReferences: [{ propertyId: '30:3', field: 'TEXT' }] },
    ]
    for (const props of index % 2 ? parts.toReversed() : parts) graph.createNode('TEXT', parent.id,
      { width: 40, height: 20, ...props })
    graph.syncInstances(component.id)
  }
  graph.updateNode(named(graph, 'Edited').id, { pluginData: [{ pluginId: 'fixture', key: 'source-path', value: 'unchanged occurrence' }] })
  return graph
}

for (const imported of [false, true]) for (const nested of [false, true]) for (const operation of ['switch', 'undo', 'redo']) {
  test(`failed variant measurement retains state: imported=${imported}, nested=${nested}, operation=${operation}`, async () => {
    let graph = composedFixture(2)
    const original = getTextMeasurer()
    for (const node of graph.getAllNodes()) {
      if (node.type === 'TEXT' && node.componentPropertyReferences.length) {
        graph.updateNode(node.id, { layoutPositioning: 'ABSOLUTE', textAutoResize: 'WIDTH_AND_HEIGHT' })
      }
    }
    if (nested) {
      const parent = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Wrapper master' })
      graph.insertChildAt(named(graph, 'Edited').id, parent.id, 0)
      graph.createInstance(parent.id, graph.getPages()[0].id, { name: 'Wrapper placement' })
    }
    if (imported) graph = await reopen(graph)
    const placedInstance = () => nested ? graph.getChildren(named(graph, 'Wrapper placement').id)[0] : named(graph, 'Edited')
    const actions = createEditor({ graph }), instance = placedInstance()
    try {
      setTextMeasurer(node => ({ width: node.text.length * 10, height: 20 }))
      actions.setInstanceComponentProperty(instance.id, '30:3', 'My own label')
      if (operation !== 'switch') actions.setInstanceComponentProperty(instance.id, '30:1', '')
      if (operation === 'redo') actions.undoAction()
      const apply = () => operation === 'switch' ? actions.setInstanceComponentProperty(instance.id, '30:1', '') : actions[`${operation}Action`]()
      const before = structuredClone([...graph.nodes]), index = structuredClone(graph.instanceIndex)
      const handles = new Map(graph.nodes), history = [actions.undo.canUndo, actions.undo.canRedo], events = []
      const deletedParents = structuredClone(graph.deletedNodeParents)
      const stops = ['node:created', 'node:updated', 'node:deleted', 'node:reordered'].map(event => graph.emitter.on(event, id => events.push(id)))
      setTextMeasurer(() => { throw new Error('Variant measurement unavailable') })
      try { assert.throws(apply, /Variant measurement unavailable/) }
      finally { stops.forEach(stop => stop()) }
      assert.ok(isDeepStrictEqual([...graph.nodes], before), 'failed switch must retain every node, override and source cache')
      assert.deepEqual(graph.instanceIndex, index)
      assert.deepEqual(graph.deletedNodeParents, deletedParents)
      assert.deepEqual(events, [], 'failed measurement publishes no native mutations')
      for (const [id, node] of handles) assert.equal(graph.getNode(id), node)
      assert.deepEqual([actions.undo.canUndo, actions.undo.canRedo], history)
      await new Promise(resolve => setTimeout(resolve, 0))
      assert.ok(isDeepStrictEqual([...graph.nodes], before), 'deferred synchronization must not replay a refused switch')
      setTextMeasurer(node => ({ width: node.text.length * 10, height: 20 }))
      apply()
      const expected = operation === 'undo' ? ' padded,a ' : ''
      assert.equal(actions.getInstanceComponentPropertyValue(instance.id, { type: 'VARIANT', name: 'value' }), expected)
      for (let cycle = 0; cycle < 2; cycle++) {
        graph = await reopen(graph)
        actions.replaceGraph(graph)
        const placed = placedInstance()
        assert.equal(actions.getInstanceComponentPropertyValue(placed.id, { type: 'VARIANT', name: 'value' }), expected)
        assert.equal(actions.getInstanceComponentPropertyValue(placed.id, { id: '30:3', type: 'TEXT' }), 'My own label')
      }
    } finally { setTextMeasurer(original); actions.replaceGraph(new SceneGraph()) }
  })
}

for (const imported of [false, true]) for (const depth of [0, 2]) {
  test(`variants inherit one shared label interface through switches, history and two saves: imported=${imported}, depth=${depth}`, async () => {
    let graph = imported ? await reopen(composedFixture(depth)) : composedFixture(depth)
    const actions = createEditor({ graph }), instance = named(graph, 'Edited')
    const owner = named(graph, 'Choices'), definitions = actions.getInstanceComponentPropertyDefinitions(instance.id)
    const label = definitions.find(item => item.type === 'TEXT'), choice = definitions.find(item => item.name === 'value')
    const reference = structuredClone(graph.getChildren(owner.id))
    const sibling = structuredClone([named(graph, 'Untouched'), ...graph.getChildren(named(graph, 'Untouched').id)])
    const origin = structuredClone(instance.pluginData.filter(item => item.pluginId === 'fixture'))
    function check(value, text) {
      const live = named(graph, 'Edited'), properties = actions.getInstanceComponentPropertyDefinitions(live.id)
      assert.equal(properties.length, 3)
      assert.equal(actions.getInstanceComponentPropertyValue(live.id, properties.find(item => item.type === 'TEXT')), text)
      assert.equal(actions.getInstanceComponentPropertyValue(live.id, properties.find(item => item.name === 'value')), value)
      let parent = live
      for (let level = 0; level < depth; level++) parent = graph.getChildren(parent.id).find(node => node.type === 'FRAME')
      const children = graph.getChildren(parent.id)
      assert.equal(children.filter(node => node.componentPropertyReferences.some(ref => ref.propertyId === label.id)).length, 1)
      assert.equal(children.find(node => node.componentPropertyReferences.some(ref => ref.propertyId === label.id)).text, text)
      assert.equal(children.find(node => node.name === 'Lookalike').text, 'Label')
      assert.deepEqual(live.pluginData.filter(item => item.pluginId === 'fixture'), origin)
    }
    try {
      actions.setInstanceComponentProperty(instance.id, label.id, 'My own label')
      check(' padded,a ', 'My own label')
      for (const value of ['', 'baseline', ' padded,a ']) {
        actions.setInstanceComponentProperty(instance.id, choice.id, value)
        check(value, 'My own label')
      }
      for (const value of ['baseline', '', ' padded,a ']) {
        actions.undoAction()
        check(value, 'My own label')
      }
      actions.undoAction()
      check(' padded,a ', 'Label')
      actions.redoAction()
      for (const value of ['', 'baseline', ' padded,a ']) {
        actions.redoAction()
        check(value, 'My own label')
      }
      assert.deepEqual(graph.getChildren(owner.id), reference)
      assert.deepEqual([named(graph, 'Untouched'), ...graph.getChildren(named(graph, 'Untouched').id)], sibling)
      for (let cycle = 0; cycle < 2; cycle++) {
        graph = await reopen(graph)
        actions.replaceGraph(graph)
        check(' padded,a ', 'My own label')
      }
    } finally { actions.replaceGraph(new SceneGraph()) }
  })
}

test('shared variant correspondence refuses missing, ambiguous and incompatible targets before graph writes', () => {
  for (const mutate of [
    ({ target }) => { target.componentPropertyReferences = [] },
    ({ graph, target }) => { graph.createNode('TEXT', target.parentId, { componentPropertyReferences: structuredClone(target.componentPropertyReferences) }) },
    ({ owner }) => { owner.componentPropertyDefinitions.push({ id: '30:3', name: 'Other', type: 'BOOLEAN', defaultValue: true }) },
    ({ component, owner }) => { component.componentPropertyDefinitions.push(structuredClone(owner.componentPropertyDefinitions.at(-1))) },
    ({ target }) => { target.type = 'RECTANGLE' },
    ({ graph, target }) => { graph.insertChildAt(target.id, graph.getNode(target.parentId).parentId, 0) },
    ({ placed }) => { placed.componentPropertyReferences = [] },
    ({ graph, placed }) => { graph.getChildren(placed.parentId).find(node => node.name === 'Lookalike').source.editedFields.push('text')
      graph.getChildren(placed.parentId).find(node => node.name === 'Lookalike').text = 'Independent edit' },
  ]) {
    const graph = composedFixture(2), owner = named(graph, 'Choices'), component = graph.getChildren(owner.id)[0]
    const text = root => {
      let parent = root
      while (graph.getChildren(parent.id).some(node => node.type === 'FRAME')) parent = graph.getChildren(parent.id).find(node => node.type === 'FRAME')
      return graph.getChildren(parent.id).find(node => node.componentPropertyReferences.some(ref => ref.propertyId === '30:3'))
    }
    const instance = named(graph, 'Edited'), placed = text(instance), target = text(component)
    mutate({ graph, owner, component, placed, target })
    const before = structuredClone([...graph.nodes]), events = []
    const stops = ['node:created', 'node:updated', 'node:deleted'].map(event => graph.emitter.on(event, id => events.push(id)))
    try { assert.throws(() => graph.swapInstanceComponent(instance.id, component.id), /shared variant|Shared variant|subtree history/) }
    finally { stops.forEach(stop => stop()) }
    assert.deepEqual([...graph.nodes], before)
    assert.deepEqual(events, [])
  }
})

test('multiple inherited properties keep distinct ancestors and refuse ambiguous layout merges', async () => {
  let graph = composedFixture(1), owner = named(graph, 'Choices')
  owner.componentPropertyDefinitions.push({ id: '30:4', name: 'Caption', type: 'TEXT', defaultValue: 'Label' })
  for (const component of graph.getChildren(owner.id)) {
    const frame = graph.createNode('FRAME', component.id, { name: 'Same parent name', width: 80, height: 24 })
    graph.createNode('TEXT', frame.id, { text: 'Label', width: 40, height: 20,
      componentPropertyReferences: [{ propertyId: '30:4', field: 'TEXT' }] })
    graph.syncInstances(component.id)
  }
  const instance = named(graph, 'Edited'), actions = createEditor({ graph })
  try {
    actions.setInstanceComponentProperty(instance.id, '30:3', 'One label')
    actions.setInstanceComponentProperty(instance.id, '30:4', 'Another caption')
    const frames = graph.getChildren(instance.id).filter(node => node.type === 'FRAME').map(node => node.id)
    actions.setInstanceComponentProperty(instance.id, '30:1', '')
    assert.deepEqual(graph.getChildren(instance.id).filter(node => node.type === 'FRAME').map(node => node.id), frames)
    const target = named(graph, 'Independent name 0'), [labelFrame, captionFrame] = graph.getChildren(target.id).filter(node => node.type === 'FRAME')
    const caption = graph.getChildren(captionFrame.id)[0]
    graph.insertChildAt(caption.id, labelFrame.id, 0)
    const before = structuredClone([...graph.nodes])
    assert.throws(() => actions.setInstanceComponentProperty(instance.id, '30:1', 'baseline'), /Incompatible shared variant property ancestry/)
    assert.deepEqual([...graph.nodes], before)
    graph.insertChildAt(caption.id, captionFrame.id, 0)
    actions.setInstanceComponentProperty(instance.id, '30:1', 'baseline')
    for (let cycle = 0; cycle < 2; cycle++) {
      graph = await reopen(graph)
      actions.replaceGraph(graph)
      for (const [name, expected] of [['Shared label', 'One label'], ['Caption', 'Another caption']]) {
        const live = named(graph, 'Edited'), definition = actions.getInstanceComponentPropertyDefinitions(live.id).find(item => item.name === name)
        assert.equal(actions.getInstanceComponentPropertyValue(live.id, definition), expected)
      }
    }
  } finally { actions.replaceGraph(new SceneGraph()) }
})

for (const imported of [false, true]) {
  test(`a nested variant inherits label edits without changing its containing template or sibling: imported=${imported}`, async () => {
    let graph = composedFixture(), page = graph.getPages()[0]
    const master = graph.createNode('COMPONENT', page.id, { name: 'Containing master', width: 100, height: 30 })
    graph.createInstance(named(graph, 'Independent name 2').id, master.id, { name: 'Choice slot' })
    graph.createInstance(master.id, page.id, { name: 'Containing placement' })
    graph.createInstance(master.id, page.id, { name: 'Containing preview' })
    if (imported) graph = await reopen(graph)
    const nested = () => graph.getChildren(named(graph, 'Containing placement').id)[0]
    const actions = createEditor({ graph }), before = structuredClone([...graph.getAllNodes()].filter(node =>
      node.type === 'COMPONENT' || node.parentId === named(graph, 'Containing master').id))
    const value = () => actions.getInstanceComponentPropertyValue(nested().id,
      actions.getInstanceComponentPropertyDefinitions(nested().id).find(item => item.id === '30:1'))
    try {
      assert.equal(value(), ' padded,a ')
      actions.setInstanceComponentProperty(nested().id, '30:3', 'Nested edit')
      actions.setInstanceComponentProperty(nested().id, '30:1', '')
      assert.equal(value(), '')
      actions.undoAction()
      assert.equal(value(), ' padded,a ')
      actions.undoAction()
      assert.equal(actions.getInstanceComponentPropertyValue(nested().id,
        actions.getInstanceComponentPropertyDefinitions(nested().id).find(item => item.id === '30:3')), 'Label')
      actions.redoAction()
      actions.redoAction()
      assert.deepEqual([...graph.getAllNodes()].filter(node => node.type === 'COMPONENT' ||
        node.parentId === named(graph, 'Containing master').id), before)
      for (let cycle = 0; cycle < 2; cycle++) {
        graph = await reopen(graph)
        actions.replaceGraph(graph)
        assert.equal(actions.getInstanceComponentPropertyValue(nested().id,
          actions.getInstanceComponentPropertyDefinitions(nested().id).find(item => item.id === '30:3')), 'Nested edit')
        assert.equal(value(), '')
        let component = graph.getNode(nested().componentId)
        while (component.type === 'INSTANCE') component = graph.getNode(component.componentId)
        assert.equal(component.componentPropertyValues.value, '')
        const preview = graph.getChildren(named(graph, 'Containing preview').id)[0]
        assert.equal(actions.getInstanceComponentPropertyValue(preview.id,
          actions.getInstanceComponentPropertyDefinitions(preview.id).find(item => item.id === '30:3')), 'Label')
      }
    } finally { actions.replaceGraph(new SceneGraph()) }
  })
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
