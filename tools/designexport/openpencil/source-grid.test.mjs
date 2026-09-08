import assert from 'node:assert/strict'
import { test } from 'node:test'
import { sourceGridTracks } from './source-grid.mjs'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

test('CSS track planning preserves authored fractions and minmax instead of observed pixel widths', () => {
  assert.deepEqual(sourceGridTracks('repeat(2, minmax(0px, 2fr) 30px) auto'), [
    { sizing: 'FR', value: 2, minValue: 0 }, { sizing: 'FIXED', value: 30 },
    { sizing: 'FR', value: 2, minValue: 0 }, { sizing: 'FIXED', value: 30 }, { sizing: 'AUTO', value: 0 },
  ])
  assert.deepEqual(sourceGridTracks('minmax(20px, 1fr)'), [{ sizing: 'FR', value: 1, minValue: 20 }])
  assert.deepEqual(sourceGridTracks('none'), [])
  for (const value of ['', 'subgrid', 'repeat(auto-fill, 1fr)', 'repeat(0, 1fr)', 'repeat(4097, 1fr)',
    'repeat(2, repeat(2, 1fr))', 'repeat(2,)', 'repeat(2 1fr)', '1fr)', '[named] 1fr',
    '10%', 'minmax(auto, 1fr)', 'minmax(0px, minmax(0px, 1fr))', '0fr', '-1px', '1e999px', 'minmax(1%, 1fr)']) {
    assert.throws(() => sourceGridTracks(value), /Native component: source grid/, value)
  }
})

test('native fixed track minima retain their layout through two FIG saves and further resizing', async () => {
  let graph = new SceneGraph()
  const grid = graph.createNode('FRAME', graph.getPages()[0].id, { name: 'Minimum grid', width: 200, height: 40,
    layoutMode: 'GRID', gridTemplateColumns: [{ sizing: 'FR', value: 1, minValue: 0 }, { sizing: 'FR', value: 1, minValue: 30 }] })
  for (let index = 0; index < 2; index++) graph.createNode('RECTANGLE', grid.id, { width: 280, height: 20, layoutAlignSelf: 'STRETCH' })
  for (let cycle = 0; cycle < 3; cycle++) {
    const root = [...graph.getAllNodes()].find(node => node.name === 'Minimum grid')
    assert.deepEqual(root.gridTemplateColumns, [{ sizing: 'FR', value: 1, minValue: 0 }, { sizing: 'FR', value: 1, minValue: 30 }])
    for (const [width, expected] of [[200, [100, 100]], [50, [20, 30]]]) {
      graph.updateNode(root.id, { width })
      computeLayout(graph, root.id)
      assert.deepEqual(graph.getChildren(root.id).map(node => node.width), expected)
    }
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
})
