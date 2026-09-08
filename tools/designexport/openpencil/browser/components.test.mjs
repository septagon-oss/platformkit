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
import { SceneGraph } from '@open-pencil/scene-graph'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { bindComponentVariants } from '../bindings.mjs'
import { verifyComponentDocument } from '../document.mjs'
import { associateSourceInstance, extractSourceProps } from '../source-changes.mjs'
import { chain } from '../exporter-correction.mjs'
import { captureExample } from './capture.mjs'
import { computedColor } from '../computed-color.mjs'

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

test('native source proposals reproject through Go after two FIG saves', async () => {
  const run = (args, input) => JSON.parse(execFileSync('go', ['run', './tools/designexport', ...args], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
    input: input === undefined ? undefined : JSON.stringify(input),
  }))
  const snapshot = run([]), beforeSource = structuredClone(snapshot)
  let graph = buildFoundation(snapshot).graph
  const page = graph.addPage('Source reprojection')
  const observation = await captureExample(browser, snapshot, primary, { fonts: faces })
  let { master, properties } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, colorCollection(graph))
  let instance = graph.createInstance(master.id, page.id, { name: 'Editable source occurrence', x: 250 })
  const sibling = graph.createInstance(master.id, page.id, { name: 'Unmapped source preview', x: 500 })
  associateSourceInstance(graph, instance, snapshot, [primary])
  assert.equal(extractSourceProps(graph, sibling, snapshot).code, 'missing-binding')
  for (const label of ['Create album', 'Retry saving']) {
    graph.updateNode(master.id, { componentPropertyDefinitions: properties.map(item => ({ ...item, name: 'Renamed in the editor' })) })
    const actions = createEditor({ graph })
    actions.setCanvasKit(ck, renderer)
    actions.setInstanceComponentProperty(instance.id, properties[0].id, label)
    const bytes = await exportFigFile(graph)
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    instance = [...graph.getAllNodes()].find(node => node.type === 'INSTANCE' && node.pluginData.some(item =>
      item.pluginId === 'platformkit' && item.key === 'platformkit.source'))
    master = graph.getNode(instance.componentId)
    const before = structuredClone([...graph.getAllNodes()]), result = extractSourceProps(graph, instance, snapshot)
    assert.equal(result.status, 'proposal')
    assert.deepEqual(result.proposal, { baseSHA256: snapshot.sha256, path: [primary], props: { label } })
    const projected = run(['--proposal'], result.proposal)
    assert.notEqual(projected.sha256, snapshot.sha256)
    assert.equal(projected.examples.length, snapshot.examples.length)
    assert.equal(projected.examples.find(example => example.id === primary).props.label, label)
    assert.deepEqual(projected.examples.filter(example => example.id !== primary), snapshot.examples.filter(example => example.id !== primary))
    const observed = await captureExample(browser, projected, primary, { fonts: faces })
    close(instance.width, observed.roots[0].bounds.width, 'native edit/source reprojection width')
    close(instance.height, observed.roots[0].bounds.height, 'native edit/source reprojection height')
    assert.deepEqual([...graph.getAllNodes()], before, 'extraction and Go reprojection are read-only for the native document')
  }
  assert.deepEqual(snapshot, beforeSource)
})

test('real source variant families retain shared copy, native geometry and typed Go proposals through two saves', async () => {
  const run = (args, input) => JSON.parse(execFileSync('go', ['run', './tools/designexport', ...args], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
    input: input === undefined ? undefined : JSON.stringify(input),
  }))
  const snapshot = run([]), tones = ['neutral', 'info', 'danger'], baseline = structuredClone(snapshot)
  for (const mode of ['light', 'dark']) {
    let graph = buildFoundation(snapshot).graph
    const page = graph.addPage('Source variant proof'), collection = graph.variableCollections.get(colorCollection(graph))
    graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
    const owner = graph.createNode('COMPONENT_SET', page.id, { name: 'Source family' }), variants = []
    for (const tone of tones) {
      const projected = tone === 'neutral' ? snapshot : run(['--proposal'], { baseSHA256: snapshot.sha256, path: [primary], props: { tone } })
      const observation = await captureExample(browser, projected, primary, { mode, fonts: faces })
      const built = await materializeComponent(graph, page.id, projected, observation, faces, renderer, collection.id)
      variants.push({ snapshot: projected, master: built.master })
    }
    const definitions = bindComponentVariants(graph, owner, snapshot, primary, 'tone', variants)
    let instance = graph.createInstance(variants[0].master.id, page.id, { name: 'Editable family' })
    graph.createInstance(variants[0].master.id, page.id, { name: 'Unchanged family preview' })
    associateSourceInstance(graph, instance, snapshot, [primary])
    const correspondence = verifyComponentDocument(graph, snapshot, [primary])
    assert.equal(correspondence[0].family.bindingVersion, 2)
    let baselineGraph = graph
    for (let cycle = 0; cycle < 2; cycle++) {
      baselineGraph = await parseFigFile((await exportFigFile(baselineGraph)).slice().buffer, { populate: 'all' })
      verifyComponentDocument(baselineGraph, snapshot, [primary], correspondence)
    }
    const actions = createEditor({ graph })
    actions.setCanvasKit(ck, renderer)
    try {
      const label = definitions.find(item => item.type === 'TEXT'), tone = definitions.find(item => item.type === 'VARIANT')
      actions.setInstanceComponentProperty(instance.id, label.id, 'Publish album')
      for (const selected of ['info', 'danger']) {
        actions.setInstanceComponentProperty(instance.id, tone.id, selected)
        const result = extractSourceProps(graph, instance, snapshot)
        assert.equal(result.status, 'proposal', JSON.stringify(result))
        assert.deepEqual(result.proposal, { baseSHA256: snapshot.sha256, path: [primary], props: { tone: selected, label: 'Publish album' } })
        const projected = run(['--proposal'], result.proposal)
        const expected = await captureExample(browser, projected, primary, { mode, fonts: faces })
        close(instance.width, expected.roots[0].bounds.width, 'variant with edited copy width')
        close(instance.height, expected.roots[0].bounds.height, 'variant with edited copy height')
        const actualColor = graph.resolveColorVariableForNode(instance.id, instance.boundVariables['fills/0/color'])
        const expectedColor = parseColor(expected.roots[0].style['background-color'])
        for (const channel of ['r', 'g', 'b', 'a']) assert.ok(Math.abs(actualColor[channel] - expectedColor[channel]) < 1e-6,
          'projected color survives FIG float32 storage')
        actions.undo.undo()
        assert.equal(actions.getInstanceComponentPropertyValue(instance.id, tone), selected === 'info' ? 'neutral' : 'info')
        close(instance.width, expected.roots[0].bounds.width, 'undo retains edited copy width')
        actions.undo.redo()
        assert.deepEqual(extractSourceProps(graph, instance, snapshot).proposal, result.proposal)
        close(instance.width, expected.roots[0].bounds.width, 'redo retains edited copy width')
        for (let cycle = 0; cycle < 2; cycle++) {
          graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
          actions.replaceGraph(graph)
          instance = [...graph.getAllNodes()].find(node => node.name === 'Editable family')
          assert.deepEqual(extractSourceProps(graph, instance, snapshot).proposal, result.proposal)
          close(instance.width, expected.roots[0].bounds.width, 'reopened variant width')
          const preview = [...graph.getAllNodes()].find(node => node.name === 'Unchanged family preview')
          assert.equal(graph.getChildren(preview.id)[0].text, 'Save')
        }
      }
    } finally { actions.replaceGraph(new SceneGraph()) }
  }
  assert.deepEqual(snapshot, baseline)
})

for (const [variant, backgroundName, foregroundName, padding] of [
  ['primary', '--pk-color-accent-default', '--pk-color-accent-on', [17, 9]],
  ['secondary', '--pk-color-surface-primary', '--pk-color-text-primary', [13, 7]],
]) test(`real source ${variant} Button retains linked paints, editable properties and fractional HUG geometry`, async () => {
  const id = `pk-ui.component.button/${variant}`, snapshot = source('Save', id), before = structuredClone(snapshot)
  for (const mode of ['light', 'dark']) {
    let graph = buildFoundation(snapshot).graph
    const page = graph.addPage('Component conformance')
    const collection = graph.variableCollections.get(colorCollection(graph))
    graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
    const observation = await observe(snapshot, mode)
    const oldMeasurer = getTextMeasurer()
    let { master, properties } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, collection.id)
    const background = [...graph.variables.values()].find(item => item.name === backgroundName)
    const foreground = [...graph.variables.values()].find(item => item.name === foregroundName)
    assert.equal(master.boundVariables['fills/0/color'], background.id)
    assert.equal(graph.getChildren(master.id)[0].boundVariables['fills/0/color'], foreground.id)
    assert.deepEqual(graph.resolveColorVariableForNode(master.id, background.id), parseColor(observation.roots[0].style['background-color']))
    assert.equal(getTextMeasurer(), oldMeasurer, 'conversion does not replace the caller measurement hook')
    const property = properties[0]
    assert.equal(property.name, 'label')
    assert.equal(property.defaultValue, 'Save')
    close(master.width, observation.roots[0].bounds.width, 'source/master width')
    close(master.height, observation.roots[0].bounds.height, 'source/master height')
    assert.deepEqual([master.paddingLeft, master.paddingTop], padding, 'CSS border space contributes once to native padding')
    assert.equal(master.strokes.length, variant === 'secondary' ? 1 : 0)
    if (variant === 'secondary') {
      assert.deepEqual([master.strokes[0].weight, master.strokes[0].align], [1, 'INSIDE'])
      const borderId = master.boundVariables['strokes/0/color']
      assert.equal(graph.variables.get(borderId).name, '--pk-color-border-default')
      assert.deepEqual(graph.resolveColorVariableForNode(master.id, borderId), parseColor(observation.roots[0].style['border-top-color']))
    }
    const provenance = JSON.parse(master.pluginData.find(item => item.key === 'platformkit.source').value)
    assert.equal(provenance.sha256, snapshot.sha256)
    assert.equal(provenance.exampleId, id)
    assert.equal(provenance.componentId, 'pk-ui.component.button')
    assert.deepEqual(provenance.environment, observation.environment)
    assert.equal(provenance.environment.fontHinting, 'none')
    assert.deepEqual(provenance.viewport, observation.viewport)
    let edited = graph.createInstance(master.id, page.id, { name: 'Edited native proof', x: 200 })
    let sibling = graph.createInstance(master.id, page.id, { name: 'Untouched native proof', x: 500 })
    for (const label of ['Create album', 'Retry saving']) {
      const expected = (await observe(source(label, id), mode)).roots[0]
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
      const expectedBorder = parseColor(label === 'Create album' ? '#abcdef' : '#456789')
      if (variant === 'secondary') {
        const border = graph.variables.get(master.boundVariables['strokes/0/color'])
        graph.addVariable({ ...border, valuesByMode: { ...border.valuesByMode, [modeId]: expectedBorder } })
      }
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
        assert.equal(graph.variables.get(backgroundId).name, backgroundName)
        assert.equal(graph.variables.get(foregroundId).name, foregroundName)
        if (variant === 'secondary') {
          assert.deepEqual([node.strokes[0].weight, node.strokes[0].align], [1, 'INSIDE'])
          assert.equal(graph.variables.get(node.boundVariables['strokes/0/color']).name, '--pk-color-border-default')
        }
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
        if (variant === 'secondary') {
          for (const [x, y] of [[0, 15], [20, 0]]) {
            const border = canvas.readPixels(x, y, { width: 1, height: 1, alphaType: ck.AlphaType.Unpremul,
              colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
            assert.deepEqual([...border], ['r', 'g', 'b', 'a'].map(channel => Math.round(expectedBorder[channel] * 255)))
          }
        }
      } finally { draw.destroy() }
    }
  }
  assert.deepEqual(snapshot, before)
})

test('transparent direct tokens keep source precision, live mode edits, history and two saves', async () => {
  const snapshot = source(), backgroundName = '--pk-color-accent-default', foregroundName = '--pk-color-accent-on'
  const palettes = [['light', 128, 64], ['dark', 191, 0]]
  for (const [mode, alpha, foregroundAlpha] of palettes) {
    const values = [[backgroundName, `#204060${alpha.toString(16).padStart(2, '0')}`],
      [foregroundName, `#604020${foregroundAlpha.toString(16).padStart(2, '0')}`]]
    for (const [name, value] of values) snapshot.themes.find(theme => theme.mode === mode).tokens.find(token => token.name === name).value = value
    snapshot.css += `\n:root[data-theme="${mode}"] { ${values.map(([name, value]) => `${name}: ${value};`).join(' ')} }`
  }
  const beforeSource = structuredClone(snapshot)
  const expectColor = (actual, expected) => {
    for (const channel of ['r', 'g', 'b', 'a']) assert.ok(Math.abs(actual[channel] - expected[channel]) <= 1e-6,
      `${channel}: ${actual[channel]} must retain ${expected[channel]}`)
  }
  for (const [mode, alpha, foregroundAlpha] of palettes) {
    const observation = await observe(snapshot, mode)
    let { graph, collection } = buildFoundation(snapshot)
    const page = graph.addPage('Transparent palette')
    graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
    const { master } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, collection.id)
    graph.createInstance(master.id, page.id, { name: 'First transparent occurrence' })
    graph.createInstance(master.id, page.id, { name: 'Second transparent occurrence', x: 200 })
    let expected = { r: 32 / 255, g: 64 / 255, b: 96 / 255, a: alpha / 255 }
    const foreground = { r: 96 / 255, g: 64 / 255, b: 32 / 255, a: foregroundAlpha / 255 }
    expectColor(master.fills[0].color, expected)
    expectColor(graph.getChildren(master.id)[0].fills[0].color, foreground)
    for (const value of ['rgba(32, 64, 96, 0.49)',
      `color(srgb ${32 / 255} ${64 / 255} ${96 / 255} / 0.5)`]) {
      const altered = structuredClone(observation), before = structuredClone([...graph.getAllNodes()])
      altered.roots[0].style['background-color'] = value
      await assert.rejects(materializeComponent(graph, page.id, snapshot, altered, faces, renderer, collection.id),
        /observed paint differs/, 'a different alpha byte or fractional modern alpha is not legacy serialization noise')
      assert.deepEqual([...graph.getAllNodes()], before)
    }
    for (let cycle = 0; cycle < 3; cycle++) {
      const first = [...graph.getAllNodes()].find(node => node.name === 'First transparent occurrence')
      const second = [...graph.getAllNodes()].find(node => node.name === 'Second transparent occurrence')
      const definition = graph.getNode(first.componentId)
      assert.equal(second.componentId, definition.id)
      for (const root of [definition, first, second]) {
        const text = graph.getChildren(root.id)[0]
        assert.equal(text.text, 'Save', 'palette edits do not change source properties')
        for (const [node, name, paint] of [[root, backgroundName, expected], [text, foregroundName, foreground]]) {
          const id = node.boundVariables['fills/0/color']
          assert.equal(graph.variables.get(id).name, name)
          expectColor(graph.resolveColorVariableForNode(node.id, id), paint)
          assert.equal(node.fills[0].opacity, 1, 'do not apply token alpha twice')
        }
      }
      if (cycle === 2) break
      const variable = graph.variables.get(first.boundVariables['fills/0/color'])
      const modeId = graph.getNodeVariableModeId(first.id, variable.collectionId)
      const actions = createEditor({ graph }), before = structuredClone(variable.valuesByMode)
      const nodes = structuredClone([...graph.getAllNodes()])
      const edited = { r: .2, g: .4, b: .6, a: cycle === 0 ? .123456 : 0 }
      actions.updateVariableValue(variable.id, modeId, edited)
      expectColor(graph.resolveColorVariableForNode(first.id, variable.id), edited)
      actions.undoAction()
      assert.deepEqual(variable.valuesByMode, before)
      actions.redoAction()
      expectColor(graph.resolveColorVariableForNode(first.id, variable.id), edited)
      for (const [otherMode, value] of Object.entries(before)) {
        if (otherMode !== modeId) assert.deepEqual(variable.valuesByMode[otherMode], value, 'other modes stay unchanged')
      }
      assert.deepEqual([...graph.getAllNodes()], nodes, 'palette history does not rewrite masters or either occurrence')
      expected = edited
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  }
  assert.deepEqual(snapshot, beforeSource)
})

test('native glyph swaps agree with nested source property edits without changing icon size or tone', async () => {
  const run = (args, input) => JSON.parse(execFileSync('go', ['run', './tools/designexport', ...args], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
    input: input === undefined ? undefined : JSON.stringify(input),
  }))
  const snapshot = run([]), before = structuredClone(snapshot)
  for (const id of ['pk-ui.component.button/with-icon', 'pk-ui.component.button/with-leading-icon']) {
    const example = snapshot.examples.find(item => item.id === id), child = example.children[0]
    assert.deepEqual([child.description.id, child.description.componentId], ['icon', 'pk-ui.component.icon'])
    const projected = run(['--proposal'], { baseSHA256: snapshot.sha256, path: [id, child.description.id], props: { name: 'x' } })
    const changed = projected.examples.find(item => item.id === id)
    assert.deepEqual(changed.props, example.props, 'the containing Button properties remain unchanged')
    assert.deepEqual(changed.children[0].description.props, { ...child.description.props, name: 'x' })
    assert.deepEqual(projected.examples.filter(item => item.id !== id), snapshot.examples.filter(item => item.id !== id))
    for (const mode of ['light', 'dark']) {
      let { graph, collection, icons } = buildFoundation(snapshot)
      const page = graph.addPage('Nested source icon proof')
      graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
      const observation = await captureExample(browser, snapshot, id, { mode, fonts: faces })
      const region = observation.roots[0].children.find(item => item.kind === 'slot')
      const { master, properties } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, collection.id,
        [{ region, master: icons.get('plus') }])
      let instance = graph.createInstance(master.id, page.id)
      const property = properties.find(item => item.type === 'INSTANCE_SWAP')
      const actions = createEditor({ graph })
      actions.setCanvasKit(ck, renderer)
      actions.setInstanceComponentProperty(instance.id, property.id, icons.get('x').id)
      const expected = await captureExample(browser, projected, id, { mode, fonts: faces })
      const svg = expected.roots[0].children.find(item => item.kind === 'slot').children[0]
      assert.equal(svg.icon.canonicalName, 'x', 'comparison is against the newly rendered source glyph')
      for (let save = 0; save < 2; save++) {
        const bytes = await exportFigFile(graph)
        graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
        instance = [...graph.getAllNodes()].find(node => node.type === 'INSTANCE' && node.componentPropertyAssignments[property.id])
        const native = graph.getChildren(instance.id).find(node => node.componentPropertyReferences.some(ref => ref.propertyId === property.id))
        assert.equal(JSON.parse(graph.getNode(native.componentId).pluginData.find(item => item.key === 'platformkit.icon').value).name, 'x')
        for (const field of ['width', 'height']) close(instance[field], expected.roots[0].bounds[field], `reopened Button ${field}`)
        for (const field of ['width', 'height', 'x', 'y']) close(native[field],
          svg.bounds[field] - (['x', 'y'].includes(field) ? expected.roots[0].bounds[field] : 0), `reopened icon ${field}`)
      }
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
      associateSourceInstance(graph, edited, snapshot, [exampleId])
      const target = node => graph.getChildren(node.id).find(child => child.componentPropertyReferences.some(ref => ref.propertyId === slotProperty.id))
      const check = (node, expected, glyph) => {
        close(node.width, expected.bounds.width, 'composed width')
        close(node.height, expected.bounds.height, 'composed height')
        const icon = target(node), svg = expected.children.find(child => child.kind === 'slot').children[0]
        assert.equal(icon.type, 'INSTANCE')
        assert.equal(chain(graph, icon, 'componentId').at(-1).name, glyph)
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
      assert.deepEqual(extractSourceProps(graph, edited, snapshot).proposal,
        { baseSHA256: snapshot.sha256, path: [exampleId], props: { label: labelValue } })
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
        assert.equal(extractSourceProps(graph, edited, snapshot).status, 'unsupported', 'slot changes are not silently dropped from a proposal')
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

test('initially transparent token borders retain live strokes through mode edits, history and two saves', async () => {
  const snapshot = source(), tokenName = '--pk-color-border-default'
  for (const theme of snapshot.themes) {
    theme.tokens.find(token => token.name === tokenName).value = '#abcdef00'
    snapshot.css += `\n:root[data-theme="${theme.mode}"] { ${tokenName}: #abcdef00; }`
  }
  snapshot.css += `\n[data-component="button"] { border-color: var(${tokenName}); }`
  for (const mode of ['light', 'dark']) {
    const observation = await observe(snapshot, mode)
    let { graph, collection } = buildFoundation(snapshot)
    const page = graph.addPage('Initially transparent stroke')
    graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
    const { master } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, collection.id)
    graph.createInstance(master.id, page.id, { name: 'Live stroke occurrence' })
    for (let cycle = 0; cycle < 3; cycle++) {
      const instance = [...graph.getAllNodes()].find(node => node.name === 'Live stroke occurrence')
      const definition = graph.getNode(instance.componentId)
      for (const node of [definition, instance]) {
        assert.equal(node.strokes.length, 1, 'a transparent token border must still have a native stroke')
        const id = node.boundVariables['strokes/0/color']
        assert.equal(graph.variables.get(id).name, tokenName)
        assert.equal(node.strokes[0].opacity, 1)
        assert.deepEqual([node.strokes[0].weight, node.strokes[0].align], [1, 'INSIDE'])
        const color = graph.resolveColorVariableForNode(node.id, id)
        assert.ok(Math.abs(color.a - (cycle === 0 ? 0 : cycle === 1 ? .6 : .2)) < 1e-6)
        close(node.width, observation.roots[0].bounds.width, 'unchanged source width')
      }
      if (cycle === 2) break
      const variable = graph.variables.get(instance.boundVariables['strokes/0/color'])
      const modeId = graph.getNodeVariableModeId(instance.id, variable.collectionId)
      const before = structuredClone(variable.valuesByMode), actions = createEditor({ graph })
      actions.updateVariableValue(variable.id, modeId, { r: .4, g: .6, b: .8, a: cycle === 0 ? .6 : .2 })
      actions.undoAction()
      assert.deepEqual(variable.valuesByMode, before)
      actions.redoAction()
      for (const [otherMode, color] of Object.entries(before)) {
        if (otherMode !== modeId) assert.deepEqual(variable.valuesByMode[otherMode], color)
      }
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  }
})

test('literal solid borders use observed width and paint instead of a component-specific preset', async () => {
  const snapshot = source('Save', 'pk-ui.component.button/secondary')
  snapshot.css += '\nbutton[data-component="button"] { border: 3px solid rgb(17, 34, 51); border-radius: 0; }'
  const observation = await observe(snapshot), built = buildFoundation(snapshot), page = built.graph.addPage('Literal border')
  let graph = built.graph
  const { master } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, built.collection.id)
  graph.createInstance(master.id, page.id, { name: 'Literal bordered instance', x: 200 })
  for (let save = 0; save < 3; save++) {
    const instance = [...graph.getAllNodes()].find(node => node.name === 'Literal bordered instance')
    assert.deepEqual([instance.paddingLeft, instance.paddingTop], [15, 9])
    assert.equal(instance.strokes.length, 1)
    assert.deepEqual([instance.strokes[0].weight, instance.strokes[0].align], [3, 'INSIDE'])
    assert.equal(instance.boundVariables['strokes/0/color'], undefined, 'literal strokes never invent a token binding')
    close(instance.width, observation.roots[0].bounds.width, 'literal border width')
    close(instance.height, observation.roots[0].bounds.height, 'literal border height')
    const surface = ck.MakeSurface(100, 60), draw = new SkiaRenderer(ck, surface)
    try {
      await draw.loadFonts()
      const canvas = surface.getCanvas()
      canvas.clear(ck.TRANSPARENT)
      canvas.translate(-instance.x, -instance.y)
      draw.renderSceneToCanvas(canvas, graph, instance.parentId)
      surface.flush()
      for (const [x, y] of [[0, 0], [2, 15], [20, 2]]) {
        const pixel = canvas.readPixels(x, y, { width: 1, height: 1, alphaType: ck.AlphaType.Unpremul,
          colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
        assert.deepEqual([...pixel], [17, 34, 51, 255])
      }
    } finally { draw.destroy() }
    if (save < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
})

test('text-row borders reject unsupported styles and inconsistent aliases without partial graph changes', async () => {
  const snapshot = source('Save', 'pk-ui.component.button/secondary'), observation = await observe(snapshot)
  for (const change of [
    root => { root.style['border-top-style'] = 'dashed' },
    root => { root.style['border-left-width'] = '2px' },
    root => { root.style['border-right-color'] = 'rgb(1, 2, 3)' },
    root => { root.paintSources['border-left-color'] = { tokens: [], directCandidate: null } },
    root => { root.paintSources['border-top-color'].directCandidate = null },
  ]) {
    const input = structuredClone(observation), { graph, collection } = buildFoundation(snapshot)
    change(input.roots[0])
    const page = graph.addPage('Refused border'), before = structuredClone({ nodes: [...graph.getAllNodes()], variables: [...graph.variables] })
    const hook = getTextMeasurer()
    await assert.rejects(materializeComponent(graph, page.id, snapshot, input, faces, renderer, collection.id), /border|paint/)
    assert.deepEqual({ nodes: [...graph.getAllNodes()], variables: [...graph.variables] }, before)
    assert.equal(getTextMeasurer(), hook)
  }
})

test('computed sRGB literal fills and text retain alpha and precision through two native saves', async () => {
  // Deliberate CSS fixtures exercise paint transport, not Go-source freshness.
  const snapshot = source('Save')
  snapshot.css += '\nbutton[data-component="button"] { background-color: color(srgb .125 .375 .625 / .5); color: color(srgb .9 .4 .2 / .75); }'
  for (const mode of ['light', 'dark']) {
    const observation = await observe(snapshot, mode), built = buildFoundation(snapshot)
    let { graph } = built
    const page = graph.addPage('Literal sRGB')
    graph.updateNode(page.id, { variableModes: { [built.collection.id]: built.collection.modes.find(item => item.name === mode).modeId } })
    const { master } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, built.collection.id)
    graph.createInstance(master.id, page.id, { name: 'Literal sRGB instance' })
    for (let cycle = 0; cycle < 3; cycle++) {
      const instance = [...graph.getAllNodes()].find(node => node.name === 'Literal sRGB instance')
      for (const [node, expected] of [[instance, { r: .125, g: .375, b: .625, a: .5 }],
        [graph.getChildren(instance.id)[0], { r: .9, g: .4, b: .2, a: .75 }]]) {
        assert.equal(node.boundVariables['fills/0/color'], undefined, 'a literal does not acquire an equal-valued token binding')
        assert.equal(node.fills[0].opacity, 1, 'color alpha is not applied twice')
        for (const channel of ['r', 'g', 'b', 'a']) assert.ok(Math.abs(node.fills[0].color[channel] - expected[channel]) < 1e-6)
      }
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
    for (const value of ['rgb(1)', 'color(srgb 1.1 0 0)', 'color(display-p3 .125 .375 .625)']) {
      const altered = structuredClone(observation); altered.roots[0].style['background-color'] = value
      const before = structuredClone([...built.graph.getAllNodes()]), hook = getTextMeasurer()
      await assert.rejects(materializeComponent(built.graph, page.id, snapshot, altered, faces, renderer, built.collection.id), /unsupported computed paint/)
      assert.deepEqual([...built.graph.getAllNodes()], before)
      assert.equal(getTextMeasurer(), hook)
    }
  }
})

test('real secondary Text binds its authored role and follows palette edits through two FIG saves', async () => {
  const id = 'pk-ui.component.text/muted'
  const snapshot = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', id, '--props'], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', input: JSON.stringify({ color: 'secondary' }),
  }))
  for (const mode of ['light', 'dark']) {
    const observation = await observe(snapshot, mode), built = buildFoundation(snapshot), page = built.graph.addPage('Derived source')
    built.graph.updateNode(page.id, { variableModes: { [built.collection.id]: built.collection.modes.find(item => item.name === mode).modeId } })
    assert.match(observation.roots[0].style.color, /^color\(srgb /)
    assert.deepEqual(observation.roots[0].paintSources.color, {
      tokens: ['--pk-color-surface-primary', '--pk-color-text-primary'], directCandidate: null,
      expressionCandidate: {
        customProperty: '--pk-role-fg-secondary',
        value: 'color-mix(in srgb, var(--pk-color-text-primary) 78%, var(--pk-color-surface-primary))', customProperties: {},
      },
    })
    let graph = built.graph
    const hook = getTextMeasurer()
    const { master } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, built.collection.id)
    graph.createInstance(master.id, page.id, { name: 'Derived placement', x: 200 })
    graph.createInstance(master.id, page.id, { name: 'Derived sibling', x: 400 })
    for (let cycle = 0; cycle < 3; cycle++) {
      const instance = [...graph.nodes.values()].find(node => node.name === 'Derived placement')
      const text = graph.getChildren(instance.id).find(node => node.type === 'TEXT')
      const role = graph.variables.get(text.boundVariables['fills/0/color'])
      assert.equal(role?.name, '--pk-role-fg-secondary')
      const ink = [...graph.variables.values()].find(variable => variable.name === '--pk-color-text-primary')
      const surface = [...graph.variables.values()].find(variable => variable.name === '--pk-color-surface-primary')
      const collection = graph.variableCollections.get(ink.collectionId), selected = collection.modes.find(item => item.name === mode)
      const other = collection.modes.find(item => item.name !== mode)
      const beforeOther = graph.resolveVariable(role.id, other.modeId), nodes = structuredClone([...graph.nodes])
      const expected = computedColor(observation.roots[0].style.color)
      for (const channel of ['r', 'g', 'b', 'a']) close(graph.resolveVariable(role.id, selected.modeId)[channel], expected[channel], channel)
      const actions = createEditor({ graph })
      actions.updateVariableValue(ink.id, selected.modeId, { r: 1, g: 0, b: 0, a: .5 })
      const paper = graph.resolveVariable(surface.id, selected.modeId), alpha = .78 * .5 + .22 * paper.a
      const mixed = { r: (.39 + .22 * paper.r * paper.a) / alpha,
        g: .22 * paper.g * paper.a / alpha, b: .22 * paper.b * paper.a / alpha, a: alpha }
      for (const channel of ['r', 'g', 'b', 'a']) assert.ok(Math.abs(graph.resolveVariable(role.id, selected.modeId)[channel] - mixed[channel]) < 1e-6)
      assert.deepEqual(graph.resolveVariable(role.id, other.modeId), beforeOther)
      actions.undoAction()
      actions.redoAction()
      actions.undoAction()
      assert.deepEqual([...graph.nodes], nodes, 'formula palette history leaves master and sibling structure unchanged')
      assert.equal(instance.componentId, [...graph.nodes.values()].find(node => node.name === 'Derived sibling').componentId)
      assert.equal(graph.getChildren(instance.componentId)[0].boundVariables['fills/0/color'], role.id)
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
    assert.equal(getTextMeasurer(), hook)
  }
})

test('native construction refuses an alpha-derived paint that looks literal in an opaque source palette', async () => {
  const snapshot = source()
  snapshot.css += '\n[data-component="button"] { background-color: rgb(from var(--pk-color-accent-default) 30 40 50 / alpha); }'
  for (const mode of ['light', 'dark']) {
    const observation = await observe(snapshot, mode), built = buildFoundation(snapshot)
    const page = built.graph.addPage('Alpha dependency')
    built.graph.updateNode(page.id, { variableModes: { [built.collection.id]: built.collection.modes.find(item => item.name === mode).modeId } })
    const before = structuredClone({ nodes: [...built.graph.getAllNodes()], variables: [...built.graph.variables] })
    const hook = getTextMeasurer()
    await assert.rejects(materializeComponent(built.graph, page.id, snapshot, observation, faces, renderer, built.collection.id),
      /mixed or derived paint dependencies/, 'an editable palette must not leave a falsely literal fill behind')
    assert.deepEqual({ nodes: [...built.graph.getAllNodes()], variables: [...built.graph.variables] }, before)
    assert.equal(getTextMeasurer(), hook)
  }
})

test('authored translucent fills and borders share one native role with source-correct pixels after two saves', async () => {
  const snapshot = source(), name = '--product-tint', tokenName = '--pk-color-accent-default'
  snapshot.css += `\n:root { ${name}: color-mix(in srgb, var(${tokenName}) 50%, transparent); }
    [data-component="button"] { background-color: var(${name}); border: 2px solid var(${name}); border-radius: 0; }`
  for (const mode of ['light', 'dark']) {
    const observation = await observe(snapshot, mode)
    let { graph, collection } = buildFoundation(snapshot)
    const page = graph.addPage('Authored translucent paints')
    graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
    const first = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, collection.id)
    const second = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, collection.id)
    assert.equal(first.master.boundVariables['fills/0/color'], second.master.boundVariables['fills/0/color'])
    graph.createInstance(first.master.id, page.id, { name: 'Translucent placement', x: 200 })
    for (let cycle = 0; cycle < 3; cycle++) {
      const roles = [...graph.variables.values()].filter(variable => variable.name === name)
      assert.equal(roles.length, 1, 'one shared authored role, not one variable per component or border side')
      const instance = [...graph.nodes.values()].find(node => node.name === 'Translucent placement')
      assert.equal(instance.boundVariables['fills/0/color'], roles[0].id)
      assert.equal(instance.boundVariables['strokes/0/color'], roles[0].id)
      const token = [...graph.variables.values()].find(variable => variable.name === tokenName)
      collection = graph.variableCollections.get(token.collectionId)
      const modeId = collection.modes.find(item => item.name === mode).modeId, editor = createEditor({ graph })
      const before = structuredClone([...graph.nodes])
      editor.updateVariableValue(token.id, modeId, { r: 1, g: 0, b: 0, a: .5 })
      const surface = ck.MakeSurface(128, 64), draw = new SkiaRenderer(ck, surface)
      try {
        await draw.loadFonts()
        const canvas = surface.getCanvas()
        canvas.clear(ck.TRANSPARENT)
        canvas.translate(-instance.x, -instance.y)
        draw.renderSceneToCanvas(canvas, graph, instance.parentId)
        surface.flush()
        for (const [x, y, alpha] of [[4, 4, .25], [0, 15, 1 - .75 ** 2]]) {
          const pixel = canvas.readPixels(x, y, { width: 1, height: 1, alphaType: ck.AlphaType.Unpremul,
            colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
          for (const [index, expected] of [255, 0, 0, alpha * 255].entries()) {
            assert.ok(Math.abs(pixel[index] - expected) <= 1, `${mode}/${cycle} at ${x},${y}: ${pixel}, expected alpha ${alpha}`)
          }
        }
      } finally { draw.destroy() }
      editor.undoAction()
      assert.deepEqual([...graph.nodes], before)
      assert.ok([...graph.nodes.values()].every(node => !Object.hasOwn(node, 'expressionBindings')))
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  }
})

test('derived paint refusals remove constructed nodes and never overwrite existing roles or retain variables', async () => {
  const snapshot = source()
  snapshot.css += '\n:root { --test-tint: color-mix(in srgb, var(--pk-color-accent-default) 50%, transparent); } [data-component="button"] { background-color: var(--test-tint); }'
  const observation = await observe(snapshot)
  for (const change of [
    input => { input.observation.roots[0].bounds.width += 20 },
    input => { input.observation.roots[0].paintSources['background-color'].expressionCandidate.value = '#abcdef' },
    input => { input.observation.roots[0].paintSources['background-color'].expressionCandidate.customProperties['--pk-color-accent-default'] = '#123456' },
    input => { input.graph.createVariable('--test-tint', 'COLOR', input.collection.id, parseColor('#123456')) },
    input => { input.graph.createVariable('--test-tint', 'STRING', input.collection.id, 'not a color') },
    input => { input.graph.bindVariable = () => { throw new Error('paint binding failure after allocation') } },
    input => { input.graph.syncInstances = () => { throw new Error('paint synchronization failure after allocation') } },
  ]) {
    const { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused derivation')
    const input = { graph, collection, observation: structuredClone(observation) }
    change(input)
    const before = structuredClone({ nodes: [...graph.nodes], variables: [...graph.variables], collections: [...graph.variableCollections] })
    await assert.rejects(materializeComponent(graph, page.id, snapshot, input.observation, faces, renderer, collection.id), /geometry|paint/)
    assert.deepEqual({ nodes: [...graph.nodes], variables: [...graph.variables], collections: [...graph.variableCollections] }, before)
  }
})

test('source currentColor formulas bind icon occurrences without changing canonical assets and survive glyph swaps', async () => {
  const id = 'pk-ui.component.button/with-icon', snapshot = source('Save', id), roleName = '--product-ink'
  snapshot.css += `\n:root { ${roleName}: color-mix(in srgb, var(--pk-color-accent-default) 50%, transparent); }
    [data-component="button"] { color: var(${roleName}); }`
  for (const mode of ['light', 'dark']) {
    const observation = await observe(snapshot, mode)
    let { graph, collection, icons } = buildFoundation(snapshot)
    const page = graph.addPage('Derived icon'), region = observation.roots[0].children.find(child => child.kind === 'slot')
    graph.updateNode(page.id, { variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId } })
    const assets = structuredClone([...icons.values()].flatMap(master => [master, ...graph.getChildren(master.id)]))
    const { master, properties } = await materializeComponent(graph, page.id, snapshot, observation, faces, renderer, collection.id,
      [{ region, master: icons.get('plus') }])
    assert.deepEqual([...icons.values()].flatMap(master => [master, ...graph.getChildren(master.id)]), assets)
    graph.createInstance(master.id, page.id, { name: 'Derived icon placement', x: 200 })
    graph.createInstance(master.id, page.id, { name: 'Derived icon sibling', x: 400 })
    const swap = properties.find(property => property.type === 'INSTANCE_SWAP')
    for (let cycle = 0; cycle < 3; cycle++) {
      const instance = [...graph.nodes.values()].find(node => node.name === 'Derived icon placement')
      const sibling = [...graph.nodes.values()].find(node => node.name === 'Derived icon sibling')
      const role = [...graph.variables.values()].find(variable => variable.name === roleName)
      const glyph = root => graph.getChildren(root.id).find(node => node.type === 'INSTANCE')
      const check = root => {
        const icon = glyph(root)
        for (const path of graph.getChildren(icon.id)) assert.equal(path.boundVariables['fills/0/color'], role.id)
        const text = graph.getChildren(root.id).find(node => node.type === 'TEXT')
        assert.equal(text.boundVariables['fills/0/color'], role.id)
        close(icon.width, 20, 'derived glyph width')
      }
      check(instance)
      check(sibling)
      const editor = createEditor({ graph })
      editor.setCanvasKit(ck, renderer)
      const replacement = [...graph.nodes.values()].find(node => node.type === 'COMPONENT' && node.name === (cycle % 2 ? 'plus' : 'x'))
      const before = structuredClone([sibling, ...graph.getChildren(sibling.id)])
      editor.setInstanceComponentProperty(instance.id, swap.id, replacement.id)
      check(instance)
      editor.undoAction()
      check(instance)
      assert.deepEqual([sibling, ...graph.getChildren(sibling.id)], before)
      editor.redoAction()
      check(instance)
      assert.equal(glyph(instance).componentId, replacement.id)
      const token = [...graph.variables.values()].find(variable => variable.name === '--pk-color-accent-default')
      const modeId = graph.variableCollections.get(token.collectionId).modes.find(item => item.name === mode).modeId
      editor.updateVariableValue(token.id, modeId, { r: 1, g: 0, b: 0, a: .5 })
      assert.deepEqual(graph.resolveColorVariableForNode(glyph(instance).id, role.id), { r: 1, g: 0, b: 0, a: .25 })
      editor.undoAction()
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  }
})

test('secondary Button source retains its accessible name, focus indicator and keyboard activation', async () => {
  const snapshot = source('Save', 'pk-ui.component.button/secondary')
  for (const mode of ['light', 'dark']) {
    const page = await browser.newPage({ colorScheme: mode, reducedMotion: 'reduce' })
    try {
      await page.setContent(`<html data-theme="${mode}"><head><style>${snapshot.css}</style></head><body>${snapshot.examples[0].html}</body></html>`)
      const button = page.getByRole('button', { name: 'Save', exact: true })
      assert.equal(await button.count(), 1)
      assert.deepEqual(await button.evaluate(node => [node.tagName, node.getAttribute('role'), node.getAttribute('aria-hidden')]), ['BUTTON', null, null])
      assert.match(await button.ariaSnapshot(), /button "Save"/)
      await button.evaluate(node => { node.dataset.activations = '0'; node.addEventListener('click', () => node.dataset.activations++) })
      const initialShadow = await button.evaluate(node => getComputedStyle(node).boxShadow)
      await page.keyboard.press('Tab')
      assert.ok(await button.evaluate(node => document.activeElement === node && node.matches(':focus-visible')))
      const focusedShadow = await button.evaluate(node => getComputedStyle(node).boxShadow)
      assert.notEqual(focusedShadow, 'none')
      assert.notEqual(focusedShadow, initialShadow, 'source focus indicator is present; contrast is a separate audit')
      for (const key of ['Enter', 'Space']) await page.keyboard.press(key)
      assert.equal(await button.getAttribute('data-activations'), '2')
    } finally { await page.close() }
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

const formId = 'pk-ui.component.form/default'

function formSource(proposal) {
  return JSON.parse(execFileSync('go', ['run', './tools/designexport', ...(proposal ? ['--proposal'] : [])], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
    input: proposal ? JSON.stringify(proposal) : undefined,
  }))
}

function suppliedFormFaces() {
  return [400, 500, 600].map(weight => {
    const bytes = readFileSync(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
    return { family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: createHash('sha256').update(bytes).digest('hex') }
  })
}

function formNodes(graph, root) {
  return [root, ...graph.getChildren(root.id).flatMap(child => formNodes(graph, child))]
}

function sourceRecord(node) {
  const records = node.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
  assert.equal(records.length, 1)
  return JSON.parse(records[0].value)
}

function nativeFormChild(graph, root, path) {
  return path.reduce((parent, localId) => {
    const children = graph.getChildren(parent.id).filter(node => node.type === 'INSTANCE' && sourceRecord(node).localId === localId)
    assert.equal(children.length, 1, `one native source occurrence ${localId}`)
    return children[0]
  }, root)
}

function sourceFormChild(snapshot, path) {
  return path.reduce((parent, id) => parent.children.find(child => child.description.id === id).description,
    snapshot.examples.find(example => example.id === formId))
}

function boundFormText(graph, root, propertyId) {
  const matches = formNodes(graph, root).filter(node => node.componentPropertyReferences.some(ref => ref.propertyId === propertyId))
  assert.equal(matches.length, 1, 'one exact property target')
  assert.equal(matches[0].type, 'TEXT')
  return matches[0]
}

async function formPixels(graph, instance) {
  const surface = ck.MakeSurface(instance.width, instance.height), draw = new SkiaRenderer(ck, surface)
  const previous = getTextMeasurer()
  try {
    await draw.loadFonts()
    const canvas = surface.getCanvas()
    canvas.clear(ck.TRANSPARENT)
    canvas.translate(-instance.x, -instance.y)
    draw.renderSceneToCanvas(canvas, graph, instance.parentId)
    surface.flush()
    return canvas.readPixels(0, 0, { width: instance.width, height: instance.height,
      alphaType: ck.AlphaType.Unpremul, colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })
  } finally { draw.destroy(); setTextMeasurer(previous) }
}

test('real source Form becomes linked nested components with native fill and end alignment', async () => {
  const snapshot = formSource(), before = structuredClone(snapshot), formFaces = suppliedFormFaces()
  for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
    const observation = await captureExample(browser, snapshot, formId, { fonts: formFaces, mode, viewport: { width, height: 900 } })
    const built = buildFoundation(snapshot), page = built.graph.addPage('Source Form')
    built.graph.updateNode(page.id, { variableModes: { [built.collection.id]: built.collection.modes.find(item => item.name === mode).modeId } })
    const result = await materializeComponent(built.graph, page.id, snapshot, observation, formFaces, renderer, built.collection.id)
    assert.deepEqual(result.components.map(component => component.path).toSorted(), [
      [formId], [formId, 'actions'], [formId, 'actions', 'cancel'], [formId, 'actions', 'create'], [formId, 'title'],
    ])
    assert.equal(result.master.type, 'COMPONENT')
    assert.deepEqual(result.properties, [], 'nonvisual Form transport properties are not invented as native text')
    close(result.master.width, width, 'source Form width')
    close(result.master.height, 126, 'source Form height')
    const field = nativeFormChild(built.graph, result.master, ['title'])
    const actions = nativeFormChild(built.graph, result.master, ['actions'])
    assert.equal(field.counterAxisSizing, 'FILL')
    assert.equal(actions.primaryAxisSizing, 'FILL')
    assert.equal(actions.primaryAxisAlign, 'MAX')
    for (const [node, bounds] of [[field, { width, height: 64, x: 0, y: 0 }], [actions, { width, height: 46, x: 0, y: 80 }]]) {
      for (const [key, value] of Object.entries(bounds)) close(node[key], value, `nested ${key}`)
    }
    const cancel = nativeFormChild(built.graph, actions, ['cancel']), create = nativeFormChild(built.graph, actions, ['create'])
    close(cancel.x, width - 163.09375, 'end-aligned Cancel x includes both solid border insets')
    close(cancel.y, 8, 'centered Cancel y')
    close(create.x, width - 77, 'end-aligned Create x')
    close(create.y, 8, 'centered Create y')
    const input = result.components.find(item => item.path.at(-1) === 'title')
    assert.deepEqual(input.properties.map(property => [property.name, property.defaultValue]), [['label', 'Title'], ['value', '']])
    const control = formNodes(built.graph, input.master).find(node => node.strokes.length > 0)
    assert.equal(control.strokes.length, 1)
    assert.equal(control.strokes[0].weight, 1)
    assert.equal(control.strokes[0].align, 'INSIDE')
    assert.equal(built.graph.variables.get(control.boundVariables['strokes/0/color']).name, '--pk-color-border-default')
    for (const item of result.components) {
      const source = sourceFormChild(snapshot, item.path.slice(1)), provenance = sourceRecord(item.master)
      assert.equal(provenance.componentId, source.componentId)
      assert.equal(provenance.exampleId, source.id)
      assert.equal(provenance.sha256, snapshot.sha256)
      assert.deepEqual(provenance.props, source.props)
      assert.ok(!Object.hasOwn(provenance, 'path'), 'definitions never claim a placed root address')
    }
  }
  assert.deepEqual(snapshot, before)
})

test('real nested Input properties survive native history and two saves into source projection', async () => {
  const snapshot = formSource(), before = structuredClone(snapshot), formFaces = suppliedFormFaces()
  const observation = await captureExample(browser, snapshot, formId, { fonts: formFaces })
  const built = buildFoundation(snapshot), page = built.graph.addPage('Source Form')
  const result = await materializeComponent(built.graph, page.id, snapshot, observation, formFaces, renderer, built.collection.id)
  let graph = built.graph, master = result.master
  let edited = graph.createInstance(master.id, page.id, { name: 'Edited source Form', y: 200 })
  let sibling = graph.createInstance(master.id, page.id, { name: 'Untouched source Form', y: 400 })
  associateSourceInstance(graph, edited, snapshot, [formId])
  const definitions = result.components.find(item => item.path.at(-1) === 'title').properties
  const label = definitions.find(item => item.name === 'label'), value = definitions.find(item => item.name === 'value')
  const props = { label: 'Delivery title', value: 'Collect samples' }
  for (let round = 0; round < 3; round++) {
    const actions = createEditor({ graph })
    actions.setCanvasKit(ck, renderer)
    const field = nativeFormChild(graph, edited, ['title'])
    if (round === 0) {
      actions.setInstanceComponentProperty(field.id, label.id, props.label)
      actions.setInstanceComponentProperty(field.id, value.id, props.value)
      await Promise.resolve()
      actions.undoAction()
      actions.undoAction()
      await Promise.resolve()
      assert.equal(boundFormText(graph, field, label.id).text, 'Title')
      assert.equal(boundFormText(graph, field, value.id).text, '')
      actions.redoAction()
      actions.redoAction()
      await Promise.resolve()
    } else {
      props.value = `Collect samples after save ${round}`
      actions.setInstanceComponentProperty(field.id, value.id, props.value)
      await Promise.resolve()
    }
    assert.equal(boundFormText(graph, field, label.id).text, props.label)
    assert.equal(boundFormText(graph, field, value.id).text, props.value)
    const beforeGraph = structuredClone([...graph.getAllNodes()])
    const extracted = extractSourceProps(graph, field, snapshot)
    assert.equal(extracted.status, 'proposal')
    assert.deepEqual(extracted.proposal, { baseSHA256: snapshot.sha256, path: [formId, 'title'], props })
    assert.deepEqual([...graph.getAllNodes()], beforeGraph, 'extraction never modifies the native document')
    const projected = formSource(extracted.proposal), originalInput = sourceFormChild(snapshot, ['title'])
    assert.deepEqual(sourceFormChild(projected, ['title']).props, { ...originalInput.props, ...props })
    assert.deepEqual(sourceFormChild(projected, []).props, sourceFormChild(snapshot, []).props, 'Form action and accessibility transport retained')
    assert.deepEqual(sourceFormChild(projected, ['actions']), sourceFormChild(snapshot, ['actions']))
    assert.deepEqual(projected.examples.filter(example => example.id !== formId), snapshot.examples.filter(example => example.id !== formId))
    const captured = await captureExample(browser, projected, formId, { fonts: formFaces })
    close(edited.width, captured.roots[0].bounds.width, 'edited Form width')
    close(edited.height, captured.roots[0].bounds.height, 'edited Form height')
    for (const untouched of [master, sibling]) {
      const input = nativeFormChild(graph, untouched, ['title'])
      assert.equal(boundFormText(graph, input, label.id).text, 'Title')
      assert.equal(boundFormText(graph, input, value.id).text, '')
    }
    if (round < 2) {
      const bytes = await exportFigFile(graph), raw = parseFigBuffer(bytes.slice().buffer)
      assert.ok(raw.nodeChanges.some(node => node.name === 'Edited source Form'), 'independent file parser sees the placed Form')
      graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
      edited = [...graph.getAllNodes()].find(node => node.name === 'Edited source Form')
      sibling = [...graph.getAllNodes()].find(node => node.name === 'Untouched source Form')
      master = graph.getNode(edited.componentId)
    }
  }
  assert.deepEqual(snapshot, before)
})

test('real nested Form actions retain end alignment and source transport through history and two saves', async () => {
  const snapshot = formSource(), formFaces = suppliedFormFaces()
  for (const localId of ['cancel', 'create']) {
    const observation = await captureExample(browser, snapshot, formId, { fonts: formFaces, viewport: { width: 320, height: 900 } })
    const built = buildFoundation(snapshot), page = built.graph.addPage('Source actions')
    const result = await materializeComponent(built.graph, page.id, snapshot, observation, formFaces, renderer, built.collection.id)
    let graph = built.graph, master = result.master
    let edited = graph.createInstance(master.id, page.id, { name: 'Edited nested actions', y: 200 })
    let sibling = graph.createInstance(master.id, page.id, { name: 'Untouched nested actions', y: 400 })
    associateSourceInstance(graph, edited, snapshot, [formId])
    const property = result.components.find(item => item.path.at(-1) === localId).properties[0]
    const path = ['actions', localId], initial = sourceFormChild(snapshot, path).props.label
    for (let round = 0; round < 3; round++) {
      const actions = createEditor({ graph })
      actions.setCanvasKit(ck, renderer)
      const target = nativeFormChild(graph, edited, path), label = `${localId === 'cancel' ? 'Go back' : 'Create delivery'} ${round}`
      actions.setInstanceComponentProperty(target.id, property.id, label)
      await Promise.resolve()
      if (round === 0) {
        actions.undoAction()
        await Promise.resolve()
        assert.equal(boundFormText(graph, target, property.id).text, initial)
        actions.redoAction()
        await Promise.resolve()
      }
      const extracted = extractSourceProps(graph, target, snapshot)
      assert.equal(extracted.status, 'proposal')
      assert.deepEqual(extracted.proposal, { baseSHA256: snapshot.sha256, path: [formId, ...path], props: { label } })
      const projected = formSource(extracted.proposal), sourceButton = sourceFormChild(snapshot, path)
      assert.deepEqual(sourceFormChild(projected, path).props, { ...sourceButton.props, label }, 'href or submit type retained')
      assert.deepEqual(sourceFormChild(projected, ['title']), sourceFormChild(snapshot, ['title']))
      assert.deepEqual(sourceFormChild(projected, []).props, sourceFormChild(snapshot, []).props)
      const captured = await captureExample(browser, projected, formId, { fonts: formFaces, viewport: { width: 320, height: 900 } })
      const sourceActions = captured.roots[0].children[1]
      const nativeActions = nativeFormChild(graph, edited, ['actions'])
      for (const child of sourceActions.children) {
        const native = nativeFormChild(graph, nativeActions, [child.source.path.at(-1)])
        for (const axis of ['x', 'y']) close(native[axis], child.bounds[axis] - sourceActions.bounds[axis], `edited action ${axis}`)
        for (const axis of ['width', 'height']) close(native[axis], child.bounds[axis], `edited action ${axis}`)
      }
      for (const unchanged of [master, sibling]) assert.equal(boundFormText(graph,
        nativeFormChild(graph, unchanged, path), property.id).text, initial)
      if (round < 2) {
        const bytes = await exportFigFile(graph)
        graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
        edited = [...graph.getAllNodes()].find(node => node.name === 'Edited nested actions')
        sibling = [...graph.getAllNodes()].find(node => node.name === 'Untouched nested actions')
        master = graph.getNode(edited.componentId)
      }
    }
  }
})

test('source composition refuses incomplete ownership, unsupported layout and invalid controls atomically', async () => {
  const snapshot = formSource(), formFaces = suppliedFormFaces()
  const observation = await captureExample(browser, snapshot, formId, { fonts: formFaces })
  const cases = [
    input => { delete input.root.source },
    input => { input.root.children[0].source.componentId = 'wrong-interface' },
    input => { input.root.children[0].source.path = [formId, 'missing'] },
    input => { input.root.children[0].source.slot = 'Missing' },
    input => { delete input.root.children[0].source },
    input => { input.root.children.push(structuredClone(input.root.children[0])) },
    input => { input.root.children.pop() },
    input => { input.example.opaqueSlots.push('Children') },
    input => { input.example.slots[0].trustedOnly = false },
    input => { delete input.example.children[0].span },
    input => { input.control.control.type = 'password' },
    input => { input.control.control.placeholder = 'Title placeholder' },
    input => { input.control.control.value = 'Not the source value' },
    input => { input.control.control.property = 'unknown' },
    input => { input.control.control.fonts = [{ isCustomFont: true, postScriptName: 'IBMPlexSans-Regular' }] },
    input => { input.control.style['border-top-style'] = 'inset' },
    input => { input.control.style['border-left-width'] = '2px' },
    input => { input.control.sizing.width = 'calc(100% - 4px)' },
    input => { input.control.style['text-align'] = 'right' },
    input => { input.root.style.display = 'grid' },
    input => { input.root.style['flex-wrap'] = 'wrap' },
    input => { input.root.children[0].style['flex-grow'] = '1' },
    input => { input.root.children[0].children[0].children[0].bounds.x += 10 },
    input => { input.control.bounds.y += 10 },
    input => { input.observation.fontFaces = [] },
    input => { input.faces = input.faces.filter(face => face.weight !== 400) },
    input => { input.renderer = { loadFonts: async () => {}, measureTextNode: () => null } },
  ]
  for (const [index, mutate] of cases.entries()) {
    const copy = structuredClone(snapshot), captured = structuredClone(observation), built = buildFoundation(copy)
    const input = { snapshot: copy, observation: captured, root: captured.roots[0],
      example: copy.examples.find(example => example.id === formId), control: captured.roots[0].children[0].children[1],
      faces: formFaces, renderer }
    mutate(input)
    const page = built.graph.addPage('Rejected composition'), before = structuredClone([...built.graph.getAllNodes()])
    const beforeInput = structuredClone({ source: copy, observation: captured }), hook = getTextMeasurer()
    await assert.rejects(materializeComponent(built.graph, page.id, copy, captured, input.faces, input.renderer, built.collection.id),
      undefined, `case ${index} must refuse`)
    assert.deepEqual([...built.graph.getAllNodes()], before, `case ${index} left partial definitions`)
    assert.deepEqual({ source: copy, observation: captured }, beforeInput)
    assert.equal(getTextMeasurer(), hook)
  }
})

test('native text refuses real source indentation, shadow, spacing and writing-direction changes', async () => {
  const formFaces = suppliedFormFaces()
  for (const rule of ['text-indent:20px', 'text-shadow:4px 0 red', 'word-spacing:4px', 'writing-mode:vertical-rl', 'direction:rtl']) {
    for (const [id, selector, fonts] of [[formId, 'input', formFaces], [primary, '[data-component="button"]', faces]]) {
      const snapshot = formSource()
      snapshot.css += `\n${selector} { ${rule}; }`
      const captured = await captureExample(browser, snapshot, id, { fonts }), built = buildFoundation(snapshot)
      const page = built.graph.addPage('Rejected text styling'), before = structuredClone([...built.graph.getAllNodes()])
      await assert.rejects(materializeComponent(built.graph, page.id, snapshot, captured, fonts, renderer, built.collection.id),
        /text shadows|indentation|word spacing|writing direction/)
      assert.deepEqual([...built.graph.getAllNodes()], before)
    }
  }
})

test('native Input values remain unwrapped with accurate text bounds through history and two saves', async () => {
  const snapshot = formSource(), formFaces = suppliedFormFaces()
  const observation = await captureExample(browser, snapshot, formId, { fonts: formFaces, viewport: { width: 320, height: 900 } })
  const built = buildFoundation(snapshot), page = built.graph.addPage('Single-line control')
  const result = await materializeComponent(built.graph, page.id, snapshot, observation, formFaces, renderer, built.collection.id)
  let graph = built.graph, instance = graph.createInstance(result.master.id, page.id, { name: 'Single-line source Form', y: 200 })
  associateSourceInstance(graph, instance, snapshot, [formId])
  const property = result.components.find(item => item.path.at(-1) === 'title').properties.find(item => item.name === 'value')
  const value = 'This is a long single line value with several words and another complete sentence to exceed the field width.'
  const emptyPixels = await formPixels(graph, instance)
  let longPixels
  for (let round = 0; round < 3; round++) {
    if (round > 0) assert.deepEqual(await formPixels(graph, instance), longPixels, 'reopened native pixels retained before any new edit')
    const actions = createEditor({ graph })
    actions.setCanvasKit(ck, renderer)
    const input = nativeFormChild(graph, instance, ['title']), text = boundFormText(graph, input, property.id)
    actions.setInstanceComponentProperty(input.id, property.id, value)
    await Promise.resolve()
    const measured = renderer.measureTextNode(text), viewport = graph.getNode(text.parentId)
    assert.equal(text.textAutoResize, 'WIDTH_AND_HEIGHT')
    assert.equal(text.layoutPositioning, 'ABSOLUTE')
    assert.equal(viewport.clipsContent, true)
    assert.equal(viewport.height, 20)
    assert.ok(measured.width > viewport.width, 'fixture overflows the real single-line content width')
    assert.equal(measured.height, 20, 'native paragraph remains one line')
    close(text.width, measured.width, 'native editable text bounds track the actual full advance')
    assert.equal(text.height, 20)
    longPixels = await formPixels(graph, instance)
    assert.notDeepEqual(longPixels, emptyPixels, 'the overflowing native value visibly renders')
    const proposal = extractSourceProps(graph, input, snapshot).proposal
    assert.deepEqual(proposal, { baseSHA256: snapshot.sha256, path: [formId, 'title'], props: { value } })
    assert.equal(sourceFormChild(formSource(proposal), ['title']).props.value, value)
    actions.setInstanceComponentProperty(input.id, property.id, '')
    await Promise.resolve()
    assert.equal(text.text, '')
    assert.equal(text.width, 0, 'clearing restores empty intrinsic width, not the previous long selection')
    assert.deepEqual(await formPixels(graph, instance), emptyPixels, 'clear removes the rendered glyphs')
    actions.undoAction()
    await Promise.resolve()
    assert.equal(text.text, value)
    close(text.width, measured.width, 'undo restores exact long-text geometry')
    actions.redoAction()
    await Promise.resolve()
    assert.equal(text.text, '')
    assert.equal(text.width, 0)
    actions.undoAction()
    await Promise.resolve()
    if (round < 2) {
      const bytes = await exportFigFile(graph)
      graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
      instance = [...graph.getAllNodes()].find(node => node.name === 'Single-line source Form')
    }
  }
  const actions = createEditor({ graph })
  actions.setCanvasKit(ck, renderer)
  actions.setInstanceComponentProperty(nativeFormChild(graph, instance, ['title']).id, property.id, '')
  const bytes = await exportFigFile(graph)
  graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
  instance = [...graph.getAllNodes()].find(node => node.name === 'Single-line source Form')
  const input = nativeFormChild(graph, instance, ['title']), cleared = boundFormText(graph, input, property.id)
  assert.equal(cleared.text, '')
  assert.equal(cleared.width, 0)
  assert.equal(input.componentPropertyAssignments[property.id], '')
  assert.equal(extractSourceProps(graph, input, snapshot).status, 'no-supported-changes')
  assert.deepEqual(await formPixels(graph, instance), emptyPixels)
  const reopened = createEditor({ graph })
  reopened.setCanvasKit(ck, renderer)
  reopened.setInstanceComponentProperty(input.id, property.id, value)
  close(cleared.width, renderer.measureTextNode(cleared).width, 'editing recovers after a saved empty value')
})

test('source-populated and zero-advance Input values initialize through the owning native auto-size path', async () => {
  const base = formSource(), formFaces = suppliedFormFaces()
  for (const value of ['Populated source value', '\u0301']) {
    const snapshot = formSource({ baseSHA256: base.sha256, path: [formId, 'title'], props: { value } })
    const observation = await captureExample(browser, snapshot, formId, { fonts: formFaces, viewport: { width: 320, height: 900 } })
    const control = observation.roots[0].children[0].children[1].control
    assert.equal(control.value, value)
    assert.equal(control.fonts.length, 1, 'populated controls have actual glyph evidence')
    const built = buildFoundation(snapshot), page = built.graph.addPage('Populated source Form')
    const result = await materializeComponent(built.graph, page.id, snapshot, observation, formFaces, renderer, built.collection.id)
    const input = result.components.find(item => item.path.at(-1) === 'title')
    const property = input.properties.find(item => item.name === 'value'), target = boundFormText(built.graph, input.master, property.id)
    const measured = renderer.measureTextNode(target)
    close(target.width, measured.width, 'initial native value advance')
    assert.equal(target.height, 20)
    assert.equal(target.text, value)
    if (value === '\u0301') assert.equal(target.width, 0, 'real combining glyph may have zero advance')
    const instance = built.graph.createInstance(result.master.id, page.id, { y: 200 })
    associateSourceInstance(built.graph, instance, snapshot, [formId])
    const actions = createEditor({ graph: built.graph })
    actions.setCanvasKit(ck, renderer)
    const nested = nativeFormChild(built.graph, instance, ['title'])
    actions.setInstanceComponentProperty(nested.id, property.id, 'Longer source value')
    actions.setInstanceComponentProperty(nested.id, property.id, value)
    close(boundFormText(built.graph, nested, property.id).width, measured.width, 'native setter restores genuine zero advance too')
  }
})

test('absolute native property measurement failures roll back graph, source caches and history', async () => {
  const snapshot = formSource(), formFaces = suppliedFormFaces()
  const observation = await captureExample(browser, snapshot, formId, { fonts: formFaces })
  for (const failure of [null, { width: NaN, height: 20 }, { width: -1, height: 20 },
    { width: Infinity, height: 20 }, { width: 10, height: 0 }, { width: 10, height: NaN }, new Error('Measurement failed')]) {
    const built = buildFoundation(snapshot), page = built.graph.addPage('Refused native value')
    const result = await materializeComponent(built.graph, page.id, snapshot, observation, formFaces, renderer, built.collection.id)
    const instance = built.graph.createInstance(result.master.id, page.id), input = nativeFormChild(built.graph, instance, ['title'])
    const property = result.components.find(item => item.path.at(-1) === 'title').properties.find(item => item.name === 'value')
    const actions = createEditor({ graph: built.graph })
    actions.setCanvasKit(ck, renderer)
    const before = structuredClone([...built.graph.getAllNodes()]), previous = getTextMeasurer()
    let calls = 0
    const broken = () => { calls++; if (failure instanceof Error) throw failure; return failure }
    try {
      setTextMeasurer(broken)
      assert.throws(() => actions.setInstanceComponentProperty(input.id, property.id, 'Nonempty value'), /measurement|Measurement/)
      await Promise.resolve()
      assert.equal(calls, 1, 'one actual measurement, no fallback or repeated shaping')
      assert.equal(getTextMeasurer(), broken, 'property action never replaces the caller measurement hook')
      assert.deepEqual([...built.graph.getAllNodes()], before)
      assert.equal(actions.undo.canUndo, false)
    } finally { setTextMeasurer(previous) }
  }
})

test('strict absolute auto-size preserves ordinary native helper fallback and zero-width behavior', async () => {
  const { textAutoResizeChanges } = await import(new URL('./editor/text/auto-resize.js', import.meta.resolve('@open-pencil/core')))
  const node = { type: 'TEXT', text: 'Previous', textAutoResize: 'WIDTH_AND_HEIGHT',
    fontFamily: 'IBM Plex Sans', fontWeight: 400, fontSize: 14, lineHeight: 20, width: 900, height: 20 }
  const previous = getTextMeasurer()
  try {
    setTextMeasurer(() => null)
    assert.ok(textAutoResizeChanges(node, { text: 'Next' }).width > 0, 'ordinary native helper retains its prior estimate fallback')
    assert.throws(() => textAutoResizeChanges(node, { text: 'Next' }, true), /Actual native text measurement/)
    setTextMeasurer(() => ({ width: 0, height: 20 }))
    assert.ok(!Object.hasOwn(textAutoResizeChanges(node, { text: '' }), 'width'), 'ordinary zero-width behavior is unchanged')
    assert.equal(textAutoResizeChanges(node, { text: '' }, true).width, 0)
  } finally { setTextMeasurer(previous) }
})
