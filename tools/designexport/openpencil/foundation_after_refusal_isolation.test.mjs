import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createEditor } from '@open-pencil/core/editor'
import { buildFoundation } from './foundation.mjs'
import { decodeSnapshot } from './source-tokens.mjs'
import { exportCore, sourceTokenFixture } from './browser/fixtures.test.mjs'

test('editing a foundation after a dimensions refusal preserves the standing graph and source', async t => {
  const run = await sourceTokenFixture(t)
  const input = decodeSnapshot(Buffer.from(run({})))
  const sourceBefore = structuredClone(input)
  const standing = buildFoundation(input.snapshot, input)
  const state = graph => structuredClone({
    nodes: [...graph.getAllNodes()],
    variables: [...graph.variables],
    collections: [...graph.variableCollections],
  })
  const standingBefore = state(standing.graph)
  const complete = exportCore(['--tokens', 'both'])

  for (const mode of ['light', 'dark']) {
    const rejected = structuredClone(input)
    const dimensions = complete.sourceTokens.modes.find(value => value.mode === mode).dimensions
    assert.ok(dimensions.length > 0)
    rejected.snapshot.sourceTokens.modes.find(value => value.mode === mode).dimensions = dimensions
    assert.throws(() => buildFoundation(rejected.snapshot, rejected), /dimensions/)

    const later = buildFoundation(input.snapshot, input)
    const laterBefore = state(later.graph)
    const spacing = [...later.graph.variables.values()].find(value => value.name === 'spacing/1')
    assert.ok(spacing)
    const modeID = later.collection.modes.find(value => value.name === mode).modeId
    const editor = createEditor({ graph: later.graph })
    editor.updateVariableValue(spacing.id, modeID, 0.75)
    assert.equal(later.graph.resolveVariable(spacing.id, modeID), 0.75)
    spacing.sourceToken.decimal = '0.7500'
    assert.notDeepEqual(state(later.graph), laterBefore)
    assert.deepEqual(state(standing.graph), standingBefore)
    assert.deepEqual(input, sourceBefore)
  }
})
