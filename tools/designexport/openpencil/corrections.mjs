import { createHash } from 'node:crypto'
import { correctExporter, correctPropertyTarget } from './exporter-correction.mjs'
import { correctPropertyActions } from './property-correction.mjs'

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
})

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
