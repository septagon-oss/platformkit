import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// Gate 10, the brief's own acceptance line: a person uses the account control in the
// shell, chooses Português, and the admin they are looking at becomes Portuguese
// around them — and the choice is theirs the next time they sign in.
//
// The Go cases around the same door (apps/platformkit/account_menu_language_test.go,
// account_language_test.go) prove the control is offered, that it offers only the
// tenant's own languages, and that the stored choice survives a session. What only a
// browser can prove is the middle of the journey: that the control is a form a person
// can use with no script, that the redirect lands them back where they were standing,
// and that the page they land on is *written* in the language they asked for rather
// than merely declaring it — the failure this task is about.
//
// The English browser is the point: the language changes because the person chose it,
// not because of what the browser sent, so nothing here depends on the locale the
// browser declares.
//
// It runs against scripts/e2e.sh's own fixture — one tenant, bootstrapped
// `--language pt-PT`, whose single administrator is this person — which is why the
// file ends by taking the choice back and why afterEach posts it again: nine specs
// after this one sign in as this same person on an English page, so a choice left
// behind here is a Portuguese suite and ten failures that say nothing about the code.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

async function signIn(page: Page) {
  await page.goto('/app/admin/login');
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
}

test.afterEach(async ({ request }) => { await clearChoice(request) });

// clearChoice posts the account command with the empty tag, which is what the
// control's own empty option posts. It is the fixture's hygiene rather than the
// assertion — the journey below asserts the form half — so it runs whatever the test
// reached, and the command's unchanged-choice branch means posting it twice publishes
// nothing.
async function clearChoice(request: APIRequestContext) {
  await request.post('/api/v1/user/users/language', { data: { locale: '' } });
}

test('a person choosing Português in the account control is answered in Portuguese, and again after signing out', async ({ page }) => {
  await signIn(page);
  // An English browser, an English page: the control's job is to change this.
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();

  const control = page.locator('[data-account-language]');
  await expect(control).toBeVisible();
  const select = control.locator('select[name="locale"]');
  await expect(select).toBeVisible();
  // What is on offer is the tenant's declaration, default first, and nothing else.
  // The empty value is the choice to stop choosing.
  const offered = await select.locator('option').evaluateAll(options =>
    options.map(option => option.value).filter(value => value !== ''));
  expect(offered).toEqual(['en', 'pt-PT']);

  await select.selectOption('pt-PT');
  await control.getByRole('button', { name: 'Save language', exact: true }).click();

  // Back to the page they were reading — a form post, not a swap — with the sentence
  // that says it landed, in the language they just asked for.
  await expect(page).toHaveURL(/\/app\?language=saved$/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
  await expect(page.locator('html')).toHaveAttribute('dir', 'ltr');
  await expect(page.getByRole('heading', { name: 'Painel', exact: true })).toBeVisible();
  await expect(page.locator('[data-account-language-status]')).toHaveText('Idioma guardado.');
  // The control reads the choice back rather than the browser, and its own button now
  // says it in the language it is written in.
  await expect(select).toHaveValue('pt-PT');
  expect((await new AxeBuilder({ page }).include('[data-account-language]').analyze()).violations)
    .toEqual([]);

  // A choice, not page state: a new session for the same person keeps it, and the
  // English browser that follows it home does not overrule it.
  await page.locator('[data-sign-out]').click();
  await expect(page).toHaveURL(/\/app\/admin\/login$/);
  await signIn(page);
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
  await expect(page.getByRole('heading', { name: 'Painel', exact: true })).toBeVisible();
  await expect(select).toHaveValue('pt-PT');

  // Taking the choice back is the other half of the control — a preference you cannot
  // withdraw is a default you did not agree to — and the empty option is that half:
  // the browser and the tenant's default answer again. The button is clicked by its
  // Portuguese name because the page is Portuguese, which is the same fact the two
  // lines above assert, asserted of the control itself.
  await select.selectOption('');
  await control.getByRole('button', { name: 'Guardar idioma', exact: true }).click();
  await expect(page).toHaveURL(/\/app\?language=saved$/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  await expect(select).toHaveValue('');
});
