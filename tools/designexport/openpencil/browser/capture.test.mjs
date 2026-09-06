import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { after, afterEach, before, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'
import { captureExample } from './capture.mjs'

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

// Independent DOM measurements use the original Go markup with only the two
// source annotations removed. No capture traversal or layout helper is reused.
async function originalLayout(snapshot, id, mode, size = viewport) {
  const example = snapshot.examples.find(example => example.id === id)
  const html = example.html.replaceAll('<!--pk-text:label-->', '').replaceAll('<!--/pk-text:label-->', '')
  const context = await browser.newContext({ viewport: size, colorScheme: mode, reducedMotion: 'reduce' })
  try {
    const page = await context.newPage()
    await page.setContent(`<!doctype html><html lang="en" data-theme="${mode}"><head><style>${snapshot.css}</style></head><body>${html}</body></html>`)
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
    assert.deepEqual(button.children.filter(node => node.icon).map(node => node.bounds), original.icons)
    const text = button.children.find(node => node.property === 'label')
    assert.equal(text.text, label)
    assert.equal(text.text, original.text)
    assert.deepEqual(text.rects, original.textRects)
  }
})

test('browser capture does not add flex gaps for empty labels or parse escaped text as markers', async () => {
  for (const text of ['', ' ', 'A & <b> <!--/pk-text:label-->']) {
    const snapshot = projection(withIcon, { label: text })
    const result = await captureExample(browser, snapshot, withIcon)
    const original = await originalLayout(snapshot, withIcon, 'light')
    const button = result.roots[0]
    assert.deepEqual(button.bounds, original.bounds)
    assert.deepEqual(button.children.filter(node => node.icon).map(node => node.bounds), original.icons)
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
