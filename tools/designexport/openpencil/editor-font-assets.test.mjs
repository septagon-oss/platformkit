import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { existsSync, mkdtempSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import { editorFonts, bundleEditorFonts } from './editor-fonts.mjs'

const hash = bytes => createHash('sha256').update(bytes).digest('hex')
function fixture(t) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'platformkit-font-assets-')))
  t.after(() => rmSync(root, { recursive: true, force: true }))
  const manifest = { schema: 'platformkit.font-delivery.v1', assets: [], faces: [], files: [] }
  for (const weight of [400, 600]) {
    const bytes = readFileSync(new URL(`node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url))
    const notice = Buffer.concat([readFileSync(new URL('node_modules/@fontsource/ibm-plex-sans/LICENSE', import.meta.url)), Buffer.from('\n'.repeat(weight / 100))])
    const id = `preview/plex/${weight}`
    manifest.assets.push({ id, sha256: hash(bytes), mediaType: 'font/woff', source: '@fontsource/ibm-plex-sans',
      license: { id: 'OFL-1.1', sha256: hash(notice), source: `owner:notice/${weight}` } })
    manifest.faces.push({ id: `face/${weight}`, asset: id, family: 'IBM Plex Sans', weight, style: 'normal',
      postScriptName: `IBMPlexSans-${weight === 400 ? 'Regular' : 'SemiBold'}` })
    manifest.files.push({ asset: id, font: `${weight}.woff`, notice: `${weight}.txt` })
    writeFileSync(join(root, `${weight}.woff`), bytes)
    writeFileSync(join(root, `${weight}.txt`), notice)
  }
  const path = join(root, 'fonts.json')
  const build = (read = readFileSync) => {
    writeFileSync(path, JSON.stringify(manifest))
    return editorFonts(['--font-assets', path], read, realpathSync)
  }
  return { root, path, manifest, build }
}

test('source font assets retain per-asset byte/license evidence through the existing SDK bundle', t => {
  const f = fixture(t), built = f.build()
  assert.equal(built.faces.length, 2)
  assert.equal(built.files.length, 4)
  for (const [i, face] of built.faces.entries()) {
    assert.deepEqual(face.sourceAsset, f.manifest.assets[i])
    assert.deepEqual(face.sourceFace, f.manifest.faces[i])
    assert.equal(face.sha256, face.sourceAsset.sha256)
    assert.equal(face.licenseSHA256, face.sourceAsset.license.sha256)
    assert.equal(hash(built.files.find(file => file.path === face.path).bytes), face.sha256)
    assert.equal(hash(built.files.find(file => file.path === face.licensePath).bytes), face.licenseSHA256)
  }
  f.manifest.assets.reverse(); f.manifest.faces.reverse(); f.manifest.files.reverse()
  assert.deepEqual(f.build(), built, 'unordered source identities must not change packaging')
  const source = readFileSync(new URL('node_modules/@open-pencil/core/dist/text/fonts.js', import.meta.url), 'utf8')
  const bundled = bundleEditorFonts(source, built, (text, before, after) => text.replace(before, after))
  assert.ok(bundled.includes(built.faces[1].path))
  assert.ok(bundled.includes('/Inter-Regular.ttf'))
})

test('source font delivery refuses inconsistent declarations and files before returning a bundle', async t => {
  for (const [name, change] of Object.entries({
    version: f => { f.manifest.schema = 'future' },
    unknown: f => { f.manifest.assets[0].unexpected = true },
    digest: f => { f.manifest.assets[1].sha256 = '0'.repeat(64) },
    notice: f => { f.manifest.assets[1].license.sha256 = '0'.repeat(64) },
    emptyNotice: f => { writeFileSync(join(f.root, '600.txt'), ' '); f.manifest.assets[1].license.sha256 = hash(Buffer.from(' ')) },
    license: f => { delete f.manifest.assets[1].license.id },
    media: f => { f.manifest.assets[1].mediaType = 'font/ttf' },
    unsupported: f => { f.manifest.assets[1].mediaType = 'font/woff2' },
    postscript: f => { f.manifest.faces[1].postScriptName = 'Wrong' },
    weight: f => { f.manifest.faces[1].weight = 650 },
    duplicateFace: f => { f.manifest.faces.push({ ...f.manifest.faces[0], id: 'alias' }) },
    duplicateID: f => { f.manifest.assets[1].id = f.manifest.assets[0].id },
    missingBinding: f => { f.manifest.files.pop() },
    unusedBinding: f => { f.manifest.files.push({ asset: 'unknown', font: '600.woff', notice: '600.txt' }) },
    escape: f => { f.manifest.files[1].font = '../outside.woff' },
    absolute: f => { f.manifest.files[1].notice = '/outside.txt' },
    symlink: f => { symlinkSync('/etc/hostname', join(f.root, 'outside')); f.manifest.files[1].notice = 'outside' },
  })) await t.test(name, t => {
    const f = fixture(t)
    change(f)
    assert.throws(() => f.build(path => { assert.ok(path.startsWith(f.root + '/'), 'read escaped the owned package'); return readFileSync(path) }))
  })
})

test('asset delivery requires its path resolver and refuses mixed preview arguments before reads', t => {
  const f = fixture(t), noRead = () => assert.fail('invalid invocation read a file')
  for (const args of [['--font-assets'], ['--font-assets', 'relative.json'], ['--font-assets', f.path, '--font-license', '/notice']]) {
    assert.throws(() => editorFonts(args, noRead, realpathSync))
  }
  assert.throws(() => editorFonts(['--font-assets', f.path], noRead))
})

test('source weights cannot round into a supported face and build refusal precedes output', t => {
  const f = fixture(t)
  for (const value of ['400.00000000000000001', '4e2', '400.0']) {
    writeFileSync(f.path, JSON.stringify(f.manifest).replace('"weight":400', `"weight":${value}`))
    assert.throws(() => editorFonts(['--font-assets', f.path], readFileSync, realpathSync), /weight.*integer literal/u)
  }
  f.manifest.assets[1].sha256 = '0'.repeat(64)
  writeFileSync(f.path, JSON.stringify(f.manifest))
  assert.throws(() => execFileSync(process.execPath, [new URL('build-editor.mjs', import.meta.url).pathname,
    f.root, '--font-assets', f.path], { timeout: 10000, stdio: 'pipe' }), error => /byte digest mismatch/u.test(String(error.stderr)))
  assert.equal(existsSync(join(f.root, 'platformkit.vite.config.ts')), false)
  assert.equal(existsSync(join(f.root, 'dist')), false)
})
