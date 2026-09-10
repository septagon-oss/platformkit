import { fileURLToPath } from 'node:url'

const helper = JSON.stringify(fileURLToPath(new URL('./variable-binding.mjs', import.meta.url)))

export function correctNumericGraph(source, replace) {
  source = `import { planNumericBinding, planNumericNodeUpdate, planNumericActiveMode, applyNumericBindings, ownNumericLiteral, ownVariableModes } from ${helper};\n` + source
  source = replace(source, 'function bindVariable(graph, nodeId, field, variableId) {', `
function bindVariable(graph, nodeId, field, variableId) {
  const plan = planNumericBinding(graph, nodeId, field, variableId);
  bindVariableUnresolved(graph, nodeId, field, variableId);
  if (plan) ownNumericLiteral(graph, graph.getNode(nodeId), field, false);
  if (plan) applyNumericBindings(graph, plan);
}
function bindVariableUnresolved(graph, nodeId, field, variableId) {`)
  source = replace(source, 'function unbindVariable(graph, nodeId, field) {',
    'function unbindVariable(graph, nodeId, field) {\n\tapplyNumericBindings(graph);')
  source = replace(source, 'node.boundVariables = omit(node.boundVariables, [field]);',
    'ownNumericLiteral(graph, node, field, true);\n\tnode.boundVariables = omit(node.boundVariables, [field]);')
  source = replace(source, 'node.boundVariables = omitBy(node.boundVariables, (varId) => varId === id);',
    `for (const [field, variableId] of Object.entries(node.boundVariables)) if (variableId === id) ownNumericLiteral(graph, node, field, true);
    node.boundVariables = omitBy(node.boundVariables, (varId) => varId === id);`)
  source = replace(source, 'function setActiveMode(graph, collectionId, modeId) {\n\tgraph.activeMode.set(collectionId, modeId);',
    `function setActiveMode(graph, collectionId, modeId) {
      const plan = planNumericActiveMode(graph, collectionId, modeId);
      graph.activeMode.set(collectionId, modeId);
      applyNumericBindings(graph, plan);`)
  source = replace(source, '  updateNode(id, changes) {', `
  updateNode(id, changes) {
    const plan = planNumericNodeUpdate(this, id, changes);
    this.updateNodeUnresolved(id, changes);
    if (plan && Object.hasOwn(changes, 'variableModes')) ownVariableModes(this, this.getNode(id));
    if (plan) applyNumericBindings(this, plan);
  }
  updateNodeUnresolved(id, changes) {`)
  source = replace(source, '\t\treturn createInstance(this, componentId, parentId, overrides);',
    '\t\tconst instance = createInstance(this, componentId, parentId, overrides);\n\t\tif (instance && Object.hasOwn(overrides, "variableModes")) ownVariableModes(this, instance);\n\t\tapplyNumericBindings(this);\n\t\treturn instance;')
  return replace(source,
    'applyNativeSync(graph, planNativeSync(graph.nodes, graph.instanceIndex, componentId, graph.deletedNodeParents))',
    'applyNativeSync(graph, planNativeSync(graph.nodes, graph.instanceIndex, componentId, graph.deletedNodeParents));\n  applyNumericBindings(graph)')
}

export function correctNumericLayout(source, replace) {
  source = `import { withNumericLayout, usesNumericLayout } from ${helper};\n` + source
  for (const [name, arg] of [['computeLayout', 'frameId'], ['computeAllLayouts', 'scopeId']]) {
    source = replace(source, `function ${name}(graph, ${arg}) {`, `
function ${name}(graph, ${arg}) {
  return withNumericLayout(graph, () => ${name}Resolved(graph, ${arg}));
}
function ${name}Resolved(graph, ${arg}) {`)
  }
  return replace(source, '!preservesImportedInstanceLayout(node)',
    '(!preservesImportedInstanceLayout(node) || usesNumericLayout(graph, node))')
}

export function correctNumericLayoutApply(source, replace) {
  source = `import { usesNumericLayout } from ${helper};\n` + source
  source = replace(source, 'function preservesImportedHugCrossSize(graph, frame, axis) {',
    'function preservesImportedHugCrossSize(graph, frame, axis) {\n\tif (usesNumericLayout(graph, frame)) return false;')
  source = replace(source, 'const preservesImportedFrameGeometry = child.type',
    'const preservesImportedFrameGeometry = !usesNumericLayout(graph, child) && child.type')
  return replace(source, 'if (preservesImportedInstanceInternals(child) &&',
    'if (preservesImportedInstanceInternals(child) && !usesNumericLayout(graph, child) &&')
}

export function correctNumericEvents(source, replace) {
  source = 'import { computeAllLayouts } from "../layout.js";\n' + source
  source = replace(source, 'unbindGraphEvents = options.getGraph().onNodeEvents({', `
    const graph = options.getGraph();
    const unbindNumeric = graph.emitter.on("numericBindings:updated", roots => {
      for (const id of roots) computeAllLayouts(graph, id);
      options.requestRender();
    });
    const unbindNodes = graph.onNodeEvents({`)
  return replace(source, '\t\t});\n\t}\n\treturn { subscribeToGraph };',
    '\t\t});\n\t\tunbindGraphEvents = () => { unbindNumeric(); unbindNodes(); };\n\t}\n\treturn { subscribeToGraph };')
}

export function correctNumericImport(source, replace) {
  source = `import { isNumericBindingField, applyNumericBindings, ownVariableModes } from ${helper};\n` + source
  source = 'import { extractVariableModes } from "./node-change2.js";\n' + source
  source = replace(source, '\tapplyOverridePaints(ov, updates);',
    '\tif (ov.variableModeBySetMap) updates.variableModes = extractVariableModes(ov);\n\tapplyOverridePaints(ov, updates);')
  source = replace(source, "const ownedFields = ['fills', 'strokes',", "const ownedFields = ['variableModes', 'fills', 'strokes',")
  source = replace(source, 'function applyResolvedNumericBindings(graph, activeNodeIds) {',
    `function applyResolvedNumericBindings(graph, activeNodeIds) {
      for (const node of overrideCandidates(graph, activeNodeIds)) {
        if (node.source.fig?.rawNodeFields?.variableModeBySetMap) ownVariableModes(graph, node);
      }
      applyNumericBindings(graph);`)
  return replace(source, 'if (Array.isArray(variableId)) continue;\n\t\t\tconst value = graph.resolveNumberVariableForNode(node.id, variableId);',
    'if (Array.isArray(variableId) || isNumericBindingField(field)) continue;\n\t\t\tconst value = graph.resolveNumberVariableForNode(node.id, variableId);')
}

export function correctNumericNodeExport(source, replace) {
  source = replace(source, 'const variableModeBySetMap = serializeVariableModes(node, context.varIdToGuid, context.modeIdToGuid);',
    `const variableModeBySetMap = node.type === 'INSTANCE' && !node.overrides.variableModes ? undefined :
      serializeVariableModes(node, context.varIdToGuid, context.modeIdToGuid);
    if (!variableModeBySetMap) delete nc.variableModeBySetMap;`)
  source = replace(source, 'const positioned = target !== instance && sourceAbsoluteRecord(target);',
    `const positioned = target !== instance && sourceAbsoluteRecord(target);
    const modeSelection = target !== instance && owns('variableModes') && serializeVariableModes(target, context.varIdToGuid, context.modeIdToGuid);`)
  source = replace(source, 'if (fields.length || paddingFields.length || sized || dashed || positioned) {',
    'if (fields.length || paddingFields.length || sized || dashed || positioned || modeSelection) {')
  source = replace(source, 'const override = { guidPath };',
    'const override = { guidPath };\n      if (modeSelection) override.variableModeBySetMap = modeSelection;')
  return source + '\nexport { extractVariableModes };\n'
}

export function correctNumericExport(source, replace) {
  source = `import { planNumericBindings, applyNumericBindings } from ${helper};\n` + source
  source = 'import { computeAllLayouts } from "../../../layout.js";\n' + source
  return replace(source, 'populateAllLazyFigImportRoots(graph);', `populateAllLazyFigImportRoots(graph);
  const numericPlan = planNumericBindings(graph);
  applyNumericBindings(graph, numericPlan);
  for (const id of numericPlan.roots) computeAllLayouts(graph, id);
  // This is the existing detached export graph. Layout can move unbound
  // children too; their old raw transforms must not replace derived geometry.
  for (const id of numericPlan.nodes) {
    const node = graph.getNode(id);
    node.source = { ...node.source, editedFields: [...new Set([
      ...node.source.editedFields, 'x', 'y', 'width', 'height'
    ])] };
  }`)
}

// Core owns measurement; the scene graph and number validator do not import
// an editor or renderer. Reuse the existing detached graph projection for an
// ordinary value edit and its undo/redo, including the API's non-history edit.
export function correctNumericValueActions(source, replace, propertyModule) {
  source = `import { projectGraphChange, projectVariableGraphChange } from ${JSON.stringify(propertyModule)};\n` + source
  source = `import { planNumericBindings } from ${helper};\n` + source
  source = replace(source, 'setCheckedVariableValue as setNativeVariableValue',
    'setCheckedVariableValue as setUnprojectedVariableValue')
  return source + `
function setNativeVariableValue(graph, variable, modeId, value, present = true) {
  const variables = new Map(graph.variables);
  variables.set(variable.id, { ...variable, valuesByMode: structuredClone(variable.valuesByMode) });
  projectGraphChange({ graph }, planned => {
    setUnprojectedVariableValue(planned.graph, variables.get(variable.id), modeId, value, present);
    for (const id of planNumericBindings(planned.graph).roots) planned.runLayoutForNode(id);
  }, { variables }, () => { variable.valuesByMode = variables.get(variable.id).valuesByMode; });
}
`
}

// The ordinary editor owns layout and history. Its graph-level counterparts
// remain renderer-free; project their effects before notifying a live editor.
export function correctNumericModeActions(source, replace) {
  for (const name of ['addMode', 'removeMode', 'setDefaultMode', 'setActiveMode']) {
    const pattern = new RegExp('ctx\\.graph\\.' + name + '\\(([^;\\n]+)\\)', 'g')
    const matches = [...source.matchAll(pattern)]
    if (!matches.length) throw new Error(`Pinned numeric mode action missing: ${name}`)
    source = source.replaceAll(pattern, (_, args) =>
      `projectVariableGraphChange(ctx.graph, graph => graph.${name}(${args}))`)
  }
  for (const name of ['changeVariableModes', 'restoreDefaultMode', 'removeVariablesWithHistory', 'restoreRemovedVariables']) {
    const suffix = source.includes(name + ',') ? ',' : ' }'
    source = replace(source, name + suffix, `${name} as unprojected${name}${suffix}`)
    source += `\nfunction ${name}(graph, ...args) {
      return projectVariableGraphChange(graph, candidate => unprojected${name}(candidate, ...args));
    }\n`
  }
  return source
}

export function correctNumericProjection(source, replace) {
  source = `import { planNumericBindings, applyNumericBindings } from ${helper};\n` + source
  source = replace(source, 'projectGraphChange };', 'projectGraphChange, projectVariableGraphChange };')
  return source + `
function projectVariableGraphChange(graph, change) {
  const resources = Object.fromEntries(["variables", "variableCollections", "activeMode"]
    .map(field => [field, structuredClone(graph[field])]));
  return projectGraphChange({ graph }, planned => {
    const result = change(planned.graph);
    const plan = planNumericBindings(planned.graph);
    applyNumericBindings(planned.graph, plan);
    for (const id of plan.roots) planned.runLayoutForNode(id);
    return result;
  }, resources, () => {
    // Preserve surviving variable and collection handles, and native order.
    for (const field of ["variables", "variableCollections", "activeMode"]) {
      const next = [...resources[field]].map(([id, value]) => [id,
        field !== "activeMode" && graph[field].has(id) ? Object.assign(graph[field].get(id), value) : value]);
      graph[field].clear();
      for (const [id, value] of next) graph[field].set(id, value);
    }
  });
}
`
}

export function correctNumericBindingActions(source, replace) {
  source = 'import { projectGraphChange } from "./components/properties.js";\n' + source
  source = `import { isNumericBindingField, planNumericBindings } from ${helper};\n` + source
  for (const name of ['bindVariable', 'unbindVariable']) {
    const pattern = new RegExp('ctx\\.graph\\.' + name + '\\(([^;\\n]+)\\)', 'g')
    const matches = [...source.matchAll(pattern)]
    if (!matches.length) throw new Error(`Pinned numeric binding action missing: ${name}`)
    source = source.replaceAll(pattern, (_, args) => `projectBinding("${name}", ${args})`)
    for (const direction of ['Undo', 'Redo']) source = replace(source,
      `console.warn("${direction} ${name} failed:", e instanceof Error ? e.message : String(e));`,
      `if (isNumericBindingField(path)) throw e;\n\t\t\t\t\tconsole.warn("${direction} ${name} failed:", e instanceof Error ? e.message : String(e));`)
  }
  return replace(source, 'function createVariableBindingActions(ctx) {', `function createVariableBindingActions(ctx) {
    function projectBinding(method, nodeId, path, ...args) {
      if (!isNumericBindingField(path)) return ctx.graph[method](nodeId, path, ...args);
      return projectGraphChange(ctx, planned => {
        planned.graph[method](nodeId, path, ...args);
        for (const id of planNumericBindings(planned.graph).roots) planned.runLayoutForNode(id);
      });
    }`)
}

export function correctNumericNodeActions(source, replace) {
  source = 'import { projectGraphChange } from "./components/properties.js";\n' + source
  source = `import { planNumericNodeUpdate, planNumericBindings } from ${helper};\n` + source
  const pattern = /ctx\.graph\.updateNode\(id, (nextChanges|previous)\);/g
  const matches = [...source.matchAll(pattern)]
  if (matches.length !== 4) throw new Error('Pinned numeric node actions changed')
  source = source.replaceAll(pattern, (_, changes) => `updateNumericNode(id, ${changes});`)
  return replace(source, 'function createNodeActions(ctx) {', `function createNodeActions(ctx) {
    function updateNumericNode(id, changes) {
      if (!planNumericNodeUpdate(ctx.graph, id, changes)) return ctx.graph.updateNode(id, changes);
      projectGraphChange(ctx, planned => {
        planned.graph.updateNode(id, changes);
        for (const root of planNumericBindings(planned.graph).roots) planned.runLayoutForNode(root);
        planned.runLayoutForNode(id);
      });
    }`)
}
