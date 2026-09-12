import { lazy, Suspense, useEffect, useState } from 'react'
import { useLlmOnboarding } from './useLlmOnboarding'

const CodingProvidersPanel = lazy(() => import('../providers/CodingProvidersPanel'))

/** The legacy showLLMModal entry points now open this page, including onboarding.
 * Keep it mounted after first use so navigating away does not cancel a login. */
export default function LlmModalHost() {
  const { showLLMModal, closeLLMConfigurationModal } = useLlmOnboarding()
  const [hasOpened, setHasOpened] = useState(showLLMModal)
  useEffect(() => {
    if (showLLMModal) setHasOpened(true)
  }, [showLLMModal])

  return (
    <div hidden={!showLLMModal} className="h-full min-h-0">
      {(showLLMModal || hasOpened) && <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">Loading providers…</div>}>
        <CodingProvidersPanel embedded isOpen={showLLMModal} onClose={closeLLMConfigurationModal} />
      </Suspense>}
    </div>
  )
}
