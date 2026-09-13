import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { expect } from 'playwright/test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { sourceAbsoluteRecord } from '../source-positioning.mjs'

const endpoint = new URL(process.env.PLATFORMKIT_OPENPENCIL_URL)
assert.ok(endpoint.protocol === 'http:' && ['127.0.0.1', 'localhost', 'openpencil'].includes(endpoint.hostname))
assert.ok(!endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash && endpoint.pathname === '/')
const hash = bytes => createHash('sha256').update(bytes).digest('hex')
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const parse = bytes => parseFigFile(Uint8Array.from(bytes).buffer, { populate: 'all' })
const source = record => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
  schema: 'platformkit.design-export.v1', scope: 'source-composition-layout', ...record,
}) }]
const shape = (graph, node) => ({ name: node.name, type: node.type, x: node.x, y: node.y,
  width: node.width, height: node.height, fills: node.fills, component: graph.getNode(node.componentId)?.name,
  source: node.pluginData.filter(item => item.pluginId !== 'open-pencil'),
  children: graph.getChildren(node.id).map(child => shape(graph, child)),
})

async function fixture(mixed = false) {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', page.id, { name: 'Position master', width: 320, height: 160,
    layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', pluginData: source({ owner: 'master' }) })
  for (const edge of ['left', 'right']) graph.createNode('FRAME', master.id, {
    name: `${edge} placement`, width: 40.1, height: 24, layoutPositioning: 'ABSOLUTE',
    horizontalConstraint: edge === 'left' ? 'MIN' : 'MAX', verticalConstraint: 'MIN',
    fills: [{ type: 'SOLID', color: { r: .2, g: .4, b: .8, a: 1 }, visible: true, opacity: 1 }],
    pluginData: source({ owner: edge, cssPosition: { version: 1,
      horizontal: { edge, inset: edge === 'left' ? .1 : 1 }, vertical: { edge: 'top', inset: mixed && edge === 'right' ? .3 : -.2 } } }),
  })
  computeLayout(graph, master.id)
  graph.createInstance(master.id, page.id, { name: 'Edited position', x: 20, y: 220 })
  graph.createInstance(master.id, page.id, { name: 'Untouched position', x: 500, y: 220 })
  const buffer = Buffer.from(await exportFigFile(graph)), baseline = await parse(buffer)
  const protectedNodes = ['Position master', 'Untouched position'].map(name => [name, shape(baseline, named(baseline, name))])
  const right = shape(baseline, baseline.getChildren(named(baseline, 'Edited position').id)[1])
  return { buffer, verify: async (bytes, x, y) => {
    const saved = await parse(bytes), edited = named(saved, 'Edited position'), children = saved.getChildren(edited.id)
    assert.equal(saved.getNode(edited.componentId)?.name, 'Position master')
    assert.deepEqual(sourceAbsoluteRecord(children[0]), { version: 1,
      horizontal: { edge: 'left', inset: x }, vertical: { edge: 'top', inset: y } })
    assert.deepEqual([children[0].x, children[0].y, children[0].width], [Math.fround(x), Math.fround(y), Math.fround(40.1)])
    assert.deepEqual(shape(saved, children[1]), right)
    for (const [name, expected] of protectedNodes) assert.deepEqual(shape(saved, named(saved, name)), expected, name)
  } }
}

async function save(page, workers) {
  const [download] = await Promise.all([page.waitForEvent('download'), page.keyboard.press('Control+s')])
  const chunks = []
  for await (const chunk of await download.createReadStream()) chunks.push(chunk)
  assert.ok(workers.some(path => /\/export-worker-.*\.js$/.test(path)))
  return Buffer.concat(chunks)
}

// Rendering follows requestRender asynchronously. Keep the exact pixel oracle,
// but take a baseline only after two successive rendered captures agree.
async function canvasPixels(page, expected, message) {
  // Element screenshots include the floating HTML toolbar above the canvas.
  // Reveal the complete scene beneath it; its SVG compositing is a separate oracle.
  await expect(page.locator('[data-test-id="toolbar"]')).toHaveCount(1)
  let previous
  await expect.poll(async () => {
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    const actual = hash(await page.locator('canvas').first().screenshot({
      timeout: 5000, style: '[data-test-id="toolbar"] { visibility: hidden !important; }',
    }))
    const stable = actual === previous
    previous = actual
    return stable && (expected === undefined || actual === expected)
  }, { message, timeout: 5000, intervals: [16, 50, 100] }).toBe(true)
  return previous
}

for (const input of ['pointer', 'keyboard']) test(`fractional coordinate ${input} history preserves exact source through worker saves`, { timeout: 180000 }, async () => {
  const provenance = await (await fetch(new URL('platformkit-provenance.json', endpoint))).json()
  assert.equal(provenance.scope, 'generic-editor-without-packaged-design')
  assert.ok(provenance.adapter.inputs['source-position-history.mjs'], 'the shared native history owner is bundled')
  for (const [name, digest] of Object.entries(provenance.adapter.inputs)) {
    assert.match(name, /^[A-Za-z0-9._-]+$/)
    const path = ['LICENSE', 'NOTICE'].includes(name) ? `../../../../${name}` : `../${name}`
    assert.equal(hash(readFileSync(new URL(path, import.meta.url))), digest, name)
  }
  const setup = await fixture(), browser = await chromium.launch({ headless: true, channel: 'chromium', args: [
    '--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--disable-blink-features=FileSystemAccessLocal',
    ...(endpoint.hostname === 'openpencil' ? [`--unsafely-treat-insecure-origin-as-secure=${endpoint.origin}`] : []),
  ] })
  let buffer = setup.buffer, x = .1, y = -.2
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const page = await context.newPage(), errors = [], workers = []
        page.on('pageerror', error => errors.push(error.message))
        page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
        page.on('worker', worker => workers.push(new URL(worker.url()).pathname))
        await page.goto(endpoint.href)
        await page.getByRole('button', { name: 'Page 1', exact: true }).waitFor()
        await page.getByRole('menuitem', { name: 'File', exact: true }).click()
        const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click()])
        await chooser.setFiles({ name: 'coordinate-history.fig', mimeType: 'application/octet-stream', buffer })
        const root = page.getByRole('treeitem', { name: 'Edited position Lock Hide', exact: true })
        await root.click(); await root.press('ArrowRight')
        const layer = page.getByRole('treeitem', { name: 'left placement Lock Hide', exact: true })
        await layer.click()
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        if (cycle === 2) { await setup.verify(await save(page, workers), x, y); assert.deepEqual(errors, []); continue }
        const field = page.getByRole('spinbutton', { name: cycle ? 'Y Axis' : 'X Axis', exact: true })
        const enter = async () => {
          if (input === 'pointer') await field.dblclick()
          else {
            for (let step = 0; step < 120 && !await field.evaluate(node => node === document.activeElement); step++) await page.keyboard.press('Tab')
          }
          await expect(field).toHaveJSProperty('tagName', 'INPUT')
          await expect(field).toHaveAttribute('data-editing', '')
          await expect(field).toBeEditable()
          await expect(field).toBeFocused()
          if (input === 'keyboard') {
            assert.equal(await field.evaluate(node => node.matches(':focus-visible')), true)
            const outline = await field.evaluate(node => { const style = getComputedStyle(node); return [style.outlineStyle, parseFloat(style.outlineWidth)] })
            assert.ok(!['none', 'hidden'].includes(outline[0]) && outline[1] > 0, 'the focused coordinate has a visible native outline')
          }
        }
        await enter(); await page.keyboard.press('Enter'); await layer.click()
        await setup.verify(await save(page, workers), x, y)
        const label = `${input} cycle ${cycle} ${cycle ? 'Y' : 'X'}`
        const pixels = await canvasPixels(page, undefined, `${label}: stable baseline`)
        for (const end of ['Escape', 'invalid']) {
          await enter(); await page.keyboard.press('Control+a'); await page.keyboard.type('55.123456789')
          if (end === 'invalid') await page.keyboard.type('x')
          await expect(field).toHaveValue(end === 'invalid' ? '55.123456789x' : '55.123456789')
          await page.keyboard.press(end === 'invalid' ? 'Enter' : end); await layer.click()
          await setup.verify(await save(page, workers), x, y)
          await canvasPixels(page, pixels, `${label}: ${end} restores exact pixels`)
        }
        if (input === 'pointer') {
          const box = await field.boundingBox(), cx = box.x + box.width / 2, cy = box.y + box.height / 2
          await page.mouse.move(cx, cy); await page.mouse.down()
          await page.mouse.move(cx + 8, cy); await page.mouse.up()
        } else { await enter(); await page.keyboard.press('ArrowUp'); await page.keyboard.press('Enter') }
        await layer.click()
        assert.notEqual(await canvasPixels(page, undefined, `${label}: stable scrub or step`), pixels, 'scrub or step previews a move')
        await page.keyboard.press('Control+z')
        await canvasPixels(page, pixels, `${label}: scrub or step undo restores exact pixels`)
        const next = cycle ? 7.123456789 : 55.123456789
        await enter(); await page.keyboard.press('Control+a'); await page.keyboard.type(String(next))
        await expect(field).toHaveValue(String(next))
        await page.keyboard.press('Enter'); await layer.click()
        const moved = await canvasPixels(page, undefined, `${label}: stable committed move`)
        assert.notEqual(moved, pixels)
        await page.keyboard.press('Control+z')
        await canvasPixels(page, pixels, `${label}: one undo restores exact pixels`)
        await setup.verify(await save(page, workers), x, y)
        await page.keyboard.press('Control+Shift+z')
        await canvasPixels(page, moved, `${label}: redo restores exact pixels`)
        if (cycle) y = next; else x = next
        buffer = await save(page, workers)
        await setup.verify(buffer, x, y)
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('mixed coordinate zero commits and cancels as one selection operation', { timeout: 180000 }, async () => {
  const setup = await fixture(true), baseline = await parse(setup.buffer)
  const initial = baseline.getChildren(named(baseline, 'Edited position').id)
  const records = initial.map(node => structuredClone(sourceAbsoluteRecord(node)))
  const protectedNodes = ['Position master', 'Untouched position'].map(name => [name, shape(baseline, named(baseline, name))])
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: [
    '--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--disable-blink-features=FileSystemAccessLocal',
    ...(endpoint.hostname === 'openpencil' ? [`--unsafely-treat-insecure-origin-as-secure=${endpoint.origin}`] : []),
  ] })
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } }), errors = [], workers = []
    page.on('pageerror', error => errors.push(error.message))
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
    page.on('worker', worker => workers.push(new URL(worker.url()).pathname))
    await page.goto(endpoint.href)
    await page.getByRole('button', { name: 'Page 1', exact: true }).waitFor()
    await page.getByRole('menuitem', { name: 'File', exact: true }).click()
    const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click()])
    await chooser.setFiles({ name: 'mixed-coordinate-history.fig', mimeType: 'application/octet-stream', buffer: setup.buffer })
    const root = page.getByRole('treeitem', { name: 'Edited position Lock Hide', exact: true })
    await root.click(); await root.press('ArrowRight')
    const left = page.getByRole('treeitem', { name: 'left placement Lock Hide', exact: true })
    const right = page.getByRole('treeitem', { name: 'right placement Lock Hide', exact: true })
    const verify = async (expected, key) => {
      const buffer = await save(page, workers), graph = await parse(buffer), parent = named(graph, 'Edited position')
      const children = graph.getChildren(parent.id)
      assert.equal(graph.getNode(parent.componentId)?.name, 'Position master')
      assert.deepEqual(children.map(sourceAbsoluteRecord), expected)
      if (!key) assert.deepEqual(children.map(node => shape(graph, node)), initial.map(node => shape(baseline, node)))
      for (const [index, node] of children.entries()) {
        assert.deepEqual([node.x, node.y], ['x', 'y'].map(axis => axis === key ? 0 : initial[index][axis]))
      }
      for (const [name, value] of protectedNodes) assert.deepEqual(shape(graph, named(graph, name)), value, name)
      return buffer
    }
    for (const [key, axis, finish] of [['x', 'horizontal', 'Enter'], ['y', 'vertical', 'Tab']]) {
      const select = async () => { await left.click(); await right.click({ modifiers: ['Control'] }) }
      const field = page.getByRole('spinbutton', { name: `${key.toUpperCase()} Axis`, exact: true })
      const enter = async () => {
        await select()
        for (let step = 0; step < 120 && !await field.evaluate(node => node === document.activeElement); step++) await page.keyboard.press('Tab')
        await expect(field).toBeFocused()
        assert.equal(await field.evaluate(node => node.matches(':focus-visible')), true)
      }
      for (const [draft, end] of [['', 'Escape'], ['', 'Enter'], ['55.123456789', 'Escape'], ['55.123456789x', 'Enter']]) {
        await enter(); await page.keyboard.press('Control+a')
        if (draft) await page.keyboard.type(draft)
        await page.keyboard.press(end); await left.click()
        await verify(records)
      }
      await enter(); await page.keyboard.type('0'); await page.keyboard.press(finish); await left.click()
      const moved = structuredClone(records)
      for (const [index, record] of moved.entries()) record[axis].inset = key === 'x' && index === 1 ? 320 - initial[index].width : 0
      await verify(moved, key)
      await page.keyboard.press('Control+z'); await verify(records)
      await page.keyboard.press('Control+Shift+z'); const movedBuffer = await verify(moved, key)
      await page.keyboard.press('Control+z'); await verify(records)
      if (key === 'y') {
        await page.getByRole('menuitem', { name: 'File', exact: true }).click()
        const [reopen] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click()])
        await reopen.setFiles({ name: 'mixed-reopened.fig', mimeType: 'application/octet-stream', buffer: movedBuffer })
        await page.getByRole('treeitem', { name: 'Edited position Lock Hide', exact: true }).click()
        await verify(moved, key)
      }
    }
    assert.deepEqual(errors, [])
  } finally { await browser.close() }
})
