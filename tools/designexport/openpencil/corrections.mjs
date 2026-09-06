import { createHash } from 'node:crypto'
import { correctExporter, correctPropertyTarget } from './exporter-correction.mjs'
import { correctPropertyActions, correctComponentSync, correctEditorCreation } from './property-correction.mjs'
import { correctLayout, correctGridRecompute } from './layout-correction.mjs'

// Source hashes pin the exact upstream implementation, not just its version
// label. A dependency upgrade requires a new review and the conformance suite.
export const sdkVersion = '0.14.0'
export const corrections = Object.freeze({
  '@open-pencil/fig/dist/node-change2.js': {
    sha256: 'bdbb599d70a5cf92300c67c385ee0d269550d4eea9c637f608d85fa321e63ee7',
    transform: correctExporter,
  },
  '@open-pencil/fig/dist/instance-overrides.js': {
    sha256: '5efd3f221660fbbed3d60879f187cb946e391bc1556f80213961d4804b027eb0',
    transform: correctImporter,
  },
  '@open-pencil/core/dist/editor/components/properties.js': {
    sha256: '7bc49a01f5148053123559f7e4a523317a7ea61339429607dd2fd338243ed123',
    transform: (source, replace) => correctPropertyActions(correctPropertyTarget(source, replace), replace),
  },
  '@open-pencil/core/dist/editor/component-sync.js': {
    sha256: '7381406b1455668e57afa48c313e89e4041e11592d9634a074d1de6da512a001',
    transform: correctComponentSync,
  },
  '@open-pencil/core/dist/editor/create.js': {
    sha256: '3d164478f09a94567cdd35e7a0c1b3d175a95afcb312695f18b284d756fc4f26',
    transform: correctEditorCreation,
  },
  '@open-pencil/core/dist/layout.js': {
    sha256: '358130698d8aa61bfcad65e4695679ed3883aac9efbb048df5a09cbe98f2b299',
    transform: correctLayout,
  },
  '@open-pencil/core/dist/layout/apply.js': {
    sha256: 'a02c896a0f808fd3ccb24ca6a8c09975ca7ef06e2555bc6313b54091e37e8c8d',
    transform: correctGridRecompute,
  },
  '@open-pencil/core/dist/text/opentype.js': {
    sha256: '4b95e351041faff7ab0fac48e78a09abcc82fb0d57e1e7d560bc2aef675cf8c4',
    transform: (source, replace) => replace(source,
      'import * as OpenTypeSync from "opentype.js";',
      'import OpenTypeSync from "opentype.js";'),
  },
  '@open-pencil/core/dist/canvas/text/index.js': {
    sha256: 'ebabf318ffdc67ffac0f90519c5681a81e0b02dbdb0552cded05b2ec0ee87a05',
    // Layout consumes shaped advances, not raster pixel bounds. Preserve the
    // paragraph's precision without changing shaping or readiness checks.
    transform: (source, replace) => replace(source,
      'width: Math.ceil(width),\n\t\theight: Math.ceil(height)',
      'width,\n\t\theight'),
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
  source = replace(source,
    'function convertOverrideToProps(ov) {\n\tconst updates = {};',
    referenceConversion + String.raw`function convertOverrideToProps(ov) {
  const updates = {};
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
