import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'
import type { ApiKey } from '@/types'

const { createMonitor, listKeys, listTemplates, getUserGroupRates } = vi.hoisted(() => ({
  createMonitor: vi.fn(),
  listKeys: vi.fn(),
  listTemplates: vi.fn(),
  getUserGroupRates: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: { create: createMonitor, update: vi.fn() },
    channelMonitorTemplate: { list: listTemplates },
  },
}))

vi.mock('@/api/keys', () => ({ keysAPI: { list: listKeys } }))
vi.mock('@/api/groups', () => ({ userGroupsAPI: { getUserGroupRates } }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const antigravityKey = {
  id: 7,
  key: 'sk-antigravity',
  name: 'Antigravity',
  group: {
    id: 9,
    name: 'Antigravity',
    platform: 'antigravity',
    subscription_type: 'standard',
    rate_multiplier: 1,
  },
} as ApiKey

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const KeyPickerStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  emits: ['pick'],
  setup() {
    return { antigravityKey }
  },
  template: '<button v-if="show" data-testid="pick-antigravity" @click="$emit(\'pick\', antigravityKey)">pick</button>',
})

describe('Antigravity channel monitor route', () => {
  beforeEach(() => {
    createMonitor.mockReset().mockResolvedValue({})
    listKeys.mockReset().mockResolvedValue({ items: [antigravityKey] })
    listTemplates.mockReset().mockResolvedValue({ items: [] })
    getUserGroupRates.mockReset().mockResolvedValue({})
  })

  it.each([
    { provider: 'gemini', model: 'gemini-3-flash' },
    { provider: 'anthropic', model: 'claude-sonnet-4-5' },
  ])('appends the fixed route for an Antigravity $provider monitor', async ({ provider, model }) => {
    const wrapper = mount(MonitorFormDialog, {
      props: { show: true, monitor: null },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Toggle: true,
          Select: true,
          ModelTagInput: true,
          MonitorKeyPickerDialog: KeyPickerStub,
          MonitorAdvancedRequestConfig: true,
        },
      },
    })
    await flushPromises()

    await wrapper.get(`[data-testid="monitor-provider-${provider}"]`).trigger('click')
    const useKeyButton = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.channelMonitor.form.useMyKey'),
    )
    expect(useKeyButton).toBeTruthy()
    await useKeyButton!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="pick-antigravity"]').trigger('click')

    expect(wrapper.get('[data-testid="monitor-antigravity-route"]').text()).toBe('/antigravity')
    await wrapper.get('input[required]').setValue('Antigravity Gemini')
    await wrapper.get('[data-testid="monitor-endpoint"]').setValue('https://gateway.example.com')
    await wrapper.get('[data-testid="monitor-primary-model"]').setValue(model)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(createMonitor).toHaveBeenCalledWith(expect.objectContaining({
      provider,
      endpoint: 'https://gateway.example.com/antigravity',
      api_key: 'sk-antigravity',
    }))
  })
})
