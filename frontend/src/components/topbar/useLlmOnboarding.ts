import { useCallback, useEffect, useState } from 'react'
import { useLLMStore } from '../../stores'
import { useModeStore } from '../../stores/useModeStore'
import { useCommandDialogStore } from '../../stores/useCommandDialogStore'
import {
  dismissLLMDiscoveryOnboarding,
  isLLMDiscoveryOnboardingDismissed,
  markLLMDiscoveryOnboardingCleared,
  markLLMDiscoveryOnboardingOpen,
} from '../../utils/onboarding'
import { shouldAutoOpenDelegationTierModal } from '../../utils/llmOnboarding'

const FORCE_LLM_DISCOVERY_ONBOARDING_FOR_TESTING = false

/**
 * Encapsulates the first-run LLM setup behavior that used to live in the
 * sidebar: auto-opening the discovery modal when no model is configured, and the
 * delegation-tier modal when entering multi-agent mode. Returns the modal open
 * states and handlers consumed by LlmControl.
 */
export function useLlmOnboarding() {
  const {
    showLLMModal,
    setShowLLMModal,
    showTierModal,
    setShowTierModal,
    delegationTierConfig,
    delegationTierDefaultsStatus,
    savedLLMs,
    defaultsLoaded,
    primaryConfig,
    agentConfig,
    chatPrimaryConfig,
    chatAgentConfig,
    workflowPrimaryConfig,
    workflowAgentConfig,
  } = useLLMStore()
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  const showDelegationTiersDialog = useCommandDialogStore(state => state.showDelegationTiers)
  const closeDialog = useCommandDialogStore(state => state.closeDialog)

  const [llmOnboardingActive, setLLMOnboardingActive] = useState(false)

  const llmCount = savedLLMs.length

  const hasConfiguredLLM =
    savedLLMs.length > 0 ||
    [primaryConfig, chatPrimaryConfig, workflowPrimaryConfig].some(config => Boolean(config?.provider && config?.model_id?.trim())) ||
    [agentConfig, chatAgentConfig, workflowAgentConfig].some(config => Boolean(config?.primary?.provider && config?.primary?.model_id?.trim()))

  const openLLMConfigModal = useCallback(() => setShowLLMModal(true), [setShowLLMModal])

  const openLLMOnboarding = useCallback(() => {
    markLLMDiscoveryOnboardingOpen()
    setLLMOnboardingActive(true)
    setShowLLMModal(true)
  }, [setShowLLMModal])

  const closeLLMConfigurationModal = useCallback(() => {
    setShowLLMModal(false)
    if (llmOnboardingActive) {
      dismissLLMDiscoveryOnboarding()
      setLLMOnboardingActive(false)
      markLLMDiscoveryOnboardingCleared()
    }
  }, [llmOnboardingActive, setShowLLMModal])

  const closeTierModal = useCallback(() => setShowTierModal(false), [setShowTierModal])

  // Auto-open delegation tier modal when triggered from multi-agent mode entry
  useEffect(() => {
    if (showDelegationTiersDialog && selectedModeCategory === 'multi-agent') {
      setShowTierModal(true)
      closeDialog('delegationTiers')
    }
  }, [showDelegationTiersDialog, selectedModeCategory, closeDialog, setShowTierModal])

  // First-run LLM setup opens the same unified Model Library used later.
  useEffect(() => {
    if (FORCE_LLM_DISCOVERY_ONBOARDING_FOR_TESTING) {
      openLLMOnboarding()
      return
    }
    if (!defaultsLoaded || hasConfiguredLLM) return
    if (isLLMDiscoveryOnboardingDismissed()) {
      markLLMDiscoveryOnboardingCleared()
      return
    }
    openLLMOnboarding()
  }, [defaultsLoaded, hasConfiguredLLM, openLLMOnboarding])

  useEffect(() => {
    if (!defaultsLoaded) return
    if (hasConfiguredLLM || isLLMDiscoveryOnboardingDismissed()) {
      markLLMDiscoveryOnboardingCleared()
    }
  }, [defaultsLoaded, hasConfiguredLLM])

  // Auto-show tier config modal when entering multi-agent mode without tiers configured
  useEffect(() => {
    if (FORCE_LLM_DISCOVERY_ONBOARDING_FOR_TESTING) {
      openLLMOnboarding()
      return
    }
    if (selectedModeCategory !== 'multi-agent') return
    if (!defaultsLoaded || delegationTierDefaultsStatus !== 'loaded') return
    if (!hasConfiguredLLM) {
      if (!isLLMDiscoveryOnboardingDismissed()) {
        openLLMOnboarding()
      }
      return
    }
    if (shouldAutoOpenDelegationTierModal({
      selectedModeCategory,
      defaultsLoaded,
      delegationTierDefaultsStatus,
      hasConfiguredLLM,
      delegationTierConfig,
    })) {
      setShowTierModal(true)
    }
  }, [
    selectedModeCategory,
    defaultsLoaded,
    delegationTierDefaultsStatus,
    delegationTierConfig,
    hasConfiguredLLM,
    openLLMOnboarding,
    setShowTierModal,
  ])

  return {
    llmCount,
    showLLMModal,
    showTierModal,
    openLLMConfigModal,
    closeLLMConfigurationModal,
    closeTierModal,
  }
}
