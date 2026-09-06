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
      primaryAxisSizing: node.primaryAxisSizing,
      counterAxisSizing: node.counterAxisSizing,
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

function checkPropertyMasterGeometry(ctx, before) {
  for (const [id, snapshot] of before) {
    const node = ctx.graph.getNode(id);
    if (node?.type === "COMPONENT" && (node.width !== snapshot.width || node.height !== snapshot.height)) {
      throw new Error("Property layout resizing another master requires downstream instance history");
    }
  }
}

function restorePropertyHistory(ctx, state) {
  for (const id of state.keys()) {
    if (!ctx.graph.getNode(id)) throw new Error("Missing node in text-property history: " + id);
  }
  ctx.withoutComponentSync(() => {
    ctx.graph.withLayoutMutations(() => ctx.graph.preserveSourceMetadataDuring(() => {
      for (const [id, snapshot] of state) {
        // updateNode emits invalidation events, but must not add source edit marks.
        // Clone on every replay so a later edit cannot change stored undo data.
        ctx.graph.updateNode(id, structuredClone(snapshot));
      }
    }));
  });
}

function refreshPropertyLayout(ctx, target) {
  if (target?.field !== "TEXT") return;
  ctx.withoutComponentSync(() => {
    for (let node = target.node; node && node.type !== "CANVAS"; node = node.parentId ? ctx.graph.getNode(node.parentId) : null) {
      ctx.graph.updateNode(node.id, { figmaDerivedLayout: null });
    }
    ctx.runLayoutForNode(target.node.id);
  });
}
`

// Reuse the scheduler's own reentrancy guard for the synchronous property
// operation only. Ordinary authored/layout changes retain upstream behavior.
export function correctComponentSync(source, replace) {
  return replace(source, '\treturn { scheduleComponentSync };', String.raw`
  function withoutComponentSync(fn) {
    const previous = isFlushingComponentSync;
    isFlushingComponentSync = true;
    try {
      return fn();
    } finally {
      isFlushingComponentSync = previous;
    }
  }
  return { scheduleComponentSync, withoutComponentSync };`)
}

export function correctEditorCreation(source, replace) {
  source = replace(source,
    'const { scheduleComponentSync } = createComponentSyncScheduler',
    'const { scheduleComponentSync, withoutComponentSync } = createComponentSyncScheduler')
  return replace(source,
    '\t\trunLayoutForNode,\n\t\tsubscribeToGraph',
    '\t\trunLayoutForNode,\n\t\twithoutComponentSync,\n\t\tsubscribeToGraph')
}

// Hash/version guards belong to corrections.mjs. Every structural anchor here
// must match exactly once before any substituted module is allowed to load.
export function correctPropertyActions(source, replaceOnce) {
  // Validate and replace the native target before publishing its assignment.
  // A refused replacement must leave both the graph and property value intact.
  source = replaceOnce(source,
    '\tconst swapComponentId = target?.field === "INSTANCE_SWAP" ? swapTargetId(ctx, value) : null;',
    '\tconst swapComponentId = target?.field === "INSTANCE_SWAP" ? swapTargetId(ctx, value) : null;\n' +
      '\tif (target?.field === "INSTANCE_SWAP") {\n' +
      '\t\tif (!swapComponentId) throw new Error("Missing native replacement component");\n' +
      '\t\tupdatePropertyTarget(ctx, target, value, swapComponentId);\n' +
      '\t}',
  )
  source = replaceOnce(source,
    '\tupdatePropertyTarget(ctx, target, value, swapComponentId);\n}',
    '\tif (target?.field !== "INSTANCE_SWAP") updatePropertyTarget(ctx, target, value, swapComponentId);\n}',
  )
  source = replaceOnce(source,
    'function createComponentPropertyActions(ctx, switchVariant) {',
    helpers + '\nfunction createComponentPropertyActions(ctx, switchVariant) {',
  )
  source = replaceOnce(source,
    '\t\tconst target = propertyTarget(ctx, instance, propertyId);\n\t\tconst assignedValue =',
    '\t\tconst target = propertyTarget(ctx, instance, propertyId);\n' +
      '\t\tconst propertyScope = propertyHistoryScope(ctx, target);\n' +
      '\t\tconst propertyBefore = propertyScope ? snapshotPropertyHistory(ctx, propertyScope) : null;\n' +
      '\t\tconst previousTargetComponentId = target?.node.componentId;\n' +
      '\t\tconst assignedValue =',
  )
  source = replaceOnce(source,
    '\t\t\t\tif (live) {\n\t\t\t\t\tctx.graph.updateNode(instanceId, {',
    '\t\t\t\tif (live) {\n' +
      '\t\t\t\t\tconst restoredTarget = propertyTarget(ctx, live, propertyId);\n' +
      '\t\t\t\t\tif (restoredTarget?.field === "INSTANCE_SWAP") {\n' +
      '\t\t\t\t\t\tconst componentId = swapTargetId(ctx, previousValue);\n' +
      '\t\t\t\t\t\tif (!componentId) throw new Error("Missing native replacement history component");\n' +
      '\t\t\t\t\t\tctx.graph.swapInstanceComponent(restoredTarget.node.id, componentId);\n' +
      '\t\t\t\t\t\tctx.graph.updateNode(restoredTarget.node.id, { componentId: previousTargetComponentId });\n' +
      '\t\t\t\t\t}\n' +
      '\t\t\t\t\tctx.graph.updateNode(instanceId, {',
  )
  source = replaceOnce(source, '\t\t\t\t\tconst restoredTarget = propertyTarget(ctx, live, propertyId);\n' +
    '\t\t\t\t\tif (restoredTarget?.field === "TEXT"', '\t\t\t\t\tif (restoredTarget?.field === "TEXT"')
  source = replaceOnce(source,
    '\t\t\t\t\telse if (restoredTarget?.field === "INSTANCE_SWAP") {\n' +
      '\t\t\t\t\t\tconst componentId = swapTargetId(ctx, previousValue);\n' +
      '\t\t\t\t\t\tif (componentId && restoredTarget.node.type === "INSTANCE") ctx.graph.swapInstanceComponent(restoredTarget.node.id, componentId);\n' +
      '\t\t\t\t\t}', '')
  source = replaceOnce(source,
    '\t\tapplyPropertyValue(ctx, instanceId, definition, value);\n\t\tctx.undo.push({',
    String.raw`
    let propertyAfter = null;
    try {
      applyPropertyValue(ctx, instanceId, definition, value);
      if (propertyBefore) {
        refreshPropertyLayout(ctx, target);
        checkPropertyMasterGeometry(ctx, propertyBefore);
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
