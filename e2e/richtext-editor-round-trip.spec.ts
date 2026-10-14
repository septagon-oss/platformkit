import { expect, test, type Page } from '@playwright/test';
import { signInForContent } from './steps/content';

// Decision 0069 §1's round trip, driven through the real editor: a stored document
// opened in the edit form and saved untouched is the same document afterwards —
// the same stored row for every construct whose spelling Normalise canonicalises,
// and the same rendered prose for the one it does not (a table's padding). The
// server is the oracle on both sides: the comparison is between two rows the
// kernel wrote and two pages it rendered, never between the editor and a string
// this file invents.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

const editable = '[data-richtext-surface] [contenteditable="true"]';

const prose = [
  '## Opening hours',
  '',
  'We are **open** on *Monday*, ~~Sunday~~ and `holidays`.  ',
  'A second line after a hard break.',
  '',
  '- Monday',
  '- Tuesday',
  '  - Early',
  '  - Late',
  '',
  '1. First',
  '2. Second',
  '',
  '- [ ] Task open',
  '- [x] Task done',
  '',
  '> A quote',
  '',
  '```go',
  'fmt.Println(1)',
  '```',
  '',
  '---',
  '',
  '[Website](https://example.com "Our site")',
  '',
  '### Smaller',
  '',
  '#### Smallest',
  '',
].join('\n');

const table = [
  '## Hours',
  '',
  '| Day | Hours |',
  '| --- | --- |',
  '| Mon | 9-5 |',
  '',
].join('\n');

async function stored(page: Page, id: string) {
  const read = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(read.status(), await read.text()).toBe(200);
  return (await read.json()).body as string;
}

async function rendered(page: Page, id: string) {
  await page.goto(`/app/content/contents/${id}`);
  return page.locator('[data-component="prose"]').innerHTML();
}

// roundTrip creates the record, reads what the server stored and rendered, saves
// the edit form untouched through the editor, and reads both again.
async function roundTrip(page: Page, slug: string, body: string) {
  const created = await page.request.post('/api/v1/content/contents', {
    data: { slug, title: 'Round trip', kind: 'page', body },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id } = await created.json();
  const before = { body: await stored(page, id), html: await rendered(page, id) };
  await page.goto(`/app/content/contents/${id}/edit`);
  await expect(page.locator(editable)).toBeVisible();
  const posted = page.waitForRequest((request) => request.method() === 'POST' && request.url().includes(`/contents/${id}`));
  await page.getByRole('button', { name: 'Save' }).click();
  const form = new URLSearchParams((await posted).postData() ?? '');
  await expect(page).toHaveURL(new RegExp(`/app/content/contents/${id}$`));
  const after = { body: await stored(page, id), html: await rendered(page, id) };
  return { before, posted: form.get('body') ?? '', after };
}

test.beforeEach(async ({ page }) => {
  await signInForContent(page, email, password);
});

test('every block and mark of §1 except tables survives an untouched save as the same stored row', async ({ page }) => {
  const { before, posted, after } = await roundTrip(page, `round-${Date.now()}`, prose);
  expect(posted.trimEnd(), 'what the editor writes back is what the server stored, up to the final newline').toBe(before.body.trimEnd());
  expect(after.body, 'the row is unchanged by an untouched save').toBe(before.body);
  expect(after.html, 'the rendered prose is unchanged by an untouched save').toBe(before.html);
});

test('a table survives an untouched save as the same rendered table', async ({ page }) => {
  const { before, after } = await roundTrip(page, `table-${Date.now()}`, table);
  expect(after.html, 'the rendered table is unchanged by an untouched save').toBe(before.html);
});
