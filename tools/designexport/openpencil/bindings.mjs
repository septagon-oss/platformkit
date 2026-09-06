import { generateId } from '@open-pencil/scene-graph'

function requireBinding(condition, message) {
  if (!condition) throw new Error(`Source text binding: ${message}`)
}

function plainObject(value) {
  return value !== null && typeof value === 'object' &&
    [Object.prototype, null].includes(Object.getPrototypeOf(value))
}

// Construction handles supplied by the converter, not a lookup by node name,
// visible text or child order. This runs before the fresh master has instances.
// Binding identity is separate from native layout and replacement readiness.
export function bindTextProperties(graph, master, example, targets) {
  requireBinding(master?.type === 'COMPONENT' && graph.getNode(master.id) === master, 'canonical component master required')
  requireBinding(master.componentPropertyDefinitions.length === 0 && graph.getInstances(master.id).length === 0,
    'bind a fresh master before definitions or instances exist')
  requireBinding(example?.propsEditable === true && plainObject(example.schema) && Object.hasOwn(example.schema, 'type') &&
    example.schema.type === 'object', 'typed source example required')
  requireBinding(plainObject(example.schema.properties) && plainObject(example.props), 'source property schemas and values must be plain objects')
  requireBinding(Array.isArray(targets) && targets.length > 0, 'explicit text targets required')
  const properties = new Set(), nodes = new Set()
  for (const target of targets) {
    const { region, nativeNode } = target ?? {}
    const property = region?.property
    requireBinding(plainObject(region) && region.kind === 'text' && Object.hasOwn(region, 'property') && typeof property === 'string' && property !== '' &&
      Object.hasOwn(example.schema.properties, property) && Object.hasOwn(example.props, property), 'unknown source text property')
    const schema = example.schema.properties[property]
    requireBinding(plainObject(schema) && Object.hasOwn(schema, 'type') && schema.type === 'string' &&
      Object.keys(schema).every(key => ['type', 'title', 'description'].includes(key)), 'unconstrained source string property required')
    requireBinding(typeof example.props[property] === 'string' && region.text === example.props[property], 'observed text differs from source value')
    requireBinding(nativeNode?.type === 'TEXT' && graph.getNode(nativeNode.id) === nativeNode &&
      nativeNode.text === region.text, 'canonical native text must retain the observed value')
    requireBinding(nativeNode.componentPropertyReferences.length === 0, 'native text already has property references')
    requireBinding(!properties.has(property) && !nodes.has(nativeNode.id), 'ambiguous property or native target')
    const visited = new Set()
    for (let current = nativeNode; current !== master;) {
      requireBinding(!visited.has(current.id), 'cyclic native ancestry')
      visited.add(current.id)
      const parent = graph.getNode(current.parentId)
      requireBinding(parent && graph.getChildren(parent.id).includes(current), 'native target is outside the master or has broken ancestry')
      requireBinding(parent === master || !['COMPONENT', 'INSTANCE', 'CANVAS', 'DOCUMENT'].includes(parent.type),
        'native text belongs to another component boundary')
      current = parent
    }
    properties.add(property)
    nodes.add(nativeNode.id)
  }
  // No source, region or graph value changes until every target passes.
  const occupied = new Set([...graph.getAllNodes()]
    .flatMap(node => node.componentPropertyDefinitions.map(definition => definition.id)))
  const definitions = targets.map(({ region }) => {
    let id = generateId()
    while (occupied.has(id)) id = generateId()
    occupied.add(id)
    return { id, name: region.property, type: 'TEXT', defaultValue: example.props[region.property] }
  })
  graph.updateNode(master.id, { componentPropertyDefinitions: structuredClone(definitions) })
  for (const [index, { nativeNode }] of targets.entries()) {
    graph.updateNode(nativeNode.id, {
      componentPropertyReferences: [{ propertyId: definitions[index].id, field: 'TEXT' }],
    })
  }
  return definitions
}
