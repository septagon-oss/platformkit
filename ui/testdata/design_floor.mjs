// Shared rendered design-floor measurements and rules. No application runtime imports.
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

export async function probe(page, width) {
  const measurement = await page.evaluate((w) => {
    const px = (v) => Math.round(parseFloat(v) || 0);
    const vis = (e) => {
      const r = e.getBoundingClientRect();
      const s = getComputedStyle(e);
      return r.width > 40 && r.height > 6 && s.visibility !== 'hidden' && s.display !== 'none';
    };
    const parse = (c) => {
      const m = c && c.match(/rgba?\(([^)]+)\)/);
      if (!m) return null;
      const [r, g, b, a = 1] = m[1].split(',').map(Number);
      return { r, g, b, a };
    };
    const lum = (c) => {
      const f = (v) => (v /= 255) <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
      return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
    };
    const contrast = (a, b) => {
      const [x, y] = [lum(a), lum(b)];
      return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
    };
    const bgOf = (e) => {
      for (let n = e; n; n = n.parentElement) {
        const c = parse(getComputedStyle(n).backgroundColor);
        if (c && c.a > 0) return c;
      }
      return { r: 255, g: 255, b: 255, a: 1 };
    };
    const computedColour = c => {
      const rgb = parse(c);
      if (rgb) return rgb;
      const match = c.match(/^color\(srgb\s+(.+)\)$/);
      if (!match) throw new Error(`Unsupported computed colour: ${c}`);
      const [channels, alpha = '1'] = match[1].split('/');
      const [r,g,b] = channels.trim().split(/\s+/).map(v => Number(v) * 255);
      return {r,g,b,a: Number(alpha)};
    };
    const sidebarBackground = e => {
      const chain = [];
      for (let n = e; n; n = n.parentElement) chain.unshift(computedColour(getComputedStyle(n).backgroundColor));
      return chain.reduce((bg, c) => ({r:c.r*c.a+bg.r*(1-c.a),g:c.g*c.a+bg.g*(1-c.a),b:c.b*c.a+bg.b*(1-c.a)}), {r:255,g:255,b:255});
    };
    const text = [...document.querySelectorAll('h1,h2,h3,h4,p,li,a,button,label,span,td,th')]
      .filter(vis).filter((e) => (e.textContent || '').trim().length > 1);
    const blocks = [...document.querySelectorAll('h1,h2,h3,p,section,article,main > *,header,footer')].filter(vis);
    const lefts = [...new Set(blocks.filter((e) => /^(H[1-3]|P)$/.test(e.tagName))
      .map((e) => px(e.getBoundingClientRect().left)))].sort((a, b) => a - b);
    const clusters = [];
    for (const l of lefts) if (!clusters.length || l - clusters[clusters.length - 1] > 3) clusters.push(l);
    const bodySizes = [...new Set([...document.querySelectorAll('p')].filter(vis)
      .map((e) => px(getComputedStyle(e).fontSize)))].sort((a, b) => a - b);
    const allSizes = [...new Set(text.map((e) => px(getComputedStyle(e).fontSize)))].sort((a, b) => a - b);
    const described = (e) => ({
      tag: e.tagName.toLowerCase(),
      classes: (e.getAttribute('class') || '').slice(0, 160),
      colour: getComputedStyle(e).color,
      background: getComputedStyle(e).backgroundColor,
    });
    let worst = { ratio: 99, text: '', size: 0, element: null };
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
    const byColour = {};
    for (const e of ctas) { const k = getComputedStyle(e).backgroundColor; byColour[k] = (byColour[k] || 0) + 1; }
    const primary = Object.entries(byColour).sort((a, b) => b[1] - a[1])[0];
    const tops = [...document.querySelectorAll('main > *, body > * > section, body > section')].filter(vis)
      .map((e) => e.getBoundingClientRect()).sort((a, b) => a.top - b.top);
    const gaps = [];
    for (let i = 1; i < tops.length; i++) gaps.push(px(tops[i].top - tops[i - 1].bottom));
    const gapSet = [...new Set(gaps.filter((g) => g >= 0))].sort((a, b) => a - b);
    const ch = (e) =>
      Math.round(e.getBoundingClientRect().width / (px(getComputedStyle(e).fontSize) * 0.5));
    const paras = [...document.querySelectorAll('p')].filter(vis).map((e) => ({
      ch: ch(e),
      text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 50),
    })).sort((a, b) => b.ch - a.ch);
    // The chrome. `main` is the shell's skip target (#content) and the header and footer are its
    // siblings, so the frame's own paragraphs are found from that column rather than by any
    // data-attribute the frame would have to carry for no other reason.
    const chrome = [];
    for (const e of [...document.querySelectorAll('main')].map((m) => m.parentElement || document.body)
      .flatMap((column) => [...column.querySelectorAll(':scope > header [data-component="text"], :scope > footer [data-component="text"]')])) {
      if (!vis(e) || e.children.length || e.closest('button,[data-component=badge]')) continue;
      chrome.push({
        region: e.closest('footer') ? 'footer' : 'header',
        size: px(getComputedStyle(e).fontSize),
        ch: ch(e),
        text: (e.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 40),
      });
    }
    // The brand link, measured as the element a person tabs to rather than the span inside it.
    const link = document.querySelector('[data-sidebar-brand] a');
    let brand = null;
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
      actual_width: innerWidth,
      actual_height: innerHeight,
      theme: document.documentElement.dataset.theme,
      sidebar: [...document.querySelectorAll('[data-sidebar-brand], [data-sidebar-brand] a, [data-sidebar-brand] span, [data-sidebar-item]:not([data-disabled]), [data-sidebar-item]:not([data-disabled]) > a')].filter(vis).map(e => ({
        ...described(e), text: (e.textContent || '').trim().slice(0,40),
        ratio: Math.round(contrast(computedColour(getComputedStyle(e).color), sidebarBackground(e)) * 100) / 100,
        size: px(getComputedStyle(e).fontSize),
      })),
      forms: [...document.querySelectorAll('main form')].filter(e => e.querySelector('[name=title]')).map(e => ({
        max: getComputedStyle(e).maxWidth, width: e.getBoundingClientRect().width,
        parent: e.parentElement.getBoundingClientRect().width,
        root: parseFloat(getComputedStyle(document.documentElement).fontSize),
        controls: [...e.querySelectorAll('input[name=title], select[name=priority], textarea')].map(c => ({
          tag: c.tagName, width: c.getBoundingClientRect().width, column: e.getBoundingClientRect().width,
        })),
      })),
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
  measurement.stylesheet_hex = await page.evaluate(async () => {
    const sheets = [];
    for (const link of document.querySelectorAll('link[rel=stylesheet]')) {
      const href = link.href;
      try {
        const response = await fetch(href);
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const css = await response.text();
        sheets.push({href: href.replace(/\?.*$/, ''), hex: (css.match(/#[0-9a-fA-F]{3,8}\b/g) || []).length,
          vars: (css.match(/var\(--/g) || []).length, defines: (css.match(/--[a-z][\w-]*\s*:/g) || []).length, bytes: css.length});
      } catch (error) { sheets.push({href, error: String(error)}); }
    }
    return sheets;
  });
  return measurement;
}

/** The floor, one line per rule, in the gate's own wording and its own numbers. */
export function refusals(m) {
  const r = [];
  if (m.left_edges.length > FLOOR.maxLeftEdges) r.push(`${m.width}px: ${m.left_edges.length} left edges ${JSON.stringify(m.left_edges)} — one content column and at most one rail`);
  if (m.body_font_sizes.length > FLOOR.maxBodySizes) r.push(`${m.width}px: body text in ${m.body_font_sizes.length} sizes ${JSON.stringify(m.body_font_sizes)} — body size is constant down the page`);
  if (m.all_font_sizes.length > FLOOR.maxFontSizes) r.push(`${m.width}px: ${m.all_font_sizes.length} font sizes ${JSON.stringify(m.all_font_sizes)} — a type scale has at most 6 steps`);
  if (m.primary_ctas_above_fold > FLOOR.maxPrimaryCtas) r.push(`${m.width}px: ${m.primary_ctas_above_fold} primary buttons above the fold — one thing to do per view`);
  if (m.worst_contrast.ratio < FLOOR.minContrast && m.worst_contrast.size < FLOOR.contrastSizeCeiling) r.push(`${m.width}px: contrast ${m.worst_contrast.ratio} on "${m.worst_contrast.text}" — below 4.5:1`);
  if (m.section_gaps.length > FLOOR.maxSectionGaps) r.push(`${m.width}px: ${m.section_gaps.length} distinct section gaps ${JSON.stringify(m.section_gaps)} — spacing comes from a scale`);
  if (m.width >= FLOOR.measureWidthFloor && m.measures_ch.max > FLOOR.maxMeasureCh) r.push(`${m.width}px: body measure up to ${m.measures_ch.max}ch — keep lines under 75 characters`);
  if (m.scroll_width_overflow) r.push(`${m.width}px: the page scrolls sideways`);
  for (const sheet of m.stylesheet_hex || []) {
    if (!sheet.error && (sheet.defines || 0) < 20 && (sheet.hex || 0) > 0)
      r.push(`${m.width}px: ${sheet.href.split('/').pop()} carries ${sheet.hex} raw hex colour(s) and defines ${sheet.defines || 0} tokens — a client writes tokens, not hex`);
  }
  return r;
}

/** Unreadable sheets are notes in the external rule; served-app tests require readable assets. */
export function notes(m) {
  return (m.stylesheet_hex || []).filter(s => s.error).map(s => `${m.width}px: could not read ${s.href}: ${s.error}`);
}

/** One line per measurement, so the run itself is the number a report quotes. */
export function report(label, m) {
  return `  ${label} @${m.width}px edges=${JSON.stringify(m.left_edges)} body=${JSON.stringify(m.body_font_sizes)} ` +
    `sizes=${m.all_font_sizes.length} cta=${m.primary_ctas_above_fold} contrast=${m.worst_contrast.ratio}(${m.worst_contrast.size}px "${m.worst_contrast.text}") ` +
    `gaps=${JSON.stringify(m.section_gaps)} measure=${m.measures_ch.max} widest=${JSON.stringify(m.widest[0])} ` +
    `chrome=${JSON.stringify(m.chrome)} brand=${JSON.stringify(m.brand_link)}`;
}

/** A refusal, with the element that caused it: for the failure message, not for an assertion. */
export function diagnosed(m, why) {
  return `${why}\n    worst element: ${JSON.stringify(m.worst_element)}\n    overflowing: ${JSON.stringify(m.overflowing.slice(0, 6))}`;
}
