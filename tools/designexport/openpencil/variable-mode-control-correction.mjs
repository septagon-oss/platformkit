// Extend the pinned sidebar; the graph still owns mode resolution and the
// ordinary node action still owns validation, layout, history and persistence.
export function correctModeControls(source, replace, bindingModule) {
  source = replace(source, "import { computed } from 'vue'", "import { computed, nextTick, ref, useId, watch } from 'vue'")
  source = replace(source, 'useI18n, useSceneComputed', 'useI18n, useSceneComputed, useSelectionState')
  source = replace(source, 'const editor = useEditorStore()', `
import { nodeModeChoice } from ${JSON.stringify(bindingModule)}
const editor = useEditorStore()
const { selectedNode, selectedCount } = useSelectionState()
const modeId = useId()
const modeError = ref<{ collection: string, message: string } | null>(null)
const modeTarget = useSceneComputed(() => selectedCount.value > 1 ? null :
  selectedNode.value ?? editor.graph.getNode(editor.state.currentPageId))
function explicitModes(id: string) {
  return (editor.graph as typeof editor.graph & {
    getNodeExplicitVariableModes(id: string): Record<string, string>
  }).getNodeExplicitVariableModes(id)
}
const modeChoices = useSceneComputed(() => modeTarget.value ? explicitModes(modeTarget.value.id) : {})
const modeCollections = useSceneComputed(() => [...editor.graph.variableCollections.values()])
const unavailableModes = computed(() => Object.entries(modeChoices.value).some(([id, value]) =>
  !modeCollections.value.find(collection => collection.id === id)?.modes.some(mode => mode.modeId === value)))
watch(() => JSON.stringify([modeTarget.value?.id, modeChoices.value]), () => { modeError.value = null })
function resolvedMode(collection: { id: string, modes: { modeId: string, name: string }[] }) {
  const value = modeTarget.value && editor.graph.getNodeVariableModeId(modeTarget.value.id, collection.id)
  return collection.modes.find(mode => mode.modeId === value)?.name ?? 'Unavailable mode'
}
function changeMode(collection: string, event: Event) {
  const control = event.target as HTMLSelectElement, target = modeTarget.value
  if (!target) return
  try {
    const variableModes = nodeModeChoice(editor.graph, target.id, collection, control.value || null)
    editor.updateNodeWithUndo(target.id, { variableModes }, 'Change variable mode')
    modeError.value = null
  } catch (error) {
    modeError.value = { collection, message: error instanceof Error ? error.message : String(error) }
  } finally {
    control.value = explicitModes(target.id)[collection] ?? ''
  }
}
async function resetModes(event: Event) {
  if (!modeTarget.value) return
  const control = event.currentTarget as HTMLButtonElement
  try {
    editor.updateNodeWithUndo(modeTarget.value.id, { variableModes: {} }, 'Reset variable modes')
    modeError.value = null
    await nextTick()
    const next = control.closest('fieldset')?.querySelector('select') ?? control.closest('section')?.querySelector('button')
    next?.focus()
  } catch (error) {
    modeError.value = { collection: '', message: error instanceof Error ? error.message : String(error) }
  }
}`)
  source = replace(source, ':empty="!hasVariables"', ':empty="collectionCount === 0"')
  source = replace(source, '</PanelSection>', `
    <fieldset v-if="modeTarget" data-test-id="variable-node-modes" class="mt-3 min-w-0 border-0 p-0 text-xs text-surface">
      <legend class="font-medium">{{ modeTarget.type === 'CANVAS' ? 'Page modes' : 'Layer modes' }}</legend>
      <p class="mt-1 break-words">{{ modeTarget.name }}</p>
      <p v-if="unavailableModes" class="mt-2">Some saved choices are unavailable. Select another mode or reset all overrides.</p>
      <div v-for="collection in modeCollections" :key="collection.id" class="mt-3 min-w-0">
        <label :for="modeId + '-' + collection.id" class="block break-words">{{ collection.name }}</label>
        <select :id="modeId + '-' + collection.id" :name="collection.id"
          :value="modeChoices[collection.id] ?? ''"
          class="mt-1 w-full min-w-0 rounded border border-border bg-surface/10 px-2 text-xs text-surface"
          :aria-invalid="modeError?.collection === collection.id ? 'true' : undefined"
          :aria-describedby="modeId + '-value-' + collection.id + (modeError?.collection === collection.id ? ' ' + modeId + '-error' : '')"
          @change="changeMode(collection.id, $event)"
          @keydown="!($event.ctrlKey || $event.metaKey) && $event.stopPropagation()">
          <option value="">Auto (inherit)</option>
          <option v-if="modeChoices[collection.id] !== undefined && !collection.modes.some(mode => mode.modeId === modeChoices[collection.id])"
            :value="modeChoices[collection.id]" disabled>Unavailable mode</option>
          <option v-for="mode in collection.modes" :key="mode.modeId" :value="mode.modeId">
            {{ mode.name }}{{ mode.modeId === collection.defaultModeId ? ' (default)' : '' }}
          </option>
        </select>
        <p :id="modeId + '-value-' + collection.id" class="mt-1 break-words">
          {{ modeChoices[collection.id] === undefined ? 'Inherited' : 'Explicit' }}: {{ resolvedMode(collection) }}
        </p>
      </div>
      <button type="button" class="mt-3 w-full rounded border border-border px-2 text-left"
        :disabled="!Object.keys(modeChoices).length" :aria-describedby="modeError?.collection === '' ? modeId + '-error' : undefined"
        @click="resetModes">Reset all mode overrides</button>
      <p :id="modeId + '-error'" class="mt-2 break-words">{{ modeError ? 'Mode change refused. Previous choices were kept. ' + modeError.message : '' }}</p>
    </fieldset>
    <p v-else class="mt-3 text-xs text-surface">Select one layer to edit its modes.</p>
  </PanelSection>`)
  return source + `
<style scoped>
/* Tailwind scans the upstream files, before this pinned template transform. */
fieldset select, fieldset button { min-height: 40px; }
select { color-scheme: dark; }
select:focus-visible, button:focus-visible { outline: revert; outline-offset: 2px; }
@media (pointer: coarse) { fieldset select, fieldset button { min-height: 44px; } }
</style>
`
}
