import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'

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
