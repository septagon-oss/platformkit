import { expect, test, type Browser, type Page } from '@playwright/test';

// The sessions screen, driven the way a person who has lost a laptop drives it.
//
// modules/admin/internal/sessions.go is the one page this brief adds, and until
// here it had been rendered by httptest and measured by the design gate, but
// never opened by a browser. This is that gap closed: one sign-in from two
// places, and the revocations the screen offers, each checked against what the
// *other* browser can still do afterwards — because a row disappearing from the
// list a person is reading is not what a revocation means. What it means is that
// the credential on the other machine stops being one, which only the other
// machine can say.
//
// Counts are relative to a baseline read off the screen, not absolute. The
// person here is the installation's only administrator, and every spec that
// signs in leaves a live session behind — this one runs third, by which time
// earlier specs have opened sessions of their own. A row count fixed at two
// would be a claim about the suite's order rather than about the module, so the
// screen is asked how many rows it has, and the assertions are the differences
// one click makes. The one absolute count comes after the bulk action, which
// ends every other session there is whoever left it here, and leaves exactly the
// row the reader is sitting on.
//
// The row action is addressed by its accessible name, which is the aria-label
// naming its device and not the two words printed on it: a person who does not
// see the table decides on what the screen says out loud, and this checks that
// what it says is the row it belongs to.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

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
const thisDevice = (page: Page) => rows(page).filter({ hasText: 'This device' });
const elsewhere = (page: Page) => rows(page).filter({ hasNotText: 'This device' });

test('each revocation on the sessions screen ends the sessions its button named, on the server', async ({ browser }) => {
  test.setTimeout(90_000);
  const here = await signIn(browser);

  await here.goto('/app/auth/sessions');
  await expect(here.getByRole('heading', { name: 'Sessions', exact: true })).toBeVisible();
  const baseline = await rows(here).count();
  expect(baseline).toBeGreaterThanOrEqual(1);

  // A second machine signs in. One row more, and the screen still marks one —
  // and only one — as the device being read.
  const laptop = await signIn(browser);
  await here.reload();
  await expect(rows(here)).toHaveCount(baseline + 1);
  await expect(thisDevice(here)).toHaveCount(1);

  // The design floor's "one thing to do per view", measured on the live page
  // rather than on a mirror of it: one filled submit control, and every row's
  // own revocation drawn unfilled, one per row.
  await expect(here.locator('button[type="submit"][data-variant="primary"]')).toHaveCount(1);
  await expect(here.locator('button[type="submit"][data-variant="ghost"]')).toHaveCount(baseline + 1);

  // One row's own button ends that row and nothing else. The row is named by
  // the ref it carries, so the assertion is about the machine that was clicked.
  const other = elsewhere(here).first();
  const otherRef = await other.getAttribute('data-pk-row');
  expect(otherRef).toBeTruthy();
  await other.getByRole('button', { name: /^End the session on / }).click();
  await expect(here).toHaveURL(/\/app\/auth\/sessions$/);
  await expect(rows(here)).toHaveCount(baseline);
  await expect(here.locator(`tr[data-pk-row="${otherRef}"]`)).toHaveCount(0);
  await expect(thisDevice(here)).toHaveCount(1);
  await expect(here.getByRole('heading', { name: 'Sessions', exact: true })).toBeVisible();

  // And the ended machine is ended where it lives. Its cookie is still in that
  // browser's jar; the answer says what the row said.
  await laptop.goto('/app/auth/sessions');
  await expect(laptop).toHaveURL(/\/app\/admin\/login/);

  // "Everywhere else", then. The button has to count what the click performs —
  // however many rows somebody else left here first — and afterwards the only
  // session of this person's that still works is the one reading the screen.
  const phone = await signIn(browser);
  await here.reload();
  const rest = await elsewhere(here).count();
  await expect(here.getByRole('button', { name: `End the other ${rest}`, exact: true })).toBeVisible();
  expect(rest).toBeGreaterThan(0);
  await here.getByRole('button', { name: `End the other ${rest}`, exact: true }).click();
  await expect(here).toHaveURL(/\/app\/auth\/sessions$/);
  await expect(rows(here)).toHaveCount(1);
  await expect(thisDevice(here)).toHaveCount(1);
  await phone.goto('/app/auth/sessions');
  await expect(phone).toHaveURL(/\/app\/admin\/login/);

  // The last one goes by its own button, which is the only revocation on the
  // page that ends the person who clicked it: the click lands them on the
  // sign-in page rather than on a list with nothing in it.
  await thisDevice(here).getByRole('button', { name: /^End the session on / }).click();
  await expect(here).toHaveURL(/\/app\/admin\/login/);
});
