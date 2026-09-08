import { fileURLToPath } from 'node:url'
import { chain } from './exporter-correction.mjs'

export function liveGridLayout(graph, node) {
  return node && chain(graph, node, 'parentId').some(parent => parent.layoutMode === 'GRID')
}

export function gridLayoutNodes(graph, frame) {
  const nodes = [frame], seen = new Set([frame.id])
  for (const parent of nodes) for (const child of graph.getChildren(parent.id)) {
    if (!child.visible || child.layoutPositioning === 'ABSOLUTE') continue
    if (seen.has(child.id)) throw new Error('Cyclic native grid layout')
    seen.add(child.id)
    nodes.push(child)
  }
  return nodes
}

// Grid and flex must participate in one Yoga tree. Measuring grid cells as
// fixed leaves freezes wrapped text and gives the parent a stale row height.
function replaceFunction(source, name, next, replacement, replace) {
  const start = source.indexOf(`function ${name}(`), end = source.indexOf(`function ${next}(`, start)
  if (start < 0 || end < 0) throw new Error('Pinned grid layout section changed')
  return replace(source, source.slice(start, end), replacement + '\n')
}

export function correctGridLayout(source, replace) {
  source = `import { liveGridLayout, gridLayoutNodes } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` +
    'import { Justify } from "yoga-layout";\n' + source
  // The native GRID model owns its measured subtree. Imported boxes are caches,
  // not layout constraints; unrelated imported documents retain their guards.
  source = replace(source, '!resizedWrappingFrame(graph, frame) && !editedSourceLayout(graph, frame)',
    '!resizedWrappingFrame(graph, frame) && !editedSourceLayout(graph, frame) && !liveGridLayout(graph, frame)')
  source = replace(source, 'const cached = [frame, ...graph.getChildren(frameId)].filter',
    'const cached = (liveGridLayout(graph, frame) ? gridLayoutNodes(graph, frame) : [frame, ...graph.getChildren(frameId)]).filter')
  source = replace(source, '|| editedSourceLayout(graph, node))',
    '|| editedSourceLayout(graph, node) || liveGridLayout(graph, node))')
  for (const axis of ['primary', 'counter']) source = replace(source,
    `frame.${axis}AxisSizing === "FILL" && sourceCompositionLayout(graph, frame)`,
    `frame.${axis}AxisSizing === "FILL" && (sourceCompositionLayout(graph, frame) || liveGridLayout(graph, frame))`)
  source = replace(source, 'import { buildGridTree, createGridChildNode } from "./layout/grid.js";\n', '')
  source = replace(source,
    'frame.layoutMode === "GRID" ? buildGridTree(graph, frame, rootDirection) : buildYogaTree(graph, frame, rootDirection)',
    'buildYogaTree(graph, frame, rootDirection)')
  source = replaceFunction(source, 'configureChildAsGrid', 'sizesFitParent', String.raw`
function configureGridContainer(yogaNode, node) {
  yogaNode.setDisplay(Display.Grid);
  yogaNode.setGridTemplateColumns(node.gridTemplateColumns.map(mapGridTrack));
  yogaNode.setGridTemplateRows(node.gridTemplateRows.map(mapGridTrack));
  yogaNode.setGap(Gutter.Column, node.gridColumnGap);
  yogaNode.setGap(Gutter.Row, node.gridRowGap);
}

function configureGridItemSizing(yogaNode, child, widthSizing, heightSizing) {
  const stretch = child.layoutGrow > 0 || child.layoutAlignSelf === "STRETCH";
  if (stretch || widthSizing === "FILL") yogaNode.setJustifySelf(Justify.Stretch);
  else if (widthSizing === "FIXED") yogaNode.setWidth(child.width);
  if (stretch || heightSizing === "FILL") yogaNode.setAlignSelf(Align.Stretch);
  else if (heightSizing === "FIXED") yogaNode.setHeight(child.height);
}

function configureGridPosition(yogaNode, child, parent) {
  if (parent.layoutMode !== "GRID" || child.layoutPositioning === "ABSOLUTE" || !child.gridPosition) return;
  const pos = child.gridPosition;
  yogaNode.setGridColumnStart(pos.column);
  yogaNode.setGridColumnEndSpan(pos.columnSpan);
  yogaNode.setGridRowStart(pos.row);
  yogaNode.setGridRowEndSpan(pos.rowSpan);
}`, replace)
  // Shared container configuration already owns direction, padding and limits.
  source = replace(source, 'const primaryGap = primaryItemGap(graph, node);',
    'if (node.layoutMode === "GRID") {\n' +
    '    configureGridContainer(yogaNode, node);\n' +
    '    applyMinMaxConstraints(yogaNode, node);\n' +
    '    return;\n  }\n  const primaryGap = primaryItemGap(graph, node);')
  source = replace(source,
    'function configureAutoLayoutChildSizing(yogaChild, child, parent, graph, widthSizing, heightSizing) {',
    'function configureAutoLayoutChildSizing(yogaChild, child, parent, graph, widthSizing, heightSizing) {\n' +
    '  if (parent.layoutMode === "GRID") return configureGridItemSizing(yogaChild, child, widthSizing, heightSizing);')
  source = replace(source, 'else configureNonTextLeaf(yogaChild, child, isRow, stretchCross);',
    'else if (parent.layoutMode === "GRID") configureGridItemSizing(yogaChild, child, "FIXED", "FIXED");\n' +
    '  else configureNonTextLeaf(yogaChild, child, isRow, stretchCross);')
  for (const [child, parent, yogaChild, yogaParent] of [
    ['child', 'frame', 'yogaChild', 'root'], ['gc', 'child', 'yogaGC', 'yogaChild'],
  ]) {
    source = replace(source,
      `else if (${child}.layoutMode === "GRID") configureChildAsGrid(${yogaChild}, ${child}, ${parent}, graph, direction);\n\t\t`, '')
    source = replace(source, `${yogaParent}.insertChild(${yogaChild}, ${yogaParent}.getChildCount());`,
      `configureGridPosition(${yogaChild}, ${child}, ${parent});\n    ` +
      `${yogaParent}.insertChild(${yogaChild}, ${yogaParent}.getChildCount());`)
  }
  return source
}

export function correctGridApply(source, replace) {
  source = `import { liveGridLayout } from ${JSON.stringify(fileURLToPath(import.meta.url))};\n` + source
  source = replace(source, 'sourceCompositionLayout(graph, frame) && !frame.figmaDerivedLayout',
    '(sourceCompositionLayout(graph, frame) || liveGridLayout(graph, frame)) && !frame.figmaDerivedLayout')
  source = replace(source, 'sourceCompositionLayout(graph, child) && !child.figmaDerivedLayout',
    '(sourceCompositionLayout(graph, child) || liveGridLayout(graph, child)) && !child.figmaDerivedLayout')
  source = replace(source, '!editedSourceLayout(graph, graph.getNode(child.parentId));',
    '!editedSourceLayout(graph, graph.getNode(child.parentId)) && !liveGridLayout(graph, child);')
  source = replace(source,
    '\tif (frame.layoutMode === "GRID") {\n\t\tif (frame.gridTemplateRows.length === 0) graph.updateNode(frame.id, { height: yogaNode.getComputedHeight() });\n\t\treturn;\n\t}\n', '')
  source = replaceFunction(source, 'recomputeGridChild', 'applyYogaLayout', '', replace)
  return replace(source,
    'if (child.layoutMode !== "NONE") if (child.layoutMode === "GRID" && child.layoutPositioning !== "ABSOLUTE") computeLayout(graph, child.id);\n' +
    '\t\telse if (frame.layoutMode === "GRID" && child.layoutPositioning !== "ABSOLUTE") recomputeGridChild(graph, child, computeLayout);\n' +
    '\t\telse applyYogaLayout(graph, child, yogaChild, computeLayout);',
    'if (child.layoutMode !== "NONE") applyYogaLayout(graph, child, yogaChild, computeLayout);')
}
