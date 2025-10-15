import React, { useState } from 'react'
import { MessageCircle, Search, Workflow, ChevronDown, ChevronUp, HelpCircle } from 'lucide-react'
import { useModeStore, type ModeCategory } from '../stores/useModeStore'

interface ModeInfoPanelProps {
  minimized?: boolean
}

const getModeIcon = (category: ModeCategory) => {
  switch (category) {
    case 'chat':
      return <MessageCircle className="w-5 h-5 text-blue-600" />
    case 'deep-research':
      return <Search className="w-5 h-5 text-blue-600" />
    case 'workflow':
      return <Workflow className="w-5 h-5 text-blue-600" />
    default:
      return <HelpCircle className="w-5 h-5 text-gray-400" />
  }
}

const getModeInfo = (category: ModeCategory) => {
  switch (category) {
    case 'chat':
      return {
        title: 'Chat Mode',
        description: 'Quick conversations with AI',
        features: [
          'Instant responses to questions',
          'Natural conversation flow',
          'Memory across the chat session',
          'Choose between Simple or ReAct reasoning'
        ],
        examples: [
          'Explain quantum computing in simple terms',
          'Help me write a professional email',
          'What are the latest trends in AI?',
          'Debug this code snippet'
        ],
        tips: [
          'Ask follow-up questions to dive deeper',
          'Use specific context for better responses',
          'Try both Simple and ReAct modes for different needs'
        ]
      }
    case 'deep-research':
      return {
        title: 'Deep Research Mode',
        description: 'Multi-step analysis with long-term memory',
        features: [
          'Multi-step planning and execution',
          'Long-term memory and context retention',
          'Comprehensive analysis and reporting',
          'Requires Tasks/ folder for organization'
        ],
        examples: [
          'Research the impact of AI on healthcare',
          'Analyze market trends for renewable energy',
          'Create a comprehensive business plan',
          'Investigate security vulnerabilities in a system'
        ],
        tips: [
          'Select or create a research preset first',
          'Ensure Tasks/ folder is available',
          'Be specific about your research goals',
          'Allow time for comprehensive analysis'
        ]
      }
    case 'workflow':
      return {
        title: 'Workflow Mode',
        description: 'Todo-based execution with human verification',
        features: [
          'Sequential task execution',
          'Human verification at each step',
          'Progress tracking and reporting',
          'Requires Workflow/ folder for organization'
        ],
        examples: [
          'Set up a new development environment',
          'Plan and execute a marketing campaign',
          'Create a data migration strategy',
          'Implement a new feature from scratch'
        ],
        tips: [
          'Select or create a workflow preset first',
          'Ensure Workflow/ folder is available',
          'Review and approve each step carefully',
          'Break complex tasks into smaller steps'
        ]
      }
    default:
      return {
        title: 'Unknown Mode',
        description: 'Please select a mode to get started',
        features: [],
        examples: [],
        tips: []
      }
  }
}

export const ModeInfoPanel: React.FC<ModeInfoPanelProps> = ({ minimized = false }) => {
  const { selectedModeCategory } = useModeStore()
  const [expanded, setExpanded] = useState(false)

  if (minimized) {
    return (
      <div className="p-2">
        <button
          onClick={() => setExpanded(!expanded)}
          className="w-full flex items-center justify-center p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
          title="Mode Information"
        >
          <HelpCircle className="w-4 h-4" />
        </button>
      </div>
    )
  }

  if (!selectedModeCategory) {
    return null
  }

  const modeInfo = getModeInfo(selectedModeCategory)

  return (
    <div className="bg-gray-50 dark:bg-slate-800 border border-gray-200 dark:border-slate-700 rounded-lg p-4">
      {/* Header */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          {getModeIcon(selectedModeCategory)}
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
            {modeInfo.title}
          </h3>
        </div>
        <button
          onClick={() => setExpanded(!expanded)}
          className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
        >
          {expanded ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
        </button>
      </div>

      {/* Description */}
      <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
        {modeInfo.description}
      </p>

      {/* Expanded Content */}
      {expanded && (
        <div className="space-y-4">
          {/* Features */}
          {modeInfo.features.length > 0 && (
            <div>
              <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                Key Features:
              </h4>
              <ul className="space-y-1">
                {modeInfo.features.map((feature, index) => (
                  <li key={index} className="flex items-start text-xs text-gray-600 dark:text-gray-400">
                    <div className="w-1.5 h-1.5 bg-blue-500 rounded-full mr-2 mt-1.5 flex-shrink-0" />
                    {feature}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Example Queries */}
          {modeInfo.examples.length > 0 && (
            <div>
              <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                Example Queries:
              </h4>
              <div className="space-y-1">
                {modeInfo.examples.map((example, index) => (
                  <div key={index} className="text-xs text-gray-500 dark:text-gray-400 italic bg-white dark:bg-slate-700 p-2 rounded border">
                    "{example}"
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Tips */}
          {modeInfo.tips.length > 0 && (
            <div>
              <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                Tips:
              </h4>
              <ul className="space-y-1">
                {modeInfo.tips.map((tip, index) => (
                  <li key={index} className="flex items-start text-xs text-gray-600 dark:text-gray-400">
                    <div className="w-1.5 h-1.5 bg-green-500 rounded-full mr-2 mt-1.5 flex-shrink-0" />
                    {tip}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Keyboard Shortcuts */}
          <div>
            <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
              Keyboard Shortcuts:
            </h4>
            <div className="text-xs text-gray-500 dark:text-gray-400 space-y-1">
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+1</kbd> Simple Mode</div>
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+2</kbd> ReAct Mode</div>
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+3</kbd> Deep Research</div>
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+4</kbd> Workflow Mode</div>
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+N</kbd> New Chat</div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
