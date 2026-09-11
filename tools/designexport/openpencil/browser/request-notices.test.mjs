import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { buildComponentDocument, verifyComponentDocument } from '../document.mjs'
import { chain } from '../exporter-correction.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'
import { completePlexFonts, sourceFixture } from './fixtures.test.mjs'

async function requestNoticeSource(t) {
  return sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/page"
)
func main() {
  var input struct { Proposal *ui.PropsProposal }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  examples := page.RequestNoticeExamples("/account/sign-in")
  var snapshot ui.DesignExport
  var err error
  if input.Proposal == nil {
    snapshot, err = ui.Export(design.Default(), examples)
  } else {
    _, snapshot, err = ui.ProjectProps(design.Default(), examples, *input.Proposal)
  }
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
}

const require = createRequire(import.meta.url)

async function accessible(t, browser, fonts, snapshot, id, mode, width) {
  const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme: mode, reducedMotion: 'reduce' })
  try {
    const fontCSS = fonts.map(face => `@font-face { font-family: "${face.family}"; font-weight: ${face.weight}; src: url(data:font/woff;base64,${face.bytes.toString('base64')}); }`).join('')
    await page.setContent(`<html lang="en" data-theme="${mode}"><head><title>Request recovery</title><style>${fontCSS}${snapshot.css}</style></head><body><main>${snapshot.examples.find(example => example.id === id).html}</main></body></html>`)
    await page.evaluate(async () => { await document.fonts.ready })
    assert.equal(await page.getByRole('alert').count(), 1)
    assert.match(await page.locator('main').ariaSnapshot(), /alert:/)
    const links = page.getByRole('link'), expectsLink = ['pk-auth-anonymous', 'pk-auth-changed'].includes(id)
    assert.equal(await links.count(), expectsLink ? 1 : 0)
    if (expectsLink) {
      await page.keyboard.press('Tab')
      assert.equal(await page.getByRole('link', { name: 'Sign in (opens a new tab)', exact: true }).evaluate(node => node === document.activeElement), true)
      assert.equal(await links.getAttribute('href'), '/account/sign-in')
      assert.equal(await links.getAttribute('target'), '_blank')
      assert.equal(await links.getAttribute('rel'), 'noopener noreferrer')
      const focus = await links.evaluate(node => ({ outline: getComputedStyle(node).outlineStyle, shadow: getComputedStyle(node).boxShadow }))
      assert.ok(focus.outline !== 'none' || focus.shadow !== 'none', 'keyboard link retains a visible focus indicator')
    }
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'recovery content fits its viewport')
    await page.addScriptTag({ path: require.resolve('axe-core/axe.min.js') })
    const audit = await page.evaluate(async () => {
      const result = await axe.run(document.querySelector('main'))
      return { violations: result.violations.map(item => ({ id: item.id, targets: item.nodes.map(node => node.target) })),
        incomplete: result.incomplete.map(item => item.id) }
    })
    assert.deepEqual(audit.violations, [], `${id}/${mode}/${width} definite accessibility violations`)
    if (audit.incomplete.length) t.diagnostic(`${id}/${mode}/${width} manual accessibility checks: ${audit.incomplete.join(', ')}`)
  } finally { await page.close() }
}

test('source recovery notices retain alert semantics, keyboard navigation and responsive accessibility', async t => {
  const source = await requestNoticeSource(t), snapshot = source({}), fonts = completePlexFonts()
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  try {
    for (const mode of ['light', 'dark']) for (const width of [320, 1280]) for (const example of snapshot.examples) {
      await accessible(t, browser, fonts, snapshot, example.id, mode, width)
    }
  } finally { await browser.close() }
})

test('real recovery notices retain linked components and editable messages through two saves', async t => {
  const source = await requestNoticeSource(t)
  const snapshot = source({}), immutable = structuredClone(snapshot), ids = snapshot.examples.map(example => example.id)
  assert.deepEqual(ids, ['pk-auth-anonymous', 'pk-auth-changed', 'pk-auth-denied', 'pk-auth-uncertain'])
  const fonts = completePlexFonts()
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
  const placed = (graph, id) => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const message = (graph, id) => descendants(graph, placed(graph, id)).find(node => origin(node)?.localId === 'message')
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
  function matches(graph, root, observed) {
    for (const field of ['width', 'height']) close(root[field], observed.bounds[field], `${root.name}/${field}`)
    const elements = observed.children.filter(child => child.kind === 'element')
    const children = graph.getChildren(root.id).filter(child => child.type !== 'TEXT')
    if (elements.length && !children.length) {
      const runs = []
      const collect = parent => {
        for (const child of parent.children) {
          if (child.kind === 'text') runs.push({ region: child, style: parent.style })
          else {
            assert.equal(child.kind, 'element')
            assert.equal(child.style.display, 'inline')
            assert.equal(child.source, undefined, 'a source-owned component cannot disappear into private text')
            collect(child)
          }
        }
      }
      collect(observed)
      const texts = graph.getChildren(root.id)
      assert.equal(texts.length, runs.length)
      for (const [index, { region, style }] of runs.entries()) {
        const text = texts[index]
        assert.equal(text.type, 'TEXT'); assert.equal(text.text, region.text)
        close(text.width, region.bounds.width, 'inline run advance')
        close(text.height, Number.parseFloat(style['line-height']), 'inline line height')
        close(text.x, region.bounds.x - observed.bounds.x, 'inline run x')
        close(text.y, region.bounds.y - observed.bounds.y - (text.height - region.bounds.height) / 2, 'inline run y')
      }
      return
    }
    assert.equal(children.length, elements.length)
    for (const [index, child] of elements.entries()) {
      const node = children[index]
      if (child.source) assert.equal(node.type, 'INSTANCE')
      for (const field of ['x', 'y']) close(node[field], child.bounds[field] - observed.bounds[field], `${node.name}/${field}`)
      matches(graph, node, child)
    }
  }
  let renderer
  try {
    const ck = await initCanvasKit()
    renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
    for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const options = { examples: ids, fonts, browser, renderer, mode, viewport: { width, height: 900 } }
      const built = await buildComponentDocument(snapshot, options)
      const correspondence = verifyComponentDocument(built.graph, snapshot, ids)
      const baseline = await exportFigFile(built.graph)
      for (const id of ids) {
        let graph = await parseFigFile(baseline.slice().buffer, { populate: 'all' })
        verifyComponentDocument(graph, snapshot, ids, correspondence)
        const target = message(graph, id), master = chain(graph, target, 'componentId').at(-1)
        const definition = master.componentPropertyDefinitions.find(item => item.name === 'message')
        assert.ok(definition, 'recovery content retains the owning Alert property')
        const value = 'Keep this page open while checking the stored result. No request will be repeated automatically.'
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const before = structuredClone([...graph.getAllNodes()])
        const mutable = new Set(descendants(graph, placed(graph, id)).map(node => node.id))
        editor.setInstanceComponentProperty(target.id, definition.id, value)
        const after = structuredClone([...graph.getAllNodes()])
        for (const node of before.filter(node => !mutable.has(node.id))) {
          assert.deepEqual(graph.getNode(node.id), node, `unrelated master, sibling or board: ${node.name}`)
        }
        editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after)
        const proposal = { baseSHA256: snapshot.sha256, path: [id, 'message'], props: { message: value } }
        const candidate = source({ Proposal: proposal })
        const expected = (await captureExample(browser, candidate, id, options)).roots[0]
        for (let save = 0; save < 3; save++) {
          assert.deepEqual(extractSourceProps(graph, message(graph, id), snapshot).proposal, proposal)
          matches(graph, placed(graph, id), expected)
          if (save < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        }
      }
    }
    assert.deepEqual(snapshot, immutable)
    assert.deepEqual(source({}), immutable, 'presentation edits do not change runtime recovery')
  } finally {
    try { renderer?.destroy() } finally { await browser.close() }
  }
})
