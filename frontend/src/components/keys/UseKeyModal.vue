<template>
  <BaseDialog
    :show="show"
    :title="t('keys.useKeyModal.title')"
    width="wide"
    @close="emit('close')"
  >
    <div class="space-y-4">
      <!-- No Group Assigned Warning -->
      <div v-if="!platform" class="flex items-start gap-3 p-4 rounded-lg bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800">
        <svg class="w-5 h-5 text-yellow-500 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
        </svg>
        <div>
          <p class="text-sm font-medium text-yellow-800 dark:text-yellow-200">
            {{ t('keys.useKeyModal.noGroupTitle') }}
          </p>
          <p class="text-sm text-yellow-700 dark:text-yellow-300 mt-1">
            {{ t('keys.useKeyModal.noGroupDescription') }}
          </p>
        </div>
      </div>

      <!-- Platform-specific content -->
      <template v-else>
        <!-- Description -->
        <p class="text-sm text-gray-600 dark:text-gray-400">
          {{ platformDescription }}
        </p>

        <!-- Actual available models (channel-level allow-list, auto-injected) -->
        <div
          data-testid="use-key-models-status"
          class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-700"
        >
          <p
            v-if="keyModelsState === 'loading'"
            class="text-xs text-gray-500 dark:text-gray-400"
          >
            {{ t('keys.useKeyModal.modelSelector.loading') }}
          </p>
          <template v-else-if="effectiveModels.length">
            <p
              data-testid="use-key-model-count"
              class="text-xs text-gray-500 dark:text-gray-400"
            >
              {{ t('keys.useKeyModal.modelSelector.count', { count: effectiveModels.length }) }}
            </p>
            <!-- Default-model pills: one-click target for single-model configs.
                 Text-capable models only — a media id would become a `default`
                 that the generated config entries intentionally exclude. -->
            <div
              v-if="primaryModelCandidates.length > 1"
              data-testid="use-key-model-pills"
              class="mt-2 flex flex-wrap gap-1.5"
              role="group"
              :aria-label="t('keys.useKeyModal.modelSelector.defaultModel')"
            >
              <button
                v-for="modelId in primaryModelCandidates"
                :key="modelId"
                type="button"
                data-testid="use-key-model-pill"
                :data-model="modelId"
                :aria-pressed="modelId === primaryModel()"
                @click="selectedPrimaryModel = modelId"
                :class="[
                  'rounded-full border px-2.5 py-1 font-mono text-xs transition-colors',
                  modelId === primaryModel()
                    ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-500 dark:bg-primary-900/30 dark:text-primary-300'
                    : 'border-gray-200 text-gray-600 hover:border-gray-300 hover:text-gray-900 dark:border-dark-600 dark:text-gray-400 dark:hover:text-gray-200'
                ]"
              >
                {{ modelId }}
              </button>
            </div>
          </template>
          <p
            v-else
            data-testid="use-key-model-empty"
            class="text-xs text-amber-600 dark:text-amber-400"
          >
            {{ t('keys.useKeyModal.modelSelector.empty') }}
          </p>
        </div>

        <!-- Client Tabs -->
        <div v-if="clientTabs.length" class="overflow-x-auto border-b border-gray-200 dark:border-dark-700">
          <nav class="-mb-px flex min-w-max gap-4 sm:gap-6" aria-label="Client">
            <button
              v-for="tab in clientTabs"
              :key="tab.id"
              type="button"
              @click="activeClientTab = tab.id"
              :class="[
                'whitespace-nowrap py-2.5 px-1 border-b-2 font-medium text-sm transition-colors',
                activeClientTab === tab.id
                  ? 'border-primary-500 text-primary-600 dark:text-primary-400'
                  : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300'
              ]"
            >
              <span class="flex items-center gap-2">
                <component :is="tab.icon" class="w-4 h-4" />
                {{ tab.label }}
              </span>
            </button>
          </nav>
        </div>

        <!-- Codex Authentication Mode -->
        <div
          v-if="showCodexAuthMode"
          class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
        >
          <div class="mb-2">
            <p class="text-sm font-medium text-gray-900 dark:text-white">
              {{ t('keys.useKeyModal.openai.authModeTitle') }}
            </p>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('keys.useKeyModal.openai.authModeDescription') }}
            </p>
          </div>
          <div
            class="grid grid-cols-2 gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
            role="radiogroup"
            :aria-label="t('keys.useKeyModal.openai.authModeTitle')"
          >
            <button
              type="button"
              role="radio"
              data-testid="codex-auth-mode-legacy"
              :aria-checked="codexAuthMode === 'legacy'"
              :class="[
                'rounded-md px-3 py-2 text-sm font-medium transition-colors',
                codexAuthMode === 'legacy'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              ]"
              @click="codexAuthMode = 'legacy'"
            >
              {{ t('keys.useKeyModal.openai.authModeLegacy') }}
            </button>
            <button
              type="button"
              role="radio"
              data-testid="codex-auth-mode-api-key"
              :aria-checked="codexAuthMode === 'api-key'"
              :class="[
                'rounded-md px-3 py-2 text-sm font-medium transition-colors',
                codexAuthMode === 'api-key'
                  ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'
              ]"
              @click="codexAuthMode = 'api-key'"
            >
              {{ t('keys.useKeyModal.openai.authModeApiKey') }}
            </button>
          </div>
          <div
            v-if="codexAuthMode === 'api-key'"
            data-testid="codex-api-key-restart-notice"
            class="mt-3 flex items-start gap-2 border-l-2 border-amber-400 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-500 dark:bg-amber-950/30 dark:text-amber-200"
          >
            <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
            <p>{{ t('keys.useKeyModal.openai.authModeApiKeyRestartNotice') }}</p>
          </div>
        </div>

        <!-- OS/Shell Tabs -->
        <div v-if="showShellTabs" class="overflow-x-auto border-b border-gray-200 dark:border-dark-700">
          <nav class="-mb-px flex min-w-max gap-4" aria-label="Tabs">
            <button
              v-for="tab in currentTabs"
              :key="tab.id"
              type="button"
              @click="activeTab = tab.id"
              :class="[
                'whitespace-nowrap py-2.5 px-1 border-b-2 font-medium text-sm transition-colors',
                activeTab === tab.id
                  ? 'border-primary-500 text-primary-600 dark:text-primary-400'
                  : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300'
              ]"
            >
              <span class="flex items-center gap-2">
                <component :is="tab.icon" class="w-4 h-4" />
                {{ tab.label }}
              </span>
            </button>
          </nav>
        </div>

        <!-- Code Blocks (Stacked for multi-file platforms) -->
        <div class="space-y-4">
          <div
            v-for="(file, index) in currentFiles"
            :key="index"
            class="relative"
          >
            <!-- File Hint (if exists) -->
            <p v-if="file.hint" class="text-xs text-amber-600 dark:text-amber-400 mb-1.5 flex items-center gap-1">
              <Icon name="exclamationCircle" size="sm" class="flex-shrink-0" />
              {{ file.hint }}
            </p>
            <div class="bg-gray-900 dark:bg-dark-900 rounded-xl overflow-hidden">
              <!-- Code Header -->
              <div class="flex items-center justify-between px-4 py-2 bg-gray-800 dark:bg-dark-800 border-b border-gray-700 dark:border-dark-700">
                <span class="min-w-0 truncate text-xs text-gray-400 font-mono">{{ file.path }}</span>
                <button
                  type="button"
                  @click="copyContent(file.content, index)"
                  class="flex flex-shrink-0 items-center gap-1.5 px-2.5 py-1 text-xs font-medium rounded-lg transition-colors"
                :class="copiedIndex === index
                    ? 'bg-gray-500/20 text-gray-400'
                    : 'bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white'"
                >
                  <svg v-if="copiedIndex === index" class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                  </svg>
                  <svg v-else class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184" />
                  </svg>
                  {{ copiedIndex === index ? t('keys.useKeyModal.copied') : t('keys.useKeyModal.copy') }}
                </button>
              </div>
              <!-- Code Content -->
              <pre class="p-4 text-sm font-mono text-gray-100 overflow-x-auto"><code v-if="file.highlighted" v-html="file.highlighted"></code><code v-else v-text="file.content"></code></pre>
            </div>
          </div>
        </div>

        <section
          v-if="showCodexModelCatalog"
          data-testid="codex-model-catalog"
          class="overflow-hidden rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-800/50"
        >
          <div class="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <div class="min-w-0">
              <h3 class="text-sm font-medium text-gray-900 dark:text-white">
                {{ t('keys.useKeyModal.codexModelCatalog.title') }}
              </h3>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('keys.useKeyModal.codexModelCatalog.description') }}
              </p>
              <p class="mt-1 truncate font-mono text-xs text-gray-700 dark:text-gray-300">
                {{ codexModelCatalogPath }}
              </p>
            </div>
            <button
              v-if="codexModelManifestState === 'ready'"
              type="button"
              class="btn btn-primary min-h-9 flex-shrink-0 px-3 text-xs"
              @click="downloadCodexModelManifest"
            >
              <Icon name="download" size="sm" class="mr-1.5" />
              {{ t('keys.useKeyModal.codexModelCatalog.download') }}
            </button>
            <button
              v-else
              type="button"
              data-testid="codex-model-catalog-fetch"
              class="btn btn-primary min-h-9 flex-shrink-0 px-3 text-xs"
              :disabled="codexModelManifestState === 'loading' || !apiKey"
              @click="loadCodexModelManifest"
            >
              <Icon
                name="refresh"
                size="sm"
                class="mr-1.5"
                :class="codexModelManifestState === 'loading' ? 'animate-spin' : ''"
              />
              {{ codexModelManifestState === 'error'
                ? t('keys.useKeyModal.codexModelCatalog.retry')
                : t('keys.useKeyModal.codexModelCatalog.fetch') }}
            </button>
          </div>
          <p
            v-if="codexModelManifestState === 'ready'"
            class="border-t border-gray-200 px-4 py-2 text-xs text-emerald-700 dark:border-dark-700 dark:text-emerald-300"
          >
            {{ t('keys.useKeyModal.codexModelCatalog.modelsCount', { count: codexModelManifestModelCount }) }}
          </p>
          <p
            v-else-if="codexModelManifestState === 'error'"
            class="border-t border-red-200 px-4 py-2 text-xs text-red-700 dark:border-red-900 dark:text-red-300"
          >
            {{ t('keys.useKeyModal.codexModelCatalog.errorDescription') }}
          </p>
        </section>

        <!-- Usage Note -->
        <div v-if="showPlatformNote" class="flex items-start gap-3 p-3 rounded-lg bg-blue-50 dark:bg-blue-900/20 border border-blue-100 dark:border-blue-800">
          <Icon name="infoCircle" size="md" class="text-blue-500 flex-shrink-0 mt-0.5" />
          <p class="text-sm text-blue-700 dark:text-blue-300">
            {{ platformNote }}
          </p>
        </div>
      </template>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button
          @click="emit('close')"
          class="btn btn-secondary"
        >
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, computed, h, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { fetchCodexModelsManifest } from '@/api/codex'
import { fetchKeyAvailableModels } from '@/api/gatewayModels'
import { getAvailable, resolveChannelModelsForGroup } from '@/api/channels'
import type { GroupPlatform } from '@/types'
import {
  findCodexCatalogModel,
  formatCodexReasoningEffortTomlLine,
  parseCodexCatalogModels,
  selectCodexConfigReasoningEffort
} from '@/utils/codexCatalogConfig'
import {
  escapeCmdValue,
  escapePsDq,
  escapeShDq,
  escapeTomlBasicString,
  isMediaModelId,
  normalizeModelIds,
  pickPrimaryModel
} from '@/utils/keyConfigEscape'

interface Props {
  show: boolean
  apiKey: string
  baseUrl: string
  platform: GroupPlatform | null
  allowMessagesDispatch?: boolean
  /** Key's group id: used to filter the channel-level allow-list. */
  groupId?: number | null
  /**
   * Channel-effective model list provided by the parent (already resolved
   * from channel restrictions). Takes precedence over in-modal fetching.
   */
  availableModels?: string[]
}

interface Emits {
  (e: 'close'): void
}

interface TabConfig {
  id: string
  label: string
  icon: Component
}

interface FileConfig {
  path: string
  content: string
  hint?: string  // Optional hint message for this file
  highlighted?: string
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const { copyToClipboard: clipboardCopy } = useClipboard()

const copiedIndex = ref<number | null>(null)
const activeTab = ref<string>('unix')
const activeClientTab = ref<string>('claude')
type CodexAuthMode = 'legacy' | 'api-key'
const codexAuthMode = ref<CodexAuthMode>('legacy')
type CodexModelManifestState = 'idle' | 'loading' | 'ready' | 'error'
const codexModelManifestState = ref<CodexModelManifestState>('idle')
const codexModelManifestContent = ref('')
const codexModelManifestModelCount = ref(0)
let codexModelManifestController: AbortController | null = null
let codexModelManifestRequestID = 0

// Actual available models: channel-level allow-list, never hardcoded fiction.
// Priority: parent-provided `availableModels` > gateway GET /v1/models (the key's
// channel-effective shelf) > user GET /channels/available filtered by group.
type KeyModelsState = 'idle' | 'loading' | 'ready' | 'error'
const keyModelsState = ref<KeyModelsState>('idle')
const gatewayKeyModels = ref<string[]>([])
// Trusted per-model token limits from the gateway /v1/models `metadata`
// section (repo-owned model data doc). Only the gateway-derived shelf carries
// these; parent-provided and channel-fallback lists never do. Used to fill
// OpenCode `limit` so unknown models don't fall back to context=0.
const gatewayModelLimits = ref<Record<string, { contextWindow?: number; maxOutputTokens?: number }>>({})
const channelAllowListModels = ref<string[]>([])
let keyModelsController: AbortController | null = null
let keyModelsRequestID = 0

// Parent-provided lists are already channel-resolved upstream: drop wildcards (`*`)
// but trust concrete membership.
const propAvailableModels = computed(() => normalizeModelIds(props.availableModels, { dropWildcards: true }))

const effectiveModels = computed(() => {
  if (propAvailableModels.value.length) return propAvailableModels.value
  if (gatewayKeyModels.value.length) return gatewayKeyModels.value
  return channelAllowListModels.value
})

// Text-client default: user-picked pill when still available, otherwise the
// first non-media model so image/video-only ids are never selected as the
// terminal / config default.
const selectedPrimaryModel = ref('')
const primaryModel = () => {
  if (selectedPrimaryModel.value && effectiveModels.value.includes(selectedPrimaryModel.value)) {
    return selectedPrimaryModel.value
  }
  return pickPrimaryModel(effectiveModels.value)
}

// Pill candidates: text-capable models only. Media ids stay visible in the
// count/list (the channel may serve them on media endpoints) but are never
// offered as a text `default`.
const primaryModelCandidates = computed(() =>
  effectiveModels.value.filter((id) => !isMediaModelId(id))
)

watch(effectiveModels, (models) => {
  if (!models.length) {
    selectedPrimaryModel.value = ''
    return
  }
  if (selectedPrimaryModel.value && models.includes(selectedPrimaryModel.value)) return
  selectedPrimaryModel.value = pickPrimaryModel(models)
}, { immediate: true })

const keyModelsContext = computed(() => {
  if (!props.show || !props.platform || !props.apiKey) return ''
  return `${props.platform}|${props.groupId ?? ''}|${props.baseUrl}|${props.apiKey}|${propAvailableModels.value.join(',')}`
})

function resetKeyModels() {
  keyModelsController?.abort()
  keyModelsController = null
  keyModelsRequestID += 1
  keyModelsState.value = 'idle'
  gatewayKeyModels.value = []
  gatewayModelLimits.value = {}
  channelAllowListModels.value = []
  selectedPrimaryModel.value = ''
}

async function loadKeyModels() {
  if (!props.show || !props.platform || !props.apiKey) return
  if (propAvailableModels.value.length) {
    keyModelsController?.abort()
    keyModelsController = null
    keyModelsState.value = 'ready'
    return
  }
  keyModelsController?.abort()
  const controller = new AbortController()
  const requestID = ++keyModelsRequestID
  keyModelsController = controller
  keyModelsState.value = 'loading'
  try {
    // The gateway resolves the channel-owned shelf for this exact key.
    const models = await fetchKeyAvailableModels(props.baseUrl, props.apiKey, controller.signal)
    if (requestID !== keyModelsRequestID) return
    // Server-derived: re-apply non-concrete filtering so backend
    // platform-default fallbacks never surface as real key models.
    const limits: Record<string, { contextWindow?: number; maxOutputTokens?: number }> = {}
    for (const info of models) {
      if (info.contextWindow || info.maxOutputTokens) {
        limits[info.id] = { contextWindow: info.contextWindow, maxOutputTokens: info.maxOutputTokens }
      }
    }
    gatewayKeyModels.value = normalizeModelIds(models.map((m) => m.id), { dropWildcards: true })
    gatewayModelLimits.value = limits
    channelAllowListModels.value = []
    keyModelsState.value = 'ready'
  } catch (gatewayError) {
    const errorName = gatewayError && typeof gatewayError === 'object' && 'name' in gatewayError
      ? String((gatewayError as { name?: unknown }).name || '')
      : ''
    if (requestID !== keyModelsRequestID || errorName === 'AbortError') return
    try {
      // Fallback: channel restrictions for the key's group (still channel-level).
      const channels = await getAvailable({ signal: controller.signal })
      if (requestID !== keyModelsRequestID) return
      gatewayKeyModels.value = []
      gatewayModelLimits.value = {}
      channelAllowListModels.value = normalizeModelIds(
        resolveChannelModelsForGroup(channels, props.groupId ?? null, props.platform),
        { dropWildcards: true }
      )
      keyModelsState.value = 'ready'
    } catch (channelError) {
      const channelErrorName = channelError && typeof channelError === 'object' && 'name' in channelError
        ? String((channelError as { name?: unknown }).name || '')
        : ''
      if (requestID !== keyModelsRequestID || channelErrorName === 'AbortError') return
      gatewayKeyModels.value = []
      gatewayModelLimits.value = {}
      channelAllowListModels.value = []
      keyModelsState.value = 'error'
    }
  } finally {
    if (requestID === keyModelsRequestID) {
      keyModelsController = null
    }
  }
}

watch(keyModelsContext, (context, previousContext) => {
  if (context === previousContext) return
  if (!context) {
    resetKeyModels()
    return
  }
  // Invalidate synchronously: while the new key's shelf loads, generated
  // configs must not keep exposing the previous key's models.
  gatewayKeyModels.value = []
  gatewayModelLimits.value = {}
  channelAllowListModels.value = []
  selectedPrimaryModel.value = ''
  keyModelsState.value = propAvailableModels.value.length ? 'ready' : 'loading'
  void loadKeyModels()
}, { immediate: true })

const showCodexModelCatalog = computed(() =>
  props.show &&
  (activeClientTab.value === 'codex' ||
    (props.platform === 'openai' && activeClientTab.value === 'codex-ws'))
)

const codexModelCatalogPath = computed(() => {
  const isWindows = activeTab.value === 'windows'
  const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
  return joinConfigPath(configDir, 'codex-models.json', isWindows)
})

const codexManifestContext = computed(() => {
  if (!showCodexModelCatalog.value) return ''
  return `${props.platform}|${props.baseUrl}|${props.apiKey}`
})

// Reset tabs when platform changes
const defaultClientTab = computed(() => {
  switch (props.platform) {
    case 'openai':
      return 'codex'
    case 'grok':
      return 'grok'
    case 'gemini':
      return 'gemini'
    case 'antigravity':
      return 'claude'
    default:
      return 'claude'
  }
})

watch(() => props.platform, () => {
  activeTab.value = 'unix'
  activeClientTab.value = defaultClientTab.value
  codexAuthMode.value = 'legacy'
}, { immediate: true })

watch(() => props.show, (show) => {
  if (show) {
    codexAuthMode.value = 'legacy'
  } else {
    resetCodexModelManifest()
  }
})

watch(codexManifestContext, (context, previousContext) => {
  if (context !== previousContext) {
    resetCodexModelManifest()
  }
})

// Reset shell tab when client changes
watch(activeClientTab, () => {
  activeTab.value = 'unix'
})

// Icon components
const AppleIcon = {
  render() {
    return h('svg', {
      fill: 'currentColor',
      viewBox: '0 0 24 24',
      class: 'w-4 h-4'
    }, [
      h('path', { d: 'M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.81-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83M13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11z' })
    ])
  }
}

const WindowsIcon = {
  render() {
    return h('svg', {
      fill: 'currentColor',
      viewBox: '0 0 24 24',
      class: 'w-4 h-4'
    }, [
      h('path', { d: 'M3 12V6.75l6-1.32v6.48L3 12zm17-9v8.75l-10 .15V5.21L20 3zM3 13l6 .09v6.81l-6-1.15V13zm7 .25l10 .15V21l-10-1.91v-5.84z' })
    ])
  }
}

// Terminal icon for Claude Code
const TerminalIcon = {
  render() {
    return h('svg', {
      fill: 'none',
      stroke: 'currentColor',
      viewBox: '0 0 24 24',
      'stroke-width': '1.5',
      class: 'w-4 h-4'
    }, [
      h('path', {
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round',
        d: 'm6.75 7.5 3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0 0 21 17.25V6.75A2.25 2.25 0 0 0 18.75 4.5H5.25A2.25 2.25 0 0 0 3 6.75v10.5A2.25 2.25 0 0 0 5.25 20.25Z'
      })
    ])
  }
}

// Sparkle icon for Gemini
const SparkleIcon = {
  render() {
    return h('svg', {
      fill: 'none',
      stroke: 'currentColor',
      viewBox: '0 0 24 24',
      'stroke-width': '1.5',
      class: 'w-4 h-4'
    }, [
      h('path', {
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round',
        d: 'M9.813 15.904 9 18.75l-.813-2.846a4.5 4.5 0 0 0-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 0 0 3.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 0 0 3.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 0 0-3.09 3.09ZM18.259 8.715 18 9.75l-.259-1.035a3.375 3.375 0 0 0-2.455-2.456L14.25 6l1.036-.259a3.375 3.375 0 0 0 2.455-2.456L18 2.25l.259 1.035a3.375 3.375 0 0 0 2.456 2.456L21.75 6l-1.035.259a3.375 3.375 0 0 0-2.456 2.456ZM16.894 20.567 16.5 21.75l-.394-1.183a2.25 2.25 0 0 0-1.423-1.423L13.5 18.75l1.183-.394a2.25 2.25 0 0 0 1.423-1.423l.394-1.183.394 1.183a2.25 2.25 0 0 0 1.423 1.423l1.183.394-1.183.394a2.25 2.25 0 0 0-1.423 1.423Z'
      })
    ])
  }
}

const clientTabs = computed((): TabConfig[] => {
  if (!props.platform) return []
  switch (props.platform) {
    case 'openai': {
      const tabs: TabConfig[] = [
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'codex-ws', label: t('keys.useKeyModal.cliTabs.codexCliWs'), icon: TerminalIcon },
      ]
      if (props.allowMessagesDispatch) {
        tabs.push({ id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon })
      }
      tabs.push({ id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon })
      return tabs
    }
    case 'gemini':
      return [
        { id: 'gemini', label: t('keys.useKeyModal.cliTabs.geminiCli'), icon: SparkleIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
    case 'antigravity':
      return [
        { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon },
        { id: 'gemini', label: t('keys.useKeyModal.cliTabs.geminiCli'), icon: SparkleIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
    case 'grok':
      return [
        { id: 'grok', label: t('keys.useKeyModal.cliTabs.grokCli'), icon: TerminalIcon },
        { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
    case 'deepseek':
    case 'composite':
      return [
        { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
    default:
      return [
        { id: 'claude', label: t('keys.useKeyModal.cliTabs.claudeCode'), icon: TerminalIcon },
        { id: 'codex', label: t('keys.useKeyModal.cliTabs.codexCli'), icon: TerminalIcon },
        { id: 'opencode', label: t('keys.useKeyModal.cliTabs.opencode'), icon: TerminalIcon }
      ]
  }
})

// Shell tabs (3 types for environment variable based configs)
const shellTabs: TabConfig[] = [
  { id: 'unix', label: 'macOS / Linux', icon: AppleIcon },
  { id: 'cmd', label: 'Windows CMD', icon: WindowsIcon },
  { id: 'powershell', label: 'PowerShell', icon: WindowsIcon }
]

// OpenAI tabs (2 OS types)
const openaiTabs: TabConfig[] = [
  { id: 'unix', label: 'macOS / Linux', icon: AppleIcon },
  { id: 'windows', label: 'Windows', icon: WindowsIcon }
]

const showShellTabs = computed(() => activeClientTab.value !== 'opencode')

const showCodexAuthMode = computed(() =>
  props.platform === 'openai' &&
  (activeClientTab.value === 'codex' || activeClientTab.value === 'codex-ws')
)

const currentTabs = computed(() => {
  if (!showShellTabs.value) return []
  if (activeClientTab.value === 'codex' || activeClientTab.value === 'codex-ws' || activeClientTab.value === 'grok') {
    return openaiTabs
  }
  return shellTabs
})

const platformDescription = computed(() => {
  if (activeClientTab.value === 'codex' &&
    props.platform !== 'openai' &&
    props.platform !== 'grok' &&
    props.platform !== 'deepseek' &&
    props.platform !== 'composite') {
    return t('keys.useKeyModal.routedCodex.description')
  }
  switch (props.platform) {
    case 'openai':
      if (activeClientTab.value === 'claude') {
        return t('keys.useKeyModal.description')
      }
      return t('keys.useKeyModal.openai.description')
    case 'gemini':
      return t('keys.useKeyModal.gemini.description')
    case 'antigravity':
      return t('keys.useKeyModal.antigravity.description')
    case 'grok':
      if (activeClientTab.value === 'claude') {
        return t('keys.useKeyModal.grok.claudeDescription')
      }
      if (activeClientTab.value === 'codex') {
        return t('keys.useKeyModal.grok.codexDescription')
      }
      return t('keys.useKeyModal.grok.description')
    case 'deepseek':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.deepseek.codexDescription')
        : t('keys.useKeyModal.deepseek.description')
    case 'composite':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.composite.codexDescription')
        : t('keys.useKeyModal.composite.description')
    default:
      return t('keys.useKeyModal.description')
  }
})

const platformNote = computed(() => {
  if (activeClientTab.value === 'codex' &&
    props.platform !== 'openai' &&
    props.platform !== 'grok' &&
    props.platform !== 'deepseek' &&
    props.platform !== 'composite') {
    return t('keys.useKeyModal.routedCodex.note')
  }
  switch (props.platform) {
    case 'openai':
      if (activeClientTab.value === 'claude') {
        return t('keys.useKeyModal.note')
      }
      return activeTab.value === 'windows'
        ? t('keys.useKeyModal.openai.noteWindows')
        : t('keys.useKeyModal.openai.note')
    case 'gemini':
      return t('keys.useKeyModal.gemini.note')
    case 'antigravity':
      return activeClientTab.value === 'claude'
        ? t('keys.useKeyModal.antigravity.claudeNote')
        : t('keys.useKeyModal.antigravity.geminiNote')
    case 'grok':
      if (activeClientTab.value === 'claude') {
        return t('keys.useKeyModal.grok.claudeNote')
      }
      if (activeClientTab.value === 'codex') {
        return activeTab.value === 'windows'
          ? t('keys.useKeyModal.grok.codexNoteWindows')
          : t('keys.useKeyModal.grok.codexNote')
      }
      // Grok CLI: shell-specific path guidance (env + ~/.grok/config.toml).
      if (activeClientTab.value === 'grok' && (activeTab.value === 'cmd' || activeTab.value === 'powershell')) {
        return t('keys.useKeyModal.grok.noteWindows')
      }
      if (activeClientTab.value === 'grok' && activeTab.value === 'windows') {
        return t('keys.useKeyModal.grok.noteWindows')
      }
      return t('keys.useKeyModal.grok.note')
    case 'deepseek':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.deepseek.codexNote')
        : t('keys.useKeyModal.note')
    case 'composite':
      return activeClientTab.value === 'codex'
        ? t('keys.useKeyModal.composite.codexNote')
        : t('keys.useKeyModal.note')
    default:
      return t('keys.useKeyModal.note')
  }
})

const showPlatformNote = computed(() => activeClientTab.value !== 'opencode')

function resetCodexModelManifest() {
  codexModelManifestController?.abort()
  codexModelManifestController = null
  codexModelManifestRequestID += 1
  codexModelManifestState.value = 'idle'
  codexModelManifestContent.value = ''
  codexModelManifestModelCount.value = 0
}

async function loadCodexModelManifest() {
  if (!showCodexModelCatalog.value || !props.apiKey) return

  codexModelManifestController?.abort()
  const controller = new AbortController()
  const requestID = ++codexModelManifestRequestID
  codexModelManifestController = controller
  codexModelManifestState.value = 'loading'

  try {
    const result = await fetchCodexModelsManifest(props.baseUrl, props.apiKey, controller.signal)
    if (requestID !== codexModelManifestRequestID) return
    codexModelManifestContent.value = result.content
    codexModelManifestModelCount.value = result.modelCount
    codexModelManifestState.value = 'ready'
  } catch (error) {
    const errorName = error && typeof error === 'object' && 'name' in error
      ? String((error as { name?: unknown }).name || '')
      : ''
    if (requestID !== codexModelManifestRequestID || errorName === 'AbortError') return
    codexModelManifestState.value = 'error'
  } finally {
    if (requestID === codexModelManifestRequestID) {
      codexModelManifestController = null
    }
  }
}

function downloadCodexModelManifest() {
  if (!codexModelManifestContent.value) return
  saveAs(
    new Blob([codexModelManifestContent.value], { type: 'application/json;charset=utf-8' }),
    'codex-models.json'
  )
}

const codexCatalogModelSlugs = computed(() =>
  parseCodexCatalogModels(codexModelManifestContent.value).map((model) => model.slug)
)

/**
 * Codex `model = "..."` default: the first channel-effective model when the
 * downloaded catalog contains it, otherwise the catalog's first entry,
 * otherwise the first channel-effective model itself. Never a hardcoded id.
 */
function selectCodexModel(): string {
  const preferred = primaryModel()
  if (preferred && codexCatalogModelSlugs.value.includes(preferred)) return preferred
  return codexCatalogModelSlugs.value[0] || preferred
}

function codexReasoningEffortTomlLine(modelSlug: string): string {
  return formatCodexReasoningEffortTomlLine(
    selectCodexConfigReasoningEffort(findCodexCatalogModel(codexModelManifestContent.value, modelSlug))
  )
}

const escapeHtml = (value: string) => value
  .replace(/&/g, '&amp;')
  .replace(/</g, '&lt;')
  .replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;')
  .replace(/'/g, '&#39;')

const wrapToken = (className: string, value: string) =>
  `<span class="${className}">${escapeHtml(value)}</span>`

const keyword = (value: string) => wrapToken('text-gray-300', value)
const variable = (value: string) => wrapToken('text-sky-200', value)
const operator = (value: string) => wrapToken('text-slate-400', value)
const string = (value: string) => wrapToken('text-amber-200', value)
const comment = (value: string) => wrapToken('text-slate-500', value)

// Syntax highlighting helpers
// Generate file configs based on platform and active tab
const currentFiles = computed((): FileConfig[] => {
  const baseUrl = props.baseUrl || window.location.origin
  const apiKey = props.apiKey
  const baseRoot = baseUrl.replace(/\/v1\/?$/, '').replace(/\/+$/, '')
  const ensureV1 = (value: string) => {
    const trimmed = value.replace(/\/+$/, '')
    return trimmed.endsWith('/v1') ? trimmed : `${trimmed}/v1`
  }
  const apiBase = ensureV1(baseRoot)
  const antigravityBase = ensureV1(`${baseRoot}/antigravity`)
  const antigravityGeminiBase = (() => {
    const trimmed = `${baseRoot}/antigravity`.replace(/\/+$/, '')
    return trimmed.endsWith('/v1beta') ? trimmed : `${trimmed}/v1beta`
  })()
  const geminiBase = (() => {
    const trimmed = baseRoot.replace(/\/+$/, '')
    return trimmed.endsWith('/v1beta') ? trimmed : `${trimmed}/v1beta`
  })()

  if (activeClientTab.value === 'opencode') {
    switch (props.platform) {
      case 'anthropic':
        return [generateOpenCodeConfig('anthropic', apiBase, apiKey)]
      case 'openai':
        return [generateOpenCodeConfig('openai', apiBase, apiKey)]
      case 'gemini':
        return [generateOpenCodeConfig('gemini', geminiBase, apiKey)]
      case 'antigravity':
        return [
          generateOpenCodeConfig('antigravity-claude', antigravityBase, apiKey, 'opencode.json (Claude)'),
          generateOpenCodeConfig('antigravity-gemini', antigravityGeminiBase, apiKey, 'opencode.json (Gemini)')
        ]
      case 'grok':
        return [generateOpenCodeConfig('grok', apiBase, apiKey)]
      default:
        return [generateOpenCodeConfig('openai', apiBase, apiKey)]
    }
  }

  switch (props.platform) {
    case 'openai':
      if (activeClientTab.value === 'claude') {
        return generateAnthropicFiles(baseUrl, apiKey)
      }
      if (activeClientTab.value === 'codex-ws') {
        return generateOpenAIWsFiles(baseUrl, apiKey)
      }
      return generateOpenAIFiles(baseUrl, apiKey)
    case 'gemini':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'gemini')
      }
      return [generateGeminiCliContent(baseUrl, apiKey)]
    case 'antigravity':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'antigravity')
      }
      if (activeClientTab.value === 'gemini') {
        return [generateGeminiCliContent(`${baseUrl}/antigravity`, apiKey)]
      }
      return generateAnthropicFiles(`${baseUrl}/antigravity`, apiKey)
    case 'grok':
      if (activeClientTab.value === 'claude') {
        return generateGrokClaudeFiles(baseRoot, apiKey)
      }
      if (activeClientTab.value === 'codex') {
        return generateGrokCodexFiles(apiBase, apiKey)
      }
      return generateGrokFiles(apiBase, apiKey)
    case 'deepseek':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'deepseek')
      }
      return generateAnthropicFiles(baseRoot, apiKey)
    case 'composite':
      if (activeClientTab.value === 'codex') {
        return generateRoutedCodexFiles(apiBase, apiKey, 'composite')
      }
      return generateAnthropicFiles(baseRoot, apiKey)
    default:
      if (activeClientTab.value === 'codex' && props.platform) {
        return generateRoutedCodexFiles(apiBase, apiKey, props.platform)
      }
      return generateAnthropicFiles(baseUrl, apiKey)
  }
})

function generateAnthropicFiles(baseUrl: string, apiKey: string): FileConfig[] {
  // Channel-effective default model for Claude Code; omitted when the key
  // has no concrete models so clients keep their own default.
  const model = primaryModel()
  const environment: Record<string, string> = {
    ANTHROPIC_BASE_URL: baseUrl,
    ANTHROPIC_AUTH_TOKEN: apiKey,
    ...(model ? { ANTHROPIC_MODEL: model } : {}),
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1'
  }
  let path: string
  let content: string

  switch (activeTab.value) {
    case 'unix':
      path = 'Terminal'
      content = Object.entries(environment)
        .map(([name, value]) => `export ${name}="${escapeShDq(value)}"`)
        .join('\n')
      break
    case 'cmd':
      path = 'Command Prompt'
      content = Object.entries(environment)
        .map(([name, value]) => `set ${name}=${escapeCmdValue(value)}`)
        .join('\n')
      break
    case 'powershell':
      path = 'PowerShell'
      content = Object.entries(environment)
        .map(([name, value]) => `$env:${name}="${escapePsDq(value)}"`)
        .join('\n')
      break
    default:
      path = 'Terminal'
      content = ''
  }

  const vscodeSettingsPath = activeTab.value === 'unix'
    ? '~/.claude/settings.json'
    : '%USERPROFILE%\\.claude\\settings.json'

  // Serialized (not interpolated) so model ids can never break JSON syntax.
  const vscodeContent = JSON.stringify({
    $schema: 'https://json.schemastore.org/claude-code-settings.json',
    env: environment
  }, null, 2)

  return [
    { path, content },
    {
      path: vscodeSettingsPath,
      content: vscodeContent,
      hint: t('keys.useKeyModal.claudeSettingsHint')
    }
  ]
}

function generateGrokClaudeFiles(baseUrl: string, apiKey: string): FileConfig[] {
  const model = primaryModel()
  const environment: Record<string, string> = {
    ANTHROPIC_BASE_URL: baseUrl,
    ANTHROPIC_AUTH_TOKEN: apiKey,
    // Model-scoped vars only when the key owns a concrete model; an empty
    // default would override (and break) the client's own model selection.
    ...(model
      ? {
        ANTHROPIC_MODEL: model,
        ANTHROPIC_DEFAULT_OPUS_MODEL: model,
        ANTHROPIC_DEFAULT_SONNET_MODEL: model,
        ANTHROPIC_DEFAULT_HAIKU_MODEL: model,
        CLAUDE_CODE_SUBAGENT_MODEL: model
      }
      : {}),
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1'
  }
  let path: string
  let content: string

  switch (activeTab.value) {
    case 'unix':
      path = 'Terminal'
      content = Object.entries(environment)
        .map(([name, value]) => `export ${name}="${escapeShDq(value)}"`)
        .join('\n')
      break
    case 'cmd':
      path = 'Command Prompt'
      content = Object.entries(environment)
        .map(([name, value]) => `set ${name}=${escapeCmdValue(value)}`)
        .join('\n')
      break
    case 'powershell':
      path = 'PowerShell'
      content = Object.entries(environment)
        .map(([name, value]) => `$env:${name}="${escapePsDq(value)}"`)
        .join('\n')
      break
    default:
      path = 'Terminal'
      content = ''
  }

  const settingsPath = activeTab.value === 'unix'
    ? '~/.claude/settings.json'
    : '%USERPROFILE%\\.claude\\settings.json'

  return [
    { path, content },
    {
      path: settingsPath,
      content: JSON.stringify({
        $schema: 'https://json.schemastore.org/claude-code-settings.json',
        env: environment
      }, null, 2),
      hint: t('keys.useKeyModal.claudeSettingsHint')
    }
  ]
}

function generateGeminiCliContent(baseUrl: string, apiKey: string): FileConfig {
  // Real model default from the key's channel-effective list (auto-injected first entry).
  const model = primaryModel()
  const modelComment = t('keys.useKeyModal.gemini.modelComment')
  const shBase = escapeShDq(baseUrl)
  const shKey = escapeShDq(apiKey)
  const shModel = escapeShDq(model)
  const modelLine = (line: string) => (model ? `${line}` : null)
  let path: string
  let content: string
  let highlighted: string

  switch (activeTab.value) {
    case 'unix':
      path = 'Terminal'
      content = [
        `export GOOGLE_GEMINI_BASE_URL="${shBase}"`,
        `export GEMINI_API_KEY="${shKey}"`,
        modelLine(`export GEMINI_MODEL="${shModel}"  # ${modelComment}`)
      ].filter((line): line is string => line !== null).join('\n')
      highlighted = `${keyword('export')} ${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(`"${shBase}"`)}
${keyword('export')} ${variable('GEMINI_API_KEY')}${operator('=')}${string(`"${shKey}"`)}${model ? `
${keyword('export')} ${variable('GEMINI_MODEL')}${operator('=')}${string(`"${shModel}"`)}  ${comment(`# ${modelComment}`)}` : ''}`
      break
    case 'cmd': {
      path = 'Command Prompt'
      const cmdBase = escapeCmdValue(baseUrl)
      const cmdKey = escapeCmdValue(apiKey)
      const cmdModel = escapeCmdValue(model)
      const cmdLines = [
        `set GOOGLE_GEMINI_BASE_URL=${cmdBase}`,
        `set GEMINI_API_KEY=${cmdKey}`,
        ...(model ? [`set GEMINI_MODEL=${cmdModel}`] : [])
      ]
      content = cmdLines.join('\n')
      highlighted = `${keyword('set')} ${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(cmdBase)}
${keyword('set')} ${variable('GEMINI_API_KEY')}${operator('=')}${string(cmdKey)}${model ? `
${keyword('set')} ${variable('GEMINI_MODEL')}${operator('=')}${string(cmdModel)}
${comment(`REM ${modelComment}`)}` : ''}`
      break
    }
    case 'powershell': {
      path = 'PowerShell'
      const psBase = escapePsDq(baseUrl)
      const psKey = escapePsDq(apiKey)
      const psModel = escapePsDq(model)
      const psLines = [
        `$env:GOOGLE_GEMINI_BASE_URL="${psBase}"`,
        `$env:GEMINI_API_KEY="${psKey}"`,
        ...(model ? [`$env:GEMINI_MODEL="${psModel}"  # ${modelComment}`] : [])
      ]
      content = psLines.join('\n')
      highlighted = `${keyword('$env:')}${variable('GOOGLE_GEMINI_BASE_URL')}${operator('=')}${string(`"${psBase}"`)}
${keyword('$env:')}${variable('GEMINI_API_KEY')}${operator('=')}${string(`"${psKey}"`)}${model ? `
${keyword('$env:')}${variable('GEMINI_MODEL')}${operator('=')}${string(`"${psModel}"`)}  ${comment(`# ${modelComment}`)}` : ''}`
      break
    }
    default:
      path = 'Terminal'
      content = ''
      highlighted = ''
  }

  return { path, content, highlighted }
}

function generateOpenAIFiles(baseUrl: string, apiKey: string): FileConfig[] {
  const isWindows = activeTab.value === 'windows'
  const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'

  const model = selectCodexModel()
  const reasoningEffortLine = codexReasoningEffortTomlLine(model)
  const tomlModel = escapeTomlBasicString(model)
  const tomlBase = escapeTomlBasicString(baseUrl)

  // config.toml content (all dynamic values TOML-escaped)
  const configContent = `model_provider = "OpenAI"
model = "${tomlModel}"
review_model = "${tomlModel}"
${reasoningEffortLine}disable_response_storage = true
model_catalog_json = "${escapeTomlBasicString(codexModelCatalogPath.value)}"
network_access = "enabled"
windows_wsl_setup_acknowledged = true

[model_providers.OpenAI]
name = "OpenAI"
base_url = "${tomlBase}"
wire_api = "responses"
${generateCodexProviderAuthConfig(apiKey)}

[features]
goals = true`

  return buildOpenAICodexFileConfigs(configDir, configContent, apiKey)
}

function generateCodexProviderAuthConfig(apiKey: string): string {
  if (codexAuthMode.value === 'api-key') {
    return `requires_openai_auth = false
experimental_bearer_token = "${escapeTomlBasicString(apiKey)}"
http_headers = { "x-openai-actor-authorization" = "local-image-extension" }`
  }

  return 'requires_openai_auth = true'
}

function buildOpenAICodexFileConfigs(
  configDir: string,
  configContent: string,
  apiKey: string
): FileConfig[] {
  const files: FileConfig[] = [
    {
      path: `${configDir}/config.toml`,
      content: configContent,
      hint: t('keys.useKeyModal.openai.configTomlHint')
    }
  ]

  if (codexAuthMode.value === 'legacy') {
    files.push({
      path: `${configDir}/auth.json`,
      content: JSON.stringify({ OPENAI_API_KEY: apiKey }, null, 2)
    })
  }

  return files
}

function joinConfigPath(dir: string, file: string, windows: boolean): string {
  if (!windows) return `${dir}/${file}`
  return `${dir}\\${file}`
}

/**
 * Grok Build `[model.*]` entries generated from the key's channel-effective
 * model list. Every entry keeps `api_backend = "responses"` (Sub2API Grok
 * serves POST /v1/responses); image/video Imagine ids stay on media endpoints.
 */
function buildGrokModelEntries(models: string[]): string {
  // Media-only ids (Imagine / image / video) must not be configured as
  // `responses` text models; they are served by media endpoints instead.
  const textModels = models.filter((id) => !isMediaModelId(id))
  if (!textModels.length) return '# No channel-effective text models found for this key.'
  return textModels
    .map((id) => {
      const safe = escapeTomlBasicString(id)
      return `[model."${safe}"]\nmodel = "${safe}"                          # id sent to the API\nenv_key = "XAI_API_KEY"\napi_backend = "responses"                   # chat_completions | responses | messages\nsupports_backend_search = true`
    })
    .join('\n\n')
}

function generateGrokFiles(baseUrl: string, apiKey: string): FileConfig[] {
  // Prefer unix/cmd/powershell when shell tabs are shown; fall back to windows tab.
  const shell = activeTab.value
  const isWindowsPath = shell === 'windows' || shell === 'cmd' || shell === 'powershell'
  const configDir = isWindowsPath ? '%userprofile%\\.grok' : '~/.grok'

  let envPath: string
  let envContent: string
  switch (shell) {
    case 'cmd':
      envPath = 'Command Prompt'
      envContent = `set GROK_MODELS_BASE_URL=${escapeCmdValue(baseUrl)}
set XAI_API_KEY=${escapeCmdValue(apiKey)}`
      break
    case 'powershell':
    case 'windows':
      envPath = 'PowerShell'
      envContent = `$env:GROK_MODELS_BASE_URL="${escapePsDq(baseUrl)}"
$env:XAI_API_KEY="${escapePsDq(apiKey)}"`
      break
    default:
      envPath = 'Terminal'
      envContent = `export GROK_MODELS_BASE_URL="${escapeShDq(baseUrl)}"
export XAI_API_KEY="${escapeShDq(apiKey)}"`
  }

  // Shape follows Grok Build user guide (~/.grok/docs + custom-models) and production-ready Sub2API setups.
  // Model entries below are generated from the key's channel-effective list
  // (auto-injected) — never hardcoded ids.
  // Credential order: api_key field → env_key → signed-in session → XAI_API_KEY global fallback.
  const modelsListUrl = `${baseUrl.replace(/\/+$/, '')}/models`
  const grokDefaultModel = escapeTomlBasicString(primaryModel())
  // A media-only shelf has no text default: emit an explanatory comment
  // instead of a `default` pointing at a model with no `[model.*]` entry.
  const grokModelsSection = grokDefaultModel
    ? `[models]
default = "${grokDefaultModel}"
web_search = "${grokDefaultModel}"               # client-side web_search tool model (must exist as [model.*])
image_description = "${grokDefaultModel}"        # vision/describe-image helper model`
    : `[models]
# No channel-effective text models for this key — Grok has no usable default.
# Add a text model to the channel allow-list and regenerate this config.`
  const tomlGrokBase = escapeTomlBasicString(baseUrl)
  const tomlModelsListUrl = escapeTomlBasicString(modelsListUrl)
  const grokModelEntries = buildGrokModelEntries(effectiveModels.value)
  const configContent = `# Grok Build CLI → Sub2API Grok group (API key auth).
# Docs: ~/.grok/docs/user-guide/05-configuration.md + 11-custom-models.md
# Verify after save: grok inspect
#
# IMPORTANT: api_backend must be "responses" for Sub2API Grok (POST /v1/responses).
# If omitted, Grok Build defaults to chat_completions (/v1/chat/completions).
# Keep api_backend = "responses" on every model entry.
#
# Prefer env_key over hardcoding api_key (never commit secrets).
# Also export GROK_MODELS_BASE_URL + XAI_API_KEY in the shell block above.

# Global inference / catalog endpoints (same role as env GROK_MODELS_BASE_URL).
# When models_base_url is set, Grok uses API-key Bearer auth (no grok login required).
[endpoints]
models_base_url = "${tomlGrokBase}"              # inference base; model list defaults to {base}/models
models_list_url = "${tomlModelsListUrl}"        # optional override (env: GROK_MODELS_LIST_URL)
xai_api_base_url = "${tomlGrokBase}"             # public xAI API base override for gateway routing
cli_chat_proxy_base_url = "${tomlGrokBase}"      # CLI chat-proxy base (env: GROK_CLI_CHAT_PROXY_BASE_URL)

# Prefer API key when using a custom gateway (matches Sub2API).
# Requires XAI_API_KEY env or per-model env_key / api_key.
[auth]
preferred_method = "api_key"

${grokModelEntries}

${grokModelsSection}
# Optional environment-wide sampling defaults (per-model values win):
# temperature = 0.7
# top_p = 0.95
# max_completion_tokens = 8192
# max_retries = 8

[session]
auto_compact_threshold_percent = 80         # auto-compact at this % of context_window (default 85)

# Imagine tools: model IDs go to Sub2API media endpoints (not the text [model.*] catalog).
# Uncomment and fill with a media model your channel actually allows.
# [features]
# image_gen = true
# video_gen = true
# image_gen_model_override = "<channel-allowed-image-model>"
# image_edit_model_override = "<channel-allowed-image-edit-model>"
# Optional feature flags (defaults shown in docs):
# telemetry = false
# remote_fetch = true                         # set false for air-gapped / pure-gateway catalogs
# lsp_tools = false`

  return [
    { path: envPath, content: envContent },
    {
      path: joinConfigPath(configDir, 'config.toml', isWindowsPath),
      content: configContent,
      hint: t('keys.useKeyModal.grok.configTomlHint')
    }
  ]
}

function generateGrokCodexFiles(baseUrl: string, apiKey: string): FileConfig[] {
  // Codex config reference: wire_api = "responses" only; prefer env_key over experimental_bearer_token.
  // Non-OpenAI gateways should set supports_websockets = false (HTTP/SSE).
  const shell = activeTab.value
  const isWindowsPath = shell === 'windows' || shell === 'cmd' || shell === 'powershell'
  const configDir = isWindowsPath ? '%userprofile%\\.codex' : '~/.codex'
  const model = selectCodexModel()
  const tomlModel = escapeTomlBasicString(model)
  const tomlBase = escapeTomlBasicString(baseUrl)

  let envPath: string
  let envContent: string
  switch (shell) {
    case 'cmd':
      envPath = 'Command Prompt'
      envContent = `set SUB2API_API_KEY=${escapeCmdValue(apiKey)}`
      break
    case 'powershell':
    case 'windows':
      envPath = 'PowerShell'
      envContent = `$env:SUB2API_API_KEY="${escapePsDq(apiKey)}"`
      break
    default:
      envPath = 'Terminal'
      envContent = `export SUB2API_API_KEY="${escapeShDq(apiKey)}"`
  }

  const configContent = `# Codex CLI → Sub2API Grok group
# Docs: Codex config reference (model_providers.*, wire_api = "responses")
#
# Text models only. Image/video: Imagine model ids on media endpoints.
# Default model below is the first channel-effective model.

model_provider = "sub2api"
model = "${tomlModel}"
model_catalog_json = "${escapeTomlBasicString(codexModelCatalogPath.value)}"
# Optional:
# review_model = "${tomlModel}"
# model_reasoning_effort = "medium"
# model_context_window = 500000
# disable_response_storage = true
# network_access = "enabled"
# windows_wsl_setup_acknowledged = true

[model_providers.sub2api]
name = "Sub2API Grok"
base_url = "${tomlBase}"
# Prefer env_key (variable NAME). Do not combine with experimental_bearer_token.
env_key = "SUB2API_API_KEY"
# Fallback only if you cannot set env (discouraged — keeps secret on disk):
# experimental_bearer_token = "<paste-key-only-if-you-cannot-set-env>"
wire_api = "responses"
# API-key providers: do not require ChatGPT OAuth login
requires_openai_auth = false
# Grok/Sub2API path is HTTP/SSE; disable WS (Codex may otherwise try WebSocket first)
supports_websockets = false

# Optional:
# [features]
# goals = true`

  return [
    { path: envPath, content: envContent },
    {
      path: joinConfigPath(configDir, 'config.toml', isWindowsPath),
      content: configContent,
      hint: t('keys.useKeyModal.grok.codexConfigTomlHint')
    }
  ]
}

function generateRoutedCodexFiles(
  baseUrl: string,
  apiKey: string,
  platform: GroupPlatform
): FileConfig[] {
  const isWindows = activeTab.value === 'windows'
  const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
  const model = selectCodexModel()
  const labels: Record<GroupPlatform, string> = {
    anthropic: 'Anthropic',
    openai: 'OpenAI',
    gemini: 'Gemini',
    antigravity: 'Antigravity',
    kiro: 'Kiro',
    grok: 'Grok',
    kimi: 'Kimi',
    zhipu: 'Zhipu',
    deepseek: 'DeepSeek',
    composite: 'Composite'
  }
  const label = labels[platform]
  const tomlModel = escapeTomlBasicString(model)
  const tomlBase = escapeTomlBasicString(baseUrl)
  const envContent = isWindows
    ? `$env:SUB2API_API_KEY="${escapePsDq(apiKey)}"`
    : `export SUB2API_API_KEY="${escapeShDq(apiKey)}"`

  const configContent = `# Codex CLI -> Sub2API ${label} group
model_provider = "sub2api"
model = "${tomlModel}"
review_model = "${tomlModel}"
disable_response_storage = true
model_catalog_json = "${escapeTomlBasicString(codexModelCatalogPath.value)}"

[model_providers.sub2api]
name = "Sub2API ${label}"
base_url = "${tomlBase}"
env_key = "SUB2API_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false`

  return [
    { path: isWindows ? 'PowerShell' : 'Terminal', content: envContent },
    {
      path: joinConfigPath(configDir, 'config.toml', isWindows),
      content: configContent,
      hint: t(
        platform === 'deepseek' || platform === 'composite'
          ? `keys.useKeyModal.${platform}.codexConfigTomlHint`
          : 'keys.useKeyModal.routedCodex.configTomlHint'
      )
    }
  ]
}

function generateOpenAIWsFiles(baseUrl: string, apiKey: string): FileConfig[] {
  const isWindows = activeTab.value === 'windows'
  const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'
  const model = selectCodexModel()
  const reasoningEffortLine = codexReasoningEffortTomlLine(model)
  const tomlModel = escapeTomlBasicString(model)
  const tomlBase = escapeTomlBasicString(baseUrl)

  // config.toml content with WebSocket v2
  const configContent = `model_provider = "OpenAI"
model = "${tomlModel}"
review_model = "${tomlModel}"
${reasoningEffortLine}disable_response_storage = true
model_catalog_json = "${escapeTomlBasicString(codexModelCatalogPath.value)}"
network_access = "enabled"
windows_wsl_setup_acknowledged = true

[model_providers.OpenAI]
name = "OpenAI"
base_url = "${tomlBase}"
wire_api = "responses"
supports_websockets = true
${generateCodexProviderAuthConfig(apiKey)}

[features]
responses_websockets_v2 = true
goals = true`

  return buildOpenAICodexFileConfigs(configDir, configContent, apiKey)
}

function generateOpenCodeConfig(platform: string, baseUrl: string, apiKey: string, pathLabel?: string): FileConfig {
  const provider: Record<string, any> = {
    [platform]: {
      options: {
        baseURL: baseUrl,
        apiKey
      }
    }
  }
  // OpenCode `models` map is generated from the key's channel-effective list
  // (auto-injected). No hardcoded ids, limits, or variants are invented here.
  const dynamicModels = buildOpenCodeModels()

/**
 * Minimal OpenCode model entries derived from real channel-effective ids.
 * `limit` is filled only from trusted gateway metadata (the repo-owned model
 * data doc): OpenCode assigns context=0 to models absent from its built-in
 * catalog, which disables auto-compaction and lets long sessions accumulate
 * until upstream rejects. Variants stay omitted — inventing capabilities
 * would mislead clients into requesting what the channel never granted.
 */
function buildOpenCodeModels(): Record<string, { name: string; limit?: { context: number; output?: number } }> {
  return Object.fromEntries(
    effectiveModels.value.map((id) => {
      const entry: { name: string; limit?: { context: number; output?: number } } = { name: id }
      const trusted = gatewayModelLimits.value[id]
      if (trusted?.contextWindow) {
        entry.limit = {
          context: trusted.contextWindow,
          ...(trusted.maxOutputTokens ? { output: trusted.maxOutputTokens } : {})
        }
      }
      return [id, entry]
    })
  )
}
  if (platform === 'gemini') {
    provider[platform].npm = '@ai-sdk/google'
    provider[platform].models = dynamicModels
  } else if (platform === 'anthropic') {
    provider[platform].npm = '@ai-sdk/anthropic'
    provider[platform].models = dynamicModels
  } else if (platform === 'antigravity-claude') {
    provider[platform].npm = '@ai-sdk/anthropic'
    provider[platform].name = 'Antigravity (Claude)'
    provider[platform].models = dynamicModels
  } else if (platform === 'antigravity-gemini') {
    provider[platform].npm = '@ai-sdk/google'
    provider[platform].name = 'Antigravity (Gemini)'
    provider[platform].models = dynamicModels
  } else if (platform === 'openai') {
    provider[platform].models = dynamicModels
  } else if (platform === 'grok') {
    // Custom provider pointing at Sub2API OpenAI-compatible Responses/Chat endpoints.
    provider[platform].npm = '@ai-sdk/openai-compatible'
    provider[platform].name = 'Grok via Sub2API'
    provider[platform].models = dynamicModels
  } else {
    provider[platform].models = dynamicModels
  }

  const agent =
    platform === 'openai'
      ? {
          build: {
            options: {
              store: false
            }
          },
          plan: {
            options: {
              store: false
            }
          }
        }
      : undefined

  const content = JSON.stringify(
    {
      provider,
      ...(agent ? { agent } : {}),
      $schema: 'https://opencode.ai/config.json'
    },
    null,
    2
  )

  return {
    path: pathLabel ?? 'opencode.json',
    content,
    hint: t('keys.useKeyModal.opencode.hint')
  }
}

const copyContent = async (content: string, index: number) => {
  const success = await clipboardCopy(content, t('keys.copied'))
  if (success) {
    copiedIndex.value = index
    setTimeout(() => {
      copiedIndex.value = null
    }, 2000)
  }
}
</script>
