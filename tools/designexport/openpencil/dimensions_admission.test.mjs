import assert from 'node:assert/strict'
import { test } from 'node:test'
import { generateId } from '@open-pencil/scene-graph'
import { buildFoundation } from './foundation.mjs'
import { decodeSnapshot } from './source-tokens.mjs'
import { exportCore, sourceTokenFixture } from './browser/fixtures.test.mjs'

test('unsupported dimensions in either mode refuse before allocation and leave the selected source intact', async t => {
  const run = await sourceTokenFixture(t)
  const selected = decodeSnapshot(Buffer.from(run({})))
  const complete = exportCore(['--tokens', 'both'])
  const baseline = buildFoundation(selected.snapshot, selected)
  assert.equal(baseline.graph.variables.size, 6)

  for (const mode of ['light', 'dark']) {
    const dimensions = complete.sourceTokens.modes.find(value => value.mode === mode).dimensions
    assert.ok(dimensions.length > 0, 'the real exporter supplies shape dimensions')
    for (const value of [dimensions, null, {}]) {
      const input = structuredClone(selected)
      input.snapshot.sourceTokens.modes.find(value => value.mode === mode).dimensions = value
      const before = structuredClone(input)
      const previousID = Number(generateId().split(':')[1])
      assert.throws(() => buildFoundation(input.snapshot, input), /dimensions/)
      assert.equal(Number(generateId().split(':')[1]), previousID + 1,
        'refusal must precede native graph allocation, including a refusal in the second mode')
      assert.deepEqual(input, before)
    }

    const supported = structuredClone(selected)
    supported.snapshot.sourceTokens.modes.find(value => value.mode === mode).dimensions = []
    const result = buildFoundation(supported.snapshot, supported)
    assert.equal(result.graph.variables.size, baseline.graph.variables.size)
    assert.deepEqual(result.collection.modes.map(value => value.name), ['light', 'dark'])
  }
})
