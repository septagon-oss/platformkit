import { fileURLToPath } from 'node:url'
import { chain } from './exporter-correction.mjs'

export function sourceLayoutScope(graph, node) {
  let master
  try { master = chain(graph, node, 'componentId').at(-1) } catch { return }
  return ownSourceLayoutScope(master)
}

export function ownSourceLayoutScope(node) {
  if (!Array.isArray(node?.pluginData)) return
  const entries = node.pluginData.filter(item => item.pluginId === 'platformkit' && item.key === 'platformkit.source')
  if (entries.length !== 1) return
  let source
  try { source = JSON.parse(entries[0].value) } catch { return }
  return source?.schema === 'platformkit.design-export.v1' ? source.scope : undefined
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

// Layout owns temporary Yoga objects. Release them at that boundary even when
// measurement or nested layout throws.
export function correctLayout(source, replace) {
  source = `import { sourceLayoutScope, sourceCompositionLayout, editedSourceLayout } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
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
  source = replace(source, 'function configureFlexContainer(yogaNode, node, direction) {',
    'function configureFlexContainer(yogaNode, node, direction, graph) {')
  source = replace(source, 'configureFlexContainer(root, frame, direction);', 'configureFlexContainer(root, frame, direction, graph);')
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
  // Authored layout edits invalidate the affected saved boxes, not reusable
  // masters or unrelated descendants. Other imported layout keeps its guards.
  const cached = [frame, ...graph.getChildren(frameId)].filter(node =>
    node === frame || node.visible && node.layoutPositioning !== "ABSOLUTE")
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
    applyYogaLayout(graph, frame, yogaRoot, computeLayoutInternal);
  } finally {
    freeYogaTree(yogaRoot);
  }`)
}

export function correctLayoutApply(source, replace) {
  source = `import { applySourceAbsolute } from ${JSON.stringify(fileURLToPath(new URL('./source-positioning.mjs', import.meta.url)))};\n` + source
  source = `import { editedSourceLayout, sourceCompositionLayout } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, 'function preservesImportedHugCrossSize(graph, frame, axis) {',
    'function preservesImportedHugCrossSize(graph, frame, axis) {\n' +
    '\tif (sourceCompositionLayout(graph, frame) && !frame.figmaDerivedLayout) return false;')
  source = replace(source, 'if (preservesImportedInstanceInternals(child)) continue;',
    'if (applySourceAbsolute(graph, frame, child, computeLayout)) continue;\n' +
    '\t\tif (preservesImportedInstanceInternals(child) && !(sourceCompositionLayout(graph, child) && !child.figmaDerivedLayout)) continue;')
  return replace(source,
    'const preservesImportedFrameGeometry = child.type === "FRAME" && child.source.format === "fig" && frameSourceIsFig(graph, child.parentId);',
    'const preservesImportedFrameGeometry = child.type === "FRAME" && child.source.format === "fig" && frameSourceIsFig(graph, child.parentId) && !editedSourceLayout(graph, graph.getNode(child.parentId));')
}
