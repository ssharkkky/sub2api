import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProfilePasskeyCard from '@/components/user/profile/ProfilePasskeyCard.vue'

const { listMock, showErrorMock, showSuccessMock } = vi.hoisted(() => ({
  listMock: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn()
}))

vi.mock('@/api', () => ({
  passkeyAPI: {
    isSupported: () => true,
    list: listMock,
    register: vi.fn(),
    rename: vi.fn(),
    remove: vi.fn()
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: showSuccessMock
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

describe('ProfilePasskeyCard', () => {
  beforeEach(() => {
    listMock.mockReset()
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
    listMock.mockResolvedValue([])
  })

  it('does not request credentials while passkeys are disabled', async () => {
    const wrapper = mount(ProfilePasskeyCard, {
      props: { enabled: false },
      global: { stubs: { Icon: true } }
    })

    await flushPromises()

    expect(listMock).not.toHaveBeenCalled()
    expect(showErrorMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('profile.passkey.featureDisabled')
    expect(wrapper.text()).not.toContain('profile.passkey.empty')
  })

  it('silences PASSKEY_DISABLED returned during a settings race', async () => {
    listMock.mockRejectedValue({ code: 403, reason: 'PASSKEY_DISABLED' })

    mount(ProfilePasskeyCard, {
      props: { enabled: true },
      global: { stubs: { Icon: true } }
    })

    await flushPromises()

    expect(listMock).toHaveBeenCalledOnce()
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('reports other credential loading failures', async () => {
    listMock.mockRejectedValue({ code: 500, reason: 'INTERNAL_ERROR' })

    mount(ProfilePasskeyCard, {
      props: { enabled: true },
      global: { stubs: { Icon: true } }
    })

    await flushPromises()

    expect(showErrorMock).toHaveBeenCalledOnce()
    expect(showErrorMock).toHaveBeenCalledWith('profile.passkey.loadFailed')
  })

  it('discards a credential response after passkeys are disabled', async () => {
    let resolveList!: (value: Array<{ id: number; name: string }>) => void
    listMock.mockReturnValue(new Promise((resolve) => {
      resolveList = resolve
    }))
    const wrapper = mount(ProfilePasskeyCard, {
      props: { enabled: true },
      global: { stubs: { Icon: true } }
    })
    await flushPromises()

    await wrapper.setProps({ enabled: false })
    resolveList([{ id: 1, name: 'stale credential' }])
    await flushPromises()

    expect(wrapper.text()).toContain('profile.passkey.featureDisabled')
    expect(wrapper.text()).not.toContain('stale credential')
    expect(showErrorMock).not.toHaveBeenCalled()
  })
})
