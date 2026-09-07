import './register.mjs'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { link, mkdtemp, readFile, realpath, rm, writeFile } from 'node:fs/promises'
import { basename, dirname, extname, isAbsolute, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

// Load SDK consumers only after the version-checked correction hook is active,
// including when this CLI is invoked directly instead of through npm.
const [{ exportFigFile, parseFigFile }, { buildFoundation }] = await Promise.all([
  import('@open-pencil/core/io/formats/fig'), import('./foundation.mjs'),
])

function options(args) {
  const usage = 'Usage: npm run generate -- /absolute/path/outside-workspace/document.fig [--snapshot-stdin] [--example ID ... --font FAMILY WEIGHT STYLE /absolute/font.woff ...] [--mode light|dark] [--viewport WIDTHxHEIGHT]'
  if (!args.length || !isAbsolute(args[0]) || extname(args[0]) !== '.fig') throw new Error(usage)
  const result = { examples: [], faces: [] }, seen = new Set()
  for (let index = 1; index < args.length;) {
    const flag = args[index++]
    if (flag === '--snapshot-stdin') {
      if (seen.has(flag)) throw new Error(`Repeated option: ${flag}`)
      seen.add(flag)
      result.snapshotStdin = true
      continue
    }
    if (!['--example', '--font', '--mode', '--viewport'].includes(flag)) throw new Error(usage)
    const count = flag === '--font' ? 4 : 1, values = args.slice(index, index + count)
    if (values.length !== count || values.some(value => !value || value.startsWith('--'))) throw new Error(usage)
    index += count
    if (flag === '--example') result.examples.push(values[0])
    else if (flag === '--font') {
      const [family, weight, style, path] = values
      if (!/^[1-9]00$/.test(weight) || !['normal', 'italic'].includes(style) || !isAbsolute(path)) throw new Error(usage)
      result.faces.push({ family, weight: Number(weight), style, path })
    } else {
      if (seen.has(flag)) throw new Error(`Repeated option: ${flag}`)
      seen.add(flag)
      if (flag === '--mode') {
        if (!['light', 'dark'].includes(values[0])) throw new Error(usage)
        result.mode = values[0]
      } else {
        if (!/^[1-9]\d*x[1-9]\d*$/.test(values[0])) throw new Error(usage)
        const [width, height] = values[0].split('x').map(Number)
        if (width > 8192 || height > 8192) throw new Error(usage)
        result.viewport = { width, height }
      }
    }
  }
  if (!result.examples.length && (result.faces.length || result.mode || result.viewport)) throw new Error('Component options require --example selections')
  if (result.examples.length && !result.faces.length) throw new Error('Component selections require caller-supplied --font faces')
  return result
}

async function readSnapshot(input) {
  const chunks = [], limit = 32 * 1024 * 1024
  let size = 0
  for await (const chunk of input) {
    size += chunk.length
    if (size > limit) throw new Error(`Design snapshot exceeds ${limit} bytes`)
    chunks.push(chunk)
  }
  try {
    return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(Buffer.concat(chunks)))
  } catch {
    throw new Error('Expected one UTF-8 JSON design-export snapshot on stdin')
  }
}

async function documentBytes(snapshot, selection) {
  if (!selection.examples.length) {
    const bytes = await exportFigFile(buildFoundation(snapshot).graph)
    await parseFigFile(bytes.slice().buffer, { populate: 'all' })
    return bytes
  }
  const [{ chromium }, { SkiaRenderer }, { initCanvasKit }, { buildComponentDocument, verifyComponentDocument }, { validateFonts }] = await Promise.all([
    import('playwright'), import('@open-pencil/core/canvas'), import('@open-pencil/core/io/formats/raster'),
    import('./document.mjs'), import('./fonts.mjs'),
  ])
  const fonts = validateFonts(await Promise.all(selection.faces.map(async ({ path, ...face }) => {
    const bytes = await readFile(path)
    return { ...face, bytes, sha256: createHash('sha256').update(bytes).digest('hex') }
  })))
  let browser, renderer
  try {
    browser = await chromium.launch({ headless: true, args: ['--enable-automation', '--font-render-hinting=none'] })
    const ck = await initCanvasKit(), surface = ck.MakeSurface(1, 1)
    if (!surface) throw new Error('Cannot allocate native text measurement surface')
    renderer = new SkiaRenderer(ck, surface)
    let { graph } = await buildComponentDocument(snapshot, { ...selection, fonts, browser, renderer })
    const correspondence = verifyComponentDocument(graph, snapshot, selection.examples)
    let bytes
    for (let save = 0; save < 2; save++) {
      bytes = await exportFigFile(graph)
      graph = await parseFigFile(bytes.slice().buffer, { populate: 'all' })
      verifyComponentDocument(graph, snapshot, selection.examples, correspondence)
    }
    return bytes
  } finally {
    try { renderer?.destroy() } finally { await browser?.close() }
  }
}

async function generate(args) {
  const selection = options(args)
  const repository = await realpath(fileURLToPath(new URL('../../../', import.meta.url)))
  const parent = await realpath(dirname(args[0]))
  const destination = join(parent, basename(args[0]))
  const within = relative(dirname(repository), destination)
  if (within !== '..' && !within.startsWith(`..${sep}`) && !isAbsolute(within)) {
    throw new Error('Generated documents must be outside the workspace, including symlink destinations')
  }
  const snapshot = selection.snapshotStdin ? await readSnapshot(process.stdin) : JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
    cwd: repository, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, timeout: 120_000,
  }))
  // Reopen and validate before any output is staged or published.
  const bytes = await documentBytes(snapshot, selection)
  const temporary = await mkdtemp(join(parent, '.platformkit-design-'))
  try {
    const staged = join(temporary, 'document.fig')
    await writeFile(staged, bytes, { flag: 'wx', mode: 0o600 })
    // An atomic no-clobber link: existing files and even dangling symlinks win.
    await link(staged, destination)
  } finally {
    await rm(temporary, { recursive: true, force: true })
  }
  const scope = selection.examples.length
    ? `tokens, icons and ${selection.examples.length} selected source compositions; not a complete library or prototype. Fonts must be supplied separately in the editor.`
    : 'tokens and icons; not a component library or prototype.'
  const producer = selection.snapshotStdin ? 'caller-supplied snapshot; source freshness is not verified' : 'fresh Core export from this checkout'
  console.log(`Created ${destination}\nSource SHA256: ${snapshot.sha256}\nInput: ${producer}\nScope: ${scope}`)
}

try {
  await generate(process.argv.slice(2))
} catch (error) {
  console.error(error.message)
  process.exitCode = 1
}
