import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import { buildFoundation } from '../foundation.mjs'

const endpoint = new URL(process.env.PLATFORMKIT_OPENPENCIL_URL)
assert.ok(endpoint.protocol === 'http:' && ['127.0.0.1', 'localhost', 'openpencil'].includes(endpoint.hostname),
  'Use a disposable local editor or the isolated CI job service, never staging')
assert.ok(!endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash && endpoint.pathname === '/',
  'Supply only the disposable editor origin, without credentials or a document path')
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const masterOf = (graph, name) => [...graph.getAllNodes()].find(node => node.type === 'COMPONENT' && node.name === name)
const figBuffer = bytes => Uint8Array.from(bytes).buffer

function geometry(graph, node) {
  return {
    ...Object.fromEntries(['type', 'name', 'x', 'y', 'width', 'height', 'uniformScaleFactor',
      'fills', 'strokes', 'vectorNetwork'].map(key => [key, node[key]])),
    component: graph.getNode(node.componentId)?.name,
    bindings: Object.fromEntries(Object.entries(node.boundVariables).map(([field, id]) => [field, graph.variables.get(id)?.name])),
    children: graph.getChildren(node.id).map(child => geometry(graph, child)),
  }
}

async function verifyDownload(bytes, untouched, trailing, replacementGeometry) {
  const graph = await parseFigFile(figBuffer(bytes), { populate: 'all' })
  for (const [name, expected] of untouched) assert.deepEqual(geometry(graph, named(graph, name)), expected, name)
  const owner = named(graph, 'Edited instance'), children = graph.getChildren(owner.id)
  assert.equal(owner.type, 'INSTANCE')
  assert.equal(graph.getNode(owner.componentId)?.name, 'Replacement owner')
  assert.equal(children.length, 2)
  assert.deepEqual(geometry(graph, children[1]), trailing)
  assert.deepEqual(geometry(graph, children[0]), replacementGeometry, 'replacement vector geometry and paints')
  assert.equal(graph.getNode(children[0].componentId)?.name, 'x')
  assert.equal(children[0].uniformScaleFactor, Math.fround(20 / 24))
  assert.ok(Math.abs(children[0].width - 20) < 1e-5)
  assert.ok(Math.abs(children[0].height - 20) < 1e-5)
  const raw = parseFigBuffer(figBuffer(bytes)), changes = raw.nodeChanges
  const replacement = changes.find(node => node.type === 'SYMBOL' && node.name === 'x')
  const source = changes.find(node => node.type === 'SYMBOL' && node.name === 'plus')
  const rawOwner = changes.find(node => node.name === 'Replacement owner')
  const rawEdited = changes.find(node => node.name === 'Edited instance')
  assert.equal(rawOwner.componentPropDefs.length, 2)
  for (const definition of rawOwner.componentPropDefs) assert.deepEqual(definition.initialValue.guidValue, source.guid)
  assert.deepEqual(rawEdited.componentPropAssignments, [{ defID: { sessionID: 91, localID: 1 },
    value: { guidValue: replacement.guid } }])
  const occurrence = changes.find(node => node.type === 'INSTANCE' &&
    JSON.stringify(node.parentIndex.guid) === JSON.stringify(rawOwner.guid) &&
    node.componentPropRefs.some(ref => ref.defID.sessionID === 91 && ref.defID.localID === 1))
  const swaps = rawEdited.symbolData.symbolOverrides.filter(override => override.overriddenSymbolID)
  assert.equal(swaps.length, 1)
  assert.deepEqual(swaps[0].guidPath.guids, [occurrence.guid])
  assert.deepEqual(swaps[0].overriddenSymbolID, replacement.guid)
}

test('browser file-input replacement survives public editing, history and two downloaded FIG saves', { timeout: 120000 }, async () => {
  const provenance = await (await fetch(new URL('/platformkit-provenance.json', endpoint))).json()
  assert.equal(provenance.scope, 'generic-editor-without-packaged-design')
  assert.deepEqual(Object.keys(provenance.adapter.inputs).sort(), [
    'Dockerfile', 'LICENSE', 'NOTICE', 'build-editor.mjs', 'corrections.mjs', 'exporter-correction.mjs',
    'layout-correction.mjs', 'nginx.conf', 'package-lock.json', 'package.json', 'property-correction.mjs',
    'scaling-correction.mjs', 'sync-correction.mjs',
  ])
  for (const [name, digest] of Object.entries(provenance.adapter.inputs)) {
    assert.match(name, /^[A-Za-z0-9._-]+$/)
    const path = ['LICENSE', 'NOTICE'].includes(name) ? `../../../../${name}` : `../${name}`
    assert.equal(createHash('sha256').update(readFileSync(new URL(path, import.meta.url))).digest('hex'), digest, name)
  }
  const source = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8',
  }))
  const graph = buildFoundation(source), pageNode = graph.addPage('Editor replacement')
  const plus = masterOf(graph, 'plus')
  const master = graph.createNode('COMPONENT', pageNode.id, {
    name: 'Replacement owner', x: 20, y: 20, width: 64, height: 32,
    componentPropertyDefinitions: [
      { id: '91:1', name: 'Leading icon', type: 'INSTANCE_SWAP', defaultValue: plus.id },
      { id: '91:2', name: 'Trailing icon', type: 'INSTANCE_SWAP', defaultValue: plus.id },
    ],
  })
  for (const [propertyId, size, x] of [['91:1', 20, 0], ['91:2', 16, 32]]) {
    graph.createInstance(plus.id, master.id, { uniformScaleFactor: size / 24, x,
      componentPropertyReferences: [{ propertyId, field: 'INSTANCE_SWAP' }] })
  }
  graph.createInstance(master.id, pageNode.id, { name: 'Edited instance', x: 150, y: 20 })
  graph.createInstance(master.id, pageNode.id, { name: 'Untouched sibling', x: 300, y: 20 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = [...baseline.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Untouched sibling')
    .map(node => [node.name, geometry(baseline, node)])
  const trailing = geometry(baseline, baseline.getChildren(named(baseline, 'Edited instance').id)[1])
  const leadingBefore = baseline.getChildren(named(baseline, 'Edited instance').id)[0]
  const reference = baseline.createInstance(masterOf(baseline, 'x').id, named(baseline, 'Editor replacement').id, {
    x: leadingBefore.x, y: leadingBefore.y, uniformScaleFactor: 20 / 24,
  })
  const replacementGeometry = geometry(baseline, reference)
  const browser = await chromium.launch({ headless: true, args: [
    '--enable-automation', '--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader',
    '--disable-blink-features=FileSystemAccessLocal',
  ] })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const page = await context.newPage(), errors = [], workers = []
        page.on('pageerror', error => errors.push(error.message))
        page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
        page.on('worker', worker => workers.push(new URL(worker.url()).pathname))
        await page.goto(endpoint.href)
        // Exercise the supported file-input/download path, not a native OS picker.
        assert.equal(await page.evaluate(() => typeof window.showOpenFilePicker), 'undefined')
        await page.getByRole('button', { name: 'Page 1', exact: true }).waitFor()
        await page.getByRole('menuitem', { name: 'File', exact: true }).click()
        const [chooser] = await Promise.all([
          page.waitForEvent('filechooser'),
          page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click(),
        ])
        await chooser.setFiles({ name: `replacement-${cycle}.fig`, mimeType: 'application/octet-stream', buffer })
        await page.getByRole('button', { name: 'Editor replacement', exact: true }).click()
        await page.getByRole('treeitem', { name: /^Edited instance / }).click()
        const leading = page.getByRole('combobox', { name: 'Leading icon', exact: true })
        const trailingControl = page.getByRole('combobox', { name: 'Trailing icon', exact: true })
        await leading.getByText(cycle === 0 ? 'plus' : 'x', { exact: true }).waitFor()
        if (cycle === 0) {
          for (let step = 0; step < 60 && !await leading.evaluate(node => node === document.activeElement); step++) {
            await page.keyboard.press('Tab')
          }
          assert.ok(await leading.evaluate(node => node === document.activeElement), 'Tab reaches the named property')
          await page.keyboard.press('Enter')
          await page.getByRole('option', { name: 'plus', exact: true }).waitFor()
          await page.keyboard.press('Escape')
          assert.ok(await leading.evaluate(node => node === document.activeElement), 'Escape restores picker focus')
          await page.keyboard.press('ArrowDown')
          await page.getByRole('option', { name: 'x', exact: true }).waitFor()
          const index = await page.getByRole('option').evaluateAll(nodes => nodes.findIndex(node => node.textContent.trim() === 'x'))
          assert.ok(index >= 0)
          await page.keyboard.press('End')
          await page.getByRole('option').last().and(page.locator(':focus')).waitFor()
          for (let step = await page.getByRole('option').count() - 2; step >= index; step--) {
            await page.keyboard.press('ArrowUp')
            await page.getByRole('option').nth(step).and(page.locator(':focus')).waitFor()
          }
          assert.notEqual(await page.getByRole('option', { name: 'x', exact: true }).getAttribute('data-highlighted'), null)
          await page.keyboard.press('Enter')
          await leading.getByText('x', { exact: true }).waitFor()
          await page.keyboard.press('Control+z')
          await leading.getByText('plus', { exact: true }).waitFor()
          await page.keyboard.press('Control+Shift+z')
          await leading.getByText('x', { exact: true }).waitFor()
        }
        assert.equal((await trailingControl.textContent()).trim(), 'plus')
        if (cycle < 2) {
          const [download] = await Promise.all([page.waitForEvent('download'), page.keyboard.press('Control+s')])
          const chunks = []
          for await (const chunk of await download.createReadStream()) chunks.push(chunk)
          buffer = Buffer.concat(chunks)
          await verifyDownload(buffer, untouched, trailing, replacementGeometry)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)), 'real browser export worker executed')
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)), 'real browser parse worker executed')
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})
