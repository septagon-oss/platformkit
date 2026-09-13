import { fileURLToPath } from 'node:url'
import { ancestryOverrides, chain } from './exporter-correction.mjs'

export function sourceLayoutScope(graph, node) {
  return sourceLayoutRecord(graph, node)?.scope
}

export function sourceLayoutRecord(graph, node) {
  let master
  try { master = chain(graph, node, 'componentId').at(-1) } catch { return }
  return ownSourceLayoutRecord(master)
}

export function ownSourceLayoutScope(node) {
  return ownSourceLayoutRecord(node)?.scope
}

export function ownSourceLayoutRecord(node) {
  if (!Array.isArray(node?.pluginData)) return
  const entries = node.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
  if (entries.length !== 1) return
  let source
  try { source = JSON.parse(entries[0].value) } catch { return }
  return source?.schema === 'platformkit.design-export.v1' ? source : undefined
}

// Walk placed normal-flow descendants, never their reusable component masters.
export function layoutNodes(graph, frame, descend = () => true) {
  const nodes = [frame], seen = new Set([frame.id])
  for (const parent of nodes) {
    if (parent !== frame && !descend(parent)) continue
    for (const child of graph.getChildren(parent.id)) {
      if (!child.visible || child.layoutPositioning === 'ABSOLUTE') continue
      if (seen.has(child.id)) throw new Error('Cyclic native layout')
      seen.add(child.id)
      nodes.push(child)
    }
  }
  return nodes
}

export function sourceCompositionLayout(graph, node) {
  // Text rows are source-owned layout too. Their placed FILL axes must use the
  // same FIG restoration and cache invalidation as other composed components.
  return ['source-composition-observed-aliases', 'source-composition-layout',
    'text-component-observed-aliases', 'text-and-icon-component-observed-aliases'].includes(sourceLayoutScope(graph, node))
}

export function editedSourceLayout(graph, frame) {
  return frame?.source.format === 'fig' && sourceCompositionLayout(graph, frame) &&
    ['width', 'height', 'layoutMode', 'layoutWrap', 'itemSpacing', 'counterAxisSpacing',
      'primaryAxisAlign', 'counterAxisAlign', 'primaryAxisSizing', 'counterAxisSizing',
      'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft'].some(field => frame.source.editedFields.includes(field) ||
        Object.hasOwn(frame.overrides, field) || Object.hasOwn(frame.overrides, `${frame.id}:${field}`))
}

const layoutInputs = ['width', 'height', 'minWidth', 'maxWidth', 'minHeight', 'maxHeight',
  'layoutMode', 'layoutDirection', 'layoutWrap', 'primaryAxisSizing', 'counterAxisSizing',
  'primaryAxisAlign', 'counterAxisAlign', 'counterAxisAlignContent', 'itemSpacing', 'counterAxisSpacing',
  'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft', 'layoutGrow', 'layoutAlignSelf',
  'visible', 'layoutPositioning']

// Native source edit records include inherited input changes, not only local
// overrides. Follow containment to the layout that consumes those inputs;
// a fixed freeform box owns its interior independently of its outer placement.
function changedLayoutInputs(graph, frame, visiting = new Set()) {
  if (!frame || frame.layoutMode === 'NONE') return false
  if (visiting.has(frame.id)) throw new Error('Cyclic native layout dependency')
  const edited = node => node.source.editedFields.some(field => layoutInputs.includes(field))
  // Ordinary imported frames retain their upstream preservation policy for
  // their own edits; changed child inputs still require dependent placement.
  if (edited(frame) && (frame.type !== 'FRAME' || frame.source.format !== 'fig' ||
    sourceCompositionLayout(graph, frame))) return true
  const next = new Set(visiting).add(frame.id)
  return graph.getChildren(frame.id).some(child => edited(child) || child.visible &&
    child.layoutPositioning !== 'ABSOLUTE' &&
    (child.primaryAxisSizing === 'HUG' || child.counterAxisSizing === 'HUG') &&
    changedLayoutInputs(graph, child, next))
}

function relativeLayoutAxis(node, parent, dimension) {
  const primary = (dimension === 'width') === (node.layoutMode === 'HORIZONTAL')
  const sizing = primary ? node.primaryAxisSizing : node.counterAxisSizing
  const parentPrimary = (dimension === 'width') === (parent.layoutMode === 'HORIZONTAL')
  return sizing === 'FILL' || (parentPrimary ? node.layoutGrow > 0 :
    node.layoutAlignSelf === 'STRETCH' || node.layoutAlignSelf === 'AUTO' && parent.counterAxisAlign === 'STRETCH')
}

export function dependentLayoutChanged(graph, frame) {
  // Available space is an input too. A FILL/growing occurrence can resize
  // without acquiring an authored width edit; its interior must then reflow.
  const visiting = new Set()
  for (let node = frame; node && node.layoutMode !== 'NONE';) {
    if (visiting.has(node.id)) throw new Error('Cyclic native layout dependency')
    visiting.add(node.id)
    if (changedLayoutInputs(graph, node)) return true
    const parent = graph.getNode(node.parentId)
    if (!parent || !node.visible || node.layoutPositioning === 'ABSOLUTE' ||
      !['width', 'height'].some(dimension => relativeLayoutAxis(node, parent, dimension))) return false
    node = parent
  }
  return false
}

export function withDependentLayoutCaches(graph, frame, compute) {
  if (!frame || frame.layoutMode === 'NONE') return compute()
  // Project the whole affected tree before Yoga measures it. An outer-only
  // calculation must not measure a nested HUG owner against stale child boxes.
  const owners = layoutNodes(graph, frame, node => node.layoutMode !== 'NONE')
    .filter(node => dependentLayoutChanged(graph, node))
  if (!owners.length) return compute()
  const cached = new Map()
  for (const owner of owners) {
    const members = graph.getChildren(owner.id).filter(child => child.visible && child.layoutPositioning !== 'ABSOLUTE')
    for (const node of [owner, ...members]) {
      if (!node.figmaDerivedLayout) continue
      const next = { ...(cached.has(node.id) ? cached.get(node.id)[1] : node.figmaDerivedLayout) }
      const overrides = ancestryOverrides(graph, node)
      if (node !== owner) { delete next.x; delete next.y }
      for (const dimension of ['width', 'height']) {
        if (Object.hasOwn(overrides, `${node.id}:${dimension}`)) continue
        const primary = (dimension === 'width') === (node.layoutMode === 'HORIZONTAL')
        const sizing = primary ? node.primaryAxisSizing : node.counterAxisSizing
        const flexible = node !== owner && relativeLayoutAxis(node, owner, dimension)
        if (flexible || sizing === 'HUG' && dependentLayoutChanged(graph, node)) delete next[dimension]
      }
      if (Object.keys(next).length !== Object.keys(node.figmaDerivedLayout).length) {
        cached.set(node.id, [node.figmaDerivedLayout, Object.keys(next).length ? next : null])
      }
    }
  }
  graph.preserveSourceMetadataDuring(() => {
    for (const [id, [, next]] of cached) graph.updateNode(id, { figmaDerivedLayout: next })
  })
  try { return graph.preserveSourceMetadataDuring(compute) }
  catch (error) {
    graph.preserveSourceMetadataDuring(() => {
      for (const [id, [previous]] of cached) graph.updateNode(id, { figmaDerivedLayout: previous })
    })
    throw error
  }
}

// Apply after the existing grid/fragment corrections: they retain their own
// stronger sizing rules. This adds dependency-driven reflow, not another owner.
export function correctDependentLayout(source, replace) {
  source = `import { dependentLayoutChanged, withDependentLayoutCaches } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, '!preservesImportedInstanceLayout(node)',
    '(!preservesImportedInstanceLayout(node) || dependentLayoutChanged(graph, node))')
  return replace(source, 'function computeLayoutMeasured(graph, frameId) {',
    `function computeLayoutMeasured(graph, frameId) {
  return withDependentLayoutCaches(graph, graph.getNode(frameId), () => computeLayoutUncached(graph, frameId));
}
function computeLayoutUncached(graph, frameId) {`)
}

export function correctDependentLayoutApply(source, replace) {
  source = `import { dependentLayoutChanged } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, 'if (preservesImportedInstanceInternals(child)',
    'if (preservesImportedInstanceInternals(child) && !dependentLayoutChanged(graph, child)')
  return replace(source, 'const preservesImportedFrameGeometry = ',
    'const preservesImportedFrameGeometry = !dependentLayoutChanged(graph, graph.getNode(child.parentId)) && ')
}

// Layout owns temporary Yoga objects. Release them at that boundary even when
// measurement or nested layout throws.
export function correctLayout(source, replace) {
  source = `import { configureSourceFlex, configureSourceRowText } from ${JSON.stringify(fileURLToPath(new URL('./source-flex.mjs', import.meta.url)))};\n` + source
  source = `import { sourceAspectRatio, settleSourceAspectRatios } from ${JSON.stringify(fileURLToPath(new URL('./source-box.mjs', import.meta.url)))};\n` + source
  source = `import { sourceLayoutScope, sourceCompositionLayout, editedSourceLayout, layoutNodes } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  // A child laid out independently still uses its parent-resolved fill size.
  // Parent layout remains responsible for assigning that size on the next pass.
  for (const axis of ['primary', 'counter']) source = replace(source, `if (frame.${axis}AxisSizing === "FIXED")`,
    `if (frame.${axis}AxisSizing === "FIXED" || frame.${axis}AxisSizing === "FILL" && sourceCompositionLayout(graph, frame))`)
  source = replace(source, 'function configureTextLeaf(yogaChild, child, parent, fixedDerivedMainAxis = false) {', String.raw`
function sourceTextRow(graph, parent) {
  return parent.layoutMode === "HORIZONTAL" && parent.layoutWrap === "NO_WRAP" &&
    ["text-component-observed-aliases", "text-and-icon-component-observed-aliases"].includes(sourceLayoutScope(graph, parent));
}

function primaryItemGap(graph, node) {
  // CSS gap remains a minimum when free space is distributed, including the
  // decision to start a new line. Unmarked native frames keep their own rule.
  return node.primaryAxisAlign === "SPACE_BETWEEN" && !sourceCompositionLayout(graph, node) ? 0 : node.itemSpacing;
}

function intrinsicSourceWidth(graph, node) {
  for (let current = node; current; current = graph.getNode(current.parentId)) {
    if (!sourceCompositionLayout(graph, current)) return false;
    const sizing = current.layoutMode === "HORIZONTAL" ? current.primaryAxisSizing : current.counterAxisSizing;
    if (sizing !== "FILL") return sizing === "HUG";
  }
  return false;
}

function configureTextLeaf(yogaChild, child, parent, fixedDerivedMainAxis = false, graph) {`)
  source = replace(source, 'const autoResize = child.textAutoResize;',
    'const autoResize = child.textAutoResize;\n' +
    '\tif (configureSourceRowText(yogaChild, graph, child, parent, getTextMeasurer(), MeasureMode)) return;')
  source = replace(source, 'configureAutoLayoutChildSizing(yogaChild, child, parent, graph, widthSizing, heightSizing);',
    'configureAutoLayoutChildSizing(yogaChild, child, parent, graph, widthSizing, heightSizing);\n' +
    '\tconfigureSourceFlex(yogaChild, graph, child, parent, getTextMeasurer());')
  source = replace(source, 'function configureFlexContainer(yogaNode, node, direction) {',
    'function configureFlexContainer(yogaNode, node, direction, graph) {\n' +
    '\tconst ratio = sourceAspectRatio(graph, node);\n\tif (ratio !== undefined) yogaNode.setAspectRatio(ratio);')
  source = replace(source, 'configureFlexContainer(root, frame, direction);', 'configureFlexContainer(root, frame, direction, graph);\n' +
    '\tconst ratio = sourceAspectRatio(graph, frame);\n\tif (ratio !== undefined) root.setHeight(frame.width / ratio);')
  source = replace(source, 'configureFlexContainer(yogaChild, child, direction);', 'configureFlexContainer(yogaChild, child, direction, graph);')
  source = replace(source, 'const primaryGap = node.primaryAxisAlign === "SPACE_BETWEEN" ? 0 : node.itemSpacing;',
    'const primaryGap = primaryItemGap(graph, node);')
  source = replace(source, 'function sizesFitParent(parent, childCount, sizes, axis) {',
    'function sizesFitParent(parent, childCount, sizes, axis, graph) {')
  source = replace(source, 'const gap = parent.primaryAxisAlign === "SPACE_BETWEEN" ? 0 : parent.itemSpacing * Math.max(0, childCount - 1);',
    'const gap = primaryItemGap(graph, parent) * Math.max(0, childCount - 1);')
  source = replace(source, 'return sizesFitParent(parent, children.length, sizes, axis) && child.figmaDerivedLayout?.[axis] !== void 0;',
    'return sizesFitParent(parent, children.length, sizes, axis, graph) && child.figmaDerivedLayout?.[axis] !== void 0;')
  source = replace(source, 'return sizesFitParent(parent, children.length, sizes, axis);',
    'return sizesFitParent(parent, children.length, sizes, axis, graph);')
  source = replace(source, 'configureTextLeaf(yogaChild, child, parent, fixedDerivedMainAxis);',
    'configureTextLeaf(yogaChild, child, parent, fixedDerivedMainAxis, graph);')
  source = replace(source, 'const fillsWidth = !isRow && stretchesCross;', String.raw`
    const fillsWidth = !isRow && stretchesCross;
    if (fillsWidth && intrinsicSourceWidth(graph, parent)) {
      // A source block contributes max-content width before the flex row gives
      // it a used width. Its previous line box cannot supply that intrinsic size.
      let intrinsic;
      yogaChild.setMeasureFunc((width, widthMode) => {
        if (intrinsic === undefined) {
          const natural = getTextMeasurer()?.({ ...child, textAutoResize: "WIDTH_AND_HEIGHT" });
          if (!natural || !Number.isFinite(natural.width) || natural.width <= 0) throw new Error("Intrinsic source text requires actual measurement");
          intrinsic = Math.ceil(natural.width * 64) / 64;
        }
        const used = widthMode === MeasureMode.Undefined ? intrinsic :
          widthMode === MeasureMode.Exactly ? width : Math.min(width, intrinsic);
        const key = widthMode + ":" + used;
        if (cache.has(key)) return cache.get(key);
        const measured = getTextMeasurer()?.(child, used);
        if (!measured || !Number.isFinite(measured.height) || measured.height <= 0) throw new Error("Intrinsic source text requires actual measurement");
        const result = { width: used, height: measured.height };
        cache.set(key, result);
        return result;
      });
      return;
    }`)
  source = replace(source, 'const result = getTextMeasurer()?.(child, maxW) ?? estimateTextSize(child, maxW);', String.raw`
      const measured = getTextMeasurer()?.(child, maxW) ?? estimateTextSize(child, maxW);
      // Chromium's intrinsic inline box rounds up to a 1/64 CSS-pixel layout
      // unit. Do this once per source text row, not per glyph or accumulated
      // position. Ordinary native text and renderer shaping stay untouched.
      const result = sourceTextRow(graph, parent) ? { ...measured, width: Math.ceil(measured.width * 64) / 64 } : measured;`)
  source = replace(source, 'function computeLayoutInternal(graph, frameId) {', String.raw`
function resizedWrappingFrame(graph, frame) {
  if (frame?.source.format !== "fig" || !frame.source.editedFields.includes("width") ||
      frame.layoutMode !== "VERTICAL" || frame.primaryAxisSizing !== "HUG" || frame.counterAxisSizing !== "FIXED") return false;
  const children = graph.getChildren(frame.id).filter(node => node.visible && node.layoutPositioning !== "ABSOLUTE");
  return children.length > 0 && children.every(node => node.type === "TEXT" && node.textAutoResize === "HEIGHT" &&
    (node.layoutAlignSelf === "STRETCH" || node.layoutAlignSelf === "AUTO" && frame.counterAxisAlign === "STRETCH"));
}

function computeLayoutInternal(graph, frameId) {
  const frame = graph.getNode(frameId);
  if (!resizedWrappingFrame(graph, frame) && !editedSourceLayout(graph, frame)) return computeLayoutMeasured(graph, frameId);
  // A resized source occurrence invalidates descendant line boxes too. Follow
  // containment, never component links; unrelated imported layout stays opaque.
  const cached = layoutNodes(graph, frame, node => sourceCompositionLayout(graph, node))
    .map(node => [node.id, node.figmaDerivedLayout]);
  graph.preserveSourceMetadataDuring(() => {
    for (const [id, previous] of cached) if (previous) graph.updateNode(id, { figmaDerivedLayout: null });
  });
  try {
    return graph.preserveSourceMetadataDuring(() => computeLayoutMeasured(graph, frameId));
  } catch (error) {
    graph.preserveSourceMetadataDuring(() => {
      for (const [id, previous] of cached) graph.updateNode(id, { figmaDerivedLayout: previous });
    });
    throw error;
  }
}

function computeLayoutMeasured(graph, frameId) {`)
  source = replace(source, '!preservesImportedInstanceLayout(node)',
    '(!preservesImportedInstanceLayout(node) || resizedWrappingFrame(graph, node) || editedSourceLayout(graph, node))')
  return replace(source,
    '\tyogaRoot.calculateLayout(void 0, void 0, yogaDirection);\n' +
    '\tapplyYogaLayout(graph, frame, yogaRoot, computeLayoutInternal);\n' +
    '\tfreeYogaTree(yogaRoot);',
    String.raw`
  try {
    yogaRoot.calculateLayout(void 0, void 0, yogaDirection);
    if (settleSourceAspectRatios(graph, frame, yogaRoot)) yogaRoot.calculateLayout(void 0, void 0, yogaDirection);
    applyYogaLayout(graph, frame, yogaRoot, computeLayoutInternal);
  } finally {
    freeYogaTree(yogaRoot);
  }`)
}

export function correctLayoutApply(source, replace) {
  source = 'import { getTextMeasurer } from "./text-measurement.js";\n' + source
  source = `import { applySourceAbsolute } from ${JSON.stringify(fileURLToPath(new URL('./source-positioning.mjs', import.meta.url)))};\n` + source
  source = `import { editedSourceLayout, sourceCompositionLayout } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, 'function preservesImportedHugCrossSize(graph, frame, axis) {',
    'function preservesImportedHugCrossSize(graph, frame, axis) {\n' +
    '\tif (sourceCompositionLayout(graph, frame) && !frame.figmaDerivedLayout) return false;')
  source = replace(source, 'if (preservesImportedInstanceInternals(child)) continue;',
    'if (applySourceAbsolute(graph, frame, child, computeLayout, getTextMeasurer())) continue;\n' +
    '\t\tif (preservesImportedInstanceInternals(child) && !(sourceCompositionLayout(graph, child) && !child.figmaDerivedLayout)) continue;')
  return replace(source,
    'const preservesImportedFrameGeometry = child.type === "FRAME" && child.source.format === "fig" && frameSourceIsFig(graph, child.parentId);',
    'const preservesImportedFrameGeometry = child.type === "FRAME" && child.source.format === "fig" && frameSourceIsFig(graph, child.parentId) && !(sourceCompositionLayout(graph, child) && !child.figmaDerivedLayout) && !editedSourceLayout(graph, graph.getNode(child.parentId));')
}

// JavaScript exceptions must not unwind through Yoga's WebAssembly stack.
// Each layout owns its callbacks and failure; only successful geometry applies.
export function correctMeasuredLayout(source, replace) {
  for (const name of ['root', 'yogaChild', 'yogaGC']) source = replace(source,
    `const ${name} = createYogaNode();`, `const ${name} = createLayoutNode();`)
  for (const signature of ['buildYogaTree(graph, frame, inheritedDirection)',
    'configureChildAsAutoLayout(yogaChild, child, parent, graph, inheritedDirection)',
    'configureChildAsAutoLayout(yogaChild, child, frame, graph, direction)',
    'configureChildAsAutoLayout(yogaGC, gc, child, graph, direction)',
    'buildYogaTree(graph, frame, rootDirection)']) {
    source = replace(source, signature, signature.slice(0, -1) + ', createLayoutNode)')
  }
  source = replace(source, 'function computeLayoutMeasured(graph, frameId) {', String.raw`
function computeLayoutMeasured(graph, frameId) {
  const failures = [], allocated = [];
  function createLayoutNode() {
    const node = createYogaNode(), setMeasure = node.setMeasureFunc.bind(node);
    allocated.push(node);
    node.setMeasureFunc = measure => setMeasure((...args) => {
      if (!failures.length) {
        try { return measure(...args); } catch (error) { failures.push(error); }
      }
      return { width: 0, height: 0 };
    });
    return node;
  }`)
  // Intrinsic premeasurement can fail while constructing a tree, before its
  // root is returned. Own every allocation, including not-yet-attached children.
  source = replace(source,
    'const yogaRoot = buildYogaTree(graph, frame, rootDirection, createLayoutNode);',
    String.raw`let yogaRoot;
  try {
    yogaRoot = buildYogaTree(graph, frame, rootDirection, createLayoutNode);
  } catch (error) {
    for (const node of allocated.reverse()) node.free();
    throw error;
  }`)
  return replace(source,
    '    yogaRoot.calculateLayout(void 0, void 0, yogaDirection);\n' +
    '    if (settleSourceAspectRatios(graph, frame, yogaRoot)) yogaRoot.calculateLayout(void 0, void 0, yogaDirection);', String.raw`
    const calculate = () => {
      yogaRoot.calculateLayout(void 0, void 0, yogaDirection);
      if (failures.length) throw failures[0];
    };
    calculate();
    if (settleSourceAspectRatios(graph, frame, yogaRoot)) calculate();`)
}
