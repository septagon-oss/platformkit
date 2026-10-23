import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { copyFileSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

test('the dependency pin refuses a reported DOMPurify release even when its override and lock agree', t => {
  const directory = mkdtempSync(join(tmpdir(), 'platformkit-dependency-rollback-'))
  t.after(() => rmSync(directory, { recursive: true, force: true }))
  copyFileSync(new URL('./locked-dependencies.test.mjs', import.meta.url), join(directory, 'locked-dependencies.test.mjs'))
  const manifest = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8'))
  const lock = JSON.parse(readFileSync(new URL('./package-lock.json', import.meta.url), 'utf8'))
  const run = () => {
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

  const current = run()
  assert.equal(current.status, 0, current.stdout + current.stderr)

  // The last reported release must remain refused when both installer inputs move together.
  Object.assign(lock.packages['node_modules/dompurify'], {
    version: '3.4.16',
    resolved: 'https://registry.npmjs.org/dompurify/-/dompurify-3.4.16.tgz',
    integrity: 'sha512-sqo+pNp3qRhCIpbgRi1y8Tgk27Bo2Ry7w0dC1NBeNTdZChWjz9Xb/KOoZbRP/R6pQZ80Qw8YhXw13hWWBbMRnQ==',
  })
  const lockOnly = run()
  assert.equal(lockOnly.status, 1, lockOnly.stdout + lockOnly.stderr)
  assert.match(lockOnly.stdout, /ERR_ASSERTION/)

  manifest.overrides.dompurify = '3.4.16'
  const both = run()
  assert.equal(both.status, 1, both.stdout + both.stderr)
  assert.match(both.stdout, /ERR_ASSERTION/)
})
