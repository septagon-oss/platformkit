// Byte spans are evidence from one Go render, not durable DOM addresses. Add
// temporary numeric boundaries without interpolating source identities as HTML.
export function prepareCaptureSource(example) {
  const occurrences = [], active = new Set(), decode = bytes => new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  function render(description, path, slot) {
    if (active.has(description) || !description.id?.trim() || !description.componentId?.trim() ||
      typeof description.html !== 'string' || /<!--\/?pk-capture:/.test(description.html)) {
      throw new Error('Invalid source occurrence or reserved capture marker')
    }
    active.add(description)
    const index = occurrences.push({ path, componentId: description.componentId, ...(slot ? { slot } : {}) }) - 1
    const bytes = Buffer.from(description.html), ids = new Set(), children = description.children ?? []
    for (const child of children) {
      if (!child.description?.id?.trim() || ids.has(child.description.id)) throw new Error('Duplicate or missing source occurrence identity')
      ids.add(child.description.id)
    }
    let offset = 0, html = ''
    for (const child of children.filter(child => child.span != null).toSorted((a, b) => a.span.start - b.span.start)) {
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
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_COMMENT)
  const markers = [], stack = [], seen = new Set(), sources = new WeakMap()
  for (let node; (node = walker.nextNode());) {
    if (!/^\/?pk-capture:/.test(node.data)) continue
    const match = /^(\/?)pk-capture:(\d+)$/.exec(node.data)
    if (!match || !occurrences[Number(match[2])]) throw new Error('Invalid capture marker')
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
    const content = []
    for (let current = opening.start.nextSibling; current !== node; current = current?.nextSibling) {
      if (!current) throw new Error('Unreachable source occurrence closing boundary')
      if (current.nodeType !== Node.COMMENT_NODE && !(current.nodeType === Node.TEXT_NODE && current.data === '')) content.push(current)
    }
    // Fragments are observable, but not a single native component root.
    if (content.length === 1 && content[0].nodeType === Node.ELEMENT_NODE) {
      if (sources.has(content[0])) throw new Error('Ambiguous source occurrences own the same DOM root')
      sources.set(content[0], occurrences[index])
    }
  }
  if (stack.length || seen.size !== occurrences.length || markers.length !== occurrences.length * 2) {
    throw new Error('Missing source occurrence capture boundaries after HTML parsing')
  }
  for (const node of markers) {
    const before = node.previousSibling, after = node.nextSibling
    node.remove()
    // Restore only the text adjacency that our own marker interrupted; an
    // unrelated caller-created text-node split must remain observable.
    if (before?.nodeType === Node.TEXT_NODE && after?.nodeType === Node.TEXT_NODE) {
      before.appendData(after.data)
      after.remove()
    }
  }
  if (document.body.innerHTML !== original.innerHTML) throw new Error('Source occurrence instrumentation changed HTML parsing')
  globalThis.__platformkitCaptureSources = sources
}
