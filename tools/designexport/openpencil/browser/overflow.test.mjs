import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SceneGraph } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { chain } from '../exporter-correction.mjs'

test('CSS padding-edge overflow matches Chromium pixels, native radius edits and two saves', async () => {
  const browser = await chromium.launch({ headless: true }), ck = await initCanvasKit()
  const failures = []
  try {
    // Inner radii are independent expected values, not the adapter's output.
    for (const [borders, corners, inner] of [
      [[2, 2, 2, 2], [12, 12, 12, 12], [[10, 10], [10, 10], [10, 10], [10, 10]]],
      [[2, 5, 4, 3], [12, 24, 32, 4], [[9, 10], [19, 22], [27, 28], [0, 0]]],
      [[2, 5, 4, 3], [0, 0, 0, 0], [[0, 0], [0, 0], [0, 0], [0, 0]]],
    ]) {
      const [top, right, bottom, left] = borders
      let graph = new SceneGraph()
      const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Source clip', width: 120, height: 80,
        clipsContent: true, fills: [], independentCorners: true,
        topLeftRadius: corners[0], topRightRadius: corners[1], bottomRightRadius: corners[2], bottomLeftRadius: corners[3],
        pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
          schema: 'platformkit.design-export.v1', cssBox: { version: 1, overflow: borders },
        }) }],
      })
      graph.createNode('RECTANGLE', master.id, { width: 500, height: 500,
        fills: [{ type: 'SOLID', color: { r: 1, g: 0, b: 0, a: 1 }, opacity: 1, visible: true }] })
      const examples = graph.addPage('Examples')
      graph.createInstance(master.id, examples.id, { name: 'Placed', pluginData: [] })
      for (let cycle = 0; cycle < 3; cycle++) {
        const width = 120 + cycle * 19, height = 80 + cycle * 7
        const node = [...graph.getAllNodes()].find(node => node.name === 'Placed')
        graph.updateNode(node.id, { width, height })
        const page = await browser.newPage({ viewport: { width, height } })
        const surface = ck.MakeSurface(width, height), renderer = new SkiaRenderer(ck, surface)
        const canvas = surface.getCanvas(), paint = new ck.Paint()
        let image
        try {
          await page.setContent(`<style>body{margin:0;background:transparent}div{box-sizing:border-box;width:${width}px;height:${height}px;border:solid transparent;border-width:${borders.map(n => `${n}px`).join(' ')};border-radius:${corners.map(n => `${n}px`).join(' ')};overflow:hidden}i{display:block;width:500px;height:500px;background:red}</style><div><i></i></div>`)
          image = ck.MakeImageFromEncoded(await page.screenshot({ omitBackground: true }))
          const format = { width, height, alphaType: ck.AlphaType.Unpremul, colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }
          const expected = image.readPixels(0, 0, format)
          // Independently calibrate rasterizer precision for this exact inner
          // shape. No adapter or CSS clipping participates in these two draws.
          const browserAlpha = await page.evaluate(({ width, height, left, top, right, bottom, inner }) => {
            const element = document.createElement('canvas'); element.width = width; element.height = height
            const context = element.getContext('2d')
            context.beginPath(); context.roundRect(left, top, width - left - right, height - top - bottom,
              inner.map(([x, y]) => ({ x, y }))); context.clip(); context.fillRect(0, 0, width, height)
            return [...context.getImageData(0, 0, width, height).data.filter((_, i) => i % 4 === 3)]
          }, { width, height, left, top, right, bottom, inner })
          canvas.save(); canvas.clear(ck.TRANSPARENT)
          canvas.clipRRect(new Float32Array([left, top, width - right, height - bottom, ...inner.flat()]), ck.ClipOp.Intersect, true)
          paint.setColor(ck.BLACK); canvas.drawRect(ck.LTRBRect(0, 0, width, height), paint); surface.flush()
          const baseline = canvas.readPixels(0, 0, format); canvas.restore()
          const differences = actual => {
            const mismatches = []
            for (let i = 3; i < expected.length; i += 4) {
              if (actual[i]) assert.deepEqual([...actual.slice(i - 3, i)], [255, 0, 0])
              const tolerance = 1 + Math.abs(browserAlpha[i / 4 | 0] - baseline[i])
              if (Math.abs(actual[i] - expected[i]) > tolerance) mismatches.push([i / 4 | 0, actual[i], expected[i]])
            }
            return mismatches
          }
          const draw = () => {
            canvas.clear(ck.TRANSPARENT); renderer.renderSceneToCanvas(canvas, graph, node.parentId); surface.flush()
            return canvas.readPixels(0, 0, format)
          }
          const actual = draw(), mismatches = differences(actual)
          if (mismatches.length) failures.push({ borders, corners, cycle, pixels: mismatches.slice(0, 8), count: mismatches.length })
          if (cycle === 0 && corners[0] === 12) {
            const canonical = chain(graph, node, 'componentId').at(-1), metadata = canonical.pluginData
            graph.updateNode(canonical.id, { pluginData: [] })
            assert.ok(differences(draw()).length > 100, 'ordinary border-box clipping cannot pass')
            graph.updateNode(canonical.id, { pluginData: metadata })
            graph.updateNode(node.id, { clipsContent: false })
            assert.ok(differences(draw()).length > 100, 'disabling the native clip releases clipping')
            graph.updateNode(node.id, { clipsContent: true, topLeftRadius: 0 })
            assert.ok(differences(draw()).length > 10, 'native radius edits affect the inherited padding clip')
            graph.updateNode(node.id, { topLeftRadius: corners[0] })
            const shifted = new Uint8Array(actual.length)
            for (let y = 0; y < height; y++) shifted.set(actual.subarray(y * width * 4, ((y + 1) * width - 1) * 4), (y * width + 1) * 4)
            assert.ok(differences(shifted).length > 10, 'a one-pixel displacement is not rasterizer precision')
          }
        } finally { image?.delete(); paint.delete(); renderer.destroy(); await page.close() }
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
    }
    assert.deepEqual(failures, [])
  } finally { await browser.close() }
})
