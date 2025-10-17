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
    title: 'Chat Mode',
    description: 'Ask questions, brainstorm ideas, or have a natural dialogue with AI',
    features: [
      'Instant responses to questions',
      'Natural conversation flow',
      'Memory across the chat session',
      'Choose between Simple or ReAct reasoning'
    ],
    examples: [],
    tips: []
  },
  'deep-research': {
    icon: <Search className="w-16 h-16 text-blue-500" />,
    title: 'Deep Research Mode',
    description: 'Advanced planning and execution for comprehensive analysis',
    features: [
      'Multi-step planning and execution',
      'Long-term memory and context retention',
      'Comprehensive analysis and reporting',
      'Requires Tasks/ folder for organization'
    ],
    examples: [],
    tips: []
  },
  'workflow': {
    icon: <Workflow className="w-16 h-16 text-blue-500" />,
    title: 'Workflow Mode',
    description: 'Structured task execution with step-by-step control',
    features: [
      'Sequential task execution',
      'Human verification at each step',
      'Progress tracking and reporting',
      'Requires Workflow/ folder for organization'
    ],
    examples: [],
    tips: []
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
