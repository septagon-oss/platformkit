import { createHash } from 'node:crypto'
import { isAbsolute } from 'node:path'
import { weightToStyle } from '@open-pencil/core/text'
import { validateFonts } from './fonts.mjs'

// Build inputs for the SDK's existing bundled-font loader. No runtime font
// registry, network resolver or implicit product typeface is introduced.
export function editorFonts(args, readFile) {
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
  const digest = bytes => createHash('sha256').update(bytes).digest('hex')
  const faces = validateFonts(inputs.map(({ path, ...face }) => {
    const bytes = readFile(path)
    return { ...face, bytes, sha256: digest(bytes) }
  }))
  const notice = readFile(license)
  if (!Buffer.from(notice).toString('utf8').trim()) throw new Error('Editor font license notice must not be empty')
  const licenseSHA256 = digest(notice), licensePath = `/licenses/font-${licenseSHA256}.txt`
  const files = [{ path: licensePath, bytes: notice }], entries = {}
  const supplied = faces.map(({ bytes, internalFamily, postscriptName, ...face }) => {
    const signature = Buffer.from(bytes.subarray(0, 4)).toString('latin1')
    const extension = signature === 'wOFF' ? 'woff' : signature === 'OTTO' ? 'otf' : 'ttf'
    const path = `/fonts/${face.sha256}.${extension}`
    files.push({ path, bytes })
    entries[`${face.family}|${weightToStyle(face.weight, face.style === 'italic')}`] = path
    return { ...face, path, postscriptName, licensePath, licenseSHA256 }
  })
  return { faces: supplied, files, entries }
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
