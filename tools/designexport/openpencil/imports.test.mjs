import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'

test('observed scoped import boundary excludes audited expression and presentation packages', () => {
  const probe = String.raw`
    import { registerHooks } from 'node:module'
    registerHooks({
      load(url, context, nextLoad) {
        if (/\/node_modules\/(?:expr-eval|pptxgenjs|image-size)(?:\/|$)/.test(url)) {
          throw new Error('Unexpected audited dependency load: ' + url)
        }
        return nextLoad(url, context)
      },
    })
    await import('./register.mjs')
    for (const entry of [
      '@open-pencil/core/editor',
      '@open-pencil/core/io/formats/fig',
      '@open-pencil/core/layout',
      '@open-pencil/fig',
      '@open-pencil/scene-graph',
    ]) await import(entry)
  `
  const result = spawnSync(process.execPath, ['--input-type=module', '--eval', probe], {
    cwd: fileURLToPath(new URL('.', import.meta.url)),
    env: { ...process.env, NODE_OPTIONS: '' },
    encoding: 'utf8',
    timeout: 30_000,
  })
  assert.ifError(result.error)
  assert.equal(result.status, 0, result.stderr || result.stdout || `signal: ${result.signal}`)
})
