import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { buildComponentDocument, verifyComponentDocument } from '../document.mjs'
import { chain } from '../exporter-correction.mjs'
import { extractSourceProps, extractSourceReplacement } from '../source-changes.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'

test('derived source components remain linked without inventing editable slot ownership through two saves', async t => {
  const run = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "strconv"
  g "maragu.dev/gomponents"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui"
  "github.com/septagon-oss/platformkit/ui/components"; "github.com/septagon-oss/platformkit/ui/components/examples"
)
type Props struct { Number int \`json:"number"\`; Nested bool \`json:"nested"\` }
func owner(p Props, children ...g.Node) g.Node {
  number := examples.ExampleOf(examples.ExampleInfo{ID: "number", ComponentID: "pk-ui.component.text"},
    components.TextProps{Content: strconv.Itoa(p.Number), Color: "muted"}, components.Text)
  if p.Nested {
    number = examples.ExampleWithChildren(examples.ExampleInfo{ID: "summary", ComponentID: "pk-ui.component.stack"},
      components.StackProps{Gap: "4"}, []g.Node{number.Node}, components.Stack)
  }
  return components.Stack(components.StackProps{Gap: "4"}, append([]g.Node{number.Node}, children...)...)
}
func main() {
  var input struct { Proposal *ui.PropsProposal; Nested bool }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  action := examples.ExampleOf(examples.ExampleInfo{ID: "action", ComponentID: "pk-ui.component.button"},
    components.ButtonProps{Label: "Continue"}, components.Button)
  caption := examples.ExampleOf(examples.ExampleInfo{ID: "caption", ComponentID: "pk-ui.component.text"},
    components.TextProps{Content: "Album notes"}, components.Text)
  example := examples.ExampleWithChildren(examples.ExampleInfo{ID: "fixture/derived", ComponentID: "fixture.component.derived"},
    Props{Number: 7, Nested: input.Nested}, []g.Node{action.Node, caption.Node}, owner)
  captures := []examples.Example{example}
  snapshot, err := ui.Export(design.Default(), captures)
  if input.Proposal != nil { _, snapshot, err = ui.ProjectProps(design.Default(), captures, *input.Proposal) }
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const id = 'fixture/derived', fonts = suppliedFonts([400, 500, 600])
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1, 1)), previous = getTextMeasurer()
  const origin = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const root = graph => [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === id)
  const child = (graph, localId) => [...graph.getAllNodes()].find(node => origin(node)?.localId === localId &&
    chain(graph, node, 'parentId').includes(root(graph)))
  const property = (graph, instance, name) => chain(graph, instance, 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === name).id
  try {
    for (const nested of [false, true]) for (const mode of ['light', 'dark']) for (const width of [320, 1280]) {
      const snapshot = run({ nested }), internalId = nested ? 'summary' : 'number'
      const sourceInternal = snapshot.examples[0].children.find(child => child.description.id === internalId)
      assert.equal(sourceInternal.slot, undefined, 'an internal invocation is observed, not a declared slot')
      assert.ok(sourceInternal.span)
      const options = { examples: [id], fonts, browser, renderer, mode, viewport: { width, height: 900 } }
      await assert.rejects(buildComponentDocument(snapshot, { ...options, variants: [{ exampleId: id,
        path: nested ? [id, 'summary', 'number'] : [id, 'number'], property: 'color', snapshot,
      }] }), /derived component/, 'a private invocation cannot be independently projected into a native family')
      const projected = run({ nested, proposal: { baseSHA256: snapshot.sha256, path: [id, 'caption'], props: { color: 'brand' } } })
      let { graph } = await buildComponentDocument(snapshot, { ...options, variants: [{ exampleId: id,
        path: [id, 'caption'], property: 'color', snapshot: projected,
      }] })
      const correspondence = verifyComponentDocument(graph, snapshot, [id])
      for (let cycle = 0; cycle < 3; cycle++) {
        const derived = child(graph, 'number'), action = child(graph, 'action')
        const master = chain(graph, derived, 'componentId').at(-1)
        assert.equal(derived.type, 'INSTANCE')
        assert.equal(origin(master).componentId, 'pk-ui.component.text')
        assert.deepEqual(origin(derived), { localId: 'number', slot: nested ? 'children' : '' })
        assert.equal(origin(child(graph, internalId)).slot, '')
        assert.equal(graph.getChildren(derived.id)[0].text, '7')
        assert.deepEqual(extractSourceProps(graph, derived, snapshot).properties, [], 'derived inputs do not advertise independent source edits')
        assert.equal(extractSourceReplacement(graph, derived, snapshot).status, 'unsupported')
        const editor = createEditor({ graph }); editor.setCanvasKit(ck, renderer)
        const before = structuredClone([...graph.getAllNodes()])
        const unchanged = () => {
          const nodes = [...graph.getAllNodes()]
          assert.equal(nodes.length, before.length)
          for (const [index, node] of nodes.entries()) assert.deepEqual(node, before[index], `${node.id}: ${node.name}`)
        }
        editor.setInstanceComponentProperty(action.id, property(graph, action, 'label'), 'Continue to the next album')
        const result = extractSourceProps(graph, action, snapshot)
        assert.equal(result.status, 'proposal')
        assert.deepEqual(result.proposal.path, [id, 'action'])
        const candidate = run({ nested, proposal: result.proposal })
        assert.deepEqual(candidate.examples[0].children.find(child => child.description.id === internalId).description, sourceInternal.description)
        assert.deepEqual(graph.getNode(master.id), before.find(node => node.id === master.id), 'the derived master is unchanged')
        editor.undoAction()
        unchanged()
        editor.redoAction(); editor.undoAction()
        const caption = child(graph, 'caption'), captionMaster = chain(graph, caption, 'componentId').at(-1).id
        const family = graph.getNode(graph.getNode(captionMaster).parentId)
        editor.setInstanceComponentProperty(caption.id, family.componentPropertyDefinitions.find(item => item.name === 'color').id, 'brand')
        assert.deepEqual(extractSourceProps(graph, caption, snapshot).proposal.props, { color: 'brand' })
        assert.equal(graph.getChildren(derived.id)[0].text, '7')
        assert.equal(caption.counterAxisSizing, 'FILL', 'a variant retains the parent-owned stretch placement')
        editor.undoAction()
        unchanged()
        editor.setInstanceComponentProperty(derived.id, property(graph, derived, 'content'), 'A forged number')
        assert.equal(extractSourceProps(graph, action, snapshot).status, 'unsupported', 'an unrelated edit cannot conceal changed derived input')
        assert.equal(extractSourceProps(graph, derived, snapshot).status, 'unsupported')
        assert.throws(() => verifyComponentDocument(graph, snapshot, [id]), /derived/i)
        editor.undoAction()
        unchanged()
        graph.swapInstanceComponent(caption.id, master.id)
        assert.equal(extractSourceReplacement(graph, caption, snapshot).status, 'unsupported', 'private definition paths cannot become replacement sources')
        graph.swapInstanceComponent(caption.id, captionMaster)
        verifyComponentDocument(graph, snapshot, [id], correspondence)
        if (cycle < 2) graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
      }
    }
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
