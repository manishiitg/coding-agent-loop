import { useState, useEffect } from 'react'
import { Loader2, Save, AlertCircle } from 'lucide-react'
import type { Skill } from '../../types/skills'

interface SkillEditorProps {
  skill: Skill
  onClose: () => void
  onSave: (folderName: string, content: string) => Promise<void>
}

export default function SkillEditor({ skill, onClose, onSave }: SkillEditorProps) {
  const [content, setContent] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isDirty, setIsDirty] = useState(false)

  // Reconstruct the full SKILL.md content from frontmatter and content
  useEffect(() => {
    const frontmatterYaml = [
      '---',
      `name: ${skill.frontmatter.name}`,
      `description: ${skill.frontmatter.description}`,
    ]

    if (skill.frontmatter.argument_hint) {
      frontmatterYaml.push(`argument-hint: ${skill.frontmatter.argument_hint}`)
    }

    if (skill.frontmatter.allowed_tools && skill.frontmatter.allowed_tools.length > 0) {
      frontmatterYaml.push(`allowed-tools: [${skill.frontmatter.allowed_tools.map(t => `"${t}"`).join(', ')}]`)
    }

    if (skill.frontmatter.model) {
      frontmatterYaml.push(`model: ${skill.frontmatter.model}`)
    }

    frontmatterYaml.push('---')
    frontmatterYaml.push('')

    const fullContent = frontmatterYaml.join('\n') + (skill.content || '')
    setContent(fullContent)
  }, [skill])

  const handleSave = async () => {
    setIsSaving(true)
    setError(null)

    try {
      await onSave(skill.folder_name, content)
      setIsDirty(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save skill')
    } finally {
      setIsSaving(false)
    }
  }

  const handleClose = () => {
    if (isDirty && !confirm('You have unsaved changes. Are you sure you want to close?')) {
      return
    }
    onClose()
  }

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-xl w-full max-w-4xl h-[85vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
          <div>
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              Edit Skill: {skill.frontmatter.name}
            </h3>
            <p className="text-sm text-gray-500 dark:text-gray-400">
              {skill.file_path}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleSave}
              disabled={isSaving || !isDirty}
              className="px-4 py-2 text-sm font-medium text-white bg-purple-600 hover:bg-purple-700 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
            >
              {isSaving ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <Save className="w-4 h-4" />
                  Save
                </>
              )}
            </button>
            <button
              onClick={handleClose}
              className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 text-xl px-2"
            >
              ✕
            </button>
          </div>
        </div>

        {/* Error Message */}
        {error && (
          <div className="mx-6 mt-4 flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-md">
            <AlertCircle className="w-5 h-5 text-red-500 flex-shrink-0" />
            <span className="text-sm text-red-700 dark:text-red-300">{error}</span>
          </div>
        )}

        {/* Editor */}
        <div className="flex-1 p-6 overflow-hidden">
          <textarea
            value={content}
            onChange={(e) => {
              setContent(e.target.value)
              setIsDirty(true)
            }}
            className="w-full h-full px-4 py-3 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 font-mono text-sm resize-none focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
            placeholder="---
name: my-skill
description: A description of what the skill does
argument-hint: <file-path>
allowed-tools: [&quot;read_workspace_file&quot;]
---

Your skill instructions here..."
          />
        </div>

        {/* Footer Help */}
        <div className="px-6 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/50">
          <div className="text-xs text-gray-500 dark:text-gray-400">
            <p className="font-medium mb-1">SKILL.md Format:</p>
            <p>
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">name</span> and{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">description</span> are required.
              Optional fields:{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">argument-hint</span>,{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">allowed-tools</span>,{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">model</span>
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
