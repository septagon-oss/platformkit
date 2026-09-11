import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { createEditor } from '@open-pencil/core/editor'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { associateSourceInstance, extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'
import { suppliedFonts, underlineFixture } from './fixtures.test.mjs'
import { sourceUnderlines } from '../source-underlines.mjs'
import { validateFonts } from '../fonts.mjs'
import { observedPaint } from '../component-paints.mjs'

const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
const formatFor = ck => ({ width: 640, height: 240, alphaType: ck.AlphaType.Unpremul,
  colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB })

async function fixture(t) {
  const source = await underlineFixture(t), fonts = suppliedFonts([400, 600]), ck = await initCanvasKit()
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  t.after(() => browser.close())
  const surface = ck.MakeSurface(640, 240), renderer = new SkiaRenderer(ck, surface)
  t.after(() => renderer.destroy())
  async function construct(snapshot, mode = 'light') {
    const observation = await captureExample(browser, snapshot, 'underlined', { fonts, mode, viewport: { width: 640, height: 240 } })
    const graph = buildFoundation(snapshot).graph, collection = [...graph.variableCollections.values()][0]
    const definitions = graph.addPage('Definitions'), page = graph.addPage('Source')
    for (const node of [definitions, page]) graph.updateNode(node.id, {
      variableModes: { [collection.id]: collection.modes.find(item => item.name === mode).modeId },
    })
    const built = await materializeComponent(graph, definitions.id, snapshot, observation, fonts, renderer, collection.id)
    const instance = graph.createInstance(built.master.id, page.id, { name: 'Editable underline' })
    graph.createInstance(built.master.id, definitions.id, { name: 'Unchanged sibling', x: 1000 })
    associateSourceInstance(graph, instance, snapshot, ['underlined'])
    return { ...built, graph, instance, observation, collection }
  }
  function render(graph, instance, origin = { x: 0, y: 0 }) {
    const before = structuredClone([...graph.getAllNodes()]), canvas = surface.getCanvas()
    canvas.clear(ck.WHITE); canvas.save(); canvas.translate(origin.x, origin.y)
    try { renderer.renderSceneToCanvas(canvas, graph, instance.parentId); surface.flush() }
    finally { canvas.restore() }
    assert.deepEqual([...graph.getAllNodes()], before, 'painting never creates line objects or overrides')
    return Uint8Array.from(canvas.readPixels(0, 0, formatFor(ck)))
  }
  return { source, fonts, ck, browser, surface, renderer, construct, render }
}

test('real Core underline compositions keep native copy, palette links, exact history and two saves', async t => {
  const { source, ck, renderer, construct, render } = await fixture(t)
  for (const kind of ['button', 'text']) for (const mode of ['light', 'dark']) {
    const input = { Kind: kind, Content: 'Typography gyjp' }, snapshot = source(input), immutable = structuredClone(snapshot)
    let { graph, master, properties, instance } = await construct(snapshot, mode)
    const text = descendants(graph, instance).find(node => node.type === 'TEXT')
    assert.equal(text.textDecoration, 'UNDERLINE')
    assert.ok(text.textDecorationThickness >= 1)
    assert.ok(text.boundVariables['fills/0/color'], 'underline keeps the existing text colour binding')
    assert.ok(properties.some(property => property.type === 'TEXT'))
    const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
    const property = properties.find(property => property.type === 'TEXT'), value = 'João café memories gyjp'
    const before = structuredClone([...graph.getAllNodes()]), pixelsBefore = render(graph, instance)
    editor.setInstanceComponentProperty(instance.id, property.id, value)
    const after = structuredClone([...graph.getAllNodes()]), pixelsAfter = render(graph, instance)
    assert.notDeepEqual(pixelsAfter, pixelsBefore)
    for (const node of before.filter(node => !descendants(graph, instance).some(child => child.id === node.id))) {
      assert.deepEqual(graph.getNode(node.id), node, 'copy editing leaves definitions, tokens and unrelated siblings alone')
    }
    editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
    assert.deepEqual(render(graph, instance), pixelsBefore)
    editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after)
    assert.deepEqual(render(graph, instance), pixelsAfter)
    for (let save = 0; save < 3; save++) {
      const proposal = extractSourceProps(graph, instance, snapshot).proposal
      assert.deepEqual(proposal, { baseSHA256: snapshot.sha256, path: ['underlined'], props: { [kind === 'text' ? 'content' : 'label']: value } })
      const projected = source({ ...input, Proposal: proposal })
      assert.equal(projected.examples[0].props[kind === 'text' ? 'content' : 'label'], value)
      const content = descendants(graph, instance).find(node => node.type === 'TEXT')
      for (const key of ['textDecoration', 'textDecorationThickness', 'textUnderlineOffset', 'textDecorationSkipInk']) assert.equal(content[key], text[key])
      assert.equal(descendants(graph, master).find(node => node.type === 'TEXT').text, input.Content)
      assert.deepEqual(render(graph, instance), pixelsAfter, 'copy and CSS ink skipping survive every save')
      if (save < 2) {
        graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        instance = [...graph.getAllNodes()].find(node => node.name === 'Editable underline')
        master = graph.getNode(instance.componentId)
      }
    }
    assert.deepEqual(snapshot, immutable)
  }
})

// Browser screenshots and native pixels each have their own no-decoration
// control, isolating underline coverage from the independent glyph rasterizers.
function decorationInk(decorated, plain) {
  const ink = new Set()
  for (let i = 0; i < plain.length; i += 4) if (plain[i] - decorated[i] > 128) ink.add(i / 4)
  return ink
}

function sameDecoration(actual, expected, glyphDifferences = new Set()) {
  assert.ok(expected.size > 20, 'the independent browser control must expose real underline ink')
  const rows = pixels => [...new Set([...pixels].map(position => Math.floor(position / 640)))].toSorted((a, b) => a - b)
  assert.deepEqual(rows(actual), rows(expected), 'every underline row must match, including wrapped lines')
  // Different Skia versions round shaped advances and intercept endpoints
  // differently. Only one horizontal edge pixel may differ, never a moved row
  // or missing interior/gap. Independently observed glyph-coverage differences
  // cannot establish decoration coverage when a negative offset crosses text.
  for (const [a, b] of [[actual, expected], [expected, actual]]) for (const position of a) {
    assert.ok(glyphDifferences.has(position) || [-1, 0, 1].some(dx => Math.floor((position + dx) / 640) === Math.floor(position / 640) && b.has(position + dx)),
      `underline interior differs at ${position % 640},${Math.floor(position / 640)}`)
  }
}

test('source solid underline paint matches Chromium for sizes, offsets, ink skipping and wrapping', async t => {
  const { source, fonts, ck, browser, construct, render } = await fixture(t)
  const page = await browser.newPage({ viewport: { width: 640, height: 240 }, reducedMotion: 'reduce' })
  t.after(() => page.close())
  for (const kind of ['button', 'text']) for (const size of [14, 16, 24, 32]) for (const [thickness, offset, skip] of [
    ['auto', 'auto', 'auto'], ['auto', '8px', 'none'], ['2px', '0px', 'auto'],
    ['12.5%', '-10%', 'none'], ['0px', '-0.5px', 'none'],
  ]) {
    const input = { Kind: kind, Content: 'Typography gyjp João café', Style: {
      'font-size': `${size}px`, 'line-height': '40px', color: '#000', 'text-decoration-color': '#000',
      'text-decoration-skip-ink': skip, 'text-underline-offset': offset, 'text-decoration-thickness': thickness,
      ...(kind === 'text' ? { 'max-width': '180px' } : {}),
    } }
    const snapshot = source(input), { graph, instance, observation } = await construct(snapshot)
    await page.setContent(`<html data-theme="light"><style>${snapshot.css}</style>${snapshot.examples[0].html}</html>`)
    await page.evaluate(async fonts => {
      for (const font of fonts) {
        const face = new FontFace(font.family, Uint8Array.from(font.bytes), { weight: String(font.weight), style: font.style })
        await face.load(); document.fonts.add(face)
      }
      await document.fonts.ready
      document.body.style.background = 'white'
    }, fonts.map(font => ({ ...font, bytes: [...font.bytes] })))
    async function pixels() {
      const image = ck.MakeImageFromEncoded(await page.screenshot())
      try { return Uint8Array.from(image.readPixels(0, 0, formatFor(ck))) } finally { image.delete() }
    }
    const painted = await pixels(), native = render(graph, instance, observation.roots[0].bounds)
    await page.locator('#subject').evaluate(node => { node.style.textDecoration = 'none' })
    for (const text of descendants(graph, instance).filter(node => node.type === 'TEXT')) graph.updateNode(text.id, { textDecoration: 'NONE' })
    const browserPlain = await pixels(), nativePlain = render(graph, instance, observation.roots[0].bounds), glyphDifferences = new Set()
    for (let i = 0; i < browserPlain.length; i += 4) if (Math.abs(browserPlain[i] - nativePlain[i]) > 1) glyphDifferences.add(i / 4)
    const expected = decorationInk(painted, browserPlain), actual = decorationInk(native, nativePlain)
    try { sameDecoration(actual, expected, glyphDifferences) }
    catch (error) {
      throw new Error(`${kind}/${size}/${thickness}/${offset}/${skip}: ${error.message}`, { cause: error })
    }
    assert.throws(() => sameDecoration(new Set([...actual].map(position => position + 640)), expected), /row/)
    assert.throws(() => sameDecoration(new Set(), expected), /row/)
  }
})

test('unsupported CSS decorations refuse without changing source, nodes or variables', async t => {
  const { source, fonts, browser, renderer } = await fixture(t)
  for (const style of [
    { 'text-decoration-style': 'wavy' }, { 'text-decoration-style': 'double' },
    { 'text-underline-position': 'under' }, { 'text-decoration-thickness': 'from-font' },
    { 'text-decoration-line': 'underline line-through' }, { 'text-decoration-color': '#123456' },
  ]) {
    const snapshot = source({ Kind: 'button', Content: 'Typography gyjp', Style: style }), immutable = structuredClone(snapshot)
    const observation = await captureExample(browser, snapshot, 'underlined', { fonts })
    const graph = buildFoundation(snapshot).graph, parent = graph.addPage('Definitions'), collection = [...graph.variableCollections.values()][0]
    graph.updateNode(parent.id, { variableModes: { [collection.id]: collection.modes.find(mode => mode.name === 'light').modeId } })
    const before = structuredClone({ nodes: [...graph.getAllNodes()], variables: [...graph.variables] })
    await assert.rejects(materializeComponent(graph, parent.id, snapshot, observation, fonts, renderer, collection.id),
      /underline|text transformations/)
    assert.deepEqual({ nodes: [...graph.getAllNodes()], variables: [...graph.variables] }, before)
    assert.deepEqual(snapshot, immutable)
  }
})

test('source underline paint follows palette history and native style edits leave the CSS projection', async t => {
  const { source, construct, render, ck, renderer } = await fixture(t)
  for (const mode of ['light', 'dark']) {
    const { graph, instance, collection } = await construct(source({ Kind: 'button', Content: 'Typography gyjp' }), mode)
    const text = descendants(graph, instance).find(node => node.type === 'TEXT'), metadata = structuredClone(text.pluginData)
    const variable = graph.variables.get(text.boundVariables['fills/0/color']), selectedMode = collection.modes.find(item => item.name === mode)
    const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
    const before = render(graph, instance), values = structuredClone(variable.valuesByMode)
    editor.updateVariableValue(variable.id, selectedMode.modeId, { r: 1, g: 0, b: 0, a: 0.5 })
    const changed = render(graph, instance)
    assert.notDeepEqual(changed, before)
    assert.ok(changed.some((value, i) => i % 4 === 0 && value === 255 && changed[i + 1] === 127 && changed[i + 2] === 127))
    assert.deepEqual(text.textDecorationFills, [], 'no copied decoration colour can detach from the variable')
    editor.undoAction(); assert.deepEqual(variable.valuesByMode, values); assert.deepEqual(render(graph, instance), before)
    editor.redoAction(); assert.deepEqual(render(graph, instance), changed)
    const detached = metadata.map(item => {
      const record = JSON.parse(item.value)
      delete record.cssUnderline
      return { ...item, value: JSON.stringify(record) }
    })
    graph.updateNode(text.id, { pluginData: detached })
    assert.notDeepEqual(render(graph, instance), changed, 'the test detects loss of CSS ink-skipping evidence')
    for (const changes of [{ fontSize: 20 }, { textUnderlineOffset: 8 }, { textDecorationThickness: 4 }]) {
      const previous = Object.fromEntries(Object.keys(changes).map(key => [key, text[key]]))
      graph.updateNode(text.id, { ...changes, pluginData: metadata })
      const nativeEdit = render(graph, instance)
      graph.updateNode(text.id, { pluginData: detached })
      assert.deepEqual(render(graph, instance), nativeEdit, 'native style edits are not overwritten by stale source geometry')
      graph.updateNode(text.id, previous)
    }
  }
})

test('real Link inline descendants retain their decorating owner, not their own computed none or colour', async t => {
  const { source, fonts, browser } = await fixture(t)
  for (const childStyle of [
    { 'text-decoration': 'none', 'text-underline-offset': '99px', 'text-decoration-thickness': '42px' },
    { 'text-decoration': 'none', display: 'inline-block' }, { 'text-decoration': 'none', position: 'absolute' },
    { 'text-decoration': 'underline' }, { color: '#123456' }, { 'vertical-align': 'super' }, { 'text-decoration-skip-ink': 'none' },
  ]) {
    const snapshot = source({ Kind: 'link', External: true, Content: 'Typography gyjp', DescendantStyle: childStyle })
    const observation = await captureExample(browser, snapshot, 'underlined', { fonts })
    const graph = buildFoundation(snapshot).graph, collection = [...graph.variableCollections.values()][0]
    const underline = sourceUnderlines(observation, (node, property) => observedPaint(graph, collection, snapshot, observation, node, property))
    const plan = (node, face) => underline(node, face, { text: node.children.find(child => child.kind === 'text').text })
    const root = observation.roots[0], child = root.children.find(node => node.kind === 'element'), face = validateFonts(fonts)[0]
    if (childStyle.display || childStyle.position) assert.deepEqual(plan(child, face), {}, 'atomic and out-of-flow boxes do not inherit decoration')
    else if (childStyle.color || childStyle['vertical-align'] || childStyle['text-decoration-skip-ink'] || childStyle['text-decoration'] === 'underline') {
      assert.throws(() => plan(child, face), /underline/, 'distinct colour or geometry cannot silently become the child text underline')
    } else {
      assert.deepEqual(plan(child, face), plan(root, face), 'child none and inherited offset do not replace the origin')
      for (const text of ['漢字', 'かな', '한글']) assert.throws(() => underline(root, face, { text }), /Latin/)
    }
  }
})
