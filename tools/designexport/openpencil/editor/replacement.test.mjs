import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import OpenType from 'opentype.js'
import axe from 'axe-core'
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
import { sourceFixture, exportCore, emptyStateFixture, sourceTokenFixture } from '../browser/fixtures.test.mjs'
import { decodeSnapshot } from '../source-tokens.mjs'

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
  const dependencies = await (await fetch(new URL('/licenses/bundled-dependencies.json', endpoint))).json()
  assert.deepEqual(dependencies.filter(item => item.name === '@ai-sdk/provider-utils').map(item => item.version),
    ['4.0.33'], 'the emitted dependency report contains only the patched provider-utils version')
  const notices = await (await fetch(new URL('/licenses/PlatformKit-NOTICE', endpoint))).text()
  assert.equal(notices, readFileSync(new URL('../../../../NOTICE', import.meta.url), 'utf8'), 'shipped notices match the source')
  assert.ok(notices.includes('Blink border geometry — BSD 3-Clause\n\nCopyright (C) 2013 Google Inc.'))
  const underlineNotice = await (await fetch(new URL('/licenses/Blink-underline-NOTICE', endpoint))).text()
  assert.equal(underlineNotice, readFileSync(new URL('../Blink-underline-NOTICE', import.meta.url), 'utf8'))
  for (const [file, digest] of [
    ['UnicodeTrie-LICENSE', 'e59138ecbc0b770010b0781905e2bcc181e4f0735494c58fd9681b24f0246187'],
    ['Unicode-LICENSE', 'e7a93b009565cfce55919a381437ac4db883e9da2126fa28b91d12732bc53d96'],
  ]) assert.equal(hash(new Uint8Array(await (await fetch(new URL(`/licenses/${file}`, endpoint))).arrayBuffer())), digest)
  assert.equal(provenance.scope, 'generic-editor-without-packaged-design')
  assert.deepEqual(Object.keys(provenance.adapter.inputs).sort(), [
    'Blink-underline-NOTICE', 'Dockerfile', 'LICENSE', 'NOTICE', 'border-correction.mjs', 'build-editor.mjs', 'color-expression.mjs', 'computed-color.mjs', 'corrections.mjs', 'editor-fonts.mjs', 'exporter-correction.mjs', 'font-correction.mjs', 'fonts.mjs',
    'grid-correction.mjs', 'grid-fig-correction.mjs', 'layout-correction.mjs', 'nginx.conf', 'package-lock.json', 'package.json', 'paragraph-correction.mjs', 'property-correction.mjs',
    'scaling-correction.mjs', 'source-box.mjs', 'source-flex.mjs', 'source-fragments.mjs', 'source-position-history.mjs', 'source-positioning.mjs', 'sync-correction.mjs', 'underline-correction.mjs', 'variable-binding-correction.mjs', 'variable-binding.mjs', 'variable-color.mjs', 'variable-history.mjs', 'variable-mode-control-correction.mjs', 'variable-modes.mjs', 'variable-number.mjs', 'variable-source.mjs', 'variant-correction.mjs',
  ])
  for (const [name, digest] of Object.entries(provenance.adapter.inputs)) {
    assert.match(name, /^[A-Za-z0-9._-]+$/)
    const path = ['LICENSE', 'NOTICE'].includes(name) ? `../../../../${name}` : `../${name}`
    assert.equal(createHash('sha256').update(readFileSync(new URL(path, import.meta.url))).digest('hex'), digest, name)
  }
}

test('source-produced typed tokens retain their baseline through editor edits and two worker saves', { timeout: 120000 }, async t => {
  await verifyBuild()
  const run = await sourceTokenFixture(t), input = decodeSnapshot(Buffer.from(run({})))
  const built = buildFoundation(input.snapshot, input)
  let buffer = Buffer.from(await exportFigFile(built.graph))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `typed-source-${cycle}.fig`)
        await page.getByRole('button', { name: 'Open variables', exact: true }).click()
        const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
        const value = dialog.getByRole('textbox', { name: 'spacing/1, light', exact: true })
        const dark = dialog.getByRole('textbox', { name: 'spacing/1, dark', exact: true })
        await expect(value).toHaveValue(cycle === 0 ? '0.1' : '0.1234567890123456')
        await expect(dark).toHaveValue('0.1')
        if (cycle === 0) {
          await value.fill('0.1234567890123456')
          await value.press('Tab')
          await expect(dark).toBeFocused()
          await expect(dialog.getByRole('status')).toBeEmpty()
        }
        await dialog.getByRole('button', { name: 'Collection actions', exact: true }).focus()
        await page.keyboard.press('Escape')
        await expect(dialog).toBeHidden()
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          const graph = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          const variable = name => [...graph.variables.values()].find(variable => variable.name === name)
          const spacing = variable('spacing/1'), primary = variable('--pk-color-text-primary')
          assert.deepEqual(spacing.sourceToken, { version: 1, snapshot: input.snapshot.sha256,
            kind: 'scale', scale: 'spacing', key: '1', decimal: '0.1000', unit: 'px' })
          assert.equal(graph.resolveVariable(spacing.id), 0.1234567890123456)
          assert.deepEqual(primary.sourceToken, { version: 1, snapshot: input.snapshot.sha256,
            kind: 'color', name: '--pk-color-text-primary' })
          const alias = variable('--selected-ink'), modes = graph.variableCollections.get(alias.collectionId).modes
          assert.deepEqual(Object.keys(alias.valuesByMode).sort(), modes.map(mode => mode.modeId).sort())
          assert.ok(Object.values(alias.valuesByMode).every(value => value.aliasId === primary.id))
          assert.equal(graph.resolveVariable(variable('--selected-mix').id).a, 0.25)
          const masters = named(graph, 'Icon masters')
          assert.equal(graph.getChildren(masters.id).length, input.snapshot.icons.length)
          for (const mode of ['light', 'dark']) for (const instance of graph.getChildren(named(graph, mode).id)) {
            assert.equal(instance.type, 'INSTANCE')
            assert.equal(graph.getNode(instance.componentId).parentId, masters.id)
            assert.ok(graph.getChildren(instance.id).some(vector => Object.values(vector.boundVariables).includes(primary.id)))
          }
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('typed token table keeps readable labels and values across its editing states', { timeout: 120000 }, async t => {
  await verifyBuild()
  const run = await sourceTokenFixture(t), input = decodeSnapshot(Buffer.from(run({})))
  const { graph } = buildFoundation(input.snapshot, input)
  graph.createCollection('Other tokens')
  const buffer = Buffer.from(await exportFigFile(graph))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
    try {
      const { page, errors } = await openDocument(context, buffer, 'token-contrast.fig')
      await page.getByRole('button', { name: 'Open variables', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
      await expect(dialog.getByRole('textbox', { name: 'spacing/1, light', exact: true })).toHaveValue('0.1')
      await page.addScriptTag({ content: axe.source })
      const audit = async state => {
        const result = await dialog.evaluate(element => window.axe.run(element))
        assert.deepEqual(result.violations.map(rule => ({ id: rule.id,
          nodes: rule.nodes.map(node => ({ target: node.target, reason: node.failureSummary })) })), [], state)
        assert.ok(result.passes.some(rule => rule.id === 'color-contrast'), 'contrast was actually evaluated')
        assert.deepEqual(result.incomplete, [], `${state}: no inconclusive checks`)
      }
      await audit('loaded tokens, aliases, derived colours and inactive collection label')
      const menu = dialog.getByRole('button', { name: 'Collection actions', exact: true })
      await menu.press('Enter')
      await page.getByRole('menuitem', { name: 'Rename collection', exact: true }).press('Enter')
      await expect(dialog.getByRole('textbox', { name: 'Rename collection: Foundation', exact: true })).toBeFocused()
      await audit('collection rename')
      await page.keyboard.press('Escape')
      await expect(menu).toBeFocused()
      const number = dialog.getByRole('textbox', { name: 'spacing/1, light', exact: true })
      await number.fill('12px')
      await number.press('Tab')
      await expect(dialog.getByRole('status')).toContainText('Variables unchanged. Native number:')
      await expect(number).toHaveValue('0.1')
      await audit('refused numeric edit')
      await dialog.getByRole('button', { name: 'Dismiss variable message', exact: true }).press('Enter')
      const search = dialog.getByRole('textbox', { name: 'Search…', exact: true })
      await search.fill('spacing')
      await expect(dialog.locator('[data-test-id="variable-row"]')).toHaveCount(1)
      await audit('filtered tokens')
      await search.fill('no matching token')
      await expect(dialog.locator('[data-test-id="variable-row"]')).toHaveCount(0)
      await expect(dialog.getByRole('cell', { name: 'No variables found', exact: true })).toBeVisible()
      await expect(search).toHaveAccessibleDescription('No variables found')
      await audit('empty result')
      await search.fill('')
      await dialog.getByRole('tab', { name: 'Foundation', exact: true }).press('ArrowRight')
      await expect(dialog.getByRole('tab', { name: 'Other tokens', exact: true })).toHaveAttribute('aria-selected', 'true')
      await expect(dialog.getByRole('cell', { name: 'No variables found', exact: true })).toBeVisible()
      await audit('empty collection')
      assert.deepEqual(errors, [])
      t.diagnostic('Live screen-reader behavior still requires manual review.')
    } finally { await context.close() }
  } finally { await browser.close() }
})

test('source token colour pickers retain named keyboard editing, history and two worker saves', { timeout: 120000 }, async t => {
  await verifyBuild()
  const run = await sourceTokenFixture(t), input = decodeSnapshot(Buffer.from(run({})))
  const { graph } = buildFoundation(input.snapshot, input)
  let buffer = Buffer.from(await exportFigFile(graph))
  const original = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const structure = owner => [...owner.getAllNodes()].map(node => ({
    name: node.name, type: node.type, parent: owner.getNode(node.parentId)?.name,
    component: owner.getNode(node.componentId)?.name,
    box: [node.x, node.y, node.width, node.height], vectors: node.vectorNetwork,
    bindings: Object.fromEntries(Object.entries(node.boundVariables).map(([field, id]) => [field, owner.variables.get(id)?.name])),
  }))
  const baseline = structure(original), values = { light: [21, 34, 31], dark: [238, 243, 236] }
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `source-picker-${cycle}.fig`)
        await page.getByRole('button', { name: 'Open variables', exact: true }).click()
        const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
        const mode = cycle === 1 ? 'dark' : 'light', name = `Edit color: --pk-color-text-primary, ${mode}`
        const trigger = dialog.getByRole('button', { name, exact: true })
        await expect(trigger).not.toHaveAttribute('aria-controls')
        await trigger.press('Enter')
        const picker = page.getByRole('dialog', { name, exact: true })
        await expect(picker).toBeVisible()
        await expect(trigger).toHaveAttribute('aria-controls', await picker.getAttribute('id'))
        await expect(picker.getByRole('slider', { name: 'Saturation, Brightness', exact: true })).toBeFocused()
        await page.addScriptTag({ content: axe.source })
        const tabTo = async (target, key = 'Tab') => {
          for (let step = 0; step < 30 && !await target.evaluate(node => node === document.activeElement); step++) {
            await page.keyboard.press(key)
          }
          await expect(target).toBeFocused()
          for (const forcedColors of ['none', 'active']) {
            await page.emulateMedia({ forcedColors })
            const focus = await target.evaluate(node => ({ visible: node.matches(':focus-visible'),
              outline: getComputedStyle(node).outlineStyle, width: parseFloat(getComputedStyle(node).outlineWidth) }))
            assert.ok(focus.visible && focus.outline === 'auto' && focus.width > 0, JSON.stringify({ forcedColors, focus, target: await target.ariaSnapshot() }))
          }
          await page.emulateMedia({ forcedColors: 'none' })
        }
        const format = picker.getByRole('combobox', { name: 'Color format', exact: true })
        for (const [index, [label, fields]] of [
          ['HSL', ['HSL hue', 'HSL saturation', 'HSL lightness']],
          ['HSB', ['HSB hue', 'HSB saturation', 'HSB brightness']],
          ['RGB', ['Red', 'Green', 'Blue']],
        ].entries()) {
          await tabTo(format, index ? 'Shift+Tab' : 'Tab')
          await format.press('Enter')
          await page.getByRole('option', { name: label, exact: true }).press('Enter')
          await expect(format).toBeFocused()
          for (const field of fields) await expect(picker.getByRole('spinbutton', { name: field, exact: true })).toBeVisible()
          const result = await picker.evaluate(element => window.axe.run(element))
          assert.deepEqual(result.violations.map(rule => ({ id: rule.id, nodes: rule.nodes.map(node => node.failureSummary) })), [], label)
          assert.deepEqual(result.incomplete, [], `${label}: no inconclusive checks`)
          const sliders = picker.locator('[data-slot="slider"]')
          await expect(sliders).toHaveCount(label === 'RGB' ? 2 : 4)
          const labelsFit = await sliders.evaluateAll(sliders => sliders.every(slider => {
            const range = document.createRange()
            range.selectNodeContents(slider.previousElementSibling)
            return range.getBoundingClientRect().right <= slider.getBoundingClientRect().left
          }))
          assert.ok(labelsFit, `${label}: slider labels must not overlap their tracks`)
          const controls = await picker.getByRole('slider').or(picker.getByRole('spinbutton')).all()
          // Non-modal popovers allow Tab to leave; walk backwards to earlier controls.
          for (const [index, control] of controls.entries()) await tabTo(control, index ? 'Tab' : 'Shift+Tab')
        }
        for (const [index, channel] of ['Red', 'Green', 'Blue'].entries()) {
          await expect(picker.getByRole('spinbutton', { name: channel, exact: true })).toHaveValue(String(values[mode][index]))
        }
        const red = picker.getByRole('spinbutton', { name: 'Red', exact: true })
        await tabTo(red, 'Shift+Tab')
        if (cycle < 2) {
          await red.press('ArrowUp')
          values[mode][0]++
          await expect(red).toHaveValue(String(values[mode][0]))
        }
        await red.press('Escape')
        await expect(picker).toBeHidden()
        await expect(trigger).toBeFocused()
        await expect(trigger).not.toHaveAttribute('aria-controls')
        await expect(dialog).toBeVisible()
        await trigger.press('Escape')
        if (cycle < 2) {
          await page.keyboard.press('Control+z')
          await page.getByRole('button', { name: 'Open variables', exact: true }).press('Enter')
          await trigger.press('Enter')
          await expect(red).toHaveValue(String(values[mode][0] - 1))
          await red.press('Escape')
          await trigger.press('Escape')
          await page.keyboard.press('Control+Shift+z')
          buffer = await saveDocument(page, errors, workers)
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          const variables = [...reopened.variables.values()], ink = variables.find(item => item.name === '--pk-color-text-primary')
          assert.deepEqual(ink.sourceToken, { version: 1, snapshot: input.snapshot.sha256, kind: 'color', name: ink.name })
          const alias = variables.find(item => item.name === '--selected-ink'), mix = variables.find(item => item.name === '--selected-mix')
          for (const selected of reopened.variableCollections.get(ink.collectionId).modes) {
            const [r, g, b] = values[selected.name].map(value => Math.fround(value / 255))
            assert.deepEqual(reopened.resolveVariable(ink.id, selected.modeId), { r, g, b, a: 1 })
            assert.deepEqual(alias.valuesByMode[selected.modeId], { aliasId: ink.id })
            assert.deepEqual(reopened.resolveVariable(mix.id, selected.modeId), { r, g, b, a: 0.25 })
          }
          assert.deepEqual(structure(reopened), baseline, 'linked icon masters, instances, geometry and binding identities')
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('collection rename keeps its labelled tab, keyboard focus and token identity through two worker saves', { timeout: 120000 }, async t => {
  await verifyBuild()
  const run = await sourceTokenFixture(t), input = decodeSnapshot(Buffer.from(run({})))
  const { graph } = buildFoundation(input.snapshot, input)
  graph.createCollection('Other tokens')
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Icon masters', 'light', 'dark'].map(name => geometry(baseline, named(baseline, name)))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `collection-name-${cycle}.fig`)
        if (cycle === 2) await page.emulateMedia({ forcedColors: 'active' })
        const open = page.getByRole('button', { name: 'Open variables', exact: true })
        const tabTo = async target => {
          for (let step = 0; step < 150 && !await target.evaluate(node => node === document.activeElement); step++) await page.keyboard.press('Tab')
          await expect(target).toBeFocused()
        }
        await tabTo(open)
        await page.keyboard.press('Enter')
        const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
        const menu = dialog.getByRole('button', { name: 'Collection actions', exact: true })
        await expect(dialog.getByRole('columnheader', { name: 'Actions', exact: true })).toHaveCount(1)
        const startRename = async name => {
          await tabTo(menu)
          await page.keyboard.press('Enter')
          await expect(page.getByRole('menuitem', { name: 'Rename collection', exact: true })).toBeFocused()
          await page.keyboard.press('Enter')
          const field = dialog.getByRole('textbox', { name: 'Rename collection: ' + name, exact: true })
          await expect(field, JSON.stringify(errors)).toBeFocused()
          await expect(field).toHaveValue(name)
          assert.ok(await field.evaluate(node => node.matches(':focus-visible') && getComputedStyle(node).outlineStyle !== 'none'))
          await expect(dialog.getByRole('tablist').getByRole('textbox')).toHaveCount(0)
          await expect(dialog.getByRole('tab', { name, exact: true })).toHaveAttribute('aria-selected', 'true')
          await expect(dialog.getByRole('tabpanel', { name, exact: true })).toBeVisible()
          return field
        }
        const name = cycle === 0 ? 'Foundation' : 'Design tokens'
        let field = await startRename(name)
        await field.fill('Cancelled collection')
        await page.keyboard.press('Escape')
        await expect(dialog).toBeVisible()
        await expect(field).toHaveCount(0)
        await expect(menu).toBeFocused()
        await expect(dialog.getByRole('tab', { name, exact: true })).toBeVisible()
        if (cycle === 1) {
          // Keep the existing pointer entry and ordinary blur-commit route.
          await dialog.getByRole('tab', { name, exact: true }).dblclick()
          field = dialog.getByRole('textbox', { name: 'Rename collection: ' + name, exact: true })
          await expect(field).toBeFocused()
          await field.fill('  ' + name + '  ')
          await page.keyboard.press('Tab')
          await expect(field).toHaveCount(0)
          await expect(dialog.getByRole('tabpanel', { name, exact: true })).toBeFocused()
          field = await startRename(name)
          await page.keyboard.press('Shift+Tab')
          await expect(field).toHaveCount(0)
          await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeFocused()
        }
        if (cycle === 0) {
          field = await startRename(name)
          await field.fill('Design tokens')
          await page.keyboard.press('Enter')
          await expect(menu).toBeFocused()
          await expect(dialog.getByRole('tabpanel', { name: 'Design tokens', exact: true })).toBeVisible()
          await page.keyboard.press('Escape')
          await expect(dialog).toBeHidden()
          await page.keyboard.press('Control+z')
          await tabTo(open)
          await page.keyboard.press('Enter')
          await expect(dialog.getByRole('tabpanel', { name: 'Foundation', exact: true })).toBeVisible()
          await tabTo(menu)
          await page.keyboard.press('Escape')
          await page.keyboard.press('Control+Shift+z')
          await tabTo(open)
          await page.keyboard.press('Enter')
        }
        await expect(dialog.getByRole('tab', { name: 'Other tokens', exact: true })).toBeVisible()
        await expect(dialog.getByRole('tabpanel', { name: 'Design tokens', exact: true })).toBeVisible()
        await expect(dialog.getByRole('textbox', { name: 'spacing/1, light', exact: true })).toHaveValue('0.1')
        await tabTo(menu)
        await page.keyboard.press('Escape')
        await expect(dialog).toBeHidden()
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          const token = [...reopened.variables.values()].find(variable => variable.name === 'spacing/1')
          assert.deepEqual([...reopened.variableCollections.values()].map(collection => collection.name), ['Design tokens', 'Other tokens'])
          assert.equal(reopened.resolveVariable(token.id), 0.1)
          assert.deepEqual(token.sourceToken, { version: 1, snapshot: input.snapshot.sha256,
            kind: 'scale', scale: 'spacing', key: '1', decimal: '0.1000', unit: 'px' })
          assert.deepEqual(['Icon masters', 'light', 'dark'].map(name => geometry(reopened, named(reopened, name))), untouched)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('token deletion restores linked icons, keyboard navigation and source identity through two worker saves', { timeout: 120000 }, async t => {
  await verifyBuild()
  const run = await sourceTokenFixture(t), input = decodeSnapshot(Buffer.from(run({})))
  const built = buildFoundation(input.snapshot, input)
  let buffer = Buffer.from(await exportFigFile(built.graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Icon masters', 'light', 'dark'].map(name => geometry(baseline, named(baseline, name)))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `deletion-${cycle}.fig`)
        if (cycle === 2) await page.emulateMedia({ forcedColors: 'active' })
        const open = page.getByRole('button', { name: 'Open variables', exact: true })
        const tabTo = async target => {
          for (let step = 0; step < 150 && !await target.evaluate(node => node === document.activeElement); step++) await page.keyboard.press('Tab')
          await expect(target).toBeFocused()
          assert.ok(await target.evaluate(node => {
            const css = getComputedStyle(node)
            return node.matches(':focus-visible') && css.outlineStyle !== 'none' && parseFloat(css.outlineWidth) > 0
          }))
        }
        await tabTo(open)
        await page.keyboard.press('Enter')
        const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
        const menu = dialog.getByRole('button', { name: 'Collection actions', exact: true })
        const spacing = dialog.getByRole('textbox', { name: 'spacing/1, light', exact: true })
        if (cycle === 0) {
          const remove = dialog.getByRole('button', { name: 'Delete spacing/1', exact: true })
          await tabTo(remove)
          const size = await remove.boundingBox()
          assert.ok(size.width >= 24 && size.height >= 24)
          await page.keyboard.press('Space')
          await expect(spacing).toHaveCount(0)
          // The existing modal focus scope falls back to its owning dialog
          // when the focused row disappears; no second focus manager is needed.
          await expect(dialog).toBeFocused()
          await page.keyboard.press('Escape')
          await expect(dialog).toBeHidden()
          await page.keyboard.press('Control+z')
          await tabTo(open)
          await page.keyboard.press('Enter')
          await expect(spacing).toHaveValue('0.1')
          await tabTo(menu)
          await page.keyboard.press('Space')
          await page.keyboard.press('End')
          await expect(page.getByRole('menuitem', { name: 'Delete collection', exact: true })).toBeFocused()
          await page.keyboard.press('Enter')
          await expect(spacing).toHaveCount(0)
          assert.ok(await dialog.evaluate(node => node.contains(document.activeElement)), 'deletion retains focus inside the dialog')
          await page.keyboard.press('Escape')
          await expect(dialog).toBeHidden()
          await page.keyboard.press('Control+z')
          await tabTo(open)
          await page.keyboard.press('Enter')
        }
        await expect(spacing).toHaveValue('0.1')
        await expect(dialog.getByRole('textbox', { name: 'leading/normal, dark', exact: true })).toHaveValue('1.5')
        await expect(dialog.getByRole('status')).toBeEmpty()
        await tabTo(menu)
        await page.keyboard.press('Escape')
        await expect(dialog).toBeHidden()
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          const graph = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          assert.deepEqual(['Icon masters', 'light', 'dark'].map(name => geometry(graph, named(graph, name))), untouched)
          const token = [...graph.variables.values()].find(variable => variable.name === 'spacing/1')
          assert.deepEqual(token.sourceToken, { version: 1, snapshot: input.snapshot.sha256,
            kind: 'scale', scale: 'spacing', key: '1', decimal: '0.1000', unit: 'px' })
          assert.equal(graph.resolveVariable(token.id), 0.1)
          for (const node of graph.nodes.values()) assert.ok(!Object.keys(node.overrides).some(key => /(^|:)boundVariables$/.test(key)))
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('mode actions retain typed token identity, keyboard history and two worker saves', { timeout: 120000 }, async t => {
  await verifyBuild()
  const run = await sourceTokenFixture(t), input = decodeSnapshot(Buffer.from(run({})))
  const { graph, collection } = buildFoundation(input.snapshot, input)
  const sparse = graph.createVariable('Sparse', 'FLOAT', collection.id, 1e-50)
  delete sparse.valuesByMode[collection.modes[1].modeId]
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Icon masters', 'light', 'dark'].map(name => geometry(baseline, named(baseline, name)))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `modes-${cycle}.fig`)
        const open = page.getByRole('button', { name: 'Open variables', exact: true })
        await open.click()
        const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
        const modeButton = name => dialog.getByRole('button', { name: new RegExp('^' + name + ' mode actions') })
        const tabTo = async target => {
          for (let step = 0; step < 150 && !await target.evaluate(node => node === document.activeElement); step++) {
            await page.keyboard.press('Tab')
          }
          await expect(target).toBeFocused()
        }
        const selectModeAction = async (name, action) => {
          const button = modeButton(name)
          await tabTo(button)
          assert.ok(await button.evaluate(node => {
            const style = getComputedStyle(node)
            return node.matches(':focus-visible') && style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0
          }))
          await button.press('Enter')
          const item = page.getByRole('menuitem', { name: action })
          await expect(item).toBeVisible()
          for (let step = 0; step < 8 && !await item.evaluate(node => node === document.activeElement); step++) {
            await page.keyboard.press('ArrowDown')
          }
          await expect(item).toBeFocused()
          await page.keyboard.press('Enter')
        }
        const closeDialog = async () => {
          await dialog.getByRole('button', { name: 'Collection actions', exact: true }).focus()
          await page.keyboard.press('Escape')
          await expect(dialog).toBeHidden()
        }
        if (cycle === 0) {
          // A default cannot be removed or changed while a numeric dependent
          // has only that default value. The failure must not consume redo.
          const spacing = dialog.getByRole('textbox', { name: 'spacing/1, light', exact: true })
          await spacing.fill('0.2')
          await spacing.press('Tab')
          await closeDialog()
          await page.keyboard.press('Control+z')
          await open.click()
          await selectModeAction('light', /Delete mode/i)
          await expect(dialog.getByRole('status')).toContainText('numeric default value missing')
          await expect(modeButton('light')).toHaveAttribute('data-default', 'true')
          await tabTo(dialog.getByRole('button', { name: 'Dismiss variable message', exact: true }))
          await page.keyboard.press('Enter')
          await closeDialog()
          await page.keyboard.press('Control+Shift+z')
          await open.click()
          await expect(spacing).toHaveValue('0.2')
          const darkValue = dialog.getByRole('textbox', { name: 'Sparse, dark', exact: true })
          await expect(darkValue).toHaveValue('1e-50')
          await expect(darkValue).toHaveAttribute('aria-description', 'Uses the default mode value until edited')
          await darkValue.fill('2e-50')
          await darkValue.press('Tab')
          await selectModeAction('dark', /default/i)
          await expect(modeButton('dark')).toHaveAttribute('data-default', 'true')
          await selectModeAction('dark', /Duplicate mode/i)
          await expect(modeButton('dark copy')).toBeVisible()
          await selectModeAction('dark copy', /Delete mode/i)
          await expect(modeButton('dark copy')).toHaveCount(0)
          await closeDialog()
          await page.keyboard.press('Control+z')
          await open.click()
          await expect(modeButton('dark copy')).toBeVisible()
          await expect(dialog.getByRole('textbox', { name: 'Sparse, dark copy', exact: true })).toHaveValue('2e-50')
          await selectModeAction('dark copy', /Rename mode/i)
          const rename = dialog.getByRole('textbox', { name: 'Rename dark copy mode', exact: true })
          await expect(rename, JSON.stringify(errors)).toBeFocused()
          await rename.fill('Contrast')
          await rename.press('Enter')
          await expect(modeButton('Contrast')).toBeVisible()
        }
        await expect(modeButton('dark')).toHaveAttribute('data-default', 'true')
        await expect(dialog.getByRole('textbox', { name: 'spacing/1, light', exact: true })).toHaveValue('0.2')
        await expect(dialog.getByRole('textbox', { name: 'spacing/1, dark', exact: true })).toHaveValue('0.1')
        await expect(dialog.getByRole('textbox', { name: 'spacing/1, Contrast', exact: true })).toHaveValue('0.1')
        await expect(dialog.getByRole('status')).toBeEmpty()
        if (cycle > 0) {
          if (cycle === 2) await page.emulateMedia({ forcedColors: 'active' })
          const button = modeButton('dark')
          await tabTo(button)
          assert.ok(await button.evaluate(node => {
            const style = getComputedStyle(node)
            return node.matches(':focus-visible') && style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0
          }))
          await button.press('Enter')
          await expect(page.getByRole('menuitem', { name: /Duplicate mode/i })).toBeVisible()
          await page.keyboard.press('Escape')
          await expect(button).toBeFocused()
          await expect(dialog).toBeVisible()
        }
        await closeDialog()
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          const token = [...reopened.variables.values()].find(variable => variable.name === 'spacing/1')
          const owner = reopened.variableCollections.get(token.collectionId)
          assert.deepEqual(owner.modes.map(mode => mode.name), ['light', 'dark', 'Contrast'])
          assert.equal(owner.modes.find(mode => mode.name === 'dark').modeId, owner.defaultModeId)
          assert.equal(reopened.resolveVariable(token.id, 'foreign-mode'), 0.1)
          assert.deepEqual(token.sourceToken, { version: 1, snapshot: input.snapshot.sha256,
            kind: 'scale', scale: 'spacing', key: '1', decimal: '0.1000', unit: 'px' })
          assert.deepEqual(['Icon masters', 'light', 'dark'].map(name => geometry(reopened, named(reopened, name))), untouched)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('numeric variables retain editor values, refusal feedback and two worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), collection = graph.createCollection('Numeric tokens')
  graph.renameMode(collection.id, collection.defaultModeId, 'light')
  graph.addMode(collection.id, 'numeric-dark', 'dark')
  const value = graph.createVariable('Precise value', 'FLOAT', collection.id, 0.1)
  value.valuesByMode['numeric-dark'] = 16777217
  graph.createVariable('Linked value', 'FLOAT', collection.id, { aliasId: value.id })
  graph.createVariable('Tiny value', 'FLOAT', collection.id, 1e-50)
  graph.createVariable('Negative zero', 'FLOAT', collection.id, -0)
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Untouched master', width: 32, height: 32 })
  graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Untouched instance', x: 80 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Untouched master', 'Untouched instance'].map(name => geometry(baseline, named(baseline, name)))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `numbers-${cycle}.fig`)
        const open = page.getByRole('button', { name: 'Open variables', exact: true })
        await open.click()
        const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
        const input = dialog.getByRole('textbox', { name: 'Precise value, light', exact: true })
        const next = dialog.getByRole('textbox', { name: 'Precise value, dark', exact: true })
        const tabTo = async target => {
          for (let step = 0; step < 100 && !await target.evaluate(node => node === document.activeElement); step++) {
            await page.keyboard.press('Tab')
          }
          await expect(target).toBeFocused()
        }
        const expected = cycle === 0 ? '0.1' : '0.1234567890123456'
        await expect(input).toHaveValue(expected)
        await expect(dialog.getByRole('textbox', { name: 'Negative zero, light', exact: true })).toHaveValue('-0')
        await expect(dialog.getByRole('textbox', { name: 'Tiny value, light', exact: true })).toHaveValue('1e-50')
        if (cycle === 2) {
          const tiny = dialog.getByRole('textbox', { name: 'Tiny value, light', exact: true })
          await tiny.fill('1e-40')
          await tiny.press('Enter')
          await expect(tiny).not.toBeFocused()
          await dialog.getByRole('button', { name: 'Collection actions', exact: true }).focus()
          await page.keyboard.press('Escape')
          await open.click()
          await expect(tiny).toHaveValue('1e-40')
          await expect(dialog.getByRole('status')).toBeEmpty()
        }
        if (cycle === 0) {
          await tabTo(input)
          assert.ok(await input.evaluate(node => {
            const style = getComputedStyle(node)
            return node.matches(':focus-visible') && style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) > 0
          }), 'the keyboard-focused numeric field has a visible native outline')
          await input.fill('0.1234567890123456')
          await input.press('Tab')
          await expect(next).toBeFocused()
          await expect(input).toHaveValue('0.1234567890123456')
          for (const invalid of ['1e39', '12px', '1e-999', '9007199254740993', '']) {
            await page.keyboard.press('Shift+Tab')
            await expect(input).toBeFocused()
            await input.fill(invalid)
            await input.press('Tab')
            await expect(next).toBeFocused()
            await expect(dialog.getByRole('status')).toContainText('Variables unchanged. Native number:')
            await expect(input).toHaveValue('0.1234567890123456')
          }
          await page.keyboard.press('Shift+Tab')
          await input.fill('0.75')
          await input.press('Escape')
          await expect(input).toHaveValue('0.1234567890123456')
          await expect(dialog).toBeVisible()
          const dismiss = dialog.getByRole('button', { name: 'Dismiss variable message', exact: true })
          await tabTo(dismiss)
          await page.keyboard.press('Enter')
          await expect(dialog.getByRole('status')).toBeEmpty()
          await expect(dialog.getByRole('button', { name: 'Collection actions', exact: true })).toBeFocused()
          await page.keyboard.press('Escape')
          await page.keyboard.press('Control+z')
          await open.click()
          await expect(input).toHaveValue('0.1')
          await page.keyboard.press('Escape')
          await page.keyboard.press('Control+Shift+z')
          await open.click()
          await expect(input).toHaveValue('0.1234567890123456')
        }
        await page.keyboard.press('Escape')
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          const variable = name => [...reopened.variables.values()].find(variable => variable.name === name)
          const precise = variable('Precise value'), col = reopened.variableCollections.get(precise.collectionId)
          assert.equal(reopened.resolveVariable(precise.id), 0.1234567890123456)
          assert.equal(reopened.resolveVariable(precise.id, col.modes.find(mode => mode.name === 'dark').modeId), 16777217)
          assert.equal(reopened.resolveVariable(variable('Linked value').id), 0.1234567890123456)
          assert.equal(reopened.resolveVariable(variable('Tiny value').id), 1e-50)
          assert.ok(Object.is(reopened.resolveVariable(variable('Negative zero').id), -0))
          assert.deepEqual(['Untouched master', 'Untouched instance'].map(name => geometry(reopened, named(reopened, name))), untouched)
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

for (const nested of [false, true]) test(`page and ${nested ? 'nested' : 'root'} layer mode controls retain inheritance, refusal and two worker saves`, { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), padding = graph.createCollection('Padding modes'), gaps = graph.createCollection('Gap modes')
  graph.renameMode(padding.id, padding.defaultModeId, 'Compact')
  graph.addMode(padding.id, 'invalid-padding', 'Invalid')
  graph.addMode(padding.id, 'comfortable-padding', 'Comfortable')
  graph.renameMode(gaps.id, gaps.defaultModeId, 'Compact')
  graph.addMode(gaps.id, 'spacious-gap', 'Spacious')
  const left = graph.createVariable('Left padding', 'FLOAT', padding.id, 16)
  left.valuesByMode['invalid-padding'] = -8
  left.valuesByMode['comfortable-padding'] = 24
  const gap = graph.createVariable('Mode gap', 'FLOAT', gaps.id, 12)
  gap.valuesByMode['spacious-gap'] = 32
  const pageId = graph.getPages()[0].id
  const master = graph.createNode('COMPONENT', pageId, { name: 'Mode master',
    layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    paddingLeft: 16, paddingRight: 8, paddingTop: 8, paddingBottom: 8, itemSpacing: 12,
    variableModes: { [padding.id]: padding.defaultModeId } })
  for (const name of ['First', 'Second']) graph.createNode('RECTANGLE', master.id, { name, width: 40, height: 24 })
  graph.bindVariable(master.id, 'paddingLeft', left.id)
  graph.bindVariable(master.id, 'itemSpacing', gap.id)
  computeLayout(graph, master.id)
  if (nested) {
    const wrapper = graph.createNode('COMPONENT', pageId, { name: 'Wrapper master', width: 300, height: 100, y: 100 })
    graph.createInstance(master.id, wrapper.id, { name: 'Mode consumer' })
    graph.createInstance(wrapper.id, pageId, { name: 'Mode wrapper', x: 350, y: 150 })
  } else graph.createInstance(master.id, pageId, { name: 'Mode consumer', x: 350 })
  graph.createInstance(master.id, pageId, { name: 'Mode sibling', x: 550 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: cycle === 2 ? 1280 : 1440, height: 1000 }, hasTouch: cycle === 2 })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `mode-controls-${nested}-${cycle}.fig`)
        if (cycle === 2) await page.emulateMedia({ forcedColors: 'active' })
        const controls = page.getByRole('group', { name: 'Page modes', exact: true })
        const tabTo = async target => {
          for (let step = 0; step < 180 && !await target.evaluate(node => node === document.activeElement); step++) await page.keyboard.press('Tab')
          await expect(target).toBeFocused()
          assert.ok(await target.evaluate(node => node.matches(':focus-visible') && getComputedStyle(node).outlineStyle !== 'none'))
        }
        const pageGap = controls.getByRole('combobox', { name: 'Gap modes', exact: true })
        await expect(pageGap).toBeVisible()
        if (cycle === 0) {
          await tabTo(pageGap)
          await page.keyboard.press('End')
        }
        await expect(pageGap.locator('option:checked')).toHaveText('Spacious')
        if (nested) {
          await page.getByRole('treeitem', { name: 'Mode wrapper Lock Hide', exact: true }).click()
          await page.keyboard.press('ArrowRight')
        }
        const layer = page.getByRole('treeitem', { name: 'Mode consumer Lock Hide', exact: true })
        await layer.click()
        const group = page.getByRole('group', { name: 'Layer modes', exact: true })
        const select = group.getByRole('combobox', { name: 'Padding modes', exact: true })
        const other = group.getByRole('combobox', { name: 'Gap modes', exact: true })
        const width = page.getByRole('spinbutton', { name: 'Width', exact: true })
        await expect(other).toHaveValue('')
        await expect(other).toHaveAccessibleDescription('Inherited: Spacious')
        await expect(width).toHaveAttribute('aria-valuenow', cycle === 0 ? '136' : '144')
        await tabTo(select)
        if (cycle === 0) {
          await page.keyboard.press('End')
          await expect(width).toHaveAttribute('aria-valuenow', '144')
          await expect(select.locator('option:checked')).toHaveText('Comfortable')
          await page.keyboard.press('ArrowUp')
          await expect(select).toBeFocused()
          await expect(select.locator('option:checked')).toHaveText('Comfortable')
          await expect(select).toHaveAttribute('aria-invalid', 'true')
          await expect(select).toHaveAccessibleDescription(/Mode change refused.*Previous choices were kept/)
          await expect(width).toHaveAttribute('aria-valuenow', '144')
          await page.addScriptTag({ content: axe.source })
          const audit = await group.evaluate(element => window.axe.run(element))
          assert.deepEqual(audit.violations, [])
          assert.deepEqual(audit.incomplete, [])
          await page.keyboard.press('Control+z')
          await expect(width).toHaveAttribute('aria-valuenow', '136')
          await expect(select).toHaveValue('')
          await expect(select).not.toHaveAttribute('aria-invalid', 'true')
          await page.keyboard.press('Control+Shift+z')
          await expect(width).toHaveAttribute('aria-valuenow', '144')
          await tabTo(select)
          await page.keyboard.press('Home')
          await expect(width).toHaveAttribute('aria-valuenow', '136')
          await page.keyboard.press('Tab')
          await expect(other).toBeFocused()
          await page.keyboard.press('ArrowDown')
          await expect(width).toHaveAttribute('aria-valuenow', '116')
          await page.keyboard.press('Tab')
          const reset = group.getByRole('button', { name: 'Reset all mode overrides', exact: true })
          await expect(reset).toBeFocused()
          await page.keyboard.press('Enter')
          await expect(select).toBeFocused()
          await expect(reset).toBeDisabled()
          await expect(other).toHaveValue('')
          await expect(width).toHaveAttribute('aria-valuenow', '136')
          await page.keyboard.press('ArrowDown')
          await expect(select).toHaveAccessibleDescription('Explicit: Compact')
          await expect(reset).toBeEnabled()
          await expect(width).toHaveAttribute('aria-valuenow', '136')
          await page.keyboard.press('Control+z')
          await expect(select).toHaveValue('')
          await expect(reset).toBeDisabled()
          await page.keyboard.press('Control+Shift+z')
          await expect(select).toHaveAccessibleDescription('Explicit: Compact')
          await page.keyboard.press('End')
          await expect(width).toHaveAttribute('aria-valuenow', '144')
        }
        await expect(select.locator('option:checked')).toHaveText('Comfortable')
        await expect(other).toHaveValue('')
        assert.equal(await page.evaluate(() => matchMedia('(pointer: coarse)').matches), cycle === 2)
        const size = await select.boundingBox()
        assert.ok(size.width >= 24 && size.height >= (cycle === 2 ? 44 : 40), `Mode control dimensions: ${JSON.stringify(size)}`)
        const resetSize = await group.getByRole('button', { name: 'Reset all mode overrides', exact: true }).boundingBox()
        assert.ok(resetSize.width >= 24 && resetSize.height >= (cycle === 2 ? 44 : 40))
        assert.ok(await group.evaluate(node => node.scrollWidth <= node.clientWidth))
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          const saved = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          const target = nested ? saved.getChildren(named(saved, 'Mode wrapper').id)[0] : named(saved, 'Mode consumer')
          const source = saved.getNode(target.componentId), token = [...saved.variables.values()].find(value => value.name === 'Left padding')
          assert.deepEqual(Object.keys(saved.getNodeExplicitVariableModes(target.id)), [token.collectionId])
          assert.deepEqual([target.paddingLeft, target.itemSpacing, target.width], [24, 32, 144])
          assert.deepEqual([source.paddingLeft, source.itemSpacing, source.width], [16, 32, 136])
          assert.deepEqual([named(saved, 'Mode sibling').paddingLeft, named(saved, 'Mode sibling').width], [16, 136])
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        if (cycle === 2) {
          await layer.click()
          await page.getByRole('treeitem', { name: 'Mode sibling Lock Hide', exact: true }).click({ modifiers: ['Control'] })
          await expect(page.getByText('Select one layer to edit its modes.', { exact: true })).toBeVisible()
          await expect(group).toHaveCount(0)
          await expect(controls).toHaveCount(0)
          await layer.click()
          await expect(select.locator('option:checked')).toHaveText('Comfortable')
          await expect(other).toHaveAccessibleDescription('Inherited: Spacious')
        }
        assert.ok(workers.some(path => /\/worker-.*\.js$/.test(path)))
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('bound pixel tokens reflow linked consumers through keyboard edits and two worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), collection = graph.createCollection('Live dimensions')
  graph.renameMode(collection.id, collection.defaultModeId, 'light')
  graph.addMode(collection.id, 'live-dark', 'dark')
  const base = graph.createVariable('Spacing', 'FLOAT', collection.id, 8)
  base.valuesByMode['live-dark'] = 24
  base.sourceToken = { version: 1, snapshot: 'b'.repeat(64), kind: 'scale', scale: 'spacing', key: '2', decimal: '8.00', unit: 'px' }
  const alias = graph.createVariable('Gap alias', 'FLOAT', collection.id, { aliasId: base.id })
  const pageId = graph.getPages()[0].id
  const master = graph.createNode('COMPONENT', pageId, { name: 'Bound master',
    layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    itemSpacing: 8, paddingLeft: 8, paddingRight: 8, paddingTop: 8, paddingBottom: 8 })
  for (const name of ['First', 'Second']) graph.createNode('RECTANGLE', master.id, { name, width: 40, height: 24 })
  graph.bindVariable(master.id, 'itemSpacing', alias.id)
  graph.bindVariable(master.id, 'paddingLeft', alias.id)
  computeLayout(graph, master.id)
  graph.createInstance(master.id, pageId, { name: 'Light consumer', x: 200,
    variableModes: { [collection.id]: collection.defaultModeId } })
  graph.createInstance(master.id, pageId, { name: 'Dark consumer', x: 400,
    variableModes: { [collection.id]: 'live-dark' } })
  graph.createNode('RECTANGLE', pageId, { name: 'Unrelated geometry', x: 37, y: 217, width: 73, height: 29 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = geometry(baseline, named(baseline, 'Unrelated geometry'))
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: [] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `live-dimensions-${cycle}.fig`)
        const layer = page.getByRole('treeitem', { name: 'Light consumer Lock Hide', exact: true })
        await layer.click()
        const width = page.getByRole('spinbutton', { name: 'Width', exact: true })
        await expect(width).toHaveAttribute('aria-valuenow', cycle === 0 ? '104' : '120')
        await page.keyboard.press('Escape')
        const open = page.getByRole('button', { name: 'Open variables', exact: true })
        await open.click()
        const dialog = page.getByRole('dialog', { name: 'Local variables', exact: true })
        const input = dialog.getByRole('textbox', { name: 'Spacing, light', exact: true })
        const next = dialog.getByRole('textbox', { name: 'Spacing, dark', exact: true })
        await expect(input).toHaveValue(cycle === 0 ? '8' : '16')
        if (cycle === 0) {
          for (let step = 0; step < 80 && !await input.evaluate(node => node === document.activeElement); step++) await page.keyboard.press('Tab')
          await expect(input).toBeFocused()
          for (const forcedColors of ['none', 'active']) {
            await page.emulateMedia({ forcedColors })
            assert.ok(await input.evaluate(node => node.matches(':focus-visible') &&
              getComputedStyle(node).outlineStyle === 'auto' && parseFloat(getComputedStyle(node).outlineWidth) > 0))
          }
          await page.emulateMedia({ forcedColors: 'none' })
          await input.fill('16')
          await input.press('Tab')
          await expect(next).toBeFocused()
          await expect(dialog.getByRole('status')).toBeEmpty()
          await input.fill('-1')
          await input.press('Tab')
          await expect(next).toBeFocused()
          await expect(input).toHaveValue('16')
          await expect(dialog.getByRole('status')).toContainText('Variables unchanged. Native numeric binding:')
          await page.addScriptTag({ content: axe.source })
          const audit = await dialog.evaluate(element => window.axe.run(element))
          assert.deepEqual(audit.violations, [])
          assert.deepEqual(audit.incomplete, [])
        }
        await dialog.getByRole('button', { name: 'Collection actions', exact: true }).focus()
        await page.keyboard.press('Escape')
        await expect(open).toBeFocused()
        await layer.click()
        await expect(width).toHaveAttribute('aria-valuenow', '120')
        if (cycle === 0) {
          await layer.click()
          await page.keyboard.press('Control+z')
          await expect(width).toHaveAttribute('aria-valuenow', '104')
          await page.keyboard.press('Control+Shift+z')
          await expect(width).toHaveAttribute('aria-valuenow', '120')
        }
        await page.getByRole('treeitem', { name: 'Dark consumer Lock Hide', exact: true }).click()
        await expect(width).toHaveAttribute('aria-valuenow', '136')
        if (cycle < 2) {
          buffer = await saveDocument(page, errors, workers)
          const saved = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          const source = [...saved.variables.values()].find(variable => variable.name === 'Spacing')
          const linked = [...saved.variables.values()].find(variable => variable.name === 'Gap alias')
          assert.deepEqual(source.sourceToken, base.sourceToken)
          assert.deepEqual(Object.keys(linked.valuesByMode).sort(),
            saved.variableCollections.get(linked.collectionId).modes.map(mode => mode.modeId).sort())
          assert.ok(Object.values(linked.valuesByMode).every(value => value.aliasId === source.id))
          for (const [name, gap] of [['Bound master', 16], ['Light consumer', 16], ['Dark consumer', 24]]) {
            const node = named(saved, name)
            assert.equal(node.width, 88 + 2 * gap)
            assert.equal(node.itemSpacing, gap)
            assert.equal(node.paddingLeft, gap)
            assert.equal(node.boundVariables.itemSpacing, linked.id)
            assert.equal(node.boundVariables.paddingLeft, linked.id)
            assert.deepEqual(saved.getChildren(node.id).map(child => child.x), [gap, 40 + 2 * gap], `${name}, save ${cycle}`)
            if (node.type === 'INSTANCE') assert.equal(saved.getNode(node.componentId).name, 'Bound master')
          }
          assert.deepEqual(geometry(saved, named(saved, 'Unrelated geometry')), untouched)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
        assert.deepEqual(errors, [])
      } finally { await context.close() }
    }
  } finally { await browser.close() }
})

test('oversized source words remain editable through keyboard history and two browser worker saves', { timeout: 120000 }, async t => {
  await verifyBuild()
  const id = 'pk-ui.component.text/muted', snapshot = exportCore(['--example', id, '--props'], { content: 'Album', size: 'base' })
  const temporary = await mkdtemp(join(tmpdir(), 'platformkit-editor-wrap-fonts-'))
  t.after(() => rm(temporary, { recursive: true, force: true }))
  const { fonts, config } = await writeEditorFontFixtures(temporary)
  const comparisonBrowser = await chromium.launch({ headless: true, args: browserArgs, env: { ...process.env, FONTCONFIG_FILE: config } })
  t.after(() => comparisonBrowser.close())
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs, env: { ...process.env, FONTCONFIG_FILE: config } })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1)), previous = getTextMeasurer()
  try {
    const { graph, selections } = await buildComponentDocument(snapshot, {
      examples: [id], fonts, browser: comparisonBrowser, renderer, viewport: { width: 320, height: 900 },
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
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: ['local-fonts'] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, 'source-wrapping.fig', page => enableLocalFonts(page, fonts))
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

test('real EmptyState copy and responsive line boxes survive editor property edits and two worker saves', { timeout: 120000 }, async t => {
  await verifyBuild()
  const run = await emptyStateFixture(t), id = 'fixture/empty'
  const input = { title: 'This album is still being made', description: 'There are no stickers in it yet.', action: true }
  const snapshot = run(input), temporary = await mkdtemp(join(tmpdir(), 'platformkit-editor-empty-fonts-'))
  t.after(() => rm(temporary, { recursive: true, force: true }))
  const { fonts, config } = await writeEditorFontFixtures(temporary)
  const comparisonBrowser = await chromium.launch({ headless: true, args: browserArgs, env: { ...process.env, FONTCONFIG_FILE: config } })
  t.after(() => comparisonBrowser.close())
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs, env: { ...process.env, FONTCONFIG_FILE: config } })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1)), previous = getTextMeasurer()
  try {
    const options = { examples: [id], fonts, browser: comparisonBrowser, renderer, viewport: { width: 320, height: 900 } }
    const { graph, selections, placements } = await buildComponentDocument(snapshot, options)
    graph.updateNode(selections[0].instance.id, { name: 'Editable empty state' })
    graph.createInstance(selections[0].master.id, placements.id, { name: 'Unchanged empty state' })
    let buffer = Buffer.from(await exportFigFile(graph)), previousValue = input.description
    const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
    const protectedNodes = [...baseline.getAllNodes()].filter(node => node.type === 'COMPONENT' || node.name === 'Unchanged empty state')
      .map(node => [node.name, geometry(baseline, node)])
    for (const [width, value] of [[1280, 'Gather the people and places we remember together. '.repeat(7).trim()], [320, input.description]]) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: ['local-fonts'] })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, 'empty-state.fig', page => enableLocalFonts(page, fonts))
        await page.getByRole('button', { name: 'Editable source instances', exact: true }).click()
        await page.getByRole('treeitem', { name: 'Editable source instances Lock Hide', exact: true }).click()
        await page.keyboard.press('ArrowRight')
        const layer = page.getByRole('treeitem', { name: 'Editable empty state Lock Hide', exact: true })
        await layer.click()
        const size = page.getByRole('spinbutton', { name: 'Width', exact: true })
        await size.dblclick(); await page.keyboard.press('Control+a'); await page.keyboard.type(String(width)); await page.keyboard.press('Enter')
        await expect(size).toHaveAttribute('aria-valuenow', String(width))
        const control = page.getByRole('textbox', { name: 'description', exact: true })
        await expect(control).toHaveValue(previousValue)
        await control.fill(value); await control.press('Tab'); await layer.click()
        await page.keyboard.press('Control+z'); await expect(control).toHaveValue(previousValue)
        await page.keyboard.press('Control+Shift+z'); await expect(control).toHaveValue(value)
        buffer = await saveDocument(page, errors, workers)
        const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' }), placed = named(reopened, 'Editable empty state')
        const observed = (await captureExample(comparisonBrowser, run({ ...input, description: value }), id, { ...options, viewport: { width, height: 900 } })).roots[0]
        for (const field of ['width', 'height']) assert.ok(Math.abs(placed[field] - observed.bounds[field]) <= 1 / 64)
        for (const [i, child] of reopened.getChildren(placed.id).entries()) {
          const source = observed.children[i]
          for (const field of ['width', 'height']) assert.ok(Math.abs(child[field] - source.bounds[field]) <= 1 / 64)
          for (const field of ['x', 'y']) assert.ok(Math.abs(child[field] - source.bounds[field] + observed.bounds[field]) <= 1 / 64)
          if (i < 2) assert.equal(reopened.getChildren(child.id)[0].width, child.width, 'saved text line boxes follow the resized paragraph')
        }
        assert.equal(reopened.getChildren(reopened.getChildren(placed.id)[1].id)[0].text, value)
        assert.equal(reopened.getChildren(placed.id)[1].maxWidth, 448)
        const extraction = extractSourceProps(reopened, placed, snapshot)
        if (value === input.description) assert.equal(extraction.status, 'no-supported-changes')
        else assert.deepEqual(extraction.proposal, { baseSHA256: snapshot.sha256, path: [id], props: { description: value } })
        for (const [name, expected] of protectedNodes) assert.deepEqual(geometry(reopened, named(reopened, name)), expected)
        assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
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
          const picker = dialog.getByRole('button', { name: 'Edit color: Ink, Mode 1', exact: true })
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

test('imported shape resizing propagates nested HUG layout through editor history and two worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), pageNode = graph.getPages()[0]
  const layout = { layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG', itemSpacing: 8 }
  const inner = graph.createNode('COMPONENT', pageNode.id, { name: 'Inner master', ...layout })
  for (const name of ['Resizable shape', 'Inner guard']) graph.createNode('RECTANGLE', inner.id, { name, width: 40, height: 24 })
  computeLayout(graph, inner.id)
  const outer = graph.createNode('COMPONENT', pageNode.id, { name: 'Outer master', y: 100, ...layout })
  graph.createInstance(inner.id, outer.id, { name: 'Inner placement' })
  graph.createNode('RECTANGLE', outer.id, { name: 'Outer guard', width: 40, height: 24 })
  computeLayout(graph, outer.id)
  for (const [name, x] of [['First placement', 250], ['Second placement', 500]]) {
    graph.createInstance(outer.id, pageNode.id, { name, x, y: 200 })
  }
  const unrelated = graph.createNode('COMPONENT', pageNode.id, { name: 'Unrelated master', y: 300, width: 23, height: 13 })
  graph.createNode('RECTANGLE', unrelated.id, { name: 'Unrelated shape', width: 23, height: 13 })
  graph.createInstance(unrelated.id, pageNode.id, { name: 'Unrelated placement', x: 250, y: 300 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Unrelated master', 'Unrelated placement'].map(name => [name, geometry(baseline, named(baseline, name))])
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `inherited-shape-${cycle}.fig`)
        const placed = page.getByRole('treeitem', { name: 'First placement Lock Hide', exact: true })
        const width = page.getByRole('spinbutton', { name: 'Width', exact: true })
        const previous = 40 + Math.min(cycle, 2) * 20, next = previous + 20
        await placed.click()
        await expect(width).toHaveAttribute('aria-valuenow', String(previous + 96))
        if (cycle < 2) {
          await page.getByRole('treeitem', { name: 'Inner master Lock Hide', exact: true }).click()
          await page.keyboard.press('ArrowRight')
          await page.getByRole('treeitem', { name: 'Resizable shape Lock Hide', exact: true }).click()
          await expect(width).toHaveAttribute('aria-valuenow', String(previous))
          await width.dblclick(); await page.keyboard.press('Control+a'); await page.keyboard.type(String(next)); await page.keyboard.press('Enter')
          await expect(width).toHaveAttribute('aria-valuenow', String(next))
          await placed.click()
          await expect(width).toHaveAttribute('aria-valuenow', String(next + 96))
          await page.keyboard.press('Control+z')
          await expect(width).toHaveAttribute('aria-valuenow', String(previous + 96))
          await page.keyboard.press('Control+Shift+z')
          await expect(width).toHaveAttribute('aria-valuenow', String(next + 96))
          buffer = await saveDocument(page, errors, workers)
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          const source = masterOf(reopened, 'Inner master')
          assert.deepEqual([source.width, source.height], [next + 48, 24])
          assert.deepEqual(reopened.getChildren(source.id).map(node => [node.x, node.width]), [[0, next], [next + 8, 40]])
          for (const name of ['Outer master', 'First placement', 'Second placement']) {
            const owner = named(reopened, name), [nested, guard] = reopened.getChildren(owner.id)
            assert.deepEqual([owner.width, owner.height, nested.width, guard.x, guard.width], [next + 96, 24, next + 48, next + 56, 40])
            assert.equal(chain(reopened, nested, 'componentId').at(-1).id, source.id)
            assert.deepEqual(reopened.getChildren(nested.id).map(node => [node.type, node.x, node.width]),
              [['RECTANGLE', 0, next], ['RECTANGLE', next + 8, 40]], 'saved plain shapes keep computed sibling positions')
            if (name !== 'Outer master') {
              assert.equal(chain(reopened, owner, 'componentId').at(-1).name, 'Outer master')
              assert.deepEqual([owner.x, owner.y], [name === 'First placement' ? 250 : 500, 200])
            }
          }
          for (const node of reopened.getAllNodes()) if (node.type === 'INSTANCE') {
            assert.ok(Object.keys(node.overrides).every(key => key !== 'width' && !key.endsWith(':width')),
              'inherited dimensions must not become authored instance overrides')
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

test('descendant rotation retains placement, inherited links and authored overrides through editor history and two worker saves', { timeout: 120000 }, async () => {
  await verifyBuild()
  const graph = new SceneGraph(), pageNode = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', pageNode.id, { name: 'Rotation master', width: 120, height: 80 })
  graph.createNode('RECTANGLE', master.id, { name: 'Rotating shape', x: 8, y: 12, width: 40, height: 24 })
  graph.createNode('RECTANGLE', master.id, { name: 'Rotation guard', x: 80, y: 12, width: 20, height: 24 })
  graph.createInstance(master.id, pageNode.id, { name: 'Inherited placement', x: 200, y: 100 })
  const authored = graph.createInstance(master.id, pageNode.id, { name: 'Authored placement', x: 400, y: 100 })
  graph.updateNode(authored.id, { overrides: { [`${graph.getChildren(authored.id)[0].id}:rotation`]: true } })
  const unrelated = graph.createNode('COMPONENT', pageNode.id, { name: 'Unrelated rotation master', x: 600, width: 30, height: 20 })
  graph.createInstance(unrelated.id, pageNode.id, { name: 'Unrelated rotation placement', x: 600, y: 100 })
  let buffer = Buffer.from(await exportFigFile(graph))
  const baseline = await parseFigFile(figBuffer(buffer), { populate: 'all' })
  const untouched = ['Authored placement', 'Unrelated rotation master', 'Unrelated rotation placement']
    .map(name => [name, geometry(baseline, named(baseline, name))])
  const browser = await chromium.launch({ headless: true, channel: 'chromium', args: browserArgs })
  try {
    for (let cycle = 0; cycle < 3; cycle++) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      try {
        const { page, errors, workers } = await openDocument(context, buffer, `rotation-${cycle}.fig`)
        await page.getByRole('treeitem', { name: 'Rotation master Lock Hide', exact: true }).click()
        await page.keyboard.press('ArrowRight')
        const source = page.getByRole('treeitem', { name: 'Rotating shape Lock Hide', exact: true }).first()
        await source.click()
        const rotation = page.getByRole('spinbutton', { name: 'Rotation', exact: true })
        await expect(rotation).toHaveAttribute('aria-valuenow', String(cycle * 30))
        if (cycle < 2) {
          const next = (cycle + 1) * 30
          await rotation.dblclick(); await page.keyboard.press('Control+a'); await page.keyboard.type(String(next)); await page.keyboard.press('Enter')
          await expect(rotation).toHaveAttribute('aria-valuenow', String(next))
          await source.click()
          await page.keyboard.press('Control+z')
          await expect(rotation).toHaveAttribute('aria-valuenow', String(cycle * 30))
          await page.keyboard.press('Control+Shift+z')
          await expect(rotation).toHaveAttribute('aria-valuenow', String(next))
          buffer = await saveDocument(page, errors, workers)
          const reopened = await parseFigFile(figBuffer(buffer), { populate: 'all' })
          for (const name of ['Rotation master', 'Inherited placement']) {
            const owner = named(reopened, name), [shape, guard] = reopened.getChildren(owner.id)
            for (const [field, value] of Object.entries({ x: 8, y: 12, width: 40, height: 24, rotation: next })) {
              assert.ok(Math.abs(shape[field] - value) < 1e-4, `${name} ${field}: ${shape[field]} versus ${value}`)
            }
            assert.deepEqual([guard.x, guard.y, guard.width, guard.height, guard.rotation], [80, 12, 20, 24, 0])
            assert.deepEqual([owner.x, owner.y, owner.width, owner.height], name === 'Rotation master' ? [0, 0, 120, 80] : [200, 100, 120, 80])
            if (name === 'Inherited placement') {
              assert.equal(chain(reopened, shape, 'componentId').at(-1).id, reopened.getChildren(masterOf(reopened, 'Rotation master').id)[0].id)
              assert.ok(!Object.hasOwn(owner.overrides, `${shape.id}:rotation`), 'inherited rotation is not an authored override')
            }
          }
          const placed = named(reopened, 'Authored placement'), child = reopened.getChildren(placed.id)[0]
          assert.equal(child.rotation, 0)
          assert.ok(Object.hasOwn(placed.overrides, `${child.id}:rotation`), 'explicit zero rotation remains authored')
          const authoredGeometry = geometry(reopened, placed), expectedAuthored = untouched[0][1]
          for (const field of ['x', 'y']) assert.ok(Math.abs(child[field] - expectedAuthored.children[0][field]) < 1e-4, `authored ${field}`)
          Object.assign(authoredGeometry.children[0], { x: expectedAuthored.children[0].x, y: expectedAuthored.children[0].y })
          for (const [name, expected] of untouched) assert.deepEqual(name === 'Authored placement' ? authoredGeometry : geometry(reopened, named(reopened, name)), expected, name)
          assert.ok(workers.some(path => /export-worker-.*\.js$/.test(path)))
        }
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
        await page.evaluate(() => new Promise(requestAnimationFrame))
        assert.deepEqual(errors, [], 'opening the lazy page')
        await page.getByRole('treeitem', { name: /^Edited instance / }).click()
        for (let level = depth; level > 0; level--) {
          await page.keyboard.press('ArrowRight')
          await page.getByRole('treeitem', { name: `Nested ${level} Lock Hide`, exact: true }).click()
        }
        const control = page.getByRole('combobox', { name: 'Leading icon', exact: true })
        await control.getByText(cycle ? 'x' : 'plus', { exact: true }).waitFor()
        assert.deepEqual(errors, [], 'selecting the nested owner')
        if (cycle === 0) {
          await control.click()
          await page.getByRole('option', { name: 'x', exact: true }).click()
          await control.getByText('x', { exact: true }).waitFor()
          await page.evaluate(() => new Promise(requestAnimationFrame))
          assert.deepEqual(errors, [], 'applying the replacement')
          await page.keyboard.press('Control+z')
          await control.getByText('plus', { exact: true }).waitFor()
          await page.evaluate(() => new Promise(requestAnimationFrame))
          assert.deepEqual(errors, [], 'undoing the replacement')
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

async function writeEditorFontFixtures(temporary) {
  // Local Font Access needs static OTF, not WOFF. Both measurement boundaries
  // use these exact process-owned bytes; no fonts are installed on the host.
  const fonts = []
  for (const { weight, bytes } of editorFontFixtures) {
    await writeFile(join(temporary, `${weight}.otf`), bytes, { flag: 'wx' })
    fonts.push({ family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: hash(bytes) })
  }
  const config = join(temporary, 'fonts.conf')
  await writeFile(config, `<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "fonts.dtd"><fontconfig><dir>${temporary}</dir><cachedir>${temporary}/cache</cachedir></fontconfig>`, { flag: 'wx' })
  return { fonts, config }
}

async function enableLocalFonts(page, fonts) {
  assert.deepEqual(await page.evaluate(() => [window.isSecureContext, typeof window.queryLocalFonts]), [true, 'function'])
  await page.keyboard.press('t')
  await page.locator('[data-test-id="canvas-element"]').click({ position: { x: 300, y: 300 } })
  await page.keyboard.type('Font access')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Font settings', exact: true }).click()
  const panel = page.locator('[data-test-id="font-settings-panel"]')
  // Online and local access are separate permissions, never interchangeable.
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
}

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
  "github.com/septagon-oss/platformkit/ui/components"; "github.com/septagon-oss/platformkit/ui/components/examples"
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
  summary := examples.ExampleOf(examples.ExampleInfo{ID: "source-summary", ComponentID: "pk-ui.component.text"},
    components.TextProps{Content: "Source-owned album states", Color: "muted"}, components.Text)
  return components.Form(p, append([]g.Node{summary.Node}, children...)...)
}
func main() {
  var input struct { Proposal *ui.PropsProposal; Dashed, Centered bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  resource := httpx.Resource{Module: "notes", Entity: "note", Path: "/api/v1/notes", Schema: crud.Schema{Fields: crud.Fields[*Note]()}}
  form := screens.FormExample("fixture/generated-form", resource, screens.Options{Root: "/admin"}, "/admin/notes", "New note", nil, nil, "", true)
  captures := append(examples.Gallery(), form)
  state := examples.ExampleOf(examples.ExampleInfo{ID: "stage", ComponentID: "pk-ui.component.select"},
    components.SelectProps{Name: "stage", Label: "Stage", Value: "draft", Required: true, Placeholder: "Choose a stage",
      Options: []components.SelectOption{{Value: "draft", Label: "Draft"}, {Value: " ready,a ", Label: "Ready"}}}, components.Select)
  choices := examples.ExampleWithChildren(examples.ExampleInfo{ID: "fixture/choice-form", ComponentID: "pk-ui.component.form"},
    components.FormProps{Label: "Album state", Action: "/albums"}, []g.Node{state.Node}, choiceForm)
  captures = append(captures, choices)
  var extra ui.Extra
  if input.Centered {
    extra.Sheets = append(extra.Sheets, css.NewSheet().Select("[data-component=text]", css.Decl("text-align", css.Literal("center"))))
  }
  if input.Dashed {
    extra.Sheets = append(extra.Sheets, css.NewSheet().Select("[data-component=button]",
      css.Decl("border", css.Literal("1px dashed var(--pk-color-border-default)")),
      css.Decl("border-radius", css.Literal("12px"))))
  }
  snapshot, err := ui.Export(design.Default(), captures, extra)
  if input.Proposal != nil { _, snapshot, err = ui.ProjectProps(design.Default(), captures, *input.Proposal, extra) }
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
    const { fonts, config } = await writeEditorFontFixtures(temporary)
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
        const { page, errors, workers } = await openDocument(context, buffer, `composition-${cycle}.fig`, page => enableLocalFonts(page, fonts))
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
            .getByRole('button', { name: `Edit color: ${inputNameForRole}, light`, exact: true }))
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
