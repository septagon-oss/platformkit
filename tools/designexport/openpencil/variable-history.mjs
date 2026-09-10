import { validateNumericVariables } from './variable-number.mjs'
import { validateCSSColors, validateResolvedColors } from './variable-color.mjs'
import { isNumericBindingField } from './variable-binding.mjs'

const reject = message => { throw new Error(`Native variable history: ${message}`) }
const bindingFlag = key => /(^|:)boundVariables$/.test(key) || isNumericBindingField(key.slice(key.lastIndexOf(':') + 1))
const same = (a, b) => Object.keys(a).length === Object.keys(b).length &&
  Object.entries(a).every(([key, value]) => Object.hasOwn(b, key) && b[key] === value)
const bindingState = node => ({ parentId: node.parentId, componentId: node.componentId, type: node.type,
  bindings: { ...node.boundVariables }, flags: Object.fromEntries(Object.entries(node.overrides).filter(([key]) => bindingFlag(key))) })

// Reinsert only the removed identities before their next surviving neighbour.
// Existing entries, including newly authored ones, retain their relative order.
function restoredOrder(current, original, removed) {
  const present = new Set(current), pending = new Map()
  let anchor = null
  for (let index = original.length - 1; index >= 0; index--) {
    const id = original[index]
    if (present.has(id)) anchor = id
    else if (removed.has(id)) {
      if (!pending.has(anchor)) pending.set(anchor, [])
      pending.get(anchor).push(id)
    }
  }
  return [...current.flatMap(id => [...(pending.get(id)?.toReversed() ?? []), id]), ...(pending.get(null)?.toReversed() ?? [])]
}

// Only the pinned editor action supplies receipts, never imported file metadata.
// The ordinary editor undo entry owns this detached receipt. Every successful
// redo captures its current deletion effects, never reuses graph-owned objects.
// Native removal remains the implementation of unbinding and instance marking.
export function removeVariablesWithHistory(graph, id, wholeCollection) {
  const variable = wholeCollection ? null : graph.variables.get(id)
  const collection = graph.variableCollections.get(wholeCollection ? id : variable?.collectionId)
  if (!collection || (!wholeCollection && !variable)) reject('removal owner no longer exists')
  const receipt = { collection: structuredClone(collection), wholeCollection,
    variables: structuredClone(wholeCollection ? graph.getVariablesForCollection(id) : [variable]),
    variableOrder: [...graph.variables.keys()], collectionOrder: [...graph.variableCollections.keys()],
    activeOrder: [...graph.activeMode.keys()], activePresent: graph.activeMode.has(collection.id), active: graph.activeMode.get(collection.id) }
  const before = new Map([...graph.nodes].map(([nodeId, node]) => [nodeId, bindingState(node)]))
  if (wholeCollection) graph.removeCollection(id)
  else graph.removeVariable(id)
  receipt.nodes = []
  for (const [nodeId, previous] of before) {
    const node = graph.nodes.get(nodeId)
    if (!node) reject('native removal unexpectedly removed a node')
    const after = bindingState(node)
    if (!same(previous.bindings, after.bindings) || !same(previous.flags, after.flags)) {
      receipt.nodes.push({ id: nodeId, before: previous, after })
    }
  }
  return receipt
}

export function restoreRemovedVariables(graph, receipt) {
  const saved = receipt.collection, removed = new Set(receipt.variables.map(variable => variable.id))
  if (receipt.variables.some(variable => graph.variables.has(variable.id))) reject('restored variable identity is already in use')
  if (receipt.wholeCollection && (graph.variableCollections.has(saved.id) || graph.activeMode.has(saved.id))) {
    reject('restored collection identity is already in use')
  }
  const owner = receipt.wholeCollection ? structuredClone(saved) : graph.variableCollections.get(saved.id)
  if (!owner) reject('restoration collection no longer exists')
  const candidate = Object.create(graph)
  const values = new Map([...graph.variables, ...receipt.variables.map(variable => [variable.id, structuredClone(variable)])])
  candidate.variables = new Map(restoredOrder([...graph.variables.keys()], receipt.variableOrder, removed).map(id => [id, values.get(id)]))
  const nextOwner = { ...owner, variableIds: receipt.wholeCollection ? [...saved.variableIds] :
    restoredOrder(owner.variableIds, saved.variableIds, removed) }
  candidate.variableCollections = new Map(graph.variableCollections).set(saved.id, nextOwner)
  if (receipt.wholeCollection) candidate.variableCollections = new Map(restoredOrder([...graph.variableCollections.keys()],
    receipt.collectionOrder, new Set([saved.id])).map(id => [id, candidate.variableCollections.get(id)]))
  candidate.activeMode = new Map(graph.activeMode)
  if (receipt.wholeCollection && receipt.activePresent) candidate.activeMode.set(saved.id, receipt.active)
  validateNumericVariables(candidate)
  validateCSSColors(candidate)
  validateResolvedColors(candidate)

  // Reuse the native binding validator on detached nodes. It may mark instance
  // owners, so every node's binding and override maps are detached, not only the
  // leaves. No graph event or render can escape this preflight.
  candidate.nodes = new Map([...graph.nodes].map(([id, node]) => [id, { ...node,
    boundVariables: { ...node.boundVariables }, overrides: { ...node.overrides } }]))
  candidate.emitter = { emit() {} }
  for (const change of receipt.nodes) {
    const node = graph.nodes.get(change.id), expected = change.after
    if (!node || node.type !== expected.type || node.parentId !== expected.parentId || node.componentId !== expected.componentId ||
        !same(node.boundVariables, expected.bindings) || !same(bindingState(node).flags, expected.flags)) {
      reject('binding restoration conflicts with a later edit')
    }
    for (const [field, variableId] of Object.entries(change.before.bindings)) {
      if (expected.bindings[field] === variableId) continue
      try { candidate.bindVariable(change.id, field, variableId) }
      catch { reject('restored binding no longer fits its node') }
    }
  }

  // All owned values and flags are restored before the first notification.
  // Keep surviving graph/collection/node objects and unrelated overrides intact.
  graph.variables.clear()
  for (const [id, variable] of candidate.variables) graph.variables.set(id, variable)
  if (receipt.wholeCollection) {
    graph.variableCollections.clear()
    for (const [id, collection] of candidate.variableCollections) graph.variableCollections.set(id, collection)
    if (receipt.activePresent) {
      const active = new Map(graph.activeMode).set(saved.id, receipt.active)
      graph.activeMode.clear()
      for (const id of restoredOrder([...active.keys()].filter(id => id !== saved.id), receipt.activeOrder, new Set([saved.id]))) {
        graph.activeMode.set(id, active.get(id))
      }
    }
  } else owner.variableIds = nextOwner.variableIds
  for (const change of receipt.nodes) {
    const node = graph.nodes.get(change.id)
    node.boundVariables = { ...change.before.bindings }
    for (const key of Object.keys(node.overrides)) if (bindingFlag(key)) delete node.overrides[key]
    Object.assign(node.overrides, change.before.flags)
  }
  for (const change of receipt.nodes) {
    if (!same(change.before.bindings, change.after.bindings)) {
      graph.emitter.emit('node:updated', change.id, { boundVariables: { ...graph.nodes.get(change.id).boundVariables } })
    }
  }
}
