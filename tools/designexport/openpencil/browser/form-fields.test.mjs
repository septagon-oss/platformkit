import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { chain } from '../exporter-correction.mjs'
import { captureExample } from './capture.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'

test('required fields retain label and value ownership inside a real Form through edits and two saves', async t => {
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"; "github.com/septagon-oss/platformkit/ui/components/examples"
)
func main() {
  var input components.InputProps
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  fields := []g.Node{examples.ExampleOf(examples.ExampleInfo{ID: "title", ComponentID: "pk-ui.component.input"}, input, components.Input).Node}
  fields = append(fields, examples.ExampleOf(examples.ExampleInfo{ID: "slug", ComponentID: "pk-ui.component.input"},
    components.InputProps{Name: "slug", Label: "Slug", Value: "field-notes", Required: true, FullWidth: true}, components.Input).Node)
  fields = append(fields, examples.ExampleWithSlots(examples.ExampleInfo{ID: "save", ComponentID: "pk-ui.component.button"},
    components.ButtonProps{Label: "Save", Type: "submit"}, components.ButtonSlots{}, components.ButtonWithSlots).Node)
  example := examples.ExampleWithChildren(examples.ExampleInfo{ID: "fixture/form-fields", ComponentID: "pk-ui.component.form"},
    components.FormProps{Label: "Album details", Action: "/albums"}, fields, components.Form)
  snapshot, err := ui.Export(design.Default(), []examples.Example{example})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const id = 'fixture/form-fields'
  const original = { name: 'title', label: 'Title', value: 'Field notes', required: true, fullWidth: true,
    helpText: 'Name your album.' }
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const fonts = suppliedFonts([400, 500, 600]), previous = getTextMeasurer()
  let renderer
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
  const root = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const field = (graph, name) => descendants(graph, root(graph)).find(node => origin(node)?.localId === name)
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64,
    `${label}: ${actual} versus ${expected}`)
  function matches(graph, observed, node = root(graph)) {
    for (const dimension of ['width', 'height']) close(node[dimension], observed.bounds[dimension], `${node.name}/${dimension}`)
    const elements = observed.children.filter(child => child.kind === 'element')
    const children = graph.getChildren(node.id)
    if (observed.tag === 'label') {
      const regions = observed.children.flatMap(child => child.kind === 'text' ? [child] : child.children)
      assert.equal(children.length, regions.length)
      for (const [index, region] of regions.entries()) {
        const text = children[index]
        assert.equal(text.text, region.text)
        close(text.x, region.bounds.x - observed.bounds.x, `${graph.getNode(node.parentId).name}/${node.name}/${text.text}/text x`)
        close(text.width, region.bounds.width, `${node.name}/text width`)
      }
      return
    }
    if (observed.control) {
      const viewport = children[0], value = graph.getChildren(viewport.id)[0]
      assert.equal(viewport.clipsContent, true)
      assert.equal(value.textAutoResize, 'WIDTH_AND_HEIGHT')
      assert.equal(value.text, observed.control.value)
      close(value.height, Number.parseFloat(observed.style['line-height']), 'input remains one line')
      return
    }
    if (observed.children.length === 1 && observed.children[0].kind === 'text') {
      assert.equal(children[0].text, observed.children[0].text)
    }
    for (const [index, element] of elements.entries()) {
      const child = children[index]
      if (element.source) assert.equal(child.type, 'INSTANCE')
      for (const axis of ['x', 'y']) close(child[axis], element.bounds[axis] - observed.bounds[axis], `${child.name}/${axis}`)
      matches(graph, element, child)
    }
  }
  async function semantics(snapshot, props, options) {
    const page = await browser.newPage({ viewport: options.viewport, colorScheme: options.mode })
    try {
      const fontCSS = fonts.map(face => `@font-face { font-family: "${face.family}"; font-weight: ${face.weight}; src: url(data:font/woff;base64,${face.bytes.toString('base64')}); }`).join('')
      await page.setContent(`<html data-theme="${options.mode}"><head><style>${fontCSS}${snapshot.css}</style></head><body>${snapshot.examples[0].html}</body></html>`)
      await page.evaluate(async () => { await document.fonts.ready })
      assert.equal(await page.getByRole('form', { name: 'Album details', exact: true }).count(), 1)
      const title = page.getByRole('textbox', { name: props.label, exact: true })
      assert.equal(await title.inputValue(), props.value)
      assert.equal(await title.evaluate(node => node.required), true)
      assert.equal(await title.getAttribute('aria-invalid'), props.error ? 'true' : null)
      const described = await title.evaluate(node => node.getAttribute('aria-describedby').split(' ').map(id => document.getElementById(id)?.textContent))
      assert.deepEqual(described, [...(props.error ? [props.error] : []), props.helpText])
      assert.equal(await page.getByRole('alert').count(), props.error ? 1 : 0)
      assert.equal(await page.locator('label [aria-hidden="true"]').count(), 2)
      for (const expected of [title, page.getByRole('textbox', { name: 'Slug', exact: true }), page.getByRole('button', { name: 'Save', exact: true })]) {
        await page.keyboard.press('Tab')
        assert.equal(await expected.evaluate(node => node === document.activeElement), true)
        assert.equal(await expected.evaluate(node => {
          const style = getComputedStyle(node)
          return style.outlineStyle !== 'none' && Number.parseFloat(style.outlineWidth) > 0
        }), true, 'every keyboard stop retains a visible source focus outline')
      }
      await page.locator('label[for="pk-input-title"]').click()
      assert.equal(await title.evaluate(node => node === document.activeElement), true)
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    } finally { await page.close() }
  }
  try {
    const ck = await initCanvasKit(); renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
    for (const error of ['', 'This title is already used.']) for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const input = { ...original, error }, snapshot = source(input)
      const options = { examples: [id], fonts, browser, renderer, mode, viewport: { width, height: 900 } }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built
      const unchanged = graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Untouched form' })
      const protectedText = descendants(graph, unchanged).filter(node => node.type === 'TEXT').map(node => node.text)
      const changed = { ...input }
      for (const [name, value] of [['label', 'Album title'], ['value', 'Memories of people and places. '.repeat(10).trim()], ['label', 'Name']]) {
        changed[name] = value
        const target = field(graph, 'title'), master = chain(graph, target, 'componentId').at(-1)
        const property = master.componentPropertyDefinitions.find(item => item.name === name)
        assert.ok(property)
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const before = structuredClone([...graph.getAllNodes()])
        editor.setInstanceComponentProperty(target.id, property.id, value)
        const after = structuredClone([...graph.getAllNodes()])
        for (const node of before.filter(node => chain(graph, graph.getNode(node.id), 'parentId')
          .some(parent => parent.type === 'COMPONENT' || parent.name === 'Untouched form'))) {
          assert.deepEqual(graph.getNode(node.id), node, `protected ${node.name}`)
        }
        editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after)
        const projected = source(changed)
        const expected = (await captureExample(browser, projected, id, options)).roots[0]
        for (let cycle = 0; cycle < 3; cycle++) {
          try { matches(graph, expected) } catch (cause) { throw new Error(`${mode}/${width}/${name}=${value}/save ${cycle}`, { cause }) }
          assert.deepEqual(extractSourceProps(graph, field(graph, 'title'), snapshot).proposal, {
            baseSHA256: snapshot.sha256, path: [id, 'title'],
            props: Object.fromEntries(Object.entries(changed).filter(([key, value]) => input[key] !== value)),
          })
          const sibling = [...graph.getAllNodes()].find(node => node.name === 'Untouched form')
          assert.deepEqual(descendants(graph, sibling).filter(node => node.type === 'TEXT').map(node => node.text), protectedText)
          if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        }
      }
      await semantics(source(changed), changed, options)
    }
    assert.equal(browser.contexts().length, 0)
  } finally { renderer?.destroy(); setTextMeasurer(previous); await browser.close() }
})
