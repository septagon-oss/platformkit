import { fileURLToPath } from 'node:url'

// Pinned SDK corrections: native paths use existing source lineage, never order.
export function chain(graph, first, field) {
  const result = []
  const seen = new Set()
  for (let node = first; node; node = node[field] ? graph.getNode(node[field]) : null) {
    if (seen.has(node.id)) throw new Error('Cyclic native component lineage')
    seen.add(node.id)
    result.push(node)
    if (node[field] && !graph.getNode(node[field])) throw new Error('Missing native source node')
  }
  return result
}

function sourceChild(graph, source, child, overrides) {
  const explicit = overrides[`${child.id}:sourceComponentId`]
  if (explicit !== undefined && typeof explicit !== 'string') throw new Error('Invalid native source identity')
  const sourceId = typeof explicit === 'string' ? explicit : child.componentId
  if (!sourceId || !graph.getNode(sourceId)) throw new Error('Missing native source identity')
  const direct = chain(graph, graph.getNode(sourceId), 'componentId')
    .find(node => node.parentId === source.id && source.childIds.includes(node.id))
  if (direct) return direct
  // Explicit slots and children recreated by swap undo can name the canonical
  // child of a cloned source occurrence. Require an exact, unique reverse link;
  // never infer correspondence from a shared name or sibling position.
  const declared = graph.getNode(sourceId)
  const owns = chain(graph, source, 'componentId').some(node => node.id === declared.parentId)
  const corresponding = owns ? source.childIds.map(id => graph.getNode(id)).filter(node =>
    node && chain(graph, node, 'componentId').some(link => link.id === sourceId)) : []
  if (corresponding.length === 1) return corresponding[0]
  if (explicit !== undefined) throw new Error('Invalid explicit native source identity')
  // Import can flatten nested INSTANCE.componentId, while descendants still
  // link through the exact original nested instance. Project those witnesses.
  const matches = new Set()
  const pending = [...child.childIds]
  const visited = new Set()
  while (pending.length) {
    const id = pending.pop()
    if (visited.has(id)) throw new Error('Cyclic native instance subtree')
    visited.add(id)
    const descendant = graph.getNode(id)
    if (!descendant) throw new Error('Missing native instance child')
    pending.push(...descendant.childIds)
    if (descendant.componentId && !graph.getNode(descendant.componentId)) {
      throw new Error('Missing native source identity')
    }
    for (const linked of chain(graph, graph.getNode(descendant.componentId), 'componentId')) {
      const candidate = chain(graph, linked, 'parentId').find(node => node.parentId === source.id)
      if (candidate && source.childIds.includes(candidate.id)) matches.add(candidate.id)
    }
  }
  if (matches.size !== 1) throw new Error(`Ambiguous or missing native source identity: ${child.id}`)
  return graph.getNode([...matches][0])
}

export function sourceChildren(graph, source, instance, overrides) {
  const result = new Map()
  const identities = new Set()
  for (const id of instance.childIds) {
    const child = graph.getNode(id)
    if (!child) throw new Error('Missing native instance child')
    const linked = sourceChild(graph, source, child, overrides)
    if (identities.has(linked.id)) throw new Error('Ambiguous duplicate native source identity')
    identities.add(linked.id)
    result.set(id, linked)
  }
  return result
}

function scopedOverrides(target, inherited) {
  if (target.type !== 'INSTANCE') return inherited
  // Qualify root fields before descending. Outer occurrence overrides take
  // precedence over inherited values of an inner instance.
  const own = Object.fromEntries(Object.entries(target.overrides).map(([key, value]) =>
    [key.includes(':') ? key : `${target.id}:${key}`, value]))
  return { ...own, ...inherited }
}

export function ancestryOverrides(graph, target) {
  return chain(graph, target, 'parentId').reverse().reduce((values, node) => scopedOverrides(node, values), {})
}

export const lineageHelpers = [chain, sourceChild, sourceChildren, scopedOverrides, ancestryOverrides].map(fn => fn.toString()).join('\n')

const exporterHelpers = String.raw`
function serializeEditedLayout(node, nc, graph) {
  // Keep untouched FIG layout encodings, including implicit sizing and older
  // alignment aliases. Only changed native fields use the SDK's current encoding.
  const fields = {
    layoutMode: ["stackMode", "stackPadding"],
    itemSpacing: ["stackSpacing"], counterAxisSpacing: ["stackCounterSpacing"],
    paddingLeft: ["stackHorizontalPadding", "stackPaddingRight"],
    paddingRight: ["stackPaddingRight"],
    paddingTop: ["stackVerticalPadding", "stackPaddingBottom"],
    paddingBottom: ["stackPaddingBottom"],
    primaryAxisAlign: ["stackPrimaryAlignItems", "stackJustify"],
    counterAxisAlign: ["stackCounterAlignItems", "stackCounterAlign"],
    primaryAxisSizing: ["stackPrimarySizing"], counterAxisSizing: ["stackCounterSizing"],
    layoutWrap: ["stackWrap"], layoutPositioning: ["stackPositioning"],
    layoutGrow: ["stackChildPrimaryGrow"], layoutAlignSelf: ["stackChildAlignSelf"],
    strokesIncludedInLayout: ["bordersTakeSpace"], itemReverseZIndex: ["stackReverseZIndex"]
  };
  const edited = node.source.editedFields;
  const keys = new Set((edited.includes("layoutMode") ? Object.values(fields) :
    edited.map(field => fields[field] ?? [])).flat());
  if (!keys.size) return;
  const current = {};
  serializeLayoutProps({ ...node, source: { ...node.source, fig: { ...node.source.fig, layout: null } } }, current, graph);
  for (const key of keys) {
    delete nc[key];
    if (Object.hasOwn(current, key)) nc[key] = current[key];
  }
}

function serializeRootSizing(node, symbolID) {
  const size = {};
  for (const [field, axis] of [["width", "x"], ["height", "y"]]) {
    const sizing = (field === "width") === (node.layoutMode === "HORIZONTAL") ? node.primaryAxisSizing : node.counterAxisSizing;
    const explicit = Object.hasOwn(node.overrides, field) || Object.hasOwn(node.overrides, node.id + ":" + field);
    const editedFixed = node.source.format === "fig" && node.source.editedFields.includes(field) &&
      (node.layoutMode === "NONE" || sizing === "FIXED");
    if (explicit || editedFixed) size[axis] = node[field];
  }
  // Kiwi's Vector requires both coordinates. HUG axes remain derived on import;
  // the pair records native size, not a second sizing or provenance language.
  return Object.keys(size).length ? [{ guidPath: { guids: [symbolID] }, size: { x: node.width, y: node.height } }] : [];
}

function nativeOverridePath(context, instance, target, counter) {
  const ancestry = chain(context.graph, target, 'parentId');
  const boundary = ancestry.findIndex(node => node.id === instance.id);
  if (boundary < 1) throw new Error('Native override target is not an instance descendant');
  let source = context.graph.getNode(resolveInstanceComponentId(context, instance.componentId));
  let parent = instance;
  let overrides = {};
  const guids = [];
  for (const child of ancestry.slice(0, boundary).reverse()) {
    if (!source) throw new Error('Missing native override source');
    overrides = scopedOverrides(parent, overrides);
    const linked = sourceChildren(context.graph, source, parent, overrides).get(child.id);
    if (!linked) throw new Error('Missing native override correspondence');
    const guid = getOrCreateNodeGuid(context, linked.id, counter);
    if (!guid) throw new Error('Missing native override GUID');
    guids.push(guid);
    source = linked.type === 'INSTANCE'
      ? context.graph.getNode(resolveInstanceComponentId(context, child.componentId))
      : linked;
    parent = child;
  }
  return { guids };
}

function nativePropertyGuid(value) {
  if (typeof value !== 'string' || !/^(0|[1-9][0-9]*):(0|[1-9][0-9]*)$/.test(value)) {
    throw new Error('Invalid native component property ID: ' + value);
  }
  const [sessionID, localID] = value.split(':').map(Number);
  if (![sessionID, localID].every(part => Number.isSafeInteger(part) && part <= 4294967295)) {
    throw new Error('Native component property ID exceeds uint32');
  }
  return { sessionID, localID };
}

function nativeComponentGuid(context, value, counter) {
  const direct = context.graph.getNode(value);
  const matches = direct?.type === 'COMPONENT' ? [direct] : [...context.graph.getAllNodes()].filter(node =>
    node.type === 'COMPONENT' && [node.source.id, node.componentKey, node.sourceLibraryKey].includes(value));
  if (matches.length !== 1) throw new Error('Missing or ambiguous native replacement component');
  return getOrCreateNodeGuid(context, matches[0].id, counter);
}

function serializeNestedReferences(context, instance, counter) {
  const result = [];
  const pending = instance.childIds.map(id => ({ id, overrides: scopedOverrides(instance, {}) }));
  const seen = new Set();
  while (pending.length) {
    const { id, overrides } = pending.pop();
    if (seen.has(id)) throw new Error('Cyclic native reference subtree');
    seen.add(id);
    const child = context.graph.getNode(id);
    if (!child) throw new Error('Missing native reference child');
    const nested = scopedOverrides(child, overrides);
    pending.push(...child.childIds.map(id => ({ id, overrides: nested })));
    const swapped = Object.hasOwn(overrides, id + ':componentId');
    const renamed = child.source.editedFields.includes('name');
    if (!child.componentPropertyReferences.length && !swapped && !renamed) continue;
    result.push({
      guidPath: nativeOverridePath(context, instance, child, counter),
      ...(renamed ? { name: child.name } : {}),
      ...(swapped ? {
        overriddenSymbolID: nativeComponentGuid(context, resolveInstanceComponentId(context, child.componentId), counter),
        size: { x: child.width, y: child.height }
      } : {}),
      componentPropRefs: child.componentPropertyReferences.map(ref => ({
        defID: nativePropertyGuid(ref.propertyId),
        componentPropNodeField: componentPropertyNodeField(ref.field)
      }))
    });
  }
  return result;
}

function serializePropertyAssignments(context, node, counter) {
  return Object.entries(node.componentPropertyAssignments).map(([propertyId, value]) => {
    const definition = context.componentPropertyDefinitionsById.get(propertyId);
    if (!definition) throw new Error('Missing native component property definition: ' + propertyId);
    return { defID: nativePropertyGuid(propertyId), value: componentPropertyValue(definition.type, value, context, counter) };
  });
}

function serializeTextOverrides(context, instance, counter) {
  const result = [];
  const pending = [instance], seen = new Set(), owners = [];
  while (pending.length) {
    const owner = pending.pop();
    if (seen.has(owner.id)) throw new Error('Cyclic native property override subtree');
    seen.add(owner.id);
    for (const id of owner.childIds) {
      const child = context.graph.getNode(id);
      if (!child || child.parentId !== owner.id) throw new Error('Missing native property override child');
      pending.push(child);
    }
    if (owner.type === 'INSTANCE') owners.push(owner);
  }
  // More distant instance scopes override inherited nested values. The merge
  // uses the last value for each exact native path, so emit outer owners last.
  for (const owner of owners.reverse()) {
    const assignments = serializePropertyAssignments(context, owner, counter);
    if (owner !== instance && assignments.length) result.push({
      guidPath: nativeOverridePath(context, instance, owner, counter), componentPropAssignments: assignments
    });
    for (const [key, value] of Object.entries(owner.overrides)) {
      if (!key.endsWith(':text')) continue;
      const target = context.graph.getNode(key.slice(0, -5));
      if (typeof value !== 'string' || target?.type !== 'TEXT' ||
          !chain(context.graph, target, 'parentId').includes(owner)) throw new Error('Invalid native text override');
      result.push({
        guidPath: nativeOverridePath(context, instance, target, counter),
        textData: { characters: value }, size: { x: target.width, y: target.height }
      });
    }
  }
  return result;
}

function serializeDerivedLayout(context, instance, counter) {
  const result = [];
  function visit(parent, seen) {
    if (seen.has(parent.id)) throw new Error('Cyclic native derived-layout subtree');
    const next = new Set(seen).add(parent.id);
    for (const id of parent.childIds) {
      const target = context.graph.getNode(id);
      if (!target || target.parentId !== parent.id) throw new Error('Missing native derived-layout child');
      if (parent.layoutMode !== 'NONE' && target.layoutPositioning !== 'ABSOLUTE' && target.visible) {
        const size = { x: target.width, y: target.height };
        const transform = context.computeExportTransform(target);
        const values = [size.x, size.y, ...['m00', 'm01', 'm02', 'm10', 'm11', 'm12'].map(key => transform[key])];
        if (size.x < 0 || size.y < 0 || !values.every(value => Number.isFinite(value) && Number.isFinite(Math.fround(value)))) {
          throw new Error('Native derived layout exceeds finite FIG geometry');
        }
        result.push({ guidPath: nativeOverridePath(context, instance, target, counter), size, transform });
      }
      visit(target, next);
    }
  }
  visit(instance, new Set());
  return result;
}

function serializeAppearanceOverrides(context, instance, counter) {
  const result = [];
  const nativePaint = paint => {
    const { colorVariableBinding, ...fields } = paint;
    if (colorVariableBinding) fields.colorVar = {
      dataType: 'ALIAS', resolvedDataType: 'COLOR', value: { alias: { guid: colorVariableBinding.variableID } }
    };
    return fields;
  };
  function visit(target, owners, seen) {
    if (seen.has(target.id)) throw new Error('Cyclic native appearance override subtree');
    const next = new Set(seen).add(target.id);
    const scopes = target.type === 'INSTANCE' ? [...owners, target] : owners;
    const owns = field => scopes.some(owner => Object.hasOwn(owner.overrides,
      owner.id === target.id ? field : target.id + ':' + field));
    const fields = target === instance ? [] : ['fills', 'strokes'].filter(field => owns(field) || owns('boundVariables'));
    const padding = { paddingTop: 'stackVerticalPadding', paddingBottom: 'stackPaddingBottom',
      paddingLeft: 'stackHorizontalPadding', paddingRight: 'stackPaddingRight' };
    const paddingFields = Object.keys(padding).filter(owns);
    const sized = target !== instance && ['width', 'height'].some(owns);
    const dashed = owns('dashPattern');
    const positioned = target !== instance && sourceAbsoluteRecord(target);
    if (fields.length || paddingFields.length || sized || dashed || positioned) {
      const guidPath = target === instance ? { guids: [getOrCreateNodeGuid(context,
        resolveInstanceComponentId(context, instance.componentId), counter)] } : nativeOverridePath(context, instance, target, counter);
      const override = { guidPath };
      if (positioned) override.transform = context.computeExportTransform(target);
      if (dashed) override.dashPattern = [...target.dashPattern];
      if (sized) override.size = { x: target.width, y: target.height };
      for (const field of paddingFields) override[padding[field]] = target[field];
      // FIG's leading-padding override also sets the trailing edge when it
      // is absent. Preserve the actual opposite edge explicitly.
      if (owns('paddingTop')) override.stackPaddingBottom = target.paddingBottom;
      if (owns('paddingLeft')) override.stackPaddingRight = target.paddingRight;
      if (fields.includes('fills')) override.fillPaints = target.fills.map((fill, index) =>
        nativePaint(applyColorVariableBinding(context, target, context.fillToKiwiPaint(fill), 'fills/' + index + '/color')));
      if (fields.includes('strokes')) {
        override.strokePaints = createStrokePaints(context, target).map(nativePaint);
        if (owns('strokes') && target.strokes.length) {
          const first = target.strokes[0];
          if (target.strokes.some(stroke => stroke.weight !== first.weight || stroke.align !== first.align)) {
            throw new Error('Native paint override requires one shared stroke weight and alignment');
          }
          override.strokeWeight = first.weight;
          override.strokeAlign = first.align;
        }
      }
      result.push(override);
    }
    for (const id of target.childIds) {
      const child = context.graph.getNode(id);
      if (!child || child.parentId !== target.id) throw new Error('Missing native appearance override child');
      visit(child, scopes, next);
    }
  }
  visit(instance, [], new Set());
  return result;
}
`

function replaceSection(source, start, end, replacement) {
  const begin = source.indexOf(start)
  const finish = source.indexOf(end, begin)
  if (begin < 0 || finish < 0 || source.indexOf(start, begin + start.length) !== -1) {
    throw new Error('Pinned SDK correction section changed')
  }
  return source.slice(0, begin) + replacement + '\n' + source.slice(finish)
}

export function correctExporter(source, replaceOnce) {
  // Native bindings resolve at render time. Import must not replace an authored
  // fallback with the default-mode value and turn a binding-only edit into paint
  // ownership; mode changes and subsequent glyph replacement need both intact.
  source = replaceOnce(source, 'const resolved = resolveColorVar(paint);',
    'const resolved = paint.color ? undefined : resolveColorVar(paint);')
  // FIG import coalesces duplicate plugin keys. Never let a save silently turn
  // ambiguous source ownership into a single apparently valid declaration.
  source = replaceOnce(source, 'function mergePluginData(pluginData) {', String.raw`
function mergePluginData(pluginData) {
  if (pluginData.filter(entry => entry.pluginId === "platformkit" && entry.key === "platformkit.source").length > 1) {
    throw new Error("Ambiguous duplicate source provenance");
  }`)
  source = replaceSection(source, 'function serializeTextOverrides(', 'function overridePathKey(',
    lineageHelpers + '\n' + exporterHelpers)
  source = replaceOnce(source, '\t\tserializeInheritedCounterAxisStretch(node, nc, graph);\n\t\treturn;',
    '\t\tserializeInheritedCounterAxisStretch(node, nc, graph);\n\t\tserializeEditedLayout(node, nc, graph);\n\t\treturn;')
  source = replaceSection(source, 'function mergeTextOverrides(', '/**\n* Fields that are ALWAYS', String.raw`
function mergeTextOverrides(symbolOverrides, overrides) {
  const merged = new Map();
  for (const override of [...symbolOverrides, ...overrides]) {
    const key = overridePathKey(override);
    if (!key) throw new Error('Native override has no ancestry path');
    for (const guid of override.guidPath.guids) nativePropertyGuid(guid.sessionID + ':' + guid.localID);
    merged.set(key, { ...merged.get(key), ...override });
  }
  symbolOverrides.splice(0, symbolOverrides.length, ...merged.values());
}
`)
  source = replaceOnce(source,
    'if (symbolOverrides.length > 0) symbolData.symbolOverrides = symbolOverrides;',
    'mergeTextOverrides(symbolOverrides, serializeRootSizing(node, symbolID));\n' +
    '\t\tif (symbolOverrides.length > 0) symbolData.symbolOverrides = symbolOverrides;')
  source = replaceOnce(source,
    'mergeTextOverrides(symbolOverrides, serializeTextOverrides(context, node, localIdCounter));',
    'mergeTextOverrides(symbolOverrides, serializeNestedReferences(context, node, localIdCounter));\n' +
    '\t\tmergeTextOverrides(symbolOverrides, serializeAppearanceOverrides(context, node, localIdCounter));\n' +
    '\t\tmergeTextOverrides(symbolOverrides, serializeTextOverrides(context, node, localIdCounter));')
  source = replaceOnce(source,
    'if (node.source.fig.componentPropAssignments.length > 0) nc.componentPropAssignments =',
    'if (!node.source.editedFields?.includes("componentPropertyAssignments") && ' +
    'node.source.fig.componentPropAssignments.length > 0) nc.componentPropAssignments =')
  source = replaceOnce(source,
    '\tif (node.source.fig.derivedSymbolDataLayoutVersion != null) nc.derivedSymbolDataLayoutVersion = node.source.fig.derivedSymbolDataLayoutVersion;',
    String.raw`
  if (node.source.fig.derivedSymbolDataLayoutVersion != null) nc.derivedSymbolDataLayoutVersion = node.source.fig.derivedSymbolDataLayoutVersion;
  const layout = serializeDerivedLayout(context, node, localIdCounter);
  if (layout.length) {
    const derived = nc.derivedSymbolData ?? [];
    mergeTextOverrides(derived, layout);
    nc.derivedSymbolData = derived;
  }`)
  source = replaceOnce(source,
    'function resolveTextAutoResize(node, graph) {\n\tif (node.source.id) return node.textAutoResize;',
    'function resolveTextAutoResize(node, graph) {\n' +
    '\tif (node.source.id || node.textAutoResize === "WIDTH_AND_HEIGHT") return node.textAutoResize;')
  source = replaceOnce(source,
    'if (field === "INSTANCE_SWAP") return "OVERRIDDEN_SYMBOL_ID";\n\treturn "VISIBLE";',
    'if (field === "INSTANCE_SWAP") return "OVERRIDDEN_SYMBOL_ID";\n' +
    '\tif (field === "VISIBLE") return "VISIBLE";\n' +
    '\tthrow new Error("Unsupported native component property field: " + field);')
  for (const [before, after] of [
    ['componentPropertyValue(type, value, graph)', 'componentPropertyValue(type, value, context, localIdCounter)'],
    ['parseGuidOrNull(graph.getNode(value)?.source.id ?? value)', 'nativeComponentGuid(context, value, localIdCounter)'],
    ['function applyComponentMetadata(context, node, nc)', 'function applyComponentMetadata(context, node, nc, localIdCounter)'],
    ['applyComponentMetadata(context, node, nc);', 'applyComponentMetadata(context, node, nc, localIdCounter);'],
    ['componentPropertyValue(def.type, def.defaultValue, context.graph)', 'componentPropertyValue(def.type, def.defaultValue, context, localIdCounter)'],
    ['const id = parseGuidOrNull(def.id);', 'const id = nativePropertyGuid(def.id);'],
    ['const defID = parseGuidOrNull(ref.propertyId);', 'const defID = nativePropertyGuid(ref.propertyId);'],
  ]) source = replaceOnce(source, before, after)
  return replaceSection(source, 'const componentPropAssignments = Object.entries(',
    '\tif (shouldSerializeRawBackedField(node, "componentPropAssignments",',
    'const componentPropAssignments = serializePropertyAssignments(context, node, localIdCounter);')
}

export function correctInstanceImporter(source, replace) {
  const layout = fileURLToPath(new URL('./layout-correction.mjs', import.meta.url))
  source = `import { sourceCompositionLayout } from ${JSON.stringify(layout)};\n` + source
  source = replace(source, '\tapplyGeneratedFreeformStretch(ctx);', String.raw`
  // FIG carries cross-axis fill as child stretch, not a third stack-sizing
  // enum. Restore the source composition's sizing after parent links resolve.
  for (const node of overrideCandidates(graph, ctx.activeNodeIds)) {
    const parent = graph.getNode(node.parentId);
    if (node.layoutAlignSelf !== "STRETCH" || node.layoutPositioning === "ABSOLUTE" ||
        !["HORIZONTAL", "VERTICAL", "GRID"].includes(node.layoutMode) || !["HORIZONTAL", "VERTICAL"].includes(parent?.layoutMode) ||
        !sourceCompositionLayout(graph, node) || !sourceCompositionLayout(graph, parent)) continue;
    const field = (node.layoutMode === "HORIZONTAL") === (parent.layoutMode === "HORIZONTAL") ? "counterAxisSizing" : "primaryAxisSizing";
    graph.preserveSourceMetadataDuring(() => graph.updateNode(node.id, { [field]: "FILL" }));
  }
` + '\tapplyGeneratedFreeformStretch(ctx);')
  source = replace(source, 'preserveInstanceRootBounds(nc.size !== void 0, nodeId, targetId, patch);', String.raw`
      if (targetId === nodeId && ov.size) {
        const node = ctx.graph.getNode(nodeId), overrides = { ...node.overrides };
        for (const [field, axis] of [["width", "x"], ["height", "y"]]) {
          const sizing = (field === "width") === (node.layoutMode === "HORIZONTAL") ? node.primaryAxisSizing : node.counterAxisSizing;
          if (ov.size[axis] != null && (node.layoutMode === "NONE" || sizing === "FIXED")) overrides[field] = true;
        }
        ctx.graph.preserveSourceMetadataDuring(() => ctx.graph.updateNode(nodeId, { overrides }));
      }
      preserveInstanceRootBounds(nc.size !== void 0, nodeId, targetId, patch);`)
  source = String.raw`
function sourceTextLayout(source, target) {
  const width = target.figmaDerivedLayout?.width ?? source.width;
  const height = target.figmaDerivedLayout?.height ?? source.height;
  return { width, height, figmaDerivedTextGlyphs:
    width === source.width && height === source.height && source.figmaDerivedTextGlyphs
      ? markCopySource(source.figmaDerivedTextGlyphs, structuredClone(source.figmaDerivedTextGlyphs)) : undefined };
}
` + source
  source = replace(source, '\t\tprops.width = source.width;\n\t\tprops.height = source.height;',
    '\t\tObject.assign(props, sourceTextLayout(source, child));')
  source = replace(source, '\t\tprops.figmaDerivedTextGlyphs = source.figmaDerivedTextGlyphs ? structuredClone(source.figmaDerivedTextGlyphs) : void 0;\n', '')
  source = replace(source, '\t\tgraph.updateNode(node.id, {\n\t\t\twidth: source.width,\n\t\t\theight: source.height,',
    '\t\tgraph.updateNode(node.id, {\n\t\t\t...sourceTextLayout(source, node),')
  source = replace(source,
    '\t\t\tstyleRuns: copyStyleRuns(source.styleRuns),\n\t\t\tfigmaDerivedTextGlyphs: source.figmaDerivedTextGlyphs ? markCopySource(source.figmaDerivedTextGlyphs, structuredClone(source.figmaDerivedTextGlyphs)) : void 0',
    '\t\t\tstyleRuns: copyStyleRuns(source.styleRuns)')
  source = lineageHelpers + '\n' + source
  source = replace(source,
    'if (!srcNode || !tgtNode || srcNode.type !== tgtNode.type) continue;',
    'if (!srcNode || !tgtNode || srcNode.type !== tgtNode.type || isFieldProtected(protections, tgtNode.id, "structure")) continue;')
  source = replace(source,
    'if (srcNode.type === "INSTANCE" && srcNode.componentId !== tgtNode.componentId) {',
    'if (srcNode.type === "INSTANCE" && chain(graph, srcNode, "componentId").at(-1).id !== chain(graph, tgtNode, "componentId").at(-1).id) {')
  source = replaceSection(source, 'function buildSizeOverriddenCloneUpdates(', 'function buildCloneUpdates(', String.raw`
function buildSizeOverriddenCloneUpdates(source, clone) {
  if (clone.type !== 'INSTANCE' || !source.figmaDerivedLayout) return {};
  // A placed occurrence's explicit derived position wins over the reusable
  // template. A size-only override still inherits unspecified coordinates.
  const layout = { ...source.figmaDerivedLayout, ...clone.figmaDerivedLayout };
  return {
    ...(layout.x === undefined ? {} : { x: layout.x }),
    ...(layout.y === undefined ? {} : { y: layout.y }),
    figmaDerivedLayout: layout
  };
}`)
  source = 'import { extractComponentPropertyAssignments } from "./node-change2.js";\n' + source
  // Lazy population may read linked sources on other pages, but must write
  // only the requested subtree. Loaded-page paint and placement edits win.
  source = replace(source,
    'propagateResolvedFills(graph, /* @__PURE__ */ new Set([...ctx.kiwiPropertyNodes, ...overriddenNodes]));',
    'propagateResolvedFills(graph, new Set([...ctx.kiwiPropertyNodes, ...overriddenNodes]), [...overrideCandidates(graph, ctx.activeNodeIds)]);')
  source = replace(source, 'propagateResolvedChildPlacementClones(graph);',
    'propagateResolvedChildPlacementClones(graph, instancePlacementPairs(graph).filter(pair => !ctx.activeNodeIds || ctx.activeNodeIds.has(pair.childId)));')
  source = replace(source, '\tdelete fields.componentPropAssignments;', '')
  source = replace(source, 'function convertOverrideToProps(ov) {\n  const updates = {};',
    `function convertOverrideToProps(ov) {\n  const updates = {};
  if (ov.componentPropAssignments) updates.componentPropertyAssignments = extractComponentPropertyAssignments(ov);`)
  source = replace(source, 'overriddenNodes.add(targetId);', String.raw`
    if (patch.swapComponentId) {
      const key = guidToString(guids.at(-1));
      const sourceId = ctx.guidToNodeId.get(ctx.overrideKeyToGuid.get(key) ?? key);
      const owner = ctx.graph.getNode(nodeId);
      if (!owner || !ctx.graph.getNode(sourceId)) throw new Error('Missing native replacement occurrence');
      ctx.graph.preserveSourceMetadataDuring(() => ctx.graph.updateNode(nodeId, { overrides: {
        ...owner.overrides, [targetId + ':sourceComponentId']: sourceId, [targetId + ':componentId']: patch.swapComponentId
      } }));
    }
    overriddenNodes.add(targetId);`)
  source = replace(source, 'const props = convertOverrideToProps(fields);', String.raw`
    const props = convertOverrideToProps(fields);
    const target = ctx.graph.getNode(targetId);
    if (props.strokes && target) props.strokes = props.strokes.map((stroke, index) => ({
      ...stroke,
      ...(fields.strokeWeight == null && target.strokes[index] ? { weight: target.strokes[index].weight } : {}),
      ...(fields.strokeAlign == null && target.strokes[index] ? { align: target.strokes[index].align } : {})
    }));`)
  source = replace(source,
    'if (props.boundVariables) props.boundVariables = {\n\t\t\t\t...target.boundVariables,\n\t\t\t\t...props.boundVariables\n\t\t\t};',
    String.raw`if (props.boundVariables || Object.hasOwn(props, 'fills') || Object.hasOwn(props, 'strokes')) {
      const replaced = ['fills', 'strokes'].filter(field => Object.hasOwn(props, field));
      props.boundVariables = {
        ...Object.fromEntries(Object.entries(target.boundVariables).filter(([key]) =>
          !replaced.some(field => key === field || key.startsWith(field + '/')))),
        ...props.boundVariables
      };
    }`)
  return replace(source,
    'overriddenNodes.add(targetId);\n\t\t\tapplyOverridePatch(ctx, patch);',
    String.raw`overriddenNodes.add(targetId);
      const target = ctx.graph.getNode(targetId);
      const sizingSource = target?.type === 'INSTANCE' ? chain(ctx.graph,
        ctx.graph.getNode(patch.swapComponentId ?? target.componentId), 'componentId').at(-1) : null;
      const ownedFields = ['fills', 'strokes', 'dashPattern', 'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft', 'width', 'height']
        .filter(field => Object.hasOwn(patch.props ?? {}, field) && (!['width', 'height'].includes(field) ||
          sizingSource && Math.fround(patch.props[field]) !== Math.fround(sizingSource[field] * (target.uniformScaleFactor ?? 1)) &&
          (target.layoutMode === 'NONE' || ((field === 'width') === (target.layoutMode === 'HORIZONTAL') ?
            target.primaryAxisSizing : target.counterAxisSizing) === 'FIXED')));
      let owner, overrides;
      if (ownedFields.length) {
        let current = ctx.graph.getNode(targetId);
        const visited = new Set();
        while (current) {
          if (visited.has(current.id)) throw new Error('Cyclic native appearance override ancestry');
          visited.add(current.id);
          if (!owner && current.type === 'INSTANCE') owner = current;
          if (current.id === nodeId) break;
          current = ctx.graph.getNode(current.parentId);
        }
        if (!current || !owner) throw new Error('Native appearance override is outside its declaring instance');
        overrides = { ...owner.overrides };
        for (const owned of ownedFields) {
          const field = owned === 'strokes' && ov.strokePaints?.length && ov.strokeWeight == null && ov.strokeAlign == null
            ? 'boundVariables' : owned;
          overrides[owner.id === targetId ? field : targetId + ':' + field] = true;
        }
      }
      applyOverridePatch(ctx, patch);
      if (owner) ctx.graph.preserveSourceMetadataDuring(() =>
        ctx.graph.updateNode(owner.id, { overrides }));`)
}

export function correctPropertyTarget(source, replace) {
  source = replace(source, '(instance.componentId ? ctx.graph.getNode(instance.componentId) : null)?.componentPropertyValues[definition.name]',
    'chain(ctx.graph, instance, "componentId").at(-1)?.componentPropertyValues[definition.name]')
  source = replace(source, 'const component = ctx.graph.getNode(instance.componentId);',
    `const component = chain(ctx.graph, instance, 'componentId').at(-1);
  if (component?.type !== 'COMPONENT') throw new Error('Missing native component definition owner');`)
  return replaceSection(source, 'function findPropertyPath(', 'function swapTargetId(',
    lineageHelpers + '\n' + String.raw`
function propertyTarget(ctx, instance, propertyId) {
  const component = chain(ctx.graph, instance, 'componentId').at(-1);
  if (!component || component.type !== 'COMPONENT') throw new Error('Missing native component');
  const matches = [];
  function visit(source, path, ancestors) {
    if (ancestors.has(source.id)) throw new Error('Cyclic component property subtree');
    const next = new Set(ancestors).add(source.id);
    for (const id of source.childIds) {
      const child = ctx.graph.getNode(id);
      if (!child) throw new Error('Missing native property source');
      const refs = child.componentPropertyReferences.filter(ref => ref.propertyId === propertyId);
      for (const ref of refs) matches.push({ path: [...path, child], source: child, field: ref.field });
      visit(child, [...path, child], next);
    }
  }
  visit(component, [], new Set());
  if (matches.length !== 1) throw new Error('Ambiguous or missing native component property target');
  const match = matches[0];
  if (!['TEXT', 'VISIBLE', 'INSTANCE_SWAP'].includes(match.field)) {
    throw new Error('Unsupported native component property field: ' + match.field);
  }
  let sourceParent = component;
  let node = instance;
  let overrides = ancestryOverrides(ctx.graph, instance);
  for (const source of match.path) {
    overrides = scopedOverrides(node, overrides);
    const mapping = sourceChildren(ctx.graph, sourceParent, node, overrides);
    const targets = [...mapping].filter(([, linked]) => linked.id === source.id);
    if (targets.length !== 1) throw new Error('Ambiguous or missing native property correspondence');
    node = ctx.graph.getNode(targets[0][0]);
    sourceParent = source;
  }
  return { node, field: match.field, source: match.source };
}
`)
}
