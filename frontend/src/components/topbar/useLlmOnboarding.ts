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

  const [showLLMDiscoveryModal, setShowLLMDiscoveryModal] = useState(false)
  const [releaseWalkthroughAfterLLMModalClose, setReleaseWalkthroughAfterLLMModalClose] = useState(false)

  const llmCount = savedLLMs.length

  const hasConfiguredLLM =
    savedLLMs.length > 0 ||
    [primaryConfig, chatPrimaryConfig, workflowPrimaryConfig].some(config => Boolean(config?.provider && config?.model_id?.trim())) ||
    [agentConfig, chatAgentConfig, workflowAgentConfig].some(config => Boolean(config?.primary?.provider && config?.primary?.model_id?.trim()))

  const openLLMConfigModal = useCallback(() => setShowLLMModal(true), [setShowLLMModal])

  const openLLMDiscoveryModal = useCallback(() => {
    markLLMDiscoveryOnboardingOpen()
    setShowLLMDiscoveryModal(true)
  }, [])

  const closeLLMDiscoveryModal = useCallback(() => {
    dismissLLMDiscoveryOnboarding()
    setShowLLMDiscoveryModal(false)
    markLLMDiscoveryOnboardingCleared()
  }, [])

  const openAdvancedSetupFromDiscovery = useCallback(() => {
    dismissLLMDiscoveryOnboarding()
    setShowLLMDiscoveryModal(false)
    setReleaseWalkthroughAfterLLMModalClose(true)
    setShowLLMModal(true)
  }, [setShowLLMModal])

  const closeLLMConfigurationModal = useCallback(() => {
    setShowLLMModal(false)
    if (releaseWalkthroughAfterLLMModalClose) {
      setReleaseWalkthroughAfterLLMModalClose(false)
      markLLMDiscoveryOnboardingCleared()
    }
  }, [releaseWalkthroughAfterLLMModalClose, setShowLLMModal])

  const closeTierModal = useCallback(() => setShowTierModal(false), [setShowTierModal])

  // Auto-open delegation tier modal when triggered from multi-agent mode entry
  useEffect(() => {
    if (showDelegationTiersDialog && selectedModeCategory === 'multi-agent') {
      setShowTierModal(true)
      closeDialog('delegationTiers')
    }
  }, [showDelegationTiersDialog, selectedModeCategory, closeDialog, setShowTierModal])

  // First-run LLM setup: if no model is configured yet, prefer discovery over
  // the advanced tier configuration modal.
  useEffect(() => {
    if (FORCE_LLM_DISCOVERY_ONBOARDING_FOR_TESTING) {
      openLLMDiscoveryModal()
      return
    }
    if (!defaultsLoaded || hasConfiguredLLM) return
    if (isLLMDiscoveryOnboardingDismissed()) {
      markLLMDiscoveryOnboardingCleared()
      return
    }
    openLLMDiscoveryModal()
  }, [defaultsLoaded, hasConfiguredLLM, openLLMDiscoveryModal])

  useEffect(() => {
    if (!defaultsLoaded) return
    if (hasConfiguredLLM || isLLMDiscoveryOnboardingDismissed()) {
      markLLMDiscoveryOnboardingCleared()
    }
  }, [defaultsLoaded, hasConfiguredLLM])

  // Auto-show tier config modal when entering multi-agent mode without tiers configured
  useEffect(() => {
    if (FORCE_LLM_DISCOVERY_ONBOARDING_FOR_TESTING) {
      openLLMDiscoveryModal()
      return
    }
    if (selectedModeCategory !== 'multi-agent') return
    if (!hasConfiguredLLM) {
      if (!isLLMDiscoveryOnboardingDismissed()) {
        openLLMDiscoveryModal()
      }
      return
    }
    const hasTiers = delegationTierConfig && (delegationTierConfig.high || delegationTierConfig.medium || delegationTierConfig.low)
    if (!hasTiers) {
      setShowTierModal(true)
    }
  }, [selectedModeCategory, delegationTierConfig, hasConfiguredLLM, openLLMDiscoveryModal, setShowTierModal])

  return {
    llmCount,
    showLLMModal,
    showLLMDiscoveryModal,
    showTierModal,
    openLLMConfigModal,
    openLLMDiscoveryModal,
    closeLLMDiscoveryModal,
    openAdvancedSetupFromDiscovery,
    closeLLMConfigurationModal,
    closeTierModal,
  }
}
