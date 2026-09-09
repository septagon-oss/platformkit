import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'
import { correctExporter, correctPropertyTarget, correctInstanceImporter } from './exporter-correction.mjs'
import { correctPropertyActions, correctComponentSync, correctEditorCreation, correctTextAutoResize, correctUndoHistory } from './property-correction.mjs'
import { correctLayout, correctLayoutApply } from './layout-correction.mjs'
import { correctScaleDefaults, correctScaleGraph, correctScaleNodeChange, correctScaleImport } from './scaling-correction.mjs'
import { correctSyncGraph } from './sync-correction.mjs'
import { correctGridLayout, correctGridApply, correctGridTrackMapping } from './grid-correction.mjs'
import { correctGridNodeChange, correctGridImport, correctGridOverrides, correctGridActions } from './grid-fig-correction.mjs'
import { correctVariantActions, correctVariantImport, correctVariantNodeChange } from './variant-correction.mjs'
import { correctCSSBorders } from './border-correction.mjs'
import { correctSourceOverflow } from './source-box.mjs'
import { correctSourcePositionActions, correctSourcePositionImport, correctSourcePositionGraph } from './source-positioning.mjs'

// Source hashes pin the exact upstream implementation, not just its version
// label. A dependency upgrade requires a new review and the conformance suite.
export const sdkVersion = '0.14.0'
const colorHelper = JSON.stringify(fileURLToPath(new URL('./variable-color.mjs', import.meta.url)))
export const corrections = Object.freeze({
  '@open-pencil/fig/dist/node-change2.js': {
    sha256: 'bdbb599d70a5cf92300c67c385ee0d269550d4eea9c637f608d85fa321e63ee7',
    transform: (source, replace) => {
      source = correctVariantNodeChange(correctGridNodeChange(correctScaleNodeChange(correctExporter(source, replace), replace), replace), replace)
      return `import { sourceAbsoluteRecord } from ${JSON.stringify(fileURLToPath(new URL('./source-positioning.mjs', import.meta.url)))};\n` +
        source + '\nexport { serializeVariableModes, extractComponentPropertyAssignments };\n'
    },
  },
  '@open-pencil/fig/dist/node-change.js': {
    sha256: 'c468a330820b16cbe552b70d7090b30a014477b6b4e3b5149cc8d2584cc8abba',
    transform: source => source + '\nexport { serializeVariableModes } from "./node-change2.js";\n',
  },
  '@open-pencil/core/dist/io/formats/fig/export.js': {
    sha256: '084acd6250329f95a0f3c92df1f863dab0e59fed559264eb4976c79ebee05d55',
    transform(source, replace) {
      source = `import { serializeCSSColors } from ${colorHelper};\n` + source
      source = replace(source, 'variableData: variableValueToKiwi(value, variable.type, varIdToGuid)',
        'variableData: variableValueToKiwi(value?.cssColor ? graph.resolveVariable(variable.id, modeId) : value, variable.type, varIdToGuid)')
      source = replace(source, 'if (variable.key) nc.key = variable.key;',
        'nc.pluginData = serializeCSSColors(graph, variable, varIdToGuid, modeIdToGuid);\n\t\tif (variable.key) nc.key = variable.key;')
      // Pages take a separate export path; reuse the native mode serializer
      // after collection and mode GUIDs have been assigned, just like frames.
      source = 'import { serializeVariableModes } from "@open-pencil/fig/node-change";\n' + source
      // Variable descriptions have a native FIG field, independent of names.
      source = replace(source, 'name: variable.name,', 'name: variable.name,\n\t\t\tdescription: variable.description,')
      return replace(source, 'for (const entry of canvasEntries) nodeChanges.push(entry.canvasNc);',
        `for (const entry of canvasEntries) {
          const modes = entry.page.variableModes && serializeVariableModes(entry.page, varIdToGuid, modeIdToGuid);
          if (modes) entry.canvasNc.variableModeBySetMap = modes;
          nodeChanges.push(entry.canvasNc);
        }`)
    },
  },
  '@open-pencil/core/dist/kiwi/fig/import.js': {
    sha256: '7e16f0f993319eba097756e94dba7ae080f3104ba4e6359483ed653fc3c08d1d',
    transform(source, replace) {
      source = correctVariantImport(correctGridImport(source, replace), replace)
      source = `import { restoreCSSColors, validateCSSColors } from ${colorHelper};\n` + source
      source = replace(source, '\n\t\t\tvaluesByMode,', '\n\t\t\tvaluesByMode: restoreCSSColors(nc, type, valuesByMode),')
      source = replace(source, 'importVariableEntries(changeMap, parentMap, graph, assetRefs);',
        'importVariableEntries(changeMap, parentMap, graph, assetRefs);\n\tvalidateCSSColors(graph);')
      source = replace(source, 'description: "",', 'description: typeof nc.description === "string" ? nc.description : "",')
      source = replace(source, 'function applyImportedCanvasMetadata(page, canvasNc) {',
        'function applyImportedCanvasMetadata(page, canvasNc) {\n\tpage.variableModes = nodeChangeToProps(canvasNc, []).variableModes;')
      // Updating the native link must also update the graph's instance index.
      // This is import bookkeeping, not an authored edit to source metadata.
      return replace(source, 'if (remapped) node.componentId = remapped;',
        'if (remapped) graph.preserveSourceMetadataDuring(() => graph.updateNode(node.id, { componentId: remapped }));')
    },
  },
  '@open-pencil/core/dist/kiwi/fig/lazy-import.js': {
    sha256: 'c7b8543c4ecd4b6e81f75b0da5ba7bef2b7efccc5f91a1d591ea2ee7ed05782e',
    // Completing an export must not replay imported state over edited pages.
    // Reuse the same pending-root filter as an ordinary page switch.
    transform: (source, replace) => replace(source,
      'if (graph.getPages(true).map((page) => page.id).every((id) => context.populatedRootIds.has(id))) return false;\n\tapplyPopulation(graph, context);\n\treturn true;',
      'return populateRoots(graph, context, graph.getPages(true).map((page) => page.id));'),
  },
  '@open-pencil/fig/dist/instance-overrides.js': {
    sha256: '5efd3f221660fbbed3d60879f187cb946e391bc1556f80213961d4804b027eb0',
    transform: (source, replace) => correctSourcePositionImport(correctGridOverrides(correctScaleImport(correctInstanceImporter(correctImporter(source, replace), replace), replace), replace), replace),
  },
  '@open-pencil/scene-graph/dist/types.js': {
    sha256: '79dcc003679545dae0cfeadfdbb68dc10c6e92b85468d344ac89a14cbbdfe8e6',
    transform(source, replace) {
      source = `import { resolveCSSColor, validateCSSColorRemoval } from ${colorHelper};\n` + source
      source = replace(source, 'function removeVariable(graph, id) {',
        `function removeVariable(graph, id) {
          if (!graph.variables.has(id)) return;
          validateCSSColorRemoval(graph, [id]);
          removeVariableUnchecked(graph, id);
        }
        function removeVariableUnchecked(graph, id) {`)
      source = replace(source,
        'if (collection) for (const varId of Array.from(collection.variableIds)) removeVariable(graph, varId);',
        `if (collection) {
          validateCSSColorRemoval(graph, collection.variableIds);
          for (const varId of Array.from(collection.variableIds)) removeVariableUnchecked(graph, varId);
        }`)
      source = replace(source, 'function resolveVariable(graph, variableId, modeId, visited) {',
        'function resolveVariable(graph, variableId, modeId, visited, work = { remaining: 4096 }, requiredType) {\n' +
        '\tif (visited?.size > 64 || --work.remaining < 0) throw new Error("Native CSS color: variable resolution limit");')
      source = replace(source, 'if (!variable) return void 0;\n\tconst collection = graph.variableCollections.get(variable.collectionId);',
        'if (!variable || requiredType && variable.type !== requiredType) return void 0;\n' +
        '\tconst collection = graph.variableCollections.get(variable.collectionId);')
      source = replace(source, 'if (value && typeof value === "object" && "aliasId" in value) {',
        `if (value && typeof value === "object" && "cssColor" in value) {
          if (variable.type !== "COLOR") throw new Error("Native CSS color: COLOR variable required");
          return resolveCSSColor(value.cssColor, id => graph.variables.get(id)?.type === "COLOR" ?
            resolveVariable(graph, id, preferredModeId, new Set([...(visited ?? []), variableId]), work, "COLOR") : undefined);
        }
        if (value && typeof value === "object" && "aliasId" in value) {`)
      source = replace(source, 'return resolveVariable(graph, value.aliasId, preferredModeId, seen);',
        'return resolveVariable(graph, value.aliasId, preferredModeId, seen, work, requiredType);')
      // Retain deletion ownership for deferred synchronization after the node
      // has left the graph. Existing one-argument listeners remain compatible.
      source = replace(source, 'this.emitter.emit("node:deleted", id);',
        'this.emitter.emit("node:deleted", id, node.parentId);')
      // A missing reference is not evidence of an authored deletion. Retain
      // parent identity only while surviving native links need reconciliation.
      source = replace(source, 'this.nodes.delete(id);', `
        const referenced = [...this.nodes.values()].some(candidate => candidate.componentId === id ||
          Object.entries(candidate.overrides).some(([key, value]) => key.endsWith(':sourceComponentId') && value === id)) ||
          [...(this.deletedNodeParents?.values() ?? [])].includes(id);
        if (referenced) {
          this.deletedNodeParents ??= new Map();
          this.deletedNodeParents.set(id, node.parentId);
        }
        this.nodes.delete(id);`)
      source = replace(source, 'const INSTANCE_SYNC_PROPS = [', 'const INSTANCE_SYNC_PROPS = [\n\t"dashPattern",')
      return correctUndoHistory(correctSyncGraph(correctSourcePositionGraph(correctScaleGraph(source, replace), replace), replace), replace)
    },
  },
  '@open-pencil/scene-graph/dist/chunks/copy.js': {
    sha256: 'a0e01d157869334504ba46f2b3e42d44c3e0ac42f49389444e0270b509dbd8cf',
    transform: correctScaleDefaults,
  },
  '@open-pencil/core/dist/editor/components/properties.js': {
    sha256: '7bc49a01f5148053123559f7e4a523317a7ea61339429607dd2fd338243ed123',
    transform: (source, replace) => correctPropertyActions(correctPropertyTarget(source, replace), replace),
  },
  '@open-pencil/core/dist/editor/components/variants.js': {
    sha256: 'b2b6ddf2575a44470f5143ed75e999a878190ad658020b6c01958210d05ef4d9',
    transform: correctVariantActions,
  },
  '@open-pencil/core/dist/editor/variables.js': {
    sha256: '95e406a14d6bf2f1057b09f31b8bf01b560d8a2dfc83cdd0fe62063e10923f47',
    transform(source, replace) {
      source = `import { setNativeVariableValue } from ${colorHelper};\n` + source
      source = replace(source, 'const prevValue = structuredClone(variable.valuesByMode[modeId]);',
        'const prevPresent = Object.hasOwn(variable.valuesByMode, modeId);\n' +
        '\t\tconst prevValue = structuredClone(variable.valuesByMode[modeId]);')
      source = replace(source, 'variable.valuesByMode[modeId] = newValue;',
        'setNativeVariableValue(ctx.graph, variable, modeId, newValue);')
      source = replace(source, 'if (v) v.valuesByMode[modeId] = structuredClone(newValue);',
        'if (v) setNativeVariableValue(ctx.graph, v, modeId, newValue);')
      return replace(source, 'if (v) v.valuesByMode[modeId] = structuredClone(prevValue);',
        'if (v) setNativeVariableValue(ctx.graph, v, modeId, prevValue, prevPresent);')
    },
  },
  '@open-pencil/core/dist/figma-api/index.js': {
    sha256: '81ad3ed7376c9866dad1d3ec3778d00129b3eb24cc63db827b8a9f3cf17198b6',
    transform: (source, replace) => `import { setNativeVariableValue } from ${colorHelper};\n` +
      replace(source, 'variable.valuesByMode[modeId] = value;',
        'setNativeVariableValue(this.graph, variable, modeId, value);'),
  },
  '@open-pencil/core/dist/editor/text/auto-resize.js': {
    sha256: '9cab5aafe825afa0d7f959536b8fe61da620a535051f6b4bee5ae31e74b0fc1a',
    transform: correctTextAutoResize,
  },
  '@open-pencil/core/dist/editor/component-sync.js': {
    sha256: '7381406b1455668e57afa48c313e89e4041e11592d9634a074d1de6da512a001',
    transform: correctComponentSync,
  },
  '@open-pencil/core/dist/editor/graph-events.js': {
    sha256: 'd8bda42ef584b716444c24c8c1dce4aeacd44a35c44267b5b132a71eeb471e5a',
    transform: (source, replace) => replace(source,
      'deleted: (id) => {\n\t\t\t\toptions.emitEditorEvent("node:deleted", id);\n\t\t\t\tonNodeStructureChanged(id);',
      'deleted: (id, parentId) => {\n\t\t\t\toptions.emitEditorEvent("node:deleted", id);\n\t\t\t\tonNodeStructureChanged(parentId ?? id);'),
  },
  '@open-pencil/core/dist/editor/create.js': {
    sha256: '3d164478f09a94567cdd35e7a0c1b3d175a95afcb312695f18b284d756fc4f26',
    transform: correctEditorCreation,
  },
  '@open-pencil/core/dist/editor/nodes.js': {
    sha256: '658a69b330f927b31cc525ec4389d6913e4ea04b9db8ce9d0688ead725ca51a5',
    transform: (source, replace) => correctGridActions(correctSourcePositionActions(source, replace), replace),
  },
  '@open-pencil/core/dist/layout.js': {
    sha256: '358130698d8aa61bfcad65e4695679ed3883aac9efbb048df5a09cbe98f2b299',
    transform: (source, replace) => correctGridLayout(correctLayout(source, replace), replace),
  },
  '@open-pencil/core/dist/layout/apply.js': {
    sha256: 'a02c896a0f808fd3ccb24ca6a8c09975ca7ef06e2555bc6313b54091e37e8c8d',
    transform: (source, replace) => correctGridApply(correctLayoutApply(source, replace), replace),
  },
  '@open-pencil/core/dist/layout/yoga-helpers.js': {
    sha256: '24a80fac2b5649055876204e4f4b8df1761bcdee01601ecc2df808b8bfadc584',
    transform: correctGridTrackMapping,
  },
  '@open-pencil/core/dist/text/opentype.js': {
    sha256: '4b95e351041faff7ab0fac48e78a09abcc82fb0d57e1e7d560bc2aef675cf8c4',
    transform: (source, replace) => replace(source,
      'import * as OpenTypeSync from "opentype.js";',
      'import OpenTypeSync from "opentype.js";'),
  },
  '@open-pencil/core/dist/text/fonts.js': {
    sha256: '6b6eb38301b35005f764634b453b221e95523445814c1819bfb7cd8efaeaeca5',
    transform(source, replace) {
      const helper = fileURLToPath(new URL('./font-correction.mjs', import.meta.url))
      source = `import { resolveLocalFont } from ${JSON.stringify(helper)};\nimport { parseFontStyle } from "./face.js";\n` + source
      return replace(source, 'const match = chooseLocalFontMatch(await window.queryLocalFonts(), family, style);\n\t\t\tif (!match) return null;',
        `const fonts = await window.queryLocalFonts();
        const match = chooseLocalFontMatch(fonts, family, style);
        if (!match) {
          const requested = parseFontStyle(style);
          return resolveLocalFont(fonts, { family, weight: requested.weight, style: requested.italic ? 'italic' : 'normal' });
        }`)
    },
  },
  '@open-pencil/core/dist/canvas/text/index.js': {
    sha256: 'ebabf318ffdc67ffac0f90519c5681a81e0b02dbdb0552cded05b2ec0ee87a05',
    transform(source, replace) {
      const helper = fileURLToPath(new URL('./layout-correction.mjs', import.meta.url))
      source = `import { ownSourceLayoutScope } from ${JSON.stringify(helper)};\n` + source
      source = `import { sourceParagraph, drawSourceParagraph } from ${JSON.stringify(fileURLToPath(new URL('./paragraph-correction.mjs', import.meta.url)))};\n` + source
      source = replace(source, 'function buildParagraph(r, node, color, { halfLeading = false } = {}) {',
        'function buildParagraph(r, node, color, options) {\n' +
        '\treturn sourceParagraph(node, value => buildNativeParagraph(r, value, color, options));\n}\n' +
        'function buildNativeParagraph(r, node, color, { halfLeading = false } = {}) {')
      source = replace(source, 'recCanvas.drawParagraph(paragraph, 0, 0);', 'drawSourceParagraph(recCanvas, paragraph, 0, 0);')
      // Source paragraph line breaks use fractional CSS widths. CanvasKit's
      // rounding hack changes those widths; native documents keep their default.
      source = replace(source, 'const paraStyle = new ck.ParagraphStyle({',
        'const paraStyle = new ck.ParagraphStyle({\n' +
        '\t\tapplyRoundingHack: ownSourceLayoutScope(node) !== "source-composition-layout",')
      // Layout consumes shaped advances, not raster pixel bounds.
      source = replace(source, 'const height = paragraph.getHeight();',
        'const height = paragraph.getHeight();\n\tconst minContentWidth = paragraph.getMinIntrinsicWidth();')
      return replace(source, 'width: Math.ceil(width),\n\t\theight: Math.ceil(height)', 'width,\n\t\theight,\n\t\tminContentWidth')
    },
  },
  '@open-pencil/core/dist/canvas/fills.js': {
    sha256: '82f7ca84f5b854b9c5a317451dd28050dc74d686a10074d0826c9ba4fb982575',
    // applyFill just installed the resolved solid color, including alpha.
    // Paint opacity multiplies that alpha; it must not replace it.
    transform: (source, replace) => replace(source, 'r.fillPaint.setAlphaf(fill.opacity);',
      'r.fillPaint.setAlphaf(fill.opacity * (fill.type === "SOLID" ? r.fillPaint.getColor()[3] : 1));'),
  },
  '@open-pencil/core/dist/canvas/scene.js': {
    sha256: '7d935160fff2f7ac19a6ef55bf05e920e813b880012251063027078370757123',
    transform(source, replace) {
      source = `import { drawSourceParagraph } from ${JSON.stringify(fileURLToPath(new URL('./paragraph-correction.mjs', import.meta.url)))};\n` + source
      source = source.replaceAll('canvas.drawParagraph(paragraph, 0, paragraphY);', 'drawSourceParagraph(canvas, paragraph, 0, paragraphY);')
      return correctSourceOverflow(correctCSSBorders(source, replace), replace)
    },
  },
  '@open-pencil/core/dist/canvas/strokes.js': {
    sha256: '8da58e7799f04db7dcf301b7033d4c113627e148e26f9c75b3533dfff1e7dc31',
    transform(source, replace) {
      // Both ordinary strokes and dashed rectangles with solid corners retain
      // the resolved color alpha in addition to their own paint opacity.
      for (const cap of ['r.ck.StrokeCap.Butt', 'getStrokeCapEntity(r, stroke.cap ?? node.strokeCap)']) {
        source = replace(source, `r.strokePaint.setAlphaf(stroke.opacity);\n\tr.strokePaint.setStrokeCap(${cap});`,
          `r.strokePaint.setAlphaf(stroke.opacity * color.a);\n\tr.strokePaint.setStrokeCap(${cap});`)
      }
      // Four independent lines lose rounded corners and double-paint alpha
      // where sides overlap. Solid inside borders are one rounded ring.
      return replace(source, 'function drawIndividualSideStrokes(r, canvas, node, align) {',
        `function drawIndividualSideStrokes(r, canvas, node, align) {
  if (align === "INSIDE" && !nodeHasSmoothCorners(node) && node.strokes.every(stroke => !stroke.dashPattern?.length)) {
    const { width: w, height: h, borderTopWeight: t, borderRightWeight: rgt, borderBottomWeight: b, borderLeftWeight: l } = node;
    const corners = node.independentCorners
      ? [node.topLeftRadius, node.topRightRadius, node.bottomRightRadius, node.bottomLeftRadius]
      : Array(4).fill(node.cornerRadius);
    const [tl, tr, br, bl] = corners;
    const scale = Math.min(1, w / (tl + tr || 1), w / (bl + br || 1), h / (tl + bl || 1), h / (tr + br || 1));
    const radii = corners.map(radius => radius * scale);
    const path = new r.ck.Path(), paint = r.strokePaint.copy();
    try {
      path.setFillType(r.ck.FillType.EvenOdd);
      path.addRRect(new Float32Array([0, 0, w, h, ...radii.flatMap(radius => [radius, radius])]));
      if (l + rgt < w && t + b < h) path.addRRect(new Float32Array([l, t, w - rgt, h - b,
        Math.max(0, radii[0] - l), Math.max(0, radii[0] - t),
        Math.max(0, radii[1] - rgt), Math.max(0, radii[1] - t),
        Math.max(0, radii[2] - rgt), Math.max(0, radii[2] - b),
        Math.max(0, radii[3] - l), Math.max(0, radii[3] - b)]));
      paint.setStyle(r.ck.PaintStyle.Fill);
      paint.setPathEffect(null);
      canvas.drawPath(path, paint);
    } finally { path.delete(); paint.delete(); }
    return;
  }`)
    },
  },
  '@open-pencil/core/dist/canvas/shadows.js': {
    sha256: '9d44ef166ff3315a40d9b2594ac6e0785f450acb876040c7d808b24d66a80e84',
    // Explicit false clips the shadow behind translucent fills too, not only
    // unfilled nodes. https://developers.figma.com/docs/plugins/api/Effect/
    transform: (source, replace) => replace(source,
      'effect.showShadowBehindNode === false && !hasVisibleFill && !shadowShapeChild',
      'effect.showShadowBehindNode === false && !shadowShapeChild'),
  },
  '@open-pencil/core/dist/tools/calc.js': {
    sha256: '35d6fd205094a3e26f5098b98833c92ffe96a2defdb377208576f58c6e71b67d',
    transform(source, replace) {
      // Expression-defined functions belong to one calculation, not a shared
      // parser or batch. The reviewed fork registers them on its Parser.
      source = replace(source, 'const parser = new ExprEval.Parser();\n', '')
      return replace(source, 'parser.evaluate(expr)', 'new ExprEval.Parser().evaluate(expr)')
    },
  },
  '@open-pencil/core/node_modules/expr-eval/dist/index.mjs': {
    sha256: 'a90044058f0447a30c7bd4fbcb9a42ad944680cdae727649542014e2212bb711',
    transform: correctParserCounter,
  },
  '@open-pencil/core/node_modules/expr-eval/dist/bundle.js': {
    sha256: '2c7aa9e7513101788f4b21cc368c05fbeff61b5be8f8eda0131b86213106e9ef',
    transform: correctParserCounter,
  },
})

function correctParserCounter(source, replace) {
  // Fork 3.0.3 otherwise overwrites every expression-defined function under
  // lambda_NaN, breaking expressions that declare more than one function.
  return replace(source, 'this.functions = {', 'this.functions = {\n    __counter: 0,')
}

function replaceOnce(source, before, after) {
  if (source.split(before).length !== 2) {
    throw new Error(`OpenPencil correction anchor is not unique: ${before.slice(0, 80)}`)
  }
  return source.replace(before, after)
}

// Shared by the Node loader and future browser build integration. This changes
// caller-owned source text only; installed packages and documents are untouched.
// Return null for unrelated modules. Reject changed or already-patched sources.
export function correctSource(path, source) {
  const entry = Object.entries(corrections).find(([suffix]) => path.endsWith(`/${suffix}`))
  if (!entry) return null
  const [name, correction] = entry
  const hash = createHash('sha256').update(source).digest('hex')
  if (hash !== correction.sha256) {
    throw new Error(`OpenPencil ${sdkVersion} source mismatch for ${name}: ${hash}`)
  }
  // Upstream source maps no longer describe the transformed module.
  return correction.transform(source, replaceOnce).replace(/^\/\/# sourceMappingURL=.*$/m, '')
}

const referenceConversion = String.raw`
function nativeReferenceField(field) {
  if (field === 'TEXT_DATA') return 'TEXT';
  if (field === 'OVERRIDDEN_SYMBOL_ID') return 'INSTANCE_SWAP';
  if (field === 'VISIBLE') return 'VISIBLE';
  throw new Error('Unsupported native property reference field: ' + field);
}
function sceneReferenceField(field) {
  if (field === 'TEXT') return 'TEXT_DATA';
  if (field === 'INSTANCE_SWAP') return 'OVERRIDDEN_SYMBOL_ID';
  if (field === 'VISIBLE') return 'VISIBLE';
  throw new Error('Unsupported scene property reference field: ' + field);
}
`

function correctImporter(source, replace) {
  source = replace(source, 'if (targetId === nodeId && ctx.kiwiPropertyNodes.has(nodeId)) continue;',
    'if (targetId === nodeId && ctx.kiwiPropertyNodes.has(nodeId) && ov.dashPattern === undefined) continue;')
  source = replace(source, 'function preserveStrokeShapeProps(target, updates) {',
    `function preserveStrokeShapeProps(target, updates) {
      if (updates.dashPattern !== undefined && !updates.strokes) updates.strokes = copyStrokes(target.strokes);`)
  source = replace(source, 'dashPattern: existing.dashPattern', 'dashPattern: updates.dashPattern ?? existing.dashPattern')
  source = replace(source, 'dashPattern: target.dashPattern', 'dashPattern: updates.dashPattern ?? target.dashPattern')
  source = replace(source,
    'function convertOverrideToProps(ov) {\n\tconst updates = {};',
    referenceConversion + String.raw`function convertOverrideToProps(ov) {
  const updates = {};
  if (ov.dashPattern !== undefined) updates.dashPattern = [...ov.dashPattern];
  if (ov.componentPropRefs) {
    updates.componentPropertyReferences = ov.componentPropRefs.map(ref => {
      if (!ref.defID) throw new Error('Native property reference has no definition ID');
      return {
        propertyId: guidToString(ref.defID),
        field: nativeReferenceField(ref.componentPropNodeField)
      };
    });
  }`)
  source = replace(source,
    'const node = ctx.graph.getNode(sourceId);\n\t\tconst overrideKey =',
    String.raw`const node = ctx.graph.getNode(sourceId);
    if (node?.componentPropertyReferences.length) {
      return node.componentPropertyReferences.map(ref => ({
        defID: stringToGuidParts(ref.propertyId),
        componentPropNodeField: sceneReferenceField(ref.field)
      }));
    }
    const overrideKey =`)
  return replace(source, '\tif (propRefsMap.size === 0) return modified;', '')
}
