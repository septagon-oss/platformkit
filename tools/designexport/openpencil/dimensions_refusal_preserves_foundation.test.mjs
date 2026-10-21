import assert from 'node:assert/strict'
import { test } from 'node:test'
import { generateId } from '@open-pencil/scene-graph'
import { buildFoundation } from './foundation.mjs'
import { decodeSnapshot } from './source-tokens.mjs'
import { exportCore, sourceTokenFixture } from './browser/fixtures.test.mjs'

test('a dimensions refusal preserves a standing foundation and allows a later supported request', async t => {
  const run = await sourceTokenFixture(t)
  const selected = decodeSnapshot(Buffer.from(run({})))
  for (const mode of selected.snapshot.sourceTokens.modes) delete mode.dimensions
  const sourceBefore = structuredClone(selected)
  const standing = buildFoundation(selected.snapshot, selected)
  const state = graph => structuredClone({
    nodes: [...graph.getAllNodes()],
    variables: [...graph.variables],
    collections: [...graph.variableCollections],
  })
  const standingBefore = state(standing.graph)
  const complete = exportCore(['--tokens', 'both'])

  for (const mode of ['light', 'dark']) {
    const rejected = structuredClone(selected)
    const dimensions = complete.sourceTokens.modes.find(value => value.mode === mode).dimensions
    assert.ok(dimensions.length > 0)
    rejected.snapshot.sourceTokens.modes.find(value => value.mode === mode).dimensions = dimensions
    const rejectedBefore = structuredClone(rejected)
    for (let attempt = 0; attempt < 2; attempt++) {
      const previousID = Number(generateId().split(':')[1])
      assert.throws(() => buildFoundation(rejected.snapshot, rejected), /dimensions/)
      assert.equal(Number(generateId().split(':')[1]), previousID + 1)
      assert.deepEqual(rejected, rejectedBefore)
      assert.deepEqual(state(standing.graph), standingBefore)
    }
  }

  const later = buildFoundation(selected.snapshot, selected)
  assert.equal(later.graph.variables.size, standing.graph.variables.size)
  assert.deepEqual(later.collection.modes.map(value => value.name), ['light', 'dark'])
  assert.notEqual(later.graph, standing.graph)
  assert.deepEqual(state(standing.graph), standingBefore)
  assert.deepEqual(selected, sourceBefore)
})
