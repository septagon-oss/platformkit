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

  // The member, through the same three calls `review-r3-refusal-floor.spec.ts` makes: a role of its own (so no other
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
    // Signed in, the chrome is the caller and the stamp — and, below the large breakpoint only, the
    // workspace's own name. That is item 5 of the rough-edges brief, and this is the line that has to
    // move with it: `components.ChromeContext` is the sidebar's mirror, painted where the sidebar is
    // not, so the name is in the header at 390px and hidden at 1440px where the sidebar carries it.
    // One name at exactly one width is the claim; the count of chrome sentences follows from it. What
    // the floor actually refuses — the chrome taking more than one body step, a page left with none —
    // is asserted on the two lines below this one and is unchanged.
    expect(m.chrome.length, `signed in at ${width}px the chrome should draw ${width < 1024 ? 'the workspace, the caller and the stamp' : 'the caller and the stamp'}: ${JSON.stringify(m.chrome)}`)
      .toBe(width < 1024 ? 3 : 2);
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

// Two more properties of the frame, both of them found by looking at a served page rather than at
// markup: a blank band between the sidebar and the content at desktop width, and a breadcrumb that
// split a record's name mid-word at phone width. Both are cured by one rule each, and both cures
// are invisible to the Go floor probes in this package, which assert the *class list* a component
// compiles to (`ui/components/sidebar_inner_width_test.go`, `breadcrumb_break_test.go`). A class
// list is a promise about the compiled sheet; the band and the split word are facts about boxes.
//
// The band's cause: the `<aside>` is the element that carries a width (`lg:w-64` expanded, `lg:w-16`
// collapsed) and paints nothing itself. The inverse column a person sees sits two levels inside it,
// and its wrapper is a flex item in a flex row, so with no width of its own it sized to its content
// and the leftover of the aside showed the page's own background — 43px of it, measured at 1440.
// The measurement below is therefore of three right edges, not of a class: the box that carries the
// width, the box that paints, and the content region that begins where the painting has to end.
test('the sidebar paints the whole width the aside carries, leaving no band beside the content', async ({ browser }) => {
  test.setTimeout(60_000);
  const page = await signedInAt(browser, 1440);
  await page.goto(tasks);
  const box = await page.evaluate(() => {
    const aside = document.querySelector('aside[data-component="sidebar"]');
    // aside > div (the column's own wrapper) > div (the column, which is what paints).
    const painted = aside?.firstElementChild?.firstElementChild ?? null;
    const main = document.querySelector('main');
    const edge = (el: Element | null, side: 'left' | 'right') =>
      el ? Math.round(el.getBoundingClientRect()[side]) : null;
    return {
      aside_right: edge(aside, 'right'),
      painted_right: edge(painted, 'right'),
      main_left: edge(main, 'left'),
      aside_width: aside ? Math.round(aside.getBoundingClientRect().width) : null,
    };
  });
  console.log(`  sidebar band @1440: ${JSON.stringify(box)}`);
  expect(box.aside_right, 'the frame drew no <aside data-component="sidebar"> to measure').not.toBeNull();
  expect(box.painted_right, 'the aside holds no painted column: nothing inside it is a box').not.toBeNull();
  // The 16rem the aside names, in px, at a breakpoint where `lg` applies.
  expect(box.aside_width, 'the sidebar is not the 16rem the aside says it is at 1440px').toBe(256);
  expect(Math.abs((box.aside_right ?? 0) - (box.painted_right ?? 0)), 'the painted column stops short of the aside that carries its width — the band').toBeLessThanOrEqual(1);
  expect(Math.abs((box.main_left ?? 0) - (box.painted_right ?? 0)), 'the content begins before the painted column ends, so the column is not what the person sees').toBeLessThanOrEqual(1);
});

// The trail's cure, measured on a record named in words. `components.clShellMain` sets
// `overflow-wrap: anywhere` on the content region and it inherits, which is right for prose and
// wrong for a trail: at 390px a crumb split "Dashboard" into "Dashboa" and "rd", and the page
// scrolled sideways. `break-normal` on each item is the refusal, `min-w-0` lets the links give
// way, and the current crumb carries no `truncate` because the name the person came to read is the
// one thing on the page that is never clipped. Each of those is a class the Go tests already pin;
// what only a browser can say is that the computed value arrives, that the name is not clipped,
// and that the page still does not scroll sideways.
test('a crumb breaks by words, and the record\'s own name is never clipped or split', async ({ browser }) => {
  test.setTimeout(120_000);
  const wordy = 'A record whose name is long enough to need more than one line at phone width';
  let wordyId = '';
  {
    const setup = await signedInAt(browser, 1440);
    await setup.goto(`${tasks}/new`);
    await setup.getByLabel('Title').fill(wordy);
    await setup.getByRole('button', { name: 'Save' }).click();
    await expect(setup).toHaveURL(new RegExp(`${tasks}/[0-9a-f-]{36}$`));
    wordyId = setup.url().split('/').pop() || '';
    await setup.context().close();
  }
  expect(wordyId).toBeTruthy();

  for (const width of [390, 1440]) {
    const page = await signedInAt(browser, width);
    await page.goto(`${tasks}/${wordyId}`);
    const m = await probe(page, width);
    const trail = await page.evaluate((name: string) => {
      const nav = document.querySelector('nav[data-component="breadcrumb"]');
      const list = nav?.querySelector('ol');
      const items = [...(nav?.querySelectorAll('li') ?? [])] as HTMLLIElement[];
      // The trail's own items — the links and the current entry. A separator is a `<li>` too, and
      // carries no word to break.
      const crumbs = items.filter((li) => li.querySelector('a') || li.getAttribute('aria-current') === 'page');
      const current = crumbs.find((li) => li.getAttribute('aria-current') === 'page');
      const line = current ? Math.round(parseFloat(getComputedStyle(current).lineHeight) || 20) : 0;
      return {
        count: crumbs.length,
        wraps: list ? getComputedStyle(list).flexWrap : null,
        // The region's rule reaching every crumb: a crumb that overrode it would read differently.
        overrides: crumbs.filter((li) => getComputedStyle(li).overflowWrap !== getComputedStyle(list!).overflowWrap).length,
        region: list ? getComputedStyle(list).overflowWrap : null,
        name: current?.textContent?.trim() ?? '',
        given: name,
        clipped: current ? current.scrollWidth - current.clientWidth : 0,
        height: current ? Math.round(current.getBoundingClientRect().height) : 0,
        line,
        scroll_width: document.documentElement.scrollWidth,
        inner: window.innerWidth,
      };
    }, wordy);
    console.log(`  crumb trail @${width}px: ${JSON.stringify(trail)}`);
    expect(trail.count, `the record page draws no breadcrumb items at ${width}px`).toBeGreaterThan(1);
    // The cure, as the browser computes it: the row wraps, so a crumb is given a line rather than a
    // share of one, and no crumb overrides the frame's own break rule. A squeezed crumb is what
    // split "Dashboard" into "Dashboa / rd"; a crumb that overrode the region's rule is what sent a
    // one-token name sideways past the viewport, which is the refusal this case exists to catch.
    expect(trail.wraps, `the trail does not wrap at ${width}px, so its crumbs are squeezed and split`).toBe('wrap');
    expect(trail.overrides, `a crumb overrides the region's break rule (${trail.region}) at ${width}px`).toBe(0);
    expect(trail.name, `the current crumb does not show the record's whole name at ${width}px`).toBe(trail.given);
    expect(trail.clipped, `the current crumb clips its own name at ${width}px`).toBeLessThanOrEqual(1);
    expect(trail.scroll_width, `the trail sets the page sideways at ${width}px`).toBeLessThanOrEqual(trail.inner);
    expect(m.scroll_width_overflow, diagnosed(m, `the record scrolls sideways at ${width}px`)).toBe(false);
    // At phone width the name has to be read in more than one line of whole words. One line means
    // it was clipped or scaled rather than broken.
    if (width === 390) {
      expect(trail.height, `the current crumb draws ${trail.height}px at ${width}px, which is not the ${trail.line}px line the name is set in`).toBeGreaterThan(trail.line);
    }
  }
});
