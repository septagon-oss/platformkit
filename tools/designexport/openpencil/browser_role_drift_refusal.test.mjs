import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

test('browser role pin refuses an expectation that drifts from the exported stylesheet', () => {
  const source = readFileSync(new URL('./browser_role_expectations.test.mjs', import.meta.url), 'utf8')
  const run = code => spawnSync(process.execPath,
    ['--import', './register.mjs', '--input-type=module', '--eval', code], {
      cwd: new URL('./', import.meta.url), encoding: 'utf8', timeout: 120_000,
    })
  const healthy = run(source)
  assert.ifError(healthy.error)
  assert.equal(healthy.status, 0, healthy.stdout + healthy.stderr)

  const binding = 'const [, name, expression] = expectation'
  assert.ok(source.includes(binding), 'the pin reads the browser expectation')
  const drifted = run(source.replace(binding,
    "const [, name] = expectation; const expression = 'var(--pk-role-fg-primary)'"))
  assert.ifError(drifted.error)
  assert.equal(drifted.status, 1, drifted.stdout + drifted.stderr)
  assert.match(drifted.stdout + drifted.stderr,
    /the browser expectation follows the emitted source expression/)
})
