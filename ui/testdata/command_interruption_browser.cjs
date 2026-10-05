const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('../../e2e/node_modules/playwright');

// Real htmx and the shipped controller, with the keyed server's responses.
async function main() {
  const scenario = process.argv[2];
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    const sent = [], committed = new Map();
    let firstRoute;
    const html = `<!doctype html><html lang="en" data-principal="member"><head>
      <script src="/htmx.min.js" defer></script><script src="/htmx-config.js" defer></script>
      <script src="/command.js" defer></script></head><body>
      <form id="save" hx-ext="command" hx-post="/save" hx-swap="none">
      <input name="value" value="original"><button type="submit">Save</button>
      <div role="status" hidden><span data-alert-message></span></div></form>
      <div id="pk-auth-uncertain" data-request-notice hidden>Check the result</div>
      </body></html>`;
    await page.route('http://command.test/**', async route => {
      const req = route.request(), pathname = new URL(req.url()).pathname;
      if (req.method() === 'POST') {
        const record = { key: req.headers()['idempotency-key'], body: req.postData() };
        sent.push(record);
        const previous = committed.get(record.key);
        if (!previous) committed.set(record.key, record.body);
        if (sent.length === 1) {
          if (scenario === 'inflight-reload') { firstRoute = route; return; }
          return route.abort('connectionrefused');
        }
        if (previous && previous !== record.body) {
          return route.fulfill({ status: 422, headers: { 'Idempotency-Refusal': 'IDEMPOTENCY_KEY_REUSE' } });
        }
        return route.fulfill({ status: 204, headers: previous ? { 'Idempotency-Replay': 'true' } : {} });
      }
      if (pathname.endsWith('.js')) return route.fulfill({ contentType: 'text/javascript',
        body: fs.readFileSync(path.join(__dirname, '../assets/js', pathname), 'utf8') });
      return route.fulfill({ contentType: 'text/html', body: html });
    });
    await page.goto('http://command.test/');
    await page.waitForFunction(() => window.htmx);
    await page.evaluate(() => {
      window.failures = 0;
      document.addEventListener('htmx:sendError', () => { window.failures++; }, true);
    });
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await page.waitForFunction(() => document.querySelector('.htmx-request') || window.failures > 0);
    while (sent.length === 0) await page.waitForTimeout(10);
    assert.equal(sent.length, 1, 'the original submission reached the server');
    assert.match(sent[0].key, /^[0-9a-f-]{36}$/);
    if (scenario === 'inflight-reload') {
      assert.equal(await page.evaluate(() => window.failures), 0, 'reload precedes network-failure notification');
      // The write is committed; its response is still travelling when the person reloads.
      await page.reload();
      await firstRoute.abort('connectionrefused').catch(() => {});
      await page.waitForFunction(() => window.htmx);
      await page.getByRole('button', { name: 'Save', exact: true }).click();
    } else if (scenario === 'edited-backoff') {
      await page.waitForFunction(() => window.failures > 0);
      // Editing is not a submission. The scheduled retry must resend the original bytes.
      await page.locator('input[name=value]').fill('unsent edit');
    } else {
      throw new Error('unknown interruption scenario');
    }
    await page.waitForTimeout(1500);
    console.log(JSON.stringify({ scenario, sent, effects: committed.size }));
    assert.equal(sent.length, 2, 'one submission needs only its original dispatch and its retry');
    assert.equal(sent[1].key, sent[0].key, 'the outstanding submission must retain its key');
    assert.equal(sent[1].body, sent[0].body, 'the retry must retain the original submitted body');
    assert.equal(committed.size, 1, 'one submission must commit exactly one command');
    if (scenario === 'edited-backoff') {
      assert.equal(await page.locator('input[name=value]').inputValue(), 'unsent edit', 'retry must preserve the later draft');
    }
  } finally {
    await browser.close();
  }
}
main().catch(err => { console.error(err); process.exitCode = 1; });
