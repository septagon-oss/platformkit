import { expect, test, type Page } from '@playwright/test';

// The instant a page shows is said twice: once by the server, in UTC, into the `datetime`
// attribute and the element's own text, and once by the reader's own engine, which is the only
// thing on either side that knows what zone the person is sitting in. `ui/assets/js/components.js`
// is the second half, and no Go test can run it: `ui/resource`'s cases prove the element, its
// instant and the server's words, and `ui`'s node case runs the script against a hand-built
// element. What neither can prove is the claim the brief makes — that a person, in their own
// browser, in their own language, reads the moment where they are.
//
// This is that case, and it is the one that would have caught the defect this file was written
// against: `tellTime` read a binding in its own temporal dead zone, threw on every call, and the
// `try`/`catch` around it in `init()` swallowed the throw, so every page on the instance quietly
// kept the server's UTC. Every rendered-HTML test in the repository passed. The browser was the
// only witness.
//
// Two choices below are load-bearing. The clock is pinned before each navigation, because the
// script reads `Date.now()` once, at init — an unpinned run measures a race with the server's
// clock instead of the reader's. And the zone is Asia/Kolkata rather than a European one: it is
// UTC+5:30 all year, so "the reader's clock is not UTC" is true on the day the suite runs, in
// whatever month the suite runs, and the half-hour offset means a title that merely copied the
// server's minutes cannot pass.
//
// The record is written through the API: the form that writes it is labelled in the language of
// the page, and one of the cases below is about that language.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
const tasks = '/app/task/tasks';
const readerZone = 'Asia/Kolkata';
// display.Moment: what the server writes as the element's text, and the same instant with seconds
// and `UTC` as the title. Both are the fallback for a browser that never speaks, and the evidence
// that it did not.
const serverText = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/;
const serverTitle = /\d{2}:\d{2}:\d{2} UTC$/;

/** The session, then one record of this test's own, and its address. */
async function signedInRecord(page: Page, title: string): Promise<string> {
  test.setTimeout(90_000);
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  const password = page.locator('input[type="password"]');
  await password.fill(admin.password);
  // Enter rather than the button: the button's label is the page's language, and one case below is
  // about that language.
  await password.press('Enter');
  await expect(page).toHaveURL(/\/app$/);
  const created = await page.request.post('/api/v1/task/tasks', { data: { title } });
  expect(created.status(), await created.text()).toBe(201);
  const id = (await created.json()).id as string;
  expect(id).toBeTruthy();
  return `${tasks}/${id}`;
}

/** Every `<time datetime>` the page drew, as the reader sees it and as the server wrote it. */
async function said(page: Page) {
  const times = page.locator('time[datetime]');
  const count = await times.count();
  expect(count, `the page drew no <time datetime> at all:\n${await page.locator('main').innerText()}`).toBeGreaterThan(0);
  const rows = await times.evaluateAll((els) => els.map((el) => ({
    text: (el.textContent ?? '').trim(),
    datetime: el.getAttribute('datetime') ?? '',
    title: el.getAttribute('title') ?? '',
  })));
  console.log(`  times: ${JSON.stringify(rows)}`);
  return rows;
}

/** The instant written both ways that matter: the reader's wall clock, and UTC's. */
function clocks(instant: string) {
  const at = new Date(instant);
  const atClock = (zone: string) => new Intl.DateTimeFormat('en-GB', {
    timeZone: zone, hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  }).format(at).replace(/^0/, '');
  const half = new Intl.DateTimeFormat('en-US', {
    timeZone: readerZone, hour: 'numeric', minute: '2-digit', second: '2-digit', hour12: true,
  }).format(at).replace(/\s*[AP]M\s*$/i, '').replace(/^0/, '');
  return { local: atClock(readerZone), local12: half, utc: atClock('UTC') };
}

test.use({ timezoneId: readerZone });

// The two halves of the sentence: under a week, the words a person uses for how long ago; and in
// the title, the exact moment read where they are. A title written in the reader's own zone cannot
// be the server's UTC stamp — in a zone that is five and a half hours ahead of UTC in every month
// of the year, the clock it shows is a different clock.
test('a recent instant reads as the reader\'s own words, and its exact moment reads in their own zone', async ({ page }) => {
  const record = await signedInRecord(page, 'The workspace loses its rough edges');
  await page.clock.install({ time: new Date(Date.now() + 6 * 60_000) });
  await page.goto(record, { waitUntil: 'domcontentloaded' });
  for (const row of await said(page)) {
    expect(row.text, `the <time> still says the server's sentence ${JSON.stringify(row.text)}`).not.toMatch(serverText);
    expect(row.title, `the title still says the server's zone ${JSON.stringify(row.title)}`).not.toMatch(serverTitle);
    // The instant the server wrote is untouched: the page re-says the moment, it does not move it.
    expect(row.datetime, `the datetime is no longer the instant the server wrote`).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
    const at = clocks(row.datetime);
    expect(at.local === at.utc, `this instant reads the same in ${readerZone} as in UTC, so nothing below proves anything: ${row.datetime}`).toBe(false);
    const shown = (row.title.match(/\d{1,2}:\d{2}:\d{2}/g) ?? []).map((c) => c.replace(/^0/, ''));
    expect(shown.includes(at.utc), `the title still carries the server's own UTC clock: ${JSON.stringify(row.title)}`).toBe(false);
    expect(
      shown.some((c) => c === at.local || c === at.local12),
      `the title shows no clock but the server's; the reader's own reads ${at.local} (or ${at.local12} in twelve-hour form): ${JSON.stringify(row.title)}`,
    ).toBe(true);
  }
  // And the words themselves, in the language the page is written in: six minutes is the largest
  // unit at least one of, long style — a sentence, not a number.
  expect((await said(page)).map((r) => r.text), 'a minute past six should read as six minutes ago').toContain('6 minutes ago');
});

// A week is the edge the brief names, and it is crossed by moving the reader's clock rather than
// the record: the same instant is words to one side of it and a date to the other. Two cases, one
// clock each — a session is signed in once, and a page that has been read is not the same page a
// person arrives at with their clock set elsewhere.
test('an instant a week old is read as a date, not counted', async ({ page }) => {
  const eightDays = await signedInRecord(page, 'Eight days of the same workspace');
  await page.clock.install({ time: new Date(Date.now() + 8 * 86_400_000) });
  await page.goto(eightDays, { waitUntil: 'domcontentloaded' });
  for (const row of await said(page)) {
    expect(row.text, `eight days is beyond the week, so it is a date and not a countdown: ${row.text}`).not.toMatch(serverText);
    expect(row.text, `a date beyond the week, and still a relative phrase: ${row.text}`).not.toMatch(/\bago\b/);
    const year = new Intl.DateTimeFormat('en', { timeZone: readerZone, year: 'numeric' }).format(new Date(row.datetime));
    expect(row.text, `the date shown is not the year the reader's calendar is in: ${row.text}`).toContain(year);
  }
});

// The same page read an hour early: the instant has not happened yet, and nothing on the page
// knows whether the field is a deadline, so it is a date rather than a countdown to it.
test('an instant still to come is shown as a date and never counted down', async ({ page }) => {
  const anHourAhead = await signedInRecord(page, 'An hour that has not arrived');
  await page.clock.install({ time: new Date(Date.now() - 60 * 60_000) });
  await page.goto(anHourAhead, { waitUntil: 'domcontentloaded' });
  for (const row of await said(page)) {
    expect(row.text, `a future instant must never be counted down: ${row.text}`).not.toMatch(/^(in|em) /);
    expect(row.text, `a future instant fell back to the server's UTC text: ${row.text}`).not.toMatch(serverText);
  }
});

// The refusal the whole thing rests on: a browser with no Intl keeps the server's sentence, its
// instant and its title rather than a blank cell. Nothing is invented for a reader the machine
// cannot speak for.
test.describe('a browser without the formatter', () => {
  test.beforeEach(async ({ context }) => {
    await context.addInitScript(() => {
      (globalThis as Record<string, unknown>).Intl = undefined;
    });
  });

  test('the server\'s sentence, its instant and its title survive untouched', async ({ page }) => {
    const record = await signedInRecord(page, 'What the server said stands on its own');
    await page.goto(record, { waitUntil: 'domcontentloaded' });
    for (const row of await said(page)) {
      expect(row.text, `with no Intl the server's own words stand, and these are not them: ${row.text}`).toMatch(serverText);
      expect(row.title, `with no Intl the server's own title stands, and this is not it: ${row.title}`).toMatch(serverTitle);
      expect(row.datetime, 'the instant the server wrote went missing').toBeTruthy();
    }
  });
});

// The same page, the same instant, the reader's own language. No catalogue is consulted: Intl
// speaks the language the document declares, which is the one the locale path negotiated for the
// request, so this case is a browser header and nothing else.
test.describe('in Portuguese', () => {
  test.use({ locale: 'pt-PT' });

  test('an instant is said in the language the page declares', async ({ page }) => {
    const record = await signedInRecord(page, 'A mesma altura, noutra língua');
    // The dashboard is English by its own declaration (modules/admin's `writtenHere`), and this is
    // the line that says the case below is not measuring that page by accident.
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await page.clock.install({ time: new Date(Date.now() + 6 * 60_000) });
    // A generated screen declares the negotiated language — its document.View.Language is empty —
    // so at a pt-PT browser it declares pt-PT. If this fails, the premise of the two assertions
    // below is the thing that broke: the page, not the formatter.
    await page.goto(record, { waitUntil: 'domcontentloaded' });
    await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
    for (const row of await said(page)) {
      expect(row.text, `a pt-PT page shows the reader's own words, and these are English: ${row.text}`).toMatch(/^há \d+ minutos$/);
      expect(row.title, `the title is not the server's, and not the reader's either: ${row.title}`).not.toMatch(serverTitle);
    }
  });
});
