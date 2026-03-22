import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { X, FileText, RefreshCw, Copy, Check, Wand2, AlertCircle, Loader2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { WorkflowFinalOutputConfig, WorkflowFinalOutputResponse } from '../../services/api-types'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

interface FinalOutputPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  selectedRunFolder: string | null
  runFolders: string[]
  workflowTitle?: string
}

const FinalOutputPopup: React.FC<FinalOutputPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  selectedRunFolder,
  runFolders,
  workflowTitle
}) => {
  const [activeRunFolder, setActiveRunFolder] = useState('')
  const [loading, setLoading] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [data, setData] = useState<WorkflowFinalOutputResponse | null>(null)
  const [copied, setCopied] = useState(false)

  const availableRunFolders = useMemo(() => {
    return runFolders.filter(folder => folder.includes('/')).sort()
  }, [runFolders])

  useEffect(() => {
    if (!isOpen) return

    const preferredRunFolder = selectedRunFolder && selectedRunFolder.includes('/')
      ? selectedRunFolder
      : availableRunFolders[0] || ''
    setActiveRunFolder(preferredRunFolder)
    setError(null)
    setData(null)
    setCopied(false)
  }, [isOpen, selectedRunFolder, availableRunFolders])

  const loadOutput = useCallback(async (runFolder: string) => {
    if (!workspacePath || !runFolder) return

    setLoading(true)
    setError(null)
    try {
      const response = await agentApi.getFinalOutput(workspacePath, runFolder)
      setData(response)
    } catch (err) {
      console.error('Failed to load final output:', err)
      setError('Failed to load final output')
      setData(null)
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => {
    if (isOpen && workspacePath && activeRunFolder) {
      loadOutput(activeRunFolder)
    }
  }, [activeRunFolder, isOpen, workspacePath, loadOutput])

  const handleGenerate = async () => {
    if (!workspacePath || !activeRunFolder) return

    setGenerating(true)
    setError(null)
    try {
      const response = await agentApi.generateFinalOutput(workspacePath, activeRunFolder, workflowTitle)
      setData(response)
    } catch (err) {
      console.error('Failed to generate final output:', err)
      setError('Failed to generate final output')
    } finally {
      setGenerating(false)
    }
  }

  const handleCopy = async () => {
    const content = data?.content || ''
    if (!content) return
    try {
      await navigator.clipboard.writeText(content)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy final output:', err)
    }
  }

  if (!isOpen) return null

  const config: WorkflowFinalOutputConfig | null = data?.config || null
  const isConfigEnabled = config?.enabled !== false
  const hasGroupScopedSelection = Boolean(activeRunFolder && activeRunFolder.includes('/'))
  const hasContent = Boolean(data?.exists && data?.content)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-6xl max-h-[90vh] flex flex-col border border-border relative">
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <div className="min-w-0">
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <FileText className="w-5 h-5 text-primary" />
              Final Report
            </h2>
            <p className="text-sm text-muted-foreground mt-1">
              Review or regenerate the markdown report for a specific iteration/group run.
            </p>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-6 py-3 border-b border-border bg-muted/30 flex items-center gap-3 flex-wrap">
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Run</span>
            <select
              value={activeRunFolder}
              onChange={(e) => setActiveRunFolder(e.target.value)}
              className="min-w-[280px] rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
            >
              {!activeRunFolder && <option value="">Select iteration/group</option>}
              {availableRunFolders.map(folder => (
                <option key={folder} value={folder}>{folder}</option>
              ))}
            </select>
          </div>

          <div className="flex items-center gap-2 ml-auto">
            <button
              onClick={() => activeRunFolder && loadOutput(activeRunFolder)}
              disabled={!workspacePath || !activeRunFolder || loading}
              className="inline-flex items-center gap-1.5 px-3 py-2 rounded-md text-sm bg-muted text-muted-foreground hover:bg-muted/80 transition-colors disabled:opacity-50"
            >
              {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
              Refresh
            </button>
            <button
              onClick={handleGenerate}
              disabled={!workspacePath || !hasGroupScopedSelection || !isConfigEnabled || generating}
              className="inline-flex items-center gap-1.5 px-3 py-2 rounded-md text-sm bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {generating ? <Loader2 className="w-4 h-4 animate-spin" /> : <Wand2 className="w-4 h-4" />}
              Generate
            </button>
            <button
              onClick={handleCopy}
              disabled={!hasContent}
              className="inline-flex items-center gap-1.5 px-3 py-2 rounded-md text-sm bg-muted text-muted-foreground hover:bg-muted/80 transition-colors disabled:opacity-50"
            >
              {copied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
              {copied ? 'Copied' : 'Copy'}
            </button>
          </div>
        </div>

        <div className="px-6 py-3 border-b border-border">
          <div className="flex items-center justify-between gap-4 flex-wrap">
            <div className="min-w-0">
              <div className="text-sm font-medium text-foreground">
                {config?.title?.trim() || 'Workflow final report'}
              </div>
              <div className="text-xs text-muted-foreground mt-1">
                File: {config?.output_filename || 'final_output.md'}
              </div>
            </div>
            <div className={`text-xs px-2 py-1 rounded-md ${isConfigEnabled ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300'}`}>
              {isConfigEnabled ? 'Enabled' : 'Disabled'}
            </div>
          </div>
          {config?.instructions?.trim() && (
            <p className="text-xs text-muted-foreground mt-3 whitespace-pre-wrap">
              {config.instructions.trim()}
            </p>
          )}
        </div>

        <div className="flex-1 overflow-y-auto p-6">
          {!workspacePath && (
            <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
              Select a workflow workspace to view final outputs.
            </div>
          )}

          {workspacePath && !availableRunFolders.length && (
            <div className="h-full flex items-center justify-center">
              <div className="max-w-md text-center space-y-3">
                <AlertCircle className="w-8 h-8 text-muted-foreground mx-auto" />
                <div className="text-sm font-medium text-foreground">No group runs found</div>
                <div className="text-sm text-muted-foreground">
                  Final output is generated per iteration/group run. Start a workflow run first, then come back here to review the generated markdown.
                </div>
              </div>
            </div>
          )}

          {workspacePath && availableRunFolders.length > 0 && !hasGroupScopedSelection && (
            <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
              Select an iteration/group run to view its final output.
            </div>
          )}

          {workspacePath && hasGroupScopedSelection && !isConfigEnabled && (
            <div className="h-full flex items-center justify-center">
              <div className="max-w-md text-center space-y-3">
                <AlertCircle className="w-8 h-8 text-yellow-600 dark:text-yellow-400 mx-auto" />
                <div className="text-sm font-medium text-foreground">Report plan is missing or disabled</div>
                <div className="text-sm text-muted-foreground">
                  Configure the single report step in `planning/output_plan.json` through workflow builder chat tools like `add_output_step` or `update_output_step`, then completed group runs will generate this artifact automatically.
                </div>
              </div>
            </div>
          )}

          {workspacePath && hasGroupScopedSelection && isConfigEnabled && loading && (
            <div className="h-full flex items-center justify-center">
              <Loader2 className="w-6 h-6 animate-spin text-primary" />
            </div>
          )}

          {workspacePath && hasGroupScopedSelection && isConfigEnabled && !loading && error && (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
              {error}
            </div>
          )}

          {workspacePath && hasGroupScopedSelection && isConfigEnabled && !loading && !error && !hasContent && (
            <div className="h-full flex items-center justify-center">
              <div className="max-w-md text-center space-y-3">
                <FileText className="w-8 h-8 text-muted-foreground mx-auto" />
                <div className="text-sm font-medium text-foreground">No final report generated yet</div>
                <div className="text-sm text-muted-foreground">
                  Generate the markdown artifact for <span className="font-mono">{activeRunFolder}</span> to review what the workflow did for this group run.
                </div>
              </div>
            </div>
          )}

          {workspacePath && hasGroupScopedSelection && isConfigEnabled && !loading && !error && hasContent && (
            <div className="rounded-lg border border-border bg-card p-4">
              <MarkdownRenderer content={data?.content || ''} className="max-w-none" showScrollbar={true} />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export default FinalOutputPopup
