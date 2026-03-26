import React from 'react'
import { Building2, MessageCircle, Workflow, Users } from 'lucide-react'
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
  },
  'multi-agent': {
    icon: <Users className="w-16 h-16 text-indigo-500" />,
    title: 'Multi Agent Chat',
    description: 'Delegate complex tasks to a team of AI sub-agents in autonomous spawn mode, with optional planning when needed',
    features: [
      'Autonomous spawn delegation',
      'Optional planner usage',
      'Multi-LLM tier support',
      'Auto tool mode per task',
      'Sub-agent tracking in Chats/ folders'
    ],
    examples: [],
    tips: []
  },
  'organization': {
    icon: <Building2 className="w-16 h-16 text-emerald-500" />,
    title: 'Organization Assistant',
    description: 'Manage employees, workflow ownership, schedules, and run outputs in a dedicated organization workspace',
    features: [
      'Dedicated organization assistant thread',
      'Employee and assignment management',
      'Schedule and run visibility',
      'Separate from multi-agent delegation chat'
    ],
    examples: [],
    tips: []
  },
}

export const getModeInfo = (category: ModeCategory | null): ModeInfo => {
  if (!category || !MODE_INFO[category]) {
    return {
      icon: <MessageCircle className="w-16 h-16 text-gray-400" />,
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
