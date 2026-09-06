import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdtemp, readFile, rm, symlink } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { basename, join } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { SkiaRenderer } from '@open-pencil/core/canvas'
import { exportFigFile, parseFigFile } from '@open-pencil/core/io/formats/fig'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'
import { buildFoundation, prepareIcon } from './foundation.mjs'

const repo = fileURLToPath(new URL('../../../', import.meta.url))
const cli = fileURLToPath(new URL('./generate.mjs', import.meta.url))
const snapshot = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
  cwd: repo, encoding: 'utf8',
}))
const digest = value => createHash('sha256').update(value).digest('hex')

function named(graph, name, parent) {
  const nodes = parent ? graph.getChildren(parent.id) : [...graph.getAllNodes()]
  const matches = nodes.filter(node => node.name === name)
  assert.equal(matches.length, 1, `one node named ${name}`)
  return matches[0]
}

function metadata(node, key) {
  const entries = node.pluginData.filter(entry => entry.pluginId === 'platformkit' && entry.key === key)
  assert.equal(entries.length, 1, `one provenance record ${key}`)
  return JSON.parse(entries[0].value)
}

function checkGraph(graph) {
  const collections = [...graph.variableCollections.values()]
  assert.equal(collections.length, 1)
  const collection = collections[0]
  assert.equal(collection.name, 'Foundation')
  assert.deepEqual(collection.modes.map(mode => mode.name).sort(), ['dark', 'light'])
  assert.equal(graph.variables.size, 25)
  assert.deepEqual(metadata(named(graph, 'Foundation', graph.getPages()[0]), 'platformkit.source'), {
    schema: snapshot.schema, sha256: snapshot.sha256, fontPolicy: snapshot.fontPolicy,
    notices: snapshot.notices, scope: 'tokens-and-icons',
  })
  const masters = named(graph, 'Icon masters')
  assert.equal(graph.getChildren(masters.id).length, 27)
  for (const theme of snapshot.themes) {
    const frame = named(graph, theme.mode)
    const mode = collection.modes.find(candidate => candidate.name === theme.mode)
    assert.equal(frame.variableModes[collection.id], mode.modeId)
    assert.equal(graph.getChildren(frame.id).length, 27)
    for (const token of theme.tokens) {
      const variables = [...graph.variables.values()].filter(variable => variable.name === token.name)
      assert.equal(variables.length, 1, token.name)
      const variable = variables[0]
      assert.equal(variable.type, token.type === 'color' ? 'COLOR' : 'STRING')
      const value = variable.valuesByMode[mode.modeId]
      if (token.type === 'fontFamily') assert.equal(value, token.value)
      else {
        const channels = token.value.slice(1).match(/../g).map(hex => parseInt(hex, 16) / 255)
        for (const [index, channel] of ['r', 'g', 'b'].entries()) {
          assert.ok(Math.abs(value[channel] - channels[index]) < 1e-6, `${theme.mode}/${token.name}/${channel}`)
        }
        assert.equal(value.a, 1)
      }
    }
    for (const icon of snapshot.icons) {
      const master = named(graph, icon.name, masters)
      const instance = named(graph, icon.name, frame)
      assert.equal(master.type, 'COMPONENT')
      assert.equal(instance.type, 'INSTANCE')
      assert.equal(graph.getNode(instance.componentId), master)
      assert.deepEqual(metadata(master, 'platformkit.icon'), {
        name: icon.name, sha256: icon.sha256, source: icon.source, license: icon.license,
      })
      const vectors = graph.getChildren(master.id)
      assert.equal(vectors.length, 1, icon.name)
      for (const vector of vectors) assert.equal(vector.type, 'VECTOR')
      for (const vector of graph.getChildren(instance.id)) {
        const variable = graph.variables.get(vector.boundVariables['fills/0/color'])
        assert.equal(variable?.name, '--pk-color-text-primary', `${theme.mode}/${icon.name}`)
      }
    }
  }
}

async function pixels(graph, instance) {
  const ck = await initCanvasKit()
  const size = 128
  const surface = ck.MakeSurface(size, size)
  assert.ok(surface)
  const renderer = new SkiaRenderer(ck, surface)
  try {
    const canvas = surface.getCanvas()
    canvas.clear(ck.TRANSPARENT)
    canvas.scale(size / instance.width, size / instance.height)
    renderer.renderSceneToCanvas(canvas, graph, instance.id)
    surface.flush()
    const result = canvas.readPixels(0, 0, {
      width: size, height: size, alphaType: ck.AlphaType.Unpremul,
      colorType: ck.ColorType.RGBA_8888, colorSpace: ck.ColorSpace.SRGB,
    })
    assert.ok(result)
    return Buffer.from(result)
  } finally {
    renderer.destroy()
  }
}

function opaqueColors(bytes) {
  const colors = new Set()
  for (let index = 0; index < bytes.length; index += 4) {
    if (bytes[index + 3] === 255) colors.add([...bytes.subarray(index, index + 3)].join(','))
  }
  return [...colors].sort()
}

test('fresh foundation retains tokens, linked icons, provenance and rendered colors through two FIG saves', async () => {
  const before = structuredClone(snapshot)
  const prepared = prepareIcon(snapshot.icons[0]), pristine = structuredClone(prepared)
  prepared.children[0].properties.vectorNetwork.vertices[0].x += 1
  assert.deepEqual(prepareIcon(snapshot.icons[0]), pristine, 'prepared geometry is deterministic and independently owned')
  assert.throws(() => prepareIcon({ ...snapshot.icons[0], name: '' }), /icon identity/)
  let graph = buildFoundation(snapshot)
  assert.deepEqual(snapshot, before)
  const baseline = new Map()
  for (let cycle = 0; cycle < 3; cycle++) {
    checkGraph(graph)
    for (const [mode, rgb] of [['light', [21, 34, 31]], ['dark', [238, 243, 236]]]) {
      for (const instance of graph.getChildren(named(graph, mode).id)) {
        const key = `${mode}/${instance.name}`
        const image = await pixels(graph, instance)
        assert.deepEqual(opaqueColors(image), [rgb.join(',')], key)
        if (cycle === 0) baseline.set(key, image)
        else assert.deepEqual(image, baseline.get(key), `round trip ${cycle}: ${key}`)
      }
    }
    if (cycle < 2) {
      const bytes = await exportFigFile(graph)
      graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    }
  }
})

test('native foreground follows changed tokens while literal paints remain independent', async () => {
  const changed = structuredClone(snapshot)
  const token = changed.themes[1].tokens.find(candidate => candidate.name === '--pk-color-text-primary')
  token.value = '#123456'
  const graph = buildFoundation(changed)
  const icon = named(graph, 'sun', named(graph, 'dark'))
  assert.deepEqual(opaqueColors(await pixels(graph, icon)), ['18,52,86'], 'changed dark Sun')
  // Keep the multi-shape paint regression independent of the catalog's Sun,
  // which is now a single Phosphor path rather than our former circle + rays.
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" fill="currentColor">' +
    '<circle cx="64" cy="128" r="40" fill="#fedcba"/><path d="M136 88h80v80h-80Z"/></svg>'
  const fixture = {
    name: 'mixed-paint-regression', svg, sha256: digest(svg),
    source: 'platformkit/native-conformance-fixture', license: 'Apache-2.0',
  }
  changed.icons.push(fixture)
  let mixed = buildFoundation(changed)
  const baseline = new Map()
  for (let cycle = 0; cycle < 3; cycle++) {
    const master = named(mixed, fixture.name, named(mixed, 'Icon masters'))
    for (const [mode, foreground] of [['light', '21,34,31'], ['dark', '18,52,86']]) {
      const instance = named(mixed, fixture.name, named(mixed, mode))
      assert.equal(instance.componentId, master.id)
      const vectors = mixed.getChildren(instance.id)
      assert.equal(vectors.length, 2, 'separate native paths preserve literal and bound paints')
      assert.equal(vectors[0].boundVariables['fills/0/color'], undefined, 'literal paint stays unbound')
      const variable = mixed.variables.get(vectors[1].boundVariables['fills/0/color'])
      assert.equal(variable?.name, '--pk-color-text-primary')
      const image = await pixels(mixed, instance)
      assert.deepEqual(opaqueColors(image), [foreground, '254,220,186'].sort())
      if (cycle === 0) baseline.set(mode, image)
      else assert.deepEqual(image, baseline.get(mode), `${mode} mixed paint round trip ${cycle}`)
    }
    if (cycle < 2) {
      const bytes = await exportFigFile(mixed)
      mixed = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    }
  }
})

test('malformed foundation inputs are refused without changing their caller-owned data', () => {
  const mutations = [
    value => { value.schema = 'unknown' },
    value => { value.themes[1].mode = 'light' },
    value => { value.themes[0].tokens.push(value.themes[0].tokens[0]) },
    value => { value.themes[1].tokens.pop() },
    value => { value.themes[0].tokens[0].value = 'not a color' },
    value => { value.themes[0].tokens[0].type = 'unknown' },
    value => { value.icons.push(value.icons[0]) },
    value => { value.icons[0].sha256 = '0'.repeat(64) },
  ].map(mutate => [mutate, Error])
  const svgCases = [
    ['<path d=""/>', /invalid or empty SVG path/],
    ['<path d="M0 nonsense"/>', /invalid or empty SVG path/],
    ['<path d="M0 0L128 0L128 128Z" fill="none" stroke="none"/>', /invisible SVG shape/],
    ['<svg viewBox="0 0 128 128"><path d="M0 0L128 0L128 128Z"/></svg>', /unsupported SVG (element|content)/],
    ['<path d="M0 0L64 64" fill="none" stroke="#f00" transform="scale(2)"/>', /transformed SVG strokes/],
    ['<g transform="scale(2)"><path d="M0 0L64 64" stroke="#f00"/></g>', /transformed SVG strokes/],
    ['<circle cx="0x10" cy="128" r="52"/>', /invalid SVG dimension/],
    ['<path d="M0 0L128 128" transform="scale(0x10)"/>', /invalid SVG transform/],
    ['<path d="M0 0L128 128" transform="translate(,1)"/>', /invalid SVG transform/],
    ['<path d="M0 0L128 128"><circle cx="128" cy="128" r="52"/></path>', /unsupported SVG content/],
    ['<path d="M0 0L128 128"/>', /unsupported SVG namespace/, 'urn:not-svg'],
  ]
  for (const [body, error, namespace = 'http://www.w3.org/2000/svg'] of svgCases) mutations.push([value => {
    value.icons[0].svg = `<svg xmlns="${namespace}" viewBox="0 0 256 256" fill="currentColor">${body}</svg>`
    value.icons[0].sha256 = digest(value.icons[0].svg)
  }, error])
  for (const [mutate, error] of mutations) {
    const invalid = structuredClone(snapshot)
    mutate(invalid)
    const before = structuredClone(invalid)
    assert.throws(() => buildFoundation(invalid), error)
    assert.deepEqual(invalid, before)
  }
})

test('CLI reads fresh source from any cwd and refuses unsafe or nonexclusive output', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'platformkit-foundation-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const output = join(directory, 'foundation.fig')
  const run = args => spawnSync(process.execPath, [cli, ...args], { cwd: directory, encoding: 'utf8' })
  const generated = run([output])
  assert.equal(generated.status, 0, generated.stderr)
  const original = await readFile(output)
  const graph = await parseFigFile(Uint8Array.from(original).buffer, { populate: 'all' })
  checkGraph(graph)
  for (const args of [[], [output, 'extra'], [output]]) assert.notEqual(run(args).status, 0)
  assert.deepEqual(await readFile(output), original)
  const blockedName = `${basename(directory)}.fig`
  const blocked = join(repo, blockedName)
  t.after(() => rm(blocked, { force: true }))
  await symlink(repo, join(directory, 'workspace'), 'dir')
  for (const destination of [blocked, join(directory, 'workspace', blockedName)]) {
    assert.notEqual(run([destination]).status, 0)
    await assert.rejects(readFile(destination), { code: 'ENOENT' })
  }
})
