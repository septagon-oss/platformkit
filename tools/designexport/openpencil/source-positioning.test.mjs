import assert from 'node:assert/strict'
import { test } from 'node:test'
import { planSourceAbsolute } from './source-positioning.mjs'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout } from '@open-pencil/core/layout'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

function fixture(horizontal = 'left', vertical = 'top') {
  const parent = { style: { position: 'relative', display: 'flex', transform: 'none', translate: 'none', rotate: 'none', scale: 'none',
    zoom: '1', 'overflow-x': 'visible', 'overflow-y': 'visible',
    'border-left-width': '3px', 'border-right-width': '5px', 'border-top-width': '2px', 'border-bottom-width': '4px' },
  bounds: { x: 20, y: 30, width: 240, height: 160 } }
  const node = { kind: 'element', style: { position: 'absolute', display: 'flex', 'box-sizing': 'border-box', 'z-index': 'auto',
    'overflow-x': 'visible', 'overflow-y': 'visible',
    'margin-top': '0px', 'margin-right': '0px', 'margin-bottom': '0px', 'margin-left': '0px' },
  sizing: { width: '40px', height: '24px', top: 'auto', right: 'auto', bottom: 'auto', left: 'auto' },
  bounds: { width: 40, height: 24,
    x: horizontal === 'left' ? 20 + 3 + 7.25 : 20 + 240 - 5 - 7.25 - 40,
    y: vertical === 'top' ? 30 + 2 - 1.5 : 30 + 160 - 4 + 1.5 - 24 } }
  node.sizing[horizontal] = '7.25px'; node.sizing[vertical] = '-1.5px'
  return { node, parent }
}

test('absolute placement derives all four corners from authored insets and the parent padding edge', () => {
  for (const horizontal of ['left', 'right']) for (const vertical of ['top', 'bottom']) {
    const { node, parent } = fixture(horizontal, vertical), before = structuredClone({ node, parent })
    const plan = planSourceAbsolute(node, parent)
    assert.deepEqual(plan, { x: node.bounds.x - parent.bounds.x, y: node.bounds.y - parent.bounds.y,
      width: 40, height: 24, layoutPositioning: 'ABSOLUTE',
      horizontalConstraint: horizontal === 'left' ? 'MIN' : 'MAX', verticalConstraint: vertical === 'top' ? 'MIN' : 'MAX',
      cssPosition: { version: 1, horizontal: { edge: horizontal, inset: horizontal === 'left' ? 10.25 : 12.25 },
        vertical: { edge: vertical, inset: vertical === 'top' ? .5 : 2.5 } } })
    assert.deepEqual({ node, parent }, before, 'planning does not mutate source observations')
  }
})

test('source absolute layout stays out of flow and releases explicitly edited axes through two saves', async () => {
  let graph = new SceneGraph()
  const data = record => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
    schema: 'platformkit.design-export.v1', scope: 'source-composition-layout', ...record,
  }) }]
  const root = graph.createNode('FRAME', graph.getPages()[0].id, { name: 'Parent', width: 240, height: 20,
    layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED', pluginData: data({}) })
  graph.createNode('RECTANGLE', root.id, { name: 'Flow', width: 20, height: 20 })
  const { cssPosition, ...placement } = planSourceAbsolute(fixture('right', 'bottom').node, fixture('right', 'bottom').parent)
  graph.createNode('FRAME', root.id, { name: 'Positioned', ...placement, layoutMode: 'VERTICAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', pluginData: data({ cssPosition }) })
  graph.createNode('RECTANGLE', root.id, { name: 'Ordinary absolute', x: 5, y: 6, width: 900, height: 900, layoutPositioning: 'ABSOLUTE' })
  const named = name => [...graph.getAllNodes()].find(node => node.name === name)
  for (let cycle = 0; cycle < 3; cycle++) {
    computeLayout(graph, named('Parent').id)
    assert.equal(named('Parent').height, 20)
    assert.equal(named('Positioned').x, 187.75)
    assert.equal(named('Positioned').y, -6.5, 'overflow does not inflate the parent')
    assert.deepEqual([named('Ordinary absolute').x, named('Ordinary absolute').y], [5, 6])
    const editor = createEditor({ graph }), before = structuredClone([...graph.getAllNodes()])
    editor.updateNodeWithUndo(named('Positioned').id, { x: 55 }, 'Place manually')
    computeLayout(graph, named('Parent').id)
    assert.equal(named('Positioned').x, 55, 'an explicit native placement is not snapped back to source CSS')
    editor.undoAction()
    assert.deepEqual([...graph.getAllNodes()], before)
    editor.redoAction(); assert.equal(named('Positioned').x, 55)
    editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
})

test('absolute placement refuses guessed containing blocks, static positions and unsupported sizing', () => {
  for (const change of [
    ({ parent }) => { parent.style.position = 'static' },
    ({ parent }) => { parent.style.display = 'grid' },
    ({ parent }) => { parent.style['overflow-x'] = 'scroll' },
    ({ parent }) => { parent.style.translate = '1px' },
    ({ node }) => { node.style.position = 'fixed' },
    ({ node }) => { node.style['z-index'] = '1' },
    ({ node }) => { node.style['overflow-x'] = 'hidden' },
    ({ node }) => { node.sizing.left = 'auto' },
    ({ node }) => { node.sizing.right = '3px' },
    ({ node }) => { node.sizing.left = '10%' },
    ({ node }) => { node.sizing.left = 'calc(1px + 1%)' },
    ({ node }) => { node.sizing.left = 'Infinitypx' },
    ({ node }) => { node.sizing.width = 'auto' },
    ({ node }) => { node.sizing.height = '100%' },
    ({ node }) => { node.sizing.width = '0px' },
    ({ node }) => { node.style['margin-left'] = 'auto' },
    ({ node }) => { node.bounds.x += .1 },
    ({ node }) => { node.bounds.width += .1 },
  ]) {
    const input = fixture(); change(input)
    assert.throws(() => planSourceAbsolute(input.node, input.parent), /Native component: source absolute/)
  }
})
