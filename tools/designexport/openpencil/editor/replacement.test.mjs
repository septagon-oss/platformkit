import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import OpenType from 'opentype.js'
import { chromium } from 'playwright'
import { expect } from 'playwright/test'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { SceneGraph } from '@open-pencil/scene-graph'
import { computeLayout } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { parseFigBuffer } from '@open-pencil/fig'
import { buildFoundation } from '../foundation.mjs'
import { buildComponentDocument } from '../document.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { chain } from '../exporter-correction.mjs'
import { captureExample } from '../browser/capture.mjs'

const endpoint = new URL(process.env.PLATFORMKIT_OPENPENCIL_URL)
assert.ok(endpoint.protocol === 'http:' && ['127.0.0.1', 'localhost', 'openpencil'].includes(endpoint.hostname),
  'Use a disposable local editor or the isolated CI job service, never staging')
assert.ok(!endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash && endpoint.pathname === '/',
  'Supply only the disposable editor origin, without credentials or a document path')
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const masterOf = (graph, name) => [...graph.getAllNodes()].find(node => node.type === 'COMPONENT' && node.name === name)
const figBuffer = bytes => Uint8Array.from(bytes).buffer
const browserArgs = ['--enable-automation', '--font-render-hinting=none', '--use-gl=angle',
  '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--disable-blink-features=FileSystemAccessLocal',
  // Full Chromium honors this CI-only HTTP secure-context test exception
  // to this already-validated origin; it never changes the deployed editor.
  ...(endpoint.hostname === 'openpencil' ? [`--unsafely-treat-insecure-origin-as-secure=${endpoint.origin}`] : [])]

async function openDocument(context, buffer, name, prepare) {
  const page = await context.newPage(), errors = [], workers = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('worker', worker => workers.push(new URL(worker.url()).pathname))
  await page.goto(endpoint.href)
  assert.equal(await page.evaluate(() => typeof window.showOpenFilePicker), 'undefined')
  await page.getByRole('button', { name: 'Page 1', exact: true }).waitFor()
  if (prepare) await prepare(page)
  await page.getByRole('menuitem', { name: 'File', exact: true }).click()
  const [chooser] = await Promise.all([page.waitForEvent('filechooser'),
    page.getByRole('menuitem', { name: 'Open… Ctrl+O', exact: true }).click()])
  await chooser.setFiles({ name, mimeType: 'application/octet-stream', buffer })
  return { page, errors, workers }
}

async function saveDocument(page, errors, workers) {
  const [download] = await Promise.all([page.waitForEvent('download'), page.keyboard.press('Control+s')]).catch(async cause => {
    const focus = await page.evaluate(() => ({ tag: document.activeElement?.tagName, role: document.activeElement?.getAttribute('role') }))
    throw new Error(JSON.stringify({ errors, workers, focus, alerts: await page.getByRole('alert').allTextContents() }), { cause })
  })
  const chunks = []
  for await (const chunk of await download.createReadStream()) chunks.push(chunk)
  return Buffer.concat(chunks)
}

function sourceNode(graph, path) {
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const roots = [...graph.getAllNodes()].filter(node => JSON.stringify(origin(node)?.path) === JSON.stringify(path.slice(0, 1)))
  assert.equal(roots.length, 1)
  let node = roots[0]
  for (const id of path.slice(1)) {
    const children = graph.getChildren(node.id).filter(child => origin(child)?.localId === id)
    assert.equal(children.length, 1)
    node = children[0]
  }
  return node
}

function geometry(graph, node) {
  return {
    ...Object.fromEntries(['type', 'name', 'x', 'y', 'width', 'height', 'uniformScaleFactor',
      'fills', 'strokes', 'vectorNetwork', 'text', 'fontFamily', 'fontWeight', 'fontSize'].map(key => [key, node[key]])),
    component: graph.getNode(node.componentId)?.name,
    bindings: Object.fromEntries(Object.entries(node.boundVariables).map(([field, id]) => [field, graph.variables.get(id)?.name])),
    children: graph.getChildren(node.id).map(child => geometry(graph, child)),
  }
}

function nestedPropertyOwner(graph, depth) {
  let owner = named(graph, 'Edited instance')
  for (let level = 0; level < depth; level++) owner = graph.getChildren(owner.id)[0]
  return owner
}

async function verifyDownload(bytes, untouched, trailing, replacementGeometry, depth = 0) {
  const graph = await parseFigFile(figBuffer(bytes), { populate: 'all' })
  for (const [name, expected] of untouched) assert.deepEqual(geometry(graph, named(graph, name)), expected, name)
  const owner = nestedPropertyOwner(graph, depth), children = graph.getChildren(owner.id)
  assert.equal(owner.type, 'INSTANCE')
  assert.equal(chain(graph, owner, 'componentId').at(-1).name, 'Replacement owner')
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
  const assignments = depth ? rawEdited.symbolData.symbolOverrides.find(override =>
    override.guidPath.guids.length === depth && override.componentPropAssignments?.length)?.componentPropAssignments : rawEdited.componentPropAssignments
  assert.deepEqual(assignments, [{ defID: { sessionID: 91, localID: 1 },
    value: { guidValue: replacement.guid } }])
  const occurrence = changes.find(node => node.type === 'INSTANCE' &&
    JSON.stringify(node.parentIndex.guid) === JSON.stringify(rawOwner.guid) &&
    node.componentPropRefs.some(ref => ref.defID.sessionID === 91 && ref.defID.localID === 1))
  const swaps = rawEdited.symbolData.symbolOverrides.filter(override => override.overriddenSymbolID)
  assert.equal(swaps.length, 1)
  const path = []
  for (let level = depth; level > 0; level--) {
    const wrapper = changes.find(node => node.type === 'SYMBOL' && node.name === `Wrapper ${level}`)
    const child = changes.find(node => node.type === 'INSTANCE' &&
      JSON.stringify(node.parentIndex.guid) === JSON.stringify(wrapper.guid))
    path.push(child.guid)
  }
  assert.deepEqual(swaps[0].guidPath.guids, [...path, occurrence.guid])
  assert.deepEqual(swaps[0].overriddenSymbolID, replacement.guid)
}

async function verifyBuild() {
  const provenance = await (await fetch(new URL('/platformkit-provenance.json', endpoint))).json()
  assert.equal(provenance.scope, 'generic-editor-without-packaged-design')
  assert.deepEqual(Object.keys(provenance.adapter.inputs).sort(), [
    'Dockerfile', 'LICENSE', 'NOTICE', 'build-editor.mjs', 'color-expression.mjs', 'computed-color.mjs', 'corrections.mjs', 'exporter-correction.mjs', 'font-correction.mjs',
    'layout-correction.mjs', 'nginx.conf', 'package-lock.json', 'package.json', 'property-correction.mjs',
    'scaling-correction.mjs', 'sync-correction.mjs', 'variable-color.mjs',
  ])
  for (const [name, digest] of Object.entries(provenance.adapter.inputs)) {
    assert.match(name, /^[A-Za-z0-9._-]+$/)
    const path = ['LICENSE', 'NOTICE'].includes(name) ? `../../../../${name}` : `../${name}`
    assert.equal(createHash('sha256').update(readFileSync(new URL(path, import.meta.url))).digest('hex'), digest, name)
  }
}

test('derived native colors follow keyboard palette edits and survive two browser worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), collection = graph.createCollection('Palette'), pageNode = graph.getPages()[0]
  graph.updateNode(pageNode.id, { name: 'Variable proof' })
  const ink = graph.createVariable('Ink', 'COLOR', collection.id, { r: 0, g: 0, b: 0, a: 1 })
  const derived = graph.createVariable('Secondary', 'COLOR', collection.id, { cssColor: {
    value: 'color-mix(in srgb, var(--ink) 75%, #fff)', customProperties: { '--ink': { aliasId: ink.id } },
  } })
  const master = graph.createNode('COMPONENT', pageNode.id, { name: 'Token master', width: 40, height: 40,
    fills: [{ type: 'SOLID', color: { r: .25, g: .25, b: .25, a: 1 }, visible: true, opacity: 1 }] })
  graph.bindVariable(master.id, 'fills/0/color', derived.id)
  graph.createInstance(master.id, pageNode.id, { name: 'Token instance', x: 80 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `formula-${cycle}.fig`)
        await page.getByRole('button', { name: 'Variable proof', exact: true }).waitFor()
        const tabTo = async target => {
          for (let step = 0; step < 80 && !await target.evaluate(node => node === document.activeElement); step++) {
            await page.keyboard.press('Tab')
          }
          assert.ok(await target.evaluate(node => node === document.activeElement && node.matches(':focus-visible')))
        }
        await tabTo(page.getByRole('button', { name: 'Open variables', exact: true }))
        await page.keyboard.press('Enter')
        const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
        await dialog.waitFor()
        const secondary = dialog.getByRole('row').filter({ hasText: 'Secondary' })
        await expect(secondary).toContainText(cycle === 0 ? 'Derived #404040' : 'Derived #414040')
        if (cycle === 0) {
          const picker = dialog.getByRole('row').filter({ hasText: 'Ink' }).getByRole('button', { name: 'Edit color', exact: true })
          await tabTo(picker)
          await page.keyboard.press('Enter')
          const red = page.getByRole('spinbutton', { name: 'Red', exact: true })
          await tabTo(red)
          await page.keyboard.press('ArrowUp')
          await expect(red).toHaveValue('1')
          await page.keyboard.press('Escape')
          await expect(secondary).toContainText('Derived #414040')
        }
        await page.keyboard.press('Escape')
        if (cycle === 0) {
          await page.keyboard.press('Control+z')
          await tabTo(page.getByRole('button', { name: 'Open variables', exact: true }))
          await page.keyboard.press('Space')
          await expect(secondary).toContainText('Derived #404040')
          await page.keyboard.press('Escape')
          await page.keyboard.press('Control+Shift+z')
        }
        if (cycle === 2) { assert.deepEqual(errors, []); continue }
        buffer = await saveDocument(page, errors, workers)
        assert.ok(workers.some(path => /\/export-worker-.*\.js$/.test(path)))
        const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
        const next = [...reopened.variables.values()].find(variable => variable.name === 'Secondary')
        assert.equal(Object.values(next.valuesByMode)[0].cssColor.value, Object.values(derived.valuesByMode)[0].cssColor.value)
        const value = reopened.resolveVariable(next.id)
        assert.deepEqual(value, { r: .25 + .75 * Math.fround(1 / 255), g: .25, b: .25, a: 1 })
        assert.equal(named(reopened, 'Token instance').componentId, named(reopened, 'Token master').id)
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('source minimum-gap wrapping survives keyboard width edits, history and two editor saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), pageNode = graph.getPages()[0]
  const item = graph.createNode('COMPONENT', pageNode.id, { name: 'Reusable item', width: 100, height: 32 })
  const row = graph.createNode('COMPONENT', pageNode.id, {
    name: 'Source row', width: 331, height: 80, layoutMode: 'HORIZONTAL', layoutWrap: 'WRAP',
    primaryAxisAlign: 'SPACE_BETWEEN', counterAxisAlign: 'MIN', primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG',
    itemSpacing: 16, counterAxisSpacing: 16, pluginData: [{
      pluginId: 'platformkit', key: 'platformkit.source',
      value: JSON.stringify({ schema: 'platformkit.design-export.v1', scope: 'source-composition-observed-aliases' }),
    }],
  })
  for (let index = 0; index < 3; index++) graph.createInstance(item.id, row.id, { name: `Item ${index}` })
  computeLayout(graph, row.id)
  graph.createInstance(row.id, pageNode.id, { name: 'Edited row', x: 20, y: 120 })
  graph.createInstance(row.id, pageNode.id, { name: 'Untouched row', x: 500, y: 120 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Reusable item', 'Source row', 'Untouched row'].map(name => [name, geometry(baseline, named(baseline, name))])
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `minimum-gap-${cycle}.fig`)
        await page.getByRole('treeitem', { name: 'Edited row Lock Hide', exact: true }).click()
        const width = page.getByRole('spinbutton', { name: 'Width', exact: true })
        const height = page.getByRole('spinbutton', { name: 'Height', exact: true })
        const previous = cycle === 1 ? 332 : 331, next = cycle === 0 ? 332 : 331
        await expect(width).toHaveAttribute('aria-valuenow', String(previous))
        await expect(height).toHaveAttribute('aria-valuenow', previous === 331 ? '80' : '32')
        if (cycle === 2) {
          assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
          assert.deepEqual(errors, [])
          continue
        }
        const borderBefore = await width.evaluate(node => getComputedStyle(node).borderColor)
        for (let step = 0; step < 60 && !await width.evaluate(node => node === document.activeElement); step++) {
          await page.keyboard.press('Tab')
        }
        assert.ok(await width.evaluate(node => node === document.activeElement), 'Tab reaches the named width control')
        assert.ok(await width.evaluate(node => node.matches(':focus-visible')))
        assert.notEqual(await width.evaluate(node => getComputedStyle(node).borderColor), borderBefore, 'keyboard focus has a visible border change')
        await page.keyboard.press(cycle === 0 ? 'ArrowUp' : 'ArrowDown')
        await expect(width).toHaveAttribute('aria-valuenow', String(next))
        await expect(height).toHaveAttribute('aria-valuenow', next === 331 ? '80' : '32')
        await page.getByRole('treeitem', { name: 'Edited row Lock Hide', exact: true }).click()
        await page.keyboard.press('Control+z')
        await expect(width).toHaveAttribute('aria-valuenow', String(previous))
        await expect(height).toHaveAttribute('aria-valuenow', previous === 331 ? '80' : '32')
        await page.keyboard.press('Control+Shift+z')
        await expect(width).toHaveAttribute('aria-valuenow', String(next))
        await expect(height).toHaveAttribute('aria-valuenow', next === 331 ? '80' : '32')
        buffer = await saveDocument(page, errors, workers)
        const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' }), edited = named(reopened, 'Edited row')
        assert.deepEqual([edited.x, edited.y, edited.width, edited.height], [20, 120, next, next === 331 ? 80 : 32])
        assert.deepEqual(reopened.getChildren(edited.id).map(node => [node.x, node.y, node.width, node.height]),
          next === 331 ? [[0, 0, 100, 32], [231, 0, 100, 32], [0, 48, 100, 32]] :
            [[0, 0, 100, 32], [116, 0, 100, 32], [232, 0, 100, 32]])
        for (const child of reopened.getChildren(edited.id)) assert.equal(chain(reopened, child, 'componentId').at(-1).name, 'Reusable item')
        for (const [name, expected] of untouched) assert.deepEqual(geometry(reopened, named(reopened, name)), expected, name)
        assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('browser file-input replacement survives public editing, history and two downloaded FIG saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const source = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8',
  }))
  const graph = buildFoundation(source).graph, pageNode = graph.addPage('Editor replacement')
  const plus = masterOf(graph, 'plus')
  const role = [...graph.variables.values()].find(variable => variable.name === '--pk-color-accent-on')
  const master = graph.createNode('COMPONENT', pageNode.id, {
    name: 'Replacement owner', x: 20, y: 20, width: 64, height: 32,
    componentPropertyDefinitions: [
      { id: '91:1', name: 'Leading icon', type: 'INSTANCE_SWAP', defaultValue: plus.id },
      { id: '91:2', name: 'Trailing icon', type: 'INSTANCE_SWAP', defaultValue: plus.id },
    ],
  })
  for (const [propertyId, size, x] of [['91:1', 20, 0], ['91:2', 16, 32]]) {
    const occurrence = graph.createInstance(plus.id, master.id, { uniformScaleFactor: size / 24, x,
      componentPropertyReferences: [{ propertyId, field: 'INSTANCE_SWAP' }] })
    if (propertyId === '91:1') for (const vector of graph.getChildren(occurrence.id)) {
      for (const field of Object.keys(vector.boundVariables)) graph.bindVariable(vector.id, field, role.id)
    }
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
  const importedRole = [...baseline.variables.values()].find(variable => variable.name === role.name)
  for (const vector of baseline.getChildren(reference.id)) {
    const paints = structuredClone({ fills: vector.fills, strokes: vector.strokes })
    for (const field of Object.keys(vector.boundVariables)) {
      baseline.bindVariable(vector.id, field, importedRole.id)
      // FIG materializes the resolved override as a float32 fallback paint.
      // The independent reference has not been serialized since this binding.
      const [paint, index] = field.split('/')
      paints[paint][Number(index)].color = Object.fromEntries(Object.entries(
        baseline.resolveColorVariableForNode(vector.id, importedRole.id)).map(([channel, value]) => [channel, Math.fround(value)]))
    }
    baseline.updateNode(vector.id, paints)
  }
  const replacementGeometry = geometry(baseline, reference)
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `replacement-${cycle}.fig`)
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
          buffer = await saveDocument(page, errors, workers)
          await verifyDownload(buffer, untouched, trailing, replacementGeometry)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)), 'real browser export worker executed')
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)), 'real browser parse worker executed')
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

for (const depth of [1, 2]) test(`nested property picker retains native ownership, history and two worker saves: depth=${depth}`, { timeout: 120000 }, async () => {
  await verifyBuild()
  const source = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8',
  }))
  const graph = buildFoundation(source).graph, pageNode = graph.addPage('Nested replacement')
  const plus = masterOf(graph, 'plus')
  const master = graph.createNode('COMPONENT', pageNode.id, { name: 'Replacement owner', width: 64, height: 32,
    componentPropertyDefinitions: [
      { id: '91:1', name: 'Leading icon', type: 'INSTANCE_SWAP', defaultValue: plus.id },
      { id: '91:2', name: 'Trailing icon', type: 'INSTANCE_SWAP', defaultValue: plus.id },
    ] })
  for (const [propertyId, size, x] of [['91:1', 20, 0], ['91:2', 16, 32]]) {
    graph.createInstance(plus.id, master.id, { uniformScaleFactor: size / 24, x,
      componentPropertyReferences: [{ propertyId, field: 'INSTANCE_SWAP' }] })
  }
  let outer = master
  for (let level = 1; level <= depth; level++) {
    const wrapper = graph.createNode('COMPONENT', pageNode.id, { name: `Wrapper ${level}`, width: 64, height: 32 })
    graph.createInstance(outer.id, wrapper.id, { name: `Nested ${level}` })
    outer = wrapper
  }
  graph.createInstance(outer.id, pageNode.id, { name: 'Edited instance', x: 150, y: 20 })
  graph.createInstance(outer.id, pageNode.id, { name: 'Untouched sibling', x: 300, y: 20 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = [...baseline.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Untouched sibling')
    .map(node => [node.name, geometry(baseline, node)])
  const trailing = geometry(baseline, baseline.getChildren(nestedPropertyOwner(baseline, depth).id)[1])
  const reference = baseline.createInstance(masterOf(baseline, 'x').id, named(baseline, 'Nested replacement').id, { uniformScaleFactor: 20 / 24 })
  const expected = geometry(baseline, reference)
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `nested-${depth}-${cycle}.fig`)
        await page.getByRole('button', { name: 'Nested replacement', exact: true }).click()
        await page.getByRole('treeitem', { name: /^Edited instance / }).click()
        for (let level = depth; level > 0; level--) {
          await page.keyboard.press('ArrowRight')
          await page.getByRole('treeitem', { name: `Nested ${level} Lock Hide`, exact: true }).click()
        }
        const control = page.getByRole('combobox', { name: 'Leading icon', exact: true })
        await control.getByText(cycle ? 'x' : 'plus', { exact: true }).waitFor()
        if (cycle === 0) {
          await control.click()
          await page.getByRole('option', { name: 'x', exact: true }).click()
          await control.getByText('x', { exact: true }).waitFor()
          await page.keyboard.press('Control+z')
          await control.getByText('plus', { exact: true }).waitFor()
          await page.keyboard.press('Control+Shift+z')
          await control.getByText('x', { exact: true }).waitFor()
        }
        assert.equal((await page.getByRole('combobox', { name: 'Trailing icon', exact: true }).textContent()).trim(), 'plus')
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          await verifyDownload(buffer, untouched, trailing, expected, depth)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('generated Form, Buttons and wrapping Text support local fonts, property edits and two worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const hash = bytes => createHash('sha256').update(bytes).digest('hex')
  const source = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8',
  }))
  const form = 'pk-ui.component.form/default', button = 'pk-ui.component.button/with-leading-icon'
  const paragraph = 'pk-ui.component.text/muted', secondary = 'pk-ui.component.button/secondary'
  const temporary = await mkdtemp(join(tmpdir(), 'platformkit-editor-fonts-'))
  let browser, comparisonBrowser, renderer
  try {
    // Chromium Local Font Access cannot read WOFF blobs. Re-encode fixtures as
    // static OTF without changing their legacy names; use these exact bytes at
    // both boundaries, not a claim of original WOFF/hinting equivalence.
    const fonts = []
    for (const weight of [400, 500, 600]) {
      const woff = readFileSync(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
      const bytes = new Uint8Array(OpenType.parse(figBuffer(woff)).toArrayBuffer())
      await writeFile(join(temporary, `${weight}.otf`), bytes, { flag: 'wx' })
      fonts.push({ family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: hash(bytes) })
    }
    const config = join(temporary, 'fonts.conf')
    await writeFile(config, `<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "fonts.dtd"><fontconfig><dir>${temporary}</dir><cachedir>${temporary}/cache</cachedir></fontconfig>`, { flag: 'wx' })
    comparisonBrowser = await chromium.launch({ headless: true, args: browserArgs, env: { ...process.env, FONTCONFIG_FILE: config } })
    const ck = await initCanvasKit()
    renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
    const built = await buildComponentDocument(source, { examples: [form, button, paragraph, secondary], fonts,
      browser: comparisonBrowser, renderer, viewport: { width: 320, height: 900 } })
    // Generation and reprojection share one declared measurement environment.
    // Full Chromium supplies editor Local Font Access and worker interaction,
    // not a substitute browser profile for source text-width comparisons.
    browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs, env: { ...process.env, FONTCONFIG_FILE: config } })
    const { graph, placements, selections } = built
    graph.updateNode(selections[1].instance.id, { name: 'Editable button' })
    graph.updateNode(selections[2].instance.id, { name: 'Editable paragraph' })
    graph.updateNode(selections[3].instance.id, { name: 'Editable bordered button' })
    graph.createInstance(selections[0].master.id, placements.id, { name: 'Untouched Form', x: 500, y: 48 })
    let buffer = Buffer.from(await exportFigFile(graph))
    const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
    const inputName = sourceNode(baseline, [form, 'title']).name
    assert.equal(inputName, 'title', 'source-owned nested instances retain meaningful layer names after import')
    const untouched = [...baseline.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Untouched Form')
      .map(node => [node.name, geometry(baseline, node)])
    const values = ['WAVY affinity album title '.repeat(6), ''], labels = ['Create affinity album', 'Return to album']
    const contents = ['Remember the people, places and small details. '.repeat(5).trim(), 'An album description.']
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: ['local-fonts'] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `composition-${cycle}.fig`, async page => {
          assert.deepEqual(await page.evaluate(() => [window.isSecureContext, typeof window.queryLocalFonts]), [true, 'function'])
          await page.keyboard.press('t')
          await page.locator('[data-test-id="canvas-element"]').click({ position: { x: 300, y: 300 } })
          await page.keyboard.type('Font access')
          await page.keyboard.press('Escape')
          await page.getByRole('button', { name: 'Font settings', exact: true }).click()
          const panel = page.locator('[data-test-id="font-settings-panel"]')
          // Online providers can already say Enabled before local access is
          // granted. Check the named rows, never either status interchangeably.
          const localFonts = panel.getByText('Local fonts', { exact: true }).locator('..')
          const onlineFonts = panel.getByText('Online fonts', { exact: true }).locator('..')
          await expect(onlineFonts.getByText('Enabled', { exact: true })).toBeVisible()
          await expect(localFonts.getByText('Enabled', { exact: true })).toHaveCount(0)
          await panel.getByRole('button', { name: 'Allow', exact: true }).click()
          await expect(localFonts.getByText('Enabled', { exact: true })).toBeVisible()
          await expect(panel.getByRole('button', { name: 'Allow', exact: true })).toBeDisabled()
          await page.keyboard.press('Escape')
          const blobs = await page.evaluate(async () => Promise.all((await window.queryLocalFonts()).map(async font =>
            [...new Uint8Array(await (await font.blob()).arrayBuffer())])))
          assert.deepEqual(blobs.map(bytes => hash(Uint8Array.from(bytes))).sort(), fonts.map(face => face.sha256).sort())
        })
        await page.getByRole('button', { name: 'Editable source instances', exact: true }).click()
        for (const name of ['Editable source instances', form]) {
          await page.getByRole('treeitem', { name: `${name} Lock Hide`, exact: true }).click()
          await page.keyboard.press('ArrowRight')
        }
        for (const [name, field, value, initial, previousValue] of [
          [inputName, 'value', values[cycle], '', values[cycle - 1]],
          ['Editable button', 'label', labels[cycle], 'Add item', labels[cycle - 1]],
          ['Editable paragraph', 'content', contents[cycle], 'Plain body copy.', contents[cycle - 1]],
          ['Editable bordered button', 'label', labels[cycle], 'Cancel', labels[cycle - 1]],
        ]) {
          await page.getByRole('treeitem', { name: `${name} Lock Hide`, exact: true }).click()
          const control = page.getByRole('textbox', { name: field, exact: true })
          const previous = cycle ? previousValue : initial
          await expect(control).toHaveValue(previous)
          if (cycle === 2) continue
          await control.fill(value)
          await control.press('Tab')
          await page.getByRole('treeitem', { name: `${name} Lock Hide`, exact: true }).click()
          assert.deepEqual(errors, [], 'property commit must not fail behind the displayed input value')
          await page.keyboard.press('Control+z')
          await expect(control).toHaveValue(previous)
          await page.keyboard.press('Control+Shift+z')
          await expect(control).toHaveValue(value)
        }
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          const fontMetadata = parseFigBuffer(figBuffer(buffer)).nodeChanges.flatMap(node => node.derivedTextData?.fontMetaData ?? [])
            .filter(font => font.key.family === 'IBM Plex Sans')
          assert.deepEqual([...new Set(fontMetadata.map(font => font.fontWeight))].sort(), [400, 500, 600])
          for (const font of fontMetadata) {
            const face = fonts.find(face => face.weight === font.fontWeight)
            assert.equal(Buffer.from(font.fontDigest ?? []).toString('hex'), createHash('sha1').update(face.bytes).digest('hex'),
              'download identifies the exact bytes actually loaded by the editor, not only available system faces')
          }
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          for (const id of [form, button, paragraph, secondary]) {
            const before = sourceNode(baseline, [id]), after = sourceNode(reopened, [id])
            const oldParent = baseline.getNode(before.parentId), newParent = reopened.getNode(after.parentId)
            assert.deepEqual([after.x, after.y, newParent.x, newParent.y], [before.x, before.y, oldParent.x, oldParent.y],
              'layer-tree navigation must not move source placements or their board')
          }
          for (const [name, expected] of untouched) assert.deepEqual(geometry(reopened, named(reopened, name)), expected, name)
          for (const [path, props] of [
            [[form, 'title'], { value: values[cycle] }], [[button], { label: labels[cycle] }],
            [[paragraph], { content: contents[cycle] }],
            [[secondary], { label: labels[cycle] }],
          ]) {
            const result = extractSourceProps(reopened, sourceNode(reopened, path), source)
            if (path.length === 2 && cycle === 1) assert.equal(result.status, 'no-supported-changes')
            else {
              assert.deepEqual(result.proposal, { baseSHA256: source.sha256, path, props })
              const projected = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--proposal'], {
                cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', input: JSON.stringify(result.proposal),
              }))
              assert.notEqual(projected.sha256, source.sha256)
              if ([paragraph, secondary].includes(path[0])) {
                const observed = await captureExample(comparisonBrowser, projected, path[0], { fonts, viewport: { width: 320, height: 900 } })
                const selected = selections.find(item => item.observation.exampleId === path[0])
                assert.deepEqual(observed.environment, selected.observation.environment, 'source comparison profile must not change after editing')
                const placed = sourceNode(reopened, path), expected = observed.roots[0].bounds
                for (const field of ['width', 'height']) assert.ok(Math.abs(placed[field] - expected[field]) <= 1 / 64,
                  `worker-saved ${path[0]} ${field}: ${placed[field]} versus ${expected[field]}`)
                if (path[0] === secondary) {
                  assert.deepEqual([placed.strokes[0].weight, placed.strokes[0].align], [1, 'INSIDE'])
                  assert.equal(reopened.variables.get(placed.boundVariables['strokes/0/color']).name, '--pk-color-border-default')
                }
              }
            }
          }
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally {
    try { renderer?.destroy() } finally {
      try { await browser?.close() } finally {
        try { await comparisonBrowser?.close() } finally { await rm(temporary, { recursive: true, force: true }) }
      }
    }
  }
})
