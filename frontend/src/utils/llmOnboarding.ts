import type { DelegationTierConfig } from '../services/api-types'

export type DelegationTierDefaultsStatus = 'idle' | 'loading' | 'loaded' | 'error'

export function hasDelegationTierConfiguration(config: DelegationTierConfig | null | undefined): boolean {
  if (!config) return false
  return Boolean(
    config.provider ||
    config.main ||
    config.chief_of_staff ||
    config.high ||
    config.medium ||
    config.low ||
    (config.custom && Object.keys(config.custom).length > 0),
  )
}

interface ShouldAutoOpenDelegationTierModalInput {
  selectedModeCategory: string
  defaultsLoaded: boolean
  delegationTierDefaultsStatus: DelegationTierDefaultsStatus
  hasConfiguredLLM: boolean
  delegationTierConfig: DelegationTierConfig | null | undefined
}

// Missing configuration is actionable only after both asynchronous config
// sources have finished loading. In particular, provider_profile is a complete
// configuration even though it intentionally has no explicit high/medium/low
// fields.
export function shouldAutoOpenDelegationTierModal({
  selectedModeCategory,
  defaultsLoaded,
  delegationTierDefaultsStatus,
  hasConfiguredLLM,
  delegationTierConfig,
}: ShouldAutoOpenDelegationTierModalInput): boolean {
  return selectedModeCategory === 'multi-agent' &&
    defaultsLoaded &&
    delegationTierDefaultsStatus === 'loaded' &&
    hasConfiguredLLM &&
    !hasDelegationTierConfiguration(delegationTierConfig)
}
