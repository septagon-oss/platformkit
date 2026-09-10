import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { sourceFixture, suppliedFonts } from './browser/fixtures.test.mjs'
import { validateFonts } from './fonts.mjs'

test('source asset evidence composes with, but does not replace, native font validation', async t => {
  const check = await sourceFixture(t, `package main
import (
  "encoding/json"
  "os"
  "github.com/septagon-oss/platformkit/design"
)
func main() {
  var input struct { Asset design.Asset; Face design.FontFace; Data, Notice []byte }
  if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil { panic(err) }
  err := design.ValidateAssets([]design.Asset{input.Asset}, []design.FontFace{input.Face})
  if err == nil { err = input.Asset.VerifyBytes(input.Data, input.Notice) }
  var out struct { Error string }
  if err != nil { out.Error = err.Error() }
  if err := json.NewEncoder(os.Stdout).Encode(out); err != nil { panic(err) }
}
`)
  const [font] = suppliedFonts([600])
  const notice = readFileSync(new URL('./node_modules/@fontsource/ibm-plex-sans/LICENSE', import.meta.url))
  const asset = { id: 'preview/plex/600', sha256: font.sha256, mediaType: 'font/woff', source: '@fontsource/ibm-plex-sans',
    license: { id: 'OFL-1.1', source: '@fontsource/ibm-plex-sans/LICENSE', sha256: createHash('sha256').update(notice).digest('hex') } }
  const face = { id: 'preview/plex/semibold', asset: asset.id, family: font.family, weight: 600,
    style: 'normal', postScriptName: 'IBMPlexSans-SemiBold' }
  const input = { Asset: asset, Face: face, Data: Buffer.from(font.bytes).toString('base64'), Notice: notice.toString('base64') }
  assert.equal(check(input).Error, '')
  const [physical] = validateFonts([{ ...font, family: face.family, weight: face.weight, style: face.style }])
  assert.equal(physical.postscriptName, face.postScriptName)
  assert.equal(physical.sha256, asset.sha256)

  assert.match(check({ ...input, Data: Buffer.from('replacement font').toString('base64') }).Error, /bytes.*SHA-256/u)
  assert.match(check({ ...input, Notice: Buffer.from('replacement notice').toString('base64') }).Error, /notice.*SHA-256/u)
  assert.match(check({ ...input, Asset: { ...asset, license: {} } }).Error, /license/u)

  // Correct byte identity is still insufficient when the source names the
  // wrong face. Core checks declarations; the existing provider inspects bytes.
  assert.equal(check({ ...input, Face: { ...face, weight: 400 } }).Error, '')
  assert.throws(() => validateFonts([{ ...font, weight: 400 }]), /Font family does not match its name table: IBM Plex Sans\|Regular/u)
  assert.equal(check({ ...input, Face: { ...face, weight: 650 } }).Error, '')
  assert.throws(() => validateFonts([{ ...font, weight: 650 }]), /100–900/u)
})
