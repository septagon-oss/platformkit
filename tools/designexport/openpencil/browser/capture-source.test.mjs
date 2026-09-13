import assert from 'node:assert/strict'
import { after, afterEach, before, test } from 'node:test'
import { chromium } from 'playwright'
import { prepareCaptureSource, indexCaptureSources } from './capture-source.mjs'
import { captureExample } from './capture.mjs'
import { exportCore } from './fixtures.test.mjs'

const source = exportCore(), rootId = 'pk-ui.component.button/primary'
let browser
before(async () => { browser = await chromium.launch({ headless: true, args: ['--enable-automation'] }) })
after(async () => { await browser?.close() })
afterEach(() => assert.equal(browser.contexts().length, 0, 'every capture and independent browser document closed'))

function fixture(html, children = [], css = '') {
  const snapshot = structuredClone(source), example = snapshot.examples.find(item => item.id === rootId)
  example.html = html
  example.children = children
  snapshot.css = css
  return { snapshot, example }
}

function child(id, html, prefix, children = []) {
  return { description: { id, componentId: 'fixture.member', html, children }, slot: 'children',
    ...(prefix === undefined ? {} : { span: { start: Buffer.byteLength(prefix), end: Buffer.byteLength(prefix + html) } }) }
}

function occurrence(records, ...path) {
  const matches = records.filter(record => JSON.stringify(record.path) === JSON.stringify([rootId, ...path]))
  assert.equal(matches.length, 1, `one record for ${JSON.stringify(path)}`)
  return matches[0]
}

const address = ({ presentation, ...member }) => member
const presentation = (state, reason, inert = false) => ({ state, ...(reason ? { reason } : {}), inert })

// This document uses the same HTML parsing context, but does not reuse capture's
// DOM traversal, membership algorithm, layout observation or expected results.
async function indexed(example, { splitText = false, mutate } = {}) {
  const prepared = prepareCaptureSource(example), context = await browser.newContext()
  try {
    const page = await context.newPage()
    await page.setContent('<!doctype html><html><head></head><body></body></html>')
    await page.evaluate(({ html, splitText }) => {
      const template = document.createElement('template')
      template.innerHTML = html
      document.body.append(template.content)
      if (splitText) [...document.body.childNodes].find(node => node.nodeType === Node.TEXT_NODE).splitText(3)
    }, { html: mutate ? mutate(prepared.html) : prepared.html, splitText })
    const records = await page.evaluate(indexCaptureSources, { occurrences: prepared.occurrences, html: example.html })
    const restored = await page.evaluate(() => document.body.innerHTML)
    return { records, restored }
  } finally { await context.close() }
}

test('capture preparation retains unknown source descendants without inventing rendered boundaries', () => {
  const leaf = child('leaf', 'Leaf', '<span>', [])
  const unknown = child('unknown', '<span>Leaf</span>', undefined, [leaf])
  const { example } = fixture('<div></div>', [unknown, child('empty', '', '<div>')])
  const before = structuredClone(example), prepared = prepareCaptureSource(example)
  const identities = prepared.occurrences.map(item => [item.source.path, item.reason]).toSorted()
  assert.deepEqual(identities, [
    [[rootId], undefined], [[rootId, 'empty'], undefined],
    [[rootId, 'unknown'], 'unobserved-span'], [[rootId, 'unknown', 'leaf'], 'unobserved-ancestor'],
  ].toSorted())
  assert.equal((prepared.html.match(/<!--pk-capture:/g) ?? []).length, 2)
  assert.deepEqual(example, before)
})

test('capture distinguishes observed zero members from unknown source correspondence', async () => {
  const empty = fixture(''), emptyCapture = await captureExample(browser, empty.snapshot, rootId)
  assert.deepEqual(occurrence(emptyCapture.sourceOccurrences), {
    path: [rootId], componentId: empty.example.componentId, correspondence: 'observed', members: [],
  })
  assert.deepEqual(emptyCapture.roots, [])
  const { snapshot } = fixture('<div></div>', [
    child('empty', '', '<div>'), child('unknown', '', undefined, [child('leaf', '', '')]),
  ])
  const capture = await captureExample(browser, snapshot, rootId)
  assert.deepEqual(occurrence(capture.sourceOccurrences, 'empty').members, [])
  for (const [path, reason] of [[['unknown'], 'unobserved-span'], [['unknown', 'leaf'], 'unobserved-ancestor']]) {
    assert.deepEqual(occurrence(capture.sourceOccurrences, ...path), {
      path: [rootId, ...path], componentId: 'fixture.member', slot: 'children', correspondence: 'unresolved', reason,
    })
  }
  assert.equal(capture.roots[0].source.path[0], rootId)
  assert.deepEqual(capture.roots[0].domPath, [0])
  assert.deepEqual(capture.roots[0].children, [])
  for (const reverse of [false, true]) {
    const siblings = [child('full', '<span>X</span>', '<div>'), child('empty', '', '<div>')]
    const sameBoundary = fixture('<div><span>X</span></div>', reverse ? siblings.reverse() : siblings)
    const result = await captureExample(browser, sameBoundary.snapshot, rootId)
    assert.deepEqual(occurrence(result.sourceOccurrences, 'empty').members, [])
    assert.deepEqual(occurrence(result.sourceOccurrences, 'full').members.map(address), [
      { kind: 'element', domPath: [0, 0], tag: 'span' },
    ])
  }
})

test('capture addresses equal Unicode siblings by source bytes rather than declaration order', async () => {
  const same = '<span>Same</span>', prefix = '<div>João 🧩 '
  const first = child('first/🧩', same, prefix), second = child('second-->', same, prefix + same)
  const { snapshot } = fixture(prefix + same + same + '</div>', [second, first])
  const before = structuredClone(snapshot), capture = await captureExample(browser, snapshot, rootId)
  assert.deepEqual(occurrence(capture.sourceOccurrences, first.description.id).members.map(address), [
    { kind: 'element', domPath: [0, 1], tag: 'span' },
  ])
  assert.deepEqual(occurrence(capture.sourceOccurrences, second.description.id).members.map(address), [
    { kind: 'element', domPath: [0, 2], tag: 'span' },
  ])
  assert.deepEqual(capture.roots[0].children.slice(1).map(node => node.source.path.at(-1)), ['first/🧩', 'second-->'])
  assert.deepEqual(capture.roots[0].domPath, [0])
  assert.deepEqual(capture.roots[0].children.slice(1).map(node => node.domPath), [[0, 1], [0, 2]])
  assert.deepEqual(capture.roots[0].children[0].domRanges, [{ domPath: [0, 0], start: 0, end: 8 }])
  assert.deepEqual(capture.roots[0].children.slice(1).map(node => node.children[0].domRanges), [
    [{ domPath: [0, 1, 0], start: 0, end: 4 }], [{ domPath: [0, 2, 0], start: 0, end: 4 }],
  ])
  assert.deepEqual(snapshot, before)
})

test('capture retains exact fragment text and authored comments without assigning a native root', async () => {
  const { snapshot } = fixture('<span>One</span> <span>Two</span><!--authored-->')
  const capture = await captureExample(browser, snapshot, rootId), members = occurrence(capture.sourceOccurrences).members
  assert.deepEqual(members.map(address), [
    { kind: 'element', domPath: [0], tag: 'span' },
    { kind: 'text', domPath: [1], start: 0, end: 1 },
    { kind: 'element', domPath: [2], tag: 'span' },
    { kind: 'comment', domPath: [3] },
  ])
  assert.deepEqual(members[3].presentation, presentation('suppressed', 'comment'))
  assert.ok(capture.roots.every(node => !node.source))
  const comments = fixture('<!--only-->'), commentCapture = await captureExample(browser, comments.snapshot, rootId)
  assert.deepEqual(occurrence(commentCapture.sourceOccurrences).members, [
    { kind: 'comment', domPath: [0], presentation: presentation('suppressed', 'comment') },
  ])
  const single = fixture('<!--before--><div>One</div><!--after-->')
  const singleCapture = await captureExample(browser, single.snapshot, rootId)
  assert.deepEqual(singleCapture.roots[0].source.path, [rootId])
  assert.deepEqual(singleCapture.roots[0].domPath, [1], 'ignored comments still occupy real DOM child indices')
})

test('capture preserves nested UTF-16 text intervals after restoring one original text node', async () => {
  const nested = child('inner', 'BC', '🧩'), outer = child('outer', '🧩BCD', '<div>A', [nested])
  const { snapshot, example } = fixture('<div>A🧩BCDZ</div>', [child('last', 'Z', '<div>A🧩BCD'), outer])
  const capture = await captureExample(browser, snapshot, rootId)
  const slices = [['outer', 1, 6], ['last', 6, 7]]
  for (const [id, start, end] of slices) assert.deepEqual(occurrence(capture.sourceOccurrences, id).members.map(address), [
    { kind: 'text', domPath: [0, 0], start, end },
  ])
  assert.deepEqual(occurrence(capture.sourceOccurrences, 'outer', 'inner').members.map(address), [
    { kind: 'text', domPath: [0, 0], start: 3, end: 5 },
  ])
  assert.equal(capture.roots[0].children.length, 1)
  assert.equal(capture.roots[0].children[0].text, 'A🧩BCDZ')
  assert.deepEqual(capture.roots[0].children[0].domRanges, [{ domPath: [0, 0], start: 0, end: 7 }],
    'the observed text covers the complete DOM node while source occurrences retain narrower slices')
  assert.equal((await indexed(example)).restored, example.html)
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  try {
    const page = await context.newPage()
    await page.setContent(`<!doctype html><html><head></head><body>${example.html}</body></html>`)
    const original = await page.evaluate(() => {
      const box = document.body.firstElementChild.getBoundingClientRect()
      const range = document.createRange()
      range.selectNode(document.body.firstElementChild.firstChild)
      const text = range.getBoundingClientRect()
      const bounds = rect => ({ x: rect.x, y: rect.y, width: rect.width, height: rect.height })
      return { box: bounds(box), text: bounds(text) }
    })
    assert.deepEqual(capture.roots[0].bounds, original.box)
    assert.deepEqual(capture.roots[0].children[0].bounds, original.text)
  } finally { await context.close() }
})

test('capture coalesces only instrumented text slices and retains pre-existing DOM text splits', async () => {
  const example = fixture('samesame', [child('first', 'same', ''), child('second', 'same', 'same')]).example
  const { records, restored } = await indexed(example)
  assert.equal(restored, 'samesame')
  assert.deepEqual(occurrence(records).members.map(address), [{ kind: 'text', domPath: [0], start: 0, end: 8 }])
  assert.deepEqual(occurrence(records, 'first').members.map(address), [{ kind: 'text', domPath: [0], start: 0, end: 4 }])
  assert.deepEqual(occurrence(records, 'second').members.map(address), [{ kind: 'text', domPath: [0], start: 4, end: 8 }])
  const split = await indexed(fixture('abcdef').example, { splitText: true })
  assert.deepEqual(occurrence(split.records).members.map(address), [
    { kind: 'text', domPath: [0], start: 0, end: 3 }, { kind: 'text', domPath: [1], start: 0, end: 3 },
  ])
  assert.equal(split.restored, 'abcdef')
})

test('capture correspondence enters nested template content without exposing it as rendered children', async () => {
  const inner = '<span>Cold</span>', nested = `<template>${inner}</template>`
  const { snapshot, example } = fixture(`<template><!--keep-->${nested}</template>`, [
    child('nested', nested, '<template><!--keep-->', [child('leaf', inner, '<template>')]),
  ])
  const { records, restored } = await indexed(example)
  assert.equal(restored, example.html)
  assert.deepEqual(occurrence(records).members, [
    { kind: 'element', domPath: [0], tag: 'template', presentation: presentation('suppressed', 'display-none') },
  ])
  assert.deepEqual(occurrence(records, 'nested').members, [
    { kind: 'element', domPath: [0, 'content', 1], tag: 'template', presentation: presentation('suppressed', 'template-content') },
  ])
  assert.deepEqual(occurrence(records, 'nested', 'leaf').members, [
    { kind: 'element', domPath: [0, 'content', 1, 'content', 0], tag: 'span', presentation: presentation('suppressed', 'template-content') },
  ])
  const capture = await captureExample(browser, snapshot, rootId)
  assert.deepEqual(capture.roots[0].domPath, [0])
  assert.deepEqual(capture.roots[0].children, [])
  assert.deepEqual(capture.sourceOccurrences, records)
})

test('capture distinguishes an SVG element named template from HTML inert template content', async () => {
  const { snapshot } = fixture('<svg xmlns="http://www.w3.org/2000/svg"><template><rect width="20" height="20"></rect></template></svg>')
  const capture = await captureExample(browser, snapshot, rootId)
  assert.equal(capture.roots[0].children[0].tag, 'template')
  assert.equal(capture.roots[0].children[0].children[0].tag, 'rect')
  assert.deepEqual(occurrence(capture.sourceOccurrences).members.map(address), [
    { kind: 'element', domPath: [0], tag: 'svg' },
  ])
})

test('capture separates explicit visual suppression from HTML interaction inertness', async () => {
  for (const [style, reason] of [['display:none', 'display-none'], ['visibility:hidden', 'visibility-hidden'], ['opacity:0', 'opacity-zero']]) {
    const prefix = `<div style="${style}">`, inner = '<span>Child</span>'
    const { snapshot } = fixture(prefix + inner + '</div>', [child('leaf', inner, prefix)])
    const { sourceOccurrences } = await captureExample(browser, snapshot, rootId)
    for (const path of [[], ['leaf']]) assert.deepEqual(occurrence(sourceOccurrences, ...path).members[0].presentation,
      presentation('suppressed', reason), `${style}: ${path}`)
  }
  const inner = '<span>Visible</span>', prefix = '<div inert>'
  const { snapshot } = fixture(prefix + inner + '</div>', [child('leaf', inner, prefix)])
  const { sourceOccurrences } = await captureExample(browser, snapshot, rootId)
  for (const path of [[], ['leaf']]) assert.deepEqual(occurrence(sourceOccurrences, ...path).members[0].presentation,
    presentation('box-observed', undefined, true))
})

test('capture records HTML inertness rather than an ignored SVG attribute with the same name', async () => {
  const svg = '<svg inert tabindex="0" width="20" height="20"></svg>'
  const standalone = fixture(svg)
  const capture = await captureExample(browser, standalone.snapshot, rootId)
  assert.equal(occurrence(capture.sourceOccurrences).members[0].presentation.inert, false)
  const context = await browser.newContext()
  try {
    const page = await context.newPage()
    await page.setContent(svg)
    assert.equal(await page.evaluate(() => { const svg = document.querySelector('svg'); svg.focus(); return document.activeElement === svg }), true)
    const prefix = '<div inert>', { snapshot } = fixture(prefix + svg + '</div>', [child('svg', svg, prefix)])
    const inherited = await captureExample(browser, snapshot, rootId)
    assert.equal(occurrence(inherited.sourceOccurrences, 'svg').members[0].presentation.inert, true)
    await page.setContent(snapshot.examples.find(item => item.id === rootId).html)
    assert.equal(await page.evaluate(() => { const svg = document.querySelector('svg'); svg.focus(); return document.activeElement === svg }), false)
  } finally { await context.close() }
})

test('capture does not inherit visibility suppression across an explicit visible descendant', async () => {
  const prefix = '<div style="visibility:hidden">', inner = '<span style="visibility:visible">Visible</span>'
  const { snapshot } = fixture(prefix + inner + '</div>', [child('leaf', inner, prefix)])
  const { sourceOccurrences } = await captureExample(browser, snapshot, rootId)
  assert.deepEqual(occurrence(sourceOccurrences).members[0].presentation, presentation('suppressed', 'visibility-hidden'))
  assert.deepEqual(occurrence(sourceOccurrences, 'leaf').members[0].presentation, presentation('box-observed'))
})

test('capture retains box absence without treating display-contents or noscript descendants as invisible CSS', async () => {
  for (const prefix of ['<div style="display:contents">', '<noscript>']) {
    const inner = '<span>Child</span>', closing = prefix.startsWith('<div') ? '</div>' : '</noscript>'
    const { snapshot } = fixture(prefix + inner + closing, [child('leaf', inner, prefix)])
    const { sourceOccurrences } = await captureExample(browser, snapshot, rootId)
    assert.deepEqual(occurrence(sourceOccurrences).members[0].presentation, presentation('unresolved', 'no-client-box'))
    assert.deepEqual(occurrence(sourceOccurrences, 'leaf').members[0].presentation,
      prefix.startsWith('<div') ? presentation('box-observed') : presentation('unresolved', 'no-client-box'))
  }
})

test('capture qualifies content-visibility evidence even when text geometry exists', async () => {
  for (const [value, expected] of [['hidden', presentation('suppressed', 'content-visibility-hidden')],
    ['auto', presentation('unresolved', 'content-visibility-auto')]]) {
    const prefix = `<div style="width:80px;height:30px;content-visibility:${value}">`, inner = '<span>Child</span>'
    const { snapshot } = fixture(prefix + inner + '</div>', [child('leaf', inner, prefix, [child('text', 'Child', '<span>')])])
    const capture = await captureExample(browser, snapshot, rootId)
    assert.deepEqual(occurrence(capture.sourceOccurrences, 'leaf').members[0].presentation, expected)
    assert.deepEqual(occurrence(capture.sourceOccurrences, 'leaf', 'text').members[0].presentation, expected)
    if (value === 'hidden') {
      assert.deepEqual(occurrence(capture.sourceOccurrences).members[0].presentation, presentation('box-observed'))
      assert.ok(capture.roots[0].children[0].children[0].rects.some(rect => rect.width > 0 && rect.height > 0),
        'this fixture exposes positive text geometry without certifying painted text')
    }
  }
})

test('capture does not claim suppression from declarations that do not affect an ancestor box', async () => {
  for (const [style, expected] of [['display:contents;opacity:0', presentation('box-observed')],
    ['display:contents;content-visibility:hidden', presentation('unresolved', 'content-visibility-context')],
    ['display:inline;content-visibility:hidden', presentation('unresolved', 'content-visibility-context')]]) {
    const prefix = `<div style="${style}">`, inner = '<span>Visible</span>'
    const { snapshot } = fixture(prefix + inner + '</div>', [child('leaf', inner, prefix)])
    const capture = await captureExample(browser, snapshot, rootId)
    assert.ok(capture.roots[0].children[0].bounds.width > 0)
    assert.deepEqual(occurrence(capture.sourceOccurrences, 'leaf').members[0].presentation, expected, style)
  }
  const prefix = '<svg><g style="content-visibility:hidden">', inner = '<rect width="20" height="20" fill="red"></rect>'
  const { snapshot } = fixture(prefix + inner + '</g></svg>', [child('leaf', inner, prefix)])
  const capture = await captureExample(browser, snapshot, rootId)
  assert.deepEqual(occurrence(capture.sourceOccurrences, 'leaf').members[0].presentation,
    presentation('unresolved', 'content-visibility-context'))
})

test('capture retains an authored template box while its content remains inert', async () => {
  const prefix = '<template style="display:block;width:20px;height:20px;background:red">', inner = '<span>Cold</span>'
  const { snapshot } = fixture(prefix + inner + '</template>', [child('leaf', inner, prefix)])
  const capture = await captureExample(browser, snapshot, rootId)
  assert.equal(capture.roots[0].bounds.width, 20)
  assert.equal(capture.roots[0].bounds.height, 20)
  assert.deepEqual(occurrence(capture.sourceOccurrences).members[0].presentation, presentation('box-observed'))
  assert.deepEqual(occurrence(capture.sourceOccurrences, 'leaf').members[0].presentation, presentation('suppressed', 'template-content'))
})

test('capture refuses invalid byte spans, ambiguous roots and parsing changed by instrumentation', async () => {
  for (const mutate of [
    example => { example.children[0].span.start++ },
    example => { example.children[0].span.end = Infinity },
    example => { example.children.push(structuredClone(example.children[0])) },
    example => { example.children[0].description.html = 'Different' },
    example => { example.html += '<!--pk-capture:0-->' },
  ]) {
    const { example } = fixture('<div>🧩</div>', [child('leaf', '🧩', '<div>')])
    mutate(example)
    assert.throws(() => prepareCaptureSource(example), /source|span|marker|identity|utf/i)
  }
  const rootHTML = '<div>Same</div>'
  await assert.rejects(indexed(fixture(rootHTML, [child('transparent', rootHTML, '')]).example), /ambiguous/i)
  await assert.rejects(indexed(fixture('<div>&amp;</div>', [child('entity', '&', '<div>')]).example), /parsing/i)
  await assert.rejects(indexed(fixture('<p><span>Broken</span></p>', [
    child('moved', '<span>Broken</span></p>', '<p>'),
  ]).example), /boundaries|parsing/i)
  await assert.rejects(indexed(fixture('<div>Same</div>').example, {
    mutate: html => html.replace('<!--/pk-capture:0-->', ''),
  }), /missing|boundaries/i)
})

test('capture refuses table parsing contexts that can move output without moving its source boundaries', async () => {
  const contexts = [
    ['<table>', 'Visible', '</table>'],
    ['<table><tbody>', 'Visible', '</tbody></table>'],
    ['<table><thead>', 'Visible', '</thead></table>'],
    ['<table><tfoot>', 'Visible', '</tfoot></table>'],
    ['<table><tbody><tr>', 'Visible', '</tr></tbody></table>'],
    ['<table>', '<tbody><tr><td>Cell</td></tr></tbody>Tail', '</table>'],
  ]
  for (const [prefix, html, suffix] of contexts) {
    const { example } = fixture(prefix + html + suffix, [child('moved', html, prefix)])
    await assert.rejects(indexed(example), /table|parser.context|parsing|boundaries/i, prefix + html + suffix)
  }
})

test('capture retains exact Unicode text correspondence within an ordinary table cell', async () => {
  const prefix = '<table><tbody><tr><td>A', suffix = 'Z</td></tr></tbody></table>'
  const { example } = fixture(prefix + '🧩B' + suffix, [child('text', '🧩B', prefix)])
  const { records, restored } = await indexed(example)
  assert.equal(restored, example.html)
  assert.deepEqual(occurrence(records, 'text').members.map(address), [
    { kind: 'text', domPath: [0, 0, 0, 0, 0], start: 1, end: 4 },
  ])
})

test('capture keeps external-resource refusals even for explicitly suppressed content', async () => {
  for (const html of ['<div hidden><img src="https://capture.invalid/image.png"></div>',
    '<noscript><img src="https://capture.invalid/image.png"></noscript>', '<div hidden><script>1</script></div>',
    '<template><template><script>1</script></template></template>',
    '<template><iframe src="https://capture.invalid/frame"></iframe></template>',
    '<template><template><img src="https://capture.invalid/image.png"></template></template>']) {
    const { snapshot } = fixture(html)
    await assert.rejects(captureExample(browser, snapshot, rootId), /executable|external|asset|resource/i)
  }
})

test('capture retains inert data image correspondence without claiming image decoding', async () => {
  const image = '<img src="data:image/png;base64,invalid-image">'
  const { snapshot } = fixture(`<template>${image}</template>`, [child('image', image, '<template>')])
  const capture = await captureExample(browser, snapshot, rootId)
  assert.deepEqual(occurrence(capture.sourceOccurrences, 'image').members, [
    { kind: 'element', domPath: [0, 'content', 0], tag: 'img', presentation: presentation('suppressed', 'template-content') },
  ])
  assert.deepEqual(capture.roots[0].children, [])
})

function imageWitness(witness, originalHTML) {
  witness.requests = []
  return { async newContext(options) {
    const context = await browser.newContext(options), newPage = context.newPage.bind(context), close = context.close.bind(context)
    context.on('request', request => witness.requests.push(request.url()))
    context.newPage = async () => {
      const page = await newPage(), setContent = page.setContent.bind(page)
      page.setContent = async (...args) => {
        await setContent(...args)
        await page.evaluate(() => {
          globalThis.imageDecodeCalls = 0
          const decode = HTMLImageElement.prototype.decode
          HTMLImageElement.prototype.decode = function (...args) {
            globalThis.imageDecodeCalls++
            return decode.apply(this, args)
          }
        })
      }
      return page
    }
    context.close = async () => {
      try {
        const page = context.pages()[0]
        if (page) Object.assign(witness, await page.evaluate(html => {
          const original = document.createElement('template')
          original.innerHTML = html
          return { html: document.body.innerHTML, original: original.innerHTML, decodes: globalThis.imageDecodeCalls,
            currentSources: [...document.images].map(image => image.currentSrc) }
        }, originalHTML))
      } finally { await close() }
    }
    return context
  } }
}

test('capture retains absent-source image placeholders only with settled display-none evidence', async () => {
  for (const [prefix, image, suffix, alt] of [
    ['', '<img hidden>', '', null],
    ['', '<img style="display:none" alt="">', '', ''],
    ['<div hidden>', '<img alt="João &amp; 🧩">', '</div>', 'João & 🧩'],
    ['<picture style="display:none">', '<img alt="Preview">', '</picture>', 'Preview'],
  ]) {
    const { snapshot, example } = fixture(prefix + image + suffix, [child('image', image, prefix)])
    // A root consisting solely of its child would retain the pre-existing
    // ambiguous-root refusal. Root images need no duplicated source identity.
    if (!prefix) example.children = []
    const before = structuredClone(snapshot), witness = {}
    const capture = await captureExample(imageWitness(witness, example.html), snapshot, rootId)
    const observedImage = prefix ? capture.roots[0].children[0] : capture.roots[0]
    assert.deepEqual(observedImage.image, { state: 'absent-source', alt, suppression: 'display-none' })
    assert.deepEqual(observedImage.bounds, { x: 0, y: 0, width: 0, height: 0 })
    const record = occurrence(capture.sourceOccurrences, ...(prefix ? ['image'] : []))
    assert.deepEqual(record.members[0].presentation, presentation('suppressed', 'display-none'))
    assert.equal(witness.html, witness.original, 'capture restores the independently parsed original DOM markup')
    assert.deepEqual(witness.currentSources, [''])
    assert.deepEqual(witness.requests, [])
    assert.equal(witness.decodes, 0, 'no decoder was invoked for an absent source')
    assert.deepEqual(snapshot, before)
  }
})

test('capture absent-source image evidence agrees with an independent browser document', async () => {
  const { snapshot, example } = fixture('<section><img alt="Upload preview"></section>', [], 'section { display:none }')
  const context = await browser.newContext(), requests = []
  try {
    context.on('request', request => requests.push(request.url()))
    await context.route('**/*', route => route.abort())
    const page = await context.newPage()
    await page.setContent(`<!doctype html><html><head><style>${snapshot.css}</style></head><body>${example.html}</body></html>`)
    const independent = await page.evaluate(() => {
      const image = document.querySelector('img'), rect = image.getBoundingClientRect()
      return { src: image.getAttribute('src'), srcset: image.getAttribute('srcset'), currentSrc: image.currentSrc,
        alt: image.getAttribute('alt'), ancestorDisplay: getComputedStyle(image.parentElement).display,
        bounds: { x: rect.x, y: rect.y, width: rect.width, height: rect.height } }
    })
    const capture = await captureExample(browser, snapshot, rootId), image = capture.roots[0].children[0]
    assert.deepEqual(independent, { src: null, srcset: null, currentSrc: '', alt: 'Upload preview',
      ancestorDisplay: 'none', bounds: { x: 0, y: 0, width: 0, height: 0 } })
    assert.deepEqual(image.bounds, independent.bounds)
    assert.deepEqual(image.image, { state: 'absent-source', alt: independent.alt, suppression: 'display-none' })
    assert.deepEqual(requests, [])
  } finally { await context.close() }
})

test('capture refuses absent-source images without display-none and all explicit or alternative sources', async () => {
  for (const html of ['<img>', '<img hidden style="display:block">', '<img style="opacity:0">',
    '<img style="visibility:hidden">', '<img inert>', '<img hidden src="">', '<img hidden srcset="">',
    '<img hidden="until-found">', '<img style="width:0;height:0">', '<img hidden src="   ">',
    '<img hidden srcset="https://capture.invalid/alternate.png 1x">',
    '<picture hidden><source srcset="https://capture.invalid/alternate.png"><img></picture>',
    '<picture hidden><source media="not all"><img></picture>',
    '<img hidden src="https://capture.invalid/image.png">',
    '<template><img src=""></template>', '<template><img srcset=""></template>',
    '<template><picture><source srcset="https://capture.invalid/alternate.png"><img></picture></template>']) {
    const { snapshot } = fixture(html), before = structuredClone(snapshot)
    await assert.rejects(captureExample(browser, snapshot, rootId), /image|asset|resource|source/i, html)
    assert.deepEqual(snapshot, before)
  }
})

test('capture keeps nested-template absent-source images dormant without image readiness claims', async () => {
  const image = '<img alt="Dormant preview">', prefix = '<template><template>'
  const { snapshot, example } = fixture(prefix + image + '</template></template>', [child('image', image, prefix)])
  const before = structuredClone(snapshot), witness = {}
  const capture = await captureExample(imageWitness(witness, example.html), snapshot, rootId)
  assert.deepEqual(occurrence(capture.sourceOccurrences, 'image').members, [{ kind: 'element',
    domPath: [0, 'content', 0, 'content', 0], tag: 'img', presentation: presentation('suppressed', 'template-content') }])
  assert.deepEqual(capture.roots[0].children, [])
  assert.equal(witness.html, example.html)
  assert.deepEqual(witness.currentSources, [])
  assert.deepEqual(witness.requests, [])
  assert.equal(witness.decodes, 0)
  assert.deepEqual(snapshot, before)
})

test('capture still decodes attached data images and refuses undecodable hidden data images', async () => {
  const data = 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>')
  const { snapshot, example } = fixture(`<img src="${data}" alt="Supplied">`), witness = {}
  const capture = await captureExample(imageWitness(witness, example.html), snapshot, rootId)
  assert.equal(capture.roots[0].tag, 'img')
  assert.equal(capture.roots[0].image, undefined, 'supplied image data is not an absent-source placeholder')
  assert.equal(witness.decodes, 1)
  assert.deepEqual(witness.requests, [])
  const invalid = fixture('<img hidden src="data:image/png;base64,invalid-image">')
  await assert.rejects(captureExample(browser, invalid.snapshot, rootId), /decode|image|source/i)
})

test('capture refuses an absent-source image revealed by a finite source animation', async () => {
  const css = '@keyframes reveal { to { --preview-display:block } } ' +
    'div { --preview-display:none; animation:reveal 200ms step-end forwards } img { display:var(--preview-display) }'
  const { snapshot, example } = fixture('<div><img alt="Revealed preview"></div>', [], css)
  const context = await browser.newContext()
  try {
    const page = await context.newPage()
    await page.setContent('<!doctype html><html><head></head><body></body></html>')
    const states = await page.evaluate(async ({ css, html }) => {
      const sheet = document.createElement('style')
      sheet.textContent = css
      document.head.append(sheet)
      document.body.innerHTML = html
      const image = document.querySelector('img'), initial = getComputedStyle(image).display
      await Promise.all(document.getAnimations().map(animation => animation.finished))
      return { initial, settled: getComputedStyle(image).display }
    }, { css, html: example.html })
    assert.deepEqual(states, { initial: 'none', settled: 'block' })
  } finally { await context.close() }
  await assert.rejects(captureExample(browser, snapshot, rootId), /image|asset|source|display.none/i)
})

test('capture times out undecoded lazy image bytes without activating or dropping the image', { timeout: 5000 }, async t => {
  t.after(async () => { for (const context of browser.contexts()) await context.close() })
  const data = 'data:image/svg+xml,' + encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="2" height="3"><!--${crypto.randomUUID()}--></svg>`)
  const { snapshot, example } = fixture(`<img hidden loading="lazy" src="${data}" alt="Deferred">`)
  const before = structuredClone(snapshot), witness = {}
  await assert.rejects(captureExample(imageWitness(witness, example.html), snapshot, rootId), /image decoding timed out/)
  assert.equal(witness.decodes, 1, 'supplied but deferred bytes are not classified as absent')
  assert.deepEqual(witness.requests, [])
  assert.deepEqual(witness.currentSources, [''])
  // Failure precedes source-marker cleanup, so compare only the authored image.
  assert.ok(witness.html.includes(witness.original))
  assert.deepEqual(snapshot, before)
  assert.equal(browser.contexts().length, 0, 'capture closes its own context before test cleanup')
})

function atCaptureIndex({ before, after } = {}) {
  return { async newContext(options) {
    const context = await browser.newContext(options), newPage = context.newPage.bind(context)
    context.newPage = async () => {
      const page = await newPage(), evaluate = page.evaluate.bind(page)
      page.evaluate = async (action, argument) => {
        if (action === indexCaptureSources && before) await evaluate(before)
        const result = await evaluate(action, argument)
        if (action === indexCaptureSources && after) await evaluate(after)
        return result
      }
      return page
    }
    return context
  } }
}

test('capture addresses split marked text and elements through slots without inventing slot DOM nodes', async () => {
  const html = '<div><!--ignored--><!--pk-slot:Content--><span>Same</span><!--pk-text:label-->A🧩B' +
    '<!--/pk-text:label--><!--pk-text:empty--><!--/pk-text:empty--><!--/pk-slot:Content--><span>Same</span></div>'
  const { snapshot } = fixture(html)
  const splitBrowser = atCaptureIndex({ before: () => {
    [...document.querySelector('div').childNodes].find(node => node.nodeType === Node.TEXT_NODE).splitText(3)
  } })
  const capture = await captureExample(splitBrowser, snapshot, rootId), [slot, sibling] = capture.roots[0].children
  assert.equal(slot.kind, 'slot')
  assert.equal(Object.hasOwn(slot, 'domPath'), false)
  assert.deepEqual(slot.children[0].domPath, [0, 2])
  assert.deepEqual(sibling.domPath, [0, 10])
  assert.equal(slot.children[1].text, 'A🧩B')
  assert.deepEqual(slot.children[1].domRanges, [
    { domPath: [0, 4], start: 0, end: 3 }, { domPath: [0, 5], start: 0, end: 1 },
  ])
  assert.equal(slot.children[2].text, '')
  assert.deepEqual(slot.children[2].domRanges, [])
})

test('capture addresses text and choice controls by their authored host rather than browser internal text', async () => {
  const { snapshot } = fixture('<div><!--index--><input data-pk-value="value" value="Same">' +
    '<textarea data-pk-value="value">Same</textarea><select data-pk-value="value" data-pk-values="values" ' +
    'data-pk-options="options"><option>Same</option></select></div>')
  const capture = await captureExample(browser, snapshot, rootId)
  assert.deepEqual(capture.roots[0].children.map(node => node.tag), ['input', 'textarea', 'select'])
  for (const [index, node] of capture.roots[0].children.entries()) {
    assert.deepEqual(node.domPath, [0, index + 1])
    assert.deepEqual(node.control.domPath, [0, index + 1])
    assert.deepEqual(node.children, [])
    assert.equal(Object.hasOwn(node.control, 'domRanges'), false)
    assert.equal(Object.hasOwn(node.control.content ?? {}, 'domRanges'), false)
  }
})

test('capture refuses missing and forged observation DOM address metadata', async () => {
  for (const after of [
    () => { delete globalThis.__platformkitCaptureAddresses },
    () => { (globalThis.__platformkitCaptureAddresses ??= new WeakMap()).set(document.body.firstElementChild, [999]) },
    () => { (globalThis.__platformkitCaptureAddresses ??= new WeakMap()).set(document.body.firstElementChild, [1]) },
  ]) {
    const { snapshot } = fixture('<span>Same</span><span>Same</span>')
    await assert.rejects(captureExample(atCaptureIndex({ after }), snapshot, rootId), /observation.*DOM address/i)
  }
})

test('capture identifies fragment parsing without claiming scripting-enabled document correspondence', async () => {
  const fallback = '<span>Enable JavaScript</span>'
  const { snapshot } = fixture(`<noscript>${fallback}</noscript>`, [child('notice', fallback, '<noscript>')])
  const capture = await captureExample(browser, snapshot, rootId)
  assert.equal(capture.environment.htmlParsing, 'template-fragment')
  assert.equal(capture.environment.javaScriptEnabled, true)
  assert.deepEqual(occurrence(capture.sourceOccurrences, 'notice').members, [{
    kind: 'element', tag: 'span', domPath: [0, 0], presentation: presentation('unresolved', 'no-client-box'),
  }])
  assert.equal(capture.roots[0].children[0].tag, 'span')
  const context = await browser.newContext({ javaScriptEnabled: true })
  try {
    const page = await context.newPage()
    await page.setContent(`<!doctype html><html><body><noscript>${fallback}</noscript></body></html>`)
    const actual = await page.evaluate(() => [...document.querySelector('noscript').childNodes]
      .map(node => ({ type: node.nodeType, text: node.textContent })))
    assert.deepEqual(actual, [{ type: 3, text: fallback }])
  } finally { await context.close() }
})
