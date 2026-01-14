import { useState, useEffect, useCallback, useRef } from 'react'
import { agentApi } from '../../../services/api'
import type { WorkspaceState } from '../../../services/api-types'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'

export interface UseWorkspaceStateReturn {
  state: WorkspaceState | null
  loading: boolean
  error: string | null
  isRetrying: boolean
  retryCountdown: number | null
  refresh: () => Promise<void>
}

/**
 * Hook to load all workspace state in a single API call
 * Replaces multiple individual API calls (getRunFolders, getVariableGroups, constants, progress)
 *
 * Benefits:
 * - Single atomic load - all data arrives together
 * - No race conditions - consistent state guaranteed
 * - One re-render instead of 6-7
 * - Backend parallelizes I/O operations
 * - ~70% reduction in network overhead
 *
 * Optimization:
 * - When workspace changes: full reload (folders, manifest, phases, progress)
 * - When only folder changes: only reload progress (not the entire workspace state)
 *
 * @param workspacePath - The workspace path (e.g., "Workflow/ICICI Bank Parsing - v2")
 * @param selectedFolder - Optional selected run folder (e.g., "iteration-4/manishiithug")
 */
export function useWorkspaceState(
  workspacePath: string | null,
  selectedFolder?: string | null
): UseWorkspaceStateReturn {
  const [state, setState] = useState<WorkspaceState | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [isRetrying, setIsRetrying] = useState(false)
  const [retryCountdown, setRetryCountdown] = useState<number | null>(null)

  // Track previous values to prevent unnecessary reloads
  const prevWorkspaceRef = useRef<string | null>(null)
  const prevFolderRef = useRef<string | null>(null)
  // Track if initial load for this workspace has completed
  const initialLoadCompleteRef = useRef<boolean>(false)
  const loadingRef = useRef<boolean>(false)
  // Track retry timeout to allow cleanup
  const retryTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  // Track countdown interval
  const countdownIntervalRef = useRef<NodeJS.Timeout | null>(null)
  // Track countdown value in ref for interval callback
  const countdownRef = useRef<number>(0)
  // Ref to store the latest loadFull function to avoid circular dependencies
  const loadFullRef = useRef<(() => Promise<void>) | null>(null)

  // Helper to clear countdown
  const clearCountdown = useCallback(() => {
    if (countdownIntervalRef.current) {
      clearInterval(countdownIntervalRef.current)
      countdownIntervalRef.current = null
    }
    setRetryCountdown(null)
  }, [])

  // Helper to schedule retry after 5 seconds
  const scheduleRetry = useCallback(() => {
    // Clear any existing retry timeout and countdown
    if (retryTimeoutRef.current) {
      clearTimeout(retryTimeoutRef.current)
      retryTimeoutRef.current = null
    }
    clearCountdown()

    // Only schedule retry if we have a workspace path
    if (!workspacePath) {
      setIsRetrying(false)
      return
    }

    console.log('[useWorkspaceState] Scheduling retry in 5 seconds...')
    setIsRetrying(true)
    countdownRef.current = 5
    setRetryCountdown(5)

    // Start countdown using ref for accurate updates
    countdownIntervalRef.current = setInterval(() => {
      countdownRef.current -= 1
      if (countdownRef.current <= 0) {
        clearCountdown()
      } else {
        setRetryCountdown(countdownRef.current)
      }
    }, 1000)

    retryTimeoutRef.current = setTimeout(() => {
      console.log('[useWorkspaceState] Retrying workspace state load...')
      retryTimeoutRef.current = null
      clearCountdown()
      setIsRetrying(false) // Will be set to true again if this retry fails
      // Use ref to call the latest loadFull function
      if (loadFullRef.current) {
        loadFullRef.current()
      }
    }, 5000) // 5 seconds
  }, [workspacePath, clearCountdown])

  // Full load - gets everything from backend
  const loadFull = useCallback(async () => {
    if (!workspacePath) {
      setState(null)
      setError(null)
      initialLoadCompleteRef.current = false
      // Clear any pending retry
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current)
        retryTimeoutRef.current = null
      }
      clearCountdown()
      setIsRetrying(false)
      return
    }

    // Prevent concurrent loads
    if (loadingRef.current) {
      console.log('[useWorkspaceState] Load already in progress, skipping')
      return
    }

    // Clear any pending retry since we're starting a new load
    if (retryTimeoutRef.current) {
      clearTimeout(retryTimeoutRef.current)
      retryTimeoutRef.current = null
    }
    clearCountdown()
    setIsRetrying(false)

    console.log('[useWorkspaceState] Loading workspace state:', {
      workspacePath,
      selectedFolder
    })

    loadingRef.current = true
    setLoading(true)
    setError(null)

    try {
      // ONE API CALL - gets everything
      const response = await agentApi.loadWorkspaceState(workspacePath, selectedFolder)

      console.log('[useWorkspaceState] Response:', {
        success: response.success,
        hasData: !!response.data,
        runFoldersCount: response.data?.run_folders?.length || 0,
        hasManifest: !!response.data?.variables_manifest,
        groupsCount: response.data?.variables_manifest?.groups?.length || 0,
        phasesCount: response.data?.phases?.length || 0
      })

      if (response.success && response.data) {
        setState(response.data)

        // Update workflow store atomically
        const workflowStore = useWorkflowStore.getState()

        // Set run folders
        const folders = response.data.run_folders.map(f => ({
          name: f.name,
          progress: f.progress || undefined
        }))
        console.log('[useWorkspaceState] Setting runFolders:', folders.length)
        workflowStore.setRunFolders(folders)

        // Set variables manifest
        workflowStore.setVariablesManifest(response.data.variables_manifest || null)

        // Set phases
        workflowStore.setPhases(response.data.phases)

        // Mark initial load as complete
        initialLoadCompleteRef.current = true
        console.log('[useWorkspaceState] ✅ Successfully loaded and updated store')

        // Restore selection state from localStorage AFTER API data is loaded
        // This ensures localStorage values take precedence over any resets during loading
        workflowStore.restoreSelectionFromLocalStorage()

        // Check if folder changed during the load (e.g., loadSavedSettings restored a different folder)
        const currentFolder = prevFolderRef.current
        const folderChangedDuringLoad = currentFolder && currentFolder !== 'new' && currentFolder !== selectedFolder

        if (folderChangedDuringLoad) {
          // Folder changed during load - load progress for the current (correct) folder
          console.log('[useWorkspaceState] Folder changed during load, loading progress for:', currentFolder)
          workflowStore.loadProgress(workspacePath, currentFolder)
        } else if (response.data.selected_progress) {
          // No folder change - use the progress from the response
          workflowStore.setStepProgress(response.data.selected_progress)
        } else {
          // No folder change and no progress in response - clear stale progress
          workflowStore.setStepProgress(null)
        }

        // Success - clear any pending retry
        if (retryTimeoutRef.current) {
          clearTimeout(retryTimeoutRef.current)
          retryTimeoutRef.current = null
        }
        clearCountdown()
        setIsRetrying(false)
      } else {
        const errorMsg = response.error || 'Failed to load workspace state'
        setError(errorMsg)
        console.error('[useWorkspaceState] ❌ Failed:', errorMsg)
        // Schedule retry after 5 seconds
        scheduleRetry()
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error'
      setError(errorMsg)
      console.error('[useWorkspaceState] ❌ Exception:', err)
      // Schedule retry after 5 seconds
      scheduleRetry()
    } finally {
      loadingRef.current = false
      setLoading(false)
    }
  }, [workspacePath, selectedFolder, scheduleRetry, clearCountdown])

  // Store the latest loadFull function in a ref
  useEffect(() => {
    loadFullRef.current = loadFull
  }, [loadFull])

  // Auto-load when workspace or selected folder changes
  useEffect(() => {
    // Check if workspace or folder actually changed
    const workspaceChanged = prevWorkspaceRef.current !== workspacePath
    const folderChanged = prevFolderRef.current !== selectedFolder

    // Update refs
    prevWorkspaceRef.current = workspacePath
    prevFolderRef.current = selectedFolder || null

    if (!workspacePath) {
      setState(null)
      setError(null)
      initialLoadCompleteRef.current = false
      // Clear any pending retry when workspace is cleared
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current)
        retryTimeoutRef.current = null
      }
      clearCountdown()
      setIsRetrying(false)
      return
    }

    if (workspaceChanged) {
      // Workspace changed - clear any pending retry and reset initial load flag
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current)
        retryTimeoutRef.current = null
      }
      clearCountdown()
      setIsRetrying(false)
      initialLoadCompleteRef.current = false
      loadFull()
    } else if (folderChanged && selectedFolder && selectedFolder !== 'new') {
      // Only folder changed - only load progress if initial load is complete
      // Otherwise, the full load will get the correct progress anyway
      if (initialLoadCompleteRef.current && !loadingRef.current) {
        console.log('[useWorkspaceState] Folder changed, loading progress only:', {
          workspacePath,
          selectedFolder
        })
        const workflowStore = useWorkflowStore.getState()
        workflowStore.loadProgress(workspacePath, selectedFolder)
      } else {
        console.log('[useWorkspaceState] Folder changed but initial load not complete, skipping progress-only load')
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspacePath, selectedFolder]) // Don't include loadFull to prevent unnecessary re-runs

  // Cleanup retry timeout and countdown on unmount
  useEffect(() => {
    return () => {
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current)
        retryTimeoutRef.current = null
      }
      clearCountdown()
    }
  }, [clearCountdown])

  return {
    state,
    loading,
    error,
    isRetrying,
    retryCountdown,
    refresh: loadFull
  }
}

export default useWorkspaceState
