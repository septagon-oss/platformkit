import { test, expect, type Page } from '@playwright/test';
import { signIn, securityHeaders } from '../steps/kernel';

// Chromium resolves the fixture's *.localhost host even when Node's resolver
// cannot. Keep requests on the browser's origin and signed-in cookie jar.
async function ask(page: Page, path: string, options: { method?: string; headers?: Record<string, string> } = {}) {
  return page.evaluate(async ({ path, options }) => {
    const response = await fetch(path, { ...options, credentials: 'same-origin' });
    return { status: response.status, headers: Object.fromEntries(response.headers), body: await response.text() };
  }, { path, options });
}

test('a generated list fetches composed SVG without JavaScript and revocation defeats a warm validator', async ({ page: loginPage, browser }) => {
  const door = process.env.PLATFORMKIT_PICTURE_DOOR!;
  const key = process.env.PLATFORMKIT_PICTURE_KEY!;
  expect(door).toBeTruthy();
  await signIn(loginPage, '/app/picturefixture/pictures');
  const context = await browser.newContext({ baseURL: process.env.PLATFORMKIT_E2E_URL, storageState: await loginPage.context().storageState(), javaScriptEnabled: false });
  const page = await context.newPage();
  const response = await page.goto('/app/picturefixture/pictures');
  securityHeaders(response, 'desk');
  const image = page.locator(`img[src="${door}"]`);
  await expect(image).toBeVisible();
  await expect(image).toHaveAttribute('alt', '');
  expect(await image.evaluate((element: HTMLImageElement) => element.complete && element.naturalWidth > 0 && element.naturalHeight > 0)).toBe(true);
  const picture = await ask(loginPage, door);
  expect(picture.status).toBe(200);
  expect(picture.headers['cache-control']).toBe('private, no-store');
  expect(picture.headers['content-type']).toBe('image/svg+xml; charset=utf-8');
  expect(picture.headers.etag).toBeTruthy();
  const svg = picture.body;
  expect(svg).toContain('<circle');
  expect(svg).toContain('fill="#AABBCC"');
  expect(svg).not.toContain('data-slot');
  const removed = await ask(loginPage, `/api/v1/content/contents/${key}`, { method: 'DELETE' });
  expect(removed.status, removed.body).toBe(204);
  const revoked = await ask(loginPage, door, { headers: { 'If-None-Match': picture.headers.etag } });
  expect(revoked.status).toBe(404);
  expect(revoked.headers.etag).toBeUndefined();
  expect(revoked.body).not.toContain('<svg');
  await context.close();
});
