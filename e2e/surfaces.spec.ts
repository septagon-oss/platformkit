import { expect, test } from '@playwright/test';

// The three surfaces, asked of a browser rather than of a router in a test
// binary. The Go cases in kit/httpx prove each rule on its own; what only a
// browser can say is that the chain a person arrives through is the one the
// address promised — that an old bookmark still opens a page, that the
// workspace sends somebody who has not signed in somewhere they can act rather
// than to a body of JSON, and that the control plane is not merely refused here
// but simply absent.
//
// The email and password are the fixture's own (scripts/e2e.sh), and this spec
// never signs in: every request here is what an anonymous visitor sends.

test('a bookmark of the old sign-in address opens the sign-in page', async ({ page }) => {
  await page.goto('/admin/login');
  await expect(page).toHaveURL(/\/app\/admin\/login$/);
  // Not just a redirect that happened: the form the address was bookmarked for.
  await expect(page.locator('[data-login-form]')).toBeVisible();
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test');
});

test('the old address of a generated screen leads to the screen that replaced it', async ({ page }) => {
  await page.goto('/admin/task/tasks');
  // Where an unauthenticated browser ends up is the workspace's own answer: it
  // sent the person to the door that opens this screen, and named the screen on
  // the way. The 404 an unmounted address gives is the answer this must not be.
  expect(new URL(page.url()).pathname).toBe('/app/admin/login');
  expect(new URL(page.url()).searchParams.get('next')).toBe('/app/task/tasks');
});

test('a workspace address sends a browser that is not signed in to the door, not to a fault', async ({ page }) => {
  const response = await page.goto('/app');
  // 200 of a page, not a 403 of a problem document: an anonymous browser is
  // refused the workspace by being taken somewhere it can act.
  expect(response?.status()).toBe(200);
  expect(new URL(page.url()).pathname).toBe('/app/admin/login');
  expect(new URL(page.url()).searchParams.get('next')).toBe('/app');
  await expect(page.locator('[data-login-form]')).toBeVisible();
});

test('the control plane answers at no host a customer is served at', async ({ page }) => {
  // Two addresses, one answer. `/api/v1/tenant/tenants` is where the control
  // plane used to answer from every tenant's host; the installation's own host
  // is the only one that serves it now, and this is not that host. The new
  // address is asked as well, so the pair says "not mounted here" rather than
  // "the alias aged".
  for (const path of ['/api/v1/tenant/tenants', '/api/v1/ops/tenant/tenants']) {
    const res = await page.request.get(path);
    expect(res.status(), path).toBe(404);
  }
});

test('a public page opens a session for nobody', async ({ page }) => {
  const res = await page.request.get('/api/v1/public/site/settings');
  expect(res.status()).toBe(200);
  // The cookie header, not its absence from a jar: a jar hides a Set-Cookie the
  // response did send, and setting no cookie is the promise the surface makes.
  expect(res.headers()['set-cookie']).toBeUndefined();
});
