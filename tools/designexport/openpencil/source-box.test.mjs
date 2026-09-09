import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { planSourceBox, clipSourceOverflow } from './source-box.mjs'

const source = cssBox => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
  schema: 'platformkit.design-export.v1', scope: 'source-composition-layout', cssBox,
}) }]

test('source box planning owns only evidenced padding clips and width-led border-box aspect ratios', () => {
  const node = { style: { display: 'flex', 'box-sizing': 'border-box', 'aspect-ratio': '3 / 2',
    'overflow-x': 'hidden', 'overflow-y': 'hidden' }, sizing: { width: 'auto', height: 'auto' } }
  const before = structuredClone(node), borders = [2, 5, 4, 3]
  assert.deepEqual(planSourceBox(node, borders), { clipsContent: true, cssBox: { version: 1, aspectRatio: 1.5, overflow: borders } })
  assert.deepEqual(node, before)
  for (const change of [
    value => { value.style['aspect-ratio'] = '0 / 2' },
    value => { value.style['aspect-ratio'] = '1 / 0' },
    value => { value.style['aspect-ratio'] = 'auto 3 / 2' },
    value => { value.style['box-sizing'] = 'content-box' },
    value => { value.style.display = 'grid' },
    value => { value.sizing.height = '20px' },
    value => { value.style['overflow-x'] = 'visible' },
  ]) {
    const changed = structuredClone(node); change(changed)
    assert.throws(() => planSourceBox(changed, borders), /Native component: source box/)
  }
  assert.deepEqual(planSourceBox({ style: { 'aspect-ratio': 'auto', 'overflow-x': 'visible', 'overflow-y': 'visible' } }, borders), {})
})

test('source aspect ratio drives root and nested layout without changing ordinary native sizing through two saves', async () => {
  let graph = new SceneGraph()
  const root = graph.createNode('FRAME', graph.getPages()[0].id, { name: 'Ratio', width: 240,
    layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG',
    primaryAxisAlign: 'CENTER', counterAxisAlign: 'CENTER', pluginData: source({ version: 1, aspectRatio: 1.5 }) })
  graph.createNode('FRAME', root.id, { name: 'Nested', width: 60, layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG', pluginData: source({ version: 1, aspectRatio: 2 }) })
  const named = name => [...graph.getAllNodes()].find(node => node.name === name)
  for (let cycle = 0; cycle < 3; cycle++) {
    for (const width of [390, 320, 240]) {
      graph.updateNode(named('Ratio').id, { width }); computeLayout(graph, named('Ratio').id)
      assert.ok(Math.abs(named('Ratio').height - width / 1.5) <= 1 / 64)
      assert.deepEqual([named('Nested').width, named('Nested').height], [60, 30])
      assert.ok(Math.abs(named('Nested').y - (width / 1.5 - 30) / 2) <= 1 / 64)
    }
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
  graph.updateNode(named('Ratio').id, { counterAxisSizing: 'FIXED', height: 99 })
  computeLayout(graph, named('Ratio').id); assert.equal(named('Ratio').height, 99, 'explicit fixed height releases the source ratio')
  const plain = graph.createNode('FRAME', graph.getPages()[0].id, { width: 240, layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG' })
  computeLayout(graph, plain.id); assert.equal(plain.height, 0)
})

test('padding clips normalize overlapping radii before subtracting borders and handle empty inner boxes', () => {
  const graph = new SceneGraph(), node = graph.createNode('FRAME', graph.getPages()[0].id, {
    width: 120, height: 40, cornerRadius: 999, pluginData: source({ version: 1, overflow: [2, 5, 4, 3] }),
  })
  const calls = [], r = { ck: { ClipOp: { Intersect: 'intersect' }, LTRBRect: (...values) => values } }
  const canvas = { clipRRect: values => calls.push([...values]), clipRect: values => calls.push(values) }
  assert.equal(clipSourceOverflow(r, canvas, graph, node), true)
  assert.deepEqual(calls.pop(), [3, 2, 115, 36, 17, 18, 15, 18, 15, 16, 17, 16])
  graph.updateNode(node.id, { width: 7 })
  assert.equal(clipSourceOverflow(r, canvas, graph, node), true); assert.deepEqual(calls.pop(), [0, 0, 0, 0])
  graph.updateNode(node.id, { pluginData: [] })
  assert.equal(clipSourceOverflow(r, canvas, graph, node), false); assert.deepEqual(calls, [])
})
