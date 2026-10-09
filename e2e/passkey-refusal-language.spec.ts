import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test, type Page } from '@playwright/test';

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? '',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
const settings = '/api/v1/auth/settings/passkey-sign-in';

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/app/admin/login');
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
}

// The fixture's tenant is served in pt-PT (scripts/e2e.sh bootstraps it so), and
// the sign-in page it serves says "Continuar com uma passkey". The sentence the
// passkey door refuses with lands in that page's [data-login-error], so it is
// copy a person reads, and the module's own pt-PT catalogue is where its
// Portuguese lives — whichever catalogue a fix puts it in. Reached by the status
// the shut door answers with.
function portugueseCatalogues(dir: string, found: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name.startsWith('.')) continue;
    const path = join(dir, name);
    if (statSync(path).isDirectory()) portugueseCatalogues(path, found);
    else if (name === 'pt-PT.json' && dir.endsWith('messages')) found.push(path);
  }
  return found;
}

test('the shut usernameless door refuses in the language the request asked for', async ({ page }) => {
  await signIn(page, admin.email, admin.password);
  await expect(page).toHaveURL(/\/app$/);
  const shut = await page.request.post(settings, { data: { enabled: false } });
  expect(shut.status(), await shut.text()).toBe(200);

  const begun = await page.request.post('/api/v1/auth/login/passkey/begin', {
    headers: { 'Accept-Language': 'pt-PT' },
  });
  expect(begun.status()).toBe(403);
  const detail = `${(await begun.json())?.detail ?? ''}`;
  expect(detail).not.toBe('');
  const root = join(__dirname, '..');
  const portuguese = ['modules', 'kit', 'ui', 'apps'].flatMap(top => portugueseCatalogues(join(root, top)))
    .flatMap(path => Object.values(JSON.parse(readFileSync(path, 'utf8')) as Record<string, { translation?: string }>))
    .map(entry => entry.translation ?? '');
  expect(portuguese.length, 'no pt-PT catalogue was read').toBeGreaterThan(0);
  expect(portuguese, `the refusal "${detail}" is not a sentence of any pt-PT catalogue`).toContain(detail);
});
