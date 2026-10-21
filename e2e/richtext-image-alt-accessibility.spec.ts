import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { openRichTextForm, pickRichTextImage } from './steps/richtext';

test('the image alt prompt names its text input and decorative choice for assistive technology', async ({ page }) => {
  await openRichTextForm(page);
  await pickRichTextImage(page);
  const prompt = page.getByRole('group', { name: 'Alt text', exact: true });
  await expect(prompt).toBeVisible();
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
  const serious = results.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical');
  expect(serious.map((violation) => `${violation.id}: ${violation.nodes.map((node) => node.target.join(' ')).join(', ')}`)).toEqual([]);
  await expect(prompt.getByRole('textbox', { name: 'Alt text', exact: true })).toBeVisible();
  await expect(prompt.getByRole('checkbox', { name: 'Decorative', exact: true })).toBeVisible();
});
