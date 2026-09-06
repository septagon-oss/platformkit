import { validateFonts } from '../fonts.mjs'

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
      css: snapshot.css, html: example.html, mode,
      fonts: faces.map(face => ({ family: face.family, weight: face.weight, style: face.style, bytes: [...face.bytes] })),
    })
    const roots = await page.evaluate(colorTokens => {
      // Only exact text-node handles cross into CDP font inspection. Element
      // font queries include descendants and cannot identify a direct region.
      globalThis.__platformkitCaptureTextNodes = []
      const elements = []
      const properties = [
        'display', 'visibility', 'opacity', 'position', 'transform', 'box-sizing',
        'color', 'background-color', 'background-image', 'box-shadow',
        'border-top-width', 'border-right-width', 'border-bottom-width', 'border-left-width',
        'border-top-color', 'border-right-color', 'border-bottom-color', 'border-left-color',
        'border-top-style', 'border-right-style', 'border-bottom-style', 'border-left-style',
        'border-top-left-radius', 'border-top-right-radius', 'border-bottom-right-radius', 'border-bottom-left-radius',
        'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
        'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
        'flex-direction', 'flex-wrap', 'flex-grow', 'flex-shrink', 'flex-basis',
        'justify-content', 'align-items', 'align-self', 'row-gap', 'column-gap',
        'font-family', 'font-size', 'font-weight', 'font-style', 'font-stretch',
        'font-feature-settings', 'font-variation-settings', 'line-height', 'letter-spacing',
        'white-space', 'text-align', 'text-transform', 'text-decoration-line',
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
          sizing: Object.fromEntries(['width', 'height', 'min-width', 'max-width', 'min-height', 'max-height']
            .map(key => [key, typed.get(key)?.toString() ?? ''])),
          children: [],
        }
        for (const pseudo of ['::before', '::after']) {
          const content = getComputedStyle(node, pseudo).content
          if (content !== 'none' && content !== 'normal') throw new Error('Generated pseudo-element content needs explicit native conversion')
        }
        if (node.localName === 'svg') out.icon = {
          name: node.getAttribute('data-pk-icon'), canonicalName: node.getAttribute('data-pk-icon-canonical'), svg: node.outerHTML,
        }
        if (isSVG(node)) out.attributes = Object.fromEntries([...node.attributes].map(attribute => [attribute.name, attribute.value]))
        out.children = children(node)
        return out
      }
      const roots = children(document.body)
      const paints = ['color', 'background-color', 'border-top-color', 'border-right-color', 'border-bottom-color', 'border-left-color', 'fill', 'stroke']
      const values = () => elements.map(node => {
        const computed = getComputedStyle(node)
        return paints.map(paint => computed.getPropertyValue(paint))
      })
      const sources = elements.map(() => Object.fromEntries(paints.map(paint => [paint, { tokens: [], directCandidate: null }])))
      // Let the browser resolve roles, inheritance and color-mix. Matching a
      // baseline RGB would incorrectly bind unrelated literals and equal-valued
      // tokens. Two matching probes suggest a direct binding; they do not prove
      // arbitrary CSS expressions are equivalent to that token in every state.
      const probeSheet = document.createElement('style')
      probeSheet.textContent = '* { transition: none !important; }'
      const root = document.documentElement
      const originalStyle = root.getAttribute('style')
      document.head.append(probeSheet)
      try {
        const baseline = values()
        for (const token of colorTokens) {
          const before = root.style.getPropertyValue(token)
          const priority = root.style.getPropertyPriority(token)
          root.style.setProperty(token, '#123456', 'important')
          const first = values()
          root.style.setProperty(token, '#abcdef', 'important')
          const second = values()
          if (before) root.style.setProperty(token, before, priority)
          else root.style.removeProperty(token)
          for (const [index] of elements.entries()) {
            for (const [paintIndex, paint] of paints.entries()) {
              const a = first[index][paintIndex], b = second[index][paintIndex]
              if (a === baseline[index][paintIndex] && b === baseline[index][paintIndex]) continue
              const source = sources[index][paint]
              source.tokens.push(token)
              if (source.tokens.length === 1 && a === 'rgb(18, 52, 86)' && b === 'rgb(171, 205, 239)') source.directCandidate = token
              else source.directCandidate = null
            }
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
    }, snapshot.themes.find(theme => theme.mode === mode).tokens.filter(token => token.type === 'color').map(token => token.name))
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
            const { fonts: used } = await session.send('CSS.getPlatformFontsForNode', { nodeId })
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
