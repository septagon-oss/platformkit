import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { expect, test } from '@playwright/test';

// Use the actual constructor and stylesheet through the existing source export.
// No application-specific CSS should be required to hide a native hidden field.
const root = resolve(__dirname, '..');
const token = 'review-42 & <exact> "ação"';
function input(props: Record<string, unknown>) {
  const exported = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', 'pk-ui.component.input/bare', '--props'], {
    cwd: root, input: JSON.stringify(props), encoding: 'utf8', maxBuffer: 8 * 1024 * 1024,
  }));
  return { css: exported.css as string, html: exported.examples[0].html as string };
}
const hidden = input({ name: 'command', type: 'hidden', value: token, label: 'Hidden command', helpText: 'Hidden help', error: 'Hidden error' });
const normalized = input({ name: 'revision', type: ' HIDDEN ', value: '0', required: true });
const disabled = input({ name: 'disabledCommand', type: 'hidden', value: 'excluded', disabled: true });

for (const javaScriptEnabled of [true, false]) {
  test(`hidden inputs preserve native form values without occupying layout, JavaScript ${javaScriptEnabled}`, async ({ browser }) => {
    const context = await browser.newContext({ javaScriptEnabled, viewport: { width: 320, height: 700 } });
    try {
      const page = await context.newPage();
      await page.route('**/__hidden_input', route => route.fulfill({ contentType: 'text/html', body: `<!doctype html>
        <html lang="en"><head><meta charset="utf-8"><title>Hidden field composition</title><style>${hidden.css}
        @media print { #print-details { display: block; } }</style></head>
        <body><p id="print-details" hidden>Details for printing</p><form id="review"><div style="display:flex;flex-direction:column;gap:24px">
        <button type="button" id="before">Before</button>${hidden.html}${normalized.html}${disabled.html}
        <button type="submit" id="after">Confirm</button></div></form></body></html>` }));
      await page.goto(new URL('/__hidden_input', process.env.PLATFORMKIT_E2E_URL ?? 'http://localhost:8099').href);
      const before = page.getByRole('button', { name: 'Before', exact: true });
      const after = page.getByRole('button', { name: 'Confirm', exact: true });
      const first = (await before.boundingBox())!, last = (await after.boundingBox())!;
      expect(last.y - first.y - first.height).toBeCloseTo(24, 3);
      await expect(page.getByText('Hidden command', { exact: true })).toBeHidden();
      await expect(page.getByText('Hidden help', { exact: true })).toBeHidden();
      await expect(page.getByText('Hidden error', { exact: true })).toBeHidden();
      await before.focus();
      await page.keyboard.press('Tab');
      await expect(after).toBeFocused();
      const entries = await page.evaluate(() => Array.from(new FormData(document.querySelector<HTMLFormElement>('#review')!).entries()));
      expect(entries).toEqual([['command', token], ['revision', '0']]);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await expect(page.getByText('Details for printing', { exact: true })).toBeHidden();
      await page.emulateMedia({ media: 'print' });
      await expect(page.getByText('Details for printing', { exact: true })).toBeVisible();
      await expect(page.getByText('Hidden command', { exact: true })).toBeHidden();
    } finally { await context.close(); }
  });
}
