import { expect, type Page } from '@playwright/test';

// The design floor, as one instrument the kernel can run against a page behind a cookie.
//
// The floor itself is `~/.local/share/pkit-999/gates/design_gate.py`, which no goal in this
// repository runs and whose probe takes no cookie — so it can only ever measure an anonymous
// page, never one inside the frame. The rules and formulas below are that gate's `rules()` and
// its probe, transcribed the way `e2e/review-r3-refusal-floor.spec.ts` transcribed them: at most
// two left edges, two body sizes, six font sizes, one primary CTA above the fold, four section
// gaps, a measure of at most 75 characters at widths of 1280 and up, no contrast under 4.5:1 for
// text below 24px, and no sideways scroll.
//
// Why a module rather than another copy: r3 and r4 each carry a private copy of this trio and the
// two copies are not identical (`diff` over their probe bodies says 39 lines), so neither can be
// imported unchanged and rewriting either would change what a reviewer's case measures. This file
// is the copy for specs written from now on, extended with the two measurements the frame needs
// and neither copy has: the `<p>` set the *chrome* draws — the header and footer paragraphs, which
// are siblings of `main` rather than descendants of it — and the brand link's own contrast, since
// a link is in the contrast set but in none of the size sets.
//
// Nothing here judges taste. Every number is the gate's, and a refusal is worded the way the gate
// words it, so a red line from this file reads like the report the clients get.

/** The floor's numbers, in one place, with the rule each one comes from. */
export const FLOOR = {
  maxLeftEdges: 2,
  maxBodySizes: 2,
  maxFontSizes: 6,
  maxPrimaryCtas: 1,
  minContrast: 4.5,
  contrastSizeCeiling: 24,
  maxSectionGaps: 4,
  maxMeasureCh: 75,
  measureWidthFloor: 1280,
};

export type Probe = {
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
  // What the frame draws outside <main>: the tenant name, the caller and the build stamp.
  chrome: { size: number; ch: number; text: string }[];
  // Diagnostics, kept so a red names the element rather than the rule: r4 carries the same idea
  // (`overflowing`) for exactly this reason. Nothing here is asserted on.
  worst_element: { tag: string; classes: string; colour: string; background: string } | null;
  overflowing: { tag: string; classes: string; text: string; left: number; right: number }[];
  // The brand link is tabbable, so the floor reads its colour; the span inside it is not.
  brand_link: {
    present: boolean;
    ratio: number;
    colour: string;
    background: string;
    size: number;
    text: string;
    href: string;
    classes: string;
  } | null;
};

/**
 * One pass over the live document. `width` is only carried into the result so a refusal can name
 * the viewport it belongs to; the page's own viewport is whatever the context was opened at.
 */
export async function probe(page: Page, width: number): Promise<Probe> {
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
    const described = (e: Element) => ({
      tag: e.tagName.toLowerCase(),
      classes: (e.getAttribute('class') || '').slice(0, 160),
      colour: getComputedStyle(e).color,
      background: getComputedStyle(e).backgroundColor,
    });
    let worst = { ratio: 99, text: '', size: 0, element: null as ReturnType<typeof described> | null };
    for (const e of text) {
      const fg = parse(getComputedStyle(e).color);
      if (!fg || fg.a === 0) continue;
      const r = contrast(fg, bgOf(e));
      if (r < worst.ratio) {
        worst = { ratio: Math.round(r * 100) / 100, text: (e.textContent || '').trim().slice(0, 40),
          size: px(getComputedStyle(e).fontSize), element: described(e) };
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
    const ch = (e: Element) =>
      Math.round(e.getBoundingClientRect().width / (px(getComputedStyle(e).fontSize) * 0.5));
    const paras = [...document.querySelectorAll('p')].filter(vis).map((e) => ({
      ch: ch(e),
      text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 50),
    })).sort((a, b) => b.ch - a.ch);
    // The chrome. `main` is the shell's skip target (#content) and the header and footer are its
    // siblings, so the frame's own paragraphs are found from that column rather than by any
    // data-attribute the frame would have to carry for no other reason.
    const chrome: { size: number; ch: number; text: string }[] = [];
    for (const e of [...document.querySelectorAll('main')].map((m) => m.parentElement || document.body)
      .flatMap((column) => [...column.querySelectorAll(':scope > header p, :scope > footer p')])) {
      if (!vis(e)) continue;
      chrome.push({
        size: px(getComputedStyle(e).fontSize),
        ch: ch(e),
        text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 40),
      });
    }
    // The brand link, measured as the element a person tabs to rather than the span inside it.
    const link = document.querySelector('[data-sidebar-brand] a') as HTMLAnchorElement | null;
    let brand: Probe['brand_link'] = null;
    if (link) {
      const fg = parse(getComputedStyle(link).color);
      // bgOf, never the link's own background: a transparent `rgba(0, 0, 0, 0)` parses to a
      // colour with alpha 0, and reading it as a background would score white-on-nothing as 21:1.
      const bg = bgOf(link);
      brand = {
        present: vis(link),
        ratio: fg ? Math.round(contrast(fg, bg) * 100) / 100 : 0,
        colour: getComputedStyle(link).color,
        background: `rgb(${Math.round(bg.r)}, ${Math.round(bg.g)}, ${Math.round(bg.b)})`,
        size: px(getComputedStyle(link).fontSize),
        text: (link.textContent || '').trim().slice(0, 40),
        href: link.getAttribute('href') || '',
        classes: link.getAttribute('class') || '',
      };
    }
    const overflowing = [...document.querySelectorAll('*')].filter((e) => {
      if (!vis(e)) return false;
      const b = e.getBoundingClientRect();
      return b.right > innerWidth + 1 || b.left < -1;
    }).map((e) => Object.assign(described(e), {
      text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 40),
      left: px(e.getBoundingClientRect().left), right: px(e.getBoundingClientRect().right),
    }));
    return {
      width: w,
      left_edges: clusters,
      body_font_sizes: bodySizes,
      all_font_sizes: allSizes,
      worst_contrast: { ratio: worst.ratio, text: worst.text, size: worst.size },
      worst_element: worst.element,
      overflowing,
      primary_ctas_above_fold: primary ? primary[1] : 0,
      section_gaps: gapSet,
      measures_ch: { min: paras.length ? paras[paras.length - 1].ch : 999, max: paras.length ? paras[0].ch : 0 },
      scroll_width_overflow: document.documentElement.scrollWidth > innerWidth,
      widest: paras.slice(0, 3),
      chrome,
      brand_link: brand,
    };
  }, width);
}

/** The floor, one line per rule, in the gate's own wording and its own numbers. */
export function refusals(m: Probe): string[] {
  const r: string[] = [];
  if (m.left_edges.length > FLOOR.maxLeftEdges) r.push(`${m.width}px: ${m.left_edges.length} left edges ${JSON.stringify(m.left_edges)} — one content column and at most one rail`);
  if (m.body_font_sizes.length > FLOOR.maxBodySizes) r.push(`${m.width}px: body text in ${m.body_font_sizes.length} sizes ${JSON.stringify(m.body_font_sizes)} — body size is constant down the page`);
  if (m.all_font_sizes.length > FLOOR.maxFontSizes) r.push(`${m.width}px: ${m.all_font_sizes.length} font sizes ${JSON.stringify(m.all_font_sizes)} — a type scale has at most 6 steps`);
  if (m.primary_ctas_above_fold > FLOOR.maxPrimaryCtas) r.push(`${m.width}px: ${m.primary_ctas_above_fold} primary buttons above the fold — one thing to do per view`);
  if (m.worst_contrast.ratio < FLOOR.minContrast && m.worst_contrast.size < FLOOR.contrastSizeCeiling) r.push(`${m.width}px: contrast ${m.worst_contrast.ratio} on "${m.worst_contrast.text}" — below 4.5:1`);
  if (m.section_gaps.length > FLOOR.maxSectionGaps) r.push(`${m.width}px: ${m.section_gaps.length} distinct section gaps ${JSON.stringify(m.section_gaps)} — spacing comes from a scale`);
  if (m.width >= FLOOR.measureWidthFloor && m.measures_ch.max > FLOOR.maxMeasureCh) r.push(`${m.width}px: body measure up to ${m.measures_ch.max}ch — keep lines under 75 characters`);
  if (m.scroll_width_overflow) r.push(`${m.width}px: the page scrolls sideways`);
  return r;
}

/** One line per measurement, so the run itself is the number a report quotes. */
export function report(label: string, m: Probe): string {
  return `  ${label} @${m.width}px edges=${JSON.stringify(m.left_edges)} body=${JSON.stringify(m.body_font_sizes)} ` +
    `sizes=${m.all_font_sizes.length} cta=${m.primary_ctas_above_fold} contrast=${m.worst_contrast.ratio}(${m.worst_contrast.size}px "${m.worst_contrast.text}") ` +
    `gaps=${JSON.stringify(m.section_gaps)} measure=${m.measures_ch.max} widest=${JSON.stringify(m.widest[0])} ` +
    `chrome=${JSON.stringify(m.chrome)} brand=${JSON.stringify(m.brand_link)}`;
}

/** A refusal, with the element that caused it: for the failure message, not for an assertion. */
export function diagnosed(m: Probe, why: string): string {
  return `${why}\n    worst element: ${JSON.stringify(m.worst_element)}\n    overflowing: ${JSON.stringify(m.overflowing.slice(0, 6))}`;
}

/** The floor as a failing assertion, with the measurements printed either way. */
export async function expectFloor(page: Page, label: string, width: number): Promise<Probe> {
  const m = await probe(page, width);
  console.log(report(label, m));
  expect(refusals(m), refusals(m).length ? diagnosed(m, `${label} at ${m.width}px fails the design floor`) : 'the floor holds').toEqual([]);
  return m;
}

// The three properties of the *frame* the floor cannot see on its own, each with its own
// assertion so a red names the thing that broke rather than "something on the page was worst".

/** The chrome is one body step: the tenant, the caller and the stamp are chrome, not the page. */
export function expectChromeOneStep(m: Probe, label: string): void {
  const steps = [...new Set(m.chrome.map((c) => c.size))].sort((a, b) => a - b);
  expect(m.chrome.length, `${label}: the frame drew no chrome paragraph at all, so this measures nothing`).toBeGreaterThan(0);
  expect(steps, `${label}: the frame's chrome takes ${steps.length} body steps ${JSON.stringify(m.chrome)}`).toEqual([steps[0]]);
}

/** The footer sentence is bounded, whatever the column it sits in is. */
export function expectFooterBounded(m: Probe, label: string): void {
  const stamp = m.chrome.filter((c) => /^PlatformKit/.test(c.text));
  expect(stamp.length, `${label}: no build-stamp sentence in the footer to measure`).toBe(1);
  expect(stamp[0].ch, `${label}: the footer sentence measures ${stamp[0].ch}ch at ${m.width}px — over the floor's ${FLOOR.maxMeasureCh}`).toBeLessThanOrEqual(FLOOR.maxMeasureCh);
}

/** The brand link is legible as the link, not as the span beside it. */
export function expectBrandLinkLegible(m: Probe, label: string): void {
  const link = m.brand_link;
  expect(link, `${label}: the sidebar draws no brand link at ${m.width}px`).not.toBeNull();
  expect(link!.present, `${label}: the brand link is not painted at ${m.width}px — nothing to read`).toBe(true);
  expect(link!.ratio, `${label}: the brand link is ${link!.ratio}:1 (${link!.colour} on ${link!.background}) — below ${FLOOR.minContrast}:1`).toBeGreaterThanOrEqual(FLOOR.minContrast);
}
