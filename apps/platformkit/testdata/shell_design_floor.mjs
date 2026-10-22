import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
import { probe, refusals, report } from '../../../ui/testdata/design_floor.mjs';
const require = createRequire(new URL('../../../e2e/package.json', import.meta.url));
const { chromium } = require('@playwright/test');
const [origin, id] = process.argv.slice(2);
assert.ok(origin && id, 'a served application and real record are required');
const browser = await chromium.launch({headless: true});
const measurements = [];
try {
  for (const locale of ['en', 'pt-PT']) for (const theme of ['light', 'dark']) {
    const context = await browser.newContext({locale, colorScheme: theme});
    await context.addInitScript(mode => localStorage.setItem('platformkit-theme', mode), theme);
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    const assetErrors = [];
    page.on('requestfailed', r => {
      if (['stylesheet','script','font'].includes(r.resourceType())) assetErrors.push(`${r.url()}: ${r.failure()?.errorText}`);
    });
    page.on('response', r => {
      if (['stylesheet','script','font'].includes(r.request().resourceType()) && r.status() >= 400)
        assetErrors.push(`${r.url()}: HTTP ${r.status()}`);
    });
    for (const width of [390, 1440]) {
      await page.setViewportSize({width, height: 900});
      for (const [path, identity] of [
        ['/app', 'dashboard'], ['/app/task/tasks', 'list'],
        [`/app/task/tasks/${id}`, 'record'], ['/app/task/tasks/new', 'new'],
        [`/app/task/tasks/${id}/edit`, 'edit'],
      ]) {
        const response = await page.goto(origin + path, {waitUntil: 'networkidle'});
        assert.equal(response.status(), 200, path);
        assert.equal(new URL(page.url()).pathname, path, 'no redirect may replace the sample');
        await page.evaluate(() => document.fonts.ready);
        assert.equal(await page.locator('main h1').count(), 1, path);
        if (identity === 'dashboard') {
          assert.ok(await page.locator('main [data-component=card][href="/app/task/tasks"]').count(), 'dashboard task destination');
        } else if (identity === 'list') {
          const link = page.locator(`main a[href="/app/task/tasks/${id}"]`);
          assert.equal(await link.count(), 1, 'the populated list links to the created record');
          assert.ok((await link.innerText()).includes('A measured task'), path);
        } else if (identity === 'record') {
          assert.equal(await page.locator('main h1').innerText(), 'A measured task', 'the record names the created row');
        } else {
          assert.equal(await page.locator('main form [name=title]').count(), 1, path);
          assert.equal(await page.locator('main form [name=title]').inputValue(), identity === 'edit' ? 'A measured task' : '');
          assert.equal(await page.locator('main form:has([name=title])').getAttribute('action'),
            identity === 'edit' ? `/app/task/tasks/${id}` : '/app/task/tasks');
        }
        const m = await probe(page, width);
        assert.equal(m.actual_width, width);
        assert.equal(m.actual_height, 900);
        assert.deepEqual(assetErrors, [], 'all page assets load');
        assert.equal(await page.locator('html').getAttribute('lang'), locale);
        assert.equal(m.chrome.length, width < 1024 ? 3 : 2, 'workspace, caller and build stamp');
        assert.ok(m.chrome.some(c => c.region === 'footer' && c.text.startsWith('PlatformKit')));
        assert.deepEqual([...new Set(m.chrome.map(c => c.size))], [14]);
        if (width === 390) assert.equal(m.chrome.filter(c => c.text === 'Acme Corporation').length, 1);
        assert.equal(m.theme, theme, 'effective theme');
        assert.ok(m.stylesheet_hex.length, 'served stylesheet must be measured');
        for (const sheet of m.stylesheet_hex) {
          assert.ok(!sheet.error && sheet.bytes > 0 && ['bytes','hex','defines'].every(k => Number.isFinite(sheet[k])), JSON.stringify(sheet));
        }
        for (const field of ['left_edges','body_font_sizes','all_font_sizes','section_gaps']) {
          assert.ok(Array.isArray(m[field]) && m[field].every(Number.isFinite), `complete ${field}`);
        }
        assert.ok(m.left_edges.length > 0 && m.all_font_sizes.length > 0, 'visible text was measured');
        assert.ok(Number.isInteger(m.primary_ctas_above_fold), 'CTA count was measured');
        assert.equal(typeof m.scroll_width_overflow, 'boolean', 'overflow was measured');
        assert.ok(Number.isFinite(m.worst_contrast.ratio) && Number.isFinite(m.worst_contrast.size) && m.worst_contrast.text, 'text contrast was measured');
        assert.ok(Number.isFinite(m.measures_ch.min) && Number.isFinite(m.measures_ch.max), 'paragraph measure was measured');
        const errors = refusals(m);
        if (identity === 'new' || identity === 'edit') {
          assert.equal(m.forms.length, 1);
          const form = m.forms[0];
          if (form.max !== `${24 * form.root}px` || form.width > 24 * form.root + 1 || form.width > form.parent + 1)
            errors.push(`form measure ${JSON.stringify(form)}`);
          assert.deepEqual([...new Set(form.controls.map(c => c.tag))].sort(), ['INPUT','SELECT','TEXTAREA'], 'text, select and textarea are measured');
          for (const control of form.controls) if (Math.abs(control.width - form.width) > 1)
            errors.push(`control width ${JSON.stringify(control)}`);
        }
        if (width === 1440) {
          assert.ok(m.sidebar.length >= 5, 'brand and nav wrappers and links are measured');
          for (const item of m.sidebar) if (item.size < 24 && item.ratio < 4.5)
            errors.push(`sidebar contrast ${JSON.stringify(item)}`);
        }
        measurements.push({path, locale, theme, ...m, refusals: errors});
        if (width === 390 && identity === 'dashboard') {
          const disclosure = page.locator('[data-component=sidebar-disclosure]');
          await disclosure.locator('summary').focus();
          await page.keyboard.press('Enter');
          assert.ok(await disclosure.getAttribute('open') !== null, 'keyboard opens navigation');
          const expanded = await probe(page, width);
          assert.ok(expanded.sidebar.length >= 4, 'mobile navigation wrappers and links are visible');
          for (const item of expanded.sidebar) if (item.size < 24)
            assert.ok(item.ratio >= 4.5, JSON.stringify(item));
          await page.keyboard.press('Enter');
          assert.equal(await disclosure.getAttribute('open'), null);
        }
      }
    }
    await context.close();
  }
  assert.equal(measurements.length, 40);
  console.log(JSON.stringify({measurements: measurements.length, pages: measurements.map(m => ({
    path: m.path, width: m.width, locale: m.locale, theme: m.theme, left_edges: m.left_edges,
    body_font_sizes: m.body_font_sizes, primary_ctas_above_fold: m.primary_ctas_above_fold,
    worst_contrast: m.worst_contrast, forms: m.forms, refusals: m.refusals,
  }))}));
  for (const m of measurements) assert.deepEqual(m.refusals, [], report(`${m.path} ${m.locale} ${m.theme}`, m));
} finally {
  await browser.close();
}
