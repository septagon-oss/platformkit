import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { test } from 'node:test'
import { SceneGraph, generateId } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import * as bindings from './bindings.mjs'
import { verifyComponentDocument } from './document.mjs'
import { associateSourceInstance, extractSourceProps, extractSourceReplacement } from './source-changes.mjs'

const { bindComponentProperties, isSourceTextProperty, sourceTextValue } = bindings
const snapshot = JSON.parse(execFileSync('go', [
  'run', './tools/designexport', '--example', 'pk-ui.component.button/primary',
], { cwd: new URL('../../../', import.meta.url), encoding: 'utf8' }))
const example = snapshot.examples[0]

function fixture(value = 'Save') {
  const graph = new SceneGraph()
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Fixture master', width: 100, height: 40 })
  const lookalike = graph.createNode('TEXT', master.id, { name: 'label', text: 'Save', width: 40, height: 20 })
  const nativeNode = graph.createNode('TEXT', master.id, { name: 'unrelated layer name', text: value, width: 40, height: 20 })
  const region = { kind: 'text', property: 'label', text: value }
  return { graph, master, lookalike, targets: [{ region, nativeNode }] }
}

function sourceMetadata(node) {
  const entries = node.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
  assert.equal(entries.length, 1)
  return JSON.parse(entries[0].value)
}

function variantFixture(property = 'tone') {
  const canonical = structuredClone(snapshot), source = canonical.examples[0]
  source.props[property] = property === 'label' ? 'Save' : 'neutral'
  const input = fixture(), { graph } = input
  graph.deleteNode(input.master.id)
  const owner = graph.createNode('COMPONENT_SET', graph.getPages()[0].id, { name: 'Source choices' })
  const variants = [source.props[property], ' padded,a ', ''].map((value, index) => {
    const projected = structuredClone(canonical)
    projected.sha256 = String(index + 1).repeat(64)
    projected.examples[0].props[property] = value
    const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: `Unrelated state ${index}`, width: 100, height: 40,
      pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
        schema: projected.schema, sha256: projected.sha256, exampleId: source.id, componentId: source.componentId,
        props: projected.examples[0].props, definitionPath: [source.id],
      }) }] })
    const label = projected.examples[0].props.label
    const nativeNode = graph.createNode('TEXT', master.id, { name: 'Copy', text: label, width: 40, height: 20 })
    bindComponentProperties(graph, master, projected.examples[0], [{ region: { kind: 'text', property: 'label', text: label }, nativeNode }])
    return { snapshot: projected, master }
  })
  canonical.sha256 = variants[0].snapshot.sha256
  return { graph, canonical, source, owner, variants }
}

function nestedVariantFixture() {
  const data = variantFixture(), path = ['fixture/form', data.source.id]
  function wrap(snapshot, suffix = '') {
    const child = snapshot.examples[0]
    child.html = child.html.replace('data-component=', `data-fixture="${suffix}" data-component=`)
    const prefix = '<form aria-label="Memórias 🌱">', sibling = { id: 'sibling', html: '<span>Keep me</span>' }
    const start = Buffer.byteLength(prefix), end = start + Buffer.byteLength(child.html)
    snapshot.examples = [{ id: path[0], props: { label: 'Memórias' }, html: prefix + child.html + sibling.html + '</form>',
      children: [{ slot: 'children', span: { start, end }, description: child },
        { slot: 'children', span: { start: end, end: end + Buffer.byteLength(sibling.html) }, description: sibling }] }]
  }
  wrap(data.canonical)
  for (const [index, state] of data.variants.entries()) {
    wrap(state.snapshot, index ? 'a longer child projection' : '')
    const record = sourceMetadata(state.master)
    data.graph.updateNode(state.master.id, { pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source',
      value: JSON.stringify({ ...record, definitionPath: path }) }] })
  }
  return { ...data, path }
}

test('nested variant context retains byte-exact ancestors and siblings while child projection lengths change', () => {
  const data = nestedVariantFixture(), before = structuredClone(data.canonical)
  const definitions = bindings.bindComponentVariants(data.graph, data.owner, data.canonical, data.path, 'tone', data.variants)
  assert.equal(definitions.at(-1).type, 'VARIANT')
  assert.deepEqual(sourceMetadata(data.owner).definitionPath, data.path)
  assert.deepEqual(data.canonical, before)
})

test('nested families refuse changed ancestors, siblings, slots, byte spans and ambiguous paths before graph writes', () => {
  for (const mutate of [
    (root, data) => { data.path = [data.path[0], 'missing'] },
    root => { root.props.label = 'An unrelated parent edit' },
    root => { root.html = root.html.replace('Memórias', 'memórias') },
    root => { root.html += '<aside>Unrelated markup</aside>' },
    root => { root.children[1].description.id = 'different sibling' },
    root => { root.children[0].slot = 'different slot' },
    root => { delete root.children[0].slot },
    root => { root.children[0].span.start-- },
    root => { root.children[0].span.end++ },
    root => { root.children[1].span.start-- },
    root => { root.children.reverse() },
    root => {
      const [selected, sibling] = root.children, bytes = Buffer.from(root.html)
      root.html = bytes.subarray(0, selected.span.start).toString() + sibling.description.html + selected.description.html +
        bytes.subarray(sibling.span.end).toString()
      const start = selected.span.start, end = start + Buffer.byteLength(sibling.description.html)
      sibling.span = { start, end }
      selected.span = { start: end, end: end + Buffer.byteLength(selected.description.html) }
    },
    root => { root.children[1].description.id = root.children[0].description.id },
    root => { root.children[0].description.name = 'Unrelated leaf metadata' },
  ]) {
    const data = nestedVariantFixture()
    mutate(data.variants[1].snapshot.examples[0], data)
    const before = structuredClone([...data.graph.nodes]), inputs = structuredClone(data.variants.map(item => item.snapshot))
    assert.throws(() => bindings.bindComponentVariants(data.graph, data.owner, data.canonical, data.path, 'tone', data.variants), /Source component binding:/)
    assert.deepEqual([...data.graph.nodes], before)
    assert.deepEqual(data.variants.map(item => item.snapshot), inputs)
  }
})

test('source variant families expose one typed property without replacing shared text ownership', async () => {
  const { graph, canonical, source, owner, variants } = variantFixture()
  const definitions = bindings.bindComponentVariants(graph, owner, canonical, source.id, 'tone', variants)
  assert.deepEqual(definitions.map(item => item.type), ['TEXT', 'VARIANT'])
  assert.equal(graph.getChildren(owner.id).length, 3)
  assert.ok(variants.every(({ master }) => master.componentPropertyDefinitions.length === 0))
  const instance = graph.createInstance(variants[0].master.id, graph.getPages()[0].id, { name: 'Source placement' })
  associateSourceInstance(graph, instance, canonical, [source.id])
  const actions = createEditor({ graph })
  try {
    const label = definitions.find(item => item.type === 'TEXT'), tone = definitions.find(item => item.type === 'VARIANT')
    actions.setInstanceComponentProperty(instance.id, label.id, 'Independent label')
    actions.setInstanceComponentProperty(instance.id, tone.id, ' padded,a ')
    const expected = { baseSHA256: canonical.sha256, path: [source.id], props: { tone: ' padded,a ', label: 'Independent label' } }
    assert.deepEqual(extractSourceProps(graph, instance, canonical).proposal, expected)
    actions.renamePropertyDefinition(owner.id, tone.id, 'Renamed native control')
    assert.deepEqual(extractSourceProps(graph, instance, canonical).proposal, expected)
    let reopened = graph
    for (let cycle = 0; cycle < 2; cycle++) {
      reopened = await parseFigFile((await exportFigFile(reopened)).slice().buffer, { populate: 'all' })
      const placed = [...reopened.getAllNodes()].find(node => node.name === 'Source placement')
      assert.deepEqual(extractSourceProps(reopened, placed, canonical).proposal, expected)
    }
    actions.replaceGraph(reopened)
    const placed = [...reopened.getAllNodes()].find(node => node.name === 'Source placement')
    actions.setInstanceComponentProperty(placed.id, tone.id, '')
    assert.deepEqual(extractSourceProps(reopened, placed, canonical).proposal,
      { ...expected, props: { tone: '', label: 'Independent label' } }, 'empty is an exact source choice, not a missing assignment')
  } finally { actions.replaceGraph(new SceneGraph()) }
})

test('a source text field can become a finite variant without retaining a competing text binding', async () => {
  const input = variantFixture('label'), { graph, owner, canonical, source, variants } = input
  const definitions = bindings.bindComponentVariants(graph, owner, canonical, source.id, 'label', variants)
  assert.deepEqual(definitions.map(item => item.type), ['VARIANT'])
  assert.ok(variants.every(({ master }) => graph.getChildren(master.id)[0].componentPropertyReferences.length === 0))
  const instance = graph.createInstance(variants[0].master.id, owner.parentId, { name: 'Finite copy' })
  associateSourceInstance(graph, instance, canonical, [source.id])
  const expected = verifyComponentDocument(graph, canonical, [source.id]), actions = createEditor({ graph })
  try {
    actions.setInstanceComponentProperty(instance.id, definitions[0].id, '')
    let reopened = graph
    for (let cycle = 0; cycle < 2; cycle++) {
      reopened = await parseFigFile((await exportFigFile(reopened)).slice().buffer, { populate: 'all' })
      const placed = [...reopened.getAllNodes()].find(node => node.name === 'Finite copy')
      assert.equal(reopened.getChildren(placed.id)[0].text, '')
      assert.deepEqual(extractSourceProps(reopened, placed, canonical).proposal,
        { baseSHA256: canonical.sha256, path: [source.id], props: { label: '' } })
    }
    actions.setInstanceComponentProperty(instance.id, definitions[0].id, 'Save')
    graph.insertChildAt(variants[0].master.id, owner.parentId, 0)
    graph.deleteNode(owner.id)
    assert.equal(extractSourceProps(graph, instance, canonical).status, 'no-supported-changes')
    assert.throws(() => verifyComponentDocument(graph, canonical, [source.id], expected), /correspondence changed/,
      'losing the entire family must not look like a valid unchanged document')
  } finally { actions.replaceGraph(new SceneGraph()) }
})

test('source variant construction refuses inconsistent projections and bindings before native writes', () => {
  for (const mutate of [
    data => { data.variants.shift() },
    data => { data.variants[1].snapshot.css += ' body {color:red}' },
    data => { data.variants[1].snapshot.examples[0].props.label = 'Different field' },
    data => { data.variants[1].snapshot.examples[0].schema = { type: 'object', properties: {} } },
    data => { data.source.schema.type = 'array' },
    data => { data.variants[1].snapshot.examples[0].children = [{}] },
    data => { data.variants[1].master.componentPropertyDefinitions.push({ id: 'other', type: 'TEXT' }) },
    data => { data.graph.getChildren(data.variants[1].master.id)[0].text = 'Not the source' },
    data => { data.variants[1] = data.variants[0] },
    data => { data.canonical.sha256 = 'f'.repeat(64) },
    data => { data.graph.createInstance(data.variants[1].master.id, data.owner.parentId) },
  ]) {
    const data = variantFixture()
    mutate(data)
    const before = structuredClone([...data.graph.nodes]), sourceBefore = structuredClone(data.canonical)
    assert.throws(() => bindings.bindComponentVariants(data.graph, data.owner, data.canonical, data.source.id, 'tone', data.variants), /Source component binding:/)
    assert.deepEqual([...data.graph.nodes], before)
    assert.deepEqual(data.canonical, sourceBefore)
  }
})

test('private instances do not own or conceal a containing family text binding', () => {
  for (const forged of [false, true]) {
    const { graph, owner, canonical, source, variants } = variantFixture()
    const decoration = graph.createNode('COMPONENT', owner.parentId)
    graph.createNode('TEXT', decoration.id, { text: 'Private label' })
    for (const { master } of variants) {
      const instance = graph.createInstance(decoration.id, master.id)
      if (forged) graph.updateNode(graph.getChildren(instance.id)[0].id, {
        componentPropertyReferences: [{ propertyId: master.componentPropertyDefinitions[0].id, field: 'TEXT' }],
      })
    }
    const before = structuredClone([...graph.nodes])
    const bind = () => bindings.bindComponentVariants(graph, owner, canonical, source.id, 'tone', variants)
    if (forged) {
      assert.throws(bind, /one direct component boundary/)
      assert.deepEqual([...graph.nodes], before)
    } else assert.deepEqual(bind().map(item => item.type), ['TEXT', 'VARIANT'])
  }
})

test('source variant extraction rejects stale, missing and conflicting correspondence without proposals or mutations', () => {
  for (const mutate of [
    data => { changeMetadata(data.owner, origin => { origin.sha256 = 'f'.repeat(64) }) },
    data => { data.graph.deleteNode(data.variants[2].master.id) },
    data => { data.variants[1].master.variantPropSpecs[0].value = 'wrong' },
    data => { changeMetadata(data.variants[1].master, origin => { origin.props.label = 'Wrong source' }) },
    data => { data.owner.componentPropertyDefinitions.find(item => item.type === 'VARIANT').variantOptions.reverse() },
    data => { data.instance.componentPropertyAssignments[data.definitions.at(-1).id] = 'ignored assignment' },
    data => { changeMetadata(data.owner, origin => { origin.variantBindings[0].projections[1].sha256 = 'f'.repeat(64) }) },
  ]) {
    const data = variantFixture()
    data.definitions = bindings.bindComponentVariants(data.graph, data.owner, data.canonical, data.source.id, 'tone', data.variants)
    data.instance = data.graph.createInstance(data.variants[0].master.id, data.owner.parentId)
    associateSourceInstance(data.graph, data.instance, data.canonical, [data.source.id])
    mutate(data)
    const before = structuredClone([...data.graph.nodes]), result = extractSourceProps(data.graph, data.instance, data.canonical)
    assert.ok(['invalid', 'stale'].includes(result.status), JSON.stringify(result))
    assert.equal(result.proposal, undefined)
    assert.deepEqual([...data.graph.nodes], before)
  }
})

function mappedFixture(second = false) {
  const input = fixture(), canonical = structuredClone(snapshot), source = canonical.examples[0]
  if (second) {
    source.props.caption = 'Save'
    source.schema.properties.caption = { type: 'string' }
    input.targets.push({ region: { kind: 'text', property: 'caption', text: 'Save' }, nativeNode: input.lookalike })
  }
  input.graph.updateNode(input.master.id, { pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
    schema: canonical.schema, sha256: canonical.sha256, exampleId: source.id, componentId: source.componentId, props: source.props,
  }) }] })
  const definitions = bindComponentProperties(input.graph, input.master, source, input.targets)
  const instance = input.graph.createInstance(input.master.id, input.graph.getPages()[0].id, { name: 'Mapped instance' })
  const sibling = input.graph.createInstance(input.master.id, input.graph.getPages()[0].id, { name: 'Unmapped preview' })
  associateSourceInstance(input.graph, instance, canonical, [source.id])
  return { ...input, snapshot: canonical, instance, sibling, definitions }
}

function changeMetadata(node, change) {
  const entry = node.pluginData.find(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
  const value = JSON.parse(entry.value)
  change(value)
  entry.value = JSON.stringify(value)
}

function nestedFixture() {
  const canonical = structuredClone(snapshot), child = canonical.examples[0]
  child.id = 'button/local'
  const root = { ...structuredClone(child), id: 'form/root', componentId: 'fixture.form', props: {},
    schema: { type: 'object', properties: {} }, slots: [{ name: 'children', supported: true, trustedOnly: true,
      multiple: true, goType: '[]gomponents.Node' }], opaqueSlots: [],
    children: [{ description: child, slot: 'children', span: { start: 0, end: child.html.length } }] }
  canonical.examples = [root]
  const input = fixture(), { graph } = input
  const provenance = source => [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
    schema: canonical.schema, sha256: canonical.sha256, exampleId: source.id, componentId: source.componentId, props: source.props,
  }) }]
  graph.updateNode(input.master.id, { pluginData: provenance(child) })
  const definitions = bindComponentProperties(graph, input.master, child, input.targets)
  const owner = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Owner', pluginData: provenance(root) })
  const childTemplate = graph.createInstance(input.master.id, owner.id, { pluginData: [{
    pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({ localId: child.id, slot: 'children' }),
  }] })
  bindComponentProperties(graph, owner, root, [])
  const instance = graph.createInstance(owner.id, graph.getPages()[0].id, { name: 'Placed owner' })
  const sibling = graph.createInstance(owner.id, graph.getPages()[0].id, { name: 'Preview owner' })
  return { ...input, snapshot: canonical, root, owner, childTemplate, instance, sibling, definitions,
    nested: graph.getChildren(instance.id)[0] }
}

function replacementFixture(rootTarget = false, nestedSource = false) {
  const input = rootTarget ? mappedFixture() : nestedFixture()
  const { graph, snapshot: canonical } = input
  const originalPath = rootTarget ? [canonical.examples[0].id] : [input.root.id, 'button/local']
  if (!rootTarget) associateSourceInstance(graph, input.instance, canonical, [input.root.id])
  changeMetadata(input.master, value => { value.definitionPath = originalPath })
  const replacement = { ...structuredClone(example), id: 'button/local', props: { ...example.props, label: 'Publish' } }
  let replacementPath
  if (nestedSource) {
    const slots = [{ name: 'children', supported: true, trustedOnly: true, multiple: true, goType: '[]gomponents.Node' }]
    const container = { id: 'choices/root', slots, children: ['left', 'right'].map(id => ({ slot: 'children', description: {
      id, slots, children: [{ slot: 'children', description: id === 'right' ? replacement : { ...structuredClone(replacement), props: { ...replacement.props, label: 'Wrong branch' } } }],
    } })) }
    canonical.examples.push(container)
    replacementPath = ['choices/root', 'right', replacement.id]
  } else {
    replacement.id = 'button/alternative'
    canonical.examples.push(replacement)
    replacementPath = [replacement.id]
  }
  const master = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Unrelated display name', width: 100, height: 40,
    pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
      schema: canonical.schema, sha256: canonical.sha256, exampleId: replacement.id, componentId: replacement.componentId,
      props: replacement.props, definitionPath: replacementPath,
    }) }] })
  const text = graph.createNode('TEXT', master.id, { text: 'Publish', width: 70, height: 20 })
  const definitions = bindComponentProperties(graph, master, replacement, [{ nativeNode: text,
    region: { kind: 'text', property: 'label', text: 'Publish' } }])
  return { ...input, target: rootTarget ? input.instance : input.nested, replacementMaster: master,
    replacement, replacementPath, originalPath, replacementDefinitions: definitions }
}

for (const root of [false, true]) for (const nestedSource of [false, true]) {
  test(`source replacement reports exact destination and definition paths through two saves: root=${root}, nestedSource=${nestedSource}`, async () => {
    const input = replacementFixture(root, nestedSource)
    let { graph, target } = input
    assert.equal(extractSourceReplacement(graph, target, input.snapshot).status, 'no-supported-changes')
    graph.swapInstanceComponent(target.id, input.replacementMaster.id)
    for (let round = 0; round < 3; round++) {
      const before = structuredClone([...graph.getAllNodes()]), source = structuredClone(input.snapshot)
      const extracted = extractSourceReplacement(graph, target, input.snapshot)
      assert.deepEqual(extracted, { status: 'proposal', capability: 'source-component-replacement', proposal: {
        baseSHA256: input.snapshot.sha256, path: input.originalPath, replacementPath: input.replacementPath,
      } })
      assert.deepEqual([...graph.getAllNodes()], before)
      assert.deepEqual(input.snapshot, source)
      extracted.proposal.path.push('caller-owned')
      extracted.proposal.replacementPath.push('caller-owned')
      assert.deepEqual(extractSourceReplacement(graph, target, input.snapshot).proposal.replacementPath, input.replacementPath)
      if (round < 2) {
        graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        const placed = [...graph.getAllNodes()].find(node => node.pluginData.some(item =>
          item.key === 'platformkit.source' && JSON.stringify(JSON.parse(item.value).path) === JSON.stringify(input.originalPath.slice(0, 1))))
        target = root ? placed : graph.getChildren(placed.id)[0]
      }
    }
  })
}

test('source replacement refuses ambiguous, stale, missing and mixed correspondence without writes', async t => {
  const cases = [
    ['missing definition path', input => changeMetadata(input.replacementMaster, value => { delete value.definitionPath }), 'missing-definition-path'],
    ['invalid definition path', input => changeMetadata(input.replacementMaster, value => { value.definitionPath = 'button/alternative' }), 'invalid-path'],
    ['unknown definition', input => changeMetadata(input.replacementMaster, value => { value.definitionPath = ['unknown'] }), 'invalid-path'],
    ['wrong branch', input => changeMetadata(input.replacementMaster, value => { value.definitionPath[1] = 'left' }), 'invalid-binding'],
    ['stale master', input => changeMetadata(input.replacementMaster, value => { value.sha256 = '0'.repeat(64) }), 'stale-base'],
    ['stale placement', input => changeMetadata(input.instance, value => { value.sha256 = '0'.repeat(64) }), 'stale-base'],
    ['duplicate source root', input => { input.snapshot.examples.push(structuredClone(input.snapshot.examples.at(-1))) }, 'invalid-path'],
    ['duplicate nested identity', input => { input.snapshot.examples.at(-1).children.push(structuredClone(input.snapshot.examples.at(-1).children[1])) }, 'invalid-path'],
    ['copied placement', input => { input.graph.cloneTree(input.instance.id, input.instance.parentId) }, 'ambiguous-occurrence'],
    ['edited replacement string', input => { createEditor({ graph: input.graph }).setInstanceComponentProperty(input.target.id, input.replacementDefinitions[0].id, 'Combined edit') }, 'mixed-replacement-edits'],
    ['unsupported assignment', input => { input.target.componentPropertyAssignments.foreign = 'Unmapped edit' }, 'unsupported-scope'],
  ]
  for (const [name, change, code] of cases) await t.test(name, () => {
    const input = replacementFixture(false, true)
    input.graph.swapInstanceComponent(input.target.id, input.replacementMaster.id)
    change(input)
    const graphBefore = structuredClone([...input.graph.getAllNodes()]), sourceBefore = structuredClone(input.snapshot)
    const result = extractSourceReplacement(input.graph, input.target, input.snapshot)
    assert.equal(result.code, code, JSON.stringify(result))
    assert.ok(!Object.hasOwn(result, 'proposal'))
    assert.deepEqual([...input.graph.getAllNodes()], graphBefore)
    assert.deepEqual(input.snapshot, sourceBefore)
  })
})

test('source replacement rejects bound-string edits throughout a replacement composition, not unrelated siblings', async () => {
  const input = nestedFixture(), { graph, snapshot: canonical, root, owner, instance } = input
  associateSourceInstance(graph, instance, canonical, [root.id])
  const replacement = structuredClone(root)
  replacement.id = 'form/alternative'
  canonical.examples.push(replacement)
  const master = graph.cloneTree(owner.id, owner.parentId)
  changeMetadata(master, value => {
    value.exampleId = replacement.id
    value.definitionPath = [replacement.id]
  })
  graph.swapInstanceComponent(instance.id, master.id)
  const proposal = { baseSHA256: canonical.sha256, path: [root.id], replacementPath: [replacement.id] }
  assert.deepEqual(extractSourceReplacement(graph, instance, canonical).proposal, proposal)
  const editor = createEditor({ graph })
  editor.setInstanceComponentProperty(graph.getChildren(input.sibling.id)[0].id, input.definitions[0].id, 'Preview edit')
  assert.deepEqual(extractSourceReplacement(graph, instance, canonical).proposal, proposal)
  editor.setInstanceComponentProperty(graph.getChildren(instance.id)[0].id, input.definitions[0].id, 'Nested edit')
  let reopened = graph
  for (let round = 0; round < 3; round++) {
    const target = [...reopened.getAllNodes()].find(node => node.type === 'INSTANCE' &&
      node.pluginData.some(item => item.key === 'platformkit.source' && JSON.parse(item.value).path?.[0] === root.id))
    const before = structuredClone([...reopened.getAllNodes()])
    const result = extractSourceReplacement(reopened, target, canonical)
    assert.equal(result.code, 'mixed-replacement-edits', JSON.stringify(result))
    assert.ok(!Object.hasOwn(result, 'proposal'))
    assert.deepEqual([...reopened.getAllNodes()], before)
    if (round < 2) reopened = await parseFigFile((await exportFigFile(reopened)).slice().buffer, { populate: 'all' })
  }
})

test('native nested property definitions resolve the canonical master before the first save', () => {
  const input = nestedFixture(), editor = createEditor({ graph: input.graph })
  assert.deepEqual(editor.getInstanceComponentPropertyDefinitions(input.nested.id), input.definitions)
  editor.setInstanceComponentProperty(input.nested.id, input.definitions[0].id, 'Edited nested')
  assert.equal(input.graph.getChildren(input.nested.id)[1].text, 'Edited nested')
  assert.deepEqual(input.childTemplate.componentPropertyDefinitions, [], 'templates do not duplicate master definitions')
  assert.equal(input.graph.getChildren(input.master.id)[1].text, 'Save')
  assert.equal(input.graph.getChildren(input.graph.getChildren(input.sibling.id)[0].id)[1].text, 'Save')
  editor.undoAction()
  assert.equal(input.graph.getChildren(input.nested.id)[1].text, 'Save')
  editor.redoAction()
  assert.equal(input.graph.getChildren(input.nested.id)[1].text, 'Edited nested')
})

test('nested source correspondence derives from one explicit root across two FIG saves', async () => {
  const input = nestedFixture()
  let { graph, instance, nested } = input
  associateSourceInstance(graph, instance, input.snapshot, [input.root.id])
  for (const label of ['Nested edit', 'Second edit']) {
    createEditor({ graph }).setInstanceComponentProperty(nested.id, input.definitions[0].id, label)
    const before = structuredClone([...graph.getAllNodes()])
    const expected = { baseSHA256: input.snapshot.sha256, path: [input.root.id, 'button/local'], props: { label } }
    const extracted = extractSourceProps(graph, nested, input.snapshot)
    assert.deepEqual(extracted.proposal, expected, JSON.stringify(extracted))
    assert.deepEqual([...graph.getAllNodes()], before)
    const bytes = await exportFigFile(graph)
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    instance = [...graph.getAllNodes()].find(node => node.name === 'Placed owner')
    nested = graph.getChildren(instance.id)[0]
    const reopened = extractSourceProps(graph, nested, input.snapshot)
    assert.deepEqual(reopened.proposal, expected, JSON.stringify({ reopened, assignments: nested.componentPropertyAssignments,
      texts: graph.getChildren(nested.id).map(node => node.text) }))
    assert.deepEqual(sourceMetadata(nested), { localId: 'button/local', slot: 'children' })
    const claims = [...graph.getAllNodes()].filter(node => node.pluginData.some(item =>
      item.pluginId === 'platformkit' && item.key === 'platformkit.source' && JSON.parse(item.value).path))
    assert.deepEqual(claims.map(node => node.id), [instance.id])
  }
})

test('outer native text overrides retain precedence over nested property assignments through two FIG saves', async () => {
  const input = nestedFixture()
  let { graph, instance, nested } = input
  createEditor({ graph }).setInstanceComponentProperty(nested.id, input.definitions[0].id, 'Inner')
  const target = graph.getChildren(nested.id)[1]
  graph.updateNode(target.id, { text: 'Outer' })
  graph.updateNode(instance.id, { overrides: { [`${target.id}:text`]: 'Outer' } })
  for (let iteration = 0; iteration < 2; iteration++) {
    const before = structuredClone([...graph.getAllNodes()]), bytes = await exportFigFile(graph)
    assert.deepEqual([...graph.getAllNodes()], before)
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    instance = [...graph.getAllNodes()].find(node => node.name === 'Placed owner')
    nested = graph.getChildren(instance.id)[0]
    assert.equal(graph.getChildren(nested.id)[1].text, 'Outer')
    assert.equal(nested.componentPropertyAssignments[input.definitions[0].id], 'Inner')
  }
})

test('nested source association preflights every descendant before adding an absolute root claim', async t => {
  const cases = [
    ['opaque ancestor', input => { input.root.opaqueSlots = ['children'] }, 'unsupported-scope'],
    ['unobserved child', input => { delete input.root.children[0].span }, 'unsupported-scope'],
    ['invalid span', input => { input.root.children[0].span.start = -1 }, 'unsupported-scope'],
    ['unsupported slot', input => { input.root.slots[0].supported = false }, 'unsupported-scope'],
    ['slot type', input => { input.root.slots[0].goType = 'string' }, 'unsupported-scope'],
    ['untrusted slot', input => { input.root.slots[0].trustedOnly = false }, 'unsupported-scope'],
    ['duplicate source ID', input => { input.root.children.push(structuredClone(input.root.children[0])) }, 'invalid-source'],
    ['missing source records', input => { input.root.children = null }, 'invalid-source'],
    ['malformed source slots', input => { input.root.slots = {} }, 'invalid-source'],
    ['missing template identity', input => { input.childTemplate.pluginData = [] }, 'invalid-binding'],
    ['wrong local identity', input => changeMetadata(input.childTemplate, value => { value.localId = 'other' }), 'invalid-binding'],
    ['wrong slot case', input => changeMetadata(input.childTemplate, value => { value.slot = 'Children' }), 'invalid-binding'],
    ['absolute template claim', input => changeMetadata(input.childTemplate, value => { value.path = ['other'] }), 'invalid-provenance'],
    ['copied template', input => { input.graph.cloneTree(input.childTemplate.id, input.owner.id) }, 'invalid-binding'],
    ['copied native child', input => { input.graph.cloneTree(input.nested.id, input.instance.id) }, 'invalid-binding'],
    ['wrong placed local identity', input => changeMetadata(input.nested, value => { value.localId = 'other' }), 'invalid-provenance'],
    ['detached child', input => { input.graph.detachInstance(input.nested.id) }, 'invalid-binding'],
    ['missing master', input => { input.nested.componentId = 'missing' }, 'invalid-binding'],
    ['cyclic master', input => { input.nested.componentId = input.nested.id }, 'invalid-binding'],
    ['stale nested master', input => changeMetadata(input.master, value => { value.sha256 = '0'.repeat(64) }), 'stale-base'],
    ['edited template baseline', input => { input.graph.getChildren(input.childTemplate.id)[1].text = 'Other' }, 'inconsistent-native-value'],
    ['unsupported nested assignment', input => { input.nested.componentPropertyAssignments.foreign = 'Other' }, 'unsupported-scope'],
  ]
  for (const [name, change, code] of cases) await t.test(name, () => {
    const input = nestedFixture()
    change(input)
    const before = structuredClone([...input.graph.getAllNodes()]), source = structuredClone(input.snapshot)
    assert.throws(() => associateSourceInstance(input.graph, input.instance, input.snapshot, [input.root.id]),
      error => error.code === code, name)
    assert.deepEqual([...input.graph.getAllNodes()], before, 'no partial root or descendant correspondence')
    assert.deepEqual(input.snapshot, source)
  })
})

test('nested source selection is scoped by linked ownership, not names, order, or repeated branch-local IDs', async () => {
  const input = nestedFixture(), { graph, root, snapshot: canonical } = input
  // Distinct branches reuse the leaf master and the same child-local ID.
  const outerSource = { ...structuredClone(root), id: 'page/root', componentId: 'fixture.page', children: ['left', 'right'].map(id => ({
    description: { ...structuredClone(root), id }, slot: 'children', span: { start: 0, end: root.html.length },
  })) }
  canonical.examples = [outerSource]
  const outer = graph.createNode('COMPONENT', graph.getPages()[0].id, { pluginData: [{
    pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({ schema: canonical.schema,
      sha256: canonical.sha256, exampleId: outerSource.id, componentId: outerSource.componentId, props: outerSource.props }),
  }] })
  // Branch identity belongs to its occurrence, not the reused master origin.
  for (const occurrence of outerSource.children) {
    const branch = graph.cloneTree(input.owner.id, graph.getPages()[0].id)
    changeMetadata(branch, value => { value.exampleId = occurrence.description.id })
    graph.createInstance(branch.id, outer.id, { pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source',
      value: JSON.stringify({ localId: occurrence.description.id, slot: 'children' }) }] })
  }
  bindComponentProperties(graph, outer, outerSource, [])
  let placed = graph.createInstance(outer.id, graph.getPages()[0].id, { name: 'Two branch owner' })
  const preview = graph.createInstance(outer.id, graph.getPages()[0].id)
  associateSourceInstance(graph, placed, canonical, [outerSource.id])
  assert.equal(extractSourceProps(graph, graph.getChildren(preview.id)[0], canonical).code, 'missing-binding')
  const branch = graph.getChildren(placed.id)[1], selected = graph.getChildren(branch.id)[0]
  graph.reorderChild(branch.id, placed.id, 0)
  for (const node of graph.getAllNodes()) graph.updateNode(node.id, { name: 'Identical display name' })
  createEditor({ graph }).setInstanceComponentProperty(selected.id, input.definitions[0].id, 'Only right')
  const expected = { baseSHA256: canonical.sha256, path: ['page/root', 'right', 'button/local'], props: { label: 'Only right' } }
  assert.deepEqual(extractSourceProps(graph, selected, canonical).proposal, expected)
  for (let iteration = 0, current = graph; iteration < 2; iteration++) {
    const bytes = await exportFigFile(current)
    current = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    placed = [...current.getAllNodes()].find(node => node.pluginData.some(item =>
      item.pluginId === 'platformkit' && item.key === 'platformkit.source' && JSON.parse(item.value).path))
    const right = current.getChildren(placed.id).find(node => sourceMetadata(node).localId === 'right')
    const selected = current.getChildren(right.id)[0], actual = extractSourceProps(current, selected, canonical)
    assert.deepEqual(actual.proposal, expected, JSON.stringify(actual))
    const left = current.getChildren(placed.id).find(node => sourceMetadata(node).localId === 'left')
    assert.equal(extractSourceProps(current, current.getChildren(left.id)[0], canonical).status, 'no-supported-changes')
  }
})

test('source string defaults are explicit empty values and native controls bind their actual value', () => {
  const source = structuredClone(example)
  source.schema.properties.value = { type: 'string', default: '' }
  assert.equal(Object.hasOwn(source.props, 'value'), false)
  assert.equal(isSourceTextProperty(source, 'value'), true)
  assert.equal(sourceTextValue(source, 'value'), '')
  const input = fixture(''), { graph, master } = input
  input.targets[0].region = { kind: 'control', property: 'value', value: '', type: 'text' }
  const [definition] = bindComponentProperties(graph, master, source, input.targets)
  assert.equal(definition.defaultValue, '')
  assert.equal(definition.name, 'value')
  assert.equal(Object.hasOwn(source.props, 'value'), false, 'default observation never rewrites source props')
  for (const change of [
    source => { delete source.schema.properties.value.default },
    source => { source.schema.properties.value.default = 'guessed' },
    source => { source.schema.required.push('value') },
    source => { source.schema.required = 1 },
    source => { source.schema.properties.value.anyOf = [{ type: 'null' }] },
    source => { source.props.value = null },
    source => { source.props.value = 1 },
  ]) {
    const altered = structuredClone(source)
    change(altered)
    assert.equal(isSourceTextProperty(altered, 'value'), false)
    assert.equal(sourceTextValue(altered, 'value'), undefined)
  }
  for (const [type, value] of [['password', ''], ['file', ''], ['text', 'placeholder is not value'], ['text', '\n']]) {
    const rejected = fixture(''), before = structuredClone([...rejected.graph.getAllNodes()])
    rejected.targets[0].region = { kind: 'control', property: 'value', value, type }
    assert.throws(() => bindComponentProperties(rejected.graph, rejected.master, source, rejected.targets))
    assert.deepEqual([...rejected.graph.getAllNodes()], before)
  }
  const placeholder = fixture('')
  placeholder.targets[0].region = { kind: 'control', property: 'value', value: '', type: 'text', placeholder: 'Hint' }
  assert.throws(() => bindComponentProperties(placeholder.graph, placeholder.master, source, placeholder.targets), /literal text/)
})

test('source-bound icon slots retain root text proposals without claiming nested asset edits', async () => {
  const canonical = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', 'pk-ui.component.button/with-icon'],
    { cwd: new URL('../../../', import.meta.url), encoding: 'utf8' })), source = canonical.examples[0]
  const input = fixture(source.props.label), { master, targets } = input
  let { graph } = input
  const glyph = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Asset master' })
  graph.createNode('VECTOR', glyph.id)
  targets.push({ region: { kind: 'slot', name: 'IconEnd', children: [{ kind: 'element', tag: 'svg' }] },
    nativeNode: graph.createInstance(glyph.id, master.id) })
  graph.updateNode(master.id, { pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
    schema: canonical.schema, sha256: canonical.sha256, exampleId: source.id, componentId: source.componentId, props: source.props,
  }) }] })
  const [text, slot] = bindComponentProperties(graph, master, source, targets)
  assert.deepEqual(sourceMetadata(master).slotBindings, [{ id: slot.id, slot: 'IconEnd' }])
  let instance = graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Icon slot owner' })
  associateSourceInstance(graph, instance, canonical, [source.id])
  createEditor({ graph }).setInstanceComponentProperty(instance.id, text.id, 'Updated text')
  const expected = { baseSHA256: canonical.sha256, path: [source.id], props: { label: 'Updated text' } }
  for (let iteration = 0; iteration < 2; iteration++) {
    const bytes = await exportFigFile(graph)
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    instance = [...graph.getAllNodes()].find(node => node.name === 'Icon slot owner')
    const result = extractSourceProps(graph, instance, canonical)
    assert.deepEqual(result.proposal, expected, JSON.stringify(result))
    const asset = graph.getChildren(instance.id).find(node => node.type === 'INSTANCE')
    assert.equal(extractSourceProps(graph, asset, canonical).code, 'missing-binding')
  }
  const owner = graph.getNode(instance.componentId), metadataEntry = owner.pluginData.find(item => item.key === 'platformkit.source')
  const original = metadataEntry.value
  for (const change of [
    value => { delete value.slotBindings },
    value => { value.slotBindings = null },
    value => { value.slotBindings.push({ ...value.slotBindings[0] }) },
    value => { value.slotBindings[0].slot = 'IconStart' },
    value => { value.slotBindings[0].id = text.id },
  ]) {
    changeMetadata(owner, change)
    const before = structuredClone([...graph.getAllNodes()])
    assert.equal(extractSourceProps(graph, instance, canonical).status, 'invalid')
    assert.deepEqual([...graph.getAllNodes()], before)
    metadataEntry.value = original
  }
  instance.componentPropertyAssignments[slot.id] = graph.getChildren(instance.id).find(node => node.type === 'INSTANCE').componentId
  assert.equal(extractSourceProps(graph, instance, canonical).code, 'unsupported-scope', 'even a same-asset assignment is not a string proposal')
})

test('source changes use persisted correspondence through duplicate display names and two FIG saves', async () => {
  const input = mappedFixture(true), beforeSource = structuredClone(input.snapshot)
  let { graph, master, instance } = input
  const path = [example.id]
  for (const value of ['Create album', '']) {
    graph.updateNode(master.id, { componentPropertyDefinitions: input.definitions.map(item => ({ ...item, name: 'Same display name' })) })
    for (const node of graph.getChildren(instance.id)) graph.updateNode(node.id, { name: 'Same layer name' })
    graph.reorderChild(graph.getChildren(instance.id)[0].id, instance.id, 1)
    const actions = createEditor({ graph })
    actions.setInstanceComponentProperty(instance.id, input.definitions[1].id, value)
    const before = structuredClone([...graph.getAllNodes()])
    const expected = { baseSHA256: input.snapshot.sha256, path, props: { caption: value } }
    assert.deepEqual(extractSourceProps(graph, instance, input.snapshot).proposal, expected)
    assert.deepEqual([...graph.getAllNodes()], before, 'extraction does not update native state')
    const bytes = await exportFigFile(graph)
    const raw = parseFigBuffer(bytes.slice().buffer).nodeChanges
    const records = raw.flatMap(node => (node.pluginData ?? []).filter(item =>
      item.pluginID === 'platformkit' && item.key === 'platformkit.source').map(item => JSON.parse(item.value)))
    assert.equal(records.filter(record => record.path?.[0] === example.id).length, 1, 'one persisted source occurrence')
    assert.deepEqual(records.find(record => record.textBindings)?.textBindings,
      input.definitions.map(item => ({ id: item.id, property: item.name })), 'raw FIG retains the original source field names')
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    instance = [...graph.getAllNodes()].find(node => node.type === 'INSTANCE' && node.pluginData.some(item =>
      item.pluginId === 'platformkit' && item.key === 'platformkit.source'))
    master = graph.getNode(instance.componentId)
    const reopened = structuredClone([...graph.getAllNodes()])
    assert.deepEqual(extractSourceProps(graph, instance, input.snapshot).proposal, expected)
    assert.deepEqual([...graph.getAllNodes()], reopened)
    assert.equal(graph.getChildren(instance.id).find(node => node.componentPropertyReferences[0]?.propertyId === input.definitions[0].id).text, 'Save')
  }
  assert.deepEqual(input.snapshot, beforeSource)
})

test('source extraction reports only supported property changes and association is explicit', () => {
  const input = mappedFixture(), { graph, instance, sibling } = input
  const before = structuredClone([...graph.getAllNodes()])
  assert.equal(associateSourceInstance(graph, instance, input.snapshot, [example.id]), instance)
  assert.deepEqual([...graph.getAllNodes()], before, 'repeating the same association is a no-op')
  graph.updateNode(instance.id, { x: 987, name: 'Unrelated native scene edit' })
  assert.deepEqual(extractSourceProps(graph, instance, input.snapshot), {
    status: 'no-supported-changes', capability: 'root-string-props', properties: ['label'],
  })
  assert.equal(extractSourceProps(graph, sibling, input.snapshot).code, 'missing-binding')
  const rejected = structuredClone([...graph.getAllNodes()])
  assert.throws(() => associateSourceInstance(graph, sibling, input.snapshot, [example.id]), /Multiple native instances/)
  assert.deepEqual([...graph.getAllNodes()], rejected)
})

test('source extraction refuses invalid correspondence without changing source or graph', async t => {
  const cases = [
    ['missing association', input => { input.instance.pluginData = [] }, 'missing-binding'],
    ['unrelated metadata', input => { input.instance.pluginData[0].pluginId = 'another-plugin' }, 'missing-binding'],
    ['duplicate provenance', input => { input.instance.pluginData.push({ ...input.instance.pluginData[0] }) }, 'invalid-provenance'],
    ['invalid JSON', input => { input.instance.pluginData[0].value = '{' }, 'invalid-provenance'],
    ['nonobject JSON', input => { input.instance.pluginData[0].value = '[]' }, 'invalid-provenance'],
    ['invalid master JSON', input => { input.master.pluginData[0].value = '{' }, 'invalid-provenance'],
    ['missing path', input => changeMetadata(input.instance, value => { delete value.path }), 'invalid-path'],
    ['string path', input => changeMetadata(input.instance, value => { value.path = example.id }), 'invalid-path'],
    ['unknown path', input => changeMetadata(input.instance, value => { value.path = ['missing'] }), 'invalid-path'],
    ['nested path', input => changeMetadata(input.instance, value => { value.path.push('nested') }), 'unsupported-scope'],
    ['wrong occurrence interface', input => changeMetadata(input.instance, value => { value.componentId = 'another' }), 'invalid-provenance'],
    ['wrong master interface', input => changeMetadata(input.master, value => { value.componentId = 'another' }), 'invalid-binding'],
    ['stale occurrence', input => changeMetadata(input.instance, value => { value.sha256 = '0'.repeat(64) }), 'stale-base'],
    ['stale master', input => changeMetadata(input.master, value => { value.sha256 = '0'.repeat(64) }), 'stale-base'],
    ['wrong baseline props', input => changeMetadata(input.master, value => { value.props.label = 'Changed' }), 'invalid-binding'],
    ['missing map', input => changeMetadata(input.master, value => { delete value.textBindings }), 'invalid-provenance'],
    ['removed map with retained native definition', input => changeMetadata(input.master, value => { value.textBindings = [] }), 'invalid-binding'],
    ['unknown map version', input => changeMetadata(input.master, value => { value.bindingVersion = 2 }), 'invalid-provenance'],
    ['duplicate map', input => changeMetadata(input.master, value => { value.textBindings.push(value.textBindings[0]) }), 'invalid-binding'],
    ['wrong case', input => changeMetadata(input.master, value => { value.textBindings[0].property = 'Label' }), 'invalid-binding'],
    ['constrained string', input => { input.snapshot.examples[0].schema.properties.label.enum = ['Save'] }, 'unsupported-scope'],
    ['unsupported source', input => { input.snapshot.examples[0].propsEditable = false }, 'unsupported-scope'],
    ['missing definition', input => { input.master.componentPropertyDefinitions = [] }, 'invalid-binding'],
    ['unmapped native definition', input => {
      input.master.componentPropertyDefinitions.push({ ...input.definitions[0], id: generateId(), name: 'Unmapped' })
    }, 'invalid-binding'],
    ['duplicated native definition', input => {
      input.master.componentPropertyDefinitions.push({ ...input.definitions[0] })
    }, 'invalid-binding'],
    ['malformed definitions', input => { input.master.componentPropertyDefinitions = null }, 'invalid-binding'],
    ['malformed references', input => { input.graph.getChildren(input.instance.id)[1].componentPropertyReferences = null }, 'invalid-binding'],
    ['malformed variables', input => { input.graph.getChildren(input.instance.id)[1].boundVariables = null }, 'invalid-binding'],
    ['native text binding', input => { input.graph.getChildren(input.instance.id)[1].boundVariables.text = 'variable' }, 'unsupported-scope'],
    ['wrong value type', input => { input.instance.componentPropertyAssignments[input.definitions[0].id] = false }, 'inconsistent-native-value'],
    ['direct text override', input => { input.graph.getChildren(input.instance.id)[1].text = 'Unassigned edit' }, 'inconsistent-native-value'],
    ['foreign assignment', input => { input.instance.componentPropertyAssignments.foreign = 'Other' }, 'unsupported-scope'],
    ['broken lineage', input => { input.graph.getChildren(input.instance.id)[1].componentId = input.lookalike.id }, 'invalid-binding'],
    ['copied correspondence', input => { input.sibling.pluginData = structuredClone(input.instance.pluginData) }, 'ambiguous-occurrence'],
    ['detached copy correspondence', input => {
      input.sibling.type = 'FRAME'
      input.sibling.pluginData = structuredClone(input.instance.pluginData)
    }, 'ambiguous-occurrence'],
    ['duplicate target reference', input => {
      input.graph.getChildren(input.instance.id)[0].componentPropertyReferences = [{ propertyId: input.definitions[0].id, field: 'TEXT' }]
    }, 'invalid-binding'],
  ]
  for (const [name, change, code] of cases) await t.test(name, () => {
    const input = mappedFixture()
    change(input)
    const beforeGraph = structuredClone([...input.graph.getAllNodes()]), beforeSource = structuredClone(input.snapshot)
    const result = extractSourceProps(input.graph, input.instance, input.snapshot)
    assert.equal(result.code, code)
    assert.equal(result.status, code === 'stale-base' ? 'stale' : ['missing-binding', 'unsupported-scope'].includes(code) ? 'unsupported' : 'invalid')
    assert.equal(result.proposal, undefined)
    assert.deepEqual([...input.graph.getAllNodes()], beforeGraph)
    assert.deepEqual(input.snapshot, beforeSource)
  })
})

test('source property provenance survives native renaming and two FIG saves', async () => {
  let { graph, master, targets } = fixture()
  graph.updateNode(master.id, { pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
    schema: snapshot.schema, sha256: snapshot.sha256, exampleId: example.id, componentId: example.componentId, props: example.props,
  }) }] })
  const [definition] = bindComponentProperties(graph, master, example, targets)
  for (const name of ['Renamed display label', 'Another editor label']) {
    graph.updateNode(master.id, { componentPropertyDefinitions: [{ ...definition, name }] })
    const bytes = await exportFigFile(graph)
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    master = [...graph.getAllNodes()].find(node => node.type === 'COMPONENT')
    const metadata = sourceMetadata(master)
    assert.equal(metadata.bindingVersion, 1)
    assert.deepEqual(metadata.textBindings, [{ id: definition.id, property: 'label' }])
    assert.equal(master.componentPropertyDefinitions[0].name, name)
  }
})

test('source property binding uses the exact constructed text handle through edits and two FIG saves', async () => {
  let { graph, master, lookalike, targets } = fixture()
  const beforeExample = structuredClone(example)
  const beforeRegion = structuredClone(targets[0].region)
  const definitions = bindComponentProperties(graph, master, example, targets)
  assert.equal(definitions.length, 1)
  const definition = definitions[0]
  assert.equal(definition.name, 'label')
  assert.equal(definition.type, 'TEXT')
  assert.equal(definition.defaultValue, example.props.label)
  assert.deepEqual(targets[0].nativeNode.componentPropertyReferences, [{ propertyId: definition.id, field: 'TEXT' }])
  assert.deepEqual(lookalike.componentPropertyReferences, [])
  let edited = graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Edited fixture' })
  let sibling = graph.createInstance(master.id, graph.getPages()[0].id, { name: 'Untouched fixture' })
  // Fixed geometry deliberately scopes this test to typed identity, not sizing.
  for (const value of ['Create album', '']) {
    const actions = createEditor({ graph })
    const before = structuredClone(graph.getChildren(edited.id))
    const targetID = before.find(node => node.componentPropertyReferences.some(ref => ref.propertyId === definition.id)).id
    actions.setInstanceComponentProperty(edited.id, definition.id, value)
    assert.equal(graph.getNode(targetID).text, value)
    actions.undoAction()
    assert.deepEqual(graph.getChildren(edited.id), before)
    actions.redoAction()
    assert.equal(graph.getNode(targetID).text, value, 'edit and history retain the exact native target')
    const bytes = await exportFigFile(graph)
    graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    // Local node IDs are remapped on import. Fixture names only locate placed
    // test objects; binding above never consults names, text or child order.
    const reopened = name => {
      const found = [...graph.getAllNodes()].filter(node => node.name === name)
      assert.equal(found.length, 1)
      return found[0]
    }
    master = reopened('Fixture master')
    edited = reopened('Edited fixture')
    sibling = reopened('Untouched fixture')
    const texts = nodeId => graph.getChildren(nodeId).filter(node => node.type === 'TEXT')
    const bound = nodeID => texts(nodeID).filter(node => node.componentPropertyReferences.some(ref => ref.propertyId === definition.id))
    assert.deepEqual([master, edited, sibling].map(node => bound(node.id).length), [1, 1, 1])
    const [target] = bound(edited.id)
    const [sourceTarget] = bound(master.id)
    const [siblingTarget] = bound(sibling.id)
    assert.equal(new Set([target.id, sourceTarget.id, siblingTarget.id]).size, 3)
    assert.equal(target.componentId, sourceTarget.id)
    assert.equal(siblingTarget.componentId, sourceTarget.id)
    assert.equal(target.text, value)
    assert.equal(texts(edited.id).find(node => node.id !== target.id).text, 'Save', 'lookalike stays unchanged')
    assert.deepEqual(texts(sibling.id).map(node => node.text), ['Save', 'Save'])
    assert.deepEqual(texts(master.id).map(node => node.text), ['Save', 'Save'])
    assert.equal(edited.componentId, master.id)
    assert.equal(sibling.componentId, master.id)
    assert.equal(edited.componentPropertyAssignments[definition.id], value)
  }
  assert.deepEqual(example, beforeExample)
  assert.deepEqual(targets[0].region, beforeRegion)
})

test('one binding pass preflights typed text and exact named slots before any definitions', () => {
  for (const invalid of [null, 'case', 'unsupported', 'duplicate', 'type', 'nested', 'copy',
    'multiple', 'untrusted', 'empty', 'not-svg', 'name', 'instance-link']) {
    const { graph, master, targets } = fixture(), source = structuredClone(example)
    const glyph = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Not a slot name', width: 24, height: 24 })
    const nativeNode = graph.createInstance(glyph.id, master.id)
    const region = { kind: 'slot', name: 'IconEnd', children: [{ kind: 'element', tag: 'svg' }] }
    const target = { region, nativeNode }
    targets.push(target)
    const declaration = source.slots.find(slot => slot.name === region.name)
    if (invalid === 'case') region.name = 'iconEnd'
    if (invalid === 'unsupported') declaration.supported = false
    if (invalid === 'duplicate') source.slots.push({ ...declaration })
    if (invalid === 'type') declaration.goType = 'string'
    if (invalid === 'multiple') declaration.multiple = false
    if (invalid === 'untrusted') declaration.trustedOnly = false
    if (invalid === 'empty') region.children = []
    if (invalid === 'not-svg') region.children[0].tag = 'span'
    if (invalid === 'name') target.region = Object.assign(Object.create({ name: 'IconEnd' }), { kind: 'slot', children: region.children })
    if (invalid === 'instance-link') graph.updateNode(nativeNode.id, { componentId: graph.createInstance(glyph.id, master.id).id })
    if (invalid === 'copy') target.nativeNode = { ...nativeNode }
    if (invalid === 'nested') {
      const other = graph.createInstance(glyph.id, master.id)
      graph.reparentNode(nativeNode.id, other.id)
    }
    const before = structuredClone([...graph.getAllNodes()])
    if (invalid) {
      assert.throws(() => bindComponentProperties(graph, master, source, targets))
      assert.deepEqual([...graph.getAllNodes()], before, invalid)
      continue
    }
    const definitions = bindComponentProperties(graph, master, source, targets)
    assert.deepEqual(definitions.map(({ name, type, defaultValue }) => ({ name, type, defaultValue })), [
      { name: 'label', type: 'TEXT', defaultValue: 'Save' },
      { name: 'IconEnd', type: 'INSTANCE_SWAP', defaultValue: glyph.id },
    ])
    assert.deepEqual(nativeNode.componentPropertyReferences, [{ propertyId: definitions[1].id, field: 'INSTANCE_SWAP' }])
  }
})

test('binding retains exact empty, whitespace and escaped labels from fresh Go projections', () => {
  for (const value of ['', ' ', '\u00a0', 'Save & <tag>"<!--/pk-text:label-->']) {
    const projected = JSON.parse(execFileSync('go', [
      'run', './tools/designexport', '--example', example.id, '--props',
    ], { cwd: new URL('../../../', import.meta.url), encoding: 'utf8', input: JSON.stringify({ label: value }) })).examples[0]
    assert.equal(projected.props.label, value)
    const before = structuredClone(projected)
    const input = fixture(value)
    const definitions = bindComponentProperties(input.graph, input.master, projected, input.targets)
    assert.equal(definitions[0].defaultValue, value)
    assert.equal(input.targets[0].nativeNode.text, value)
    assert.equal(input.targets[0].region.text, value)
    assert.deepEqual(projected, before)
  }
})

test('binding preflights all identities, types, values and ownership without mutating rejected input', () => {
  const cases = [
    ...['null', '{', '{"bindingVersion":1}'].map(value => input => {
      input.master.pluginData = [{ pluginId: 'platformkit', key: 'platformkit.source', value }]
    }),
    input => { input.master.pluginData = [1, 2].map(() => ({ pluginId: 'platformkit', key: 'platformkit.source', value: '{}' })) },
    input => { input.example.propsEditable = false },
    input => { input.example.schema.properties.label.type = 'boolean' },
    input => { input.example.schema.properties.label = { $ref: '#/$defs/text' } },
    input => { input.example.schema.properties.label.minLength = 1 },
    ...Object.entries({ maxLength: 4, pattern: '^Save$', enum: ['Save'], const: 'Save', readOnly: true })
      .map(([key, value]) => input => { input.example.schema.properties.label[key] = value }),
    input => { delete input.example.props.label },
    input => { input.example.props.label = 'Different source value' },
    input => { input.targets[0].region.kind = 'element' },
    input => { delete input.targets[0].region.property },
    input => { input.targets[0].region.property = 'unknown' },
    input => { input.targets[0].region.text = 'Different observed value' },
    input => { input.targets[0].nativeNode.text = 'Different native value' },
    input => { input.targets[0].nativeNode = { ...input.targets[0].nativeNode } },
    input => { input.master = { ...input.master } },
    input => { input.targets.push({ ...input.targets[0], nativeNode: input.lookalike }) },
    input => { input.targets.push(input.targets[0]) },
    input => { input.targets[0].nativeNode.componentPropertyReferences = [{ propertyId: '99:1', field: 'TEXT' }] },
    input => { input.master.componentPropertyDefinitions = [{ id: '99:1', name: 'other', type: 'TEXT', defaultValue: '' }] },
    input => { input.targets[0].nativeNode = input.graph.createNode('RECTANGLE', input.master.id) },
    input => { input.targets[0].nativeNode.parentId = input.graph.getPages()[0].id },
    input => { input.targets[0].nativeNode.parentId = 'missing-parent' },
    input => { input.master.childIds = input.master.childIds.filter(id => id !== input.targets[0].nativeNode.id) },
    input => {
      const node = input.targets[0].nativeNode
      node.parentId = node.id
      node.childIds.push(node.id)
    },
    input => { input.graph.createInstance(input.master.id, input.graph.getPages()[0].id) },
    input => {
      const nested = input.graph.createNode('COMPONENT', input.master.id)
      input.graph.reparentNode(input.targets[0].nativeNode.id, nested.id)
    },
    input => {
      const other = input.graph.createNode('COMPONENT', input.graph.getPages()[0].id)
      const nested = input.graph.createInstance(other.id, input.master.id)
      input.graph.reparentNode(input.targets[0].nativeNode.id, nested.id)
    },
    input => {
      const second = input.graph.createNode('TEXT', input.master.id, { text: 'Second' })
      input.targets.push({ region: { kind: 'text', property: 'missing', text: 'Second' }, nativeNode: second })
    },
  ]
  for (const change of cases) {
    const input = { ...fixture(), example: structuredClone(example) }
    change(input)
    const before = structuredClone([...input.graph.getAllNodes()])
    const beforeExample = structuredClone(input.example)
    const beforeRegions = structuredClone(input.targets.map(target => target.region))
    assert.throws(() => bindComponentProperties(input.graph, input.master, input.example, input.targets))
    assert.deepEqual([...input.graph.getAllNodes()], before, 'no partial definitions, references or value changes')
    assert.deepEqual(input.example, beforeExample)
    assert.deepEqual(input.targets.map(target => target.region), beforeRegions)
  }
})

test('binding requires own source fields and object string schemas', async t => {
  const cases = [
    ['inherited schema type', input => { input.example.schema.properties.label = Object.create({ type: 'string' }) }],
    ['array schema', input => { input.example.schema.properties.label = Object.assign([], { type: 'string' }) }],
    ['inherited schema property', input => { input.example.schema.properties = Object.create({ label: { type: 'string' } }) }],
    ['inherited source value', input => { input.example.props = Object.create({ label: 'Save' }) }],
  ]
  for (const [name, change] of cases) await t.test(name, () => {
    const input = { ...fixture(), example: structuredClone(example) }
    change(input)
    const before = structuredClone([...input.graph.getAllNodes()])
    const props = input.example.props
    const properties = input.example.schema.properties
    const descriptor = properties.label
    assert.throws(() => bindComponentProperties(input.graph, input.master, input.example, input.targets), /source.*property/)
    assert.deepEqual([...input.graph.getAllNodes()], before)
    assert.equal(input.example.props, props)
    assert.equal(input.example.schema.properties, properties)
    assert.equal(properties.label, descriptor)
  })
})

test('binding supports distinct source string fields without aliasing returned definitions', () => {
  const input = fixture()
  const custom = structuredClone(example)
  custom.props.caption = 'Save'
  custom.schema.properties.caption = { type: 'string', description: 'Independent text with the same visible value' }
  input.targets.push({ region: { kind: 'text', property: 'caption', text: 'Save' }, nativeNode: input.lookalike })
  const definitions = bindComponentProperties(input.graph, input.master, custom, input.targets)
  assert.deepEqual(definitions.map(item => item.name), ['label', 'caption'])
  assert.notEqual(definitions[0].id, definitions[1].id)
  definitions[0].name = 'Caller mutation'
  assert.equal(input.master.componentPropertyDefinitions[0].name, 'label')
  assert.notEqual(input.targets[0].nativeNode.componentPropertyReferences[0].propertyId,
    input.lookalike.componentPropertyReferences[0].propertyId)
})

test('binding avoids imported property IDs before native edits and FIG serialization', async () => {
  let graph = new SceneGraph()
  const other = graph.createNode('COMPONENT', graph.getPages()[0].id, { name: 'Existing definitions' })
  // Imported definition IDs are independent of the process-local allocator.
  // Reserve a short range so import and fresh-node creation stay inside it.
  const [session, local] = generateId().split(':')
  const reserved = Array.from({ length: 32 }, (_, index) => `${session}:${Number(local) + index + 1}`)
  graph.updateNode(other.id, {
    componentPropertyDefinitions: reserved.map((id, index) => ({
      id, name: `Existing boolean ${index}`, type: 'BOOLEAN', defaultValue: 'true',
    })),
  })
  const importedBytes = await exportFigFile(graph)
  graph = await parseFigFile(importedBytes.slice().buffer, { populate: 'all' })
  const existing = [...graph.getAllNodes()].find(node => node.name === 'Existing definitions')
  assert.deepEqual(existing.componentPropertyDefinitions.map(definition => definition.id), reserved)
  const beforeDefinitions = structuredClone(existing.componentPropertyDefinitions)
  const page = graph.getPages()[0]
  const master = graph.createNode('COMPONENT', page.id, { name: 'Fresh master', width: 100, height: 40 })
  const nativeNode = graph.createNode('TEXT', master.id, { text: 'Save', width: 40, height: 20 })
  const [nextSession, nextLocal] = generateId().split(':')
  assert.ok(reserved.includes(`${nextSession}:${Number(nextLocal) + 1}`), 'fixture would collide with the next allocation')
  const [definition] = bindComponentProperties(graph, master, example, [{
    region: { kind: 'text', property: 'label', text: 'Save' }, nativeNode,
  }])
  const instance = graph.createInstance(master.id, page.id, { name: 'Edited fresh instance' })
  createEditor({ graph }).setInstanceComponentProperty(instance.id, definition.id, 'Changed')
  assert.equal(instance.componentPropertyAssignments[definition.id], 'Changed')
  const bytes = await exportFigFile(graph)
  const reopened = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
  const edited = [...reopened.getAllNodes()].find(node => node.name === 'Edited fresh instance')
  assert.equal(edited.componentPropertyAssignments[definition.id], 'Changed', 'a prior BOOLEAN must not coerce this TEXT assignment')
  assert.equal(reopened.getChildren(edited.id)[0].text, 'Changed')
  assert.ok(!reserved.includes(definition.id))
  assert.deepEqual(existing.componentPropertyDefinitions, beforeDefinitions)
  const retained = [...reopened.getAllNodes()].find(node => node.name === 'Existing definitions')
  assert.deepEqual(retained.componentPropertyDefinitions, beforeDefinitions)
})
