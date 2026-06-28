import LLMConfigurationModal from '../LLMConfigurationModal'
import LLMDiscoveryOnboardingModal from '../LLMDiscoveryOnboardingModal'
import DelegationTierConfigModal from '../DelegationTierConfigModal'
import { useLlmOnboarding } from './useLlmOnboarding'

/**
 * LlmModalHost renders the LLM configuration modals (full config, first-run
 * discovery, delegation tiers) and runs their auto-open effects via
 * useLlmOnboarding. Mount it EXACTLY ONCE (in the global top bar). The triggers
 * open these modals through the shared LLM store (showLLMModal / showTierModal):
 * the global Models trigger lives in the top bar as <LlmTriggerButton>, and the
 * CoS delegation-tier trigger lives in the Chief of Staff heading (ChatTabs) —
 * both drive the single modal instance rendered here.
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
