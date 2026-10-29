import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (name) => JSON.parse(readFileSync(new URL(name, import.meta.url), 'utf8'));

test('the lock resolves undici and zod to the releases package.json pins', () => {
  const pins = read('./package.json').overrides ?? read('./package.json').dependencies;
  const lock = read('./package-lock.json').packages;
  for (const name of ['undici', 'zod']) {
    assert.ok(pins[name], `${name} is pinned`);
    assert.equal(lock[`node_modules/${name}`].version, pins[name]);
  }
});
