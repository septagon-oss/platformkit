import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

// Parent ranges still accept vulnerable releases of these transitive dependencies.
// Exact overrides keep a regenerated lock on the patched releases verified here.
// The tooling README records the dependency paths and advisories.
const reviewed = { dompurify: '3.4.17', 'source-map-js': '1.2.2', undici: '8.12.2', zod: '4.6.6' }

const manifest = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8'))
const lock = JSON.parse(readFileSync(new URL('./package-lock.json', import.meta.url), 'utf8'))

test('the audited transitive dependencies resolve to their reviewed releases in the lock', () => {
  for (const [name, version] of Object.entries(reviewed)) {
    assert.equal(manifest.overrides[name], version)
    const entry = lock.packages[`node_modules/${name}`]
    assert.equal(entry?.version, version, name)
    assert.equal(entry?.resolved, `https://registry.npmjs.org/${name}/-/${name}-${version}.tgz`, name)
    assert.ok(entry?.integrity?.startsWith('sha512-'), name)
  }
})
