import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeAllLayouts, computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

const source = scope => ({ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
  schema: 'platformkit.design-export.v1', scope,
}) })

test('source space-between keeps its minimum gap while ordinary native rows keep automatic spacing', async () => {
  const composition = source('source-composition-observed-aliases'), frame = source('source-composition-layout')
  for (const [kind, pluginData, minimum] of [
    ['COMPONENT', [composition], true],
    ['FRAME', [frame], true],
    ['FRAME', [], false],
    ['FRAME', [source('unrelated')], false],
    ['FRAME', [{ ...frame, value: '{' }], false],
    ['FRAME', [frame, frame], false],
    ['FRAME', [{ ...frame, value: JSON.stringify({ schema: 'another-schema', scope: 'source-composition-layout' }) }], false],
  ]) for (const layoutMode of ['HORIZONTAL', 'VERTICAL']) for (const childType of minimum ? ['RECTANGLE', 'FRAME'] : ['RECTANGLE']) {
    let graph = new SceneGraph(), page = graph.getPages()[0]
    const master = graph.createNode(kind, page.id, {
      name: 'Minimum gap owner', layoutMode, primaryAxisAlign: 'SPACE_BETWEEN',
      primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG', counterAxisAlign: 'MIN',
      width: layoutMode === 'HORIZONTAL' ? 320 : 32, height: layoutMode === 'HORIZONTAL' ? 32 : 320,
      itemSpacing: 16, counterAxisSpacing: 16, pluginData,
    })
    for (let i = 0; i < 3; i++) graph.createNode(childType, master.id, {
      name: `Child ${i}`, width: layoutMode === 'HORIZONTAL' ? 100 : 32,
      height: layoutMode === 'HORIZONTAL' ? 32 : 100,
    })
    if (kind === 'COMPONENT') graph.createInstance(master.id, page.id, { name: 'Minimum gap placement' })
    for (let cycle = 0; cycle < 3; cycle++) {
      const root = [...graph.getAllNodes()].find(node => node.name ===
        (kind === 'COMPONENT' ? 'Minimum gap placement' : 'Minimum gap owner'))
      const main = layoutMode === 'HORIZONTAL' ? 'width' : 'height'
      const cross = layoutMode === 'HORIZONTAL' ? 'height' : 'width'
      for (const [size, wrap, gap, expected, extent] of [
        [320, 'NO_WRAP', 16, minimum ? [[0, 0], [116, 0], [232, 0]] : [[0, 0], [110, 0], [220, 0]], 32],
        [332, 'NO_WRAP', 16, [[0, 0], [116, 0], [232, 0]], 32],
        [500, 'NO_WRAP', 16, [[0, 0], [200, 0], [400, 0]], 32],
        [320, 'WRAP', 16, minimum ? [[0, 0], [220, 0], [0, 48]] : [[0, 0], [110, 0], [220, 0]], minimum ? 80 : 32],
        [320, 'WRAP', 0, [[0, 0], [110, 0], [220, 0]], 32],
      ]) {
        if (layoutMode === 'VERTICAL' && wrap === 'WRAP') continue
        graph.updateNode(root.id, { [main]: size, layoutWrap: wrap, itemSpacing: gap })
        computeLayout(graph, root.id)
        assert.equal(root[cross], extent, `${kind}/${layoutMode}/${cycle}/${size}/${wrap}/${minimum} cross extent`)
        assert.deepEqual(graph.getChildren(root.id).map(node => layoutMode === 'HORIZONTAL' ? [node.x, node.y] : [node.y, node.x]),
          expected, `${kind}/${childType}/${layoutMode}/${cycle}/${size}/${wrap}/${minimum} placement`)
      }
      if (pluginData.length > 1) {
        await assert.rejects(exportFigFile(graph), /Ambiguous duplicate source provenance/)
        break
      }
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  }
})

test('source layout cache invalidation restores failures and leaves ordinary imported frames alone', () => {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  for (const marked of [false, true]) {
    const root = graph.createNode('FRAME', page.id, {
      layoutMode: 'HORIZONTAL', primaryAxisAlign: 'SPACE_BETWEEN', primaryAxisSizing: 'FIXED',
      counterAxisSizing: 'HUG', width: 200, height: 20, itemSpacing: 16,
      pluginData: marked ? [source('source-composition-layout')] : [],
    })
    const child = graph.createNode('FRAME', root.id, { x: 31, y: 7, width: 50, height: 20 })
    root.source = { ...root.source, format: 'fig', editedFields: ['width'] }
    child.source = { ...child.source, format: 'fig', editedFields: [] }
    computeLayout(graph, root.id)
    assert.deepEqual([child.x, child.y], marked ? [0, 0] : [31, 7])
    graph.updateNode(root.id, { primaryAxisSizing: 'FILL', primaryAxisAlign: 'MAX' })
    computeLayout(graph, root.id)
    assert.deepEqual([child.x, child.y], marked ? [150, 0] : [31, 7], 'independent source fill layout retains its resolved width')
  }
  const root = graph.createNode('FRAME', page.id, {
    layoutMode: 'HORIZONTAL', primaryAxisAlign: 'SPACE_BETWEEN', primaryAxisSizing: 'FIXED',
    counterAxisSizing: 'HUG', width: 200, height: 20, itemSpacing: 16,
    pluginData: [source('source-composition-layout')],
  })
  const nested = graph.createNode('FRAME', root.id, {
    layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    pluginData: [source('source-composition-layout')],
  })
  const child = graph.createNode('TEXT', nested.id, { text: 'Source label', textAutoResize: 'WIDTH_AND_HEIGHT' })
  for (const node of [root, nested, child]) {
    node.source = { ...node.source, format: 'fig', editedFields: node === root ? ['itemSpacing'] : [] }
    node.figmaDerivedLayout = { width: 100, height: 20, x: 0, y: 0 }
  }
  const before = structuredClone([...graph.getAllNodes()]), previous = getTextMeasurer()
  try {
    setTextMeasurer(() => {
      for (const node of [root, nested, child]) assert.equal(node.figmaDerivedLayout, null, 'nested layout caches are derived too')
      throw new Error('Source measurement unavailable')
    })
    for (let attempt = 0; attempt < 1024; attempt++) {
      assert.throws(() => computeLayout(graph, root.id), /Source measurement unavailable/)
      assert.deepEqual([...graph.getAllNodes()], before, 'failure restores caches, geometry and source edit metadata')
    }
    setTextMeasurer(() => ({ width: 50, height: 20 }))
    computeLayout(graph, root.id)
    assert.equal(child.width, 50, 'the same native engine still measures successfully after repeated refusals')
  } finally { setTextMeasurer(previous) }
})

test('saved fixed-size ownership survives synchronization while HUG sizing and unedited instances follow their source', async () => {
  let graph = new SceneGraph()
  const page = graph.getPages()[0], find = name => [...graph.getAllNodes()].find(node => node.name === name)
  const master = graph.createNode('COMPONENT', page.id, {
    name: 'Source', width: 331, height: 80, layoutMode: 'HORIZONTAL', layoutWrap: 'WRAP',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG', primaryAxisAlign: 'SPACE_BETWEEN',
    itemSpacing: 16, counterAxisSpacing: 16, pluginData: [source('source-composition-observed-aliases')],
  })
  for (let i = 0; i < 3; i++) graph.createNode('RECTANGLE', master.id, { width: 100, height: 32 })
  computeLayout(graph, master.id)
  graph.createInstance(master.id, page.id, { name: 'Edited' })
  graph.createInstance(master.id, page.id, { name: 'Unedited' })
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  graph.updateNode(find('Edited').id, { width: 332 })
  computeLayout(graph, find('Edited').id)
  for (let cycle = 0; cycle < 2; cycle++) {
    graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    const edited = find('Edited'), unchanged = find('Unedited'), owner = find('Source')
    assert.equal(edited.overrides.width, true, 'native fixed size retains its ownership, not merely its saved pixels')
    assert.equal(Object.hasOwn(edited.overrides, 'height'), false, 'HUG height is derived, not frozen by paired FIG coordinates')
    assert.deepEqual(edited.source.editedFields, [], 'import must not fabricate authored history')
    graph.syncInstances(owner.id)
    computeAllLayouts(graph)
    assert.deepEqual([edited.width, edited.height, unchanged.width, unchanged.height], [332, 32, 331, 80])
    assert.deepEqual(graph.getChildren(edited.id).map(child => [child.x, child.y]), [[0, 0], [116, 0], [232, 0]])
  }
  const owner = find('Source'), edited = find('Edited'), unchanged = find('Unedited')
  graph.updateNode(owner.id, { width: 350, itemSpacing: 20 })
  computeLayout(graph, owner.id)
  graph.syncInstances(owner.id)
  computeAllLayouts(graph)
  assert.deepEqual([edited.width, edited.height, edited.itemSpacing], [332, 80, 20], 'fixed width stays local; source gap and derived height still change')
  assert.deepEqual([unchanged.width, unchanged.height], [350, 32], 'the unedited instance still follows source dimensions')
})
