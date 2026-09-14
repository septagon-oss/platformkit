import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdtemp, readFile, readdir, rm, symlink, writeFile, lstat } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { parseFigFile } from '@open-pencil/core/io/formats/fig'
import { extractSourceProps } from '../source-changes.mjs'
import { exportCore } from './fixtures.test.mjs'

const cli = fileURLToPath(new URL('../generate.mjs', import.meta.url))
const source = exportCore()
const form = 'pk-ui.component.form/default', button = 'pk-ui.component.button/with-leading-icon'
const fontPath = weight => fileURLToPath(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
const fontArgs = weights => weights.flatMap(weight => ['--font', 'IBM Plex Sans', String(weight), 'normal', fontPath(weight)])
const run = (directory, args, input) => spawnSync(process.execPath, [cli, ...args], {
  cwd: directory, encoding: 'utf8', input, timeout: 120_000, maxBuffer: 4 * 1024 * 1024,
})
function refused(result, reason) {
  assert.equal(result.error, undefined)
  assert.equal(result.signal, null)
  assert.equal(result.status, 1, result.stderr)
  assert.match(result.stderr, reason)
  assert.equal(result.stdout, '', 'refusal does not report a published document')
}

async function fixture(t) {
  const directory = await mkdtemp(join(tmpdir(), 'platformkit-library-cli-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  return directory
}

function origin(node) {
  const entries = node.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
  assert.ok(entries.length <= 1, 'native provenance is not duplicated')
  return entries.length ? JSON.parse(entries[0].value) : null
}

function masterOf(graph, node) {
  const seen = new Set()
  while (node?.type === 'INSTANCE') {
    assert.ok(!seen.has(node.id), 'native source lineage must not cycle')
    seen.add(node.id)
    node = graph.getNode(node.componentId)
  }
  assert.equal(node?.type, 'COMPONENT', 'every placed occurrence links to a real native master')
  return node
}

test('CLI assembles supplied variant projections without replacing ordinary source selection', async t => {
  const directory = await fixture(t), id = 'pk-ui.component.button/primary', output = join(directory, 'family.fig')
  const args = [output, '--example', id, '--example', form, ...fontArgs([400, 500, 600])]
  for (const tone of ['info', 'danger']) {
    const snapshot = exportCore(['--proposal'], { baseSHA256: source.sha256, path: [id], props: { tone } })
    const path = join(directory, `${tone}.json`)
    await writeFile(path, JSON.stringify(snapshot), { flag: 'wx' })
    args.push('--variant', id, 'tone', path)
  }
  const result = run(directory, args)
  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stdout, /Variants: 2 caller-supplied projections; source freshness is not verified/)
  const graph = await parseFigFile(Uint8Array.from(await readFile(output)).buffer, { populate: 'all' })
  const roots = [...graph.getAllNodes()].filter(node => node.type === 'INSTANCE' && origin(node)?.path)
  assert.equal(roots.length, 2)
  const instance = roots.find(node => origin(node).path[0] === id), master = masterOf(graph, instance), family = graph.getNode(master.parentId)
  assert.equal(family.type, 'COMPONENT_SET')
  assert.deepEqual(graph.getChildren(family.id).map(node => node.componentPropertyValues.tone), ['neutral', 'info', 'danger'])
  assert.deepEqual(family.componentPropertyDefinitions.map(item => item.type), ['TEXT', 'VARIANT'])
  assert.ok(graph.getChildren(family.id).every(node => node.componentPropertyDefinitions.length === 0))
  for (const root of roots) assert.equal(extractSourceProps(graph, root, source).status, 'no-supported-changes')
  const invalid = join(directory, 'invalid.json'), saved = await readFile(output)
  await writeFile(invalid, '{invalid json', { flag: 'wx' })
  const files = await readdir(directory)
  for (const [extra, reason] of [
    [['--variant', id, 'tone', invalid], /Expected one UTF-8 JSON design-export snapshot/],
    [['--variant', id, 'tone', join(directory, 'missing.json')], /ENOENT.*missing\.json/],
    [['--variant', id, 'tone', join(directory, 'info.json')], /duplicate source variant value/],
    [['--variant', id, 'label', join(directory, 'info.json')], /families currently support one source property/],
    [['--variant', 'unselected', 'tone', join(directory, 'info.json')], /variants require a selected exampleId/],
  ]) {
    refused(run(directory, [join(directory, 'refused.fig'), ...args.slice(1), ...extra]), reason)
    assert.deepEqual(await readdir(directory), files)
    assert.deepEqual(await readFile(output), saved)
  }
})

test('CLI addresses a nested family by exact source path and refuses malformed paths without publishing', async t => {
  const directory = await fixture(t), path = [form, 'actions', 'create'], output = join(directory, 'nested.fig')
  const projected = exportCore(['--proposal'], { baseSHA256: source.sha256, path, props: { size: 'lg' } })
  const snapshotPath = join(directory, 'large.json')
  await writeFile(snapshotPath, JSON.stringify(projected), { flag: 'wx' })
  const args = ['--example', form, ...fontArgs([400, 500, 600])]
  const result = run(directory, [output, ...args, '--variant-at', JSON.stringify(path), 'size', snapshotPath])
  assert.equal(result.status, 0, result.stderr)
  const graph = await parseFigFile(Uint8Array.from(await readFile(output)).buffer, { populate: 'all' })
  const root = [...graph.getAllNodes()].find(node => origin(node)?.path?.[0] === form)
  const actions = graph.getChildren(root.id).find(node => origin(node)?.localId === 'actions')
  const target = graph.getChildren(actions.id).find(node => origin(node)?.localId === 'create')
  assert.equal(graph.getNode(masterOf(graph, target).parentId).type, 'COMPONENT_SET')
  assert.equal(extractSourceProps(graph, target, source).status, 'no-supported-changes')
  const saved = await readFile(output), files = await readdir(directory)
  for (const address of ['not json', 'null', '{}', '[]', '[""]', '[1]', JSON.stringify([form, 'missing']), JSON.stringify(['unselected'])]) {
    const reason = address === JSON.stringify([form, 'missing']) ? /derived component cannot own an independently projected variant/ :
      address === JSON.stringify(['unselected']) ? /variants require a selected exampleId/ : /Usage: npm run generate/
    refused(run(directory, [join(directory, 'refused.fig'), ...args, '--variant-at', address, 'size', snapshotPath]), reason)
    assert.deepEqual(await readdir(directory), files)
    assert.deepEqual(await readFile(output), saved)
  }
})

test('CLI packages supplied source properties instead of silently regenerating the Core gallery', async t => {
  const directory = await fixture(t), exampleId = 'pk-ui.component.button/secondary'
  const supplied = exportCore(['--example', exampleId, '--props'], { label: 'Publish album draft' })
  const output = join(directory, 'supplied.fig')
  const generated = run(directory, [output, '--snapshot-stdin', '--example', exampleId, ...fontArgs([600])], JSON.stringify(supplied))
  assert.equal(generated.status, 0, generated.stderr)
  const graph = await parseFigFile(Uint8Array.from(await readFile(output)).buffer, { populate: 'all' })
  const placed = [...graph.getAllNodes()].filter(node => node.type === 'INSTANCE' && origin(node)?.path)
  assert.equal(placed.length, 1)
  assert.deepEqual(origin(placed[0]).path, [exampleId])
  assert.equal(origin(placed[0]).sha256, supplied.sha256)
  assert.notEqual(supplied.sha256, source.sha256)
  assert.equal(graph.getChildren(placed[0].id)[0].text, 'Publish album draft')
  assert.equal(extractSourceProps(graph, placed[0], supplied).status, 'no-supported-changes')
  const projection = exportCore(['--example', exampleId, '--props'], { label: 'Publish album draft', tone: 'danger' })
  const projectionPath = join(directory, 'supplied-projection.json'), familyOutput = join(directory, 'supplied-family.fig')
  await writeFile(projectionPath, JSON.stringify(projection), { flag: 'wx' })
  const familyResult = run(directory, [familyOutput, '--snapshot-stdin', '--example', exampleId,
    '--variant', exampleId, 'tone', projectionPath, ...fontArgs([600])], JSON.stringify(supplied))
  assert.equal(familyResult.status, 0, familyResult.stderr)
  const familyGraph = await parseFigFile(Uint8Array.from(await readFile(familyOutput)).buffer, { populate: 'all' })
  const familyRoot = [...familyGraph.getAllNodes()].find(node => node.type === 'INSTANCE' && origin(node)?.path)
  const family = familyGraph.getNode(masterOf(familyGraph, familyRoot).parentId)
  assert.equal(family.type, 'COMPONENT_SET')
  assert.equal(origin(family).sha256, supplied.sha256, 'family generation must not fall back to the Core gallery')
  assert.ok(familyGraph.getChildren(family.id).some(node => origin(node).sha256 === projection.sha256))
  assert.equal(familyGraph.getChildren(familyRoot.id)[0].text, 'Publish album draft')
  assert.equal(extractSourceProps(familyGraph, familyRoot, supplied).status, 'no-supported-changes')
  const missing = join(directory, 'missing.fig')
  refused(run(directory, [missing, '--snapshot-stdin', '--example', form, ...fontArgs([600])], JSON.stringify(supplied)),
    /Document requires exactly one source example: pk-ui\.component\.form\/default/)
  await assert.rejects(readFile(missing), { code: 'ENOENT' })
})

test('CLI packages exact selected native examples from fresh source, including nested Form and linked icon', async t => {
  const directory = await fixture(t)
  for (const selection of [
    { name: 'default', ids: [button], weights: [600], options: [], mode: 'light', viewport: { width: 1280, height: 900 } },
    { name: 'composed', ids: [form, button], weights: [400, 500, 600], options: ['--mode', 'dark', '--viewport', '640x480'], mode: 'dark', viewport: { width: 640, height: 480 } },
    { name: 'matched-weight', ids: [form, button], weights: [400, 600], options: [], mode: 'light', viewport: { width: 1280, height: 900 } },
  ]) {
    const output = join(directory, `${selection.name}.fig`)
    const args = [output, ...selection.ids.flatMap(id => ['--example', id]), ...selection.options, ...fontArgs(selection.weights)]
    const generated = run(directory, args)
    assert.equal(generated.status, 0, generated.stderr || generated.error?.message)
    assert.ok(generated.stdout.includes(source.sha256), 'CLI records the fresh full Go export revision')
    const bytes = await readFile(output)
    const graph = await parseFigFile(Uint8Array.from(bytes).buffer, { populate: 'all' })
    const nodes = [...graph.getAllNodes()], placed = nodes.filter(node => node.type === 'INSTANCE' && origin(node)?.path)
    assert.deepEqual(placed.map(node => origin(node).path).toSorted(), selection.ids.map(id => [id]).toSorted())
    assert.deepEqual(nodes.filter(node => node.type === 'COMPONENT' && origin(node)?.exampleId).map(node => origin(node).exampleId).toSorted(),
      selection.ids.flatMap(id => id === form ? [id, 'title', 'actions', 'cancel', 'create'] : [id]).toSorted())
    const collection = [...graph.variableCollections.values()][0]
    assert.equal(graph.variableCollections.size, 1)
    for (const instance of placed) {
      const correspondence = origin(instance), master = masterOf(graph, instance)
      assert.equal(master.type, 'COMPONENT')
      assert.equal(correspondence.sha256, source.sha256)
      assert.equal(origin(master).exampleId, correspondence.path[0])
      assert.deepEqual(origin(master).viewport, selection.viewport)
      assert.equal(origin(master).mode, selection.mode)
      assert.equal(origin(master).environment.fontHinting, 'none')
      assert.equal(graph.getNodeVariableModeId(instance.id, collection.id), collection.modes.find(mode => mode.name === selection.mode).modeId)
      assert.equal(extractSourceProps(graph, instance, source).status, 'no-supported-changes')
    }
    const iconOwner = placed.find(node => origin(node).path[0] === button)
    const definitions = masterOf(graph, iconOwner).componentPropertyDefinitions
    assert.equal(definitions.find(property => property.name === 'label').type, 'TEXT')
    const swap = definitions.find(property => property.type === 'INSTANCE_SWAP')
    assert.equal(swap.name, 'IconStart')
    const icon = graph.getChildren(iconOwner.id).find(node => node.componentPropertyReferences.some(ref => ref.propertyId === swap.id))
    assert.equal(icon.type, 'INSTANCE')
    assert.ok(Math.abs(icon.width - 16) < 1e-6)
    assert.equal(masterOf(graph, icon).width, 24)
    if (selection.ids.includes(form)) {
      const formInstance = placed.find(node => origin(node).path[0] === form)
      assert.equal(formInstance.width, selection.viewport.width)
      const input = graph.getChildren(formInstance.id).find(node => origin(node)?.localId === 'title')
      assert.equal(input.type, 'INSTANCE')
      assert.deepEqual(masterOf(graph, input).componentPropertyDefinitions.map(({ name, type, defaultValue }) => ({ name, type, defaultValue })).toSorted((a, b) => a.name.localeCompare(b.name)), [
        { name: 'label', type: 'TEXT', defaultValue: 'Title' }, { name: 'value', type: 'TEXT', defaultValue: '' },
      ])
      const actions = graph.getChildren(formInstance.id).find(node => origin(node)?.localId === 'actions')
      assert.equal(actions.type, 'INSTANCE')
      assert.deepEqual(graph.getChildren(actions.id).map(node => origin(node)?.localId).toSorted(), ['cancel', 'create'])
      for (const action of graph.getChildren(actions.id)) masterOf(graph, action)
    }
    refused(run(directory, args), /EEXIST.*link/)
    assert.deepEqual(await readFile(output), bytes)
    assert.ok((await readdir(directory)).every(name => name.endsWith('.fig')), 'no staging directory remains')
  }
})

test('selected generation refuses invalid requests completely and preserves symlinks', async t => {
  const directory = await fixture(t), output = join(directory, 'refused.fig')
  const valid = ['--example', form, ...fontArgs([400, 500, 600])]
  const usage = /Usage: npm run generate/, selection = /Component options require --example selections/
  const cases = [
    [['--example', 'missing', ...fontArgs([600])], /Document requires exactly one source example: missing/],
    [[...valid, '--example', form], /Document requires nonempty, unique source example IDs/],
    [['--example', 'pk-ui.component.input/email', ...fontArgs([400, 500, 600])], /input\/email: Native component: inline fragments need undecorated single-line presentation/],
    [['--example', button, '--example', 'pk-ui.component.input/email', ...fontArgs([400, 500, 600])], /input\/email: Native component: inline fragments need undecorated single-line presentation/],
    [['--example', form, ...fontArgs([400, 500])], /face.*missing or synthesized/],
    [['--example', form, ...fontArgs([500, 600])], /face.*missing or synthesized/],
    [['--example', form], /Component selections require caller-supplied --font faces/],
    [['--mode', 'dark'], selection],
    [['--viewport', '320x480'], selection],
    [fontArgs([600]), selection],
    [[...valid, '--mode', 'sepia'], usage],
    [[...valid, '--mode', 'light', '--mode', 'dark'], /Repeated option: --mode/],
    [[...valid, '--viewport', '320x480', '--viewport', '640x480'], /Repeated option: --viewport/],
    [[...valid, '--viewport', '0x480'], usage],
    [[...valid, '--viewport', '320.5x480'], usage],
    [[...valid, '--unknown'], usage],
    [['--example'], usage],
    [['--example', form, '--font', 'IBM Plex Sans', '400'], usage],
    [['--example', form, '--font', 'IBM Plex Sans', '400', 'normal', 'relative.woff'], usage],
    [['--example', form, '--font', 'IBM Plex Sans', '400', 'normal', join(directory, 'missing.woff')], /ENOENT.*missing\.woff/],
    [['--example', form, '--font', 'IBM Plex Sans', '500', 'normal', fontPath(400)], /Font weight or italic face does not match its metadata/],
  ]
  for (const [args, reason] of cases) {
    refused(run(directory, [output, ...args]), reason)
    assert.deepEqual(await readdir(directory), [], 'refusal created an output or left staging debris')
  }
  const target = join(directory, 'existing.txt'), destination = join(directory, 'existing.fig')
  await writeFile(target, 'keep existing bytes', { flag: 'wx' })
  await symlink(target, destination)
  refused(run(directory, [destination, ...valid]), /EEXIST.*link/)
  assert.ok((await lstat(destination)).isSymbolicLink())
  assert.equal(await readFile(target, 'utf8'), 'keep existing bytes')
  assert.deepEqual((await readdir(directory)).toSorted(), ['existing.fig', 'existing.txt'])
})
