import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { chain } from '../exporter-correction.mjs'
import { captureExample } from './capture.mjs'
import { parseFigBuffer } from '@open-pencil/fig'
import { exportCore as source, suppliedFonts } from './fixtures.test.mjs'

const id = 'pk-ui.component.text/muted'
const fonts = suppliedFonts([400, 500, 600, 700])
const originalMeasurer = getTextMeasurer()
let browser, renderer, ck
before(async () => {
  browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  ck = await initCanvasKit()
  renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900))
})
after(async () => { renderer?.destroy(); setTextMeasurer(originalMeasurer); await browser?.close() })

test('text blocks construct wrapped source with exact weights and keep paragraph semantics', async () => {
  const content = 'Album notes & memories. '.repeat(8).trim()
  for (const [exampleId, weight] of [[id, 400], ['pk-ui.component.text/loud', 700]]) {
    const snapshot = source(['--example', exampleId, '--props'], { content })
    const built = await buildComponentDocument(snapshot, {
      examples: [exampleId], fonts, browser, renderer, viewport: { width: 320, height: 900 },
    })
    const { graph } = built, item = built.selections[0], text = graph.getChildren(item.instance.id)[0]
    const observed = item.observation.roots[0]
    assert.ok(observed.children[0].rects.length > 1)
    assert.equal(text.fontWeight, weight)
    close(item.instance.height, observed.bounds.height, 'initial wrapped height')
    assert.deepEqual([text.x, text.y], [0, 0], 'wrapping text owns the content box, not the font rectangle')
    assert.equal(extractSourceProps(graph, item.instance, snapshot).status, 'no-supported-changes')
    const page = await browser.newPage({ viewport: { width: 320, height: 900 } })
    try {
      await page.setContent(`<style>${snapshot.css}</style>${snapshot.examples[0].html}`)
      const paragraph = page.getByRole('paragraph')
      assert.equal(await paragraph.textContent(), content)
      assert.deepEqual(await paragraph.evaluate(node => ({ tag: node.tagName, children: node.children.length,
        role: node.getAttribute('role'), hidden: node.getAttribute('aria-hidden') })),
      { tag: 'P', children: 0, role: null, hidden: null })
      assert.ok((await paragraph.ariaSnapshot()).includes(content), 'source content remains exposed to browser accessibility')
    } finally { await page.close() }
  }
})

test('unsupported text presentation fails without partial construction or invented whitespace semantics', async () => {
  for (const props of [
    { content: '' }, { content: ' ' }, { content: ' Leading space' }, { content: 'Two  spaces' },
    { content: 'Two\nlines' }, { truncate: true }, { align: 'justify' }, { lines: 2 },
  ]) {
    const snapshot = source(['--example', id, '--props'], props)
    const observation = await captureExample(browser, snapshot, id, { fonts })
    const { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused text')
    const before = structuredClone([...graph.getAllNodes()]), previous = getTextMeasurer()
    await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, fonts, renderer, collection.id))
    assert.deepEqual([...graph.getAllNodes()], before)
    assert.equal(getTextMeasurer(), previous)
  }
})

test('gallery construction coverage is explicit under the supplied-font comparison profile', async t => {
  const snapshot = source(), accepted = [], refused = [], captureRefused = []
  for (const example of snapshot.examples) {
    let observation
    try {
      observation = await captureExample(browser, snapshot, example.id, { fonts })
    } catch (error) {
      assert.equal(error.message.split('\n')[0], 'page.evaluate: Error: Capture does not support executable or externally composed example content',
        'unexpected capture failures are not support refusals')
      assert.equal(browser.contexts().length, 0, `refused capture closes its context: ${example.id}`)
      captureRefused.push(example.id)
      continue
    }
    const { graph, collection, icons } = buildFoundation(snapshot), page = graph.addPage('Coverage')
    const before = structuredClone([...graph.getAllNodes()])
    const visit = region => region.kind === 'slot' || region.tag === 'svg' ? [{ region,
      master: icons.get((region.kind === 'slot' ? region.children[0] : region)?.icon?.canonicalName) }] :
      (region.children ?? []).flatMap(visit)
    const targets = observation.roots.flatMap(visit)
    try {
      await materializeComponent(graph, page.id, snapshot, observation, fonts, renderer, collection.id, targets)
      accepted.push(example.id)
    } catch (error) {
      assert.match(error.message, /^Native component:/, 'unexpected failures are not support refusals')
      assert.deepEqual([...graph.getAllNodes()], before, `refused construction leaves no nodes: ${example.id}`)
      refused.push({ id: example.id, reason: error.message })
    }
  }
  t.diagnostic(JSON.stringify({ accepted, refused, captureRefused }))
  assert.deepEqual(accepted, [
    'pk-ui.component.alert/bordered', 'pk-ui.component.alert/compact', 'pk-ui.component.alert/danger',
    'pk-ui.component.alert/dismissible', 'pk-ui.component.alert/info', 'pk-ui.component.alert/success',
    'pk-ui.component.button/as-link', 'pk-ui.component.button/danger', 'pk-ui.component.button/disabled-link',
    'pk-ui.component.button/ghost', 'pk-ui.component.button/info', 'pk-ui.component.button/link', 'pk-ui.component.button/primary',
    'pk-ui.component.button/secondary', 'pk-ui.component.button/success', 'pk-ui.component.button/warning', 'pk-ui.component.button/with-icon',
    'pk-ui.component.button/with-leading-icon', 'pk-ui.component.form/default', 'pk-ui.component.grid/default',
    'pk-ui.component.input/bare', 'pk-ui.component.input/invalid', 'pk-ui.component.input/read-only',
    'pk-ui.component.select/default', 'pk-ui.component.select/invalid',
    'pk-ui.component.text/loud', 'pk-ui.component.text/muted',
    'pk-ui.component.textarea/invalid',
  ])
  assert.equal(refused.length, 84)
  const sectionRefusals = {
    'pk-ui.component.grid/responsive': 'Native component: typed, nonopaque source composition required',
    'pk-ui.component.heading/display': 'Native component: composition text requires one supplied actual face',
    'pk-ui.component.hero/default': 'Native component: typed, nonopaque source composition required',
    'pk-ui.component.section-header/default': 'Native component: inline composition cannot flatten a source component or non-inline child',
    'pk-ui.component.section/default': 'Native component: typed, nonopaque source composition required',
  }
  assert.deepEqual(Object.fromEntries(refused.filter(item => Object.hasOwn(sectionRefusals, item.id))
    .map(({ id, reason }) => [id, reason])), sectionRefusals)
  assert.deepEqual(captureRefused, ['pk-ui.component.video/default', 'pk-ui.component.video/disabled'])
  assert.equal(browser.contexts().length, 0)
})

test('the invalid Input retains its visible source error and editable value through two saves', async () => {
  const exampleId = 'pk-ui.component.input/invalid', snapshot = source()
  const example = snapshot.examples.find(item => item.id === exampleId)
  for (const mode of ['light', 'dark']) {
    const built = await buildComponentDocument(snapshot, {
      examples: [exampleId], fonts, browser, renderer, mode, viewport: { width: 320, height: 900 },
    })
    let { graph } = built
    for (let cycle = 0; cycle < 3; cycle++) {
      const instance = placement(graph, exampleId), descendants = []
      const visit = node => { descendants.push(node); for (const child of graph.getChildren(node.id)) visit(child) }
      visit(instance)
      assert.equal(descendants.filter(node => node.type === 'TEXT' && node.text === example.props.error).length, 1)
      assert.deepEqual(extractSourceProps(graph, instance, snapshot).properties.toSorted(), ['label', 'value'],
        'construction does not claim editable error properties without source bindings')
      close(instance.height, built.selections[0].observation.roots[0].bounds.height, 'invalid Input height')
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  }
})

function close(actual, expected, name) {
  assert.ok(Math.abs(actual - expected) <= 1 / 64, `${name}: ${actual} versus ${expected}`)
}

function placement(graph, exampleId) {
  return [...graph.getAllNodes()].find(node => node.type === 'INSTANCE' && node.pluginData.some(item =>
    item.pluginId === 'platformkit' && item.key === 'platformkit.source' && JSON.parse(item.value).path?.[0] === exampleId))
}

test('a reopened text instance reflows at a new container width without resizing its reusable master', async () => {
  const content = 'Record the people and places in this album. '.repeat(8).trim()
  const snapshot = source(['--example', id, '--props'], { content })
  const built = await buildComponentDocument(snapshot, {
    examples: [id], fonts, browser, renderer, viewport: { width: 320, height: 900 },
  })
  let graph = await parseFigFile((await exportFigFile(built.graph)).slice().buffer, { populate: 'all' })
  for (const width of [1280, 390, 320]) {
    const instance = placement(graph, id), master = chain(graph, instance, 'componentId').at(-1)
    const unchanged = structuredClone([master, ...graph.getChildren(master.id)])
    const previous = getTextMeasurer()
    try {
      setTextMeasurer((node, maxWidth) => renderer.measureTextNode(node, maxWidth))
      graph.updateNode(instance.id, { width })
      graph.withLayoutMutations(() => graph.preserveSourceMetadataDuring(() => computeLayout(graph, instance.id)))
    } finally { setTextMeasurer(previous) }
    assert.deepEqual(unchanged.map(node => graph.getNode(node.id)), unchanged)
    const observation = await captureExample(browser, snapshot, id, { fonts, viewport: { width, height: 900 } })
    for (let cycle = 0; cycle < 3; cycle++) {
      const current = placement(graph, id), text = graph.getChildren(current.id)[0]
      close(current.width, width, 'resized content width')
      close(current.height, observation.roots[0].bounds.height, 'resized block height')
      close(text.width, width, `resized text width at save cycle ${cycle}`)
      assert.equal(extractSourceProps(graph, current, snapshot).status, 'no-supported-changes')
      if (cycle < 2) {
        const bytes = await exportFigFile(graph)
        const raw = parseFigBuffer(bytes.slice().buffer).nodeChanges.find(node => node.type === 'INSTANCE' && node.name === id)
        assert.deepEqual(raw.derivedSymbolData.map(item => item.size), [{ x: width, y: observation.roots[0].bounds.height }])
        graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
      }
    }
  }
})

for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
  test(`editable source text wraps and reprojects through history and two saves: ${mode}, ${width}px`, async () => {
    const snapshot = source(), baseline = snapshot.examples.find(example => example.id === id).props.content
    const viewport = { width, height: 900 }
    const built = await buildComponentDocument(snapshot, { examples: [id], fonts, browser, renderer, mode, viewport })
    let { graph } = built, instance = placement(graph, id)
    const property = built.selections[0].properties[0].id
    const master = chain(graph, instance, 'componentId').at(-1)
    const sibling = graph.createInstance(master.id, built.placements.id, { name: 'Untouched preview', y: 450 })
    const otherNodes = structuredClone([master, ...graph.getChildren(master.id), sibling, ...graph.getChildren(sibling.id)])
    const values = ['This album records the people, places and small details we want to remember. '.repeat(4).trim(), 'A short description.']
    for (const content of values) {
      const editor = createEditor({ graph })
      editor.setCanvasKit(ck, renderer)
      const before = structuredClone([...graph.getAllNodes()])
      editor.setInstanceComponentProperty(instance.id, property, content)
      const edited = structuredClone([...graph.getAllNodes()])
      editor.undoAction()
      assert.deepEqual([...graph.getAllNodes()], before, 'undo restores text and wrapping geometry')
      editor.redoAction()
      assert.deepEqual([...graph.getAllNodes()], edited, 'redo restores the derived layout')
      for (let cycle = 0; cycle < 3; cycle++) {
        const result = extractSourceProps(graph, instance, snapshot)
        assert.deepEqual(result.proposal, { baseSHA256: snapshot.sha256, path: [id], props: { content } })
        const projected = source(['--proposal'], result.proposal)
        const observed = await captureExample(browser, projected, id, { mode, viewport, fonts })
        const root = observed.roots[0], text = graph.getChildren(instance.id)[0]
        close(instance.width, root.bounds.width, 'paragraph width')
        close(instance.height, root.bounds.height, 'paragraph height')
        close(text.width, root.bounds.width, 'wrapping text width')
        close(text.height, root.bounds.height, 'wrapping text height')
        assert.equal(text.textAutoResize, 'HEIGHT')
        assert.equal(text.text, content)
        const paragraph = renderer.buildParagraph(text, undefined, { halfLeading: true })
        try {
          const lines = paragraph.getLineMetrics(), rects = root.children[0].rects
          assert.equal(lines.length, rects.length, 'native and browser line breaks agree')
          for (const [index, line] of lines.entries()) close(line.width, rects[index].width, `line ${index} advance`)
        } finally { paragraph.delete() }
        if (width === 320 && content === values[0]) assert.ok(root.children[0].rects.length > 1, 'the source really wraps')
        if (cycle === 0 && content === values[0]) {
          assert.deepEqual(otherNodes.map(node => graph.getNode(node.id)), otherNodes, 'edits do not alter reusable definitions or previews')
        }
        if (cycle < 2) {
          graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
          instance = placement(graph, id)
        }
      }
      editor.replaceGraph(new (graph.constructor)())
    }
    assert.equal(snapshot.examples.find(example => example.id === id).props.content, baseline)
    assert.equal(browser.contexts().length, 0)
  })
}
