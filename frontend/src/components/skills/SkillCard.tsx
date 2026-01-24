import { Trash2, Wrench, FileText, ExternalLink } from 'lucide-react'
import type { Skill } from '../../types/skills'

interface SkillCardProps {
  skill: Skill
  onDelete: () => void
}

export default function SkillCard({ skill, onDelete }: SkillCardProps) {
  const { frontmatter, folder_name, source_url } = skill

  return (
    <div className="bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
      <div className="flex items-start justify-between">
        <div className="flex-1">
          <div className="flex items-center gap-2 mb-2">
            <div className="w-3 h-3 rounded-full bg-gradient-to-r from-purple-500 to-pink-500"></div>
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

          {/* Metadata */}
          <div className="flex flex-wrap gap-2 text-xs">
            {frontmatter.argument_hint && (
              <span className="flex items-center gap-1 px-2 py-1 bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 rounded">
                <FileText className="w-3 h-3" />
                {frontmatter.argument_hint}
              </span>
            )}

            {frontmatter.allowed_tools && frontmatter.allowed_tools.length > 0 && (
              <span className="flex items-center gap-1 px-2 py-1 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded">
                <Wrench className="w-3 h-3" />
                {frontmatter.allowed_tools.length} tool{frontmatter.allowed_tools.length !== 1 ? 's' : ''}
              </span>
            )}

            {frontmatter.model && (
              <span className="px-2 py-1 bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300 rounded">
                {frontmatter.model}
              </span>
            )}
          </div>

        </div>

        <div className="flex items-center gap-1 ml-4">
          <button
            onClick={onDelete}
            className="p-2 text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400 transition-colors"
            title="Delete skill"
          >
            <Trash2 className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Allowed Tools Detail */}
      {frontmatter.allowed_tools && frontmatter.allowed_tools.length > 0 && (
        <div className="mt-3 pt-3 border-t border-gray-200 dark:border-gray-700">
          <div className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">
            Allowed Tools:
          </div>
          <div className="flex flex-wrap gap-1">
            {frontmatter.allowed_tools.map((tool) => (
              <span
                key={tool}
                className="px-2 py-0.5 bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300 text-xs rounded font-mono"
              >
                {tool}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Source URL */}
      {source_url && (
        <div className="mt-3 pt-3 border-t border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-2 text-xs">
            <span className="font-medium text-gray-600 dark:text-gray-400">Source:</span>
            <a
              href={source_url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-blue-600 dark:text-blue-400 hover:underline truncate flex items-center gap-1"
            >
              {source_url}
              <ExternalLink className="w-3 h-3 flex-shrink-0" />
            </a>
          </div>
        </div>
      )}
    </div>
  )
}
