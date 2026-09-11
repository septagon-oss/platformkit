import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { completePlexFonts, suppliedFonts } from './browser/fixtures.test.mjs'
import { loadFonts, validateFonts } from './fonts.mjs'

test('complete static faces retain binary family identity, licensed bytes and actual underline offsets', async t => {
  const fonts = completePlexFonts(), hash = bytes => createHash('sha256').update(bytes).digest('hex')
  assert.deepEqual(fonts.map(face => face.sha256), [
    'b731cf56514a4bd711ab2f9acf641f9311707f4386772eb306d25c2b29b73b1a',
    'fff45f420f0d026b4a39f99b3bfc47dfc06561c598c8db825cfce5fd706bda7a',
  ])
  assert.deepEqual(validateFonts(fonts).map(face => face.internalFamily), ['IBM Plex Sans', 'IBM Plex Sans SmBld'])
  const notice = readFileSync(new URL('./node_modules/@ibm/plex-sans/LICENSE.txt', import.meta.url))
  for (const face of fonts) assert.equal(face.licenseSHA256, hash(notice))
  const metadata = JSON.parse(readFileSync(new URL('./node_modules/@ibm/plex-sans/package.json', import.meta.url)))
  assert.equal(metadata.version, '1.0.0'); assert.deepEqual(metadata.dependencies ?? {}, {})
  for (const hook of ['preinstall', 'install', 'postinstall']) assert.equal(metadata.scripts?.[hook], undefined)
  for (const face of suppliedFonts([400, 600])) {
    await assert.rejects(loadFonts([face], [{ ...face, text: '↗' }]), /Missing font glyph/)
  }
  await loadFonts(fonts, fonts.map(face => ({ ...face, text: 'HHHH ↗' })))
  const ck = await initCanvasKit(), surface = ck.MakeSurface(300, 140), renderer = new SkiaRenderer(ck, surface)
  t.after(() => renderer.destroy())
  await renderer.loadFonts()
  for (const face of fonts) {
    const graph = new SceneGraph(), page = graph.getPages()[0]
    const fill = (r, g, b) => ({ type: 'SOLID', color: { r, g, b, a: 1 }, visible: true, opacity: 1 })
    const node = graph.createNode('TEXT', page.id, { text: 'HHHH ↗', x: 12, y: 12, width: 260, height: 100,
      fontFamily: face.family, fontWeight: face.weight, fontSize: 24, lineHeight: 40,
      textDecoration: 'UNDERLINE', textDecorationThickness: 2, textDecorationSkipInk: false,
      textDecorationFills: [fill(1, 0, 0)], fills: [fill(0, 0, 0)] })
    function paint(offset) {
      graph.updateNode(node.id, { textUnderlineOffset: offset })
      const before = structuredClone([...graph.getAllNodes()]), canvas = surface.getCanvas()
      canvas.clear(ck.WHITE); renderer.renderSceneToCanvas(canvas, graph, page.id); surface.flush()
      assert.deepEqual([...graph.getAllNodes()], before)
      const pixels = canvas.readPixels(0, 0, { width: 300, height: 140, alphaType: ck.AlphaType.Unpremul,
        colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }), ink = []
      for (let i = 0; i < pixels.length; i += 4) if (pixels[i] > pixels[i + 1]) ink.push([i / 4, pixels[i] - pixels[i + 1]])
      return ink
    }
    const before = paint(8)
    assert.ok(before.length > 100)
    assert.deepEqual(paint(16), before.map(([position, amount]) => [position + 8 * 300, amount]),
      'a preferred family must not silently fall back to an underline that ignores the native offset')
  }
})
