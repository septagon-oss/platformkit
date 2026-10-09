import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// What a person sees when the kernel refused before any screen existed.
//
// e2e/design-audit.spec.ts is the repository's eye, and it cannot see these pages:
// it measures inside `main`, and a refusal page is frameless by design (see
// `faultFrame` in apps/platformkit/fault.go — the navigation a signed-in person
// gets is built from what *they* may reach, and a refusal is most often met by
// somebody who is not signed in). So the pages a stranger is most likely to meet
// were the pages nothing measured. This spec is what stops them being invisible.
//
// The one axe rule it disables is `region`, and the reason is the same T-0184
// decision the comment above cites: `region` is a best-practice rule, not WCAG 2.1
// AA, and giving this page a landmark would change the shape of every refusal page
// in the tree to satisfy a rule about landmarks. Everything else must pass, at the
// small width a phone uses and the wide one a desktop does.
process.env.PLAYWRIGHT_NO_COPY_PROMPT = '1';

/** The addresses a browser can reach that no handler serves, and the one a guard
 *  refuses at — the three answers this delivery is about. */
const unmounted = '/there-is-nothing-at-this-address';

async function refuse(page: import('@playwright/test').Page, path: string) {
  const response = await page.goto(path, { waitUntil: 'domcontentloaded' });
  expect(response, `${path} answered`).not.toBeNull();
  return response!;
}

for (const width of [390, 1440]) {
  test(`the page for an address nobody mounted is accessible at ${width}px`, async ({ page, request }) => {
    await page.setViewportSize({ width, height: 900 });
    const got = await refuse(page, unmounted);
    expect(got!.status(), 'the verdict survives the rendering').toBe(404);
    expect(got!.headers()['content-type']).toContain('text/html');
    expect(got!.headers()['x-content-type-options']).toBe('nosniff');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

    const results = await new AxeBuilder({ page })
      .disableRules(['region'])
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
      .analyze();
    expect(results.violations, JSON.stringify(results.violations)).toEqual([]);

    // The other shape of the same verdict, at the same address. This is the line
    // that used to record the opposite: `request.get` with
    // `Accept: application/json` here was answered `text/html`, because the
    // refusal was not one the API seam wrote — an address outside `/api/v1`
    // belongs to the site's own page mount (modules/web/internal/mount.go, which
    // answers `there is no page at /<slug>`), and a page operation handed markup
    // to whoever asked, whatever they asked for. The gap closed at 5480f9b: the
    // page branch now asks the same `WantsValue` question the guards ask, so a
    // client that named a JSON media type and refused markup gets the same
    // problem document kit/httpx's table pins body for body, and the navigation
    // two assertions above — a browser named text/html — still gets this page.
    // modules/web/json_refusal_test.go is the Go side of the same rule.
    const value = await request.get(unmounted, { headers: { Accept: 'application/json' } });
    expect(value.status()).toBe(404);
    expect(value.headers()['content-type']).toContain('application/problem+json');
    expect(await value.text(), 'the JSON answer says what is missing').toContain('"status":404');

    // And a person who lands here is not left with a status code and nowhere to go:
    // the page the site mount answers carries the *site's* way onward. An earlier
    // reading of this address wrote it up as a page with "no link on it at all";
    // asked again, at both widths, with a browser and with curl, the link that was
    // absent is the workspace's — the site's own is on the page, and this is the
    // case that says so.
    await expect(page.getByRole('link', { name: 'Back to the site' })).toBeVisible();
  });

  // The affordances, in their own case: the verdict is that this one leaves, and the
  // brief's acceptance line is that a person is never left with a status code and
  // nowhere to go. Retry is asserted absent because waiting does not make an address
  // exist — a page that offered it here would be promising a rescue that cannot come.
  //
  // The address is inside the API surface, and that is the claim rather than a detail:
  // a refusal the kernel wrote is the one that carries the two shapes and the way on.
  // A path outside it is answered by the site's page mount with the site's own chrome —
  // the case above asserts the link it does offer, so the two cases together say which
  // affordances live where, and neither rests on a claim this run did not measure.
  test(`the page for an address nobody mounted offers the way on at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await refuse(page, '/api/v1/public/no-such-address');
    await expect(page.getByRole('link', { name: 'Back to the workspace' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Try again' })).toHaveCount(0);
  });
}

test('the readiness address answers a person a page and a monitor its document', async ({ page, request }) => {
  // A healthy instance owes everybody the same small body: the negotiation only
  // touches faults, so this is the guard against "made the probe a page".
  const ready = await request.get('/ready', { headers: { Accept: 'text/html,*/*' } });
  expect(ready.status()).toBe(200);
  expect(await ready.text()).toBe('{"status":"ok"}');
  const monitor = await request.get('/ready');
  expect(await monitor.text()).toBe('{"status":"ok"}');
});
