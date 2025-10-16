import { MessageCircle, Search, Workflow, Settings } from 'lucide-react'
import { type ModeCategory } from '../stores/useModeStore'

/**
 * Get the appropriate icon for a mode category
 * @param category - The mode category
 * @param className - Optional className to apply to the icon (defaults to "w-6 h-6 text-blue-600")
 * @returns JSX element with the appropriate icon
 */
export const getModeIcon = (category: ModeCategory, className?: string) => {
  const defaultClassName = className || "w-6 h-6 text-blue-600"
  
  switch (category) {
    case 'chat':
      return <MessageCircle className={defaultClassName} />
    case 'deep-research':
      return <Search className={defaultClassName} />
    case 'workflow':
      return <Workflow className={defaultClassName} />
    default:
      return <Settings className="w-6 h-6 text-gray-400" />
  }
}

/**
 * Get the display name for a mode category
 * @param category - The mode category
 * @returns The display name string
 */
export const getModeName = (category: ModeCategory) => {
  switch (category) {
    case 'chat':
      return 'Chat Mode'
    case 'deep-research':
      return 'Deep Research Mode'
    case 'workflow':
      return 'Workflow Mode'
    default:
      return 'Unknown Mode'
  }
}

/**
 * Get the description for a mode category
 * @param category - The mode category
 * @returns The description string
 */
export const getModeDescription = (category: ModeCategory) => {
  switch (category) {
    case 'chat':
      return 'Quick conversations with AI'
    case 'deep-research':
      return 'Multi-step analysis with long-term memory'
    case 'workflow':
      return 'Todo-based execution with human verification'
    default:
      return 'Unknown mode'
  }
}
