import { expect, test } from '@playwright/test';

// What the reduced-motion floor actually reaches, measured in a browser.
//
// ARCHITECTURE.md says the `@layer base` `prefers-reduced-motion` fallback "outranks a consumer's
// `!important` animation in `client` and unlayered alike — an accessibility floor a page cannot
// shout over", and calls it "the one important declaration the kernel authors". The first half is a
// browser fact this repository had never measured; the second is not what the sheet holds — the
// `@layer components` block carries four `display: none !important` rules of its own (the
// `[data-component][hidden]` rule and the checkbox's parts), and a `style` attribute declared
// `!important` still outranks the whole sheet. What the floor silences is the *author* half of the
// cascade, in whichever layer or in none. These two cases pin the boundary that sentence needs.
//
// The public site is the page to read, as in cascade-layers.spec.ts: modules/web is the composition
// that passes ui.Extra.Sheets, so its @layer client carries a consumer's rules, and the sheet is
// same-origin, so it can be fetched and re-parsed. The consumer rules are appended to the served
// bytes rather than written into a second sheet: a client's sheet reaches the file inside the one
// document that has the kernel's layers, and an adopted sheet beside the linked one ranks those
// layers differently — measured, and the reason the first draft of this case read a clamp that the
// cut-out column then refused to reproduce.

test.use({ reducedMotion: 'reduce' });

const spin = 'r18-spin 1s linear infinite !important';
const consumerRules = `@keyframes r18-spin { to { transform: rotate(360deg) } }
  #r18-unlayered { animation: ${spin}; }
  @layer client { #r18-inclient { animation: ${spin}; } }
  @layer r18late { #r18-late { animation: ${spin}; } }`;

type Read = { missing?: string; byLayer?: [string, string][]; fallbackInBase?: boolean };

const readSheet = () => {
  const sheet = Array.from(document.styleSheets).find(s => (s.href ?? '').includes('/web/assets/app.css'));
  if (!sheet) return { missing: 'the served sheet is not in document.styleSheets' };
  const important = (rule: CSSStyleRule) =>
    Array.from(rule.style).filter(name => rule.style.getPropertyPriority(name) === 'important');
  const walk = (rules: CSSRuleList, layer: string | null, seen: [string, string][]): [string, string][] => {
    for (const rule of Array.from(rules)) {
      const own = rule.constructor.name === 'CSSLayerBlockRule' ? (rule as CSSLayerBlockRule).name : layer;
      if (rule.constructor.name === 'CSSStyleRule') {
        for (const name of important(rule as CSSStyleRule)) seen.push([String(own), name]);
      } else if ('cssRules' in rule) {
        walk((rule as CSSGroupingRule).cssRules, own, seen);
      }
    }
    return seen;
  };
  const base = Array.from(sheet.cssRules).find(r => r.constructor.name === 'CSSLayerBlockRule' && (r as CSSLayerBlockRule).name === 'base');
  const reduce = Array.from((base as CSSLayerBlockRule | undefined)?.cssRules ?? [])
    .find(r => r.constructor.name === 'CSSMediaRule' && (r as CSSMediaRule).conditionText.includes('prefers-reduced-motion'));
  return {
    byLayer: walk(sheet.cssRules, null, [] as [string, string][]),
    // The fallback the sentence is about: authored inside @layer base, carrying the important
    // priority the reversal is said to work on.
    fallbackInBase: Array.from((reduce as CSSMediaRule | undefined)?.cssRules ?? [])
      .some(r => important(r as CSSStyleRule).includes('animation-iteration-count')),
  };
};

test('the sheet authors its reduced-motion floor inside @layer base and leaves no !important unlayered', async ({ page }) => {
  await page.goto('/');
  const read: Read = await page.evaluate(readSheet);
  expect(read.missing ?? '', await page.content()).toBe('');
  expect(read.fallbackInBase, 'the fallback sits in the base layer with its !important animation properties').toBe(true);
  expect(read.byLayer!.filter(([layer]) => layer === 'null'), 'no !important declaration escapes the layers the sheet states').toEqual([]);
  // More than "the one important declaration" the sentence counts: the components layer authors its
  // own, and they are what keeps a [hidden] component hidden.
  expect(read.byLayer!.filter(([layer]) => layer === 'components').length, 'the components layer authors !important rules of its own').toBeGreaterThan(0);
  const properties = ['animation-duration', 'animation-iteration-count', 'transition-property', 'transition-duration', 'scroll-behavior', 'display'];
  expect(read.byLayer!.every(([, property]) => properties.includes(property)), JSON.stringify(read.byLayer)).toBe(true);
});

test('the floor silences an author !important in client, in a later layer and unlayered, and stops at the style attribute', async ({ page }) => {
  await page.goto('/');
  const href = await page.locator('link[rel="stylesheet"][href*="/web/assets/app.css"]').first().getAttribute('href');
  const css = await (await page.request.get(href!)).text();
  // The same four declarations measured against the served sheet and against the same sheet with its
  // reduced-motion block cut out. Without the second column the first proves nothing: a rule that
  // never applied computes to the same `1 0.00001` as a rule the floor silenced, and a case that is
  // green only because the probe never reached the cascade is a mistake this programme has made.
  const computed = await page.evaluate(({ sheet, sheetWithoutFallback }) => {
    for (const link of Array.from(document.querySelectorAll('link[rel="stylesheet"]'))) link.remove();
    const host = document.createElement('div');
    host.innerHTML = `<span id="r18-unlayered">a</span><span id="r18-inclient">b</span>` +
      `<span id="r18-late">c</span><span id="r18-inline" style="animation: r18-spin 1s linear infinite !important">d</span>`;
    document.body.append(host);
    const measure = (text: string) => {
      const adopted = new CSSStyleSheet();
      adopted.replaceSync(text);
      document.adoptedStyleSheets = [adopted];
      const read = (id: string) => {
        const s = getComputedStyle(document.getElementById(id)!);
        return `${s.animationIterationCount} ${Number.parseFloat(s.animationDuration)}`;
      };
      return ['r18-unlayered', 'r18-inclient', 'r18-late', 'r18-inline'].map(read);
    };
    return { served: measure(sheet), cut: measure(sheetWithoutFallback) };
  }, { sheet: css + consumerRules, sheetWithoutFallback: cutReduceBlock(css) + consumerRules });
  const clamped = '1 0.00001', animating = 'infinite 1';
  expect(computed.cut, 'with the fallback cut out, all four animate — the probe reaches the cascade').toEqual([animating, animating, animating, animating]);
  expect(computed.served[0], 'an unlayered !important animation is the weakest author rule of all').toBe(clamped);
  expect(computed.served[1], 'a !important rule in the client layer is outranked by the base layer').toBe(clamped);
  expect(computed.served[2], 'so is one in a layer declared after the sheet\'s four').toBe(clamped);
  expect(computed.served[3], 'a style attribute is no author rule: the floor does not reach it').toBe(animating);
});

// The @media (prefers-reduced-motion: reduce) block, brace-matched out of the sheet text. The cut is
// of that block only: an assertion satisfied by "no base layer at all" is not testing the floor.
function cutReduceBlock(css: string): string {
  const at = css.indexOf('@media (prefers-reduced-motion: reduce)');
  if (at < 0) throw new Error('the served sheet carries no reduced-motion block to cut');
  let depth = 0, end = at;
  for (let i = css.indexOf('{', at); i < css.length; i++) {
    if (css[i] === '{') depth++;
    if (css[i] === '}' && --depth === 0) { end = i + 1; break; }
  }
  return css.slice(0, at) + css.slice(end);
}
