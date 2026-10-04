import { expect, type APIRequestContext, type Locator, type Page } from '@playwright/test';

// The admin module's browser steps.
//
// A spec that types the sign-in fields out is a spec that re-decides, every time,
// where the email box is and what its label says. When the label changes — which is
// what this repository does to admin copy, on purpose, because it is catalogue copy
// now — every one of those specs breaks at the same time and says nothing about the
// change that broke it. So the address, the labels and the button names of the
// shell this module writes live here once, exported, and a spec composes them.
//
// Two rules keep these steps worth importing rather than merely available:
//
//   - A step says what a person does, in the language the page is written in, and
//     takes the language as an argument rather than assuming English. A step that
//     clicks `Save language` cannot be used by the Portuguese half of a journey,
//     and the Portuguese half is the point of the journeys that exist.
//   - A step asserts the arrival it is responsible for. `signIn` ends with the
//     workspace URL because a step that returned before the redirect landed hands
//     the next selector a race.
//
// The credentials are the ones scripts/e2e.sh's fixture bootstraps, read from the
// same environment the fixture writes, so a spec names no address of its own.

/** The fixture's one administrator, as scripts/e2e.sh bootstrapped them. */
export const adminEmail = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const adminPassword = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

/** The account control the shell offers every signed-in person. */
export function accountLanguage(page: Page): Locator {
  return page.locator('[data-account-language]');
}

/** The signed-in person's own language, chosen in the shell's account control. */
export function accountLanguageStatus(page: Page): Locator {
  return page.locator('[data-account-language-status]');
}

/** The language selector inside the account control. */
export function languageSelect(page: Page): Locator {
  return accountLanguage(page).locator('select[name="locale"]');
}

/**
 * Sign in through the admin shell's own form and land in the workspace.
 *
 * `language` names the language the *labels* are expected in, because the form a
 * person who chose Portuguese meets is a Portuguese form — passing nothing means
 * the installation's own English. It is a string rather than an enum because the
 * tenant's declaration, not this file, decides which tags exist.
 */
export async function signIn(page: Page, labels: { email?: string; password?: string; submit?: string } = {}) {
  await page.goto('/app/admin/login');
  await page.getByRole('textbox', { name: labels.email ?? 'Email', exact: true }).fill(adminEmail);
  await page.getByLabel(labels.password ?? 'Password').fill(adminPassword);
  await page.getByRole('button', { name: labels.submit ?? 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
}

/** Sign out of the shell, which is the half of a session a person can see. */
export async function signOut(page: Page) {
  await page.locator('[data-sign-out]').click();
  await expect(page).toHaveURL(/\/app\/admin\/login$/);
}

/**
 * Choose a language in the account control and land back where the person stood.
 *
 * The save is a form post, so the button is named in the language the page is
 * written in — which is why `saveLabel` is an argument and not a constant: an
 * English constant would make the Portuguese half of a journey untypeable.
 */
export async function chooseLanguage(page: Page, locale: string, saveLabel: string) {
  await languageSelect(page).selectOption(locale);
  await accountLanguage(page).getByRole('button', { name: saveLabel, exact: true }).click();
  await expect(page).toHaveURL(/[?&]language=saved$/);
}

/**
 * Take the choice back, which is the control's empty option: the browser and the
 * tenant's default answer again.
 *
 * This posts the account command directly rather than driving the control, because
 * it is the fixture's hygiene rather than the assertion — it runs whatever a test
 * reached, and the command's unchanged-choice branch means posting it twice
 * publishes nothing.
 */
export async function clearLanguageChoice(request: APIRequestContext) {
  await request.post('/api/v1/user/users/language', { data: { locale: '' } });
}
