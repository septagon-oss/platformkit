import { parseColor } from '@open-pencil/core/color'
import { computeAllLayouts, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { bindTextProperties } from './bindings.mjs'
import { loadFonts, validateFonts } from './fonts.mjs'

function requireComponent(condition, message) {
  if (!condition) throw new Error(`Native component: ${message}`)
}

function pixels(value) {
  requireComponent(typeof value === 'string' && /^\d+(?:\.\d+)?px$/.test(value), `unsupported length ${value}`)
  return Number.parseFloat(value)
}

function color(value) {
  requireComponent(/^rgba?\([\d.,\s]+\)$/.test(value), `unsupported computed paint ${value}`)
  return parseColor(value)
}

const paint = value => ({ type: 'SOLID', color: color(value), opacity: 1, visible: true })

function sameColor(a, b, tolerance = 1e-6) {
  return a && b && ['r', 'g', 'b', 'a'].every(channel => Number.isFinite(a[channel]) &&
    Number.isFinite(b[channel]) && Math.abs(a[channel] - b[channel]) <= tolerance)
}

// Bind only the direct aliases witnessed by capture, with matching source and
// native values in every supplied mode. Mixed/derived paints need their own
// representation; silently freezing them would break palette replacement.
function observedPaint(graph, collection, snapshot, observation, root, property) {
  const fill = paint(root.style[property]), source = root.paintSources[property]
  requireComponent(Array.isArray(source?.tokens), 'paint dependency evidence required')
  if (source.tokens.length === 0) {
    requireComponent(source.directCandidate === null, 'literal paint has contradictory alias evidence')
    return { fills: [fill], boundVariables: {} }
  }
  requireComponent(source.tokens.length === 1 && source.directCandidate === source.tokens[0],
    'mixed or derived paint dependencies need further native conversion')
  const variables = graph.getVariablesForCollection(collection.id).filter(item => item.name === source.directCandidate)
  requireComponent(variables.length === 1 && variables[0].type === 'COLOR', 'one matching native color variable required')
  const variable = variables[0]
  for (const theme of snapshot.themes) {
    const tokens = theme.tokens.filter(item => item.name === source.directCandidate)
    const modes = collection.modes.filter(item => item.name === theme.mode)
    requireComponent(tokens.length === 1 && tokens[0].type === 'color' && modes.length === 1,
      'source color and native mode identities must be unambiguous')
    const expected = parseColor(tokens[0].value)
    requireComponent(sameColor(graph.resolveVariable(variable.id, modes[0].modeId), expected),
      'native color variable differs from the source palette')
    if (theme.mode === observation.mode) {
      // Chromium serializes alpha to a limited decimal precision.
      requireComponent(sameColor(fill.color, expected, 0.00051), 'observed paint differs from its source token')
    }
  }
  return { fills: [fill], boundVariables: { 'fills/0/color': variable.id } }
}

// Construct a native component from an existing source observation. This first
// layout boundary is a single row of explicitly bound text. Paint aliases use
// the caller's native collection; icon/slot replacement and interaction follow.
export async function materializeComponent(graph, parentId, snapshot, observation, faces, renderer, colorCollectionId) {
  requireComponent(['FRAME', 'CANVAS'].includes(graph.getNode(parentId)?.type), 'existing definition parent required')
  requireComponent(snapshot?.schema === 'platformkit.design-export.v1' && /^[a-f0-9]{64}$/.test(snapshot.sha256) &&
    observation?.sourceSHA === snapshot.sha256, 'observation must identify the selected source snapshot')
  const examples = snapshot.examples.filter(item => item.id === observation.exampleId)
  requireComponent(examples.length === 1 && examples[0].componentId === observation.componentId, 'one matching source invocation required')
  requireComponent(snapshot.themes.some(theme => theme.mode === observation.mode), 'unknown observed theme')
  const example = examples[0]
  const collection = graph.variableCollections.get(colorCollectionId)
  requireComponent(collection, 'explicit native color collection required')
  const modes = collection.modes.filter(item => item.name === observation.mode)
  requireComponent(modes.length === 1 && graph.getNodeVariableModeId(parentId, collection.id) === modes[0].modeId,
    'definition parent variable mode must match the observation')
  requireComponent(observation.roots.length === 1 && observation.roots[0].kind === 'element', 'one component root required')
  const root = observation.roots[0], style = root.style
  requireComponent(['inline-flex', 'flex'].includes(style.display) && style['flex-direction'] === 'row' &&
    style['flex-wrap'] === 'nowrap' && style['justify-content'] === 'center' && style['align-items'] === 'center',
  'centered, nonwrapping row layout required')
  requireComponent(['static', 'relative'].includes(style.position) && style.transform === 'none' &&
    style.visibility === 'visible' && style['background-image'] === 'none' && style['box-shadow'] === 'none' &&
    style['animation-name'] === 'none', 'positioning, effects or motion require further native conversion')
  requireComponent(['top', 'right', 'bottom', 'left'].every(side => pixels(style[`margin-${side}`]) === 0),
    'external margins require a composing parent')
  requireComponent(root.sizing.width === 'auto' && root.sizing.height === 'auto' &&
    ['auto', '0px'].includes(root.sizing['min-width']) && ['auto', '0px'].includes(root.sizing['min-height']) &&
    root.sizing['max-width'] === 'none' && root.sizing['max-height'] === 'none', 'constrained sizing requires parent layout conversion')
  requireComponent(root.children.length === 1 && root.children[0].kind === 'text',
    'adjacent text regions, icons, nested elements and named slots require their own native composition mapping')
  requireComponent(root.children.some(child => Object.hasOwn(child, 'property')), 'explicit source text properties required')
  requireComponent(style['white-space'] === 'normal' && style['text-transform'] === 'none' &&
    style['text-decoration-line'] === 'none' && style['font-feature-settings'] === 'normal' &&
    style['font-variation-settings'] === 'normal' && style['font-stretch'] === '100%', 'text transformations require further conversion')
  const supplied = validateFonts(faces)
  const fontSize = pixels(style['font-size']), lineHeight = pixels(style['line-height'])
  requireComponent(fontSize > 0 && lineHeight > 0, 'positive text metrics required')
  const letterSpacing = style['letter-spacing'] === 'normal' ? 0 : pixels(style['letter-spacing'])
  const texts = root.children.map(region => {
    requireComponent(region.text !== '' && region.text === region.text.replace(/[\t\n\r\f ]+/g, ' ').replace(/^ | $/g, '') &&
      region.rects.length === 1, 'empty, collapsed-whitespace or multiline text needs additional layout semantics')
    requireComponent(region.fonts.length === 1 && region.fonts[0].isCustomFont, 'one actual supplied face per text region required')
    const matches = supplied.filter(face => face.postscriptName === region.fonts[0].postScriptName &&
      face.weight === Number(style['font-weight']) && face.style === style['font-style'])
    requireComponent(matches.length === 1, 'actual text face is missing or synthesized')
    const face = matches[0]
    requireComponent(observation.fontFaces.some(item => item.family === face.family && item.weight === face.weight &&
      item.style === face.style && item.sha256 === face.sha256), 'observed and supplied font bytes differ')
    return { region, face }
  })
  const insets = Object.fromEntries(['Top', 'Right', 'Bottom', 'Left'].map(side => {
    const key = side.toLowerCase(), border = pixels(style[`border-${key}-width`])
    requireComponent(border === 0 || color(style[`border-${key}-color`]).a === 0,
      'visible border styles require independent native paint fidelity')
    // Native layout ignores strokesIncludedInLayout. Retain used CSS border
    // space even when the border itself is transparent.
    return [`padding${side}`, pixels(style[`padding-${key}`]) + border]
  }))
  const masterProps = {
    name: example.name, width: root.bounds.width, height: root.bounds.height,
    layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    primaryAxisAlign: 'CENTER', counterAxisAlign: 'CENTER', layoutWrap: 'NO_WRAP',
    itemSpacing: pixels(style['column-gap']), counterAxisSpacing: pixels(style['row-gap']), ...insets,
    ...observedPaint(graph, collection, snapshot, observation, root, 'background-color'), opacity: Number(style.opacity),
    independentCorners: true,
    topLeftRadius: pixels(style['border-top-left-radius']), topRightRadius: pixels(style['border-top-right-radius']),
    bottomLeftRadius: pixels(style['border-bottom-left-radius']), bottomRightRadius: pixels(style['border-bottom-right-radius']),
    pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
      schema: snapshot.schema, sha256: snapshot.sha256, exampleId: example.id, componentId: example.componentId,
      mode: observation.mode, scope: 'text-component-observed-aliases',
      environment: observation.environment, viewport: observation.viewport,
      fontFaces: observation.fontFaces, props: example.props,
    }) }],
  }
  const textPaint = observedPaint(graph, collection, snapshot, observation, root, 'color')
  let master
  try {
    await loadFonts(faces, texts.map(({ region, face }) => ({
      family: face.family, weight: face.weight, style: face.style, text: region.text,
    })))
    await renderer.loadFonts()
    master = graph.createNode('COMPONENT', parentId, masterProps)
    const targets = []
    for (const { region, face } of texts) {
      const nativeNode = graph.createNode('TEXT', master.id, {
        name: region.property ?? 'Text', text: region.text,
        width: region.bounds.width, height: lineHeight,
        fontFamily: face.family, fontWeight: face.weight, italic: face.style === 'italic',
        fontSize, lineHeight, letterSpacing, textAutoResize: 'WIDTH_AND_HEIGHT', ...structuredClone(textPaint),
      })
      if (Object.hasOwn(region, 'property')) targets.push({ region, nativeNode })
    }
    const properties = bindTextProperties(graph, master, example, targets)
    // The hook is process-wide: own it only during synchronous native layout,
    // never across font-loading awaits that allow another caller to replace it.
    const previousMeasurer = getTextMeasurer()
    try {
      setTextMeasurer((node, maxWidth) => {
        const measured = renderer.measureTextNode(node, maxWidth)
        requireComponent(measured && Number.isFinite(measured.width) && measured.width >= 0 &&
          Number.isFinite(measured.height) && measured.height > 0, 'valid native text measurement required')
        return measured
      })
      computeAllLayouts(graph, master.id)
    } finally { setTextMeasurer(previousMeasurer) }
    requireComponent(Math.abs(master.width - root.bounds.width) <= 1 / 64 && Math.abs(master.height - root.bounds.height) <= 1 / 64,
      'native geometry differs from the observed rendering environment')
    return { master, properties }
  } catch (error) {
    if (master) graph.deleteNode(master.id)
    throw error
  }
}
