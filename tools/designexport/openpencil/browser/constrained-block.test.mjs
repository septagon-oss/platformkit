import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { buildComponentDocument } from '../document.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'
import { emptyStateFixture, suppliedFonts } from './fixtures.test.mjs'
import { chain } from '../exporter-correction.mjs'

test('real EmptyState keeps enlarged copy and its keyboard action usable at 320px', async t => {
  const run = await emptyStateFixture(t), browser = await chromium.launch({ headless: true })
  const fonts = suppliedFonts([400, 500, 600])
  const fontCSS = fonts.map(face => `@font-face { font-family: "${face.family}"; font-weight: ${face.weight}; src: url(data:font/woff;base64,${face.bytes.toString('base64')}); }`).join('')
  try {
    for (const compact of [false, true]) for (const mode of ['light', 'dark']) for (const scale of [1, 2]) {
      const snapshot = run({ title: 'This album is still being made', description: 'Gather the people and places we remember together. '.repeat(7).trim(), compact, action: true })
      const page = await browser.newPage({ viewport: { width: 320, height: 900 }, colorScheme: mode, reducedMotion: 'reduce' })
      try {
        await page.setContent(`<!doctype html><html lang="en" data-theme="${mode}"><head><meta name="viewport" content="width=device-width, initial-scale=1"><style>${fontCSS}${snapshot.css}</style></head><body>${snapshot.examples[0].html}</body></html>`)
        const sizes = await page.evaluate(async scale => {
          await document.fonts.ready
          const nodes = [...document.querySelectorAll('#empty > p, #empty button')]
          const original = nodes.map(node => parseFloat(getComputedStyle(node).fontSize))
          document.documentElement.style.fontSize = `${parseFloat(getComputedStyle(document.documentElement).fontSize) * scale}px`
          await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))
          return nodes.map((node, i) => [original[i], parseFloat(getComputedStyle(node).fontSize)])
        }, scale)
        assert.equal(sizes.length, 3)
        for (const [before, after] of sizes) assert.equal(after, before * scale, 'text is actually enlarged, not only a viewport simulation')
        const button = page.getByRole('button', { name: 'Create album', exact: true })
        assert.match(await button.ariaSnapshot(), /button "Create album"/)
        await button.evaluate(node => { node.dataset.activations = '0'; node.addEventListener('click', () => node.dataset.activations++) })
        const initialShadow = await button.evaluate(node => getComputedStyle(node).boxShadow)
        await page.keyboard.press('Tab')
        assert.ok(await button.evaluate(node => document.activeElement === node && node.matches(':focus-visible')))
        await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
        assert.notEqual(await button.evaluate(node => getComputedStyle(node).boxShadow), initialShadow)
        for (const key of ['Enter', 'Space']) await page.keyboard.press(key)
        assert.equal(await button.getAttribute('data-activations'), '2')
        assert.ok(await page.evaluate(() => {
          if (document.documentElement.scrollWidth > innerWidth) return false
          return [...document.querySelectorAll('#empty > p, #empty button')].every(node => {
            const box = node.getBoundingClientRect(), range = document.createRange()
            range.selectNodeContents(node)
            return node.scrollWidth <= node.clientWidth + 1 && node.scrollHeight <= node.clientHeight + 1 &&
              [...range.getClientRects()].every(rect => rect.left >= box.left - 1 && rect.right <= box.right + 1 &&
                rect.top >= box.top - 1 && rect.bottom <= box.bottom + 1)
          })
        }), 'enlarged text stays inside its growing boxes without horizontal scrolling')
        const box = await button.boundingBox()
        assert.ok(box.y >= 0 && box.y + box.height <= 900, 'keyboard focus brings the action into view')
      } finally { await page.close() }
    }
  } finally { await browser.close() }
})

test('real EmptyState retains intrinsic centered text, maximum width, linked actions and edits through two saves', async t => {
  const run = await emptyStateFixture(t)
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1)), previous = getTextMeasurer()
  const id = 'fixture/empty'
  function matches(graph, node, observed) {
    for (const field of ['width', 'height']) assert.ok(Math.abs(node[field] - observed.bounds[field]) <= 1 / 64,
      `${node.name}/${field}: ${node[field]} versus ${observed.bounds[field]}`)
    for (const [i, child] of observed.children.entries()) {
      const native = graph.getChildren(node.id)[i]
      if (child.kind === 'text') {
        assert.equal(native.type, 'TEXT'); assert.equal(native.text, child.text)
        const paragraph = renderer.buildParagraph(native, undefined, { halfLeading: true })
        try {
          const lines = paragraph.getLineMetrics()
          assert.equal(lines.length, child.rects.length, `${native.text}: native width ${native.width}, parent width ${node.width}, source width ${observed.bounds.width}`)
          for (const [index, line] of lines.entries()) {
            assert.ok(Math.abs(line.width - child.rects[index].width) <= 1 / 64)
            assert.ok(Math.abs(native.x + line.left - child.rects[index].x + observed.bounds.x) <= 1 / 64,
              `${native.text}/line ${index} horizontal alignment`)
          }
        } finally { paragraph.delete() }
      } else {
        for (const field of ['x', 'y']) assert.ok(Math.abs(native[field] - child.bounds[field] + observed.bounds[field]) <= 1 / 64,
          `${native.name}/${field}: ${native[field]} versus ${child.bounds[field] - observed.bounds[field]}`)
        matches(graph, native, child)
      }
    }
  }
  try {
    for (const compact of [false, true]) for (const action of [false, true]) for (const mode of ['light', 'dark']) {
      const input = { title: 'This album is still being made', description: 'There are no stickers in it yet.', compact, action }
      const snapshot = run(input), options = { examples: [id], fonts: suppliedFonts([400, 500, 600]), browser, renderer, mode, viewport: { width: 320, height: 900 } }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built, instance = built.selections[0].instance
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Untouched empty state' })
      for (let cycle = 0; cycle < 3; cycle++) {
        setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        for (const width of [1280, 390, 320]) {
          options.viewport.width = width
          graph.updateNode(instance.id, { width }); computeLayout(graph, instance.id)
          matches(graph, instance, (await captureExample(browser, snapshot, id, options)).roots[0])
          const description = graph.getChildren(instance.id)[1]
          assert.equal(description.maxWidth, 448)
          assert.equal(description.counterAxisSizing, 'HUG')
          if (action) assert.equal(graph.getChildren(instance.id)[2].type, 'INSTANCE')
          const property = built.selections[0].properties.find(item => item.name === 'description')
          const before = structuredClone([...graph.getAllNodes()])
          const value = 'Gather the people and places we remember together. '.repeat(7).trim()
          editor.setInstanceComponentProperty(instance.id, property.id, value)
          matches(graph, instance, (await captureExample(browser, run({ ...input, description: value }), id, options)).roots[0])
          for (const node of before.filter(node => chain(graph, graph.getNode(node.id), 'parentId').some(parent =>
            parent.type === 'COMPONENT' || parent.name === 'Untouched empty state'))) {
            assert.deepEqual(graph.getNode(node.id), node, 'masters and unrelated occurrence subtrees remain unchanged')
          }
          assert.deepEqual(extractSourceProps(graph, instance, snapshot).proposal,
            { baseSHA256: snapshot.sha256, path: [id], props: { description: value } })
          const after = structuredClone([...graph.getAllNodes()])
          editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
          editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after)
          editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
          assert.throws(() => editor.setInstanceComponentProperty(instance.id, property.id, ''), /Intrinsic source text requires actual measurement/)
          assert.deepEqual([...graph.getAllNodes()], before, 'removing a conditional paragraph is not an ordinary native text edit')
        }
        const title = 'An album of the people and places we remember together.'
        const before = structuredClone([...graph.getAllNodes()])
        editor.setInstanceComponentProperty(instance.id, built.selections[0].properties.find(item => item.name === 'title').id, title)
        matches(graph, instance, (await captureExample(browser, run({ ...input, title }), id, options)).roots[0])
        assert.deepEqual(extractSourceProps(graph, instance, snapshot).proposal,
          { baseSHA256: snapshot.sha256, path: [id], props: { title } })
        editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        if (cycle < 2) {
          graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
          instance = [...graph.getAllNodes()].find(node => node.name === id && node.type === 'INSTANCE')
        }
      }
    }
    const input = { title: 'This album is still being made', description: 'Gather memories together. '.repeat(20).trim() }
    for (const align of ['flex-start', 'flex-end', 'stretch']) for (const width of [320, 1280]) {
      const snapshot = run({ ...input, align }), options = { examples: [id], fonts: suppliedFonts([400, 600]), browser, renderer, viewport: { width, height: 900 } }
      const { graph, selections } = await buildComponentDocument(snapshot, options)
      matches(graph, selections[0].instance, (await captureExample(browser, snapshot, id, options)).roots[0])
    }
    for (const [constraint, value] of [['max-width', '50%'], ['max-width', 'fit-content'], ['min-width', '20px'], ['max-height', '100px'], ['box-sizing', 'content-box']]) {
      await assert.rejects(buildComponentDocument(run({ ...input, constraint, value }), {
        examples: [id], fonts: suppliedFonts([400, 600]), browser, renderer,
      }), /composition constrained sizing/, `${constraint}: ${value} must not lose its sizing semantics`)
    }
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
