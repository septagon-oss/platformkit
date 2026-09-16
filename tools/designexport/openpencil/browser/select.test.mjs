import assert from 'node:assert/strict'
import { isDeepStrictEqual } from 'node:util'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { chain } from '../exporter-correction.mjs'
import { captureExample } from './capture.mjs'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'

async function selectSource(t) {
  return sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui/export"
  "github.com/septagon-oss/platformkit/ui/components"; "github.com/septagon-oss/platformkit/ui/components/examples"
)
func main() {
  var input struct { Props components.SelectProps; Proposal *export.PropsProposal }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  selectExample := examples.ExampleOf(examples.ExampleInfo{ID: "fixture/select", ComponentID: "pk-ui.component.select"}, input.Props, components.Select)
  field := examples.ExampleOf(examples.ExampleInfo{ID: "state", ComponentID: "pk-ui.component.select"}, input.Props, components.Select)
  form := examples.ExampleWithChildren(examples.ExampleInfo{ID: "fixture/form", ComponentID: "pk-ui.component.form"},
    components.FormProps{Label: "Album details", Action: "/albums"}, []gomponents.Node{field.Node}, components.Form)
  captures := []examples.Example{selectExample, form}
  snapshot, err := export.Export(design.Default(), captures)
  if input.Proposal != nil { _, snapshot, err = export.ProjectProps(design.Default(), captures, *input.Proposal) }
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
}

const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
function at(graph, path) {
  const root = [...graph.getAllNodes()].find(node => JSON.stringify(origin(node)?.path) === JSON.stringify(path.slice(0, 1)))
  return path.slice(1).reduce((parent, id) => descendants(graph, parent).find(node => origin(node)?.localId === id), root)
}

test('source Select retains native semantics and linked exact-value families alone and inside Form through two saves', async t => {
  const run = await selectSource(t), props = { name: 'state', label: 'Stage', value: 'draft', required: true,
    helpText: 'Choose the album stage.', options: [{ value: 'draft', label: 'Draft' }, { value: ' ready,a ', label: 'Ready' }] }
  const source = proposal => run({ props, proposal }), snapshot = source()
  const paths = [['fixture/select'], ['fixture/form', 'state']], examples = paths.map(path => path[0])
  const variants = paths.map(path => ({ exampleId: path[0], path, property: 'value',
    snapshot: source({ baseSHA256: snapshot.sha256, path, props: { value: ' ready,a ' } }) }))
  const before = structuredClone({ snapshot, variants })
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const fonts = suppliedFonts([400, 500, 600]), previousMeasurer = getTextMeasurer()
  let renderer
  try {
    const ck = await initCanvasKit(); renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
    for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const viewport = { width, height: 900 }, page = await browser.newPage({ viewport, colorScheme: mode })
      try {
        await page.setContent(`<html data-theme="${mode}"><style>${snapshot.css}</style><body>${snapshot.examples.find(item => item.id === 'fixture/form').html}</body></html>`)
        const select = page.getByRole('combobox', { name: 'Stage', exact: true })
        assert.equal(await select.evaluate(node => getComputedStyle(node).appearance), 'none')
        const indicator = page.locator('svg[data-pk-icon-canonical="caret-down"]')
        assert.equal(await indicator.count(), 1)
        assert.equal(await indicator.getAttribute('aria-hidden'), 'true')
        assert.equal(await indicator.evaluate(node => {
          const rect = node.getBoundingClientRect()
          return document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2)?.tagName === 'SELECT'
        }), true, 'the decoration must not intercept pointer selection')
        await page.keyboard.press('Tab')
        assert.equal(await select.evaluate(node => node === document.activeElement), true)
        const focus = await select.evaluate(node => {
          const style = getComputedStyle(node)
          return { width: style.outlineWidth, style: style.outlineStyle, visible: node.matches(':focus-visible'),
            ring: style.getPropertyValue('--pk-ring-width'), shadow: style.boxShadow }
        })
        assert.ok(focus.visible && focus.style !== 'none' && Number.parseFloat(focus.width) > 0, JSON.stringify(focus))
        assert.equal(focus.ring, '2px'); assert.notEqual(focus.shadow, 'none')
        await select.press('ArrowDown')
        assert.equal(await select.inputValue(), ' ready,a ')
        assert.deepEqual(await page.evaluate(() => new FormData(document.querySelector('form')).getAll('state')), [' ready,a '])
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
        await page.emulateMedia({ forcedColors: 'active' })
        assert.equal(await select.evaluate(node => getComputedStyle(node).forcedColorAdjust), 'auto')
        assert.ok(await select.evaluate(node => Number.parseFloat(getComputedStyle(node).outlineWidth) > 0))
        assert.notEqual(await indicator.locator('path').evaluate(node => getComputedStyle(node).fill),
          await select.evaluate(node => getComputedStyle(node).backgroundColor), 'system colors retain the indicator')
      } finally { await page.close() }
      const built = await buildComponentDocument(snapshot, { examples, variants, fonts, viewport, mode, browser, renderer })
      let graph = built.graph
      const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
      const protectedNodes = structuredClone([...graph.getAllNodes()].filter(node =>
        chain(graph, node, 'parentId').some(parent => ['COMPONENT', 'COMPONENT_SET'].includes(parent.type))))
      const target = at(graph, paths[1]), definitions = editor.getInstanceComponentPropertyDefinitions(target.id)
      const value = definitions.find(item => item.name === 'value'), label = definitions.find(item => item.name === 'label')
      assert.equal(value.type, 'VARIANT')
      assert.deepEqual(value.variantOptions, ['draft', ' ready,a '], 'stored values, not visible option labels')
      editor.setInstanceComponentProperty(target.id, label.id, 'Album stage')
      editor.setInstanceComponentProperty(target.id, value.id, ' ready,a ')
      editor.undoAction()
      assert.deepEqual(extractSourceProps(graph, target, snapshot).proposal,
        { baseSHA256: snapshot.sha256, path: paths[1], props: { label: 'Album stage' } })
      editor.redoAction()
      for (const node of protectedNodes) assert.deepEqual(graph.getNode(node.id), node, `protected ${node.name}`)
      const expected = await captureExample(browser, source({ baseSHA256: snapshot.sha256, path: paths[1],
        props: { value: ' ready,a ', label: 'Album stage' } }), paths[1][0], { fonts, viewport, mode })
      const observed = expected.roots[0].children[0], elements = node => [node, ...(node.children ?? []).flatMap(elements)]
      for (let cycle = 0; cycle < 3; cycle++) {
        const selected = at(graph, paths[1])
        assert.deepEqual(extractSourceProps(graph, selected, snapshot).proposal,
          { baseSHA256: snapshot.sha256, path: paths[1], props: { value: ' ready,a ', label: 'Album stage' } })
        assert.equal(extractSourceProps(graph, at(graph, paths[0]), snapshot).status, 'no-supported-changes')
        assert.ok(descendants(graph, selected).some(node => node.type === 'TEXT' && node.text === 'Ready'))
        const icons = descendants(graph, selected).filter(node => node.type === 'INSTANCE' &&
          chain(graph, node, 'componentId').at(-1)?.pluginData.some(item => item.key === 'platformkit.icon'))
        assert.equal(icons.length, 1, 'the Select indicator must inherit a canonical native asset')
        const control = descendants(graph, selected).find(node => node.name === 'Source select')
        for (const [native, source] of [[control, elements(observed).find(node => node.tag === 'select')],
          [icons[0], elements(observed).find(node => node.tag === 'svg')]]) {
          const position = graph.getAbsolutePosition(native.id), rootPosition = graph.getAbsolutePosition(selected.id)
          for (const axis of ['x', 'y']) assert.ok(Math.abs(position[axis] - rootPosition[axis] -
            (source.bounds[axis] - observed.bounds[axis])) <= 1 / 64, `${native.name}/${axis} cycle ${cycle}`)
          for (const axis of ['width', 'height']) assert.ok(Math.abs(native[axis] - source.bounds[axis]) <= 1 / 64)
        }
        editor.replaceGraph(graph)
        const definition = editor.getInstanceComponentPropertyDefinitions(selected.id).find(item => item.name === 'value')
        const iconState = structuredClone(icons[0])
        graph.updateNode(icons[0].id, { opacity: 0.25 })
        const authored = structuredClone([...graph.nodes])
        assert.throws(() => editor.setInstanceComponentProperty(selected.id, definition.id, 'draft'), /edited descendants requires subtree history/)
        assert.deepEqual([...graph.nodes], authored, 'a real private-asset edit must not be discarded by a value switch')
        graph.preserveSourceMetadataDuring(() => graph.updateNode(icons[0].id, { opacity: iconState.opacity, source: iconState.source }))
        editor.setInstanceComponentProperty(selected.id, definition.id, 'draft')
        assert.ok(descendants(graph, selected).some(node => node.type === 'TEXT' && node.text === 'Draft'))
        editor.undoAction()
        assert.ok(descendants(graph, selected).some(node => node.type === 'TEXT' && node.text === 'Ready'))
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
    }
    assert.ok(isDeepStrictEqual({ snapshot, variants }, before))
  } finally { renderer?.destroy(); setTextMeasurer(previousMeasurer); await browser.close() }
})

test('closed choices support empty and disabled states and refuse ambiguous or unsupported selection without graph leftovers', async t => {
  const run = await selectSource(t), fonts = suppliedFonts([400, 500])
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
  const base = { name: 'state', label: 'Stage', required: true, value: 'one', options: [{ value: 'one', label: 'One', group: 'Available' }] }
  try {
    for (const [props, accepted] of [
      [{ value: '', placeholder: 'Choose a stage' }, true], [{ value: '', options: [] }, true],
      [{ disabled: true, error: 'Stage is unavailable.' }, true],
      [{ multiple: true }, false], [{ visibleRows: 3 }, false], [{ value: 'missing' }, false],
      [{ value: '' }, false], [{ values: ['one'] }, false],
      [{ options: [{ value: 'one', label: 'One' }, { value: 'one', label: 'Another' }] }, false],
    ]) {
      const snapshot = run({ props: { ...base, ...props } }), id = 'fixture/select'
      const observation = await captureExample(browser, snapshot, id, { fonts })
      const { graph, collection, icons } = buildFoundation(snapshot), page = graph.addPage('Choice proof')
      const visit = node => node.tag === 'svg' ? [{ region: node, master: icons.get(node.icon?.canonicalName) }] :
        (node.children ?? []).flatMap(visit)
      const targets = observation.roots.flatMap(visit), before = structuredClone([...graph.nodes]), variables = structuredClone([...graph.variables])
      const construct = () => materializeComponent(graph, page.id, snapshot, observation, fonts, renderer, collection.id, targets)
      if (accepted) {
        const built = await construct()
        assert.ok(!built.properties.some(item => item.name === 'value'), 'unprojected values cannot masquerade as literal display text')
      } else {
        await assert.rejects(construct, /Native component:/, JSON.stringify(props))
        assert.deepEqual([...graph.nodes], before); assert.deepEqual([...graph.variables], variables)
      }
    }
    const snapshot = run({ props: base }), observation = await captureExample(browser, snapshot, 'fixture/select', { fonts })
    const { graph, collection, icons } = buildFoundation(snapshot), page = graph.addPage('Forged observation')
    const walk = node => [node, ...(node.children ?? []).flatMap(walk)]
    for (const mutate of [
      nodes => { nodes.find(node => node.tag === 'select').style.appearance = 'auto' },
      nodes => { nodes.find(node => node.tag === 'select').control.content.text = 'Not the label' },
      nodes => { nodes.find(node => node.tag === 'select').control.content.bounds.x++ },
      nodes => { nodes.find(node => node.tag === 'svg').attributes.viewBox = '0 0 25 24' },
    ]) {
      const forged = structuredClone(observation), nodes = forged.roots.flatMap(walk)
      mutate(nodes)
      const targets = nodes.filter(node => node.tag === 'svg').map(region => ({ region, master: icons.get(region.icon.canonicalName) }))
      const before = structuredClone([...graph.nodes])
      await assert.rejects(() => materializeComponent(graph, page.id, snapshot, forged, fonts, renderer, collection.id, targets), /Native (component|icon composition):/)
      assert.deepEqual([...graph.nodes], before)
    }
  } finally { renderer.destroy(); await browser.close() }
})
