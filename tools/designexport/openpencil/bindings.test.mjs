import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { test } from 'node:test'
import { SceneGraph, generateId } from '@open-pencil/scene-graph'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { bindComponentProperties } from './bindings.mjs'

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
