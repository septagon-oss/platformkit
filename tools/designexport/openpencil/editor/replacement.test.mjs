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
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { parseFigBuffer } from '@open-pencil/fig'
import { buildFoundation } from '../foundation.mjs'
import { buildComponentDocument } from '../document.mjs'
import { materializeComponent } from '../components.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { chain } from '../exporter-correction.mjs'
import { captureExample } from '../browser/capture.mjs'
import { sourceFixture, exportCore } from '../browser/fixtures.test.mjs'

const endpoint = new URL(process.env.PLATFORMKIT_OPENPENCIL_URL)
assert.ok(endpoint.protocol === 'http:' && ['127.0.0.1', 'localhost', 'openpencil'].includes(endpoint.hostname),
  'Use a disposable local editor or the isolated CI job service, never staging')
assert.ok(!endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash && endpoint.pathname === '/',
  'Supply only the disposable editor origin, without credentials or a document path')
const named = (graph, name) => [...graph.getAllNodes()].find(node => node.name === name)
const masterOf = (graph, name) => [...graph.getAllNodes()].find(node => node.type === 'COMPONENT' && node.name === name)
const figBuffer = bytes => Uint8Array.from(bytes).buffer
const hash = bytes => createHash('sha256').update(bytes).digest('hex')
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
      'fills', 'strokes', 'vectorNetwork', 'text', 'fontFamily', 'fontWeight', 'fontSize',
      'lineHeight', 'letterSpacing', 'italic', 'textDecoration', 'textAlignHorizontal'].map(key => [key, node[key]])),
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
  const notices = await (await fetch(new URL('/licenses/PlatformKit-NOTICE', endpoint))).text()
  assert.equal(notices, readFileSync(new URL('../../../../NOTICE', import.meta.url), 'utf8'), 'shipped notices match the source')
  assert.ok(notices.includes('Blink border geometry — BSD 3-Clause\n\nCopyright (C) 2013 Google Inc.'))
  for (const [file, digest] of [
    ['UnicodeTrie-LICENSE', 'e59138ecbc0b770010b0781905e2bcc181e4f0735494c58fd9681b24f0246187'],
    ['Unicode-LICENSE', 'e7a93b009565cfce55919a381437ac4db883e9da2126fa28b91d12732bc53d96'],
  ]) assert.equal(hash(new Uint8Array(await (await fetch(new URL(`/licenses/${file}`, endpoint))).arrayBuffer())), digest)
  assert.equal(provenance.scope, 'generic-editor-without-packaged-design')
  assert.deepEqual(Object.keys(provenance.adapter.inputs).sort(), [
    'Dockerfile', 'LICENSE', 'NOTICE', 'border-correction.mjs', 'build-editor.mjs', 'color-expression.mjs', 'computed-color.mjs', 'corrections.mjs', 'editor-fonts.mjs', 'exporter-correction.mjs', 'font-correction.mjs', 'fonts.mjs',
    'grid-correction.mjs', 'grid-fig-correction.mjs', 'layout-correction.mjs', 'nginx.conf', 'package-lock.json', 'package.json', 'paragraph-correction.mjs', 'property-correction.mjs',
    'scaling-correction.mjs', 'source-box.mjs', 'source-positioning.mjs', 'sync-correction.mjs', 'variable-color.mjs', 'variant-correction.mjs',
  ])
  for (const [name, digest] of Object.entries(provenance.adapter.inputs)) {
    assert.match(name, /^[A-Za-z0-9._-]+$/)
    const path = ['LICENSE', 'NOTICE'].includes(name) ? `../../../../${name}` : `../${name}`
    assert.equal(createHash('sha256').update(readFileSync(new URL(path, import.meta.url))).digest('hex'), digest, name)
  }
}

test('oversized source words remain editable through keyboard history and two browser worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const id = 'pk-ui.component.text/muted', snapshot = exportCore(['--example', id, '--props'], { content: 'Album', size: 'base' })
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1)), previous = getTextMeasurer()
  try {
    const fonts = editorFontFixtures.filter(face => face.weight === 400).map(({ weight, bytes }) =>
      ({ family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: hash(bytes) }))
    const { graph, selections } = await buildComponentDocument(snapshot, {
      examples: [id], fonts, browser, renderer, viewport: { width: 320, height: 900 },
    })
    const instance = selections[0].instance
    setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
    graph.updateNode(instance.id, { name: 'Wrapping paragraph', width: 41.328125 })
    computeLayout(graph, instance.id)
    let buffer = Buffer.from(await exportFigFile(graph)), previousValue = 'Album'
    const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
    const definition = chain(baseline, named(baseline, 'Wrapping paragraph'), 'componentId').at(-1)
    const untouched = geometry(baseline, definition)
    for (const [value, height] of [['The people and places we remember together.', 168], ['Album', 24]]) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, 'source-wrapping.fig')
        await page.getByRole('button', { name: 'Editable source instances', exact: true }).click()
        await page.getByRole('treeitem', { name: 'Editable source instances Lock Hide', exact: true }).click()
        await page.keyboard.press('ArrowRight')
        const layer = page.getByRole('treeitem', { name: 'Wrapping paragraph Lock Hide', exact: true })
        await layer.click()
        const control = page.getByRole('textbox', { name: 'content', exact: true })
        await expect(control).toHaveValue(previousValue)
        await control.fill(value); await control.press('Tab'); await layer.click()
        assert.deepEqual(errors, [], 'canvas painting and property edits complete without runtime errors')
        await page.keyboard.press('Control+z'); await expect(control).toHaveValue(previousValue)
        await page.keyboard.press('Control+Shift+z'); await expect(control).toHaveValue(value)
        buffer = await saveDocument(page, errors, workers)
        const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' }), placed = named(reopened, 'Wrapping paragraph')
        assert.equal(placed.width, 41.328125)
        assert.equal(placed.height, height)
        assert.equal(reopened.getChildren(placed.id)[0].text, value)
        assert.deepEqual(geometry(reopened, chain(reopened, placed, 'componentId').at(-1)), untouched)
        assert.deepEqual(errors, [])
        previousValue = value
      } finally { await context.close() }
    }
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})

test('native variant choices retain empty, reserved-looking and exact values through keyboard history and worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), pageNode = graph.getPages()[0], values = ['', 'MIXED', ' padded,a ']
  const owner = graph.createNode('COMPONENT_SET', pageNode.id, { name: 'Choice definitions',
    componentPropertyDefinitions: [{ id: '33:1', name: 'Choice', type: 'VARIANT', defaultValue: '', variantOptions: values },
      { id: '33:2', name: 'Shared label', type: 'TEXT', defaultValue: 'Label' }] })
  const variants = values.map((value, index) => {
    const variant = graph.createNode('COMPONENT', owner.id, { name: `Choice state ${index}`, x: index * 100,
      width: 80, height: 24, componentPropertyValues: { Choice: value }, variantPropSpecs: [{ propDefId: '33:1', value }] })
    graph.createNode('RECTANGLE', variant.id, { name: 'Shared geometry', width: 80, height: 24 })
    const content = graph.createNode('FRAME', variant.id, { name: `Layout ${index}`, width: 80, height: 24 })
    graph.createNode('TEXT', content.id, { name: `Label ${index}`, text: 'Label', width: 50, height: 20,
      componentPropertyReferences: [{ propertyId: '33:2', field: 'TEXT' }] })
    return variant
  })
  graph.createInstance(variants[0].id, pageNode.id, { name: 'Edited choice', x: 400, y: 100 })
  graph.createInstance(variants[1].id, pageNode.id, { name: 'Untouched choice', x: 500, y: 100 })
  graph.addPage('Unopened choices')
  let buffer = Buffer.from(await exportFigFile(graph)), expected = ''
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Choice definitions', 'Untouched choice'].map(name => [name, geometry(baseline, named(baseline, name))])
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (const next of [' padded,a ', 'MIXED', '']) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, 'native-choices.fig')
        await page.getByRole('treeitem', { name: 'Edited choice Lock Hide', exact: true }).click()
        const choice = page.getByRole('combobox', { name: 'Choice', exact: true })
        const label = page.getByRole('textbox', { name: 'Shared label', exact: true })
        await expect(label).toHaveValue(next === ' padded,a ' ? 'Label' : 'My label')
        await expect(choice).toBeVisible()
        await expect(choice).toHaveText(expected.trim() || 'None')
        if (next === ' padded,a ') {
          await page.getByRole('treeitem', { name: 'Untouched choice Lock Hide', exact: true }).click({ modifiers: ['Control'] })
          await expect(choice).toHaveText('Mixed')
          await choice.press('Enter')
          await expect(page.getByRole('option')).toHaveCount(3)
          await expect(page.getByRole('option', { name: 'MIXED', exact: true })).toBeVisible()
          await expect(page.getByRole('option', { name: 'None', exact: true })).toBeVisible()
          await page.keyboard.press('Escape')
          await expect(choice).toBeFocused()
          await page.getByRole('treeitem', { name: 'Edited choice Lock Hide', exact: true }).click()
          await expect(choice).toHaveText('None')
          await label.fill('My label')
          await label.press('Tab')
        }
        for (let step = 0; step < 200 && !await choice.evaluate(node => node === document.activeElement); step++) await page.keyboard.press('Tab')
        assert.equal(await choice.evaluate(node => node === document.activeElement), true,
          JSON.stringify(await page.evaluate(() => ({ tag: document.activeElement?.tagName, label: document.activeElement?.getAttribute('aria-label') }))))
        assert.equal(await choice.evaluate(node => node.matches(':focus-visible')), true)
        await page.keyboard.press('Enter')
        await expect(page.getByRole('option')).toHaveCount(3, { timeout: 3000 })
        assert.deepEqual(errors, [])
        await page.keyboard.press('Home')
        await expect(page.getByRole('option').nth(0)).toBeFocused()
        for (let index = 0; index < values.indexOf(next); index++) {
          await page.keyboard.press('ArrowDown')
          await expect(page.getByRole('option').nth(index + 1)).toBeFocused()
        }
        await page.keyboard.press('Enter')
        await expect(choice).toHaveText(next.trim() || 'None')
        await expect(label).toHaveValue('My label')
        await expect(choice).toBeFocused()
        await page.keyboard.press('Control+z')
        await expect(choice).toHaveText(expected.trim() || 'None')
        await expect(label).toHaveValue('My label')
        await page.keyboard.press('Control+Shift+z')
        await expect(choice).toHaveText(next.trim() || 'None')
        buffer = await saveDocument(page, errors, workers)
        assert.deepEqual(errors, [])
        assert.ok(workers.some(path => /export-worker/.test(path)))
        const saved = await parseFigFile(figBuffer(buffer), { populate: 'all' })
        const edited = named(saved, 'Edited choice'), master = saved.getNode(edited.componentId)
        assert.equal(master.componentPropertyValues.Choice, next)
        assert.deepEqual(master.variantPropSpecs, [{ propDefId: '33:1', value: next }])
        assert.equal(edited.componentPropertyAssignments['33:2'], 'My label')
        assert.equal(saved.getChildren(saved.getChildren(edited.id).find(node => node.type === 'FRAME').id)[0].text, 'My label')
        assert.deepEqual([edited.x, edited.y, edited.width, edited.height], [400, 100, 80, 24])
        for (const [name, before] of untouched) assert.deepEqual(geometry(saved, named(saved, name)), before)
        expected = next
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('native grid track edits retain layout and local ownership through two browser worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), pageNode = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', pageNode.id, { name: 'Grid master', width: 312, height: 60,
    layoutMode: 'GRID', gridTemplateColumns: [{ sizing: 'FR', value: 1, minValue: 0 }, { sizing: 'FR', value: 1, minValue: 0 }],
    gridTemplateRows: [{ sizing: 'FIXED', value: 60 }], gridColumnGap: 12 })
  for (let column = 1; column <= 2; column++) graph.createNode('RECTANGLE', master.id, {
    name: `Cell ${column}`, width: 20, height: 20, layoutAlignSelf: 'STRETCH',
    gridPosition: { row: 1, column, rowSpan: 1, columnSpan: 1 },
  })
  computeLayout(graph, master.id)
  graph.createInstance(master.id, pageNode.id, { name: 'Edited grid', x: 20, y: 120 })
  graph.createInstance(master.id, pageNode.id, { name: 'Untouched grid', x: 500, y: 120 })
  graph.addPage('Unopened grid page')
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Grid master', 'Untouched grid'].map(name => [name, geometry(baseline, named(baseline, name))])
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `native-grid-${cycle}.fig`)
        const layer = page.getByRole('treeitem', { name: 'Edited grid Lock Hide', exact: true })
        await layer.click()
        const column = page.getByRole('spinbutton', { name: 'C1', exact: true })
        await expect(column).toHaveAttribute('aria-valuenow', String(cycle + 1))
        if (cycle === 2) { assert.deepEqual(errors, []); continue }
        for (let step = 0; step < 60 && !await column.evaluate(node => node === document.activeElement); step++) {
          await page.keyboard.press('Tab')
        }
        assert.ok(await column.evaluate(node => node === document.activeElement && node.matches(':focus-visible')),
          'Tab reaches the named column control with keyboard focus')
        await page.keyboard.press('ArrowUp')
        await expect(column).toHaveAttribute('aria-valuenow', String(cycle + 2))
        await layer.click()
        await page.keyboard.press('Control+z')
        await expect(column).toHaveAttribute('aria-valuenow', String(cycle + 1))
        await page.keyboard.press('Control+Shift+z')
        await expect(column).toHaveAttribute('aria-valuenow', String(cycle + 2))
        buffer = await saveDocument(page, errors, workers)
        const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' }), edited = named(reopened, 'Edited grid')
        assert.equal(edited.layoutMode, 'GRID')
        assert.deepEqual(edited.gridTemplateColumns, [{ sizing: 'FR', value: cycle + 2, minValue: 0 }, { sizing: 'FR', value: 1, minValue: 0 }])
        assert.deepEqual([edited.x, edited.y, edited.width, edited.height], [20, 120, 312, 60])
        const left = cycle === 0 ? 200 : 225
        assert.deepEqual(reopened.getChildren(edited.id).map(child => [child.x, child.y, child.width, child.height]),
          [[0, 0, left, 60], [left + 12, 0, 300 - left, 60]])
        reopened.syncInstances(named(reopened, 'Grid master').id)
        assert.equal(edited.gridTemplateColumns[0].value, cycle + 2)
        for (const [name, expected] of untouched) assert.deepEqual(geometry(reopened, named(reopened, name)), expected, name)
        assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('derived native colors follow keyboard palette edits and survive two browser worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), collection = graph.createCollection('Palette'), pageNode = graph.getPages()[0]
  graph.updateNode(pageNode.id, { name: 'Variable proof' })
  const ink = graph.createVariable('Ink', 'COLOR', collection.id, { r: 0, g: 0, b: 0, a: 1 })
  const derived = graph.createVariable('Secondary', 'COLOR', collection.id, { cssColor: {
    value: 'color-mix(in srgb, var(--ink) 75%, #fff)', customProperties: { '--ink': { aliasId: ink.id } },
  } })
  const outside = graph.createCollection('Outside')
  graph.createVariable('Outside dependency', 'COLOR', outside.id, { cssColor: {
    value: 'var(--secondary)', customProperties: { '--secondary': { aliasId: derived.id } },
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
        const removeInk = dialog.getByRole('button', { name: 'Delete Ink', exact: true })
        await tabTo(removeInk)
        for (const forcedColors of ['none', 'active']) {
          await page.emulateMedia({ forcedColors })
          const indicator = await removeInk.evaluate(node => {
            const css = getComputedStyle(node), box = node.getBoundingClientRect()
            return { outline: css.outlineStyle, width: parseFloat(css.outlineWidth), opacity: css.opacity,
              targetWidth: box.width, targetHeight: box.height }
          })
          assert.equal(indicator.outline, 'auto')
          assert.ok(indicator.width > 0)
          assert.equal(indicator.opacity, '1')
          assert.ok(indicator.targetWidth >= 24 && indicator.targetHeight >= 24)
        }
        await page.emulateMedia({ forcedColors: 'none' })
        await expect(dialog.getByRole('status')).toHaveText('')
        await page.keyboard.press('Space')
        await expect(dialog.getByRole('status')).toContainText('Variables unchanged.')
        await expect(removeInk).toBeFocused()
        await expect(secondary).toContainText(cycle === 0 ? 'Derived #404040' : 'Derived #414040')
        await tabTo(dialog.getByRole('button', { name: 'Dismiss variable message', exact: true }))
        await page.keyboard.press('Enter')
        await expect(dialog.getByRole('status')).toHaveText('')
        await expect(dialog.getByRole('button', { name: 'Collection actions', exact: true })).toBeFocused()
        await page.keyboard.press('Space')
        await page.keyboard.press('End')
        await expect(page.getByRole('menuitem', { name: 'Delete collection', exact: true })).toBeFocused()
        await page.keyboard.press('Enter')
        await expect(dialog.getByRole('status')).toContainText('Variables unchanged.')
        await expect(dialog.getByRole('tab', { name: 'Palette', exact: true })).toHaveAttribute('aria-selected', 'true')
        await expect(secondary).toContainText(cycle === 0 ? 'Derived #404040' : 'Derived #414040')
        await tabTo(dialog.getByRole('button', { name: 'Dismiss variable message', exact: true }))
        await page.keyboard.press('Enter')
        await expect(dialog.getByRole('status')).toHaveText('')
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
        assert.deepEqual([...reopened.variableCollections.values()].map(item => item.name).sort(), ['Outside', 'Palette'])
        assert.equal(reopened.variables.size, 3)
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

for (const square of [false, true])
test(`source absolute placements follow editor resizing, history and two worker saves: square=${square}`, { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), pageNode = graph.getPages()[0]
  const provenance = record => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
    schema: 'platformkit.design-export.v1', scope: 'source-composition-layout', ...record,
  }) }]
  const badge = graph.createNode('COMPONENT', pageNode.id, { name: 'Reusable positioned content', width: 80, height: 32 })
  const master = graph.createNode('COMPONENT', pageNode.id, { name: 'Positioning master', width: 320, height: 160,
    layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FIXED', counterAxisSizing: square ? 'HUG' : 'FIXED',
    clipsContent: square, cornerRadius: 12,
    pluginData: provenance(square ? { cssBox: { version: 1, aspectRatio: 1, overflow: [2, 5, 4, 3] } } : {}) })
  for (const [horizontal, vertical] of [['left', 'top'], ['right', 'top'], ['left', 'bottom'], ['right', 'bottom']]) {
    const wrapper = graph.createNode('FRAME', master.id, { name: `${horizontal} ${vertical}`, width: 80, height: 32,
      layoutMode: 'VERTICAL', primaryAxisSizing: 'FIXED', counterAxisSizing: 'FIXED', layoutPositioning: 'ABSOLUTE',
      horizontalConstraint: horizontal === 'left' ? 'MIN' : 'MAX', verticalConstraint: vertical === 'top' ? 'MIN' : 'MAX',
      pluginData: provenance({ cssPosition: { version: 1, horizontal: { edge: horizontal, inset: 12.25 },
        vertical: { edge: vertical, inset: 2.5 } } }) })
    graph.createInstance(badge.id, wrapper.id)
  }
  computeLayout(graph, master.id)
  graph.createInstance(master.id, pageNode.id, { name: 'Edited positioning', x: 20, y: 220 })
  graph.createInstance(master.id, pageNode.id, { name: 'Untouched positioning', x: 500, y: 220 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Reusable positioned content', 'Positioning master', 'Untouched positioning'].map(name => [name, geometry(baseline, named(baseline, name))])
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `positioning-${cycle}.fig`)
        const selected = page.getByRole('treeitem', { name: 'Edited positioning Lock Hide', exact: true })
        await selected.click()
        const width = page.getByRole('spinbutton', { name: 'Width', exact: true }), previous = cycle === 1 ? 321 : 320
        await expect(width).toHaveAttribute('aria-valuenow', String(previous))
        if (cycle < 2) {
          await width.focus(); await width.press(cycle === 0 ? 'ArrowUp' : 'ArrowDown')
          await selected.click(); await page.keyboard.press('Control+z')
          await expect(width).toHaveAttribute('aria-valuenow', String(previous))
          await page.keyboard.press('Control+Shift+z')
          const next = cycle === 0 ? 321 : 320
          await expect(width).toHaveAttribute('aria-valuenow', String(next))
          buffer = await saveDocument(page, errors, workers)
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' }), edited = named(reopened, 'Edited positioning')
          assert.deepEqual([edited.x, edited.y, edited.width, edited.height], [20, 220, next, square ? next : 160])
          assert.equal(edited.clipsContent, square)
          for (const wrapper of reopened.getChildren(edited.id)) {
            const [horizontal, vertical] = wrapper.name.split(' ')
            assert.deepEqual([wrapper.x, wrapper.y, wrapper.width, wrapper.height],
              [horizontal === 'left' ? 12.25 : next - 92.25, vertical === 'top' ? 2.5 : edited.height - 34.5, 80, 32])
            assert.equal(chain(reopened, reopened.getChildren(wrapper.id)[0], 'componentId').at(-1).name, 'Reusable positioned content')
          }
          for (const [name, expected] of untouched) assert.deepEqual(geometry(reopened, named(reopened, name)), expected, name)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
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
    // A binding changes the rendered role, not the authored fallback paint.
    const paints = structuredClone({ fills: vector.fills, strokes: vector.strokes })
    for (const field of Object.keys(vector.boundVariables)) {
      baseline.bindVariable(vector.id, field, importedRole.id)
    }
    assert.deepEqual({ fills: vector.fills, strokes: vector.strokes }, paints)
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

// OTF encoding adds timestamps. Parameterized cases must reuse identical bytes
// for the same face, as required by the process-wide native font identity guard.
const editorFontFixtures = [400, 500, 600].map(weight => {
  const woff = readFileSync(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
  return { weight, bytes: new Uint8Array(OpenType.parse(figBuffer(woff)).toArrayBuffer()) }
})

for (const [field, choices, editFamilyCopy = true, dashed = false, centered = false] of [
  ['tone', ['neutral', 'info', 'danger']], ['size', ['md', 'sm', 'lg']], ['size', ['md', 'xs', '2xl'], false, false, true],
  ['tone', ['neutral', 'info', 'danger'], true, true],
])
test(`Core and schema-generated forms inherit native ${field} properties through local fonts, history and two worker saves: editFamilyCopy=${editFamilyCopy}, dashed=${dashed}, centered=${centered}`, { timeout: 120000 }, async t => {
  await verifyBuild()
  const exportSource = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/kit/crud"
  "github.com/septagon-oss/platformkit/kit/httpx"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
  "github.com/septagon-oss/platformkit/ui/css"
  "github.com/septagon-oss/platformkit/ui/screens"
)
type Note struct {
  crud.Base
  Title string \`json:"title" validate:"required"\`
  Description string \`json:"description" ui:"widget:textarea"\`
}
func (Note) TableName() string { return "notes" }
func choiceForm(p components.FormProps, children ...g.Node) g.Node {
  summary := components.ExampleOf(components.ExampleInfo{ID: "source-summary", ComponentID: "pk-ui.component.text"},
    components.TextProps{Content: "Source-owned album states", Color: "muted"}, components.Text)
  return components.Form(p, append([]g.Node{summary.Node}, children...)...)
}
func main() {
  var input struct { Proposal *ui.PropsProposal; Dashed, Centered bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  resource := httpx.Resource{Module: "notes", Entity: "note", Path: "/api/v1/notes", Schema: crud.Schema{Fields: crud.Fields[*Note]()}}
  form := screens.FormExample("fixture/generated-form", resource, screens.Options{Root: "/admin"}, "/admin/notes", "New note", nil, nil, "", true)
  examples := append(components.Gallery(), form)
  state := components.ExampleOf(components.ExampleInfo{ID: "stage", ComponentID: "pk-ui.component.select"},
    components.SelectProps{Name: "stage", Label: "Stage", Value: "draft", Required: true, Placeholder: "Choose a stage",
      Options: []components.SelectOption{{Value: "draft", Label: "Draft"}, {Value: " ready,a ", Label: "Ready"}}}, components.Select)
  choices := components.ExampleWithChildren(components.ExampleInfo{ID: "fixture/choice-form", ComponentID: "pk-ui.component.form"},
    components.FormProps{Label: "Album state", Action: "/albums"}, []g.Node{state.Node}, choiceForm)
  examples = append(examples, choices)
  var extra ui.Extra
  if input.Centered {
    extra.Sheets = append(extra.Sheets, css.NewSheet().Select("[data-component=text]", css.Decl("text-align", css.Literal("center"))))
  }
  if input.Dashed {
    extra.Sheets = append(extra.Sheets, css.NewSheet().Select("[data-component=button]",
      css.Decl("border", css.Literal("1px dashed var(--pk-color-border-default)")),
      css.Decl("border-radius", css.Literal("12px"))))
  }
  snapshot, err := ui.Export(design.Default(), examples, extra)
  if input.Proposal != nil { _, snapshot, err = ui.ProjectProps(design.Default(), examples, *input.Proposal, extra) }
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const project = input => exportSource({ ...input, dashed, centered })
  const source = project({})
  const form = 'pk-ui.component.form/default', button = 'pk-ui.component.button/with-leading-icon'
  const paragraph = 'pk-ui.component.text/muted', secondary = 'pk-ui.component.button/secondary'
  const description = 'pk-ui.component.textarea/invalid'
  const family = 'pk-ui.component.button/primary'
  const variants = choices.slice(1).map(value => ({ exampleId: family, property: field,
    snapshot: project({ proposal: { baseSHA256: source.sha256, path: [family], props: { [field]: value } } }) }))
  const nestedPath = [form, 'actions', 'create'], nestedChoices = ['', ...choices.slice(1)]
  for (const value of nestedChoices.slice(1)) variants.push({ exampleId: form, path: nestedPath, property: field,
    snapshot: project({ proposal: { baseSHA256: source.sha256, path: nestedPath, props: { [field]: value } } }) })
  const choiceForm = 'fixture/choice-form', choicePath = [choiceForm, 'stage'], selectValues = ['draft', ' ready,a ', '']
  for (const value of selectValues.slice(1)) variants.push({ exampleId: choiceForm, path: choicePath, property: 'value',
    snapshot: project({ proposal: { baseSHA256: source.sha256, path: choicePath, props: { value } } }) })
  const generated = 'fixture/generated-form', examples = [form, button, paragraph, secondary, description, generated, family, choiceForm]
  const temporary = await mkdtemp(join(tmpdir(), 'platformkit-editor-fonts-'))
  let browser, comparisonBrowser, renderer
  try {
    // Chromium Local Font Access cannot read WOFF blobs. Re-encode fixtures as
    // static OTF without changing their legacy names; use these exact bytes at
    // both boundaries, not a claim of original WOFF/hinting equivalence.
    const fonts = []
    for (const { weight, bytes } of editorFontFixtures) {
      await writeFile(join(temporary, `${weight}.otf`), bytes, { flag: 'wx' })
      fonts.push({ family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: hash(bytes) })
    }
    const config = join(temporary, 'fonts.conf')
    await writeFile(config, `<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "fonts.dtd"><fontconfig><dir>${temporary}</dir><cachedir>${temporary}/cache</cachedir></fontconfig>`, { flag: 'wx' })
    comparisonBrowser = await chromium.launch({ headless: true, args: browserArgs, env: { ...process.env, FONTCONFIG_FILE: config } })
    const ck = await initCanvasKit()
    renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
    const built = await buildComponentDocument(source, { examples, fonts, variants,
      browser: comparisonBrowser, renderer, viewport: { width: 320, height: 900 } })
    // Generation and reprojection share one declared measurement environment.
    // Full Chromium supplies editor Local Font Access and worker interaction,
    // not a substitute browser profile for source text-width comparisons.
    browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs, env: { ...process.env, FONTCONFIG_FILE: config } })
    const { graph, placements, selections } = built
    const derivedSource = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', paragraph, '--props'], {
      cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', input: JSON.stringify({ color: 'secondary' }),
    }))
    const observedRole = await captureExample(comparisonBrowser, derivedSource, paragraph, { fonts, viewport: { width: 320, height: 900 } })
    const derived = await materializeComponent(graph, built.definitions.id, derivedSource, observedRole, fonts, renderer, built.collection.id)
    graph.updateNode(derived.master.id, { name: 'Derived paragraph master', x: 800 })
    graph.createInstance(derived.master.id, placements.id, { name: 'Derived paragraph', x: 800, y: 48 })
    graph.updateNode(selections.find(item => item.exampleId === family).instance.id, { name: 'Editable family', x: 800, y: 400 })
    graph.updateNode(sourceNode(graph, choicePath).id, { name: 'Editable choice' })
    const choiceSizing = node => [node.primaryAxisSizing, node.counterAxisSizing]
    const sourceChoiceSizing = choiceSizing(sourceNode(graph, choicePath))
    assert.ok(sourceChoiceSizing.includes('FILL'), 'the source form owns its nested choice width')
    graph.updateNode(sourceNode(graph, nestedPath).id, { name: 'Editable nested family' })
    graph.updateNode(sourceNode(graph, nestedPath.slice(0, -1)).id, { name: 'Editable Form actions' })
    const roleName = '--pk-role-fg-secondary', inputNameForRole = '--pk-color-text-primary'
    graph.updateNode(selections[1].instance.id, { name: 'Editable button' })
    graph.updateNode(selections[2].instance.id, { name: 'Editable paragraph' })
    graph.updateNode(selections[3].instance.id, { name: 'Editable bordered button' })
    graph.updateNode(selections[4].instance.id, { name: 'Editable description' })
    graph.createInstance(selections[0].master.id, placements.id, { name: 'Untouched Form', x: 500, y: 48 })
    let buffer = Buffer.from(await exportFigFile(graph))
    const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
    assert.equal(sourceNode(baseline, nestedPath).name, 'Editable nested family')
    assert.equal(sourceNode(baseline, nestedPath.slice(0, -1)).name, 'Editable Form actions')
    const inputName = sourceNode(baseline, [form, 'title']).name
    assert.equal(inputName, 'title', 'source-owned nested instances retain meaningful layer names after import')
    const generatedInputName = sourceNode(baseline, [generated, 'field/description']).name
    assert.equal(generatedInputName, 'Description', 'generated fields inherit their Core name, independently of their field path')
    const definitionKey = node => {
      const origin = JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
      return JSON.stringify(origin?.definitionPath ? [node.type, origin.sha256, origin.definitionPath] : [node.type, node.name])
    }
    const untouched = [...baseline.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Untouched Form')
      .map(node => [definitionKey(node), geometry(baseline, node)])
    const values = ['WAVY affinity album title '.repeat(6), ''], labels = ['Create affinity album', 'Return to album']
    const fieldLabels = ['Album title', 'Name']
    const descriptions = ['\nFirst line\nSecond line\n', 'A short description.']
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
        for (const name of ['Editable source instances', form, generated, 'Editable Form actions', choiceForm]) {
          await page.getByRole('treeitem', { name: `${name} Lock Hide`, exact: true }).click({ timeout: 5000 }).catch(async cause => {
            throw new Error(JSON.stringify({ cycle, name, tree: await page.getByRole('treeitem').allTextContents() }), { cause })
          })
          await page.keyboard.press('ArrowRight')
        }
        for (const [name, field, value, initial, previousValue] of [
          [inputName, 'value', values[cycle], '', values[cycle - 1]],
          [inputName, 'label', fieldLabels[cycle], 'Title', fieldLabels[cycle - 1]],
          ['Editable button', 'label', labels[cycle], 'Add item', labels[cycle - 1]],
          ['Editable family', 'label', labels[cycle], 'Save', labels[cycle - 1]],
          ['Editable nested family', 'label', labels[cycle], 'Create', labels[cycle - 1]],
          ['Editable choice', 'label', labels[cycle], 'Stage', labels[cycle - 1]],
          ['Editable paragraph', 'content', contents[cycle], 'Plain body copy.', contents[cycle - 1]],
          ['Editable bordered button', 'label', labels[cycle], 'Cancel', labels[cycle - 1]],
          ['Editable description', 'value', descriptions[cycle], '', descriptions[cycle - 1]],
          [generatedInputName, 'value', descriptions[cycle], '', descriptions[cycle - 1]],
        ]) {
          await page.getByRole('treeitem', { name: `${name} Lock Hide`, exact: true }).click()
          const control = page.getByRole('textbox', { name: field, exact: true })
          if (!editFamilyCopy && ['Editable family', 'Editable nested family'].includes(name)) {
            await expect(control).toHaveValue(name === 'Editable family' ? 'Save' : 'Create')
            continue
          }
          const previous = cycle ? previousValue : initial
          await expect(control).toHaveValue(previous)
          if (cycle === 2) continue
          await control.fill(value)
          const multiline = ['Editable description', generatedInputName].includes(name)
          if (multiline && cycle === 0) {
            await control.fill('\nFirst line\nSecond line')
            await control.press('Control+End'); await control.press('Enter')
            assert.equal(await control.evaluate(node => node.tagName), 'TEXTAREA')
            assert.ok(await control.evaluate(node => getComputedStyle(node).outlineStyle !== 'none'))
          }
          await control.press(multiline && cycle === 1 ? 'Control+Enter' : 'Tab')
          await page.getByRole('treeitem', { name: `${name} Lock Hide`, exact: true }).click()
          assert.deepEqual(errors, [], 'property commit must not fail behind the displayed input value')
          await page.keyboard.press('Control+z')
          await expect(control).toHaveValue(previous)
          await page.keyboard.press('Control+Shift+z')
          await expect(control).toHaveValue(value)
        }
        for (const [name, values, property = field] of [['Editable family', choices], ['Editable nested family', nestedChoices],
          ['Editable choice', selectValues, 'value']]) {
          await page.getByRole('treeitem', { name: `${name} Lock Hide`, exact: true }).click()
          const propertyControl = page.getByRole('combobox', { name: property, exact: true })
          const previousChoice = values[Math.min(cycle, 2)] || 'None'
          await expect(propertyControl).toHaveText(previousChoice)
          if (cycle < 2) {
            await propertyControl.focus()
            await page.keyboard.press('Enter')
            await expect(page.getByRole('option')).toHaveCount(values.length)
            await page.keyboard.press('Home')
            await expect(page.getByRole('option').nth(0)).toBeFocused()
            for (let index = 0; index <= cycle; index++) {
              await page.keyboard.press('ArrowDown')
              await expect(page.getByRole('option').nth(index + 1)).toBeFocused()
            }
            await page.keyboard.press('Enter')
            await expect(propertyControl).toHaveText(values[cycle + 1] || 'None')
            await page.keyboard.press('Control+z')
            await expect(propertyControl).toHaveText(previousChoice)
            await page.keyboard.press('Control+Shift+z')
            await expect(propertyControl).toHaveText(values[cycle + 1] || 'None')
            const label = !editFamilyCopy && name !== 'Editable choice' ?
              name === 'Editable family' ? 'Save' : 'Create' : labels[cycle]
            await expect(page.getByRole('textbox', { name: 'label', exact: true })).toHaveValue(label)
          }
        }
        const variables = page.getByRole('dialog', { name: 'Local variables', exact: true })
        const openVariables = page.getByRole('button', { name: 'Open variables', exact: true })
        // Escape exits the entered composition before it clears its selection.
        for (let depth = 0; depth < 4 && !await openVariables.count(); depth++) await page.keyboard.press('Escape')
        const tabTo = async target => {
          for (let step = 0; step < 200 && !await target.evaluate(node => node === document.activeElement); step++) {
            await page.keyboard.press('Tab')
          }
          await expect(target).toBeFocused()
          assert.ok(await target.evaluate(node => node.matches(':focus-visible')))
        }
        await tabTo(openVariables)
        await page.keyboard.press('Enter')
        const roleRow = variables.getByRole('row').filter({ hasText: roleName })
        const previousRole = await roleRow.innerText()
        assert.match(previousRole, /Derived #/)
        if (cycle === 0) {
          await tabTo(variables.getByRole('row').filter({ hasText: inputNameForRole })
            .getByRole('button', { name: 'Edit color', exact: true }).first())
          await page.keyboard.press('Enter')
          const red = page.getByRole('spinbutton', { name: 'Red', exact: true })
          await tabTo(red)
          await page.keyboard.press('Control+a')
          await page.keyboard.type('200')
          await page.keyboard.press('Tab')
          await page.keyboard.press('Escape')
          const nextRole = await roleRow.innerText()
          assert.notEqual(nextRole, previousRole)
          await page.keyboard.press('Escape')
          await page.keyboard.press('Control+z')
          await tabTo(openVariables)
          await page.keyboard.press('Enter')
          assert.equal(await roleRow.innerText(), previousRole)
          await page.keyboard.press('Escape')
          await page.keyboard.press('Control+Shift+z')
        } else await page.keyboard.press('Escape')
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
          const role = [...reopened.variables.values()].find(variable => variable.name === roleName)
          const mode = reopened.variableCollections.get(role.collectionId).modes.find(mode => mode.name === 'light').modeId
          const paper = [...reopened.variables.values()].find(variable => variable.name === '--pk-color-surface-primary')
          const instance = named(reopened, 'Derived paragraph'), text = reopened.getChildren(instance.id).find(node => node.type === 'TEXT')
          assert.equal(text.boundVariables['fills/0/color'], role.id)
          assert.equal(instance.componentId, named(reopened, 'Derived paragraph master').id)
          assert.equal(role.valuesByMode[mode].cssColor.value, observedRole.roots[0].paintSources.color.expressionCandidate.value)
          assert.ok(Math.abs(reopened.resolveVariable(role.id, mode).r -
            (.78 * Math.fround(200 / 255) + .22 * reopened.resolveVariable(paper.id, mode).r)) < 1e-6)
          for (const id of examples) {
            const before = sourceNode(baseline, [id]), after = sourceNode(reopened, [id])
            const oldParent = baseline.getNode(before.parentId), newParent = reopened.getNode(after.parentId)
            assert.deepEqual([after.x, after.y, newParent.x, newParent.y], [before.x, before.y, oldParent.x, oldParent.y],
              'layer-tree navigation must not move source placements or their board')
          }
          for (const [key, expected] of untouched) {
            const matches = [...reopened.getAllNodes()].filter(node => definitionKey(node) === key)
            assert.equal(matches.length, 1, `one exact definition or preview: ${key}`)
            assert.deepEqual(geometry(reopened, matches[0]), expected, key)
          }
          for (const sibling of ['field/title', 'actions']) {
            const path = [generated, sibling]
            assert.deepEqual(geometry(reopened, sourceNode(reopened, path)), geometry(baseline, sourceNode(baseline, path)),
              `editing a generated field preserves its sibling ${sibling}`)
          }
          for (const [path, props] of [
            [[form, 'title'], { ...(cycle === 0 ? { value: values[cycle] } : {}), label: fieldLabels[cycle] }],
            [[button], { label: labels[cycle] }],
            [[family], { [field]: choices[cycle + 1], ...(editFamilyCopy ? { label: labels[cycle] } : {}) }],
            [nestedPath, { [field]: nestedChoices[cycle + 1], ...(editFamilyCopy ? { label: labels[cycle] } : {}) }],
            [choicePath, { value: selectValues[cycle + 1], label: labels[cycle] }],
            [[paragraph], { content: contents[cycle] }],
            [[secondary], { label: labels[cycle] }],
            [[description], { value: descriptions[cycle] }],
            [[generated, 'field/description'], { value: descriptions[cycle] }],
          ]) {
            const result = extractSourceProps(reopened, sourceNode(reopened, path), source)
            assert.deepEqual(result.proposal, { baseSHA256: source.sha256, path, props })
            const projected = project({ proposal: result.proposal })
            assert.notEqual(projected.sha256, source.sha256)
            if (path[0] === paragraph) {
              const text = reopened.getChildren(sourceNode(reopened, path).id).find(node => node.type === 'TEXT')
              assert.equal(text.textAlignHorizontal, centered ? 'CENTER' : 'LEFT')
            }
            if (path[0] === choiceForm) {
              const summary = sourceNode(reopened, [choiceForm, 'source-summary'])
              assert.equal(summary.type, 'INSTANCE', 'source-owned internal composition remains linked after browser saves')
              assert.equal(reopened.getChildren(summary.id)[0].text, 'Source-owned album states')
              assert.deepEqual(extractSourceProps(reopened, summary, source).properties, [])
              const selected = sourceNode(reopened, choicePath), pending = [selected], descendants = []
              assert.deepEqual(choiceSizing(selected), sourceChoiceSizing, 'variant edits and worker saves retain parent-owned fill sizing')
              while (pending.length) {
                const node = pending.pop(); descendants.push(node); pending.push(...reopened.getChildren(node.id))
              }
              assert.ok(descendants.some(node => node.type === 'TEXT' && node.text === (cycle ? 'Choose a stage' : 'Ready')))
              assert.equal(descendants.filter(node => node.type === 'INSTANCE' &&
                chain(reopened, node, 'componentId').at(-1).pluginData.some(item => item.key === 'platformkit.icon')).length, 1)
            }
            if ([description, generated].includes(path[0])) {
              const observed = await captureExample(comparisonBrowser, projected, path[0], { fonts, viewport: { width: 320, height: 900 } })
              const field = path[0] === generated ? observed.roots[0].children[1] : observed.roots[0]
              const expected = field.children.find(node => node.tag === 'textarea')
              const area = reopened.getChildren(sourceNode(reopened, path).id).find(node => node.name === 'Source textarea')
              const viewport = reopened.getChildren(area.id)[0], value = reopened.getChildren(viewport.id)[0]
              assert.equal(value.text, descriptions[cycle])
              assert.equal(viewport.clipsContent, true)
              assert.equal(area.height, expected.bounds.height)
              assert.ok(Math.abs(value.height - expected.control.content.bounds.height) <= 1 / 64)
            }
            if (path[0] === form && path.length === 2) {
              const observed = await captureExample(comparisonBrowser, projected, form, { fonts, viewport: { width: 320, height: 900 } })
              const label = observed.roots[0].children[0].children[0]
              const nativeLabel = reopened.getChildren(sourceNode(reopened, path).id)[0]
              const regions = label.children.flatMap(child => child.kind === 'text' ? [child] : child.children)
              const runs = reopened.getChildren(nativeLabel.id)
              assert.equal(runs.length, regions.length)
              for (const [index, region] of regions.entries()) {
                assert.equal(runs[index].text, region.text)
                assert.ok(Math.abs(runs[index].x - (region.bounds.x - label.bounds.x)) <= 1 / 64,
                  'required marker follows the edited label after a browser worker save')
                assert.ok(Math.abs(runs[index].width - region.bounds.width) <= 1 / 64)
              }
            }
            if ([paragraph, secondary, family].includes(path[0]) || JSON.stringify(path) === JSON.stringify(nestedPath)) {
              const observed = await captureExample(comparisonBrowser, projected, path[0], { fonts, viewport: { width: 320, height: 900 } })
              const selected = selections.find(item => item.observation.exampleId === path[0])
              assert.deepEqual(observed.environment, selected.observation.environment, 'source comparison profile must not change after editing')
              let observedNode = observed.roots[0]
              for (const id of path.slice(1)) observedNode = observedNode.children.find(child => child.source?.path.at(-1) === id)
              const placed = sourceNode(reopened, path), expected = observedNode.bounds
              for (const field of ['width', 'height']) assert.ok(Math.abs(placed[field] - expected[field]) <= 1 / 64,
                `worker-saved ${path[0]} ${field}: ${placed[field]} versus ${expected[field]}`)
              if (path[0] === secondary) {
                assert.deepEqual([placed.strokes[0].weight, placed.strokes[0].align], [1, 'INSIDE'])
                assert.equal(reopened.variables.get(placed.boundVariables['strokes/0/color']).name, '--pk-color-border-default')
                assert.deepEqual(placed.dashPattern, dashed ? [3, 2] : [])
                assert.deepEqual(placed.strokes[0].dashPattern, dashed ? [3, 2] : [])
                if (dashed) {
                  const master = chain(reopened, placed, 'componentId').at(-1)
                  const origin = JSON.parse(master.pluginData.find(item => item.key === 'platformkit.source').value)
                  assert.deepEqual(origin.cssBorder, { version: 1, style: 'dashed', weight: 1 })
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
