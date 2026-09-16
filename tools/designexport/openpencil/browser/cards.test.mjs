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
import { parseColor } from '@open-pencil/core/color'

test('default linked cards retain private copy, shadows and page-grid geometry through edits and two saves', async t => {
  const source = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"; "github.com/septagon-oss/platformkit/ui/export"
  "github.com/septagon-oss/platformkit/ui/components"; "github.com/septagon-oss/platformkit/ui/components/examples"
  "github.com/septagon-oss/platformkit/ui/css"
)
func main() {
  var input struct { components.CardProps; ShadowCSS, BackgroundCSS string }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  props := input.CardProps
  cards := []g.Node{examples.ExampleWithSlots(examples.ExampleInfo{ID: "first", ComponentID: "pk-ui.component.card"},
    props, components.CardSlots{}, components.CardWithSlots).Node}
  cards = append(cards, examples.ExampleWithSlots(examples.ExampleInfo{ID: "second", ComponentID: "pk-ui.component.card"},
    components.CardProps{Title: "Coastal trail", Description: "Two stickers", Clickable: true, Href: "/packs/coast"},
    components.CardSlots{}, components.CardWithSlots).Node)
  grid := examples.ExampleWithChildren(examples.ExampleInfo{ID: "cards", ComponentID: "pk-ui.component.grid"},
    components.GridProps{Columns: "2", Gap: "4"}, cards, components.Grid)
  heading := examples.ExampleOf(examples.ExampleInfo{ID: "heading", ComponentID: "pk-ui.component.heading"},
    components.HeadingProps{Level: 1, Text: "Packs"}, components.Heading)
  type slots struct { Header, Content g.Node }
  example := examples.ExampleWithSlots(examples.ExampleInfo{ID: "fixture/cards", ComponentID: "fixture.page"},
    struct{}{}, slots{Header: heading.Node, Content: grid.Node}, func(_ struct{}, content slots) g.Node {
      return components.Stack(components.StackProps{Gap: "8"}, content.Header, content.Content)
    })
  theme := design.Default()
  theme.Light.Typography.Display, theme.Dark.Typography.Display = design.FontBody, design.FontBody
  var extra []ui.Extra
  if input.ShadowCSS != "" {
    sheet := css.NewSheet().Select("[data-component=card]", css.Decl("box-shadow", css.Literal(input.ShadowCSS)))
    extra = append(extra, ui.Extra{Sheets: []*css.Sheet{sheet}})
  }
  if input.BackgroundCSS != "" {
    sheet := css.NewSheet().Select("[data-component=card]", css.Decl("background-color", css.Literal(input.BackgroundCSS)))
    extra = append(extra, ui.Extra{Sheets: []*css.Sheet{sheet}})
  }
  snapshot, err := export.Export(theme, []examples.Example{example}, extra...)
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const id = 'fixture/cards', original = { title: 'Northern light', description: 'Two stickers', clickable: true, href: '/packs/north' }
  const fonts = suppliedFonts([400, 600]), previous = getTextMeasurer()
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  let renderer
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
  const placed = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const card = graph => descendants(graph, placed(graph)).find(node => origin(node)?.localId === 'first')
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
  function matches(graph, node, observed) {
    for (const field of ['width', 'height']) close(node[field], observed.bounds[field], `${node.name}/${field}`)
    const children = observed.children.filter(child => child.kind === 'element')
    assert.equal(graph.getChildren(node.id).filter(child => child.type !== 'TEXT').length, children.length)
    for (const [index, child] of children.entries()) {
      const native = graph.getChildren(node.id)[index]
      if (child.source) assert.equal(native.type, 'INSTANCE')
      for (const field of ['x', 'y']) close(native[field], child.bounds[field] - observed.bounds[field], `${native.name}/${field}`)
      matches(graph, native, child)
    }
  }
  async function pixelsAndSemantics(ck, graph, snapshot, expected, options) {
    const width = options.viewport.width, height = Math.ceil(expected.bounds.height) + 12
    const background = snapshot.themes.find(theme => theme.mode === options.mode).tokens.find(token => token.name === '--pk-color-surface-canvas').value
    const page = await browser.newPage({ viewport: { width, height }, colorScheme: options.mode })
    const surface = ck.MakeSurface(width, height), draw = new SkiaRenderer(ck, surface)
    let image
    try {
      const fontCSS = fonts.map(face => `@font-face { font-family: "${face.family}"; font-weight: ${face.weight}; src: url(data:font/woff;base64,${face.bytes.toString('base64')}); }`).join('')
      await page.setContent(`<html data-theme="${options.mode}"><head><style>${fontCSS}${snapshot.css} body { background: ${background}; }</style></head><body>${snapshot.examples[0].html}</body></html>`)
      await page.evaluate(async () => { await document.fonts.ready })
      assert.equal(await page.getByRole('heading', { level: 1, name: 'Packs', exact: true }).count(), 1)
      assert.equal(await page.getByRole('link').count(), 2)
      for (const href of ['/packs/north', '/packs/coast']) {
        await page.keyboard.press('Tab')
        assert.equal(await page.locator(':focus').getAttribute('href'), href)
        await page.locator(':focus').evaluate(async node => {
          getComputedStyle(node).boxShadow
          await new Promise(requestAnimationFrame)
        })
      }
      await page.locator(':focus').evaluate(node => node.blur())
      // Keyboard focus changes the actual source box-shadow. A loaded runner
      // can paint between the Tab calls; compare its settled default, not a
      // timing-dependent frame of the returning focus ring.
      await page.waitForFunction(() => {
        for (const node of document.querySelectorAll('[data-component=card]')) getComputedStyle(node).boxShadow
        return document.getAnimations().every(animation => ['finished', 'idle'].includes(animation.playState))
      }, undefined, { timeout: 5000 })
      image = ck.MakeImageFromEncoded(await page.screenshot())
      const format = { width, height, alphaType: ck.AlphaType.Unpremul, colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }
      const sourcePixels = image.readPixels(0, 0, format)
      const canvas = surface.getCanvas(), bg = parseColor(background)
      canvas.clear(ck.Color4f(bg.r, bg.g, bg.b, bg.a))
      await draw.loadFonts()
      draw.renderSceneToCanvas(canvas, graph, placed(graph).id)
      surface.flush()
      const nativePixels = canvas.readPixels(0, 0, format)
      const bounds = expected.children[1].children[0].bounds
      const points = []
      for (let y = Math.ceil(bounds.y + 24); y < bounds.y + bounds.height - 24; y++) {
        for (let x = Math.ceil(bounds.x + bounds.width - 6); x < bounds.x + bounds.width + 6; x++) points.push([x, y])
      }
      for (let y = Math.ceil(bounds.y + bounds.height); y < bounds.y + bounds.height + 6; y++) {
        for (let x = Math.ceil(bounds.x + 24); x < bounds.x + bounds.width - 24; x++) points.push([x, y])
      }
      assert.ok(points.length > 0)
      for (const [x, y] of points) for (let channel = 0; channel < 4; channel++) {
        const index = (y * width + x) * 4 + channel
        assert.ok(Math.abs(sourcePixels[index] - nativePixels[index]) <= 1,
          `shadow pixel ${options.mode}/${x}/${y}/${channel}: ${nativePixels[index]} versus ${sourcePixels[index]}`)
      }
    } finally { image?.delete(); draw.destroy(); await page.close() }
  }
  try {
    const ck = await initCanvasKit(); renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900))
    for (const [shadowCSS, reason] of [
      ['inset 0 1px 2px #000', /non-inset/],
      ['0 1px 2px #000, 0 1px 4px #000', /one non-inset/],
      ['0 1px 2px 3px #000', /zero-spread/],
      ['0 1px 2px var(--pk-color-text-primary)', /shadow color dependencies/],
    ]) {
      const candidate = source({ ...original, shadowCSS })
      const observation = await captureExample(browser, candidate, id, { fonts })
      const shadow = observation.roots[0].children[1].children[0]
      if (shadowCSS.includes('var(')) assert.deepEqual(shadow.paintSources['box-shadow'].tokens, ['--pk-color-text-primary'])
      const foundation = buildFoundation(candidate), page = foundation.graph.addPage('Refused shadow')
      foundation.graph.updateNode(page.id, { variableModes: { [foundation.collection.id]: foundation.collection.modes.find(item => item.name === 'light').modeId } })
      const before = structuredClone([...foundation.graph.getAllNodes()]), hook = getTextMeasurer()
      await assert.rejects(materializeComponent(foundation.graph, page.id, candidate, observation, fonts, renderer, foundation.collection.id), reason)
      assert.deepEqual([...foundation.graph.getAllNodes()], before)
      assert.equal(getTextMeasurer(), hook)
    }
    for (const backgroundCSS of ['', 'rgba(255,255,255,.5)', 'transparent'])
    for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const snapshot = source({ ...original, backgroundCSS })
      const options = { examples: [id], fonts, browser, renderer, mode, viewport: { width, height: 900 } }
      const built = await buildComponentDocument(snapshot, options)
      let { graph } = built
      assert.deepEqual(built.selections[0].components.map(item => item.path).toSorted(),
        [[id], [id, 'heading'], [id, 'cards'], [id, 'cards', 'first'], [id, 'cards', 'second']].toSorted())
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Untouched page' })
      const protectedText = descendants(graph, [...graph.getAllNodes()].find(node => node.name === 'Untouched page'))
        .filter(node => node.type === 'TEXT').map(node => node.text)
      const props = { ...original }
      for (const [name, value] of [['title', 'Northern light and the people we remember'],
        ['description', 'An album of memories. '.repeat(8).trim()]]) {
        props[name] = value
        const instance = card(graph), master = chain(graph, instance, 'componentId').at(-1)
        const definition = master.componentPropertyDefinitions.find(item => item.name === name)
        assert.ok(definition, `card owns its actual ${name} source text`)
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const before = structuredClone([...graph.getAllNodes()])
        editor.setInstanceComponentProperty(instance.id, definition.id, value)
        const after = structuredClone([...graph.getAllNodes()])
        for (const node of before.filter(node => chain(graph, graph.getNode(node.id), 'parentId')
          .some(parent => parent.type === 'COMPONENT' || parent.name === 'Untouched page'))) {
          assert.deepEqual(graph.getNode(node.id), node, `protected ${node.name}`)
        }
        editor.undoAction(); assert.deepEqual([...graph.getAllNodes()], before)
        editor.redoAction(); assert.deepEqual([...graph.getAllNodes()], after)
        const changed = source({ ...props, backgroundCSS }), expected = (await captureExample(browser, changed, id, options)).roots[0]
        for (let cycle = 0; cycle < 3; cycle++) {
          try { matches(graph, placed(graph), expected) } catch (error) {
            throw new Error(`${mode}, ${width}px, ${name}, save ${cycle}`, { cause: error })
          }
          const current = card(graph)
          const grid = graph.getNode(current.parentId)
          assert.equal(grid.primaryAxisSizing, 'HUG', 'grid height remains content-driven after reopening')
          assert.equal(grid.counterAxisSizing, 'FILL', 'grid width remains owned by its Stack placement')
          assert.equal(current.effects.length, 1)
          assert.ok(Math.abs(current.effects[0].color.a - 0.05) <= 1e-8, 'FIG float32 alpha retains the source shadow')
          assert.deepEqual(current.effects.map(effect => ({ ...effect, color: { ...effect.color, a: 0.05 } })), [{ type: 'DROP_SHADOW', color: { r: 0, g: 0, b: 0, a: 0.05 },
            offset: { x: 0, y: 1 }, radius: 2, spread: 0, visible: true, blendMode: 'NORMAL', showShadowBehindNode: false }])
          assert.deepEqual(extractSourceProps(graph, current, snapshot).proposal, {
            baseSHA256: snapshot.sha256, path: [id, 'cards', 'first'],
            props: Object.fromEntries(Object.entries(props).filter(([name, value]) => original[name] !== value)),
          })
          const sibling = [...graph.getAllNodes()].find(node => node.name === 'Untouched page')
          assert.deepEqual(descendants(graph, sibling).filter(node => node.type === 'TEXT').map(node => node.text), protectedText)
          if (cycle === 2 && name === 'description') await pixelsAndSemantics(ck, graph, changed, expected, options)
          if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
        }
      }
    }
    assert.equal(browser.contexts().length, 0)
  } finally { renderer?.destroy(); setTextMeasurer(previous); await browser.close() }
})
