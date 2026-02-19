import React, { useState, useEffect, useCallback } from 'react'
import { X, Zap, Eye, Code, FileText, MessageCircle, Search, Bookmark, Star, Terminal } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { commandsApi } from '../../api/commands'
import { loadAndRegisterUserCommands } from '../../commands'

const ICON_OPTIONS = [
  { name: 'terminal', icon: <Terminal className="w-5 h-5" /> },
  { name: 'zap', icon: <Zap className="w-5 h-5" /> },
  { name: 'eye', icon: <Eye className="w-5 h-5" /> },
  { name: 'code', icon: <Code className="w-5 h-5" /> },
  { name: 'file-text', icon: <FileText className="w-5 h-5" /> },
  { name: 'message-circle', icon: <MessageCircle className="w-5 h-5" /> },
  { name: 'search', icon: <Search className="w-5 h-5" /> },
  { name: 'bookmark', icon: <Bookmark className="w-5 h-5" /> },
  { name: 'star', icon: <Star className="w-5 h-5" /> },
]

const MODE_OPTIONS = [
  { value: 'chat', label: 'Chat' },
  { value: 'multi-agent', label: 'Multi-Agent' },
  { value: 'workflow', label: 'Workflow' },
]

interface CommandEditorDialogProps {
  isOpen: boolean
  onClose: () => void
  editingCommand?: {
    folder_name: string
    frontmatter: { name: string; description: string; icon?: string; modes?: string[] }
    content: string
  } | null
  onSaved?: () => void
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9-_]/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '')
}

function buildCommandMd(name: string, description: string, icon: string, modes: string[], promptTemplate: string): string {
  let yaml = `---\nname: ${name}\ndescription: ${description}\nicon: ${icon}\n`
  if (modes.length > 0) {
    yaml += `modes:\n${modes.map(m => `  - ${m}`).join('\n')}\n`
  } else {
    yaml += `modes: []\n`
  }
  yaml += `---\n\n${promptTemplate}`
  return yaml
}

export const CommandEditorDialog: React.FC<CommandEditorDialogProps> = ({
  isOpen,
  onClose,
  editingCommand,
  onSaved
}) => {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [icon, setIcon] = useState('terminal')
  const [modes, setModes] = useState<string[]>([])
  const [promptTemplate, setPromptTemplate] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isEditing = !!editingCommand

  useEffect(() => {
    if (isOpen && editingCommand) {
      setName(editingCommand.frontmatter.name)
      setDescription(editingCommand.frontmatter.description)
      setIcon(editingCommand.frontmatter.icon || 'terminal')
      setModes(editingCommand.frontmatter.modes || [])
      setPromptTemplate(editingCommand.content)
    } else if (isOpen) {
      setName('')
      setDescription('')
      setIcon('terminal')
      setModes([])
      setPromptTemplate('')
    }
    setError(null)
  }, [isOpen, editingCommand])

  const handleBackdropClick = useCallback((e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose()
  }, [onClose])

  const toggleMode = useCallback((mode: string) => {
    setModes(prev => prev.includes(mode) ? prev.filter(m => m !== mode) : [...prev, mode])
  }, [])

  const handleSave = useCallback(async () => {
    if (!name.trim()) { setError('Name is required'); return }
    if (!description.trim()) { setError('Description is required'); return }
    if (!promptTemplate.trim()) { setError('Prompt template is required'); return }

    setSaving(true)
    setError(null)

    try {
      const folderName = isEditing ? editingCommand!.folder_name : slugify(name)
      const content = buildCommandMd(name.trim(), description.trim(), icon, modes, promptTemplate.trim())

      if (isEditing) {
        await commandsApi.updateCommand(folderName, { content })
      } else {
        await commandsApi.createCommand({ name: folderName, content })
      }

      await loadAndRegisterUserCommands()
      onSaved?.()
      onClose()
    } catch (err: any) {
      setError(err?.response?.data || err?.message || 'Failed to save command')
    } finally {
      setSaving(false)
    }
  }, [name, description, icon, modes, promptTemplate, isEditing, editingCommand, onClose, onSaved])

  useEffect(() => {
    if (!isOpen) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
      onClick={handleBackdropClick}
    >
      <Card
        className="w-full max-w-lg mx-4 p-6 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-lg font-semibold">
            {isEditing ? 'Edit Command' : 'Create Custom Command'}
          </h2>
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground">
            <X className="w-5 h-5" />
          </button>
        </div>

        {error && (
          <div className="mb-4 p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-sm text-red-600 dark:text-red-400">
            {error}
          </div>
        )}

        <div className="space-y-4">
          {/* Name */}
          <div>
            <label className="block text-sm font-medium mb-1">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="quick-review"
              disabled={isEditing}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm focus:outline-none focus:ring-2 focus:ring-primary/50 disabled:opacity-50"
            />
            {!isEditing && name && (
              <p className="text-xs text-muted-foreground mt-1">Folder: {slugify(name)}</p>
            )}
          </div>

          {/* Description */}
          <div>
            <label className="block text-sm font-medium mb-1">Description</label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Review current code changes"
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
            />
          </div>

          {/* Icon */}
          <div>
            <label className="block text-sm font-medium mb-1">Icon</label>
            <div className="flex flex-wrap gap-2">
              {ICON_OPTIONS.map(opt => (
                <button
                  key={opt.name}
                  onClick={() => setIcon(opt.name)}
                  className={`p-2 rounded-md border transition-colors ${
                    icon === opt.name
                      ? 'border-primary bg-primary/10 text-primary'
                      : 'border-border hover:bg-secondary text-muted-foreground'
                  }`}
                  title={opt.name}
                >
                  {opt.icon}
                </button>
              ))}
            </div>
          </div>

          {/* Modes */}
          <div>
            <label className="block text-sm font-medium mb-1">
              Visible in modes
              <span className="text-muted-foreground font-normal ml-1">(empty = all)</span>
            </label>
            <div className="flex flex-wrap gap-2">
              {MODE_OPTIONS.map(opt => (
                <button
                  key={opt.value}
                  onClick={() => toggleMode(opt.value)}
                  className={`px-3 py-1 rounded-full text-sm border transition-colors ${
                    modes.includes(opt.value)
                      ? 'border-primary bg-primary/10 text-primary'
                      : 'border-border hover:bg-secondary text-muted-foreground'
                  }`}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>

          {/* Prompt Template */}
          <div>
            <label className="block text-sm font-medium mb-1">
              Prompt template
            </label>
            <textarea
              value={promptTemplate}
              onChange={(e) => setPromptTemplate(e.target.value)}
              placeholder={'Review my current code changes for bugs, security issues, and performance problems.\n\n{{context}}'}
              rows={6}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm focus:outline-none focus:ring-2 focus:ring-primary/50 resize-y font-mono"
            />
            <p className="text-xs text-muted-foreground mt-1">
              Use <code className="px-1 bg-secondary rounded">{'{{context}}'}</code> to include text typed before the slash command.
            </p>
          </div>
        </div>

        {/* Actions */}
        <div className="flex justify-end gap-2 mt-6">
          <Button variant="outline" onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? 'Saving...' : isEditing ? 'Update' : 'Create'}
          </Button>
        </div>
      </Card>
    </div>
  )
}

export default CommandEditorDialog
