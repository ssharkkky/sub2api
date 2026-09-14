// =====================
// 模型列表（硬编码，与 new-api 一致）
// =====================

// OpenAI
const openaiModels = [
  // GPT-5.2 系列
  'gpt-5.2', 'gpt-5.2-2025-12-11', 'gpt-5.2-chat-latest',
  'gpt-5.2-pro', 'gpt-5.2-pro-2025-12-11',
	// GPT-5.6 系列
  'gpt-5.6', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna',
  // GPT-6 系列
  'gpt-6', 'gpt-6-astra',
  // GPT-5.5 系列
  'gpt-5.5',
  // GPT-5.4 系列
  'gpt-5.4', 'gpt-5.4-mini', 'gpt-5.4-2026-03-05',
  // GPT-5.3 / Codex 系列
  'gpt-5.3-codex-spark', 'codex-auto-review',
  'gpt-4o-audio-preview', 'gpt-4o-realtime-preview',
  // GPT Image 系列
  'gpt-image-1', 'gpt-image-1.5', 'gpt-image-2'
]

// Anthropic Claude
export const claudeModels = [
  'claude-3-5-sonnet-20241022', 'claude-3-5-sonnet-20240620',
  'claude-3-5-haiku-20241022',
  'claude-3-7-sonnet-20250219',
  'claude-sonnet-4-20250514', 'claude-opus-4-20250514',
  'claude-opus-4-1-20250805',
  'claude-sonnet-4-5-20250929', 'claude-haiku-4-5-20251001',
  'claude-opus-4-5-20251101',
  'claude-opus-4-6',
  'claude-opus-4-7',
  'claude-opus-4-8',
  'claude-opus-5',
  'claude-sonnet-4-6',
  'claude-sonnet-5',
  'claude-fable-5-1',
  'claude-fable-5'
]

// Google Gemini
const geminiModels = [
  // Keep in sync with backend curated Gemini lists.
  // This list is intentionally conservative (models commonly available across OAuth/API key).
  'gemini-3.1-flash-image',
  'gemini-2.5-flash-image',
  'gemini-2.0-flash',
  'gemini-2.5-flash',
  'gemini-2.5-pro',
  'gemini-3.5-flash',
  'gemini-3-flash-preview',
  'gemini-3-pro-preview'
]

// Antigravity 官方支持的模型（精确匹配）
// 基于官方 API 返回的模型列表，只支持 Claude 4.5+ 和 Gemini 2.5+
const antigravityModels = [
  // Claude 4.5+ 系列
  'claude-fable-5-1',
  'claude-fable-5',
  'claude-opus-4-6',
  'claude-opus-4-6-thinking',
  'claude-opus-4-7',
  'claude-opus-4-8',
  'claude-opus-4-5-thinking',
  'claude-sonnet-4-6',
  'claude-sonnet-4-5',
  'claude-sonnet-4-5-thinking',
  // Gemini 2.5 系列
  'gemini-3.1-flash-image',
  'gemini-2.5-flash-image',
  'gemini-2.5-flash',
  'gemini-2.5-flash-lite',
  'gemini-2.5-flash-thinking',
  'gemini-2.5-pro',
  // Gemini 3 系列
  'gemini-3-flash',
  'gemini-3-pro-high',
  'gemini-3-pro-low',
  // Gemini 3.1 系列
  'gemini-3.1-pro',
  'gemini-3.1-pro-high',
  'gemini-3.1-pro-low',
  'gemini-3-pro-image',
  // 其他
  'gpt-oss-120b-medium',
  'tab_flash_lite_preview'
]

const kiroModels = [
  'gpt-5.6-sol',
  'gpt-5.6-terra',
  'gpt-5.6-luna',
  'codex-auto-review',
  'claude-opus-4-8',
  'claude-opus-4-8-thinking',
  'claude-opus-4-7',
  'claude-opus-4-7-thinking',
  'claude-opus-4-6',
  'claude-opus-4-6-thinking',
  'claude-opus-5',
  'claude-opus-5-thinking',
  'claude-sonnet-5',
  'claude-sonnet-5-thinking',
  'claude-sonnet-4-6',
  'claude-sonnet-4-6-thinking',
  'claude-opus-4-5-20251101',
  'claude-opus-4-5-20251101-thinking',
  'claude-sonnet-4-5-20250929',
  'claude-sonnet-4-5-20250929-thinking',
  'claude-haiku-4-5-20251001',
  'claude-haiku-4-5-20251001-thinking'
]

// 智谱 GLM
const zhipuModels = [
  'glm-4', 'glm-4v', 'glm-4-plus', 'glm-4-0520',
  'glm-4-air', 'glm-4-airx', 'glm-4-long', 'glm-4-flash',
  'glm-4v-plus', 'glm-4.5', 'glm-4.5-x', 'glm-4.5-air', 'glm-4.5-flash',
  'glm-4.6', 'glm-4.7', 'glm-4.7-flash', 'glm-4.7-flashx',
  'glm-5', 'glm-5-turbo', 'glm-5.1', 'glm-5.2',
  'glm-5.3', 'glm-5.3-flash',
  'glm-3-turbo', 'glm-4-alltools',
  'chatglm_turbo', 'chatglm_pro', 'chatglm_std', 'chatglm_lite',
  'cogview-3', 'cogvideo'
]

// 阿里 通义千问
const qwenModels = [
  'qwen-turbo', 'qwen-plus', 'qwen-max', 'qwen-max-longcontext', 'qwen-long',
  'qwen2-72b-instruct', 'qwen2-57b-a14b-instruct', 'qwen2-7b-instruct',
  'qwen2.5-72b-instruct', 'qwen2.5-32b-instruct', 'qwen2.5-14b-instruct',
  'qwen2.5-7b-instruct', 'qwen2.5-3b-instruct', 'qwen2.5-1.5b-instruct',
  'qwen2.5-coder-32b-instruct', 'qwen2.5-coder-14b-instruct', 'qwen2.5-coder-7b-instruct',
  'qwen3-235b-a22b',
  'qwq-32b', 'qwq-32b-preview'
]

// DeepSeek
const deepseekModels = [
  'deepseek-v4-pro', 'deepseek-v4-flash', 'deepseek-v4-flash-vision-exp',
  'deepseek-coder',
  'deepseek-v3', 'deepseek-v3-0324',
  'deepseek-r1', 'deepseek-r1-0528',
  'deepseek-r1-distill-qwen-32b', 'deepseek-r1-distill-qwen-14b', 'deepseek-r1-distill-qwen-7b',
  'deepseek-r1-distill-llama-70b', 'deepseek-r1-distill-llama-8b'
]

// Mistral
const mistralModels = [
  'mistral-small-latest', 'mistral-medium-latest', 'mistral-large-latest',
  'open-mistral-7b', 'open-mixtral-8x7b', 'open-mixtral-8x22b',
  'codestral-latest', 'codestral-mamba',
  'pixtral-12b-2409', 'pixtral-large-latest'
]

// Meta Llama
const metaModels = [
  'llama-3.3-70b-instruct',
  'llama-3.2-90b-vision-instruct', 'llama-3.2-11b-vision-instruct',
  'llama-3.2-3b-instruct', 'llama-3.2-1b-instruct',
  'llama-3.1-405b-instruct', 'llama-3.1-70b-instruct', 'llama-3.1-8b-instruct',
  'llama-3-70b-instruct', 'llama-3-8b-instruct',
  'codellama-70b-instruct', 'codellama-34b-instruct', 'codellama-13b-instruct'
]

// xAI Grok
const xaiModels = [
  'grok-4.6',
  'grok-4.5',
  'grok-4.3',
  'grok-build-0.1',
  'grok-composer-2.5-fast',
  'grok-4.20-0309-reasoning',
  'grok-4.20-0309-non-reasoning',
  'grok-4.20-multi-agent-0309',
  'grok-4.20-multi-agent',
  'grok-4.20-multi-agent-latest',
  'grok-4.3-latest',
  'grok-latest',
  'grok-4.6-latest',
  'grok-4.5-latest',
  'grok-build-latest',
  'composer-2.5',
  'grok-4.20-reasoning',
  'grok-4.20-non-reasoning',
  'grok-imagine',
  'grok-imagine-image-quality',
  'grok-imagine-image',
  'grok-imagine-image-2.0',
  'grok-imagine-video',
  'grok-imagine-video-1.5'
]

// Cohere
const cohereModels = [
  'command-a-03-2025',
  'command-r', 'command-r-plus',
  'command-r-08-2024', 'command-r-plus-08-2024',
  'c4ai-aya-23-35b', 'c4ai-aya-23-8b',
  'command', 'command-light'
]

// Yi (01.AI)
const yiModels = [
  'yi-large', 'yi-large-turbo', 'yi-large-rag',
  'yi-medium', 'yi-medium-200k',
  'yi-spark', 'yi-vision',
  'yi-1.5-34b-chat', 'yi-1.5-9b-chat', 'yi-1.5-6b-chat'
]

// Moonshot/Kimi
const moonshotModels = [
  'moonshot-v1-8k', 'moonshot-v1-32k', 'moonshot-v1-128k',
  'kimi-latest',
  'kimi-for-coding',
  'kimi-k2'
]

// 字节跳动 豆包
const doubaoModels = [
  'doubao-pro-256k', 'doubao-pro-128k', 'doubao-pro-32k', 'doubao-pro-4k',
  'doubao-lite-128k', 'doubao-lite-32k', 'doubao-lite-4k',
  'doubao-vision-pro-32k', 'doubao-vision-lite-32k',
  'doubao-1.5-pro-256k', 'doubao-1.5-pro-32k', 'doubao-1.5-lite-32k',
  'doubao-1.5-pro-vision-32k', 'doubao-1.5-thinking-pro'
]

// MiniMax
const minimaxModels = [
  'abab6.5-chat', 'abab6.5s-chat', 'abab6.5s-chat-pro',
  'abab6-chat',
  'abab5.5-chat', 'abab5.5s-chat'
]

// 百度 文心
const baiduModels = [
  'ernie-4.0-8k-latest', 'ernie-4.0-8k', 'ernie-4.0-turbo-8k',
  'ernie-3.5-8k', 'ernie-3.5-128k',
  'ernie-speed-8k', 'ernie-speed-128k', 'ernie-speed-pro-128k',
  'ernie-lite-8k', 'ernie-lite-pro-128k',
  'ernie-tiny-8k'
]

// 讯飞 星火
const sparkModels = [
  'spark-desk', 'spark-desk-v1.1', 'spark-desk-v2.1',
  'spark-desk-v3.1', 'spark-desk-v3.5', 'spark-desk-v4.0',
  'spark-lite', 'spark-pro', 'spark-max', 'spark-ultra'
]

// 腾讯 混元
const hunyuanModels = [
  'hunyuan-lite', 'hunyuan-standard', 'hunyuan-standard-256k',
  'hunyuan-pro', 'hunyuan-turbo', 'hunyuan-large',
  'hunyuan-vision', 'hunyuan-code'
]

// Perplexity
const perplexityModels = [
  'sonar', 'sonar-pro', 'sonar-reasoning',
  'llama-3-sonar-small-32k-online', 'llama-3-sonar-large-32k-online',
  'llama-3-sonar-small-32k-chat', 'llama-3-sonar-large-32k-chat'
]

// 所有模型（去重）
const allModelsList: string[] = [
  ...openaiModels,
  ...claudeModels,
  ...geminiModels,
  ...zhipuModels,
  ...qwenModels,
  ...deepseekModels,
  ...mistralModels,
  ...metaModels,
  ...xaiModels,
  ...cohereModels,
  ...yiModels,
  ...moonshotModels,
  ...doubaoModels,
  ...minimaxModels,
  ...baiduModels,
  ...sparkModels,
  ...hunyuanModels,
  ...perplexityModels
]

// 转换为下拉选项格式
export const allModels = allModelsList.map(m => ({ value: m, label: m }))

// =====================
// 常用错误码
// =====================

export const commonErrorCodes = [
  { value: 401, label: 'Unauthorized' },
  { value: 403, label: 'Forbidden' },
  { value: 429, label: 'Rate Limit' },
  { value: 500, label: 'Server Error' },
  { value: 502, label: 'Bad Gateway' },
  { value: 503, label: 'Unavailable' },
  { value: 529, label: 'Overloaded' }
]

// =====================
// 辅助函数
// =====================

// 按平台获取模型
export function getModelsByPlatform(platform: string): string[] {
  switch (platform) {
    case 'openai': return openaiModels
    case 'anthropic':
    case 'claude': return claudeModels
    case 'gemini': return geminiModels
    case 'antigravity': return antigravityModels
    case 'kiro': return kiroModels
    case 'zhipu': return zhipuModels
    case 'qwen': return qwenModels
    case 'deepseek': return deepseekModels
    case 'mistral': return mistralModels
    case 'meta': return metaModels
    case 'xai':
    case 'grok': return xaiModels
    case 'cohere': return cohereModels
    case 'yi': return yiModels
    case 'moonshot':
    case 'kimi': return moonshotModels
    case 'doubao': return doubaoModels
    case 'minimax': return minimaxModels
    case 'baidu': return baiduModels
    case 'spark': return sparkModels
    case 'hunyuan': return hunyuanModels
    case 'perplexity': return perplexityModels
    default: return claudeModels
  }
}

// =====================
// 构建模型映射对象（用于 API）
// =====================

// isValidWildcardPattern 校验通配符格式：* 只能放在末尾
// 导出供表单组件使用实时校验
export function isValidWildcardPattern(pattern: string): boolean {
  const starIndex = pattern.indexOf('*')
  if (starIndex === -1) return true // 无通配符，有效
  // * 必须在末尾，且只能有一个
  return starIndex === pattern.length - 1 && pattern.lastIndexOf('*') === starIndex
}

export type ModelRestrictionMode = 'whitelist' | 'mapping' | 'combined'

export interface ModelMappingEntry {
  from: string
  to: string
}

export function splitModelMappingObject(
  modelMapping?: Record<string, unknown> | null
): { allowedModels: string[]; modelMappings: ModelMappingEntry[] } {
  const allowedModels: string[] = []
  const modelMappings: ModelMappingEntry[] = []

  if (!modelMapping || typeof modelMapping !== 'object') {
    return { allowedModels, modelMappings }
  }

  for (const [rawFrom, rawTo] of Object.entries(modelMapping)) {
    if (typeof rawTo !== 'string') continue
    const from = rawFrom.trim()
    const to = rawTo.trim()
    if (!from || !to) continue

    if (from === to) {
      allowedModels.push(from)
    } else {
      modelMappings.push({ from, to })
    }
  }

  return { allowedModels, modelMappings }
}

export function buildModelMappingObject(
  mode: ModelRestrictionMode,
  allowedModels: string[],
  modelMappings: ModelMappingEntry[]
): Record<string, string> | null {
  const mapping: Record<string, string> = {}

  if (mode === 'whitelist' || mode === 'combined') {
    for (const model of allowedModels) {
      const normalizedModel = model.trim()
      if (!normalizedModel) continue
      // whitelist 模式的本意是"精确模型列表"，如果用户输入了通配符（如 claude-*），
      // 写入 model_mapping 会导致 GetMappedModel() 把真实模型映射成 "claude-*"，从而转发失败。
      // 因此这里跳过包含通配符的条目。
      if (!normalizedModel.includes('*')) {
        mapping[normalizedModel] = normalizedModel
      }
    }
  }

  if (mode === 'mapping' || mode === 'combined') {
    for (const m of modelMappings) {
      const from = m.from.trim()
      const to = m.to.trim()
      if (!from || !to) continue
      // 校验通配符格式：* 只能放在末尾
      if (!isValidWildcardPattern(from)) {
        console.warn(`[buildModelMappingObject] Invalid wildcard pattern, skipped: ${from}`)
        continue
      }
      // to 不允许包含通配符
      if (to.includes('*')) {
        console.warn(`[buildModelMappingObject] Target model cannot contain a wildcard, skipped: ${from} -> ${to}`)
        continue
      }
      mapping[from] = to
    }
  }

  return Object.keys(mapping).length > 0 ? mapping : null
}
