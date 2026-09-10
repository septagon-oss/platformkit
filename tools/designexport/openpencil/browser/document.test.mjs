import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { isDeepStrictEqual } from 'node:util'
import { exportCore, sourceFixture, suppliedFonts } from './fixtures.test.mjs'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { populateLazyFigImportRoots } from '@open-pencil/core/kiwi'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument, verifyComponentDocument } from '../document.mjs'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { chain } from '../exporter-correction.mjs'
import { extractSourceProps, extractSourceReplacement } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'

const form = 'pk-ui.component.form/default', button = 'pk-ui.component.button/with-leading-icon'
const examples = [form, button], viewport = { width: 320, height: 900 }
const fonts = suppliedFonts([400, 500, 600])
const originalMeasurer = getTextMeasurer()
let browser, ck, renderer, snapshot

function source(proposal) {
  return exportCore(proposal ? ['--proposal'] : [], proposal)
}

before(async () => {
  snapshot = source()
  browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  ck = await initCanvasKit()
  renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900))
})
after(async () => { renderer?.destroy(); setTextMeasurer(originalMeasurer); await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0))

function options(extra = {}) {
  return { examples, fonts, viewport, browser, renderer, ...extra }
}

function origin(node, key = 'platformkit.source') {
  const entries = node.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === key)
  assert.ok(entries.length <= 1, 'no duplicate provenance')
  return entries.length ? JSON.parse(entries[0].value) : null
}

function placed(graph, exampleId) {
  const matches = [...graph.getAllNodes()].filter(node => JSON.stringify(origin(node)?.path) === JSON.stringify([exampleId]))
  assert.equal(matches.length, 1, 'one exact associated source root across the entire document')
  return matches[0]
}

function nested(graph, root, localId) {
  const matches = graph.getChildren(root.id).filter(node => origin(node)?.localId === localId)
  assert.equal(matches.length, 1)
  return matches[0]
}

function property(graph, instance, field) {
  const master = chain(graph, instance, 'componentId').at(-1)
  const bindings = origin(master).textBindings.filter(item => item.property === field)
  assert.equal(bindings.length, 1, 'exact source binding, independent of mutable native names')
  return bindings[0].id
}

function vertical(nodes) {
  let y = 48
  for (const node of nodes) {
    assert.equal(node.x, 48)
    assert.equal(node.y, y)
    assert.ok(node.width > 0 && node.height > 0)
    y += node.height + 48
  }
}

for (const [field, choices] of [
  ['tone', ['info', 'danger']],
  ['size', ['sm', 'lg']],
]) test(`document generation assembles source ${field} families beside ordinary compositions`, async () => {
  const id = 'pk-ui.component.button/primary', selected = [id, form]
  const description = snapshot.examples.find(example => example.id === id)
  const baseline = description.props[field] ?? description.schema.properties[field].default
  const variants = choices.map(value => ({ exampleId: id, property: field,
    snapshot: source({ baseSHA256: snapshot.sha256, path: [id], props: { [field]: value } }) }))
  const before = structuredClone(variants)
  for (const mode of ['light', 'dark']) {
    const built = await buildComponentDocument(snapshot, options({ examples: selected, variants, mode }))
    const selection = built.selections[0], family = selection.family
    assert.equal(family?.type, 'COMPONENT_SET', 'generation must assemble the requested family, not ignore it')
    assert.equal(family.parentId, built.definitions.id)
    assert.equal(selection.master.parentId, family.id)
    const states = built.graph.getChildren(family.id)
    assert.deepEqual(states.map(master => master.componentPropertyValues[field]), [baseline, ...choices])
    if (field === 'size') assert.ok(new Set(states.map(master => master.height)).size > 1, 'size variants change native geometry')
    assert.deepEqual(selection.properties, family.componentPropertyDefinitions)
    assert.ok(selection.components.every(component => component.properties.length === 0 && component.master.parentId === family.id))
    vertical(built.graph.getChildren(family.id))
    vertical(built.graph.getChildren(built.definitions.id))
    const expected = verifyComponentDocument(built.graph, snapshot, selected)
    let graph = built.graph
    for (let cycle = 0; cycle < 2; cycle++) {
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      verifyComponentDocument(graph, snapshot, selected, expected)
    }
    const actions = createEditor({ graph }), instance = placed(graph, id)
    actions.setCanvasKit(ck, renderer)
    const definitions = actions.getInstanceComponentPropertyDefinitions(instance.id)
    actions.setInstanceComponentProperty(instance.id, definitions.find(item => item.type === 'TEXT').id, 'Publish album')
    actions.setInstanceComponentProperty(instance.id, definitions.find(item => item.type === 'VARIANT').id, choices.at(-1))
    const proposal = { baseSHA256: snapshot.sha256, path: [id], props: { [field]: choices.at(-1), label: 'Publish album' } }
    assert.deepEqual(extractSourceProps(graph, instance, snapshot).proposal, proposal)
    const observed = await captureExample(browser, source(proposal), id, { mode, fonts, viewport })
    for (const dimension of ['width', 'height']) assert.ok(Math.abs(instance[dimension] - observed.roots[0].bounds[dimension]) <= 1 / 64,
      `${mode}/${field}/${dimension}: native ${instance[dimension]} versus source ${observed.roots[0].bounds[dimension]}; ` +
      JSON.stringify(graph.getChildren(instance.id).map(node => ({ text: node.text, fontSize: node.fontSize, lineHeight: node.lineHeight,
        width: node.width, height: node.height, source: node.source, scale: node.uniformScaleFactor }))))
    assert.equal(extractSourceProps(graph, placed(graph, form), snapshot).status, 'no-supported-changes')
  }
  assert.deepEqual(variants, before)
})

test('document generation refuses invalid or inconsistent variant requests instead of dropping them', async () => {
  const id = 'pk-ui.component.button/primary'
  const projected = source({ baseSHA256: snapshot.sha256, path: [id], props: { tone: 'info' } })
  const valid = { exampleId: id, property: 'tone', snapshot: projected }
  const stale = structuredClone(projected)
  stale.css += '\n/* different source stylesheet */'
  for (const variants of [null, {}, [{}], [{ ...valid, exampleId: form }], [{ ...valid, property: '' }],
    [{ ...valid, path: null }], [{ ...valid, path: [] }], [{ ...valid, path: [form] }],
    [valid, { ...valid, property: 'label' }], [valid, valid], [{ ...valid, snapshot }], [{ ...valid, snapshot: stale }]]) {
    const before = structuredClone({ snapshot, variants })
    await assert.rejects(buildComponentDocument(snapshot, options({ examples: [id], variants })), /Document/)
    assert.deepEqual({ snapshot, variants }, before)
  }
})

test('nested source families inherit through Form and FormActions without replacing siblings or parent definitions', async () => {
  const path = [form, 'actions', 'create'], choices = ['sm', 'lg']
  const variants = choices.map(size => ({ exampleId: form, path, property: 'size',
    snapshot: source({ baseSHA256: snapshot.sha256, path, props: { size } }) }))
  const before = structuredClone({ snapshot, variants })
  for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
    const profile = options({ examples: [form], variants, mode, viewport: { width, height: 900 } })
    const built = await buildComponentDocument(snapshot, profile)
    let graph = built.graph
    const selection = built.selections[0]
    const family = selection.families.find(item => JSON.stringify(item.path) === JSON.stringify(path))?.family
    assert.equal(family?.type, 'COMPONENT_SET')
    assert.equal(selection.family, undefined, 'the Form is not converted into duplicated whole-page variants')
    assert.equal(graph.getChildren(family.id).length, 3)
    assert.equal(graph.getChildren(built.definitions.id).filter(node => node.type === 'COMPONENT_SET').length, 1)
    assert.equal(selection.components.filter(item => item.path.length === 1).length, 1)
    const expected = verifyComponentDocument(graph, snapshot, [form])
    graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    verifyComponentDocument(graph, snapshot, [form], expected)
    const instance = placed(graph, form), actions = nested(graph, instance, 'actions')
    const target = nested(graph, actions, 'create'), sibling = nested(graph, actions, 'cancel')
    const preview = graph.createInstance(chain(graph, instance, 'componentId').at(-1).id, instance.parentId, { name: 'Untouched preview' })
    const master = chain(graph, target, 'componentId').at(-1), owner = graph.getNode(master.parentId)
    assert.equal(owner.type, 'COMPONENT_SET')
    const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
    const definitions = editor.getInstanceComponentPropertyDefinitions(target.id)
    const label = definitions.find(item => item.name === 'label'), size = definitions.find(item => item.name === 'size')
    assert.equal(size.type, 'VARIANT')
    const siblingProps = extractSourceProps(graph, sibling, snapshot)
    const protectedNodes = [...graph.getAllNodes()].filter(node =>
      chain(graph, node, 'parentId').some(parent => parent.id === preview.id || ['COMPONENT', 'COMPONENT_SET'].includes(parent.type)))
    const protectedBefore = structuredClone(protectedNodes)
    editor.setInstanceComponentProperty(target.id, label.id, 'Publish album')
    for (const choice of ['lg', 'sm', '']) {
      const prior = structuredClone([...graph.getAllNodes()])
      editor.setInstanceComponentProperty(target.id, size.id, choice)
      const changed = structuredClone([...graph.getAllNodes()])
      editor.undoAction(); assert.ok(isDeepStrictEqual([...graph.getAllNodes()], prior), 'undo restores the exact native occurrence')
      editor.redoAction(); assert.ok(isDeepStrictEqual([...graph.getAllNodes()], changed), 'redo restores the exact native occurrence')
      for (const node of protectedBefore) assert.deepEqual(graph.getNode(node.id), node, `unchanged ${node.name}`)
      assert.deepEqual(extractSourceProps(graph, sibling, snapshot), siblingProps, 'layout may move the sibling but must not edit it')
      const proposal = { baseSHA256: snapshot.sha256, path, props: { label: 'Publish album', ...(choice ? { size: choice } : {}) } }
      assert.deepEqual(extractSourceProps(graph, target, snapshot).proposal, proposal)
      const observed = await captureExample(browser, source(proposal), form, profile)
      const sourceActions = observed.roots[0].children.find(child => child.source?.path.at(-1) === 'actions')
      const sourceButton = sourceActions.children.find(child => child.source?.path.at(-1) === 'create')
      for (const [native, actual] of [[instance, observed.roots[0]], [actions, sourceActions], [target, sourceButton]]) {
        for (const dimension of ['width', 'height']) assert.ok(Math.abs(native[dimension] - actual.bounds[dimension]) <= 1 / 64,
          `${mode}/${width}/${choice}/${native.name}/${dimension}: ${native[dimension]} versus ${actual.bounds[dimension]}`)
      }
    }
    for (let cycle = 0; cycle < 2; cycle++) {
      graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      const reopened = nested(graph, nested(graph, placed(graph, form), 'actions'), 'create')
      assert.deepEqual(extractSourceProps(graph, reopened, snapshot).proposal,
        { baseSHA256: snapshot.sha256, path, props: { label: 'Publish album' } })
      assert.equal(extractSourceProps(graph, placed(graph, form), snapshot).status, 'no-supported-changes')
    }
  }
  assert.deepEqual({ snapshot, variants }, before)
})

test('failed nested family construction removes only its own masters, sets and derived variables', async () => {
  const path = [form, 'actions', 'create']
  const projected = source({ baseSHA256: snapshot.sha256, path, props: { tone: 'info' } })
  const observation = await captureExample(browser, snapshot, form, options())
  const request = { path, property: 'tone', snapshot: projected,
    observation: await captureExample(browser, projected, form, options()) }
  for (const variants of [[request, request], [request, { ...request, path: [form, 'missing'] }],
    [{ ...request, property: 'size' }]]) {
    const { graph, collection } = buildFoundation(snapshot)
    const parent = graph.createNode('FRAME', graph.getPages()[0].id, { name: 'Caller-owned definitions',
      variableModes: { [collection.id]: collection.modes.find(item => item.name === 'light').modeId } })
    const before = structuredClone({ nodes: [...graph.nodes], variables: [...graph.variables], collections: [...graph.variableCollections] })
    const measurer = getTextMeasurer()
    await assert.rejects(materializeComponent(graph, parent.id, snapshot, observation, fonts, renderer, collection.id, [], { variants }),
      /Source component binding:|Native component:/)
    assert.ok(isDeepStrictEqual({ nodes: [...graph.nodes], variables: [...graph.variables], collections: [...graph.variableCollections] }, before),
      'refusal must preserve the entire caller-owned graph and variable environment')
    assert.equal(getTextMeasurer(), measurer)
  }
})

test('one Form inherits independent Input and Button families through their existing construction paths', async () => {
  const choices = [{ path: [form, 'title'], property: 'value', value: 'field-notes' },
    { path: [form, 'actions', 'create'], property: 'tone', value: 'danger' }]
  const variants = choices.map(({ path, property, value }) => ({ exampleId: form, path, property,
    snapshot: source({ baseSHA256: snapshot.sha256, path, props: { [property]: value } }) }))
  const built = await buildComponentDocument(snapshot, options({ examples: [form], variants }))
  assert.equal(built.selections[0].families.length, 2)
  assert.equal(built.selections[0].components.filter(item => item.path.length === 1).length, 1)
  let graph = built.graph
  const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
  const at = path => path.slice(1).reduce((parent, localId) => nested(graph, parent, localId), placed(graph, path[0]))
  for (const { path, property, value } of choices) {
    const target = at(path)
    const definition = editor.getInstanceComponentPropertyDefinitions(target.id).find(item => item.name === property)
    assert.equal(definition.type, 'VARIANT')
    editor.setInstanceComponentProperty(target.id, definition.id, value)
  }
  for (let cycle = 0; cycle < 3; cycle++) {
    for (const { path, property, value } of choices) assert.deepEqual(extractSourceProps(graph, at(path), snapshot).proposal,
      { baseSHA256: snapshot.sha256, path, props: { [property]: value } })
    const texts = node => node.type === 'TEXT' ? [node.text] : graph.getChildren(node.id).flatMap(texts)
    assert.ok(texts(at([form, 'title'])).includes('field-notes'), 'Input must render its selected source value, not only report it')
    if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
  }
})

async function wrappingSource(t) {
  const run = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
)
func main() {
  var input struct { Props components.FlexProps; Label string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  var children []g.Node
  for _, item := range []struct{ id, label string }{
    {"cancel", "Return to library"}, {"save", input.Label}, {"preview", "Preview changes"},
  } {
    children = append(children, components.ExampleWithSlots(components.ExampleInfo{
      ID: item.id, ComponentID: "pk-ui.component.button",
    }, components.ButtonProps{Label: item.label, Variant: "secondary"},
      components.ButtonSlots{}, components.ButtonWithSlots).Node)
  }
  example := components.ExampleWithChildren(components.ExampleInfo{
    ID: "fixture/actions", ComponentID: "pk-ui.component.flex",
  }, input.Props, children, components.Flex)
  snapshot, err := ui.Export(design.Default(), []components.Example{example})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  return (props, label = 'Save album') => run({ props, label })
}

test('unsupported wrap direction, line alignment and child order reject atomically', async t => {
  const source = await wrappingSource(t), snapshot = source({ wrap: true, gap: '4' }), id = 'fixture/actions'
  const observation = await captureExample(browser, snapshot, id, { fonts, viewport })
  for (const change of [
    root => { root.style['flex-direction'] = 'column' },
    root => { root.style['flex-direction'] = 'row-reverse' },
    root => { root.style['flex-wrap'] = 'wrap-reverse' },
    root => { root.style['align-content'] = 'center' },
    root => { root.children[0].style.order = '1' },
    root => { delete root.children[0].style.order },
  ]) {
    const altered = structuredClone(observation)
    change(altered.roots[0])
    const { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused wrap')
    const before = structuredClone([...graph.getAllNodes()]), previous = getTextMeasurer()
    await assert.rejects(materializeComponent(graph, page.id, snapshot, altered, fonts, renderer, collection.id), /wrap/)
    assert.deepEqual([...graph.getAllNodes()], before)
    assert.equal(getTextMeasurer(), previous)
  }
})

test('wrapping rows and stretched columns keep linked actions, reflow and property history across two saves', async t => {
  const source = await wrappingSource(t), id = 'fixture/actions'
  function assertRow(graph, root, observed, stage) {
    assert.equal(root.layoutWrap, observed.style['flex-wrap'] === 'wrap' ? 'WRAP' : 'NO_WRAP')
    assert.equal(root.layoutMode, observed.style['flex-direction'] === 'column' ? 'VERTICAL' : 'HORIZONTAL')
    for (const field of ['width', 'height']) assert.ok(Math.abs(root[field] - observed.bounds[field]) <= 1 / 64,
      `${stage} root ${field}: ${root[field]} versus ${observed.bounds[field]}`)
    for (const child of observed.children) {
      const native = nested(graph, root, child.source.path.at(-1))
      assert.equal(native.type, 'INSTANCE')
      for (const field of ['x', 'y', 'width', 'height']) {
        const offset = ['x', 'y'].includes(field) ? observed.bounds[field] : 0
        assert.ok(Math.abs(native[field] - (child.bounds[field] - offset)) <= 1 / 64,
          `${stage} ${child.source.path.at(-1)} ${field}: ${native[field]} versus ${child.bounds[field] - offset}`)
      }
      const text = graph.getChildren(native.id)[0], region = child.children[0]
      assert.ok(Math.abs(text.x - (region.bounds.x - child.bounds.x)) <= 1 / 64,
        `${stage} ${child.source.path.at(-1)} label x: ${text.x} versus ${region.bounds.x - child.bounds.x}`)
      assert.ok(Math.abs(text.y - (region.bounds.y - child.bounds.y - (text.height - region.bounds.height) / 2)) <= 1 / 64,
        `${stage} ${child.source.path.at(-1)} label y`)
    }
  }
  const cases = [{ direction: 'column', wrap: false, gap: '4', align: 'stretch', justify: 'start' },
    ...['start', 'center', 'end', 'between'].map(justify => ({ wrap: true, gap: '4', align: 'center', justify }))]
  for (const mode of ['light', 'dark']) for (const props of cases) {
    const justify = props.direction ?? props.justify
    const snapshot = source(props), built = await buildComponentDocument(snapshot, options({ examples: [id], mode }))
    let { graph } = built
    graph.createInstance(chain(graph, placed(graph, id), 'componentId').at(-1).id, built.placements.id,
      { name: 'Untouched row preview', x: 400, y: 400 })
    for (const width of [320, 1280, 390]) {
      const instance = placed(graph, id), master = chain(graph, instance, 'componentId').at(-1)
      const originalMaster = structuredClone(master)
      const previous = getTextMeasurer()
      try {
        setTextMeasurer((node, maxWidth) => renderer.measureTextNode(node, maxWidth))
        graph.updateNode(instance.id, { width })
        graph.withLayoutMutations(() => graph.preserveSourceMetadataDuring(() => computeLayout(graph, instance.id)))
      } finally { setTextMeasurer(previous) }
      const baseline = await captureExample(browser, snapshot, id, { fonts, mode, viewport: { width, height: 900 } })
      assertRow(graph, instance, baseline.roots[0], `${mode}/${justify}/${width} before any text edit`)
      const untouched = structuredClone([...graph.getAllNodes()].filter(node =>
        !chain(graph, node, 'parentId').some(parent => parent.id === instance.id) &&
        !chain(graph, instance, 'parentId').some(parent => parent.id === node.id)))
      const editor = createEditor({ graph })
      editor.setCanvasKit(ck, renderer)
      const save = nested(graph, instance, 'save'), binding = property(graph, save, 'label')
      const before = structuredClone([...graph.getAllNodes()])
      editor.setInstanceComponentProperty(save.id, binding, 'Save a revised album')
      await Promise.resolve()
      const edited = structuredClone([...graph.getAllNodes()])
      editor.undoAction()
      await Promise.resolve()
      assert.deepEqual([...graph.getAllNodes()], before, 'undo restores the entire graph')
      editor.redoAction()
      await Promise.resolve()
      assert.deepEqual([...graph.getAllNodes()], edited, 'redo restores the entire graph')
      for (const node of untouched) assert.deepEqual(graph.getNode(node.id), node, `untouched master or sibling: ${node.name}`)
      const expected = await captureExample(browser, source(props, 'Save a revised album'), id, { fonts, mode, viewport: { width, height: 900 } })
      for (let cycle = 0; cycle < 3; cycle++) {
        const root = placed(graph, id)
        assertRow(graph, root, expected.roots[0], `${mode}/${justify}/${width} after save ${cycle}`)
        const target = nested(graph, root, 'save')
        assert.deepEqual(extractSourceProps(graph, target, snapshot).proposal,
          { baseSHA256: snapshot.sha256, path: [id, 'save'], props: { label: 'Save a revised album' } })
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
      const reopened = placed(graph, id), reopenedMaster = chain(graph, reopened, 'componentId').at(-1)
      assert.deepEqual([reopenedMaster.width, reopenedMaster.height], [originalMaster.width, originalMaster.height])
      assert.equal(graph.getChildren(nested(graph, reopenedMaster, 'save').id)[0].text, 'Save album')
      const reset = createEditor({ graph })
      reset.setCanvasKit(ck, renderer)
      const target = nested(graph, reopened, 'save')
      reset.setInstanceComponentProperty(target.id, property(graph, target, 'label'), 'Save album')
      await Promise.resolve()
    }
  }
})

test('document packages exact selections, foundation handles and ordered nonoverlapping source masters', async () => {
  const before = structuredClone(snapshot), beforeFonts = fonts.map(face => ({ ...face, bytes: Buffer.from(face.bytes) }))
  for (const mode of ['light', 'dark']) {
    const selected = mode === 'light' ? examples : examples.toReversed()
    const previous = getTextMeasurer(), built = await buildComponentDocument(snapshot, options({ examples: selected, mode }))
    const { graph, collection, definitions, placements, selections } = built
    assert.equal(getTextMeasurer(), previous)
    assert.notEqual(definitions.id, placements.id)
    assert.equal(graph.getPages().length, 3)
    assert.equal(graph.variables.size, 28)
    assert.equal(built.icons.size, 27)
    assert.deepEqual(selections.map(item => item.exampleId), selected)
    const components = selections.flatMap(item => item.components)
    assert.equal(components.length, 6, 'Form has five occurrence-specific masters; Button has one')
    assert.deepEqual(components.map(item => item.path).toSorted(), [
      [button], [form], [form, 'actions'], [form, 'actions', 'cancel'], [form, 'actions', 'create'], [form, 'title'],
    ].toSorted())
    assert.equal([...graph.getAllNodes()].filter(node => node.type === 'COMPONENT').length, 33)
    vertical(components.map(item => item.master))
    vertical(selections.map(item => item.instance))
    const modeId = collection.modes.find(item => item.name === mode).modeId
    for (const page of [definitions, placements]) assert.equal(graph.getNodeVariableModeId(page.id, collection.id), modeId)
    for (const item of components) {
      assert.equal(item.master.parentId, definitions.id)
      assert.equal(graph.getNode(item.master.id), item.master)
      assert.equal(origin(item.master).sha256, snapshot.sha256)
      assert.equal(origin(item.master).mode, mode)
      assert.deepEqual(origin(item.master).viewport, viewport)
      assert.deepEqual(origin(item.master).fontFaces.map(face => face.sha256), fonts.map(face => face.sha256))
      assert.ok(!Object.hasOwn(origin(item.master), 'path'), 'reusable masters never claim an absolute occurrence')
      assert.deepEqual(origin(item.master).definitionPath, item.path, 'definitions identify their exact source capture, not a placement')
      assert.deepEqual(item.master.componentPropertyDefinitions, item.properties)
    }
    for (const item of selections) {
      assert.equal(item.observation.sourceSHA, snapshot.sha256)
      assert.equal(item.instance.parentId, placements.id)
      assert.equal(graph.getNode(item.instance.componentId), item.master)
      assert.equal(placed(graph, item.exampleId), item.instance)
      assert.equal(extractSourceProps(graph, item.instance, snapshot).status, 'no-supported-changes')
    }
    assert.doesNotThrow(() => verifyComponentDocument(graph, snapshot, selected))
    const selectedButton = selections.find(item => item.exampleId === button)
    const asset = graph.getChildren(selectedButton.instance.id).find(node => node.type === 'INSTANCE')
    assert.equal(chain(graph, asset, 'componentId').at(-1), built.icons.get('plus'))
    assert.equal(graph.variables.get(selectedButton.master.boundVariables['fills/0/color']).name, '--pk-color-accent-default')
  }
  assert.deepEqual(snapshot, before)
  assert.deepEqual(fonts, beforeFonts)
})

for (const populate of ['all', 'first-page']) test(`packaged instances remain editable and reusable through two saves without changing source masters or siblings: ${populate}`, async () => {
  const built = await buildComponentDocument(snapshot, options()), beforeSource = structuredClone(snapshot)
  let graph = built.graph
  const baseline = built.selections.flatMap(item => item.components).map(item => ({
    path: item.path, x: item.master.x, y: item.master.y, props: structuredClone(origin(item.master).props),
  }))
  const originalButtonLabel = snapshot.examples.find(item => item.id === button).props.label
  if (populate === 'first-page') graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate })
  for (let round = 0; round < 3; round++) {
    const root = placed(graph, form), selectedButton = placed(graph, button)
    if (populate === 'first-page') {
      populateLazyFigImportRoots(graph, [chain(graph, root, 'parentId').find(node => node.type === 'CANVAS').id])
    }
    const field = nested(graph, root, 'title'), valueID = property(graph, field, 'value')
    const labelID = property(graph, selectedButton, 'label')
    if (round > 0) {
      assert.equal(extractSourceProps(graph, field, snapshot).proposal.props.value, `Packaged input after save ${round - 1}`)
      assert.equal(extractSourceProps(graph, selectedButton, snapshot).proposal.props.label, `Packaged action ${round - 1}`)
      const collection = [...graph.variableCollections.values()][0]
      assert.equal(collection.modes.find(item => item.modeId === graph.getNodeVariableModeId(root.id, collection.id)).name, 'light')
    }
    const actions = createEditor({ graph })
    actions.setCanvasKit(ck, renderer)
    const sibling = graph.createInstance(root.componentId, root.parentId, { x: 500, y: 48 })
    assert.equal(extractSourceProps(graph, sibling, snapshot).code, 'missing-binding')
    const siblingBefore = structuredClone([...graph.getAllNodes()].filter(node => chain(graph, node, 'parentId').includes(sibling)))
    const value = `Packaged input after save ${round}`, label = `Packaged action ${round}`
    const previous = field.componentPropertyAssignments[valueID] ?? ''
    actions.setInstanceComponentProperty(field.id, valueID, value)
    actions.undoAction()
    assert.equal(field.componentPropertyAssignments[valueID] ?? '', previous)
    actions.redoAction()
    actions.setInstanceComponentProperty(selectedButton.id, labelID, label)
    const beforeGraph = structuredClone([...graph.getAllNodes()])
    const proposal = extractSourceProps(graph, field, snapshot).proposal
    assert.deepEqual(proposal, { baseSHA256: snapshot.sha256, path: [form, 'title'], props: { value } })
    assert.deepEqual(extractSourceProps(graph, selectedButton, snapshot).proposal.props, { label })
    assert.throws(() => verifyComponentDocument(graph, snapshot, examples), /proposal/)
    const projected = source(proposal)
    assert.equal(extractSourceProps(graph, root, projected).code, 'stale-base')
    assert.deepEqual(projected.examples.filter(item => item.id !== form), snapshot.examples.filter(item => item.id !== form))
    assert.deepEqual([...graph.getAllNodes()], beforeGraph, 'source checking is read-only')
    assert.deepEqual([...graph.getAllNodes()].filter(node => chain(graph, node, 'parentId').includes(sibling)), siblingBefore)
    for (const expected of baseline) {
      let occurrence = placed(graph, expected.path[0])
      for (const id of expected.path.slice(1)) occurrence = nested(graph, occurrence, id)
      const master = chain(graph, occurrence, 'componentId').at(-1)
      assert.deepEqual(origin(master).props, expected.props)
      assert.deepEqual([master.x, master.y], [expected.x, expected.y])
    }
    const buttonMaster = chain(graph, selectedButton, 'componentId').at(-1)
    assert.equal(buttonMaster.componentPropertyDefinitions.find(item => item.id === labelID).defaultValue, originalButtonLabel)
    assert.equal(graph.getChildren(selectedButton.id).filter(node => node.type === 'INSTANCE').length, 1)
    if (round < 2) {
      const bytes = await exportFigFile(graph)
      assert.deepEqual([...graph.getAllNodes()], beforeGraph, 'saving never replays imported state into the edited graph')
      graph = await parseFigFile(bytes.slice().buffer, { populate })
    }
  }
  assert.deepEqual(snapshot, beforeSource)
})

test('document refuses invalid or unsupported requested selections and fonts without returning a filtered library', async () => {
  const before = structuredClone(snapshot), previous = getTextMeasurer()
  const invalid = [
    { examples: [] }, { examples: [form, form] }, { examples: ['missing-example'] },
    { examples: [null] }, { examples: form }, { examples: [` ${form}`] },
    { mode: 'unknown' }, { viewport: { width: 0, height: 900 } },
    { fonts: [] }, { fonts: fonts.filter(face => face.weight !== 400) },
    { fonts: [{ ...fonts[0], sha256: '0'.repeat(64) }] },
  ]
  for (const change of invalid) {
    await assert.rejects(buildComponentDocument(snapshot, options(change)))
    assert.deepEqual(snapshot, before)
    assert.equal(browser.contexts().length, 0)
    assert.equal(getTextMeasurer(), previous)
  }
  const unsupported = 'pk-ui.component.badge/default'
  await assert.rejects(buildComponentDocument(snapshot, options({ examples: [form, unsupported] })),
    error => error.message.includes(unsupported))
  assert.deepEqual(snapshot, before)
  assert.equal(getTextMeasurer(), previous)
})

for (const mode of ['light', 'dark']) test(`generated Form replacement agrees with Go projection through two saves: ${mode}`, async () => {
  const primary = 'pk-ui.component.button/primary', path = [form, 'actions', 'create']
  const proposal = { baseSHA256: snapshot.sha256, path, replacementPath: [primary] }
  const projected = exportCore(['--replacement'], proposal)
  const observed = await captureExample(browser, projected, form, { fonts, viewport, mode })
  const sourceActions = observed.roots[0].children[1]
  const built = await buildComponentDocument(snapshot, options({ examples: [form, primary], mode }))
  let graph = built.graph
  const root = placed(graph, form), actions = nested(graph, root, 'actions'), target = nested(graph, actions, 'create')
  const cancel = nested(graph, actions, 'cancel'), initialID = target.id, initialOrigin = structuredClone(origin(target))
  const subtree = node => [node, ...graph.getChildren(node.id).flatMap(subtree)]
  const untouched = [...graph.getAllNodes()].filter(node => node.type === 'COMPONENT')
  const mastersBefore = structuredClone(untouched.map(subtree)), cancelBefore = structuredClone(subtree(cancel))
  const editor = createEditor({ graph })
  editor.setCanvasKit(ck, renderer)
  try {
    graph.swapInstanceComponent(target.id, built.selections.find(item => item.exampleId === primary).master.id)
    graph.withLayoutMutations(() => graph.preserveSourceMetadataDuring(() => computeLayout(graph, root.id)))
    await Promise.resolve()
    assert.equal(target.id, initialID)
    assert.deepEqual(origin(target), initialOrigin, 'destination retains its local source identity')
    assert.deepEqual(untouched.map(subtree), mastersBefore)
    // End alignment moves Cancel when needed; its content and styling stay intact.
    const currentCancel = structuredClone(subtree(cancel))
    for (const node of currentCancel) {
      const before = cancelBefore.find(item => item.id === node.id)
      assert.deepEqual({ ...node, x: before.x, y: before.y }, before)
    }
    for (let cycle = 0; cycle < 3; cycle++) {
      const currentActions = nested(graph, placed(graph, form), 'actions')
      for (const child of sourceActions.children) {
        const native = nested(graph, currentActions, child.source.path.at(-1))
        for (const field of ['width', 'height']) assert.ok(Math.abs(native[field] - child.bounds[field]) <= 1 / 64, `${field}, save ${cycle}`)
        for (const axis of ['x', 'y']) assert.ok(Math.abs(native[axis] - (child.bounds[axis] - sourceActions.bounds[axis])) <= 1 / 64,
          `${child.source.path.at(-1)} ${axis}: ${native[axis]} versus ${child.bounds[axis] - sourceActions.bounds[axis]}, save ${cycle}`)
      }
      const current = nested(graph, currentActions, 'create'), master = chain(graph, current, 'componentId').at(-1)
      assert.equal(origin(master).exampleId, primary)
      assert.equal(graph.getChildren(current.id)[0].text, 'Save')
      assert.deepEqual(origin(current), initialOrigin)
      const extracted = extractSourceReplacement(graph, current, snapshot)
      assert.deepEqual(extracted.proposal, proposal, JSON.stringify(extracted))
      const reprojected = exportCore(['--replacement'], extracted.proposal)
      assert.deepEqual(reprojected, projected, 'the extracted provider-neutral proposal reaches the owning Go operation')
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
  } finally { editor.replaceGraph(new (graph.constructor)()) }
})

test('source projection, not native replacement, decides cross-interface compatibility', async () => {
  const built = await buildComponentDocument(snapshot, options({ examples: [form] }))
  const { graph } = built, root = placed(graph, form), actions = nested(graph, root, 'actions')
  const target = nested(graph, actions, 'create')
  const replacement = built.selections[0].components.find(item => JSON.stringify(item.path) === JSON.stringify([form, 'title']))
  graph.swapInstanceComponent(target.id, replacement.master.id)
  const result = extractSourceReplacement(graph, target, snapshot)
  assert.deepEqual(result.proposal, { baseSHA256: snapshot.sha256, path: [form, 'actions', 'create'], replacementPath: [form, 'title'] })
  const before = structuredClone([...graph.getAllNodes()])
  assert.throws(() => execFileSync('go', ['run', './tools/designexport', '--replacement'], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', input: JSON.stringify(result.proposal), stdio: ['pipe', 'pipe', 'pipe'],
  }), error => error.status === 1 && error.stdout === '' && /different source interface/.test(error.stderr))
  assert.deepEqual([...graph.getAllNodes()], before, 'rejected source projection does not undo or mutate an editor document')
})

test('document checks construction-time correspondence across two saves and refuses complete property loss', async () => {
  const built = await buildComponentDocument(snapshot, options()), beforeSource = structuredClone(snapshot)
  let graph = built.graph
  const expected = verifyComponentDocument(graph, snapshot, examples), beforeExpected = structuredClone(expected)
  assert.equal(expected.length, 6, 'structural Form and actions retain their empty bindings alongside text owners')
  for (let save = 0; save < 2; save++) {
    graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    const before = structuredClone([...graph.getAllNodes()])
    assert.deepEqual(verifyComponentDocument(graph, snapshot, examples, expected), expected)
    assert.deepEqual([...graph.getAllNodes()], before, 'successful verification is read-only')
  }
  const instance = placed(graph, button), master = chain(graph, instance, 'componentId').at(-1)
  const id = property(graph, instance, 'label'), metadata = origin(master)
  metadata.textBindings = metadata.textBindings.filter(binding => binding.id !== id)
  graph.updateNode(master.id, {
    componentPropertyDefinitions: master.componentPropertyDefinitions.filter(definition => definition.id !== id),
    pluginData: master.pluginData.map(entry => entry.pluginId === 'platformkit' && entry.key === 'platformkit.source'
      ? { ...entry, value: JSON.stringify(metadata) } : entry),
  })
  for (const node of graph.getAllNodes()) {
    if (node.componentPropertyReferences.some(reference => reference.propertyId === id)) {
      graph.updateNode(node.id, { componentPropertyReferences: node.componentPropertyReferences.filter(reference => reference.propertyId !== id) })
    }
  }
  assert.equal(extractSourceProps(graph, instance, snapshot).status, 'no-supported-changes')
  assert.doesNotThrow(() => verifyComponentDocument(graph, snapshot, examples), 'self-consistency cannot prove a lost capability survived')
  const before = structuredClone([...graph.getAllNodes()])
  assert.throws(() => verifyComponentDocument(graph, snapshot, examples, expected), /correspondence changed/)
  assert.deepEqual([...graph.getAllNodes()], before, 'rejection does not repair the damaged graph')
  assert.deepEqual(expected, beforeExpected)
  assert.deepEqual(snapshot, beforeSource)
})
