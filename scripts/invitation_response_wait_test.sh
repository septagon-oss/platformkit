#!/usr/bin/env bash
# Exercise the invitation spec's actual submission assertions against controlled responses.
set -euo pipefail
cd "$(dirname "$0")/.."
node <<'JS'
const assert = require('node:assert/strict');
const fs = require('node:fs');
const { chromium, expect: baseExpect } = require('./e2e/node_modules/@playwright/test');

const source = fs.readFileSync('e2e/invitation-mail.spec.ts', 'utf8');
const start = source.indexOf('  const spent =');
const end = source.indexOf("  const me =", start);
assert(start >= 0 && end > start, 'the invitation submission assertions must exist');
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const submit = new AsyncFunction('as', 'expect', source.slice(start, end));
const expect = baseExpect.configure({ timeout: 10_000 });
const session = fs.readFileSync('ui/assets/js/session.js', 'utf8');

(async () => {
  const browser = await chromium.launch();
  try {
    for (const [status, delay] of [[204, 12_000], [400, 0], [403, 0], [302, 0], [500, 0]]) {
      const page = await browser.newPage();
      page.setDefaultTimeout(status === 302 ? 2_000 : 30_000);
      let requests = 0;
      await page.route('https://invitation.test/**', async route => {
        const request = route.request();
        if (request.method() === 'POST') {
          assert.equal(new URL(request.url()).pathname, '/api/v1/auth/password/reset');
          assert.equal(request.postDataJSON().token, 'fixture-token');
          requests++;
          await new Promise(resolve => setTimeout(resolve, delay));
          await route.fulfill({ status, body: '', headers: status === 302 ? { location: '/app' } : {} });
          return;
        }
        await route.fulfill({ contentType: 'text/html', body: `<!doctype html>
          <form data-auth-form="reset" action="/api/v1/auth/password/reset" data-next="/app">
            <input name="new" value="fixture-password"><button type="submit">Submit</button>
            <div data-auth-error hidden></div>
          </form>` });
      });
      try {
        await page.goto('https://invitation.test/app/auth/reset?token=fixture-token');
        await page.addScriptTag({ content: session });
        assert.equal(new URL(page.url()).search, '', 'the real controller removes the token');
        if (status === 204) {
          await submit(page, expect);
          assert.equal(new URL(page.url()).pathname, '/app');
        } else if (status === 302) {
          // redirect:error can prevent Chromium from publishing a response event.
          // Either way, a redirect must never make this journey pass.
          await assert.rejects(submit(page, expect), /HTTP 302|waitForResponse: Timeout/);
        } else {
          await assert.rejects(submit(page, expect), new RegExp(`HTTP ${status}`));
        }
        assert.equal(requests, 1, 'each case must reach exactly one reset request');
        console.log(`ok: invitation submission ${status === 204 ? 'waits for a 12-second success' : `rejects HTTP ${status}`}`);
      } finally {
        await page.close();
      }
    }
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
JS
