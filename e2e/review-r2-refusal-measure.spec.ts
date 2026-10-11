import { expect, test } from '@playwright/test';

// The second review's pin over the page this delivery created: the confirmation a
// person lands on after they ask for access, measured rather than admired.
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
  test.setTimeout(60_000);

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

  await person.locator('form[action="/app/access-request"] button[type="submit"]').click();
  await expect(person).toHaveURL(/\/app\/access-request\/sent$/);

  const wide = await wideLines(person);
  expect(wide, 'body lines over the 75-character measure on the access-request confirmation').toEqual([]);

  await context.close();
});
