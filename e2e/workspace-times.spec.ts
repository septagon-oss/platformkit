import { expect, test, type Page } from '@playwright/test';

// The instant a page shows is said twice: once by the server, in UTC, into the `datetime`
// attribute and the element's own text, and once by the reader's own engine, which is the only
// thing on either side that knows what zone the person is sitting in. `ui/assets/js/components.js`
// is the second half, and no Go test can run it: `ui/resource`'s cases prove the element, its
// instant and the server's words, and `ui`'s node case runs the script against a hand-built
// element. What neither can prove is the claim the page makes — that a person, in their own
// browser, in their own language, reads the moment where they are.
//
// The spelling of that moment is the resolved policy's, and it is numeric: an absolute instant
// reads `YYYY-MM-DD HH:MM` in the zone the page resolves it in, at the minute a person compares.
// Wording about age belongs to a field that asked for it (`format:relative`), and no field here
// has asked, so one instant is spelled like another whether it is six minutes old, eight days old
// or still to come. That is what these cases read: the shape cannot tell the reader's minute from
// the server's, because the two are the same sixteen characters some hours apart, so every case
// compares the *value* the browser wrote against the instant read where the reader is.
//
// This is the case that would have caught the defect this file was written against: `tellTime`
// read a binding in its own temporal dead zone, threw on every call, and the `try`/`catch`
// around it in `init()` swallowed the throw, so every page on the instance quietly kept the
// server's UTC. Every rendered-HTML test in the repository passed. The browser was the only
// witness.
//
// Two choices below are load-bearing. The clock is pinned before each navigation, because the
// script reads `Date.now()` once, at init — an unpinned run measures a race with the server's
// clock instead of the reader's. And the zone is Asia/Kolkata rather than a European one: it is
// UTC+5:30 all year, so "the reader's clock is not UTC" is true on the day the suite runs, in
// whatever month the suite runs, and the half-hour offset means a minute that merely copied the
// server's minutes cannot pass. The expected minute is then that offset added by hand, which is
// arithmetic rather than the same zone lookup the page under test performs.
//
// The record is written through the API: the form that writes it is labelled in the language of
// the page, and one of the cases below is about that language.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
const tasks = '/app/task/tasks';
const readerZone = 'Asia/Kolkata';
// Asia/Kolkata answers this offset for every instant in the year, with no summer time to move it.
const readerOffset = 5.5 * 3600;
// What the server writes when the page names no location of its own: the instant in UTC at the
// minute a person reads, and its zone-explicit RFC3339 title. Both stand for a browser that never
// speaks, which is the case `a browser without the formatter` below walks.
const serverText = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/;
const serverTitle = /T\d{2}:\d{2}:\d{2}Z$/;

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

/** Every `<time>` the page drew: what the reader is shown, and what the server wrote. */
async function said(page: Page) {
  const times = page.locator('time[datetime]');
  const count = await times.count();
  expect(count, `the page drew no <time datetime> at all:\n${await page.locator('main').innerText()}`).toBeGreaterThan(0);
  const rows = await times.evaluateAll((els) => els.map((el) => ({
    text: (el.textContent ?? '').trim(),
    datetime: el.getAttribute('datetime') ?? '',
    title: el.getAttribute('title') ?? '',
    kind: el.dataset.timeKind ?? '',
    relative: el.dataset.timeRelative ?? '',
    zone: el.dataset.timeZone ?? '',
    unit: el.dataset.timeUnit ?? '',
    language: el.dataset.timeLanguage ?? '',
  })));
  console.log(`  times: ${JSON.stringify(rows)}`);
  return rows;
}

/** The zone the reader's own engine answers for — the name the title carries. */
function engineZone(page: Page): Promise<string> {
  return page.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone);
}

/** The instant as written, without the sub-second digits no case below compares. */
function toTheSecond(instant: string): string {
  const whole = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(\.\d+)?(Z|[+-]\d{2}:\d{2})$/.exec(instant);
  expect(whole, `datetime is not the instant these cases read: ${instant}`).not.toBeNull();
  return `${whole![1]}${whole![3]}`;
}

/** One instant's `YYYY-MM-DD HH:MM` spelling `offset` seconds east of UTC — arithmetic, not a zone lookup. */
function minuteWhere(offset: number, instant: string): string {
  const parts = Object.fromEntries(new Intl.DateTimeFormat('en-CA', {
    timeZone: 'UTC', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).formatToParts(new Date(Date.parse(toTheSecond(instant)) + offset * 1000)).map((p) => [p.type, p.value]));
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`;
}

/** The offset the reader's zone answers for one instant, in seconds — the premise of adding it by hand. */
function offsetOf(instant: string, zone: string): number {
  const named = new Intl.DateTimeFormat('en-GB', { timeZone: zone, timeZoneName: 'longOffset' })
    .formatToParts(new Date(toTheSecond(instant))).find((p) => p.type === 'timeZoneName')?.value ?? '';
  const difference = /^GMT([+-])(\d{1,2}):(\d{2})$/.exec(named);
  expect(difference, `${zone} answers no whole offset for ${instant}: ${named}`).not.toBeNull();
  const [, sign, hours, minutes] = difference!;
  return (sign === '-' ? -1 : 1) * (Number(hours) * 3600 + Number(minutes) * 60);
}

/** What every case below reads of one `<time>`, before it reads the moment itself. */
function itIsAnAbsoluteInstant(row: { kind: string; relative: string; zone: string; unit: string; datetime: string }) {
  // The premise, named rather than assumed: this page's fields declared no relative reading, so
  // nothing here may be counted, and the moment the reader is shown is the one their own zone
  // answers — which needs a page that named no location for the value.
  expect(row.kind, 'a task timestamp is a moment, not a calendar date').toBe('instant');
  expect(row.relative, 'the field was declared absolute, and the policy says otherwise').toBe('false');
  expect(row.zone, 'the page names a location of its own, so the reader\'s zone is not the one read here').toBe('');
  expect(row.unit, 'an absolute instant wrote the unit of a relative reading').toBe('');
  // The instant the server wrote is untouched: the page re-says the moment, it does not move it.
  expect(row.datetime, 'the datetime is no longer the instant the server wrote').toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
  expect(offsetOf(row.datetime, readerZone), `${readerZone} is not the offset these cases add by hand, so they prove nothing`).toBe(readerOffset);
}

test.use({ timezoneId: readerZone });

// The moment is said in the zone the person is sitting in, to the minute the policy names, and
// the title says which zone that minute is in. A title in the reader's own zone cannot be the
// server's UTC stamp — in a zone five and a half hours ahead of UTC in every month of the year,
// the two clocks read differently — and neither can the minute the page shows.
test('a recent instant reads as the numeric minute of the reader\'s own zone', async ({ page }) => {
  const record = await signedInRecord(page, 'The workspace loses its rough edges');
  await page.clock.install({ time: new Date(Date.now() + 6 * 60_000) });
  await page.goto(record, { waitUntil: 'domcontentloaded' });
  const zone = await engineZone(page);
  for (const row of await said(page)) {
    itIsAnAbsoluteInstant(row);
    const at = minuteWhere(readerOffset, row.datetime);
    expect(row.text, `the page does not say the reader's own minute, which is ${at} and not the server's ${minuteWhere(0, row.datetime)}: ${JSON.stringify(row.text)}`).toBe(at);
    expect(row.title, `the title names no minute and no zone it is read in: ${JSON.stringify(row.title)}`).toBe(`${at} ${zone}`);
  }
});

// Age changes nothing about an absolute instant: the same page read with the reader's clock eight
// days on says the moment the same way it said it six minutes after the write, in the same
// numbers, with no countdown and no words about how long ago.
test('an instant eight days old reads in the same numbers as one six minutes old', async ({ page }) => {
  const eightDays = await signedInRecord(page, 'Eight days of the same workspace');
  await page.clock.install({ time: new Date(Date.now() + 8 * 86_400_000) });
  await page.goto(eightDays, { waitUntil: 'domcontentloaded' });
  for (const row of await said(page)) {
    itIsAnAbsoluteInstant(row);
    expect(row.text, `eight days on, the moment still reads as the reader's minute: ${row.text}`).toBe(minuteWhere(readerOffset, row.datetime));
    expect(row.text, `an absolute instant fell into wording about its age: ${row.text}`).not.toMatch(/[a-z]/i);
    const year = new Intl.DateTimeFormat('en', { timeZone: readerZone, year: 'numeric' }).format(new Date(toTheSecond(row.datetime)));
    expect(row.text, `the date shown is not the year the reader's calendar is in: ${row.text}`).toContain(year);
  }
});

// The same page read an hour early: the instant has not happened yet. Nothing on the page knows
// whether the field is a deadline, so nothing counts down to it — and it is not a different mode
// either, only the same numbers for an instant on the far side of now.
test('an instant still to come reads in the same numbers and is never counted down', async ({ page }) => {
  const anHourAhead = await signedInRecord(page, 'An hour that has not arrived');
  await page.clock.install({ time: new Date(Date.now() - 60 * 60_000) });
  await page.goto(anHourAhead, { waitUntil: 'domcontentloaded' });
  for (const row of await said(page)) {
    itIsAnAbsoluteInstant(row);
    expect(row.text, `a future instant must never be counted down: ${row.text}`).toBe(minuteWhere(readerOffset, row.datetime));
    expect(row.text, `a future instant fell into a countdown: ${row.text}`).not.toMatch(/[a-z]/i);
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

// The same page, the same instant, the reader's own language. No catalogue is consulted for the
// numbers themselves — a minute is the same digits in either language — so what this case reads
// is that the language reached the element, that the page is served in it, and that the digits the
// reader is shown are their own minute in it.
test.describe('in Portuguese', () => {
  test.use({ locale: 'pt-PT' });

  test('an instant is the same numeric minute in the language the page declares', async ({ page }) => {
    const record = await signedInRecord(page, 'A mesma altura, noutra língua');
    // The dashboard and generated record both use the negotiated request language.
    await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
    await page.clock.install({ time: new Date(Date.now() + 6 * 60_000) });
    // A generated screen declares the negotiated language — its document.View.Language is empty —
    // so at a pt-PT browser it declares pt-PT. If this fails, the premise of the assertions
    // below is the thing that broke: the page, not the formatter.
    await page.goto(record, { waitUntil: 'domcontentloaded' });
    await expect(page).toHaveURL(record);
    await expect(page.locator('html')).toHaveAttribute('lang', 'pt-PT');
    const zone = await engineZone(page);
    for (const row of await said(page)) {
      itIsAnAbsoluteInstant(row);
      expect(row.language, 'the element carries no language for its browser to read the moment in').toBe('pt-PT');
      const at = minuteWhere(readerOffset, row.datetime);
      expect(row.text, `a pt-PT page shows the same numeric minute the reader's zone answers: ${row.text}`).toBe(at);
      expect(row.title, `the title names no minute and no zone it is read in: ${JSON.stringify(row.title)}`).toBe(`${at} ${zone}`);
    }
  });
});
