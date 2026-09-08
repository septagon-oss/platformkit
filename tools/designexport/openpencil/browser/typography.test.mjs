import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { buildFoundation } from '../foundation.mjs'
import { buildComponentDocument } from '../document.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { captureExample } from './capture.mjs'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'

test('configured typography and Heading edits agree across source themes, responsive layout and two saves', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'platformkit-typography-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const file = join(directory, 'main.go')
  await writeFile(file, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"
)
func main() {
  var input struct { Light, Dark design.Typography; Level int; Text string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  theme := design.Default()
  theme.Light.Typography, theme.Dark.Typography = input.Light, input.Dark
  examples := components.Gallery()
  if input.Level > 0 {
    examples = []components.Example{components.ExampleOf(components.ExampleInfo{
      ID: "fixture/heading", ComponentID: "pk-ui.component.heading",
    }, components.HeadingProps{Level: input.Level, Text: input.Text, Anchor: "album"}, components.Heading)}
  }
  snapshot, err := ui.Export(theme, examples)
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`, { flag: 'wx' })
  const configured = { light: { display: '"Customer Display", serif', body: '"Customer Body", sans-serif' },
    dark: { display: '"Alternate Display", serif', body: '"Alternate Body", sans-serif', mono: '"Customer Mono", monospace' } }
  const source = input => JSON.parse(execFileSync('go', ['run', file], { cwd: new URL('../../../../', import.meta.url),
    encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, input: JSON.stringify(input) }))
  const snapshot = source(configured), previous = getTextMeasurer()
  let renderer
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  try {
    for (const preference of ['light', 'dark']) for (const attribute of [null, 'light', 'dark']) {
      const page = await browser.newPage({ colorScheme: preference })
      try {
        await page.setContent(`<html${attribute ? ` data-theme="${attribute}"` : ''}><head><style>${snapshot.css}</style></head><body></body></html>`)
        const tokens = snapshot.themes.find(theme => theme.mode === (attribute ?? preference)).tokens.filter(token => token.type === 'fontFamily')
        for (const token of tokens) assert.equal(await page.evaluate(name =>
          getComputedStyle(document.documentElement).getPropertyValue(name).trim(), token.name), token.value)
      } finally { await page.close() }
    }
    let { graph } = buildFoundation(snapshot)
    for (let cycle = 0; cycle < 3; cycle++) {
      const collection = [...graph.variableCollections.values()].find(item => item.name === 'Foundation')
      for (const theme of snapshot.themes) {
        const mode = collection.modes.find(item => item.name === theme.mode)
        for (const role of ['display', 'body', 'mono']) {
          const variable = graph.getVariablesForCollection(collection.id).find(item => item.name === `--pk-font-${role}`)
          assert.equal(variable.type, 'STRING')
          const expected = configured[theme.mode][role] ?? '"IBM Plex Mono", "SFMono-Regular", Consolas, monospace'
          assert.equal(variable.valuesByMode[mode.modeId], expected, `${theme.mode}/${role}/save ${cycle}`)
        }
      }
      if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
    }
    const bytes = readFileSync(new URL('../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-600-normal.woff', import.meta.url))
    const fonts = [{ family: 'IBM Plex Sans', weight: 600, style: 'normal', bytes, sha256: createHash('sha256').update(bytes).digest('hex') }]
    const ck = await initCanvasKit(); renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900))
    const id = 'fixture/heading', typography = { display: '"IBM Plex Sans", sans-serif' }
    for (const level of [1, 2, 3, 4, 5]) for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const input = { light: typography, dark: typography, level,
        text: level === 1 ? 'An album of people and places. '.repeat(4).trim() : 'Album & memories' }, snapshot = source(input)
      const options = { examples: [id], fonts, browser, renderer, mode, viewport: { width, height: 900 } }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built, instance = built.selections[0].instance
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Untouched heading' })
      const binding = built.selections[0].properties.find(item => item.name === 'text')
      assert.ok(binding, 'Heading exposes its actual source text, not a guessed layer name')
      const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
      const before = structuredClone([...graph.getAllNodes()]), value = 'Remember the people and places. '.repeat(6).trim()
      editor.setInstanceComponentProperty(instance.id, binding.id, value)
      const after = structuredClone([...graph.getAllNodes()])
      editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
      editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after)
      const changed = source({ ...input, text: value }), observed = (await captureExample(browser, changed, id, options)).roots[0]
      for (let cycle = 0; cycle < 3; cycle++) {
        const native = graph.getChildren(instance.id)[0], master = graph.getNode(instance.componentId)
        assert.equal(native.fontFamily, 'IBM Plex Sans'); assert.equal(native.fontWeight, 600)
        assert.equal(native.text, value)
        for (const field of ['width', 'height']) assert.ok(Math.abs(instance[field] - observed.bounds[field]) <= 1 / 64, `${level}/${mode}/${width}/${field}`)
        assert.equal(native.height / native.lineHeight, observed.children[0].rects.length)
        assert.equal(graph.getChildren(master.id)[0].text, input.text)
        const sibling = [...graph.getAllNodes()].find(node => node.name === 'Untouched heading')
        assert.equal(graph.getChildren(sibling.id)[0].text, input.text)
        assert.deepEqual(extractSourceProps(graph, instance, snapshot).proposal,
          { baseSHA256: snapshot.sha256, path: [id], props: { text: value } })
        if (cycle < 2) {
          graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
          instance = [...graph.getAllNodes()].find(node => node.name === id && node.type === 'INSTANCE')
        }
      }
      const page = await browser.newPage()
      try {
        await page.setContent(changed.examples[0].html)
        assert.equal(await page.getByRole('heading', { level, name: value, exact: true }).getAttribute('id'), 'album')
      } finally { await page.close() }
    }
    const unsupported = source({ light: typography, dark: typography, level: 6, text: 'Eyebrow' })
    await assert.rejects(buildComponentDocument(unsupported, { examples: [id], fonts, browser, renderer }), /text transformations/)
    assert.equal(browser.contexts().length, 0)
  } finally { renderer?.destroy(); setTextMeasurer(previous); await browser.close() }
})
