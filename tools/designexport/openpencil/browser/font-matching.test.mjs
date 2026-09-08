import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildFoundation } from '../foundation.mjs'
import { buildComponentDocument } from '../document.mjs'
import { materializeComponent } from '../components.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'

const button = 'pk-ui.component.button/primary', text = 'pk-ui.component.text/muted'
const faces = [400, 500, 600, 700].map(weight => {
  const bytes = readFileSync(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
  return { family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: createHash('sha256').update(bytes).digest('hex') }
})
const previousMeasurer = getTextMeasurer()
let browser, ck, renderer
before(async () => {
  browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  ck = await initCanvasKit()
  renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900))
})
after(async () => { renderer?.destroy(); setTextMeasurer(previousMeasurer); await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0))

function source(id, props) {
  return JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', id, '--props'], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', input: JSON.stringify(props),
  }))
}
function placement(graph, id) {
  return [...graph.getAllNodes()].find(node => node.type === 'INSTANCE' && node.pluginData.some(item =>
    item.pluginId === 'platformkit' && item.key === 'platformkit.source' && JSON.parse(item.value).path?.[0] === id))
}
function close(actual, expected, label) {
  assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
}
function descendants(graph, node) {
  return [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
}

for (const id of [button, text]) test(`nearby-weight ${id} retains physical font, edits and two saves`, async () => {
  const property = id === button ? 'label' : 'content'
  const props = { [property]: 'An album begins here.', ...(id === text ? { weight: 'semibold' } : {}) }
  const snapshot = source(id, props), unchanged = structuredClone(snapshot)
  const fonts = faces.filter(face => face.weight === 700)
  for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
    const viewport = { width, height: 900 }, options = { examples: [id], fonts, browser, renderer, mode, viewport }
    const built = await buildComponentDocument(snapshot, options)
    let { graph } = built, instance = built.selections[0].instance
    const observed = built.selections[0].observation.roots[0]
    assert.equal(observed.style['font-weight'], '600')
    assert.equal(observed.children[0].fonts[0].postScriptName, 'IBMPlexSans-Bold')
    assert.equal(observed.children[0].fonts[0].isCustomFont, true)
    const sibling = graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Unchanged preview', y: 500 })
    const protectedNodes = structuredClone([
      ...descendants(graph, built.selections[0].master), ...descendants(graph, sibling),
    ])
    for (const value of ['Remember the people and places. '.repeat(id === text ? 5 : 1).trim(), 'Try again']) {
      const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
      const master = graph.getNode(instance.componentId)
      const definition = master.componentPropertyDefinitions.find(item => item.name === property)
      const before = structuredClone([...graph.getAllNodes()])
      editor.setInstanceComponentProperty(instance.id, definition.id, value)
      const after = structuredClone([...graph.getAllNodes()])
      editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
      editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after)
      const expected = (await captureExample(browser, source(id, { ...props, [property]: value }), id, { fonts, mode, viewport })).roots[0]
      for (let cycle = 0; cycle < 3; cycle++) {
        const native = graph.getChildren(instance.id).find(node => node.type === 'TEXT')
        assert.equal(native.fontFamily, 'IBM Plex Sans')
        assert.equal(native.fontWeight, 700, 'native typography names the real face, not the requested CSS weight')
        assert.equal(native.italic, false)
        assert.equal(native.text, value)
        close(instance.width, expected.bounds.width, 'root width')
        close(instance.height, expected.bounds.height, 'root height')
        const paragraph = renderer.buildParagraph(native, undefined, { halfLeading: true })
        try {
          const lines = paragraph.getLineMetrics(), rects = expected.children[0].rects
          assert.equal(lines.length, rects.length)
          for (const [index, line] of lines.entries()) close(line.width, rects[index].width, `line ${index}`)
        } finally { paragraph.delete() }
        assert.deepEqual(extractSourceProps(graph, instance, snapshot).proposal,
          { baseSHA256: snapshot.sha256, path: [id], props: { [property]: value } })
        if (cycle === 0 && value.startsWith('Remember')) {
          assert.deepEqual(protectedNodes.map(node => graph.getNode(node.id)), protectedNodes)
        }
        if (cycle < 2) {
          graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
          instance = placement(graph, id)
        }
      }
      editor.replaceGraph(new (graph.constructor)())
    }
  }
  assert.deepEqual(snapshot, unchanged)
})

test('nearby-weight matching is shared by nested Form composition without fabricating empty-control glyphs', async () => {
  const id = 'pk-ui.component.form/default', snapshot = source(id, {})
  const fonts = faces.filter(face => [400, 700].includes(face.weight))
  const built = await buildComponentDocument(snapshot, { examples: [id], fonts, browser, renderer, mode: 'light' })
  let { graph } = built
  for (let cycle = 0; cycle < 3; cycle++) {
    const native = descendants(graph, placement(graph, id)).filter(node => node.type === 'TEXT')
    assert.ok(native.some(node => node.text === 'Create' && node.fontWeight === 700))
    assert.ok(native.some(node => node.text === '' && node.fontWeight === 400))
    assert.ok(native.every(node => [400, 700].includes(node.fontWeight)))
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
})

test('CSS weight matching keeps light, medium and bold physical faces distinct', async () => {
  for (const [weight, supplied, expected] of [
    ['medium', [400], 400], ['normal', [500], 500], ['normal', [600, 700], 600],
    ['bold', [600], 600], ['black', [700], 700], ['semibold', [400, 700], 700],
  ]) {
    const snapshot = source(text, { content: 'Album notes', weight }), fonts = faces.filter(face => supplied.includes(face.weight))
    const built = await buildComponentDocument(snapshot, { examples: [text], fonts, browser, renderer, mode: 'light' })
    assert.equal(built.graph.getChildren(built.selections[0].instance.id)[0].fontWeight, expected)
  }
})

test('synthetic or unproven faces reject atomically in rows and composition', async () => {
  for (const id of [button, text]) {
    const snapshot = source(id, id === button ? { label: 'Audit' } : { content: 'Audit', weight: 'semibold' })
    for (const weight of [400, 500]) {
      const fonts = faces.filter(face => face.weight === weight)
      const observation = await captureExample(browser, snapshot, id, { fonts })
      const { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused synthesis')
      const before = structuredClone([...graph.getAllNodes()]), measurer = getTextMeasurer()
      await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, fonts, renderer, collection.id), /face.*synthesized/)
      assert.deepEqual([...graph.getAllNodes()], before)
      assert.equal(getTextMeasurer(), measurer)
    }
    const fonts = faces.filter(face => face.weight === 700)
    const observed = await captureExample(browser, snapshot, id, { fonts })
    for (const alter of [
      input => { input.roots[0].children[0].fonts[0].isCustomFont = false },
      input => { input.roots[0].children[0].fonts[0].postScriptName = 'Missing-Bold' },
      input => { input.fontFaces[0].sha256 = '0'.repeat(64) },
      input => { input.roots[0].style['font-style'] = 'italic' },
      input => { delete input.roots[0].style['font-synthesis-weight'] },
      input => { input.roots[0].style['font-synthesis-weight'] = 'unexpected' },
      input => { input.roots[0].style['font-weight'] = 'NaN' },
      input => { input.environment.browser = 'UnknownBrowser/1' },
    ]) {
      const observation = structuredClone(observed); alter(observation)
      const { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused font evidence')
      const before = structuredClone([...graph.getAllNodes()]), measurer = getTextMeasurer()
      await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, fonts, renderer, collection.id), /font|face/)
      assert.deepEqual([...graph.getAllNodes()], before)
      assert.equal(getTextMeasurer(), measurer)
    }
  }
  const snapshot = source(text, { content: 'Actual synthetic italic', italic: true })
  const fonts = faces.filter(face => face.weight === 400)
  await assert.rejects(buildComponentDocument(snapshot, { examples: [text], fonts, browser, renderer, mode: 'light' }), /face.*synthesized/)
})

test('explicitly disabled weight synthesis allows the actual regular face, not an invented bold face', async () => {
  for (const id of [button, text]) {
    const snapshot = source(id, id === button ? { label: 'Audit' } : { content: 'Audit', weight: 'semibold' })
    // A CSS observation fixture, not a claim that the Go constructor changes
    // synthesis or that a modified snapshot has authoritative source freshness.
    snapshot.css += '\nbody { font-synthesis-weight: none; }'
    const fonts = faces.filter(face => face.weight === 400)
    const built = await buildComponentDocument(snapshot, { examples: [id], fonts, browser, renderer, mode: 'light' })
    assert.equal(built.selections[0].observation.roots[0].style['font-synthesis-weight'], 'none')
    assert.equal(built.graph.getChildren(built.selections[0].instance.id)[0].fontWeight, 400)
  }
})
