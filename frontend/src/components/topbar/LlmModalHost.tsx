import CodingProvidersPanel from '../providers/CodingProvidersPanel'
import { useLlmOnboarding } from './useLlmOnboarding'

/**
 * Renders the shared coding-provider surface and its first-run onboarding.
 * Mount exactly once in the global top bar.
 */
export default function LlmModalHost() {
  const {
    showLLMModal,
    closeLLMConfigurationModal,
  } = useLlmOnboarding()

  return (
    <>
      <CodingProvidersPanel
        isOpen={showLLMModal}
        onClose={closeLLMConfigurationModal}
      />
    </>
  )
}
