import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { copyFileSync, mkdirSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { corrections, correctSource, sdkVersion } from './corrections.mjs'

// Run only in the disposable Docker build stage. Application sources come
// from the checksum-pinned archive; engine modules come from our npm lock.
const adapter = dirname(fileURLToPath(import.meta.url))
const upstream = realpathSync(process.argv[2])
const require = createRequire(import.meta.url)
const upstreamRequire = createRequire(join(upstream, 'package.json'))
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex')
const engine = /^@open-pencil\/(core|fig|scene-graph|pen|kiwi)(\/.*)?$/
const engineRoot = /\/packages\/(core|fig|scene-graph|pen|kiwi)\//
const pinned = specifier => require.resolve(specifier)
const dependencies = JSON.parse(readFileSync(join(adapter, 'package.json'), 'utf8')).dependencies
const manifest = JSON.parse(readFileSync(join(upstream, 'package.json'), 'utf8'))
if (manifest.version !== sdkVersion) throw new Error('Upstream app and corrected SDK versions differ')
for (const name of ['core', 'fig', 'scene-graph', 'pen', 'kiwi']) {
  const path = join(dirname(pinned(`@open-pencil/${name}`)), '../package.json')
  if (JSON.parse(readFileSync(path, 'utf8')).version !== sdkVersion) {
    throw new Error(`Unexpected native package version: ${name}`)
  }
}
const coreRequire = createRequire(pinned('@open-pencil/core'))
for (const [specifier, name, version] of [
  ['expr-eval', 'expr-eval-fork', '3.0.3'], ['pptxgenjs', '@neo-ma/pptxgenjs', '4.3.0'],
]) {
  const dependency = JSON.parse(readFileSync(join(dirname(coreRequire.resolve(specifier)), '../package.json'), 'utf8'))
  if (dependency.name !== name || dependency.version !== version) throw new Error(`Unreviewed browser dependency: ${specifier}`)
}

function replaceOnce(source, before, after) {
  if (source.split(before).length !== 2) throw new Error(`Browser build anchor changed: ${before}`)
  return source.replace(before, after)
}

// Keep upstream's UI configuration, excluding desktop/dev automation and PWA
// installation. Neither belongs in this static, digest-promoted browser image.
let configSource = readFileSync(join(upstream, 'vite.config.ts'), 'utf8')
for (const text of [
  "import { localAutomationToken, openPencilAutomationPlugin } from './vite/automation'\n",
  "import { openPencilPwaPlugin } from './vite/pwa'\n",
  '    openPencilAutomationPlugin(command, host),\n',
  '    openPencilPwaPlugin()\n',
]) configSource = replaceOnce(configSource, text, '')
configSource = replaceOnce(configSource, 'JSON.stringify(localAutomationToken(command))', 'JSON.stringify(null)')
const configFile = join(upstream, 'platformkit.vite.config.ts')
writeFileSync(configFile, configSource, { flag: 'wx' })
const { build, loadConfigFromFile } = await import(pathToFileURL(upstreamRequire.resolve('vite')))
const { config } = await loadConfigFromFile({ command: 'build', mode: 'production' }, configFile)

const seen = new Set()
let correctedNudgeKeys = false
const correctedControls = new Set()
function nativeBoundary() {
  return {
    name: 'platformkit-native-boundary', enforce: 'pre',
    async resolveId(specifier, importer, options) {
      const name = specifier.split('/').slice(0, specifier.startsWith('@') ? 2 : 1).join('/')
      if (engine.test(specifier)) return pinned(specifier)
      if (!Object.hasOwn(dependencies, name)) return null
      // Resolve from our lock while retaining Vite's browser/import conditions.
      const resolved = await this.resolve(specifier, join(adapter, 'package.json'), { ...options, skipSelf: true })
      if (!resolved || resolved.external || !resolved.id.startsWith(adapter + '/node_modules/')) {
        throw new Error(`Browser dependency escaped the adapter lock: ${specifier}`)
      }
      return resolved
    },
    load(id) {
      const path = id.split('?')[0]
      if (path.startsWith(upstream + '/') && engineRoot.test(path)) {
        throw new Error(`Upstream engine source bypasses the corrected SDK: ${path}`)
      }
      if (!path.startsWith(adapter + '/node_modules/') || !path.endsWith('.js') && !path.endsWith('.mjs')) return null
      const source = readFileSync(path, 'utf8')
      let result = correctSource(path, source)
      if (result !== null) seen.add(path.slice(adapter.length + '/node_modules/'.length))
      // The published JS distribution retains two TypeScript worker URLs.
      // Correct them before Vite discovers worker entries, in both builds.
      if (path.endsWith('/@open-pencil/core/dist/io/formats/fig/read.js')) {
        if (sha256(source) !== '137b1c1619c6841a04085157eaebca5b7c2e8a894257892abd647cc6d9eafe80') {
          throw new Error('FIG reader source changed')
        }
        result = replaceOnce(source, 'parse/worker.ts', 'parse/worker.js')
      }
      if (path.endsWith('/@open-pencil/core/dist/io/formats/fig/export.js')) {
        result = replaceOnce(result, './export-worker.ts', './export-worker.js')
      }
      return result === null ? null : { code: result, map: null }
    },
    transform(source, id) {
      const controlCorrections = {
        'src/components/properties/component-properties/ComponentPropertyTextField.vue': ['14210f35374fd53f5478e9708c6ba21139977dc40b6bc5e4e4deae7a87d0a8c5',
          "import { ref, watch } from 'vue'", "import { computed, ref, watch } from 'vue'",
          "import AppInput from '@/components/ui/AppInput.vue'",
          "import { tv } from 'tailwind-variants'\nimport theme from '@/theme/input'",
          "const draft = ref('')",
          "const draft = ref('')\nconst inputClass = computed(() => tv(theme)({ tone: 'panel', size: 'sm', state: value === MIXED ? 'mixed' : 'idle' }))",
          '<AppInput\n    v-model="draft"\n    tone="panel"\n    size="sm"\n    :state="value === MIXED ? \'mixed\' : \'idle\'"',
          `<textarea
    v-model="draft"
    :class="inputClass"
    :rows="Math.max(1, Math.min(6, draft.split('\\n').length))"
    style="height: auto; min-height: 1.5rem"
    @keydown.ctrl.enter.prevent="($event.target as HTMLTextAreaElement).blur()"
    @keydown.meta.enter.prevent="($event.target as HTMLTextAreaElement).blur()"`],
        'packages/vue/src/variables/helpers.ts': ['64956a42ec74f537186b528baf0379f2d2bd445c833df8bcf4e48fa0c90cec8e',
          'const value = variable.valuesByMode[modeId]',
          `const value = variable.valuesByMode[modeId]
    if (value && typeof value === 'object' && 'cssColor' in value) {
      return 'Derived #' + colorToHexRaw(editor.graph.resolveVariable(variable.id, modeId))
    }`],
        'packages/vue/src/variables/table/helpers.ts': ['50328f508e780665677e575098dd34ebf01e8e77973036d82d1351a54b736b52',
          'const value = variable.valuesByMode[mode.modeId]',
          `const value = variable.valuesByMode[mode.modeId]
      if (value && typeof value === 'object' && 'cssColor' in value) {
        return h('span', { class: 'font-mono text-xs text-muted' }, options.formatModeValue(variable, mode.modeId))
      }`,
          "'flex size-5 cursor-pointer items-center justify-center rounded border-none bg-transparent text-muted opacity-0 transition-opacity group-hover:opacity-100 hover:text-surface'",
          "'flex size-6 cursor-pointer items-center justify-center rounded border-none bg-transparent text-muted hover:text-surface'",
          'onClick: () => options.removeVariable(row.original.id)',
          "'aria-label': 'Delete ' + row.original.name,\n          onClick: () => options.removeVariable(row.original.id)"],
        'packages/vue/src/variables/use.ts': ['75e76700f882d4dc6b80615977363b9a061a08067a0cc61fffb4472234b81f47',
          "import type { Variable }", "import type { Variable, VariableValue }",
          "  const searchTerm = ref('')",
          `  const searchTerm = ref('')
  const variableError = ref('')
  function checkedVariableAction(action: () => void) {
    try { action() } catch (error) {
      if (!(error instanceof Error) || !/^(Native CSS color|CSS color expression):/.test(error.message)) throw error
      variableError.value = 'Variables unchanged. This change would break a colour dependency. ' +
        'Update the dependent colours before removing or replacing their input. ' + error.message
    }
  }`,
          '    ...variableActions',
          `    ...variableActions,
    variableError,
    dismissVariableError: () => { variableError.value = '' },
    removeVariable: (id: string) => checkedVariableAction(() => variableActions.removeVariable(id)),
    removeCollection: (id: string) => checkedVariableAction(() => collectionActions.removeCollection(id)),
    updateVariableValue: (id: string, modeId: string, value: VariableValue) =>
      checkedVariableAction(() => variableActions.updateVariableValue(id, modeId, value))`],
        'src/components/variables/VariablesDialog.vue': ['d8f09a9aefffceb59356cddf38208ad6e25976ebf2e641f08c1eeff6c2657cbe',
          "const collectionInput = templateRef<HTMLInputElement>('collectionInput')",
          "const collectionMenu = templateRef<HTMLButtonElement>('collectionMenu')\nconst collectionInput = templateRef<HTMLInputElement>('collectionInput')",
          '    <DialogTitle class="sr-only">{{ dialogs.localVariables }}</DialogTitle>',
          `    <DialogTitle class="sr-only">{{ dialogs.localVariables }}</DialogTitle>
    <div class="shrink-0 px-4">
      <p role="status" class="text-xs text-surface">{{ ctx.variableError.value }}</p>
      <button v-if="ctx.variableError.value" class="my-2 rounded px-2 py-1 text-xs text-surface"
        @click="ctx.dismissVariableError(); collectionMenu?.focus()">Dismiss variable message</button>
    </div>`,
          'data-test-id="variables-collection-menu"', 'data-test-id="variables-collection-menu" ref="collectionMenu" aria-label="Collection actions"',
          'data-test-id="variables-add-collection"', 'data-test-id="variables-add-collection" :aria-label="dialogs.createCollection"',
          'data-test-id="variables-search-input"', 'data-test-id="variables-search-input" :aria-label="dialogs.search"',
          'data-test-id="variables-add-mode"', 'data-test-id="variables-add-mode" :aria-label="dialogs.addMode"'],
        'src/app/shell/keyboard/registry.ts': ['5df738b1929c454d61c3665d8794ed0488cf8f712ae3211b5eeeaeedbca51cd0',
          'hasOpenDismissableLayer() ||',
          `((event.key === 'Enter' || event.code === 'Space') && event.composedPath().some(target =>
      target instanceof Element && target.matches('button, a[href], [role="button"]'))) ||
    hasOpenDismissableLayer() ||`],
        'src/app/shell/keyboard/space-tool.ts': ['3583591ca4f6bdc6be33b7f68537f45e17f956006b21c1cca51a19da3e0a0be6',
          "if (event.code !== 'Space') return",
          `if (event.code !== 'Space' || event.defaultPrevented || event.composedPath().some(target =>
      target instanceof Element && target.matches('button, a[href], [role="button"], [role="dialog"]'))) return`],
      }
      for (const [path, [digest, ...edits]] of Object.entries(controlCorrections)) if (id === join(upstream, path)) {
        if (sha256(source) !== digest) throw new Error('Editor control source changed: ' + path)
        correctedControls.add(path)
        for (let index = 0; index < edits.length; index += 2) source = replaceOnce(source, edits[index], edits[index + 1])
        if (path.endsWith('/ComponentPropertyTextField.vue')) source += `
<style scoped>
textarea:focus-visible { outline: revert; outline-offset: 2px; }
</style>\n`
        if (path.endsWith('/VariablesDialog.vue')) source += `
<style scoped>
:deep(button:focus-visible) { outline: revert; outline-offset: 2px; }
</style>\n`
        return { code: source, map: null }
      }
      if (id === join(upstream, 'src/app/shell/keyboard/nudging.ts')) {
        if (sha256(source) !== '164f12035c8949b4780e95622950ac0503c778ffcd977e09d61b233fb5f04ae7') {
          throw new Error('Browser nudge keyboard source changed')
        }
        // Tree arrows belong to navigation, including keys that the tree does
        // not cancel at its edges. Never turn those keys into canvas history.
        correctedNudgeKeys = true
        return { code: replaceOnce(source,
          'if (isEditing(e) || store.state.editingTextId) return',
          `if (e.defaultPrevented || isEditing(e) || store.state.editingTextId) return
    if (e.composedPath().some(target => target instanceof Element && target.getAttribute('role') === 'tree')) return`), map: null }
      }
      if (id !== join(upstream, 'src/main.ts')) return null
      if (sha256(source) !== 'ba0318dd65f3cbadaa406d01655b7190c5e1cfc55c5e92b33335d0fd1e5b8bbc') {
        throw new Error('Browser entry source changed')
      }
      return { code: replaceOnce(source, source.slice(source.indexOf('if (!IS_TAURI) {')), ''), map: null }
    },
  }
}

// Use Node's package-export resolver, not a second subpath mapping. Vue and
// dom-css stay upstream UI sources. Direct adapter dependencies also override
// bare UI imports, keeping their reviewed versions and browser entry points.
const aliases = config.resolve.alias.filter(alias => {
  const replacement = alias.replacement
  return !engineRoot.test(replacement) && alias.find !== 'opentype.js'
})
config.resolve.alias = [
  { find: 'opentype.js', replacement: pinned('opentype.js') },
  ...aliases,
]
config.plugins.unshift(nativeBoundary())
config.worker = { plugins: () => [nativeBoundary()] }
// Vite owns dependency discovery; this report is not a copied-asset license audit.
const dependencyLicenses = 'licenses/bundled-dependencies.json'
await build({ ...config, configFile: false, root: upstream, build: {
  ...config.build, reportCompressedSize: false, license: { fileName: dependencyLicenses },
} })

// Missing transforms are a build failure, not a silently less-correct editor.
if (!correctedNudgeKeys) throw new Error('Browser omitted the tree keyboard correction')
if (correctedControls.size !== 7) throw new Error('Browser omitted a required editor control correction')
// CommonJS expression code is tested by Node; the browser selects its ESM entry.
for (const path of Object.keys(corrections).filter(path => !path.endsWith('/bundle.js'))) {
  if (!seen.has(path)) throw new Error(`Browser omitted a required native correction: ${path}`)
}
const licenses = join(upstream, 'dist/licenses')
mkdirSync(licenses, { recursive: true })
for (const [source, name] of [
  [join(upstream, 'LICENSE'), 'OpenPencil-LICENSE'],
  [join(adapter, 'LICENSE'), 'PlatformKit-LICENSE'], [join(adapter, 'NOTICE'), 'PlatformKit-NOTICE'],
]) copyFileSync(source, join(licenses, name))
const inputs = Object.fromEntries(readdirSync(adapter).filter(name => name.endsWith('.mjs') ||
  ['package.json', 'package-lock.json', 'Dockerfile', 'nginx.conf', 'LICENSE', 'NOTICE'].includes(name))
  .sort().map(name => [name, sha256(readFileSync(join(adapter, name)))]))
writeFileSync(join(upstream, 'dist/platformkit-provenance.json'), JSON.stringify({
  schema: 'platformkit.openpencil.provenance.v1',
  upstream: { version: sdkVersion, commit: 'c29654cd07ac46b53e76c16b18505919f16571be' },
  adapter: { inputs, correctedModules: [...seen].sort() },
  upstreamLockSHA256: sha256(readFileSync(join(upstream, 'bun.lock'))),
  dependencyLicenses: { path: dependencyLicenses, sha256: sha256(readFileSync(join(upstream, 'dist', dependencyLicenses))) },
  designProfile: null, defaultDocument: null,
  scope: 'generic-editor-without-packaged-design',
}, null, 2) + '\n', { flag: 'wx' })
