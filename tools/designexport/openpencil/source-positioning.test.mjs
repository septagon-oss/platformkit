import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import { planSourceAbsolute, sourceAbsoluteRecord } from './source-positioning.mjs'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { fontManager } from '@open-pencil/core/text'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { nodeChangeToProps } from '@open-pencil/fig/node-change'
import { parseFigBuffer } from '@open-pencil/fig'
import { importNodeChanges, populateAllLazyFigImportRoots } from '@open-pencil/core/kiwi'

const data = record => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
  schema: 'platformkit.design-export.v1', scope: 'source-composition-layout', ...record,
}) }]

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
  const root = graph.createNode('FRAME', graph.getPages()[0].id, { name: 'Parent', width: 240, height: 20,
    layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED', pluginData: data({}) })
  graph.createNode('RECTANGLE', root.id, { name: 'Flow', width: 20, height: 20 })
  const { cssPosition, ...placement } = planSourceAbsolute(fixture('right', 'bottom').node, fixture('right', 'bottom').parent)
  const positioned = graph.createNode('FRAME', root.id, { name: 'Positioned', ...placement, layoutMode: 'VERTICAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', pluginData: data({ cssPosition }) })
  const content = graph.createNode('FRAME', positioned.id, { name: 'Private fill', layoutMode: 'VERTICAL',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'FILL', layoutAlignSelf: 'STRETCH', pluginData: data({}) })
  graph.createNode('RECTANGLE', content.id, { width: 20, height: 20 })
  graph.createNode('RECTANGLE', root.id, { name: 'Ordinary absolute', x: 5, y: 6, width: 900, height: 900, layoutPositioning: 'ABSOLUTE' })
  const named = name => [...graph.getAllNodes()].find(node => node.name === name)
  for (let cycle = 0; cycle < 3; cycle++) {
    assert.equal(named('Private fill').counterAxisSizing, 'FILL', 'private frame sizing survives import before layout')
    computeLayout(graph, named('Parent').id)
    assert.equal(named('Private fill').width, 40, 'private content fills the positioned parent, not the outer flow')
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

test('master synchronization retains parent-owned fractional grid sizing and badge insets', () => {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', page.id, {
    name: 'Tile', width: 43.328125, height: 43.328125, layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG', pluginData: data({ cssBox: { version: 1, aspectRatio: 1 } }),
  })
  graph.createNode('FRAME', master.id, {
    width: 19.203125, height: 20, layoutMode: 'VERTICAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', layoutPositioning: 'ABSOLUTE',
    horizontalConstraint: 'MAX', verticalConstraint: 'MAX', pluginData: data({ cssPosition: {
      version: 1, horizontal: { edge: 'right', inset: 1 }, vertical: { edge: 'bottom', inset: 1 },
    } }),
  })
  computeLayout(graph, master.id)
  const grid = graph.createNode('FRAME', page.id, {
    width: 320, layoutMode: 'GRID', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED',
    gridTemplateColumns: Array.from({ length: 6 }, () => ({ sizing: 'FR', value: 1 })),
    gridTemplateRows: [], gridColumnGap: 12, gridRowGap: 12, pluginData: data({}),
  })
  const instance = graph.createInstance(master.id, grid.id, {
    primaryAxisSizing: 'FILL', counterAxisSizing: 'HUG', layoutAlignSelf: 'STRETCH',
  })
  computeLayout(graph, grid.id)
  const before = structuredClone(instance), badge = graph.getChildren(instance.id)[0]
  assert.notEqual(instance.width, master.width, 'grid and master have distinct resolved widths')
  assert.equal(instance.width - badge.width - badge.x, 1)
  graph.syncInstances(master.id)
  assert.equal(instance.width, before.width, 'a canonical master does not own its occurrence fill width')
  assert.equal(instance.width - badge.width - badge.x, 1)
  assert.deepEqual(instance.overrides, before.overrides, 'derived sizing does not invent an authored override')
  assert.equal(sourceAbsoluteRecord(badge).horizontal.inset, 1)
})

test('canonical synchronization preserves both physical fill axes across layout direction changes', () => {
  for (const layoutMode of ['HORIZONTAL', 'VERTICAL']) {
    const graph = new SceneGraph(), page = graph.getPages()[0]
    const master = graph.createNode('COMPONENT', page.id, {
      width: 13, height: 17, layoutMode, primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED',
    })
    const fill = graph.createInstance(master.id, page.id, {
      width: 33, height: 37, primaryAxisSizing: 'FILL', counterAxisSizing: 'FILL',
    })
    const fixed = graph.createInstance(master.id, page.id)
    graph.updateNode(master.id, { width: 55, height: 66, layoutMode: layoutMode === 'HORIZONTAL' ? 'VERTICAL' : 'HORIZONTAL' })
    graph.syncInstances(master.id)
    assert.deepEqual([fill.width, fill.height], [33, 37])
    assert.deepEqual([fixed.width, fixed.height], [55, 66])
    assert.deepEqual(fill.overrides, {})
  }
})

test('linked placements retain exact fractional insets and native moves through two saves', async () => {
  for (const horizontal of ['left', 'right']) for (const moved of [false, true]) {
    let graph = new SceneGraph(), page = graph.getPages()[0]
    const master = graph.createNode('COMPONENT', page.id, {
      name: 'Master', width: 320, height: 160, layoutMode: 'HORIZONTAL',
      primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', pluginData: data({}),
    })
    graph.createNode('FRAME', master.id, {
      name: 'Badge', width: 40.1, height: 20, layoutMode: 'VERTICAL', layoutPositioning: 'ABSOLUTE',
      primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED',
      horizontalConstraint: horizontal === 'left' ? 'MIN' : 'MAX', verticalConstraint: 'MIN',
      pluginData: [...data({ example: 'canonical', cssPosition: {
        version: 1, horizontal: { edge: horizontal, inset: horizontal === 'left' ? .1 : 1 },
        vertical: { edge: 'top', inset: .2 },
      } }), { pluginId: 'another-owner', key: 'evidence', value: 'untouched' }],
    })
    computeLayout(graph, master.id)
    graph.createInstance(master.id, page.id, { name: 'Edited' })
    graph.createInstance(master.id, page.id, { name: 'Sibling' })
    const named = name => [...graph.getAllNodes()].find(node => node.name === name)
    const badge = name => graph.getChildren(named(name).id)[0]
    const evidence = name => badge(name).pluginData.filter(item => item.pluginId !== 'open-pencil')
    const canonical = structuredClone(badge('Master').pluginData)
    if (moved) {
      const editor = createEditor({ graph }), before = structuredClone(badge('Edited').pluginData)
      editor.updateNodeWithUndo(badge('Edited').id, { x: 55.123456789 }, 'Move badge')
      editor.undoAction()
      assert.deepEqual(badge('Edited').pluginData, before)
      editor.redoAction()
    }
    let expected = structuredClone(sourceAbsoluteRecord(badge('Edited')))
    for (let cycle = 0; cycle < 3; cycle++) {
      assert.deepEqual(sourceAbsoluteRecord(badge('Edited')), expected, `${horizontal}/${moved}/${cycle}: exact authored anchors`)
      assert.deepEqual(evidence('Master'), canonical, 'master source and unrelated provenance are unchanged')
      assert.deepEqual(evidence('Sibling'), canonical, 'sibling source and unrelated provenance are unchanged')
      assert.equal(JSON.parse(badge('Edited').pluginData[0].value).example, 'canonical')
      assert.deepEqual(badge('Edited').pluginData[1], canonical[1], 'unrelated plugin data is retained')
      assert.equal(named('Edited').componentId, named('Master').id)
      if (moved && cycle === 1) {
        const editor = createEditor({ graph })
        editor.updateNodeWithUndo(badge('Edited').id, { x: badge('Edited').x, y: 7.123456789 }, 'Move after import')
        assert.deepEqual(sourceAbsoluteRecord(badge('Edited')).horizontal, expected.horizontal, 'an unchanged coordinate does not reauthor its anchor')
        editor.undoAction()
        assert.deepEqual(sourceAbsoluteRecord(badge('Edited')), expected, 'post-import undo restores exact authored anchors')
        editor.redoAction()
        expected = structuredClone(sourceAbsoluteRecord(badge('Edited')))
      }
      graph.updateNode(named('Edited').id, { width: cycle ? 390 : 320 })
      computeLayout(graph, named('Edited').id)
      const parent = named('Edited'), current = badge('Edited')
      assert.equal(current.x, horizontal === 'left' ? expected.horizontal.inset : parent.width - current.width - expected.horizontal.inset)
      assert.equal(current.y, expected.vertical.inset)
      if (cycle < 2) {
        const bytes = await exportFigFile(graph)
        if (cycle === 0 && !moved) {
          const changes = parseFigBuffer(bytes.slice().buffer).nodeChanges
          const overrideOf = nodes => nodes.find(node => node.name === 'Edited').symbolData.symbolOverrides.find(node => node.pluginData)
          for (const legacy of [false, true]) {
            const external = structuredClone(changes), override = overrideOf(external)
            override.transform.m02 = Math.fround(override.transform.m02 + 10)
            if (legacy) delete override.pluginData
            const imported = importNodeChanges(external)
            populateAllLazyFigImportRoots(imported)
            const parent = [...imported.getAllNodes()].find(node => node.name === 'Edited')
            const target = imported.getChildren(parent.id)[0], x = target.x
            assert.equal(x, override.transform.m02)
            computeLayout(imported, parent.id)
            assert.equal(target.x, x, `${horizontal}/${legacy}: a native external move is not snapped back`)
            assert.equal(JSON.parse(target.pluginData[0].value).example, 'canonical')
          }
          for (const corrupt of [
            override => { delete override.transform },
            override => { override.pluginData.push(structuredClone(override.pluginData[0])) },
            override => { override.pluginData[0].value = 'invalid JSON' },
            override => {
              const value = JSON.parse(override.pluginData[0].value)
              value.cssPosition.horizontal.inset = null
              override.pluginData[0].value = JSON.stringify(value)
            },
            override => {
              const value = JSON.parse(override.pluginData[0].value)
              value.wireGeometry.size.x = null
              override.pluginData[0].value = JSON.stringify(value)
            },
          ]) {
            const invalid = structuredClone(changes)
            corrupt(overrideOf(invalid))
            assert.throws(() => populateAllLazyFigImportRoots(importNodeChanges(invalid)), /source absolute/)
          }
        }
        graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
      }
    }
  }
})

test('automatic absolute frames save current derived geometry after native text and parent edits', async t => {
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
  const previous = getTextMeasurer()
  t.after(() => { setTextMeasurer(previous); renderer.destroy() })
  fontManager.markLoaded('Inter', 'Regular', Uint8Array.from(readFileSync(
    new URL('./node_modules/@open-pencil/core/assets/Inter-Regular.ttf', import.meta.url))).buffer)
  await renderer.loadFonts()
  setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
  let graph = new SceneGraph()
  const page = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', page.id, {
    name: 'Badge master', width: 80, height: 24, layoutMode: 'VERTICAL',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED', counterAxisAlign: 'STRETCH',
    paddingLeft: 6, paddingRight: 6, paddingTop: 2, paddingBottom: 2, pluginData: data({}),
    componentPropertyDefinitions: [{ id: '88:1', name: 'Label', type: 'TEXT', defaultValue: '7' }],
  })
  graph.createNode('TEXT', master.id, {
    text: '7', width: 68, height: 20, fontFamily: 'Inter', fontWeight: 400, fontSize: 14, lineHeight: 20,
    textAutoResize: 'HEIGHT', textDirection: 'LTR', textAlignHorizontal: 'LEFT', layoutAlignSelf: 'STRETCH',
    pluginData: data({ textWrap: 'normal-v1' }), componentPropertyReferences: [{ propertyId: '88:1', field: 'TEXT' }],
  })
  const root = graph.createNode('FRAME', page.id, {
    name: 'Parent', width: 320, height: 160, layoutMode: 'HORIZONTAL',
    primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', pluginData: data({}),
  })
  const wrapper = graph.createNode('FRAME', root.id, {
    name: 'Automatic placement', width: 80, height: 24, layoutMode: 'VERTICAL',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED', layoutPositioning: 'ABSOLUTE',
    horizontalConstraint: 'MAX', verticalConstraint: 'MAX', pluginData: data({ cssPosition: {
      version: 1, horizontal: { edge: 'right', inset: 12.25 }, vertical: { edge: 'bottom', inset: 2.5 },
      autoSize: { oppositeBorder: 3 },
    } }),
  })
  graph.createInstance(master.id, wrapper.id, { counterAxisSizing: 'FILL', layoutAlignSelf: 'STRETCH' })
  graph.createNode('RECTANGLE', root.id, {
    name: 'Ordinary absolute', x: 5, y: 6, width: 10, height: 12, layoutPositioning: 'ABSOLUTE',
  })
  computeLayout(graph, master.id)
  computeLayout(graph, root.id)
  const roundtrip = async () => parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  const initial = await exportFigFile(graph)
  const named = name => [...graph.getAllNodes()].find(node => node.name === name)
  const geometry = node => [node.x, node.y, node.width, node.height]
  for (const [label, width, expected] of [
    ['99', null, [278.375, 133.5, 29.375, 24]],
    ['Available for exchange with another collector in this album collection', 319, [3, 113.5, 303.75, 44]],
  ]) {
    // Import first: stale raw geometry exists only after the initial FIG roundtrip.
    graph = await parseFigFile(initial.slice().buffer, { populate: 'all' })
    const content = graph.getChildren(named('Automatic placement').id)[0]
    const definition = graph.getNode(content.componentId).componentPropertyDefinitions[0]
    const editor = createEditor({ graph })
    editor.setCanvasKit(ck, renderer)
    editor.setInstanceComponentProperty(content.id, definition.id, label)
    if (width !== null) editor.updateNodeWithUndo(named('Parent').id, { width }, 'Resize')
    for (let cycle = 0; cycle < 3; cycle++) {
      assert.deepEqual(geometry(named('Automatic placement')), expected, `save ${cycle}: current source geometry for ${label}`)
      assert.deepEqual(geometry(named('Ordinary absolute')), [5, 6, 10, 12], `save ${cycle}: unmarked geometry is preserved`)
      if (cycle < 2) graph = await roundtrip()
    }
  }
})

test('source placement saves preserve imported affine terms and explicitly edited native rotation', async () => {
  let graph = new SceneGraph(), serial = 100
  const transforms = [
    { m00: Math.cos(Math.PI / 6), m01: -.5, m02: 30, m10: .5, m11: Math.cos(Math.PI / 6), m12: 40 },
    { m00: 1, m01: .5, m02: 30, m10: 0, m11: 1, m12: 40 },
    { m00: -1, m01: 0, m02: 30, m10: 0, m11: 1, m12: 40 },
  ]
  for (const marked of [false, true]) for (const [index, transform] of transforms.entries()) {
    graph.createNode('FRAME', graph.getPages()[0].id, {
      ...nodeChangeToProps({ guid: { sessionID: 88, localID: serial++ }, type: 'FRAME',
        name: `${marked}/${index}`, size: { x: 40, y: 24 }, transform, stackPositioning: 'ABSOLUTE' }, []),
      pluginData: marked ? data({ cssPosition: {
        version: 1, horizontal: { edge: 'left', inset: 30 }, vertical: { edge: 'top', inset: 40 },
      } }) : [],
    })
  }
  const frames = () => graph.getChildren(graph.getPages()[0].id)
  const geometry = () => frames().map(node => ({
    name: node.name, values: ['x', 'y', 'width', 'height', 'rotation'].map(field => node[field]), flipX: node.flipX,
  }))
  for (const edited of [false, true]) {
    if (edited) for (const node of frames().filter(node => node.name.startsWith('true/'))) {
      createEditor({ graph }).updateNodeWithUndo(node.id, { rotation: 60, width: 70, height: 33 }, 'Rotate and resize')
    }
    const before = geometry()
    for (let save = 0; save < 2; save++) {
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      for (const actual of geometry()) {
        const expected = before.find(node => node.name === actual.name)
        assert.equal(actual.flipX, expected.flipX)
        actual.values.forEach((value, i) => assert.ok(Math.abs(value - expected.values[i]) < 1e-5,
          `${actual.name}/${edited}/${save}/${i}: ${value} versus ${expected.values[i]}`))
      }
      if (!edited) for (const node of frames()) {
        const original = transforms[Number(node.name.split('/')[1])]
        for (const field of ['m00', 'm01', 'm10', 'm11']) {
          assert.ok(Math.abs(node.source.fig.rawTransform[field] - original[field]) < 1e-7, `${node.name}: preserve ${field}`)
        }
      }
    }
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
