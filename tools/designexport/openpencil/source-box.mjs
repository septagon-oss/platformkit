import { fileURLToPath } from 'node:url'
import { sourceLayoutRecord } from './layout-correction.mjs'

const requireBox = (condition, message) => { if (!condition) throw new Error(`Native component: source box ${message}`) }

// CSS overflow clips children at the padding edge, while native frames use
// their border edge. Keep that distinction in the existing source record.
// https://drafts.csswg.org/css-overflow-3/#corner-clipping
export function planSourceBox(node, borders) {
  const style = node.style, box = { version: 1 }
  if (style['overflow-x'] === 'hidden' || style['overflow-y'] === 'hidden') {
    requireBox(style['overflow-x'] === 'hidden' && style['overflow-y'] === 'hidden', 'requires matching hidden overflow axes')
    box.overflow = borders
  }
  if (style['aspect-ratio'] && style['aspect-ratio'] !== 'auto') {
    const ratio = /^(\d+(?:\.\d+)?)\s*\/\s*(\d+(?:\.\d+)?)$/.exec(style['aspect-ratio'])
    requireBox(ratio && Number(ratio[1]) > 0 && Number(ratio[2]) > 0 && style['box-sizing'] === 'border-box' &&
      ['flex', 'inline-flex'].includes(style.display) && node.sizing.height === 'auto', 'requires a width-led border-box flex aspect ratio')
    box.aspectRatio = Number(ratio[1]) / Number(ratio[2])
  }
  return Object.keys(box).length > 1 ? { cssBox: box, ...(box.overflow ? { clipsContent: true } : {}) } : {}
}

export function sourceAspectRatio(graph, node) {
  const box = sourceLayoutRecord(graph, node)?.cssBox
  const heightSizing = node.layoutMode === 'HORIZONTAL' ? node.counterAxisSizing : node.primaryAxisSizing
  const widthSizing = node.layoutMode === 'HORIZONTAL' ? node.primaryAxisSizing : node.counterAxisSizing
  return box?.version === 1 && Number.isFinite(box.aspectRatio) && box.aspectRatio > 0 && heightSizing === 'HUG' &&
    ['FIXED', 'FILL'].includes(widthSizing) ? box.aspectRatio : undefined
}

export function clipSourceOverflow(r, canvas, graph, node) {
  const box = sourceLayoutRecord(graph, node)?.cssBox, borders = box?.overflow
  if (box?.version !== 1 || !Array.isArray(borders) || borders.length !== 4 || !borders.every(value => Number.isFinite(value) && value >= 0)) return false
  const [top, right, bottom, left] = borders, { width, height } = node
  if (left + right >= width || top + bottom >= height) {
    canvas.clipRect(r.ck.LTRBRect(0, 0, 0, 0), r.ck.ClipOp.Intersect, true)
    return true
  }
  const corners = node.independentCorners ? [node.topLeftRadius, node.topRightRadius, node.bottomRightRadius, node.bottomLeftRadius] : Array(4).fill(node.cornerRadius)
  const [tl, tr, br, bl] = corners
  const scale = Math.min(1, width / (tl + tr || 1), width / (bl + br || 1), height / (tl + bl || 1), height / (tr + br || 1))
  const radii = corners.map(radius => radius * scale)
  canvas.clipRRect(new Float32Array([left, top, width - right, height - bottom,
    Math.max(0, radii[0] - left), Math.max(0, radii[0] - top),
    Math.max(0, radii[1] - right), Math.max(0, radii[1] - top),
    Math.max(0, radii[2] - right), Math.max(0, radii[2] - bottom),
    Math.max(0, radii[3] - left), Math.max(0, radii[3] - bottom)]), r.ck.ClipOp.Intersect, true)
  return true
}

export function correctSourceOverflow(source, replace) {
  source = `import { clipSourceOverflow } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  return replace(source, '\t\tif (nodeHasSmoothCorners(node)) {\n\t\t\tconst clipPath',
    '\t\tif (clipSourceOverflow(r, canvas, graph, node)) {\n\t\t} else if (nodeHasSmoothCorners(node)) {\n\t\t\tconst clipPath')
}
