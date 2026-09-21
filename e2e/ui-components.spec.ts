import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// Exercise the actual exported constructors and the scripts the Go owner ships.
// Each example gets its own document: a live modal must make its background inert.
const root = resolve(__dirname, '..');
const snapshot = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
  cwd: root, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024,
}));
const scripts = readFileSync(resolve(root, 'ui/ui.go'), 'utf8')
  .match(/var Controllers = \[\]string\{([\s\S]*?)\}/)![1]
  .match(/"[^"]+\.js"/g)!
  .map(name => `<script defer src="/app/admin/assets/js/${JSON.parse(name)}"></script>`).join('');

const pageFaults = new WeakMap<Page, string[]>();
test.beforeEach(({ page }) => {
  const faults: string[] = [];
  pageFaults.set(page, faults);
  page.on('pageerror', error => faults.push(error.message));
  page.on('response', response => { if (response.request().resourceType() === 'script' && response.status() !== 200) faults.push(`${response.status()} ${response.url()}`); });
});
test.afterEach(({ page }) => expect(pageFaults.get(page)).toEqual([]));

// theme, when given, is served on the document instead of being switched after
// it is styled. That is what a person sees: the shell writes a stored choice
// before first paint. Switching it afterwards starts the colour transition
// every themed utility declares, and until a frame advances that transition the
// outgoing theme's foreground is still the computed one over the incoming
// theme's background — a state no reader ever reads, and a measurement no
// check should take.
async function specimen(page: Page, id: string, before = '', after = '', props?: Record<string, unknown>, theme = '') {
  const source = props ? JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', id, '--props'], {
    cwd: root, encoding: 'utf8', input: JSON.stringify(props), maxBuffer: 8 * 1024 * 1024,
  })) : snapshot;
  const example = source.examples.find((entry: { id: string }) => entry.id === id);
  if (!example) throw new Error(`Unknown source example: ${id}`);
  await page.route('**/__ui_specimen', route => route.fulfill({
    contentType: 'text/html',
    body: `<!doctype html><html lang="en"${theme ? ` data-theme="${theme}"` : ''}><head><title>Component interaction</title>
      <style>${snapshot.css}</style>${scripts}</head><body>
      ${before}${example.html}${after}</body></html>`,
  }));
  await page.goto('/__ui_specimen');
}

test('panel tabs skip disabled items, navigate by keyboard, and load a panel once', async ({ page }) => {
  let requests = 0;
  await page.route('**/activity', route => {
    requests++;
    return route.fulfill({ contentType: 'text/html', body: '<p>Recent activity</p>' });
  });
  await specimen(page, 'pk-ui.component.tabs/vertical-pills');
  const profile = page.getByRole('tab', { name: /Profile/ });
  const activity = page.getByRole('tab', { name: 'Activity' });
  await profile.focus();
  await page.keyboard.press('ArrowDown');
  await expect(activity).toBeFocused();
  await expect(activity).toHaveAttribute('aria-selected', 'true');
  await expect(profile).toHaveAttribute('tabindex', '-1');
  await expect(page.getByRole('tabpanel')).toContainText('Recent activity');
  await page.keyboard.press('Home');
  await expect(profile).toBeFocused();
  await expect(page.getByRole('tabpanel')).toContainText('Profile panel');
  await activity.click();
  await expect(page.getByRole('tabpanel')).toContainText('Recent activity');
  expect(requests).toBe(1);
  await page.keyboard.press('End');
  await expect(activity).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(profile).toBeFocused();
});

test('an open modal traps focus, dismisses with Escape and Close, and restores its trigger', async ({ page }) => {
  await specimen(page, 'pk-ui.component.modal/default',
    '<button id="opener" data-modal-open="confirm-modal">Open archive dialog</button>',
    '<button id="outside">Outside action</button>');
  const dialog = page.getByRole('dialog', { name: 'Archive' });
  const close = dialog.getByRole('button', { name: 'Close', exact: true });
  await expect(close).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(close).toBeFocused();
  await page.locator('#outside').evaluate((element: HTMLElement) => element.focus());
  await expect(close).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  const opener = page.getByRole('button', { name: 'Open archive dialog' });
  await opener.click();
  await expect(dialog).toBeVisible();
  await close.click();
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();
  await opener.click();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();
});

test('explicit modal dismissal restrictions survive keyboard and backdrop activation', async ({ page }) => {
  await specimen(page, 'pk-ui.component.modal/undismissable');
  const dialog = page.getByRole('dialog', { name: 'Required decision' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeVisible();
  await dialog.click({ position: { x: 2, y: 2 } });
  await expect(dialog).toBeVisible();
});

test('responsive composition changes columns while headings retain their semantic level', async ({ page }) => {
  await specimen(page, 'pk-ui.component.grid/responsive');
  for (const [width, count] of [[320, 1], [768, 2], [1280, 3]]) {
    await page.setViewportSize({ width, height: 900 });
    expect(await page.locator('.grid').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length)).toBe(count);
  }
  await specimen(page, 'pk-ui.component.heading/display');
  const heading = page.getByRole('heading', { name: 'A section with presence', level: 2 });
  await expect(heading).toHaveCSS('font-size', '30px');
});

test('deferred dialogs open on swap, take the panel name, and clear on dismissal', async ({ page }) => {
  const panel = snapshot.examples.find((entry: { id: string }) => entry.id === 'pk-ui.component.modal/panel');
  let requests = 0;
  await page.route('**/__modal_panel', route => {
    requests++;
    return route.fulfill({ contentType: 'text/html', body: panel.html });
  });
  // The application defaults to outerHTML swaps; a deferred dialog keeps its root.
  await specimen(page, 'pk-ui.component.modal/deferred', '<button hx-get="/__modal_panel" hx-target="#server-modal" hx-swap="innerHTML">Load dialog</button>');
  const opener = page.getByRole('button', { name: 'Load dialog' });
  const dialog = page.getByRole('dialog', { name: 'Panel only' });
  for (let i = 0; i < 2; i++) {
    await opener.click();
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Close' })).toBeFocused();
    await dialog.getByRole('button', { name: 'Close' }).click();
    await expect(page.locator('#server-modal')).toBeEmpty();
    await expect(opener).toBeFocused();
  }
  expect(requests).toBe(2);
});

async function modalEditor(page: Page, swap: string, clearOnClose = true) {
  const formSource = snapshot.examples.find((entry: { id: string }) => entry.id === 'pk-ui.component.modal/form').html;
  await specimen(page, 'pk-ui.component.modal/default',
    '<button data-modal-open="confirm-modal">Edit record</button>', '', { open: false, clearOnClose });
  const fields = '<label for="record-name">Name</label><input id="record-name" name="name" value="Draft"><button type="submit">Save record</button>';
  // Keep ModalForm's exported contract when composing the request fixture.
  await page.evaluate(({ formSource, fields, swap }) => {
    const body = document.querySelector('[data-modal-body]')!;
    body.innerHTML = formSource;
    const form = body.querySelector('form')!;
    form.innerHTML = fields;
    form.id = 'record-form';
    form.setAttribute('hx-post', '/__save_modal');
    form.setAttribute('hx-swap', swap);
    (window as any).htmx.process(form);
  }, { formSource, fields, swap });
  const opener = page.getByRole('button', { name: 'Edit record', exact: true });
  await opener.click();
  return { fields, opener, dialog: page.getByRole('dialog', { name: 'Archive', exact: true }), form: page.locator('#record-form') };
}

for (const swap of ['innerHTML', 'outerHTML']) {
  test(`modal saves retain validation and failures, then close after success with ${swap}`, async ({ page }) => {
    const { fields, opener, dialog, form } = await modalEditor(page, swap);
    const formHTML = await form.evaluate(element => element.outerHTML);
    let status = 422;
    await page.route('**/__save_modal', route => {
      let body = '<p>Saved</p>';
      if (status === 422) {
        body = swap === 'outerHTML'
          ? formHTML.replace('</form>', '<p role="alert">Name needs correction</p></form>')
          : fields + '<p role="alert">Name needs correction</p>';
      }
      return route.fulfill({ status, contentType: 'text/html', body });
    });
    let completed = 0;
    await page.exposeFunction('modalRequestCompleted', () => { completed++; });
    await page.evaluate(() => document.addEventListener('htmx:afterRequest', () => (window as any).modalRequestCompleted()));
    await form.getByRole('button', { name: 'Save record' }).click();
    await expect.poll(() => completed).toBe(1);
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('alert')).toHaveText('Name needs correction');
    await dialog.getByLabel('Name').fill('Corrected draft');
    for (const failure of [403, 500]) {
      status = failure;
      await form.getByRole('button', { name: 'Save record' }).click();
      await expect.poll(() => completed).toBe(failure === 403 ? 2 : 3);
      await expect(dialog).toBeVisible();
      await expect(dialog.getByLabel('Name')).toHaveValue('Corrected draft');
    }
    status = 200;
    await form.getByRole('button', { name: 'Save record' }).click();
    await expect.poll(() => completed).toBe(4);
    await expect(dialog).toBeHidden();
    await expect(page.locator('#confirm-modal')).toBeEmpty();
    await expect(opener).toBeFocused();
  });
}

test('a late save cannot close a reopened modal, while its own 204 save can', async ({ page }) => {
  const { opener, dialog, form } = await modalEditor(page, 'outerHTML', false);
  let release!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  let sent = 0;
  let completed = 0;
  await page.exposeFunction('modalRequestCompleted', () => { completed++; });
  await page.evaluate(() => document.addEventListener('htmx:afterRequest', () => (window as any).modalRequestCompleted()));
  await page.route('**/__save_modal', async route => {
    sent++;
    await pending;
    await route.fulfill({ status: 204 });
  });
  await form.getByRole('button', { name: 'Save record' }).click();
  await expect.poll(() => sent).toBe(1);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await opener.click();
  await dialog.getByLabel('Name').fill('New opening draft');
  release();
  await expect.poll(() => completed).toBe(1);
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel('Name')).toHaveValue('New opening draft');
  await form.getByRole('button', { name: 'Save record' }).click();
  await expect.poll(() => completed).toBe(2);
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();
});

test('field and status enhancements reflect their native state', async ({ page }) => {
  await specimen(page, 'pk-ui.component.checkbox/indeterminate');
  const checkbox = page.getByRole('checkbox', { name: 'Some' });
  await expect(checkbox).toHaveJSProperty('indeterminate', true);
  await checkbox.press('Space');
  await expect(checkbox).toHaveJSProperty('indeterminate', false);
  await expect(checkbox).toBeChecked();
  await expect(page.locator('[data-checkbox-checkmark]')).toBeVisible();
  await expect(page.locator('[data-checkbox-bar]')).toBeHidden();
  await checkbox.press('Space');
  await expect(page.locator('[data-checkbox-checkmark]')).toBeHidden();
  await expect(page.locator('[data-checkbox-box]')).toHaveAttribute('data-state', 'unchecked');
  await specimen(page, 'pk-ui.component.alert/dismissible');
  await page.getByRole('button', { name: 'Dismiss notification' }).click();
  await expect(page.getByText('Dismiss me.')).toHaveCount(0);
  await specimen(page, 'pk-ui.component.textarea/default');
  await page.getByRole('textbox', { name: 'Body' }).fill('Two 😀');
  await expect(page.locator('[data-textarea-counter-target="display"]')).toHaveText('5 / 500');
  await specimen(page, 'pk-ui.component.textarea/autoresize', '', '', { disabled: false });
  const input = page.getByRole('textbox', { name: 'Details' });
  const before = (await input.boundingBox())!.height;
  await input.fill(Array.from({ length: 10 }, () => 'More detail').join('\n'));
  expect((await input.boundingBox())!.height).toBeGreaterThan(before);
  await specimen(page, 'pk-ui.component.button/primary', '', '', { hidden: true });
  await expect(page.locator('[data-component="button"]')).toBeHidden();
});

test('the enhanced source examples pass automated accessibility checks in both themes', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const theme of ['light', 'dark']) {
    for (const id of ['pk-ui.component.button/primary', 'pk-ui.component.tabs/vertical-pills', 'pk-ui.component.modal/default', 'pk-ui.component.hero/default']) {
      await specimen(page, id, '', '', undefined, theme);
      const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
      expect(results.violations, `${id} in ${theme}`).toEqual([]);
    }
  }
});

test('confirmation cancels and reopens without losing handlers or sending duplicate requests', async ({ page }) => {
  let requests = 0;
  await page.route('**/__confirm-action', route => {
    requests++;
    return route.fulfill({ status: 204 });
  });
  await specimen(page, 'pk-ui.component.confirmdialog/default',
    '<button hx-post="/__confirm-action" hx-confirm="Archive this item?" hx-swap="none" data-confirm-label="Archive">Archive item</button>' +
    '<button data-confirm="Continue?">Default confirmation</button>');
  const trigger = page.getByRole('button', { name: 'Archive item', exact: true });
  const dialog = page.getByRole('dialog', { name: 'Delete this row?' });
  await trigger.click();
  await dialog.getByRole('button', { name: 'Keep' }).click();
  await trigger.click();
  await page.keyboard.press('Escape');
  expect(requests).toBe(0);
  await trigger.click();
  await dialog.getByRole('button', { name: 'Archive', exact: true }).click();
  await expect.poll(() => requests).toBe(1);
  await expect(trigger).toBeFocused();
  await page.getByRole('button', { name: 'Default confirmation' }).click();
  await expect(dialog.getByRole('button', { name: 'Delete', exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Keep' }).click();
  expect(requests).toBe(1);
});

test('confirmation preview opens, cancels, accepts, and restores focus with a custom ID', async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL('**/app');
  const props = encodeURIComponent(JSON.stringify({ AcceptLabel: 'Proceed' }));
  await page.goto(`/app/admin/_gallery/preview?example=pk-ui.component.confirmdialog/default&props=${props}`);
  const opener = page.getByRole('button', { name: 'Open confirmation' });
  const dialog = page.getByRole('dialog', { name: 'Delete this row?' });
  // ID is a trusted Go property, not a browser-editable prop. The controller
  // must still find the single page dialog when its owner gives it another ID.
  await page.locator('dialog').evaluate(element => { element.id = 'custom-confirm'; });
  for (const decision of ['Escape', 'Keep', 'Proceed']) {
    await opener.focus();
    await page.keyboard.press('Enter');
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Keep' })).toBeFocused();
    if (decision === 'Escape') await page.keyboard.press('Escape');
    else await dialog.getByRole('button', { name: decision }).click();
    await expect(dialog).toBeHidden();
    await expect(opener).toBeFocused();
  }
});

test('gallery edits use typed properties, viewport controls, and an isolated live preview', async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
  await page.goto('/app/admin/_gallery?example=pk-ui.component.button/primary');
  const preview = page.frameLocator('iframe');
  await expect(preview.getByRole('button', { name: 'Save', exact: true })).toBeVisible();
  await page.getByLabel('label', { exact: true }).fill('Preview purchase');
  await expect(preview.getByRole('button', { name: 'Preview purchase', exact: true })).toBeVisible();
  await expect(page.getByLabel('label', { exact: true })).toBeFocused();
  await page.getByLabel('variant', { exact: true }).selectOption('outline');
  await page.getByRole('combobox', { name: 'Preview theme', exact: true }).selectOption('dark');
  await page.getByRole('combobox', { name: 'Preview width', exact: true }).selectOption('320');
  await expect(preview.getByRole('button', { name: 'Preview purchase', exact: true })).toHaveAttribute('data-variant', 'outline');
  await expect(preview.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.locator('iframe')).toHaveCSS('width', '320px');
  await page.getByText('Go properties', { exact: true }).click();
  await expect(page.getByLabel('Copyable Go properties')).toHaveValue(/Label:\s+"Preview purchase"/);
  await page.getByRole('button', { name: 'Copy Go properties' }).click();
  await expect(page.locator('[data-gallery-status]')).toContainText(/copied|selected/);
  const errors = await new AxeBuilder({ page }).exclude('iframe').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
  expect(errors.violations).toEqual([]);

  // Response headers protect somebody who opens the URL outside the iframe too.
  const source = await page.locator('iframe').getAttribute('src');
  const response = await page.goto(source!);
  expect(response!.headers()['content-security-policy']).toContain('sandbox allow-scripts;');
  expect(response!.headers()['cache-control']).toBe('no-store');
  expect(await page.evaluate(async () => {
    try { await fetch('/api/v1/app/resources'); return false; } catch { return true; }
  })).toBe(true);
  expect(await page.evaluate(() => {
    try { localStorage.getItem('platformkit-theme'); return false; } catch { return true; }
  })).toBe(true);
});

test('gallery controls retain invalid drafts and recover without losing the preview', async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);
  await page.goto('/app/admin/_gallery?example=pk-ui.component.select/default');
  const preview = page.frameLocator('iframe');
  const options = page.getByLabel('options (JSON)', { exact: true });
  await options.fill('[');
  await expect(options).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByRole('status')).toContainText('Enter valid JSON for options');
  await expect(preview.getByRole('combobox', { name: 'Kind', exact: true })).toHaveValue('post');
  await expect(options).toBeFocused();
  await options.fill('[{"label":"Latest option","value":"post"}]');
  await expect(preview.getByRole('combobox', { name: 'Kind', exact: true }).locator('option')).toHaveText(['Latest option']);
  await expect(options).not.toHaveAttribute('aria-invalid');

  await page.goto('/app/admin/_gallery?example=pk-ui.component.button/primary');
  await page.getByLabel('disabled', { exact: true }).selectOption('true');
  await expect(preview.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await page.getByLabel('disabled', { exact: true }).selectOption('false');
  await expect(preview.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();

  // A failed render preserves the last good component and the editable draft.
  await page.route('**/app/admin/_gallery?**', route => route.fulfill({ status: 422, body: 'Invalid properties' }), { times: 1 });
  await page.getByLabel('label', { exact: true }).fill('Retry purchase');
  await expect(page.getByRole('status')).toContainText('Preview could not be updated (422)');
  await expect(preview.getByRole('button', { name: 'Save', exact: true })).toBeVisible();
  await expect(page.getByLabel('label', { exact: true })).toHaveValue('Retry purchase');
  await page.getByRole('button', { name: 'Apply preview' }).click();
  await expect(preview.getByRole('button', { name: 'Retry purchase', exact: true })).toBeVisible();
  await page.reload();
  await expect(preview.getByRole('button', { name: 'Retry purchase', exact: true })).toBeVisible();
});
