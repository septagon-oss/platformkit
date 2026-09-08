import { isDeepStrictEqual } from 'node:util'
import { captureExample } from './browser/capture.mjs'
import { materializeComponent } from './components.mjs'
import { sourceVariantContext } from './bindings.mjs'
import { buildFoundation } from './foundation.mjs'
import { chain } from './exporter-correction.mjs'
import { validateFonts } from './fonts.mjs'
import { associateSourceInstance, extractSourceProps } from './source-changes.mjs'

// Build a new document from one caller-owned export. Selection is explicit:
// failure of any requested root rejects the document, never a partial library.
export async function buildComponentDocument(snapshot, {
  examples, fonts, browser, renderer, variants = [], mode = 'light', viewport = { width: 1280, height: 900 },
}) {
  if (!Array.isArray(examples) || examples.length === 0 ||
    examples.some(id => typeof id !== 'string' || id === '') || new Set(examples).size !== examples.length) {
    throw new Error('Document requires nonempty, unique source example IDs')
  }
  const selected = [...examples]
  for (const id of selected) {
    if (snapshot?.examples?.filter(example => example.id === id).length !== 1) {
      throw new Error(`Document requires exactly one source example: ${id}`)
    }
  }
  if (!Array.isArray(variants)) throw new Error('Document variants must be an array of source projections')
  const families = new Map()
  for (const variant of variants) {
    if (!variant || !selected.includes(variant.exampleId) || typeof variant.property !== 'string' ||
      variant.property === '' || !Object.hasOwn(variant, 'snapshot')) {
      throw new Error('Document variants require a selected exampleId, exact property and projected snapshot')
    }
    const path = Object.hasOwn(variant, 'path') ? variant.path : [variant.exampleId]
    try { sourceVariantContext(snapshot, path) } catch (error) { throw new Error(`Document variant: ${error.message}`, { cause: error }) }
    if (path[0] !== variant.exampleId) throw new Error('Document variant path must belong to its selected root')
    const states = families.get(variant.exampleId) ?? []
    if (states.some(state => isDeepStrictEqual(state.path, path) && state.property !== variant.property)) {
      throw new Error('Document families currently support one source property')
    }
    families.set(variant.exampleId, [...states, { ...variant, path: [...path] }])
  }
  const faces = validateFonts(fonts)
  if (faces.length === 0) throw new Error('Component documents require caller-supplied fonts')
  const foundation = buildFoundation(snapshot), { graph, collection, icons } = foundation
  const selectedMode = collection.modes.find(item => item.name === mode)
  if (!selectedMode) throw new Error(`Unknown document theme: ${mode}`)
  const background = graph.getVariablesForCollection(collection.id).find(item => item.name === '--pk-color-surface-canvas')
  function board(name) {
    const page = graph.addPage(name)
    const frame = graph.createNode('FRAME', page.id, {
      name, fills: [{ type: 'SOLID', color: background.valuesByMode[selectedMode.modeId], opacity: 1, visible: true }],
      variableModes: { [collection.id]: selectedMode.modeId }, clipsContent: false,
    })
    graph.bindVariable(frame.id, 'fills/0/color', background.id)
    return frame
  }
  const definitions = board('Component definitions'), placements = board('Editable source instances')
  async function construct(projected, exampleId) {
    const observation = await captureExample(browser, projected, exampleId, { mode, viewport, fonts: faces })
    const slots = observation.roots[0]?.children?.filter(child => child.kind === 'slot') ?? []
    // Resolve only explicit canonical glyph handles; the materializer owns
    // region, geometry, slot and source-interface validation for every state.
    const targets = slots.map(region => ({ region, master: icons.get(region.children[0]?.icon?.canonicalName) }))
    const variants = []
    for (const request of families.get(exampleId) ?? []) variants.push({ ...request,
      observation: await captureExample(browser, request.snapshot, exampleId, { mode, viewport, fonts: faces }) })
    return { observation, ...await materializeComponent(graph, definitions.id, projected, observation, faces, renderer, collection.id, targets, { variants }) }
  }
  const selections = []
  let definitionY = 48, placementY = 48, definitionWidth = 0, placementWidth = 0
  for (const exampleId of selected) {
    try {
      const built = await construct(snapshot, exampleId)
      for (const { family } of built.families) {
        let y = 48, width = 0
        const property = family.componentPropertyDefinitions.find(item => item.type === 'VARIANT').name
        for (const master of graph.getChildren(family.id)) {
          graph.updateNode(master.id, { name: `${property} = ${JSON.stringify(master.componentPropertyValues[property])}`, x: 48, y })
          y += master.height + 48
          width = Math.max(width, master.width)
        }
        graph.updateNode(family.id, { width: width + 96, height: y })
      }
      const units = [...built.components.filter(item => graph.getNode(item.master.parentId)?.type !== 'COMPONENT_SET'),
        ...built.families.map(item => ({ path: item.path, master: item.family }))]
      for (const { path, master } of units) {
        graph.insertChildAt(master.id, definitions.id, definitions.childIds.length)
        graph.updateNode(master.id, { name: path.join(' / '), x: 48, y: definitionY })
        definitionY += master.height + 48
        definitionWidth = Math.max(definitionWidth, master.width)
      }
      const instance = graph.createInstance(built.master.id, placements.id, { name: exampleId, x: 48, y: placementY })
      associateSourceInstance(graph, instance, snapshot, [exampleId])
      placementY += instance.height + 48
      placementWidth = Math.max(placementWidth, instance.width)
      selections.push({ exampleId, ...built, instance })
    } catch (error) {
      throw new Error(`Document example ${exampleId}: ${error.message}`, { cause: error })
    }
  }
  graph.updateNode(definitions.id, { width: definitionWidth + 96, height: definitionY })
  graph.updateNode(placements.id, { width: placementWidth + 96, height: placementY })
  verifyComponentDocument(graph, snapshot, selected)
  return { ...foundation, definitions, placements, selections }
}

// Reopened IDs are file-local. Recover correspondence from existing source
// records, then let the owning native/source contract validate each subtree.
// This checks supported baseline properties, not arbitrary scene equality.
export function verifyComponentDocument(graph, snapshot, examples, expected) {
  const roots = new Map(), correspondence = []
  const source = node => {
    const entries = node.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
    if (entries.length > 1) throw new Error('Duplicate document source provenance')
    return entries.length ? JSON.parse(entries[0].value) : null
  }
  for (const node of graph.getAllNodes()) {
    const origin = source(node)
    if (!origin || !Object.hasOwn(origin, 'path')) continue
    if (node.type !== 'INSTANCE' || !Array.isArray(origin.path) || origin.path.length !== 1 ||
      !examples.includes(origin.path[0]) || roots.has(origin.path[0])) {
      throw new Error('Document source placements differ from the requested selection')
    }
    roots.set(origin.path[0], node)
  }
  if (roots.size !== examples.length) throw new Error('Document lost a requested source placement')
  for (const [id, root] of roots) {
    const pending = [{ node: root, path: [id] }], seen = new Set()
    while (pending.length) {
      const { node, path } = pending.pop()
      if (seen.has(node.id)) throw new Error(`Document example ${id}: cyclic native subtree`)
      seen.add(node.id)
      if (node === root || Object.hasOwn(source(node) ?? {}, 'localId')) {
        const result = extractSourceProps(graph, node, snapshot)
        if (result.status !== 'no-supported-changes') {
          throw new Error(`Document example ${id}: ${result.message ?? result.status}`)
        }
        const master = chain(graph, node, 'componentId').at(-1), parent = graph.getNode(master.parentId)
        correspondence.push({ path, origin: source(master),
          ...(parent?.type === 'COMPONENT_SET' ? { family: source(parent) } : {}) })
      }
      for (const child of graph.getChildren(node.id)) {
        const localId = source(child)?.localId
        pending.push({ node: child, path: localId === undefined ? path : [...path, localId] })
      }
    }
  }
  correspondence.sort((a, b) => JSON.stringify(a.path).localeCompare(JSON.stringify(b.path)))
  // Compare the construction-time records, not only the imported graph against
  // itself: losing both a definition and its mapping must not become success.
  if (expected !== undefined && !isDeepStrictEqual(correspondence, expected)) {
    throw new Error('Native source correspondence changed during FIG save')
  }
  return correspondence
}
