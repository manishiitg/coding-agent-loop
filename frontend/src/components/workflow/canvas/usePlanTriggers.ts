import { useCallback, useEffect, useState } from 'react'
import { schedulerApi } from '../../../api/scheduler'
import type { ScheduledJob } from '../../../services/api-types'
import { useWorkflowManifestStore } from '../../../stores/useWorkflowManifestStore'

export function usePlanTriggers(workspacePath: string | null, disabled = false) {
  const [revision, setRevision] = useState(0)
  const manifestRevision = useWorkflowManifestStore(state => state.lastRefreshed)
  const [snapshot, setSnapshot] = useState<{ workspace: string | null; jobs: ScheduledJob[]; error?: string }>({ workspace: null, jobs: [] })
  const refresh = useCallback(() => setRevision(value => value + 1), [])
  useEffect(() => {
    if (!workspacePath || disabled) return
    let cancelled = false
    let busy = false
    const load = async () => {
      if (busy) return
      busy = true
      try {
        const jobs: ScheduledJob[] = []
        let offset = 0
        while (!cancelled) {
          const response = await schedulerApi.listJobs({ entity_type: 'workflow', limit: 100, offset })
          jobs.push(...(response.jobs || []))
          offset += response.jobs?.length || 0
          if (!response.jobs?.length || offset >= response.total) break
        }
        if (!cancelled) setSnapshot({ workspace: workspacePath, jobs: jobs.filter(job => job.workspace_path?.replace(/\/+$/, '') === workspacePath.replace(/\/+$/, '')) })
      } catch {
        if (!cancelled) setSnapshot(previous => ({ workspace: workspacePath, jobs: previous.workspace === workspacePath ? previous.jobs : [], error: 'Could not refresh triggers' }))
      } finally { busy = false }
    }
    void load()
    const interval = window.setInterval(() => { if (!document.hidden) void load() }, 15000)
    return () => { cancelled = true; window.clearInterval(interval) }
  }, [workspacePath, disabled, revision, manifestRevision])
  return { jobs: snapshot.workspace === workspacePath ? snapshot.jobs : [], error: snapshot.workspace === workspacePath ? snapshot.error : undefined, loading: snapshot.workspace !== workspacePath, refresh }
}
