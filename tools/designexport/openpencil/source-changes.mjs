import { isDeepStrictEqual } from 'node:util'
import { isSourceTextProperty, sourceTextValue } from './bindings.mjs'
import { chain, sourceChildren } from './exporter-correction.mjs'

const sourceEntry = item => item?.pluginId === 'platformkit' && item.key === 'platformkit.source'
const object = value => value !== null && typeof value === 'object' &&
  [Object.prototype, null].includes(Object.getPrototypeOf(value))

class SourceRefusal extends Error {
  constructor(code, message, status = 'invalid') {
    super(message)
    Object.assign(this, { code, status })
  }
}

function requireSource(condition, code, message, status) {
  if (!condition) throw new SourceRefusal(code, message, status)
}

function metadata(node, required = true) {
  requireSource(Array.isArray(node.pluginData), 'invalid-provenance', 'Native provenance must be an array')
  const entries = node.pluginData.filter(sourceEntry)
  if (!entries.length && !required) return null
  requireSource(entries.length > 0, 'missing-binding', 'Explicit source correspondence required', 'unsupported')
  requireSource(entries.length === 1, 'invalid-provenance', 'Duplicate PlatformKit source provenance')
  let value
  try { value = JSON.parse(entries[0].value) } catch {
    throw new SourceRefusal('invalid-provenance', 'Source provenance is not valid JSON')
  }
  requireSource(object(value), 'invalid-provenance', 'Source provenance must be an object')
  return value
}

function uniqueOccurrence(graph, instance, path) {
  for (const node of graph.getAllNodes()) {
    if (node === instance) continue
    const other = metadata(node, false)
    requireSource(!other || !isDeepStrictEqual(other.path, path), 'ambiguous-occurrence',
      'Multiple native instances claim this source occurrence')
  }
}

function propertyTargets(graph, root, id) {
  const pending = [root], seen = new Set(), matches = []
  while (pending.length) {
    const node = pending.pop()
    requireSource(!seen.has(node.id), 'invalid-binding', 'Cyclic or duplicated native property subtree')
    seen.add(node.id)
    requireSource(Array.isArray(node.childIds) && Array.isArray(node.componentPropertyReferences) &&
      node.componentPropertyReferences.every(object), 'invalid-binding', 'Malformed native property subtree')
    if (node.componentPropertyReferences.some(ref => ref.propertyId === id)) matches.push(node)
    if (node !== root && ['COMPONENT', 'INSTANCE'].includes(node.type)) continue
    for (const childId of node.childIds) {
      const child = graph.getNode(childId)
      requireSource(child && child.parentId === node.id, 'invalid-binding', 'Broken native property subtree')
      pending.push(child)
    }
  }
  return matches
}

function nativeLineage(operation) {
  try { return operation() } catch (error) {
    if (error instanceof SourceRefusal) throw error
    throw new SourceRefusal('invalid-binding', error.message)
  }
}

function sourceRoot(snapshot, path) {
  requireSource(snapshot?.schema === 'platformkit.design-export.v1' && /^[a-f0-9]{64}$/.test(snapshot.sha256) &&
    Array.isArray(snapshot.examples), 'invalid-source', 'An identified canonical source snapshot is required')
  requireSource(Array.isArray(path) && path.length > 0 && path.every(id => typeof id === 'string' && id !== ''),
    'invalid-path', 'Source path must contain exact invocation identities')
  requireSource(path.length === 1, 'unsupported-scope', 'Only one placed root may carry an absolute source path', 'unsupported')
  const examples = snapshot.examples.filter(example => example?.id === path[0])
  requireSource(examples.length === 1, 'invalid-path', 'Source path must identify exactly one root')
  return examples[0]
}

function readProps(graph, instance, snapshot, example) {
  requireSource(instance?.type === 'INSTANCE' && graph.getNode(instance.id) === instance,
    'invalid-binding', 'An exact native instance handle is required')
  const master = nativeLineage(() => chain(graph, instance, 'componentId').at(-1))
  requireSource(example.propsEditable === true && example.schema?.type === 'object',
    'unsupported-scope', 'Source invocation has no editable object contract', 'unsupported')
  requireSource(object(example.props) && object(example.schema.properties), 'invalid-source', 'Malformed source property contract')
  requireSource(master?.type === 'COMPONENT', 'invalid-binding', 'Instance must link to its canonical source master')
  const origin = metadata(master)
  requireSource(origin.schema === snapshot.schema && typeof origin.sha256 === 'string' &&
    /^[a-f0-9]{64}$/.test(origin.sha256), 'invalid-provenance', 'Master lacks complete source provenance')
  requireSource(origin.sha256 === snapshot.sha256, 'stale-base', 'Master source revision differs from the supplied snapshot', 'stale')
  requireSource(origin.exampleId === example.id && origin.componentId === example.componentId &&
    isDeepStrictEqual(origin.props, example.props), 'invalid-binding', 'Master does not match the source invocation')
  requireSource(origin.bindingVersion === 1 && Array.isArray(origin.textBindings),
    'invalid-provenance', 'Versioned source text bindings are required')
  requireSource(object(instance.componentPropertyAssignments), 'invalid-binding', 'Native assignments must be an object')
  requireSource(Array.isArray(master.componentPropertyDefinitions) && master.componentPropertyDefinitions.every(object),
    'invalid-binding', 'Malformed native property definitions')
  const ids = new Set(), fields = new Set(), changes = []
  for (const binding of origin.textBindings) {
    requireSource(object(binding) && Object.keys(binding).length === 2 && typeof binding.id === 'string' &&
      binding.id !== '' && typeof binding.property === 'string', 'invalid-provenance', 'Malformed source text binding')
    const { id, property } = binding
    requireSource(!ids.has(id) && !fields.has(property), 'invalid-binding', 'Duplicate source property correspondence')
    ids.add(id)
    fields.add(property)
    requireSource(Object.hasOwn(example.schema.properties, property),
      'invalid-binding', 'Binding must name an exact source property')
    requireSource(isSourceTextProperty(example, property), 'unsupported-scope', 'Only unconstrained source strings are supported', 'unsupported')
    const baseline = sourceTextValue(example, property)
    const definitions = master.componentPropertyDefinitions.filter(item => item.id === id)
    requireSource(definitions.length === 1 && definitions[0].type === 'TEXT' &&
      definitions[0].defaultValue === baseline, 'invalid-binding', 'Native definition differs from the source contract')
    // Exact text handles may sit in layout frames, but remain inside this
    // component boundary. Native names, order and visible lookalikes never bind.
    const sources = propertyTargets(graph, master, id)
    requireSource(sources.length === 1, 'invalid-binding', 'One direct native source target is required')
    const source = sources[0], targets = propertyTargets(graph, instance, id)
    requireSource(targets.length === 1, 'invalid-binding', 'One exact native target occurrence is required')
    const target = targets[0], reference = [{ propertyId: id, field: 'TEXT' }]
    requireSource(source.type === 'TEXT' && target.type === 'TEXT' && mappedNode(graph, master, instance, source) === target &&
      isDeepStrictEqual(source.componentPropertyReferences, reference) &&
      isDeepStrictEqual(target.componentPropertyReferences, reference) && source.text === baseline,
    'invalid-binding', 'Native target ownership or baseline differs from the source')
    requireSource(object(source.boundVariables) && object(target.boundVariables), 'invalid-binding', 'Malformed native variable bindings')
    requireSource(!Object.hasOwn(source.boundVariables, 'text') && !Object.hasOwn(target.boundVariables, 'text'),
      'unsupported-scope', 'Variable-bound text is not a literal property edit', 'unsupported')
    const value = Object.hasOwn(instance.componentPropertyAssignments, id) ? instance.componentPropertyAssignments[id] : definitions[0].defaultValue
    requireSource(typeof value === 'string' && target.text === value, 'inconsistent-native-value',
      'Native property assignment and bound text must agree')
    if (value !== baseline) changes.push([property, value])
  }
  requireSource(Object.keys(instance.componentPropertyAssignments).every(id => ids.has(id)), 'unsupported-scope',
    'Native assignments outside the supported source string bindings need another capability', 'unsupported')
  const slots = new Set(), slotBindings = origin.slotBindings === undefined ? [] : origin.slotBindings
  requireSource(Array.isArray(slotBindings), 'invalid-provenance', 'Malformed source slot correspondence')
  for (const binding of slotBindings) {
    requireSource(object(binding) && Object.keys(binding).length === 2 && typeof binding.id === 'string' &&
      binding.id !== '' && typeof binding.slot === 'string' && binding.slot !== '' &&
      !ids.has(binding.id) && !slots.has(binding.slot), 'invalid-binding', 'Unique exact source slot correspondence required')
    ids.add(binding.id)
    slots.add(binding.slot)
    requireSource(Array.isArray(example.slots), 'invalid-source', 'Source slot declarations are required')
    const declarations = example.slots.filter(slot => slot?.name === binding.slot), slot = declarations[0]
    requireSource(declarations.length === 1 && slot.supported === true && slot.trustedOnly === true &&
      (slot.goType === 'gomponents.Node' && slot.multiple === false || slot.goType === '[]gomponents.Node' && slot.multiple === true),
      'invalid-binding', 'Native asset binding must retain its source slot contract')
    const definitions = master.componentPropertyDefinitions.filter(item => item.id === binding.id)
    const sources = propertyTargets(graph, master, binding.id), targets = propertyTargets(graph, instance, binding.id)
    requireSource(definitions.length === 1 && definitions[0].type === 'INSTANCE_SWAP' &&
      typeof definitions[0].defaultValue === 'string' && definitions[0].defaultValue !== '' && sources.length === 1 &&
      targets.length === 1, 'invalid-binding', 'One native asset definition and target are required')
    const source = sources[0], target = targets[0], reference = [{ propertyId: binding.id, field: 'INSTANCE_SWAP' }]
    const asset = nativeLineage(() => chain(graph, source, 'componentId').at(-1))
    requireSource(source.type === 'INSTANCE' && target.type === 'INSTANCE' && asset?.type === 'COMPONENT' &&
      [asset.id, asset.source?.id, asset.componentKey, asset.sourceLibraryKey].includes(definitions[0].defaultValue) &&
      nativeLineage(() => chain(graph, target, 'componentId').at(-1)) === asset &&
      mappedNode(graph, master, instance, source) === target &&
      isDeepStrictEqual(source.componentPropertyReferences, reference) && isDeepStrictEqual(target.componentPropertyReferences, reference),
      'invalid-binding', 'Native asset correspondence differs from the source slot binding')
  }
  requireSource(master.componentPropertyDefinitions.length === ids.size &&
    master.componentPropertyDefinitions.every(definition => ids.has(definition.id)),
  'invalid-binding', 'Native definitions exceed the source property bindings')
  return { example, master, slots, props: Object.fromEntries(changes), properties: [...fields] }
}

// Follow the SDK's persisted correspondence at each level. Import can flatten
// a nested instance link; the existing resolver verifies descendant witnesses.
function mappedNode(graph, sourceRoot, instanceRoot, sourceTarget) {
  return nativeLineage(() => {
    const ancestry = chain(graph, sourceTarget, 'parentId'), end = ancestry.indexOf(sourceRoot)
    requireSource(end > 0, 'invalid-binding', 'Native target must be inside its owning master')
    let source = sourceRoot, instance = instanceRoot
    for (const child of ancestry.slice(0, end).reverse()) {
      const mapping = sourceChildren(graph, source, instance, instanceRoot.overrides)
      const matches = [...mapping].filter(([, linked]) => linked === child)
      requireSource(matches.length === 1, 'invalid-binding', 'One exact linked native occurrence is required')
      source = child
      instance = graph.getNode(matches[0][0])
    }
    return instance
  })
}

function nestedTemplates(graph, master) {
  requireSource(Array.isArray(master.childIds), 'invalid-binding', 'Malformed native composition')
  const pending = [...master.childIds], seen = new Set(), result = []
  while (pending.length) {
    const id = pending.pop(), node = graph.getNode(id)
    requireSource(node && !seen.has(id), 'invalid-binding', 'Missing or cyclic native composition')
    seen.add(id)
    requireSource(Array.isArray(node.childIds), 'invalid-binding', 'Malformed native composition')
    if (node.type === 'INSTANCE') {
      const local = metadata(node, false)
      if (local) {
        requireSource(Object.keys(local).length === 2 && typeof local.localId === 'string' && local.localId !== '' &&
          typeof local.slot === 'string' && local.slot !== '', 'invalid-provenance', 'Nested templates need only relative source identity and slot')
        result.push({ node, local })
      }
    } else {
      requireSource(node.type !== 'COMPONENT', 'invalid-binding', 'A nested definition is not a linked occurrence')
      pending.push(...node.childIds)
    }
  }
  return result
}

function visitSource(graph, instance, snapshot, example, path, visit, active = new Set()) {
  requireSource(!active.has(instance.id), 'invalid-binding', 'Cyclic source placement')
  const next = new Set(active).add(instance.id), result = readProps(graph, instance, snapshot, example)
  visit(instance, path, result)
  requireSource(Array.isArray(example.children) && Array.isArray(example.opaqueSlots), 'invalid-source', 'Source composition records are required')
  const templates = nestedTemplates(graph, result.master), used = new Set(), identities = new Set()
  for (const occurrence of example.children) {
    const child = occurrence?.description
    requireSource(child && typeof child.id === 'string' && child.id !== '' && !identities.has(child.id),
      'invalid-source', 'Nested source identities must be locally unique')
    identities.add(child.id)
    requireSource(example.opaqueSlots.length === 0 && object(occurrence.span) &&
      Number.isSafeInteger(occurrence.span.start) && Number.isSafeInteger(occurrence.span.end) &&
      occurrence.span.start >= 0 && occurrence.span.end >= occurrence.span.start && typeof occurrence.slot === 'string' &&
      occurrence.slot !== '', 'unsupported-scope', 'Nested source ownership must be observed and nonopaque', 'unsupported')
    requireSource(Array.isArray(example.slots), 'invalid-source', 'Source slot declarations are required')
    const slots = example.slots.filter(slot => slot?.name === occurrence.slot), slot = slots[0]
    requireSource(slots.length === 1 && slot.supported === true && slot.trustedOnly === true &&
      (slot.goType === 'gomponents.Node' && slot.multiple === false || slot.goType === '[]gomponents.Node' && slot.multiple === true),
      'unsupported-scope', 'Nested source slots must support trusted composition', 'unsupported')
    // Asset-slot properties are an existing, separate native capability. Their
    // source descendants gain no string-proposal correspondence by association.
    if (result.slots.has(occurrence.slot)) continue
    const matches = templates.filter(({ local }) => local.localId === child.id && local.slot === occurrence.slot)
    requireSource(matches.length === 1, 'invalid-binding', 'One relative native template must match each source child')
    const { node, local } = matches[0], placed = mappedNode(graph, result.master, instance, node)
    const baseline = readProps(graph, node, snapshot, child)
    requireSource(Object.keys(baseline.props).length === 0, 'invalid-binding', 'Nested template values differ from the source baseline')
    requireSource(isDeepStrictEqual(metadata(placed), local), 'invalid-provenance', 'Placed relative correspondence differs from its template')
    used.add(node.id)
    visitSource(graph, placed, snapshot, child, [...path, child.id], visit, next)
  }
  requireSource(used.size === templates.length, 'invalid-binding', 'Native relative templates exceed the source composition')
}

function placementRoot(graph, instance) {
  requireSource(instance?.type === 'INSTANCE' && graph.getNode(instance.id) === instance,
    'invalid-binding', 'An exact native instance handle is required')
  let root = instance
  const ancestors = new Set()
  for (let node = instance; node.type !== 'CANVAS';) {
    requireSource(!ancestors.has(node.id), 'invalid-binding', 'Cyclic native placement')
    ancestors.add(node.id)
    const parent = graph.getNode(node.parentId)
    requireSource(parent && Array.isArray(parent.childIds) && parent.childIds.includes(node.id), 'invalid-binding', 'Broken native placement')
    requireSource(['FRAME', 'INSTANCE', 'CANVAS'].includes(parent.type), 'unsupported-scope', 'A page-placed source instance is required', 'unsupported')
    if (parent.type === 'INSTANCE') root = parent
    node = parent
  }
  return root
}

// Call explicitly after graph.createInstance. Originating from a master does
// not make every preview or copied instance another source-owned placement.
export function associateSourceInstance(graph, instance, snapshot, path) {
  requireSource(placementRoot(graph, instance) === instance, 'unsupported-scope', 'Associate the placed root, not a nested occurrence', 'unsupported')
  const example = sourceRoot(snapshot, path)
  visitSource(graph, instance, snapshot, example, path, () => {})
  uniqueOccurrence(graph, instance, path)
  const value = { schema: snapshot.schema, sha256: snapshot.sha256, path: [...path], componentId: example.componentId }
  const previous = metadata(instance, false)
  requireSource(!previous || isDeepStrictEqual(previous, value), 'invalid-provenance', 'Instance already carries different source correspondence')
  if (!previous) graph.updateNode(instance.id, { pluginData: [...instance.pluginData, {
    pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify(value),
  }] })
  return instance
}

// Read only this capability's bound properties, not whole-document equality.
// The caller supplies canonical source; metadata is correspondence, not trust.
export function extractSourceProps(graph, instance, snapshot) {
  let capability = 'root-string-props'
  try {
    const root = placementRoot(graph, instance), origin = metadata(root), example = sourceRoot(snapshot, origin.path)
    if (root !== instance) capability = 'nested-string-props'
    requireSource(origin.schema === snapshot.schema && origin.componentId === example.componentId,
      'invalid-provenance', 'Occurrence interface differs from the canonical source')
    requireSource(origin.sha256 === snapshot.sha256, 'stale-base', 'Occurrence source revision differs from the supplied snapshot', 'stale')
    uniqueOccurrence(graph, root, origin.path)
    let selected
    visitSource(graph, root, snapshot, example, origin.path, (node, path, result) => {
      if (node === instance) selected = { ...result, path }
    })
    requireSource(selected, 'missing-binding', 'Selected instance has no source correspondence inside the associated root', 'unsupported')
    const { props, properties, path } = selected
    if (!Object.keys(props).length) return { status: 'no-supported-changes', capability, properties }
    return { status: 'proposal', capability, properties, proposal: { baseSHA256: snapshot.sha256, path, props } }
  } catch (error) {
    if (!(error instanceof SourceRefusal)) throw error
    return { status: error.status, capability, code: error.code, message: error.message }
  }
}
