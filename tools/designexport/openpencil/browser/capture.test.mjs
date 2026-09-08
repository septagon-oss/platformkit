import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { after, afterEach, before, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'
import { captureExample } from './capture.mjs'
import { resolveColorExpression } from '../color-expression.mjs'
import { computedColor } from '../computed-color.mjs'

const repo = fileURLToPath(new URL('../../../../', import.meta.url))
const primary = 'pk-ui.component.button/primary'
const withIcon = 'pk-ui.component.button/with-icon'
const brandBadge = 'pk-ui.component.badge/brand-dot'
const viewport = { width: 1280, height: 900 }
const source = JSON.parse(execFileSync('go', ['run', './tools/designexport'], { cwd: repo, encoding: 'utf8' }))
const require = createRequire(import.meta.url)
const faces = [400, 600].map(weight => {
  const bytes = readFileSync(require.resolve(`@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`))
  return { family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: createHash('sha256').update(bytes).digest('hex') }
})
const face = faces[1]
let browser
before(async () => { browser = await chromium.launch({ headless: true, args: ['--enable-automation'] }) })
after(async () => { await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0, 'capture closed every disposable context'))

function projection(id, props) {
  return JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', id, '--props'], {
    cwd: repo, encoding: 'utf8', input: JSON.stringify(props),
  }))
}

function observed(nodes) {
  return nodes.flatMap(node => [node, ...observed(node.children ?? [])])
}

test('authored Go color roles match Chromium across palettes and independent transparent token edits', async () => {
  const context = await browser.newContext()
  try {
    const page = await context.newPage()
    const cases = await page.evaluate(({ css, themes }) => {
      const sheet = new CSSStyleSheet()
      sheet.replaceSync(css)
      document.adoptedStyleSheets = [sheet]
      const definitions = [...sheet.cssRules].filter(rule => rule.selectorText === ':root').flatMap(rule =>
        [...rule.style].filter(name => name.startsWith('--pk-role-')).map(name => [name, rule.style.getPropertyValue(name)]))
      const sample = document.createElement('span'), results = []
      document.body.append(sample)
      for (const theme of themes) {
        document.documentElement.dataset.theme = theme.mode
        const tokens = theme.tokens.filter(token => token.type === 'color').map(token => [token.name, token.value])
        for (const edit of [null, ...tokens.flatMap(([name]) => ['#20406080', '#abcdef00'].map(value => [name, value]))]) {
          const values = new Map(tokens)
          if (edit) values.set(...edit)
          for (const [name, value] of values) document.documentElement.style.setProperty(name, value, 'important')
          const colors = definitions.map(([name]) => {
            // Ask Chromium for modern serialization: legacy rgba() can print
            // #80 alpha as 0.5, concealing its actual 128/255 value.
            sample.style.color = `color(from var(${name}) srgb r g b / alpha)`
            return getComputedStyle(sample).color
          })
          results.push({ mode: theme.mode, edit, definitions, values: [...values], colors })
        }
      }
      return results
    }, source)
    assert.equal(cases.length, source.themes.reduce((count, theme) => count + 1 + 2 * theme.tokens.filter(token => token.type === 'color').length, 0))
    for (const item of cases) {
      const definitions = new Map(item.definitions), values = new Map(item.values)
      assert.equal(definitions.size, item.definitions.length, 'each tested role has one authored declaration')
      assert.ok(item.definitions.some(([, value]) => value.startsWith('color-mix(')))
      for (const [index, [name, expression]] of item.definitions.entries()) {
        const actual = resolveColorExpression(expression, key => values.get(key) ?? definitions.get(key))
        const expected = computedColor(item.colors[index])
        for (const channel of ['r', 'g', 'b', 'a']) assert.ok(Math.abs(actual[channel] - expected[channel]) <= .000001,
          `${item.mode} ${name} ${item.edit}: ${channel} ${actual[channel]} != ${expected[channel]}`)
      }
    }
  } finally { await context.close() }
})

function occurrenceFixture() {
  const snapshot = structuredClone(source), example = snapshot.examples.find(item => item.id === primary)
  const html = '<span>Same</span>', prefix = '<div>João 🧩 '
  example.html = `${prefix}${html}${html}</div>`
  const start = Buffer.byteLength(prefix), size = Buffer.byteLength(html)
  example.children = ['first/🧩', 'second-->'].map((id, index) => ({
    description: { id, componentId: 'fixture.same', html, children: [] }, slot: 'children',
    span: { start: start + index * size, end: start + (index + 1) * size },
  })).reverse()
  return { snapshot, example }
}

test('capture maps source byte spans to exact nested elements without name or order matching', async () => {
  const { snapshot } = occurrenceFixture(), before = structuredClone(snapshot)
  const capture = await captureExample(browser, snapshot, primary)
  const owned = observed(capture.roots).filter(node => node.source)
  assert.deepEqual(owned.map(node => node.source), [
    { path: [primary], componentId: 'pk-ui.component.button' },
    { path: [primary, 'first/🧩'], componentId: 'fixture.same', slot: 'children' },
    { path: [primary, 'second-->'], componentId: 'fixture.same', slot: 'children' },
  ])
  assert.deepEqual(owned.slice(1).map(node => node.children[0].text), ['Same', 'Same'])
  assert.deepEqual(capture.roots[0].bounds, (await originalLayout(snapshot, primary, 'light')).bounds)
  assert.deepEqual(snapshot, before)
  const form = 'pk-ui.component.form/default'
  const real = await captureExample(browser, source, form)
  assert.deepEqual(observed(real.roots).filter(node => node.source).map(node => node.source.path), [
    [form], [form, 'title'], [form, 'actions'], [form, 'actions', 'cancel'], [form, 'actions', 'create'],
  ])
})

test('capture refuses invalid, overlapping, ambiguous or parser-moved occurrence spans', async () => {
  for (const mutate of [
    example => { example.children[0].span.start++ },
    example => { example.children[0].span.end = Infinity },
    example => { example.children[0].span = { ...example.children[1].span } },
    example => { example.children[0].description.id = example.children[1].description.id },
    example => { example.html += '<!--pk-capture:0-->' },
    example => {
      example.children = [{ description: { id: 'transparent', componentId: 'fixture', html: example.html, children: [] },
        span: { start: 0, end: Buffer.byteLength(example.html) } }]
    },
    example => {
      const html = '<span>Broken</span></p>'
      example.html = `<p>${html}`
      example.children = [{ description: { id: 'moved', componentId: 'fixture', html, children: [] },
        span: { start: 3, end: Buffer.byteLength(example.html) } }]
    },
  ]) {
    const { snapshot, example } = occurrenceFixture()
    mutate(example)
    await assert.rejects(captureExample(browser, snapshot, primary), /occurrence|capture marker/i)
    assert.equal(browser.contexts().length, 0)
  }
})

test('capture does not invent single-element ownership for fragments or unobserved children', async () => {
  const { snapshot, example } = occurrenceFixture()
  delete example.children[0].span
  let result = await captureExample(browser, snapshot, primary)
  assert.equal(observed(result.roots).filter(node => node.source).length, 2)
  example.html = '<span>One</span><span>Two</span>'
  example.children = []
  result = await captureExample(browser, snapshot, primary)
  assert.equal(result.roots.length, 2)
  assert.ok(result.roots.every(node => !node.source))
  for (const prefix of [' ', '\u00a0']) {
    example.html = `${prefix}<span>One</span>`
    snapshot.css += '\nbody { white-space: pre; }'
    result = await captureExample(browser, snapshot, primary)
    assert.ok(result.roots.every(node => !node.source), 'even whitespace may paint outside the candidate element')
  }
})

test('capture preserves adjacent source text nodes and refuses instrumentation that changes HTML parsing', async () => {
  const { snapshot, example } = occurrenceFixture()
  example.html = '<div>Before child after</div>'
  example.children = [{ description: { id: 'text', componentId: 'fixture', html: 'child', children: [] }, span: { start: 12, end: 17 } }]
  const result = await captureExample(browser, snapshot, primary)
  assert.equal(result.roots[0].children.length, 1)
  assert.equal(result.roots[0].children[0].text, 'Before child after')
  // A real invocation can emit '&' and its parent the rest of an entity.
  // Instrumenting inside that boundary must not turn '&' into '&amp;'.
  example.html = '<div>&amp;</div>'
  example.children[0].description.html = '&'
  example.children[0].span = { start: 5, end: 6 }
  await assert.rejects(captureExample(browser, snapshot, primary), /occurrence.*parsing/i)
})

test('capture observes actual text-control values and exact fonts without inventing DOM text', async () => {
  for (const value of ['', 'João Ação 123', '<b> & "']) {
    const snapshot = projection('pk-ui.component.input/bare', { value })
    const result = await captureExample(browser, snapshot, snapshot.examples[0].id, { fonts: faces })
    const input = observed(result.roots).find(node => node.tag === 'input')
    assert.deepEqual(input.children, [])
    assert.deepEqual(Object.keys(input.control).toSorted(), ['fonts', 'kind', 'placeholder', 'property', 'type', 'value'])
    assert.deepEqual({ ...input.control, fonts: [] }, { kind: 'control', property: 'value', type: 'text', value, placeholder: '', fonts: [] })
    if (value === '') assert.deepEqual(input.control.fonts, [])
    else {
      assert.equal(input.control.fonts.length, 1)
      assert.equal(input.control.fonts[0].postScriptName, 'IBMPlexSans-Regular')
      assert.equal(input.control.fonts[0].isCustomFont, true)
      assert.ok(input.control.fonts[0].glyphCount > 0)
    }
  }
  const placeholder = projection('pk-ui.component.input/bare', { placeholder: 'Not a value' })
  placeholder.css += '\ninput::placeholder { font-weight: 600; }'
  const paint = observed((await captureExample(browser, placeholder, placeholder.examples[0].id, { fonts: faces })).roots).find(node => node.control).control
  assert.equal(paint.value, '')
  assert.equal(paint.placeholder, 'Not a value')
  assert.equal(paint.fonts[0].postScriptName, 'IBMPlexSans-SemiBold', 'these glyphs belong to the placeholder, not an invented value')
  const { snapshot, example } = occurrenceFixture()
  example.children = []
  example.html = '<input type="text" data-pk-value="value" value="line&#10;break">'
  assert.equal((await captureExample(browser, snapshot, primary)).roots[0].control.value, 'linebreak', 'observe browser normalization; binding must reject mismatched source')
  for (const html of ['<input type="text" value="unmarked">', '<input type="password" value="private">']) {
    example.html = html
    assert.equal((await captureExample(browser, snapshot, primary)).roots[0].control, undefined)
  }
})

test('capture retains text presentation that native construction must not silently discard', async () => {
  const snapshot = projection('pk-ui.component.input/bare', { value: 'Audit' })
  snapshot.css += '\nbody { font-synthesis: none; }'
  snapshot.css += '\ninput { text-indent: 20px; text-shadow: 4px 0 red; word-spacing: 3px; writing-mode: vertical-rl; direction: rtl; font-synthesis-weight: auto; }'
  const result = await captureExample(browser, snapshot, snapshot.examples[0].id, { fonts: faces })
  const { style } = observed(result.roots).find(node => node.control)
  assert.equal(style['text-indent'], '20px')
  assert.equal(style['text-shadow'], 'rgb(255, 0, 0) 4px 0px 0px')
  assert.equal(style['word-spacing'], '3px')
  assert.equal(style['writing-mode'], 'vertical-rl')
  assert.equal(style.direction, 'rtl')
  assert.equal(style['font-synthesis-weight'], 'auto', 'the control overrides its inherited weight synthesis')
  assert.equal(style['font-synthesis-style'], 'none', 'style synthesis remains disabled by inheritance')
})

// Independent DOM measurements remove the source annotations entirely.
// No capture traversal or layout helper is reused.
async function originalLayout(snapshot, id, mode, size = viewport, fonts = []) {
  const example = snapshot.examples.find(example => example.id === id)
  const html = example.html.replace(/<!--\/?pk-(?:text|slot):[A-Za-z][A-Za-z0-9]*-->/g, '')
  const context = await browser.newContext({ viewport: size, colorScheme: mode, reducedMotion: 'reduce' })
  try {
    const page = await context.newPage()
    await page.setContent(`<!doctype html><html lang="en" data-theme="${mode}"><head><style>${snapshot.css}</style></head><body>${html}</body></html>`)
    await page.evaluate(async fonts => {
      for (const font of fonts) {
        const face = new FontFace(font.family, Uint8Array.from(font.bytes), { weight: String(font.weight), style: font.style })
        document.fonts.add(await face.load())
      }
      await document.fonts.ready
    }, fonts.map(font => ({ family: font.family, weight: font.weight, style: font.style, bytes: [...font.bytes] })))
    return await page.locator('body > :first-child').evaluate(element => {
      const rect = element => {
        const { x, y, width, height } = element.getBoundingClientRect()
        return { x, y, width, height }
      }
      const textRects = [...element.childNodes].filter(node => node.nodeType === Node.TEXT_NODE).flatMap(node => {
        const range = document.createRange()
        range.selectNodeContents(node)
        return [...range.getClientRects()].map(({ x, y, width, height }) => ({ x, y, width, height }))
      })
      const style = getComputedStyle(element)
      return {
        bounds: rect(element), icons: [...element.querySelectorAll('svg')].map(rect),
        color: style.color, backgroundColor: style.backgroundColor, text: element.textContent, textRects,
      }
    })
  } finally { await context.close() }
}

test('browser capture preserves real Button layout, text regions and source identity', async () => {
  const beforeSource = structuredClone(source)
  for (const mode of ['light', 'dark']) {
    const result = await captureExample(browser, source, primary, { mode, viewport })
    const original = await originalLayout(source, primary, mode)
    assert.equal(result.sourceSHA, source.sha256)
    assert.equal(result.exampleId, primary)
    assert.equal(result.componentId, 'pk-ui.component.button')
    assert.equal(result.mode, mode)
    assert.deepEqual(result.viewport, viewport)
    assert.deepEqual(Object.keys(result.environment).toSorted(), ['browser', 'fontHinting', 'headless', 'protocol'])
    assert.equal(typeof result.environment.browser, 'string')
    assert.ok(result.environment.browser.length > 0)
    assert.equal(typeof result.environment.protocol, 'string')
    assert.ok(result.environment.protocol.length > 0)
    assert.equal(result.environment.headless, true)
    assert.equal(result.environment.fontHinting, 'default')
    assert.equal(result.roots.length, 1)
    const button = result.roots[0]
    assert.equal(button.style['font-synthesis-weight'], 'auto')
    assert.equal(button.style['font-synthesis-style'], 'auto')
    assert.equal(button.kind, 'element')
    assert.equal(button.tag, 'button')
    assert.equal(button.component, 'button')
    assert.deepEqual(button.bounds, original.bounds)
    assert.equal(button.bounds.height, 38)
    assert.equal(button.style.display, 'inline-flex')
    assert.equal(button.style['column-gap'], '8px')
    assert.equal(button.style.color, original.color)
    assert.equal(button.sizing.width, 'auto')
    assert.equal(button.children.length, 1)
    const label = button.children[0]
    assert.equal(label.kind, 'text')
    assert.equal(label.property, 'label')
    assert.equal(label.text, original.text)
    assert.equal(label.text, 'Save')
    assert.ok(label.bounds.width > 0 && label.rects.length > 0)
    assert.deepEqual(label.rects, original.textRects)
    assert.ok(label.fonts.length > 0)
    assert.ok(label.fonts.every(font => font.glyphCount > 0))
    assert.deepEqual(Object.keys(label).toSorted(), ['bounds', 'fonts', 'kind', 'property', 'rects', 'text'])
    assert.ok(observed(result.roots).filter(node => node.kind === 'element').every(node => !Object.hasOwn(node, 'fonts')))
    assert.ok(observed(result.roots).every(node =>
      !Object.hasOwn(node, 'observationId') && !Object.hasOwn(node, 'fontObservationIds')))
  }
  assert.deepEqual(source, beforeSource)
})

test('browser capture records explicit font hinting without leaking browser arguments', async () => {
  const privateMarker = 'private-capture-fixture:/private/capture-fixture'
  const unhinted = await chromium.launch({
    headless: true, args: ['--enable-automation', '--font-render-hinting=none', `--user-agent=${privateMarker}`],
  })
  try {
    const result = await captureExample(unhinted, source, primary)
    assert.equal(result.environment.fontHinting, 'none')
    assert.equal(result.environment.headless, true)
    assert.deepEqual(Object.keys(result.environment).toSorted(), ['browser', 'fontHinting', 'headless', 'protocol'])
    assert.ok(!JSON.stringify(result.environment).includes(privateMarker))
    assert.equal(unhinted.contexts().length, 0)
  } finally { await unhinted.close() }
})

test('browser capture refuses uninspectable rendering metadata without leaking contexts', async () => {
  const uninspectable = await chromium.launch({ headless: true, ignoreDefaultArgs: ['--enable-automation'] })
  try {
    await assert.rejects(captureExample(uninspectable, source, primary), /requires Chromium launched with --enable-automation/)
    assert.equal(uninspectable.contexts().length, 0)
  } finally { await uninspectable.close() }
})

test('browser capture observes token alias candidates, mixed paints and equal-colored literals', async () => {
  const accent = '--pk-color-accent-default'
  const onAccent = '--pk-color-accent-on'
  const surface = '--pk-color-surface-primary'
  for (const mode of ['light', 'dark']) {
    const button = (await captureExample(browser, source, primary, { mode })).roots[0]
    const originalButton = await originalLayout(source, primary, mode)
    assert.deepEqual(button.paintSources['background-color'], { tokens: [accent], directCandidate: accent })
    assert.deepEqual(button.paintSources.color, { tokens: [onAccent], directCandidate: onAccent })
    assert.equal(button.style['background-color'], originalButton.backgroundColor)
    assert.equal(button.style.color, originalButton.color)

    const badge = (await captureExample(browser, source, brandBadge, { mode })).roots[0]
    const originalBadge = await originalLayout(source, brandBadge, mode)
    assert.deepEqual(badge.paintSources['background-color'].tokens.toSorted(), [accent, surface].toSorted())
    assert.equal(badge.paintSources['background-color'].directCandidate, null)
    assert.deepEqual(badge.paintSources.color, { tokens: [accent], directCandidate: accent })
    assert.equal(badge.style['background-color'], originalBadge.backgroundColor)
    assert.equal(badge.style.color, originalBadge.color)

    const literal = structuredClone(source)
    const value = source.themes.find(theme => theme.mode === mode).tokens.find(token => token.name === accent).value
    literal.css += `\n[data-component="button"] { background-color: ${value}; }\n`
    const fixed = (await captureExample(browser, literal, primary, { mode })).roots[0]
    assert.equal(fixed.style['background-color'], button.style['background-color'], 'the literal is visually identical')
    assert.deepEqual(fixed.paintSources['background-color'], { tokens: [], directCandidate: null })
    assert.deepEqual(fixed.paintSources.color, button.paintSources.color)
  }
})

test('paint capture observes alpha-only dependencies without turning them into literals or direct aliases', async () => {
  const accent = '--pk-color-accent-default', surface = '--pk-color-surface-primary'
  for (const mode of ['light', 'dark']) {
    for (const [expression, tokens] of [
      [`rgb(from var(${accent}) 30 40 50 / alpha)`, [accent]],
      [`rgb(from var(${accent}) 30 40 50 / min(1, calc(alpha * 100)))`, [accent]],
      [`rgb(from color-mix(in srgb, var(${accent}), var(${surface})) 30 40 50 / alpha)`, [accent, surface]],
      ['rgb(30 40 50)', []],
    ]) {
      const snapshot = structuredClone(source)
      snapshot.css += `\n[data-component="button"] { color: ${expression}; background-color: ${expression}; border-color: ${expression}; }`
      const before = structuredClone(snapshot)
      const capture = await captureExample(browser, snapshot, withIcon, { mode, fonts: faces })
      const root = capture.roots[0], paints = [
        root.paintSources.color, root.paintSources['background-color'],
        ...['top', 'right', 'bottom', 'left'].map(side => root.paintSources[`border-${side}-color`]),
        ...observed(root.children).filter(node => node.tag === 'path').map(node => node.paintSources.fill),
      ]
      assert.ok(paints.length > 6, 'include inherited currentColor on the actual source SVG paths')
      for (const paint of paints) {
        assert.deepEqual(paint.tokens.toSorted(), tokens.toSorted(), expression)
        assert.equal(paint.directCandidate, null, 'fixed RGB channels do not directly alias the token')
      }
      const original = await originalLayout(snapshot, withIcon, mode, viewport, faces)
      assert.equal(root.style.color, original.color, 'capture retains the unmodified source paint')
      assert.deepEqual(root.bounds, original.bounds, 'probing does not leak altered layout')
      assert.deepEqual(snapshot, before)
    }
  }
})

test('capture retains uniquely witnessed authored color expressions without a second role map', async () => {
  const accent = '--pk-color-accent-default', surface = '--pk-color-surface-primary'
  for (const mode of ['light', 'dark']) {
    const badge = (await captureExample(browser, source, brandBadge, { mode })).roots[0]
    assert.deepEqual(badge.paintSources['background-color'].expressionCandidate, {
      customProperty: '--pk-role-surface-brand-soft',
      value: `color-mix(in srgb, var(${accent}) 12%, var(${surface}))`, customProperties: {},
    })
    const snapshot = structuredClone(source)
    const value = `color-mix(in srgb, var(${accent}) 27%, var(--product-paper))`
    snapshot.css += `\n:root { --product-tint: ${value}; --product-paper: var(${surface}); }
      [data-component="button"] { background-color: var(--product-tint); color: var(--product-tint); }`
    const before = structuredClone(snapshot)
    const root = (await captureExample(browser, snapshot, withIcon, { mode })).roots[0]
    const paint = root.paintSources['background-color']
    assert.deepEqual({ ...paint, tokens: paint.tokens.toSorted() }, {
      tokens: [accent, surface].toSorted(), directCandidate: null,
      expressionCandidate: { customProperty: '--product-tint', value, customProperties: { '--product-paper': `var(${surface})` } },
    })
    const original = await originalLayout(snapshot, withIcon, mode)
    assert.deepEqual(root.bounds, original.bounds)
    assert.equal(root.style['background-color'], original.backgroundColor)
    const vector = observed(root.children).find(node => node.tag === 'path')
    assert.deepEqual(vector.paintSources.fill.expressionCandidate, paint.expressionCandidate, 'currentColor retains the same authored relationship')
    paint.expressionCandidate.customProperties['--product-paper'] = 'transparent'
    assert.equal(root.paintSources.color.expressionCandidate.customProperties['--product-paper'], `var(${surface})`, 'paint records are caller-owned, not shared mutable definitions')
    assert.equal(vector.paintSources.fill.expressionCandidate.customProperties['--product-paper'], `var(${surface})`)
    assert.deepEqual(snapshot, before)
    assert.deepEqual(Object.keys(badge.paintSources.color).toSorted(), ['directCandidate', 'tokens'], 'direct aliases keep their existing evidence')
  }
})

test('expression capture leaves ambiguous, shadowed, unsupported and probe-mismatched definitions unclaimed', async () => {
  const accent = '--pk-color-accent-default', surface = '--pk-color-surface-primary'
  const value = `color-mix(in srgb, var(${accent}) 27%, var(${surface}))`
  for (const mode of ['light', 'dark']) {
    for (const extra of [
      `:root { --product-tint: ${value}; }`,
      `[data-component="button"] { --product-tint: ${value}; }`,
      ':root { --product-alias: var(--product-tint); } [data-component="button"] { background-color: var(--product-alias); }',
      `@media (min-width: 1px) { :root { --product-tint: color-mix(in srgb, var(${accent}) 80%, var(${surface})); } }`,
      `@media (min-width: 99999px) { :root { --product-tint: ${value}; } }`,
      `:root[data-theme="${mode === 'light' ? 'dark' : 'light'}"] { --product-tint: ${value}; }`,
    ]) {
      const snapshot = structuredClone(source)
      // Equal baseline tokens conceal different mix weights until a token is probed.
      for (const theme of snapshot.themes) {
        const paper = theme.tokens.find(token => token.name === surface).value
        theme.tokens.find(token => token.name === accent).value = paper
        snapshot.css += `\n:root[data-theme="${theme.mode}"] { ${accent}: ${paper}; }`
      }
      snapshot.css += `\n:root { --product-tint: ${value}; }
        [data-component="button"] { background-color: var(--product-tint); } ${extra}`
      const paint = (await captureExample(browser, snapshot, primary, { mode })).roots[0].paintSources['background-color']
      assert.deepEqual(paint.tokens.toSorted(), [accent, surface].toSorted())
      assert.equal(paint.directCandidate, null)
      assert.equal(paint.expressionCandidate, undefined, extra)
      assert.deepEqual(Object.keys(paint).toSorted(), ['directCandidate', 'tokens'], 'temporary probe records never escape capture')
    }
    const snapshot = structuredClone(source)
    snapshot.css += `\n:root { --product-tint: color-mix(in oklab, var(${accent}), var(${surface})); }
      [data-component="button"] { background-color: var(--product-tint); }`
    const paint = (await captureExample(browser, snapshot, primary, { mode })).roots[0].paintSources['background-color']
    assert.deepEqual(paint.tokens.toSorted(), [accent, surface].toSorted())
    assert.equal(paint.expressionCandidate, undefined, 'unimplemented color spaces are not rewritten as sRGB')
  }
})

test('browser capture follows source full-width layout at mobile and wide viewports', async () => {
  const label = 'Save in this viewport'
  const snapshot = projection(withIcon, { fullWidth: true, label })
  for (const width of [320, 1280]) {
    const size = { width, height: 900 }
    const result = await captureExample(browser, snapshot, withIcon, { viewport: size })
    const original = await originalLayout(snapshot, withIcon, 'light', size)
    const button = result.roots[0]
    assert.deepEqual(result.viewport, size)
    assert.deepEqual(button.bounds, original.bounds)
    assert.equal(button.bounds.width, width)
    assert.equal(button.bounds.height, 38)
    assert.equal(button.style['column-gap'], '8px')
    assert.deepEqual(observed(button.children).filter(node => node.icon).map(node => node.bounds), original.icons)
    const text = button.children.find(node => node.property === 'label')
    assert.equal(text.text, label)
    assert.equal(text.text, original.text)
    assert.deepEqual(text.rects, original.textRects)
  }
})

test('browser capture does not add flex gaps for empty labels or parse escaped text as markers', async () => {
  for (const text of ['', ' ', 'A & <b> <!--/pk-text:label--> <!--pk-slot:Content-->']) {
    const snapshot = projection(withIcon, { label: text })
    const result = await captureExample(browser, snapshot, withIcon)
    const original = await originalLayout(snapshot, withIcon, 'light')
    const button = result.roots[0]
    assert.deepEqual(button.bounds, original.bounds)
    assert.deepEqual(observed(button.children).filter(node => node.icon).map(node => node.bounds), original.icons)
    const labels = observed(result.roots).filter(node => node.property === 'label')
    assert.equal(labels.length, 1)
    assert.equal(labels[0].text, text)
    if (text === '' || text === ' ') assert.deepEqual(labels[0].fonts, [])
    if (text === '') {
      assert.equal(button.bounds.width, 54)
      assert.equal(labels[0].bounds.width, 0)
      assert.deepEqual(labels[0].rects, [])
    }
  }
})

test('browser capture records markers rather than inferring properties from lookalike text', async () => {
  const unmarked = structuredClone(source)
  const example = unmarked.examples.find(example => example.id === primary)
  example.html = example.html.replaceAll('<!--pk-text:label-->', '').replaceAll('<!--/pk-text:label-->', '')
  const plain = observed((await captureExample(browser, unmarked, primary)).roots).filter(node => node.kind === 'text')
  assert.deepEqual(plain.map(node => node.text), ['Save'])
  assert.ok(plain.every(node => !Object.hasOwn(node, 'property')))
  const iconOnly = projection(withIcon, { iconOnly: true })
  const icon = observed((await captureExample(browser, iconOnly, withIcon)).roots)
  assert.equal(icon.filter(node => node.icon).length, 1)
  assert.ok(icon.every(node => !Object.hasOwn(node, 'property')))
  // Balanced observations are not a typed/native binding-readiness decision.
  example.html = '<button><!--pk-text:unknown-->Save<!--/pk-text:unknown--></button>'
  const unknown = observed((await captureExample(browser, unmarked, primary)).roots)
  assert.equal(unknown.find(node => node.kind === 'text').property, 'unknown')
})

test('Select keeps its own copy binding and native single/multiple keyboard selection', async () => {
  const id = 'pk-ui.component.select/default', label = 'State & <kind>'
  for (const multiple of [false, true]) for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
    const snapshot = projection(id, { label, multiple, error: 'Check the selection.' })
    const capture = await captureExample(browser, snapshot, id, { mode, viewport: { width, height: 900 } })
    const regions = observed(capture.roots).filter(node => Object.hasOwn(node, 'property'))
    assert.deepEqual(regions.map(node => [node.property, node.text]), [['label', label]])
    const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme: mode })
    try {
      await page.setContent(`<html data-theme="${mode}"><style>${snapshot.css}</style><body>${snapshot.examples[0].html}</body></html>`)
      const control = page.getByRole(multiple ? 'listbox' : 'combobox', { name: label, exact: false })
      assert.equal(await control.inputValue(), 'post')
      assert.equal(await control.getAttribute('aria-invalid'), 'true')
      assert.deepEqual(await control.evaluate(node => node.getAttribute('aria-describedby').split(' ').map(id => document.getElementById(id).textContent)),
        ['Check the selection.', 'What the entry renders as.'])
      await page.locator('label[for="pk-select-kind"]').click()
      assert.equal(await control.evaluate(node => node === document.activeElement), true)
      if (multiple) { await control.press('Home'); await control.press('Shift+End') }
      else await control.press('ArrowDown')
      assert.deepEqual(await control.evaluate(node => [...node.selectedOptions].map(option => option.value)), multiple ? ['post', 'page'] : ['page'])
      assert.equal(await control.evaluate(node => getComputedStyle(node).outlineStyle !== 'none'), true)
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    } finally { await page.close() }
  }
})

test('Select submits exact opaque values including an explicit empty multi-selection', async () => {
  const options = [{ value: 'padded', label: 'Plain' }, { value: ' padded ', label: 'Padded' }, { value: '', label: 'Empty' }]
  for (const [props, expected] of [[{ value: ' padded ' }, [' padded ']],
    [{ multiple: true, value: '', values: ['', ' padded '] }, [' padded ', '']]]) {
    const snapshot = projection('pk-ui.component.select/default', { ...props, options })
    const page = await browser.newPage()
    try {
      await page.setContent(`<style>${snapshot.css}</style><form>${snapshot.examples[0].html}</form>`)
      assert.deepEqual(await page.locator('select').evaluate(node => [...node.selectedOptions].map(option => option.value)), expected)
      assert.deepEqual(await page.evaluate(() => new FormData(document.querySelector('form')).getAll('kind')), expected)
    } finally { await page.close() }
  }
})

test('browser capture retains source-owned canonical icon identities for aliases and fallback', async () => {
  for (const [name, canonicalName] of [['upload', 'upload-simple'], [' X_MARK ', 'x'], ['missing-glyph', 'question']]) {
    const snapshot = projection('pk-ui.component.icon/check', { name, tone: 'neutral' })
    const captured = await captureExample(browser, snapshot, snapshot.examples[0].id)
    const icon = captured.roots[0].icon
    assert.equal(captured.roots[0].style.color, 'rgb(21, 34, 31)', 'source foreground has settled before observation')
    assert.equal(icon.name, name)
    assert.equal(icon.canonicalName, canonicalName)
    assert.ok(snapshot.icons.some(asset => asset.name === icon.canonicalName))
    assert.ok(icon.svg.includes(`data-pk-icon-canonical="${canonicalName}"`))
  }
})

test('browser capture retains actual Button SVG topology, attributes and descendant paint dependencies', async () => {
  for (const mode of ['light', 'dark']) {
    const result = await captureExample(browser, source, withIcon, { mode })
    const svg = observed(result.roots).find(node => node.icon)
    assert.deepEqual(observed([svg]).map(node => node.tag), ['svg', 'path'])
    assert.equal(svg.attributes.fill, 'currentColor')
    assert.equal(svg.attributes['aria-hidden'], 'true')
    assert.equal(svg.attributes['data-pk-icon-canonical'], 'plus')
    const path = svg.children[0]
    assert.deepEqual(path.attributes, { d: source.icons.find(icon => icon.name === 'plus').svg.match(/<path d="([^"]+)"/)[1] })
    assert.equal(path.style.fill, svg.style.color)
    assert.equal(path.style.stroke, 'none')
    assert.ok(path.paintSources.fill.directCandidate)
    assert.deepEqual(path.paintSources.fill, svg.paintSources.color)
    assert.ok(observed([svg]).every(node => !Object.hasOwn(node, 'observationId')))
  }
})

test('browser capture retains CSS-altered SVG evidence despite unchanged canonical metadata', async () => {
  const original = observed((await captureExample(browser, source, withIcon)).roots).find(node => node.icon)
  const snapshot = structuredClone(source)
  snapshot.css += `
    svg[data-pk-icon] { fill: #13579b; opacity: .6; transform: translateX(3px); }
    svg[data-pk-icon] > path {
      fill: var(--pk-color-text-primary); fill-opacity: .7; fill-rule: evenodd;
      stroke: color-mix(in srgb, var(--pk-color-accent-on) 50%, #010203); stroke-opacity: .8;
      stroke-width: 2px; stroke-linecap: round; stroke-linejoin: bevel; stroke-miterlimit: 5;
      stroke-dasharray: 2px 3px; stroke-dashoffset: 1px; vector-effect: non-scaling-stroke;
      transform: scale(.5); transform-origin: 2px 3px; transform-box: fill-box;
      d: path("M0 0H20V20Z"); clip-path: inset(1px); filter: blur(1px); visibility: hidden;
    }`
  const result = await captureExample(browser, snapshot, withIcon)
  const svg = observed(result.roots).find(node => node.icon), path = svg.children[0]
  assert.equal(svg.icon.canonicalName, 'plus')
  assert.deepEqual(svg.icon, original.icon)
  assert.equal(svg.style.fill, 'rgb(19, 87, 155)')
  assert.deepEqual(svg.paintSources.fill, { tokens: [], directCandidate: null })
  assert.equal(svg.style.opacity, '0.6')
  assert.equal(svg.style.transform, 'matrix(1, 0, 0, 1, 3, 0)')
  assert.equal(path.paintSources.fill.directCandidate, '--pk-color-text-primary')
  assert.deepEqual(path.paintSources.stroke, { tokens: ['--pk-color-accent-on'], directCandidate: null })
  for (const [field, expected] of Object.entries({
    'fill-opacity': '0.7', 'fill-rule': 'evenodd', 'stroke-opacity': '0.8', 'stroke-width': '2px',
    'stroke-linecap': 'round', 'stroke-linejoin': 'bevel', 'stroke-miterlimit': '5',
    'stroke-dasharray': '2px, 3px', 'stroke-dashoffset': '1px', 'vector-effect': 'non-scaling-stroke',
    transform: 'matrix(0.5, 0, 0, 0.5, 0, 0)', 'transform-origin': '2px 3px', 'transform-box': 'fill-box',
    'clip-path': 'inset(1px)', filter: 'blur(1px)', visibility: 'hidden',
  })) assert.equal(path.style[field], expected, field)
  assert.match(path.style.d, /^path\(/)
  assert.notEqual(path.style.d, `path("${path.attributes.d}")`)
})

test('browser capture retains named slot boundaries without changing source layout or paints', async () => {
  const result = await captureExample(browser, source, withIcon, { fonts: faces })
  const button = result.roots[0], slot = button.children.find(node => node.kind === 'slot')
  assert.deepEqual(Object.keys(slot).toSorted(), ['children', 'kind', 'name'])
  assert.equal(slot.name, 'IconEnd')
  assert.equal(slot.children.length, 1)
  assert.equal(slot.children[0].icon.canonicalName, 'plus')
  assert.deepEqual(slot.children[0].paintSources.color, button.paintSources.color)
  assert.ok(button.children.find(node => node.property === 'label').fonts[0].isCustomFont)
  const snapshot = structuredClone(source)
  snapshot.examples.find(example => example.id === primary).html = `<button data-component="button">
    <!--pk-slot:Content--><!--pk-slot:IconStart--><!--/pk-slot:IconStart--><span>First</span>
    <!--pk-slot:Nested--><!--pk-text:unknown-->Second<!--/pk-text:unknown--><!--/pk-slot:Nested--><!--/pk-slot:Content--></button>`
  const capture = await captureExample(browser, snapshot, primary, { fonts: faces })
  const root = capture.roots[0], content = root.children.find(node => node.kind === 'slot')
  assert.equal(content.name, 'Content')
  assert.deepEqual(content.children[0], { kind: 'slot', name: 'IconStart', children: [] })
  assert.equal(content.children[1].tag, 'span')
  assert.equal(content.children[1].paintSources.color.directCandidate, '--pk-color-text-primary')
  const nested = content.children.find(node => node.kind === 'slot' && node.name === 'Nested')
  assert.equal(nested.children[0].text, 'Second')
  assert.equal(nested.children[0].property, 'unknown', 'balanced observations do not certify typed ownership')
  assert.ok(nested.children[0].fonts[0].isCustomFont)
  assert.deepEqual(root.bounds, (await originalLayout(snapshot, primary, 'light', viewport, faces)).bounds)
})

test('browser capture refuses malformed slot boundaries without leaking contexts', async () => {
  for (const content of [
    '<!--/pk-slot:IconStart-->', '<!--pk-slot:IconStart-->Save',
    '<!--pk-slot:IconStart--><!--/pk-slot:IconEnd-->',
    '<!--pk-slot:bad-name--><!--/pk-slot:bad-name-->', '<!--pk-slot:--><!--/pk-slot:-->',
    '<!--pk-slot:Content--><!--pk-slot:IconStart--><!--/pk-slot:Content--><!--/pk-slot:IconStart-->',
    '<!--pk-text:label--><!--pk-slot:Content-->Save<!--/pk-slot:Content--><!--/pk-text:label-->',
    '<!--pk-slot:Content--><!--pk-text:label-->Save<!--/pk-slot:Content--><!--/pk-text:label-->',
    '<!--pk-slot:Content--><span><!--/pk-slot:Content--></span>',
  ]) {
    const snapshot = structuredClone(source)
    snapshot.examples.find(example => example.id === primary).html = `<button>${content}</button>`
    await assert.rejects(captureExample(browser, snapshot, primary), /marker|text/i)
    assert.equal(browser.contexts().length, 0)
  }
})

test('browser capture refuses missing identities and invalid themes or viewports', async () => {
  await assert.rejects(captureExample(browser, source, 'missing'), /exactly one example/)
  await assert.rejects(captureExample(browser, source, primary, { mode: 'missing' }), /theme/)
  for (const viewport of [{ width: 0, height: 100 }, { width: 100 }, { width: 1.5, height: 100 }, { width: 100, height: 8193 }]) {
    await assert.rejects(captureExample(browser, source, primary, { viewport }), /viewport/)
  }
  const duplicate = structuredClone(source)
  duplicate.examples.push(duplicate.examples.find(example => example.id === primary))
  await assert.rejects(captureExample(browser, duplicate, primary), /exactly one example/)
  await assert.rejects(captureExample(browser, { ...source, sha256: '' }, primary), /identified/)
})

test('browser capture refuses malformed text regions without leaking their contexts', async () => {
  for (const content of [
    '<!--/pk-text:label-->Save', '<!--pk-text:label-->Save',
    '<!--pk-text:label-->Save<!--/pk-text:other-->',
    '<!--pk-text:bad-name-->Save<!--/pk-text:bad-name-->',
    '<!--pk-text:label--><span>Save</span><!--/pk-text:label-->',
    '<!--pk-text:label--><!--pk-text:label-->Save<!--/pk-text:label--><!--/pk-text:label-->',
  ]) {
    const snapshot = structuredClone(source)
    snapshot.examples.find(example => example.id === primary).html = `<button>${content}</button>`
    await assert.rejects(captureExample(browser, snapshot, primary), /marker|text/i)
    assert.equal(browser.contexts().length, 0)
  }
})

test('browser capture refuses executable content and external assets', async () => {
  for (const html of [
    '<script>throw new Error("executed")</script>',
    '<iframe src="https://capture.invalid/frame"></iframe>',
    '<link rel="stylesheet" href="https://capture.invalid/style.css">',
    '<img src="https://capture.invalid/image.png">',
  ]) {
    const snapshot = structuredClone(source)
    snapshot.examples.find(example => example.id === primary).html = html
    await assert.rejects(captureExample(browser, snapshot, primary), /executable|external|asset|resource/i)
  }
  const imported = { ...source, css: '@import url("https://capture.invalid/style.css");\n' + source.css }
  await assert.rejects(captureExample(browser, imported, primary), /external|resource|policy|CSP|asset/i)
})

test('browser capture proves supplied font use through CDP and records byte-free provenance', async () => {
  const snapshot = projection(primary, { label: 'João Ação Été Save 123 €' })
  const beforeFace = { ...face, bytes: Buffer.from(face.bytes) }
  const result = await captureExample(browser, snapshot, primary, { fonts: [face] })
  const button = result.roots[0]
  const label = button.children.find(node => node.property === 'label')
  assert.equal(button.style['font-weight'], '600')
  assert.equal(label.fonts.length, 1, 'known Latin text did not fall back to another face')
  assert.equal(label.fonts[0].isCustomFont, true)
  assert.equal(label.fonts[0].postScriptName, 'IBMPlexSans-SemiBold')
  assert.ok(label.fonts[0].glyphCount > 0)
  assert.deepEqual(result.fontFaces, [{
    family: face.family, weight: 600, style: 'normal', sha256: face.sha256,
    postscriptName: 'IBMPlexSans-SemiBold',
  }])
  assert.deepEqual(face, beforeFace)
})

test('browser capture attributes fonts to exact text regions rather than their descendants', async () => {
  const snapshot = structuredClone(source)
  snapshot.examples.find(example => example.id === primary).html =
    '<div style="font-family: IBM Plex Sans; font-weight: 400"><!--pk-text:label-->ABCD<!--/pk-text:label--><span style="font-weight: 600">XYZ</span></div>'
  const result = await captureExample(browser, snapshot, primary, { fonts: faces })
  const regions = observed(result.roots).filter(node => node.kind === 'text')
  assert.deepEqual(regions.map(node => node.text), ['ABCD', 'XYZ'])
  assert.deepEqual(regions.map(node => node.fonts?.map(({ postScriptName, isCustomFont, glyphCount }) => ({
    postScriptName, isCustomFont, glyphCount,
  }))), [
    [{ postScriptName: 'IBMPlexSans-Regular', isCustomFont: true, glyphCount: 4 }],
    [{ postScriptName: 'IBMPlexSans-SemiBold', isCustomFont: true, glyphCount: 3 }],
  ])
  assert.ok(observed(result.roots).filter(node => node.kind === 'element').every(node => !Object.hasOwn(node, 'fonts')))
  assert.ok(regions.every(node => !Object.hasOwn(node, 'fontObservationIds')))
})

test('browser capture aggregates one face across separate text nodes in a marked region', async () => {
  const snapshot = structuredClone(source)
  snapshot.examples.find(example => example.id === primary).html =
    '<div data-font-segments style="font-family: IBM Plex Sans; font-weight: 400"><!--pk-text:label-->ABCD<!--/pk-text:label--></div>'
  let segments
  // HTML parsing normally coalesces text. Split the fixture through the DOM API
  // without changing its markup, styling or the capture implementation.
  const segmentedBrowser = {
    async newContext(options) {
      const context = await browser.newContext(options)
      await context.exposeBinding('reportFontSegments', (_source, value) => { segments = value })
      await context.addInitScript(() => {
        const observer = new MutationObserver(() => {
          const parent = document.querySelector('[data-font-segments]')
          if (!parent) return
          observer.disconnect()
          const text = [...parent.childNodes].find(node => node.nodeType === Node.TEXT_NODE)
          text.splitText(2)
          void globalThis.reportFontSegments([...parent.childNodes]
            .filter(node => node.nodeType === Node.TEXT_NODE).map(node => node.data))
        })
        observer.observe(document, { childList: true, subtree: true })
      })
      return context
    },
  }
  const result = await captureExample(segmentedBrowser, snapshot, primary, { fonts: faces })
  assert.deepEqual(segments, ['AB', 'CD'], 'the fixture exercised two real TEXT nodes')
  const regions = observed(result.roots).filter(node => node.kind === 'text')
  assert.equal(regions.length, 1)
  assert.equal(regions[0].property, 'label')
  assert.equal(regions[0].text, 'ABCD')
  assert.deepEqual(regions[0].fonts?.map(({ postScriptName, isCustomFont, glyphCount }) => ({
    postScriptName, isCustomFont, glyphCount,
  })), [{ postScriptName: 'IBMPlexSans-Regular', isCustomFont: true, glyphCount: 4 }])
})

test('browser capture retains font evidence for a zero-advance combining glyph', async () => {
  const snapshot = structuredClone(source)
  snapshot.examples.find(example => example.id === primary).html =
    '<div style="font-family: IBM Plex Sans; font-weight: 400"><!--pk-text:label-->\u0301<!--/pk-text:label--></div>'
  const result = await captureExample(browser, snapshot, primary, { fonts: [faces[0]] })
  const regions = observed(result.roots).filter(node => node.kind === 'text')
  assert.equal(regions.length, 1)
  assert.equal(regions[0].text, '\u0301')
  assert.equal(regions[0].bounds.width, 0)
  assert.ok(regions[0].rects.some(rect => rect.width === 0 && rect.height > 0))
  assert.deepEqual(regions[0].fonts.map(({ postScriptName, isCustomFont, glyphCount }) => ({
    postScriptName, isCustomFont, glyphCount,
  })), [{ postScriptName: 'IBMPlexSans-Regular', isCustomFont: true, glyphCount: 1 }])
})

test('source reduced-motion fallback wins later consumer specificity without changing normal motion', async () => {
  const spinner = source.examples.find(example => example.id === 'pk-ui.component.spinner/brand').html
  const skeleton = source.examples.find(example => example.id === 'pk-ui.component.skeleton/block').html
  const consumer = `#consumer .motion, #consumer .motion::before, #consumer .motion::after {
    animation: pk-spin 3s linear infinite; transition: opacity 4s; scroll-behavior: smooth;
  }`
  for (const reducedMotion of ['reduce', 'no-preference']) {
    const context = await browser.newContext({ reducedMotion })
    try {
      const page = await context.newPage()
      await page.setContent(`<!doctype html><html><head><style>${source.css}</style><style>${consumer}</style></head>
        <body><section id="source">${spinner}${skeleton}</section><section id="consumer"><span class="motion">Consumer</span></section></body></html>`)
      const computed = await page.evaluate(() => {
        const values = style => [style.animationDuration, style.animationIterationCount, style.transitionDuration, style.scrollBehavior]
        const target = document.querySelector('#consumer .motion')
        return {
          reduced: matchMedia('(prefers-reduced-motion: reduce)').matches,
          source: [...document.querySelectorAll('#source .animate-spin, #source .animate-pulse')]
            .map(node => values(getComputedStyle(node)).slice(0, 2)),
          consumer: [null, '::before', '::after'].map(pseudo => values(getComputedStyle(target, pseudo))),
        }
      })
      const reduced = reducedMotion === 'reduce'
      assert.equal(computed.reduced, reduced)
      assert.deepEqual(computed.source, reduced ? [['1e-05s', '1'], ['1e-05s', '1']] : [['1s', 'infinite'], ['2s', 'infinite']])
      const expected = reduced ? ['1e-05s', '1', '1e-05s', 'auto'] : ['3s', 'infinite', '4s', 'smooth']
      assert.deepEqual(computed.consumer, [expected, expected, expected])
    } finally { await context.close() }
  }
})

test('source reduced-motion fallback preserves animation and transition completion events', async () => {
  const context = await browser.newContext({ reducedMotion: 'reduce' })
  try {
    const page = await context.newPage()
    await page.setContent(`<!doctype html><html><head><style>${source.css}</style><style>
      #events { animation: pk-spin 3s linear infinite; transition: opacity 4s; }
    </style></head><body></body></html>`)
    const events = await page.evaluate(async () => {
      const target = document.createElement('span')
      target.id = 'events'
      target.textContent = 'Completion event fixture'
      let timer
      const completed = Promise.all(['animationend', 'transitionend'].map(type => new Promise(resolve => {
        target.addEventListener(type, event => resolve({ type: event.type, target: event.target.id }), { once: true })
      })))
      document.body.append(target)
      try {
        await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))
        target.style.opacity = '0.5'
        return await Promise.race([
          completed,
          new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('Source completion events did not fire')), 1000) }),
        ])
      } finally { clearTimeout(timer) }
    })
    assert.deepEqual(events, [{ type: 'animationend', target: 'events' }, { type: 'transitionend', target: 'events' }])
  } finally { await context.close() }
})

test('browser capture settles all real loading indicators through source reduced-motion styles', async () => {
  const beforeSource = structuredClone(source)
  for (const suffix of [
    'button/loading', 'skeleton/block', 'skeleton/block-lg', 'skeleton/block-sm', 'skeleton/circle', 'skeleton/text',
    'spinner/brand', 'spinner/labelled', 'spinner/success', 'tableskeleton/table', 'tableskeleton/table-compact',
  ]) {
    const exampleId = `pk-ui.component.${suffix}`
    const result = await captureExample(browser, source, exampleId)
    assert.equal(result.exampleId, exampleId)
    const animated = observed(result.roots).filter(node => node.kind === 'element' && node.style['animation-name'] !== 'none')
    assert.ok(animated.length > 0, `${exampleId} retained its authored animation rather than capture removing it`)
    assert.ok(animated.every(node => node.style['animation-duration'] === '1e-05s'), exampleId)
    assert.equal(browser.contexts().length, 0)
  }
  assert.deepEqual(source, beforeSource)
})

test('browser capture still refuses explicit infinite or paused animations under reduced motion', async () => {
  for (const animation of ['pk-spin 1s linear infinite', 'pk-spin 1s linear 1 paused']) {
    const snapshot = structuredClone(source)
    snapshot.css += `\n[data-component="button"] { animation: ${animation} !important; }`
    const beforeSnapshot = structuredClone(snapshot)
    await assert.rejects(captureExample(browser, snapshot, primary), /requires finite, running source animations to settle/)
    assert.deepEqual(snapshot, beforeSnapshot)
    assert.equal(browser.contexts().length, 0)
  }
})
