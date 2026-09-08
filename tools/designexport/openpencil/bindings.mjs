import { generateId } from '@open-pencil/scene-graph'
import { isDeepStrictEqual } from 'node:util'

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

// Group already constructed source projections, not synthetic label variants.
// The set owns one interface; each child retains its own projection provenance.
export function bindComponentVariants(graph, owner, snapshot, exampleId, property, variants) {
  requireBinding(snapshot?.schema === 'platformkit.design-export.v1' && /^[a-f0-9]{64}$/.test(snapshot.sha256) &&
    typeof property === 'string' && property !== '', 'identified source export and exact property name required')
  requireBinding(owner?.type === 'COMPONENT_SET' && graph.getNode(owner.id) === owner &&
    owner.childIds.length === 0 && owner.componentPropertyDefinitions.length === 0 &&
    !owner.pluginData.some(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source'), 'fresh native component set required')
  const examples = snapshot?.examples?.filter(item => item.id === exampleId), example = examples?.[0]
  requireBinding(examples?.length === 1 && example.propsEditable === true && plainObject(example.schema) &&
    Object.hasOwn(example.schema, 'type') && example.schema.type === 'object' && isSourceTextProperty(example, property),
    'one editable source string property required')
  requireBinding(Array.isArray(variants) && variants.length > 1 &&
    new Set(variants.map(item => item?.master?.id)).size === variants.length, 'distinct native source variants required')
  const without = (value, keys) => Object.fromEntries(Object.entries(value).filter(([key]) => !keys.includes(key)))
  const ambient = value => without(value, ['sha256', 'examples'])
  const states = variants.map(({ snapshot: projected, master }) => {
    requireBinding(master?.type === 'COMPONENT' && graph.getNode(master.id) === master && graph.getInstances(master.id).length === 0 &&
      master.parentId === owner.parentId && master.variantPropSpecs.length === 0 && Object.keys(master.componentPropertyValues).length === 0,
    'fresh canonical source component required')
    const candidates = projected?.examples?.filter(item => item.id === exampleId), candidate = candidates?.[0]
    requireBinding(candidates?.length === 1 && /^[a-f0-9]{64}$/.test(projected.sha256) && isDeepStrictEqual(ambient(projected), ambient(snapshot)) &&
      isDeepStrictEqual(projected.examples.filter(item => item.id !== exampleId), snapshot.examples.filter(item => item.id !== exampleId)),
    'variant projection must retain the source export context')
    requireBinding(candidate.componentId === example.componentId && candidate.propsEditable === true &&
      isDeepStrictEqual(candidate.schema, example.schema) && isDeepStrictEqual(candidate.slots, example.slots) &&
      candidate.children?.length === 0 && candidate.opaqueSlots?.length === 0 && example.children?.length === 0 && example.opaqueSlots?.length === 0 &&
      plainObject(candidate.props) && isDeepStrictEqual(without(candidate.props, [property]), without(example.props, [property])),
    'variant projection must change only one property of the same nonopaque leaf interface')
    const value = sourceTextValue(candidate, property)
    requireBinding(typeof value === 'string', 'variant projection requires an exact source string value')
    const records = master.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
    requireBinding(records.length === 1, 'one constructed source projection record required')
    const origin = JSON.parse(records[0].value)
    requireBinding(origin.schema === projected.schema && origin.sha256 === projected.sha256 && origin.exampleId === exampleId &&
      origin.componentId === candidate.componentId && isDeepStrictEqual(origin.props, candidate.props) && origin.bindingVersion === 1 &&
      Array.isArray(origin.textBindings) && origin.slotBindings?.length === 0 &&
      master.componentPropertyDefinitions.length === origin.textBindings.length, 'constructed variant must retain its source bindings')
    const targets = [], ids = new Set(), properties = new Set()
    for (const binding of origin.textBindings) {
      const definitions = master.componentPropertyDefinitions.filter(item => item.id === binding.id), definition = definitions[0]
      requireBinding(!ids.has(binding.id) && !properties.has(binding.property) && definitions.length === 1 && definition.type === 'TEXT' &&
        isSourceTextProperty(candidate, binding.property) && definition.defaultValue === sourceTextValue(candidate, binding.property),
      'one literal definition per source text property required')
      ids.add(binding.id); properties.add(binding.property)
      const pending = [...graph.getChildren(master.id)], found = [], seen = new Set()
      while (pending.length) {
        const node = pending.pop()
        requireBinding(!seen.has(node.id) && !['COMPONENT', 'INSTANCE', 'COMPONENT_SET', 'CANVAS', 'DOCUMENT'].includes(node.type),
          'variant text must retain one direct component boundary')
        seen.add(node.id)
        if (node.componentPropertyReferences.some(ref => ref.propertyId === binding.id)) found.push(node)
        pending.push(...graph.getChildren(node.id))
      }
      requireBinding(found.length === 1 && found[0].type === 'TEXT' && found[0].text === definition.defaultValue &&
        isDeepStrictEqual(found[0].componentPropertyReferences, [{ propertyId: binding.id, field: 'TEXT' }]),
      'one exact native text target must retain the source value')
      targets.push({ property: binding.property, definition, node: found[0] })
    }
    return { master, origin, targets, value }
  })
  requireBinding(new Set(states.map(item => item.value)).size === states.length, 'duplicate source variant value')
  const baseline = states.find(item => item.value === sourceTextValue(example, property))
  requireBinding(baseline?.origin.sha256 === snapshot.sha256, 'the exact baseline source projection must be included')
  const shared = baseline.targets.filter(item => item.property !== property)
  for (const state of states) {
    requireBinding(shared.length === state.targets.filter(item => item.property !== property).length && shared.every(item =>
      state.targets.some(target => target.property === item.property && target.definition.defaultValue === item.definition.defaultValue)) &&
      ['mode', 'environment', 'viewport', 'fontFaces', 'definitionPath'].every(key => isDeepStrictEqual(state.origin[key], baseline.origin[key])),
    'variants must share text properties and one observation profile')
  }
  const occupied = new Set([...graph.getAllNodes()].flatMap(node => node.componentPropertyDefinitions.map(definition => definition.id)))
  let id = generateId()
  while (occupied.has(id)) id = generateId()
  const definitions = [...shared.map(item => ({ ...item.definition })),
    { id, name: property, type: 'VARIANT', defaultValue: baseline.value, variantOptions: states.map(item => item.value) }]
  requireBinding(new Set(definitions.map(item => item.name)).size === definitions.length, 'shared native control names must be distinct')
  const textBindings = shared.map(item => ({ id: item.definition.id, property: item.property }))
  const origin = { ...baseline.origin, scope: 'source-variant-family', bindingVersion: 2, textBindings,
    variantBindings: [{ id, property, projections: states.map(item => ({ value: item.value, sha256: item.origin.sha256 })) }] }
  // All source/native correspondence checks precede the first graph write.
  graph.updateNode(owner.id, { componentPropertyDefinitions: structuredClone(definitions), pluginData: [...owner.pluginData,
    { pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify(origin) }] })
  for (const [index, state] of states.entries()) {
    for (const target of state.targets) graph.updateNode(target.node.id, { componentPropertyReferences: target.property === property ? [] :
      [{ propertyId: shared.find(item => item.property === target.property).definition.id, field: 'TEXT' }] })
    graph.updateNode(state.master.id, { componentPropertyDefinitions: [], componentPropertyValues: { [property]: state.value },
      variantPropSpecs: [{ propDefId: id, value: state.value }], pluginData: state.master.pluginData.map(item =>
        item.pluginId === 'platformkit' && item.key === 'platformkit.source' ? { ...item, value: JSON.stringify({ ...state.origin, textBindings }) } : item) })
    graph.insertChildAt(state.master.id, owner.id, index)
  }
  return definitions
}
