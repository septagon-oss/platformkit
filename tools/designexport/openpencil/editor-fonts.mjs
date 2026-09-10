import { createHash } from 'node:crypto'
import { dirname, isAbsolute, join, relative, sep } from 'node:path'
import { weightToStyle } from '@open-pencil/core/text'
import { validateFonts } from './fonts.mjs'

// Build inputs for the SDK's existing bundled-font loader. No runtime font
// registry, network resolver or implicit product typeface is introduced.
export function editorFonts(args, readFile, realpath) {
  if (args[0] === '--font-assets') {
    if (args.length !== 2 || !isAbsolute(args[1]) || typeof realpath !== 'function') {
      throw new Error('Editor font assets require --font-assets /absolute/manifest and a canonical path resolver')
    }
    return assetFonts(args[1], readFile, realpath)
  }
  const inputs = []
  let license
  for (let index = 0; index < args.length;) {
    const flag = args[index++]
    if (flag === '--font-license' && !license && isAbsolute(args[index] ?? '')) {
      license = args[index++]
    } else if (flag === '--font' && index + 4 <= args.length) {
      const [family, weight, style, path] = args.slice(index, index + 4)
      if (!/^[1-9]00$/.test(weight) || !isAbsolute(path)) throw new Error('Editor font requires exact weight and absolute file path')
      inputs.push({ family, weight: Number(weight), style, path })
      index += 4
    } else throw new Error('Use repeated --font FAMILY WEIGHT STYLE /absolute/font and one --font-license /absolute/notice')
  }
  if (!inputs.length && !license) return { faces: [], files: [], entries: {} }
  if (!inputs.length || !license) throw new Error('Supplied editor fonts require their license notice')
  const faces = validateFonts(inputs.map(({ path, ...face }) => {
    const bytes = readFile(path)
    return { ...face, bytes, sha256: digest(bytes) }
  }))
  const notice = Uint8Array.from(readFile(license))
  return packageFonts(faces, faces.map(() => ({ notice })))
}

const digest = bytes => createHash('sha256').update(bytes).digest('hex')
const fields = (value, keys) => value && !Array.isArray(value) && typeof value === 'object' &&
  Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key))
const text = value => typeof value === 'string' && value !== '' && value === value.trim() &&
  value.isWellFormed() && !/\p{Cc}/u.test(value)
const sha = value => typeof value === 'string' && /^[a-f0-9]{64}$/u.test(value)
const requireAsset = (condition, message) => { if (!condition) throw new Error(`Editor font assets: ${message}`) }
const extension = bytes => ({ wOFF: 'woff', OTTO: 'otf', '\x00\x01\x00\x00': 'ttf' })[Buffer.from(bytes.subarray(0, 4)).toString('latin1')]

function assetFonts(manifestPath, readFile, realpath) {
  const root = realpath(dirname(manifestPath))
  const inside = path => {
    const canonical = realpath(path), local = relative(root, canonical)
    requireAsset(local !== '' && !isAbsolute(local) && local !== '..' && !local.startsWith('..' + sep), 'file escapes the package directory')
    return canonical
  }
  const bytes = readFile(inside(manifestPath))
  requireAsset(bytes instanceof Uint8Array && bytes.length <= 1024 * 1024, 'manifest must be at most 1 MiB of UTF-8 JSON')
  const input = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes), (key, value, context) => {
    // Reject decimal/exponent spellings rather than round a source weight into
    // a provider-supported face. Core's numeric domain is deliberately wider.
    if (key === 'weight') requireAsset(/^[1-9]00$/u.test(context.source ?? ''), 'weight must be an exact 100–900 integer literal')
    return value
  })
  requireAsset(fields(input, ['schema', 'assets', 'faces', 'files']) && input.schema === 'platformkit.font-delivery.v1', 'unsupported manifest schema or fields')
  requireAsset([input.assets, input.faces, input.files].every(Array.isArray), 'assets, faces and files must be arrays')
  const assets = new Map(), bindings = new Map(), identities = new Set(), referenced = new Set()
  for (const asset of input.assets) {
    requireAsset(fields(asset, ['id', 'sha256', 'mediaType', 'source', 'license']) && text(asset.id) && text(asset.source) &&
      sha(asset.sha256) && !assets.has(asset.id), 'invalid or duplicate asset identity/evidence')
    requireAsset(['font/ttf', 'font/otf', 'font/woff'].includes(asset.mediaType), 'unsupported font media type')
    requireAsset(fields(asset.license, ['id', 'sha256', 'source']) && text(asset.license.id) && text(asset.license.source) && sha(asset.license.sha256),
      `asset ${asset.id} requires its own license evidence`)
    assets.set(asset.id, asset)
  }
  for (const face of input.faces) {
    requireAsset(fields(face, ['id', 'asset', 'family', 'postScriptName', 'weight', 'style']) && text(face.id) &&
      !identities.has(face.id) && assets.has(face.asset) && text(face.postScriptName), 'invalid, duplicate or unbound face identity')
    identities.add(face.id)
    referenced.add(face.asset)
  }
  const locate = name => {
    requireAsset(text(name) && !isAbsolute(name) && !/[\\:]/u.test(name) && name.split('/').every(part => part && part !== '.' && part !== '..'),
      'file binding must be a portable relative path inside the package')
    return inside(join(root, name))
  }
  for (const binding of input.files) {
    requireAsset(fields(binding, ['asset', 'font', 'notice']) && assets.has(binding.asset) && !bindings.has(binding.asset), 'invalid or duplicate file binding')
    bindings.set(binding.asset, { font: locate(binding.font), notice: locate(binding.notice) })
  }
  requireAsset(bindings.size === assets.size && referenced.size === assets.size, 'every selected asset needs a face and file binding')
  // Resolve every path before reading any asset/notice. Captured bytes, not
  // another read of the source path during publication, feed the existing SDK.
  const evidence = new Map()
  for (const [id, asset] of assets) {
    const binding = bindings.get(id), font = Uint8Array.from(readFile(binding.font)), notice = Uint8Array.from(readFile(binding.notice))
    requireAsset(digest(font) === asset.sha256, `asset ${id} byte digest mismatch`)
    requireAsset(digest(notice) === asset.license.sha256, `asset ${id} notice digest mismatch`)
    requireAsset('font/' + extension(font) === asset.mediaType, `asset ${id} media type differs from bytes`)
    evidence.set(id, { font, notice })
  }
  const sources = [...input.faces].sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
  const faces = validateFonts(sources.map(face => ({ family: face.family, weight: face.weight, style: face.style,
    bytes: evidence.get(face.asset).font, sha256: assets.get(face.asset).sha256 })))
  const proofs = sources.map((sourceFace, i) => {
    requireAsset(faces[i].postscriptName === sourceFace.postScriptName, `face ${sourceFace.id} PostScript name differs from bytes`)
    return { sourceFace, sourceAsset: assets.get(sourceFace.asset), notice: evidence.get(sourceFace.asset).notice }
  })
  return packageFonts(faces, proofs)
}

// Both the explicit preview and source-backed inputs use this one packaging
// path. Equal notice bytes share a file while each face retains its own evidence.
function packageFonts(faces, proofs) {
  const files = new Map(), entries = {}
  const supplied = faces.map(({ bytes, internalFamily, postscriptName, ...face }, i) => {
    const { notice, ...proof } = proofs[i]
    if (!new TextDecoder('utf-8', { fatal: true }).decode(notice).trim()) throw new Error('Editor font license notice must not be empty')
    const licenseSHA256 = digest(notice), licensePath = `/licenses/font-${licenseSHA256}.txt`
    const path = `/fonts/${face.sha256}.${extension(bytes)}`
    files.set(licensePath, { path: licensePath, bytes: notice })
    files.set(path, { path, bytes })
    entries[`${face.family}|${weightToStyle(face.weight, face.style === 'italic')}`] = path
    return { ...face, path, postscriptName, licensePath, licenseSHA256, ...proof }
  })
  return { faces: supplied, files: [...files.values()], entries }
}

export function bundleEditorFonts(source, fonts, replace) {
  if (!fonts.faces.length) return source
  const bundled = source.match(/const BUNDLED_FONTS = (\{[\s\S]*?\n\});/)
  if (!bundled) throw new Error('SDK bundled-font declaration changed')
  // Its version-checked declaration is a literal object, not evaluated code.
  const existing = JSON.parse(bundled[1])
  for (const key of Object.keys(fonts.entries)) {
    if (Object.hasOwn(existing, key)) throw new Error(`Supplied font would replace an SDK face: ${key}`)
  }
  source = replace(source, bundled[0], `const BUNDLED_FONTS = ${JSON.stringify({ ...existing, ...fonts.entries })};`)
  return replace(source, 'for (const { provider, families } of webFontFamilies)',
    `for (const key of Object.keys(BUNDLED_FONTS)) {
      const family = key.slice(0, key.indexOf('|'));
      byFamily.set(family, { family, source: 'bundled' });
    }
    for (const { provider, families } of webFontFamilies)`)
}
