import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { exportCore } from './browser/fixtures.test.mjs'

test('browser secondary text expectations name the role the source stylesheet emits', () => {
  const snapshot = exportCore(['--example', 'pk-ui.component.text/muted', '--props'], { color: 'secondary' })
  const browserCases = readFileSync(new URL('./browser/components.test.mjs', import.meta.url), 'utf8')
  const expectation = browserCases.match(/customProperty: '(--pk-role-fg-secondary)',\s*value: '([^']+)'/)
  assert.ok(expectation, 'the browser case declares its secondary text source expression')
  const [, name, expression] = expectation
  const declarations = [...snapshot.css.matchAll(/(--pk-role-fg-secondary)\s*:\s*([^;{}]+);/g)]
  assert.ok(declarations.length > 0, 'the real exporter supplies the secondary text role')
  for (const [, property, value] of declarations) {
    assert.equal(property, name)
    assert.equal(value.trim(), expression, 'the browser expectation follows the emitted source expression')
  }
})
