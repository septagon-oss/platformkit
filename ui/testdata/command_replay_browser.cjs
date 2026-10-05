const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('../../e2e/node_modules/playwright');

// The server here answers as kit/httpx does: a key it has already run is answered
// from its record with Idempotency-Replay: true, and runs nothing.
async function main() {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    const sent = [], committed = new Map();
    const html = `<!doctype html><html lang="en" data-principal="member"><head>
      <script src="/htmx.min.js" defer></script><script src="/htmx-config.js" defer></script>
      <script src="/command.js" defer></script></head><body>
      <form id="save" hx-ext="command" hx-post="/save" hx-swap="none">
      <input name="value" value="original"><button type="submit">Save</button></form>
      <div id="pk-auth-uncertain" data-request-notice hidden>Check the result</div>
      </body></html>`;
    await page.route('http://command.test/**', async route => {
      const req = route.request(), pathname = new URL(req.url()).pathname;
      if (req.method() === 'POST') {
        const key = req.headers()['idempotency-key'];
        sent.push({ key, body: req.postData() });
        if (committed.has(key)) return route.fulfill({ status: 204, headers: { 'Idempotency-Replay': 'true' } });
        committed.set(key, req.postData());
        // The first command ran; its answer is lost on the way back.
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
    assert.equal(sent.length, 1, 'the submission reached the server');
    // The person reloads before the scheduled retry and presses Save once.
    await page.reload();
    await page.waitForFunction(() => window.htmx);
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await page.waitForTimeout(1500);
    assert.equal(sent[1]?.key, sent[0].key, `the press after the reload resumes the submission: ${JSON.stringify(sent)}`);
    assert.deepEqual(sent.map(s => s.key).filter(k => k !== sent[0].key), [],
      `one submission was sent under a second key: ${JSON.stringify(sent)}`);
    assert.equal(committed.size, 1, `one submission and one press committed ${committed.size} commands`);
    console.log(`command replay after reload: passed; requests=${sent.length}, effects=${committed.size}`);
  } finally {
    await browser.close();
  }
}
main().catch(err => { console.error(err); process.exitCode = 1; });
