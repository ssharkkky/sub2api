import { describe, expect, it } from 'vitest'

import { resolveChannelModelsForGroup } from '../channels'

const channels = [
  {
    name: 'channel-a',
    description: '',
    platforms: [
      {
        platform: 'grok',
        groups: [{ id: 7, name: 'g', platform: 'grok' }],
        supported_models: [{ name: 'grok-4.5', platform: 'grok' }, { name: 'grok-build-0.1', platform: 'grok' }]
      },
      {
        platform: 'anthropic',
        groups: [{ id: 9, name: 'other', platform: 'anthropic' }],
        supported_models: [{ name: 'claude-x', platform: 'anthropic' }]
      }
    ]
  },
  {
    name: 'channel-b',
    description: '',
    platforms: [
      {
        platform: 'grok',
        groups: [{ id: 7, name: 'g', platform: 'grok' }],
        supported_models: [{ name: 'grok-4.5', platform: 'grok' }, { name: 'grok-4.3', platform: 'grok' }]
      }
    ]
  }
] as never

describe('resolveChannelModelsForGroup', () => {
  it('returns channel-level models for the bound group and platform', () => {
    expect(resolveChannelModelsForGroup(channels, 7, 'grok')).toEqual([
      'grok-4.5',
      'grok-build-0.1',
      'grok-4.3'
    ])
  })

  it('ignores sections bound to other groups or platforms', () => {
    expect(resolveChannelModelsForGroup(channels, 9, 'grok')).toEqual([])
    expect(resolveChannelModelsForGroup(channels, 9, 'anthropic')).toEqual(['claude-x'])
  })

  it('accepts every bound section for composite groups', () => {
    expect(resolveChannelModelsForGroup(channels, 7, 'composite')).toEqual([
      'grok-4.5',
      'grok-build-0.1',
      'grok-4.3'
    ])
  })

  it('returns empty for unknown groups', () => {
    expect(resolveChannelModelsForGroup(channels, 999, 'grok')).toEqual([])
    expect(resolveChannelModelsForGroup(null, 7, 'grok')).toEqual([])
    expect(resolveChannelModelsForGroup(channels, null, 'grok')).toEqual([])
  })
})
