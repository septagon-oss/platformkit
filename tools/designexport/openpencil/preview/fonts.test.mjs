import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { expect } from 'playwright/test'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { SceneGraph } from '@open-pencil/scene-graph'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import { buildComponentDocument } from '../document.mjs'
import { suppliedFonts, sourceFixture } from '../browser/fixtures.test.mjs'
import { loadFonts } from '../fonts.mjs'

const endpoint = new URL(process.env.PLATFORMKIT_OPENPENCIL_URL)
assert.ok(endpoint.protocol === 'http:' && ['localhost', '127.0.0.1', 'openpencil-preview'].includes(endpoint.hostname))
assert.ok(!endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash && endpoint.pathname === '/')
const hash = bytes => createHash('sha256').update(bytes).digest('hex')
const arrayBuffer = bytes => Uint8Array.from(bytes).buffer
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const descendants = (graph, node) => graph.getChildren(node.id).flatMap(child => [child, ...descendants(graph, child)])
const definitionGeometry = (graph, node) => descendants(graph, node).filter(child => child.parentId === node.id || child.type === 'TEXT')
  .map(child => [child.name, child.text, child.x, child.y, child.width, child.height])

test('native underline paint survives preview property editing, history and two worker saves', { timeout: 120000 }, async () => {
  const provenance = await (await fetch(new URL('/platformkit-provenance.json', endpoint))).json()
  for (const name of ['underline-correction.mjs', 'paragraph-correction.mjs', 'corrections.mjs']) {
    assert.equal(provenance.adapter.inputs[name], hash(readFileSync(new URL(`../${name}`, import.meta.url))))
  }
  const fonts = suppliedFonts([400])
  assert.ok(provenance.fontFaces.some(face => face.sha256 === fonts[0].sha256))
  await loadFonts(fonts, [{ family: 'IBM Plex Sans', weight: 400, style: 'normal', text: 'Typography gyjp Remember gyjp' }])
  const ck = await initCanvasKit(), graph = new SceneGraph(), definitions = graph.addPage('Definitions')
  const master = graph.createNode('COMPONENT', definitions.id, { name: 'Underline master', width: 380, height: 140,
    componentPropertyDefinitions: [{ id: '30:1', name: 'Label', type: 'TEXT', defaultValue: 'Typography gyjp' }] })
  const text = graph.createNode('TEXT', master.id, { name: 'Native linked text', text: 'Typography gyjp', x: 20, y: 30,
    width: 330, height: 80, fontFamily: 'IBM Plex Sans', fontWeight: 400, fontSize: 24, lineHeight: 40,
    textDecoration: 'UNDERLINE', textDecorationThickness: 2, textUnderlineOffset: 8, textDecorationSkipInk: false,
    textDecorationFills: [{ type: 'SOLID', color: { r: 1, g: 0, b: 0, a: 1 }, visible: true, opacity: 1 }],
    fills: [{ type: 'SOLID', color: { r: 0, g: 0, b: 0, a: 1 }, visible: true, opacity: 1 }],
    componentPropertyReferences: [{ propertyId: '30:1', field: 'TEXT' }] })
  graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Edited underline' })
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: [
    '--enable-automation', '--font-render-hinting=none', '--use-gl=angle', '--use-angle=swiftshader',
    '--enable-unsafe-swiftshader', '--disable-blink-features=FileSystemAccessLocal',
    ...(endpoint.hostname === 'openpencil-preview' ? [`--unsafely-treat-insecure-origin-as-secure=${endpoint.origin}`] : []),
  ] })
  async function ink(page) {
    const image = ck.MakeImageFromEncoded(await page.locator('[data-test-id="canvas-element"]').screenshot({
      style: '[data-test-id="toolbar"] { visibility: hidden !important; }',
    }))
    try {
      const pixels = image.readPixels(0, 0, { width: image.width(), height: image.height(), colorType: ck.ColorType.RGBA_8888,
        alphaType: ck.AlphaType.Unpremul, colorSpace: ck.ColorSpace.SRGB }), red = []
      for (let i = 0; i < pixels.length; i += 4) if (pixels[i] > pixels[i + 1] + 30 && pixels[i] > pixels[i + 2] + 30) red.push(i, pixels[i], pixels[i + 1], pixels[i + 2])
      assert.ok(red.length > 100, 'the editor must paint the underline, not just store its settings')
      return hash(JSON.stringify(red))
    } finally { image.delete() }
  }
  let buffer = Buffer.from(await exportFigFile(graph)), previous
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const page = await context.newPage(), errors = [], workers = []
        page.on('pageerror', error => errors.push(error.message))
        page.on('worker', worker => workers.push(worker.url()))
        await page.goto(endpoint.href)
        await page.getByRole('menuitem', { name: 'File', exact: true }).click()
        const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click()])
        await chooser.setFiles({ name: 'underline.fig', mimeType: 'application/octet-stream', buffer })
        const selected = page.getByRole('treeitem', { name: 'Edited underline Lock Hide', exact: true })
        await selected.click()
        const label = page.getByRole('textbox', { name: 'Label', exact: true }), value = cycle ? 'Remember gyjp' : 'Typography gyjp'
        await expect(label).toHaveValue(value)
        await expect.poll(() => page.evaluate(() => [...document.fonts].some(face => face.family === 'IBM Plex Sans' && face.weight === '400' && face.status === 'loaded'))).toBe(true)
        const current = await ink(page)
        if (previous) assert.equal(current, previous, `worker save ${cycle} retains the painted underline`)
        if (cycle === 0) {
          await label.fill('Remember gyjp'); await label.press('Tab'); await selected.click()
          const edited = await ink(page)
          assert.notEqual(edited, current)
          await page.keyboard.press('Control+z'); await expect(label).toHaveValue(value)
          assert.equal(await ink(page), current)
          await page.keyboard.press('Control+Shift+z'); await expect(label).toHaveValue('Remember gyjp')
          assert.equal(await ink(page), edited)
        }
        previous = await ink(page)
        assert.deepEqual(errors, [])
        if (cycle === 2) continue
        const [download] = await Promise.all([page.waitForEvent('download'), page.keyboard.press('Control+s')])
        const chunks = []
        for await (const chunk of await download.createReadStream()) chunks.push(chunk)
        buffer = Buffer.concat(chunks)
        assert.ok(workers.some(url => /export-worker/.test(url)))
        const reopened = await parseFigFile(arrayBuffer(buffer), { populate: 'all' }), instance = named(reopened, 'Edited underline')
        assert.equal(instance.type, 'INSTANCE')
        const copied = reopened.getChildren(instance.id)[0], original = reopened.getChildren(named(reopened, 'Underline master').id)[0]
        assert.equal(copied.text, 'Remember gyjp'); assert.equal(original.text, text.text)
        for (const key of ['fontFamily', 'fontWeight', 'textDecoration', 'textDecorationThickness', 'textUnderlineOffset', 'textDecorationSkipInk']) {
          assert.equal(copied[key], text[key]); assert.equal(original[key], text[key])
        }
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

// Inspect actual editor-canvas pixels, not just text stored in the scene graph.
// This fixture has one green button; its interior paint locates it without
// depending on panel widths, screen coordinates or private editor automation.
async function buttonInk(page, ck) {
  // A locator screenshot includes overlapping DOM. Exclude only the floating
  // tool palette: its backdrop compositing differs by one channel under load
  // and is not document paint. Glyph checks still require both visible controls.
  const bytes = await page.locator('[data-test-id="canvas-element"]').screenshot({
    style: '[data-test-id="toolbar"] { visibility: hidden !important; }',
  })
  const image = ck.MakeImageFromEncoded(bytes)
  try {
    const width = image.width(), height = image.height()
    const pixels = image.readPixels(0, 0, { width, height, colorType: ck.ColorType.RGBA_8888,
      alphaType: ck.AlphaType.Unpremul, colorSpace: ck.ColorSpace.SRGB })
    const bounds = predicate => {
      let left = width, right = -1, top = height, bottom = -1, count = 0
      for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
        const offset = 4 * (y * width + x)
        if (!predicate(x, y, pixels.subarray(offset, offset + 4))) continue
        left = Math.min(left, x); right = Math.max(right, x)
        top = Math.min(top, y); bottom = Math.max(bottom, y); count++
      }
      return { left, right, top, bottom, count }
    }
    // Locate its green hue; screenshot color-space conversion is not the
    // subject of this glyph-presence and alignment assertion.
    const fill = bounds((x, y, pixel) => pixel[1] > 70 && pixel[1] < 120 &&
      pixel[1] > pixel[0] * 1.5 && pixel[1] > pixel[2] * 1.1)
    assert.ok(fill.count > 500, `button interior must actually be rendered: ${JSON.stringify(fill)}`)
    const ink = bounds((x, y, pixel) => x > fill.left + 5 && x < fill.right - 5 &&
      y > fill.top + 5 && y < fill.bottom - 5 && pixel[0] > 200 && pixel[1] > 200 && pixel[2] > 200)
    assert.ok(ink.count > 25, 'editable button must draw glyphs, not a blank text node')
    assert.ok(Math.abs((ink.top + ink.bottom) - (fill.top + fill.bottom)) <= 3,
      `vertical glyph alignment: ${JSON.stringify({ fill, ink })}`)
    assert.ok(Math.abs((ink.left + ink.right) - (fill.left + fill.right)) <= 3,
      `horizontal glyph alignment: ${JSON.stringify({ fill, ink })}`)
    // The invalid field has one red outline. Long horizontal runs locate its
    // two borders without confusing the short red validation message below.
    const redRows = new Map()
    const red = pixel => pixel[0] > 120 && pixel[0] > pixel[1] * 1.5 && pixel[0] > pixel[2] * 1.5
    bounds((x, y, pixel) => {
      if (red(pixel)) redRows.set(y, (redRows.get(y) ?? 0) + 1)
      return false
    })
    const outline = bounds((x, y, pixel) => redRows.get(y) > 100 && red(pixel))
    assert.ok(outline.count > 200, 'field outline must actually be rendered')
    const value = bounds((x, y, pixel) => x > outline.left + 5 && x < outline.right - 5 &&
      y > outline.top + 5 && y < outline.bottom - 5 && pixel[0] < 120 && pixel[1] < 120 && pixel[2] < 120)
    assert.ok(value.count > 25, 'editable field value must draw glyphs')
    assert.ok(Math.abs((value.top + value.bottom) - (outline.top + outline.bottom)) <= 3,
      `field vertical alignment: ${JSON.stringify({ outline, value })}`)
    assert.ok(value.left - outline.left >= 10 && value.left - outline.left <= 16,
      'field value keeps source left alignment, not horizontal centering')
    return { hash: hash(pixels), pixels, width, height }
  } finally { image.delete() }
}

test('bundled preview fonts render centered editable text without local access through two worker saves', { timeout: 120000 }, async t => {
  const provenance = await (await fetch(new URL('/platformkit-provenance.json', endpoint))).json()
  const fonts = suppliedFonts([400, 500, 600, 700])
  assert.deepEqual(provenance.fontFaces.map(face => [face.family, face.weight, face.style, face.sha256]),
    fonts.map(face => [face.family, face.weight, face.style, face.sha256]))
  for (const face of provenance.fontFaces) {
    const response = await fetch(new URL(face.path, endpoint))
    assert.equal(response.status, 200)
    assert.equal(hash(new Uint8Array(await response.arrayBuffer())), face.sha256)
    assert.equal(hash(new Uint8Array(await (await fetch(new URL(face.licensePath, endpoint))).arrayBuffer())), face.licenseSHA256)
  }
  const project = await sourceFixture(t, `package main
import ("encoding/json"; "os"; "github.com/septagon-oss/platformkit/design"; "github.com/septagon-oss/platformkit/ui"; "github.com/septagon-oss/platformkit/ui/components")
func main() {
  snapshot, err := ui.Export(design.Default(), components.Gallery())
  if err != nil { panic(err) }; if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const args = ['--enable-automation', '--font-render-hinting=none', '--use-gl=angle',
    '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--disable-blink-features=FileSystemAccessLocal']
  const comparison = await chromium.launch({ headless: true, args })
  // FIG font hashing requires WebCrypto. Like the generic editor check, mark
  // only the validated disposable CI origin secure; grant no local-font access.
  const editorArgs = [...args, ...(endpoint.hostname === 'openpencil-preview'
    ? [`--unsafely-treat-insecure-origin-as-secure=${endpoint.origin}`] : [])]
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: editorArgs })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
  try {
    const examples = ['pk-ui.component.button/primary', 'pk-ui.component.input/invalid', 'pk-ui.component.select/default']
    const { graph, placements, selections } = await buildComponentDocument(project({}), {
      examples, fonts, browser: comparison, renderer, viewport: { width: 320, height: 900 },
    })
    graph.updateNode(selections[0].instance.id, { name: 'Editable button' })
    graph.updateNode(selections[1].instance.id, { name: 'Editable field' })
    const requiredFaces = [...new Map([...graph.getAllNodes()].filter(node => node.type === 'TEXT')
      .map(node => [`${node.fontFamily}/${node.fontWeight}`, { family: node.fontFamily, weight: String(node.fontWeight) }])).values()]
    let buffer = Buffer.from(await exportFigFile(graph)), previousPixels
    const baseline = await parseFigFile(arrayBuffer(buffer), { populate: 'all' })
    const definitions = [...baseline.getAllNodes()].filter(node => node.type === 'COMPONENT')
      .map(node => [node.name, definitionGeometry(baseline, node)])
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const page = await context.newPage(), errors = [], workers = [], fontRequests = []
        // Slow one referenced face independently: a different loaded font is
        // not evidence that the document is ready to paint after import.
        if (cycle === 1) await page.route(new URL(provenance.fontFaces.find(face => face.weight === 500).path, endpoint).href, async route => {
          await new Promise(resolve => setTimeout(resolve, 3000))
          await route.continue()
        })
        page.on('pageerror', error => errors.push(error.message))
        page.on('worker', worker => workers.push(worker.url()))
        page.on('response', response => { if (provenance.fontFaces.some(face => response.url() === new URL(face.path, endpoint).href)) fontRequests.push(response) })
        await page.goto(endpoint.href)
        assert.equal(await page.evaluate(() => window.isSecureContext), true, 'FIG export requires a secure browser context')
        assert.notEqual(await page.evaluate(async () => {
          try { return (await navigator.permissions.query({ name: 'local-fonts' })).state } catch { return 'unavailable' }
        }), 'granted')
        await page.getByRole('menuitem', { name: 'File', exact: true }).click()
        const [chooser] = await Promise.all([page.waitForEvent('filechooser'),
          page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click()])
        await chooser.setFiles({ name: 'font-preview.fig', mimeType: 'application/octet-stream', buffer })
        await page.getByRole('button', { name: 'Editable source instances', exact: true }).click()
        await page.getByRole('treeitem', { name: `${placements.name} Lock Hide`, exact: true }).click()
        await page.keyboard.press('ArrowRight')
        await page.getByRole('treeitem', { name: 'Editable button Lock Hide', exact: true }).click()
        const label = page.getByRole('textbox', { name: 'label', exact: true })
        await expect(label).toHaveValue(['Save', 'Create album', 'Save'][cycle])
        await expect.poll(async () => page.evaluate(required => required.every(wanted => [...document.fonts]
          .some(face => face.family === wanted.family && face.weight === wanted.weight && face.status === 'loaded')), requiredFaces)).toBe(true)
        const pixels = await buttonInk(page, ck)
        if (previousPixels && pixels.hash !== previousPixels.hash) {
          const diff = { left: pixels.width, top: pixels.height, right: 0, bottom: 0, count: 0, samples: [] }
          for (let y = 0; y < pixels.height; y++) for (let x = 0; x < pixels.width; x++) {
            const i = 4 * (y * pixels.width + x)
            if (pixels.pixels.subarray(i, i + 4).every((v, c) => v === previousPixels.pixels[i + c])) continue
            diff.count++; diff.left = Math.min(diff.left, x); diff.top = Math.min(diff.top, y)
            diff.right = Math.max(diff.right, x); diff.bottom = Math.max(diff.bottom, y)
            if (diff.samples.length < 8) diff.samples.push([x, y, [...previousPixels.pixels.subarray(i, i + 4)], [...pixels.pixels.subarray(i, i + 4)]])
          }
          assert.fail(`save/reopen pixel difference: ${JSON.stringify(diff)}`)
        }
        await page.getByRole('treeitem', { name: 'Editable field Lock Hide', exact: true }).click()
        const value = page.getByRole('textbox', { name: 'value', exact: true })
        await expect(value).toHaveValue(['hello', 'album-title', 'next-title'][cycle])
        if (cycle === 2) continue
        await value.fill(['album-title', 'next-title'][cycle]); await value.press('Tab')
        await page.getByRole('treeitem', { name: 'Editable button Lock Hide', exact: true }).click()
        await label.fill(['Create album', 'Save'][cycle]); await label.press('Tab')
        await page.getByRole('treeitem', { name: 'Editable button Lock Hide', exact: true }).click()
        previousPixels = await buttonInk(page, ck)
        const [download] = await Promise.all([page.waitForEvent('download'), page.keyboard.press('Control+s')])
        const chunks = []
        for await (const chunk of await download.createReadStream()) chunks.push(chunk)
        buffer = Buffer.concat(chunks)
        assert.ok(workers.some(url => /export-worker/.test(url)))
        assert.ok(fontRequests.length > 0)
        for (const response of fontRequests) assert.equal(response.status(), 200)
        assert.deepEqual(errors, [])
        const reopened = await parseFigFile(arrayBuffer(buffer), { populate: 'all' })
        for (const [name, children] of definitions) assert.deepEqual(definitionGeometry(reopened, named(reopened, name)), children)
        for (const metadata of parseFigBuffer(arrayBuffer(buffer)).nodeChanges.flatMap(node => node.derivedTextData?.fontMetaData ?? [])) {
          const face = fonts.find(face => face.family === metadata.key.family && face.weight === metadata.fontWeight)
          assert.ok(face, 'saved text must use one of the exact supplied faces')
          assert.equal(Buffer.from(metadata.fontDigest).toString('hex'), createHash('sha1').update(face.bytes).digest('hex'))
        }
      } finally { await context.close() }
    }
  } finally { renderer.destroy(); await browser.close(); await comparison.close() }
})
