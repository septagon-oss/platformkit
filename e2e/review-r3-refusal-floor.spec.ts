import { expect, test } from '@playwright/test';

// The third review's pin over the page this delivery *changed*: the refusal a
// signed-in person is answered with.
//
// The brief holds the design floor HIGH for a page a delivery creates or changes,
// and the floor is `~/.local/share/pkit-999/gates/design_gate.py` — a tool no goal
// in this repository runs, whose probe takes no cookie, so it can only ever measure
// the sign-in page an anonymous visitor is redirected to and never the refusal a
// member is actually shown. The one case the second review left measures the ask's
// confirmation page and nothing else; the refusal page itself — the page this branch
// gave its three new sentences, its granter line and its ask form — has never been
// measured by anything.
//
// Every number below is the gate's own rule, read off that file's `rules()`: at most
// two left edges, two body sizes, six font sizes, one primary CTA above the fold,
// four section gaps, a measure of at most 75 characters at widths of 1280 and up, no
// contrast under 4.5:1 for text below 24px, and no sideways scroll. Two widths,
// 390 and 1440, because the brief names those two.
//
// Nothing here reads a sentence the page might or might not carry: the journey is
// reached by the verdicts the correct behaviour answers with (403 for the refused
// person, 303 for the ask, 200 for the page behind it) and the assertion is geometry.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
const person = {
  email: `r3-floor-${Date.now()}@e2e.test`,
  password: 'a-refused-person-chooses-this-1',
};
// A role of this spec's own, holding a grant nothing to do with tasks has to do
// with: whoever else ticks or unticks a shared role, this person stays refused at
// /app/task/tasks. Sorting must not decide whether a journey can start.
const role = `r3_nothing_${Date.now()}`;
const refusedScreen = '/app/task/tasks';

type Probe = {
  width: number;
  left_edges: number[];
  body_font_sizes: number[];
  all_font_sizes: number[];
  worst_contrast: { ratio: number; text: string; size: number };
  primary_ctas_above_fold: number;
  section_gaps: number[];
  measures_ch: { min: number; max: number };
  scroll_width_overflow: boolean;
  // The sentences, kept in the failure message so a red says which line is too long.
  widest: { ch: number; text: string }[];
};

// The gate's own probe (gates/design_probe.mjs), same selectors, same formulas.
async function probe(page: import('@playwright/test').Page, width: number): Promise<Probe> {
  return page.evaluate((w) => {
    const px = (v: string) => Math.round(parseFloat(v) || 0);
    const vis = (e: Element) => {
      const r = e.getBoundingClientRect();
      const s = getComputedStyle(e);
      return r.width > 40 && r.height > 6 && s.visibility !== 'hidden' && s.display !== 'none';
    };
    const parse = (c: string | null) => {
      const m = c && c.match(/rgba?\(([^)]+)\)/);
      if (!m) return null;
      const [r, g, b, a = 1] = m[1].split(',').map(Number);
      return { r, g, b, a };
    };
    const lum = (c: { r: number; g: number; b: number }) => {
      const f = (v: number) => (v /= 255) <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
      return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
    };
    const contrast = (a: any, b: any) => {
      const [x, y] = [lum(a), lum(b)];
      return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
    };
    const bgOf = (e: Element | null) => {
      for (let n: any = e; n; n = n.parentElement) {
        const c = parse(getComputedStyle(n).backgroundColor);
        if (c && c.a > 0) return c;
      }
      return { r: 255, g: 255, b: 255, a: 1 };
    };
    const text = [...document.querySelectorAll('h1,h2,h3,h4,p,li,a,button,label,span,td,th')]
      .filter(vis).filter((e) => (e.textContent || '').trim().length > 1);
    const blocks = [...document.querySelectorAll('h1,h2,h3,p,section,article,main > *,header,footer')].filter(vis);
    const lefts = [...new Set(blocks.filter((e) => /^(H[1-3]|P)$/.test(e.tagName))
      .map((e) => px(e.getBoundingClientRect().left)))].sort((a, b) => a - b);
    const clusters: number[] = [];
    for (const l of lefts) if (!clusters.length || l - clusters[clusters.length - 1] > 3) clusters.push(l);
    const bodySizes = [...new Set([...document.querySelectorAll('p')].filter(vis)
      .map((e) => px(getComputedStyle(e).fontSize)))].sort((a, b) => a - b);
    const allSizes = [...new Set(text.map((e) => px(getComputedStyle(e).fontSize)))].sort((a, b) => a - b);
    let worst = { ratio: 99, text: '', size: 0 };
    for (const e of text) {
      const fg = parse(getComputedStyle(e).color);
      if (!fg || fg.a === 0) continue;
      const r = contrast(fg, bgOf(e));
      if (r < worst.ratio) {
        worst = { ratio: Math.round(r * 100) / 100, text: (e.textContent || '').trim().slice(0, 40),
          size: px(getComputedStyle(e).fontSize) };
      }
    }
    const ctas = [...document.querySelectorAll('a,button')].filter(vis)
      .filter((e) => { const c = parse(getComputedStyle(e).backgroundColor); return !!c && c.a > 0 && (c.r + c.g + c.b) < 600; })
      .filter((e) => e.getBoundingClientRect().top < innerHeight);
    const byColour: Record<string, number> = {};
    for (const e of ctas) { const k = getComputedStyle(e).backgroundColor; byColour[k] = (byColour[k] || 0) + 1; }
    const primary = Object.entries(byColour).sort((a, b) => b[1] - a[1])[0];
    const tops = [...document.querySelectorAll('main > *, body > * > section, body > section')].filter(vis)
      .map((e) => e.getBoundingClientRect()).sort((a, b) => a.top - b.top);
    const gaps: number[] = [];
    for (let i = 1; i < tops.length; i++) gaps.push(px(tops[i].top - tops[i - 1].bottom));
    const gapSet = [...new Set(gaps.filter((g) => g >= 0))].sort((a, b) => a - b);
    const paras = [...document.querySelectorAll('p')].filter(vis).map((e) => ({
      ch: Math.round(e.getBoundingClientRect().width / (px(getComputedStyle(e).fontSize) * 0.5)),
      text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 50),
    })).sort((a, b) => b.ch - a.ch);
    return {
      width: w,
      left_edges: clusters,
      body_font_sizes: bodySizes,
      all_font_sizes: allSizes,
      worst_contrast: worst,
      primary_ctas_above_fold: primary ? primary[1] : 0,
      section_gaps: gapSet,
      measures_ch: { min: paras.length ? paras[paras.length - 1].ch : 999, max: paras.length ? paras[0].ch : 0 },
      scroll_width_overflow: document.documentElement.scrollWidth > innerWidth,
      widest: paras.slice(0, 3),
    };
  }, width);
}

// The floor, one line per rule, in the gate's own wording and its own numbers.
function refusals(m: Probe): string[] {
  const r: string[] = [];
  if (m.left_edges.length > 2) r.push(`${m.width}px: ${m.left_edges.length} left edges ${JSON.stringify(m.left_edges)} — one content column and at most one rail`);
  if (m.body_font_sizes.length > 2) r.push(`${m.width}px: body text in ${m.body_font_sizes.length} sizes ${JSON.stringify(m.body_font_sizes)} — body size is constant down the page`);
  if (m.all_font_sizes.length > 6) r.push(`${m.width}px: ${m.all_font_sizes.length} font sizes ${JSON.stringify(m.all_font_sizes)} — a type scale has at most 6 steps`);
  if (m.primary_ctas_above_fold > 1) r.push(`${m.width}px: ${m.primary_ctas_above_fold} primary buttons above the fold — one thing to do per view`);
  if (m.worst_contrast.ratio < 4.5 && m.worst_contrast.size < 24) r.push(`${m.width}px: contrast ${m.worst_contrast.ratio} on "${m.worst_contrast.text}" — below 4.5:1`);
  if (m.section_gaps.length > 4) r.push(`${m.width}px: ${m.section_gaps.length} distinct section gaps ${JSON.stringify(m.section_gaps)} — spacing comes from a scale`);
  if (m.width >= 1280 && m.measures_ch.max > 75) r.push(`${m.width}px: body measure up to ${m.measures_ch.max}ch — keep lines under 75 characters`);
  if (m.scroll_width_overflow) r.push(`${m.width}px: the page scrolls sideways`);
  return r;
}

async function expectFloor(page: import('@playwright/test').Page, label: string, width: number) {
  const m = await probe(page, width);
  console.log(`  ${label} @${m.width}px edges=${JSON.stringify(m.left_edges)} body=${JSON.stringify(m.body_font_sizes)} ` +
    `sizes=${m.all_font_sizes.length} cta=${m.primary_ctas_above_fold} contrast=${m.worst_contrast.ratio} ` +
    `gaps=${JSON.stringify(m.section_gaps)} measure=${m.measures_ch.max} widest=${JSON.stringify(m.widest[0])}`);
  expect(refusals(m), `${label} at ${m.width}px fails the design floor`).toEqual([]);
}

test('the refusal page a signed-in person is shown holds the design floor at 390 and at 1440', async ({ page, browser }) => {
  test.setTimeout(90_000);

  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  const created = await page.request.put(`/api/v1/auth/roles/${role}`, { data: { permissions: ['audit:read'] } });
  expect(created.status(), await created.text()).toBe(200);
  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: person.email, displayName: 'Floored Member', roles: [role] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const id = (await invited.json()).id as string;
  const activated = await page.request.post(`/api/v1/user/users/${id}/set-password`, {
    data: { password: person.password },
  });
  expect(activated.status(), await activated.text()).toBe(200);

  for (const width of [390, 1440]) {
    const context = await browser.newContext({ viewport: { width, height: 900 } });
    const p = await context.newPage();
    await p.goto('/app/admin/login');
    await p.getByLabel('Email').fill(person.email);
    await p.getByLabel('Password').fill(person.password);
    await p.getByRole('button', { name: 'Sign in' }).click();
    await expect(p).toHaveURL(/\/app$/);

    // The way in is the verdict, not a sentence: a person who is not refused has
    // no refusal page to measure, and that is a broken fixture, not a green case.
    const refusal = await p.goto(refusedScreen);
    expect(refusal?.status(), 'the member was not refused, so this page has no start').toBe(403);
    await expectFloor(p, 'the refusal page', width);

    // The confirmation page at the width the second review's case did not probe.
    await p.locator('form[action="/app/access-request"] button[type="submit"]').click();
    await expect(p).toHaveURL(/\/app\/access-request\/sent$/);
    await expectFloor(p, 'the ask confirmation', width);

    await context.close();
  }
});

// The third review's second pin, and the one item rounds 1 and 2 both left unverified: two writes
// that each take away a grant the tenant cannot lose, at the same moment.
//
// Rule 8 of the house rules is *refuse the write that takes the last one away, never the write that
// finds none*, and the third test of e2e/admin-roles.spec.ts pins its single-writer half: one
// administrator unticking the last box is refused. The half nobody drove is the pair — two
// administrators standing down at once, two tabs, each emptying one of the only two roles that
// grant role management. Either write alone leaves somebody who can administer, so a floor that
// reads "is there another administering role" and then writes says yes twice about a fact that
// stops being true in between, and the tenant ends with a role nobody holds and nobody who can
// grant anything. `modules/auth`'s own comment names this exact double-submit as the reason for
// its `pg_advisory_xact_lock`; nothing in the tree drove it.
//
// The assertion is read off the two verdicts, so it needs no privileged read to state it: at most
// one of the two writes may be accepted. It has a passing branch because one write is allowed and
// the other is refused by the floor (200 and 422); a tree that lost the lock answers 200 twice.
test('two concurrent writes cannot take the last administering grant away between them', async ({ page }) => {
  test.setTimeout(60_000);

  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);
  const me = (await (await page.request.get('/api/v1/auth/me')).json()) as { userId?: string };
  const actor = me.userId ?? '';
  expect(actor).toBeTruthy();

  // Two administering roles, both held by the signed-in administrator, who holds the wildcard
  // through `admin`. The grant is a promotion, and the branch's new Deps.Granting port lets it
  // through precisely because this caller holds role:manage (see TestTheJSONAskDoor… for the
  // refusal on the other side of that line).
  const other = `coadmin_${Date.now()}`;
  const created = await page.request.put(`/api/v1/auth/roles/${other}`, { data: { permissions: ['role:manage'] } });
  expect(created.status(), await created.text()).toBe(200);
  const held = await page.request.post(`/api/v1/user/users/${actor}/roles`, { data: { roles: ['admin', other] } });
  expect(held.status(), await held.text()).toBe(200);

  const emptying = (role: string) =>
    page.request.put(`/api/v1/auth/roles/${role}`, { data: { permissions: [] } });
  const [a, b] = await Promise.all([emptying('admin'), emptying(other)]);
  const accepted = [a, b].filter((r) => r.status() < 300);
  console.log(`  standing down from both administering roles at once = ` +
    `${JSON.stringify([a.status(), b.status()])} (${accepted.length} accepted)`);
  expect(accepted.length,
    `both writes were accepted (${JSON.stringify([a.status(), b.status()])}): the floor read ` +
    `"another administering role exists" twice about a fact the first write destroyed, and the ` +
    `tenant now has no role that grants role:manage — ` +
    `admin=${(await a.text()).slice(0, 160)} ${other}=${(await b.text()).slice(0, 160)}`)
    .toBeLessThanOrEqual(1);

  // Whoever is refused can still be granted something: the screen that hands out roles answers for
  // the caller who kept their grant.
  const screen = await page.request.get('/app/auth/roles');
  expect(screen.status(), `the roles screen answers ${screen.status()} after the pair of writes`).toBe(200);

  // Leave the tenant as it was found, in the order that keeps the door open: put the wildcard back
  // on `admin`, then remove the role this spec made. A failed restore prints rather than poisoning
  // the next spec quietly.
  const restored = await page.request.put('/api/v1/auth/roles/admin', { data: { permissions: ['*'] } });
  console.log(`  restoring admin = ${restored.status()} ${(await restored.text()).replace(/\s+/g, ' ').slice(0, 90)}`);
  expect(restored.status(), 'this spec cannot leave the tenant without an administering role').toBe(200);
  // The auth module offers no delete verb on a role (405), so the role this spec made stays — the
  // database is dropped at teardown — but it stops granting anything.
  const emptied = await page.request.put(`/api/v1/auth/roles/${other}`, { data: { permissions: [] } });
  console.log(`  emptying ${other} = ${emptied.status()} (delete is ${await (await page.request.delete(`/api/v1/auth/roles/${other}`)).status()})`);
  expect(emptied.status(), 'this spec cannot leave a second role granting role:manage behind').toBe(200);
});
