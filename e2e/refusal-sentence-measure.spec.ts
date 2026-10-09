import { expect, test } from '@playwright/test';

// The confirmation page a person lands on after they ask for access, measured
// rather than admired.
//
// The brief holds the design floor HIGH for "a page this delivery creates with any
// refusal", and gates/design_gate.py refuses a body line longer than 75 characters
// (`width / (font-size × 0.5)` over every visible <p> at the widths it probes). The
// refusal chrome this page joins is drawn with no frame at all — apps/platformkit/
// fault.go's `Frame` returns the body as it came — so its sentences run the full
// viewport. That is true of the refusal page at the merge base too, and this case is
// not about that page: it is about the one this branch added, and it asks only the
// confirmation page.
//
// The journey is reached the way the fixed behaviour serves it — the refused person's
// own session, the ask form the refusal page writes, the 303 it answers, the page at
// the other end — so nothing here depends on the defect: the assertion is a width,
// and the way to it is a redirect.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
const member = {
  email: `measure-${Date.now()}@e2e.test`,
  password: 'a-refused-person-chooses-this-1',
};

/** Every visible body line wider than the gate's 75ch, as sentences a person can read. */
async function wideLines(page: import('@playwright/test').Page) {
  return page.evaluate(() =>
    [...document.querySelectorAll('p')]
      .filter((e) => {
        const box = e.getBoundingClientRect();
        const s = getComputedStyle(e);
        return box.width > 40 && box.height > 6 && s.visibility !== 'hidden' && s.display !== 'none';
      })
      .map((e) => {
        const size = parseFloat(getComputedStyle(e).fontSize);
        return {
          text: (e.textContent ?? '').trim().replace(/\s+/g, ' ').slice(0, 40),
          ch: Math.round(e.getBoundingClientRect().width / (size * 0.5)),
        };
      })
      .filter((m) => m.ch > 75),
  );
}

test('the page that says an access request was sent keeps its sentences to a readable measure', async ({
  page,
  browser,
}) => {
  // The journey's own budget, named: 16 s of admin sign-in, invitation and password
  // at CI run 503, a second sign-in in a fresh context, the refusal, and this case's
  // 30 s bound on the ask's response — see `e2e/refusal-floor.spec.ts`.
  test.setTimeout(90_000);

  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: member.email, displayName: 'Measured Member', roles: ['member'] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const id = (await invited.json()).id as string;
  const activated = await page.request.post(`/api/v1/user/users/${id}/set-password`, {
    data: { password: member.password },
  });
  expect(activated.status(), await activated.text()).toBe(200);

  // 1440 is the width the gate probes and the width a laptop is. The person is refused
  // first, because the confirmation page belongs to the ask and the ask belongs to the
  // refusal.
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const person = await context.newPage();
  await person.goto('/app/admin/login');
  await person.getByLabel('Email').fill(member.email);
  await person.getByLabel('Password').fill(member.password);
  await person.getByRole('button', { name: 'Sign in' }).click();
  await expect(person).toHaveURL(/\/app$/);

  const refusal = await person.goto('/app/task/tasks');
  expect(refusal?.status(), 'the member was not refused, so this journey has no start').toBe(403);

  // The ask is waited for as the 303 `ui/page/access.go` answers with, for the reason
  // and the measured numbers written out in `e2e/refusal-floor.spec.ts` — this case
  // was the second one CI run 503 refused, at 15:45:26Z, on the URL alone.
  const asked = person.waitForResponse((r) => r.url().endsWith('/app/access-request'), { timeout: 30_000 });
  await person.locator('form[action="/app/access-request"] button[type="submit"]').click();
  const answer = await asked;
  // The body of a redirect is not readable — Playwright says so — so the sentence a
  // red prints carries the Location for the answer that works and the problem
  // document for the 422, 429 and 503 kit/httpx/access.go can answer instead.
  const detail = answer.status() >= 400 ? (await answer.text()).slice(0, 300)
    : `Location=${answer.headers()['location'] ?? 'none'}`;
  expect(answer.status(), `the ask answered ${answer.status()}: ${detail}`).toBe(303);
  await expect(person).toHaveURL(/\/app\/access-request\/sent$/);

  const wide = await wideLines(person);
  expect(wide, 'body lines over the 75-character measure on the access-request confirmation').toEqual([]);

  await context.close();
});
