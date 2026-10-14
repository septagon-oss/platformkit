import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { signInForContent } from './steps/content';

// Every visible string of the editor comes from the UI catalogues, in the
// request's language (decision 0012; the brief's fifth scope item). The toolbar
// is drawn by the server from the catalogue; the sentences the glue writes at
// runtime — the counter is the first one a person sees — must come from the same
// catalogue, so on a Portuguese page the counter's words are Portuguese words the
// catalogue carries.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

const editable = '[data-richtext-surface] [contenteditable="true"]';

function portugueseCatalogue(): string {
  const table = JSON.parse(readFileSync(join(__dirname, '..', 'ui', 'forms', 'messages', 'pt-PT.json'), 'utf8'));
  return Object.values(table as Record<string, { translation?: string }>)
    .map((entry) => entry.translation ?? '')
    .join('\n')
    .toLowerCase();
}

test('the editor on a Portuguese page counts in the words of the Portuguese catalogue', async ({ page }) => {
  await signInForContent(page, email, password);
  await page.goto('/app/content/contents/new?lang=pt-PT');
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
  await expect(page.locator(editable)).toBeVisible();
  await expect(page.getByRole('toolbar', { name: 'Formatação' })).toBeVisible();
  const counter = page.locator('[data-richtext-count]');
  await expect(counter).toBeVisible();
  const words = (await counter.innerText()).replace(/[\d·/]/g, ' ').trim().split(/\s+/).filter(Boolean);
  expect(words.length, 'the counter says something beside its numbers').toBeGreaterThan(0);
  const catalogue = portugueseCatalogue();
  for (const word of words) {
    expect(catalogue, `"${word}" in the counter is not a word of the Portuguese catalogue`).toContain(word.toLowerCase());
  }
});
