import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';

// Exercise the journey's own expression in JavaScript, including the reset
// address that must remain readable when confirmation moves to its public face.
test('the mailed-link reader preserves both account paths and their credentials', () => {
  const source = readFileSync(join(__dirname, 'mailed-links.spec.ts'), 'utf8');
  const expression = source.match(/^const linkPattern = \/(.*)\/g;$/m);
  expect(expression, 'the journey exposes its complete link expression').not.toBeNull();
  const reader = new RegExp(expression![1], 'g');

  for (const origin of ['http://localhost:43123', 'https://accounts.example.test']) {
    for (const path of ['/auth/verify-email', '/app/auth/reset']) {
      const credential = 'Abc_123-xyz%3D';
      const link = `${origin}${path}?token=${credential}`;
      const found = `Follow this link:\n${link}\nThank you.`.match(reader);
      expect(found).toEqual([link]);
      expect(new URL(found![0]).pathname).toBe(path);
      expect(new URL(found![0]).searchParams.get('token')).toBe('Abc_123-xyz=');
    }
  }
});
