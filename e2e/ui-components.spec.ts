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

test('a DataList loading transition reserves its final geometry', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await specimen(page, 'pk-ui.component.data-list/loading-en', '', '<p id="review-next">Content after the list</p>');
  await page.waitForLoadState('networkidle');
  const before = await page.locator('#review-next').boundingBox();
  expect(before).not.toBeNull();
  const ready = snapshot.examples.find((entry: { id: string }) => entry.id === 'pk-ui.component.data-list/default-en').html;
  await page.locator('#list-en').evaluate((list, html) => {
    list.outerHTML = html;
    document.dispatchEvent(new CustomEvent('htmx:afterSwap', { bubbles: true }));
  }, ready);
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const after = await page.locator('#review-next').boundingBox();
  expect(after).not.toBeNull();
  expect(Math.abs(after!.y - before!.y), 'loading should reserve space instead of moving the following content').toBeLessThanOrEqual(1);
});

for (const kind of ['side-panel', 'detail-sheet']) test(`a delayed ${kind} swap cannot reopen a closed selection`, async ({ page }) => {
  const response = snapshot.examples.find((entry: { id: string }) => entry.id === `pk-ui.component.${kind}/default-en`).html;
  await page.route('**/review-details/a', route => route.fulfill({ contentType: 'text/html', body: response }));
  await specimen(page, `pk-ui.component.${kind}/closed-en`,
    '<a id="review-open" href="/items/a" hx-get="/review-details/a" hx-target="#detail-en" hx-swap="outerHTML swap:500ms" data-detail-open="detail-en">Read item</a>');
  await page.evaluate(() => {
    document.addEventListener('htmx:afterRequest', () => {
      document.querySelector<HTMLAnchorElement>('[data-detail-close]')?.click();
      document.body.dataset.reviewRequestEnded = 'true';
    }, { once: true });
  });
  await page.locator('#review-open').click();
  await expect(page.locator('body')).toHaveAttribute('data-review-request-ended', 'true');
  await expect(page.locator('#detail-en')).toBeHidden();
  // Wait beyond the caller's declared swap delay, not for defect-specific copy.
  await page.waitForTimeout(650);
  await expect(page.locator('#detail-en')).toBeHidden();
});

for (const kind of ['side-panel', 'detail-sheet']) test(`modified ${kind} link activation retains native navigation`, async ({ page }) => {
  await specimen(page, `pk-ui.component.${kind}/closed-en`,
    '<a id="review-open" href="/items/a" data-detail-open="detail-en">Read item</a>');
  const allowed = await page.locator('#review-open').evaluate(link => link.dispatchEvent(
    new MouseEvent('click', { bubbles: true, cancelable: true, ctrlKey: true })));
  expect(allowed, 'Ctrl-click must keep the link default instead of opening a panel in this tab').toBe(true);
  await expect(page.locator('#detail-en')).toBeHidden();
});

test.beforeEach(({ page }) => {
  const faults: string[] = [];
  pageFaults.set(page, faults);
  page.on('pageerror', error => faults.push(error.message));
  // 304 is a script the browser already holds, revalidated against its content ETag (docs/cache.md):
  // a success, not a fault. Anything else that is not 200 is a script that did not load.
  page.on('response', response => { if (response.request().resourceType() === 'script' && ![200, 304].includes(response.status())) faults.push(`${response.status()} ${response.url()}`); });
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

for (const locale of ['en', 'pt-PT']) {
  for (const theme of ['light', 'dark']) {
    test(`rich states: ${locale} / ${theme} names, contrast, targets, reflow and reduced motion`, async ({ page }) => {
      test.setTimeout(180_000);
      await page.emulateMedia({ reducedMotion: 'reduce' });
      const entries = snapshot.examples.filter((entry: { id: string }) =>
        /^pk-ui.component\.(empty-state|notice|skeleton|table-skeleton)\//.test(entry.id) && entry.id.endsWith(`-${locale}`));
      expect(entries).toHaveLength(21);
      for (const entry of entries) {
        await specimen(page, entry.id, '<main>', '</main>', undefined, theme);
        const audit = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
        expect(audit.violations, `${entry.id} / ${theme}`).toEqual([]);
        for (const width of [320, 360, 768, 1024, 1440]) {
          await page.setViewportSize({ width, height: 900 });
          const geometry = await page.evaluate(() => ({
            overflow: document.documentElement.scrollWidth > innerWidth,
            small: [...document.querySelectorAll('button, a[href]')].filter(el => {
              const box = el.getBoundingClientRect();
              return box.width > 0 && box.height > 0 && (box.width < 44 || box.height < 44);
            }).map(el => el.outerHTML),
          }));
          expect(geometry, `${entry.id} / ${theme} / ${width}`).toEqual({ overflow: false, small: [] });
        }
        await page.setViewportSize({ width: 320, height: 900 });
        await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${entry.id} / 200%`).toBe(true);
        if (/skeleton\//.test(entry.id)) {
          await expect(page.locator('[aria-busy="true"]')).toHaveCount(1);
          expect(await page.locator('[aria-hidden="true"]').first().evaluate(el => getComputedStyle(el).animationName)).toBe('none');
        }
      }
    });
  }
}

test('rich states: keyboard actions, one live region, persistent success and dismissal focus', async ({ page }) => {
  await specimen(page, 'pk-ui.component.notice/dismissible-pt-PT', '<button id="associated">Continue</button>', '<button>Next</button>');
  const notice = page.getByRole('status');
  await notice.evaluate(el => { (el as HTMLElement).dataset.alertReturnFocus = 'associated'; });
  await expect(page.locator('[aria-live]')).toHaveCount(1);
  await page.getByRole('button', { name: 'Continue' }).focus();
  await page.keyboard.press('Tab');
  const close = page.getByRole('button', { name: 'Fechar aviso' });
  await expect(close).toBeFocused();
  expect(await close.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none');
  await page.keyboard.press('Enter');
  await expect(notice).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Continue' })).toBeFocused();

  await specimen(page, 'pk-ui.component.notice/success-en');
  await expect(page.getByRole('status')).toHaveText('Your changes have been saved.');
  await expect(page.locator('[data-alert-close]')).toHaveCount(0);
  // The same region survives repeated outcome updates; no nested announcer.
  for (const message of ['First save complete', 'Second save complete']) {
    await page.getByRole('status').locator('p').evaluate((el, value) => { el.textContent = value; }, message);
    await expect(page.getByRole('status')).toHaveText(message);
    await expect(page.locator('[aria-live]')).toHaveCount(1);
  }

  await specimen(page, 'pk-ui.component.empty-state/with-action-en');
  await page.keyboard.press('Tab');
  const create = page.getByRole('link', { name: 'Create item' });
  await expect(create).toBeFocused();
  await create.hover();
  expect(await create.evaluate(el => el.matches(':hover'))).toBe(true);
  await page.mouse.down();
  expect(await create.evaluate(el => el.matches(':active'))).toBe(true);
  await page.mouse.move(0, 0);
  await page.mouse.up();
  await page.emulateMedia({ forcedColors: 'active' });
  await create.focus();
  await expect(create).toBeFocused();
  await specimen(page, 'pk-ui.component.notice/retry-pending-en');
  const pending = page.getByRole('link', { name: 'Try again' });
  await expect(pending).toHaveAttribute('aria-disabled', 'true');
  await expect(pending).toHaveAttribute('aria-busy', 'true');
  await expect(pending).not.toHaveAttribute('href');
});

test('rich states: native recovery without JavaScript and no inert dismissal', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  try {
    await specimen(page, 'pk-ui.component.notice/dismissible-en');
    await expect(page.getByText('Your changes have been saved.')).toBeVisible();
    await expect(page.locator('[data-alert-close]')).toBeHidden();
    await specimen(page, 'pk-ui.component.empty-state/with-action-pt-PT');
    await page.route('**/items/new', route => route.fulfill({ contentType: 'text/html', body: '<h1>Create</h1>' }));
    await page.getByRole('link', { name: 'Criar item' }).click();
    await expect(page.getByRole('heading', { name: 'Create' })).toBeVisible();
  } finally {
    await context.close();
  }
});

for (const locale of ['en', 'pt-PT']) for (const theme of ['light', 'dark']) {
  test(`data lists: ${locale} / ${theme} state and accessibility matrix`, async ({ page }) => {
    test.setTimeout(180_000);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    const entries = snapshot.examples.filter((entry: { id: string }) => entry.id.startsWith('pk-ui.component.data-list/') && entry.id.endsWith(`-${locale}`));
    expect(entries).toHaveLength(22);
    for (const entry of entries) {
      await specimen(page, entry.id, '<main>', '</main>', undefined, theme);
      const audit = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
      expect(audit.violations, entry.id).toEqual([]);
      for (const width of [320, 360, 768, 1024, 1440]) {
        await page.setViewportSize({ width, height: 900 });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${entry.id} / ${width}`).toBe(true);
        const small = await page.locator('button, a[href], input[type="checkbox"], summary').evaluateAll(elements => elements.filter(element => {
          const target = element.matches('input') ? element.closest('label')! : element;
          const box = target.getBoundingClientRect();
          return box.width > 0 && box.height > 0 && (box.width < 44 || box.height < 44);
        }).map(element => element.outerHTML));
        expect(small, `${entry.id} / ${width}`).toEqual([]);
      }
      await page.setViewportSize({ width: 320, height: 900 });
      await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${entry.id} / 200%`).toBe(true);
    }
  });
}

test('data lists: selection is native, includes collapsed groups and clears on a new result', async ({ page }) => {
  await specimen(page, 'pk-ui.component.data-list/grouped-en');
  const root = page.locator('#list-en');
  const all = page.getByRole('checkbox', { name: 'Select all items' }).first();
  await all.focus();
  await page.keyboard.press('Space');
  await expect(page.getByRole('status')).toHaveText('Two selected');
  expect(await page.locator('#bulk-en').evaluate((form: HTMLFormElement) => new FormData(form).getAll('selected'))).toEqual(['a', 'b']);
  await page.getByRole('checkbox', { name: 'Beta', exact: true }).uncheck();
  expect(await all.evaluate((input: HTMLInputElement) => input.indeterminate)).toBe(true);
  await expect(page.getByRole('status')).toHaveText('One selected');
  // A separately rendered instance can reuse row IDs without sharing selection.
  const second = snapshot.examples.find((entry: { id: string }) => entry.id === 'pk-ui.component.data-list/default-pt-PT');
  await page.evaluate(html => {
    document.body.insertAdjacentHTML('beforeend', html);
    document.dispatchEvent(new CustomEvent('htmx:afterSwap', { bubbles: true }));
  }, second.html);
  await expect(page.locator('#list-pt-PT [data-selection-count]')).toHaveText('Nenhum selecionado');
  // Replacement, not just a changed attribute, exercises snapshot identity.
  await root.evaluate(element => {
    const replacement = element.cloneNode(true) as HTMLElement;
    replacement.dataset.resultKey = 'page-two';
    element.replaceWith(replacement);
    replacement.dispatchEvent(new CustomEvent('htmx:afterSwap', { bubbles: true }));
  });
  await expect(page.locator('#list-en [data-selection-count]')).toHaveText('None selected');
  await expect(page.locator('#list-en [data-pk-select-row]:checked')).toHaveCount(0);
  await page.locator('#bulk-en').evaluate((form: HTMLFormElement) => form.reset());
  await expect(page.locator('#list-en [data-pk-select-row]:checked')).toHaveCount(0);
  await page.locator('#list-en summary').click();
  await page.locator('#list-en [data-pk-select="all"]:not([data-pk-select-row])').last().check();
  await expect(page.locator('#list-en [data-selection-count]')).toHaveText('Two selected');
  await page.getByRole('button', { name: 'Clear selection' }).click();
  await expect(page.locator('#list-en [data-selection-count]')).toHaveText('None selected');
  await expect(page.getByRole('checkbox', { name: 'Unavailable', exact: true }).first()).toBeDisabled();
  const checkedResult = snapshot.examples.find((entry: { id: string }) => entry.id === 'pk-ui.component.data-list/selection-some-en');
  await page.locator('#list-en').evaluate((element, html) => {
    const loading = document.createElement('section');
    loading.id = element.id;
    loading.dataset.component = 'data-list';
    element.replaceWith(loading);
    loading.dispatchEvent(new CustomEvent('htmx:afterSwap', { bubbles: true }));
    loading.outerHTML = html.replace('data-result-key="page-one"', 'data-result-key="page-three"');
    document.dispatchEvent(new CustomEvent('htmx:afterSwap', { bubbles: true }));
  }, checkedResult.html);
  await expect(page.locator('#list-en [data-selection-count]')).toHaveText('None selected');
  await expect(page.locator('#list-en [data-pk-select-row]:checked')).toHaveCount(0);
});

test('data lists: no-JavaScript selection submits only eligible checked rows', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  try {
    let submitted = '';
    await page.route('**/items/bulk', route => {
      submitted = route.request().postData() || '';
      return route.fulfill({ contentType: 'text/html', body: '<h1>Applied</h1>' });
    });
    await specimen(page, 'pk-ui.component.data-list/default-en');
    await expect(page.locator('[data-pk-select="all"]')).toBeHidden();
    await expect(page.getByRole('button', { name: 'Clear selection' })).toBeHidden();
    await page.getByRole('checkbox', { name: 'Alpha', exact: true }).check();
    await page.getByRole('button', { name: 'Apply to selection' }).click();
    await expect(page.getByRole('heading', { name: 'Applied' })).toBeVisible();
    expect(new URLSearchParams(submitted).getAll('selected')).toEqual(['a']);
  } finally { await context.close(); }
});

for (const locale of ['en', 'pt-PT']) for (const theme of ['light', 'dark']) {
  test(`detail panels: ${locale} / ${theme} states, targets and large text`, async ({ page }) => {
    test.setTimeout(180_000);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    const entries = snapshot.examples.filter((entry: { group: string; id: string }) => entry.group === 'Detail panels' && entry.id.endsWith(`-${locale}`));
    expect(entries).toHaveLength(25);
    for (const entry of entries) {
      await specimen(page, entry.id, '<main>', '</main>', undefined, theme);
      expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze()).violations, entry.id).toEqual([]);
      for (const width of [320, 360, 768, 1024, 1440]) {
        await page.setViewportSize({ width, height: 800 });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${entry.id} / ${width}`).toBe(true);
        const small = await page.locator('[data-detail-panel] button, [data-detail-panel] a[href], [data-detail-panel] input').evaluateAll(elements => elements.filter(el => {
          const box = el.getBoundingClientRect();
          return box.width > 0 && box.height > 0 && (box.width < 44 || box.height < 44);
        }).map(el => el.outerHTML));
        expect(small, entry.id).toEqual([]);
      }
      await page.setViewportSize({ width: 320, height: 600 });
      await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${entry.id} / 200%`).toBe(true);
      if (entry.id.includes('long-content')) {
        const field = page.getByRole('textbox');
        await field.focus();
        await field.scrollIntoViewIfNeeded();
        expect(await field.evaluate(el => {
          const box = el.getBoundingClientRect();
          const top = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
          return top === el;
        }), 'sticky actions cover the final field').toBe(true);
      }
    }
  });
}

test('detail panels: modal focus, side-panel independence, return fallback and busy Escape', async ({ page }) => {
  const before = '<main><h1 id="list-heading" tabindex="-1">Items</h1><a id="opener" href="/items/a" data-detail-open="detail-en">Read item</a><button id="outside">Outside</button>';
  await specimen(page, 'pk-ui.component.detail-sheet/closed-en', before, '</main>');
  await page.getByRole('link', { name: 'Read item' }).focus();
  await page.keyboard.press('Enter');
  const sheet = page.getByRole('dialog');
  await expect(sheet).toBeVisible();
  expect(await sheet.evaluate(el => el.matches(':modal'))).toBe(true);
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press('Tab');
    expect(await sheet.evaluate(el => el.contains(document.activeElement))).toBe(true);
  }
  await page.locator('#opener').evaluate(el => el.remove());
  await page.keyboard.press('Escape');
  await expect(sheet).not.toBeVisible();
  await expect(page.locator('#list-heading')).toBeFocused();

  await specimen(page, 'pk-ui.component.side-panel/closed-en', before, '</main>');
  await page.getByRole('link', { name: 'Read item' }).click();
  await expect(page.getByRole('heading', { name: 'Item Alpha' })).toBeFocused();
  await expect(page.getByRole('complementary')).not.toHaveAttribute('aria-modal');
  await page.locator('#outside').click();
  await expect(page.locator('#outside')).toBeFocused();
  await page.getByRole('link', { name: 'Return to items' }).click();
  await expect(page.getByRole('complementary')).not.toBeVisible();
  await expect(page.locator('#opener')).toBeFocused();

  await specimen(page, 'pk-ui.component.detail-sheet/pending-en');
  await expect(page.getByRole('dialog')).toHaveAttribute('aria-busy', 'true');
  await expect(page.getByRole('button', { name: 'Save changes' })).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).not.toBeVisible();
  await specimen(page, 'pk-ui.component.detail-sheet/restricted-en');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.getByRole('link', { name: 'Return to items' }).click();
  await expect(page.getByRole('dialog')).not.toBeVisible();
});

for (const kind of ['side-panel', 'detail-sheet']) test(`detail panels: latest selection wins and closing invalidates an outstanding response / ${kind}`, async ({ page }) => {
  const id = `pk-ui.component.${kind}/default-en`;
  const beta = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', id, '--props'], {
    cwd: root, encoding: 'utf8', input: JSON.stringify({ itemID: 'b', title: 'Item Beta' }), maxBuffer: 8 * 1024 * 1024,
  })).examples.find((entry: { id: string }) => entry.id === id).html;
  const alpha = snapshot.examples.find((entry: { id: string }) => entry.id === id).html;
  const pending: Array<() => Promise<void>> = [];
  await page.route('**/details/*', route => {
    const body = route.request().url().endsWith('/b') ? beta : alpha;
    pending.push(() => route.fulfill({ contentType: 'text/html', body }));
  });
  await specimen(page, `pk-ui.component.${kind}/closed-en`, '<main><h1 id="list-heading" tabindex="-1">Items</h1><a id="a" href="/items/a" hx-get="/details/a" hx-target="#detail-en" hx-swap="outerHTML" data-detail-open="detail-en">Read A</a><a id="b" href="/items/b" hx-get="/details/b" hx-target="#detail-en" hx-swap="outerHTML" data-detail-open="detail-en">Read B</a>', '</main>');
  await page.locator('#a').click();
  await expect.poll(() => pending.length).toBe(1);
  if (kind === 'detail-sheet') await page.getByRole('link', { name: 'Return to items' }).click();
  await page.locator('#b').click();
  await expect.poll(() => pending.length).toBe(2);
  await pending[1]();
  await expect(page.getByRole('heading', { name: 'Item Beta' })).toBeVisible();
  await pending[0]();
  await expect(page.getByRole('heading', { name: 'Item Beta' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Item Alpha' })).toHaveCount(0);
  if (kind === 'detail-sheet') await page.getByRole('link', { name: 'Return to items' }).click();
  await page.locator('#a').click();
  await expect.poll(() => pending.length).toBe(3);
  await page.getByRole('link', { name: 'Return to items' }).click();
  await pending[2]();
  await expect(page.locator('#detail-en')).toBeHidden();
  await expect(page.locator('#a')).toBeFocused();
});

test('detail panels: without JavaScript the opener and return are native navigation', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  try {
    const full = snapshot.examples.find((entry: { id: string }) => entry.id === 'pk-ui.component.side-panel/read-only-en').html;
    await page.route('**/items/a', route => route.fulfill({ contentType: 'text/html', body: `<main>${full}</main>` }));
    await page.route('**/items', route => route.fulfill({ contentType: 'text/html', body: '<h1>Items</h1>' }));
    await specimen(page, 'pk-ui.component.detail-sheet/closed-en', '<a href="/items/a" data-detail-open="detail-en">Read item</a>');
    await page.getByRole('link', { name: 'Read item' }).click();
    await expect(page.getByRole('heading', { name: 'Item Alpha' })).toBeVisible();
    await page.getByRole('link', { name: 'Return to items' }).click();
    await expect(page.getByRole('heading', { name: 'Items' })).toBeVisible();
  } finally { await context.close(); }
});

for (const locale of ['en', 'pt-PT']) for (const theme of ['light', 'dark']) {
  test(`timelines: ${locale} / ${theme} states, UTC semantics and reflow`, async ({ page }) => {
    test.setTimeout(120_000);
    const entries = snapshot.examples.filter((entry: { group: string; id: string }) => entry.group === 'Timelines' && entry.id.endsWith(`-${locale}`));
    expect(entries).toHaveLength(15);
    for (const entry of entries) {
      await specimen(page, entry.id, '<main>', '</main>', undefined, theme);
      expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze()).violations, entry.id).toEqual([]);
      for (const width of [320, 360, 768, 1024, 1440]) {
        await page.setViewportSize({ width, height: 900 });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${entry.id}/${width}`).toBe(true);
      }
      const small = await page.locator('summary, button, a[href]').evaluateAll(elements => elements.filter(el => {
        const box = el.getBoundingClientRect(); return box.width > 0 && box.height > 0 && (box.width < 44 || box.height < 44);
      }).map(el => el.outerHTML));
      expect(small, entry.id).toEqual([]);
      await page.setViewportSize({ width: 320, height: 900 });
      await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), entry.id).toBe(true);
      for (const element of await page.locator('time').all()) await expect(element).toHaveAttribute('datetime', '2026-09-29T12:00:00Z');
    }
  });
}

test('timelines: native disclosure expands by keyboard with JavaScript disabled', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  try {
    await specimen(page, 'pk-ui.component.timeline/audit-pt-PT');
    const summary = page.locator('summary');
    await summary.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('details')).toHaveAttribute('open', '');
    await expect(page.getByText('Rascunho', { exact: true })).toBeVisible();
    await expect(page.getByText('Publicado', { exact: true })).toBeVisible();
    await specimen(page, 'pk-ui.component.timeline/redacted-en');
    await page.locator('summary').click();
    await expect(page.getByText('Restricted', { exact: true })).toBeVisible();
    await expect(page.getByText('Draft', { exact: true })).toHaveCount(0);
  } finally { await context.close(); }
});

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
