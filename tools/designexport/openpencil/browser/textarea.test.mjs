import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { buildFoundation } from '../foundation.mjs'
import { materializeComponent } from '../components.mjs'
import { extractSourceProps } from '../source-changes.mjs'
import { chain } from '../exporter-correction.mjs'
import { captureExample } from './capture.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'

test('multiline description retains the real textarea value, wrapping and fixed rows across source edits and two saves', async t => {
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/kit/crud"
  "github.com/septagon-oss/platformkit/kit/httpx"
  "github.com/septagon-oss/platformkit/ui/export"
  "github.com/septagon-oss/platformkit/ui/components"; "github.com/septagon-oss/platformkit/ui/components/examples"
  "github.com/septagon-oss/platformkit/ui/screens"
)
func main() {
  var input struct { components.TextareaProps; Generated bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  children := []g.Node{examples.ExampleOf(examples.ExampleInfo{ID: "description", ComponentID: "pk-ui.component.textarea"}, input.TextareaProps, components.Textarea).Node,
    examples.ExampleOf(examples.ExampleInfo{ID: "note", ComponentID: "pk-ui.component.text"}, components.TextProps{Content: "Changes remain local."}, components.Text).Node,
    examples.ExampleWithSlots(examples.ExampleInfo{ID: "save", ComponentID: "pk-ui.component.button"}, components.ButtonProps{Label: "Save", Type: "submit"}, components.ButtonSlots{}, components.ButtonWithSlots).Node}
  example := examples.ExampleWithChildren(examples.ExampleInfo{ID: "fixture/textarea", ComponentID: "pk-ui.component.form"},
    components.FormProps{Label: "Album description", Action: "/albums"}, children, components.Form)
  if input.Generated {
    resource := httpx.Resource{Module: "notes", Entity: "note", Path: "/api/v1/notes", Schema: crud.Schema{Fields: []crud.Field{
      {Name: "description", Type: crud.TypeText, Widget: "textarea", Doc: input.HelperText},
    }}}
    example = screens.FormExample("fixture/textarea", resource, screens.Options{Root: "/admin"}, "/admin/notes", "Note description",
      map[string]any{"description": input.Value}, map[string]string{"description": input.ErrorMessage}, "", true)
  }
  snapshot, err := export.Export(design.Default(), []examples.Example{example})
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const id = 'fixture/textarea', original = { name: 'description', label: 'Description', rows: 5, fullWidth: true, helperText: 'What the collection is about.' }
  const fonts = suppliedFonts([400, 500, 600]), previous = getTextMeasurer()
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
  const root = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const field = graph => descendants(graph, root(graph)).find(node => ['description', 'field/description'].includes(origin(node)?.localId))
  const close = (actual, expected, name) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${name}: ${actual} versus ${expected}`)
  let renderer, ck
  function matches(graph, observed, value) {
    const placed = field(graph), area = graph.getChildren(placed.id)[1], viewport = graph.getChildren(area.id)[0], text = graph.getChildren(viewport.id)[0]
    const expectedField = observed.children[0], expected = expectedField.children.find(node => node.tag === 'textarea')
    assert.equal(expected.control.value, value, 'HTML parsing retains authored leading and trailing LF')
    assert.deepEqual(expected.children, [], 'native control value is not its unpainted light-DOM text')
    assert.equal(expected.control.type, 'textarea')
    for (const [node, bounds] of [[root(graph), observed.bounds], [placed, expectedField.bounds], [area, expected.bounds]]) {
      for (const dimension of ['width', 'height']) close(node[dimension], bounds[dimension], `${node.name}/${dimension}`)
    }
    assert.equal(viewport.clipsContent, true)
    assert.equal(viewport.height, 100, 'five real 20px rows do not grow with edited text')
    assert.equal(text.text, value)
    assert.equal(text.textAutoResize, 'HEIGHT')
    close(text.width, expected.control.content.bounds.width, 'native content width')
    close(text.height, expected.control.content.bounds.height, 'native multiline content height')
    if (value !== '') {
      if (/[^\n]/.test(value)) assert.equal(expected.control.fonts[0].postScriptName, 'IBMPlexSans-Regular')
      else assert.deepEqual(expected.control.fonts, [], 'blank lines have no painted glyphs')
      const paragraph = renderer.buildParagraph(text, undefined, { halfLeading: true })
      try {
        const lines = paragraph.getLineMetrics(), advances = expected.control.content.advances
        assert.equal(lines.length, advances.length)
        for (const [index, width] of advances.entries()) close(lines[index].width, width, `line ${index} without hanging spaces`)
      } finally { paragraph.delete() }
    } else assert.deepEqual(expected.control.fonts, [])
  }
  try {
    ck = await initCanvasKit(); renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1))
    for (const value of ['\n', '\nSaved description\n']) {
      const snapshot = source({ ...original, value })
      const built = await buildComponentDocument(snapshot, { examples: [id], fonts, browser, renderer })
      matches(built.graph, built.selections[0].observation.roots[0], value)
    }
    for (const generated of [false, true]) for (const mode of ['light', 'dark']) for (const width of [320, 1280]) for (const errorMessage of ['', 'Add more detail.']) {
      const input = { ...original, errorMessage, generated }, snapshot = source(input)
      const options = { examples: [id], fonts, browser, renderer, mode, viewport: { width, height: 900 } }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built
      matches(graph, built.selections[0].observation.roots[0], '')
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Untouched form' })
      const values = ['\nFirst line\nSecond line\n', 'People, places and small details. '.repeat(15).trim(), 'A  spaced & <description>.', '']
      for (const value of values) {
        const target = field(graph), master = chain(graph, target, 'componentId').at(-1)
        const property = master.componentPropertyDefinitions.find(item => item.name === 'value')
        assert.ok(property, 'the textarea owns its native value property')
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const before = structuredClone([...graph.getAllNodes()])
        const hook = getTextMeasurer()
        try {
          setTextMeasurer(() => null)
          if (value) assert.throws(() => editor.setInstanceComponentProperty(target.id, property.id, value), /native text measurement/i)
          assert.deepEqual([...graph.getAllNodes()], before)
        } finally { setTextMeasurer(hook) }
        editor.setInstanceComponentProperty(target.id, property.id, value)
        const after = structuredClone([...graph.getAllNodes()])
        for (const node of before.filter(node => chain(graph, graph.getNode(node.id), 'parentId')
          .some(parent => parent.type === 'COMPONENT' || parent.name === 'Untouched form'))) assert.deepEqual(graph.getNode(node.id), node)
        editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after)
        const expected = (await captureExample(browser, source({ ...input, value }), id, options)).roots[0]
        for (let cycle = 0; cycle < 3; cycle++) {
          try { matches(graph, expected, value) } catch (cause) { throw new Error(`${generated ? 'generated' : 'composed'}/${mode}/${width}/${JSON.stringify(value)}/save ${cycle}`, { cause }) }
          const proposal = extractSourceProps(graph, field(graph), snapshot)
          if (value === '') assert.equal(proposal.status, 'no-supported-changes')
          else assert.deepEqual(proposal.proposal, { baseSHA256: snapshot.sha256, path: [id, generated ? 'field/description' : 'description'], props: { value } })
          if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        }
      }
      const page = await browser.newPage({ viewport: options.viewport, colorScheme: mode })
      try {
        const fontCSS = fonts.map(face => `@font-face { font-family: "${face.family}"; font-weight: ${face.weight}; src: url(data:font/woff;base64,${face.bytes.toString('base64')}); }`).join('')
        const actual = source({ ...input, value: '\nA description\n' })
        await page.setContent(`<html data-theme="${mode}"><head><style>${fontCSS}${actual.css}</style></head><body>${actual.examples[0].html}</body></html>`)
        await page.evaluate(async () => { await document.fonts.ready })
        const area = page.getByRole('textbox', { name: 'Description', exact: true })
        assert.equal(await area.inputValue(), '\nA description\n')
        assert.equal(await area.getAttribute('aria-invalid'), errorMessage ? 'true' : null)
        assert.deepEqual(await area.evaluate(node => node.getAttribute('aria-describedby').split(' ').map(id => document.getElementById(id)?.textContent)),
          [...(errorMessage ? [errorMessage] : []), original.helperText])
        const cancel = generated ? [page.getByRole('link', { name: 'Cancel', exact: true })] : []
        for (const target of [area, ...cancel, page.getByRole('button', { name: 'Save', exact: true })]) {
          await page.keyboard.press('Tab')
          assert.equal(await target.evaluate(node => node === document.activeElement && getComputedStyle(node).outlineStyle !== 'none'), true)
        }
        // The field's own label must focus it. Which label that is is derived from the
        // address the form posts to, so follow the association instead of naming an id:
        // a literal would break on every namespace change and would still pass if some
        // unrelated label happened to carry that id.
        const label = page.locator(`label[for="${await area.getAttribute('id')}"]`)
        assert.equal(await label.count(), 1, 'exactly one label owns the field')
        await label.click()
        assert.equal(await area.evaluate(node => node === document.activeElement), true)
        await area.fill('First line'); await page.keyboard.press('End'); await page.keyboard.press('Enter'); await page.keyboard.type('Second line')
        assert.equal(await area.inputValue(), 'First line\nSecond line')
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
      } finally { await page.close() }
    }
    for (const props of [{ autoResize: true }, { maxLength: 200 }, { placeholder: 'Add a description' }, { value: 'Tab\tstop' }, { value: '\r\nLeading\r\n' }]) {
      const snapshot = source({ ...original, ...props }), observation = await captureExample(browser, snapshot, id, { fonts })
      if (props.value?.includes('\r')) assert.equal(observation.roots[0].children[0].children.find(node => node.control).control.value, '\nLeading\n',
        'HTML preserves the leading blank line but normalizes CRLF; source binding must reject that mismatch')
      const { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refused textarea')
      const before = structuredClone([...graph.getAllNodes()]), hook = getTextMeasurer()
      await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, fonts, renderer, collection.id))
      assert.deepEqual([...graph.getAllNodes()], before)
      assert.equal(getTextMeasurer(), hook)
    }
    assert.equal(browser.contexts().length, 0)
  } finally { renderer?.destroy(); setTextMeasurer(previous); await browser.close() }
})
