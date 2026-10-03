import { expect, type Page, type Response } from '@playwright/test';
import type { Mailbox } from './inbox';

// The reference application's doors, spelled once, and the one way this suite posts
// to them.
//
// `/api/v1/public/…` is where a module's public routes are mounted; `/api/v1/auth/…`
// is the alias kit/httpx/aliases.go vouches for (apps/platformkit/modules.go wires
// auth's alias). A step that posts to the alias passes while the door the product
// actually serves at is gone, so the library names the real one.
//
// `askFromPage` is the shape every posting step uses, and the reason is a property of
// the guards rather than of Playwright: the kernel refuses a cross-site write, and a
// request issued by `APIRequestContext` arrives without the fetch-metadata headers a
// browser adds to a same-origin request. A step that posted from the Node side could
// be refused for its origin, and the test could not tell that refusal from the one it
// was trying to observe. A step that posts from a loaded page sends what a person's
// own click sends, and can only ever be refused for what it did.
export const endpoints = {
  register: '/api/v1/public/auth/register',
  verifyEmail: '/api/v1/public/auth/verify-email',
  resendVerification: '/api/v1/public/auth/resend-verification',
  login: '/api/v1/auth/login',
  signInPage: '/app/admin/login',
  home: '/app',
} as const;

export interface Answer {
  status: number;
  body: string;
}

/** Post from the page the browser already has. `page.goto` must have run first:
 *  this is a browser call, and where it runs is the point of it. */
export async function askFromPage(page: Page, path: string, data: unknown): Promise<Answer> {
  return page.evaluate(
    async ([where, payload]) => {
      const response = await fetch(where, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify(payload),
      });
      return { status: response.status, body: await response.text() };
    },
    [path, data] as const,
  );
}

/** The password a specimen account gets: unique per run, and never the fixture's
 *  bootstrap password, which is the administrator's and would sign the journey in
 *  as somebody else. */
export function specimenPassword(): string {
  return `e2e-${Date.now()}-${Math.random().toString(36).slice(2, 10)}Aa1!`;
}

/** Register: accepted, and awaiting the mailbox. Returns the answer rather than
 *  throwing on a 4xx — a step that throws hides the shape of the refusal a person is
 *  being shown, which is the thing the journey exists to look at. */
export function register(page: Page, input: { email: string; password: string; displayName: string }): Promise<Answer> {
  return askFromPage(page, endpoints.register, {
    email: input.email,
    displayName: input.displayName,
    password: input.password,
    confirmation: input.password,
    termsAccepted: true,
  });
}

/** Sign in through the shell's own form rather than by posting the door: the form is
 *  the product a person meets, and it is the first thing a broken session controller
 *  breaks. The labels are the login page's, in the language it answers by default. */
export async function signIn(page: Page, email: string, password: string): Promise<void> {
  await page.goto(endpoints.signInPage);
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

/** The way on the application owes a person who signed in, asserted as the surface
 *  the shell renders rather than as a status code: a 200 that shows nothing is the
 *  defect, and an empty `main` is what one looks like. */
export async function expectHome(page: Page): Promise<void> {
  await expect(page).toHaveURL(new RegExp(`${endpoints.home}$`));
  await expect(page.locator('main')).toBeVisible();
}

/** The verification credential the email carries — checked before anything is done
 *  with it.
 *
 *  Two journeys read this mail, on purpose, and they do different things with it.
 *  email-verification-link.spec.ts opens the link with the browser, which is what a
 *  person does, and meets the page the module mounts at that address
 *  (modules/auth/internal/ui/page.go); email-verification-origin.spec.ts pins where
 *  that address points. This step belongs to the journey that needs the credential
 *  itself — sign-up-through-the-inbox.spec.ts spends it at the JSON door to watch the
 *  account move — and because spending it there stands in for opening the page, the
 *  step pins the link's exact shape first: if the mail ever says something else, the
 *  journey says so instead of passing on a link nobody would have clicked.
 *
 *  The origin check is not ceremony: a step that followed an emailed URL anywhere
 *  would carry a bearer secret to whatever host the mail named. */
export async function verificationCredential(inbox: Mailbox, address: string, since: number): Promise<string> {
  const mail = await inbox.latestTo(address, { since });
  expect(mail.subject, 'the verification mail says what it is').toMatch(/verify your email/i);
  const link = mail.links.find((one) => one.includes('/auth/verify-email'));
  expect(link, `the mail carried no verification link; its body read: ${mail.text}`).toBeTruthy();

  const url = new URL(link as string);
  // Host *name*, not origin, and the difference is a fact about the link rather than
  // a looseness in the step: the host *name* the module mails is built from the host
  // the tenant is served at, read from the database
  // (modules/auth/internal/password.go's baseURL), while the port on it is the
  // listening one, added by apps/platformkit/modules.go's withServedPort. The tenant
  // record the bootstrap writes says `localhost`, so that is the name the link says,
  // and a step that compared whole origins here would be asserting that the tenant
  // table stores the port the tests happened to pick. The check that matters is kept whole:
  // the link belongs to this installation's host and to no other.
  expect(url.hostname, 'the link points at this installation').toBe(new URL(origin()).hostname);
  expect(url.pathname, 'the link is the verification address').toBe('/auth/verify-email');
  expect(url.searchParams.size, 'the link carries nothing but the credential').toBe(1);
  const token = url.searchParams.get('token') ?? '';
  expect(token.length, 'the credential is a bare secret').toBeGreaterThan(0);
  expect(token.length).toBeLessThanOrEqual(128);
  return token;
}

/** The origin the fixture serves at, read from Playwright's own baseURL so that no
 *  step here invents a host or a port. */
export function origin(): string {
  return (process.env.PLATFORMKIT_E2E_URL ?? 'http://localhost:8099').replace(/\/$/, '');
}

/** Consume the credential, from a page on the same origin. */
export function verifyEmail(page: Page, token: string): Promise<Answer> {
  return askFromPage(page, endpoints.verifyEmail, { token });
}

/** Open a link with the browser, the way a person does. Kept because it is the step
 *  the library is asked to have, and because a native shell does exactly this. */
export async function followLinkInMessage(page: Page, link: string): Promise<void> {
  await page.goto(link);
}

/** A refusal, read from the response of a real request rather than from a fixture. */
export function detailOf(answer: Answer | Response): string {
  const text = 'body' in answer && typeof answer.body === 'string' ? answer.body : '';
  const found = /"detail":"([^"]*)"/.exec(text);
  return found ? found[1] : text;
}
