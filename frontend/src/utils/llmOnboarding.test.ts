import { describe, expect, it } from 'vitest'
import type { DelegationTierConfig } from '../services/api-types'
import {
  hasDelegationTierConfiguration,
  shouldAutoOpenDelegationTierModal,
} from './llmOnboarding'

const providerProfile: DelegationTierConfig = {
  schema_version: 2,
  mode: 'provider_profile',
  provider: 'cursor-cli',
}

describe('delegation tier onboarding', () => {
  it('accepts a provider profile as a complete tier configuration', () => {
    expect(hasDelegationTierConfiguration(providerProfile)).toBe(true)
  })

  it('does not open while saved tier configuration is still loading', () => {
    expect(shouldAutoOpenDelegationTierModal({
      selectedModeCategory: 'multi-agent',
      defaultsLoaded: true,
      delegationTierDefaultsStatus: 'loading',
      hasConfiguredLLM: true,
      delegationTierConfig: null,
    })).toBe(false)
  })

  it('does not open for a loaded provider profile', () => {
    expect(shouldAutoOpenDelegationTierModal({
      selectedModeCategory: 'multi-agent',
      defaultsLoaded: true,
      delegationTierDefaultsStatus: 'loaded',
      hasConfiguredLLM: true,
      delegationTierConfig: providerProfile,
    })).toBe(false)
  })

  it('opens only after loading proves that tier configuration is absent', () => {
    expect(shouldAutoOpenDelegationTierModal({
      selectedModeCategory: 'multi-agent',
      defaultsLoaded: true,
      delegationTierDefaultsStatus: 'loaded',
      hasConfiguredLLM: true,
      delegationTierConfig: null,
    })).toBe(true)
  })

  it('does not open after a tier configuration load error', () => {
    expect(shouldAutoOpenDelegationTierModal({
      selectedModeCategory: 'multi-agent',
      defaultsLoaded: true,
      delegationTierDefaultsStatus: 'error',
      hasConfiguredLLM: true,
      delegationTierConfig: null,
    })).toBe(false)
  })
})
