import React from 'react'
import { MessageCircle, Search, Workflow, Lightbulb } from 'lucide-react'
import { type ModeCategory } from '../stores/useModeStore'

export interface ModeInfo {
  icon: React.ReactNode
  title: string
  description: string
  features: string[]
  examples: string[]
  tips: string[]
}

export const MODE_INFO: Record<Exclude<ModeCategory, null>, ModeInfo> = {
  'chat': {
    icon: <MessageCircle className="w-16 h-16 text-blue-500" />,
    title: 'Start a Conversation',
    description: 'Ask questions, brainstorm ideas, or have a natural dialogue with AI',
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
      'Be specific about what you need help with',
      'Ask follow-up questions to dive deeper',
      'Try both Simple and ReAct modes for different needs'
    ]
  },
  'deep-research': {
    icon: <Search className="w-16 h-16 text-blue-500" />,
    title: 'Select a Research Preset',
    description: 'Choose a research preset to organize your analysis projects, or create a new one',
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
      'Investigate security vulnerabilities'
    ],
    tips: [
      'Ensure Tasks/ folder is available',
      'Be specific about your research goals',
      'Allow time for comprehensive analysis',
      'Review generated reports carefully'
    ]
  },
  'workflow': {
    icon: <Workflow className="w-16 h-16 text-blue-500" />,
    title: 'Select a Workflow Preset',
    description: 'Choose a workflow preset to organize your task execution, or create a new one',
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
      'Ensure Workflow/ folder is available',
      'Review and approve each step carefully',
      'Break complex tasks into smaller steps',
      'Monitor progress throughout execution'
    ]
  }
}

export const getModeInfo = (category: ModeCategory | null): ModeInfo => {
  if (!category || !MODE_INFO[category]) {
    return {
      icon: <Lightbulb className="w-16 h-16 text-gray-400" />,
      title: 'Welcome to AI Assistant',
      description: 'Select a mode to get started with your AI-powered workflow',
      features: [],
      examples: [],
      tips: []
    }
  }
  
  return MODE_INFO[category]
}

// Helper functions for different display contexts
export const getModeInfoForModal = (category: Exclude<ModeCategory, null>) => {
  const info = MODE_INFO[category]
  return {
    title: info.title.replace('Start a ', '').replace('Select a ', ''),
    description: info.description,
    features: info.features,
    examples: info.examples.slice(0, 3), // Limit for modal display
    icon: <MessageCircle className="w-5 h-5 text-blue-600" /> // Will be overridden per component
  }
}

export const getModeInfoForPanel = (category: Exclude<ModeCategory, null>) => {
  const info = MODE_INFO[category]
  return {
    title: info.title.replace('Start a ', '').replace('Select a ', ''),
    description: info.description,
    features: info.features,
    examples: info.examples,
    tips: info.tips
  }
}
