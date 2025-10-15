import React from 'react'
import { MessageCircle, Search, Workflow, ArrowRight, Lightbulb } from 'lucide-react'
import { type ModeCategory } from '../stores/useModeStore'

interface ModeEmptyStateProps {
  modeCategory: ModeCategory | null
}

const getModeInfo = (category: ModeCategory | null) => {
  switch (category) {
    case 'chat':
      return {
        icon: <MessageCircle className="w-16 h-16 text-blue-500" />,
        title: 'Start a Conversation',
        description: 'Ask questions, brainstorm ideas, or have a natural dialogue with AI',
        examples: [
          'Explain quantum computing in simple terms',
          'Help me write a professional email',
          'What are the latest trends in AI?',
          'Debug this code snippet'
        ],
        tips: [
          'Be specific about what you need help with',
          'Ask follow-up questions to dive deeper',
          'Try both Simple and ReAct modes for different needs'
        ]
      }
    case 'deep-research':
      return {
        icon: <Search className="w-16 h-16 text-blue-500" />,
        title: 'Select a Research Preset',
        description: 'Choose a research preset to organize your analysis projects, or create a new one',
        examples: [
          'Research the impact of AI on healthcare',
          'Analyze market trends for renewable energy',
          'Create a comprehensive business plan',
          'Investigate security vulnerabilities'
        ],
        tips: [
          'Ensure Tasks/ folder is available',
          'Be specific about your research goals',
          'Allow time for comprehensive analysis',
          'Review generated reports carefully'
        ]
      }
    case 'workflow':
      return {
        icon: <Workflow className="w-16 h-16 text-blue-500" />,
        title: 'Select a Workflow Preset',
        description: 'Choose a workflow preset to organize your task execution, or create a new one',
        examples: [
          'Set up a new development environment',
          'Plan and execute a marketing campaign',
          'Create a data migration strategy',
          'Implement a new feature from scratch'
        ],
        tips: [
          'Ensure Workflow/ folder is available',
          'Review and approve each step carefully',
          'Break complex tasks into smaller steps',
          'Monitor progress throughout execution'
        ]
      }
    default:
      return {
        icon: <Lightbulb className="w-16 h-16 text-gray-400" />,
        title: 'Welcome to AI Assistant',
        description: 'Select a mode to get started with your AI-powered workflow',
        examples: [],
        tips: []
      }
  }
}

export const ModeEmptyState: React.FC<ModeEmptyStateProps> = ({ modeCategory }) => {
  const modeInfo = getModeInfo(modeCategory)

  return (
    <div className="flex flex-col items-center justify-center h-full p-8 text-center">
      {/* Icon */}
      <div className="mb-6">
        {modeInfo.icon}
      </div>

      {/* Title */}
      <h3 className="text-2xl font-bold text-gray-900 dark:text-white mb-3">
        {modeInfo.title}
      </h3>

      {/* Description */}
      <p className="text-gray-600 dark:text-gray-400 mb-8 max-w-md">
        {modeInfo.description}
      </p>

      {/* Examples */}
      {modeInfo.examples.length > 0 && (
        <div className="mb-8 w-full max-w-lg">
          <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-4">
            Example Queries:
          </h4>
          <div className="grid grid-cols-1 gap-2">
            {modeInfo.examples.map((example, index) => (
              <div
                key={index}
                className="text-sm text-gray-500 dark:text-gray-400 italic bg-gray-50 dark:bg-gray-800 p-3 rounded-lg border border-gray-200 dark:border-gray-700"
              >
                "{example}"
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Tips */}
      {modeInfo.tips.length > 0 && (
        <div className="w-full max-w-lg">
          <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-4">
            Tips for Success:
          </h4>
          <div className="space-y-2">
            {modeInfo.tips.map((tip, index) => (
              <div key={index} className="flex items-start text-sm text-gray-600 dark:text-gray-400">
                <div className="w-1.5 h-1.5 bg-blue-500 rounded-full mr-3 mt-2 flex-shrink-0" />
                {tip}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Action Hint */}
      {modeCategory && (
        <div className="mt-8 flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
          <ArrowRight className="w-4 h-4" />
          <span>
            {modeCategory === 'chat' 
              ? 'Type your message below to get started'
              : 'Select a preset from the sidebar to begin'
            }
          </span>
        </div>
      )}
    </div>
  )
}
