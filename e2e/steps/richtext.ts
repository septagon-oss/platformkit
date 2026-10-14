import { expect, type Page } from '@playwright/test';
import { signInForContent } from './content';

export const imageBytes = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAIAAAADCAYAAAC56t6BAAAAEUlEQVR4nGP4z8DwH4QZMBgAoXkL9U3EmgcAAAAASUVORK5CYII=', 'base64');
export const editingSurface = '[data-richtext-surface] [contenteditable="true"]';

export async function openRichTextForm(page: Page) {
  await signInForContent(page, process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test', process.env.PLATFORMKIT_E2E_PASSWORD ?? '');
  await page.goto('/app/content/contents/new');
  await page.getByLabel('Address name').fill(`images-${Date.now()}`);
  await page.getByLabel('Title').fill('Pictures in a page');
  await expect(page.locator(editingSurface)).toBeVisible();
  await page.locator(editingSurface).click();
}

export async function pickRichTextImage(page: Page, name = 'picture.png') {
  const uploaded = page.waitForResponse((response) => response.request().method() === 'POST' && response.url().includes('/api/v1/file/files'));
  await page.locator('[data-richtext-file]').setInputFiles({ name, mimeType: 'image/png', buffer: imageBytes });
  const response = await uploaded;
  expect(response.status(), await response.text()).toBe(201);
  const file = await response.json();
  expect(file.id).toMatch(/^[0-9a-f-]{36}$/);
  expect(new URL(response.url()).searchParams.get('visibility')).toBeNull();
  return file.id as string;
}
