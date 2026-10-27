import { expect, test, type Browser, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { signIn } from './steps/kernel';
import { signInForContent } from './steps/content';
import { imageBytes, editingSurface } from './steps/richtext';

const artifact = JSON.parse(readFileSync(`${__dirname}/../ui/screens/testdata/screens.json`, 'utf8'));
const taskView = (kind: string) => `app/task/resource/task/${kind}`;

async function anchors(page: Page, id: string) {
  const view = artifact.views.find((v: { id: string }) => v.id === id);
  expect(view, `published view ${id}`).toBeTruthy();
  await expect(page.locator(`[data-screen-view="${id}"]`)).toHaveCount(1);
  const expected = view.anchors.map((a: { name: string }) => `${id}#${a.name}`).sort();
  const actual = await page.locator('[data-screen-anchor]').evaluateAll((nodes, prefix) =>
    nodes.map(n => n.getAttribute('data-screen-anchor')!).filter(a => a.startsWith(prefix)).sort(), `${id}#`);
  expect(actual).toEqual(expected);
  for (const ref of expected) {
    const control = page.locator(`[data-screen-anchor="${ref}"]`);
    await expect(control).toHaveCount(1);
    await control.scrollIntoViewIfNeeded();
    await expect(control).toBeVisible();
    expect(await control.evaluate((n, owner) => n.isConnected && n.closest('[data-screen-view]')?.getAttribute('data-screen-view') === owner, id)).toBe(true);
    const box = await control.boundingBox();
    expect(box).not.toBeNull();
    expect(Number.isFinite(box!.width) && box!.width > 0).toBe(true);
    expect(Number.isFinite(box!.height) && box!.height > 0).toBe(true);
  }
}

async function person(admin: Page, browser: Browser, role: string) {
  const email = `anchors-${role}-${Date.now()}@e2e.test`;
  const password = 'Screen fixture 9! stable';
  const invited = await admin.request.post('/api/v1/user/invitations', {
    data: { email, displayName: `Screen ${role}`, roles: [role] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const { id } = await invited.json();
  const activated = await admin.request.post(`/api/v1/user/users/${id}/set-password`, { data: { password } });
  expect(activated.status(), await activated.text()).toBe(200);
  const context = await browser.newContext();
  const page = await context.newPage();
  await signInForContent(page, email, password);
  return { context, page, id };
}

test('published generic kinds have unique visible anchors and grants hide their controls', async ({ page, browser }) => {
  test.setTimeout(120_000);
  const kinds = [...new Set(artifact.views.filter((v: { id: string }) => v.id.includes('/resource/')).map((v: { kind: string }) => v.kind))].sort();
  expect(kinds).toEqual(['command', 'create', 'detail', 'edit', 'list']);
  await page.goto('/app/admin/login');
  await anchors(page, 'app/admin/custom/login');
  await signIn(page);
  const writer = await person(page, browser, 'coordinator');
  const reader = await person(page, browser, 'observer');
  let id = '';
  try {
    await writer.page.goto('/app/task/tasks');
    await anchors(writer.page, taskView('list'));
    await expect(writer.page.locator('tbody [data-screen-anchor]')).toHaveCount(0);
    await writer.page.locator(`[data-screen-anchor="${taskView('list')}#action.create"]`).click();
    await anchors(writer.page, taskView('create'));
    await writer.page.locator('form[data-screen-view]').evaluate((form: HTMLFormElement) => { form.noValidate = true; });
    await writer.page.locator(`[data-screen-anchor="${taskView('create')}#action.submit"]`).click();
    await expect(writer.page.getByLabel('Title')).toHaveAttribute('aria-invalid', 'true');
    await anchors(writer.page, taskView('create'));
    await writer.page.getByLabel('Title').fill('Screen anchor specimen');
    await writer.page.locator(`[data-screen-anchor="${taskView('create')}#action.submit"]`).click();
    await expect(writer.page).toHaveURL(/\/app\/task\/tasks\/[0-9a-f-]{36}$/);
    id = writer.page.url().split('/').at(-1)!;
    await anchors(writer.page, taskView('detail'));
    await anchors(writer.page, taskView('command/resolve'));
    await anchors(writer.page, taskView('command/assign'));
    await writer.page.locator(`[data-screen-anchor="${taskView('detail')}#action.edit"]`).click();
    await anchors(writer.page, taskView('edit'));
    await writer.page.getByLabel('Title').fill('Screen anchor edited');
    await writer.page.locator(`[data-screen-anchor="${taskView('edit')}#action.submit"]`).click();
    await anchors(writer.page, taskView('detail'));
    await writer.page.locator(`[data-screen-anchor="${taskView('command/resolve')}#field.resolution"]`).fill('Verified');
    await writer.page.locator(`[data-screen-anchor="${taskView('command/resolve')}#command.resolve"]`).click();
    await anchors(writer.page, taskView('detail'));
    await reader.page.goto('/app/task/tasks');
    await expect(reader.page.locator(`[data-screen-anchor="${taskView('list')}#action.create"]`)).toHaveCount(0);
    await reader.page.goto(`/app/task/tasks/${id}`);
    await expect(reader.page.locator('[data-screen-anchor]')).toHaveCount(0);
    await expect(reader.page.locator('[data-screen-view*="/command/"]')).toHaveCount(0);
    const refused = await reader.page.goto(`/app/task/tasks/${id}/edit`);
    expect(refused?.status()).toBe(403);
  } finally {
    if (id) await page.request.delete(`/api/v1/task/tasks/${id}`);
    await writer.context.close();
    await reader.context.close();
  }
});

test('custom access and administration controls keep their published anchors', async ({ page, browser }) => {
  test.setTimeout(90_000);
  for (const name of ['login', 'register', 'forgot', 'set-password', 'verify-email']) {
    const id = `app/admin/custom/${name}`;
    const view = artifact.views.find((v: { id: string }) => v.id === id);
    const route = view.routes.find((r: { method: string }) => r.method === 'GET');
    await page.goto(`${route.path}?token=screen-fixture`);
    await anchors(page, id);
  }
  await signIn(page);
  await anchors(page, 'app/admin/custom/dashboard');
  await page.goto('/app/auth/roles');
  await page.locator('summary').filter({ hasText: /^New role ·/ }).click();
  await anchors(page, 'app/admin/custom/roles');
  const other = await browser.newContext();
  try {
    await signIn(await other.newPage());
    await page.goto('/app/auth/sessions');
    await anchors(page, 'app/admin/custom/sessions');
    await expect(page.locator('tbody [data-screen-anchor]')).toHaveCount(0);
  } finally { await other.close(); }
});

test('rich text keeps one visible anchor with its editor enhanced or unavailable', async ({ page, browser }) => {
  await signIn(page);
  await page.goto('/app/content/contents/new');
  await expect(page.locator(editingSurface)).toBeVisible();
  await anchors(page, 'app/content/resource/content/create');
  await expect(page.locator('textarea[data-screen-anchor]')).toHaveCount(0);
  const fallback = await browser.newContext({ storageState: await page.context().storageState() });
  try {
    const plain = await fallback.newPage();
    await plain.route('**/js/richtext.js', route => route.abort());
    await plain.goto('/app/content/contents/new');
    await expect(plain.getByLabel('Body', { exact: true })).toBeVisible();
    await anchors(plain, 'app/content/resource/content/create');
  } finally { await fallback.close(); }
});

test('the selected media panel publishes each unique field and action once', async ({ page }) => {
  await signIn(page);
  const uploaded = await page.request.post('/api/v1/file/files?image=true', {
    multipart: { file: { name: 'screen-anchor.png', mimeType: 'image/png', buffer: imageBytes } },
  });
  expect(uploaded.status(), await uploaded.text()).toBe(201);
  const { id } = await uploaded.json();
  try {
    await page.goto(`/app/file/files?file=${id}&q=screen-anchor`);
    await anchors(page, 'app/file/custom/library');
    await expect(page.locator('[data-screen-anchor$="#field.decorative"]')).toHaveJSProperty('tagName', 'LABEL');
  } finally { await page.request.delete(`/api/v1/file/files/${id}`); }
});

test('singleton views and commands without arguments retain visible targets', async ({ page, browser }) => {
  await signIn(page);
  await page.goto('/app/site/settings');
  await anchors(page, 'app/site/resource/settings/detail');
  await page.locator('[data-screen-anchor="app/site/resource/settings/detail#action.edit"]').click();
  await anchors(page, 'app/site/resource/settings/edit');
  await page.locator('[data-screen-anchor="app/site/resource/settings/edit#action.cancel"]').click();
  await expect(page).toHaveURL(/\/app\/site\/settings$/);
  const subject = await person(page, browser, 'observer');
  try {
    await page.goto(`/app/user/users/${subject.id}`);
    await anchors(page, 'app/user/resource/user/command/deactivate');
  } finally { await subject.context.close(); }
});
