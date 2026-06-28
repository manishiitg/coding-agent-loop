import { BrainCircuit } from 'lucide-react'
import { iconButtonClass } from '../ui/IconPopover'
import { useLLMStore } from '../../stores'

type LlmTriggerButtonProps = {
  className?: string
}

/**
 * LlmTriggerButton opens the LLM configuration modal (rendered once by
 * LlmModalHost) via the shared LLM store. It's stateless, so it can be mounted
 * in multiple per-mode headers (Chief of Staff / workflow) without duplicating
 * the modal. Models config is per-context, so it lives in the mode heading
 * rather than the global top bar.
 */
export default function LlmTriggerButton({ className }: LlmTriggerButtonProps) {
  const setShowLLMModal = useLLMStore(s => s.setShowLLMModal)
  const llmCount = useLLMStore(s => s.savedLLMs.length)

  return (
    <button
      type="button"
      data-tour="sidebar-llm-settings"
      data-testid="tour-sidebar-llm-settings"
      onClick={() => setShowLLMModal(true)}
      aria-label="Models"
      title={`${llmCount} model${llmCount !== 1 ? 's' : ''} enabled — configure models`}
      className={className ?? iconButtonClass}
    >
      <BrainCircuit className="w-4 h-4" />
    </button>
  )
}
