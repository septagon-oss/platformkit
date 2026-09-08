import assert from 'node:assert/strict'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'

test('explicit shadow cutout respects translucent geometry without changing show-behind behavior', async () => {
  const ck = await initCanvasKit()
  const hidden = [[0, 0, 0, 0], [255, 255, 255, 128], [255, 255, 255, 255]]
  const behind = [[0, 0, 0, 128], [170, 170, 170, 191], [255, 255, 255, 255]]
  for (const type of ['RECTANGLE', 'FRAME']) for (const showShadowBehindNode of [false, true, undefined]) {
    for (const [index, alpha] of [0, 0.5, 1].entries()) {
      const graph = new SceneGraph(), page = graph.getPages()[0]
      graph.createNode(type, page.id, {
        width: 40, height: 40,
        fills: [{ type: 'SOLID', color: { r: 1, g: 1, b: 1, a: alpha }, opacity: 1, visible: true }],
        effects: [{ type: 'DROP_SHADOW', color: { r: 0, g: 0, b: 0, a: 0.5 },
          offset: { x: 5, y: 5 }, radius: 0, spread: 0, visible: true,
          ...(showShadowBehindNode === undefined ? {} : { showShadowBehindNode }) }],
      })
      const surface = ck.MakeSurface(50, 50), renderer = new SkiaRenderer(ck, surface)
      try {
        const canvas = surface.getCanvas()
        canvas.clear(ck.TRANSPARENT)
        renderer.renderSceneToCanvas(canvas, graph, page.id)
        surface.flush()
        for (const [x, expected] of [[20, (showShadowBehindNode === false ? hidden : behind)[index]], [42, [0, 0, 0, 128]]]) {
          const pixel = canvas.readPixels(x, 20, { width: 1, height: 1, alphaType: ck.AlphaType.Unpremul,
            colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
          expected.forEach((value, channel) => assert.ok(Math.abs(pixel[channel] - value) <= 1,
            `${type}/behind=${showShadowBehindNode}/alpha=${alpha}/x=${x}: ${pixel}`))
        }
      } finally { renderer.destroy() }
    }
  }
})
