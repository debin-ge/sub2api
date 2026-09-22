import { describe, expect, it } from 'vitest'
import { CREATABLE_PROVIDERS, PROVIDERS, PROVIDER_GLM } from '@/constants/channelMonitor'

describe('CREATABLE_PROVIDERS', () => {
  it('excludes the legacy glm provider ID', () => {
    expect(CREATABLE_PROVIDERS).not.toContain(PROVIDER_GLM)
  })

  it('otherwise matches PROVIDERS exactly', () => {
    expect(CREATABLE_PROVIDERS).toEqual(PROVIDERS.filter((provider) => provider !== PROVIDER_GLM))
    expect(CREATABLE_PROVIDERS).toHaveLength(PROVIDERS.length - 1)
  })
})
