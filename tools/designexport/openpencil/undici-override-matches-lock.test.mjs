import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import assert from 'node:assert/strict'

const manifest = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8'))
const lock = JSON.parse(readFileSync(new URL('./package-lock.json', import.meta.url), 'utf8'))

test('every locked undici copy is the version the manifest overrides to', () => {
  const want = manifest.overrides.undici
  const copies = Object.entries(lock.packages).filter(([path]) => path.endsWith('node_modules/undici'))
  assert.ok(copies.length > 0, 'the lock holds at least one undici')
  for (const [path, entry] of copies) assert.equal(entry.version, want, path)
})
