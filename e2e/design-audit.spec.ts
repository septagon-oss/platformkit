import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// The eye: what the other specs cannot see.
//
// e2e/ui-components.spec.ts already runs axe against every gallery specimen in both
// themes, and e2e/session-recovery.spec.ts already proves a session survives at two
// widths. This spec covers the measurements neither makes, because they are the ones a
// generated page can get wrong without any single component, rule or script being broken:
//
//   - whether the page fits the viewport at all, at four widths;
//   - whether a box that scrolls can be scrolled without a mouse;
//   - whether focus is actually painted, rather than merely declared in a class name;
//   - whether a picture arrived, and whether it says what it is;
//   - whether the smallest thing a person must hit is big enough to hit.
//
// Calibration matters more than the findings here. A first draft of this instrument
// reported 2,159 problems on one page: it compared text against `background-color:
// rgba(0,0,0,0)`, which is not a colour; parsed `color(srgb .28 .32 .31)` as 0-255
// channels; applied the 44px platform *guideline* as though it were the WCAG floor; read a
// class name containing `focus:` as proof that a ring paints; and counted a
// visually-hidden skip link as a 1×1 target. Every one of those was a finding about the
// instrument. Colour is therefore left to axe, which resolves painted backgrounds
// properly; what is kept here measures geometry and real keyboard focus, which axe does not.
//
// Two things are deliberately absent: any judgement, and any claim about taste. Whether a
// page reads as a form when it should read as a map is not measurable here, and a person
// still decides it.

const email = process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test';
const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
const tasks = '/admin/task/tasks';

/** 320 is the WCAG reflow floor, not a device. The others are the shell's own breakpoints. */
const widths = [320, 390, 768, 1280];

/** What a person meets without writing code: the shell, one generated list, the gallery. */
const surfaces: [string, string][] = [
  ['dashboard', '/admin'],
  ['generated list', tasks],
  ['component gallery', '/admin/_gallery'],
];

/**
 * Seed through the generated form rather than the JSON API: this spec has no business
 * discovering the shape of a create request, and admin-tasks.spec.ts already proves that
 * form works. Exactly one specimen row is enough — the geometry of a row is not a function
 * of how many there are, and an empty table measures nothing about a row's hit area. One
 * row also keeps this file honest about the shared fixture: every other spec's assertions
 * are scoped to a title of its own, and session-recovery.spec.ts compares a page's items to
 * its total, which holds while a page holds everything. Two or three hundred rows would
 * make this file responsible for another one's failure.
 */
test.beforeAll(async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto('/admin/login');
  await signIn(page);
  await page.goto('/admin/task/tasks/new');
  await page.getByLabel('Title').fill('Design audit specimen');
  await page.getByLabel('Priority').selectOption('high');
  await page.getByLabel('Description').fill('Long enough that the description column has a real width to report.');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(new RegExp(`${tasks}/[0-9a-f-]{36}$`));
  await context.close();
});

test.beforeEach(async ({ page }) => {
  await page.goto('/admin/login');
  await signIn(page);
});

async function signIn(page: Page) {
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/admin$/);
}

/**
 * One pass over the live document, returning findings as sentences: a failing assertion
 * here should read like a defect report, not like a diff of two opaque arrays.
 *
 * `targets` is opt-in per surface on purpose. The gallery is a wall of isolated specimens,
 * and several of them are deliberately small to show a variant off; the claim this file
 * defends is that a *generated page* is usable, which is what the shell and the generated
 * list are. Specimen scale belongs to ui-components.spec.ts, which sees every one of them.
 */
async function inspect(page: Page, targets: boolean) {
  return page.evaluate((checkTargets: boolean) => {
    const shown = (e: Element) => {
      const el = e as HTMLElement;
      return el.offsetWidth > 0 || el.offsetHeight > 0 || el.getClientRects().length > 0;
    };
    const label = (e: Element) =>
      (e.getAttribute('aria-label') || e.textContent || e.tagName).trim().replace(/\s+/g, ' ').slice(0, 40);
    // The visually-hidden pattern, recognised by what it paints rather than by the class
    // that carries it: a 1px box, taken out of flow, clipped to nothing, never wrapping.
    // Such a target is not too small to hit — it is not a pointer target at all, and the
    // claim it makes is that it paints when the keyboard reaches it. That claim has its own
    // test below; treating the pattern as a 1x1 defect is how an instrument cries wolf.
    const hiddenTillFocused = (el: Element) => {
      const s = getComputedStyle(el);
      const box = el.getBoundingClientRect();
      return (
        box.width <= 1 && box.height <= 1 && s.position === 'absolute' && s.overflow === 'hidden' && s.whiteSpace === 'nowrap'
      );
    };
    const out: string[] = [];

    // A box that scrolls horizontally and cannot take focus is unreachable in the
    // strongest sense a page can guarantee from markup alone: no keyboard user can
    // even arrive at it, and a screen reader never enters it. (aria-hidden boxes are
    // exempt: a skeleton is a placeholder, and announcing it is its own defect.)
    //
    // This is a necessary condition, not a sufficient one, and the difference is not
    // academic. Verified on this box: a bare `tabindex=0` `overflow:auto` div in an
    // otherwise empty document does not move its scrollLeft on ArrowRight, End or
    // Space under Chromium. Focusability is what the markup can promise; scrolling a
    // focused region with the keyboard is behaviour, and behaviour is asserted where
    // behaviour is asserted - e2e/known-defects.spec.ts, as an expected failure, so
    // this rule can never be mistaken for the whole requirement.
    for (const el of Array.from(document.querySelectorAll('*'))) {
      if (!/auto|scroll/.test(getComputedStyle(el).overflowX) || !shown(el)) continue;
      if ((el as HTMLElement).scrollWidth <= el.clientWidth + 1) continue;
      if (el.closest('[aria-hidden="true"]') || (el as HTMLElement).tabIndex >= 0) continue;
      out.push(`"${label(el)}" scrolls horizontally (${(el as HTMLElement).scrollWidth}px of ${el.clientWidth}px visible) and is not in the tab order at all`);
    }

    if (checkTargets) {
      // WCAG 2.5.8 sets 24x24 as the minimum. 44 is a platform guideline and is not
      // asserted as a defect anywhere in this file. The exceptions are the guideline's own:
      // text that is inline in a sentence, and a control whose reachable hit area is its
      // wrapping label rather than the 16px box inside it — which is how this family's own
      // checkbox is built, so measuring the input alone would report a defect that a
      // pointer and a finger do not have.
      for (const el of Array.from(document.querySelectorAll('a,button,input,select,summary,[role=button],[role=checkbox]'))) {
        if (!shown(el) || el.closest('[aria-hidden="true"]') || hiddenTillFocused(el)) continue;
        if (getComputedStyle(el).display === 'inline' && el.closest('p,li,dd')) continue;
        const hit = (el instanceof HTMLElement && el.closest('label')) ?? el;
        const box = hit.getBoundingClientRect();
        if (box.width >= 24 && box.height >= 24) continue;
        out.push(`"${label(el)}" is hit through a ${Math.round(box.width)}x${Math.round(box.height)} target, under the 24x24 minimum`);
      }
    }

    // A picture that did not arrive is not a smaller picture. A missing alt attribute is a
    // bug; alt="" is a decision, and the difference between the two is the whole question.
    for (const img of Array.from(document.images)) {
      if (!shown(img)) continue;
      if (!img.complete || img.naturalWidth === 0) out.push(`image did not decode: ${img.currentSrc || img.src}`);
      else if (img.getAttribute('alt') === null) out.push(`image carries no alt attribute at all: ${img.currentSrc || img.src}`);
    }

    // A viewBox is what makes an overlay's coordinates mean the same thing at every
    // rendered size. Decorative icons are exempt as long as they are hidden: an icon with
    // no viewBox is a fixed picture, which is what an icon is.
    for (const svg of Array.from(document.querySelectorAll('svg'))) {
      if (!shown(svg) || svg.closest('[aria-hidden="true"]')) continue;
      if (!svg.getAttribute('viewBox')) out.push(`a visible <svg> has no viewBox, so its geometry cannot scale with anything`);
    }

    return out;
  }, targets);
}

/**
 * Focus that is painted, measured by moving focus with the keyboard and comparing what the
 * browser computes before and after. Reading the element's own class list for the word
 * "focus" — which a previous version of this check did — proves only that the author
 * intended a ring, which is exactly the thing in question.
 */
async function focusWithoutARing(page: Page) {
  await page.evaluate(() => {
    const nodes = Array.from(
      document.querySelectorAll('a[href],button,input,select,textarea,summary,[tabindex]:not([tabindex="-1"])'),
    ).filter((el) => !el.hasAttribute('disabled') && !el.closest('[inert]')) as HTMLElement[];
    (window as any).__pkFocusBaseline = nodes.map((el, i) => {
      el.setAttribute('data-pk-probe', String(i));
      const s = getComputedStyle(el);
      const box = el.getBoundingClientRect();
      return { shadow: s.boxShadow, border: s.borderColor, width: box.width, height: box.height };
    });
  });

  const bare: string[] = [];
  const visited = new Set<string>();
  // Bounded on purpose: the gallery holds hundreds of specimens, and two hundred stops is
  // enough to catch a component whose ring never paints without walking the whole page.
  for (let step = 0; step < 200; step++) {
    await page.keyboard.press('Tab');
    const now = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null;
      if (!el || el === document.body) return null;
      const probe = el.getAttribute('data-pk-probe');
      const base = probe === null ? undefined : (window as any).__pkFocusBaseline?.[Number(probe)];
      const s = getComputedStyle(el);
      const ring =
        (s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0) ||
        (base ? s.boxShadow !== base.shadow || s.borderColor !== base.border : s.boxShadow !== 'none');
      return {
        key: probe ?? `${el.tagName} ${String(el.className).slice(0, 30)}`,
        label: (el.getAttribute('aria-label') || el.textContent || el.tagName).trim().replace(/\s+/g, ' ').slice(0, 40),
        // A target hidden until it receives focus is the visually-hidden pattern working.
        // The skip link has its own test below, which checks what the pattern promises.
        hiddenUntilFocused: !!base && base.width === 0 && base.height === 0,
        ring,
      };
    });
    if (!now) break;
    if (visited.has(now.key)) break; // focus has come round
    visited.add(now.key);
    if (!now.ring && !now.hiddenUntilFocused) bare.push(now.label);
  }
  return bare;
}

for (const width of widths) {
  for (const [name, path] of surfaces) {
    test(`the ${name} keeps its pictures, its scroll regions and its width at ${width}px`, async ({ page }) => {
      test.setTimeout(60_000);
      await page.setViewportSize({ width, height: 900 });
      await page.goto(path);
      await expect(page.locator('main').first()).toBeVisible();

      const findings = await inspect(page, name !== 'component gallery');
      expect(findings, `findings on the ${name} at ${width}px`).toEqual([]);

      // Every width, including the widest: a table whose columns do not fit is exactly as
      // wide at 1280 as at 320, and a page that overflows horizontally is one a person on a
      // 320px screen cannot reach the end of.
      const box = await page.evaluate(() => ({
        scroll: document.documentElement.scrollWidth,
        viewport: window.innerWidth,
      }));
      expect(box.scroll, `the ${name} is ${box.scroll}px wide in a ${box.viewport}px viewport at ${width}px`).toBeLessThanOrEqual(
        box.viewport + 1,
      );
    });
  }
}

test('a generated list names its scroll region, so a keyboard can reach the columns past the fold', async ({ page }) => {
  await page.goto(tasks);
  const region = page.locator('[data-component="table"]');
  await expect(region).toHaveAttribute('role', 'region');
  await expect(region).toHaveAttribute('tabindex', '0');
  await expect(region).toHaveAttribute('aria-label', /Design audit specimens|Tasks/);
  // Named and reachable, in that order. Focus without a name announces a landmark nothing
  // identifies, which is the same defect wearing a different hat.
  await region.focus();
  await expect(region).toBeFocused();
});

test('focus paints on the shell and on a generated screen, rather than merely being declared', async ({ page }) => {
  for (const [name, path] of surfaces.slice(0, 2)) {
    await page.goto(path);
    const bare = await focusWithoutARing(page);
    expect(bare.map((label) => `nothing is painted on the focused "${label}" on the ${name}`), `focus on the ${name}`).toEqual([]);
  }
});

test('the skip link is hidden ink until the keyboard reaches it, then paints', async ({ page }) => {
  await page.goto(tasks);
  const skip = page.getByRole('link', { name: /skip/i }).first();
  // toBeVisible() cannot answer this: a 1px box has a bounding box, so Playwright calls the
  // visually-hidden pattern visible. What is actually claimed is about ink — nothing is
  // painted before focus, and a hit-area-sized, backed rectangle is painted after it.
  const before = await skip.evaluate((el) => {
    const box = el.getBoundingClientRect();
    const s = getComputedStyle(el);
    return { width: box.width, height: box.height, clip: s.clip, position: s.position };
  });
  expect(before.width, `the skip link is ${before.width}px wide before focus; it should be clipped out`).toBeLessThanOrEqual(1);
  expect(before.height, `the skip link is ${before.height}px tall before focus`).toBeLessThanOrEqual(1);
  expect(before.clip).not.toBe('auto');

  await page.keyboard.press('Tab');
  await expect(skip).toBeFocused();
  const after = await skip.evaluate((el) => {
    const box = el.getBoundingClientRect();
    const s = getComputedStyle(el);
    return {
      width: box.width, height: box.height, top: box.top, left: box.left,
      clip: s.clip, background: s.backgroundColor, boxShadow: s.boxShadow,
    };
  });
  expect(after.clip, 'the focused skip link is still clipped to nothing').toBe('auto');
  expect(after.width, `the focused skip link is ${Math.round(after.width)}px wide`).toBeGreaterThanOrEqual(24);
  expect(after.height, `the focused skip link is ${Math.round(after.height)}px tall`).toBeGreaterThanOrEqual(24);
  expect(after.background, 'the focused skip link paints no background to read against').not.toBe('rgba(0, 0, 0, 0)');
  expect(after.top, 'the focused skip link is not where the eye goes first').toBeLessThan(160);
});

// axe is the expensive half of this file, and 320 and 1280 are the two widths where a
// layout has most often given up. The gallery gets axe from ui-components.spec.ts at
// component scale, in both themes, which is where a specimen's own contrast belongs.
for (const width of [320, 1280]) {
  for (const [name, path] of surfaces.slice(0, 2)) {
    test(`axe finds nothing on the ${name} at ${width}px`, async ({ page }) => {
      test.setTimeout(60_000);
      await page.setViewportSize({ width, height: 900 });
      await page.goto(path);
      const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
      expect(results.violations, `axe on the ${name} at ${width}px`).toEqual([]);
    });
  }
}
