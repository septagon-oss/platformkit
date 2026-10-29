import { expect, test } from '@playwright/test';
import { signIn } from './steps/kernel';

// The capital a line begins with belongs to the page, and to that line alone: where the resolved
// noun stands alone it starts with a capital, and where it sits inside a sentence the word stays
// the way the catalogue wrote it. `modules/task/module.go` declares the plural `tasks`, and the
// module's pt-PT catalogue spells the same hint `tarefas` — both lower-case, both the author's
// word — so one screen answers the question in either language and neither answer can be copied
// from the other. A rule that only holds in the language of its test would pass an English-only
// journey: the capital has to be the page's, not the string's.
//
// The count is the sentence; the heading and the table region's name are lines. A name match in
// the browser is case-insensitive, so every assertion below compares the served text or attribute.

const tasks = '/app/task/tasks';

test("the noun that begins a line takes the page's capital in the language the page is served in", async ({ page }) => {
  test.setTimeout(120_000);
  await signIn(page, tasks);
  // Two records of this run's own: the count's phrase follows the language's own plural rule, and
  // only a plural prints the noun the policy resolved rather than the singular the module declares.
  for (const title of ['The noun that begins a line', 'The noun that reads second']) {
    const created = await page.request.post('/api/v1/task/tasks', { data: { title } });
    expect(created.status(), await created.text()).toBe(201);
  }

  for (const { language, noun } of [
    { language: 'en', noun: 'tasks' },
    { language: 'pt-PT', noun: 'tarefas' },
  ]) {
    const capital = noun.charAt(0).toUpperCase() + noun.slice(1);
    await page.goto(`${tasks}?lang=${language}`);
    await expect(page.locator('html')).toHaveAttribute('lang', language);

    // The lines: the list's own heading and the name of its table region.
    await expect(page.locator('main h1')).toHaveText(capital);
    await expect(page.locator('[data-component="data-list"]')).toHaveAttribute('aria-label', capital);

    // The sentence: the result count, whose surrounding words the catalogue's own copy owns in
    // each language. The digits are read rather than predicted and no word but the noun is named:
    // what is pinned is that the noun arrives spelled and cased as the catalogue wrote it, and not
    // with the capital the line above it was given.
    const count = page.locator('[data-component="data-list"] > p');
    await expect(count).toHaveCount(1);
    await expect(count).toHaveText(new RegExp(`^\\d+ ${noun}\\b`));
    await expect(count).not.toHaveText(new RegExp(`^\\d+ ${capital}\\b`));
  }
});
