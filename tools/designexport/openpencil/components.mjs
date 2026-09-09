import { computeAllLayouts, getTextMeasurer, setTextMeasurer } from '@open-pencil/core/layout'
import { bindComponentProperties, bindComponentVariants, sourceVariantContext, sourceTextValue } from './bindings.mjs'
import { loadFonts, validateFonts } from './fonts.mjs'
import { planIcon } from './icon-composition.mjs'
import { computedColor as color } from './computed-color.mjs'
import { observedPaint, sameColor, createPaintedNode, bindPaintExpressions } from './component-paints.mjs'
import { planSourceGrid } from './source-grid.mjs'
import { planSourceAbsolute } from './source-positioning.mjs'
import { planSourceBox } from './source-box.mjs'
import { cssDashIntervals } from './border-correction.mjs'

// The exact owning helper is version/source-pinned by the adapter correction.
const { textAutoResizeChanges } = await import(new URL('./editor/text/auto-resize.js', import.meta.resolve('@open-pencil/core')))

function requireComponent(condition, message) {
  if (!condition) throw new Error(`Native component: ${message}`)
}

function constructIcon(graph, parentId, icon, name, pending) {
  const node = graph.createInstance(icon.master.id, parentId, { name, uniformScaleFactor: icon.scale })
  for (const { sourceNode, field, variableId, expression } of icon.paints) {
    const children = graph.getChildren(node.id).filter(child => child.componentId === sourceNode.id)
    requireComponent(children.length === 1, 'one exact native vector occurrence required')
    if (expression) pending.push({ node: children[0], bindings: { [field]: expression } })
    else graph.bindVariable(children[0].id, field, variableId)
  }
  return node
}

function pixels(value) {
  requireComponent(typeof value === 'string' && /^\d+(?:\.\d+)?px$/.test(value), `unsupported length ${value}`)
  return Number.parseFloat(value)
}

// Definitions own intrinsic dimensions; only a witnessed parent placement can
// assign a different used size. Placed instances must match both observed axes.
function matchesSourceSize(node, bounds, placement = {}) {
  return ['width', 'height'].every(field => {
    const primary = (field === 'width') === (node.layoutMode === 'HORIZONTAL')
    return placement[primary ? 'primaryAxisSizing' : 'counterAxisSizing'] === 'FILL' ||
      Math.abs(node[field] - bounds[field]) <= 1 / 64
  })
}

function requirePlainText(style) {
  requireComponent(style['text-shadow'] === 'none' && style['text-indent'] === '0px' &&
    ['normal', '0px'].includes(style['word-spacing']) && style['writing-mode'] === 'horizontal-tb' && style.direction === 'ltr',
  'text shadows, indentation, word spacing or writing direction require further conversion')
}

function boxShadow(node) {
  const value = node.style['box-shadow']
  if (value === 'none') return []
  const source = node.paintSources?.['box-shadow']
  requireComponent(source?.tokens?.length === 0 && source.directCandidate === null && !source.expressionCandidate,
    'shadow color dependencies require further native conversion')
  const match = /^((?:rgba?|color)\([^)]*\)) (-?\d+(?:\.\d+)?px) (-?\d+(?:\.\d+)?px) (\d+(?:\.\d+)?px) 0px$/.exec(value)
  requireComponent(match && node.style.display !== 'inline', 'one non-inset zero-spread box shadow required')
  requireComponent(match.slice(2).every(value => Number.isFinite(Math.fround(Number.parseFloat(value)))), 'finite native shadow geometry required')
  return [{ type: 'DROP_SHADOW', color: color(match[1]), offset: { x: Number.parseFloat(match[2]), y: Number.parseFloat(match[3]) },
    radius: pixels(match[4]), spread: 0, visible: true, blendMode: 'NORMAL', showShadowBehindNode: false }]
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

// Read-only presentation planning is shared by text rows and composed frames.
// Layout-specific constraints remain with the owning construction path.
function numericFeatures(style) {
  requireComponent(['normal', 'tabular-nums'].includes(style['font-variant-numeric'] ?? 'normal'), 'numeric typography requires further conversion')
  return style['font-variant-numeric'] === 'tabular-nums' ? [{ tag: 'tnum', enabled: true }] : []
}

function planPresentation(node, paintFor, parentLayout = null) {
  const style = node.style
  requirePlainText(style)
  requireComponent((['static', 'relative'].includes(style.position) || style.position === 'absolute' && parentLayout === 'absolute') && style.visibility === 'visible' &&
    style.transform === 'none' && style.filter === 'none' && style['background-image'] === 'none' &&
    style['animation-name'] === 'none', 'positioning, filters, effects or motion require further conversion')
  requireComponent(['none', 'hidden'].includes(style['outline-style']) || pixels(style['outline-width']) === 0 ||
    color(style['outline-color']).a === 0, 'visible outlines require further conversion')
  requireComponent((parentLayout === 'flex' ? [] : parentLayout === 'block' ? ['right', 'left'] : ['top', 'right', 'bottom', 'left']).every(side => pixels(style[`margin-${side}`]) === 0),
    'external margins require further layout conversion')
  requireComponent(style['text-transform'] === 'none' && style['text-decoration-line'] === 'none' &&
    style['font-feature-settings'] === 'normal' && style['font-variation-settings'] === 'normal' &&
    style['font-stretch'] === '100%', 'text transformations require further conversion')
  const sides = ['top', 'right', 'bottom', 'left']
  const borders = sides.map(side => ({ width: pixels(style[`border-${side}-width`]),
    style: style[`border-${side}-style`], color: style[`border-${side}-color`] }))
  const borderPaints = borders.map((border, index) => border.width > 0 ? paintFor(node, `border-${sides[index]}-color`) : null)
  // A bound zero-alpha border can become visible after a palette edit.
  const retained = borders.some((border, index) => border.width > 0 &&
    (color(border.color).a > 0 || Object.keys(borderPaints[index].boundVariables).length > 0 || borderPaints[index].expressionBindings))
  const active = borderPaints.filter(Boolean), strokes = retained ? active[0] : null
  const radii = ['top-left', 'top-right', 'bottom-right', 'bottom-left'].map(corner => pixels(style[`border-${corner}-radius`]))
  const dashed = retained && borders.every(border => border.style === 'dashed' && border.width === borders[0].width)
  if (retained) {
    requireComponent(borders.every(border => border.width === 0 || (border.style === 'solid' || dashed) &&
      sameColor(color(border.color), color(borders.find(item => item.width > 0).color))), 'solid or uniform dashed borders with one shared paint required')
    if (dashed) requireComponent(radii.every(radius => radius === radii[0]) &&
      [node.bounds.width, node.bounds.height].every(value => value > 2 * borders[0].width),
    'dashed borders require uniform circular corners and a nonempty inner box')
    for (const borderPaint of active.slice(1)) requireComponent(JSON.stringify(borderPaint) ===
      JSON.stringify(strokes), 'border aliases must match on every side')
  }
  // Native layout ignores strokesIncludedInLayout. Account for used CSS border
  // space exactly once in the padding, even when the border is transparent.
  const insets = Object.fromEntries(sides.map((side, index) => [
    `padding${side[0].toUpperCase()}${side.slice(1)}`, pixels(style[`padding-${side}`]) + borders[index].width,
  ]))
  const background = paintFor(node, 'background-color')
  return {
    ...planSourceBox(node, borders.map(border => border.width)),
    ...background, ...insets, opacity: Number(style.opacity), effects: boxShadow(node),
    independentCorners: true,
    topLeftRadius: radii[0], topRightRadius: radii[1], bottomRightRadius: radii[2], bottomLeftRadius: radii[3],
    ...(strokes ? {
      ...(dashed ? { cssBorder: { version: 1, style: 'dashed', weight: borders[0].width },
        dashPattern: cssDashIntervals(borders[0].width) } : {}),
      strokes: strokes.fills.map(fill => ({ ...fill, weight: Math.max(...borders.map(border => border.width)), align: 'INSIDE',
        ...(dashed ? { dashPattern: cssDashIntervals(borders[0].width) } : {}),
      })),
      ...(borders.some(border => border.width !== borders[0].width) ? {
        independentStrokeWeights: true,
        ...Object.fromEntries(sides.map((side, index) => [`border${side[0].toUpperCase()}${side.slice(1)}Weight`, borders[index].width])),
      } : {}),
      boundVariables: { ...background.boundVariables, ...Object.fromEntries(Object.entries(strokes.boundVariables)
        .map(([field, value]) => [field.replace('fills/', 'strokes/'), value])) },
      ...((background.expressionBindings || strokes.expressionBindings) ? {
        expressionBindings: { ...background.expressionBindings, ...Object.fromEntries(Object.entries(strokes.expressionBindings ?? {})
          .map(([field, value]) => [field.replace('fills/', 'strokes/'), value])) },
      } : {}),
    } : {}),
  }
}

// Construct one observed text row with explicit named SVG slots. The caller
// supplies exact foundation handles; source names never locate native layers.
export async function materializeComponent(graph, parentId, snapshot, observation, faces, renderer, colorCollectionId, iconTargets = [], { variants = [] } = {}) {
  return materializeOccurrence(graph, parentId, snapshot, observation, faces, renderer, colorCollectionId, iconTargets,
    { path: [observation?.exampleId], variants })
}

// Parent sizing allowances are private construction evidence, never caller
// overrides that could bypass comparison with the observed source geometry.
async function materializeOccurrence(graph, parentId, snapshot, observation, faces, renderer, colorCollectionId, iconTargets, {
  path, placement = {}, variants = [],
}) {
  requireComponent(['FRAME', 'CANVAS'].includes(graph.getNode(parentId)?.type), 'existing definition parent required')
  requireComponent(snapshot?.schema === 'platformkit.design-export.v1' && /^[a-f0-9]{64}$/.test(snapshot.sha256) &&
    observation?.sourceSHA === snapshot.sha256, 'observation must identify the selected source snapshot')
  const examples = snapshot.examples.filter(item => item.id === observation.exampleId)
  requireComponent(examples.length === 1 && examples[0].componentId === observation.componentId, 'one matching source invocation required')
  requireComponent(snapshot.themes.some(theme => theme.mode === observation.mode), 'unknown observed theme')
  const example = sourceVariantContext(snapshot, path).example
  requireComponent(path[0] === observation.exampleId, 'definition path must belong to the observed source root')
  const collection = graph.variableCollections.get(colorCollectionId)
  requireComponent(collection, 'explicit native color collection required')
  const modes = collection.modes.filter(item => item.name === observation.mode)
  requireComponent(modes.length === 1 && graph.getNodeVariableModeId(parentId, collection.id) === modes[0].modeId,
    'definition parent variable mode must match the observation')
  requireComponent(observation.roots.length === 1 && observation.roots[0].kind === 'element', 'one component root required')
  const observed = [], visit = node => {
    if (JSON.stringify(node.source?.path) === JSON.stringify(path)) observed.push(node)
    for (const child of node.children ?? []) visit(child)
  }
  visit(observation.roots[0])
  requireComponent(observed.length === 1 && observed[0].source.componentId === example.componentId,
    'definition requires one exactly observed source occurrence')
  const root = observed[0]
  requireComponent(Array.isArray(variants), 'source variants must be an array')
  const pending = [], variables = [], families = [], familyNodes = [], projectedComponents = []
  async function finish(master, definitionPath, usedPlacement = {}) {
    const requests = variants.filter(item => JSON.stringify(item.path) === JSON.stringify(definitionPath))
    if (!requests.length) return
    const states = [{ snapshot, master }]
    for (const request of requests) {
      requireComponent(request.property === requests[0].property, 'one source property per variant family required')
      const existingVariables = new Set(graph.variables.keys())
      const built = await materializeOccurrence(graph, parentId, request.snapshot, request.observation, faces, renderer,
        colorCollectionId, request.iconTargets ?? [], { path: definitionPath, placement: usedPlacement })
      variables.push(...[...graph.variables.keys()].filter(id => !existingVariables.has(id)))
      familyNodes.push(...built.components.map(item => item.master.id))
      projectedComponents.push(...built.components)
      states.push({ snapshot: request.snapshot, master: built.master })
    }
    const family = graph.createNode('COMPONENT_SET', parentId, { name: definitionPath.join(' / '), clipsContent: false })
    familyNodes.push(family.id)
    const properties = bindComponentVariants(graph, family, snapshot, definitionPath, requests[0].property, states)
    families.push({ path: definitionPath, family, properties })
  }
  let result
  try {
    if (root.source && (root.style.display === 'block' || root.children.some(child => child.kind === 'element'))) {
      result = await materializeComposition(graph, parentId, snapshot, observation, faces, renderer, collection, example, root, pending, finish, placement, iconTargets)
    } else {
      result = await materializeTextRow(graph, parentId, snapshot, observation, faces, renderer, collection, example, root, path, iconTargets, pending, placement)
      result = { ...result, components: [{ path, ...result }] }
      await finish(result.master, path, placement)
    }
    requireComponent(variants.every(request => families.some(item => JSON.stringify(item.path) === JSON.stringify(request.path))),
      'every requested variant path must be constructed')
    result.components.push(...projectedComponents)
    for (const component of result.components) if (graph.getNode(component.master.parentId)?.type === 'COMPONENT_SET') component.properties = []
    bindPaintExpressions(graph, collection, snapshot, pending, variables)
    if (pending.length) for (const { master } of result.components) graph.syncInstances(master.id)
    const own = families.find(item => JSON.stringify(item.path) === JSON.stringify(path))
    return { ...result, families, ...(own ? { family: own.family, properties: own.properties } : {}) }
  } catch (error) {
    for (const { master } of result?.components?.toReversed() ?? []) if (graph.getNode(master.id)) graph.deleteNode(master.id)
    for (const id of familyNodes.toReversed()) if (graph.getNode(id)) graph.deleteNode(id)
    for (const id of variables.toReversed()) graph.removeVariable(id)
    throw error
  }
}

async function materializeTextRow(graph, parentId, snapshot, observation, faces, renderer, collection, example, root, definitionPath, iconTargets = [], pending, placement = {}) {
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
    master = createPaintedNode(graph, 'COMPONENT', parentId, masterProps, pending)
    const targets = []
    for (const region of root.children) {
      const icon = icons.get(region)
      let nativeNode
      if (icon) {
        nativeNode = constructIcon(graph, master.id, icon, region.name, pending)
      } else {
        const { face } = texts.find(text => text.region === region)
        nativeNode = createPaintedNode(graph, 'TEXT', master.id, {
          name: region.property, text: region.text, width: region.bounds.width, height: lineHeight,
          fontFamily: face.family, fontWeight: face.weight, italic: face.style === 'italic',
          fontSize, lineHeight, letterSpacing, fontFeatures: numericFeatures(style), textAutoResize: 'WIDTH_AND_HEIGHT', ...structuredClone(textPaint),
          pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
            schema: snapshot.schema, sha256: snapshot.sha256, scope: 'source-composition-layout',
          }) }],
        }, pending)
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
    requireComponent(matchesSourceSize(master, root.bounds, placement),
      'native geometry differs from the observed rendering environment')
    for (const { region, nativeNode } of targets.filter(target => target.region.kind === 'text')) {
      requireComponent(matchesSourceSize(nativeNode, { width: region.bounds.width, height: lineHeight }),
        'native text advance differs from the observed rendering environment')
    }
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
function planComposition(graph, snapshot, observation, faces, collection, example, root, iconTargets) {
  const supplied = validateFonts(faces), requirements = [], occurrences = new Map(), seen = new Set()
  function describe(description, path, slot) {
    const key = JSON.stringify(path)
    requireComponent(!occurrences.has(key), 'duplicate source occurrence path')
    requireComponent(description.propsEditable && !(description.opaqueSlots?.length), 'typed, nonopaque source composition required')
    occurrences.set(key, { description, path, slot })
    for (const child of description.children ?? []) {
      const declarations = description.slots?.filter(declaration => declaration.name === child.slot) ?? []
      const declaration = declarations[0]
      requireComponent(child.span &&
        Number.isSafeInteger(child.span.start) && Number.isSafeInteger(child.span.end) && child.span.start >= 0 && child.span.end >= child.span.start &&
        (child.slot === undefined || declarations.length === 1 && declaration.supported === true && declaration.trustedOnly === true &&
          (declaration.goType === 'gomponents.Node' && declaration.multiple === false ||
            declaration.goType === '[]gomponents.Node' && declaration.multiple === true)), 'composition needs observed components and supported declared slots')
      describe(child.description, [...path, child.description.id], child.slot)
    }
  }
  describe(example, root.source.path, root.source.slot)
  const paintFor = (node, property) => observedPaint(graph, collection, snapshot, observation, node, property)
  const near = (a, b) => Number.isFinite(a) && Number.isFinite(b) && Math.abs(a - b) <= 1 / 64

  function text(region, node, { control = false, wrapping = false } = {}) {
    const style = node.style, value = control ? region.value : region.text
    requireComponent(typeof value === 'string' && !(control && wrapping ? /[\r\t]/ : /[\r\n\t]/).test(value), 'composition requires supported text whitespace')
    if (wrapping && !control) requireComponent(value !== '' && value === value.replace(/[\t\n\r\f ]+/g, ' ').replace(/^ | $/g, ''),
      'text blocks need nonempty text without collapsed whitespace')
    if (!control) requireComponent(region.rects?.length > 0 && (wrapping || region.rects.length === 1) && region.bounds.height > 0,
      'composition text needs observed lines')
    const observed = region.fonts
    requireComponent(Array.isArray(observed), 'composition text requires actual font evidence')
    const weight = Number(style['font-weight'])
    let matching
    if (control && (value === '' || wrapping && /^\n+$/.test(value))) {
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
      letterSpacing: style['letter-spacing'] === 'normal' ? 0 : pixels(style['letter-spacing']), fontFeatures: numericFeatures(style),
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

  function element(node, owner, isRoot = false, parentLayout = null) {
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
    if (node.tag === 'svg' && !occurrence) {
      const targets = iconTargets.filter(target => target.region === node)
      const assets = snapshot.icons.filter(asset => asset.name === node.icon?.canonicalName)
      requireComponent(targets.length === 1 && assets.length === 1, 'private SVG needs one explicit canonical construction handle')
      return { kind: 'icon', observation: node,
        icon: planIcon(graph, assets[0], node, targets[0].master, collection.id, paintFor) }
    }
    if (occurrence && ['flex', 'inline-flex'].includes(node.style.display) && node.sizing.width === 'auto' && node.sizing.height === 'auto' &&
      node.children.every(child => ['text', 'slot'].includes(child.kind))) {
      return { kind: 'component', occurrence, observation: node, textRow: true }
    }
    const native = planPresentation(node, paintFor, parentLayout), style = node.style
    const fixedWidth = /^\d+(?:\.\d+)?px$/.test(node.sizing.width), fixedHeight = /^\d+(?:\.\d+)?px$/.test(node.sizing.height)
    requireComponent(!(fixedWidth || fixedHeight) || ['flex', 'inline-flex'].includes(style.display), 'fixed composition sizing requires flex layout')
    requireComponent((['auto', '100%'].includes(node.sizing.width) || fixedWidth && style['box-sizing'] === 'border-box' && near(pixels(node.sizing.width), node.bounds.width)) &&
      (node.sizing.height === 'auto' || fixedHeight && style['box-sizing'] === 'border-box' && near(pixels(node.sizing.height), node.bounds.height)) &&
      ['auto', '0px'].includes(node.sizing['min-width']) && ['auto', '0px'].includes(node.sizing['min-height']) &&
      node.sizing['max-width'] === 'none' && node.sizing['max-height'] === 'none', 'composition constrained sizing requires further conversion')
    let plan
    if (node.control) {
      const control = node.control, multiline = node.tag === 'textarea' && control.type === 'textarea'
      const select = node.tag === 'select' && control.type === 'select-one' && control.size <= 1
      requireComponent((select || (multiline || node.tag === 'input' && control.type === 'text') && control.property === 'value') &&
        control.kind === 'control' && node.children.length === 0, 'one explicitly bound native text or closed choice control required')
      requireComponent(select || control.placeholder === '', 'control placeholder layout and paint require further conversion')
      requireComponent(style['text-align'] === 'start' || style['text-align'] === 'left', 'control text alignment requires further conversion')
      requireComponent(!multiline || Number.isSafeInteger(node.control.rows) && node.control.rows > 0 &&
        ['', 'soft'].includes(node.control.wrap) && !node.control.controllers && !node.control.counter &&
        style['white-space'] === 'pre-wrap', 'textarea requires fixed rows, soft wrapping and no active controllers')
      if (select) {
        const fields = control.properties, selected = control.options.filter(option => option.selected)
        requireComponent(style.appearance === 'none', 'choice chrome must be source-owned')
        requireComponent(fields && new Set(Object.values(fields)).size === 3 &&
          sourceTextValue(owner.description, fields.value) === control.value &&
          (owner.description.props[fields.values] ?? []).length === 0 &&
          new Set(control.options.map(option => option.value)).size === control.options.length &&
          selected.length <= 1 && control.values.length === selected.length &&
          (selected.length ? selected[0].value === control.value && control.values[0] === control.value &&
            selected[0].label === control.content?.text : control.value === '' && control.content?.text === ''),
        'choice requires an unambiguous exact source value and observed display label')
      }
      // A choice's visible label is private presentation, never its stored value.
      const value = text(select ? { value: control.content.text, fonts: control.fonts } : control, node, { control: true, wrapping: multiline })
      const width = node.bounds.width - native.paddingLeft - native.paddingRight
      const height = value.native.lineHeight * (multiline ? node.control.rows : 1)
      if (!multiline) requireComponent(control.content && near(control.content.bounds.width, width) && near(control.content.bounds.height, height) &&
        near(control.content.bounds.x, node.bounds.x + native.paddingLeft) &&
        near(control.content.bounds.y, node.bounds.y + native.paddingTop), 'control display viewport requires further conversion')
      if (multiline) {
        requireComponent(near(node.control.content?.bounds.width, width), 'textarea scrollbar or content width requires further conversion')
        value.native.width = width
      }
      // Both controls own fixed viewports: one unwrapped input line or a
      // textarea's declared rows. Editing text does not grow the control.
      const viewport = { kind: 'frame', children: [value], native: {
        name: 'Input content viewport', width,
        height, layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FILL', counterAxisSizing: 'FIXED',
        clipsContent: true, fills: [],
      } }
      plan = { kind: 'frame', observation: node, children: [viewport], native: {
        name: `Source ${node.tag}`, width: node.bounds.width, height: height + native.paddingTop + native.paddingBottom,
        layoutMode: 'HORIZONTAL', primaryAxisSizing: 'FILL', counterAxisSizing: 'FIXED',
        primaryAxisAlign: 'MIN', counterAxisAlign: 'CENTER', clipsContent: true, ...native,
      } }
    } else if (style.display === 'block' && node.children.length === 1 && node.children[0].kind === 'text') {
      requireComponent(style['white-space'] === 'normal' && ['start', 'left'].includes(style['text-align']) &&
        style['overflow-x'] === 'visible' && style['overflow-y'] === 'visible',
      'text blocks require normal wrapping, left alignment and visible overflow')
      const value = text(node.children[0], node, { wrapping: true })
      value.native.width = node.bounds.width - native.paddingLeft - native.paddingRight
      value.native.height = value.region.rects.length * value.native.lineHeight
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
      const children = node.children.map(child => element(child, owner, false, 'block'))
      for (const child of children) child.placement = {
        layoutAlignSelf: 'STRETCH', [child.native.layoutMode === 'VERTICAL' ? 'counterAxisSizing' : 'primaryAxisSizing']: 'FILL',
      }
      plan = { kind: 'frame', blockFlow: true, observation: node, children, native: {
        name: node.tag, width: node.bounds.width, height: node.bounds.height,
        layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED',
        primaryAxisAlign: 'MIN', counterAxisAlign: 'STRETCH', itemSpacing: gaps[0] ?? 0, ...native,
      } }
    } else if (style.display === 'grid') {
      const grid = planSourceGrid(node)
      const children = node.children.map(child => element(child, owner))
      for (const [index, child] of children.entries()) {
        const { widthFill, heightFill, ...placement } = grid.children[index]
        const horizontal = child.native?.layoutMode === 'HORIZONTAL' || child.textRow
        child.placement = { ...placement,
          ...(widthFill ? { [horizontal ? 'primaryAxisSizing' : 'counterAxisSizing']: 'FILL' } : {}),
          ...(heightFill ? { [horizontal ? 'counterAxisSizing' : 'primaryAxisSizing']: 'FILL' } : {}) }
      }
      plan = { kind: 'frame', observation: node, children, native: {
        name: node.tag, width: node.bounds.width, height: node.bounds.height, ...grid.native, ...native,
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
      const gap = axis => style[`${axis}-gap`] === 'normal' ? 0 : pixels(style[`${axis}-gap`])
      const firstAbsolute = node.children.findIndex(child => child.style?.position === 'absolute')
      requireComponent(firstAbsolute < 0 || node.children.slice(firstAbsolute).every(child => child.style?.position === 'absolute'),
        'positioned paint order requires trailing absolute children')
      requireComponent(firstAbsolute < 0 || node.children.every(child => child.style?.['z-index'] === 'auto'),
        'positioned paint order requires unstacked siblings')
      const children = node.children.map(child => {
        if (child.style?.position === 'absolute') {
          const position = planSourceAbsolute(child, node)
          const content = element(child, owner, false, 'absolute')
          if (position.cssPosition.autoSize) content.placement = { counterAxisSizing: 'FILL' }
          return { kind: 'frame', positioned: true, observation: child, children: [content], native: {
            name: 'Source absolute placement', ...position, fills: [],
            layoutMode: 'VERTICAL', primaryAxisSizing: position.cssPosition.autoSize ? 'HUG' : 'FIXED', counterAxisSizing: 'FIXED',
            primaryAxisAlign: 'MIN', counterAxisAlign: 'MIN',
          } }
        }
        const content = element(child, owner, false, 'flex')
        const [top, right, bottom, left] = ['top', 'right', 'bottom', 'left'].map(side => pixels(child.style[`margin-${side}`]))
        if (![top, right, bottom, left].some(Boolean)) return content
        requireComponent(!content.textRow, 'text-row margins require placement conversion')
        if (['auto', '100%'].includes(child.sizing.width)) content.placement = {
          [content.native?.layoutMode === 'HORIZONTAL' ? 'primaryAxisSizing' : 'counterAxisSizing']: 'FILL', layoutAlignSelf: 'STRETCH',
        }
        // CSS flex margins do not collapse. A private padding frame represents
        // the derived margin box without changing the reusable child's border box.
        const bounds = { x: child.bounds.x - left, y: child.bounds.y - top,
          width: child.bounds.width + left + right, height: child.bounds.height + top + bottom }
        return { kind: 'frame', observation: { ...child, bounds }, children: [content], native: {
          name: 'Source margin box', width: bounds.width, height: bounds.height, fills: [],
          layoutMode: 'VERTICAL', primaryAxisSizing: 'HUG', counterAxisSizing: 'FIXED',
          primaryAxisAlign: 'MIN', counterAxisAlign: 'MIN',
          paddingTop: top, paddingRight: right, paddingBottom: bottom, paddingLeft: left,
        } }
      })
      plan = { kind: 'frame', observation: node, children, native: {
        name: node.tag, width: node.bounds.width, height: node.bounds.height,
        layoutMode: vertical ? 'VERTICAL' : 'HORIZONTAL',
        primaryAxisSizing: vertical && !fixedHeight ? 'HUG' : 'FIXED', counterAxisSizing: vertical || fixedHeight ? 'FIXED' : 'HUG',
        primaryAxisAlign: justify, counterAxisAlign: align, layoutWrap: wrapping ? 'WRAP' : 'NO_WRAP',
        itemSpacing: gap(vertical ? 'row' : 'column'),
        counterAxisSpacing: gap(vertical ? 'column' : 'row'), ...native,
      } }
      for (const child of plan.children) {
        if (child.positioned) continue
        const childStyle = child.observation.style
        if (!vertical && child.blockFlow) {
          requireComponent(wrapping && child.observation.sizing.width === 'auto',
            'intrinsic blocks require automatic width in a wrapping row')
          child.placement = { counterAxisSizing: 'HUG' }
        }
        const grows = !vertical && !wrapping && childStyle['flex-grow'] === '1' && ['0%', '0px'].includes(childStyle['flex-basis'])
        requireComponent((grows || childStyle['flex-grow'] === '0' && childStyle['flex-basis'] === 'auto') &&
          ['0', '1'].includes(childStyle['flex-shrink']) && childStyle['align-self'] === 'auto', 'composition child flex sizing requires further conversion')
        requireComponent(!wrapping || childStyle.order === '0', 'wrapping rows require source child order')
        if (grows || vertical && align === 'STRETCH' && ['auto', '100%'].includes(child.observation.sizing.width)) child.placement = {
          ...(!grows ? { layoutAlignSelf: 'STRETCH' } : {}),
          [child.textRow || child.native?.layoutMode === 'HORIZONTAL' ? 'primaryAxisSizing' : 'counterAxisSizing']: 'FILL',
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

async function materializeComposition(graph, parentId, snapshot, observation, faces, renderer, collection, example, root, pending, finish, placement, iconTargets) {
  const { plan, requirements } = planComposition(graph, snapshot, observation, faces, collection, example, root, iconTargets)
  plan.placement = placement
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
      const targets = iconTargets.filter(target => current.observation.children.includes(target.region))
      const result = await materializeTextRow(graph, parentId, snapshot, observation, faces, renderer, collection, description, current.observation, path, targets, pending, current.placement)
      created.push(result.master.id)
      components.push({ path, ...result })
      await finish(result.master, path, current.placement)
      return result.master
    }
    const master = createPaintedNode(graph, 'COMPONENT', parentId, {
      ...current.native, name: description.name || description.id, pluginData: provenance(description, path),
    }, pending)
    created.push(master.id)
    const targets = []
    for (const child of current.children) await construct(child, master, targets, current)
    const properties = bindComponentProperties(graph, master, description, targets)
    components.push({ path, master, properties })
    geometry.push({ plan: current, node: master, definition: true })
    await finish(master, path, current.placement)
    return master
  }
  async function construct(current, parent, targets, parentPlan) {
    if (current.kind === 'icon') {
      const node = constructIcon(graph, parent.id, current.icon, current.observation.icon.canonicalName, pending)
      geometry.push({ plan: current, node, parentPlan })
      return node
    }
    if (current.kind === 'component') {
      const definition = await component(current)
      const node = graph.createInstance(definition.id, parent.id, {
        ...current.placement, name: current.occurrence.description.name || current.occurrence.description.id,
        pluginData: [{ pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
          localId: current.occurrence.description.id, slot: current.occurrence.slot ?? '',
        }) }],
      })
      geometry.push({ plan: current, node, parentPlan })
      return node
    }
    // Private paragraphs and inline runs share linked Text's source-owned layout.
    const pluginData = current.positioned || current.blockFlow || current.textBlock || current.wrapping || current.inline || parentPlan?.inline ||
      ['flex', 'inline-flex'].includes(current.observation?.style?.display) ? [{
      pluginId: 'platformkit', key: 'platformkit.source', value: JSON.stringify({
        schema: snapshot.schema, sha256: snapshot.sha256, scope: 'source-composition-layout',
      }),
    }] : []
    const node = createPaintedNode(graph, current.kind === 'text' ? 'TEXT' : 'FRAME', parent.id, { ...current.native, ...current.placement, pluginData }, pending)
    if (current.control) {
      const previous = getTextMeasurer()
      try {
        setTextMeasurer((node, maxWidth) => renderer.measureTextNode(node, maxWidth))
        graph.updateNode(node.id, textAutoResizeChanges(node, { text: node.text }, true))
        requireComponent(current.wrapping || Math.abs(node.height - node.lineHeight) <= 1 / 64, 'control requires one unwrapped native line')
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
    for (const { plan: current, node, parentPlan, definition } of geometry) {
      if (!current.observation) continue
      const expected = current.observation.bounds
      const height = current.kind === 'text' || current.inline ? current.native.height : expected.height
      const width = current.wrapping ? current.native.width : expected.width
      requireComponent(matchesSourceSize(node, { width, height }, definition ? current.placement : undefined),
        `composition native geometry differs from source ${current.observation.tag ?? 'text'}: ${node.width}×${node.height}, expected ${width}×${height}`)
      if (current.textRow) {
        const text = graph.getChildren(node.id)[0], region = current.observation.children[0]
        requireComponent(Math.abs(text.x - (region.bounds.x - expected.x)) <= 1 / 64 &&
          Math.abs(text.y - (region.bounds.y - expected.y - (text.height - region.bounds.height) / 2)) <= 1 / 64,
          'composition text placement differs from the source parent')
      }
      if (parentPlan) {
        const parentBounds = parentPlan.observation.bounds
        // Wrapping TEXT owns the CSS content box, not Range's font rectangle.
        // The latter can have asymmetric leading and cannot locate a line box.
        const lineInset = current.kind === 'text' ? (height - expected.height) / 2 : 0
        const x = current.wrapping ? parentPlan.native.paddingLeft : expected.x - parentBounds.x
        const y = current.wrapping ? parentPlan.native.paddingTop : expected.y - parentBounds.y - lineInset
        requireComponent(!current.wrapping || Math.abs(expected.x - parentBounds.x - x) <= 1 / 64,
          'composition text placement differs from the source content box')
        requireComponent(Math.abs(node.x - x) <= 1 / 64 && Math.abs(node.y - y) <= 1 / 64,
        `composition native placement differs from the source parent: ${node.name} at ${node.x},${node.y}, expected ${x},${y}`)
      }
    }
    return { master, properties: components.find(item => item.master === master).properties, components }
  } catch (error) {
    for (const id of created.toReversed()) if (graph.getNode(id)) graph.deleteNode(id)
    throw error
  }
}
