import { expect, test } from '@playwright/test';
import { expectInboxReady, mailbox, specimenAddress } from './steps/inbox';
import {
  askFromPage,
  endpoints,
  expectHome,
  register,
  signIn,
  specimenPassword,
  verificationCredential,
  verifyEmail,
} from './steps/journey';

// Signing up, and getting in.
//
// The journey the walkthrough scored 0 of 3: a person fills a registration form, is
// told to check their email, and there is nothing to check — not because the
// application is broken but because a development stack had no mail sink, and the
// auth module rightly refuses email registration without one. Mailpit is that sink
// now (`make up`), so this is the whole path a stranger takes, read through the
// inbox the application actually sent to:
//
//   register → the email arrives → its credential is consumed → sign in → home.
//
// Two things it deliberately does not do. It does not intercept the mail: every
// claim about the message goes through the sink's own API, so an application that
// recorded a notification and never handed it to a sender fails here. And it asserts
// that the account cannot sign in *before* verification, because a journey that
// shows only the happy path cannot tell verification apart from a form that did
// nothing at all.
//
// The origin the fixture serves at is Playwright's baseURL, and the inbox is
// PLATFORMKIT_E2E_MAIL_URL, which scripts/e2e.sh exports; neither is a literal here.
process.env.PLAYWRIGHT_NO_COPY_PROMPT = '1';

test('a stranger registers, reads the inbox, and lands on their home', async ({ page }) => {
  const inbox = mailbox();
  await expectInboxReady(inbox);

  // Loaded once, before any posting: every request this journey makes is made from
  // a page on the installation's own origin, so no refusal in this file can be the
  // CSRF guard mistaking the test for another site.
  await page.goto(endpoints.signInPage);

  const address = specimenAddress('signup');
  const password = specimenPassword();
  const before = Date.now();

  const registered = await register(page, { email: address, password, displayName: 'Inbox Journey' });
  expect(registered.status, 'registration is accepted and awaits the mailbox').toBe(202);

  // The account exists and is not yet usable. This is the line that proves the
  // verification below changed something rather than nothing having been needed.
  const tooEarly = await askFromPage(page, endpoints.login, { email: address, password });
  // Measured: the module answers an unverified account's sign-in as 401
  // "those credentials are not right", not 403. Asserted as measured, because the
  // claim this line defends is that the account cannot get in yet, and dressing the
  // status up here would hide what a client actually has to handle.
  expect(tooEarly.status, `an unverified account must not sign in: ${tooEarly.body}`).toBe(401);

  const mail = await inbox.latestTo(address, { since: before });
  expect(mail.subject).toMatch(/verify your email/i);
  expect(await inbox.countTo(address), 'one registration sends one mail, not two').toBe(1);

  const token = await verificationCredential(inbox, address, before);
  const verified = await verifyEmail(page, token);
  expect(verified.status, `the credential the mail carried must open the account: ${verified.body}`).toBe(200);

  await signIn(page, address, password);
  await expectHome(page);
});

test('a registration nobody asked for sends no mail at all', async ({ page }) => {
  const inbox = mailbox();
  await page.goto(endpoints.signInPage);
  const address = specimenAddress('refused');

  // The negative, on the same stack: a registration the module refuses never reaches
  // the mailer, so the inbox has to stay empty for that address. This is what tells
  // "the mail sink is down" apart from "nothing was ever asked of it" — the
  // difference a person staring at an empty inbox cannot see from the outside.
  const refused = await register(page, { email: address, password: 'short', displayName: 'No Mail' });
  expect(refused.status, 'a password under twelve characters is refused').toBe(422);
  expect(await inbox.countTo(address), 'a refused registration asks nothing of the mailer').toBe(0);
});
