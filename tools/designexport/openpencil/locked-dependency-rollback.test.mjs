import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { copyFileSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

// Every release the dependency pin must keep refusing: the last version npm reports for each
// transitive dependency the override in package.json holds above its advisory. A reported release
// stays refused when both installer inputs move together, which is what a person pulling the lock
// by hand (or a fixture copied from another branch) can produce.
const REPORTED_RELEASES = [
  {
    name: 'dompurify',
    version: '3.4.16',
    resolved: 'https://registry.npmjs.org/dompurify/-/dompurify-3.4.16.tgz',
    integrity: 'sha512-sqo+pNp3qRhCIpbgRi1y8Tgk27Bo2Ry7w0dC1NBeNTdZChWjz9Xb/KOoZbRP/R6pQZ80Qw8YhXw13hWWBbMRnQ==',
  },
  {
    name: 'zod',
    version: '4.6.5',
    resolved: 'https://registry.npmjs.org/zod/-/zod-4.6.5.tgz',
    integrity: 'sha512-v5l/aFXZQeai4awLbOpSoHecE9UiMrnfx75tEXLjNonXVARxQ5mOeipTjROUchszUNCqnE+hqAMujRsRHsut2Q==',
  },
  {
    name: 'undici',
    version: '8.12.1',
    resolved: 'https://registry.npmjs.org/undici/-/undici-8.12.1.tgz',
    integrity: 'sha512-usz3OVQBeh1LxNn0DzU0voLXMgn5CU/WUdpBsY6Ti5afNao+vTkmn4H/NxoXolc10cOqe2+YIG0Hspq2rsl64w==',
  },
]

test('the dependency pin refuses a reported release even when its override and lock agree', t => {
  const directory = mkdtempSync(join(tmpdir(), 'platformkit-dependency-rollback-'))
  t.after(() => rmSync(directory, { recursive: true, force: true }))
  copyFileSync(new URL('./locked-dependencies.test.mjs', import.meta.url), join(directory, 'locked-dependencies.test.mjs'))
  const pristine = {
    manifest: JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8')),
    lock: JSON.parse(readFileSync(new URL('./package-lock.json', import.meta.url), 'utf8')),
  }
  const run = (manifest, lock) => {
    writeFileSync(join(directory, 'package.json'), JSON.stringify(manifest))
    writeFileSync(join(directory, 'package-lock.json'), JSON.stringify(lock))
    const result = spawnSync(process.execPath, ['--test', 'locked-dependencies.test.mjs'], {
      cwd: directory, encoding: 'utf8', timeout: 30_000, stdio: ['ignore', 'pipe', 'pipe'],
      env: { ...process.env, NODE_TEST_CONTEXT: undefined },
    })
    assert.equal(result.error, undefined, result.stderr)
    assert.equal(result.signal, null, result.stderr)
    return result
  }

  const current = run(pristine.manifest, pristine.lock)
  assert.equal(current.status, 0, current.stdout + current.stderr)

  for (const reported of REPORTED_RELEASES) {
    const manifest = structuredClone(pristine.manifest)
    const lock = structuredClone(pristine.lock)

    // The last reported release must remain refused when both installer inputs move together.
    Object.assign(lock.packages[`node_modules/${reported.name}`], {
      version: reported.version,
      resolved: reported.resolved,
      integrity: reported.integrity,
    })
    const lockOnly = run(manifest, lock)
    assert.equal(lockOnly.status, 1, `${reported.name}: ${lockOnly.stdout}${lockOnly.stderr}`)
    assert.match(lockOnly.stdout, /ERR_ASSERTION/)

    manifest.overrides[reported.name] = reported.version
    const both = run(manifest, lock)
    assert.equal(both.status, 1, `${reported.name}: ${both.stdout}${both.stderr}`)
    assert.match(both.stdout, /ERR_ASSERTION/)
  }
})
