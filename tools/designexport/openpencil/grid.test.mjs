import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'

const fr = value => ({ sizing: 'FR', value })
const fixed = value => ({ sizing: 'FIXED', value })
const box = node => [node.x, node.y, node.width, node.height]

function fixture({ rows = [], height = 40, sizing = 'HUG', nested = false } = {}) {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const parent = nested ? graph.createNode('FRAME', page.id, {
    width: 320, height: 1, layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG',
    counterAxisSizing: 'FIXED', counterAxisAlign: 'STRETCH',
  }) : page
  const grid = graph.createNode('COMPONENT', parent.id, {
    name: 'Editable grid', width: 320, height, layoutMode: 'GRID',
    primaryAxisSizing: sizing, counterAxisSizing: nested ? 'FILL' : 'FIXED',
    gridTemplateColumns: [fr(1), fr(1)], gridTemplateRows: rows, gridRowGap: 12, gridColumnGap: 12,
  })
  const children = Array.from({ length: 3 }, (_, i) => {
    const child = graph.createNode('FRAME', grid.id, {
      name: `Cell ${i}`, width: 60, height: 40, layoutMode: 'VERTICAL',
      primaryAxisSizing: 'HUG', counterAxisSizing: 'FILL', counterAxisAlign: 'STRETCH',
    })
    graph.createNode('RECTANGLE', child.id, { width: 60, height: 40 })
    return child
  })
  return { graph, page, parent, grid, children }
}

for (const nested of [false, true]) test(`grid HUG height uses its rows, including nested=${nested}`, () => {
  const { graph, parent, grid, children } = fixture({ nested })
  computeLayout(graph, nested ? parent.id : grid.id)
  assert.equal(grid.height, 92)
  if (nested) assert.equal(parent.height, 92)
  assert.deepEqual(children.map(box), [[0, 0, 154, 40], [166, 0, 154, 40], [0, 52, 154, 40]])
  graph.updateNode(nested ? parent.id : grid.id, { width: 212 })
  computeLayout(graph, nested ? parent.id : grid.id)
  assert.deepEqual(children.map(box), [[0, 0, 100, 40], [112, 0, 100, 40], [0, 52, 100, 40]])
})

test('explicit fixed and fractional rows retain FIXED height while HUG follows track content', () => {
  for (const sizing of ['FIXED', 'HUG']) {
    const { graph, grid, children } = fixture({ rows: [fixed(50), fixed(60)], height: 300, sizing })
    computeLayout(graph, grid.id)
    assert.equal(grid.height, sizing === 'FIXED' ? 300 : 122)
    assert.deepEqual(children.map(node => node.y), [0, 0, 62])
  }
  const { graph, grid, children } = fixture({ rows: [fr(1), fr(2)], height: 312, sizing: 'FIXED' })
  computeLayout(graph, grid.id)
  assert.equal(grid.height, 312)
  assert.deepEqual(children.map(node => node.y), [0, 0, 112])
})

test('grid measures nested text at the assigned cell width in one layout pass', () => {
  const { graph, grid, children } = fixture(), previous = getTextMeasurer()
  for (const child of children) {
    // This case opts into shrinking below content's automatic minimum. FR
    // tracks without that opt-in retain their native content minimum.
    graph.updateNode(child.id, { minWidth: 0 })
    graph.deleteNode(child.childIds[0])
    graph.createNode('TEXT', child.id, {
      text: 'A wrapping paragraph', width: 60, height: 20, textAutoResize: 'HEIGHT',
    })
  }
  try {
    setTextMeasurer((_node, width) => ({ width: width ?? 280, height: Math.ceil(280 / (width ?? 280)) * 20 }))
    for (const [width, cellWidth, rowHeight, total] of [[320, 154, 40, 92], [212, 100, 60, 132], [600, 294, 20, 52]]) {
      graph.updateNode(grid.id, { width })
      computeLayout(graph, grid.id)
      assert.equal(grid.height, total)
      assert.deepEqual(children.map(box), [[0, 0, cellWidth, rowHeight], [cellWidth + 12, 0, cellWidth, rowHeight],
        [0, rowHeight + 12, cellWidth, rowHeight]])
      for (const child of children) assert.deepEqual(box(graph.getChildren(child.id)[0]), [0, 0, cellWidth, rowHeight])
    }
  } finally { setTextMeasurer(previous) }
})

test('grid placement spans, padding, hidden and absolute children do not freeze automatic rows', () => {
  const { graph, grid, children } = fixture()
  graph.updateNode(grid.id, { paddingTop: 4, paddingRight: 8, paddingBottom: 6, paddingLeft: 8 })
  graph.updateNode(children[0].id, { gridPosition: { column: 1, row: 1, columnSpan: 2, rowSpan: 1 } })
  graph.updateNode(children[1].id, { visible: false })
  const absolute = graph.createNode('RECTANGLE', grid.id, { x: 9, y: 7, width: 13, height: 500, layoutPositioning: 'ABSOLUTE' })
  computeLayout(graph, grid.id)
  assert.equal(grid.height, 102)
  assert.deepEqual(box(children[0]), [8, 4, 304, 40])
  assert.deepEqual(box(children[2]), [8, 56, 146, 40])
  assert.deepEqual(box(absolute), [9, 7, 13, 500])
})

test('grid leaves preserve explicit stretch while fixed leaves retain their size', () => {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const grid = graph.createNode('FRAME', page.id, {
    layoutMode: 'GRID', width: 212, height: 50,
    gridTemplateColumns: [fr(1), fr(1)], gridTemplateRows: [fixed(50)], gridColumnGap: 12,
  })
  const fill = graph.createNode('RECTANGLE', grid.id, { width: 20, height: 10, layoutAlignSelf: 'STRETCH' })
  const fixedLeaf = graph.createNode('RECTANGLE', grid.id, { width: 20, height: 10 })
  computeLayout(graph, grid.id)
  assert.deepEqual(box(fill), [0, 0, 100, 50])
  assert.deepEqual(box(fixedLeaf), [112, 0, 20, 10])
})

test('automatic grid spans do not anchor every cell to the first track', () => {
  const graph = new SceneGraph(), grid = graph.createNode('FRAME', graph.getPages()[0].id, {
    layoutMode: 'GRID', width: 300, height: 40, gridTemplateColumns: [fr(1), fr(1), fr(1)],
  })
  const children = [2, 1, 1].map(columnSpan => graph.createNode('RECTANGLE', grid.id, {
    width: 20, height: 20, gridPosition: { column: 0, row: 0, columnSpan, rowSpan: 1 },
  }))
  computeLayout(graph, grid.id)
  assert.deepEqual(children.map(box), [[0, 0, 20, 20], [200, 0, 20, 20], [0, 20, 20, 20]])
})
