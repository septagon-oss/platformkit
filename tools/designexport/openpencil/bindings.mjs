import { generateId } from '@open-pencil/scene-graph'

function requireBinding(condition, message) {
  if (!condition) throw new Error(`Source component binding: ${message}`)
}

function plainObject(value) {
  return value !== null && typeof value === 'object' &&
    [Object.prototype, null].includes(Object.getPrototypeOf(value))
}

export function isSourceTextProperty(example, property) {
  const properties = example?.schema?.properties, props = example?.props
  if (!plainObject(properties) || !plainObject(props) || !Object.hasOwn(properties, property)) return false
  const schema = properties[property]
  return plainObject(schema) && Object.hasOwn(schema, 'type') && schema.type === 'string' &&
    (example.schema.required === undefined || Array.isArray(example.schema.required)) &&
    Object.keys(schema).every(key => ['type', 'title', 'description', 'default'].includes(key)) &&
    (!Object.hasOwn(schema, 'default') || schema.default === '') &&
    (Object.hasOwn(props, property) ? typeof props[property] === 'string' :
      Object.hasOwn(schema, 'default') && !example.schema.required?.includes(property))
}

export function sourceTextValue(example, property) {
  if (!isSourceTextProperty(example, property)) return undefined
  return Object.hasOwn(example.props, property) ? example.props[property] : example.schema.properties[property].default
}

// Construction handles supplied by the converter, not a lookup by node name,
// visible text or child order. This runs before the fresh master has instances.
// Binding identity is separate from native layout and replacement readiness.
export function bindComponentProperties(graph, master, example, targets) {
  requireBinding(master?.type === 'COMPONENT' && graph.getNode(master.id) === master, 'canonical component master required')
  requireBinding(master.componentPropertyDefinitions.length === 0 && graph.getInstances(master.id).length === 0,
    'bind a fresh master before definitions or instances exist')
  requireBinding(example?.propsEditable === true && plainObject(example.schema) && Object.hasOwn(example.schema, 'type') &&
    example.schema.type === 'object', 'typed source example required')
  requireBinding(plainObject(example.schema.properties) && plainObject(example.props), 'source property schemas and values must be plain objects')
  requireBinding(Array.isArray(targets), 'explicit property targets required')
  const records = master.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
  requireBinding(records.length <= 1, 'duplicate source provenance')
  const provenance = records.length ? JSON.parse(records[0].value) : {}
  requireBinding(plainObject(provenance) && !Object.hasOwn(provenance, 'textBindings') &&
    !Object.hasOwn(provenance, 'slotBindings') && !Object.hasOwn(provenance, 'bindingVersion'), 'fresh source binding provenance required')
  const properties = new Set(), nodes = new Set(), planned = []
  for (const target of targets) {
    const { region, nativeNode } = target ?? {}
    requireBinding(plainObject(region) && Object.hasOwn(region, 'kind') && ['text', 'control', 'slot'].includes(region.kind),
      'known source property region required')
    const key = region.kind === 'slot' ? 'name' : 'property', property = region[key]
    requireBinding(Object.hasOwn(region, key) && typeof property === 'string' && property !== '', 'own source property name required')
    requireBinding(nativeNode && graph.getNode(nativeNode.id) === nativeNode, 'canonical native property target required')
    if (region.kind !== 'slot') {
      requireBinding(isSourceTextProperty(example, property), 'unconstrained source string property required')
      const value = sourceTextValue(example, property), observed = region.kind === 'text' ? region.text : region.value
      requireBinding(region.kind !== 'control' || Object.hasOwn(region, 'type') && ['text', 'textarea'].includes(region.type) &&
        (region.placeholder === undefined || region.placeholder === ''),
        'only literal text controls are supported')
      requireBinding(observed === value, 'observed text differs from source value')
      requireBinding(nativeNode.type === 'TEXT' && nativeNode.text === observed, 'canonical native text must retain the observed value')
      planned.push({ name: property, type: 'TEXT', defaultValue: value })
    } else {
      requireBinding(Array.isArray(example.slots), 'source slot declarations required')
      const declarations = example.slots.filter(slot => slot?.name === property), declaration = declarations[0]
      requireBinding(declarations.length === 1 && plainObject(declaration) &&
        ['name', 'goType', 'supported', 'multiple', 'trustedOnly'].every(field => Object.hasOwn(declaration, field)) &&
        declaration.supported === true && declaration.trustedOnly === true &&
        (declaration.goType === 'gomponents.Node' && declaration.multiple === false ||
          declaration.goType === '[]gomponents.Node' && declaration.multiple === true), 'one supported trusted source slot declaration required')
      requireBinding(Array.isArray(region.children) && region.children.length === 1 && plainObject(region.children[0]) &&
        region.children[0].kind === 'element' && region.children[0].tag === 'svg', 'one rendered SVG slot child required')
      requireBinding(nativeNode.type === 'INSTANCE' && graph.getNode(nativeNode.componentId)?.type === 'COMPONENT',
        'native slot must link directly to a component master')
      planned.push({ name: property, type: 'INSTANCE_SWAP', defaultValue: nativeNode.componentId })
    }
    requireBinding(nativeNode.componentPropertyReferences.length === 0, 'native target already has property references')
    requireBinding(!properties.has(property) && !nodes.has(nativeNode.id), 'ambiguous property or native target')
    const visited = new Set()
    for (let current = nativeNode; current !== master;) {
      requireBinding(!visited.has(current.id), 'cyclic native ancestry')
      visited.add(current.id)
      const parent = graph.getNode(current.parentId)
      requireBinding(parent && graph.getChildren(parent.id).includes(current), 'native target is outside the master or has broken ancestry')
      requireBinding(parent === master || !['COMPONENT', 'INSTANCE', 'CANVAS', 'DOCUMENT'].includes(parent.type),
        'native target belongs to another component boundary')
      current = parent
    }
    properties.add(property)
    nodes.add(nativeNode.id)
  }
  // No source, region or graph value changes until every target passes.
  const occupied = new Set([...graph.getAllNodes()]
    .flatMap(node => node.componentPropertyDefinitions.map(definition => definition.id)))
  const definitions = planned.map(definition => {
    let id = generateId()
    while (occupied.has(id)) id = generateId()
    occupied.add(id)
    return { id, ...definition }
  })
  // Only construction establishes this correspondence. Native display names
  // can subsequently change; binding alone does not invent a source address.
  const textBindings = definitions.filter(item => item.type === 'TEXT').map(item => ({ id: item.id, property: item.name }))
  const slotBindings = definitions.filter(item => item.type === 'INSTANCE_SWAP').map(item => ({ id: item.id, slot: item.name }))
  graph.updateNode(master.id, {
    componentPropertyDefinitions: structuredClone(definitions),
    pluginData: [...master.pluginData.filter(item => !records.includes(item)), {
      pluginId: 'platformkit', key: 'platformkit.source',
      value: JSON.stringify({ ...provenance, bindingVersion: 1, textBindings, slotBindings }),
    }],
  })
  for (const [index, { nativeNode }] of targets.entries()) {
    graph.updateNode(nativeNode.id, {
      componentPropertyReferences: [{ propertyId: definitions[index].id, field: definitions[index].type }],
    })
  }
  return definitions
}
