import React from 'react'
import { MessageCircle, Workflow, Users } from 'lucide-react'
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
    description: 'Structured task execution with step-by-step control, plus its own views for what ran, why, and how well.',
    features: [
      'Sequential, step-by-step execution with human verification',
      'Plan view — the live workflow plan and its changelog',
      'Report view — the agent-built dashboard of run outputs',
      'Pulse view — one agent-curated log of every run, with two verdicts: Bug (did it run right) and Goal (is it hitting its success criteria)',
      'Soul view — the workflow\'s objective and success criteria (its source of truth)',
      'Monitor — a per-run review that catches silent breakage and goal drift, and can notify you on Slack/WhatsApp',
      'Auto-improve — scheduled hardening and replan proposals that act on what Monitor finds',
      'Requires a Workflow/ folder for organization'
    ],
    examples: [],
    tips: []
  },
  'multi-agent': {
    icon: <Users className="w-16 h-16 text-indigo-500" />,
    title: 'Chief of Staff',
    description: 'Your operations hub — it runs your workflows, remembers what matters across them, and surfaces what needs your attention.',
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
