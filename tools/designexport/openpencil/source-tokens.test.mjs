import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { mkdtemp, readFile, readdir, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'
import { createEditor } from '@open-pencil/core/editor'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { generateId } from '@open-pencil/scene-graph'
import { parseFigBuffer } from '@open-pencil/fig'
import { decodeSnapshot, planSourceTokens } from './source-tokens.mjs'
import { buildFoundation } from './foundation.mjs'
import { restoreTokenOrigin } from './variable-source.mjs'
import { sourceTokenFixture } from './browser/fixtures.test.mjs'

const repo = fileURLToPath(new URL('../../../', import.meta.url))
const captured = JSON.parse(execFileSync('go', ['run', './tools/designexport', '--tokens', 'both'], { cwd: repo, encoding: 'utf8' }))
const byName = (graph, name) => [...graph.variables.values()].find(variable => variable.name === name)
const reopen = async graph => parseFigFile((await exportFigFile(graph)).slice().buffer, { populate: 'all' })

function iconPixels(graph, mode, ck, surface, renderer) {
  const frame = [...graph.getAllNodes()].find(node => node.name === mode && node.type === 'FRAME')
  const instance = graph.getChildren(frame.id).find(node => node.type === 'INSTANCE')
  const canvas = surface.getCanvas()
  canvas.clear(ck.TRANSPARENT)
  canvas.save(); canvas.scale(4, 4)
  renderer.renderSceneToCanvas(canvas, graph, instance.id)
  canvas.restore(); surface.flush()
  return Buffer.from(canvas.readPixels(0, 0, { width: 96, height: 96,
    alphaType: ck.AlphaType.Unpremul, colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB }))
}

function selection() {
  const snapshot = structuredClone(captured)
  snapshot.examples = []
  snapshot.icons = snapshot.icons.slice(0, 1)
  snapshot.sourceTokens = {
    modes: snapshot.sourceTokens.modes.map(mode => ({ mode: mode.mode, colors: mode.colors.filter(color =>
      ['--pk-color-text-primary', '--pk-color-surface-canvas'].includes(color.name)) })),
    colors: [
      { name: '--selected-ink', value: { reference: '--pk-color-text-primary' } },
      { name: '--selected-mix', value: { mix: { first: { reference: '--selected-ink' }, firstPercent: 25,
        second: { literal: 'transparent' } } } },
    ],
    scales: [
      { scale: 'spacing', key: '1', number: { value: 0.1, unit: 'px' } },
      { scale: 'leading', key: 'normal', number: { value: 1.5, unit: '' } },
    ],
  }
  return snapshot
}

function decoded(snapshot = selection()) {
  return decodeSnapshot(Buffer.from(JSON.stringify(snapshot).replace('"value":0.1,"unit":"px"', '"value":0.1000,"unit":"px"')))
}

test('typed token planning is detached and retains authored decimals before native allocation', () => {
  const { snapshot, scalarSpellings } = decoded(), before = structuredClone(snapshot)
  assert.deepEqual(scalarSpellings, [
    { scale: 'spacing', key: '1', decimal: '0.1000' }, { scale: 'leading', key: 'normal', decimal: '1.5' },
  ])
  const plan = planSourceTokens(snapshot, scalarSpellings)
  assert.deepEqual(plan.modes, ['light', 'dark'])
  assert.equal(plan.variables.length, 6)
  const scale = plan.variables.find(variable => variable.name === 'spacing/1')
  assert.equal(scale.type, 'FLOAT')
  assert.deepEqual(scale.values, [0.1, 0.1])
  assert.equal(scale.sourceToken.decimal, '0.1000')
  assert.equal(scale.sourceToken.unit, 'px')
  scale.sourceToken.unit = 'changed'
  assert.equal(planSourceTokens(snapshot, scalarSpellings).variables.find(variable => variable.name === 'spacing/1').sourceToken.unit, 'px')
  const mixed = plan.variables.find(variable => variable.name === '--selected-mix')
  mixed.values[0].formula.inputs.push('--changed')
  assert.deepEqual(mixed.values[1].formula.inputs, ['--selected-ink'])
  assert.deepEqual(snapshot, before)
})

test('equal numeric values retain their own decimal evidence when source order changes', () => {
  const snapshot = selection()
  snapshot.sourceTokens.scales = [
    { scale: 'spacing', key: '1', number: { value: 0.1, unit: 'px' } },
    { scale: 'spacing', key: '2', number: { value: 0.1, unit: 'px' } },
  ]
  const bytes = JSON.stringify(snapshot).replace('"value":0.1', '"value":0.1000').replace('"value":0.1,', '"value":1e-1,')
  const input = decodeSnapshot(Buffer.from(bytes))
  input.snapshot.sourceTokens.scales.reverse()
  const plan = planSourceTokens(input.snapshot, input.scalarSpellings)
  assert.equal(plan.variables.find(variable => variable.name === 'spacing/1').sourceToken.decimal, '0.1000')
  assert.equal(plan.variables.find(variable => variable.name === 'spacing/2').sourceToken.decimal, '1e-1')
})

test('typed tokens retain source identity, decimal spelling, native edits and aliases through two saves', async () => {
  const { snapshot, scalarSpellings } = decoded()
  let { graph } = buildFoundation(snapshot, { scalarSpellings })
  const before = structuredClone(snapshot)
  for (let cycle = 0; cycle < 3; cycle++) {
    const spacing = byName(graph, cycle === 0 ? 'spacing/1' : 'Renamed spacing'), editor = createEditor({ graph })
    assert.equal(spacing.type, 'FLOAT')
    assert.deepEqual(spacing.sourceToken, { version: 1, snapshot: snapshot.sha256,
      kind: 'scale', scale: 'spacing', key: '1', decimal: '0.1000', unit: 'px' })
    assert.equal(graph.resolveVariable(spacing.id), cycle === 0 ? 0.1 : 0.1234567890123456)
    const col = graph.variableCollections.get(spacing.collectionId), dark = col.modes.find(mode => mode.name === 'dark').modeId
    assert.equal(graph.resolveVariable(spacing.id, dark), 0.1)
    const ink = byName(graph, '--selected-ink'), primary = byName(graph, '--pk-color-text-primary')
    for (const mode of col.modes) assert.deepEqual(ink.valuesByMode[mode.modeId], { aliasId: primary.id })
    const mixed = byName(graph, '--selected-mix'), resolved = graph.resolveVariable(mixed.id)
    assert.equal(resolved.a, 0.25)
    assert.deepEqual(mixed.valuesByMode[col.defaultModeId].cssColor.customProperties, { '--selected-ink': { aliasId: ink.id } })
    if (cycle === 0) {
      editor.updateVariableValue(spacing.id, col.defaultModeId, 0.1234567890123456)
      editor.undoAction()
      assert.equal(graph.resolveVariable(spacing.id), 0.1)
      editor.redoAction()
      editor.renameVariable(spacing.id, 'Renamed spacing')
    }
    if (cycle < 2) graph = await reopen(graph)
  }
  assert.deepEqual(snapshot, before, 'native edits never write the source capture')
})

test('a scale-only selection needs no theme, icons, layout or invented theme mode', async () => {
  const snapshot = selection()
  snapshot.themes = []; snapshot.icons = []
  snapshot.sourceTokens = { scales: snapshot.sourceTokens.scales }
  const input = decoded(snapshot), { graph, collection, icons } = buildFoundation(input.snapshot, input)
  assert.equal(graph.variables.size, 2)
  assert.equal(icons.size, 0)
  assert.deepEqual(collection.modes.map(mode => mode.name), ['Default'])
  assert.equal((await reopen(graph)).variables.size, 2)
})

test('typed admission refuses the entire unsupported request without mutating the caller', () => {
  for (const [mutate, message] of [
    [s => { s.requiredFeatures.push('future.v1') }, /feature/],
    [s => { s.requiredFeatures.push('source-tokens.v1') }, /feature/],
    [s => { s.requiredFeatures = [] }, /feature/],
    [s => { s.sourceTokens.modes[0].fonts = captured.sourceTokens.modes[0].fonts }, /font/],
    [s => { s.sourceTokens.shadows = captured.sourceTokens.shadows }, /shadow/],
    [s => { s.sourceTokens.easings = captured.sourceTokens.easings }, /easing/],
    [s => { s.sourceTokens.transitions = captured.sourceTokens.transitions }, /transition/],
    [s => { s.sourceTokens.scales[0].number.unit = 'rem' }, /unit/],
    [s => { s.sourceTokens.scales[0] = { scale: 'spacing', key: 'auto', keyword: 'auto' } }, /keyword/],
    [s => { s.sourceTokens.scales.push(s.sourceTokens.scales[0]) }, /duplicate/],
    [s => { s.sourceTokens.colors[0].value.reference = '--missing' }, /missing/],
    [s => { s.sourceTokens.colors[0].value.reference = '--selected-mix' }, /cycle/],
    [s => { s.sourceTokens.modes[1].colors.pop() }, /identit/],
    [s => { s.sourceTokens.modes[0].colors[0].value = '#ff0000' }, /capture/],
    [s => { s.sourceTokens.modes[0].colors.push(s.sourceTokens.modes[0].colors[0]) }, /duplicate/],
    [s => { s.sourceTokens.future = [{ value: 1 }] }, /field/],
    [s => { s.measurements = [{ scale: 'spacing', key: '1', value: 0.1, unit: 'px' }] }, /feature/],
  ]) {
    const snapshot = selection(); mutate(snapshot)
    const input = decoded(snapshot), before = structuredClone(input.snapshot)
    const id = Number(generateId().split(':')[1])
    assert.throws(() => buildFoundation(input.snapshot, input), message)
    assert.equal(Number(generateId().split(':')[1]), id + 1, 'refusal precedes every native ID allocation')
    assert.deepEqual(input.snapshot, before)
  }
  assert.throws(() => buildFoundation(captured), /font|unit|shadow/)
  assert.throws(() => buildFoundation(selection()), /decimal/)
})

test('real source-producer tokens drive linked icon pixels and preserve independent modes through two saves', async t => {
  const run = await sourceTokenFixture(t), input = decodeSnapshot(Buffer.from(run({})))
  assert.deepEqual(input.scalarSpellings, [
    { scale: 'spacing', key: '1', decimal: '0.1000' }, { scale: 'leading', key: 'normal', decimal: '1.5' },
  ])
  let { graph } = buildFoundation(input.snapshot, input)
  const ck = await initCanvasKit(), surface = ck.MakeSurface(96, 96), renderer = new SkiaRenderer(ck, surface)
  try {
    const render = mode => iconPixels(graph, mode, ck, surface, renderer)
    const dark = render('dark'), light = render('light')
    assert.notDeepEqual(light, dark)
    const primary = byName(graph, '--pk-color-text-primary'), editor = createEditor({ graph })
    const col = graph.variableCollections.get(primary.collectionId)
    editor.updateVariableValue(primary.id, col.defaultModeId, { r: 1, g: 0, b: 0, a: 1 })
    const red = render('light')
    assert.notDeepEqual(red, light)
    assert.deepEqual(render('dark'), dark)
    editor.undoAction(); assert.deepEqual(render('light'), light)
    editor.redoAction(); assert.deepEqual(render('light'), red)
    for (let save = 0; save < 2; save++) {
      graph = await reopen(graph)
      assert.deepEqual(render('light'), red)
      assert.deepEqual(render('dark'), dark)
      const scale = byName(graph, 'spacing/1')
      assert.equal(scale.sourceToken.decimal, '0.1000')
      assert.equal(graph.resolveVariable(scale.id), 0.1)
      assert.deepEqual(graph.resolveVariable(byName(graph, '--selected-mix').id), { r: 1, g: 0, b: 0, a: 0.25 })
    }
  } finally { renderer.destroy() }
})

test('selected or reversed modes keep their order without constructing unrequested themes', async () => {
  for (const modes of [['light'], ['dark'], ['dark', 'light']]) {
    const snapshot = selection()
    snapshot.sourceTokens.modes = modes.map(name => snapshot.sourceTokens.modes.find(mode => mode.mode === name))
    const input = decoded(snapshot), initial = buildFoundation(input.snapshot, input)
    for (const graph of [initial.graph, await reopen(initial.graph)]) {
      const col = [...graph.variableCollections.values()][0]
      assert.deepEqual(col.modes.map(mode => mode.name), modes)
      assert.equal(col.defaultModeId, col.modes[0].modeId)
      const previews = [...graph.getAllNodes()].filter(node => node.type === 'FRAME' && ['light', 'dark'].includes(node.name))
      assert.deepEqual(previews.map(node => node.name), modes)
      assert.equal(graph.resolveVariable(byName(graph, 'spacing/1').id), 0.1)
    }
  }
})

test('a selected foreground blend constructs actual linked translucent icon paints through two saves', async () => {
  const snapshot = selection()
  for (const mode of snapshot.sourceTokens.modes) mode.colors = mode.colors.filter(color => color.name === '--pk-color-surface-canvas')
  snapshot.sourceTokens.colors = [
    { name: '--source-red', value: { literal: '#f008' } },
    { name: '--pk-color-text-primary', value: { mix: { first: { reference: '--source-red' }, firstPercent: 50,
      second: { literal: 'transparent' } } } },
  ]
  const input = decoded(snapshot)
  let { graph } = buildFoundation(input.snapshot, input)
  const ck = await initCanvasKit(), surface = ck.MakeSurface(96, 96), renderer = new SkiaRenderer(ck, surface)
  try {
    const baseline = iconPixels(graph, 'light', ck, surface, renderer)
    const interiors = []
    for (let index = 0; index < baseline.length; index += 4) {
      if (baseline[index + 3] === 68) interiors.push([...baseline.subarray(index, index + 4)])
      assert.ok(baseline[index + 3] <= 68)
    }
    assert.ok(interiors.length > 100)
    assert.ok(interiors.every(pixel => String(pixel) === '255,0,0,68'))
    for (let save = 0; save < 2; save++) {
      graph = await reopen(graph)
      for (const mode of ['light', 'dark']) assert.deepEqual(iconPixels(graph, mode, ck, surface, renderer), baseline)
    }
  } finally { renderer.destroy() }
})

test('token metadata refuses ambiguous ownership or changed source meaning without mutating decoded records', async () => {
  const input = decoded(), { graph } = buildFoundation(input.snapshot, input)
  const raw = parseFigBuffer((await exportFigFile(graph)).slice().buffer)
  const node = raw.nodeChanges.find(node => node.type === 'VARIABLE' && node.name === 'spacing/1')
  const get = node => node.pluginData.find(entry => entry.key === 'source-token')
  const expected = byName(graph, 'spacing/1').sourceToken
  assert.deepEqual(restoreTokenOrigin(node, 'FLOAT'), expected)
  for (const mutate of [
    data => { data.source.version = 2 },
    data => { data.source.snapshot = 'missing' },
    data => { data.source.unit = 'rem' },
    data => { data.source.unit = undefined },
    data => { data.source.scale = 'path/escape' },
    data => { data.source.key = '' },
    data => { data.source.decimal = '9007199254740993' },
    data => { data.source.decimal = '1e-999' },
    data => { data.source.decimal = 0.1 },
    data => { data.source.extra = true },
    data => { data.variableId = '0:999999' },
    data => { data.extra = true },
  ]) {
    const changed = structuredClone(node), data = JSON.parse(get(changed).value)
    mutate(data); get(changed).value = JSON.stringify(data)
    const before = structuredClone(changed)
    assert.throws(() => restoreTokenOrigin(changed, 'FLOAT'), /metadata|Native number/)
    assert.deepEqual(changed, before)
  }
  for (const mutate of [
    node => node.pluginData.push(get(node)),
    node => { get(node).value = '{' },
    node => { get(node).value = ' '.repeat(16385) },
    node => { get(node).value = get(node).value.replace('{', '{"variableId":"ignored",') },
  ]) {
    const changed = structuredClone(node); mutate(changed)
    assert.throws(() => restoreTokenOrigin(changed, 'FLOAT'), /metadata/)
  }
  assert.throws(() => restoreTokenOrigin(node, 'COLOR'), /metadata/)
})

test('feature and SVG refusal also precede native IDs for cyclic or inconsistent captured data', () => {
  const input = decoded(), before = structuredClone(input.snapshot)
  input.snapshot.icons[0].sha256 = '0'.repeat(64)
  let id = Number(generateId().split(':')[1])
  assert.throws(() => buildFoundation(input.snapshot, input), /SHA256 mismatch/)
  assert.equal(Number(generateId().split(':')[1]), id + 1)
  const cycle = { children: [] }; cycle.children.push({ description: cycle })
  before.examples = [cycle]
  id = Number(generateId().split(':')[1])
  assert.throws(() => planSourceTokens(before, input.scalarSpellings), /cyclic/)
  assert.equal(Number(generateId().split(':')[1]), id + 1)
})

test('native formula limits are checked before IDs, with a positive boundary case', () => {
  for (const size of [256, 257]) {
    const snapshot = selection()
    snapshot.icons = []
    const inputs = Array.from({ length: size }, (_, index) => ({ name: `--c${index}`, value: { literal: '#f00' } }))
    const mix = names => names.length === 1 ? { reference: names[0] } : { mix: {
      first: mix(names.slice(0, Math.floor(names.length / 2))), firstPercent: 50,
      second: mix(names.slice(Math.floor(names.length / 2))),
    } }
    snapshot.sourceTokens.colors = [...inputs, { name: '--wide', value: mix(inputs.map(input => input.name)) }]
    const input = decoded(snapshot), id = Number(generateId().split(':')[1])
    if (size === 256) {
      const { graph } = buildFoundation(input.snapshot, input)
      assert.deepEqual(graph.resolveVariable(byName(graph, '--wide').id), { r: 1, g: 0, b: 0, a: 1 })
    } else {
      assert.throws(() => buildFoundation(input.snapshot, input), /too many expression inputs/)
      assert.equal(Number(generateId().split(':')[1]), id + 1)
    }
  }
})

test('source decimal decoding retains spelling and refuses loss instead of reconstructing precision', () => {
  for (const [literal, expected] of [['1e-50', 1e-50], ['-0.000', -0], ['9007199254740992', 9007199254740992]]) {
    const bytes = JSON.stringify(selection()).replace('"value":0.1,"unit":"px"', `"value":${literal},"unit":"px"`)
    const input = decodeSnapshot(Buffer.from(bytes))
    const plan = planSourceTokens(input.snapshot, input.scalarSpellings), scale = plan.variables.find(variable => variable.name === 'spacing/1')
    assert.ok(Object.is(scale.values[0], expected))
    assert.equal(scale.sourceToken.decimal, literal)
  }
  for (const literal of ['1e-999', '9007199254740993', '0.10000000000000001', '1e39']) {
    const input = decodeSnapshot(Buffer.from(JSON.stringify(selection()).replace('"value":0.1,"unit":"px"', `"value":${literal},"unit":"px"`)))
    assert.throws(() => buildFoundation(input.snapshot, input), /precision|finite/)
  }
  const input = decoded()
  const mismatched = structuredClone(input.scalarSpellings)
  mismatched[0].decimal = '0.2'
  assert.throws(() => buildFoundation(input.snapshot, { scalarSpellings: mismatched }), /decimal/)
  assert.throws(() => decodeSnapshot(Buffer.from('{"schema":"a","schema":"b"}')), /duplicate/)
  assert.throws(() => decodeSnapshot(Buffer.from([0xff])), /UTF-8/)
})

test('the existing CLI writes selected typed tokens and leaves no file for a refused complete Core selection', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'platformkit-source-tokens-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const cli = fileURLToPath(new URL('./generate.mjs', import.meta.url)), destination = join(directory, 'selected.fig')
  const run = (output, input) => spawnSync(process.execPath, [cli, output, '--snapshot-stdin'], { input, encoding: 'utf8', cwd: directory })
  const bytes = JSON.stringify(selection()).replace('"value":0.1,"unit":"px"', '"value":0.1000,"unit":"px"')
  const result = run(destination, bytes)
  assert.equal(result.status, 0, result.stderr)
  const graph = await parseFigFile(Uint8Array.from(await readFile(destination)).buffer, { populate: 'all' })
  assert.equal(byName(graph, 'spacing/1').sourceToken.decimal, '0.1000')
  const rejected = run(join(directory, 'complete.fig'), JSON.stringify(captured))
  assert.notEqual(rejected.status, 0)
  assert.match(rejected.stderr, /font|unit|shadow/)
  assert.deepEqual(await readdir(directory), ['selected.fig'])
})
