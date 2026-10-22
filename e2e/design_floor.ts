import { expect, type Page } from '@playwright/test';
import { FLOOR, probe as measure, refusals as applyRules, report as describe, diagnosed as diagnosis } from '../ui/testdata/design_floor.mjs';
export { FLOOR };

export function refusals(m: Probe): string[] { return applyRules(m); }
export function report(label: string, m: Probe): string { return describe(label, m); }
export function diagnosed(m: Probe, why: string): string { return diagnosis(m, why); }

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
  chrome: { region: string; size: number; ch: number; text: string }[];
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

export type CompleteProbe = Probe & {
  stylesheet_hex: {href: string; hex?: number; defines?: number; bytes?: number; error?: string}[];
};

/** Measure the served page with the canonical UI instrument. */
export async function probe(page: Page, width: number): Promise<CompleteProbe> {
  return measure(page, width);
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
  // A tenant may itself be named PlatformKit; only the footer holds the build stamp.
  const stamp = m.chrome.filter((c) => c.region === 'footer' && /^PlatformKit/.test(c.text));
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
