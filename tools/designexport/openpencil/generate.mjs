import './register.mjs'
import { execFileSync } from 'node:child_process'
import { link, mkdtemp, realpath, rm, writeFile } from 'node:fs/promises'
import { basename, dirname, extname, isAbsolute, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

// Load SDK consumers only after the version-checked correction hook is active,
// including when this CLI is invoked directly instead of through npm.
const [{ exportFigFile, parseFigFile }, { buildFoundation }] = await Promise.all([
  import('@open-pencil/core/io/formats/fig'), import('./foundation.mjs'),
])

async function generate(args) {
  if (args.length !== 1 || !isAbsolute(args[0]) || extname(args[0]) !== '.fig') {
    throw new Error('Usage: npm run generate -- /absolute/path/outside-workspace/foundation.fig')
  }
  const repository = await realpath(fileURLToPath(new URL('../../../', import.meta.url)))
  const parent = await realpath(dirname(args[0]))
  const destination = join(parent, basename(args[0]))
  const within = relative(dirname(repository), destination)
  if (within !== '..' && !within.startsWith(`..${sep}`) && !isAbsolute(within)) {
    throw new Error('Generated documents must be outside the workspace, including symlink destinations')
  }
  const snapshot = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
    cwd: repository, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, timeout: 120_000,
  }))
  const bytes = await exportFigFile(buildFoundation(snapshot).graph)
  // Fail before creating an output if the produced FIG cannot be reopened.
  await parseFigFile(bytes.slice().buffer, { populate: 'all' })
  const temporary = await mkdtemp(join(parent, '.platformkit-design-'))
  try {
    const staged = join(temporary, 'foundation.fig')
    await writeFile(staged, bytes, { flag: 'wx', mode: 0o600 })
    // An atomic no-clobber link: existing files and even dangling symlinks win.
    await link(staged, destination)
  } finally {
    await rm(temporary, { recursive: true, force: true })
  }
  console.log(`Created ${destination}\nSource SHA256: ${snapshot.sha256}\nScope: tokens and icons; not a component library or prototype.`)
}

try {
  await generate(process.argv.slice(2))
} catch (error) {
  console.error(error.message)
  process.exitCode = 1
}
