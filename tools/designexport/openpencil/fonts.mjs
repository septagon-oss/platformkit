import { createHash } from 'node:crypto'
import OpenType from 'opentype.js'
import { fontManager, weightToStyle } from '@open-pencil/core/text'
import { initCanvasKit } from '@open-pencil/core/io/formats/raster'

const digest = bytes => createHash('sha256').update(bytes).digest('hex')

function faceKey(face) {
  if (!face || typeof face.family !== 'string' || !face.family.trim() ||
      face.family !== face.family.trim() || /[|,\x00-\x1f]/u.test(face.family) ||
      !Number.isInteger(face.weight) || face.weight < 100 || face.weight > 900 || face.weight % 100 ||
      !['normal', 'italic'].includes(face.style)) {
    throw new Error('Font requires one family, an exact 100–900 face weight and normal or italic style')
  }
  return `${face.family}|${weightToStyle(face.weight, face.style === 'italic')}`
}

function fontNames(font, field) {
  return [...new Set(Object.values(font.names).flatMap(platform => Object.values(platform[field] ?? {})))]
}

// Caller-owned static font files, not a font catalog or a CSS fallback resolver.
// Validation alone does not initialize CanvasKit or register a native font.
export function validateFonts(faces) {
  if (!Array.isArray(faces)) throw new Error('Fonts must be an array')
  const seen = new Set()
  return faces.map(face => {
    const key = faceKey(face)
    if (seen.has(key)) throw new Error(`Duplicate font face: ${key}`)
    seen.add(key)
    if (!(face.bytes instanceof Uint8Array) || !/^[a-f0-9]{64}$/u.test(face.sha256 ?? '')) {
      throw new Error(`Font requires bytes and a SHA-256 digest: ${key}`)
    }
    const bytes = Uint8Array.from(face.bytes)
    if (digest(bytes) !== face.sha256) throw new Error(`Font byte digest mismatch: ${key}`)
    const signature = Buffer.from(bytes.subarray(0, 4)).toString('latin1')
    if (!['\x00\x01\x00\x00', 'OTTO', 'wOFF'].includes(signature)) {
      throw new Error(`Unsupported font format: ${key}; static TTF, OTF or WOFF required, not WOFF2`)
    }
    const font = OpenType.parse(bytes.buffer)
    if (font.tables.fvar) throw new Error(`Variable font faces are not supported: ${key}`)
    const preferred = fontNames(font, 'preferredFamily')
    const families = preferred.length ? preferred : fontNames(font, 'fontFamily')
    const sdkStyle = weightToStyle(face.weight, face.style === 'italic')
    // Static WOFF name tables may include the weight in the legacy family
    // (IBM Plex Sans SemiBold), without a separate typographic-family entry.
    if (!families.includes(face.family) && !families.includes(`${face.family} ${sdkStyle}`)) {
      throw new Error(`Font family does not match its name table: ${key}`)
    }
    const italic = !!(font.tables.os2?.fsSelection & 1)
    if (font.tables.os2?.usWeightClass !== face.weight || italic !== (face.style === 'italic')) {
      throw new Error(`Font weight or italic face does not match its metadata: ${key}`)
    }
    const names = fontNames(font, 'postScriptName')
    if (names.length !== 1) throw new Error(`Font needs one unambiguous PostScript name: ${key}`)
    return {
      family: face.family, weight: face.weight, style: face.style, bytes, sha256: face.sha256,
      postscriptName: names[0], internalFamily: fontNames(font, 'fontFamily')[0],
    }
  })
}

// Every requested face and character must be supplied. The SDK's normal
// local/web/Inter fallback chain is not evidence that these requirements hold.
export async function loadFonts(faces, requirements) {
  const validated = validateFonts(faces)
  if (!Array.isArray(requirements)) throw new Error('Font requirements must be an array')
  const byKey = new Map(validated.map(face => [faceKey(face), face]))
  for (const requirement of requirements) {
    const face = byKey.get(faceKey(requirement))
    if (!face) throw new Error(`Missing exact supplied font face: ${faceKey(requirement)}`)
    if (typeof requirement.text !== 'string') throw new Error('Font requirement text must be a string')
    const font = OpenType.parse(face.bytes.buffer)
    for (const character of requirement.text) {
      if (!'\r\n\t'.includes(character) && !font.charToGlyphIndex(character)) {
        throw new Error(`Missing font glyph ${JSON.stringify(character)}: ${faceKey(requirement)}`)
      }
    }
  }
  const ck = await initCanvasKit()
  for (const face of validated) {
    const typeface = ck.Typeface.MakeFreeTypeFaceFromData(face.bytes.buffer)
    if (!typeface) throw new Error(`CanvasKit cannot decode font: ${faceKey(face)}`)
    try {
      if (typeface.getFamilyName() !== face.internalFamily) throw new Error('CanvasKit font family mismatch')
      for (const requirement of requirements.filter(item => faceKey(item) === faceKey(face))) {
        const text = requirement.text.replace(/[\r\n\t]/gu, '')
        if ([...typeface.getGlyphIDs(text)].includes(0)) throw new Error(`CanvasKit font glyph missing: ${faceKey(face)}`)
      }
    } finally { typeface.delete() }
    // SDK FIG digests are cached by family/style, with no public reset. Never
    // replace a loaded face; use another process for a different byte identity.
    const loaded = fontManager.loadedData(face.family, weightToStyle(face.weight, face.style === 'italic'))
    if (loaded && digest(new Uint8Array(loaded)) !== face.sha256) {
      throw new Error(`Font already loaded with different bytes: ${faceKey(face)}`)
    }
  }
  // Registration is synchronous, after the complete preflight, so a rejected
  // requirement cannot partially populate the SDK's process-wide font cache.
  for (const face of validated) {
    const style = weightToStyle(face.weight, face.style === 'italic')
    if (!fontManager.loadedData(face.family, style)) fontManager.markLoaded(face.family, style, face.bytes.slice().buffer)
  }
  return validated
}
