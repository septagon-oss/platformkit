import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { expect } from 'playwright/test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { completePlexFonts } from '../browser/fixtures.test.mjs'
import { loadFonts } from '../fonts.mjs'

test('complete-font editor preserves the arrow, preferred-family underline, editing and two worker saves', { timeout: 120000 }, async t => {
  const endpoint = new URL(process.env.PLATFORMKIT_OPENPENCIL_URL), hash = bytes => createHash('sha256').update(bytes).digest('hex')
  assert.ok(endpoint.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(endpoint.hostname))
  assert.ok(!endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash && endpoint.pathname === '/')
  const fonts = completePlexFonts(), provenance = await (await fetch(new URL('/platformkit-provenance.json', endpoint))).json()
  for (const name of ['font-correction.mjs', 'fonts.mjs', 'underline-correction.mjs', 'package-lock.json']) {
    assert.equal(provenance.adapter.inputs[name], hash(readFileSync(new URL(`../${name}`, import.meta.url))))
  }
  for (const face of fonts) {
    assert.ok(provenance.fontFaces.some(item => item.sha256 === face.sha256 && item.licenseSHA256 === face.licenseSHA256))
    for (const [path, digest] of [[face.path, face.sha256], [face.licensePath, face.licenseSHA256]]) {
      assert.equal(hash(new Uint8Array(await (await fetch(new URL(path, endpoint))).arrayBuffer())), digest)
    }
  }
  await loadFonts(fonts, fonts.map(face => ({ ...face, text: 'Sign in ↗ Recover draft ↗' })))
  const ck = await initCanvasKit(), graph = new SceneGraph(), definitions = graph.addPage('Definitions')
  const master = graph.createNode('COMPONENT', definitions.id, { name: 'Full-font master', width: 360, height: 120,
    componentPropertyDefinitions: [{ id: '30:1', name: 'Label', type: 'TEXT', defaultValue: 'Sign in ↗' }] })
  const text = graph.createNode('TEXT', master.id, { text: 'Sign in ↗', x: 20, y: 20, width: 320, height: 80,
    fontFamily: 'IBM Plex Sans', fontWeight: 600, fontSize: 24, lineHeight: 40, textDecoration: 'UNDERLINE',
    textDecorationThickness: 2, textUnderlineOffset: 8, textDecorationSkipInk: false,
    fills: [{ type: 'SOLID', color: { r: 0, g: 0, b: 0, a: 1 }, visible: true, opacity: 1 }],
    textDecorationFills: [{ type: 'SOLID', color: { r: 1, g: 0, b: 0, a: 1 }, visible: true, opacity: 1 }],
    componentPropertyReferences: [{ propertyId: '30:1', field: 'TEXT' }] })
  graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Recovery label' })
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: ['--enable-automation',
    '--font-render-hinting=none', '--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--disable-blink-features=FileSystemAccessLocal'] })
  t.after(() => browser.close())
  async function ink(page) {
    const image = ck.MakeImageFromEncoded(await page.locator('[data-test-id="canvas-element"]').screenshot({
      style: '[data-test-id="toolbar"] { visibility: hidden !important; }',
    }))
    try {
      const pixels = image.readPixels(0, 0, { width: image.width(), height: image.height(), colorType: ck.ColorType.RGBA_8888,
        alphaType: ck.AlphaType.Unpremul, colorSpace: ck.ColorSpace.SRGB }), red = []
      for (let i = 0; i < pixels.length; i += 4) if (pixels[i] > pixels[i + 1] + 30 && pixels[i] > pixels[i + 2] + 30) red.push(i, pixels[i], pixels[i + 1], pixels[i + 2])
      assert.ok(red.length > 100)
      return hash(JSON.stringify(red))
    } finally { image.delete() }
  }
  let buffer = Buffer.from(await exportFigFile(graph)), previous
  for (let cycle = 0; cycle < 3; cycle++) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
    try {
      const page = await context.newPage(), errors = [], workers = []
      page.on('pageerror', error => errors.push(error.message)); page.on('worker', worker => workers.push(worker.url()))
      await page.goto(endpoint.href); await page.getByRole('menuitem', { name: 'File', exact: true }).click()
      const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click()])
      await chooser.setFiles({ name: 'full-fonts.fig', mimeType: 'application/octet-stream', buffer })
      const selected = page.getByRole('treeitem', { name: 'Recovery label Lock Hide', exact: true }), label = page.getByRole('textbox', { name: 'Label', exact: true })
      await selected.click(); await expect(label).toHaveValue(cycle ? 'Recover draft ↗' : 'Sign in ↗')
      await expect.poll(() => page.evaluate(() => [...document.fonts].some(face => face.family === 'IBM Plex Sans' && face.weight === '600' && face.status === 'loaded'))).toBe(true)
      // Canvas painting settles after the DOM property/font state changes.
      if (previous) await expect.poll(() => ink(page)).toBe(previous)
      const before = await ink(page)
      if (!cycle) {
        await label.fill('Recover draft ↗'); await label.press('Tab'); await selected.click()
        await expect.poll(() => ink(page)).not.toBe(before)
        const edited = await ink(page); assert.notEqual(edited, before)
        await page.keyboard.press('Control+z'); await expect(label).toHaveValue('Sign in ↗'); await expect.poll(() => ink(page)).toBe(before)
        await page.keyboard.press('Control+Shift+z'); await expect(label).toHaveValue('Recover draft ↗'); await expect.poll(() => ink(page)).toBe(edited)
      }
      previous = await ink(page); assert.deepEqual(errors, [])
      if (cycle === 2) continue
      const [download] = await Promise.all([page.waitForEvent('download'), page.keyboard.press('Control+s')]), chunks = []
      for await (const chunk of await download.createReadStream()) chunks.push(chunk)
      buffer = Buffer.concat(chunks); assert.ok(workers.some(url => /export-worker/.test(url)))
      const reopened = await parseFigFile(Uint8Array.from(buffer).buffer, { populate: 'all' })
      const instance = [...reopened.getAllNodes()].find(node => node.name === 'Recovery label'), copied = reopened.getChildren(instance.id)[0]
      assert.equal(instance.type, 'INSTANCE'); assert.equal(copied.text, 'Recover draft ↗')
      assert.equal(reopened.getChildren(instance.componentId)[0].text, text.text)
      for (const key of ['fontFamily', 'fontWeight', 'textDecoration', 'textDecorationThickness', 'textUnderlineOffset']) assert.equal(copied[key], text[key])
    } finally { await context.close() }
  }
})
