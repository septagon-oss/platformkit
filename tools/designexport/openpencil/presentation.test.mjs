import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { posix } from 'node:path'
import { test } from 'node:test'
import { pathToFileURL } from 'node:url'
import { DOMParser } from '@xmldom/xmldom'
import { SceneGraph } from '@open-pencil/scene-graph'
import { renderNodesToPPTX } from '@open-pencil/core/io'

const require = createRequire(import.meta.resolve('@open-pencil/core'))
const { unzipSync, strFromU8 } = require('fflate')
const manifestURL = new URL('../package.json', pathToFileURL(require.resolve('pptxgenjs')))
const manifest = JSON.parse(readFileSync(manifestURL))
const drawingNS = 'http://schemas.openxmlformats.org/drawingml/2006/main'
const slideNS = 'http://schemas.openxmlformats.org/presentationml/2006/main'
const relationshipNS = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships'
const packageNS = 'http://schemas.openxmlformats.org/package/2006/relationships'
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/a9sAAAAASUVORK5CYII=', 'base64')
const solid = (r, g, b) => ({ type: 'SOLID', color: { r, g, b, a: 1 }, opacity: 1, visible: true })

function elements(node, namespace, name) {
  return [...node.getElementsByTagNameNS(namespace, name)]
}

function xml(files, name) {
  assert.ok(files[name], `package contains ${name}`)
  return new DOMParser({ onError: (_level, message) => { throw new Error(message) } })
    .parseFromString(strFromU8(files[name]), 'application/xml')
}

function fixture() {
  const graph = new SceneGraph()
  const page = graph.getPages()[0]
  const frame = graph.createNode('FRAME', page.id, {
    name: 'Presentation', width: 960, height: 540, fills: [solid(1, 1, 1)],
  })
  graph.createNode('TEXT', frame.id, {
    text: 'Editable & <Title>', x: 48, y: 24, width: 480, height: 48,
    fontFamily: 'Inter', fontSize: 24, fills: [solid(0, 0, 0)],
    styleRuns: [{ start: 0, length: 8, style: { fontWeight: 700 } }],
  })
  graph.createNode('RECTANGLE', frame.id, {
    x: 48, y: 96, width: 192, height: 96, fills: [solid(1, 0, 0)],
  })
  graph.createNode('ELLIPSE', frame.id, {
    x: 288, y: 96, width: 96, height: 96, fills: [solid(0, 0, 1)],
  })
  graph.createNode('LINE', frame.id, {
    x: 48, y: 300, width: 192, height: 0,
    strokes: [{ color: { r: 0, g: 1, b: 0, a: 1 }, weight: 2, opacity: 1, visible: true }],
  })
  const vector = graph.createNode('VECTOR', frame.id, { x: 480, y: 96, width: 48, height: 48 })
  graph.createNode('TEXT', frame.id, { text: 'Hidden', visible: false })
  return { graph, page, frame, vector }
}

test('SDK presentation dependency is the reviewed fork without image-size', () => {
  assert.equal(manifest.name, '@neo-ma/pptxgenjs')
  assert.equal(manifest.version, '4.3.0')
  assert.equal(manifest.license, 'MIT')
  assert.deepEqual(manifest.dependencies, { jszip: '^3.10.1' })
  assert.throws(() => require.resolve('image-size'), { code: 'MODULE_NOT_FOUND' })
})

test('actual SDK exports editable text and shapes, geometry, and related PNG fallback without mutating its graph', async () => {
  const { graph, page, frame, vector } = fixture()
  const before = structuredClone([...graph.getAllNodes()])
  const calls = []
  let stats
  const bytes = await renderNodesToPPTX(graph, page.id, [frame.id], {
    // The explicit callback tests the SDK/package image boundary, not raster rendering.
    rasterize: async (...args) => { calls.push(args); return png },
    onStats: value => { stats = value },
  })
  assert.ok(bytes instanceof Uint8Array)
  assert.deepEqual(calls, [[[vector.id], 2, undefined]])
  assert.deepEqual(stats, { editable: 4, fallback: 1, skipped: 1, fallbackReasons: { 'node type VECTOR': 1 } })
  assert.deepEqual([...graph.getAllNodes()], before)
  const files = unzipSync(bytes)
  for (const name of Object.keys(files).filter(name => /\.(xml|rels)$/.test(name))) xml(files, name)
  const presentation = xml(files, 'ppt/presentation.xml')
  assert.equal(elements(presentation, slideNS, 'sldId').length, 1)
  const size = elements(presentation, slideNS, 'sldSz')[0]
  const width = Number(size.getAttribute('cx'))
  const height = Number(size.getAttribute('cy'))
  assert.ok(Math.abs(width / height - 16 / 9) < 1e-6)
  const slide = xml(files, 'ppt/slides/slide1.xml')
  assert.equal(elements(slide, drawingNS, 't').map(node => node.textContent).join(''), 'Editable & <Title>')
  assert.equal(elements(slide, slideNS, 'sp').length, 4, 'text and shapes remain editable, not flattened images')
  const runs = elements(slide, drawingNS, 'rPr')
  assert.equal(runs[0].getAttribute('b'), '1')
  assert.notEqual(runs[1].getAttribute('b'), '1')
  assert.equal(runs[0].getAttribute('sz'), '2400')
  assert.equal(elements(runs[0], drawingNS, 'latin')[0].getAttribute('typeface'), 'Inter')
  // A native shape may carry an empty text body; only actual text distinguishes the label.
  const shapes = elements(slide, slideNS, 'sp').filter(node => elements(node, drawingNS, 't').length === 0)
  assert.deepEqual(shapes.map(node => elements(node, drawingNS, 'prstGeom')[0].getAttribute('prst')), ['rect', 'ellipse', 'line'])
  for (const [index, color] of ['FF0000', '0000FF', '00FF00'].entries()) {
    assert.ok(elements(shapes[index], drawingNS, 'srgbClr').some(node => node.getAttribute('val') === color))
  }
  const rectangle = shapes[0]
  const offset = elements(rectangle, drawingNS, 'off')[0]
  const extent = elements(rectangle, drawingNS, 'ext')[0]
  for (const [actual, expected] of [
    [Number(offset.getAttribute('x')), width * 48 / 960],
    [Number(offset.getAttribute('y')), height * 96 / 540],
    [Number(extent.getAttribute('cx')), width * 192 / 960],
    [Number(extent.getAttribute('cy')), height * 96 / 540],
  ]) assert.ok(Math.abs(actual - expected) <= 1, 'native shape geometry follows slide dimensions')
  assert.equal(elements(slide, slideNS, 'pic').length, 1)
  const blip = elements(slide, drawingNS, 'blip')[0]
  const relationships = xml(files, 'ppt/slides/_rels/slide1.xml.rels')
  const image = elements(relationships, packageNS, 'Relationship')
    .find(node => node.getAttribute('Id') === blip.getAttributeNS(relationshipNS, 'embed'))
  assert.ok(image)
  assert.equal(image.getAttribute('Type'), `${relationshipNS}/image`)
  assert.deepEqual(Buffer.from(files[posix.join('ppt/slides', image.getAttribute('Target'))]), png)
})

test('SDK presentation keeps empty selection, unavailable raster, and raster errors explicit', async () => {
  const { graph, page, frame } = fixture()
  let rasterCalls = 0
  const rasterize = async () => { rasterCalls++; return null }
  assert.equal(await renderNodesToPPTX(graph, page.id, [], { rasterize }), null)
  assert.equal(await renderNodesToPPTX(graph, page.id, ['missing'], { rasterize }), null)
  assert.equal(rasterCalls, 0)
  let stats
  const bytes = await renderNodesToPPTX(graph, page.id, [frame.id], {
    rasterize, onStats: value => { stats = value },
  })
  assert.equal(rasterCalls, 1)
  assert.deepEqual(stats, { editable: 4, fallback: 0, skipped: 2, fallbackReasons: {} })
  assert.equal(elements(xml(unzipSync(bytes), 'ppt/slides/slide1.xml'), slideNS, 'pic').length, 0)
  const failure = new Error('fixture raster failure')
  await assert.rejects(renderNodesToPPTX(graph, page.id, [frame.id], {
    rasterize: async () => { throw failure },
  }), error => error === failure)
})

test('reviewed ESM and CommonJS package builds produce text and PNG presentations', async () => {
  const { default: esm } = await import(new URL(manifest.exports.import, manifestURL))
  for (const Constructor of [esm, require('pptxgenjs')]) {
    const presentation = new Constructor()
    const slide = presentation.addSlide()
    slide.addText('Retained & editable', { x: 1, y: 1, w: 3, h: 1 })
    slide.addImage({ data: `data:image/png;base64,${png.toString('base64')}`, x: 1, y: 3, w: 1, h: 1 })
    const bytes = await presentation.write({ outputType: 'arraybuffer' })
    assert.ok(bytes instanceof ArrayBuffer)
    const document = xml(unzipSync(new Uint8Array(bytes)), 'ppt/slides/slide1.xml')
    assert.equal(elements(document, drawingNS, 't')[0].textContent, 'Retained & editable')
    assert.equal(elements(document, slideNS, 'pic').length, 1)
  }
})
