import assert from 'node:assert/strict'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { SceneGraph } from '@open-pencil/core'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { populateAllLazyFigImportRoots } from '@open-pencil/core/kiwi'
import { loadFonts } from '../fonts.mjs'
import { chain } from '../exporter-correction.mjs'
import { suppliedFonts } from './fixtures.test.mjs'

// Provider primitive only: these native fixtures do not establish source
// materialization, runtime shell fidelity or support for the Collect creator.
const source = extra => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
  schema: 'platformkit.design-export.v1', scope: 'source-composition-observed-aliases', ...extra,
}) }]
const fonts = suppliedFonts([400]), short = 'A small collection', long = 'A small collection of stories shared by friends. '.repeat(5).trim()
const previousMeasurer = getTextMeasurer()
let browser, renderer, ck
before(async () => {
  browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  ck = await initCanvasKit()
  await loadFonts(fonts, [{ family: 'IBM Plex Sans', weight: 400, style: 'normal', text: long }])
  renderer = new SkiaRenderer(ck, ck.MakeSurface(640, 900))
  await renderer.loadFonts()
  setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
})
after(async () => { renderer?.destroy(); setTextMeasurer(previousMeasurer); await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0, 'independent browser documents are closed'))

function fragment(graph, parent, name) {
  return graph.createNode('COMPONENT', parent.id, { name, layoutMode: 'NONE',
    primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG', width: 0, height: 0,
    fills: [], strokes: [], clipsContent: false, pluginData: source({ cssFragment: { version: 1 } }),
  })
}

function rectangle(graph, parent, name, width) {
  return graph.createNode('RECTANGLE', parent.id, { name, width, height: 24, fills: [], strokes: [] })
}

function frame(graph, page, name, width, mode = 'HORIZONTAL', type = 'FRAME') {
  return graph.createNode(type, page.id, { name, x: 30, y: 40, width, height: 1,
    layoutMode: mode, primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED',
    ...(mode === 'HORIZONTAL' ? { primaryAxisSizing: 'FIXED', counterAxisSizing: 'HUG', layoutWrap: 'WRAP' } : {}),
    paddingTop: 10, paddingRight: 10, paddingBottom: 10, paddingLeft: 10,
    itemSpacing: 8, counterAxisSpacing: 8, primaryAxisAlign: 'MIN', counterAxisAlign: 'MIN',
    ...(mode === 'GRID' ? { gridTemplateColumns: [{ sizing: 'FR', value: 1 }, { sizing: 'FR', value: 1 }],
      gridColumnGap: 8, gridRowGap: 8 } : {}),
    fills: [], strokes: [], clipsContent: false, pluginData: source({}),
  })
}

function named(graph, name, parent) {
  const values = parent ? graph.getChildren(parent.id) : [...graph.getAllNodes()]
  const matches = values.filter(node => node.name === name)
  assert.equal(matches.length, 1, `one fixture node ${name}`)
  return matches[0]
}

function leaves(graph, parent) {
  return graph.getChildren(parent.id).flatMap(node => ['INSTANCE', 'COMPONENT'].includes(node.type) ? leaves(graph, node) : [node])
}

function bounds(graph, node, parent) {
  const point = graph.getAbsolutePosition(node.id), origin = graph.getAbsolutePosition(parent.id)
  return { x: point.x - origin.x, y: point.y - origin.y, width: node.width, height: node.height }
}

function close(actual, expected, label) {
  for (const field of ['x', 'y', 'width', 'height']) assert.ok(Math.abs(actual[field] - expected[field]) <= 1 / 64,
    `${label}.${field}: native ${actual[field]}, independent browser ${expected[field]}`)
}

async function browserLayout(width, mode, text, gridPositions = {}) {
  const context = await browser.newContext({ viewport: { width: 640, height: 900 }, serviceWorkers: 'block' })
  const requests = []
  try {
    await context.route('**/*', route => { requests.push(route.request().resourceType()); return route.abort('blockedbyclient') })
    const page = await context.newPage()
    // The source has no element corresponding to either logical fragment.
    // Chromium receives ordinary siblings, independently of native hierarchy.
    await page.setContent(`<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; font-src 'none'">
      <style>*{box-sizing:border-box}body{margin:0}.parent{padding:10px;gap:8px;width:${width}px;
      display:${mode === 'GRID' ? 'grid;grid-template-columns:1fr 1fr' : `flex;flex-direction:${mode === 'VERTICAL' ? 'column' : 'row'};flex-wrap:wrap`}}
      .box{flex:none;height:24px}.copy{margin:0;font:400 16px/24px "IBM Plex Sans";width:100%}</style></head>
      <body><div class="parent"><div class="box" data-name="before" style="width:36px"></div>
      ${text === undefined ? '<div class="box" data-name="first" style="width:52px"></div><div class="box" data-name="second" style="width:64px"></div>' : '<p class="copy" data-name="copy"></p>'}
      <div class="box" data-name="after" style="width:40px"></div></div></body></html>`)
    const observed = await page.evaluate(async ({ fonts, text, gridPositions }) => {
      for (const font of fonts) {
        const face = new FontFace(font.family, Uint8Array.from(font.bytes), { weight: String(font.weight), style: font.style })
        await face.load(); document.fonts.add(face)
      }
      if (text !== undefined) document.querySelector('.copy').textContent = text
      for (const node of document.querySelectorAll('[data-name]')) {
        const position = gridPositions[node.dataset.name]
        if (!position) continue
        node.style.removeProperty('width')
        node.style.gridColumn = `${position.column} / span ${position.columnSpan}`
        node.style.gridRow = `${position.row} / span ${position.rowSpan}`
      }
      await document.fonts.ready
      const parent = document.querySelector('.parent'), origin = parent.getBoundingClientRect()
      const box = node => { const rect = node.getBoundingClientRect(); return {
        x: rect.x - origin.x, y: rect.y - origin.y, width: rect.width, height: rect.height,
      } }
      return { parent: box(parent), children: Object.fromEntries([...parent.children].map(node => [node.dataset.name, box(node)])) }
    }, { fonts: fonts.map(font => ({ ...font, bytes: [...font.bytes] })), text, gridPositions })
    assert.deepEqual(requests, [])
    return observed
  } finally { await context.close() }
}

function geometricFixture(width, mode) {
  const graph = new SceneGraph(), definitions = graph.addPage('Definitions'), page = graph.addPage('Placements')
  const empty = fragment(graph, definitions, 'Empty definition'), nested = fragment(graph, definitions, 'Nested definition')
  rectangle(graph, nested, 'second', 64)
  const master = fragment(graph, definitions, 'Fragment definition')
  rectangle(graph, master, 'first', 52)
  graph.createInstance(empty.id, master.id, { name: 'Empty instance' })
  graph.createInstance(nested.id, master.id, { name: 'Nested instance' })
  const parent = frame(graph, page, 'Parent', width, mode)
  rectangle(graph, parent, 'before', 36)
  graph.createInstance(master.id, parent.id, { name: 'Editable fragment' })
  rectangle(graph, parent, 'after', 40)
  return { graph, parent }
}

async function verifyGeometry(graph, width, mode, gridPositions, parentName = 'Parent', recompute = true) {
  const parent = named(graph, parentName), expected = await browserLayout(width, mode, undefined, gridPositions)
  if (recompute) computeLayout(graph, parent.id)
  close(bounds(graph, parent, parent), expected.parent, 'parent')
  const actual = leaves(graph, parent)
  assert.deepEqual(actual.map(node => node.name), Object.keys(expected.children), 'nested and empty identities add no visual items')
  for (const node of actual) close(bounds(graph, node, parent), expected.children[node.name], node.name)
  if (!recompute) return // Verify saved bytes without repairing their layout caches.
  const once = structuredClone([...graph.getAllNodes()])
  computeLayout(graph, parent.id)
  assert.deepEqual([...graph.getAllNodes()], once, 'layout is idempotent')
}

test('independent browser fixtures agree with ordinary native siblings before introducing fragments', async () => {
  for (const mode of ['HORIZONTAL', 'GRID']) for (const width of [140, 260]) {
    const graph = new SceneGraph(), page = graph.addPage('Ordinary siblings'), parent = frame(graph, page, 'Parent', width, mode)
    for (const [name, size] of [['before', 36], ['first', 52], ['second', 64], ['after', 40]]) rectangle(graph, parent, name, size)
    await verifyGeometry(graph, width, mode)
  }
})

for (const mode of ['HORIZONTAL', 'GRID']) test(`native fragments preserve browser ${mode} sibling participation through two FIG saves`, async () => {
  for (const width of [140, 260]) {
    let { graph } = geometricFixture(width, mode)
    for (let cycle = 0; cycle < 3; cycle++) {
      await verifyGeometry(graph, width, mode)
      const placed = named(graph, 'Editable fragment'), master = chain(graph, placed, 'componentId').at(-1)
      assert.equal(placed.type, 'INSTANCE'); assert.equal(master.type, 'COMPONENT')
      assert.deepEqual(JSON.parse(master.pluginData.find(item => item.key === 'platformkit.source').value).cssFragment, { version: 1 })
      assert.equal(chain(graph, named(graph, 'Nested instance', placed), 'componentId').at(-1).type, 'COMPONENT')
      if (cycle < 2) {
        graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        await verifyGeometry(graph, width, mode, undefined, 'Parent', false)
      }
    }
  }
})

test('native fragment text growth follows its effective parent and preserves masters and sibling instances after reopen', async () => {
  let graph = new SceneGraph()
  const definitions = graph.addPage('Definitions'), page = graph.addPage('Text placements')
  const master = fragment(graph, definitions, 'Text fragment definition')
  graph.updateNode(master.id, { componentPropertyDefinitions: [{ id: '1:100', name: 'message', type: 'TEXT', defaultValue: short }] })
  graph.createNode('TEXT', master.id, { name: 'copy', text: short, fontFamily: 'IBM Plex Sans', fontWeight: 400,
    fontSize: 16, lineHeight: 24, width: 120, height: 24, textAutoResize: 'HEIGHT', layoutAlignSelf: 'STRETCH',
    componentPropertyReferences: [{ propertyId: '1:100', field: 'TEXT' }], fills: [],
  })
  for (const [name, width] of [['Edited parent', 140], ['Sibling parent', 260]]) {
    const parent = frame(graph, page, name, width, 'VERTICAL')
    rectangle(graph, parent, 'before', 36)
    graph.createInstance(master.id, parent.id, { name: name === 'Edited parent' ? 'Edited fragment' : 'Sibling fragment' })
    rectangle(graph, parent, 'after', 40)
    computeLayout(graph, parent.id)
  }
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  const edited = named(graph, 'Edited fragment'), parent = named(graph, 'Edited parent'), sibling = named(graph, 'Sibling parent')
  const protectedIds = new Set([sibling.id, ...leaves(graph, sibling).map(node => node.id), named(graph, 'Sibling fragment').id,
    ...[...graph.getAllNodes()].filter(node => node.type === 'COMPONENT' || graph.getNode(node.parentId)?.type === 'COMPONENT').map(node => node.id)])
  const before = structuredClone([...graph.getAllNodes()].filter(node => protectedIds.has(node.id)))
  const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
  const property = editor.getInstanceComponentPropertyDefinitions(edited.id).find(item => item.name === 'message')
  const previous = structuredClone([...graph.getAllNodes()])
  editor.setInstanceComponentProperty(edited.id, property.id, long)
  const changed = structuredClone([...graph.getAllNodes()])
  editor.undoAction(); await Promise.resolve()
  assert.deepEqual([...graph.getAllNodes()], previous, 'undo restores the complete pre-edit graph')
  editor.redoAction(); await Promise.resolve()
  assert.deepEqual([...graph.getAllNodes()], changed, 'redo restores the exact edited graph')
  for (const node of before) assert.deepEqual(graph.getNode(node.id), node, `unchanged ${node.name}`)
  assert.equal(named(graph, 'copy', edited).text, long)
  for (let cycle = 0; cycle < 3; cycle++) {
    for (const [name, width, content] of [['Edited parent', 140, long], ['Sibling parent', 260, short]]) {
      const parent = named(graph, name), expected = await browserLayout(width, 'VERTICAL', content)
      computeLayout(graph, parent.id)
      close(bounds(graph, parent, parent), expected.parent, `${name} height`)
      for (const node of leaves(graph, parent)) close(bounds(graph, node, parent), expected.children[node.name], `${name}/${node.name}`)
    }
    if (cycle < 2) {
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      for (const [name, width, content] of [['Edited parent', 140, long], ['Sibling parent', 260, short]]) {
        const parent = named(graph, name), expected = await browserLayout(width, 'VERTICAL', content)
        close(bounds(graph, parent, parent), expected.parent, `${name} saved height`)
        for (const node of leaves(graph, parent)) close(bounds(graph, node, parent), expected.children[node.name], `${name}/${node.name} saved`)
      }
    }
  }
})

test('fragment sizing survives unpopulated reads and deferred page population without fabricating a lazy context', async () => {
  let { graph } = geometricFixture(140, 'HORIZONTAL')
  await verifyGeometry(graph, 140, 'HORIZONTAL')
  for (let cycle = 0; cycle < 2; cycle++) {
    const bytes = await exportFigFile(graph)
    const unpopulated = await parseFigFile(bytes.slice().buffer, { populate: 'none' })
    const placed = named(unpopulated, 'Editable fragment'), master = chain(unpopulated, placed, 'componentId').at(-1)
    for (const node of [placed, master]) assert.deepEqual(
      [node.layoutMode, node.primaryAxisSizing, node.counterAxisSizing], ['NONE', 'HUG', 'HUG'])
    assert.equal(placed.childIds.length, 0)
    const untouched = structuredClone([...unpopulated.getAllNodes()])
    assert.equal(populateAllLazyFigImportRoots(unpopulated), false, 'populate:none intentionally retains no lazy import context')
    assert.deepEqual([...unpopulated.getAllNodes()], untouched)
    // Deferred population is supported by first-page, not by populate:none.
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'first-page' })
    assert.equal(named(graph, 'Editable fragment').childIds.length, 0)
    const canonical = named(graph, 'Fragment definition'), provenance = structuredClone(canonical.pluginData)
    assert.equal(populateAllLazyFigImportRoots(graph), true)
    assert.equal(populateAllLazyFigImportRoots(graph), false, 'already populated pages are not replayed')
    assert.deepEqual(canonical.pluginData, provenance)
    for (const width of [260, 140]) {
      graph.updateNode(named(graph, 'Parent').id, { width })
      await verifyGeometry(graph, width, 'HORIZONTAL')
    }
    const restored = named(graph, 'Editable fragment')
    assert.equal(chain(graph, restored, 'componentId').at(-1).id, canonical.id)
    assert.deepEqual([restored.primaryAxisSizing, restored.counterAxisSizing], ['HUG', 'HUG'])
  }
})

function spanFixture() {
  const position = (column, row, columnSpan = 1) => ({ column, row, columnSpan, rowSpan: 1 })
  const positions = { before: position(1, 1), first: position(1, 2, 2), second: position(1, 3), after: position(2, 3) }
  const graph = new SceneGraph()
  const page = graph.addPage('Grid placements'), parent = frame(graph, page, 'Parent', 140, 'GRID', 'COMPONENT')
  graph.updateNode(parent.id, { gridTemplateRows: [24, 24, 24].map(value => ({ sizing: 'FIXED', value })) })
  rectangle(graph, parent, 'before', 0)
  const outer = fragment(graph, parent, 'Grid fragment')
  fragment(graph, outer, 'Empty grid fragment')
  rectangle(graph, outer, 'first', 0)
  const inner = fragment(graph, outer, 'Nested grid fragment')
  rectangle(graph, inner, 'second', 0)
  rectangle(graph, parent, 'after', 0)
  for (const node of leaves(graph, parent)) graph.updateNode(node.id, {
    layoutAlignSelf: 'STRETCH', gridPosition: positions[node.name],
  })
  return { graph, parent, positions }
}

test('fragment grid members retain explicit track anchors and spans through two FIG saves', async () => {
  let { graph, positions } = spanFixture()
  for (let cycle = 0; cycle < 3; cycle++) {
    for (const width of [140, 260]) {
      graph.updateNode(named(graph, 'Parent').id, { width })
      await verifyGeometry(graph, width, 'GRID', positions)
      for (const node of leaves(graph, named(graph, 'Parent'))) assert.deepEqual(node.gridPosition, positions[node.name])
      assert.equal(named(graph, 'first').parentId, named(graph, 'Grid fragment').id)
      assert.equal(named(graph, 'second').parentId, named(graph, 'Nested grid fragment').id)
      assert.equal(named(graph, 'Empty grid fragment').childIds.length, 0)
    }
    if (cycle < 2) {
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      await verifyGeometry(graph, 260, 'GRID', positions, 'Parent', false)
    }
  }
})

test('placed fragment member span edits preserve sibling and master ownership across history and FIG saves', async () => {
  let { graph, parent, positions } = spanFixture()
  for (const name of ['Edited grid', 'Sibling grid']) graph.createInstance(parent.id, parent.parentId, { name })
  for (const name of ['Parent', 'Edited grid', 'Sibling grid']) await verifyGeometry(graph, 140, 'GRID', positions, name)
  graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  for (const name of ['Parent', 'Edited grid', 'Sibling grid']) await verifyGeometry(graph, 140, 'GRID', positions, name)
  const edited = named(graph, 'Edited grid'), first = leaves(graph, edited).find(node => node.name === 'first')
  const replacement = { column: 2, row: 2, columnSpan: 1, rowSpan: 1 }
  const actions = createEditor({ graph }), before = structuredClone([...graph.getAllNodes()])
  actions.updateNodeWithUndo(first.id, { gridPosition: replacement })
  await Promise.resolve()
  const changed = structuredClone([...graph.getAllNodes()])
  actions.undoAction(); await Promise.resolve()
  for (const node of before) assert.deepEqual(graph.getNode(node.id), node, `undo restores ${node.name}`)
  actions.redoAction(); await Promise.resolve()
  for (const node of changed) assert.deepEqual(graph.getNode(node.id), node, `redo restores ${node.name}`)
  graph.updateNode(first.id, { opacity: 0.75 })
  const unrelatedSource = structuredClone(first.source)
  actions.undoAction(); await Promise.resolve()
  assert.equal(first.opacity, 0.75, 'undo leaves unrelated authored values intact')
  assert.deepEqual(first.source, { ...unrelatedSource, editedFields: unrelatedSource.editedFields.filter(field => field !== 'gridPosition') })
  actions.redoAction(); await Promise.resolve()
  assert.equal(first.opacity, 0.75)
  assert.deepEqual(first.source, unrelatedSource, 'redo restores only its own source marker')
  const protectedNodes = before.filter(node => !chain(graph, graph.getNode(node.id), 'parentId').some(owner => owner.id === edited.id))
  for (const node of protectedNodes) assert.deepEqual(graph.getNode(node.id), node, `unchanged ${node.name}`)
  for (let cycle = 0; cycle < 3; cycle++) {
    graph.syncInstances(named(graph, 'Parent').id)
    for (const name of ['Parent', 'Edited grid', 'Sibling grid']) {
      const expected = name === 'Edited grid' ? { ...positions, first: replacement } : positions
      for (const width of [140, 260]) {
        graph.updateNode(named(graph, name).id, { width })
        await verifyGeometry(graph, width, 'GRID', expected, name)
        for (const node of leaves(graph, named(graph, name))) assert.deepEqual(node.gridPosition, expected[node.name])
      }
    }
    if (cycle < 2) {
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      for (const name of ['Parent', 'Edited grid', 'Sibling grid']) await verifyGeometry(graph, 260, 'GRID',
        name === 'Edited grid' ? { ...positions, first: replacement } : positions, name, false)
    }
  }
})
