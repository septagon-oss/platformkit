import { expect, test, type Page } from '@playwright/test';

async function signIn(page: Page) {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
}

async function openStory(page: Page, title: string, name = 'Default') {
  const response = await page.request.get('/app/admin/_gallery/storybook/index.json');
  expect(response.status()).toBe(200);
  const stories = Object.values((await response.json()).entries) as { id: string; title: string; name: string }[];
  const story = stories.find(story => story.title === title && story.name === name);
  expect(story).toBeTruthy();
  await page.goto('/app/admin/_gallery/storybook/index.html?path=/story/' + story!.id);
  return page.frameLocator('#storybook-preview-iframe').frameLocator('iframe');
}

test('real Storybook serves its private index, updates Go controls, and runs confirmation', async ({ page, request }) => {
  const base = '/app/admin/_gallery/storybook/';
  const anonymous = await request.get(base + 'index.json');
  expect(anonymous.status()).toBe(403);
  await signIn(page);
  const response = await page.request.get(base + 'index.json');
  expect(response.status()).toBe(200);
  expect(response.headers()['cache-control']).toBe('no-store');
  const index = await response.json();
  const stories = Object.values(index.entries) as { id: string; title: string; name: string; type: string }[];
  expect(stories.length).toBeGreaterThan(100);
  const primary = stories.find(story => story.id.startsWith('pk-ui-component-button-primary-'));
  expect(primary).toBeTruthy();
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(base + 'index.html?path=/story/' + primary!.id);
  const canvas = page.frameLocator('#storybook-preview-iframe');
  const specimen = canvas.frameLocator('iframe');
  await expect(specimen.getByRole('button', { name: 'Save', exact: true })).toBeVisible();
  await page.getByRole('tab', { name: /Controls/ }).click();
  await page.locator('#control-label').fill('Saved from Storybook');
  await expect(specimen.getByRole('button', { name: 'Saved from Storybook', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'light' }).click();
  await page.getByText('dark', { exact: true }).click();
  await expect(specimen.locator('html')).toHaveAttribute('data-theme', 'dark');
  const confirm = stories.find(story => story.title === 'Overlay/ConfirmDialog');
  expect(confirm).toBeTruthy();
  await page.goto(base + 'index.html?path=/story/' + confirm!.id);
  await specimen.getByRole('button', { name: 'Open confirmation' }).click();
  const dialog = specimen.getByRole('dialog', { name: 'Delete this row?' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Keep' }).click();
  await expect(dialog).toBeHidden();
  expect(errors).toEqual([]);
});

test('SkipLink demonstrates keyboard focus and moves past navigation into content', async ({ page }) => {
  await signIn(page);
  const specimen = await openStory(page, 'Frame/SkipLink');
  const link = specimen.getByRole('link', { name: 'Skip to content' });
  const focus = specimen.getByRole('button', { name: 'Focus skip link' });
  const main = specimen.getByRole('main');
  await expect(focus).toBeVisible();
  await expect(link).toHaveCSS('clip', 'rect(0px, 0px, 0px, 0px)');
  await focus.focus();
  await page.keyboard.press('Shift+Tab');
  await expect(link).toBeFocused();
  await expect(link).toHaveCSS('clip', 'auto');
  expect((await link.boundingBox())!.height).toBeGreaterThan(24);
  await page.keyboard.press('Enter');
  await expect(main).toHaveCount(1);
  await expect(main).toBeFocused();
  await expect(main).toContainText('Sample page content');
  await page.keyboard.press('Tab');
  await expect(main.getByLabel('Sample content field')).toBeFocused();
  await expect(link).toHaveCSS('clip', 'rect(0px, 0px, 0px, 0px)');
  await focus.click();
  await expect(link).toBeFocused();
});

test('Sidebar stories are visible on a laptop and explain responsive hiding', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await signIn(page);
  const specimen = await openStory(page, 'Navigation/Sidebar');
  const sidebar = specimen.getByRole('complementary');
  await expect(sidebar).toBeVisible();
  // The current item of the sidebar story is the example's own
  // {Label: "New user", Href: "/app/user/users/new"}, which the kernel serves as a
  // generated screen; the claim under test is that the example's `current` is the
  // item marked current, so the name follows the specimen and the attribute does not.
  await expect(sidebar.getByRole('link', { name: 'New user', exact: true })).toHaveAttribute('aria-current', 'page');
  await page.getByRole('button', { name: 'Viewport size' }).click();
  await page.getByText('Small mobile', { exact: true }).click();
  await expect(sidebar).toBeHidden();
  await expect(specimen.getByText(/The admin sidebar is hidden at this viewport width/)).toBeVisible();
  await page.getByRole('button', { name: 'Viewport size' }).click();
  await page.getByText('Desktop', { exact: true }).click();
  await expect(sidebar).toBeVisible();
  await expect(specimen.getByText(/The admin sidebar is hidden at this viewport width/)).toBeHidden();
  const collapsed = await openStory(page, 'Navigation/Sidebar', 'collapsed');
  await expect(collapsed.getByRole('complementary')).toBeVisible();
  await expect(collapsed.getByRole('complementary')).toHaveAttribute('data-sidebar-collapsed', 'true');
  const content = await openStory(page, 'Navigation/Sidebar', 'content flavor');
  await page.getByRole('button', { name: 'Viewport size' }).click();
  await page.getByText('Small mobile', { exact: true }).click();
  await expect(content.getByRole('navigation', { name: 'Documentation sections' })).toBeVisible();
  await expect(content.getByText(/The admin sidebar is hidden at this viewport width/)).toBeHidden();
});

test('Storybook preserves exact JSON integers, rejects malformed edits, and resets to source', async ({ page }) => {
  await page.goto('/health');
  const result = await page.evaluate(async () => {
    const { render } = await import('/app/admin/assets/js/storybook.js');
    const context = {
      initialArgs: { count: '9007199254740993' }, globals: { theme: 'dark' }, name: 'Exact count',
      parameters: { platformkit: { example: 'product/count', rawFields: ['count'] } },
    };
    const edited = render({ count: '9007199254740995' }, context) as HTMLIFrameElement;
    const reset = render({ count: '9007199254740993' }, context) as HTMLIFrameElement;
    const invalid = render({ count: '1,"other":true' }, context);
    return { edited: new URL(edited.src).searchParams.get('props'),
      reset: new URL(reset.src).searchParams.get('props'),
      invalidRole: invalid.getAttribute('role'), invalidMessage: invalid.textContent };
  });
  expect(result.edited).toBe('{"count":9007199254740995}');
  expect(result.reset).toBe('{}');
  expect(result.invalidRole).toBe('alert');
  expect(result.invalidMessage).toBe('count must contain one valid JSON value.');
});
