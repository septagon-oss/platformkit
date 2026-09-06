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
import { parseFigBuffer } from '@open-pencil/fig'
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
    let graph = buildFoundation(snapshot).graph
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

test('source icon slots become linked editable native composition through mixed history and two saves', async () => {
  for (const [id, slotName, size] of [['with-icon', 'IconEnd', 20], ['with-leading-icon', 'IconStart', 16]]) {
    for (const mode of ['light', 'dark']) {
      const exampleId = `pk-ui.component.button/${id}`, snapshot = source('Save', exampleId)
      const built = buildFoundation(snapshot), page = built.graph.addPage('Source composition')
      let graph = built.graph
      graph.updateNode(page.id, { variableModes: { [built.collection.id]: built.collection.modes.find(item => item.name === mode).modeId } })
      const observation = await observe(snapshot, mode)
      const region = observation.roots[0].children.find(child => child.kind === 'slot')
      const result = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, built.collection.id,
        [{ region, master: built.icons.get(region.children[0].icon.canonicalName) }])
      let master = result.master
      const labelProperty = result.properties.find(property => property.name === 'label')
      const slotProperty = result.properties.find(property => property.name === slotName)
      assert.equal(slotProperty.type, 'INSTANCE_SWAP')
      assert.equal(slotProperty.defaultValue, built.icons.get('plus').id)
      let edited = graph.createInstance(master.id, page.id, { name: 'Edited source composition', x: 250 })
      let sibling = graph.createInstance(master.id, page.id, { name: 'Untouched source composition', x: 600 })
      const target = node => graph.getChildren(node.id).find(child => child.componentPropertyReferences.some(ref => ref.propertyId === slotProperty.id))
      const check = (node, expected, glyph) => {
        close(node.width, expected.bounds.width, 'composed width')
        close(node.height, expected.bounds.height, 'composed height')
        const icon = target(node), svg = expected.children.find(child => child.kind === 'slot').children[0]
        assert.equal(icon.type, 'INSTANCE')
        assert.equal(graph.getNode(icon.componentId).name, glyph)
        close(icon.x, svg.bounds.x - expected.bounds.x, 'slot x')
        close(icon.y, svg.bounds.y - expected.bounds.y, 'slot y')
        close(icon.width, size, 'slot width')
        close(icon.height, size, 'slot height')
        for (const vector of graph.getChildren(icon.id)) {
          assert.equal(graph.variables.get(vector.boundVariables['fills/0/color']).name, svg.paintSources.fill.directCandidate)
        }
      }
      check(master, observation.roots[0], 'plus')
      const actions = createEditor({ graph })
      actions.setCanvasKit(ck, renderer)
      let labelValue = 'Create album', changed = await observe(source(labelValue, exampleId), mode)
      actions.setInstanceComponentProperty(edited.id, labelProperty.id, labelValue)
      actions.setInstanceComponentProperty(edited.id, slotProperty.id, built.icons.get('x').id)
      await Promise.resolve()
      check(edited, changed.roots[0], 'x')
      actions.undoAction()
      actions.undoAction()
      await Promise.resolve()
      check(edited, observation.roots[0], 'plus')
      actions.redoAction()
      actions.redoAction()
      await Promise.resolve()
      check(edited, changed.roots[0], 'x')
      for (let cycle = 0; cycle < 3; cycle++) {
        check(edited, changed.roots[0], 'x')
        check(sibling, observation.roots[0], 'plus')
        check(master, observation.roots[0], 'plus')
        const label = graph.getChildren(edited.id).find(child => child.type === 'TEXT')
        assert.equal(label.text, labelValue)
        assert.equal(graph.getChildren(sibling.id).find(child => child.type === 'TEXT').text, 'Save')
        assert.equal(edited.componentId, master.id)
        if (cycle < 2) {
          const bytes = await exportFigFile(graph)
          const raw = parseFigBuffer(bytes.slice().buffer).nodeChanges
          const rawEdited = raw.find(node => node.name === edited.name)
          const rawMaster = raw.find(node => node.type === 'SYMBOL' && node.name === master.name)
          const [sessionID, localID] = slotProperty.id.split(':').map(Number)
          const occurrences = raw.filter(node => node.type === 'INSTANCE' &&
            JSON.stringify(node.parentIndex.guid) === JSON.stringify(rawMaster.guid) &&
            node.componentPropRefs.some(ref => ref.defID.sessionID === sessionID && ref.defID.localID === localID))
          assert.equal(occurrences.length, 1, 'one exact source icon occurrence in the file')
          const path = JSON.stringify({ guids: [occurrences[0].guid] })
          const derived = rawEdited.derivedSymbolData?.filter(entry => JSON.stringify(entry.guidPath) === path) ?? []
          assert.equal(derived.length, 1, 'one fresh derived layout entry, before any importer runs')
          const icon = target(edited)
          assert.deepEqual(derived[0].size, { x: Math.fround(icon.width), y: Math.fround(icon.height) })
          const transform = { m00: 1, m01: 0, m02: icon.x, m10: 0, m11: 1, m12: icon.y }
          for (const [field, value] of Object.entries(transform)) {
            assert.ok(Math.abs(derived[0].transform[field] - Math.fround(value)) < 1e-6, `persisted icon ${field}`)
          }
          assert.equal(rawEdited.symbolData.symbolOverrides.some(entry =>
            JSON.stringify(entry.guidPath) === path && Object.hasOwn(entry, 'transform')), false)
          graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
          edited = [...graph.getAllNodes()].find(node => node.name === 'Edited source composition')
          sibling = [...graph.getAllNodes()].find(node => node.name === 'Untouched source composition')
          master = graph.getNode(edited.componentId)
          check(edited, changed.roots[0], 'x')
          labelValue = cycle === 0 ? 'Retry saving' : 'Create album again'
          changed = await observe(source(labelValue, exampleId), mode)
          const reopenedActions = createEditor({ graph })
          reopenedActions.setCanvasKit(ck, renderer)
          reopenedActions.setInstanceComponentProperty(edited.id, labelProperty.id, labelValue)
          await Promise.resolve()
        }
      }
    }
  }
})

test('icon composition refuses forged identities, altered geometry and unsupported presentation without leftovers', async () => {
  const snapshot = source('Save', 'pk-ui.component.button/with-icon'), observation = await observe(snapshot)
  for (const mutate of [
    input => { input.targets[0].region = structuredClone(input.targets[0].region) },
    input => { input.targets.push(input.targets[0]) },
    input => { input.targets[0].master = { ...input.targets[0].master } },
    input => { input.targets[0].master = input.built.icons.get('x') },
    input => { input.graph.getChildren(input.targets[0].master.id)[0].vectorNetwork.vertices[0].x += 1 },
    input => { input.svg.children[0].attributes.d = 'M0 0L20 20' },
    input => { input.svg.children[0].style.d = 'path("M0 0L20 20")' },
    input => { input.svg.children[0].style.filter = 'blur(1px)' },
    input => { input.svg.children[0].style.transform = 'matrix(1, 0, 0, 1, 1, 0)' },
    input => { input.svg.children[0].style.opacity = '0.5' },
    input => { input.svg.children[0].style.stroke = 'rgb(0, 0, 0)' },
    input => { input.svg.children[0].style['stroke-dasharray'] = '2px, 2px' },
    input => { input.svg.children[0].paintSources.fill = { tokens: [], directCandidate: null } },
    input => { input.source.examples[0].slots.find(slot => slot.name === 'IconEnd').supported = false },
    input => { input.targets[0].region.name = 'iconEnd' },
  ]) {
    const built = buildFoundation(snapshot), captured = structuredClone(observation), selected = captured.roots[0].children.find(child => child.kind === 'slot')
    const input = { built, graph: built.graph, source: structuredClone(snapshot), observation: captured, svg: selected.children[0],
      targets: [{ region: selected, master: built.icons.get('plus') }] }
    mutate(input)
    const page = input.graph.addPage('Rejected icon'), before = structuredClone([...input.graph.getAllNodes()])
    const hook = getTextMeasurer()
    await assert.rejects(materializeComponent(input.graph, page.id, input.source, input.observation, faces, renderer,
      built.collection.id, input.targets))
    assert.deepEqual([...input.graph.getAllNodes()], before)
    assert.equal(getTextMeasurer(), hook)
  }
})

for (const rule of ['outline: 4px solid red', 'filter: opacity(0.5)']) test(`source root effects fail closed: ${rule}`, async () => {
  const snapshot = source('Save', 'pk-ui.component.button/with-icon')
  snapshot.css += `\nbutton[data-component="button"] { ${rule}; }`
  const observation = await observe(snapshot), built = buildFoundation(snapshot)
  const page = built.graph.addPage('Rejected source effect')
  const region = observation.roots[0].children.find(child => child.kind === 'slot')
  const before = structuredClone([...built.graph.getAllNodes()]), hook = getTextMeasurer()
  await assert.rejects(materializeComponent(built.graph, page.id, snapshot, observation, faces, renderer,
    built.collection.id, [{ region, master: built.icons.get('plus') }]), /outline|filter/i)
  assert.deepEqual([...built.graph.getAllNodes()], before)
  assert.equal(getTextMeasurer(), hook)
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
    const graph = buildFoundation(snapshot).graph, page = graph.addPage('Rejected')
    const before = structuredClone([...graph.getAllNodes()])
    await assert.rejects(materializeComponent(graph, page.id, snapshot, input, faces, renderer, colorCollection(graph)))
    assert.deepEqual([...graph.getAllNodes()], before)
  }
  for (const [label, id, extra] of [
    ['', primary, {}], [' ', primary, {}], [' Save ', primary, {}],
    ['Save', 'pk-ui.component.button/with-icon', {}], ['Save', primary, { fullWidth: true }],
  ]) {
    const unsupported = source(label, id, extra), captured = await observe(unsupported)
    const graph = buildFoundation(unsupported).graph, page = graph.addPage('Rejected')
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
    const graph = buildFoundation(snapshot).graph, page = graph.addPage('Rejected slot')
    const before = structuredClone([...graph.getAllNodes()])
    await assert.rejects(materializeComponent(graph, page.id, snapshot, input, faces, renderer, colorCollection(graph)), /named slots/)
    assert.deepEqual([...graph.getAllNodes()], before)
  }
})

test('a mismatched default-headless rendering environment rolls back native construction', async () => {
  const defaultBrowser = await chromium.launch({ headless: true, args: ['--enable-automation'] })
  try {
    const snapshot = source(), observation = await observe(snapshot, 'light', defaultBrowser)
    const graph = buildFoundation(snapshot).graph, page = graph.addPage('Rejected font metrics')
    const before = structuredClone([...graph.getAllNodes()]), measurer = getTextMeasurer()
    await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, faces, renderer, colorCollection(graph)), /rendering environment/)
    assert.deepEqual([...graph.getAllNodes()], before)
    assert.equal(getTextMeasurer(), measurer)
    assert.equal(defaultBrowser.contexts().length, 0)
  } finally { await defaultBrowser.close() }
})

for (const fails of [false, true]) test(`font loading preserves a newer caller measurement hook, failure=${fails}`, async () => {
  const snapshot = source(), observation = await observe(snapshot)
  const graph = buildFoundation(snapshot).graph, page = graph.addPage('Loading hook ownership')
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
  const graph = buildFoundation(snapshot).graph, page = graph.addPage('Zero-advance shaping')
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
    const graph = buildFoundation(snapshot).graph, page = graph.addPage('Rejected measurement')
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
    const graph = buildFoundation(snapshot).graph, page = graph.addPage('Refused paint')
    const input = { graph, collectionId: colorCollection(graph), observation: structuredClone(observation) }
    change(input)
    const before = structuredClone({ nodes: [...graph.getAllNodes()], variables: [...graph.variables] })
    await assert.rejects(materializeComponent(graph, page.id, snapshot, input.observation, faces, renderer, input.collectionId),
      /paint|variable|collection|mode/)
    assert.deepEqual({ nodes: [...graph.getAllNodes()], variables: [...graph.variables] }, before)
  }
})
