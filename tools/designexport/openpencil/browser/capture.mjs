import { validateFonts } from '../fonts.mjs'
import { indexCaptureSources, prepareCaptureSource } from './capture-source.mjs'
import { resolveColorExpression } from '../color-expression.mjs'
import { computedColor } from '../computed-color.mjs'

// Exact 8-bit inputs keep mix math distinct from rounded CSSOM alpha text.
const colorProbes = [
  { value: '#123456', serialized: 'rgb(18, 52, 86)' },
  { value: '#abcdef', serialized: 'rgb(171, 205, 239)' },
  { value: '#12345640', serialized: 'rgba(18, 52, 86, 0.25)' },
  { value: '#abcdefbf', serialized: 'rgba(171, 205, 239, 0.75)' },
  { value: '#20406000', serialized: 'rgba(32, 64, 96, 0)' },
]

// Candidate declarations come from the supplied stylesheet, never a role map.
// Only the existing CSS evaluator's subset can provide a comparison witness.
function colorExpressionCandidates(declarations, tokens) {
  const definitions = new Map(), palette = new Map(tokens.map(token => [token.name, token.value]))
  if (palette.size !== tokens.length) return []
  for (const [name, value] of declarations) definitions.set(name, definitions.has(name) ? null : value)
  return [...definitions].filter(([name]) => !palette.has(name)).flatMap(([name, value]) => {
    const references = new Map(), dependencies = new Set()
    const resolve = (editedName, editedValue) => key => {
      if (palette.has(key)) {
        dependencies.add(key)
        return key === editedName ? editedValue : palette.get(key)
      }
      const definition = definitions.get(key)
      if (typeof definition === 'string') references.set(key, definition)
      return definition
    }
    try {
      const baseline = resolveColorExpression(value, resolve())
      const tokens = [...dependencies].toSorted()
      if (!tokens.length) return []
      const colors = [baseline, ...tokens.flatMap(token => colorProbes.map(probe => resolveColorExpression(value, resolve(token, probe.value))))]
      return [{ name, value, customProperties: Object.fromEntries(references), tokens, colors }]
    } catch { return [] } // Unsupported or ambiguous CSS remains unclaimed.
  })
}

function retainColorExpressions(nodes, expressions) {
  for (const node of nodes) {
    for (const [property, source] of Object.entries(node.paintSources ?? {})) {
      const { responses, candidates } = source
      delete source.responses
      delete source.candidates
      if (candidates?.length !== 1) continue
      const expression = expressions.find(item => item.name === candidates[0])
      if (JSON.stringify(source.tokens.toSorted()) !== JSON.stringify(expression.tokens)) continue
      try {
        const colors = [node.style[property], ...expression.tokens.flatMap(token => responses[token])].map(computedColor)
        if (!colors.every((color, index) => ['r', 'g', 'b', 'a'].every(channel =>
          Math.abs(color[channel] - expression.colors[index][channel]) <= 1e-6))) continue
        source.expressionCandidate = {
          customProperty: expression.name, value: expression.value, customProperties: structuredClone(expression.customProperties),
        }
      } catch { /* Unsupported computed colors provide no expression witness. */ }
    }
    if (node.children) retainColorExpressions(node.children, expressions)
  }
}

// Observe the existing Go HTML and CSS in a disposable, unauthenticated browser
// document. This is static adapter input, not a second component renderer or a
// claim that an application's controllers and interactions have been exercised.
export async function captureExample(browser, snapshot, exampleId, {
  mode = 'light', viewport = { width: 1280, height: 900 }, fonts = [],
} = {}) {
  if (snapshot?.schema !== 'platformkit.design-export.v1' || !/^[\da-f]{64}$/.test(snapshot.sha256)) {
    throw new Error('Capture requires an identified Go design-export snapshot')
  }
  const examples = snapshot.examples.filter(example => example.id === exampleId)
  if (examples.length !== 1) throw new Error(`Expected exactly one example: ${exampleId}`)
  if (!snapshot.themes.some(theme => theme.mode === mode)) throw new Error(`Unknown theme: ${mode}`)
  if (!['width', 'height'].every(key => Number.isInteger(viewport[key]) && viewport[key] > 0 && viewport[key] <= 8192)) {
    throw new Error('Capture viewport dimensions must be integers from 1 to 8192')
  }
  const faces = validateFonts(fonts)
  const example = examples[0]
  const prepared = prepareCaptureSource(example)
  const context = await browser.newContext({ viewport, colorScheme: mode, reducedMotion: 'reduce', serviceWorkers: 'block' })
  try {
    const requests = []
    await context.route('**/*', route => {
      requests.push(route.request().resourceType())
      return route.abort('blockedbyclient')
    })
    const page = await context.newPage()
    await page.setContent(`<!doctype html><html><head><meta charset="utf-8">
      <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src 'none'; base-uri 'none'; form-action 'none'">
      <meta name="viewport" content="width=device-width, initial-scale=1"></head><body></body></html>`)
    await page.evaluate(async ({ css, html, mode, fonts }) => {
      const violations = []
      document.addEventListener('securitypolicyviolation', event => violations.push(event.effectiveDirective))
      document.documentElement.lang = 'en'
      document.documentElement.dataset.theme = mode
      for (const font of fonts) {
        const face = new FontFace(font.family, Uint8Array.from(font.bytes), { weight: String(font.weight), style: font.style })
        await face.load()
        document.fonts.add(face)
      }
      const sheet = document.createElement('style')
      sheet.textContent = css
      document.head.append(sheet)
      // Template parsing is inert; scripts and event handlers are additionally
      // denied by CSP. The input still belongs to trusted Go constructors.
      const template = document.createElement('template')
      template.innerHTML = html
      if (template.content.querySelector('script, iframe, object, embed, canvas, video, audio, link, style, base')) {
        throw new Error('Capture does not support executable or externally composed example content')
      }
      document.body.append(template.content)
      await document.fonts.ready
      for (const image of document.images) {
        if (!image.src.startsWith('data:')) throw new Error('Capture requires supplied, in-memory image assets')
        await image.decode()
      }
      // Inherited transitions can start only when descendant styles are read.
      // Flush those styles and await actual completion, not a fixed frame count.
      let settlingTimer
      try {
        await Promise.race([
          (async () => {
            while (true) {
              for (const node of document.querySelectorAll('*')) getComputedStyle(node).color
              const active = document.getAnimations().filter(animation => !['finished', 'idle'].includes(animation.playState))
              if (active.length === 0) return
              if (active.some(animation => animation.playState === 'paused' ||
                !Number.isFinite(animation.effect?.getComputedTiming().endTime))) {
                throw new Error('Capture requires finite, running source animations to settle')
              }
              // A cancelled transition can start a replacement; inspect again.
              await Promise.all(active.map(animation => animation.finished.catch(() => {})))
            }
          })(),
          new Promise((_, reject) => {
            settlingTimer = setTimeout(() => reject(new Error('Capture source animation settling timed out')), 1000)
          }),
        ])
      } finally { clearTimeout(settlingTimer) }
      if (violations.length) throw new Error(`Capture refused resources blocked by CSP: ${violations.join(', ')}`)
    }, {
      css: snapshot.css, html: prepared.html, mode,
      fonts: faces.map(face => ({ family: face.family, weight: face.weight, style: face.style, bytes: [...face.bytes] })),
    })
    await page.evaluate(indexCaptureSources, { occurrences: prepared.occurrences, html: example.html })
    const colorTokens = snapshot.themes.find(theme => theme.mode === mode).tokens.filter(token => token.type === 'color')
    const declarations = await page.evaluate(() => {
      // A sampled mode cannot prove an inactive override equivalent. Only one
      // unconditional root declaration may define a portable formula or alias.
      const inspect = (rules, topLevel) => [...rules].flatMap(rule => [
        ...[...(rule.style ?? [])].filter(name => name.startsWith('--')).map(name =>
          [name, topLevel && rule.selectorText === ':root' ? rule.style.getPropertyValue(name).trim() : null]),
        ...(rule.cssRules ? inspect(rule.cssRules, false) : []),
      ])
      return [...document.styleSheets].flatMap(sheet => inspect(sheet.cssRules, true))
    })
    const expressions = colorExpressionCandidates(declarations, colorTokens)
    const roots = await page.evaluate(({ colorTokens, probes, expressionNames }) => {
      // Exact text nodes and native text controls cross into CDP inspection.
      // Other element queries aggregate descendants and cannot identify regions.
      globalThis.__platformkitCaptureTextNodes = []
      const elements = []
      const properties = [
        'display', 'visibility', 'opacity', 'position', 'transform', 'box-sizing', 'float', 'clear', 'column-count', 'column-width',
        'color', 'background-color', 'background-image', 'box-shadow',
        'border-top-width', 'border-right-width', 'border-bottom-width', 'border-left-width',
        'border-top-color', 'border-right-color', 'border-bottom-color', 'border-left-color',
        'border-top-style', 'border-right-style', 'border-bottom-style', 'border-left-style',
        'border-top-left-radius', 'border-top-right-radius', 'border-bottom-right-radius', 'border-bottom-left-radius',
        'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
        'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
        'flex-direction', 'flex-wrap', 'flex-grow', 'flex-shrink', 'flex-basis',
        'justify-content', 'align-items', 'align-self', 'align-content', 'order', 'row-gap', 'column-gap',
        'justify-items', 'justify-self', 'grid-auto-flow', 'grid-template-areas',
        'grid-column-start', 'grid-column-end', 'grid-row-start', 'grid-row-end',
        'font-family', 'font-size', 'font-weight', 'font-style', 'font-stretch',
        'font-synthesis-weight', 'font-synthesis-style',
        'font-feature-settings', 'font-variation-settings', 'line-height', 'letter-spacing',
        'white-space', 'text-align', 'text-transform', 'text-decoration-line',
        'text-indent', 'text-shadow', 'word-spacing', 'writing-mode', 'direction',
        'overflow-x', 'overflow-y', 'outline-style', 'outline-width', 'outline-color',
        'animation-name', 'animation-duration', 'filter',
      ]
      const svgProperties = [
        'fill', 'fill-opacity', 'fill-rule', 'stroke', 'stroke-opacity', 'stroke-width',
        'stroke-linecap', 'stroke-linejoin', 'stroke-miterlimit', 'stroke-dasharray', 'stroke-dashoffset',
        'transform-origin', 'transform-box', 'translate', 'rotate', 'scale', 'zoom',
        'vector-effect', 'clip', 'clip-path', 'clip-rule', 'mask', 'mask-type',
        'paint-order', 'shape-rendering', 'color-interpolation', 'color-interpolation-filters',
        'mix-blend-mode', 'isolation', 'marker-start', 'marker-mid', 'marker-end',
        'x', 'y', 'width', 'height', 'rx', 'ry', 'cx', 'cy', 'r', 'd',
      ]
      const isSVG = node => node.namespaceURI === 'http://www.w3.org/2000/svg'
      const bounds = rect => ({ x: rect.x, y: rect.y, width: rect.width, height: rect.height })
      const style = element => {
        const computed = getComputedStyle(element)
        const names = isSVG(element) ? [...properties, ...svgProperties] : properties
        return Object.fromEntries(names.map(name => [name, computed.getPropertyValue(name)]))
      }
      function text(range, nodes, property) {
        return {
          kind: 'text', text: range.toString(), ...(property === undefined ? {} : { property }),
          bounds: bounds(range.getBoundingClientRect()), rects: [...range.getClientRects()].map(bounds),
          fontObservationIds: nodes.map(node => globalThis.__platformkitCaptureTextNodes.push(node) - 1),
        }
      }
      function children(parent) {
        const result = []
        const regions = [{ children: result }]
        const nodes = [...parent.childNodes]
        for (let index = 0; index < nodes.length; index++) {
          const node = nodes[index]
          const target = regions.at(-1).children
          if (node.nodeType === Node.ELEMENT_NODE) target.push(element(node))
          else if (node.nodeType === Node.TEXT_NODE) {
            const range = document.createRange()
            range.selectNode(node)
            target.push(text(range, [node]))
          } else if (node.nodeType === Node.COMMENT_NODE) {
            if (/^\/?pk-slot:/.test(node.data)) {
              const marker = /^(\/?)pk-slot:([A-Za-z][A-Za-z0-9]*)$/.exec(node.data)
              if (!marker) throw new Error('Invalid slot marker')
              const [, closing, name] = marker
              if (closing) {
                if (regions.length === 1 || regions.at(-1).name !== name) throw new Error('Unmatched slot closing marker')
                regions.pop()
              } else {
                // Structural evidence only: the converter must establish the
                // declaration and owning component before binding this name.
                const region = { kind: 'slot', name, children: [] }
                target.push(region)
                regions.push(region)
              }
              continue
            }
            if (node.data.startsWith('/pk-text:')) throw new Error('Unmatched text-property closing marker')
            if (!node.data.startsWith('pk-text:')) continue
            const property = node.data.slice('pk-text:'.length)
            if (!/^[A-Za-z][A-Za-z0-9]*$/.test(property)) throw new Error('Invalid text-property marker')
            const range = document.createRange()
            range.setStartAfter(node)
            const textNodes = []
            while (++index < nodes.length && nodes[index].nodeType === Node.TEXT_NODE) {
              textNodes.push(nodes[index])
            }
            const end = nodes[index]
            if (end?.nodeType !== Node.COMMENT_NODE || end.data !== `/pk-text:${property}`) {
              throw new Error('Text-property markers must enclose only text and close exactly')
            }
            range.setEndBefore(end)
            target.push(text(range, textNodes, property))
          }
        }
        if (regions.length !== 1) throw new Error('Unclosed slot marker')
        return result
      }
      function element(node) {
        const id = elements.push(node) - 1
        const computed = style(node)
        const typed = node.computedStyleMap()
        const out = {
          kind: 'element', observationId: id, tag: node.localName,
          component: node.getAttribute('data-component'),
          bounds: bounds(node.getBoundingClientRect()), style: computed,
          sizing: Object.fromEntries(['width', 'height', 'min-width', 'max-width', 'min-height', 'max-height',
            'grid-template-columns', 'grid-template-rows', 'grid-auto-columns', 'grid-auto-rows']
            .map(key => [key, typed.get(key)?.toString() ?? ''])),
          children: [],
        }
        const source = globalThis.__platformkitCaptureSources.get(node)
        if (source) out.source = source
        if (node.hasAttribute('data-pk-options') || node.hasAttribute('data-pk-values') ||
            node instanceof HTMLSelectElement && node.hasAttribute('data-pk-value')) {
          const properties = Object.fromEntries(['value', 'values', 'options'].map(name => [name, node.getAttribute(`data-pk-${name}`)]))
          if (!(node instanceof HTMLSelectElement) || Object.values(properties).some(name => !/^[A-Za-z][A-Za-z0-9]*$/.test(name ?? '')) ||
              new Set(Object.values(properties)).size !== 3) throw new Error('Invalid choice-control property markers')
          out.control = { kind: 'control', type: node.type, properties, value: node.value,
            values: [...node.selectedOptions].map(option => option.value), size: node.size,
            required: node.required, disabled: node.disabled,
            options: [...node.options].map(option => ({ value: option.value, label: option.label,
              selected: option.selected, disabled: option.disabled,
              group: option.parentElement instanceof HTMLOptGroupElement ? {
                label: option.parentElement.label, disabled: option.parentElement.disabled,
              } : null })),
            // For a listbox, font use includes all painted options, not just
            // the selected values. Closed selects paint their displayed label.
            fontObservationIds: [globalThis.__platformkitCaptureTextNodes.push(node) - 1] }
        } else if (node.hasAttribute('data-pk-value')) {
          const property = node.getAttribute('data-pk-value')
          const multiline = node instanceof HTMLTextAreaElement
          if (!(node instanceof HTMLInputElement || multiline) || !/^[A-Za-z][A-Za-z0-9]*$/.test(property)) {
            throw new Error('Invalid text-control property marker')
          }
          if (!multiline && node.type !== 'text') throw new Error('Capture only supports marked text controls')
          // CDP observes painted control content: with an empty value, glyphs
          // may belong to its placeholder. Preserve that distinction explicitly.
          out.control = { kind: 'control', property, type: node.type, value: node.value, placeholder: node.placeholder,
            ...(multiline ? { rows: node.rows, wrap: node.wrap, controllers: node.getAttribute('data-controller'),
              counter: node.hasAttribute('data-textarea-counter-target') } : {}),
            fontObservationIds: [globalThis.__platformkitCaptureTextNodes.push(node) - 1] }
        }
        for (const pseudo of ['::before', '::after']) {
          const content = getComputedStyle(node, pseudo).content
          if (content !== 'none' && content !== 'normal') throw new Error('Generated pseudo-element content needs explicit native conversion')
        }
        if (node.localName === 'svg') out.icon = {
          name: node.getAttribute('data-pk-icon'), canonicalName: node.getAttribute('data-pk-icon-canonical'), svg: node.outerHTML,
        }
        if (isSVG(node)) out.attributes = Object.fromEntries([...node.attributes].map(attribute => [attribute.name, attribute.value]))
        out.children = out.control ? [] : children(node)
        return out
      }
      const roots = children(document.body)
      const paints = ['color', 'background-color', 'border-top-color', 'border-right-color', 'border-bottom-color', 'border-left-color', 'fill', 'stroke', 'box-shadow']
      const values = () => elements.map(node => {
        const computed = getComputedStyle(node)
        return paints.map(paint => computed.getPropertyValue(paint))
      })
      const sources = elements.map(() => Object.fromEntries(paints.map(paint => [paint, { tokens: [], directCandidate: null }])))
      // Let the browser resolve roles, inheritance and color-mix. Matching a
      // baseline RGB would incorrectly bind unrelated literals and equal-valued
      // tokens. Probe opacity too: opaque-only samples miss alpha dependencies.
      // Matching these samples suggests a direct binding, not equivalence for
      // arbitrary CSS expressions in every state.
      const probeSheet = document.createElement('style')
      probeSheet.textContent = '* { transition: none !important; }'
      const root = document.documentElement
      const originalStyle = root.getAttribute('style')
      document.head.append(probeSheet)
      try {
        const baseline = values()
        const sample = token => {
          const before = root.style.getPropertyValue(token)
          const priority = root.style.getPropertyPriority(token)
          const samples = probes.map(probe => {
            root.style.setProperty(token, probe.value, 'important')
            return values()
          })
          if (before) root.style.setProperty(token, before, priority)
          else root.style.removeProperty(token)
          return samples
        }
        for (const token of colorTokens) {
          const samples = sample(token)
          for (const [index] of elements.entries()) {
            for (const [paintIndex, paint] of paints.entries()) {
              const observed = samples.map(sample => sample[index][paintIndex])
              if (observed.every(value => value === baseline[index][paintIndex])) continue
              const source = sources[index][paint]
              source.tokens.push(token)
              source.responses ??= {}
              source.responses[token] = observed
              if (source.tokens.length === 1 && observed.every((value, index) => value === probes[index].serialized)) source.directCandidate = token
              else source.directCandidate = null
            }
          }
        }
        const targets = sources.flatMap((paintsByName, index) => paints.flatMap((paint, paintIndex) => {
          const source = paintsByName[paint]
          if (!source.tokens.length || source.directCandidate !== null) return []
          source.candidates = []
          return [{ index, paintIndex, source }]
        }))
        if (targets.length) for (const name of expressionNames) {
          const samples = sample(name)
          for (const { index, paintIndex, source } of targets) {
            if (samples.every((values, probe) => values[index][paintIndex] === probes[probe].serialized)) source.candidates.push(name)
          }
        }
      } finally {
        if (originalStyle === null) root.removeAttribute('style')
        else root.setAttribute('style', originalStyle)
        probeSheet.remove()
      }
      function annotate(nodes) {
        for (const node of nodes) {
          if (node.kind === 'element') {
            node.paintSources = sources[node.observationId]
            delete node.observationId
          }
          if (node.children) annotate(node.children)
        }
      }
      annotate(roots)
      return roots
    }, { colorTokens: colorTokens.map(token => token.name), probes: colorProbes, expressionNames: expressions.map(item => item.name) })
    retainColorExpressions(roots, expressions)
    const session = await context.newCDPSession(page)
    let environment
    try {
      const [version, commandLine] = await Promise.all([
        session.send('Browser.getVersion'),
        session.send('Browser.getBrowserCommandLine').catch(() => {
          throw new Error('Capture requires Chromium launched with --enable-automation to verify rendering metadata')
        }),
      ])
      // Retain only rendering metadata. Launch arguments can contain private
      // profile paths or credentials and must never enter a design snapshot.
      const hint = commandLine.arguments.findLast(argument => argument.startsWith('--font-render-hinting='))
      const fontHinting = hint ? hint.slice('--font-render-hinting='.length) : 'default'
      if (!['default', 'none', 'slight', 'medium', 'full'].includes(fontHinting)) {
        throw new Error('Capture requires a recognized Chromium font-hinting mode')
      }
      environment = {
        browser: version.product, protocol: version.protocolVersion,
        headless: commandLine.arguments.some(argument => argument === '--headless' || argument.startsWith('--headless=')),
        fontHinting,
      }
      // Whitespace nodes otherwise may have no frontend DOM ID. Query actual
      // font use even at zero advance: combining marks can still paint ink.
      await session.send('DOM.enable', { includeWhitespace: 'all' })
      await session.send('CSS.enable')
      await session.send('DOM.getDocument')
      async function inspect(nodes) {
        for (const node of nodes) {
          if (node.control) await inspect([node.control])
          if (node.children) {
            await inspect(node.children)
            continue
          }
          const fonts = new Map()
          for (const id of node.fontObservationIds) {
            const { result } = await session.send('Runtime.evaluate', {
              expression: `globalThis.__platformkitCaptureTextNodes[${id}]`, objectGroup: 'platformkit-capture',
            })
            const { nodeId } = await session.send('DOM.requestNode', { objectId: result.objectId })
            let fontNodes = [nodeId]
            if (node.type === 'select-one' && node.size <= 1) {
              const { node: control } = await session.send('DOM.describeNode', { nodeId, depth: -1, pierce: true })
              const interiors = control.shadowRoots?.filter(root => root.shadowRootType === 'user-agent')
                .flatMap(root => root.children ?? []).filter(item => item.localName === 'div' &&
                  item.attributes?.some((attribute, index) => index % 2 === 0 && attribute === 'pseudo' &&
                    item.attributes[index + 1] === '-internal-select-inner-element')) ?? []
              if (interiors.length !== 1) throw new Error('Select requires an observed browser display viewport')
              const { object } = await session.send('DOM.resolveNode', { backendNodeId: interiors[0].backendNodeId })
              const { result: content } = await session.send('Runtime.callFunctionOn', { objectId: object.objectId, returnByValue: true,
                functionDeclaration: `function() {
                  const rect = r => ({ x: r.x, y: r.y, width: r.width, height: r.height });
                  const range = document.createRange(); range.selectNodeContents(this);
                  return { text: this.textContent, bounds: rect(this.getBoundingClientRect()), rects: [...range.getClientRects()].map(rect) };
                }` })
              node.content = content.value
            }
            if (node.type === 'textarea') {
              const { node: control } = await session.send('DOM.describeNode', { nodeId, depth: -1, pierce: true })
              const editor = control.shadowRoots?.find(root => root.shadowRootType === 'user-agent')?.children?.at(-1)
              if (editor?.localName !== 'div') throw new Error('Textarea requires an observed browser editing viewport')
              const { object } = await session.send('DOM.resolveNode', { backendNodeId: editor.backendNodeId })
              const { result: content } = await session.send('Runtime.callFunctionOn', { objectId: object.objectId, returnByValue: true,
                functionDeclaration: `function() {
                  const rect = r => ({ x: r.x, y: r.y, width: r.width, height: r.height });
                  const range = document.createRange(); range.selectNodeContents(this);
                  const bounds = rect(this.getBoundingClientRect()), rects = [...range.getClientRects()].map(rect);
                  // Paragraph metrics exclude hanging spaces; retain blank lines
                  // and measure the actual non-space range endpoints separately.
                  const advances = new Map(rects.map(box => [box.y, 0]));
                  const walker = document.createTreeWalker(this, NodeFilter.SHOW_TEXT);
                  for (let text; (text = walker.nextNode());) for (let offset = 0; offset < text.length;) {
                    const next = offset + (text.data.codePointAt(offset) > 65535 ? 2 : 1);
                    range.setStart(text, offset); range.setEnd(text, next);
                    const box = range.getBoundingClientRect();
                    if (text.data.slice(offset, next) !== ' ') advances.set(box.y, Math.max(advances.get(box.y) ?? 0, box.right - bounds.x));
                    offset = next;
                  }
                  return { bounds, rects, advances: [...advances.values()] };
                }` })
              node.content = content.value
              const leaves = item => item.nodeType === 3 ? [item.backendNodeId] : (item.children ?? []).flatMap(leaves)
              const { nodeIds } = await session.send('DOM.pushNodesByBackendIdsToFrontend', { backendNodeIds: leaves(editor) })
              fontNodes = nodeIds
            }
            const used = (await Promise.all(fontNodes.map(nodeId => session.send('CSS.getPlatformFontsForNode', { nodeId })))).flatMap(result => result.fonts)
            for (const font of used) {
              const key = JSON.stringify([font.familyName, font.postScriptName, font.isCustomFont])
              const previous = fonts.get(key)
              fonts.set(key, { ...font, glyphCount: font.glyphCount + (previous?.glyphCount ?? 0) })
            }
          }
          node.fonts = [...fonts.values()]
          delete node.fontObservationIds
        }
      }
      await inspect(roots)
    } finally {
      await session.detach()
    }
    if (requests.length > 0) throw new Error(`Capture refused external resources: ${requests.join(', ')}`)
    return {
      sourceSHA: snapshot.sha256, exampleId, componentId: example.componentId,
      mode, viewport: { ...viewport }, environment, roots,
      fontFaces: faces.map(({ family, weight, style, sha256, postscriptName }) => ({ family, weight, style, sha256, postscriptName })),
    }
  } finally {
    await context.close()
  }
}
