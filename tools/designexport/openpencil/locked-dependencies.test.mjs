import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

// The two transitive dependencies the audit gate pointed at, each reached through
// a package this adapter does not depend on directly: @open-pencil/core → jspdf →
// dompurify@^3.3.1, whose lock carried 3.4.14, and unifont → css-tree →
// source-map-js@^1.2.1, whose lock carried 1.2.1. Both parents still accept the
// vulnerable range, so the override in package.json is what keeps `npm ci` out of
// it, and this check is what keeps that override from drifting quietly: the lock
// has to resolve each name to the reviewed release named here, and to the very
// release package.json overrides it to.
const reviewed = { dompurify: '3.4.16', 'source-map-js': '1.2.2' }

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
