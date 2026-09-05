import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { copyToClipboardMock, saveAsMock } = vi.hoisted(() => ({
  copyToClipboardMock: vi.fn().mockResolvedValue(true),
  saveAsMock: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: copyToClipboardMock
  })
}))

vi.mock('file-saver', () => ({
  saveAs: saveAsMock
}))

import UseKeyModal from '../UseKeyModal.vue'

function readBlobAsText(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.addEventListener('load', () => resolve(String(reader.result || '')))
    reader.addEventListener('error', () => reject(reader.error))
    reader.readAsText(blob)
  })
}

function mountModal(props: Record<string, unknown> = {}) {
  return mount(UseKeyModal, {
    props: {
      show: true,
      apiKey: 'sk-test',
      baseUrl: 'https://example.com/v1',
      platform: 'grok',
      ...props
    },
    global: {
      stubs: {
        BaseDialog: {
          template: '<div><slot /><slot name="footer" /></div>'
        },
        Icon: {
          template: '<span />'
        }
      }
    }
  })
}

function stubGatewayModels(models: string[], metadata?: Record<string, Record<string, number>>) {
  const fetchMock = vi.fn().mockImplementation((url: string) => {
    if (String(url).includes('client_version')) {
      return Promise.resolve({ ok: true, status: 200, json: async () => ({ models: [] }) })
    }
    return Promise.resolve({
      ok: true,
      status: 200,
      json: async () => ({
        object: 'list',
        data: models.map((id) => ({ id })),
        ...(metadata ? { metadata } : {})
      })
    })
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function allCode(wrapper: ReturnType<typeof mountModal>): string {
  return wrapper.findAll('pre code').map((code) => code.text()).join('\n')
}

describe('UseKeyModal', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    saveAsMock.mockClear()
    copyToClipboardMock.mockClear()
  })

  it('auto-loads parent-provided channel models with no manual selector', async () => {
    const wrapper = mountModal({ availableModels: ['grok-4.5', 'grok-build-0.1'] })
    await nextTick()

    expect(wrapper.find('[data-testid="use-key-model-select"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="use-key-model-selector"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="use-key-models-status"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="use-key-model-count"]').exists()).toBe(true)
    const code = allCode(wrapper)
    expect(code).toContain('[model."grok-4.5"]')
    expect(code).toContain('[model."grok-build-0.1"]')
  })

  it('uses the first channel model by default in Grok Claude setup instead of hardcoded ids', async () => {
    const wrapper = mountModal({ availableModels: ['grok-aaa', 'grok-bbb'] })
    await nextTick()

    const claudeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.claudeCode')
    )
    expect(claudeTab).toBeDefined()
    await claudeTab!.trigger('click')
    await nextTick()

    expect(allCode(wrapper)).toContain('export ANTHROPIC_MODEL="grok-aaa"')
    expect(allCode(wrapper)).toContain('export CLAUDE_CODE_SUBAGENT_MODEL="grok-aaa"')
    expect(allCode(wrapper)).not.toContain('grok-4.5')
    expect(allCode(wrapper)).not.toContain('grok-4.3')
    expect(allCode(wrapper)).not.toContain('grok-build-0.1')
    expect(allCode(wrapper)).not.toContain('grok-4.20')
  })

  it('generates Grok Build config entries from the real model list', async () => {
    const wrapper = mountModal({ availableModels: ['grok-aaa', 'grok-bbb'] })
    await nextTick()

    const code = allCode(wrapper)
    expect(code).toContain('[model."grok-aaa"]')
    expect(code).toContain('[model."grok-bbb"]')
    expect(code).toContain('default = "grok-aaa"')
    expect(code).toContain('web_search = "grok-aaa"')
    expect(code).toContain('api_backend = "responses"')
    expect(code).not.toContain('[model."grok-4.5"]')
    expect(code).not.toContain('grok-4.20-multi-agent-0309')
    expect(code).not.toContain('grok-composer')
  })

  it('uses the first channel model by default in Gemini CLI setup', async () => {
    const wrapper = mountModal({
      platform: 'gemini',
      availableModels: ['gemini-hello-world']
    })
    await nextTick()

    expect(allCode(wrapper)).toContain('export GEMINI_MODEL="gemini-hello-world"')
    expect(allCode(wrapper)).not.toContain('gemini-2.0-flash')
  })

  it('builds OpenCode provider models from the real list without invented metadata', async () => {
    const wrapper = mountModal({
      platform: 'openai',
      availableModels: ['gpt-real-a', 'gpt-real-b']
    })
    await nextTick()

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )
    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const parsed = JSON.parse(wrapper.find('pre code').text())
    expect(Object.keys(parsed.provider.openai.models).sort()).toEqual(['gpt-real-a', 'gpt-real-b'])
    expect(parsed.provider.openai.models['gpt-real-a']).toEqual({ name: 'gpt-real-a' })
    const raw = wrapper.find('pre code').text()
    expect(raw).not.toContain('gpt-5.6')
    expect(raw).not.toContain('variants')
    expect(raw).not.toContain('claude-fable')
  })

  it('loads the key-scoped shelf from the gateway when the parent provides nothing', async () => {
    stubGatewayModels(['grok-from-gateway'])

    const wrapper = mountModal({ availableModels: undefined })
    await flushPromises()

    expect(wrapper.find('[data-testid="use-key-model-select"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="use-key-model-count"]').exists()).toBe(true)
    expect(allCode(wrapper)).toContain('[model."grok-from-gateway"]')
    expect(allCode(wrapper)).not.toContain('[model."grok-4.5"]')
  })

  it('falls back to channel restrictions filtered by group when the gateway rejects the key', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 401, json: async () => ({}) })
    )
    const channelsModule = await import('@/api/channels')
    const getAvailableSpy = vi
      .spyOn(channelsModule, 'getAvailable')
      .mockResolvedValue([
        {
          name: 'channel-a',
          description: '',
          platforms: [
            {
              platform: 'grok',
              groups: [{ id: 7, name: 'g' }],
              supported_models: [{ name: 'grok-channel-only', platform: 'grok' }]
            },
            {
              platform: 'grok',
              groups: [{ id: 8, name: 'other' }],
              supported_models: [{ name: 'grok-other-group', platform: 'grok' }]
            }
          ]
        }
      ] as never)

    try {
      const wrapper = mountModal({ groupId: 7, availableModels: [] })
      await flushPromises()

      expect(wrapper.find('[data-testid="use-key-model-select"]').exists()).toBe(false)
      expect(wrapper.get('[data-testid="use-key-model-count"]').exists()).toBe(true)
      expect(allCode(wrapper)).toContain('[model."grok-channel-only"]')
      expect(allCode(wrapper)).not.toContain('grok-other-group')
    } finally {
      getAvailableSpy.mockRestore()
    }
  })

  it('shows an empty notice and invents no models when every source fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) })
    )
    const channelsModule = await import('@/api/channels')
    const getAvailableSpy = vi.spyOn(channelsModule, 'getAvailable').mockRejectedValue(new Error('down'))

    try {
      const wrapper = mountModal({ availableModels: [] })
      await flushPromises()

      expect(wrapper.get('[data-testid="use-key-model-empty"]').exists()).toBe(true)
      const code = allCode(wrapper)
      expect(code).not.toContain('gpt-5.6')
      expect(code).not.toContain('grok-4.5')
      expect(code).not.toContain('claude-fable')
      expect(code).not.toContain('gemini-2.0-flash')
    } finally {
      getAvailableSpy.mockRestore()
    }
  })

  it('omits the attribution override from every standard Claude Code setup form', async () => {
    const wrapper = mountModal({
      platform: 'anthropic',
      apiKey: 'sk-anthropic-test',
      availableModels: ['claude-real']
    })
    await nextTick()

    for (const [shell, trafficSetting] of [
      ['macOS / Linux', 'export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"'],
      ['Windows CMD', 'set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1'],
      ['PowerShell', '$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"']
    ]) {
      if (shell !== 'macOS / Linux') {
        const shellTab = wrapper.findAll('button').find(
          (button) => button.text().trim() === shell
        )
        expect(shellTab).toBeDefined()
        await shellTab!.trigger('click')
        await nextTick()
      }

      const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
      const code = codeBlocks.join('\n')
      const settings = JSON.parse(codeBlocks.find((content) => content.includes('"$schema"'))!)

      expect(code).not.toContain('CLAUDE_CODE_ATTRIBUTION_HEADER')
      expect(code).toContain(trafficSetting)
      expect(settings.env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC).toBe('1')
      expect(settings.env).not.toHaveProperty('CLAUDE_CODE_ATTRIBUTION_HEADER')
    }
  })

  it('keeps legacy OpenAI Codex auth behavior while using the real model', async () => {
    const wrapper = mountModal({
      platform: 'openai',
      apiKey: 'sk-test',
      availableModels: ['gpt-real']
    })
    await nextTick()

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('model_provider = "OpenAI"'))

    expect(configToml).toBeDefined()
    expect(configToml).toContain('model = "gpt-real"')
    expect(configToml).toContain('review_model = "gpt-real"')
    expect(configToml).not.toContain('gpt-5.6')
    expect(configToml).toContain('requires_openai_auth = true')
    expect(configToml).not.toContain('experimental_bearer_token')
    expect(codeBlocks).toContain('{\n  "OPENAI_API_KEY": "sk-test"\n}')
    expect(wrapper.find('[data-testid="codex-api-key-restart-notice"]').exists()).toBe(false)
  })

  it('renders API Key Mode authorization in OpenAI Codex config', async () => {
    const wrapper = mountModal({
      platform: 'openai',
      apiKey: 'sk-test',
      availableModels: ['gpt-real']
    })
    await nextTick()

    const apiKeyMode = wrapper.get('[data-testid="codex-auth-mode-api-key"]')
    await apiKeyMode.trigger('click')
    await nextTick()

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const configToml = codeBlocks.find((content) => content.includes('model_provider = "OpenAI"'))

    expect(apiKeyMode.attributes('aria-checked')).toBe('true')
    expect(configToml).toBeDefined()
    expect(configToml).toContain('requires_openai_auth = false')
    expect(configToml).toContain('experimental_bearer_token = "sk-test"')
    expect(configToml).toContain('http_headers = { "x-openai-actor-authorization" = "local-image-extension" }')
    expect(configToml).toContain('model = "gpt-real"')
    expect(codeBlocks).not.toContain('{\n  "OPENAI_API_KEY": "sk-test"\n}')

    const restartNotice = wrapper.get('[data-testid="codex-api-key-restart-notice"]')
    expect(restartNotice.text()).toContain(
      'keys.useKeyModal.openai.authModeApiKeyRestartNotice'
    )

    await wrapper.get('[data-testid="codex-auth-mode-legacy"]').trigger('click')
    await nextTick()

    expect(wrapper.find('[data-testid="codex-api-key-restart-notice"]').exists()).toBe(false)
  })

  it('resets Codex authentication mode when the modal reopens or platform changes', async () => {
    const wrapper = mountModal({
      platform: 'openai',
      availableModels: ['gpt-real']
    })
    await nextTick()

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(wrapper.get('[data-testid="codex-auth-mode-legacy"]').attributes('aria-checked')).toBe('true')

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await wrapper.setProps({ platform: 'gemini' })
    await wrapper.setProps({ platform: 'openai' })
    await nextTick()

    expect(wrapper.get('[data-testid="codex-auth-mode-legacy"]').attributes('aria-checked')).toBe('true')
  })

  it('prefers the first real model when the Codex catalog contains it', async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes('client_version')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({
            models: [
              { slug: 'gpt-real' },
              { slug: 'gpt-other' }
            ]
          })
        })
      }
      return Promise.resolve({ ok: false, status: 404, json: async () => ({}) })
    })
    vi.stubGlobal('fetch', fetchMock)
    const channelsModule = await import('@/api/channels')
    const getAvailableSpy = vi.spyOn(channelsModule, 'getAvailable').mockResolvedValue([])

    try {
      const wrapper = mountModal({
        platform: 'composite',
        apiKey: 'sk-composite-test',
        availableModels: ['gpt-real', 'gpt-missing']
      })
      await nextTick()

      const codexTab = wrapper.findAll('button').find((button) =>
        button.text().includes('keys.useKeyModal.cliTabs.codexCli')
      )
      expect(codexTab).toBeDefined()
      await codexTab!.trigger('click')
      await nextTick()

      await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
      await flushPromises()

      const config = wrapper.findAll('pre code')
        .map((code) => code.text())
        .find((content) => content.includes('[model_providers.sub2api]'))
      expect(config).toContain('model = "gpt-real"')
      expect(config).toContain('review_model = "gpt-real"')
      expect(config).not.toContain('gpt-5.5')
    } finally {
      getAvailableSpy.mockRestore()
    }
  })

  it('downloads the fetched Codex catalog', async () => {
    const manifest = {
      models: [{ slug: 'gpt-real' }]
    }
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes('client_version')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => manifest })
      }
      return Promise.resolve({ ok: false, status: 404, json: async () => ({}) })
    })
    vi.stubGlobal('fetch', fetchMock)
    const channelsModule = await import('@/api/channels')
    const getAvailableSpy = vi.spyOn(channelsModule, 'getAvailable').mockResolvedValue([])

    try {
      const wrapper = mountModal({
        platform: 'composite',
        apiKey: 'sk-composite-test',
        availableModels: ['gpt-real']
      })
      await nextTick()

      const codexTab = wrapper.findAll('button').find((button) =>
        button.text().includes('keys.useKeyModal.cliTabs.codexCli')
      )
      expect(codexTab).toBeDefined()
      await codexTab!.trigger('click')
      await nextTick()

      await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
      await flushPromises()

      expect(fetchMock).toHaveBeenCalledWith(
        'https://example.com/v1/models?client_version=0.147.0',
        expect.objectContaining({
          headers: expect.objectContaining({ Authorization: 'Bearer sk-composite-test' })
        })
      )
      expect(wrapper.get('[data-testid="codex-model-catalog"]').text())
        .toContain('keys.useKeyModal.codexModelCatalog.download')

      const downloadButton = wrapper.findAll('button').find((button) =>
        button.text().includes('keys.useKeyModal.codexModelCatalog.download')
      )
      expect(downloadButton).toBeDefined()
      await downloadButton!.trigger('click')
      expect(saveAsMock).toHaveBeenCalledWith(expect.any(Blob), 'codex-models.json')
      const downloadedBlob = saveAsMock.mock.calls[0]?.[0] as Blob
      expect(JSON.parse(await readBlobAsText(downloadedBlob))).toEqual(manifest)
    } finally {
      getAvailableSpy.mockRestore()
    }
  })

  it('neutralizes shell metacharacters in dynamic model ids', async () => {
    const wrapper = mountModal({
      platform: 'gemini',
      availableModels: ['a"b`c$d$(touch pwn)', 'gemini-real']
    })
    await nextTick()

    const code = allCode(wrapper)
    expect(code).not.toContain('d$(touch pwn)')
    expect(code).toContain('export GEMINI_MODEL="a\\"b\\`c\\$d\\$(touch pwn)"')
  })

  it('escapes TOML string breaks from hostile model ids', async () => {
    const wrapper = mountModal({
      availableModels: ['grok-real', 'evil"break']
    })
    await nextTick()

    const code = allCode(wrapper)
    expect(code).toContain('[model."grok-real"]')
    expect(code).toContain('evil\\"break')
    expect(code).not.toContain('evil"break')
    expect(code).toContain('default = "grok-real"')
  })

  it('drops wildcard pricing rules instead of configuring them', async () => {
    const wrapper = mountModal({
      platform: 'openai',
      availableModels: ['gpt-4*', 'gpt-real']
    })
    await nextTick()

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )
    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const parsed = JSON.parse(wrapper.find('pre code').text())
    expect(Object.keys(parsed.provider.openai.models)).toEqual(['gpt-real'])
    expect(wrapper.find('pre code').text()).not.toContain('gpt-4*')
  })

  it('trusts concrete model ids from the gateway shelf', async () => {
    stubGatewayModels(['gpt-5.6-sol', 'gpt-real'])

    const wrapper = mountModal({ platform: 'openai', availableModels: undefined })
    await flushPromises()

    const code = allCode(wrapper)
    expect(code).toContain('model = "gpt-5.6-sol"')
  })

  it('never selects a media-only model as the text-client default', async () => {
    const wrapper = mountModal({ availableModels: ['grok-imagine-image', 'grok-aaa'] })
    await nextTick()

    const code = allCode(wrapper)
    expect(code).toContain('default = "grok-aaa"')
    expect(code).toContain('[model."grok-aaa"]')
    expect(code).not.toContain('grok-imagine-image')
  })

  it('injects the channel model into standard Claude Code setups', async () => {
    const wrapper = mountModal({
      platform: 'anthropic',
      availableModels: ['claude-real']
    })
    await nextTick()

    const codeBlocks = wrapper.findAll('pre code').map((code) => code.text())
    const code = codeBlocks.join('\n')
    expect(code).toContain('export ANTHROPIC_MODEL="claude-real"')
    const settings = JSON.parse(codeBlocks.find((content) => content.includes('"$schema"'))!)
    expect(settings.env.ANTHROPIC_MODEL).toBe('claude-real')
    expect(settings.env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC).toBe('1')
  })

  it('clears the previous key models while the next key loads', async () => {
    stubGatewayModels(['model-a'])
    const wrapper = mountModal({ availableModels: undefined })
    await flushPromises()
    expect(allCode(wrapper)).toContain('model-a')

    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise(() => {})))
    await wrapper.setProps({ apiKey: 'sk-next' })
    await nextTick()

    expect(allCode(wrapper)).not.toContain('model-a')
    expect(wrapper.text()).toContain('keys.useKeyModal.modelSelector.loading')
  })

  it('switches the Claude default model through the model pills', async () => {
    const wrapper = mountModal({ availableModels: ['grok-4.5', 'grok-4.6'] })
    await nextTick()

    const pills = wrapper.findAll('[data-testid="use-key-model-pill"]')
    expect(pills.map((pill) => pill.text())).toEqual(['grok-4.5', 'grok-4.6'])
    expect(pills[0].attributes('aria-pressed')).toBe('true')

    const claudeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.claudeCode')
    )
    expect(claudeTab).toBeDefined()
    await claudeTab!.trigger('click')
    await nextTick()

    expect(allCode(wrapper)).toContain('export ANTHROPIC_MODEL="grok-4.5"')

    await pills[1].trigger('click')
    await nextTick()

    expect(wrapper.findAll('[data-testid="use-key-model-pill"]')[1].attributes('aria-pressed')).toBe('true')
    expect(allCode(wrapper)).toContain('export ANTHROPIC_MODEL="grok-4.6"')
    expect(allCode(wrapper)).toContain('export CLAUDE_CODE_SUBAGENT_MODEL="grok-4.6"')
    expect(allCode(wrapper)).not.toContain('export ANTHROPIC_MODEL="grok-4.5"')
  })

  it('switches Codex and Grok Build defaults through the model pills', async () => {
    const wrapper = mountModal({ availableModels: ['grok-4.5', 'grok-4.6'] })
    await nextTick()

    await wrapper.findAll('[data-testid="use-key-model-pill"]')[1].trigger('click')
    await nextTick()

    expect(allCode(wrapper)).toContain('default = "grok-4.6"')
    expect(allCode(wrapper)).toContain('web_search = "grok-4.6"')
    expect(allCode(wrapper)).toContain('[model."grok-4.5"]')
    expect(allCode(wrapper)).toContain('[model."grok-4.6"]')

    const codexTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCli')
    )
    expect(codexTab).toBeDefined()
    await codexTab!.trigger('click')
    await nextTick()

    const config = allCode(wrapper)
    expect(config).toContain('model = "grok-4.6"')
    expect(config).not.toContain('model = "grok-4.5"')
  })

  it('hides the model pills when only one model is available', async () => {
    const wrapper = mountModal({ availableModels: ['grok-4.5'] })
    await nextTick()

    expect(wrapper.find('[data-testid="use-key-model-pills"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="use-key-model-pill"]')).toHaveLength(0)
    expect(wrapper.get('[data-testid="use-key-model-count"]').exists()).toBe(true)
  })

  it('derives OpenAI Codex reasoning effort from the auto default catalog descriptor', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({
          models: [
            {
              slug: 'glm-5.3',
              default_reasoning_level: 'none',
              supported_reasoning_levels: [{ effort: 'none' }]
            }
          ]
        })
      })
    )

    const wrapper = mountModal({
      platform: 'openai',
      apiKey: 'sk-openai-test',
      availableModels: ['glm-5.3']
    })
    await nextTick()

    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()

    const configToml = wrapper.findAll('pre code')
      .map((code) => code.text())
      .find((content) => content.includes('model_provider = "OpenAI"'))
    expect(configToml).toContain('model = "glm-5.3"')
    expect(configToml).not.toContain('model_reasoning_effort')
  })

  describe('media-only shelves and default selection', () => {
    it('emits no Grok default when the shelf has only media models', async () => {
      const wrapper = mountModal({ availableModels: ['grok-imagine-image'] })
      await nextTick()

      const code = allCode(wrapper)
      // No `default` dangling without a matching [model.*] entry.
      expect(code).toContain('No channel-effective text models')
      expect(code).not.toContain('default = ""')
      expect(code).not.toContain('default = "grok-imagine-image"')
    })

    it('offers only text-capable models as default pills', async () => {
      const wrapper = mountModal({
        availableModels: ['grok-4.5', 'grok-imagine-image', 'grok-build-0.1']
      })
      await nextTick()

      const pills = wrapper.findAll('[data-testid="use-key-model-pill"]')
      expect(pills.map((pill) => pill.text())).toEqual(['grok-4.5', 'grok-build-0.1'])
    })

    it('does not render pills when only one text model is selectable', async () => {
      const wrapper = mountModal({ availableModels: ['grok-4.5', 'grok-imagine-image'] })
      await nextTick()

      expect(wrapper.find('[data-testid="use-key-model-pills"]').exists()).toBe(false)
      // The single text model still becomes the default.
      expect(allCode(wrapper)).toContain('default = "grok-4.5"')
    })
  })

  describe('OpenCode trusted model limits', () => {
    async function openOpencodeTab(wrapper: ReturnType<typeof mountModal>) {
      const opencodeTab = wrapper.findAll('button').find((button) =>
        button.text().includes('keys.useKeyModal.cliTabs.opencode')
      )
      expect(opencodeTab).toBeDefined()
      await opencodeTab!.trigger('click')
      await nextTick()
      return JSON.parse(wrapper.find('pre code').text())
    }

    it('fills limit from trusted gateway metadata', async () => {
      stubGatewayModels(
        ['claude-sonnet-4-5', 'grok-4.5'],
        { 'claude-sonnet-4-5': { context_window: 200000, max_output_tokens: 64000 } }
      )
      const wrapper = mountModal({ platform: 'anthropic', availableModels: undefined })
      await flushPromises()

      const parsed = await openOpencodeTab(wrapper)
      // Trusted metadata becomes a concrete limit…
      expect(parsed.provider.anthropic.models['claude-sonnet-4-5']).toEqual({
        name: 'claude-sonnet-4-5',
        limit: { context: 200000, output: 64000 }
      })
      // …while models without trusted metadata keep the minimal shape
      // (no invented limits).
      expect(parsed.provider.anthropic.models['grok-4.5']).toEqual({ name: 'grok-4.5' })
    })

    it('does not invent limits for gateway models without metadata', async () => {
      stubGatewayModels(['some-custom-model'])
      const wrapper = mountModal({ platform: 'openai', availableModels: undefined })
      await flushPromises()

      const parsed = await openOpencodeTab(wrapper)
      expect(parsed.provider.openai.models['some-custom-model']).toEqual({
        name: 'some-custom-model'
      })
    })

    it('does not attach limits to parent-provided shelves', async () => {
      const wrapper = mountModal({ platform: 'openai', availableModels: ['gpt-real'] })
      await nextTick()

      const parsed = await openOpencodeTab(wrapper)
      expect(parsed.provider.openai.models['gpt-real']).toEqual({ name: 'gpt-real' })
    })
  })
})
