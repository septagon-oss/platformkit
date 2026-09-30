import { expect, test } from '@playwright/test';

// The cascade, as a browser reads the sheet the application actually serves.
//
// ui/ui_test.go and the review pins of rounds 12–15 read the composed bytes: they say what
// Compose emits and what refuseClientSheet refuses. What no Go test can say is what a browser
// *does* with those bytes, and the whole argument rests on one: the sheet's first line ranks
// the client layer last, which for normal declarations makes it the strongest of the four, so
// a consumer rule addressed at a kernel class would win the kernel's own element whatever its
// own selector says — and the refusal, not the ranking, is what protects the component. That
// claim was never measured in any revision of this repository: every case that exists reads
// the bytes, and one refusal message had it stated backwards before round 14 caught it.
//
// The public site is the page to read. modules/web is the only composition in this repository
// that passes ui.Extra.Sheets to ui.Compose, so its @layer client carries a consumer's rules
// (the prose sheet), and its sheet is same-origin, so document.styleSheets exposes cssRules.
// The admin shell's sheet is composed without one and would prove nothing about the ranking.
//
// e2e/site.spec.ts already proves the site renders; this spec reads its stylesheet.

const LAYERS = ['tokens', 'base', 'components', 'client'];

test('the site serves one stylesheet whose rules all sit inside the four layers it states', async ({ page }) => {
  await page.goto('/');
  const href = await page
    .locator('link[rel="stylesheet"][href*="/web/assets/app.css"]')
    .first()
    .getAttribute('href');
  expect(href, 'the site shell links the sheet its composition composed').toBeTruthy();

  // The bytes over HTTP, not the value ui.Compose returned: no other case in this
  // repository fetches app.css at all, and the fingerprint is the link's cache key.
  const served = await page.request.get(href!);
  expect(served.status(), await served.text()).toBe(200);
  expect(await served.text(), 'the sheet begins with its layer order').toMatch(
    /^@layer tokens, base, components, client;\n/,
  );

  // Chromium's own parse. A rule the emitter left unlayered would be here as a plain
  // CSSStyleRule, at the top level, and would outrank every layer the line above names —
  // which is the fact every structural pin in ui/ argues about and none of them measures.
  // page.evaluate serialises its function, so everything it reads is an argument.
  const read = await page.evaluate(({ needle, layers }: { needle: string; layers: string[] }) => {
    const sheet = Array.from(document.styleSheets).find(s => (s.href ?? '').includes(needle));
    if (!sheet) return { missing: 'the served sheet is not in document.styleSheets' };
    const block = (name: string) =>
      Array.from(sheet.cssRules).find(r => (r as CSSLayerBlockRule).name === name) as CSSLayerBlockRule | undefined;
    const selectorsOf = (name: string) =>
      Array.from(block(name)?.cssRules ?? []).map(r => (r as CSSStyleRule).selectorText ?? '');
    // Class names the browser resolved in each layer's selectors. A name is read up to any
    // escape, which only ever collects more on the kernel's side (`.bg-surface-overlay\/50`
    // reads as bg-surface-overlay), so the comparison below is stricter than a full selector
    // engine would be, never looser.
    const classes = (name: string) =>
      Array.from(new Set(selectorsOf(name).flatMap(s => Array.from(s.matchAll(/\.([a-zA-Z_][-\w]*)/g), m => m[1]))));
    return {
      // Each top-level rule, as the kind the browser read it and the layer it names.
      top: Array.from(sheet.cssRules).map(r =>
        r.constructor.name === 'CSSLayerStatementRule'
          ? ['order', ...(r as CSSLayerStatementRule).nameList]
          : [r.constructor.name, (r as CSSLayerBlockRule).name ?? ''],
      ),
      sizes: layers.map(name => [name, block(name)?.cssRules.length ?? -1] as [string, number]),
      clientClasses: classes('client'),
      componentClasses: classes('components'),
    };
  }, { needle: href!.split('?')[0], layers: LAYERS });

  expect(read, `the shell's own sheet: ${JSON.stringify(read)}`).not.toHaveProperty('missing');
  const shaped = read as { top: string[][]; sizes: [string, number][]; clientClasses: string[]; componentClasses: string[] };
  expect(shaped.top[0], 'the order statement is the sheet\'s first rule').toEqual(['order', ...LAYERS]);
  expect(shaped.top.slice(1)).toEqual(LAYERS.map(name => ['CSSLayerBlockRule', name]));
  // Every layer carries rules: tokens the palette, base the preflight, components the
  // resolved class lists, client modules/web's prose sheet. @layer client of the kernel's own
  // composition is empty, so a rule in it is what says this page was served by a module that
  // handed Compose a sheet — the composition this spec's second case then measures.
  for (const [name, count] of shaped.sizes) expect(count, `@layer ${name}`).toBeGreaterThan(0);
  // The invariant, read off the served sheet as the browser parsed it: no class name in the
  // layer a consumer writes is a class name the kernel declares and styles in its own layer.
  expect(shaped.clientClasses.filter(name => shaped.componentClasses.includes(name))).toEqual([]);
});

test('the client layer outranks the components layer on specificity alone, so the class gate is what protects a component', async ({ page }) => {
  await page.goto('/');
  // `.flex` is one of the kernel's own: one class selector, one declaration, compiled into
  // @layer components by the class lists ui/components renders the shell with.
  const measured = await page.evaluate(() => {
    const probe = document.createElement('div');
    probe.className = 'flex';
    document.body.appendChild(probe);
    const shown = () => getComputedStyle(probe).display;
    const kernel = shown();
    // A consumer rule with the specificity of a tag name — the weakest selector that can
    // still reach this element — stated in each layer in turn. Appending a <style> is what a
    // page does, and it lands after app.css in document order, so the second measurement is
    // the control: same layer, same later position, and only specificity decides between them.
    const state = (layer: string) => {
      const style = document.createElement('style');
      style.textContent = `@layer ${layer} { div { display: table-caption } }`;
      document.head.appendChild(style);
      const value = shown();
      style.remove();
      return value;
    };
    const inClient = state('client');
    const inComponents = state('components');
    probe.remove();
    return { kernel, inClient, inComponents };
  });
  // The layer, not the specificity, decides: a tag selector in the later layer wins.
  expect(measured.kernel, '@layer components styles .flex').toBe('flex');
  expect(measured.inClient, 'a tag selector in the client layer').toBe('table-caption');
  // …and the client rule did not win because it came later in the document: the same tag
  // selector stated in the kernel's own layer still loses to the kernel's class selector.
  expect(measured.inComponents, 'the same rule in @layer components').toBe('flex');
});
