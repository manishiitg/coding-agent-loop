import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { X, FileText, RefreshCw, Copy, Check, Wand2, AlertCircle, Loader2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { VariablesManifest, WorkflowFinalOutputConfig, WorkflowFinalOutputResponse } from '../../services/api-types'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { extractGroupNameFromFolder, extractIterationFolder } from '../../utils/workflowUtils'

interface FinalOutputPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  selectedRunFolder: string | null
  runFolders: string[]
  variablesManifest?: VariablesManifest | null
  workflowTitle?: string
  onRunReport?: (runFolder: string) => Promise<void>
}

const FinalOutputPopup: React.FC<FinalOutputPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  selectedRunFolder,
  runFolders,
  variablesManifest,
  workflowTitle,
  onRunReport,
}) => {
  const extractErrorMessage = (err: unknown, fallback: string): string => {
    const responseData = (err as any)?.response?.data
    if (typeof responseData === 'string' && responseData.trim()) {
      return responseData.trim()
    }
    if (responseData && typeof responseData === 'object') {
      if (typeof responseData.error === 'string' && responseData.error.trim()) {
        return responseData.error.trim()
      }
      if (typeof responseData.message === 'string' && responseData.message.trim()) {
        return responseData.message.trim()
      }
    }
    if (err instanceof Error && err.message.trim()) {
      return err.message.trim()
    }
    return fallback
  }

  const [selectedGroupKey, setSelectedGroupKey] = useState('')
  const [loading, setLoading] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [data, setData] = useState<WorkflowFinalOutputResponse | null>(null)
  const [copied, setCopied] = useState(false)

  const runEntries = useMemo(() => {
    const uniqueFolders = Array.from(new Set(runFolders.filter(folder => {
      if (!folder.includes('/')) return false
      return folder.split('/').length === 2
    })))

    const entries = uniqueFolders.map(runFolder => {
      const iteration = extractIterationFolder(runFolder) || ''
      const groupFolderName = runFolder.split('/')[1] || ''
      const groupName = extractGroupNameFromFolder(runFolder, variablesManifest)
      const manifestGroup = groupName
        ? variablesManifest?.groups?.find(group => group.name === groupName)
        : undefined

      return {
        runFolder,
        iteration,
        groupKey: groupName || groupFolderName,
        groupLabel: manifestGroup?.name || groupFolderName || groupName || 'Unknown group',
      }
    })

    return entries.sort((a, b) => {
      const iterA = parseInt(a.iteration.replace('iteration-', ''), 10) || 0
      const iterB = parseInt(b.iteration.replace('iteration-', ''), 10) || 0
      if (a.groupLabel !== b.groupLabel) {
        return a.groupLabel.localeCompare(b.groupLabel)
      }
      return iterB - iterA
    })
  }, [runFolders, variablesManifest])

  const groupOptions = useMemo(() => {
    const seen = new Set<string>()
    return runEntries.filter(entry => {
      if (seen.has(entry.groupKey)) return false
      seen.add(entry.groupKey)
      return true
    }).map(entry => ({
      value: entry.groupKey,
      label: entry.groupLabel,
    }))
  }, [runEntries])

  const activeRunFolder = useMemo(() => {
    if (!selectedGroupKey) return ''

    const preferredEntry = runEntries.find(entry =>
      entry.groupKey === selectedGroupKey && entry.runFolder === selectedRunFolder
    )
    if (preferredEntry) {
      return preferredEntry.runFolder
    }

    return runEntries.find(entry => entry.groupKey === selectedGroupKey)?.runFolder || ''
  }, [runEntries, selectedGroupKey, selectedRunFolder])

  useEffect(() => {
    if (!isOpen) return

    const preferredEntry = runEntries.find(entry => entry.runFolder === selectedRunFolder)
      || runEntries[0]
      || null

    setSelectedGroupKey(preferredEntry?.groupKey || '')
    setError(null)
    setData(null)
    setCopied(false)
  }, [isOpen, selectedRunFolder, runEntries])

  useEffect(() => {
    if (!isOpen) return

    const selectionStillExists = runEntries.some(entry => entry.groupKey === selectedGroupKey)
    if (selectionStillExists) return

    const preferredEntry = runEntries.find(entry => entry.groupKey === selectedGroupKey) || runEntries[0] || null

    setSelectedGroupKey(preferredEntry?.groupKey || '')
  }, [isOpen, runEntries, selectedGroupKey])

  const loadOutput = useCallback(async (runFolder: string) => {
    if (!workspacePath || !runFolder) return

    setLoading(true)
    setError(null)
    try {
      const response = await agentApi.getFinalOutput(workspacePath, runFolder)
      setData(response)
    } catch (err) {
      console.error('Failed to load final output:', err)
      setError(extractErrorMessage(err, 'Failed to load final output'))
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

  const handleGenerate = useCallback(async () => {
    if (!workspacePath || !activeRunFolder) return

    setGenerating(true)
    setError(null)
    try {
      if (onRunReport) {
        await onRunReport(activeRunFolder)
        onClose()
        return
      }

      const response = await agentApi.generateFinalOutput(workspacePath, activeRunFolder, workflowTitle)
      setData(response)
    } catch (err) {
      console.error('Failed to generate final output:', err)
      setError(extractErrorMessage(err, 'Failed to generate final output'))
    } finally {
      setGenerating(false)
    }
  }, [activeRunFolder, onClose, onRunReport, workspacePath, workflowTitle])

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
  const archiveGroupFolderName = (activeRunFolder.split('/').pop() || '').trim()
  const archivePathHint = data?.output_path || (archiveGroupFolderName
    ? `reports/${archiveGroupFolderName}/<timestamp>.md`
    : `reports/${'<group>'}/<timestamp>.md`)
  const latestArchiveLabel = data?.output_path?.split('/').pop() || ''

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-6xl max-h-[90vh] flex flex-col border border-border relative overflow-hidden">
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <div className="min-w-0">
            <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <FileText className="w-5 h-5 text-primary" />
              Final Report
            </h2>
            <p className="text-sm text-muted-foreground mt-1">
              Generate from the latest available run for a group and review that group's latest archived report.
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
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Group</span>
            <select
              value={selectedGroupKey}
              onChange={(e) => {
                setSelectedGroupKey(e.target.value)
              }}
              className="min-w-[280px] rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground"
            >
              {!selectedGroupKey && <option value="">Select group</option>}
              {groupOptions.map(group => (
                <option key={group.value} value={group.value}>{group.label}</option>
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
              {generating ? 'Starting...' : 'Run Report'}
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
                Archive: {archivePathHint}
              </div>
              {latestArchiveLabel && (
                <div className="text-xs text-muted-foreground mt-1">
                  Latest archived file: {latestArchiveLabel}
                </div>
              )}
            </div>
            <div className={`text-xs px-2 py-1 rounded-md ${isConfigEnabled ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300'}`}>
              {isConfigEnabled ? 'Enabled' : 'Disabled'}
            </div>
          </div>
          {config?.instructions?.trim() && (
            <div className="mt-3 rounded-lg border border-border bg-muted/30 p-3">
              <MarkdownRenderer
                content={config.instructions.trim()}
                className="max-w-none !text-xs [&_p]:!text-xs [&_li]:!text-xs [&_h1]:!text-sm [&_h2]:!text-xs [&_h3]:!text-xs [&_code]:!text-[11px]"
                maxHeight="160px"
                showScrollbar={true}
              />
            </div>
          )}
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto p-6">
          {!workspacePath && (
            <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
              Select a workflow workspace to view final outputs.
            </div>
          )}

          {workspacePath && !runEntries.length && (
            <div className="h-full flex items-center justify-center">
              <div className="max-w-md text-center space-y-3">
                <AlertCircle className="w-8 h-8 text-muted-foreground mx-auto" />
                <div className="text-sm font-medium text-foreground">No group runs found</div>
                <div className="text-sm text-muted-foreground">
                  Final output is generated from a completed group run and archived under {`reports/${'<group>'}/`}. Start a workflow run first, then come back here to review it.
                </div>
              </div>
            </div>
          )}

          {workspacePath && runEntries.length > 0 && !hasGroupScopedSelection && (
            <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
              Select a group to view its latest archived report.
            </div>
          )}

          {workspacePath && hasGroupScopedSelection && !isConfigEnabled && (
            <div className="h-full flex items-center justify-center">
              <div className="max-w-md text-center space-y-3">
                <AlertCircle className="w-8 h-8 text-yellow-600 dark:text-yellow-400 mx-auto" />
                <div className="text-sm font-medium text-foreground">Report plan is missing or disabled</div>
                <div className="text-sm text-muted-foreground">
                  Define the single report step in `planning/output_plan.json`, validate it with `validate_report_plan`, then completed group runs can generate this artifact automatically.
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
                <div className="text-sm font-medium text-foreground">No archived report generated yet</div>
                <div className="text-sm text-muted-foreground">
                  Generate the markdown artifact using the latest available run for this group, currently <span className="font-mono">{activeRunFolder}</span>. It will be archived under <span className="font-mono">{archivePathHint}</span>.
                </div>
              </div>
            </div>
          )}

          {workspacePath && hasGroupScopedSelection && isConfigEnabled && !loading && !error && hasContent && (
            <div className="rounded-lg border border-border bg-card p-4 overflow-hidden">
              <MarkdownRenderer
                content={data?.content || ''}
                className="max-w-none"
                maxHeight="calc(90vh - 320px)"
                showScrollbar={true}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export default FinalOutputPopup
