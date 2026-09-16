import assert from 'node:assert/strict'
import { test } from 'node:test'
import { chromium } from 'playwright'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { computeLayout, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { buildComponentDocument } from '../document.mjs'
import { materializeComponent } from '../components.mjs'
import { buildFoundation } from '../foundation.mjs'
import { sourceFragment } from '../source-fragments.mjs'
import { ancestryOverrides, chain } from '../exporter-correction.mjs'
import { extractSourceProps, extractSourceReplacement } from '../source-changes.mjs'
import { sourceFixture, suppliedFonts } from './fixtures.test.mjs'
import { captureExample } from './capture.mjs'

test('real Go fragments retain source identities and actual parent layout through edits and two saves', async t => {
  const run = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
  "github.com/septagon-oss/platformkit/ui/export"
  c "github.com/septagon-oss/platformkit/ui/components"; "github.com/septagon-oss/platformkit/ui/components/examples"
  g "maragu.dev/gomponents"
  h "maragu.dev/gomponents/html"
)
func main() {
  var input struct { Layout, Label, Heading string; Nested, Empty, Private bool; Proposal *export.PropsProposal; Replacement *export.ReplacementProposal }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  info := func(id, component string) examples.ExampleInfo { return examples.ExampleInfo{ID:id, ComponentID:component} }
  button := func(id, label string) g.Node {
    return examples.ExampleOf(info(id, "pk-ui.component.button"), c.ButtonProps{Label:label}, c.Button).Node
  }
  group := func(id string, children ...g.Node) g.Node {
    return examples.ExampleWithChildren(info(id, "fixture.fragment"), c.TextProps{Content:input.Heading}, children,
      func(p c.TextProps, nodes ...g.Node) g.Node {
        if input.Private && id == "fragment" {
          heading := h.Div(h.Style("display:flex;flex-direction:column;margin-bottom:8px"),
            h.P(g.Raw("<!--pk-text:content-->"), g.Text(p.Content), g.Raw("<!--/pk-text:content-->")))
          return g.Group{heading, g.Group(nodes)}
        }
        return g.Group(nodes)
      }).Node
  }
  children := []g.Node{button("first", input.Label), button("second", "Keep editing")}
  if input.Empty { children = nil }
  if input.Nested { children = []g.Node{group("inner", children...)} }
  members := []g.Node{button("before", "Before"), group("fragment", children...), button("after", "After")}
  var example examples.Example
  switch input.Layout {
  case "grid":
    example = examples.ExampleWithChildren(info("fixture", "pk-ui.component.grid"), c.GridProps{Columns:"2", Gap:"8"}, members, c.Grid)
  case "wrap":
    example = examples.ExampleWithChildren(info("fixture", "pk-ui.component.flex"), c.FlexProps{Wrap:true, Gap:"8"}, members, c.Flex)
  default:
    example = examples.ExampleWithChildren(info("fixture", "pk-ui.component.stack"), c.StackProps{Gap:"8"}, members, c.Stack)
  }
  snapshot, err := export.Export(design.Default(), []examples.Example{example})
  if input.Proposal != nil { _, snapshot, err = export.ProjectProps(design.Default(), []examples.Example{example}, *input.Proposal) }
  if input.Replacement != nil { _, snapshot, err = export.ProjectReplacement(design.Default(), []examples.Example{example}, *input.Replacement) }
  if err != nil { panic(err) }
  if err := json.NewEncoder(os.Stdout).Encode(snapshot); err != nil { panic(err) }
}
`)
  const browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
  const ck = await initCanvasKit(), renderer = new SkiaRenderer(ck, ck.MakeSurface(1280, 900)), previous = getTextMeasurer()
  const fonts = suppliedFonts([400, 600]), original = 'Create album'
  const record = node => JSON.parse(node.pluginData.find(item => item.key === 'platformkit.source')?.value ?? 'null')
  const descendants = (graph, node) => [node, ...graph.getChildren(node.id).flatMap(child => descendants(graph, child))]
  const close = (actual, expected, label) => assert.ok(Math.abs(actual - expected) <= 1 / 64, `${label}: ${actual} versus ${expected}`)
  function compare(graph, root, observed) {
    const native = descendants(graph, root), actualRoot = graph.getAbsolutePosition(root.id)
    for (const field of ['width', 'height']) close(root[field], observed.bounds[field], `parent ${field}`)
    for (const expected of observed.children) {
      const id = expected.source.path.at(-1), matches = native.filter(node => record(node)?.localId === id)
      assert.equal(matches.length, 1, `one native source member ${id}`)
      const node = matches[0], point = graph.getAbsolutePosition(node.id)
      for (const field of ['width', 'height']) close(node[field], expected.bounds[field], `${id} ${field}`)
      for (const field of ['x', 'y']) close(point[field] - actualRoot[field], expected.bounds[field] - observed.bounds[field], `${id} ${field}`)
    }
  }
  try {
    for (const layout of ['stack', 'grid', 'wrap']) for (const nested of [false, true]) await t.test(`${layout}/nested=${nested}`, async () => {
      const config = { layout, nested, label: original }, snapshot = run(config), baseline = structuredClone(snapshot)
      const viewport = { width: 640, height: 900 }
      const built = await buildComponentDocument(snapshot, { examples: ['fixture'], fonts, browser, renderer, viewport })
      let { graph } = built
      const root = () => [...graph.getAllNodes()].find(node => record(node)?.path?.length === 1 && record(node).path[0] === 'fixture')
      graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Unchanged sibling', y: 600 })
      const actions = createEditor({ graph }); actions.setCanvasKit(ck, renderer)
      try {
        const first = () => descendants(graph, root()).find(node => record(node)?.localId === 'first')
        const property = () => chain(graph, first(), 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === 'label')
        for (const width of [320, 640]) {
          actions.updateNodeWithUndo(root().id, { width })
          setTextMeasurer((node, width) => renderer.measureTextNode(node, width))
          computeLayout(graph, root().id)
          for (const label of ['Go', 'Create a collection']) {
            const expected = await captureExample(browser, run({ ...config, label }), 'fixture', { fonts, viewport: { ...viewport, width } })
            const before = structuredClone([...graph.getAllNodes()])
            actions.setInstanceComponentProperty(first().id, property().id, label)
            compare(graph, root(), expected.roots[0])
            actions.undo.undo()
            assert.deepEqual([...graph.getAllNodes()], before, 'one property undo restores the whole graph')
            actions.undo.redo()
            compare(graph, root(), expected.roots[0])
            const proposal = extractSourceProps(graph, first(), snapshot)
            assert.equal(proposal.status, 'proposal', JSON.stringify(proposal))
            assert.equal(proposal.proposal.props.label, label)
            assert.deepEqual(proposal.proposal.path, ['fixture', 'fragment', ...(nested ? ['inner'] : []), 'first'])
            assert.deepEqual(run({ ...config, proposal: proposal.proposal }), run({ ...config, label }),
              'native child edit projects through its fragment path into the real Go source')
            for (let cycle = 0; cycle < 2; cycle++) {
              graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
              actions.replaceGraph(graph)
              compare(graph, root(), expected.roots[0])
              const owners = descendants(graph, root()).filter(node => sourceFragment(graph, node))
              assert.equal(owners.length, nested ? 2 : 1)
              assert.ok(owners.every(node => node.type === 'INSTANCE' && node.layoutMode === 'NONE'))
              if (layout !== 'wrap') {
                assert.equal(first().primaryAxisSizing, 'FILL', 'placed width remains contextual after import')
                assert.equal(Object.hasOwn(ancestryOverrides(graph, first()), `${first().id}:primaryAxisSizing`), false,
                  'inherited fill must not become an authored override')
              }
              const sibling = [...graph.getAllNodes()].find(node => node.name === 'Unchanged sibling')
              const siblingFirst = descendants(graph, sibling).find(node => record(node)?.localId === 'first')
              assert.equal(graph.getChildren(siblingFirst.id)[0].text, original)
              const definition = chain(graph, first(), 'componentId').at(-1)
              assert.equal(graph.getChildren(definition.id)[0].text, original, 'master remains unchanged')
            }
          }
        }
      } finally { actions.replaceGraph(buildFoundation(snapshot).graph) }
      assert.deepEqual(snapshot, baseline)
    })
    await t.test('private text and its margin stay inside the source fragment owner', async () => {
      const config = { layout: 'stack', label: original, heading: 'Your collection', private: true }
      const snapshot = run(config), viewport = { width: 320, height: 900 }
      const built = await buildComponentDocument(snapshot, { examples: ['fixture'], fonts, browser, renderer, viewport })
      let { graph } = built
      const root = () => [...graph.getAllNodes()].find(node => record(node)?.path?.[0] === 'fixture')
      const owner = () => descendants(graph, root()).find(node => record(node)?.localId === 'fragment')
      const actions = createEditor({ graph }); actions.setCanvasKit(ck, renderer)
      try {
        const heading = 'Your collection of people, photographs and stories shared together'
        const property = chain(graph, owner(), 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === 'content')
        actions.setInstanceComponentProperty(owner().id, property.id, heading)
        const expected = await captureExample(browser, run({ ...config, heading }), 'fixture', { fonts, viewport })
        const proposal = extractSourceProps(graph, owner(), snapshot).proposal
        assert.deepEqual(proposal,
          { baseSHA256: snapshot.sha256, path: ['fixture', 'fragment'], props: { content: heading } })
        assert.deepEqual(run({ ...config, proposal }), run({ ...config, heading }),
          'private text edit projects through the fragment owner into the real Go source')
        for (let cycle = 0; cycle < 2; cycle++) {
          graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
          actions.replaceGraph(graph)
          close(root().height, expected.roots[0].bounds.height, 'private heading grows real parent')
          const margin = graph.getChildren(owner().id)[0]
          assert.equal(margin.paddingBottom, 8, 'existing CSS margin contribution remains with its owner')
          const content = descendants(graph, margin).find(node => node.type === 'TEXT')
          assert.equal(content.text, heading)
          const actual = graph.getAbsolutePosition(content.id), origin = graph.getAbsolutePosition(root().id)
          const paragraph = expected.roots[0].children[1].children[0]
          close(actual.x - origin.x, paragraph.bounds.x - expected.roots[0].bounds.x, 'private paragraph x')
          close(actual.y - origin.y, paragraph.bounds.y - expected.roots[0].bounds.y, 'private paragraph y')
          assert.equal(chain(graph, owner(), 'componentId').at(-1).componentPropertyDefinitions[0].defaultValue, config.heading)
        }
      } finally { actions.replaceGraph(buildFoundation(snapshot).graph) }
    })
    for (const layout of ['stack', 'grid', 'wrap']) for (const whole of [false, true]) await t.test(`source replacement/${layout}/whole=${whole}`, async () => {
      const config = { layout, nested: true, label: original }, snapshot = run(config), viewport = { width: 320, height: 900 }
      const built = await buildComponentDocument(snapshot, { examples: ['fixture'], fonts, browser, renderer, viewport })
      let { graph } = built
      const root = () => [...graph.getAllNodes()].find(node => record(node)?.path?.[0] === 'fixture')
      const local = id => descendants(graph, root()).find(node => record(node)?.localId === id)
      const target = local(whole ? 'fragment' : 'first'), replacement = local(whole ? 'inner' : 'second')
      const path = whole ? ['fixture', 'fragment'] : ['fixture', 'fragment', 'inner', 'first']
      const replacementPath = ['fixture', 'fragment', 'inner', ...(whole ? [] : ['second'])]
      const proposal = { baseSHA256: snapshot.sha256, path, replacementPath }
      const projected = run({ ...config, replacement: proposal })
      const observed = await captureExample(browser, projected, 'fixture', { fonts, viewport })
      const id = target.id, origin = structuredClone(record(target))
      const masters = [...graph.getAllNodes()].filter(node => node.type === 'COMPONENT')
      const beforeMasters = structuredClone(masters.map(node => descendants(graph, node)))
      const sibling = graph.createInstance(built.selections[0].master.id, built.placements.id, { name: 'Replacement sibling', y: 600 })
      const beforeSibling = structuredClone(descendants(graph, sibling))
      const actions = createEditor({ graph }); actions.setCanvasKit(ck, renderer)
      try {
        graph.swapInstanceComponent(target.id, chain(graph, replacement, 'componentId').at(-1).id)
        graph.withLayoutMutations(() => graph.preserveSourceMetadataDuring(() => computeLayout(graph, root().id)))
        await Promise.resolve()
        assert.equal(target.id, id)
        assert.deepEqual(record(target), origin, 'replacement keeps destination source identity')
        assert.deepEqual(masters.map(node => descendants(graph, node)), beforeMasters)
        assert.deepEqual(descendants(graph, sibling), beforeSibling)
        for (let cycle = 0; cycle < 3; cycle++) {
          compare(graph, root(), observed.roots[0])
          const current = local(whole ? 'fragment' : 'first')
          assert.deepEqual(record(current), origin)
          assert.deepEqual(extractSourceReplacement(graph, current, snapshot).proposal, proposal)
          assert.deepEqual(run({ ...config, replacement: extractSourceReplacement(graph, current, snapshot).proposal }), projected)
          assert.equal(descendants(graph, root()).filter(node => sourceFragment(graph, node)).length, whole ? 1 : 2)
          const first = local('first'), property = chain(graph, first, 'componentId').at(-1).componentPropertyDefinitions.find(item => item.name === 'label')
          const before = structuredClone([...graph.getAllNodes()])
          actions.setInstanceComponentProperty(first.id, property.id, 'Edit after replacement')
          assert.equal(graph.getChildren(first.id)[0].text, 'Edit after replacement')
          actions.undo.undo()
          assert.deepEqual([...graph.getAllNodes()], before, 'text history restores the replacement without changing other instances')
          if (cycle < 2) {
            graph = await parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })
            actions.replaceGraph(graph)
          }
        }
      } finally { actions.replaceGraph(buildFoundation(snapshot).graph) }
    })
    await t.test('empty member refuses atomically instead of disappearing', async () => {
      const snapshot = run({ layout: 'stack', label: original, empty: true })
      const observation = await captureExample(browser, snapshot, 'fixture', { fonts })
      const { graph, collection } = buildFoundation(snapshot), page = graph.addPage('Refusal')
      const before = structuredClone([...graph.getAllNodes()])
      await assert.rejects(materializeComponent(graph, page.id, snapshot, observation, fonts, renderer, collection.id), /empty output needs an observed insertion context/)
      assert.deepEqual([...graph.getAllNodes()], before)
    })
  } finally { renderer.destroy(); setTextMeasurer(previous); await browser.close() }
})
