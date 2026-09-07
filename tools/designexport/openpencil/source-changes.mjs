import { isDeepStrictEqual } from 'node:util'
import { isSourceTextProperty } from './bindings.mjs'

const capability = 'root-string-props'
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
    for (const childId of node.childIds) {
      const child = graph.getNode(childId)
      requireSource(child && child.parentId === node.id, 'invalid-binding', 'Broken native property subtree')
      pending.push(child)
    }
  }
  return matches
}

function readProps(graph, instance, snapshot, path) {
  requireSource(instance?.type === 'INSTANCE' && graph.getNode(instance.id) === instance,
    'invalid-binding', 'An exact native instance handle is required')
  requireSource(snapshot?.schema === 'platformkit.design-export.v1' && /^[a-f0-9]{64}$/.test(snapshot.sha256) &&
    Array.isArray(snapshot.examples), 'invalid-source', 'An identified canonical source snapshot is required')
  requireSource(Array.isArray(path) && path.length > 0 && path.every(id => typeof id === 'string' && id !== ''),
    'invalid-path', 'Source path must contain exact invocation identities')
  requireSource(path.length === 1, 'unsupported-scope', 'Nested source projection is not supported here', 'unsupported')
  const examples = snapshot.examples.filter(example => example?.id === path[0])
  requireSource(examples.length === 1, 'invalid-path', 'Source path must identify exactly one root')
  const example = examples[0], master = graph.getNode(instance.componentId)
  requireSource(example.propsEditable === true && example.schema?.type === 'object',
    'unsupported-scope', 'Source invocation has no editable object contract', 'unsupported')
  requireSource(object(example.props) && object(example.schema.properties), 'invalid-source', 'Malformed source property contract')
  requireSource(master?.type === 'COMPONENT', 'invalid-binding', 'Instance must link directly to its source master')
  const origin = metadata(master)
  requireSource(origin.schema === snapshot.schema && typeof origin.sha256 === 'string' &&
    /^[a-f0-9]{64}$/.test(origin.sha256), 'invalid-provenance', 'Master lacks complete source provenance')
  requireSource(origin.sha256 === snapshot.sha256, 'stale-base', 'Master source revision differs from the supplied snapshot', 'stale')
  requireSource(origin.exampleId === example.id && origin.componentId === example.componentId &&
    isDeepStrictEqual(origin.props, example.props), 'invalid-binding', 'Master does not match the source invocation')
  requireSource(origin.bindingVersion === 1 && Array.isArray(origin.textBindings),
    'invalid-provenance', 'Versioned source text bindings are required')
  requireSource(origin.textBindings.length > 0, 'unsupported-scope', 'No source string properties are bound', 'unsupported')
  const ancestors = new Set()
  for (let node = instance; node.type !== 'CANVAS';) {
    requireSource(!ancestors.has(node.id), 'invalid-binding', 'Cyclic native placement')
    ancestors.add(node.id)
    const parent = graph.getNode(node.parentId)
    requireSource(parent && Array.isArray(parent.childIds) && parent.childIds.includes(node.id), 'invalid-binding', 'Broken native placement')
    requireSource(['FRAME', 'CANVAS'].includes(parent.type), 'unsupported-scope', 'A page-placed source instance is required', 'unsupported')
    node = parent
  }
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
    requireSource(Object.hasOwn(example.props, property) && Object.hasOwn(example.schema.properties, property),
      'invalid-binding', 'Binding must name an exact source property')
    requireSource(isSourceTextProperty(example, property), 'unsupported-scope', 'Only unconstrained source strings are supported', 'unsupported')
    const definitions = master.componentPropertyDefinitions.filter(item => item.id === id)
    requireSource(definitions.length === 1 && definitions[0].type === 'TEXT' &&
      definitions[0].defaultValue === example.props[property], 'invalid-binding', 'Native definition differs from the source contract')
    // This capability covers the direct text handles the current constructor
    // creates. Native names, sibling order and visible lookalikes never bind.
    const sources = propertyTargets(graph, master, id)
    requireSource(sources.length === 1, 'invalid-binding', 'One direct native source target is required')
    const source = sources[0], targets = propertyTargets(graph, instance, id)
    requireSource(targets.length === 1, 'invalid-binding', 'One exact native target occurrence is required')
    const target = targets[0], reference = [{ propertyId: id, field: 'TEXT' }]
    requireSource(source.type === 'TEXT' && target.type === 'TEXT' && target.componentId === source.id && source.parentId === master.id &&
      target.parentId === instance.id && isDeepStrictEqual(source.componentPropertyReferences, reference) &&
      isDeepStrictEqual(target.componentPropertyReferences, reference) && source.text === example.props[property],
    'invalid-binding', 'Native target ownership or baseline differs from the source')
    requireSource(object(source.boundVariables) && object(target.boundVariables), 'invalid-binding', 'Malformed native variable bindings')
    requireSource(!Object.hasOwn(source.boundVariables, 'text') && !Object.hasOwn(target.boundVariables, 'text'),
      'unsupported-scope', 'Variable-bound text is not a literal property edit', 'unsupported')
    const value = Object.hasOwn(instance.componentPropertyAssignments, id) ? instance.componentPropertyAssignments[id] : definitions[0].defaultValue
    requireSource(typeof value === 'string' && target.text === value, 'inconsistent-native-value',
      'Native property assignment and bound text must agree')
    if (value !== example.props[property]) changes.push([property, value])
  }
  requireSource(Object.keys(instance.componentPropertyAssignments).every(id => ids.has(id)), 'unsupported-scope',
    'Native assignments outside the supported source string bindings need another capability', 'unsupported')
  uniqueOccurrence(graph, instance, path)
  return { example, props: Object.fromEntries(changes), properties: [...fields] }
}

// Call explicitly after graph.createInstance. Originating from a master does
// not make every preview or copied instance another source-owned placement.
export function associateSourceInstance(graph, instance, snapshot, path) {
  const { example } = readProps(graph, instance, snapshot, path)
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
  try {
    requireSource(instance?.type === 'INSTANCE' && graph.getNode(instance.id) === instance,
      'invalid-binding', 'An exact native instance handle is required')
    const origin = metadata(instance)
    const { example, props, properties } = readProps(graph, instance, snapshot, origin.path)
    requireSource(origin.schema === snapshot.schema && origin.componentId === example.componentId,
      'invalid-provenance', 'Occurrence interface differs from the canonical source')
    requireSource(origin.sha256 === snapshot.sha256, 'stale-base', 'Occurrence source revision differs from the supplied snapshot', 'stale')
    if (!Object.keys(props).length) return { status: 'no-supported-changes', capability, properties }
    return { status: 'proposal', capability, properties, proposal: { baseSHA256: snapshot.sha256, path: [...origin.path], props } }
  } catch (error) {
    if (!(error instanceof SourceRefusal)) throw error
    return { status: error.status, capability, code: error.code, message: error.message }
  }
}
