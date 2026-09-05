// Injected into the pinned browser-compatible property action module. These
// helpers retain node identity; they never replace a page or rebuild a tree.
const helpers = String.raw`
function propertyHistoryScope(ctx, target) {
  if (target?.field !== "TEXT" || target.node.type !== "TEXT") return null;
  const nodes = new Map();
  let layoutRoot = target.node;
  for (let node = target.node; node; node = node.parentId ? ctx.graph.getNode(node.parentId) : null) {
    if (node.type === "COMPONENT") {
      throw new Error("Exact text-property history supports page-placed instances only");
    }
    if (node.type !== "CANVAS" && node.id !== ctx.graph.rootId) nodes.set(node.id, node);
    if (node.layoutMode !== "NONE") layoutRoot = node;
  }
  // Layout can move siblings and resize their descendants, not just ancestors.
  function visit(node) {
    nodes.set(node.id, node);
    for (const child of ctx.graph.getChildren(node.id)) visit(child);
  }
  visit(layoutRoot);
  return nodes.keys();
}

function snapshotPropertyHistory(ctx, ids) {
  const state = new Map();
  for (const id of ids) {
    const node = ctx.graph.getNode(id);
    if (!node) throw new Error("Missing node in text-property history: " + id);
    state.set(id, structuredClone({
      x: node.x, y: node.y, width: node.width, height: node.height,
      figmaDerivedLayout: node.figmaDerivedLayout,
      source: node.source,
      text: node.text,
      textPicture: node.textPicture,
      figmaDerivedTextGlyphs: node.figmaDerivedTextGlyphs,
      componentPropertyAssignments: node.componentPropertyAssignments,
      overrides: node.overrides
    }));
  }
  return state;
}

function retainChangedPropertyHistory(before, after) {
  for (const [id, previous] of before) {
    if (JSON.stringify(previous) === JSON.stringify(after.get(id))) {
      before.delete(id);
      after.delete(id);
    }
  }
}

function restorePropertyHistory(ctx, state) {
  for (const id of state.keys()) {
    if (!ctx.graph.getNode(id)) throw new Error("Missing node in text-property history: " + id);
  }
  ctx.graph.preserveSourceMetadataDuring(() => {
    for (const [id, snapshot] of state) {
      // updateNode emits invalidation events, but must not add source edit marks.
      // Clone on every replay so a later edit cannot change stored undo data.
      ctx.graph.updateNode(id, structuredClone(snapshot));
    }
  });
}

function refreshPropertyLayout(ctx, target) {
  if (target?.field !== "TEXT") return;
  for (let node = target.node; node && node.type !== "CANVAS"; node = node.parentId ? ctx.graph.getNode(node.parentId) : null) {
    ctx.graph.updateNode(node.id, { figmaDerivedLayout: null });
  }
  ctx.runLayoutForNode(target.node.id);
}
`

// Hash/version guards belong to corrections.mjs. Every structural anchor here
// must match exactly once before any substituted module is allowed to load.
export function correctPropertyActions(source, replaceOnce) {
  source = replaceOnce(source,
    'function createComponentPropertyActions(ctx, switchVariant) {',
    helpers + '\nfunction createComponentPropertyActions(ctx, switchVariant) {',
  )
  source = replaceOnce(source,
    '\t\tconst target = propertyTarget(ctx, instance, propertyId);\n\t\tconst assignedValue =',
    '\t\tconst target = propertyTarget(ctx, instance, propertyId);\n' +
      '\t\tconst propertyScope = propertyHistoryScope(ctx, target);\n' +
      '\t\tconst propertyBefore = propertyScope ? snapshotPropertyHistory(ctx, propertyScope) : null;\n' +
      '\t\tconst assignedValue =',
  )
  source = replaceOnce(source,
    '\t\tapplyPropertyValue(ctx, instanceId, definition, value);\n\t\tctx.undo.push({',
    String.raw`
    let propertyAfter = null;
    try {
      applyPropertyValue(ctx, instanceId, definition, value);
      if (propertyBefore) {
        refreshPropertyLayout(ctx, target);
        propertyAfter = snapshotPropertyHistory(ctx, propertyBefore.keys());
        retainChangedPropertyHistory(propertyBefore, propertyAfter);
      }
    } catch (error) {
      if (propertyBefore) restorePropertyHistory(ctx, propertyBefore);
      throw error;
    }
    ctx.undo.push({`,
  )
  source = replaceOnce(source,
    '\t\t\tforward: () => {\n\t\t\t\tapplyPropertyValue(ctx, instanceId, definition, value);',
    '\t\t\tforward: () => {\n' +
      '\t\t\t\tif (propertyAfter) {\n' +
      '\t\t\t\t\trestorePropertyHistory(ctx, propertyAfter);\n' +
      '\t\t\t\t\tctx.requestRender();\n' +
      '\t\t\t\t\treturn;\n' +
      '\t\t\t\t}\n' +
      '\t\t\t\tapplyPropertyValue(ctx, instanceId, definition, value);',
  )
  return replaceOnce(source,
    '\t\t\tinverse: () => {\n\t\t\t\tconst live = ctx.graph.getNode(instanceId);',
    '\t\t\tinverse: () => {\n' +
      '\t\t\t\tif (propertyBefore) {\n' +
      '\t\t\t\t\trestorePropertyHistory(ctx, propertyBefore);\n' +
      '\t\t\t\t\tctx.requestRender();\n' +
      '\t\t\t\t\treturn;\n' +
      '\t\t\t\t}\n' +
      '\t\t\t\tconst live = ctx.graph.getNode(instanceId);',
  )
}
