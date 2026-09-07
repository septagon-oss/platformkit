import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { mkdtemp, readFile, readdir, rm, symlink, writeFile, lstat } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { parseFigFile } from '@open-pencil/core/io/formats/fig'
import { extractSourceProps } from '../source-changes.mjs'

const repo = fileURLToPath(new URL('../../../../', import.meta.url))
const cli = fileURLToPath(new URL('../generate.mjs', import.meta.url))
const source = JSON.parse(execFileSync('go', ['run', './tools/designexport'], { cwd: repo, encoding: 'utf8' }))
const form = 'pk-ui.component.form/default', button = 'pk-ui.component.button/with-leading-icon'
const fontPath = weight => fileURLToPath(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
const fontArgs = weights => weights.flatMap(weight => ['--font', 'IBM Plex Sans', String(weight), 'normal', fontPath(weight)])
const run = (directory, args, input) => spawnSync(process.execPath, [cli, ...args], {
  cwd: directory, encoding: 'utf8', input, timeout: 120_000, maxBuffer: 4 * 1024 * 1024,
})

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

test('CLI packages supplied source properties instead of silently regenerating the Core gallery', async t => {
  const directory = await fixture(t), exampleId = 'pk-ui.component.button/secondary'
  const supplied = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--example', exampleId, '--props'], {
    cwd: repo, encoding: 'utf8', input: JSON.stringify({ label: 'Publish album draft' }),
  }))
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
  const missing = join(directory, 'missing.fig')
  assert.notEqual(run(directory, [missing, '--snapshot-stdin', '--example', form, ...fontArgs([600])], JSON.stringify(supplied)).status, 0)
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
    const repeated = run(directory, args)
    assert.notEqual(repeated.status, 0, 'selected generation must not overwrite an existing document')
    assert.deepEqual(await readFile(output), bytes)
    assert.ok((await readdir(directory)).every(name => name.endsWith('.fig')), 'no staging directory remains')
  }
})

test('selected generation refuses invalid requests completely and preserves symlinks', async t => {
  const directory = await fixture(t), output = join(directory, 'refused.fig')
  const valid = ['--example', form, ...fontArgs([400, 500, 600])]
  const cases = [
    ['--example', 'missing', ...fontArgs([600])],
    [...valid, '--example', form],
    ['--example', 'pk-ui.component.input/email', ...fontArgs([400, 500, 600])],
    ['--example', button, '--example', 'pk-ui.component.input/email', ...fontArgs([400, 500, 600])],
    ['--example', form, ...fontArgs([400, 500])],
    ['--example', form, ...fontArgs([500, 600])],
    ['--example', form],
    ['--mode', 'dark'],
    ['--viewport', '320x480'],
    fontArgs([600]),
    [...valid, '--mode', 'sepia'],
    [...valid, '--mode', 'light', '--mode', 'dark'],
    [...valid, '--viewport', '320x480', '--viewport', '640x480'],
    [...valid, '--viewport', '0x480'],
    [...valid, '--viewport', '320.5x480'],
    [...valid, '--unknown'],
    ['--example'],
    ['--example', form, '--font', 'IBM Plex Sans', '400'],
    ['--example', form, '--font', 'IBM Plex Sans', '400', 'normal', 'relative.woff'],
    ['--example', form, '--font', 'IBM Plex Sans', '400', 'normal', join(directory, 'missing.woff')],
    ['--example', form, '--font', 'IBM Plex Sans', '500', 'normal', fontPath(400)],
  ]
  for (const args of cases) {
    const result = run(directory, [output, ...args])
    assert.notEqual(result.status, 0, `invalid request succeeded: ${JSON.stringify(args)}`)
    assert.equal(result.signal, null, result.error?.message)
    assert.notEqual(result.stderr.trim(), '', 'refusal needs an actionable diagnostic')
    assert.deepEqual(await readdir(directory), [], 'refusal created an output or left staging debris')
  }
  const target = join(directory, 'existing.txt'), destination = join(directory, 'existing.fig')
  await writeFile(target, 'keep existing bytes', { flag: 'wx' })
  await symlink(target, destination)
  const refused = run(directory, [destination, ...valid])
  assert.notEqual(refused.status, 0)
  assert.ok((await lstat(destination)).isSymbolicLink())
  assert.equal(await readFile(target, 'utf8'), 'keep existing bytes')
  assert.deepEqual((await readdir(directory)).toSorted(), ['existing.fig', 'existing.txt'])
})
