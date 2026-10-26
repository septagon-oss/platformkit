import { expect, test } from '@playwright/test';
import { signIn } from './steps/kernel';

// Three pages read the one word `ui/resource` resolves: a list takes it as its heading, a record as
// the collection entry of its trail, and a form — the create page and the edit page alike — as that
// same entry. `ui/resource/resource.go` spells the rule once, in `collectionName`, but a journey
// that walked only the list and the record would let the third answer to nothing: the form's trail
// is drawn at its own call site, and a change there is invisible to a case that never opens a form.
//
// The capital is the page's, not the string's, so both languages the reference application speaks
// are walked: `modules/task` declares the plural `tasks` and its pt-PT catalogue spells the same
// hint `tarefas`, each lower-case, each the author's word, and only the letter that begins the line
// comes from the page. A browser name match ignores case, so every assertion below compares the
// served text itself.

const tasks = '/app/task/tasks';

test("the noun that begins a page's trail begins the trail of a form page too, in either language", async ({ page }) => {
  test.setTimeout(120_000);
  await signIn(page, tasks);
  const created = await page.request.post('/api/v1/task/tasks', { data: { title: 'The record whose trail a form repeats' } });
  expect(created.status(), await created.text()).toBe(201);

  for (const { language, noun } of [
    { language: 'en', noun: 'tasks' },
    { language: 'pt-PT', noun: 'tarefas' },
  ]) {
    const capital = noun.charAt(0).toUpperCase() + noun.slice(1);
    // The record's address is read off the list rather than built, and every page below is asked in
    // the language the question is about.
    await page.goto(`${tasks}?lang=${language}`);
    const record = await page.locator('[data-component="data-list"] table tbody tr a').first().getAttribute('href');
    expect(record, `the list offers no record in ${language}`).toBeTruthy();

    for (const form of [`${tasks}/new`, `${record}/edit`]) {
      await page.goto(`${form}?lang=${language}`);
      await expect(page.locator('html')).toHaveAttribute('lang', language);
      const trail = page.locator('[data-component="breadcrumb"] a');
      await expect(trail, `${form} in ${language}: the collection is not the second stop of the trail`).toHaveCount(2);
      await expect(trail.nth(1), `${form} in ${language}`).toHaveText(capital);
    }
  }
});
