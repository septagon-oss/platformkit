// Every design test below this directory embeds a real Go program: sourceFixture
// (browser/fixtures.test.mjs) writes it out, builds it, and what the browser then
// observes is that executable's output. Those programs consume kit/httpx,
// ui/screens and ui/resource from no Go package, so the compiler that checks a
// kernel change never saw one: httpx.Resource.Screen and screens.Options.Workspace
// arrived with docs/adr/0017 and the first thing to notice was CI's browser suite,
// after make check had passed. This is that missing compile, with no browser
// involved — it asks nothing of a program beyond compiling, which is the half a
// renamed field breaks. What a fixture renders stays the claim of the test that
// embeds it, in browser/ or editor/ or preview/, where the browser is.
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { devNull, tmpdir } from 'node:os'
import { join, relative } from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'

// The Go module root, which is where a fixture builds: the file lives outside the
// checkout and the module path resolves from the working directory.
const repo = fileURLToPath(new URL('../../../', import.meta.url))

// escapes is the whole of the translation. A Go program inside a template literal
// has to spell its raw-string delimiters, its own backslash and a literal dollar
// of a "${" the harness must not interpolate; the control characters are here
// because a Go string writes them as escapes and JavaScript would have turned them
// into the characters themselves. Any other pair is left alone.
const escapes = { '\\': '\\', '`': '`', '$': '$', n: '\n', t: '\t', r: '\r' }

// programs collects the embedded Go source of every design test: each template
// literal that opens with the package clause and ends at the next backtick that
// does not belong to the Go text.
const programs = []
const collect = file => {
  for (const match of readFileSync(file, 'utf8').matchAll(/`(?<go>package main[\s\S]*?)(?<!\\)`/g))
    programs.push({ name: relative(repo, file), go: match.groups.go.replace(/\\(.)/g, (pair, character) => escapes[character] ?? pair) })
}
const walk = directory => {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === 'node_modules') continue // the vendored dependencies' own sources
    const path = join(directory, entry.name)
    if (entry.isDirectory()) walk(path)
    else if (entry.name.endsWith('.mjs')) collect(path)
  }
}
walk(fileURLToPath(new URL('.', import.meta.url)))

test('every Go program a design test embeds still compiles against the module it composes', t => {
  assert.ok(programs.length > 0, `no embedded Go program was found below ${repo}: the design tests moved and this check went blind`)
  const directory = mkdtempSync(join(tmpdir(), 'platformkit-design-compile-'))
  t.after(() => rmSync(directory, { recursive: true, force: true }))
  const broken = []
  for (const [number, program] of programs.entries()) {
    const file = join(directory, `${number}.go`)
    writeFileSync(file, program.go, { flag: 'wx' })
    const build = spawnSync('go', ['build', '-o', devNull, file],
      { cwd: repo, encoding: 'utf8', env: { ...process.env, GOWORK: 'off', GOFLAGS: '' } })
    assert.equal(build.error, undefined, `${program.name}: go build could not run`)
    if (build.status !== 0) broken.push(`${program.name} (exit ${build.status})\n${(build.stderr || build.stdout).trim()}`)
  }
  // Every failure at once: a rename usually breaks several fixtures, and the fixer
  // should see all of them and the compiler's own words for each.
  assert.deepEqual(broken, [], 'design fixtures no longer compile:\n\n' + broken.join('\n\n'))
})
