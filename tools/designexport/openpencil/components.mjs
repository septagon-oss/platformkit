import { parseColor } from '@open-pencil/core/color'
import { computeAllLayouts, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { bindComponentProperties } from './bindings.mjs'
import { loadFonts, validateFonts } from './fonts.mjs'
import { planIcon } from './icon-composition.mjs'
import { computedColor as color } from './computed-color.mjs'

// The exact owning helper is version/source-pinned by the adapter correction.
const { textAutoResizeChanges } = await import(new URL('./editor/text/auto-resize.js', import.meta.resolve('@open-pencil/core')))

function requireComponent(condition, message) {
  if (!condition) throw new Error(`Native component: ${message}`)
}

function pixels(value) {
  requireComponent(typeof value === 'string' && /^\d+(?:\.\d+)?px$/.test(value), `unsupported length ${value}`)
  return Number.parseFloat(value)
}

const paint = value => ({ type: 'SOLID', color: color(value), opacity: 1, visible: true })

function requirePlainText(style) {
  requireComponent(style['text-shadow'] === 'none' && style['text-indent'] === '0px' &&
    ['normal', '0px'].includes(style['word-spacing']) && style['writing-mode'] === 'horizontal-tb' && style.direction === 'ltr',
  'text shadows, indentation, word spacing or writing direction require further conversion')
}

// CSS requests select a face; they do not rename its binary weight. For static
// custom faces, this Chromium revision synthesizes bold only when the selected
// weight is below 600 and the request is at least 600. Keep that inference scoped
// to the verified browser; explicit synthesis refusal needs no engine inference.
// chromium@782af9cb: core/css/css_segmented_font_face.cc, GetFontData.
function matchesTextFace(face, observed, style, environment) {
  if (face.postscriptName !== observed.postScriptName || face.style !== style['font-style']) return false
  const weight = Number(style['font-weight'])
  if (!Number.isFinite(weight) || weight < 1 || weight > 1000) return false
  if (face.weight === weight) return true
  const synthesis = style['font-synthesis-weight']
  if (synthesis === 'none') return true
  if (synthesis !== 'auto' || environment?.protocol !== '1.3' ||
      !/^(?:Headless)?Chrome\/151\.0\.7922\.34$/.test(environment?.browser ?? '')) return false
  return face.weight >= 600 || weight < 600
}

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

// Read-only presentation planning is shared by text rows and composed frames.
// Layout-specific constraints remain with the owning construction path.
function planPresentation(node, paintFor, blockMargins = false) {
  const style = node.style
  requirePlainText(style)
  requireComponent(['static', 'relative'].includes(style.position) && style.visibility === 'visible' &&
    style.transform === 'none' && style.filter === 'none' && style['background-image'] === 'none' &&
    style['box-shadow'] === 'none' && style['animation-name'] === 'none', 'positioning, filters, effects or motion require further conversion')
  requireComponent(['none', 'hidden'].includes(style['outline-style']) || pixels(style['outline-width']) === 0 ||
    color(style['outline-color']).a === 0, 'visible outlines require further conversion')
  requireComponent((blockMargins ? ['right', 'left'] : ['top', 'right', 'bottom', 'left']).every(side => pixels(style[`margin-${side}`]) === 0),
    'external margins require further layout conversion')
  requireComponent(style['text-transform'] === 'none' && style['text-decoration-line'] === 'none' &&
    style['font-feature-settings'] === 'normal' && style['font-variation-settings'] === 'normal' &&
    style['font-stretch'] === '100%', 'text transformations require further conversion')
  const sides = ['top', 'right', 'bottom', 'left']
  const borders = sides.map(side => ({ width: pixels(style[`border-${side}-width`]),
    style: style[`border-${side}-style`], color: style[`border-${side}-color`] }))
  const visible = borders.some(border => border.width > 0 && color(border.color).a > 0)
  const strokes = visible ? paintFor(node, 'border-top-color') : null
  if (visible) {
    requireComponent(borders.every(border => border.style === 'solid' && border.width === borders[0].width &&
      sameColor(color(border.color), color(borders[0].color))), 'uniform solid borders required')
    for (const side of sides.slice(1)) requireComponent(JSON.stringify(paintFor(node, `border-${side}-color`)) ===
      JSON.stringify(strokes), 'border aliases must match on every side')
  }
  // Native layout ignores strokesIncludedInLayout. Account for used CSS border
  // space exactly once in the padding, even when the border is transparent.
  const insets = Object.fromEntries(sides.map((side, index) => [
    `padding${side[0].toUpperCase()}${side.slice(1)}`, pixels(style[`padding-${side}`]) + borders[index].width,
  ]))
  const background = paintFor(node, 'background-color')
  return {
    ...background, ...insets, opacity: Number(style.opacity),
    independentCorners: true,
    topLeftRadius: pixels(style['border-top-left-radius']), topRightRadius: pixels(style['border-top-right-radius']),
    bottomLeftRadius: pixels(style['border-bottom-left-radius']), bottomRightRadius: pixels(style['border-bottom-right-radius']),
    ...(strokes ? {
      strokes: strokes.fills.map(fill => ({ ...fill, weight: borders[0].width, align: 'INSIDE' })),
      boundVariables: { ...background.boundVariables, ...Object.fromEntries(Object.entries(strokes.boundVariables)
        .map(([field, value]) => [field.replace('fills/', 'strokes/'), value])) },
    } : {}),
  }
}

// Construct one observed text row with explicit named SVG slots. The caller
// supplies exact foundation handles; source names never locate native layers.
export async function materializeComponent(graph, parentId, snapshot, observation, faces, renderer, colorCollectionId, iconTargets = []) {
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
  const root = observation.roots[0]
  if (root.source && (root.style.display === 'block' || root.children.some(child => child.kind === 'element'))) {
    return materializeComposition(graph, parentId, snapshot, observation, faces, renderer, collection, example, root)
  }
  const result = await materializeTextRow(graph, parentId, snapshot, observation, faces, renderer, collection, example, root, [example.id], iconTargets)
  return { ...result, components: [{ path: [example.id], ...result }] }
}

async function materializeTextRow(graph, parentId, snapshot, observation, faces, renderer, collection, example, root, definitionPath, iconTargets = []) {
  const style = root.style
  const presentation = planPresentation(root, (node, property) => observedPaint(graph, collection, snapshot, observation, node, property))
  requireComponent(['inline-flex', 'flex'].includes(style.display) && style['flex-direction'] === 'row' &&
    style['flex-wrap'] === 'nowrap' && style['justify-content'] === 'center' && style['align-items'] === 'center',
  'centered, nonwrapping row layout required')
  requireComponent(root.sizing.width === 'auto' && root.sizing.height === 'auto' &&
    ['auto', '0px'].includes(root.sizing['min-width']) && ['auto', '0px'].includes(root.sizing['min-height']) &&
    root.sizing['max-width'] === 'none' && root.sizing['max-height'] === 'none', 'constrained sizing requires parent layout conversion')
  const textRegions = root.children.filter(child => child.kind === 'text')
  const slots = root.children.filter(child => child.kind === 'slot')
  requireComponent(textRegions.length === 1 && root.children.length === textRegions.length + slots.length &&
    Array.isArray(iconTargets) && iconTargets.length === slots.length &&
    iconTargets.every(target => slots.includes(target?.region)),
  'one text region and explicit construction handles for named slots required')
  requireComponent(root.children.some(child => Object.hasOwn(child, 'property')), 'explicit source text properties required')
  const icons = new Map(slots.map(region => {
    const targets = iconTargets.filter(target => target.region === region)
    requireComponent(targets.length === 1 && region.children.length === 1, 'one unambiguous icon target per named slot required')
    const svg = region.children[0]
    const assets = snapshot.icons.filter(asset => asset.name === svg.icon?.canonicalName)
    requireComponent(svg.kind === 'element' && svg.tag === 'svg' && assets.length === 1, 'one canonical source SVG required')
    return [region, planIcon(graph, assets[0], svg, targets[0].master, collection.id,
      (node, property) => observedPaint(graph, collection, snapshot, observation, node, property))]
  }))
  requireComponent(style['white-space'] === 'normal', 'text-row whitespace requires further conversion')
  const supplied = validateFonts(faces)
  const fontSize = pixels(style['font-size']), lineHeight = pixels(style['line-height'])
  requireComponent(fontSize > 0 && lineHeight > 0, 'positive text metrics required')
  const letterSpacing = style['letter-spacing'] === 'normal' ? 0 : pixels(style['letter-spacing'])
  const texts = textRegions.map(region => {
    requireComponent(region.text !== '' && region.text === region.text.replace(/[\t\n\r\f ]+/g, ' ').replace(/^ | $/g, '') &&
      region.rects.length === 1, 'empty, collapsed-whitespace or multiline text needs additional layout semantics')
    requireComponent(region.fonts.length === 1 && region.fonts[0].isCustomFont, 'one actual supplied face per text region required')
    const matches = supplied.filter(face => matchesTextFace(face, region.fonts[0], style, observation.environment))
    requireComponent(matches.length === 1, 'actual text face is missing or synthesized')
    const face = matches[0]
    requireComponent(observation.fontFaces.some(item => item.family === face.family && item.weight === face.weight &&
      item.style === face.style && item.sha256 === face.sha256), 'observed and supplied font bytes differ')
    return { region, face }
  })
  const masterProps = {
    name: example.name || example.id, width: root.bounds.width, height: root.bounds.height,
    layoutMode: 'HORIZONTAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
    primaryAxisAlign: 'CENTER', counterAxisAlign: 'CENTER', layoutWrap: 'NO_WRAP',
    itemSpacing: pixels(style['column-gap']), counterAxisSpacing: pixels(style['row-gap']), ...presentation,
    pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
      schema: snapshot.schema, sha256: snapshot.sha256, exampleId: example.id, componentId: example.componentId,
      mode: observation.mode, scope: icons.size ? 'text-and-icon-component-observed-aliases' : 'text-component-observed-aliases',
      environment: observation.environment, viewport: observation.viewport,
      fontFaces: observation.fontFaces, props: example.props, definitionPath,
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
    for (const region of root.children) {
      const icon = icons.get(region)
      let nativeNode
      if (icon) {
        nativeNode = graph.createInstance(icon.master.id, master.id, { name: region.name, uniformScaleFactor: icon.scale })
        for (const { sourceNode, field, variableId } of icon.paints) {
          const children = graph.getChildren(nativeNode.id).filter(child => child.componentId === sourceNode.id)
          requireComponent(children.length === 1, 'one exact native vector occurrence required')
          graph.bindVariable(children[0].id, field, variableId)
        }
      } else {
        const { face } = texts.find(text => text.region === region)
        nativeNode = graph.createNode('TEXT', master.id, {
          name: region.property, text: region.text, width: region.bounds.width, height: lineHeight,
          fontFamily: face.family, fontWeight: face.weight, italic: face.style === 'italic',
          fontSize, lineHeight, letterSpacing, textAutoResize: 'WIDTH_AND_HEIGHT', ...structuredClone(textPaint),
          pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
            schema: snapshot.schema, sha256: snapshot.sha256, scope: 'source-composition-layout',
          }) }],
        })
      }
      targets.push({ region, nativeNode })
    }
    const properties = bindComponentProperties(graph, master, example, targets)
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
    for (const { region, nativeNode } of targets.filter(target => target.region.kind === 'slot')) {
      const expected = region.children[0].bounds
      requireComponent(['width', 'height', 'x', 'y'].every(field => Math.abs(nativeNode[field] -
        (expected[field] - (['x', 'y'].includes(field) ? root.bounds[field] : 0))) <= 1 / 64),
      'native icon geometry differs from the source slot')
    }
    return { master, properties }
  } catch (error) {
    if (master) graph.deleteNode(master.id)
    throw error
  }
}

// This is an export-local native construction plan, not a component registry or
// another renderer. Every boundary comes from a captured source occurrence and
// every supported layout/paint comes from that occurrence's browser observation.
function planComposition(graph, snapshot, observation, faces, collection, example, root) {
  const supplied = validateFonts(faces), requirements = [], occurrences = new Map(), seen = new Set()
  function describe(description, path, slot) {
    const key = JSON.stringify(path)
    requireComponent(!occurrences.has(key), 'duplicate source occurrence path')
    requireComponent(description.propsEditable && !(description.opaqueSlots?.length), 'typed, nonopaque source composition required')
    occurrences.set(key, { description, path, slot })
    for (const child of description.children ?? []) {
      const declarations = description.slots?.filter(declaration => declaration.name === child.slot) ?? []
      const declaration = declarations[0]
      requireComponent(declarations.length === 1 && declaration.supported === true && declaration.trustedOnly === true && child.span &&
        Number.isSafeInteger(child.span.start) && Number.isSafeInteger(child.span.end) && child.span.start >= 0 && child.span.end >= child.span.start &&
        (declaration.goType === 'gomponents.Node' && declaration.multiple === false ||
          declaration.goType === '[]gomponents.Node' && declaration.multiple === true), 'composition needs observed, supported source slots')
      describe(child.description, [...path, child.description.id], child.slot)
    }
  }
  describe(example, [example.id])
  const paintFor = (node, property) => observedPaint(graph, collection, snapshot, observation, node, property)
  const near = (a, b) => Number.isFinite(a) && Number.isFinite(b) && Math.abs(a - b) <= 1 / 64

  function text(region, node, { control = false, wrapping = false } = {}) {
    const style = node.style, value = control ? region.value : region.text
    requireComponent(typeof value === 'string' && !/[\r\n\t]/.test(value), 'composition requires single-line text')
    if (wrapping) requireComponent(value !== '' && value === value.replace(/[\t\n\r\f ]+/g, ' ').replace(/^ | $/g, ''),
      'text blocks need nonempty text without collapsed whitespace')
    if (!control) requireComponent(region.rects?.length > 0 && (wrapping || region.rects.length === 1) && region.bounds.height > 0,
      'composition text needs observed lines')
    const observed = region.fonts
    requireComponent(Array.isArray(observed), 'composition text requires actual font evidence')
    const weight = Number(style['font-weight'])
    let matching
    if (control && value === '') {
      requireComponent(observed.length === 0, 'empty control must not invent glyph evidence')
      const firstFamily = style['font-family'].split(',')[0].trim().replace(/^(["'])(.*)\1$/, '$2')
      matching = supplied.filter(face => face.family === firstFamily && face.weight === weight && face.style === style['font-style'])
    } else {
      requireComponent(observed.length === 1 && observed[0].isCustomFont, 'composition text requires one supplied actual face')
      matching = supplied.filter(face => matchesTextFace(face, observed[0], style, observation.environment))
    }
    requireComponent(matching.length === 1, 'composition text face is missing or synthesized')
    const face = matching[0]
    requireComponent(observation.fontFaces.some(item => item.family === face.family && item.weight === face.weight &&
      item.style === face.style && item.sha256 === face.sha256), 'composition observed and supplied fonts differ')
    const lineHeight = pixels(style['line-height']), fontSize = pixels(style['font-size'])
    requireComponent(lineHeight > 0 && fontSize > 0, 'composition text requires positive font metrics')
    requirements.push({ family: face.family, weight: face.weight, style: face.style, text: value })
    return { kind: 'text', region, observation: control ? null : region, control, wrapping, native: {
      name: region.property ?? 'Source text', text: value, width: control ? 0 : region.bounds.width, height: lineHeight,
      fontFamily: face.family, fontWeight: face.weight, italic: face.style === 'italic', fontSize, lineHeight,
      letterSpacing: style['letter-spacing'] === 'normal' ? 0 : pixels(style['letter-spacing']),
      textAutoResize: wrapping ? 'HEIGHT' : 'WIDTH_AND_HEIGHT',
      ...(control ? { layoutPositioning: 'ABSOLUTE', x: 0, y: 0 } : {}),
      ...paintFor(node, 'color'),
    } }
  }

  function inline(node) {
    const native = planPresentation(node, paintFor)
    requireComponent(['block', 'inline'].includes(node.style.display) &&
      Object.entries(native).filter(([key]) => key.startsWith('padding')).every(([, value]) => value === 0) &&
      !native.strokes && native.fills.every(fill => fill.color.a === 0) && native.opacity === 1,
    'inline fragments need undecorated single-line presentation')
    const children = node.children.flatMap(child => {
      if (child.kind === 'text') return [text(child, node)]
      requireComponent(child.kind === 'element' && !child.source && child.style.display === 'inline',
        'inline composition cannot flatten a source component or non-inline child')
      return inline(child).children
    })
    requireComponent(children.length > 0 && children.every(child => near(child.native.height, children[0].native.height)),
      'inline fragments require one shared line height')
    let next = node.bounds.x
    for (const child of children) {
      requireComponent(near(child.observation.bounds.x, next), 'inline fragments require contiguous observed advances')
      next += child.observation.bounds.width
    }
    return { kind: 'frame', observation: node, inline: true, children, native: {
      name: node.tag, width: node.bounds.width, height: children[0].native.height,
      layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FILL', counterAxisSizing: 'HUG',
      primaryAxisAlign: 'MIN', counterAxisAlign: 'CENTER', itemSpacing: 0, ...native,
    } }
  }

  function element(node, owner, isRoot = false, blockMargins = false) {
    requireComponent(node?.kind === 'element', 'composition requires explicit element roots')
    let occurrence
    if (node.source) {
      occurrence = occurrences.get(JSON.stringify(node.source.path))
      requireComponent(occurrence && occurrence.description.componentId === node.source.componentId &&
        occurrence.slot === node.source.slot && !seen.has(JSON.stringify(occurrence.path)), 'composition source correspondence is invalid or repeated')
      if (owner) requireComponent(JSON.stringify(occurrence.path.slice(0, -1)) === JSON.stringify(owner.path),
        'composition source child belongs to another owner')
      seen.add(JSON.stringify(occurrence.path))
      owner = occurrence
    }
    requireComponent(owner && (!isRoot || occurrence), 'composition requires exact source root ownership')
    requireComponent(!node.component || occurrence, 'composition cannot flatten an uncaptured component boundary')
    if (occurrence && ['flex', 'inline-flex'].includes(node.style.display) &&
      node.children.every(child => ['text', 'slot'].includes(child.kind))) {
      requireComponent(node.children.every(child => child.kind === 'text'), 'nested SVG slots require explicit native construction handles')
      return { kind: 'component', occurrence, observation: node, textRow: true }
    }
    const native = planPresentation(node, paintFor, blockMargins), style = node.style
    requireComponent(['auto', '100%'].includes(node.sizing.width) && node.sizing.height === 'auto' &&
      ['auto', '0px'].includes(node.sizing['min-width']) && ['auto', '0px'].includes(node.sizing['min-height']) &&
      node.sizing['max-width'] === 'none' && node.sizing['max-height'] === 'none', 'composition constrained sizing requires further conversion')
    let plan
    if (node.control) {
      requireComponent(node.tag === 'input' && node.control.kind === 'control' && node.control.type === 'text' &&
        node.control.property === 'value' && node.children.length === 0, 'one explicitly bound native text control required')
      requireComponent(node.control.placeholder === '', 'control placeholder layout and paint require further conversion')
      requireComponent(style['text-align'] === 'start' || style['text-align'] === 'left', 'control text alignment requires further conversion')
      const value = text(node.control, node, { control: true })
      // A browser text input has a fixed one-line content viewport, not a
      // wrapping paragraph. Its actual value remains an editable native TEXT.
      const viewport = { kind: 'frame', children: [value], native: {
        name: 'Input content viewport', width: node.bounds.width - native.paddingLeft - native.paddingRight,
        height: value.native.lineHeight, layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FILL', counterAxisSizing: 'FIXED',
        clipsContent: true, fills: [],
      } }
      plan = { kind: 'frame', observation: node, children: [viewport], native: {
        name: 'Source input', width: node.bounds.width, height: value.native.lineHeight + native.paddingTop + native.paddingBottom,
        layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FILL', counterAxisSizing: 'FIXED',
        primaryAxisAlign: 'MIN', counterAxisAlign: 'CENTER', clipsContent: true, ...native,
      } }
    } else if (style.display === 'block' && node.children.length === 1 && node.children[0].kind === 'text') {
      requireComponent(style['white-space'] === 'normal' && ['start', 'left'].includes(style['text-align']) &&
        style['overflow-x'] === 'visible' && style['overflow-y'] === 'visible',
      'text blocks require normal wrapping, left alignment and visible overflow')
      const value = text(node.children[0], node, { wrapping: true })
      value.native.width = node.bounds.width - native.paddingLeft - native.paddingRight
      value.native.height = node.bounds.height - native.paddingTop - native.paddingBottom
      value.native.layoutAlignSelf = 'STRETCH'
      plan = { kind: 'frame', textBlock: true, observation: node, children: [value], native: {
        name: node.tag, width: node.bounds.width, height: node.bounds.height,
        layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED',
        primaryAxisAlign: 'MIN', counterAxisAlign: 'STRETCH', ...native,
      } }
    } else if (style.display === 'block' && node.children.some(child => child.kind === 'element' && child.style.display === 'block')) {
      requireComponent(node.children.every(child => child.kind === 'element' && child.style.display === 'block' && child.bounds.height > 0),
        'block flow requires nonempty block children without mixed inline content')
      requireComponent([node, ...node.children].every(item => item.style.float === 'none' && item.style.clear === 'none' &&
        item.style.position === 'static' && ['auto', '1'].includes(item.style['column-count']) && item.style['column-width'] === 'auto'),
      'block flow requires ordinary unfragmented layout without floats or clearance')
      const margins = node.children.map(child => [pixels(child.style['margin-top']), pixels(child.style['margin-bottom'])])
      requireComponent(margins[0][0] === 0 && margins.at(-1)[1] === 0, 'outer block margins require parent-collapse conversion')
      const gaps = margins.slice(1).map(([top], index) => Math.max(top, margins[index][1]))
      requireComponent(gaps.every(gap => gap === gaps[0]), 'nonuniform block margins require individual native spacing')
      const children = node.children.map(child => element(child, owner, false, true))
      for (const child of children) child.placement = {
        layoutAlignSelf: 'STRETCH', [child.native.layoutMode === 'VERTICAL' ? 'counterAxisSizing' : 'primaryAxisSizing']: 'FILL',
      }
      plan = { kind: 'frame', blockFlow: true, observation: node, children, native: {
        name: node.tag, width: node.bounds.width, height: node.bounds.height,
        layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED',
        primaryAxisAlign: 'MIN', counterAxisAlign: 'STRETCH', itemSpacing: gaps[0] ?? 0, ...native,
      } }
    } else if (['block', 'inline'].includes(style.display)) {
      plan = inline(node)
    } else {
      const wrapping = style['flex-wrap'] === 'wrap'
      requireComponent(['flex', 'inline-flex'].includes(style.display) && ['row', 'column'].includes(style['flex-direction']) &&
        (style['flex-wrap'] === 'nowrap' || wrapping && style['flex-direction'] === 'row'),
      'composition requires nonwrapping flex or forward-wrapping row layout')
      requireComponent(!wrapping || ['normal', 'stretch', 'flex-start'].includes(style['align-content']),
        'wrapping row content alignment requires further conversion')
      const justify = { normal: 'MIN', 'flex-start': 'MIN', 'flex-end': 'MAX', center: 'CENTER', 'space-between': 'SPACE_BETWEEN' }[style['justify-content']]
      const align = { normal: 'STRETCH', stretch: 'STRETCH', 'flex-start': 'MIN', 'flex-end': 'MAX', center: 'CENTER' }[style['align-items']]
      requireComponent(justify && align, 'composition alignment requires further conversion')
      const vertical = style['flex-direction'] === 'column'
      plan = { kind: 'frame', observation: node, children: node.children.map(child => element(child, owner)), native: {
        name: node.tag, width: node.bounds.width, height: node.bounds.height,
        layoutMode: vertical ? 'VERTICAL' : 'HORIZONTAL',
        primaryAxisSizing: vertical ? 'HUG' : 'FIXED', counterAxisSizing: vertical ? 'FIXED' : 'HUG',
        primaryAxisAlign: justify, counterAxisAlign: align, layoutWrap: wrapping ? 'WRAP' : 'NO_WRAP',
        itemSpacing: pixels(style[vertical ? 'row-gap' : 'column-gap']),
        counterAxisSpacing: pixels(style[vertical ? 'column-gap' : 'row-gap']), ...native,
      } }
      for (const child of plan.children) {
        const childStyle = child.observation.style
        if (!vertical && child.blockFlow) {
          requireComponent(wrapping && child.observation.sizing.width === 'auto',
            'intrinsic blocks require automatic width in a wrapping row')
          child.placement = { counterAxisSizing: 'HUG' }
        }
        requireComponent(childStyle['flex-grow'] === '0' && childStyle['flex-shrink'] === '1' &&
          childStyle['flex-basis'] === 'auto' && childStyle['align-self'] === 'auto', 'composition child flex sizing requires further conversion')
        requireComponent(!wrapping || childStyle.order === '0', 'wrapping rows require source child order')
        if (vertical && align === 'STRETCH') child.placement = {
          layoutAlignSelf: 'STRETCH', [child.native?.layoutMode === 'VERTICAL' ? 'counterAxisSizing' : 'primaryAxisSizing']: 'FILL',
        }
      }
    }
    if (occurrence) return { ...plan, kind: 'component', occurrence }
    return plan
  }
  const plan = element(root, null, true)
  requireComponent(seen.size === occurrences.size, 'composition has unobserved source children')
  return { plan, requirements }
}

async function materializeComposition(graph, parentId, snapshot, observation, faces, renderer, collection, example, root) {
  const { plan, requirements } = planComposition(graph, snapshot, observation, faces, collection, example, root)
  const components = [], created = [], geometry = []
  function provenance(description, definitionPath) {
    return [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
      schema: snapshot.schema, sha256: snapshot.sha256, exampleId: description.id, componentId: description.componentId,
      mode: observation.mode, scope: 'source-composition-observed-aliases', props: description.props,
      environment: observation.environment, viewport: observation.viewport, fontFaces: observation.fontFaces, definitionPath,
    }) }]
  }
  async function component(current) {
    const { description, path } = current.occurrence
    if (current.textRow) {
      const result = await materializeTextRow(graph, parentId, snapshot, observation, faces, renderer, collection, description, current.observation, path)
      created.push(result.master.id)
      components.push({ path, ...result })
      return result.master
    }
    const master = graph.createNode('COMPONENT', parentId, {
      ...current.native, name: description.name || description.id, pluginData: provenance(description, path),
    })
    created.push(master.id)
    const targets = []
    for (const child of current.children) await construct(child, master, targets, current)
    const properties = bindComponentProperties(graph, master, description, targets)
    components.push({ path, master, properties })
    geometry.push({ plan: current, node: master })
    return master
  }
  async function construct(current, parent, targets, parentPlan) {
    if (current.kind === 'component') {
      const definition = await component(current)
      const node = graph.createInstance(definition.id, parent.id, {
        ...current.placement, name: current.occurrence.description.name || current.occurrence.description.id,
        pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
          localId: current.occurrence.description.id, slot: current.occurrence.slot,
        }) }],
      })
      geometry.push({ plan: current, node, parentPlan })
      return node
    }
    // Private text boxes need the same fill/intrinsic measurement path as linked Text masters.
    const pluginData = current.blockFlow || current.textBlock || current.wrapping || ['flex', 'inline-flex'].includes(current.observation?.style?.display) ? [{
      pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
        schema: snapshot.schema, sha256: snapshot.sha256, scope: 'source-composition-layout',
      }),
    }] : []
    const node = graph.createNode(current.kind === 'text' ? 'TEXT' : 'FRAME', parent.id, { ...current.native, ...current.placement, pluginData })
    if (current.control) {
      const previous = getTextMeasurer()
      try {
        setTextMeasurer((node, maxWidth) => renderer.measureTextNode(node, maxWidth))
        graph.updateNode(node.id, textAutoResizeChanges(node, { text: node.text }, true))
        requireComponent(Math.abs(node.height - node.lineHeight) <= 1 / 64, 'control requires one unwrapped native line')
      } finally { setTextMeasurer(previous) }
    }
    if (current.kind === 'text' && current.region.property) targets.push({ region: current.region, nativeNode: node })
    geometry.push({ plan: current, node, parentPlan })
    for (const child of current.children ?? []) await construct(child, node, targets, current)
    return node
  }
  try {
    await loadFonts(faces, requirements)
    await renderer.loadFonts()
    const master = await component(plan)
    const previousMeasurer = getTextMeasurer()
    try {
      setTextMeasurer((node, maxWidth) => {
        const measured = renderer.measureTextNode(node, maxWidth)
        requireComponent(measured && Number.isFinite(measured.width) && measured.width >= 0 &&
          Number.isFinite(measured.height) && measured.height > 0, 'composition requires working native text measurement')
        return measured
      })
      for (const item of components) computeAllLayouts(graph, item.master.id)
      computeAllLayouts(graph, master.id)
    } finally { setTextMeasurer(previousMeasurer) }
    for (const { plan: current, node, parentPlan } of geometry) {
      if (!current.observation) continue
      const expected = current.observation.bounds
      const height = current.kind === 'text' || current.inline ? current.native.height : expected.height
      const width = current.wrapping ? current.native.width : expected.width
      requireComponent(Math.abs(node.width - width) <= 1 / 64 && Math.abs(node.height - height) <= 1 / 64,
        `composition native geometry differs from source ${current.observation.tag ?? 'text'}: ${node.width}×${node.height}, expected ${width}×${height}`)
      if (parentPlan) {
        const parentBounds = parentPlan.observation.bounds
        // Wrapping TEXT owns the CSS content box, not Range's font rectangle.
        // The latter can have asymmetric leading and cannot locate a line box.
        const lineInset = current.kind === 'text' ? (height - expected.height) / 2 : 0
        const x = current.wrapping ? parentPlan.native.paddingLeft : expected.x - parentBounds.x
        const y = current.wrapping ? parentPlan.native.paddingTop : expected.y - parentBounds.y - lineInset
        requireComponent(Math.abs(node.x - x) <= 1 / 64 && Math.abs(node.y - y) <= 1 / 64,
        'composition native placement differs from the source parent')
      }
    }
    return { master, properties: components.find(item => item.master === master).properties, components }
  } catch (error) {
    for (const id of created.toReversed()) if (graph.getNode(id)) graph.deleteNode(id)
    throw error
  }
}
