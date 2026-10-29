import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

// The four dependencies the audit gate pointed at. Three are transitive, reached through a package
// this adapter does not depend on directly: @open-pencil/core → jspdf → dompurify@^3.3.1, whose lock
// carried 3.4.14, and unifont → css-tree → source-map-js@^1.2.1 and unifont → undici, whose locks
// carried 1.2.1 and 8.10.2. The fourth is a peer — @ai-sdk/provider-utils declares zod
// ^3.25.76 || ^4.1.8, so `npm ci` installs it at the top of the tree and `--omit=dev` audits it,
// because omitting dev-dependencies does not omit a peer. Every parent still accepts the vulnerable
// range, so the override in package.json is what keeps `npm ci` out of it, and this check is what
// keeps an override from drifting quietly: the lock resolves each name to the release named here.
const reviewed = { dompurify: '3.4.17', 'source-map-js': '1.2.2', undici: '8.12.3', zod: '4.6.7' }

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
