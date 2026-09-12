import React, { useCallback, useEffect, useMemo } from 'react'
import { Loader2 } from 'lucide-react'
import { useAppStore } from '../stores/useAppStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { openWorkflowPresetPage } from '../utils/workflowSessionRestore'
import { OrgDashboard } from './org/OrgDashboard'

/**
 * Activity is intentionally a summary-only surface. Workflow editing,
 * execution details, reports, logs, and costs belong to the workflow workspace.
 */
export const EmployeeDashboard: React.FC = () => {
  const showWorkflowsOverview = useAppStore(state => state.showWorkflowsOverview)
  const workflowPresets = useGlobalPresetStore(state => state.workflowPresets)
  const workflowPresetsLoaded = useGlobalPresetStore(state => state.workflowPresetsLoaded)
  const presetsLoading = useGlobalPresetStore(state => state.loading)
  const refreshPresets = useGlobalPresetStore(state => state.refreshPresets)

  useEffect(() => {
    if (!showWorkflowsOverview || workflowPresetsLoaded || presetsLoading) return
    refreshPresets().catch(error => {
      console.error('[EmployeeDashboard] Failed to refresh workflow presets:', error)
    })
  }, [presetsLoading, refreshPresets, showWorkflowsOverview, workflowPresetsLoaded])

  const workflows = useMemo(() => workflowPresets
    .flatMap(preset => {
      const workspacePath = preset.selectedFolder?.filepath
      if (!workspacePath) return []
      return [{
        workspacePath,
        label: preset.label || workspacePath.split('/').filter(Boolean).pop() || workspacePath,
      }]
    })
    .sort((a, b) => a.label.localeCompare(b.label)), [workflowPresets])

  const handleOpenWorkflow = useCallback((workspacePath: string) => {
    const preset = workflowPresets.find(item => item.selectedFolder?.filepath === workspacePath)
    if (!preset) return
    void openWorkflowPresetPage(preset, {
      title: preset.label,
      source: 'org-dashboard-decision',
    }).catch(error => {
      console.error('[EmployeeDashboard] Failed to open workflow:', error)
    })
  }, [workflowPresets])

  if (!workflowPresetsLoaded && presetsLoading && workflows.length === 0) {
    return <div className="flex h-full min-h-[320px] items-center justify-center text-muted-foreground"><Loader2 className="mr-2 h-5 w-5 animate-spin" />Loading activity…</div>
  }

  return <div className="h-full min-h-0 overflow-hidden bg-background"><OrgDashboard embedded workflows={workflows} onOpenWorkflow={handleOpenWorkflow} /></div>
}

export default EmployeeDashboard
