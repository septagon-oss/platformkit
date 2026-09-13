import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { expect } from 'playwright/test'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { chain } from '../exporter-correction.mjs'

const endpoint = new URL(process.env.PLATFORMKIT_OPENPENCIL_URL)
assert.ok(endpoint.protocol === 'http:' && ['127.0.0.1', 'localhost', 'openpencil'].includes(endpoint.hostname),
  'Use a disposable local editor or isolated CI service, never staging')
assert.ok(!endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash && endpoint.pathname === '/',
  'Supply only the disposable editor origin')
const source = fragment => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
  schema: 'platformkit.design-export.v1', scope: 'source-composition-observed-aliases',
  ...(fragment ? { cssFragment: { version: 1 } } : {}),
}) }]
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const figBuffer = bytes => Uint8Array.from(bytes).buffer
const box = node => [node.x, node.y, node.width, node.height]
const leaves = (graph, parent) => graph.getChildren(parent.id).flatMap(node =>
  ['COMPONENT', 'INSTANCE'].includes(node.type) ? leaves(graph, node) : [node])

function geometry(graph, node) {
  return { name: node.name, type: node.type, box: box(node),
    sizing: [node.layoutMode, node.primaryAxisSizing, node.counterAxisSizing], pluginData: node.pluginData,
    component: node.componentId ? chain(graph, node, 'componentId').at(-1).name : null,
    children: graph.getChildren(node.id).map(child => geometry(graph, child)) }
}

function fixture() {
  const graph = new SceneGraph(), page = graph.getPages()[0]
  const fragment = name => graph.createNode('COMPONENT', page.id, { name, width: 0, height: 0,
    layoutMode: 'NONE', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    fills: [], strokes: [], clipsContent: false, pluginData: source(true) })
  const rectangle = (parent, name, width) => graph.createNode('RECTANGLE', parent.id, { name, width, height: 24,
    fills: [{ type: 'SOLID', color: { r: 0.2, g: 0.4, b: 0.8, a: 1 }, opacity: 1, visible: true }], strokes: [] })
  const empty = fragment('Empty definition'), nested = fragment('Nested definition')
  rectangle(nested, 'second', 64)
  const master = fragment('Fragment definition')
  rectangle(master, 'first', 52)
  graph.createInstance(empty.id, master.id, { name: 'Empty fragment' })
  graph.createInstance(nested.id, master.id, { name: 'Nested fragment' })
  const row = graph.createNode('COMPONENT', page.id, { name: 'Row definition', x: 20, y: 40, width: 235, height: 1,
    layoutMode: 'HORIZONTAL', layoutWrap: 'WRAP', primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG',
    itemSpacing: 8, counterAxisSpacing: 8, paddingTop: 10, paddingRight: 10, paddingBottom: 10, paddingLeft: 10,
    fills: [], strokes: [], clipsContent: false, pluginData: source(false) })
  rectangle(row, 'before', 36)
  graph.createInstance(master.id, row.id, { name: 'Logical fragment' })
  rectangle(row, 'after', 40)
  computeLayout(graph, row.id)
  graph.createInstance(row.id, page.id, { name: 'Edited row', x: 20, y: 160 })
  graph.createInstance(row.id, page.id, { name: 'Untouched row', x: 500, y: 160 })
  return graph
}

async function browserBoxes(browser, width) {
  const context = await browser.newContext()
  try {
    const page = await context.newPage()
    // Ordinary siblings: no DOM wrapper corresponding to a native fragment.
    await page.setContent(`<style>*{box-sizing:border-box}body{margin:0}.row{display:flex;flex-wrap:wrap;
      width:${width}px;padding:10px;gap:8px}.box{height:24px;flex:none}</style><div class="row">
      ${[36, 52, 64, 40].map(size => `<div class="box" style="width:${size}px"></div>`).join('')}</div>`)
    return await page.evaluate(() => {
      const row = document.querySelector('.row'), origin = row.getBoundingClientRect()
      return { height: origin.height, children: [...row.children].map(node => {
        const rect = node.getBoundingClientRect()
        return [rect.x - origin.x, rect.y - origin.y, rect.width, rect.height]
      }) }
    })
  } finally { await context.close() }
}

function verifyGeometry(graph, width, expected) {
  const row = named(graph, 'Edited row'), origin = graph.getAbsolutePosition(row.id)
  assert.deepEqual(box(row), [20, 160, width, expected.height])
  assert.deepEqual(leaves(graph, row).map(node => {
    const point = graph.getAbsolutePosition(node.id)
    return [point.x - origin.x, point.y - origin.y, node.width, node.height]
  }), expected.children, 'saved worker geometry matches the independently rendered siblings without local recomputation')
  assert.deepEqual(leaves(graph, row).map(node => node.name), ['before', 'first', 'second', 'after'])
  const fragment = graph.getChildren(row.id).find(node => node.name === 'Logical fragment')
  assert.equal(fragment.type, 'INSTANCE')
  assert.equal(chain(graph, fragment, 'componentId').at(-1).name, 'Fragment definition')
  assert.deepEqual([fragment.layoutMode, fragment.primaryAxisSizing, fragment.counterAxisSizing], ['NONE', 'HUG', 'HUG'])
  for (const [name, definition, count] of [['Empty fragment', 'Empty definition', 0], ['Nested fragment', 'Nested definition', 1]]) {
    const node = graph.getChildren(fragment.id).find(child => child.name === name)
    assert.equal(chain(graph, node, 'componentId').at(-1).name, definition)
    assert.equal(node.childIds.length, count)
  }
}

test('fragment ownership crosses the real editor, width controls and two FIG worker saves', { timeout: 120000 }, async () => {
  // Provider primitive only; this fixture does not claim creator conversion.
  const provenance = await (await fetch(new URL('/platformkit-provenance.json', endpoint))).json()
  assert.equal(provenance.scope, 'generic-editor-without-packaged-design')
  for (const name of ['source-fragments.mjs', 'grid-fig-correction.mjs', 'corrections.mjs']) assert.equal(
    provenance.adapter.inputs[name], createHash('sha256').update(readFileSync(new URL(`../${name}`, import.meta.url))).digest('hex'), name)
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: [
    '--enable-automation', '--font-render-hinting=none', '--use-gl=angle', '--use-angle=swiftshader',
    '--enable-unsafe-swiftshader', '--disable-blink-features=FileSystemAccessLocal',
    ...(endpoint.hostname === 'openpencil' ? [`--unsafely-treat-insecure-origin-as-secure=${endpoint.origin}`] : []),
  ] })
  try {
    const expected = new Map(await Promise.all([235, 236].map(async width => [width, await browserBoxes(browser, width)])))
    const graph = fixture()
    verifyGeometry(graph, 235, expected.get(235))
    let buffer = Buffer.from(await exportFigFile(graph))
    const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
    verifyGeometry(baseline, 235, expected.get(235))
    const untouched = ['Fragment definition', 'Empty definition', 'Nested definition', 'Row definition', 'Untouched row']
      .map(name => [name, geometry(baseline, named(baseline, name))])
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const page = await context.newPage(), errors = [], workers = []
        page.on('pageerror', error => errors.push(error.message))
        page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
        page.on('worker', worker => workers.push(new URL(worker.url()).pathname))
        await page.goto(endpoint.href)
        assert.equal(await page.evaluate(() => typeof window.showOpenFilePicker), 'undefined')
        await page.getByRole('button', { name: 'Page 1', exact: true }).waitFor()
        await page.getByRole('menuitem', { name: 'File', exact: true }).click()
        const [chooser] = await Promise.all([page.waitForEvent('filechooser'),
          page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click()])
        await chooser.setFiles({ name: `fragments-${cycle}.fig`, mimeType: 'application/octet-stream', buffer })
        const selected = page.getByRole('treeitem', { name: 'Edited row Lock Hide', exact: true })
        await selected.click()
        const width = page.getByRole('spinbutton', { name: 'Width', exact: true })
        const height = page.getByRole('spinbutton', { name: 'Height', exact: true })
        const previous = cycle === 1 ? 236 : 235, next = cycle === 0 ? 236 : 235
        await expect(width).toHaveAttribute('aria-valuenow', String(previous))
        await expect(height).toHaveAttribute('aria-valuenow', String(expected.get(previous).height))
        if (cycle < 2) {
          await width.focus(); await width.press(cycle === 0 ? 'ArrowUp' : 'ArrowDown')
          await expect(width).toHaveAttribute('aria-valuenow', String(next))
          await expect(height).toHaveAttribute('aria-valuenow', String(expected.get(next).height))
          await selected.click(); await page.keyboard.press('Control+z')
          await expect(width).toHaveAttribute('aria-valuenow', String(previous))
          await expect(height).toHaveAttribute('aria-valuenow', String(expected.get(previous).height))
          await page.keyboard.press('Control+Shift+z')
          await expect(width).toHaveAttribute('aria-valuenow', String(next))
          await expect(height).toHaveAttribute('aria-valuenow', String(expected.get(next).height))
          const [download] = await Promise.all([page.waitForEvent('download'), page.keyboard.press('Control+s')])
          const chunks = []
          for await (const chunk of await download.createReadStream()) chunks.push(chunk)
          buffer = Buffer.concat(chunks)
          await download.delete()
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          verifyGeometry(reopened, next, expected.get(next))
          for (const [name, value] of untouched) assert.deepEqual(geometry(reopened, named(reopened, name)), value, name)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})
