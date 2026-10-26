import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { signInForContent } from './steps/content';
import { editingSurface } from './steps/richtext';
import { openHistoryPanel, restoreVersion, storedVersions } from './steps/history';

// A record's past, walked in the browser.
//
// The version series is the one part of a record the row cannot answer: the row
// says what the record is, and only the series says what it used to be. Two
// journeys matter, and both are a person's rather than an endpoint's. The first is
// the one that ends with a write: read what an edit moved — the words it added
// marked as added, the words it dropped marked as dropped — then bring the older
// words back, which writes a new version and rewrites nothing. The second is the
// one this feature exists to refuse: a page read at one write count, a second
// person editing underneath it, and a restore that arrives quoting a count the
// record has left behind. It must be refused, and the edit it would have
// overwritten must still be standing.
//
// A browser and not a fixture, because the parts at risk here are the ones a Go
// test cannot see: that the disclosure opens, that the marks survive into the
// painted page, that the question the restore asks is a dialog a person can answer,
// and that none of it is invisible to a screen reader.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

/**
 * Types a body into the form as a person does. The content form mounts a rich-text
 * editor over the `body` textarea and keeps that textarea hidden, so the words only
 * reach the record through the editing surface: filling the hidden control works on a
 * page whose editor module was refused, which is not the page opened here.
 */
async function writeBody(page: Page, text: string) {
  await page.locator(editingSurface).click();
  await page.keyboard.press('Control+A');
  await page.keyboard.type(text);
}

/** One page, one first write. Returns its id and the address its detail is served at. */
async function createPage(page: Page, body: string) {
  await page.goto('/app/content/contents/new');
  await page.getByLabel('Address name').fill(`history-${Date.now()}`);
  await page.getByLabel('Title').fill('Pool hours');
  await page.getByLabel('Type').selectOption('page');
  await writeBody(page, body);
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/app\/content\/contents\/[0-9a-f-]{36}$/);
  const id = page.url().split('/').at(-1)!;
  return { id, detail: `/app/content/contents/${id}` };
}

test('the version panel shows what an edit moved and brings the older words back', async ({ page }) => {
  await signInForContent(page, email, password);
  const { id, detail } = await createPage(page, 'The pool opens at nine on weekdays.');

  await page.goto(`${detail}/edit`);
  await writeBody(page, 'The pool opens at ten on weekdays.');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.locator('[data-component="prose"]')).toContainText('opens at ten');

  const panel = await openHistoryPanel(page);
  // The comparison, not two values side by side: what the write put in, and what it
  // took out, each in the markup that means it.
  await expect(panel.locator('ins')).toContainText('ten');
  await expect(panel.locator('del')).toContainText('nine');
  // Every version says when it was written, and the number a person would quote.
  await expect(panel).toContainText(/v2 · /);

  // Bring v1 back. The panel asks first, and the answer is the sentence that says what the write
  // did — a new version, not an overwrite — with one door back to the record beside it. A person
  // who restored reads that sentence before they read the page again.
  const restored = await restoreVersion(page, 1);
  expect(restored.status(), 'the restore was accepted').toBeLessThan(400);
  await expect(page.getByText(/new version was written|nova vers[ãa]o/i)).toBeVisible();
  await page.getByRole('link', { name: /^(Continue|Continuar)$/ }).click();
  await expect(page).toHaveURL(new RegExp(`^http://[^/]+${detail}$`));
  await expect(page.locator('[data-component="prose"]')).toContainText('opens at nine');

  const series = await storedVersions(page, id);
  expect(series.map(v => v.version)).toEqual([3, 2, 1]);
  expect(series[0].reason).toBe('restore:1');
  expect(series[0].changed).toEqual(['body']);
  // The two older versions are exactly what they were: a restore adds to a series,
  // it never edits one.
  expect(series[1].fields.body).toContain('ten');
  expect(series[2].fields.body).toContain('nine');

  // And the panel says so: the entry the restore wrote names the version it brought
  // back, so the series reads as a sequence and not as three anonymous saves. The
  // person came back to the record by the door the answer gave them, and opens the
  // panel there.
  const reopened = await openHistoryPanel(page);
  await expect(reopened.getByText(/v3/).first()).toBeVisible();
  await expect(reopened).toContainText(/Restored from v1|restaurada desde a vers[ãa]o 1|from v1/i);
});

test('a restore that quotes a write the record has left behind is refused and writes nothing', async ({ page }) => {
  await signInForContent(page, email, password);
  const { id, detail } = await createPage(page, 'The pool opens at nine on weekdays.');
  await page.goto(`${detail}/edit`);
  await writeBody(page, 'The pool opens at ten on weekdays.');
  await page.getByRole('button', { name: 'Save' }).click();

  const panel = await openHistoryPanel(page);
  await expect(panel.locator('ins')).toContainText('ten');
  const concurrent = await page.request.patch(`/api/v1/content/contents/${id}`, {
    data: { body: 'The pool opens at eleven on weekdays.' },
  });
  expect(concurrent.status(), await concurrent.text()).toBe(200);
  const afterEdit = (await storedVersions(page, id)).map(v => v.version);

  // The decision they had already formed is submitted against the page they read.
  const refusal = await restoreVersion(page, 1);
  expect(refusal.status(), await refusal.text()).toBe(412);

  // Nothing moved: the concurrent text stands, no version was added, and the series
  // is the one the edit left behind.
  const after = await storedVersions(page, id);
  expect(after.map(v => v.version)).toEqual(afterEdit);
  expect(after[0].fields.body).toContain('eleven');
});

test('axe finds nothing on a record with a version series open', async ({ page }) => {
  await signInForContent(page, email, password);
  const { detail } = await createPage(page, 'The pool opens at nine on weekdays.');
  await page.goto(`${detail}/edit`);
  await writeBody(page, 'The pool opens at ten on weekdays.');
  await page.getByRole('button', { name: 'Save' }).click();
  // Saving lands on the record, which is the page under test; a goto here would be
  // a second navigation over the one the form already made.
  await expect(page).toHaveURL(new RegExp(detail));
  await openHistoryPanel(page);
  for (const width of [390, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
    expect(results.violations, `axe on the version panel at ${width}px`).toEqual([]);
  }
});
