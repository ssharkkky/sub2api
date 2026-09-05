import { describe, expect, it, vi } from 'vitest'

import { buildKeyModelsUrl, fetchKeyAvailableModels } from '../gatewayModels'

function stubFetchOnce(payload: unknown, ok = true, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok,
    status,
    json: async () => payload
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

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
    expect(models).toEqual(['model-b', 'model-a'])
  })

  it('accepts plain string entries', async () => {
    stubFetchOnce({ object: 'list', data: ['model-a', 'model-b'] })
    await expect(fetchKeyAvailableModels('https://example.com/v1', 'sk-test')).resolves.toEqual([
      'model-a',
      'model-b'
    ])
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
    await expect(fetchKeyAvailableModels('https://example.com/v1', 'sk-test')).resolves.toEqual([
      'gpt-real',
      'gpt-5.6-sol',
      'claude-fable-5-1',
      'evilmodel'
    ])
  })
})
