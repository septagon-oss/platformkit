// Byte spans are evidence from one Go render, not durable DOM addresses. Add
// temporary numeric boundaries without interpolating source identities as HTML.
export function prepareCaptureSource(example) {
  const occurrences = [], active = new Set(), decode = bytes => new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  function render(description, path, slot, reason) {
    if (active.has(description) || !description.id?.trim() || !description.componentId?.trim() ||
      typeof description.html !== 'string' || /<!--\/?pk-capture:/.test(description.html)) {
      throw new Error('Invalid source occurrence or reserved capture marker')
    }
    active.add(description)
    const source = { path, componentId: description.componentId, ...(slot ? { slot } : {}) }
    const index = occurrences.push({ source, ...(reason ? { reason } : {}) }) - 1
    const bytes = Buffer.from(description.html), ids = new Set(), children = description.children ?? []
    for (const child of children) {
      if (!child.description?.id?.trim() || ids.has(child.description.id)) throw new Error('Duplicate or missing source occurrence identity')
      ids.add(child.description.id)
    }
    for (const child of children.filter(child => reason || child.span == null)) {
      render(child.description, [...path, child.description.id], child.slot, reason ? 'unobserved-ancestor' : 'unobserved-span')
    }
    if (reason) {
      active.delete(description)
      return ''
    }
    let offset = 0, html = ''
    for (const child of children.filter(child => child.span != null).toSorted((a, b) => a.span.start - b.span.start || a.span.end - b.span.end)) {
      const { start, end } = child.span
      if (![start, end].every(Number.isSafeInteger) || start < offset || end < start || end > bytes.length ||
        !bytes.subarray(start, end).equals(Buffer.from(child.description.html))) {
        throw new Error('Invalid or overlapping source occurrence byte span')
      }
      html += decode(bytes.subarray(offset, start))
      html += render(child.description, [...path, child.description.id], child.slot)
      offset = end
    }
    active.delete(description)
    return `<!--pk-capture:${index}-->${html}${decode(bytes.subarray(offset))}<!--/pk-capture:${index}-->`
  }
  return { html: render(example, [example.id]), occurrences }
}

// Runs in the disposable browser. Parse exact boundaries before removing them;
// normal text/slot observation then sees the original DOM, not instrumentation.
export function indexCaptureSources({ occurrences, html }) {
  const original = document.createElement('template')
  original.innerHTML = html
  const markers = [], stack = [], seen = new Set(), sources = new WeakMap()
  const members = new Map(), textMembers = new WeakMap(), templateOwners = new WeakMap()
  function* descendants(parent) {
    for (const node of parent.childNodes) {
      yield node
      if (node instanceof HTMLTemplateElement) {
        templateOwners.set(node.content, node)
        yield* descendants(node.content)
      } else yield* descendants(node)
    }
  }
  function member(node) {
    const result = { node }
    if (node.nodeType === Node.TEXT_NODE) {
      result.start = 0
      result.end = node.length
      if (!textMembers.has(node)) textMembers.set(node, new Set())
      textMembers.get(node).add(result)
    }
    return result
  }
  for (const node of descendants(document.body)) {
    if (node.nodeType !== Node.COMMENT_NODE) continue
    if (!/^\/?pk-capture:/.test(node.data)) continue
    const match = /^(\/?)pk-capture:(\d+)$/.exec(node.data)
    if (!match || !occurrences[Number(match[2])] || occurrences[Number(match[2])].reason) throw new Error('Invalid capture marker')
    const index = Number(match[2])
    markers.push(node)
    if (!match[1]) {
      if (seen.has(index)) throw new Error('Duplicate source occurrence capture marker')
      seen.add(index)
      stack.push({ index, start: node })
      continue
    }
    const opening = stack.pop()
    if (opening?.index !== index || opening.start.parentNode !== node.parentNode) {
      throw new Error('Source occurrence boundaries moved or crossed during HTML parsing')
    }
    const parent = node.parentElement
    if (parent?.namespaceURI === 'http://www.w3.org/1999/xhtml' && ['table', 'tbody', 'thead', 'tfoot', 'tr'].includes(parent.localName)) {
      // Foster parenting can move output outside two otherwise intact markers.
      // Whole table roots and ordinary td/th content do not have this context.
      throw new Error('Source occurrence has an unsupported HTML table parsing context')
    }
    const content = []
    for (let current = opening.start.nextSibling; current !== node; current = current?.nextSibling) {
      if (!current) throw new Error('Unreachable source occurrence closing boundary')
      if (!(current.nodeType === Node.COMMENT_NODE && /^\/?pk-capture:/.test(current.data))) content.push(member(current))
    }
    members.set(index, content)
    // Fragments are observable, but not a single native component root.
    const roots = content.map(item => item.node).filter(node => node.nodeType !== Node.COMMENT_NODE &&
      !(node.nodeType === Node.TEXT_NODE && node.data === ''))
    if (roots.length === 1 && roots[0].nodeType === Node.ELEMENT_NODE) {
      if (sources.has(roots[0])) throw new Error('Ambiguous source occurrences own the same DOM root')
      sources.set(roots[0], occurrences[index].source)
    }
  }
  const observedCount = occurrences.filter(occurrence => !occurrence.reason).length
  if (stack.length || seen.size !== observedCount || markers.length !== observedCount * 2) {
    throw new Error('Missing source occurrence capture boundaries after HTML parsing')
  }
  for (const node of markers) {
    const before = node.previousSibling, after = node.nextSibling
    node.remove()
    // Restore only the text adjacency that our own marker interrupted; an
    // unrelated caller-created text-node split must remain observable.
    if (before?.nodeType === Node.TEXT_NODE && after?.nodeType === Node.TEXT_NODE) {
      const offset = before.length
      if (!textMembers.has(before)) textMembers.set(before, new Set())
      for (const item of textMembers.get(after) ?? []) {
        item.node = before
        item.start += offset
        item.end += offset
        textMembers.get(before).add(item)
      }
      textMembers.delete(after)
      before.appendData(after.data)
      after.remove()
    }
  }
  if (document.body.innerHTML !== original.innerHTML) throw new Error('Source occurrence instrumentation changed HTML parsing')
  const addresses = new WeakMap()
  function address(parent, path) {
    for (const [index, node] of [...parent.childNodes].entries()) {
      const current = [...path, index]
      addresses.set(node, current)
      if (node instanceof HTMLTemplateElement) address(node.content, [...current, 'content'])
      else address(node, current)
    }
  }
  address(document.body, [])
  function presentation(item) {
    const { node } = item
    const element = node.nodeType === Node.ELEMENT_NODE ? node : node.parentElement
    let inert = false, templateContent = false
    for (let current = node; current; current = current.parentNode ?? templateOwners.get(current)) {
      if (templateOwners.has(current)) templateContent = true
      if (current instanceof HTMLElement && current.inert) inert = true
    }
    const result = (state, reason) => ({ state, ...(reason ? { reason } : {}), inert })
    if (node.nodeType === Node.COMMENT_NODE) return result('suppressed', 'comment')
    if (templateContent) return result('suppressed', 'template-content')
    if (!element) return result('unresolved', 'no-client-box')
    for (let current = element; current; current = current.parentElement) {
      const style = getComputedStyle(current)
      if (style.display === 'none') return result('suppressed', 'display-none')
      if (style.display !== 'contents' && Number.parseFloat(style.opacity) === 0) return result('suppressed', 'opacity-zero')
      // A content-visibility container may still paint its own box.
      if (current !== node && style.contentVisibility === 'hidden') {
        const boxed = current.namespaceURI === 'http://www.w3.org/1999/xhtml' &&
          ['block', 'flow-root', 'flex', 'grid', 'inline-block', 'inline-flex', 'inline-grid'].includes(style.display)
        return boxed ? result('suppressed', 'content-visibility-hidden') : result('unresolved', 'content-visibility-context')
      }
      if (style.contentVisibility === 'auto') return result('unresolved', 'content-visibility-auto')
    }
    if (['hidden', 'collapse'].includes(getComputedStyle(element).visibility)) return result('suppressed', 'visibility-hidden')
    const range = document.createRange()
    if (node.nodeType === Node.TEXT_NODE) {
      range.setStart(node, item.start)
      range.setEnd(node, item.end)
    }
    const rects = node.nodeType === Node.TEXT_NODE ? range.getClientRects() : node.getClientRects()
    // A client box is evidence of layout, never proof of visible pixels,
    // interaction, accessibility, native conversion or subtree visibility.
    return [...rects].some(rect => rect.width > 0 && rect.height > 0)
      ? result('box-observed') : result('unresolved', 'no-client-box')
  }
  const correspondence = occurrences.map(({ source, reason }, index) => {
    if (reason) return { ...source, correspondence: 'unresolved', reason }
    const normalized = []
    for (const item of members.get(index)) {
      const previous = normalized.at(-1)
      if (item.node.nodeType === Node.TEXT_NODE && previous?.node === item.node && previous.end === item.start) previous.end = item.end
      else normalized.push({ ...item })
    }
    return { ...source, correspondence: 'observed', members: normalized.map(item => {
      const { node } = item, domPath = addresses.get(node)
      if (!domPath) throw new Error('Source occurrence DOM member lost during boundary removal')
      return {
        kind: node.nodeType === Node.ELEMENT_NODE ? 'element' : node.nodeType === Node.TEXT_NODE ? 'text' : 'comment',
        domPath, ...(node.nodeType === Node.ELEMENT_NODE ? { tag: node.localName } : {}),
        ...(node.nodeType === Node.TEXT_NODE ? { start: item.start, end: item.end } : {}), presentation: presentation(item),
      }
    }) }
  })
  globalThis.__platformkitCaptureSources = sources
  globalThis.__platformkitCaptureAddresses = addresses
  return correspondence
}
