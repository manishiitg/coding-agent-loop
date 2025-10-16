import React, { useState } from 'react'
import { MessageCircle, Search, Workflow, ArrowRight, Info } from 'lucide-react'
import { useModeStore, type ModeCategory } from '../stores/useModeStore'
import { useAppStore } from '../stores/useAppStore'
import { usePresetStore } from '../stores/usePresetStore'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { PresetSelectionOverlay } from './PresetSelectionOverlay'
import { getModeInfoForModal } from '../constants/modeInfo'

interface ModeSelectionModalProps {
  isOpen: boolean
  onClose: () => void
}

interface ModeCardProps {
  category: ModeCategory
  title: string
  description: string
  icon: React.ReactNode
  features: string[]
  exampleQueries: string[]
  onSelect: () => void
}

const ModeCard: React.FC<ModeCardProps> = ({
  category,
  title,
  description,
  icon,
  features,
  exampleQueries,
  onSelect
}) => {
  return (
    <div className="group relative bg-white dark:bg-slate-800 rounded-lg border border-gray-200 dark:border-slate-700 p-4 hover:border-blue-300 dark:hover:border-blue-600 hover:shadow-lg transition-all duration-200 cursor-pointer">
      {/* Icon */}
      <div className="flex items-center justify-center w-10 h-10 bg-blue-50 dark:bg-blue-900/20 rounded-lg mb-3 group-hover:bg-blue-100 dark:group-hover:bg-blue-900/30 transition-colors">
        {icon}
      </div>

      {/* Title */}
      <h3 className="text-base font-bold text-gray-900 dark:text-white mb-2">
        {title}
      </h3>

      {/* Description */}
      <p className="text-xs text-gray-600 dark:text-gray-300 mb-3 leading-relaxed">
        {description}
      </p>

      {/* Features */}
      <div className="mb-3">
        <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-1">
          Key Features:
        </h4>
        <ul className="space-y-0.5">
          {features.map((feature, index) => (
            <li key={index} className="flex items-center text-xs text-gray-600 dark:text-gray-400">
              <div className="w-1 h-1 bg-blue-500 rounded-full mr-2 flex-shrink-0" />
              {feature}
            </li>
          ))}
        </ul>
      </div>

      {/* Example Queries */}
      <div className="mb-4">
        <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-1">
          Examples:
        </h4>
        <div className="space-y-0.5">
          {exampleQueries.map((query, index) => (
            <div key={index} className="text-xs text-gray-500 dark:text-gray-400 italic">
              "{query}"
            </div>
          ))}
        </div>
      </div>

      {/* Get Started Button */}
      <button
        onClick={onSelect}
        className="w-full flex items-center justify-center gap-2 px-3 py-2 bg-blue-600 hover:bg-blue-700 text-white text-xs font-medium rounded-md transition-colors group-hover:shadow-md"
      >
        Get Started
        <ArrowRight className="w-3 h-3 group-hover:translate-x-1 transition-transform" />
      </button>

      {/* Learn More Tooltip */}
      <div className="absolute top-2 right-2">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <button className="p-1 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 transition-colors">
                <Info className="w-3 h-3" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="left" className="max-w-xs">
              <div className="text-sm">
                {category === 'chat' && (
                  <div>
                    <p className="font-semibold mb-2">Chat Mode</p>
                    <p className="mb-2">Perfect for quick questions and conversations. Choose between:</p>
                    <ul className="list-disc list-inside space-y-1">
                      <li><strong>Simple:</strong> Direct answers without reasoning</li>
                      <li><strong>ReAct:</strong> Step-by-step reasoning with memory</li>
                    </ul>
                  </div>
                )}
                {category === 'deep-research' && (
                  <div>
                    <p className="font-semibold mb-2">Deep Research Mode</p>
                    <p className="mb-2">For complex analysis that may take hours. Features:</p>
                    <ul className="list-disc list-inside space-y-1">
                      <li>Multi-step planning and execution</li>
                      <li>Long-term memory and context</li>
                      <li>Requires Tasks/ folder for organization</li>
                      <li>Creates detailed research reports</li>
                    </ul>
                  </div>
                )}
                {category === 'workflow' && (
                  <div>
                    <p className="font-semibold mb-2">Workflow Mode</p>
                    <p className="mb-2">Todo-based execution with human verification. Features:</p>
                    <ul className="list-disc list-inside space-y-1">
                      <li>Sequential task completion</li>
                      <li>Human approval at each step</li>
                      <li>Requires Workflow/ folder for organization</li>
                      <li>Progress tracking and reporting</li>
                    </ul>
                  </div>
                )}
              </div>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </div>
  )
}

export const ModeSelectionModal: React.FC<ModeSelectionModalProps> = ({
  isOpen,
  onClose
}) => {
  const { setModeCategory, completeInitialSetup, getAgentModeFromCategory } = useModeStore()
  const { setAgentMode } = useAppStore()
  const { getActivePreset } = usePresetStore()
  
  // State for preset selection
  const [showPresetSelection, setShowPresetSelection] = useState(false)
  const [pendingModeCategory, setPendingModeCategory] = useState<'deep-research' | 'workflow' | null>(null)

  const handleModeSelect = (category: ModeCategory) => {
    if (!category) return

    if (category === 'chat') {
      // Chat mode doesn't need preset selection
      setModeCategory(category)
      const agentMode = getAgentModeFromCategory(category)
      setAgentMode((agentMode || 'ReAct') as 'simple' | 'ReAct' | 'orchestrator' | 'workflow')
      completeInitialSetup()
      onClose()
    } else {
      // Deep Research or Workflow mode - check if preset is needed
      const activePreset = getActivePreset(category)
      
      if (activePreset) {
        // Preset already selected, proceed with mode selection
        setModeCategory(category)
        const agentMode = getAgentModeFromCategory(category)
        setAgentMode((agentMode || 'ReAct') as 'simple' | 'ReAct' | 'orchestrator' | 'workflow')
        completeInitialSetup()
        onClose()
      } else {
        // No preset selected, show preset selection overlay
        setPendingModeCategory(category)
        setShowPresetSelection(true)
      }
    }
  }

  // Handle preset selection from overlay
  const handlePresetSelected = (presetId: string) => {
    if (pendingModeCategory) {
      const { setActivePreset } = usePresetStore.getState()
      setActivePreset(pendingModeCategory, presetId)
      
      // Now proceed with mode selection
      setModeCategory(pendingModeCategory)
      const agentMode = getAgentModeFromCategory(pendingModeCategory)
      setAgentMode((agentMode || 'ReAct') as 'simple' | 'ReAct' | 'orchestrator' | 'workflow')
      completeInitialSetup()
      
      // Close overlays
      setShowPresetSelection(false)
      setPendingModeCategory(null)
      onClose()
    }
  }

  // Handle preset selection overlay close
  const handlePresetSelectionClose = () => {
    setShowPresetSelection(false)
    setPendingModeCategory(null)
  }

  if (!isOpen) return null

  return (
    <>
      {/* Preset Selection Overlay */}
      {showPresetSelection && pendingModeCategory && (
        <PresetSelectionOverlay
          isOpen={showPresetSelection}
          onClose={handlePresetSelectionClose}
          onPresetSelected={handlePresetSelected}
          modeCategory={pendingModeCategory}
        />
      )}

      {/* Mode Selection Modal */}
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
        <div className="relative w-full max-w-3xl mx-4">
          {/* Header */}
          <div className="text-center mb-4">
            <h1 className="text-xl font-bold text-white mb-2">
              Choose Your AI Assistant Mode
            </h1>
            <p className="text-gray-300 text-sm">
              Select the mode that best fits your needs. You can always change this later.
            </p>
          </div>

          {/* Mode Cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            {/* Chat Mode */}
            <ModeCard
              category="chat"
              title="Chat Mode"
              description="Quick conversations with AI. Perfect for answering questions, brainstorming ideas, and having natural dialogues."
              icon={<MessageCircle className="w-5 h-5 text-blue-600" />}
              features={getModeInfoForModal('chat').features}
              exampleQueries={getModeInfoForModal('chat').examples}
              onSelect={() => handleModeSelect('chat')}
            />

            {/* Deep Research Mode */}
            <ModeCard
              category="deep-research"
              title="Deep Research Mode"
              description="Multi-step analysis with long-term memory. Ideal for complex research, detailed analysis, and comprehensive reports."
              icon={<Search className="w-5 h-5 text-blue-600" />}
              features={getModeInfoForModal('deep-research').features}
              exampleQueries={getModeInfoForModal('deep-research').examples}
              onSelect={() => handleModeSelect('deep-research')}
            />

            {/* Workflow Mode */}
            <ModeCard
              category="workflow"
              title="Workflow Mode"
              description="Todo-based execution with human verification. Perfect for structured tasks, project management, and step-by-step processes."
              icon={<Workflow className="w-5 h-5 text-blue-600" />}
              features={getModeInfoForModal('workflow').features}
              exampleQueries={getModeInfoForModal('workflow').examples}
              onSelect={() => handleModeSelect('workflow')}
            />
          </div>

          {/* Footer */}
          <div className="text-center mt-4">
            <p className="text-gray-400 text-xs">
              You can change your mode anytime from the sidebar
            </p>
          </div>
        </div>
      </div>
    </>
  )
}
