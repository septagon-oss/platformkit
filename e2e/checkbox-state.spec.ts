import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { expect, test, type Locator, type Page } from '@playwright/test';

// Project the actual atom and stylesheet through the existing source exporter.
// Browser tests exercise its native control and the shared served runtime.
test.use({ trace: 'off', screenshot: 'off', video: 'off' });
const root = resolve(__dirname, '..');
function checkbox(props: Record<string, unknown>) {
  const exported = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', 'pk-ui.component.checkbox/default', '--props'], {
    cwd: root, input: JSON.stringify(props), encoding: 'utf8', maxBuffer: 8 * 1024 * 1024,
  }));
  return { css: exported.css as string, html: exported.examples[0].html as string };
}
const consent = checkbox({ name: 'termsAccepted', label: 'Accept the terms', required: true });
const checked = checkbox({ name: 'existing', label: 'Existing choice', checked: true, required: false });
const mixed = checkbox({ name: 'mixed', label: 'Mixed choice', indeterminate: true, required: false });
const disabled = checkbox({ name: 'unavailable', label: 'Unavailable choice', disabled: true, required: false });
const hidden = checkbox({ name: 'hidden', label: 'Hidden choice', hidden: true, required: false });
const longLabel = 'I accept the Terms of Service and Privacy Policy, both clearly labeled drafts.';
const longConsent = checkbox({ name: 'draftTerms', label: longLabel, required: true });
const bare = checkbox({ name: 'bareChoice', label: '', required: false });

async function specimen(page: Page) {
  await page.route('**/__checkbox', route => route.fulfill({ contentType: 'text/html', body: `<!doctype html>
    <html lang="en"><head><meta charset="utf-8"><title>Checkbox contract</title><style>${consent.css}</style>
    <script defer src="/app/admin/assets/js/htmx.min.js"></script><script defer src="/app/admin/assets/js/components.js"></script></head>
    <body><main><h1>Checkbox contract</h1><form id="consent">
    <button type="button" id="before">Before controls</button><div>${consent.html}</div>
    <div>${checked.html}</div><div>${mixed.html}</div><div>${disabled.html}</div>${hidden.html}<div>${bare.html}</div>
    <button type="reset">Reset choices</button></form>
    <button type="button" hx-get="/__checkbox-fragment" hx-target="#dynamic">Load choice</button><div id="dynamic"></div>
    <button type="button" id="replace" hx-get="/__checkbox-input" hx-target="#dynamic input" hx-swap="outerHTML">Replace input</button>
    </main></body></html>` }));
  await page.goto(new URL('/__checkbox', process.env.PLATFORMKIT_E2E_URL ?? 'http://localhost:8099').href);
  await expect(page.getByRole('heading', { name: 'Checkbox contract' })).toBeVisible();
}

function control(page: Page, name = 'Accept the terms') {
  const input = page.getByRole('checkbox', { name, exact: true });
  const wrapper = page.locator('[data-component=checkbox]').filter({ has: input });
  return { input, wrapper, mark: wrapper.locator('[data-checkbox-checkmark]'), bar: wrapper.locator('[data-checkbox-bar]') };
}

async function inkVisible(mark: Locator) {
  return mark.evaluate(node => {
    const style = getComputedStyle(node);
    const color = style.color.replaceAll(' ', '');
    return style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0'
      && color !== 'transparent' && !color.endsWith(',0)') && node.getBoundingClientRect().width > 0;
  });
}

async function contrast(box: Locator, focus = false) {
  return box.evaluate((node, focus) => {
    const luminance = (color: string) => {
      const channels = color.match(/[\d.]+/g)!.slice(0, 3).map(value => {
        const channel = Number(value) / 255;
        return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
      });
      return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
    };
    const style = getComputedStyle(node);
    const a = luminance(focus ? style.outlineColor : style.color);
    const b = luminance(focus ? getComputedStyle(document.body).backgroundColor : style.backgroundColor);
    return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
  }, focus);
}

for (const javaScriptEnabled of [true, false]) {
  test(`native label and keyboard changes stay visible, JavaScript ${javaScriptEnabled}`, async ({ browser }) => {
    const context = await browser.newContext({ javaScriptEnabled, viewport: { width: 320, height: 700 } });
    try {
      const page = await context.newPage();
      await specimen(page);
      const { input, wrapper, mark } = control(page);
      expect(await inkVisible(mark)).toBe(false);
      expect(await input.evaluate((node: HTMLInputElement) => node.validity.valueMissing)).toBe(true);
      await wrapper.click();
      await expect(input).toBeChecked();
      await expect.poll(() => inkVisible(mark)).toBe(true);
      expect(await input.evaluate((node: HTMLInputElement) => node.validity.valueMissing)).toBe(false);
      await page.getByRole('button', { name: 'Before controls' }).focus();
      await page.keyboard.press('Tab');
      await expect(input).toBeFocused();
      await page.keyboard.press('Space');
      await expect(input).not.toBeChecked();
      await expect.poll(() => inkVisible(mark)).toBe(false);
      expect(await wrapper.evaluate(node => node.getBoundingClientRect().height >= 24)).toBe(true);
      await expect(page.getByRole('checkbox', { name: 'Unavailable choice' })).toBeDisabled();
      await expect(page.getByText('Hidden choice')).toBeHidden();
      const bareBox = await control(page, 'bareChoice').wrapper.boundingBox();
      expect(bareBox!.width).toBeGreaterThanOrEqual(24);
      expect(bareBox!.height).toBeGreaterThanOrEqual(24);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    } finally { await context.close(); }
  });
}

test('focus and checked ink remain visible in both themes and forced colors', async ({ page }) => {
  for (const forcedColors of ['none', 'active'] as const) {
    await page.emulateMedia({ forcedColors });
    await specimen(page);
    for (const theme of ['light', 'dark']) {
      await page.locator('html').evaluate((node, value) => node.setAttribute('data-theme', value), theme);
      const { input, wrapper, mark } = control(page);
      await page.getByRole('button', { name: 'Before controls' }).focus();
      await page.keyboard.press('Tab');
      await expect(input).toBeFocused();
      const box = wrapper.locator('[data-checkbox-box]');
      expect(await box.evaluate(node => {
        const style = getComputedStyle(node);
        return style.outlineStyle !== 'none' && parseFloat(style.outlineWidth) >= 2;
      })).toBe(true);
      await expect.poll(() => contrast(box, true), { message: `${forcedColors}/${theme}: visible focus contrast` }).toBeGreaterThanOrEqual(3);
      if (forcedColors === 'active') {
        expect(await box.evaluate(node => {
          const reference = document.createElement('span');
          reference.style.color = 'Highlight';
          document.body.append(reference);
          const matches = getComputedStyle(node).outlineColor === getComputedStyle(reference).color;
          reference.remove();
          return matches;
        })).toBe(true);
      }
      await page.keyboard.press('Space');
      await expect(input).toBeChecked();
      await expect.poll(() => inkVisible(mark)).toBe(true);
      // The existing color transition must settle before measuring its final ink.
      await expect.poll(() => contrast(box), { message: `${forcedColors}/${theme}: checked ink contrast` }).toBeGreaterThanOrEqual(3);
      await page.keyboard.press('Space');
    }
  }
});

test('mixed state initializes once and form reset restores original native choices', async ({ page }) => {
  await page.goto(new URL('/app/admin/login', process.env.PLATFORMKIT_E2E_URL ?? 'http://localhost:8099').href);
  await expect(page.locator('script[src="/app/admin/assets/js/components.js"]')).toHaveCount(1);
  await specimen(page);
  const original = control(page, 'Existing choice');
  const partial = control(page, 'Mixed choice');
  await expect.poll(() => partial.input.evaluate((node: HTMLInputElement) => node.indeterminate)).toBe(true);
  await expect(partial.bar).toBeVisible();
  await expect(partial.mark).toBeHidden();
  await original.wrapper.click();
  await partial.wrapper.click();
  await expect(original.input).not.toBeChecked();
  await expect(partial.input).toBeChecked();
  await expect(partial.bar).toBeHidden();
  await expect(partial.wrapper).toHaveAttribute('data-state', 'checked');
  await page.getByRole('button', { name: 'Reset choices' }).click();
  await expect(original.input).toBeChecked();
  await expect.poll(() => inkVisible(original.mark)).toBe(true);
  await expect(partial.input).not.toBeChecked();
  await expect.poll(() => partial.input.evaluate((node: HTMLInputElement) => node.indeterminate)).toBe(true);
  await expect(partial.bar).toBeVisible();
  await expect(partial.wrapper).toHaveAttribute('data-state', 'indeterminate');
  expect(await page.locator('#consent').evaluate(node => [...new FormData(node as HTMLFormElement).keys()])).toEqual(['existing']);
});

test('cancelled reset and direct native property changes keep the visible state honest', async ({ page }) => {
  await specimen(page);
  const { input, mark, wrapper } = control(page);
  await input.evaluate((node: HTMLInputElement) => { node.checked = true; });
  await expect.poll(() => inkVisible(mark)).toBe(true);
  await page.locator('#consent').evaluate(node => node.addEventListener('reset', event => event.preventDefault()));
  await page.getByRole('button', { name: 'Reset choices' }).click();
  await expect(input).toBeChecked();
  await expect.poll(() => inkVisible(mark)).toBe(true);
  await input.evaluate((node: HTMLInputElement) => { node.checked = false; node.dispatchEvent(new Event('change', { bubbles: true })); });
  await expect.poll(() => inkVisible(mark)).toBe(false);
  await expect(wrapper).toHaveAttribute('data-state', 'unchecked');
});

test('HTMX initializes a new mixed control and a replaced input within its existing label', async ({ page }) => {
  const fragment = mixed.html.replaceAll('mixed', 'fragment');
  await page.route('**/__checkbox-fragment', route => route.fulfill({ contentType: 'text/html', body: fragment }));
  await specimen(page);
  await page.getByRole('button', { name: 'Load choice' }).click();
  const input = page.locator('#dynamic input');
  await expect.poll(() => input.evaluate((node: HTMLInputElement) => node.indeterminate)).toBe(true);
  const replacement = await input.evaluate(node => node.outerHTML);
  await page.locator('#dynamic label').click();
  await expect(input).toBeChecked();
  await page.route('**/__checkbox-input', route => route.fulfill({ contentType: 'text/html', body: replacement }));
  await page.getByRole('button', { name: 'Replace input' }).click();
  await expect(input).not.toBeChecked();
  await expect.poll(() => input.evaluate((node: HTMLInputElement) => node.indeterminate)).toBe(true);
  await expect(page.locator('#dynamic [data-checkbox-bar]')).toBeVisible();
});

test('long consent wording wraps without clipping and its native validation anchor stays within the label', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 700 });
  await specimen(page);
  await page.locator('#dynamic').evaluate((node, html) => { node.innerHTML = html; }, longConsent.html);
  const wrapper = page.locator('#dynamic label');
  const label = wrapper.locator('span').filter({ hasText: longLabel });
  expect(await label.evaluate(node => {
    const style = getComputedStyle(node);
    return style.whiteSpace !== 'nowrap' && style.textOverflow !== 'ellipsis' && node.scrollWidth <= node.clientWidth;
  })).toBe(true);
  const input = page.getByRole('checkbox', { name: longLabel, exact: true });
  const labelBox = (await wrapper.boundingBox())!;
  const inputBox = (await input.boundingBox())!;
  expect(labelBox.height).toBeGreaterThanOrEqual(24);
  expect(inputBox.x >= labelBox.x && inputBox.x < labelBox.x + labelBox.width).toBe(true);
  expect(inputBox.y >= labelBox.y && inputBox.y < labelBox.y + labelBox.height).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await label.click();
  await expect(input).toBeChecked();
});

test('changing native disabled state updates appearance without preventing later consent', async ({ page }) => {
  await specimen(page);
  const { input, wrapper, mark } = control(page, 'Unavailable choice');
  await expect(input).toBeDisabled();
  await input.evaluate((node: HTMLInputElement) => { node.disabled = false; });
  await expect(input).toBeEnabled();
  await expect(wrapper).toHaveCSS('opacity', '1');
  await expect(wrapper).toHaveCSS('cursor', 'pointer');
  await wrapper.click();
  await expect(input).toBeChecked();
  await expect.poll(() => inkVisible(mark)).toBe(true);
  await input.evaluate((node: HTMLInputElement) => { node.disabled = true; });
  await expect(wrapper).toHaveCSS('cursor', 'not-allowed');
  await expect(wrapper).toHaveCSS('opacity', '0.5');
});
