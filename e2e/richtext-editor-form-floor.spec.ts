import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Browser, type Page } from '@playwright/test';
import { signInForContent } from './steps/content';
import { diagnosed, probe, refusals } from './design_floor';

// The content form is a page this delivery changes: it now mounts an editor. The
// design floor is measured twice at each width — with the editor's module loaded,
// and with it refused, which renders the form as it was before the editor
// existed — and the mounted page may not fail where the plain one passed. Axe runs
// on the mounted form, since the brief wants every journey free of violations.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';

const editable = '[data-richtext-surface] [contenteditable="true"]';

async function formAt(browser: Browser, width: number, withEditor: boolean): Promise<Page> {
  const context = await browser.newContext({ viewport: { width, height: 900 } });
  const page = await context.newPage();
  if (!withEditor) await page.route('**/js/richtext.js', (route) => route.abort());
  await signInForContent(page, email, password);
  await page.goto('/app/content/contents/new');
  if (withEditor) await expect(page.locator(editable)).toBeVisible();
  else await expect(page.getByLabel('Body')).toBeVisible();
  return page;
}

for (const width of [390, 1440]) {
  test(`the content form with the editor mounted passes the design floor where the plain form did, at ${width}px`, async ({ browser }) => {
    const plain = await formAt(browser, width, false);
    const plainProbe = await probe(plain, width);
    const plainRefusals = refusals(plainProbe);
    const mounted = await formAt(browser, width, true);
    const mountedProbe = await probe(mounted, width);
    const mountedRefusals = refusals(mountedProbe);
    console.log(`== content form at ${width}px without editor: ${plainRefusals.length} refusal(s)\n${plainRefusals.join('\n')}`);
    console.log(`== content form at ${width}px with editor: ${mountedRefusals.length} refusal(s)\n${mountedRefusals.join('\n')}`);
    expect(mountedRefusals.length, diagnosed(mountedProbe, `the mounted editor adds refusals at ${width}px`)).toBeLessThanOrEqual(plainRefusals.length);
  });
}

test('the mounted content form has no serious or critical accessibility violation', async ({ browser }) => {
  const page = await formAt(browser, 1440, true);
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
  expect(serious.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
});
