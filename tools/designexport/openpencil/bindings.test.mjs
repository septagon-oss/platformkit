import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { test } from 'node:test'
import { SceneGraph, generateId } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { parseFigBuffer } from '@open-pencil/fig'
import { bindComponentProperties } from './bindings.mjs'
import { associateSourceInstance, extractSourceProps } from './source-changes.mjs'

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
    ['unknown map version', input => changeMetadata(input.master, value => { value.bindingVersion = 2 }), 'invalid-provenance'],
    ['duplicate map', input => changeMetadata(input.master, value => { value.textBindings.push(value.textBindings[0]) }), 'invalid-binding'],
    ['wrong case', input => changeMetadata(input.master, value => { value.textBindings[0].property = 'Label' }), 'invalid-binding'],
    ['constrained string', input => { input.snapshot.examples[0].schema.properties.label.enum = ['Save'] }, 'unsupported-scope'],
    ['unsupported source', input => { input.snapshot.examples[0].propsEditable = false }, 'unsupported-scope'],
    ['missing definition', input => { input.master.componentPropertyDefinitions = [] }, 'invalid-binding'],
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
