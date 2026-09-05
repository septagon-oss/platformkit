import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, resolve } from 'node:path'
import { createHash } from 'node:crypto'
import { corrections, correctSource } from './corrections.mjs'

const require = createRequire(import.meta.url)

test('every correction matches the locked SDK and leaves installed source unchanged', () => {
  for (const [name, correction] of Object.entries(corrections)) {
    const parts = name.split('/')
    const entry = require.resolve(parts.slice(0, 2).join('/'))
    const path = resolve(dirname(entry), '..', ...parts.slice(2))
    const original = readFileSync(path, 'utf8')
    assert.equal(createHash('sha256').update(original).digest('hex'), correction.sha256)
    const transformed = correctSource(path, original)
    assert.notEqual(transformed, original)
    assert.equal(readFileSync(path, 'utf8'), original)
    assert.throws(() => correctSource(path, original + '\n'), /source mismatch/)
    assert.throws(() => correctSource(path, transformed), /source mismatch/)
  }
})

test('unrelated modules are not rewritten even when their contents resemble the SDK', () => {
  assert.equal(correctSource('/app/my-node-change2.js', 'function serializeTextOverrides() {}'), null)
  assert.equal(correctSource('/app/not-@open-pencil/fig/dist/node-change2.js', ''), null)
})
