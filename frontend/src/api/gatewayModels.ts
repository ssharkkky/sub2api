import { normalizeModelIds } from '@/utils/keyConfigEscape'

/**
 * Gateway `GET /v1/models` client for user API keys.
 *
 * The gateway resolves the effective model shelf server-side: a bound channel
 * with model restrictions owns the list, so the response already reflects
 * channel-level limits for the calling key — never group-level guesses.
 * Every platform answers with `{ object: "list", data: [{ id }] }`.
 */

function normalizeGatewayBaseUrl(baseUrl: string): string {
  const fallback = typeof window !== 'undefined' ? window.location.origin : ''
  const value = (baseUrl || fallback).trim().replace(/\/+$/, '')
  if (!value) return '/v1'
  return /\/v1$/i.test(value) ? value : `${value}/v1`
}

export function buildKeyModelsUrl(baseUrl: string): string {
  return `${normalizeGatewayBaseUrl(baseUrl)}/models`
}

function extractModelIDs(payload: unknown): string[] {
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
  return normalizeModelIds(raw, { dropWildcards: true })
}

/** Fetch the channel-effective model list for an API key. Throws on failure. */
export async function fetchKeyAvailableModels(
  baseUrl: string,
  apiKey: string,
  signal?: AbortSignal
): Promise<string[]> {
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
  return extractModelIDs(await response.json())
}
