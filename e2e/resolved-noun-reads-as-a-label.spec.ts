import { expect, test } from '@playwright/test';
import { signIn } from './steps/kernel';

// The collection name a person reads is the resolved policy's own word, and the policy holds it as
// the module wrote it: `modules/task/module.go` declares the plural `tasks`. Two readings of that
// one word appear on one screen. Where the word stands alone — the list's heading, the name of its
// table region, the collection entry in a record's trail — it begins a line, so the line starts with
// a capital. Where it sits inside a sentence, the result count, the sentence is already under way
// and the noun stays exactly as its author wrote it.
//
// Only an exact read of the served text tells those readings apart. `getByRole({name})` matches a
// name case-insensitively, so a journey addressed that way passes whether the page says `Tasks` or
// `tasks`, and a renderer that capitalised the count as well — or capitalised none of them — would
// satisfy it. Every assertion here compares the text or the attribute itself, case-sensitively.

const tasks = '/app/task/tasks';

test('a resolved noun starts a line with its own capital and stays the noun inside a sentence', async ({ page }) => {
  await signIn(page, tasks);
  // Two records of this run's own, so the result count is plural. Written over the same session the
  // page carries: the claim below is about the words a screen shows, not about the form that writes.
  for (const title of ['The shape that reads first', 'The shape that reads second']) {
    const created = await page.request.post('/api/v1/task/tasks', { data: { title } });
    expect(created.status(), await created.text()).toBe(201);
  }

  await page.goto(tasks);
  await expect(page.locator('main h1')).toHaveText('Tasks');
  await expect(page.locator('[data-component="data-list"]')).toHaveAttribute('aria-label', 'Tasks');

  // The result count is the one line on this page that holds the noun inside a sentence. The number
  // is whatever the shared test tenant holds at this moment, so the count's text is read rather than
  // predicted; what is pinned is the noun after it — the policy's own word, with no capital added.
  const count = page.locator('[data-component="data-list"] > p');
  await expect(count).toHaveCount(1);
  await expect(count).toHaveText(/^\d+ tasks$/);

  // The record's trail names the set the person came out of, and it is a line of its own.
  const record = await page.locator('[data-component="data-list"] table tbody tr a').first().getAttribute('href');
  expect(record, 'the list offers no record to open').toBeTruthy();
  await page.goto(record!);
  const trail = page.locator('[data-component="breadcrumb"] a');
  await expect(trail).toHaveCount(2);
  await expect(trail.nth(1)).toHaveText('Tasks');
});
