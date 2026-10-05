const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('../../e2e/node_modules/playwright');

async function main() {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    const sent = [], committed = new Map();
    const scenario = process.argv[2];
    const html = `<!doctype html><html lang="en" data-principal="member"><head>
      <script src="/htmx.min.js" defer></script><script src="/htmx-config.js" defer></script>
      <script src="/command.js" defer></script></head><body>
      <form id="save" hx-ext="command" hx-post="/save" ${scenario === 'refusal' ? '' : 'hx-swap="none"'}>
      <input name="value" value="original"><button type="submit">Save</button></form>
      <div id="pk-auth-uncertain" data-request-notice hidden>Check the result</div>
      </body></html>`;
    await page.route('http://command.test/**', async route => {
      const req = route.request(), pathname = new URL(req.url()).pathname;
      if (req.method() === 'POST') {
        const record = { key: req.headers()['idempotency-key'], body: req.postData() };
        sent.push(record);
        if (scenario === 'refusal') {
          return route.fulfill({ status: 422, contentType: 'application/problem+json',
            headers: { 'Idempotency-Refusal': 'IDEMPOTENCY_KEY_REUSE' },
            body: JSON.stringify({ status: 422, detail: 'IDEMPOTENCY_KEY_REUSE: this key was already used for a different request' }) });
        }
        if (!committed.has(record.key)) committed.set(record.key, record.body);
        if (sent.length === 1) return route.abort('connectionrefused');
        return route.fulfill({ status: 204 });
      }
      if (pathname.endsWith('.js')) return route.fulfill({ contentType: 'text/javascript', body: fs.readFileSync(path.join(__dirname, '../assets/js', pathname), 'utf8') });
      return route.fulfill({ contentType: 'text/html', body: html });
    });
    await page.goto('http://command.test/');
    await page.waitForFunction(() => window.htmx);
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await page.waitForFunction(() => !document.querySelector('.htmx-request'));
    assert.equal(sent.length, 1, 'initial submission must reach the network');
    assert.match(sent[0].key, /^[0-9a-f-]{36}$/);
    if (scenario === 'reload') {
      // No answer arrived and no new intent was declared. The same form resumes
      // its pending submission, including the original values, after a reload.
      await page.reload();
      await page.getByRole('button', { name: 'Save', exact: true }).click();
      await page.waitForTimeout(150);
      assert.equal(sent[1]?.key, sent[0].key, `pending reload minted a second key: ${JSON.stringify(sent)}`);
      assert.equal(sent[1]?.body, sent[0].body);
      assert.equal(committed.size, 1, 'one pending submission must commit once');
    } else if (scenario === 'timer') {
      // A deliberate exact retry completes before the scheduled backoff expires.
      // An obsolete timer must not create a new submission afterwards.
      await page.getByRole('button', { name: 'Save', exact: true }).click();
      await page.waitForTimeout(900);
      assert.equal(committed.size, 1, `obsolete retry created another command: ${JSON.stringify(sent)}`);
      assert.equal(sent.length, 2, 'completed submission must have no later automatic dispatch');
    } else if (scenario === 'refusal') {
      // For this scenario permit normal form swapping, as real command forms do.
      await page.waitForTimeout(50);
      assert.equal(await page.locator('#save').count(), 1, 'key-reuse refusal erased the form');
      assert.equal(await page.locator('input[name=value]').inputValue(), 'original');
    } else {
      throw new Error('unknown command browser scenario');
    }
    console.log(`command ${scenario}: passed; requests=${sent.length}, effects=${committed.size}`);
  } finally {
    await browser.close();
  }
}
main().catch(err => { console.error(err); process.exitCode = 1; });
