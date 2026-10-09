import { expect, test } from '@playwright/test';

// Rule 2 in the browser: a stranger who has no account and no session is offered
// exactly the doors this composition mounted, and each door answers from the page that
// names it.
//
// The rule is a wiring rule, so the journey is too. The sign-in card offers the
// forgotten-password link always and the sign-up link only when the composition names
// an email registration (`admin.Deps.Registration`), and one value decides the page,
// the form kind and the link — so a card can only be falsified by walking through it.
// What the walkthrough of record scored zero on was a page that looked right: the two
// account pages beside sign-in rendered their forms, named an address for them, and
// that address was an alias row answering 307, which `ui/assets/js/session.js` refuses
// to follow by design. The button said "The request outcome is unknown" and sent
// nothing, and no assertion about markup alone could see it.
//
// So both halves here are behavioural: the link is followed, the form is filled, the
// response is read, and the acknowledgment the person sees has to become visible on the
// page that asked. The negative half of the rule — a composition that does *not* mount
// registration shows no sign-up link — cannot be walked in this browser, because the
// reference application always mounts it; `modules/admin`'s page cases hold that side.
//
// Fields are named, never labelled: this tenant declares pt-PT (scripts/e2e.sh passes
// --language), and an English word in a spec is a sentence that breaks the day the copy
// moves into a catalogue. The consent box is taken by keyboard for the reason
// mailed-links.spec.ts gives — the design system paints a styled box over the native
// input, which intercepts a click on it.

// The one address the public face answers, in the form the page was rendered with.
// Read off the page rather than written down here: which surface a door is mounted on
// is the thing under test, and a spec that names the address would pass on the address
// it expected instead of the one the application gave.
function actionOf(page: import('@playwright/test').Page, kind: string): Promise<string | null> {
  return page.locator(`form[data-auth-form="${kind}"]`).first().getAttribute('action');
}

async function consent(page: import('@playwright/test').Page): Promise<void> {
  const box = page.locator('input[name="termsAccepted"]');
  await box.focus();
  await page.keyboard.press('Space');
  await expect(box).toBeChecked();
}

test('a stranger is offered the sign-up door on the sign-in card, and the page it leads to registers them', async ({ page }) => {
  test.setTimeout(90_000);

  const stranger = {
    email: `door-${Date.now()}@e2e.test`,
    password: 'e2e-sign-up-at-the-door-password',
    name: 'Stranger At The Door',
  };

  await page.goto('/app/admin/login');
  const signUp = page.locator('a[href="/app/admin/register"]');
  await expect(signUp, 'the card offers no way for a stranger to create an account').toBeVisible();
  await expect(page.locator('a[href="/app/admin/login/forgot"]'), 'the card offers no way to be sent a link')
    .toBeVisible();

  await signUp.click();
  await expect(page, 'the card\'s sign-up link does not lead to the page that mounts it')
    .toHaveURL(/\/app\/admin\/register$/);
  await expect(page.locator('form[data-auth-form="register-password"]')).toBeVisible();

  const posted = page.waitForResponse(r => r.request().method() === 'POST');
  await page.locator('input[name="email"]').fill(stranger.email);
  await page.locator('input[name="displayName"]').fill(stranger.name);
  await page.locator('input[name="password"]').fill(stranger.password);
  await page.locator('input[name="confirmation"]').fill(stranger.password);
  await consent(page);
  const action = await actionOf(page, 'register-password');
  await page.locator('form[data-auth-form="register-password"] button[type="submit"]').click();
  const answer = await posted;

  // The address the page named is the address that answered: a 3xx would have been
  // swallowed by the script's `redirect: "error"` and the person would be holding the
  // sentence about an unknown outcome instead of this one.
  expect(answer.url(), 'the form posted somewhere other than the address it names').toContain(action);
  // The status only, with no `await answer.text()`: the body belongs to the page that
  // posted, and reading it from here hangs until the test's own timeout — the failure
  // this repository's own journey learned about the hard way.
  expect(answer.status(), 'the sign-up door did not answer the 202 a sign-up gets').toBe(202);
  // What the page does with the 202 it was given, which is what a person standing at
  // this door can tell apart: the acknowledgment has become visible, the error alert —
  // the one sentence the alias defect produced, "The request outcome is unknown" — has
  // not, the person is still on the page that asked, and the two credentials are gone
  // from the form. See the note above the forgot journey below for the failure this
  // journey was written against.
  await expect(page.locator('[data-auth-message]').first()).toBeVisible();
  await expect(page.locator('[data-auth-error]').first()).toBeHidden();
  await expect(page).toHaveURL(/\/app\/admin\/register$/);
  for (const name of ['password', 'confirmation']) {
    await expect(page.locator(`input[name="${name}"]`)).toHaveValue('');
  }

  // And the door that creates an account does not open a session: what it starts is a
  // confirmation the person has to read, which is the whole difference between rule 2
  // and rule 1.
  expect((await page.request.get('/api/v1/auth/me')).status(), 'a sign-up should not sign anybody in')
    .toBeGreaterThanOrEqual(400);
});

test('a stranger who cannot sign in is sent to the forgot door by the card, and the page it leads to answers them', async ({ page }) => {
  test.setTimeout(90_000);

  await page.goto('/app/admin/login');
  await page.locator('a[href="/app/admin/login/forgot"]').click();
  await expect(page).toHaveURL(/\/app\/admin\/login\/forgot$/);
  const form = page.locator('form[data-auth-form="forgot"]');
  await expect(form).toBeVisible();

  // An address this tenant has never heard of: the page owes the same answer either
  // way, which is the enumeration rule the route keeps, and the journey should not be
  // the reason a person learns which addresses are accounts here.
  const posted = page.waitForResponse(r => r.request().method() === 'POST');
  await page.locator('input[name="email"]').fill(`nobody-at-${Date.now()}@e2e.test`);
  const action = await actionOf(page, 'forgot');
  await form.locator('button[type="submit"]').click();
  const answer = await posted;

  expect(answer.url(), 'the form posted somewhere other than the address it names').toContain(action);
  expect(answer.ok(), `the forgot door answered ${answer.status()}, not the page's own acknowledgment`).toBe(true);
  expect(answer.status(), 'the forgot door answers without telling anybody who does not exist').toBe(200);
  await expect(page.locator('[data-auth-message]').first()).toBeVisible();
  await expect(page.locator('[data-auth-error]').first()).toBeHidden();
  await expect(page).toHaveURL(/\/app\/admin\/login\/forgot$/);
});
