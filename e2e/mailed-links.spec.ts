import { expect, test } from '@playwright/test';

// Rule 4's three remaining mails, in a browser and read out of the catcher: the
// confirmation a sign-up sends, the second confirmation an explicit resend replaces
// it with, and the set-password link a person who cannot sign in asks for. The
// fourth mail rule 4 names — the invitation — is read in invitation-mail.spec.ts,
// in the invited person's own browser.
//
// Every search goes to Mailpit's own API by recipient, so what is asserted is a
// delivered message: what its link is built from, and whether that link answers.
// Nothing here reads a log, and nothing skips when the catcher is missing:
// scripts/e2e.sh refuses the run before it builds anything if nobody answers the
// address it points these journeys at.
//
// Forms are found by the name of their field and the kind of their form, never by a
// label: this tenant declares pt-PT (scripts/e2e.sh passes --language), and an
// English word in a spec is a sentence that breaks the day the copy moves into a
// catalogue.

const mailpit = process.env.PLATFORMKIT_E2E_MAILPIT_URL ?? 'http://localhost:8025';

// One delivered message, as the catcher holds it.
type Mailed = { id: string; body: string };

// The messages Mailpit holds for `to`, read through its search API. An empty list is
// what "nothing has arrived yet" looks like, so a caller can poll on it.
async function mailbox(request: import('@playwright/test').APIRequestContext, to: string): Promise<Mailed[]> {
  const found = await request
    .get(`${mailpit}/api/v1/search`, { params: { query: `to:"${to}"` } })
    .catch(() => null);
  if (!found || !found.ok()) return [];
  const { messages } = (await found.json()) as { messages?: { ID: string }[] };
  const held: Mailed[] = [];
  for (const message of messages ?? []) {
    const detail = await request.get(`${mailpit}/api/v1/message/${message.ID}`);
    if (!detail.ok()) continue;
    const { Text, HTML } = (await detail.json()) as { Text?: string; HTML?: string };
    held.push({ id: message.ID, body: `${Text ?? ''}\n${HTML ?? ''}` });
  }
  return held;
}

// The account links one message carries, at the two addresses auth mails them at.
// They are not the same surface, and the difference is the module's, not this
// journey's: the confirmation link sits on auth's *public* face — contracts.VerifyEmailPath
// is /auth/verify-email, because the person opening it holds no session yet
// (modules/auth/internal/ui/page.go mounts it there and refuses a composition that
// moves it) — while the set-password link is a workspace screen, ResetPath being
// httpx.Workspace("/auth/reset"). One expression spells both, so neither journey
// gets to invent a third address.
const linkPattern = /https?:\/\/[^\s"'<>]+\/(?:app\/)?auth\/(reset|verify-email)\?token=[A-Za-z0-9_\-=%]+/g;

function links(messages: Mailed[]): string[] {
  return messages.flatMap(message => message.body.match(linkPattern) ?? []);
}

// Which notice a message is: the page its link points at, without the credential.
function pages(held: string[]): string[] {
  return held.map(link => new URL(link).pathname);
}

// The consent box is the design system's own: the native input is deliberately
// invisible (ui/components/checkbox.go paints a styled box in its place), so a click
// on it is intercepted by that box. The keyboard path is the one the component
// promises a person who never touches a mouse, and it names no copy, so this is what
// a journey uses to accept terms in a tenant whose copy it does not read.
async function consent(page: import('@playwright/test').Page): Promise<void> {
  const box = page.locator('input[name="termsAccepted"]');
  await box.focus();
  await page.keyboard.press('Space');
  await expect(box).toBeChecked();
}

// The one credential on a link, which is the only part two notices to the same
// person differ by.
function token(link: string): string {
  return new URL(link).searchParams.get('token') ?? '';
}

test('a sign-up is confirmed by one mailed link and an explicit resend by another', async ({ page, baseURL }) => {
  // The second confirmation cannot be asked for inside the first one's minute: auth
  // keeps one live verification link per person, and a request inside that window
  // is acknowledged and not sent. So the resend this case reads is the one a person
  // who is still waiting chooses after the window, waited out rather than asked for
  // early and asserted away.
  test.setTimeout(180_000);

  const stranger = {
    email: `signed-up-${Date.now()}@e2e.test`,
    password: 'e2e-sign-up-confirmation-password',
    name: 'Signed Up Stranger',
  };
  const served = new URL(baseURL ?? 'http://localhost:8099').origin;

  // A stranger, in a browser holding no session: the sign-up page is the door rule 2
  // puts on the sign-in card, and this is the person it is there for.
  await page.goto('/app/admin/register');
  await page.locator('input[name="email"]').fill(stranger.email);
  await page.locator('input[name="displayName"]').fill(stranger.name);
  await page.locator('input[name="password"]').fill(stranger.password);
  await page.locator('input[name="confirmation"]').fill(stranger.password);
  await consent(page);
  const registered = page.waitForResponse(r =>
    r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/auth/register'));
  await page.locator('form[data-auth-form="register-password"] button[type="submit"]').click();
  expect((await registered).status(), 'the sign-up was not accepted').toBe(202);

  let held: Mailed[] = [];
  await expect
    .poll(async () => (held = await mailbox(page.request, stranger.email)).length, {
      message: `Mailpit at ${mailpit} holds the sign-up confirmation for ${stranger.email}`,
      timeout: 30_000,
    })
    .toBe(1);
  const first = links(held);
  expect(pages(first), 'one confirmation and nothing else').toEqual(['/auth/verify-email']);
  expect(first[0].startsWith(`${served}/auth/verify-email?token=`), `the link: ${first[0]}`).toBe(true);

  // Auth's own window between one confirmation and the next is a minute, counted
  // from the message that is already in the catcher, so this waits it out.
  await page.waitForTimeout(65_000);
  const again = await page.request.post('/api/v1/auth/resend-verification', { data: { email: stranger.email } });
  expect(again.status(), await again.text()).toBe(202);

  // The neutral acknowledgment is the whole of the answer a stranger gets; whether
  // it cost a mail is only ever visible here.
  let replaced: string[] = [];
  await expect
    .poll(async () => (replaced = links(await mailbox(page.request, stranger.email))).length, {
      message: `Mailpit at ${mailpit} holds the resent confirmation for ${stranger.email}`,
      timeout: 45_000,
    })
    .toBe(2);
  expect(pages(replaced), 'both messages are confirmations').toEqual(
    ['/auth/verify-email', '/auth/verify-email']);
  for (const link of replaced) {
    expect(link.startsWith(`${served}/auth/verify-email?token=`), `the link: ${link}`).toBe(true);
  }
  expect(new Set(replaced.map(token)).size, 'the resent link replaces the first').toBe(2);
});

test('a person who signed up, confirmed and then forgot their password is sent a link on the address this tenant is served at', async ({ page, browser, baseURL }) => {
  test.setTimeout(120_000);

  const person = {
    email: `forgot-${Date.now()}@e2e.test`,
    password: 'e2e-forgotten-password',
    replacement: 'e2e-chosen-at-the-link-password',
    name: 'Forgot What I Chose',
  };
  const served = new URL(baseURL ?? 'http://localhost:8099').origin;

  await page.goto('/app/admin/register');
  await page.locator('input[name="email"]').fill(person.email);
  await page.locator('input[name="displayName"]').fill(person.name);
  await page.locator('input[name="password"]').fill(person.password);
  await page.locator('input[name="confirmation"]').fill(person.password);
  await consent(page);
  const registered = page.waitForResponse(r =>
    r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/auth/register'));
  await page.locator('form[data-auth-form="register-password"] button[type="submit"]').click();
  expect((await registered).status(), 'the sign-up was not accepted').toBe(202);

  let mailed: string[] = [];
  await expect
    .poll(async () => (mailed = links(await mailbox(page.request, person.email))).length, {
      message: `Mailpit at ${mailpit} holds the confirmation for ${person.email}`,
      timeout: 30_000,
    })
    .toBe(1);
  expect(mailed[0].startsWith(`${served}/auth/verify-email?token=`), `the link: ${mailed[0]}`).toBe(true);

  // The link is opened by the person it was addressed to, in their own browser, and
  // it turns the account on: a mailed link that opens a page answering nothing is a
  // sign-up nobody finishes.
  const theirs = await browser.newContext();
  const as = await theirs.newPage();
  await as.goto(mailed[0]);
  const confirmed = as.waitForResponse(r =>
    r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/auth/verify-email'));
  // This form is found by the one field it carries. It has no data-auth-form and is not
  // this page's to grow one: that attribute is session.js's instruction to submit a form
  // as JSON (ui/assets/js/session.js), and this page spends its credential through a real
  // form post whose answer is a redirect — the one answer that script's fetch refuses
  // (redirect: "error"), so a marker borrowed from it would turn the button into "the
  // request outcome is unknown" on every page that ever ships that script.
  await as.locator('form:has(input[name="token"]) button[type="submit"]').click();
  // A confirmed address is answered with the 303 to sign-in — which Response.ok(), being
  // 2xx only, reports as a failure. For a form a browser submits rather than a fetch, the
  // redirect is what "it worked" is spelled as, and the Go journey of this same page
  // (apps/platformkit/email_link_page_test.go) pins the same status.
  expect((await confirmed).status(), 'the mailed confirmation link did not confirm the address').toBe(303);
  await expect(as).toHaveURL(/\/app\/admin\/login$/);

  // The password they chose at sign-up now opens the door.
  await as.goto('/app/admin/login');
  await as.locator('form[data-login-form] input[name="email"]').fill(person.email);
  await as.locator('form[data-login-form] input[name="password"]').fill(person.password);
  await as.locator('form[data-login-form] button[type="submit"]').click();
  await expect(as).not.toHaveURL(/\/login/);
  expect((await as.request.get('/api/v1/auth/me')).status()).toBe(200);
  await theirs.close();

  // Later the same person cannot remember it. A browser with no session: the forgot
  // door is what the sign-in card offers somebody who is not signed in, and it has
  // to answer from there.
  const lost = await browser.newContext();
  const anonymous = await lost.newPage();
  await anonymous.goto('/app/admin/login/forgot');
  await anonymous.locator('input[name="email"]').fill(person.email);
  const asked = anonymous.waitForResponse(r =>
    r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/auth/password/forgot'));
  await anonymous.locator('form[data-auth-form="forgot"] button[type="submit"]').click();
  // The answer is the same acknowledgment either way — the address is not looked up
  // in the request — and it is an acknowledgment, not a promise: only the catcher
  // below says whether anything was sent.
  const refused = await asked;
  // No body is read here: the page is a form that has just been submitted, and the
  // answer that matters is the status the door gave.
  expect(refused.ok(), `the request for a link was refused with ${refused.status()}`).toBe(true);

  let reset: string[] = [];
  await expect
    .poll(async () => (reset = links(await mailbox(anonymous.request, person.email))
      .filter(link => new URL(link).pathname === '/app/auth/reset')).length, {
      message: `Mailpit at ${mailpit} holds a set-password link for ${person.email}`,
      timeout: 30_000,
    })
    .toBe(1);
  expect(reset[0].startsWith(`${served}/app/auth/reset?token=`), `the reset link: ${reset[0]}`).toBe(true);

  // The link answers the page it names, the person chooses a password there, and
  // choosing it is what signs that browser in.
  await anonymous.goto(reset[0]);
  await anonymous.locator('input[name="new"]').fill(person.replacement);
  const resetDone = anonymous.waitForResponse(r =>
    r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/auth/password/reset'));
  await anonymous.locator('form[data-auth-form="reset"] button[type="submit"]').click();
  expect((await resetDone).ok(), 'the mailed set-password link did not set a password').toBe(true);
  await expect(anonymous).not.toHaveURL(/\/app\/auth\/reset/);
  expect((await anonymous.request.get('/api/v1/auth/me')).status()).toBe(200);

  await lost.close();
});
