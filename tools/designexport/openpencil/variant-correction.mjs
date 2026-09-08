import { chain } from './exporter-correction.mjs'

// The native set owns definitions; its direct variants own values and FIG
// specifications keyed by definition ID. History must retain all three together.
const helpers = String.raw`
function variantDefinitionState(ctx, owner) {
  return [owner, ...ctx.graph.getChildren(owner.id).filter(node => node.type === "COMPONENT")].map(node => ({
    id: node.id,
    editedFields: [...node.source.editedFields],
    props: structuredClone(node === owner ? {
      componentPropertyDefinitions: node.componentPropertyDefinitions
    } : {
      componentPropertyValues: node.componentPropertyValues,
      variantPropSpecs: node.variantPropSpecs
    })
  }));
}

function restoreVariantDefinitionState(ctx, state, preserveSource) {
  for (const entry of state) {
    if (!ctx.graph.getNode(entry.id)) throw new Error("Native variant: missing history owner");
  }
  ctx.withoutComponentSync(() => {
    const apply = () => {
      for (const entry of state) {
        const node = ctx.graph.getNode(entry.id);
        const props = structuredClone(entry.props);
        if (preserveSource) {
          // Restore only this operation's dirty markers. An unrelated paint,
          // layout or display-name edit retains its own source metadata.
          const current = node.source.editedFields, owns = field => Object.hasOwn(props, field);
          const editedFields = entry.editedFields.filter(field => owns(field) || current.includes(field));
          editedFields.push(...current.filter(field => !owns(field) && !editedFields.includes(field)));
          props.source = { ...node.source, editedFields };
        }
        const changed = Object.fromEntries(Object.entries(props).filter(([field, value]) => !isEqual(node[field], value)));
        if (Object.keys(changed).length) ctx.graph.updateNode(entry.id, changed);
      }
    };
    if (preserveSource) ctx.graph.preserveSourceMetadataDuring(apply);
    else apply();
  });
}

function changeVariantDefinition(ctx, componentSetId, propertyId, newName, removing = false) {
  const owner = ctx.graph.getNode(componentSetId);
  if (owner?.type !== "COMPONENT_SET") return;
  const definitions = owner.componentPropertyDefinitions;
  const matches = definitions.filter(definition => definition.id === propertyId);
  if (matches.length === 0) return;
  const definition = matches[0];
  if (matches.length !== 1 || definitions.filter(item => item.name === definition.name).length !== 1) {
    throw new Error("Native variant: ambiguous property definition");
  }
  if (!removing) {
    if (typeof newName !== "string" || newName.length === 0 ||
        definitions.some(item => item.id !== propertyId && item.name === newName)) {
      throw new Error("Native variant: invalid or occupied property name");
    }
    if (newName === definition.name) return;
  }
  const before = variantDefinitionState(ctx, owner), planned = structuredClone(before);
  planned[0].props.componentPropertyDefinitions = removing ? definitions.filter(item => item.id !== propertyId) :
    definitions.map(item => item.id === propertyId ? { ...item, name: newName } : item);
  for (const entry of planned.slice(1)) {
    if (definition.type !== "VARIANT") continue;
    const values = entry.props.componentPropertyValues;
    if (!removing && Object.hasOwn(values, newName)) throw new Error("Native variant: occupied variant value name");
    entry.props.componentPropertyValues = Object.fromEntries(Object.entries(values).flatMap(([name, value]) =>
      name !== definition.name ? [[name, value]] : removing ? [] : [[newName, value]]));
    if (removing) entry.props.variantPropSpecs = entry.props.variantPropSpecs.filter(spec => spec.propDefId !== propertyId);
  }
  let after;
  try {
    restoreVariantDefinitionState(ctx, planned, false);
    after = variantDefinitionState(ctx, owner);
  } catch (error) {
    restoreVariantDefinitionState(ctx, before, true);
    throw error;
  }
  const replay = (expected, replacement) => {
    const owned = state => state.map(({ id, props }) => ({ id, props }));
    if (ctx.graph.getNode(owner.id) !== owner || !isEqual(owned(variantDefinitionState(ctx, owner)), owned(expected))) {
      throw new Error("Native variant: stale definition history");
    }
    try {
      restoreVariantDefinitionState(ctx, replacement, true);
    } catch (error) {
      restoreVariantDefinitionState(ctx, expected, true);
      throw error;
    }
    ctx.requestRender();
  };
  ctx.undo.push({
    label: removing ? "Remove property" : "Rename property",
    forward: () => replay(before, after),
    inverse: () => replay(after, before)
  });
  ctx.requestRender();
}
`

export function correctVariantActions(source, replace) {
  source = 'import { isEqual } from "es-toolkit";\n' + source
  source = replace(source, 'function createVariantActions(ctx) {', chain.toString() + '\n' + helpers + '\nfunction createVariantActions(ctx) {')
  source = replace(source, 'const component = ctx.graph.getNode(instance.componentId);',
    'const component = chain(ctx.graph, instance, "componentId").at(-1);')
  source = replace(source, 'const prevComponentId = instance.componentId;',
    'const prevComponentId = instance.componentId, prevMasterId = component.id;')
  source = replace(source, 'ctx.graph.swapInstanceComponent(instanceId, prevComponentId);',
    'ctx.graph.swapInstanceComponent(instanceId, prevMasterId);\n' +
    '\t\t\t\tctx.graph.updateNode(instanceId, { componentId: prevComponentId });')
  for (const [name, next, argumentsList, callArguments] of [
    ['removePropertyDefinition', 'renamePropertyDefinition', 'componentSetId, propertyId', 'componentSetId, propertyId, undefined, true'],
    ['renamePropertyDefinition', 'collectVariantOptions', 'componentSetId, propertyId, newName', 'componentSetId, propertyId, newName'],
  ]) {
    const start = source.indexOf(`\tfunction ${name}(`), end = source.indexOf(`\tfunction ${next}(`)
    if (start < 0 || end <= start) throw new Error(`Native variant: missing ${name} action boundary`)
    source = replace(source, source.slice(start, end),
      `\tfunction ${name}(${argumentsList}) {\n\t\treturn changeVariantDefinition(ctx, ${callArguments});\n\t}\n`)
  }
  source = replace(source, 'import { omit } from "es-toolkit/object";\n', '')
  return replace(source, 'def.type === "VARIANT" && def.defaultValue', 'def.type === "VARIANT" && typeof def.defaultValue === "string"')
}

// Property names are data, including __proto__. Resolve the existing native
// ID specifications without invoking Object.prototype setters during import.
export function correctVariantImport(source, replace) {
  return replace(source, '\t\tconst values = {};\n\t\tfor (const spec of node.variantPropSpecs) values[defs.get(spec.propDefId) ?? spec.propDefId] = spec.value;',
    '\t\tconst values = Object.fromEntries(node.variantPropSpecs.map(spec => [defs.get(spec.propDefId) ?? spec.propDefId, spec.value]));')
}

export function correctVariantNodeChange(source, replace) {
  return replace(source, '\t\tconst values = {};\n\t\tfor (const spec of specs) values[defs.get(spec.propDefId) ?? spec.propDefId] = spec.value;\n\t\treturn values;',
    '\t\treturn Object.fromEntries(specs.map(spec => [defs.get(spec.propDefId) ?? spec.propDefId, spec.value]));')
}
