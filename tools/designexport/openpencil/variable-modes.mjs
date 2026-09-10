import { validateCSSColors, validateResolvedColors } from './variable-color.mjs'
import { validateNumericVariables } from './variable-number.mjs'

const reject = message => { throw new Error(`Native variable mode: ${message}`) }
const draftOwner = Symbol('detached variable mode owner')

function validateCollection(collection) {
  const modes = new Set(collection.modes.map(mode => mode.modeId))
  if (!modes.size || modes.size !== collection.modes.length || !modes.has(collection.defaultModeId) ||
      collection.modes.some(mode => typeof mode.modeId !== 'string' || !mode.modeId || typeof mode.name !== 'string')) {
    reject('unique named modes and an owned default are required')
  }
}

// The native wire order selects the default; sortPosition independently owns
// the visible column order. Neither needs another plugin metadata envelope.
export function exportModeOrder(collection) {
  validateCollection(collection)
  const modes = collection.modes.map((mode, index) => ({ mode: { ...mode }, index }))
  return [modes.find(({ mode }) => mode.modeId === collection.defaultModeId),
    ...modes.filter(({ mode }) => mode.modeId !== collection.defaultModeId)]
}

export function importModeOrder(modes) {
  const positioned = modes.filter(mode => mode.sortPosition !== undefined)
  if (!positioned.length) return [...modes]
  if (positioned.length !== modes.length || positioned.some(mode => typeof mode.sortPosition !== 'string' ||
      !mode.sortPosition.length || mode.sortPosition.length > 1024) ||
      new Set(positioned.map(mode => mode.sortPosition)).size !== modes.length) reject('ambiguous native mode order')
  return modes.toSorted((a, b) => a.sortPosition < b.sortPosition ? -1 : 1)
}

// Only the pinned mode operations call this boundary. They run unchanged on a
// detached owner, including a complete undo restoration, before any live write.
// Nested calls belong to that same draft; no invalid intermediate step is
// validated or exposed as a live graph, render event or history entry.
export function changeVariableModes(graph, collectionId, change) {
  if (graph[draftOwner] !== undefined) {
    if (graph[draftOwner] !== collectionId) reject('a mode edit must have one collection owner')
    return change(graph)
  }
  const collection = graph.variableCollections.get(collectionId)
  if (!collection) return
  const candidate = Object.create(graph), next = structuredClone(collection)
  candidate[draftOwner] = collectionId
  candidate.variableCollections = new Map(graph.variableCollections).set(collectionId, next)
  candidate.variables = new Map(graph.variables)
  candidate.activeMode = new Map(graph.activeMode)
  for (const id of collection.variableIds) {
    const variable = graph.variables.get(id)
    if (variable) candidate.variables.set(id, { ...variable, valuesByMode: structuredClone(variable.valuesByMode) })
  }
  const result = change(candidate)
  validateCollection(next)
  validateNumericVariables(candidate)
  validateCSSColors(candidate)
  validateResolvedColors(candidate)
  collection.modes = next.modes
  collection.defaultModeId = next.defaultModeId
  for (const id of collection.variableIds) {
    const variable = graph.variables.get(id)
    if (variable) variable.valuesByMode = candidate.variables.get(id).valuesByMode
  }
  if (candidate.activeMode.has(collectionId)) graph.activeMode.set(collectionId, candidate.activeMode.get(collectionId))
  else graph.activeMode.delete(collectionId)
  return result
}

export function captureModeValues(graph, collectionId, modeId) {
  return new Map(graph.getVariablesForCollection(collectionId).map(variable => [variable.id, {
    present: Object.hasOwn(variable.valuesByMode, modeId), value: structuredClone(variable.valuesByMode[modeId]),
  }]))
}

export function restoreModeValues(graph, collectionId, modeId, values) {
  for (const variable of graph.getVariablesForCollection(collectionId)) {
    const previous = values.get(variable.id)
    if (previous?.present) variable.valuesByMode[modeId] = structuredClone(previous.value)
    else delete variable.valuesByMode[modeId]
  }
}

export function restoreDefaultMode(graph, collectionId, expected, target) {
  if (graph.variableCollections.get(collectionId)?.defaultModeId !== expected) reject('default mode history is stale')
  graph.setDefaultMode(collectionId, target)
}
