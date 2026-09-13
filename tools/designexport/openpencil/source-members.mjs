const key = value => JSON.stringify(value)
const within = (path, parent) => parent.length <= path.length && parent.every((part, index) => path[index] === part)
const requireMembers = (condition, message) => {
  if (!condition) throw new Error(`Native component: source members ${message}`)
}

// Selection bounds locate a logical owner in the editor. They are never a
// formatting box, an intrinsic size declaration or a source layout witness.
export function memberSelection(plans) {
  const boxes = plans.map(plan => plan.fragment ? memberSelection(plan.children) : plan.observation.bounds)
  requireMembers(boxes.length > 0 && boxes.every(box => ['x', 'y', 'width', 'height'].every(field => Number.isFinite(box[field])) &&
    box.width >= 0 && box.height >= 0), 'require finite nonnegative member geometry')
  const x = Math.min(...boxes.map(box => box.x)), y = Math.min(...boxes.map(box => box.y))
  const bounds = { x, y, width: Math.max(...boxes.map(box => box.x + box.width)) - x,
    height: Math.max(...boxes.map(box => box.y + box.height)) - y }
  requireMembers(Object.values(bounds).every(value => Number.isFinite(Math.fround(value))),
    'selection geometry must fit native coordinates')
  return bounds
}

// Extend the existing occurrence map; do not discover identities from DOM names
// or create another catalog. Old captures keep their single-root admission path.
export function planSourceMembers(observation, occurrences) {
  const fragments = [], records = observation.sourceOccurrences
  if (records === undefined) return { hasFragments: false, owner: (_node, owner) => owner, group: (_node, plans) => plans }
  requireMembers(Array.isArray(records), 'require an explicit correspondence array')
  const nodes = new Map(), captured = new Map()
  function visit(node) {
    if (node.kind === 'element') {
      requireMembers(Array.isArray(node.domPath) && !nodes.has(key(node.domPath)), 'require unique observed DOM addresses')
      nodes.set(key(node.domPath), node)
    }
    for (const child of node.children ?? []) visit(child)
  }
  for (const root of observation.roots) visit(root)
  for (const record of records) {
    requireMembers(Array.isArray(record?.path) && !captured.has(key(record.path)), 'have duplicate or missing source paths')
    captured.set(key(record.path), record)
  }
  for (const occurrence of occurrences.values()) {
    const record = captured.get(key(occurrence.path))
    requireMembers(record && record.componentId === occurrence.description.componentId && record.slot === occurrence.slot,
      'must match the exact source identity and slot')
    requireMembers(record.correspondence === 'observed' && Array.isArray(record.members), 'require observed source boundaries')
    const members = record.members.filter(member => !(member.kind === 'comment' &&
      member.presentation?.state === 'suppressed' && member.presentation.reason === 'comment'))
    requireMembers(members.length > 0, 'empty output needs an observed insertion context')
    const addresses = members.map(member => key(member.domPath))
    requireMembers(new Set(addresses).size === members.length && members.every(member => member.kind === 'element' &&
      member.presentation?.state === 'box-observed' && Array.isArray(member.domPath) &&
      member.domPath.every(step => Number.isSafeInteger(step) && step >= 0)),
    'require distinct boxed element members; dormant content and text ranges need their own conversion')
    for (const member of members) {
      const node = nodes.get(key(member.domPath))
      requireMembers(node && node.tag === member.tag, 'must resolve to the exact observed element')
    }
    if (members.length === 1) {
      const source = nodes.get(addresses[0]).source
      requireMembers(source && key(source.path) === key(occurrence.path) && source.componentId === record.componentId && source.slot === record.slot,
        'single element ownership conflicts with the observed source root')
      continue
    }
    const parentPath = members[0].domPath.slice(0, -1)
    requireMembers(parentPath.length > 0 && members.every((member, index) => key(member.domPath.slice(0, -1)) === key(parentPath) &&
      (index === 0 || member.domPath.at(-1) > members[index - 1].domPath.at(-1))),
    'fragments need ordered siblings inside an observed containing box')
    const parent = nodes.get(key(parentPath))
    requireMembers(parent && ['flex', 'inline-flex', 'grid'].includes(parent.style.display),
      'fragment formatting belongs to an observed flex or grid parent')
    requireMembers(members.every(member => nodes.get(key(member.domPath)).style.position !== 'absolute'),
      'absolute members need an effective containing-block model')
    fragments.push({ occurrence, addresses, parentPath })
  }
  const rootPath = [...occurrences.values()][0]?.path
  requireMembers([...captured.values()].filter(record => within(record.path, rootPath)).length === occurrences.size,
    'contain unknown source occurrences')
  for (const [index, fragment] of fragments.entries()) for (const other of fragments.slice(index + 1)) {
    const overlap = fragment.addresses.some(address => other.addresses.includes(address))
    if (!overlap) continue
    const [outer, inner] = fragment.occurrence.path.length < other.occurrence.path.length ? [fragment, other] : [other, fragment]
    requireMembers(within(inner.occurrence.path, outer.occurrence.path) && inner.addresses.every(address => outer.addresses.includes(address)),
      'overlapping members require nested source ownership')
  }
  return {
    hasFragments: fragments.length > 0,
    owner(node, fallback) {
      const chain = fragments.filter(fragment => fragment.addresses.includes(key(node.domPath)))
        .toSorted((a, b) => a.occurrence.path.length - b.occurrence.path.length)
      let owner = fallback
      for (const fragment of chain) {
        requireMembers(owner && key(fragment.occurrence.path.slice(0, -1)) === key(owner.path),
          'cannot skip a boxed source ancestor')
        owner = fragment.occurrence
      }
      return owner
    },
    group(node, plans, owner, seen) {
      const groups = fragments.filter(fragment => key(fragment.parentPath) === key(node.domPath))
        .toSorted((a, b) => b.occurrence.path.length - a.occurrence.path.length)
      if (!groups.length) return plans
      requireMembers(node.children.length === plans.length && node.children.every(child => child.kind === 'element'),
        'need exact direct-element contribution handles')
      const entries = plans.map((plan, index) => ({ plan, addresses: [key(node.children[index].domPath)] }))
      const pending = []
      function owned(plans, owner) {
        for (const plan of plans) {
          if (plan.kind === 'component') requireMembers(key(plan.occurrence.path.slice(0, -1)) === key(owner.path),
            'component belongs to another logical source parent')
          else owned(plan.children ?? [], owner)
        }
      }
      for (const fragment of groups) {
        requireMembers(within(fragment.occurrence.path, owner.path) && !seen.has(key(fragment.occurrence.path)),
          'fragment belongs to another source owner or was repeated')
        const indexes = entries.flatMap((entry, index) => entry.addresses.some(address => fragment.addresses.includes(address)) ? [index] : [])
        const selected = indexes.map(index => entries[index])
        requireMembers(indexes.length > 0 && indexes.every((index, offset) => index === indexes[0] + offset) &&
          key(selected.flatMap(entry => entry.addresses)) === key(fragment.addresses), 'must cover exact contiguous source contributions')
        const children = selected.map(entry => entry.plan), bounds = memberSelection(children)
        owned(children, fragment.occurrence)
        const plan = { kind: 'component', fragment: true, occurrence: fragment.occurrence, children, native: {
          width: bounds.width, height: bounds.height, layoutMode: 'NONE', primaryAxisSizing: 'HUG', counterAxisSizing: 'HUG',
          clipsContent: false, fills: [],
        } }
        entries.splice(indexes[0], indexes.length, { plan, addresses: fragment.addresses })
        pending.push(key(fragment.occurrence.path))
      }
      owned(entries.map(entry => entry.plan), owner)
      for (const path of pending) seen.add(path)
      return entries.map(entry => entry.plan)
    },
  }
}
