import { Trash2, Cpu } from 'lucide-react'
import type { SubAgent } from '../../types/subagents'

interface SubAgentCardProps {
  subagent: SubAgent
  onDelete: () => void
}

export default function SubAgentCard({ subagent, onDelete }: SubAgentCardProps) {
  const { frontmatter, folder_name } = subagent

  return (
    <div className="bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
      <div className="flex items-start justify-between">
        <div className="flex-1">
          <div className="flex items-center gap-2 mb-2">
            <div className="w-3 h-3 rounded-full bg-gradient-to-r from-blue-500 to-cyan-500"></div>
            <h4 className="text-sm font-semibold text-gray-900 dark:text-gray-100">
              {frontmatter.name}
            </h4>
            <span className="text-xs text-gray-500 dark:text-gray-400 bg-gray-200 dark:bg-gray-700 px-2 py-0.5 rounded">
              {folder_name}
            </span>
          </div>

          <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
            {frontmatter.description}
          </p>

          {/* Metadata badges */}
          <div className="flex flex-wrap gap-2 text-xs">
            {frontmatter.default_reasoning_level && (
              <span className="flex items-center gap-1 px-2 py-1 bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 rounded">
                <Cpu className="w-3 h-3" />
                {frontmatter.default_reasoning_level}
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-1 ml-4">
          <button
            onClick={onDelete}
            className="p-2 text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400 transition-colors"
            title="Delete sub-agent template"
          >
            <Trash2 className="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  )
}
