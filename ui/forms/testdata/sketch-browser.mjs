import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { chromium } = require(process.argv[2]);
const browser = await chromium.launch({ headless: true });
const url = process.argv[3];
const A = [[[0, 0, 0], [500, 250, 17], [1000, 1000, 42]], [[900, 100, 0]]];
let checks = 0;
try {
  for (const ratio of [1, 2]) {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: ratio });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const input = page.locator('[data-sketch-value]');
    const canvas = page.locator('canvas');
    const value = async () => JSON.parse(await input.inputValue());
    const load = async initial => {
      await page.goto(url + '?value=' + encodeURIComponent(initial));
      await canvas.waitFor({ state: 'visible' });
    };
    const pointer = async (type, x, y, time, pointerType = 'mouse', id = 1) => {
      await canvas.evaluate((canvas, args) => {
        const rect = canvas.getBoundingClientRect();
        const event = new PointerEvent(args.type, { bubbles: true, pointerId: args.id, isPrimary: true, button: 0,
          pointerType: args.pointerType, clientX: rect.left + args.x * rect.width / 1000, clientY: rect.top + args.y * rect.height / 1000 });
        Object.defineProperty(event, 'timeStamp', { value: args.time });
        // Synthetic pointers have no native capture lifecycle. Real mouse capture is checked separately.
        canvas.setPointerCapture = () => {}; canvas.hasPointerCapture = () => false;
        canvas.dispatchEvent(event);
      }, { type, x, y, time, pointerType, id });
    };
    await load('null');
    await canvas.focus(); await page.keyboard.press('Space'); await page.keyboard.press('ArrowRight');
    await page.keyboard.press('Shift+ArrowDown'); await page.keyboard.press('Enter');
    let points = (await value())[0];
    assert.deepEqual(points.map(p => p.slice(0, 2)), [[500, 500], [510, 500], [510, 501]]); checks++;
    await page.getByRole('button', { name: 'Undo', exact: true }).click(); assert.deepEqual(await value(), []); checks++;
    await load(JSON.stringify(A));
    await page.getByRole('button', { name: 'Clear', exact: true }).click(); assert.deepEqual(await value(), []);
    await page.getByRole('button', { name: 'Undo', exact: true }).click(); assert.deepEqual(await value(), A); checks++;
    for (const kind of ['mouse', 'pen', 'touch']) {
      await load('[]');
      await pointer('pointerdown', 0, 0, 100, kind);
      await pointer('pointermove', 500, 250, 117, kind);
      await pointer('pointerup', 1200, 1000, 142, kind);
      assert.deepEqual(await value(), [A[0]]); checks++;
      await pointer('pointerdown', 500, 500, 200, kind);
      await pointer('pointercancel', 500, 500, 217, kind);
      assert.deepEqual(await value(), [A[0]]); checks++;
    }
    await load('[]');
    await canvas.click(); assert.equal((await value())[0].length, 1); checks++;
    await canvas.focus(); await page.keyboard.press('Space'); await page.keyboard.press('ArrowDown'); await page.keyboard.press('Tab');
    assert.equal((await value()).length, 1); checks++;
    await page.locator('form').evaluate(form => form.reset()); assert.deepEqual(await value(), []); checks++;
    await load('[[[1.0,2,0]]]');
    assert.equal(await input.inputValue(), '[[[1.0,2,0]]]');
    assert.equal(await page.getByRole('button', { name: 'Undo', exact: true }).isDisabled(), true);
    await page.getByRole('button', { name: 'Clear', exact: true }).click(); assert.deepEqual(await value(), []); checks++;
    for (const [initial, reason] of [
      [Array.from({ length: 400 }, () => [[1, 2, 0]]), '400 strokes'],
      [[Array.from({ length: 20000 }, () => [1, 2, 0])], '20,000 points'],
    ]) {
      await load(JSON.stringify(initial));
      await canvas.focus(); await page.keyboard.press('Space');
      assert.deepEqual(await value(), initial);
      assert.ok((await page.getByRole('status').textContent()).includes(reason)); checks++;
      await page.getByRole('button', { name: 'Undo', exact: true }).click();
      await canvas.focus(); await page.keyboard.press('Space'); await page.keyboard.press('Enter');
      assert.ok((await value()).length > 0); checks++;
    }
    await load(JSON.stringify(A));
    await page.getByRole('button', { name: 'Clear', exact: true }).click();
    await canvas.focus(); await page.keyboard.press('Space'); await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Undo', exact: true }).click(); assert.deepEqual(await value(), A); checks++;
    await page.getByRole('button', { name: 'Clear', exact: true }).click();
    await canvas.focus(); await page.keyboard.press('Space'); await page.keyboard.press('Enter');
    await page.keyboard.press('Control+z'); assert.deepEqual(await value(), []);
    assert.equal(await page.getByRole('button', { name: 'Undo', exact: true }).isDisabled(), true); checks++;
    await canvas.focus(); await page.keyboard.press('Space');
    await page.locator('form').evaluate(form => { form.addEventListener('submit', e => { e.preventDefault(); e.stopImmediatePropagation(); }, { capture: true }); form.requestSubmit(); });
    assert.deepEqual(await value(), [[[500, 500, 0]]]); checks++;
    await load(JSON.stringify([Array.from({ length: 19999 }, () => [1, 2, 0])]));
    const capped = await input.inputValue();
    await pointer('pointerdown', 500, 500, 10);
    await pointer('pointermove', 501, 501, 20);
    assert.equal(await input.inputValue(), capped);
    assert.ok((await page.getByRole('status').textContent()).includes('20,000')); checks++;
    await load('null');
    await pointer('pointerdown', 10, 10, 10);
    await pointer('pointerup', 20, 20, 20, 'mouse', 2);
    assert.equal(await value(), null);
    await pointer('lostpointercapture', 20, 20, 20);
    assert.equal(await value(), null); checks++;
    await pointer('pointerdown', 10, 10, 10);
    await pointer('pointermove', 20, 20, 9007199254741010);
    assert.equal(await value(), null);
    assert.ok((await page.getByRole('status').textContent()).includes('milliseconds')); checks++;
    await canvas.focus(); await page.keyboard.press('Space');
    await page.locator('[data-sketch]').evaluate(root => { window.detachedSketch = root; root.remove(); });
    await page.waitForTimeout(20);
    await page.locator('form').evaluate(form => form.prepend(window.detachedSketch));
    await canvas.waitFor({ state: 'visible' });
    assert.equal(await value(), null);
    await canvas.focus(); await page.keyboard.press('Space'); await page.keyboard.press('Enter');
    assert.equal((await value()).length, 1); checks++;
    await page.emulateMedia({ forcedColors: 'active' });
    await canvas.focus();
    assert.notEqual(await canvas.evaluate(c => getComputedStyle(c).backgroundColor), 'rgba(0, 0, 0, 0)'); checks++;
    await page.locator('body').evaluate(body => { body.style.zoom = '2'; });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false); checks++;
    await page.locator('body').evaluate(body => { body.style.zoom = ''; });
    await page.emulateMedia({ forcedColors: 'none' });
    for (const width of [320, 390, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false); checks++;
    }
    assert.deepEqual(errors, []);
    await context.close();
  }
  const pair = await browser.newContext();
  const pairPage = await pair.newPage();
  await pairPage.goto(url + '?second=1');
  await pairPage.getByRole('group', { name: 'Drawing', exact: true }).last().focus();
  await pairPage.keyboard.press('Space'); await pairPage.keyboard.press('Enter');
  assert.equal(JSON.parse(await pairPage.locator('form').nth(0).locator('input[name="drawing"]').inputValue()).length, 1);
  assert.equal(await pairPage.locator('form').nth(1).locator('input[name="drawing"]').inputValue(), 'null');
  assert.equal(await pairPage.evaluate(() => { const ids = [...document.querySelectorAll('[id]')].map(n => n.id); return new Set(ids).size === ids.length; }), true);
  const tree = await pairPage.locator('main').ariaSnapshot();
  assert.ok(tree.includes('Drawing') && tree.includes('Other drawing') && tree.includes('Undo') && tree.includes('Clear')); checks += 4;
  await pairPage.goto(url + '?error=' + encodeURIComponent('Repair the drawing.'));
  await pairPage.locator('canvas').waitFor({ state: 'visible' });
  await pairPage.locator('form').evaluate(form => form.dispatchEvent(new CustomEvent('htmx:afterSettle', { bubbles: true, detail: { xhr: { status: 422 }, elt: form } })));
  assert.equal(await pairPage.evaluate(() => document.activeElement.tagName), 'CANVAS');
  assert.equal(await pairPage.locator('canvas').getAttribute('aria-invalid'), 'true'); checks += 2;
  await pairPage.locator('form').evaluate(form => { window.sketchEvents = []; for (const kind of ['input', 'change']) form.addEventListener(kind, event => window.sketchEvents.push(event.type)); });
  await pairPage.locator('canvas').focus(); await pairPage.keyboard.press('Space'); await pairPage.keyboard.press('Enter');
  await pairPage.locator('form').evaluate(form => form.reset());
  assert.deepEqual(await pairPage.evaluate(() => window.sketchEvents), ['input', 'change', 'input', 'change']); checks++;
  await pairPage.locator('form').evaluate(form => form.reset());
  assert.deepEqual(await pairPage.evaluate(() => window.sketchEvents), ['input', 'change', 'input', 'change']); checks++;
  await pair.close();
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto(url + '?value=' + encodeURIComponent(JSON.stringify(A)));
  assert.equal(await page.locator('[data-sketch-value]').inputValue(), JSON.stringify(A));
  assert.equal(await page.locator('svg[role="img"]').getAttribute('aria-label'), 'Drawing');
  assert.equal(await page.getByRole('button', { name: 'Undo', exact: true }).count(), 0);
  checks += 3;
  await context.close();
  console.log(`PASS: ${checks} drawing interaction, limit, fallback and reflow assertions at device ratios 1 and 2.`);
} finally { await browser.close(); }
