import { describe, expect, it, vi } from 'vitest'

import { buildKeyModelsUrl, fetchKeyAvailableModels } from '../gatewayModels'
import type { KeyModelInfo } from '../gatewayModels'

function stubFetchOnce(payload: unknown, ok = true, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok,
    status,
    json: async () => payload
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const ids = (models: KeyModelInfo[]): string[] => models.map((m) => m.id)

describe('gatewayModels', () => {
  it('builds the /v1/models url from a bare base url', () => {
    expect(buildKeyModelsUrl('https://example.com')).toBe('https://example.com/v1/models')
    expect(buildKeyModelsUrl('https://example.com/v1')).toBe('https://example.com/v1/models')
    expect(buildKeyModelsUrl('https://example.com/v1/')).toBe('https://example.com/v1/models')
  })

  it('fetches the key-scoped model shelf with bearer auth', async () => {
    const fetchMock = stubFetchOnce({
      object: 'list',
      data: [{ id: 'model-b' }, { id: 'model-a' }, { id: 'model-b' }, { id: '  ' }]
    })

    const models = await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')

    expect(fetchMock).toHaveBeenCalledWith(
      'https://example.com/v1/models',
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: 'Bearer sk-test' })
      })
    )
    expect(ids(models)).toEqual(['model-b', 'model-a'])
  })

  it('accepts plain string entries', async () => {
    stubFetchOnce({ object: 'list', data: ['model-a', 'model-b'] })
    const models = await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')
    expect(ids(models)).toEqual(['model-a', 'model-b'])
  })

  it('throws when the gateway rejects the key', async () => {
    stubFetchOnce({ error: 'unauthorized' }, false, 401)
    await expect(fetchKeyAvailableModels('https://example.com/v1', 'sk-bad')).rejects.toThrow('401')
  })

  it('drops wildcard rules and control chars', async () => {
    stubFetchOnce({
      object: 'list',
      data: [
        { id: 'gpt-real' },
        { id: 'gpt-4*' },
        { id: 'gpt-5.6-sol' },
        { id: 'claude-fable-5-1' },
        { id: 'evil\nmodel' },
        { id: '  ' }
      ]
    })
    const models = await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')
    expect(ids(models)).toEqual(['gpt-real', 'gpt-5.6-sol', 'claude-fable-5-1', 'evilmodel'])
  })

  describe('trusted model metadata', () => {
    it('attaches context limits from the top-level metadata object', async () => {
      stubFetchOnce({
        object: 'list',
        data: [{ id: 'claude-sonnet-4-5' }, { id: 'grok-4.5' }, { id: 'unknown-model' }],
        metadata: {
          'claude-sonnet-4-5': { context_window: 200000, max_output_tokens: 64000 },
          'grok-4.5': { context_window: 200000 },
          'unknown-model': { context_window: 128000, max_output_tokens: 8192 }
        }
      })

      const models = await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')
      expect(models).toEqual([
        { id: 'claude-sonnet-4-5', contextWindow: 200000, maxOutputTokens: 64000 },
        { id: 'grok-4.5', contextWindow: 200000 },
        { id: 'unknown-model', contextWindow: 128000, maxOutputTokens: 8192 }
      ])
    })

    it('ignores metadata for models absent from the shelf', async () => {
      stubFetchOnce({
        object: 'list',
        data: [{ id: 'model-a' }],
        metadata: {
          'model-b': { context_window: 200000 },
          'model-a': { context_window: 128000, max_output_tokens: 8192 }
        }
      })

      const models = await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')
      expect(models).toEqual([{ id: 'model-a', contextWindow: 128000, maxOutputTokens: 8192 }])
    })

    it('drops non-positive and non-numeric limit values', async () => {
      stubFetchOnce({
        object: 'list',
        data: [{ id: 'model-a' }, { id: 'model-b' }, { id: 'model-c' }],
        metadata: {
          'model-a': { context_window: 0, max_output_tokens: -5 },
          'model-b': { context_window: '200000' },
          'model-c': { max_output_tokens: 8192 }
        }
      })

      const models = await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')
      expect(models).toEqual([
        { id: 'model-a' },
        { id: 'model-b' },
        { id: 'model-c', maxOutputTokens: 8192 }
      ])
    })

    it('tolerates a missing or malformed metadata section', async () => {
      stubFetchOnce({ object: 'list', data: [{ id: 'model-a' }] })
      expect(await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')).toEqual([
        { id: 'model-a' }
      ])

      stubFetchOnce({ object: 'list', data: [{ id: 'model-a' }], metadata: 'oops' })
      expect(await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')).toEqual([
        { id: 'model-a' }
      ])

      stubFetchOnce({ object: 'list', data: [{ id: 'model-a' }], metadata: { 'model-a': null } })
      expect(await fetchKeyAvailableModels('https://example.com/v1', 'sk-test')).toEqual([
        { id: 'model-a' }
      ])
    })
  })
})
