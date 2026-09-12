import { ownSourceLayoutRecord, sourceLayoutRecord, sourceCompositionLayout } from './layout-correction.mjs'

// Retain CSS semantics missing from native HUG/FILL. This is private provider
// evidence on the existing source record, not another component contract.
export function planSourceFlex(node) {
  const { style, sizing } = node
  if (!['flex', 'inline-flex'].includes(style.display) || style['flex-direction'] !== 'row' ||
      style['flex-wrap'] !== 'nowrap' || sizing.width !== 'auto' || style['flex-grow'] !== '0' ||
      style['flex-basis'] !== 'auto' || !['0', '1'].includes(style['flex-shrink']) ||
      !['auto', '0px'].includes(sizing['min-width'])) return {}
  return { cssFlex: { version: 1, shrink: Number(style['flex-shrink']),
    autoMinimum: sizing['min-width'] === 'auto' && style['overflow-x'] === 'visible' && style['overflow-y'] === 'visible' } }
}

function sourceFlex(graph, node) {
  const value = sourceLayoutRecord(graph, node)?.cssFlex
  return sourceCompositionLayout(graph, node) && node.layoutMode === 'HORIZONTAL' && node.layoutWrap === 'NO_WRAP' &&
    node.primaryAxisSizing === 'HUG' && node.layoutGrow === 0 && value?.version === 1 && [0, 1].includes(value.shrink) &&
    typeof value.autoMinimum === 'boolean' ? value : undefined
}

function autoSourceText(node) {
  return node.type === 'TEXT' && node.textAutoResize === 'WIDTH_AND_HEIGHT' &&
    ownSourceLayoutRecord(node)?.textWrap === 'normal-v1'
}

function measureText(measure, node, width) {
  const result = measure?.(node, width)
  if (!result || !Number.isFinite(result.width) || result.width < 0 || !Number.isFinite(result.height) || result.height <= 0) {
    throw new Error('Intrinsic source flex requires actual text measurement')
  }
  return { width: Math.ceil(result.width * 64) / 64, height: result.height }
}

function constrainedWidth(node, width) {
  const padding = node.layoutMode === 'NONE' ? 0 : node.paddingLeft + node.paddingRight
  return Math.max(padding, node.minWidth ?? 0, Math.min(node.maxWidth ?? Infinity, width))
}

function intrinsicWidth(graph, node, measure, minimum, visiting = new Set()) {
  if (autoSourceText(node)) return measureText(measure, node, minimum ? 0 : undefined).width
  if (!sourceFlex(graph, node)) return node.width
  if (visiting.has(node.id)) throw new Error('Cyclic intrinsic source flex')
  visiting.add(node.id)
  try {
    const children = graph.getChildren(node.id).filter(child => child.visible && child.layoutPositioning !== 'ABSOLUTE')
    // Child contributions honor their constraints; this item's own flex basis does not.
    // https://www.w3.org/TR/css-flexbox-1/#intrinsic-item-contributions
    return node.paddingLeft + node.paddingRight + node.itemSpacing * Math.max(0, children.length - 1) +
      children.reduce((width, child) => width + constrainedWidth(child, intrinsicWidth(graph, child, measure, minimum, visiting)), 0)
  } finally { visiting.delete(node.id) }
}

export function configureSourceFlex(yoga, graph, node, parent, measure) {
  const flex = sourceFlex(graph, node)
  if (!flex || parent.layoutMode !== 'HORIZONTAL' || !sourceCompositionLayout(graph, parent) || node.figmaDerivedLayout) return
  const natural = intrinsicWidth(graph, node, measure, false), padding = node.paddingLeft + node.paddingRight
  const minimum = flex.autoMinimum ? constrainedWidth(node, intrinsicWidth(graph, node, measure, true)) : padding
  if (node.minWidth == null) yoga.setMinWidth(minimum)
  // CSS scales negative free space by the inner flex base size; this Yoga
  // revision uses the outer basis. Padding/borders must not receive that weight.
  // https://www.w3.org/TR/css-flexbox-1/#resolve-flexible-lengths
  yoga.setFlexBasis(natural)
  yoga.setFlexShrink(flex.shrink * (natural > 0 ? Math.max(0, natural - padding) / natural : 1))
}

export function configureSourceRowText(yoga, graph, node, parent, measure, modes) {
  if (!autoSourceText(node) || !sourceFlex(graph, parent) || node.layoutGrow !== 0 || node.figmaDerivedLayout) return false
  const natural = measureText(measure, node).width, minimum = measureText(measure, node, 0).width
  yoga.setMinWidth(minimum)
  yoga.setFlexShrink(1) // The source's anonymous text flex item has the CSS initial value.
  yoga.setMeasureFunc((width, mode) => {
    const used = Math.max(minimum, mode === modes.Undefined ? natural : mode === modes.Exactly ? width : Math.min(natural, width))
    // A used content box can be wider than its longest painted line.
    return { width: used, height: measureText(measure, node, used).height }
  })
  return true
}
