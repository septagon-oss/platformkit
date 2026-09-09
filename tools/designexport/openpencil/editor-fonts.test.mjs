import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import { editorFonts, bundleEditorFonts } from './editor-fonts.mjs'

const file = weight => new URL(`node_modules/@fontsource/ibm-plex-sans/files/ibm-plex-sans-latin-${weight}-normal.woff`, import.meta.url).pathname
const license = new URL('node_modules/@fontsource/ibm-plex-sans/LICENSE', import.meta.url).pathname
const fontArgs = weight => ['--font', 'IBM Plex Sans', String(weight), 'normal', file(weight)]
const notice = ['--font-license', license]
const hash = bytes => createHash('sha256').update(bytes).digest('hex')
const replace = (source, before, after) => {
  assert.equal(source.split(before).length, 2)
  return source.replace(before, after)
}

test('editor font packaging is opt-in and retains exact licensed faces, not workstation dependencies', () => {
  assert.deepEqual(editorFonts([], () => assert.fail('No font inputs require no file reads')), { faces: [], files: [], entries: {} })
  const built = editorFonts([...fontArgs(400), ...fontArgs(600), ...notice], readFileSync)
  assert.equal(built.faces.length, 2)
  assert.equal(built.files.length, 3)
  assert.deepEqual(Object.keys(built.entries), ['IBM Plex Sans|Regular', 'IBM Plex Sans|SemiBold'])
  for (const [index, weight] of [400, 600].entries()) {
    const face = built.faces[index], original = readFileSync(file(weight))
    assert.equal(face.family, 'IBM Plex Sans')
    assert.equal(face.weight, weight)
    assert.equal(face.sha256, hash(original))
    assert.deepEqual(Buffer.from(built.files.find(file => file.path === face.path).bytes), original)
    assert.equal(face.licenseSHA256, hash(readFileSync(license)))
    assert.equal(face.licensePath, built.files[0].path)
    assert.match(face.path, /^\/fonts\/[a-f0-9]{64}\.woff$/)
  }
})

test('font packaging rejects unknown inputs, duplicate faces, missing notices and false binary identities', () => {
  for (const args of [
    ['--unknown'], ['--font'], [...fontArgs(400)], notice,
    [...fontArgs(400), ...fontArgs(400), ...notice], [...fontArgs(400), ...notice, ...notice],
    ['--font', 'Wrong Family', '400', 'normal', file(400), ...notice],
    ['--font', 'IBM Plex Sans', '500', 'normal', file(400), ...notice],
    ['--font', 'IBM Plex Sans', '400', 'italic', file(400), ...notice],
    ['--font', 'IBM Plex Sans', '4e2', 'normal', file(400), ...notice],
    ['--font', 'IBM Plex Sans', '400', 'normal', 'relative.woff', ...notice],
  ]) assert.throws(() => editorFonts(args, readFileSync), args.join(' '))
  assert.throws(() => editorFonts([...fontArgs(400), ...notice], path => path === license ? Buffer.from('  ') : readFileSync(path)), /must not be empty/)
})

test('editor fonts extend the owning SDK bundled loader and picker without replacing its original faces', () => {
  const source = readFileSync(new URL('node_modules/@open-pencil/core/dist/text/fonts.js', import.meta.url), 'utf8')
  assert.equal(bundleEditorFonts(source, editorFonts([], readFileSync), replace), source)
  const built = editorFonts([...fontArgs(600), ...notice], readFileSync)
  const result = bundleEditorFonts(source, built, replace)
  const entries = JSON.parse(result.match(/const BUNDLED_FONTS = (\{.*\});/)[1])
  assert.equal(entries['Inter|Regular'], '/Inter-Regular.ttf')
  assert.equal(entries['IBM Plex Sans|SemiBold'], built.faces[0].path)
  assert.match(result, /for \(const key of Object.keys\(BUNDLED_FONTS\)\)/)
  assert.throws(() => bundleEditorFonts(source, { ...built, entries: { 'Inter|Regular': '/other.ttf' } }, replace), /replace an SDK face/)
  assert.throws(() => bundleEditorFonts('unexpected source', built, replace), /declaration changed/)
})
