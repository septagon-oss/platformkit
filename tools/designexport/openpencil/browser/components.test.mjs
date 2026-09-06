import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { parseColor } from '@open-pencil/core/color'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { captureExample } from './capture.mjs'

const primary = 'pk-ui.component.button/primary'
const bytes = readFileSync(new URL('../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-600-normal.woff', import.meta.url))
const faces = [{ family: 'IBM Plex Sans', weight: 600, style: 'normal', bytes, sha256: createHash('sha256').update(bytes).digest('hex') }]
const originalMeasurer = getTextMeasurer()
let browser, ck, renderer
before(async () => {
  // This is a declared comparison environment, not default-platform fidelity.
  browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  ck = await initCanvasKit()
  renderer = new SkiaRenderer(ck, ck.MakeSurface(800, 200))
})
after(async () => { renderer?.destroy(); setTextMeasurer(originalMeasurer); await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0))

function source(label = 'Save', id = primary, extra = {}) {
  return JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', id, '--props'], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', input: JSON.stringify({ label, ...extra }),
  }))
}

async function observe(snapshot, mode = 'light', usingBrowser = browser) {
  return captureExample(usingBrowser, snapshot, snapshot.examples[0].id, { mode, fonts: faces })
}

function close(actual, expected, message) {
  assert.ok(Math.abs(actual - expected) <= 1 / 64, `${message}: ${actual} versus ${expected}`)
}

function colorCollection(graph) {
  assert.equal(graph.variableCollections.size, 1, 'fixture has one explicit foundation collection')
  return [...graph.variableCollections.keys()][0]
}

test('real source Button becomes a linked editable component with fractional native HUG geometry', async () => {
  const snapshot = source(), before = structuredClone(snapshot)
  for (const mode of ['light', 'dark']) {
    let graph = buildFoundation(snapshot)
    const page = graph.addPage('Component conformance')
    const collection = graph.variableCollections.get(colorCollection(graph))
    graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
    const observation = await observe(snapshot, mode)
    const oldMeasurer = getTextMeasurer()
    let { master, properties } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, collection.id)
    const background = [...graph.variables.values()].find(item => item.name === '--pk-color-accent-default')
    const foreground = [...graph.variables.values()].find(item => item.name === '--pk-color-accent-on')
    assert.equal(master.boundVariables['fills/0/color'], background.id)
    assert.equal(graph.getChildren(master.id)[0].boundVariables['fills/0/color'], foreground.id)
    assert.deepEqual(graph.resolveColorVariableForNode(master.id, background.id), parseColor(observation.roots[0].style['background-color']))
    assert.equal(getTextMeasurer(), oldMeasurer, 'conversion does not replace the caller measurement hook')
    const property = properties[0]
    assert.equal(property.name, 'label')
    assert.equal(property.defaultValue, 'Save')
    close(master.width, observation.roots[0].bounds.width, 'source/master width')
    close(master.height, observation.roots[0].bounds.height, 'source/master height')
    assert.equal(master.paddingLeft, 17, 'includes the transparent CSS border inset')
    assert.equal(master.paddingTop, 9)
    const provenance = JSON.parse(master.pluginData.find(item => item.key === 'platformkit.source').value)
    assert.equal(provenance.sha256, snapshot.sha256)
    assert.equal(provenance.exampleId, primary)
    assert.equal(provenance.componentId, 'pk-ui.component.button')
    assert.deepEqual(provenance.environment, observation.environment)
    assert.equal(provenance.environment.fontHinting, 'none')
    assert.deepEqual(provenance.viewport, observation.viewport)
    let edited = graph.createInstance(master.id, page.id, { name: 'Edited native proof', x: 200 })
    let sibling = graph.createInstance(master.id, page.id, { name: 'Untouched native proof', x: 500 })
    for (const label of ['Create album', 'Retry saving']) {
      const expected = (await observe(source(label), mode)).roots[0]
      const actions = createEditor({ graph })
      actions.setCanvasKit(ck, renderer)
      const before = structuredClone([edited, ...graph.getChildren(edited.id)])
      actions.setInstanceComponentProperty(edited.id, property.id, label)
      close(edited.width, expected.bounds.width, 'edited source/native width')
      close(edited.height, expected.bounds.height, 'edited source/native height')
      actions.undoAction()
      assert.deepEqual([edited, ...graph.getChildren(edited.id)], before)
      actions.redoAction()
      close(edited.width, expected.bounds.width, 'redo width')
      const expectedPaint = parseColor(label === 'Create album' ? '#123456' : '#fedcba')
      const variable = graph.variables.get(master.boundVariables['fills/0/color'])
      const modeId = graph.getNodeVariableModeId(master.id, variable.collectionId)
      graph.addVariable({ ...variable, valuesByMode: { ...variable.valuesByMode, [modeId]: expectedPaint } })
      const encoded = await exportFigFile(graph)
      graph = await parseFigFile(encoded.slice().buffer, { populate: 'all' })
      const find = name => {
        const matches = [...graph.getAllNodes()].filter(node => node.name === name)
        assert.equal(matches.length, 1)
        return matches[0]
      }
      edited = find('Edited native proof')
      sibling = find('Untouched native proof')
      master = graph.getNode(edited.componentId)
      assert.deepEqual(JSON.parse(master.pluginData.find(item => item.key === 'platformkit.source').value), provenance)
      close(edited.width, expected.bounds.width, 'reopened width')
      close(sibling.width, observation.roots[0].bounds.width, 'unchanged sibling width')
      close(master.width, observation.roots[0].bounds.width, 'unchanged master width')
      const target = graph.getChildren(edited.id).find(node => node.componentPropertyReferences.some(ref => ref.propertyId === property.id))
      assert.equal(target.text, label)
      assert.equal(target.fontFamily, faces[0].family)
      assert.equal(target.fontWeight, 600)
      assert.equal(sibling.componentId, master.id)
      assert.equal(graph.getChildren(sibling.id)[0].text, 'Save')
      assert.equal(graph.getChildren(master.id)[0].text, 'Save')
      for (const node of [master, sibling, edited]) {
        const backgroundId = node.boundVariables['fills/0/color']
        const foregroundId = graph.getChildren(node.id)[0].boundVariables['fills/0/color']
        assert.equal(graph.variables.get(backgroundId).name, '--pk-color-accent-default')
        assert.equal(graph.variables.get(foregroundId).name, '--pk-color-accent-on')
        const resolved = graph.resolveColorVariableForNode(node.id, backgroundId)
        for (const channel of ['r', 'g', 'b', 'a']) assert.ok(Math.abs(resolved[channel] - expectedPaint[channel]) < 1e-6,
          `${mode}/${label}/${node.name}/${channel}: ${resolved[channel]} != ${expectedPaint[channel]}; mode ${graph.getNodeVariableModeId(node.id, graph.variables.get(backgroundId).collectionId)}`)
      }
      const surface = ck.MakeSurface(128, 64), draw = new SkiaRenderer(ck, surface)
      try {
        await draw.loadFonts()
        const canvas = surface.getCanvas()
        canvas.clear(ck.TRANSPARENT)
        canvas.translate(-edited.x, -edited.y)
        draw.renderSceneToCanvas(canvas, graph, edited.parentId)
        surface.flush()
        const pixel = canvas.readPixels(3, 18, { width: 1, height: 1, alphaType: ck.AlphaType.Unpremul,
          colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
        assert.deepEqual([...pixel], ['r', 'g', 'b', 'a'].map(channel => Math.round(expectedPaint[channel] * 255)))
      } finally { draw.destroy() }
    }
  }
  assert.deepEqual(snapshot, before)
})

test('unsupported native input is explicit and does not leave partial definitions in the caller graph', async () => {
  const snapshot = source(), observation = await observe(snapshot)
  const cases = [
    { ...observation, sourceSHA: '0'.repeat(64) },
    { ...observation, exampleId: 'missing' },
    { ...observation, componentId: 'another-component' },
    { ...observation, fontFaces: [] },
  ]
  for (const input of cases) {
    const graph = buildFoundation(snapshot), page = graph.addPage('Rejected')
    const before = structuredClone([...graph.getAllNodes()])
    await assert.rejects(materializeComponent(graph, page.id, snapshot, input, faces, renderer, colorCollection(graph)))
    assert.deepEqual([...graph.getAllNodes()], before)
  }
  for (const [label, id, extra] of [
    ['', primary, {}], [' ', primary, {}], [' Save ', primary, {}],
    ['Save', 'pk-ui.component.button/with-icon', {}], ['Save', primary, { fullWidth: true }],
  ]) {
    const unsupported = source(label, id, extra), captured = await observe(unsupported)
    const graph = buildFoundation(unsupported), page = graph.addPage('Rejected')
    const before = structuredClone([...graph.getAllNodes()])
    await assert.rejects(materializeComponent(graph, page.id, unsupported, captured, faces, renderer, colorCollection(graph)))
    assert.deepEqual([...graph.getAllNodes()], before)
  }
})

test('native composition refuses unbound slot groups without flattening or dropping their content', async () => {
  const snapshot = source(), observation = await observe(snapshot)
  for (const empty of [false, true]) {
    const input = structuredClone(observation), root = input.roots[0]
    const slot = { kind: 'slot', name: empty ? 'IconEnd' : 'Content', children: empty ? [] : root.children }
    root.children = empty ? [...root.children, slot] : [slot]
    const graph = buildFoundation(snapshot), page = graph.addPage('Rejected slot')
    const before = structuredClone([...graph.getAllNodes()])
    await assert.rejects(materializeComponent(graph, page.id, snapshot, input, faces, renderer, colorCollection(graph)), /named slots/)
    assert.deepEqual([...graph.getAllNodes()], before)
  }
})

test('a mismatched default-headless rendering environment rolls back native construction', async () => {
  const defaultBrowser = await chromium.launch({ headless: true, args: ['--enable-automation'] })
  try {
    const snapshot = source(), observation = await observe(snapshot, 'light', defaultBrowser)
    const graph = buildFoundation(snapshot), page = graph.addPage('Rejected font metrics')
    const before = structuredClone([...graph.getAllNodes()]), measurer = getTextMeasurer()
    await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, faces, renderer, colorCollection(graph)), /rendering environment/)
    assert.deepEqual([...graph.getAllNodes()], before)
    assert.equal(getTextMeasurer(), measurer)
    assert.equal(defaultBrowser.contexts().length, 0)
  } finally { await defaultBrowser.close() }
})

for (const fails of [false, true]) test(`font loading preserves a newer caller measurement hook, failure=${fails}`, async () => {
  const snapshot = source(), observation = await observe(snapshot)
  const graph = buildFoundation(snapshot), page = graph.addPage('Loading hook ownership')
  const before = structuredClone([...graph.getAllNodes()]), previous = getTextMeasurer()
  const entered = Promise.withResolvers(), resume = Promise.withResolvers()
  const initial = () => null, concurrent = () => null
  const loadingError = new Error('font loading failed')
  const gated = {
    async loadFonts() {
      entered.resolve()
      await resume.promise
      if (fails) throw loadingError
      await renderer.loadFonts()
    },
    measureTextNode: (...args) => renderer.measureTextNode(...args),
  }
  try {
    setTextMeasurer(initial)
    const pending = materializeComponent(graph, page.id, snapshot, observation, faces, gated, colorCollection(graph))
    await entered.promise
    setTextMeasurer(concurrent)
    resume.resolve()
    if (fails) {
      await assert.rejects(pending, error => error === loadingError)
      assert.deepEqual([...graph.getAllNodes()], before)
    } else await pending
    assert.equal(getTextMeasurer(), concurrent, 'do not restore a hook captured before an await')
  } finally { setTextMeasurer(previous) }
})

test('zero-advance text requires a working native renderer even when fallback dimensions would match', async () => {
  const snapshot = source('\u0301'), observation = await observe(snapshot)
  assert.equal(observation.roots[0].children[0].bounds.width, 0)
  const graph = buildFoundation(snapshot), page = graph.addPage('Zero-advance shaping')
  const { master } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, colorCollection(graph))
  assert.equal(graph.getChildren(master.id)[0].width, 0, 'a real zero advance remains valid')
  const destroyed = new SkiaRenderer(ck, ck.MakeSurface(100, 50))
  destroyed.destroy()
  const before = structuredClone([...graph.getAllNodes()]), previous = getTextMeasurer()
  await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, faces, destroyed, colorCollection(graph)), /native text measurement/)
  assert.deepEqual([...graph.getAllNodes()], before)
  assert.equal(getTextMeasurer(), previous)
})

test('unavailable or invalid native measurements reject without layout fallback or partial graph changes', async () => {
  const snapshot = source(), observation = await observe(snapshot)
  for (const measured of [
    null, undefined, {}, { width: NaN, height: 20 }, { width: 31, height: Infinity },
    { width: -1, height: 20 }, { width: 31, height: -1 }, { width: 31, height: 0 },
    { width: '31', height: 20 },
  ]) {
    const graph = buildFoundation(snapshot), page = graph.addPage('Rejected measurement')
    const before = structuredClone([...graph.getAllNodes()]), previous = getTextMeasurer()
    const invalid = { loadFonts: () => renderer.loadFonts(), measureTextNode: () => measured }
    await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, faces, invalid, colorCollection(graph)), /native text measurement/)
    assert.deepEqual([...graph.getAllNodes()], before)
    assert.equal(getTextMeasurer(), previous)
  }
})

test('paint binding refuses ambiguous, stale or derived inputs before graph mutation', async () => {
  const snapshot = source(), observation = await observe(snapshot)
  for (const change of [
    input => { input.collectionId = undefined },
    input => { input.graph.createVariable('--pk-color-accent-default', 'COLOR', input.collectionId, parseColor('#123456')) },
    input => { input.graph.variables.values().find(item => item.name === '--pk-color-accent-default').valuesByMode = {} },
    input => { input.observation.roots[0].style['background-color'] = 'rgb(1, 2, 3)' },
    input => { input.observation.roots[0].paintSources['background-color'].directCandidate = null },
    input => { input.observation.roots[0].paintSources['background-color'].tokens = [] },
    input => { input.graph.renameMode(input.collectionId, input.graph.variableCollections.get(input.collectionId).defaultModeId, 'other') },
  ]) {
    const graph = buildFoundation(snapshot), page = graph.addPage('Refused paint')
    const input = { graph, collectionId: colorCollection(graph), observation: structuredClone(observation) }
    change(input)
    const before = structuredClone({ nodes: [...graph.getAllNodes()], variables: [...graph.variables] })
    await assert.rejects(materializeComponent(graph, page.id, snapshot, input.observation, faces, renderer, input.collectionId),
      /paint|variable|collection|mode/)
    assert.deepEqual({ nodes: [...graph.getAllNodes()], variables: [...graph.variables] }, before)
  }
})
