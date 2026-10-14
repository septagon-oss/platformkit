import { expect, test } from '@playwright/test';
import { editingSurface, openRichTextForm } from './steps/richtext';

test('an untouched link with a quoted title keeps its words address and title after saving', async ({ page }) => {
  await openRichTextForm(page);
  const created = await page.request.post('/api/v1/content/contents', {
    data: {
      slug: `quoted-title-${Date.now()}`,
      title: 'A link with a title',
      kind: 'page',
      body: '[Website](https://example.com "A \\"quoted\\" title")\n',
    },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { id } = await created.json();
  const before = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(before.status()).toBe(200);
  const original = (await before.json()).body;
  await page.goto(`/app/content/contents/${id}/edit`);
  await expect(page.locator(editingSurface)).toBeVisible();
  await expect(page.locator(`${editingSurface} a`)).toHaveAttribute('title', 'A "quoted" title');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/app/content/contents/${id}$`));
  const after = await page.request.get(`/api/v1/content/contents/${id}`);
  expect(after.status()).toBe(200);
  const saved = (await after.json()).body;
  console.log(`stored before: ${original}; stored after: ${saved}`);
  const renderedLink = page.locator('[data-component="prose"] a[href="https://example.com"]');
  await expect(renderedLink, 'an untouched save must not turn a supported link into plain text').toHaveText('Website');
  await expect(renderedLink).toHaveAttribute('title', 'A "quoted" title');
  expect(saved, 'saving untouched supported Markdown preserves the entire link').toBe(original);
});
