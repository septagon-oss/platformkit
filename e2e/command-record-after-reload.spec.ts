import { expect, test, type Browser, type Page } from '@playwright/test';

// A record the command controller kept across a reload belongs to the submission
// that left it, and to nothing the person asks for afterwards.
//
// ui/assets/js/command.js keeps one record per form in sessionStorage and reuses its
// key for that form's next submission. A reload that happens after the application
// answered, but before the answer arrived, leaves that record behind. The person
// then sees the list the command produced, and later asks for the same command
// again because the world has changed — here, a new device has signed in, and they
// press "End the other N" once more. That is a new command, and it has to run: the
// device that signed in after the first revocation must be signed out.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const revokeRestUrl = '**/app/auth/sessions/revoke-rest';

async function signIn(browser: Browser): Promise<Page> {
  const page = await browser.newPage();
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
  return page;
}

const rows = (page: Page) => page.locator('tr[data-pk-row]');

test('a later deliberate submission after a reload runs, rather than replaying the lost one', async ({ browser }) => {
  test.setTimeout(90_000);
  const here = await signIn(browser);
  await signIn(browser);

  await here.goto('/app/auth/sessions');
  const before = await rows(here).count();
  expect(before).toBeGreaterThanOrEqual(2);

  // The first revocation reaches the application, which runs it and answers; the
  // answer is destroyed on its way back, and the person reloads before any retry.
  let ran = 0;
  await here.route(revokeRestUrl, async route => {
    if (route.request().method() !== 'POST') return route.fallback();
    // Only the first attempt reaches the application; a retry the controller makes
    // before the reload is lost before it arrives, so the reload is what ends it.
    if (ran === 0) await route.fetch();
    ran++;
    return route.abort('connectionrefused');
  });
  await here.getByRole('button', { name: `End the other ${before - 1}`, exact: true }).click();
  await expect.poll(() => ran, { timeout: 10_000 }).toBeGreaterThanOrEqual(1);
  await here.reload();
  await here.unroute(revokeRestUrl);
  await expect(rows(here)).toHaveCount(1);

  // A new device signs in afterwards, and the person ends it with the same button.
  const phone = await signIn(browser);
  await here.reload();
  await expect(rows(here)).toHaveCount(2);
  await here.getByRole('button', { name: 'End the other 1', exact: true }).click();
  await expect(here).toHaveURL(/\/app\/auth\/sessions$/);

  await expect(rows(here)).toHaveCount(1);
  await phone.goto('/app/auth/sessions');
  await expect(phone).toHaveURL(/\/app\/admin\/login/);
});
