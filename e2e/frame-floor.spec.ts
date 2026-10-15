import { expect, test, type Page } from '@playwright/test';
import { expectFloor, expectBrandLinkLegible, expectChromeOneStep, expectFooterBounded, diagnosed, probe, refusals, report } from './design_floor';

// The four properties of the kernel's frame that every client inherits, each pinned as the thing
// a person meets rather than as a class name.
//
// The clients' own floor (`generated_pages_visual_floor_test.py` in septagon-clients, which runs
// `~/.local/share/pkit-999/gates/design_gate.py` over 16 dumped generated pages) refused 66 things
// on 16 pages, and four of the causes were in the frame every client inherits: the brand link
// carried no colour of its own (`a { color: inherit }` on an inverse column — 1.02:1 measured), the
// tenant name / caller line / build stamp were three body sizes on a page allowed two, the footer
// sentence spanned the column (189ch at 1440px), and a record was named by its UUID under a heading
// that could not break a token it could not hyphenate. None of those four is visible to a Go test:
// two of them are properties of the *computed* sheet, and one is a box width. So this spec is the
// case the fix needs, and `e2e/design_floor.ts` is the instrument.
//
// Everything below runs against a page behind a cookie, which the loop's gate cannot reach: the
// generated pages are only served to a signed-in caller. The fixture creates what it measures —
// one member with a name, one task whose title is one unbreakable token — and every number the
// probe prints goes to stdout, so the run is the measurement a report quotes.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
// A member of this spec's own, with a name, so "does a row name itself" has an answer that cannot
// come from anywhere else. The bootstrap administrator's own display name is what the bootstrap set,
// and a row found by sorting is a case that depends on sorting.
const member = {
  email: `frame-floor-${Date.now()}@e2e.test`,
  password: 'the-frame-floor-chooses-this-1',
  name: 'Floored Member',
};
const role = `frame_floor_${Date.now()}`;
// One token, no break opportunity: 152 characters, under the schema's own maxLength of 200 so the
// write is accepted and the heading is what fails. This is the shape of a UUID pasted into a title,
// a commit hash, or a URL — all of which a real tenant has in its task list.
const unbreakable = 'a'.repeat(152);
const tasks = '/app/task/tasks';
const users = '/app/user/users';

let unbreakableId = '';
let memberId = '';

test.beforeAll(async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto('/app/admin/login');
  await signIn(page);

  // The member, through the same three calls `refusal-floor.spec.ts` makes: a role of its own (so no other
  // spec's grant changes what this person can see), an invitation carrying the name, and a password.
  const created = await page.request.put(`/api/v1/auth/roles/${role}`, { data: { permissions: ['task:read'] } });
  expect(created.status(), await created.text()).toBe(200);
  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: member.email, displayName: member.name, roles: [role] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  memberId = (await invited.json()).id as string;
  expect(memberId).toBeTruthy();
  const activated = await page.request.post(`/api/v1/user/users/${memberId}/set-password`, {
    data: { password: member.password },
  });
  expect(activated.status(), await activated.text()).toBe(200);

  // The record with the title nothing can hyphenate — through the generated form, which is the
  // way a person writes a title, and which design-audit.spec.ts already proves works.
  await page.goto(`${tasks}/new`);
  await page.getByLabel('Title').fill(unbreakable);
  await page.getByLabel('Priority').selectOption('high');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(new RegExp(`${tasks}/[0-9a-f-]{36}$`));
  unbreakableId = page.url().split('/').pop() || '';
  expect(unbreakableId).toBeTruthy();

  await context.close();
});

async function signIn(page: Page) {
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
}

/** A context signed in as the administrator at one width — the frame only exists behind a cookie. */
async function signedInAt(browser: import('@playwright/test').Browser, width: number): Promise<Page> {
  const context = await browser.newContext({ viewport: { width, height: 900 } });
  const page = await context.newPage();
  await page.goto('/app/admin/login');
  await signIn(page);
  return page;
}

// Item 1, and the reason the brief says "the link is what a person tabs to and what a probe
// measures": the span beside the link has always carried FgOnInverse, and no probe or finger ever
// reaches the span. At 390px the admin sidebar is `hidden lg:flex`, so the link is painted from `lg`
// up and this is a wide-viewport case — which is the shape the clients' report had (refusals at
// 1440px, none at 390).
test('the brand link is legible as the link itself, not as the span beside it', async ({ browser }) => {
  test.setTimeout(60_000);
  const page = await signedInAt(browser, 1440);
  await page.goto(tasks);
  const m = await probe(page, 1440);
  console.log(`  brand link: ${JSON.stringify(m.brand_link)}`);
  console.log(`  worst on the page: ${JSON.stringify(m.worst_element)}`);
  expectBrandLinkLegible(m, 'the sidebar brand on a generated page');
  // The floor's own contrast rule over the whole page, kept as a separate assertion so a fix that
  // lights the link and darkens something else is caught rather than traded.
  expect(m.worst_contrast.ratio, diagnosed(m, `the worst text on a generated page is ${m.worst_contrast.ratio}:1 on "${m.worst_contrast.text}"`)).toBeGreaterThanOrEqual(4.5);
});

// Item 2, on the surface the brief names: "two of the three <p> sizes every generated page serves
// belong to the tenant name, the caller's id and the build stamp". Measured before the fix, a
// generated page's entire body-size set was [12,14,16] and all three came from the chrome, so the
// page itself had no step left. The hand-written dashboard's own copy inside <main> is a named
// follow-up (IMPLEMENT.md §5) and is not asserted here.
test('the frame\'s chrome is one body step, and a generated page stays inside the floor\'s two', async ({ browser }) => {
  test.setTimeout(90_000);
  for (const width of [390, 1440]) {
    const page = await signedInAt(browser, width);
    await page.goto(tasks);
    const m = await probe(page, width);
    console.log(`  generated list @${width}px chrome=${JSON.stringify(m.chrome)} body=${JSON.stringify(m.body_font_sizes)}`);
    expectChromeOneStep(m, `the chrome above a generated list at ${width}px`);
    // Signed in, the chrome is three lines: the tenant, the caller and the stamp.
    expect(m.chrome.length, `signed in at ${width}px the chrome should draw tenant, caller and stamp: ${JSON.stringify(m.chrome)}`).toBe(3);
    // The floor's rule, asserted as itself: the page gets a body size because the chrome takes one.
    expect(m.body_font_sizes.length, `a generated list at ${width}px sets body text in ${m.body_font_sizes.length} sizes ${JSON.stringify(m.body_font_sizes)}`)
      .toBeLessThanOrEqual(2);
  }
});

// Item 3. The footer spans the column — its top border is the frame's rule — so the bound sits on
// what is inside it. Before, the stamp's box was 1136px at 1440, which the probe reads as 189
// characters, because the measure rule divides a box by half the font size and a short sentence in
// a full-width block is still a 189-character measure.
test('the footer sentence has a bounded measure at the width the floor enforces', async ({ browser }) => {
  test.setTimeout(60_000);
  const page = await signedInAt(browser, 1440);
  await page.goto(tasks);
  const m = await probe(page, 1440);
  console.log(`  footer chrome=${JSON.stringify(m.chrome)} widest=${JSON.stringify(m.widest[0])}`);
  expectFooterBounded(m, 'the build stamp at 1440px');
  expect(m.measures_ch.max, `the widest paragraph on a generated page at 1440px is ${m.measures_ch.max}ch`).toBeLessThanOrEqual(75);
});

// Item 4, first half. An entity can now name the field its rows are called by (`ui:"display"`), and
// what the person meets is the sentence this pins: the row's only link says who the row is, and the
// record page opens with that name rather than with a UUID. Before, this list wore a column headed
// "Id", the record's <h1> was the id, and so was the browser tab, while "Display name" sat four
// columns later and was filled in.
test('a record names itself, in the list, on the record and in the tab', async ({ browser }) => {
  test.setTimeout(60_000);
  const page = await signedInAt(browser, 1440);

  await page.goto(users);
  const row = page.getByRole('link', { name: member.name, exact: true }).first();
  await expect(row, `no row on ${users} is called "${member.name}"`).toBeVisible();
  await expect(row).toHaveAttribute('href', new RegExp(`/app/user/users/${memberId}$`));
  // The id is not a column any more: it is the href, which is where an id belongs.
  const heading = await page.locator('table th').allInnerTexts();
  expect(heading.map((h) => h.replace(/↕|↑|↓/, '').trim()), `the id is still a column on ${users}: ${JSON.stringify(heading)}`)
    .not.toContain('Id');
  expect(await page.locator('table').innerText(), `the row link is named by the id it points at`).not.toContain(memberId);

  await page.goto(`${users}/${memberId}`);
  await expect(page.locator('h1')).toHaveText(member.name);
  await expect(page).toHaveTitle(new RegExp(member.name));
});

// Item 4, second half. A page with no break rule lets one long token set the min-content width of the
// column the heading sits in, so it scrolls sideways — measured before the fix at *both* 390 and 1440,
// which is the point: there was no rule to participate in sizing at all. overflow-wrap: anywhere is the
// value that does; break-words is the one that reads like the fix and is not. The rule sits on the
// frame's content region and inherits, which is what makes this a measurement of the heading's *computed*
// style rather than of a class on it: `components.clShellMain` sets it, and the Heading component
// carries none of its own because the design tool projects text only where the element computes ordinary
// line breaking.
test('a heading breaks a token nothing can hyphenate', async ({ browser }) => {
  test.setTimeout(90_000);
  for (const width of [390, 1440]) {
    const page = await signedInAt(browser, width);
    await page.goto(`${tasks}/${unbreakableId}`);
    const m = await probe(page, width);
    const box = await page.locator('h1').first().evaluate((e) => {
      const r = e.getBoundingClientRect();
      return { right: Math.round(r.right), style: getComputedStyle(e).overflowWrap };
    });
    console.log(`  unbreakable heading @${width}px overflowWrap=${box.style} h1.right=${box.right} of ${width} scrollWidth=${m.scroll_width_overflow}`);
    console.log(`  overflowing @${width}px: ${JSON.stringify(m.overflowing.slice(0, 8))}`);
    expect(m.scroll_width_overflow, diagnosed(m, `a record titled with one ${unbreakable.length}-character token scrolls sideways at ${width}px`)).toBe(false);
    expect(box.right, `the heading itself is wider than the viewport at ${width}px`).toBeLessThanOrEqual(width);
    expect(box.style, `the heading inherits no break rule from the frame at ${width}px`).toBe('anywhere');
  }
});

// The whole floor, on the two generated surfaces the clients measure. The four assertions above name
// the causes this brief fixes; this one is the claim the acceptance makes — a generated page in the
// kernel's own frame passes, at both widths the brief names, with the gate's eight rules unchanged and
// nothing filtered out. Until this run the record page carried one refusal of its own: the field help
// sentence, 189ch under a full-width control, which `components.clHelp` now bounds. Nothing is scoped
// out below, because the gate the clients run names no element and no cause — it reads the sentence
// `body measure up to 189ch`, and a rule switched off on one surface stays red on every client's.
test('a generated list and a generated record hold the whole design floor', async ({ browser }) => {
  test.setTimeout(120_000);
  for (const width of [390, 1440]) {
    const page = await signedInAt(browser, width);
    await page.goto(tasks);
    await expectFloor(page, `generated list ${tasks}`, width);
    await page.goto(`${tasks}/${unbreakableId}`);
    const m = await probe(page, width);
    console.log(report(`generated record ${unbreakableId}`, m));
    expect(refusals(m), diagnosed(m, `generated record ${unbreakableId} at ${width}px fails the design floor`)).toEqual([]);
  }
});
