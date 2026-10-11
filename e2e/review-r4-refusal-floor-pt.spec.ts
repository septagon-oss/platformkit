import { expect, test } from '@playwright/test';

// The fourth review's pin over the same two pages this delivery owns, in the
// language the tenant is actually served in.
//
// The brief holds the design floor HIGH for a page a delivery creates or changes, and the tree
// already measures both of these pages — in English. `e2e/review-r3-refusal-floor.spec.ts` walks
// every rule of gates/design_gate.py over the refusal and the ask confirmation; the delivery's own
// `e2e/review-r2-refusal-measure.spec.ts` measures the confirmation's line length. What nobody ran,
// in any round, is the same floor over the *Portuguese* render, which review 3 lists under
// "Unverified" in its own words: "the pt-PT render of both fault pages under the floor: I measured
// the English render at both widths". The tenant this fixture boots is served in pt-PT
// (scripts/e2e.sh bootstraps `--language pt-PT`), `e2e/localization.spec.ts` proves that
// declaration is live, and every sentence this branch added to the refusal page is longer in
// Portuguese than in English — "Ask your administrator — anyone whose role grants them manage
// roles." against "Peça ao seu administrador — qualquer pessoa cuja função lhe dê gerir funções."
// A page whose box is set by the container does not care, and that is exactly the claim this file
// tests rather than assumes: the container, at the narrowest width the brief names, with the
// longest thing the page can say.
//
// Nothing here waits on a defect. The way in is the verdict the correct behaviour answers with —
// 403 for a member with no grant, 303 to the confirmation behind the ask — and the language is
// read from what the response says about itself (`Content-Language`, the document's `lang`), not
// from an English string. The two Go cases that already read the Portuguese refusal
// (`TestARefusalNamesTheGrantInTheLanguageTheRequestAskedFor`) check copy; this one checks
// geometry, which no Go case can.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};
const person = {
  email: `r4-floor-pt-${Date.now()}@e2e.test`,
  password: 'a-refused-person-chooses-this-1',
};
// A role of this spec's own, holding a grant nothing else ticks or unticks: the journey must
// start with a refusal whatever another spec did to a shared role.
const role = `r4_nothing_${Date.now()}`;
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
  overflowing: { text: string; width: number }[];
  widest: { ch: number; text: string }[];
};

// The gate's own probe (gates/design_probe.mjs), same selectors and formulas as
// e2e/review-r3-refusal-floor.spec.ts, plus one thing that file had no reason to
// measure: an element whose own box is wider than the viewport, which is how a
// long unbreakable Portuguese sentence fails a narrow screen while every count
// above stays inside the floor.
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
      text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 60),
    })).sort((a, b) => b.ch - a.ch);
    const over = [...document.querySelectorAll('body *')]
      .filter((e) => vis(e) || (e.textContent || '').trim().length > 1)
      .filter((e) => e.getBoundingClientRect().right > innerWidth + 1)
      .map((e) => ({ text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 50),
        width: Math.round(e.getBoundingClientRect().width) }))
      .slice(0, 4);
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
      overflowing: over,
      widest: paras.slice(0, 3),
    };
  }, width);
}

// The floor, in the gate's own wording and numbers, plus the overflow the pt copy is
// the reason to look for.
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
  if (m.overflowing.length) r.push(`${m.width}px: ${JSON.stringify(m.overflowing)} run past the right edge`);
  return r;
}

async function expectFloor(page: import('@playwright/test').Page, label: string, width: number) {
  const m = await probe(page, width);
  console.log(`  ${label} @${m.width}px edges=${JSON.stringify(m.left_edges)} body=${JSON.stringify(m.body_font_sizes)} ` +
    `sizes=${m.all_font_sizes.length} cta=${m.primary_ctas_above_fold} contrast=${m.worst_contrast.ratio} ` +
    `gaps=${JSON.stringify(m.section_gaps)} measure=${m.measures_ch.max} widest=${JSON.stringify(m.widest[0])} ` +
    `over=${JSON.stringify(m.overflowing)}`);
  expect(refusals(m), `${label} at ${m.width}px, in Portuguese, fails the design floor`).toEqual([]);
}

test('the refusal page speaks Portuguese without leaving the design floor at 390 and at 1440', async ({ page, browser }) => {
  test.setTimeout(120_000);

  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  const created = await page.request.put(`/api/v1/auth/roles/${role}`, { data: { permissions: ['audit:read'] } });
  expect(created.status(), await created.text()).toBe(200);
  const invited = await page.request.post('/api/v1/user/invitations', {
    data: { email: person.email, displayName: 'Piso Português', roles: [role] },
  });
  expect(invited.status(), await invited.text()).toBe(201);
  const id = (await invited.json()).id as string;
  const activated = await page.request.post(`/api/v1/user/users/${id}/set-password`, {
    data: { password: person.password },
  });
  expect(activated.status(), await activated.text()).toBe(200);

  for (const width of [390, 1440]) {
    // The language travels the way a person's browser sends it — the context's
    // locale, which is what puts `Accept-Language: pt-PT` on a navigation. A
    // measured note from writing this file: `setExtraHTTPHeaders` with that
    // header does *not* reach a navigation (Chromium keeps its own `en-US`, and
    // the refusal is answered in English honestly declaring "en"), so a case that
    // set the header would be measuring English and calling it Portuguese.
    const context = await browser.newContext({ viewport: { width, height: 900 }, locale: 'pt-PT' });
    const p = await context.newPage();
    await p.goto('/app/admin/login');
    await p.getByLabel('Email').fill(person.email);
    await p.getByLabel('Palavra-passe').fill(person.password);
    await p.getByRole('button', { name: 'Iniciar sessão', exact: true }).click();
    await expect(p).toHaveURL(/\/app$/);

    const refusal = await p.goto(refusedScreen);
    expect(refusal?.status(), 'the member was not refused, so this page has no start').toBe(403);

    // The page says which language it is, and the assertion reads that off the
    // response rather than off a sentence: a refusal answered in English declares
    // "en" honestly, and it is the *declaration* plus the geometry this file is
    // about. (The copy itself is pinned by
    // apps/platformkit/review_r1_way_on_and_notified_test.go.)
    expect(refusal?.headers()['content-language'], 'the refusal declared no language this person asked for')
      .toBe('pt-PT');
    await expect(p.locator('html')).toHaveAttribute('lang', 'pt-PT');

    // The way on is still there and still answers, in this language too.
    const back = p.locator('a[href="/app"]').first();
    await expect(back).toHaveCount(1);
    const way = (await back.getAttribute('href')) ?? '';
    expect(way, 'the refusal offers no way on').not.toBe('');

    await expectFloor(p, 'the refusal page (pt-PT)', width);

    // The confirmation the ask lands on, in the same language, at the same width.
    await p.locator('form[action="/app/access-request"] button[type="submit"]').click();
    await expect(p).toHaveURL(/\/app\/access-request\/sent$/);
    await expect(p.locator('html')).toHaveAttribute('lang', 'pt-PT');
    await expectFloor(p, 'the ask confirmation (pt-PT)', width);

    await context.close();
  }
});
