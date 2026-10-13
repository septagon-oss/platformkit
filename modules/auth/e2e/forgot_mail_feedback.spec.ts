import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { test, expect } from '../../../e2e/node_modules/@playwright/test';

// Exercise the published controller with the paths mounted by auth. The
// registration case checks the fixture; forgot has an extra path segment.
for (const kind of ['register', 'forgot']) {
  test(`${kind} reports a failed mail through auth's delivery endpoint`, async ({ page }) => {
    const origin = 'http://localhost:9876';
    const action = `/api/v1/public/auth/${kind === 'forgot' ? 'password/forgot' : 'register'}`;
    const requests: string[] = [];
    await page.route(`${origin}/**`, async route => {
      const path = new URL(route.request().url()).pathname;
      if (path === '/form') {
        await route.fulfill({ contentType: 'text/html', body: `<!doctype html><html lang="en"><body>
          <form data-auth-form="${kind}" action="${action}">
            <label>Email<input name="email" type="email" required value="ada@example.test"></label>
            <p role="alert" data-auth-error hidden></p>
            <p role="status" data-auth-message hidden>If this address can receive an account email, a link will be sent.</p>
            <button type="submit">Request link</button>
          </form></body></html>` });
      } else if (path === action) {
        await route.fulfill({ status: 202, contentType: 'application/json',
          headers: { 'X-Request-ID': 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' }, body: '{}' });
      } else {
        requests.push(path);
        await route.fulfill({ status: path === '/api/v1/public/auth/mail-delivery' ? 200 : 404,
          contentType: 'application/json', body: JSON.stringify({ state: 'failed' }) });
      }
    });
    await page.goto(`${origin}/form`);
    await page.addScriptTag({ content: readFileSync(resolve(__dirname, '../../../ui/assets/js/session.js'), 'utf8') });
    await page.getByRole('button', { name: 'Request link' }).click();
    await expect(page.locator('[data-auth-message]')).toBeVisible();
    await expect.poll(() => requests.length).toBeGreaterThan(0);
    expect.soft(requests, 'the delivery lookup must reach the route auth actually mounts')
      .toEqual(['/api/v1/public/auth/mail-delivery']);
    await expect(page.locator('[data-auth-message]')).toContainText('could not be sent');
  });
}
