import { normalizeModelIds } from '@/utils/keyConfigEscape'

/**
 * Gateway `GET /v1/models` client for user API keys.
 *
 * The gateway resolves the effective model shelf server-side: a bound channel
 * with model restrictions owns the list, so the response already reflects
 * channel-level limits for the calling key — never group-level guesses.
 * Every platform answers with `{ object: "list", data: [{ id }] }` and may add
 * a top-level `metadata` object with trusted per-model token limits (context
 * window / max output) resolved from the repo-owned model data document —
 * clients generate config `limit` values from it instead of context=0.
 */

export interface KeyModelInfo {
  id: string
  /** Trusted input context window, when the model data document knows it. */
  contextWindow?: number
  /** Trusted max output tokens, when the model data document knows it. */
  maxOutputTokens?: number
}

function normalizeGatewayBaseUrl(baseUrl: string): string {
  const fallback = typeof window !== 'undefined' ? window.location.origin : ''
  const value = (baseUrl || fallback).trim().replace(/\/+$/, '')
  if (!value) return '/v1'
  return /\/v1$/i.test(value) ? value : `${value}/v1`
}

export function buildKeyModelsUrl(baseUrl: string): string {
  return `${normalizeGatewayBaseUrl(baseUrl)}/models`
}

function extractModelInfos(payload: unknown): KeyModelInfo[] {
  if (typeof payload !== 'object' || payload === null) return []
  const data = (payload as { data?: unknown }).data
  if (!Array.isArray(data)) return []
  const raw: unknown[] = []
  for (const entry of data) {
    if (typeof entry === 'string') {
      raw.push(entry)
      continue
    }
    if (typeof entry === 'object' && entry !== null && 'id' in entry) {
      raw.push((entry as { id?: unknown }).id)
    }
  }
  // Server-derived shelf: drop wildcard rules and fictional platform-default
  // fallbacks (e.g. backend default tables when no restricted channel binds
  // the group) so they are never presented as the key's real models.
  const ids = normalizeModelIds(raw, { dropWildcards: true })

  // Trusted per-model token metadata (top-level `metadata`): only positive
  // numbers from the gateway count; anything else is treated as unknown.
  const metadata = (payload as { metadata?: unknown }).metadata
  const limits = new Map<string, { contextWindow?: number; maxOutputTokens?: number }>()
  if (typeof metadata === 'object' && metadata !== null) {
    for (const [id, value] of Object.entries(metadata as Record<string, unknown>)) {
      if (typeof value !== 'object' || value === null) continue
      const contextWindow = (value as { context_window?: unknown }).context_window
      const maxOutputTokens = (value as { max_output_tokens?: unknown }).max_output_tokens
      const entry: { contextWindow?: number; maxOutputTokens?: number } = {}
      if (typeof contextWindow === 'number' && Number.isInteger(contextWindow) && contextWindow > 0) {
        entry.contextWindow = contextWindow
      }
      if (typeof maxOutputTokens === 'number' && Number.isInteger(maxOutputTokens) && maxOutputTokens > 0) {
        entry.maxOutputTokens = maxOutputTokens
      }
      if (entry.contextWindow || entry.maxOutputTokens) limits.set(id, entry)
    }
  }

  return ids.map((id) => {
    const trusted = limits.get(id)
    return trusted ? { id, ...trusted } : { id }
  })
}

/** Fetch the channel-effective model list for an API key. Throws on failure. */
export async function fetchKeyAvailableModels(
  baseUrl: string,
  apiKey: string,
  signal?: AbortSignal
): Promise<KeyModelInfo[]> {
  const response = await fetch(buildKeyModelsUrl(baseUrl), {
    method: 'GET',
    headers: {
      Accept: 'application/json',
      Authorization: `Bearer ${apiKey}`
    },
    cache: 'no-store',
    signal
  })
  if (!response.ok) {
    throw new Error(`Key models request failed with status ${response.status}`)
  }
  return extractModelInfos(await response.json())
}
