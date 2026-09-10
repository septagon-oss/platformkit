import { expect, test } from '@playwright/test';

test('real Storybook serves its private index, updates Go controls, and runs confirmation', async ({ page, request }) => {
  const base = '/admin/_gallery/storybook/';
  const anonymous = await request.get(base + 'index.json');
  expect(anonymous.status()).toBe(403);
  await page.goto('/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/admin$/);
  const response = await page.request.get(base + 'index.json');
  expect(response.status()).toBe(200);
  expect(response.headers()['cache-control']).toBe('no-store');
  const index = await response.json();
  const stories = Object.values(index.entries) as { id: string; title: string; name: string; type: string }[];
  expect(stories.length).toBeGreaterThan(100);
  const primary = stories.find(story => story.id.startsWith('pk-ui-component-button-primary-'));
  expect(primary).toBeTruthy();
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(base + 'index.html?path=/story/' + primary!.id);
  const canvas = page.frameLocator('#storybook-preview-iframe');
  const specimen = canvas.frameLocator('iframe');
  await expect(specimen.getByRole('button', { name: 'Save', exact: true })).toBeVisible();
  await page.getByRole('tab', { name: /Controls/ }).click();
  await page.locator('#control-label').fill('Saved from Storybook');
  await expect(specimen.getByRole('button', { name: 'Saved from Storybook', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'light' }).click();
  await page.getByText('dark', { exact: true }).click();
  await expect(specimen.locator('html')).toHaveAttribute('data-theme', 'dark');
  const confirm = stories.find(story => story.title === 'Overlay/ConfirmDialog');
  expect(confirm).toBeTruthy();
  await page.goto(base + 'index.html?path=/story/' + confirm!.id);
  await specimen.getByRole('button', { name: 'Open confirmation' }).click();
  const dialog = specimen.getByRole('dialog', { name: 'Delete this row?' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Keep' }).click();
  await expect(dialog).toBeHidden();
  expect(errors).toEqual([]);
});

test('Storybook preserves exact JSON integers, rejects malformed edits, and resets to source', async ({ page }) => {
  await page.goto('/health');
  const result = await page.evaluate(async () => {
    const { render } = await import('/admin/assets/js/storybook.js');
    const context = {
      initialArgs: { count: '9007199254740993' }, globals: { theme: 'dark' }, name: 'Exact count',
      parameters: { platformkit: { example: 'product/count', rawFields: ['count'] } },
    };
    const edited = render({ count: '9007199254740995' }, context) as HTMLIFrameElement;
    const reset = render({ count: '9007199254740993' }, context) as HTMLIFrameElement;
    const invalid = render({ count: '1,"other":true' }, context);
    return { edited: new URL(edited.src).searchParams.get('props'),
      reset: new URL(reset.src).searchParams.get('props'),
      invalidRole: invalid.getAttribute('role'), invalidMessage: invalid.textContent };
  });
  expect(result.edited).toBe('{"count":9007199254740995}');
  expect(result.reset).toBe('{}');
  expect(result.invalidRole).toBe('alert');
  expect(result.invalidMessage).toBe('count must contain one valid JSON value.');
});
