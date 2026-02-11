import { useState, useEffect } from 'react'
import { Loader2, Save, AlertCircle } from 'lucide-react'
import type { SubAgent } from '../../types/subagents'

interface SubAgentEditorProps {
  subagent: SubAgent
  onClose: () => void
  onSave: (folderName: string, content: string) => Promise<void>
}

export default function SubAgentEditor({ subagent, onClose, onSave }: SubAgentEditorProps) {
  const [content, setContent] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isDirty, setIsDirty] = useState(false)

  // Reconstruct the full SUBAGENT.md content from frontmatter and content
  useEffect(() => {
    const frontmatterYaml = [
      '---',
      `name: ${subagent.frontmatter.name}`,
      `description: ${subagent.frontmatter.description}`,
    ]

    if (subagent.frontmatter.default_reasoning_level) {
      frontmatterYaml.push(`default_reasoning_level: ${subagent.frontmatter.default_reasoning_level}`)
    }

    if (subagent.frontmatter.default_tool_mode) {
      frontmatterYaml.push(`default_tool_mode: ${subagent.frontmatter.default_tool_mode}`)
    }

    if (subagent.frontmatter.skills) {
      frontmatterYaml.push(`skills: ${subagent.frontmatter.skills}`)
    }

    if (subagent.frontmatter.servers) {
      frontmatterYaml.push(`servers: ${subagent.frontmatter.servers}`)
    }

    frontmatterYaml.push('---')
    frontmatterYaml.push('')

    const fullContent = frontmatterYaml.join('\n') + (subagent.content || '')
    setContent(fullContent)
  }, [subagent])

  const handleSave = async () => {
    setIsSaving(true)
    setError(null)

    try {
      await onSave(subagent.folder_name, content)
      setIsDirty(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save template')
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
              Edit Template: {subagent.frontmatter.name}
            </h3>
            <p className="text-sm text-gray-500 dark:text-gray-400">
              {subagent.file_path}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleSave}
              disabled={isSaving || !isDirty}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
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
            className="w-full h-full px-4 py-3 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 font-mono text-sm resize-none focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            placeholder={`---
name: my-subagent
description: A specialized sub-agent for...
default_reasoning_level: medium
default_tool_mode: simple
skills: skill-1, skill-2
servers: server-1
---

# Instructions
You are a specialized agent for...`}
          />
        </div>

        {/* Footer Help */}
        <div className="px-6 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/50">
          <div className="text-xs text-gray-500 dark:text-gray-400">
            <p className="font-medium mb-1">SUBAGENT.md Format:</p>
            <p>
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">name</span> and{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">description</span> are required.
              Optional:{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">default_reasoning_level</span>,{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">default_tool_mode</span>,{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">skills</span>,{' '}
              <span className="font-mono bg-gray-200 dark:bg-gray-700 px-1 rounded">servers</span>
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
