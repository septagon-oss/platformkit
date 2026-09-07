import { fileURLToPath } from 'node:url'

// Layout owns temporary Yoga objects and grid sizing modes. Release/restore
// them at that boundary even when measurement or nested layout throws.
export function correctLayout(source, replace) {
  const lineage = fileURLToPath(new URL('./exporter-correction.mjs', import.meta.url))
  source = `import { chain } from ${JSON.stringify(lineage)};\n` + source
  source = replace(source, 'function configureTextLeaf(yogaChild, child, parent, fixedDerivedMainAxis = false) {', String.raw`
function sourceTextRow(graph, parent) {
  if (parent.layoutMode !== "HORIZONTAL" || parent.primaryAxisSizing !== "HUG" ||
      parent.counterAxisSizing !== "HUG" || parent.layoutWrap !== "NO_WRAP") return false;
  let master;
  try { master = chain(graph, parent, "componentId").at(-1); } catch { return false; }
  const entries = master.pluginData.filter(item => item.pluginId === "platformkit" && item.key === "platformkit.source");
  if (entries.length !== 1) return false;
  let source;
  try { source = JSON.parse(entries[0].value); } catch { return false; }
  return source?.schema === "platformkit.design-export.v1" &&
    ["text-component-observed-aliases", "text-and-icon-component-observed-aliases"].includes(source.scope);
}

function configureTextLeaf(yogaChild, child, parent, fixedDerivedMainAxis = false, graph) {`)
  source = replace(source, 'configureTextLeaf(yogaChild, child, parent, fixedDerivedMainAxis);',
    'configureTextLeaf(yogaChild, child, parent, fixedDerivedMainAxis, graph);')
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
  if (!resizedWrappingFrame(graph, frame)) return computeLayoutMeasured(graph, frameId);
  // An authored width invalidates the saved line boxes, not reusable masters
  // or unrelated descendants. Other imported layout keeps its existing guards.
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
  source = replace(source, '!preservesImportedInstanceLayout(node)', '(!preservesImportedInstanceLayout(node) || resizedWrappingFrame(graph, node))')
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

export function correctGridRecompute(source, replace) {
  return replace(source,
    '\tcomputeLayout(graph, child.id);\n' +
    '\tconst restore = {};\n' +
    '\tif (updates.primaryAxisSizing) restore.primaryAxisSizing = savedPrimary;\n' +
    '\tif (updates.counterAxisSizing) restore.counterAxisSizing = savedCounter;\n' +
    '\tif (Object.keys(restore).length > 0) graph.updateNode(child.id, restore);',
    String.raw`
  const restore = {};
  if (updates.primaryAxisSizing) restore.primaryAxisSizing = savedPrimary;
  if (updates.counterAxisSizing) restore.counterAxisSizing = savedCounter;
  try {
    computeLayout(graph, child.id);
  } finally {
    if (Object.keys(restore).length > 0) graph.updateNode(child.id, restore);
  }`)
}
