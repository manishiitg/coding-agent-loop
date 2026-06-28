import { BrainCircuit } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { iconButtonClass } from '../ui/IconPopover'
import LLMConfigurationModal from '../LLMConfigurationModal'
import LLMDiscoveryOnboardingModal from '../LLMDiscoveryOnboardingModal'
import DelegationTierConfigModal from '../DelegationTierConfigModal'
import { useLlmOnboarding } from './useLlmOnboarding'

/**
 * LlmControl - the model-configuration trigger plus its modal wiring (full
 * configuration, first-run discovery, and delegation tiers). Auto-open behavior
 * lives in the useLlmOnboarding hook.
 */
export default function LlmControl() {
  const {
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
  } = useLlmOnboarding()

  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            data-tour="sidebar-llm-settings"
            data-testid="tour-sidebar-llm-settings"
            onClick={openLLMConfigModal}
            aria-label="Models"
            className={iconButtonClass}
          >
            <BrainCircuit className="w-4 h-4" />
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom">{llmCount} model{llmCount !== 1 ? 's' : ''} enabled</TooltipContent>
      </Tooltip>

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
