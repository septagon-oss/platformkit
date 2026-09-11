import OpenType from 'opentype.js'
import { weightToStyle } from '@open-pencil/scene-graph'

function fontNames(font, field) {
  return [...new Set(Object.values(font.names).flatMap(platform => Object.values(platform[field] ?? {})))]
}

// A renderer may expose either naming generation from the same font bytes.
// Do not substitute the caller's CSS alias or infer a family from a style name.
export function fontFamilyNames(font) {
  return [...new Set([...fontNames(font, 'fontFamily'), ...fontNames(font, 'preferredFamily')])]
}

// Binary metadata is shared by supplied-font validation and the browser's
// local-font fallback. Display names only narrow candidates; they are not proof.
export function fontMetadata(font, face) {
  const sdkStyle = weightToStyle(face.weight, face.style === 'italic')
  const key = `${face.family}|${sdkStyle}`
  if (font.tables.fvar) throw new Error(`Variable font faces are not supported: ${key}`)
  const preferred = fontNames(font, 'preferredFamily')
  const families = preferred.length ? preferred : fontNames(font, 'fontFamily')
  // Some static faces carry their weight in the legacy family without a
  // separate typographic-family entry. Their OS/2 weight must still agree.
  if (!families.includes(face.family) && !families.includes(`${face.family} ${sdkStyle}`)) {
    throw new Error(`Font family does not match its name table: ${key}`)
  }
  const italic = !!(font.tables.os2?.fsSelection & 1)
  if (font.tables.os2?.usWeightClass !== face.weight || italic !== (face.style === 'italic')) {
    throw new Error(`Font weight or italic face does not match its metadata: ${key}`)
  }
  const names = fontNames(font, 'postScriptName')
  if (names.length !== 1) throw new Error(`Font needs one unambiguous PostScript name: ${key}`)
  return { postscriptName: names[0], internalFamily: fontNames(font, 'fontFamily')[0] }
}

// Resolve only an unambiguous, static binary face. Neither local font labels
// nor enumeration order may select a weight; unreadable candidates are ignored.
export async function resolveLocalFont(fonts, face) {
  if (!Array.isArray(fonts) || !face || typeof face.family !== 'string' || !face.family.trim() ||
    face.family !== face.family.trim() || /[|,\x00-\x1f]/u.test(face.family) ||
    !Number.isInteger(face.weight) || face.weight < 100 || face.weight > 900 || face.weight % 100 ||
    !['normal', 'italic'].includes(face.style)) return null
  const legacyFamily = `${face.family} ${weightToStyle(face.weight, face.style === 'italic')}`
  let selected = null
  for (const candidate of fonts) {
    if (!candidate || ![face.family, legacyFamily].includes(candidate.family)) continue
    let bytes
    try {
      const data = await (await candidate.blob()).arrayBuffer()
      if (!(data instanceof ArrayBuffer)) continue
      bytes = data.slice(0)
      const signature = String.fromCharCode(...new Uint8Array(bytes, 0, 4))
      if (!['\x00\x01\x00\x00', 'OTTO', 'wOFF'].includes(signature)) continue
      fontMetadata(OpenType.parse(bytes), face)
    } catch {
      continue
    }
    if (selected !== null) return null
    selected = bytes
  }
  return selected
}
