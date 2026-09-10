import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { copyFileSync, mkdirSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { corrections, correctSource, sdkVersion } from './corrections.mjs'
import { chain } from './exporter-correction.mjs'
import { editorFonts, bundleEditorFonts } from './editor-fonts.mjs'

// Run only in the disposable Docker build stage. Application sources come
// from the checksum-pinned archive; engine modules come from our npm lock.
const adapter = dirname(fileURLToPath(import.meta.url))
const upstream = realpathSync(process.argv[2])
const require = createRequire(import.meta.url)
const upstreamRequire = createRequire(join(upstream, 'package.json'))
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex')
const suppliedFonts = editorFonts(process.argv.slice(3), readFileSync, realpathSync)
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

// Check the existing loader/picker and stock-face collisions before even the
// disposable Vite configuration is written, not only during its later transform.
bundleEditorFonts(readFileSync(join(dirname(pinned('@open-pencil/core/text')), 'fonts.js'), 'utf8'), suppliedFonts, replaceOnce)

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
const popoverTrigger = relative(upstream, join(dirname(upstreamRequire.resolve('reka-ui')), 'Popover/PopoverTrigger.js'))

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
      if (path.endsWith('/@open-pencil/core/dist/text/fonts.js')) {
        result = bundleEditorFonts(result, suppliedFonts, replaceOnce)
      }
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
        'packages/vue/src/controls/component-props/use.ts': ['c9430929645a8981f4ef669f4deb039dcbfc5a96a25969a4eba0bf6e6f398f86',
          'function variantOptions(', chain.toString() + '\n\nfunction variantOptions(',
          'const component = instance.componentId ? editor.graph.getNode(instance.componentId) : null',
          "const component = chain(editor.graph, instance, 'componentId').at(-1)"],
        'src/components/ui/AppSelect.vue': ['a7faaee2db2d26a3324382ff3833373e799cf77e89bf87c246c18e09533e040c',
          "import { tv } from 'tailwind-variants'", "import { computed } from 'vue'\nimport { tv } from 'tailwind-variants'",
          '  placeholder?: string', '  placeholder?: string\n  mixed?: boolean',
          'const { options, label, placeholder, ui }', 'const { options, label, placeholder, mixed, ui }',
          'const styles = tv(theme)()',
          `const styles = tv(theme)()
// Reka reserves the empty string for clearing. Keep that wire constraint out
// of the public typed option values, including strings that look like tokens.
const optionKey = (value: T) => JSON.stringify([typeof value, value])
const selectedValue = computed({
  get: () => mixed ? undefined : optionKey(modelValue.value),
  set: (key: string) => {
    const option = options.find(item => optionKey(item.value) === key)
    if (option) modelValue.value = option.value
  }
})`,
          '<SelectRoot v-model="modelValue">', '<SelectRoot v-model="selectedValue">',
          ':key="String(opt.value)"', ':key="optionKey(opt.value)"',
          ':value="opt.value"', ':value="optionKey(opt.value)"'],
        'src/components/properties/component-properties/ComponentPropertiesSection.vue': ['3014fc5a451e89f674d262686853dd2591afe9044af8ca92727d6a64dcc958b7',
          `  return control.value === MIXED
    ? [{ value: 'MIXED', label: panels.value.mixed }, ...control.options]
    : control.options`,
          `  return control.options.map(option => ({ ...option,
    label: option.label.trim() === '' ? panels.value.none : option.label
  }))`,
          "return control.value === MIXED ? 'MIXED' : control.value", "return control.value === MIXED ? '' : control.value",
          "if (value !== 'MIXED') setValue(propertyId, value)", 'setValue(propertyId, value)',
          ':model-value="selectValue(control)"', ':model-value="selectValue(control)"\n          :mixed="control.value === MIXED"\n          :placeholder="control.value === MIXED ? panels.mixed : undefined"'],
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
          "import { colorToHexRaw, parseColor }", "import { parseNativeNumber } from " + JSON.stringify(join(adapter, 'variable-number.mjs')) + "\nimport { colorToHexRaw, parseColor }",
          `      const num = Number.parseFloat(raw)
      return Number.isNaN(num) ? undefined : num`, '      return parseNativeNumber(raw)',
          'const value = variable.valuesByMode[modeId]',
          `const value = Object.hasOwn(variable.valuesByMode, modeId) ? variable.valuesByMode[modeId] : editor.graph.resolveVariable(variable.id, modeId)
    if (Object.is(value, -0)) return '-0'
    if (value && typeof value === 'object' && 'cssColor' in value) {
      return 'Derived #' + colorToHexRaw(editor.graph.resolveVariable(variable.id, modeId))
    }`],
        'packages/vue/src/variables/table/helpers.ts': ['50328f508e780665677e575098dd34ebf01e8e77973036d82d1351a54b736b52',
          'return h(options.ColorInput, {\n          color: value,',
          'return h(options.ColorInput, {\n          label: `Edit color: ${variable.name}, ${mode.name}`,\n          color: value,',
          "    header: '',", "    header: () => h('span', { class: 'sr-only' }, 'Actions'),",
          `  if (newName && newName !== variable.name) {
    options.renameVariable(variable.id, newName)
  }`,
          `  const prefix = variable.name.slice(0, variable.name.lastIndexOf('/') + 1)
  const name = newName.includes('/') ? newName : prefix + newName
  if (newName && name !== variable.name) options.renameVariable(variable.id, name)`,
          `      return h(
        EditableRoot,
        {
          defaultValue: options.formatModeValue(variable, mode.modeId),
          class: 'min-w-0 flex-1',
          onSubmit: (submitted: string | null | undefined) =>
            submitted && commitValueEdit(options, variable, mode.modeId, submitted)
        },
        () =>
          h(EditableArea, { class: 'flex' }, () => [
            h(EditablePreview, {
              class: 'min-w-0 flex-1 cursor-text truncate font-mono text-xs text-muted'
            }),
            h(EditableInput, {
              class:
                'min-w-0 flex-1 rounded border border-border bg-surface/10 px-1 py-0.5 font-mono text-xs text-surface outline-none'
            })
          ])
      )`,
          `      // Native change commits when focus moves to any sibling cell. The
      // upstream editable layer treats later cells as overlays and skips it.
      return h('input', {
        value: options.formatModeValue(variable, mode.modeId),
        'aria-label': variable.name + ', ' + mode.name,
        'aria-description': Object.hasOwn(variable.valuesByMode, mode.modeId) ? undefined : 'Uses the default mode value until edited',
        class: 'w-full min-w-0 rounded border border-border bg-surface/10 px-1 py-0.5 font-mono text-xs text-surface',
        onChange: (event: Event) => {
          const input = event.target as HTMLInputElement
          commitValueEdit(options, variable, mode.modeId, input.value)
          input.value = options.formatModeValue(variable, mode.modeId)
        },
        onKeydown: (event: KeyboardEvent) => {
          if (event.isComposing) return
          const input = event.target as HTMLInputElement
          if (event.key === 'Enter') { event.preventDefault(); input.blur() }
          if (event.key === 'Escape') {
            event.preventDefault(); event.stopPropagation()
            input.value = options.formatModeValue(variable, mode.modeId)
          }
        }
      })`,
          'const value = variable.valuesByMode[mode.modeId]',
          `const value = variable.valuesByMode[mode.modeId]
      if (value && typeof value === 'object' && 'cssColor' in value) {
        return h('span', { class: 'font-mono text-xs text-surface' }, options.formatModeValue(variable, mode.modeId))
      }`,
          "'flex size-5 cursor-pointer items-center justify-center rounded border-none bg-transparent text-muted opacity-0 transition-opacity group-hover:opacity-100 hover:text-surface'",
          "'flex size-6 cursor-pointer items-center justify-center rounded border-none bg-transparent text-muted hover:text-surface'",
          'onClick: () => options.removeVariable(row.original.id)',
          "'aria-label': 'Delete ' + row.original.name,\n          onClick: () => options.removeVariable(row.original.id)"],
        'packages/vue/src/variables/use.ts': ['75e76700f882d4dc6b80615977363b9a061a08067a0cc61fffb4472234b81f47',
          "import type { Variable }", "import type { Variable, VariableValue }",
          'const activeCollection = computed(() => editor.getCollection(activeCollectionId.value) ?? null)',
          'const activeCollection = useSceneComputed(() => editor.getCollection(activeCollectionId.value) ?? null)',
          'const activeModes = computed(() => activeCollection.value?.modes ?? [])',
          `const observedModes = useSceneComputed(() => activeCollection.value?.modes.map(mode => ({ ...mode })) ?? [])
  const activeModes = computed((previous?: typeof observedModes.value) => {
    const modes = observedModes.value
    // Keep cell renderers and focus stable when only a variable value changes.
    return previous?.length === modes.length && previous.every((mode, index) =>
      mode.modeId === modes[index].modeId && mode.name === modes[index].name) ? previous : modes
  })`,
          "  const searchTerm = ref('')",
          `  const searchTerm = ref('')
  const variableError = ref('')
  function checkedVariableAction<T>(action: () => T): T | undefined {
    try { return action() } catch (error) {
      if (!(error instanceof Error) || !/^(Native CSS color|CSS color expression|Native number|Native variable mode):/.test(error.message)) throw error
      variableError.value = 'Variables unchanged. ' + error.message
    }
  }`,
          '    ...variableActions',
          `    ...variableActions,
    variableError,
    dismissVariableError: () => { variableError.value = '' },
    parseVariableValue: (variable: Variable, raw: string) => {
      let value: VariableValue | undefined
      checkedVariableAction(() => { value = variableActions.parseVariableValue(variable, raw) })
      return value
    },
    removeVariable: (id: string) => checkedVariableAction(() => variableActions.removeVariable(id)),
    removeCollection: (id: string) => checkedVariableAction(() => collectionActions.removeCollection(id)),
    addMode: () => checkedVariableAction(() => collectionActions.addMode()),
    removeMode: (id: string) => checkedVariableAction(() => collectionActions.removeMode(id)),
    duplicateMode: (id: string) => checkedVariableAction(() => collectionActions.duplicateMode(id)),
    setDefaultMode: (id: string) => checkedVariableAction(() => collectionActions.setDefaultMode(id)),
    updateVariableValue: (id: string, modeId: string, value: VariableValue) =>
      checkedVariableAction(() => variableActions.updateVariableValue(id, modeId, value))`],
        'src/components/ColorPicker/ColorInput.vue': ['8cf39a99c75e377a25f8e020ac50e8d158bdd865a827858f01af063bee80340a',
          '  editable = false,', "  editable = false,\n  label = 'Edit color',",
          '  editable?: boolean', '  editable?: boolean\n  label?: string',
          '<ColorPicker :color="color"', '<ColorPicker :label="label" :color="color"',
          '<span v-else class="min-w-0 flex-1 truncate font-mono text-xs text-muted">',
          '<span v-else class="min-w-0 flex-1 truncate font-mono text-xs text-surface">'],
        'src/components/ColorPicker/ColorPicker.vue': ['6e5cd950098cb3bd44212df502074eb69e943dbf429ac47d86b72e6fb5151b00',
          'const { color, okhcl = null } = defineProps<{ color: Color; okhcl?: OkHCLControls | null }>()',
          "const { color, label = 'Edit color', okhcl = null } = defineProps<{ color: Color; label?: string; okhcl?: OkHCLControls | null }>()",
          ':color="color"', ':color="color"\n    :label="label"',
          "swatch: 'size-5", "swatch: 'size-6"],
        // The primitive owns this relationship and its lazy content identity.
        // Consumer attributes are overwritten by its internal as-child slot.
        [popoverTrigger]: ['e4c409d310c4ee2a53d7b4c95fe697519ee01bb09a395d0b5c5abb7bf12975fa',
          '"aria-controls": unref(rootContext).contentId,',
          '"aria-controls": unref(rootContext).open.value ? unref(rootContext).contentId || void 0 : void 0,'],
        'src/components/color-picker-panel/FormatControls.vue': ['4234dcc5c2a20317118bbb031433f6cb438d3d98d3e15b7d62130bdc859012b0',
          'data-test-id="color-format-select"', 'data-test-id="color-format-select"\n      label="Color format"'],
        'src/theme/color-slider.ts': ['3a683f6b9543981f6bf12a04a080f5fb20ece8eae8179b770b9f76e2afbc0686',
          "label: 'w-7", "label: 'w-14",
          'text-[10px] font-medium text-muted', 'text-[10px] font-medium text-surface',
          ' outline-none ring-offset-1 focus-visible:ring-2 focus-visible:ring-primary', ''],
        'src/components/color-picker-panel/StandardColorSlider.vue': ['a9392bf8bfcec4ec6be4665bc3999a8fbede1eac2759e5b11603af510e7e0cf5'],
        // Retain both existing channel parsers; supply their omitted names.
        ...Object.fromEntries([
          ['HslFields.vue', 'd01c3313bca682f87d386184802fc12d3379296d55c86e9a2bf4a44fe94d7eca', 'hsl', [['h', 'HSL hue'], ['s', 'HSL saturation'], ['l', 'HSL lightness']]],
          ['HsbFields.vue', '84215eff2027c2b38683f4e101e6783531d73f1a4b5273343fbaef545b5e736a', 'hsb', [['h', 'HSB hue'], ['s', 'HSB saturation'], ['b', 'HSB brightness']]],
        ].map(([file, digest, space, fields]) => [`src/components/color-picker-panel/${file}`, [digest,
          ...fields.flatMap(([channel, label]) => {
            const value = `:value="Math.round(ctx.${space}Color.${channel}${space === 'hsl' ? ' ?? 0' : ''})"`
            return [value, `aria-label="${label}"\n      ${value}`]
          }),
          'text-[10px] leading-4 text-muted', 'text-[10px] leading-4 text-surface',
        ]])),
        'src/components/properties/VariablesSection.vue': ['5e566f51c71bee2c77d1902d5d1f0d7f9d5f30e4b6a0db6a9b2ad410fe5f9175',
          '<IconButton :label="panels.openVariables"', '<IconButton class="min-h-6 min-w-6" :label="panels.openVariables"'],
        'src/components/variables/VariablesDialog.vue': ['d8f09a9aefffceb59356cddf38208ad6e25976ebf2e641f08c1eeff6c2657cbe',
          'text-xs whitespace-nowrap text-muted data-[state=active]:bg-hover data-[state=active]:text-surface',
          'text-xs whitespace-nowrap text-surface data-[state=active]:bg-hover data-[state=active]:text-surface',
          'text-left text-[11px] font-medium text-muted', 'text-left text-[11px] font-medium text-surface',
          'placeholder:text-muted', 'placeholder:text-surface',
          '<span class="text-xs text-muted">{{ panels.createVariable }}</span>',
          '<span class="text-xs text-surface">{{ panels.createVariable }}</span>',
          "import { watch, type Component } from 'vue'", "import { computed, nextTick, useId, type Component } from 'vue'",
          "const collectionInput = templateRef<HTMLInputElement>('collectionInput')",
          `const collectionMenu = templateRef<HTMLButtonElement>('collectionMenu')
const emptyMessageId = useId()
let pendingCollectionRename: string | undefined
function onCollectionMenuClose(event: Event) {
  // A selected item still belongs to the closing menu's focus trap. Start
  // the rename at its lifecycle handoff, not during selection or on a timer.
  const id = pendingCollectionRename
  pendingCollectionRename = undefined
  if (!id) return
  event.preventDefault()
  ctx.startRenameCollection(id)
}
function setCollectionInput(value: unknown) {
  void ctx.collectionRename.focusInput(value instanceof HTMLInputElement ? value : null)
}
const renamingCollection = computed(() => ctx.collections.value.find(collection => collection.id === ctx.collectionRename.editingId.value))
async function onCollectionRenameKeydown(event: KeyboardEvent) {
  if (event.isComposing || !['Enter', 'Escape'].includes(event.code)) return
  event.preventDefault()
  event.stopPropagation()
  ctx.collectionRename.onKeydown(event)
  await nextTick()
  collectionMenu.value?.focus()
}
async function commitCollectionRename(event: FocusEvent) {
  const id = ctx.collectionRename.editingId.value
  if (!id) return
  const next = event.relatedTarget
  const dialog = event.target instanceof Element ? event.target.closest('[role="dialog"]') : null
  ctx.collectionRename.commit(id, event)
  await nextTick()
  // The modal's removal fallback can replace the browser's chosen Tab target.
  // Retain that destination, but never steal a subsequent focus change.
  if (next instanceof HTMLElement && next.isConnected && dialog?.contains(next) && document.activeElement === dialog) next.focus()
}`,
          `watch(collectionInput, (input) => {
  void ctx.collectionRename.focusInput(input)
})`, '',
          `              <input
                v-if="ctx.collectionRename.editingId.value === col.id"
                ref="collectionInput"
                class="w-24 rounded border border-accent bg-input px-2 py-0.5 text-xs text-surface outline-none"
                :value="col.name"
                @blur="ctx.collectionRename.commit(col.id, $event)"
                @keydown="ctx.collectionRename.onKeydown"
              />
              <TabsTrigger
                v-else`, `              <TabsTrigger`,
          `        <TabsContent
          v-for="col in ctx.collections.value"`,
          `        <label v-if="renamingCollection" class="flex shrink-0 items-center gap-2 border-b border-border px-4 py-2 text-xs text-surface">
          <span>{{ dialogs.renameCollection }}: {{ renamingCollection.name }}</span>
          <input :ref="setCollectionInput" type="text"
            class="min-w-0 flex-1 rounded border border-accent bg-input px-2 py-1 text-xs text-surface"
            :value="renamingCollection.name"
            @blur="commitCollectionRename"
            @keydown="onCollectionRenameKeydown" />
        </label>
        <TabsContent
          v-for="col in ctx.collections.value"`,
          `                <DropdownMenuContent
                  side="bottom"`,
          `                <DropdownMenuContent
                  @close-auto-focus="onCollectionMenuClose"
                  side="bottom"`,
          '@select="ctx.startRenameCollection(ctx.activeCollectionId.value)"',
          '@select="pendingCollectionRename = ctx.activeCollectionId.value"',
          "const modeInput = templateRef<HTMLInputElement>('modeInput')",
          `function setModeInput(value: unknown) {
  void ctx.modeRename.focusInput(value instanceof HTMLInputElement ? value : null)
}`,
          `watch(modeInput, (input) => {
  void ctx.modeRename.focusInput(input)
})`, '',
          '    <DialogTitle class="sr-only">{{ dialogs.localVariables }}</DialogTitle>',
          `    <DialogTitle class="sr-only">{{ dialogs.localVariables }}</DialogTitle>
    <div class="shrink-0 px-4">
      <p role="status" class="text-xs text-surface">{{ ctx.variableError.value }}</p>
      <button v-if="ctx.variableError.value" class="my-2 rounded px-2 py-1 text-xs text-surface"
        @click="ctx.dismissVariableError(); collectionMenu?.focus()">Dismiss variable message</button>
    </div>`,
          'data-test-id="variables-collection-menu"', 'data-test-id="variables-collection-menu" ref="collectionMenu" aria-label="Collection actions"',
          'data-test-id="variables-add-collection"', 'data-test-id="variables-add-collection" :aria-label="dialogs.createCollection"',
          'data-test-id="variables-search-input"',
          `data-test-id="variables-search-input" :aria-label="dialogs.search"
                :aria-describedby="!ctx.variables.value.length ? emptyMessageId + '-' + ctx.activeCollectionId.value : undefined"`,
          '              <tbody>',
          `              <tbody>
                <tr v-if="!ctx.table.getRowModel().rows.length">
                  <td :colspan="ctx.table.getVisibleLeafColumns().length + 1" class="px-4 py-6 text-sm text-surface">
                    <p :id="emptyMessageId + '-' + col.id">{{ panels.noVariablesFound }}</p>
                  </td>
                </tr>`,
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
        if (path.endsWith('/VariablesDialog.vue')) {
          // Mode actions use the same menu components as collection actions.
          // A right-click-only span has no ordinary keyboard activation path.
          for (const part of ['Content', 'Item', 'Portal', 'Root', 'Separator', 'Trigger']) {
            source = replaceOnce(source, '  ContextMenu' + part + ',\n', '')
            source = source.replaceAll('ContextMenu' + part, 'DropdownMenu' + part)
          }
          source = replaceOnce(source, '<span\n                            :data-default=',
            `<button type="button" class="min-h-6 cursor-pointer border-none bg-transparent p-0 text-left"
                            :aria-label="String(header.column.columnDef.header) + ' mode actions' + (getModeId(header.column.id) === col.defaultModeId ? ', default' : '')"
                            :data-default=`)
          source = replaceOnce(source, '{{ header.column.columnDef.header }}\n                          </span>',
            '{{ header.column.columnDef.header }}\n                          </button>')
          source = replaceOnce(source, 'ref="modeInput"',
            ':ref="setModeInput" :aria-label="\'Rename \' + String(header.column.columnDef.header) + \' mode\'"')
          source = replaceOnce(source, '<DropdownMenuContent :class="menuCls.content">',
            '<DropdownMenuContent :class="menuCls.content" @close-auto-focus="ctx.modeRename.editingId.value && $event.preventDefault()">')
        }
        if (path.endsWith('/VariablesDialog.vue') || path.endsWith('/VariablesSection.vue')) source += `
<style scoped>
:deep(button:focus-visible), :deep(input:focus-visible) { outline: revert; outline-offset: 2px; }
</style>\n`
        if (path.endsWith('/FormatControls.vue') || path.endsWith('/StandardColorSlider.vue')) source += `
<style scoped>
:deep(button:focus-visible), :deep(input:focus-visible),
:deep([role="slider"]:focus-visible), :deep([role="spinbutton"]:focus-visible) { outline: revert; outline-offset: -2px; }
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
if (correctedControls.size !== 19) throw new Error('Browser omitted a required editor control correction')
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
for (const file of suppliedFonts.files) {
  const destination = join(upstream, 'dist', file.path)
  mkdirSync(dirname(destination), { recursive: true })
  writeFileSync(destination, file.bytes, { flag: 'wx' })
}
const inputs = Object.fromEntries(readdirSync(adapter).filter(name => name.endsWith('.mjs') ||
  ['package.json', 'package-lock.json', 'Dockerfile', 'nginx.conf', 'LICENSE', 'NOTICE'].includes(name))
  .sort().map(name => [name, sha256(readFileSync(join(adapter, name)))]))
writeFileSync(join(upstream, 'dist/platformkit-provenance.json'), JSON.stringify({
  schema: 'platformkit.openpencil.provenance.v1',
  upstream: { version: sdkVersion, commit: 'c29654cd07ac46b53e76c16b18505919f16571be' },
  adapter: { inputs, correctedModules: [...seen].sort() },
  upstreamLockSHA256: sha256(readFileSync(join(upstream, 'bun.lock'))),
  dependencyLicenses: { path: dependencyLicenses, sha256: sha256(readFileSync(join(upstream, 'dist', dependencyLicenses))) },
  fontFaces: suppliedFonts.faces, designProfile: null, defaultDocument: null,
  scope: 'generic-editor-without-packaged-design',
}, null, 2) + '\n', { flag: 'wx' })
