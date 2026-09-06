import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { test } from 'node:test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { fontManager, missingGlyphCharacters } from '@open-pencil/core/text'
import { loadFonts, validateFonts } from './fonts.mjs'

const require = createRequire(import.meta.url)
const hash = bytes => createHash('sha256').update(bytes).digest('hex')
const family = 'IBM Plex Sans'
const sample = 'João Ação Été Save 123 €'
const faces = [400, 600].map(weight => {
  const bytes = readFileSync(require.resolve(
    `@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`))
  return { family, weight, style: 'normal', bytes, sha256: hash(bytes) }
})
const requirements = faces.map(({ family, weight, style }) => ({ family, weight, style, text: sample }))

test('font validation keeps exact supplied bytes and face identities without loading native fonts', () => {
  assert.deepEqual(faces.map(face => face.sha256), [
    '828907bfd14855c880789878bd2b38ffd284a6c27c8b80f6069900f70dae3901',
    '7861a349af1e925a80d56547c2c9e0b1e9f6a9002a9a6867351da2f05122ad21',
  ])
  const before = faces.map(face => ({ ...face, bytes: Buffer.from(face.bytes) }))
  const validated = validateFonts(faces)
  assert.deepEqual(validated.map(face => face.postscriptName), ['IBMPlexSans-Regular', 'IBMPlexSans-SemiBold'])
  assert.deepEqual(validated.map(face => face.internalFamily), [family, `${family} SemiBold`])
  assert.equal(fontManager.loadedData(family, 'Regular'), null)
  for (const [index, face] of validated.entries()) {
    assert.notEqual(face.bytes, faces[index].bytes)
    assert.equal(hash(face.bytes), faces[index].sha256)
    face.bytes[0] = 0
  }
  assert.deepEqual(faces, before)
  const inter = readFileSync(new URL('./node_modules/@open-pencil/core/assets/Inter-Regular.ttf', import.meta.url))
  assert.equal(validateFonts([{ family: 'Inter', weight: 400, style: 'normal', bytes: inter, sha256: hash(inter) }])[0].internalFamily, 'Inter')
  const italic = readFileSync(require.resolve('@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-400-italic.woff'))
  assert.equal(validateFonts([{ family, weight: 400, style: 'italic', bytes: italic, sha256: hash(italic) }])[0].postscriptName, 'IBMPlexSans-Italic')
})

test('font validation rejects wrong identities, hashes, duplicate faces and unsupported formats', () => {
  const regular = faces[0]
  for (const patch of [
    { family: 'Arial' }, { family: `${family}, Arial` }, { family: '' },
    { weight: 600 }, { weight: 450 }, { weight: '400' }, { style: 'italic' }, { style: 'oblique' },
    { sha256: '0'.repeat(64) }, { sha256: '' }, { bytes: [] },
  ]) assert.throws(() => validateFonts([{ ...regular, ...patch }]))
  assert.throws(() => validateFonts([regular, regular]), /duplicate/i)
  assert.throws(() => validateFonts(null), /array/i)
  const malformed = Buffer.from('not a font')
  assert.throws(() => validateFonts([{ ...regular, bytes: malformed, sha256: hash(malformed) }]))
  const woff2 = readFileSync(require.resolve('@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-400-normal.woff2'))
  assert.throws(() => validateFonts([{ ...regular, bytes: woff2, sha256: hash(woff2) }]), /WOFF2|format/i)
})

test('requirements fail before registration for missing exact faces or glyph coverage', async () => {
  for (const patch of [{ weight: 500 }, { style: 'italic' }, { family: 'Arial' }, { text: '中文' }, { text: null }]) {
    await assert.rejects(loadFonts(faces, [{ ...requirements[0], ...patch }]))
    assert.equal(fontManager.loadedData(family, 'Regular'), null)
    assert.equal(fontManager.loadedData(family, 'SemiBold'), null)
  }
  await assert.rejects(loadFonts(faces, null), /array/i)
})

test('supplied Plex faces shape and retain real outlines and pixels through two FIG saves', async () => {
  const loaded = await loadFonts(faces, requirements)
  const registered = fontManager.loadedData(family, 'Regular')
  loaded[0].bytes[0] = 0
  assert.equal(hash(new Uint8Array(registered)), faces[0].sha256, 'SDK does not alias returned bytes')
  await loadFonts(faces, requirements)
  assert.equal(fontManager.loadedData(family, 'Regular'), registered, 'idempotent registration')
  const ck = await initCanvasKit()
  const surface = ck.MakeSurface(600, 150)
  assert.ok(surface)
  const renderer = new SkiaRenderer(ck, surface)
  let graph = new SceneGraph()
  for (const face of faces) graph.createNode('TEXT', graph.getPages()[0].id, {
    name: String(face.weight), text: sample, x: 0, y: face.weight === 400 ? 0 : 60,
    width: 600, height: 50, fontFamily: family, fontWeight: face.weight, fontSize: 24,
    lineHeight: 32, textAutoResize: 'NONE',
    fills: [{ type: 'SOLID', color: { r: 0, g: 0, b: 0, a: 1 }, opacity: 1, visible: true }],
  })
  let baseline, fig
  try {
    await renderer.loadFonts()
    for (let cycle = 0; cycle < 3; cycle++) {
      const page = graph.getPages()[0]
      const texts = graph.getChildren(page.id)
      const restore = await renderer.prepareForExport(graph, page.id, texts.map(node => node.id))
      try {
        for (const [index, node] of texts.entries()) {
          assert.equal(node.fontFamily, family)
          assert.equal(node.fontWeight, faces[index].weight)
          const paragraph = renderer.buildParagraph(node)
          try {
            assert.equal(paragraph.getLongestLine(), [279.023193359375, 285.7431945800781][index])
            assert.equal(paragraph.getHeight(), 32)
            const lines = paragraph.getShapedLines()
            assert.deepEqual(missingGlyphCharacters(sample, lines), [])
            const actualFamilies = lines.flatMap(line => line.runs.map(run => run.typeface.getFamilyName()))
            assert.deepEqual(actualFamilies, [index === 0 ? family : `${family} SemiBold`])
          } finally { paragraph.delete() }
          if (cycle > 0) {
            assert.equal(node.figmaDerivedTextGlyphs.length, [...sample].length)
            assert.ok(node.figmaDerivedTextGlyphs.some(glyph => glyph.commandsBlob.length > 8))
          }
        }
        surface.getCanvas().clear(ck.WHITE)
        renderer.invalidateAllPictures()
        renderer.renderSceneToCanvas(surface.getCanvas(), graph, page.id)
        surface.flush()
        const pixels = Buffer.from(surface.getCanvas().readPixels(0, 0, {
          width: 600, height: 150, alphaType: ck.AlphaType.Unpremul,
          colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB,
        }))
        if (cycle === 0) baseline = pixels
        else assert.deepEqual(pixels, baseline, `rendered cycle ${cycle}`)
      } finally { restore() }
      if (cycle < 2) {
        fig = await exportFigFile(graph)
        graph = await parseFigFile(fig.slice().buffer, { populate: 'all' })
      }
    }
  } finally { renderer.destroy() }
  // FIG carries editable font references and derived outlines, not font files.
  const child = spawnSync(process.execPath, ['--import', './register.mjs', '--input-type=module', '-e', `
    import { readFileSync } from 'node:fs';
    import { parseFigFile } from '@open-pencil/core/io/formats/fig';
    import { fontManager, weightToStyle } from '@open-pencil/core/text';
    const bytes = readFileSync(0);
    const graph = await parseFigFile(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.length), { populate: 'all' });
    console.log(JSON.stringify([...graph.getAllNodes()].filter(node => node.type === 'TEXT').map(node => ({
      family: node.fontFamily, weight: node.fontWeight, glyphs: node.figmaDerivedTextGlyphs.length,
      loaded: fontManager.isStyleLoaded(node.fontFamily, weightToStyle(node.fontWeight)),
    }))));
  `], { cwd: new URL('.', import.meta.url), input: fig, encoding: 'utf8' })
  assert.equal(child.status, 0, child.stderr)
  assert.deepEqual(JSON.parse(child.stdout), faces.map(face => ({
    family, weight: face.weight, glyphs: [...sample].length, loaded: false,
  })))
})

test('one native process cannot replace a loaded face with different bytes', async () => {
  const bytes = Buffer.from(faces[0].bytes)
  bytes[23] ^= 1 // WOFF header minor version: still a valid face, different bytes.
  const alternate = { ...faces[0], bytes, sha256: hash(bytes) }
  assert.equal(validateFonts([alternate]).length, 1, 'otherwise valid font with a different digest')
  const before = fontManager.loadedData(family, 'Regular')
  await assert.rejects(loadFonts([alternate], [requirements[0]]), /already loaded|different bytes/i)
  assert.equal(fontManager.loadedData(family, 'Regular'), before)
})
