// Pinned SDK corrections: native paths use existing source lineage, never order.
function chain(graph, first, field) {
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

function sourceChildren(graph, source, instance, overrides) {
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

export const lineageHelpers = [chain, sourceChild, sourceChildren].map(fn => fn.toString()).join('\n')

const exporterHelpers = String.raw`
function nativeOverridePath(context, instance, target, counter) {
  const ancestry = chain(context.graph, target, 'parentId');
  const boundary = ancestry.findIndex(node => node.id === instance.id);
  if (boundary < 1) throw new Error('Native override target is not an instance descendant');
  let source = context.graph.getNode(resolveInstanceComponentId(context, instance.componentId));
  let parent = instance;
  const guids = [];
  for (const child of ancestry.slice(0, boundary).reverse()) {
    if (!source) throw new Error('Missing native override source');
    const linked = sourceChildren(context.graph, source, parent, instance.overrides).get(child.id);
    if (!linked) throw new Error('Missing native override correspondence');
    const guid = getOrCreateNodeGuid(context, linked.id, counter);
    if (!guid) throw new Error('Missing native override GUID');
    guids.push(guid);
    source = linked.type === 'INSTANCE'
      ? context.graph.getNode(resolveInstanceComponentId(context, linked.componentId))
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

function serializeNestedReferences(context, instance, counter) {
  const result = [];
  const pending = [...instance.childIds];
  const seen = new Set();
  while (pending.length) {
    const id = pending.pop();
    if (seen.has(id)) throw new Error('Cyclic native reference subtree');
    seen.add(id);
    const child = context.graph.getNode(id);
    if (!child) throw new Error('Missing native reference child');
    pending.push(...child.childIds);
    if (!child.componentPropertyReferences.length) continue;
    result.push({
      guidPath: nativeOverridePath(context, instance, child, counter),
      componentPropRefs: child.componentPropertyReferences.map(ref => ({
        defID: nativePropertyGuid(ref.propertyId),
        componentPropNodeField: componentPropertyNodeField(ref.field)
      }))
    });
  }
  return result;
}

function serializeTextOverrides(context, instance, counter) {
  const result = [];
  for (const [key, value] of Object.entries(instance.overrides)) {
    if (!key.endsWith(':text')) continue;
    const target = context.graph.getNode(key.slice(0, -5));
    if (typeof value !== 'string' || !target || target.type !== 'TEXT') {
      throw new Error('Invalid native text override');
    }
    result.push({
      guidPath: nativeOverridePath(context, instance, target, counter),
      textData: { characters: value },
      size: { x: target.width, y: target.height }
    });
  }
  return result;
}

function serializePaintOverrides(context, instance, counter) {
  const result = [];
  const nativePaint = paint => {
    const { colorVariableBinding, ...fields } = paint;
    if (colorVariableBinding) fields.colorVar = {
      dataType: 'ALIAS', resolvedDataType: 'COLOR', value: { alias: { guid: colorVariableBinding.variableID } }
    };
    return fields;
  };
  function visit(parent, owners, seen) {
    if (seen.has(parent.id)) throw new Error('Cyclic native paint override subtree');
    const next = new Set(seen).add(parent.id);
    for (const id of parent.childIds) {
      const target = context.graph.getNode(id);
      if (!target || target.parentId !== parent.id) throw new Error('Missing native paint override child');
      const scopes = target.type === 'INSTANCE' ? [...owners, target] : owners;
      const owns = field => scopes.some(owner => Object.hasOwn(owner.overrides,
        owner.id === target.id ? field : target.id + ':' + field));
      const fields = ['fills', 'strokes'].filter(field => owns(field) || owns('boundVariables'));
      if (fields.length) {
        const override = { guidPath: nativeOverridePath(context, instance, target, counter) };
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
      visit(target, scopes, next);
    }
  }
  visit(instance, [instance], new Set());
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
  source = replaceSection(source, 'function serializeTextOverrides(', 'function overridePathKey(',
    lineageHelpers + '\n' + exporterHelpers)
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
    'mergeTextOverrides(symbolOverrides, serializeTextOverrides(context, node, localIdCounter));',
    'mergeTextOverrides(symbolOverrides, serializeNestedReferences(context, node, localIdCounter));\n' +
    '\t\tmergeTextOverrides(symbolOverrides, serializePaintOverrides(context, node, localIdCounter));\n' +
    '\t\tmergeTextOverrides(symbolOverrides, serializeTextOverrides(context, node, localIdCounter));')
  source = replaceOnce(source,
    'if (node.source.fig.componentPropAssignments.length > 0) nc.componentPropAssignments =',
    'if (!node.source.editedFields?.includes("componentPropertyAssignments") && ' +
    'node.source.fig.componentPropAssignments.length > 0) nc.componentPropAssignments =')
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
    ['const id = parseGuidOrNull(def.id);', 'const id = nativePropertyGuid(def.id);'],
    ['const defID = parseGuidOrNull(ref.propertyId);', 'const defID = nativePropertyGuid(ref.propertyId);'],
    ['const defID = parseGuidOrNull(propertyId);', 'const defID = nativePropertyGuid(propertyId);'],
  ]) source = replaceOnce(source, before, after)
  return replaceOnce(source,
    'const definition = context.componentPropertyDefinitionsById.get(propertyId);',
    'const definition = context.componentPropertyDefinitionsById.get(propertyId);\n' +
    '\t\tif (!definition) throw new Error("Missing native component property definition: " + propertyId);')
}

export function correctPaintImporter(source, replace) {
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
      const paintFields = ['fills', 'strokes'].filter(field => Object.hasOwn(patch.props ?? {}, field));
      let paintOwner, paintOverrides;
      if (paintFields.length) {
        let current = ctx.graph.getNode(targetId);
        const visited = new Set();
        while (current) {
          if (visited.has(current.id)) throw new Error('Cyclic native paint override ancestry');
          visited.add(current.id);
          if (!paintOwner && current.type === 'INSTANCE') paintOwner = current;
          if (current.id === nodeId) break;
          current = ctx.graph.getNode(current.parentId);
        }
        if (!current || !paintOwner) throw new Error('Native paint override is outside its declaring instance');
        paintOverrides = { ...paintOwner.overrides };
        for (const paintField of paintFields) {
          const field = paintField === 'strokes' && ov.strokePaints?.length && ov.strokeWeight == null && ov.strokeAlign == null
            ? 'boundVariables' : paintField;
          paintOverrides[paintOwner.id === targetId ? field : targetId + ':' + field] = true;
        }
      }
      applyOverridePatch(ctx, patch);
      if (paintOwner) ctx.graph.preserveSourceMetadataDuring(() =>
        ctx.graph.updateNode(paintOwner.id, { overrides: paintOverrides }));`)
}

export function correctPropertyTarget(source) {
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
  for (const source of match.path) {
    const mapping = sourceChildren(ctx.graph, sourceParent, node, instance.overrides);
    const targets = [...mapping].filter(([, linked]) => linked.id === source.id);
    if (targets.length !== 1) throw new Error('Ambiguous or missing native property correspondence');
    node = ctx.graph.getNode(targets[0][0]);
    sourceParent = source;
  }
  return { node, field: match.field, source: match.source };
}
`)
}
