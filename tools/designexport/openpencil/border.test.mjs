import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { cssDashPattern, cssBorderRecord } from './border-correction.mjs'

test('dash fitting uses whole end dashes, minimum gaps and the thin-line threshold', () => {
  assert.deepEqual(cssDashPattern(12, 2), [])
  assert.deepEqual(cssDashPattern(14, 2), [5.25, 3.5])
  assert.deepEqual(cssDashPattern(18, 2, true), [5.4, 3.6])
  assert.deepEqual(cssDashPattern(120, 2), [6, 48 / 11])
  assert.deepEqual(cssDashPattern(372, 2, true), [6, 150 / 37])
  assert.deepEqual(cssDashPattern(120, 3), [6, 36 / 13])
})

test('native dashed instances inherit the FIG dash field without sharing mutable arrays', async () => {
  let graph = new SceneGraph()
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Dashed', width: 120, height: 80,
    fills: [], dashPattern: [6, 4], strokes: [{ type: 'SOLID', visible: true, weight: 2, align: 'INSIDE', opacity: 1,
      color: { r: 1, g: 0, b: 0, a: 1 }, dashPattern: [6, 4] }],
  })
  const wrapper = graph.createNode('COMPONENT', master.parentId, { name: 'Wrapper', width: 120, height: 80, fills: [] })
  const nested = graph.createInstance(master.id, wrapper.id, { name: 'Nested' })
  const page = graph.addPage('Examples')
  graph.createInstance(wrapper.id, page.id, { name: 'Placed wrapper' })
  const direct = graph.createInstance(master.id, page.id, { name: 'Direct' })
  assert.notEqual(direct.dashPattern, master.dashPattern)
  assert.notEqual(nested.dashPattern, master.dashPattern)
  assert.notEqual(direct.strokes[0].dashPattern, master.strokes[0].dashPattern)
  for (let cycle = 0; cycle < 3; cycle++) {
    const nodes = [...graph.getAllNodes()].filter(node => ['Dashed', 'Nested', 'Direct'].includes(node.name))
    assert.equal(nodes.length, 4)
    for (const node of nodes) {
      assert.deepEqual(node.dashPattern, [6, 4], `${cycle}/${node.name}/FIG dash field`)
      assert.deepEqual(node.strokes[0].dashPattern, [6, 4], `${cycle}/${node.name}/paint dash field`)
      assert.equal(cssBorderRecord(graph, node, node.strokes[0]), undefined, 'ordinary native dashes keep native rendering')
    }
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
})

test('CSS dash interpretation follows source lineage but releases unrelated native edits', () => {
  const graph = new SceneGraph(), weight = 2
  const source = { schema: 'platformkit.design-export.v1', cssBorder: { version: 1, style: 'dashed', weight } }
  const entry = value => ({ pluginId: 'platformkit', key: 'platformkit.source', value })
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { width: 120, height: 80,
    cornerRadius: 12, dashPattern: [6, 4], pluginData: [entry(JSON.stringify(source))],
    strokes: [{ type: 'SOLID', visible: true, weight, align: 'INSIDE', opacity: 1,
      color: { r: 1, g: 0, b: 0, a: 1 }, dashPattern: [6, 4] }],
  })
  const instance = graph.createInstance(master.id, master.parentId, { pluginData: [] })
  assert.deepEqual(cssBorderRecord(graph, instance, instance.strokes[0]), { weight, radius: 12 })
  for (const change of [{ dashPattern: [6, 5] }, { dashPattern: [] }, { weight: 3 }, { align: 'CENTER' }, { type: 'GRADIENT_LINEAR' }]) {
    assert.equal(cssBorderRecord(graph, instance, { ...instance.strokes[0], ...change }), undefined)
  }
  for (const change of [{ dashPattern: [9, 5] }, { dashPattern: [] }, { width: NaN }, { height: 0 }, { independentStrokeWeights: true }, { cornerSmoothing: 0.5 },
    { independentCorners: true, topLeftRadius: 4 }, { strokes: [...instance.strokes, ...instance.strokes] }]) {
    const edited = { ...instance, ...change }
    assert.equal(cssBorderRecord(graph, edited, instance.strokes[0]), undefined)
  }
  for (const pluginData of [[], [entry('{')], [entry('null')], [entry('{}')], [entry(JSON.stringify({ ...source, cssBorder: { version: 2 } }))],
    [entry(JSON.stringify(source)), entry(JSON.stringify(source))]]) {
    graph.updateNode(master.id, { pluginData })
    assert.equal(cssBorderRecord(graph, instance, instance.strokes[0]), undefined)
  }
  graph.updateNode(master.id, { componentId: instance.id })
  assert.equal(cssBorderRecord(graph, instance, instance.strokes[0]), undefined, 'cyclic source correspondence cannot activate a correction')
})

test('inherited dash edits synchronize without overwriting an explicit instance pattern', async () => {
  let graph = new SceneGraph()
  const stroke = { color: { r: 1, g: 0, b: 0, a: 1 }, weight: 2, align: 'INSIDE', opacity: 1, visible: true }
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Master', width: 80, height: 40,
    dashPattern: [6, 4], strokes: [{ ...stroke, dashPattern: [6, 4] }],
  })
  graph.createInstance(master.id, master.parentId, { name: 'Inherited' })
  const own = [{ ...stroke, dashPattern: [3, 2] }]
  graph.createInstance(master.id, master.parentId, { name: 'Override', dashPattern: [3, 2], strokes: own,
    overrides: { dashPattern: [3, 2], strokes: own },
  })
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  for (const pattern of [[9, 5], []]) {
    const source = [...graph.getAllNodes()].find(node => node.name === 'Master')
    graph.updateNode(source.id, { dashPattern: pattern, strokes: [{ ...stroke, dashPattern: pattern }] })
    graph.syncInstances(source.id)
    for (let cycle = 0; cycle < 3; cycle++) {
      for (const node of graph.getAllNodes()) if (['Master', 'Inherited', 'Override'].includes(node.name)) {
        const expected = node.name === 'Override' ? [3, 2] : pattern
        assert.deepEqual(node.dashPattern, expected, `${cycle}/${node.name}/FIG`)
        assert.deepEqual(node.strokes[0].dashPattern, expected, `${cycle}/${node.name}/paint`)
      }
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  }
})

test('independent inside borders retain rounded holes and paint translucent corners once through two saves', async () => {
  const ck = await initCanvasKit()
  for (const radii of [[0, 0, 0, 0], [12, 6, 18, 10], [500, 500, 500, 500]]) {
    let graph = new SceneGraph(), baseline
    graph.createNode('RECTANGLE', graph.getPages()[0].id, { name: 'Border', width: 80, height: 60, fills: [],
      independentCorners: true, topLeftRadius: radii[0], topRightRadius: radii[1], bottomRightRadius: radii[2], bottomLeftRadius: radii[3],
      independentStrokeWeights: true, borderTopWeight: 4, borderRightWeight: 8, borderBottomWeight: 12, borderLeftWeight: 16,
      strokes: [{ type: 'SOLID', color: { r: 1, g: 0, b: 0, a: 0.5 }, opacity: 0.5, visible: true, weight: 16, align: 'INSIDE' }],
    })
    for (let cycle = 0; cycle < 3; cycle++) {
      const surface = ck.MakeSurface(80, 60), renderer = new SkiaRenderer(ck, surface)
      try {
        const canvas = surface.getCanvas()
        canvas.clear(ck.TRANSPARENT)
        renderer.renderSceneToCanvas(canvas, graph, graph.getPages()[0].id); surface.flush()
        const pixels = canvas.readPixels(0, 0, { width: 80, height: 60, alphaType: ck.AlphaType.Unpremul,
          colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
        for (const [x, y, painted] of [[2, 30, true], [78, 30, true], [40, 2, true], [40, 55, true],
          [40, 30, false], [0, 0, radii[0] === 0], ...(radii[0] < 20 ? [[7, 3, true], [13, 8, true]] : [])]) {
          assert.deepEqual([...pixels.subarray((y * 80 + x) * 4, (y * 80 + x) * 4 + 4)],
            painted ? [255, 0, 0, 64] : [0, 0, 0, 0], `${radii}/${cycle}/${x},${y}`)
        }
        if (baseline) assert.deepEqual(pixels, baseline, 'all border pixels survive serialization')
        else baseline = pixels
      } finally { renderer.destroy() }
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  }
})
