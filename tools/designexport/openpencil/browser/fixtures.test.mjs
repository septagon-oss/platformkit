// Test-only source and font fixtures; kept in the design-test budget.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

// Shared process setup, not an expected-result oracle. Every call runs the
// owning Go exporter with fresh inputs; assertions remain in the calling test.
export function exportCore(args = [], input) {
  return JSON.parse(execFileSync('go', ['run', './tools/designexport', ...args], {
    cwd: new URL('../../../../', import.meta.url), encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
    input: input === undefined ? undefined : JSON.stringify(input),
  }))
}

// Compile the caller's real Go composition once; every observation still runs
// it afresh with independent JSON input. The test owns source/binary cleanup,
// including build failures. Nothing is installed or cached in the checkout.
export async function sourceFixture(t, source) {
  const directory = await mkdtemp(join(tmpdir(), 'platformkit-design-source-'))
  t.after(() => rm(directory, { recursive: true, force: true }))
  const file = join(directory, 'main.go'), executable = join(directory, 'fixture')
  await writeFile(file, source, { flag: 'wx' })
  execFileSync('go', ['build', '-o', executable, file], { cwd: new URL('../../../../', import.meta.url) })
  return input => JSON.parse(execFileSync(executable, [], {
    encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, input: JSON.stringify(input),
  }))
}

export function suppliedFonts(weights) {
  return weights.map(weight => {
    const bytes = readFileSync(new URL(`../node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
    return { family: 'IBM Plex Sans', weight, style: 'normal', bytes, sha256: createHash('sha256').update(bytes).digest('hex') }
  })
}
