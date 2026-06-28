import LLMConfigurationModal from '../LLMConfigurationModal'
import LLMDiscoveryOnboardingModal from '../LLMDiscoveryOnboardingModal'
import DelegationTierConfigModal from '../DelegationTierConfigModal'
import { useLlmOnboarding } from './useLlmOnboarding'

/**
 * LlmModalHost renders the LLM configuration modals (full config, first-run
 * discovery, delegation tiers) and runs their auto-open effects via
 * useLlmOnboarding. Mount it EXACTLY ONCE (in the global top bar). The visible
 * trigger lives in the per-mode headers as <LlmTriggerButton>, which opens these
 * modals through the shared LLM store — so the trigger can appear in several
 * headers while the modal still renders only once.
 */
export default function LlmModalHost() {
  const {
    showLLMModal,
    showLLMDiscoveryModal,
    showTierModal,
    openLLMDiscoveryModal,
    closeLLMDiscoveryModal,
    openAdvancedSetupFromDiscovery,
    closeLLMConfigurationModal,
    closeTierModal,
  } = useLlmOnboarding()

  return (
    <>
      <LLMConfigurationModal
        isOpen={showLLMModal}
        onClose={closeLLMConfigurationModal}
        onOpenDiscovery={openLLMDiscoveryModal}
      />
      <LLMDiscoveryOnboardingModal
        isOpen={showLLMDiscoveryModal}
        onClose={closeLLMDiscoveryModal}
        onAdvancedSetup={openAdvancedSetupFromDiscovery}
      />
      <DelegationTierConfigModal isOpen={showTierModal} onClose={closeTierModal} />
    </>
  )
}
